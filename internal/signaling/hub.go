package signaling

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/Alice088/worm/internal/auth"
	"github.com/Alice088/worm/internal/call"
	"github.com/Alice088/worm/internal/metrics"
	"github.com/Alice088/worm/internal/protocol"
)

const Subprotocol = "worm.v1"

type Config struct {
	Auth    auth.Authenticator
	Manager *call.Manager
	Metrics *metrics.Metrics
	Logger  *slog.Logger

	SendQueue      int
	PendingQueue   int
	ReadLimit      int64
	WriteTimeout   time.Duration
	IdleTimeout    time.Duration
	MessageRate    float64
	MessageBurst   int
	CreateRate     float64
	CreateBurst    int
	ReconnectGrace time.Duration
	OriginPatterns []string
	InsecureOrigin bool
}

func (c *Config) withDefaults() {
	if c.SendQueue <= 0 {
		c.SendQueue = 64
	}
	if c.PendingQueue <= 0 {
		c.PendingQueue = 64
	}
	if c.ReadLimit <= 0 {
		c.ReadLimit = protocol.MaxMessageBytes
	}
	if c.WriteTimeout <= 0 {
		c.WriteTimeout = 10 * time.Second
	}
	if c.IdleTimeout <= 0 {
		c.IdleTimeout = 60 * time.Second
	}
	if c.MessageRate <= 0 {
		c.MessageRate = 40
	}
	if c.MessageBurst <= 0 {
		c.MessageBurst = 60
	}
	if c.CreateRate <= 0 {
		c.CreateRate = 0.1
	}
	if c.CreateBurst <= 0 {
		c.CreateBurst = 5
	}
	if c.ReconnectGrace <= 0 {
		c.ReconnectGrace = 20 * time.Second
	}
	if c.Logger == nil {
		c.Logger = slog.Default()
	}
}

type Server struct {
	cfg Config

	mu       sync.Mutex
	sessions map[string]*session
	pending  map[string][]protocol.Envelope
	absent   map[string]time.Time
	closed   bool
}

func New(cfg Config) (*Server, error) {
	if cfg.Auth == nil {
		return nil, errors.New("signaling: auth is required")
	}
	if cfg.Manager == nil {
		return nil, errors.New("signaling: call manager is required")
	}
	if cfg.Metrics == nil {
		cfg.Metrics = metrics.New()
	}
	cfg.withDefaults()

	return &Server{
		cfg:      cfg,
		sessions: make(map[string]*session),
		pending:  make(map[string][]protocol.Envelope),
		absent:   make(map[string]time.Time),
	}, nil
}

func (s *Server) Manager() *call.Manager { return s.cfg.Manager }

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	token := bearerToken(r)
	if token == "" {
		http.Error(w, "missing token", http.StatusUnauthorized)
		return
	}
	identity, err := s.cfg.Auth.Verify(token)
	if err != nil {
		http.Error(w, "invalid token", http.StatusUnauthorized)
		return
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		Subprotocols:       []string{Subprotocol},
		OriginPatterns:     s.cfg.OriginPatterns,
		InsecureSkipVerify: s.cfg.InsecureOrigin,
	})
	if err != nil {
		s.cfg.Logger.Warn("websocket accept failed", "user_id", identity.ID, "error", err.Error())
		return
	}
	conn.SetReadLimit(s.cfg.ReadLimit)

	sess := newSession(s, conn, identity)
	s.register(sess)
	defer s.unregister(sess)

	go sess.writeLoop()
	sess.readLoop(r.Context())
}

func bearerToken(r *http.Request) string {
	if h := r.Header.Get("Authorization"); h != "" {
		if len(h) > 7 && (h[:7] == "Bearer " || h[:7] == "bearer ") {
			return h[7:]
		}
	}
	return r.URL.Query().Get("token")
}

func (s *Server) register(sess *session) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		_ = sess.conn.Close(websocket.StatusGoingAway, "server shutting down")
		return
	}
	old := s.sessions[sess.id.ID]
	_, wasAbsent := s.absent[sess.id.ID]
	s.sessions[sess.id.ID] = sess
	delete(s.absent, sess.id.ID)
	s.mu.Unlock()

	if wasAbsent {
		s.cfg.Metrics.ReconnectAttempts.WithLabelValues("ok").Inc()
	}

	if old != nil {
		old.shutdown("replaced by a newer connection")
	}
	s.cfg.Metrics.ActiveSockets.Inc()
	s.cfg.Logger.Info("signaling connected", "user_id", sess.id.ID)

	s.flushPending(sess)
}

func (s *Server) unregister(sess *session) {
	s.cfg.Metrics.ReconnectAttempts.WithLabelValues("failed").Inc()
	s.mu.Lock()
	if s.sessions[sess.id.ID] == sess {
		delete(s.sessions, sess.id.ID)
		s.absent[sess.id.ID] = time.Now()
	}
	s.cfg.Metrics.ActiveSockets.Dec()
	s.mu.Unlock()
	sess.shutdown("connection closed")
	s.cfg.Logger.Info("signaling disconnected", "user_id", sess.id.ID)
}

func (s *Server) sessionFor(userID string) *session {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sessions[userID]
}

func (s *Server) online(userID string) bool {
	return s.sessionFor(userID) != nil
}

func (s *Server) send(userID string, env protocol.Envelope) bool {
	sess := s.sessionFor(userID)
	if sess == nil {
		return false
	}
	return sess.send(env)
}

