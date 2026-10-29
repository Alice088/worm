package signaling

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/Alice088/worm/internal/auth"
	"github.com/Alice088/worm/internal/call"
	"github.com/Alice088/worm/internal/metrics"
	"github.com/Alice088/worm/internal/protocol"
)

const wsTestSecret = "signaling-test-secret-0123456789"

type testHub struct {
	srv    *Server
	issuer *auth.Issuer
	http   *httptest.Server
}

func newTestHub(t *testing.T) *testHub {
	t.Helper()

	issuer, err := auth.NewIssuer([]byte(wsTestSecret), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	mgr, err := call.NewManager(call.DefaultConfig(), []byte(wsTestSecret))
	if err != nil {
		t.Fatal(err)
	}
	srv, err := New(Config{
		Auth:           issuer,
		Manager:        mgr,
		Metrics:        metrics.New(),
		Logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
		MessageRate:    100,
		MessageBurst:   200,
		CreateRate:     20,
		CreateBurst:    40,
		ReconnectGrace: 200 * time.Millisecond,
		IdleTimeout:    30 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	httpSrv := httptest.NewServer(srv)
	t.Cleanup(func() {
		httpSrv.Close()
		srv.Close()
	})
	return &testHub{srv: srv, issuer: issuer, http: httpSrv}
}

func (h *testHub) token(t *testing.T, name string) string {
	t.Helper()
	_, token, err := h.issuer.Issue(name)
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func (h *testHub) dial(t *testing.T, token string) *wsClient {
	t.Helper()
	url := "ws" + strings.TrimPrefix(h.http.URL, "http") + "/ws"

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{
		HTTPHeader: map[string][]string{"Authorization": {"Bearer " + token}},
	})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	c := &wsClient{t: t, conn: conn, envs: make(chan protocol.Envelope, 64)}
	go c.read()
	t.Cleanup(c.close)
	return c
}

type wsClient struct {
	t       *testing.T
	conn    *websocket.Conn
	envs    chan protocol.Envelope
	pending []protocol.Envelope
}

func (c *wsClient) read() {
	defer close(c.envs)
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		kind, data, err := c.conn.Read(ctx)
		cancel()
		if err != nil {
			return
		}
		if kind != websocket.MessageText {
			continue
		}
		env, err := protocol.Decode(data)
		if err != nil {
			continue
		}
		c.envs <- env
	}
}

func (c *wsClient) send(env protocol.Envelope) {
	c.t.Helper()
	data, err := env.Encode()
	if err != nil {
		c.t.Fatalf("encode: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.conn.Write(ctx, websocket.MessageText, data); err != nil {
		c.t.Fatalf("write: %v", err)
	}
}

func (c *wsClient) close() {
	_ = c.conn.Close(websocket.StatusNormalClosure, "test done")
}

func (c *wsClient) next(types ...protocol.Type) protocol.Envelope {
	c.t.Helper()

	for i, env := range c.pending {
		if len(types) == 0 || containsType(types, env.Type) {
			c.pending = append(c.pending[:i], c.pending[i+1:]...)
			return env
		}
	}

	deadline := time.After(5 * time.Second)
	for {
		select {
		case env, ok := <-c.envs:
			if !ok {
				c.t.Fatalf("connection closed while waiting for %v", types)
			}
			if len(types) == 0 || containsType(types, env.Type) {
				return env
			}
			c.pending = append(c.pending, env)
		case <-deadline:
			c.t.Fatalf("timeout waiting for %v", types)
		}
	}
}

func containsType(types []protocol.Type, t protocol.Type) bool {
	for _, want := range types {
		if want == t {
			return true
		}
	}
	return false
}

func payload[T any](t *testing.T, env protocol.Envelope) T {
	t.Helper()
	var out T
	if err := env.DecodePayload(&out); err != nil {
		t.Fatalf("decode %s payload: %v", env.Type, err)
	}
	return out
}

func sendCreate(c *wsClient, requestID string) protocol.Envelope {
	raw, _ := json.Marshal(protocol.CallCreatePayload{})
	c.send(protocol.Envelope{Version: protocol.Version, Type: protocol.TypeCallCreate, RequestID: requestID, Payload: raw})
	return c.next(protocol.TypeCallState, protocol.TypeCallError)
}

func sendAccept(c *wsClient, callID, invite, requestID string) protocol.Envelope {
	raw, _ := json.Marshal(protocol.CallAcceptPayload{Invite: invite})
	c.send(protocol.Envelope{
		Version: protocol.Version, Type: protocol.TypeCallAccept,
		CallID: callID, RequestID: requestID, Payload: raw,
	})
	return c.next(protocol.TypeCallState, protocol.TypeCallError)
}

func TestUnauthorizedConnectionRejected(t *testing.T) {
	h := newTestHub(t)
	url := "ws" + strings.TrimPrefix(h.http.URL, "http") + "/ws"

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, _, err := websocket.Dial(ctx, url, nil); err == nil {
		t.Fatal("connection without a token must fail")
	}
}

func TestCreateCallReturnsRingingState(t *testing.T) {
	h := newTestHub(t)
	caller := h.dial(t, h.token(t, "alice"))

	env := sendCreate(caller, "req-create")
	state := payload[protocol.CallStatePayload](t, env)
	if env.Type != protocol.TypeCallState {
		t.Fatalf("unexpected response %s: %+v", env.Type, state)
	}
	if state.State != "ringing" || state.Role != "caller" {
		t.Fatalf("unexpected state: %+v", state)
	}
	if state.Invite == "" {
		t.Fatal("caller must receive the invite token")
	}
	if state.Deadline == nil {
		t.Fatal("expected an invitation deadline")
	}
}

func TestFullSignalingFlow(t *testing.T) {
	h := newTestHub(t)
	caller := h.dial(t, h.token(t, "alice"))
	callee := h.dial(t, h.token(t, "bob"))

	createEnv := sendCreate(caller, "req-1")
	created := payload[protocol.CallStatePayload](t, createEnv)
	callID := created.CallID

	bad := sendAccept(callee, callID, "forged", "req-bad")
	if bad.Type != protocol.TypeCallError {
		t.Fatalf("expected error for forged invite, got %s", bad.Type)
	}
	if payload[protocol.ErrorPayload](t, bad).Code != protocol.CodeForbidden {
		t.Fatalf("unexpected error code: %+v", payload[protocol.ErrorPayload](t, bad))
	}

	acceptEnv := sendAccept(callee, callID, created.Invite, "req-2")
	accepted := payload[protocol.CallStatePayload](t, acceptEnv)
	if accepted.State != "accepted" || accepted.Role != "callee" {
		t.Fatalf("unexpected accept state: %+v", accepted)
	}
	incoming := callee.next(protocol.TypeCallIncoming)
	if payload[protocol.CallStatePayload](t, incoming).Peer.Name != "alice" {
		t.Fatalf("unexpected incoming payload: %+v", payload[protocol.CallStatePayload](t, incoming))
	}

	callerState := payload[protocol.CallStatePayload](t, caller.next(protocol.TypeCallState))
	if callerState.State != "accepted" || callerState.Peer.Name != "bob" {
		t.Fatalf("caller did not see the callee: %+v", callerState)
	}

	offerRaw, _ := json.Marshal(protocol.SDPPayload{SDP: "v=0\r\noffer"})
	caller.send(protocol.Envelope{
		Version: protocol.Version, Type: protocol.TypeWebRTCOffer,
		CallID: callID, RequestID: "offer-1", Payload: offerRaw,
	})
	offer := payload[protocol.SDPPayload](t, callee.next(protocol.TypeWebRTCOffer))
	if offer.SDP != "v=0\r\noffer" {
		t.Fatalf("unexpected offer: %+v", offer)
	}

	answerRaw, _ := json.Marshal(protocol.SDPPayload{SDP: "v=0\r\nanswer"})
	callee.send(protocol.Envelope{
		Version: protocol.Version, Type: protocol.TypeWebRTCAnswer,
		CallID: callID, RequestID: "answer-1", Payload: answerRaw,
	})
	answer := payload[protocol.SDPPayload](t, caller.next(protocol.TypeWebRTCAnswer))
	if answer.SDP != "v=0\r\nanswer" {
		t.Fatalf("unexpected answer: %+v", answer)
	}

	sendStatus(t, caller, callID, "connected", "connected", "flowing")
	connecting := payload[protocol.CallStatePayload](t, caller.next(protocol.TypeCallState))
	if connecting.State != "connecting" {
		t.Fatalf("expected connecting after one report, got %s", connecting.State)
	}

	sendStatus(t, callee, callID, "connected", "connected", "flowing")
	connected := waitState(t, caller, "connected")
	if connected.State != "connected" {
		t.Fatalf("expected connected, got %s", connected.State)
	}

	cancelRaw, _ := json.Marshal(protocol.CallEndPayload{Reason: "hangup"})
	caller.send(protocol.Envelope{
		Version: protocol.Version, Type: protocol.TypeCallEnd,
		CallID: callID, RequestID: "end-1", Payload: cancelRaw,
	})
	if got := waitState(t, callee, "ended"); got.State != "ended" {
		t.Fatalf("expected ended for callee, got %s", got.State)
	}
	if got := waitState(t, caller, "ended"); got.State != "ended" {
		t.Fatalf("expected ended for caller, got %s", got.State)
	}
}

func sendStatus(t *testing.T, c *wsClient, callID, pc, ice, media string) {
	t.Helper()
	raw, err := json.Marshal(protocol.ConnectionStatusPayload{
		PCState: pc, ICEState: ice, MediaState: media,
	})
	if err != nil {
		t.Fatal(err)
	}
	c.send(protocol.Envelope{
		Version: protocol.Version, Type: protocol.TypeConnectionStatus,
		CallID: callID, RequestID: "status-" + pc + ice, Payload: raw,
	})
}

func waitState(t *testing.T, c *wsClient, want string) protocol.CallStatePayload {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatalf("timeout waiting for state %s", want)
		default:
		}
		env := c.next(protocol.TypeCallState)
		state := payload[protocol.CallStatePayload](t, env)
		if state.State == want {
			return state
		}
	}
}

func TestSignalingRequiresMembership(t *testing.T) {
	h := newTestHub(t)
	caller := h.dial(t, h.token(t, "alice"))
	outsider := h.dial(t, h.token(t, "carol"))
	callee := h.dial(t, h.token(t, "bob"))

	created := payload[protocol.CallStatePayload](t, sendCreate(caller, "req"))
	sendAccept(callee, created.CallID, created.Invite, "req-acc")

	raw, _ := json.Marshal(protocol.SDPPayload{SDP: "v=0"})
	outsider.send(protocol.Envelope{
		Version: protocol.Version, Type: protocol.TypeWebRTCOffer,
		CallID: created.CallID, RequestID: "spy", Payload: raw,
	})
	env := outsider.next(protocol.TypeCallError)
	if got := payload[protocol.ErrorPayload](t, env); got.Code != protocol.CodeNotFound {
		t.Fatalf("expected not_found for outsider, got %+v", got)
	}

	rawEnd, _ := json.Marshal(protocol.CallEndPayload{})
	outsider.send(protocol.Envelope{
		Version: protocol.Version, Type: protocol.TypeCallEnd,
		CallID: created.CallID, RequestID: "spy-end", Payload: rawEnd,
	})
	env = outsider.next(protocol.TypeCallError)
	if got := payload[protocol.ErrorPayload](t, env); got.Code != protocol.CodeNotFound {
		t.Fatalf("expected not_found for outsider end, got %+v", got)
	}
}

func TestDuplicateCreateIsIdempotent(t *testing.T) {
	h := newTestHub(t)
	caller := h.dial(t, h.token(t, "alice"))

	first := payload[protocol.CallStatePayload](t, sendCreate(caller, "req-same"))
	second := payload[protocol.CallStatePayload](t, sendCreate(caller, "req-same"))
	if first.CallID != second.CallID {
		t.Fatalf("duplicate create made a new call: %s != %s", first.CallID, second.CallID)
	}
}

func TestDuplicateOfferForwardedOnce(t *testing.T) {
	h := newTestHub(t)
	caller := h.dial(t, h.token(t, "alice"))
	callee := h.dial(t, h.token(t, "bob"))

	created := payload[protocol.CallStatePayload](t, sendCreate(caller, "req"))
	sendAccept(callee, created.CallID, created.Invite, "req-acc")

	raw, _ := json.Marshal(protocol.SDPPayload{SDP: "v=0"})
	env := protocol.Envelope{
		Version: protocol.Version, Type: protocol.TypeWebRTCOffer,
		CallID: created.CallID, RequestID: "same-offer", Payload: raw,
	}
	caller.send(env)
	caller.send(env)

	first := callee.next(protocol.TypeWebRTCOffer)
	if first.CallID != created.CallID {
		t.Fatalf("unexpected offer: %+v", first)
	}

	select {
	case dup := <-callee.envs:
		if dup.Type == protocol.TypeWebRTCOffer {
			t.Fatal("duplicate offer was forwarded twice")
		}
	case <-time.After(150 * time.Millisecond):
	}
}

func TestInvalidMessageTypeRejected(t *testing.T) {
	h := newTestHub(t)
	caller := h.dial(t, h.token(t, "alice"))

	raw, _ := json.Marshal(struct{}{})
	caller.send(protocol.Envelope{
		Version: protocol.Version, Type: protocol.TypeCallError,
		CallID: "abc", Payload: raw,
	})
	env := caller.next(protocol.TypeCallError)
	if got := payload[protocol.ErrorPayload](t, env); got.Code != protocol.CodeInvalidMessage {
		t.Fatalf("expected invalid_message, got %+v", got)
	}
}

func TestPingPong(t *testing.T) {
	h := newTestHub(t)
	c := h.dial(t, h.token(t, "alice"))

	raw, _ := json.Marshal(protocol.PingPayload{Nonce: "abc"})
	c.send(protocol.Envelope{
		Version: protocol.Version, Type: protocol.TypePing,
		RequestID: "ping-1", Payload: raw,
	})
	env := c.next(protocol.TypePong)
	if got := payload[protocol.PingPayload](t, env); got.Nonce != "abc" {
		t.Fatalf("unexpected pong: %+v", got)
	}
	if env.RequestID != "ping-1" {
		t.Fatalf("pong must echo the request id, got %q", env.RequestID)
	}
}

func TestCandidatesAreBufferedWhilePeerIsOffline(t *testing.T) {
	h := newTestHub(t)
	caller := h.dial(t, h.token(t, "alice"))
	bobToken := h.token(t, "bob")
	callee := h.dial(t, bobToken)

	created := payload[protocol.CallStatePayload](t, sendCreate(caller, "req"))
	sendAccept(callee, created.CallID, created.Invite, "req-acc")
	callee.next(protocol.TypeCallIncoming)
	caller.next(protocol.TypeCallState)

	callee.close()
	time.Sleep(100 * time.Millisecond)

	raw, _ := json.Marshal(protocol.ICECandidatePayload{Candidate: "candidate:1"})
	caller.send(protocol.Envelope{
		Version: protocol.Version, Type: protocol.TypeWebRTCICECandidate,
		CallID: created.CallID, RequestID: "cand-1", Payload: raw,
	})

	callee2 := h.dial(t, bobToken)
	candEnv := callee2.next(protocol.TypeWebRTCICECandidate)
	if got := payload[protocol.ICECandidatePayload](t, candEnv); got.Candidate != "candidate:1" {
		t.Fatalf("unexpected buffered candidate: %+v", got)
	}
}

func TestReconnectGraceFailsCall(t *testing.T) {
	h := newTestHub(t)
	caller := h.dial(t, h.token(t, "alice"))
	callee := h.dial(t, h.token(t, "bob"))

	created := payload[protocol.CallStatePayload](t, sendCreate(caller, "req"))
	sendAccept(callee, created.CallID, created.Invite, "req-acc")
	callee.next(protocol.TypeCallIncoming)
	caller.next(protocol.TypeCallState)

	callee.close()
	time.Sleep(300 * time.Millisecond)

	h.srv.Sweep(context.Background())
	if got := waitState(t, caller, "failed"); got.State != "failed" {
		t.Fatalf("expected failed call, got %s", got.State)
	}
}

func TestRateLimitReturnsError(t *testing.T) {
	h := newTestHub(t)
	h.srv.cfg.MessageRate = 2
	h.srv.cfg.MessageBurst = 2
	c := h.dial(t, h.token(t, "alice"))

	raw, _ := json.Marshal(protocol.PingPayload{})
	for i := 0; i < 8; i++ {
		c.send(protocol.Envelope{
			Version: protocol.Version, Type: protocol.TypePing,
			RequestID: "p" + string(rune('a'+i)), Payload: raw,
		})
	}

	deadline := time.After(5 * time.Second)
	for {
		select {
		case env, ok := <-c.envs:
			if !ok {
				t.Fatal("closed while waiting for rate limit error")
			}
			if env.Type == protocol.TypeCallError {
				if got := payload[protocol.ErrorPayload](t, env); got.Code == protocol.CodeRateLimited {
					return
				}
			}
		case <-deadline:
			t.Fatal("rate limit error was not returned")
		}
	}
}

func TestMalformedMessageReturnsError(t *testing.T) {
	h := newTestHub(t)
	c := h.dial(t, h.token(t, "alice"))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.conn.Write(ctx, websocket.MessageText, []byte(`{"version":1,"type":"nope"}`)); err != nil {
		t.Fatal(err)
	}
	env := c.next(protocol.TypeCallError)
	if got := payload[protocol.ErrorPayload](t, env); got.Code != protocol.CodeInvalidMessage {
		t.Fatalf("expected invalid_message, got %+v", got)
	}
}

func TestRecoverReturnsCurrentState(t *testing.T) {
	h := newTestHub(t)
	caller := h.dial(t, h.token(t, "alice"))
	callee := h.dial(t, h.token(t, "bob"))

	created := payload[protocol.CallStatePayload](t, sendCreate(caller, "req"))
	sendAccept(callee, created.CallID, created.Invite, "req-acc")

	raw, _ := json.Marshal(protocol.RecoverPayload{})
	caller.send(protocol.Envelope{
		Version: protocol.Version, Type: protocol.TypeConnectionRecover,
		CallID: created.CallID, RequestID: "recover-1", Payload: raw,
	})
	state := waitState(t, caller, "accepted")
	if state.Peer.Name != "bob" {
		t.Fatalf("recover did not include the peer: %+v", state)
	}
}
