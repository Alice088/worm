package tui

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Alice088/worm/internal/client"
	"github.com/Alice088/worm/internal/config"
	"github.com/Alice088/worm/internal/engine"
	"github.com/Alice088/worm/internal/protocol"
	"github.com/Alice088/worm/internal/session"
	"github.com/Alice088/worm/internal/tui/pages"
)

type fakeEngine struct {
	mu     sync.Mutex
	events chan engine.Event

	created   int
	accepted  int
	rejected  int
	cancelled int
	ended     int
	closed    bool
	muted     bool
}

func newFakeEngine() *fakeEngine {
	return &fakeEngine{events: make(chan engine.Event, 16)}
}

func (f *fakeEngine) Events() <-chan engine.Event { return f.events }

func (f *fakeEngine) Connect(context.Context) error    { return nil }
func (f *fakeEngine) CreateCall(context.Context) error { f.inc(&f.created); return nil }
func (f *fakeEngine) Accept(context.Context, string, string) error {
	f.inc(&f.accepted)
	return nil
}
func (f *fakeEngine) Reject(context.Context, string, string, string) error {
	f.inc(&f.rejected)
	return nil
}
func (f *fakeEngine) Cancel(context.Context, string) error { f.inc(&f.cancelled); return nil }
func (f *fakeEngine) End(context.Context, string, string) error {
	f.inc(&f.ended)
	return nil
}
func (f *fakeEngine) SetMuted(muted bool) {
	f.mu.Lock()
	f.muted = muted
	f.mu.Unlock()
}
func (f *fakeEngine) Close() error {
	f.mu.Lock()
	f.closed = true
	f.mu.Unlock()
	return nil
}

func (f *fakeEngine) inc(counter *int) {
	f.mu.Lock()
	*counter++
	f.mu.Unlock()
}

func (f *fakeEngine) push(ev engine.Event) { f.events <- ev }

func (f *fakeEngine) counts() (created, accepted, rejected, cancelled, ended int, muted, closed bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.created, f.accepted, f.rejected, f.cancelled, f.ended, f.muted, f.closed
}

func newTestModel(t *testing.T) (Model, *fakeEngine) {
	t.Helper()

	cfgPath := filepath.Join(t.TempDir(), config.FileName)
	cfg := config.Config{}.WithName("tester").WithServer("http://127.0.0.1:1")
	if err := cfg.SaveTo(cfgPath); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	fake := newFakeEngine()
	m := newModel("test", "worm", cfgPath)
	m.newEngine = func(engine.Config) CallEngine { return fake }
	m.register = func(context.Context, string, string) (client.Session, error) {
		return client.Session{
			UserID: "u-1",
			Name:   "tester",
			Token:  "token",
			WSURL:  "ws://fake",
		}, nil
	}

	m.main, _, _, _ = m.main.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.call, _, _ = m.call.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return m, fake
}

func key(m tea.Model, k string) (Model, tea.Cmd) {
	var msg tea.KeyPressMsg
	switch k {
	case "enter":
		msg = tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		msg = tea.KeyPressMsg{Code: tea.KeyEscape}
	case "down":
		msg = tea.KeyPressMsg{Code: tea.KeyDown}
	case "backspace":
		msg = tea.KeyPressMsg{Code: tea.KeyBackspace}
	default:
		r := []rune(k)[0]
		msg = tea.KeyPressMsg{Code: r, Text: k}
	}
	next, cmd := m.Update(msg)
	return next.(Model), cmd
}

func runCmd(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	if cmd == nil {
		return m
	}
	msg := cmd()
	if msg == nil {
		return m
	}
	next, _ := m.Update(msg)
	return next.(Model)
}

func startCreatedCall(t *testing.T, m Model) (Model, *fakeEngine) {
	t.Helper()
	m, cmd := key(m, "1")
	m = runCmd(t, m, cmd)
	if m.screen != ScreenCall {
		t.Fatalf("expected call screen, got %v", m.screen)
	}
	return m, m.eng.(*fakeEngine)
}

func TestMainScreenShowsActionsAndServer(t *testing.T) {
	m, _ := newTestModel(t)
	view := ansi.Strip(m.View().Content)
	for _, want := range []string{"Create a call", "Join a call", "http://127.0.0.1:1"} {
		if !strings.Contains(view, want) {
			t.Fatalf("main screen missing %q:\n%s", want, view)
		}
	}
}