func (s *Server) sendOrBuffer(userID string, env protocol.Envelope) {
	if s.send(userID, env) {
		return
	}
	s.buffer(userID, env)
}

func (s *Server) buffer(userID string, env protocol.Envelope) {
	if !isBufferable(env.Type) {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	queue := s.pending[userID]
	if len(queue) >= s.cfg.PendingQueue {
		queue = queue[1:]
		s.cfg.Metrics.SignalingDropped.Inc()
	}
	s.pending[userID] = append(queue, env)
}

func (s *Server) flushPending(sess *session) {
	s.mu.Lock()
	queue := s.pending[sess.id.ID]
	delete(s.pending, sess.id.ID)
	s.mu.Unlock()

	for _, env := range queue {
		if !sess.send(env) {
			return
		}
	}
}

func isBufferable(t protocol.Type) bool {
	switch t {
	case protocol.TypeWebRTCOffer, protocol.TypeWebRTCAnswer,
		protocol.TypeWebRTCICECandidate, protocol.TypeWebRTCICEComplete,
		protocol.TypeWebRTCRenegotiate:
		return true
	}
	return false
}

func (s *Server) broadcastState(snap call.Snapshot) {
	if snap.Callee.ID == "" && snap.State == call.StateRinging {
		if env, err := s.stateEnvelope(snap, snap.Caller.ID); err == nil {
			s.send(snap.Caller.ID, env)
		}
		return
	}
	for _, member := range snap.Members() {
		env, err := s.stateEnvelope(snap, member)
		if err != nil {
			continue
		}
		s.send(member, env)
	}
}

func (s *Server) stateEnvelope(snap call.Snapshot, userID string) (protocol.Envelope, error) {
	payload := statePayload(snap, userID)
	raw, err := marshalPayload(payload)
	if err != nil {
		return protocol.Envelope{}, err
	}
	return protocol.Envelope{
		Version: protocol.Version,
		Type:    protocol.TypeCallState,
		CallID:  snap.ID,
		Payload: raw,
	}, nil
}

func (s *Server) incomingEnvelope(snap call.Snapshot, userID string) (protocol.Envelope, error) {
	raw, err := marshalPayload(statePayload(snap, userID))
	if err != nil {
		return protocol.Envelope{}, err
	}
	return protocol.Envelope{
		Version: protocol.Version,
		Type:    protocol.TypeCallIncoming,
		CallID:  snap.ID,
		Payload: raw,
	}, nil
}

func (s *Server) Sweep(ctx context.Context) {
	snapshots := s.cfg.Manager.Sweep()
	for _, snap := range snapshots {
		s.recordTerminal(snap)
		s.broadcastState(snap)
	}
	s.cfg.Metrics.CallsActive.Set(float64(s.cfg.Manager.Active()))

	s.mu.Lock()
	for userID, at := range s.absent {
		if s.sessions[userID] != nil || time.Since(at) > 10*time.Minute {
			delete(s.absent, userID)
		}
	}
	s.mu.Unlock()

	for _, snap := range s.cfg.Manager.ActiveSnapshots() {
		if snap.State == call.StateConnected {
			continue
		}
		for _, member := range snap.Members() {
			if s.online(member) {
				continue
			}
			since, ok := s.absence(member)
			if !ok || time.Since(since) < s.cfg.ReconnectGrace {
				continue
			}
			failed, err := s.cfg.Manager.Fail(snap.ID, "signaling_unavailable")
			if err != nil {
				continue
			}
			s.cfg.Logger.InfoContext(ctx, "call failed: participant absent",
				"call_id", failed.ID, "user_id", member)
			s.recordTerminal(failed)
			s.broadcastState(failed)
		}
	}
}

func (s *Server) absence(userID string) (time.Time, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.absent[userID]
	return t, ok
}

func (s *Server) recordTerminal(snap call.Snapshot) {
	if !snap.State.Terminal() {
		return
	}
	setup := 0.0
	if !snap.ConnectedAt.IsZero() {
		setup = snap.ConnectedAt.Sub(snap.CreatedAt).Seconds()
	}
	s.cfg.Metrics.ObserveTerminal(string(snap.State), setup)
}

func statePayload(snap call.Snapshot, userID string) protocol.CallStatePayload {
	payload := protocol.CallStatePayload{
		CallID: snap.ID,
		State:  string(snap.State),
		Reason: snap.Reason,
	}
	if self, role, ok := snap.Participant(userID); ok {
		payload.Role = string(role)
		payload.Self = protocol.Party{ID: self.ID, Name: self.Name}
	}
	if peer, ok := snap.Peer(userID); ok {
		payload.Peer = protocol.Party{ID: peer.ID, Name: peer.Name}
	}
	if role, ok := snap.RoleOf(userID); ok && role == call.RoleCaller {
		payload.Invite = snap.Invite
	}
	if !snap.Deadline.IsZero() {
		deadline := snap.Deadline
		payload.Deadline = &deadline
	}
	if !snap.ConnectedAt.IsZero() {
		connected := snap.ConnectedAt
		payload.Connected = &connected
	}
	return payload
}

func (s *Server) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	sessions := make([]*session, 0, len(s.sessions))
	for _, sess := range s.sessions {
		sessions = append(sessions, sess)
	}
	s.sessions = make(map[string]*session)
	s.mu.Unlock()

	for _, sess := range sessions {
		sess.shutdown("server shutting down")
	}
}

func (s *Server) Shutdown(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		s.Close()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
