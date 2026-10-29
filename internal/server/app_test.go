package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func testConfig() Config {
	cfg := DefaultConfig()
	cfg.SigningSecret = "server-test-secret-0123456789"
	cfg.TURN.Secret = "turn-test-secret-0123456789"
	cfg.TURN.URLs = []string{"turn:127.0.0.1:3478"}
	return cfg
}

func newTestApp(t *testing.T) *App {
	t.Helper()
	app, err := New(testConfig(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = app.Shutdown(ctx)
	})
	return app
}

func postSession(t *testing.T, handler http.Handler, body string) (*httptest.ResponseRecorder, sessionResponse) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/session", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	var resp sessionResponse
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode session: %v", err)
		}
	}
	return rec, resp
}

func TestSessionEndpointIssuesTokenAndIceServers(t *testing.T) {
	app := newTestApp(t)

	rec, resp := postSession(t, app.Handler(), `{"name":"alice"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if resp.Token == "" || resp.UserID == "" {
		t.Fatalf("incomplete session: %+v", resp)
	}
	if !strings.HasSuffix(resp.WSURL, "/ws") {
		t.Fatalf("unexpected ws url %q", resp.WSURL)
	}
	if len(resp.ICEServers) == 0 || len(resp.ICEServers[0].URLs) == 0 {
		t.Fatalf("missing ice servers: %+v", resp.ICEServers)
	}
	if resp.TURN == nil || resp.TURN.Username == "" {
		t.Fatalf("missing turn credentials: %+v", resp.TURN)
	}
	if !resp.ExpiresAt.After(time.Now()) {
		t.Fatal("token already expired")
	}
}

func TestSessionEndpointRejectsInvalidName(t *testing.T) {
	app := newTestApp(t)

	rec, _ := postSession(t, app.Handler(), `{"name":""}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}

	rec, _ = postSession(t, app.Handler(), `not-json`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for malformed json, got %d", rec.Code)
	}
}

func TestSessionRateLimit(t *testing.T) {
	app := newTestApp(t)
	app.limiter = newIPLimiter(0, 1)

	if rec, _ := postSession(t, app.Handler(), `{"name":"a"}`); rec.Code != http.StatusOK {
		t.Fatalf("first request: %d", rec.Code)
	}
	if rec, _ := postSession(t, app.Handler(), `{"name":"b"}`); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", rec.Code)
	}
}

func TestTurnCredentialsEndpoint(t *testing.T) {
	app := newTestApp(t)
	_, session := postSession(t, app.Handler(), `{"name":"alice"}`)

	req := httptest.NewRequest(http.MethodGet, "/v1/turn/credentials", nil)
	rec := httptest.NewRecorder()
	app.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without token, got %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/turn/credentials", nil)
	req.Header.Set("Authorization", "Bearer "+session.Token)
	rec = httptest.NewRecorder()
	app.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var cred CredentialResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &cred); err != nil {
		t.Fatal(err)
	}
	if cred.Username == "" || cred.Password == "" {
		t.Fatalf("incomplete credential: %+v", cred)
	}
}

type CredentialResponse struct {
	URLs      []string  `json:"urls"`
	Username  string    `json:"username"`
	Password  string    `json:"password"`
	ExpiresAt time.Time `json:"expires_at"`
}

func TestTurnDisabled(t *testing.T) {
	cfg := testConfig()
	cfg.TURN.Enabled = false
	app, err := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/turn/credentials", nil)
	rec := httptest.NewRecorder()
	app.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}

	_, session := postSession(t, app.Handler(), `{"name":"alice"}`)
	if len(session.ICEServers) != 1 || !strings.HasPrefix(session.ICEServers[0].URLs[0], "stun:") {
		t.Fatalf("expected stun fallback, got %+v", session.ICEServers)
	}
}

func TestMetricsEndpoint(t *testing.T) {
	app := newTestApp(t)
	postSession(t, app.Handler(), `{"name":"alice"}`)

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	app.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("metrics status %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"worm_call_attempts_total", "worm_websockets_active", "go_goroutines"} {
		if !strings.Contains(body, want) {
			t.Fatalf("metrics missing %q", want)
		}
	}
}

func TestHealthAndReady(t *testing.T) {
	app := newTestApp(t)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	app.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("healthz status %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec = httptest.NewRecorder()
	app.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("readyz before start should be 503, got %d", rec.Code)
	}
}

func TestWSRequiresToken(t *testing.T) {
	app := newTestApp(t)

	req := httptest.NewRequest(http.MethodGet, "/ws", nil)
	rec := httptest.NewRecorder()
	app.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/ws?token=bogus", nil)
	rec = httptest.NewRecorder()
	app.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for bogus token, got %d", rec.Code)
	}
}

func TestPublicURLUsedForWS(t *testing.T) {
	cfg := testConfig()
	cfg.PublicURL = "https://calls.example.com"
	app, err := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}

	_, session := postSession(t, app.Handler(), `{"name":"alice"}`)
	if session.WSURL != "wss://calls.example.com/ws" {
		t.Fatalf("unexpected ws url %q", session.WSURL)
	}
}

func TestSessionResponseIsNotCached(t *testing.T) {
	app := newTestApp(t)
	rec, _ := postSession(t, app.Handler(), `{"name":"alice"}`)
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("unexpected cache header %q", got)
	}
}

func TestStartAndShutdown(t *testing.T) {
	cfg := testConfig()
	cfg.TURN.Enabled = false
	cfg.Listen = "127.0.0.1:0"
	app, err := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if err := app.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	app.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("readyz after start: %d %s", rec.Code, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"ready"`)) {
		t.Fatalf("unexpected ready body: %s", rec.Body.String())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := app.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
}