func TestCreateFlowStartsEngine(t *testing.T) {
	m, fake := startCreatedCall(t, m0(t))
	created, _, _, _, _, _, _ := fake.counts()
	if created != 1 {
		t.Fatalf("expected one create call, got %d", created)
	}

	fake.push(engine.Event{Kind: engine.EventState, Call: protocol.CallStatePayload{
		CallID: "abc12345",
		State:  "ringing",
		Role:   "caller",
		Self:   protocol.Party{ID: "u-1", Name: "tester"},
		Invite: "invite-token",
	}})
	next, _ := m.Update(engineEventMsg(engine.Event{Kind: engine.EventState, Call: protocol.CallStatePayload{
		CallID: "abc12345", State: "ringing", Role: "caller",
		Self: protocol.Party{ID: "u-1", Name: "tester"}, Invite: "invite-token",
	}}))
	m = next.(Model)

	if m.call.State != "ringing" {
		t.Fatalf("expected ringing, got %q", m.call.State)
	}
	if m.call.Invite == "" {
		t.Fatal("expected an invite code")
	}
	inv, err := session.DecodeInvite(m.call.Invite)
	if err != nil {
		t.Fatalf("invite code is not decodable: %v", err)
	}
	if inv.CallID != "abc12345" || inv.Token != "invite-token" || inv.Caller != "tester" {
		t.Fatalf("unexpected invite: %+v", inv)
	}
}

func m0(t *testing.T) Model {
	m, _ := newTestModel(t)
	return m
}

func TestJoinFlowAcceptsIncomingCall(t *testing.T) {
	m, _ := newTestModel(t)

	invite, err := session.Invite{
		Server: "http://127.0.0.1:1",
		CallID: "call-42",
		Token:  "secret-token",
		Caller: "alice",
	}.Encode()
	if err != nil {
		t.Fatal(err)
	}

	m, _ = key(m, "down")
	m, _ = key(m, "enter")
	if m.main.Mode() != pages.ModeJoin {
		t.Fatalf("expected join mode, got %v", m.main.Mode())
	}

	for _, r := range invite {
		var press tea.Cmd
		m, press = key(m, string(r))
		_ = press
	}
	m, cmd := key(m, "enter")
	m = runCmd(t, m, cmd)

	if m.screen != ScreenCall {
		t.Fatalf("expected call screen, got %v", m.screen)
	}
	if m.call.State != "ringing" || m.call.Role != "callee" {
		t.Fatalf("expected an incoming call, got %+v", m.call)
	}
	if m.call.Peer != "alice" {
		t.Fatalf("expected caller name alice, got %q", m.call.Peer)
	}

	m, cmd = key(m, "a")
	m = runCmd(t, m, cmd)
	_, accepted, _, _, _, _, _ := m.eng.(*fakeEngine).counts()
	if accepted != 1 {
		t.Fatalf("expected accept to be sent, got %d", accepted)
	}
}

func TestJoinRejectsBadInvite(t *testing.T) {
	m, _ := newTestModel(t)

	m, _ = key(m, "down")
	m, _ = key(m, "enter")
	for _, r := range "deadbeef" {
		m, _ = key(m, string(r))
	}
	m, _ = key(m, "enter")

	if m.screen != ScreenMain {
		t.Fatalf("expected to stay on main screen, got %v", m.screen)
	}
	if m.main.Err() == "" {
		t.Fatal("expected a validation error")
	}
}

func TestMissingServerIsReported(t *testing.T) {
	m, _ := newTestModel(t)
	m.server = ""
	m.main.SetServer("")

	next, _ := m.Update(tea.KeyPressMsg{Code: '1', Text: "1"})
	m = next.(Model)
	if m.screen != ScreenMain {
		t.Fatalf("expected to stay on main screen, got %v", m.screen)
	}
	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "not configured") {
		t.Fatalf("expected a server configuration error:\n%s", view)
	}
}

func TestRegisterFailureIsShown(t *testing.T) {
	m, _ := newTestModel(t)
	m.register = func(context.Context, string, string) (client.Session, error) {
		return client.Session{}, errors.New("boom")
	}

	m, cmd := key(m, "1")
	m = runCmd(t, m, cmd)
	if m.screen != ScreenMain {
		t.Fatalf("expected main screen after failure, got %v", m.screen)
	}
	if !strings.Contains(ansi.Strip(m.View().Content), "boom") {
		t.Fatalf("expected the error to be visible:\n%s", ansi.Strip(m.View().Content))
	}
}

func TestMuteAndLeave(t *testing.T) {
	m, fake := startCreatedCall(t, m0(t))
	m.call.State = "connected"
	m.call.Role = "caller"

	m, _ = key(m, "m")
	if !m.call.Muted {
		t.Fatal("expected muted")
	}
	_, _, _, _, _, muted, _ := fake.counts()
	if !muted {
		t.Fatal("engine was not muted")
	}

	m, cmd := key(m, "q")
	m = runCmd(t, m, cmd)
	if m.screen != ScreenMain {
		t.Fatalf("expected main screen after hangup, got %v", m.screen)
	}
	_, _, _, _, ended, _, closed := fake.counts()
	if ended != 1 {
		t.Fatalf("expected end to be sent, got %d", ended)
	}
	if !closed {
		t.Fatal("engine was not closed")
	}
}

