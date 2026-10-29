package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Alice088/worm/internal/auth"
	"github.com/Alice088/worm/internal/call"
	"github.com/Alice088/worm/internal/metrics"
	"github.com/Alice088/worm/internal/protocol"
	"github.com/Alice088/worm/internal/signaling"
	"github.com/Alice088/worm/internal/turnserver"
	"golang.org/x/time/rate"
)

type App struct {
	cfg       Config
	log       *slog.Logger
	metrics   *metrics.Metrics
	issuer    *auth.Issuer
	calls     *call.Manager
	signaling *signaling.Server
	turnCreds *turnserver.CredentialIssuer
	turnSrv   *turnserver.Server
	limiter   *ipLimiter
	handler   http.Handler
	httpSrv   *http.Server

	started  time.Time
	stop     chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup
}

func New(cfg Config, logger *slog.Logger) (*App, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if logger == nil {
		logger = slog.Default()
	}

	metricSet := metrics.New()
	issuer, err := auth.NewIssuer([]byte(cfg.SigningSecret), cfg.TokenTTL)
	if err != nil {
		return nil, fmt.Errorf("auth: %w", err)
	}
	manager, err := call.NewManager(cfg.Call, []byte(cfg.SigningSecret))
	if err != nil {
		return nil, err
	}
	sigs, err := signaling.New(signaling.Config{
		Auth:           issuer,
		Manager:        manager,
		Metrics:        metricSet,
		Logger:         logger,
		SendQueue:      cfg.Signaling.SendQueue,
		PendingQueue:   cfg.Signaling.PendingQueue,
		ReadLimit:      cfg.Signaling.ReadLimit,
		WriteTimeout:   cfg.Signaling.WriteTimeout,
		IdleTimeout:    cfg.Signaling.IdleTimeout,
		MessageRate:    cfg.Signaling.MessageRate,
		MessageBurst:   cfg.Signaling.MessageBurst,
		CreateRate:     cfg.Signaling.CreateRate,
		CreateBurst:    cfg.Signaling.CreateBurst,
		ReconnectGrace: cfg.Signaling.ReconnectGrace,
		OriginPatterns: cfg.AllowedOrigins,
		InsecureOrigin: cfg.InsecureOrigin,
	})
	if err != nil {
		return nil, err
	}

	app := &App{
		cfg:       cfg,
		log:       logger,
		metrics:   metricSet,
		issuer:    issuer,
		calls:     manager,
		signaling: sigs,
		limiter:   newIPLimiter(rate.Limit(0.2), 5),
		stop:      make(chan struct{}),
	}

	if cfg.TURN.Enabled {
		creds, err := turnserver.NewCredentialIssuer(cfg.TURN.Secret, cfg.TURN.CredentialTTL, cfg.TURN.URLs, cfg.TURN.Realm)
		if err != nil {
			return nil, err
		}
		app.turnCreds = creds
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/session", app.handleSession)
	mux.HandleFunc("GET /v1/turn/credentials", app.handleTurnCredentials)
	mux.HandleFunc("GET /healthz", app.handleHealth)
	mux.HandleFunc("GET /readyz", app.handleReady)
	mux.Handle("GET /metrics", metricSet.Handler())
	mux.Handle("/ws", sigs)
	app.handler = app.wrap(mux)

	app.httpSrv = &http.Server{
		Addr:              cfg.Listen,
		Handler:           app.handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    32 << 10,
	}

	return app, nil
}

func (a *App) Start() error {
	a.started = time.Now()

	if a.cfg.TURN.Enabled {
		srv, err := turnserver.Start(turnserver.ServerConfig{
			ListenAddr:    a.cfg.TURN.ListenAddr,
			PublicIP:      a.cfg.TURN.PublicIP,
			Realm:         a.cfg.TURN.Realm,
			Secret:        a.cfg.TURN.Secret,
			CredentialTTL: a.cfg.TURN.CredentialTTL,
			URLs:          a.cfg.TURN.URLs,
			Hooks: turnserver.Hooks{
				AllocationCreated: a.metrics.TURNRelays.Inc,
				AllocationError:   a.metrics.TURNErrors.Inc,
			},
		})
		if err != nil {
			return err
		}
		a.turnSrv = srv
		a.log.Info("turn relay listening", "addr", srv.Addr(), "urls", srv.URLs())
	}

	a.wg.Add(1)
	go a.sweepLoop()

	return nil
}

func (a *App) sweepLoop() {
	defer a.wg.Done()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-a.stop:
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			a.signaling.Sweep(ctx)
			a.metrics.CallsActive.Set(float64(a.calls.Active()))
			a.limiter.cleanup()
			cancel()
		}
	}
}

func (a *App) Handler() http.Handler { return a.handler }

func (a *App) Serve(ln net.Listener) error {
	a.httpSrv.Addr = ln.Addr().String()
	err := a.httpSrv.Serve(ln)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (a *App) ListenAndServe() error {
	ln, err := net.Listen("tcp", a.cfg.Listen)
	if err != nil {
		return err
	}
	return a.Serve(ln)
}

func (a *App) Shutdown(ctx context.Context) error {
	a.stopOnce.Do(func() { close(a.stop) })

	var errs []error
	if err := a.httpSrv.Shutdown(ctx); err != nil {
		errs = append(errs, err)
	}
	a.signaling.Close()
	if a.turnSrv != nil {
		if err := a.turnSrv.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	a.wg.Wait()
	return errors.Join(errs...)
}

func (a *App) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *App) handleReady(w http.ResponseWriter, _ *http.Request) {
	if a.started.IsZero() {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "starting"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":        "ready",
		"active_calls":  a.calls.Active(),
		"since_seconds": int(time.Since(a.started).Seconds()),
	})
}

