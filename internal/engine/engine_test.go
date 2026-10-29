package engine_test

import (
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pion/webrtc/v4"

	"github.com/Alice088/worm/internal/audio"
	"github.com/Alice088/worm/internal/auth"
	"github.com/Alice088/worm/internal/call"
	"github.com/Alice088/worm/internal/client"
	"github.com/Alice088/worm/internal/engine"
	"github.com/Alice088/worm/internal/metrics"
	"github.com/Alice088/worm/internal/protocol"
	"github.com/Alice088/worm/internal/signaling"
)

type testServer struct {
	issuer *auth.Issuer
	http   *httptest.Server
	sig    *signaling.Server
}

func newTestServer(t *testing.T) *testServer {
	t.Helper()

	const secret = "engine-test-secret-0123456789"
	issuer, err := auth.NewIssuer([]byte(secret), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	callCfg := call.DefaultConfig()
	callCfg.ConnectionTimeout = 3 * time.Minute
	mgr, err := call.NewManager(callCfg, []byte(secret))
	if err != nil {
		t.Fatal(err)
	}
	sig, err := signaling.New(signaling.Config{
		Auth:           issuer,
		Manager:        mgr,
		Metrics:        metrics.New(),
		Logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
		MessageRate:    200,
		MessageBurst:   200,
		CreateRate:     20,
		CreateBurst:    20,
		ReconnectGrace: time.Second,
		IdleTimeout:    30 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	httpSrv := httptest.NewServer(sig)
	t.Cleanup(func() {
		httpSrv.Close()
		sig.Close()
	})
	return &testServer{issuer: issuer, http: httpSrv, sig: sig}
}

func (s *testServer) session(t *testing.T, name string) (token, wsURL string) {
	t.Helper()
	_, token, err := s.issuer.Issue(name)
	if err != nil {
		t.Fatal(err)
	}
	return token, "ws" + strings.TrimPrefix(s.http.URL, "http") + "/ws"
}

type recorder struct {
	mu    sync.Mutex
	bytes int
}

func (r *recorder) Write(p []byte) (int, error) {
	r.mu.Lock()
	r.bytes += len(p)
	r.mu.Unlock()
	return len(p), nil
}

func (r *recorder) Close() error { return nil }

func (r *recorder) received() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.bytes
}

type eventLog struct {
	mu     sync.Mutex
	events []engine.Event
}

func collect(ch <-chan engine.Event) *eventLog {
	log := &eventLog{}
	go func() {
		for ev := range ch {
			log.mu.Lock()
			log.events = append(log.events, ev)
			log.mu.Unlock()
		}
	}()
	return log
}

func (l *eventLog) snapshot() []engine.Event {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]engine.Event(nil), l.events...)
}