func TestLeaveWhileRingingCancels(t *testing.T) {
	m, fake := startCreatedCall(t, m0(t))
	m.call.State = "ringing"
	m.call.Role = "caller"

	m, cmd := key(m, "q")
	m = runCmd(t, m, cmd)
	_, _, _, cancelled, _, _, _ := fake.counts()
	if cancelled != 1 {
		t.Fatalf("expected cancel to be sent, got %d", cancelled)
	}
}

func TestRejectFromIncomingCall(t *testing.T) {
	m, fake := startCreatedCall(t, m0(t))
	m.call.State = "ringing"
	m.call.Role = "callee"
	m.invite = session.Invite{CallID: "call-1", Token: "tok"}

	m, cmd := key(m, "r")
	m = runCmd(t, m, cmd)
	if m.screen != ScreenMain {
		t.Fatalf("expected main screen after reject, got %v", m.screen)
	}
	_, _, rejected, _, _, _, closed := fake.counts()
	if rejected != 1 || !closed {
		t.Fatalf("expected reject and cleanup, got rejected=%d closed=%v", rejected, closed)
	}
}

func TestTerminalStateClosesEngine(t *testing.T) {
	m, fake := startCreatedCall(t, m0(t))
	m.call.State = "connected"

	next, _ := m.Update(engineEventMsg(engine.Event{Kind: engine.EventState, Call: protocol.CallStatePayload{
		CallID: "abc", State: "ended", Reason: "hangup",
		Self: protocol.Party{ID: "u-1", Name: "tester"},
	}}))
	m = next.(Model)

	_, _, _, _, _, _, closed := fake.counts()
	if !closed {
		t.Fatal("engine must be closed after a terminal state")
	}
	if !m.call.Terminal() {
		t.Fatalf("expected a terminal call screen, got %q", m.call.State)
	}
	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "Back to main") {
		t.Fatalf("expected a back control:\n%s", view)
	}
}

func TestEngineErrorIsShown(t *testing.T) {
	m, _ := startCreatedCall(t, m0(t))
	next, _ := m.Update(engineEventMsg(engine.Event{
		Kind: engine.EventError, Code: protocol.CodeInvalidState, Message: "nope",
	}))
	m = next.(Model)
	if !strings.Contains(ansi.Strip(m.View().Content), "nope") {
		t.Fatalf("expected the engine error to be visible")
	}
}

func TestCallScreenShowsPanels(t *testing.T) {
	m, _ := startCreatedCall(t, m0(t))
	m.call.State = "connected"
	m.call.Peer = "bob"
	m.call.Invite = "abcd1234"

	view := ansi.Strip(m.View().Content)
	for _, want := range []string{"PARTICIPANTS", "INVITE", "tester", "bob", "End call"} {
		if !strings.Contains(view, want) {
			t.Fatalf("call screen missing %q:\n%s", want, view)
		}
	}
}

func TestRenamePersistsToConfig(t *testing.T) {
	m, _ := newTestModel(t)

	m, _ = key(m, "3")
	if m.main.Mode() != pages.ModeName {
		t.Fatalf("expected name mode, got %v", m.main.Mode())
	}
	for range []rune(m.main.UserName) {
		m, _ = key(m, "backspace")
	}
	for _, r := range "neo" {
		m, _ = key(m, string(r))
	}
	m, _ = key(m, "enter")

	if m.selfName != "neo" {
		t.Fatalf("expected selfName neo, got %q", m.selfName)
	}
	cfg, err := config.LoadFrom(m.cfg.Path())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Name != "neo" {
		t.Fatalf("expected persisted name neo, got %q", cfg.Name)
	}
	if cfg.Server != "http://127.0.0.1:1" {
		t.Fatalf("server must survive a rename, got %q", cfg.Server)
	}
}

func TestRenameRejectsEmptyName(t *testing.T) {
	m, _ := newTestModel(t)
	m, _ = key(m, "3")
	for range []rune(m.main.UserName) {
		m, _ = key(m, "backspace")
	}
	m, _ = key(m, "enter")

	if m.main.Err() == "" {
		t.Fatal("expected a validation error")
	}
	if m.selfName != "tester" {
		t.Fatalf("expected name to stay tester, got %q", m.selfName)
	}
}

func TestWindowSizePropagates(t *testing.T) {
	m, _ := newTestModel(t)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = next.(Model)
	if m.Window.Width != 120 || m.Window.Heigh != 40 {
		t.Fatalf("unexpected window: %+v", m.Window)
	}
	if m.call.W != 0 && m.call.W != 120 {
		t.Fatalf("call model width not updated: %d", m.call.W)
	}
	_ = time.Second
}