type sessionRequest struct {
	Name string `json:"name"`
}

type sessionResponse struct {
	UserID     string                 `json:"user_id"`
	Name       string                 `json:"name"`
	Token      string                 `json:"token"`
	ExpiresAt  time.Time              `json:"expires_at"`
	WSURL      string                 `json:"ws_url"`
	ICEServers []protocol.ICEServer   `json:"ice_servers"`
	TURN       *turnserver.Credential `json:"turn,omitempty"`
}

func (a *App) handleSession(w http.ResponseWriter, r *http.Request) {
	if !a.limiter.allow(clientIP(r)) {
		a.metrics.HTTPRequests.WithLabelValues(r.Method, "429").Inc()
		writeError(w, http.StatusTooManyRequests, "rate limit exceeded")
		return
	}

	var req sessionRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	if err := protocol.ValidateName(req.Name); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	name := strings.TrimSpace(req.Name)

	identity, token, err := a.issuer.Issue(name)
	if err != nil {
		a.log.Error("issue token", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "cannot issue token")
		return
	}

	origin := a.origin(r)
	resp := sessionResponse{
		UserID:    identity.ID,
		Name:      identity.Name,
		Token:     token,
		ExpiresAt: identity.ExpiresAt,
		WSURL:     wsURL(origin),
	}
	if a.turnCreds != nil {
		cred, err := a.turnCreds.Issue(identity.ID)
		if err != nil {
			a.metrics.TURNErrors.Inc()
			a.log.Error("issue turn credentials", "error", err.Error())
		} else {
			resp.TURN = &cred
			resp.ICEServers = []protocol.ICEServer{{URLs: cred.URLs, Username: cred.Username, Credential: cred.Password}}
			a.metrics.TURNCredentials.Inc()
		}
	}
	if len(resp.ICEServers) == 0 {
		resp.ICEServers = []protocol.ICEServer{{URLs: []string{"stun:stun.l.google.com:19302"}}}
	}

	writeJSON(w, http.StatusOK, resp)
}

func (a *App) handleTurnCredentials(w http.ResponseWriter, r *http.Request) {
	if a.turnCreds == nil {
		writeError(w, http.StatusNotFound, "turn is disabled")
		return
	}
	id, err := a.issuer.Verify(bearer(r))
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid token")
		return
	}
	cred, err := a.turnCreds.Issue(id.ID)
	if err != nil {
		a.metrics.TURNErrors.Inc()
		writeError(w, http.StatusInternalServerError, "cannot issue credentials")
		return
	}
	a.metrics.TURNCredentials.Inc()
	writeJSON(w, http.StatusOK, cred)
}

func (a *App) origin(r *http.Request) string {
	if a.cfg.PublicURL != "" {
		return a.cfg.PublicURL
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if proto := r.Header.Get("X-Forwarded-Proto"); proto == "https" || proto == "http" {
		scheme = proto
	}
	host := r.Host
	if forwarded := r.Header.Get("X-Forwarded-Host"); forwarded != "" {
		host = forwarded
	}
	return scheme + "://" + host
}

func wsURL(origin string) string {
	switch {
	case strings.HasPrefix(origin, "https://"):
		return "wss://" + strings.TrimPrefix(origin, "https://") + "/ws"
	case strings.HasPrefix(origin, "http://"):
		return "ws://" + strings.TrimPrefix(origin, "http://") + "/ws"
	}
	return origin + "/ws"
}

func bearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if len(h) > 7 && strings.EqualFold(h[:7], "bearer ") {
		return h[7:]
	}
	return r.URL.Query().Get("token")
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(data)
}

func (a *App) wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w}

		defer func() {
			if rec := recover(); rec != nil {
				a.log.Error("http panic", "path", r.URL.Path, "panic", fmt.Sprint(rec))
				if sw.status == 0 {
					writeError(sw, http.StatusInternalServerError, "internal error")
				}
			}
			status := sw.status
			if status == 0 {
				status = http.StatusOK
			}
			a.metrics.HTTPRequests.WithLabelValues(r.Method, statusClass(status)).Inc()
			a.log.Debug("http request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", status,
				"duration_ms", time.Since(start).Milliseconds(),
			)
		}()

		sw.Header().Set("X-Content-Type-Options", "nosniff")
		next.ServeHTTP(sw, r)
	})
}

func statusClass(status int) string {
	switch {
	case status >= 500:
		return "5xx"
	case status >= 400:
		return "4xx"
	case status >= 300:
		return "3xx"
	default:
		return "2xx"
	}
}

type ipLimiter struct {
	mu      sync.Mutex
	entries map[string]*ipEntry
	rate    rate.Limit
	burst   int
}

type ipEntry struct {
	limiter *rate.Limiter
	seen    time.Time
}

func newIPLimiter(limit rate.Limit, burst int) *ipLimiter {
	return &ipLimiter{entries: make(map[string]*ipEntry), rate: limit, burst: burst}
}

func (l *ipLimiter) allow(ip string) bool {
	if ip == "" {
		return true
	}
	l.mu.Lock()
	entry, ok := l.entries[ip]
	if !ok {
		entry = &ipEntry{limiter: rate.NewLimiter(l.rate, l.burst)}
		l.entries[ip] = entry
	}
	entry.seen = time.Now()
	l.mu.Unlock()
	return entry.limiter.Allow()
}

func (l *ipLimiter) cleanup() {
	cutoff := time.Now().Add(-10 * time.Minute)
	l.mu.Lock()
	defer l.mu.Unlock()
	for ip, entry := range l.entries {
		if entry.seen.Before(cutoff) {
			delete(l.entries, ip)
		}
	}
}