func (l *eventLog) waitState(t *testing.T, want string, timeout time.Duration) protocol.CallStatePayload {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		for _, ev := range l.snapshot() {
			if ev.Kind == engine.EventState && ev.Call.State == want {
				return ev.Call
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for state %q; last events: %s", want, summarize(l.snapshot()))
	return protocol.CallStatePayload{}
}

func summarize(events []engine.Event) string {
	start := 0
	if len(events) > 4 {
		start = len(events) - 4
	}
	var b strings.Builder
	for _, ev := range events[start:] {
		switch ev.Kind {
		case engine.EventState:
			b.WriteString("state=" + ev.Call.State + " ")
		default:
			b.WriteString("msg=" + ev.Message + " ")
		}
	}
	return b.String()
}

func newEngine(t *testing.T, wsURL, token, name string, sink audio.Sink) (*engine.Engine, *eventLog) {
	t.Helper()

	tune := func(settings *webrtc.SettingEngine) {
		settings.SetNetworkTypes([]webrtc.NetworkType{webrtc.NetworkTypeUDP4})
	}

	eng := engine.New(engine.Config{
		Name: name,
		Audio: audio.Opener{
			Source: func() (audio.Source, error) {
				src := audio.NewNullSource()
				src.SetDelay(time.Millisecond)
				return src, nil
			},
			Sink: func() (audio.Sink, error) {
				if sink == nil {
					return audio.Discard{}, nil
				}
				return sink, nil
			},
		},
		Signaling:     client.SignalingConfig{URL: wsURL, Token: token},
		Logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
		StatsInterval: 200 * time.Millisecond,
		Tune:          tune,
	})
	t.Cleanup(func() { _ = eng.Close() })
	return eng, collect(eng.Events())
}

func TestEndToEndCallWithMedia(t *testing.T) {
	if raceDetectorEnabled {
		t.Skip("media handshake exceeds ICE timers under the race detector; covered by the non-race run")
	}
	srv := newTestServer(t)

	aliceToken, wsURL := srv.session(t, "alice")
	bobToken, _ := srv.session(t, "bob")

	sink := &recorder{}
	alice, aliceLog := newEngine(t, wsURL, aliceToken, "alice", &recorder{})
	bob, bobLog := newEngine(t, wsURL, bobToken, "bob", sink)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := alice.Connect(ctx); err != nil {
		t.Fatalf("alice connect: %v", err)
	}
	if err := alice.CreateCall(ctx); err != nil {
		t.Fatalf("create call: %v", err)
	}

	ringing := aliceLog.waitState(t, "ringing", 10*time.Second)
	if ringing.Invite == "" {
		t.Fatal("expected an invite token")
	}

	if err := bob.Connect(ctx); err != nil {
		t.Fatalf("bob connect: %v", err)
	}
	if err := bob.Accept(ctx, ringing.CallID, ringing.Invite); err != nil {
		t.Fatalf("accept: %v", err)
	}

	aliceLog.waitState(t, "connected", 90*time.Second)
	bobLog.waitState(t, "connected", 90*time.Second)

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) && sink.received() == 0 {
		time.Sleep(20 * time.Millisecond)
	}
	if sink.received() == 0 {
		t.Fatal("no audio was received by the callee")
	}

	if err := alice.End(ctx, ringing.CallID, "test done"); err != nil {
		t.Fatalf("end: %v", err)
	}
	aliceLog.waitState(t, "ended", 10*time.Second)
	bobLog.waitState(t, "ended", 10*time.Second)
}

func TestCallRejectNotifiesCaller(t *testing.T) {
	srv := newTestServer(t)

	aliceToken, wsURL := srv.session(t, "alice")
	bobToken, _ := srv.session(t, "bob")

	alice, aliceLog := newEngine(t, wsURL, aliceToken, "alice", nil)
	bob, bobLog := newEngine(t, wsURL, bobToken, "bob", nil)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	if err := alice.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	if err := alice.CreateCall(ctx); err != nil {
		t.Fatal(err)
	}
	ringing := aliceLog.waitState(t, "ringing", 10*time.Second)

	if err := bob.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	if err := bob.Reject(ctx, ringing.CallID, ringing.Invite, "busy"); err != nil {
		t.Fatal(err)
	}

	rejected := aliceLog.waitState(t, "rejected", 10*time.Second)
	if rejected.Reason != "busy" {
		t.Fatalf("unexpected reason %q", rejected.Reason)
	}
	bobLog.waitState(t, "rejected", 10*time.Second)
}

func TestCallerCancelNotifiesCallee(t *testing.T) {
	srv := newTestServer(t)

	aliceToken, wsURL := srv.session(t, "alice")
	bobToken, _ := srv.session(t, "bob")

	alice, aliceLog := newEngine(t, wsURL, aliceToken, "alice", nil)
	bob, bobLog := newEngine(t, wsURL, bobToken, "bob", nil)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	if err := alice.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	if err := alice.CreateCall(ctx); err != nil {
		t.Fatal(err)
	}
	ringing := aliceLog.waitState(t, "ringing", 10*time.Second)

	if err := bob.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	if err := bob.Accept(ctx, ringing.CallID, ringing.Invite); err != nil {
		t.Fatal(err)
	}
	bobLog.waitState(t, "accepted", 10*time.Second)

	if err := alice.Cancel(ctx, ringing.CallID); err != nil {
		t.Fatal(err)
	}
	aliceLog.waitState(t, "cancelled", 10*time.Second)
	bobLog.waitState(t, "cancelled", 10*time.Second)
}

func TestUnauthorizedCallIsRejectedByServer(t *testing.T) {
	srv := newTestServer(t)

	aliceToken, wsURL := srv.session(t, "alice")
	bobToken, _ := srv.session(t, "bob")

	alice, aliceLog := newEngine(t, wsURL, aliceToken, "alice", nil)
	bob, bobLog := newEngine(t, wsURL, bobToken, "bob", nil)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	if err := alice.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	if err := alice.CreateCall(ctx); err != nil {
		t.Fatal(err)
	}
	ringing := aliceLog.waitState(t, "ringing", 10*time.Second)

	if err := bob.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	if err := bob.Accept(ctx, ringing.CallID, "forged-invite"); err != nil {
		t.Fatalf("accept should be sent even with a bad invite: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, ev := range bobLog.snapshot() {
			if ev.Kind == engine.EventError {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("expected an error event for the forged invite; events: %+v", bobLog.snapshot())
}

func TestEngineRequiresConnection(t *testing.T) {
	eng := engine.New(engine.Config{})
	defer func() { _ = eng.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := eng.CreateCall(ctx); err == nil {
		t.Fatal("create without a connection must fail")
	}
}
