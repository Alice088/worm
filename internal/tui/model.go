package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Alice088/worm/internal/audio"
	"github.com/Alice088/worm/internal/client"
	"github.com/Alice088/worm/internal/config"
	"github.com/Alice088/worm/internal/engine"
	"github.com/Alice088/worm/internal/home"
	"github.com/Alice088/worm/internal/session"
	"github.com/Alice088/worm/internal/tui/pages"
	"github.com/Alice088/worm/internal/updater"
)

type Screen int

const (
	ScreenMain Screen = iota
	ScreenCall
	ScreenManual
)

type CallEngine interface {
	Events() <-chan engine.Event
	Connect(ctx context.Context) error
	CreateCall(ctx context.Context) error
	Accept(ctx context.Context, callID, invite string) error
	Reject(ctx context.Context, callID, invite, reason string) error
	Cancel(ctx context.Context, callID string) error
	End(ctx context.Context, callID, reason string) error
	SetMuted(muted bool)
	Close() error
}

type Model struct {
	Window  Window
	Version string
	Name    string

	screen   Screen
	main     pages.MainModel
	call     pages.CallModel
	manualUI pages.ManualModel
	manual   *engine.Manual

	cfg      config.Config
	selfName string
	server   string

	eng       CallEngine
	invite    session.Invite
	newEngine func(engine.Config) CallEngine
	register  func(context.Context, string, string) (client.Session, error)

	fatal error
}

type Window struct {
	Heigh int
	Width int
}

func InitialModel(v string) Model {
	if _, err := home.Ensure(); err != nil {
		fmt.Fprintf(os.Stderr, "worm: cannot prepare %s: %v\n", home.DirName, err)
	}

	branding, err := home.LoadAsset()
	if err != nil || strings.TrimSpace(branding) == "" {
		branding = home.DefaultAsset()
	}

	return newModel(v, branding, config.DefaultPath())
}

func newModel(version, branding, cfgPath string) Model {
	var (
		cfg     config.Config
		loadErr error
	)

	if cfgPath != "" {
		cfg, loadErr = config.LoadFrom(cfgPath)
	} else {
		cfg = config.Config{}
	}

	self := strings.TrimSpace(cfg.Name)
	if self == "" {
		self = defaultName()
	}

	server := strings.TrimSpace(os.Getenv("WORM_SERVER"))
	if server == "" {
		server = strings.TrimSpace(cfg.Server)
	}

	appName := strings.TrimRight(branding, "\n")

	main := pages.NewMain(appName, version, self, cfg.Path())
	main.SetServer(server)
	if selfPath, err := updater.SelfPath(); err == nil {
		main.SetSelfPath(selfPath)
	}

	return Model{
		Version:  version,
		Name:     branding,
		screen:   ScreenMain,
		selfName: self,
		server:   server,
		cfg:      cfg,
		fatal:    loadErr,
		main:     main,
		newEngine: func(cfg engine.Config) CallEngine {
			return engine.New(cfg)
		},
		register: client.Register,
	}
}

func defaultName() string {
	if h, err := os.Hostname(); err == nil && h != "" {
		if i := strings.IndexByte(h, '.'); i > 0 {
			h = h[:i]
		}
		return h
	}
	if u := os.Getenv("USER"); u != "" {
		return u
	}
	return "peer-" + session.RandomID(2)
}

type copiedMsg struct{ err error }

type updateCheckMsg struct {
	tag string
	err error
}

type updateInstallMsg struct {
	tag string
	err error
}

type uninstallMsg struct{ err error }

func checkUpdate() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		tag, err := updater.Latest(ctx)
		return updateCheckMsg{tag: tag, err: err}
	}
}

func installUpdate(tag string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		_, err := updater.Install(ctx, tag)
		return updateInstallMsg{tag: tag, err: err}
	}
}

func uninstallWorm() tea.Cmd {
	return func() tea.Msg {
		return uninstallMsg{err: updater.Uninstall()}
	}
}

type oscCopiedMsg string

func copyToClipboard(s string) tea.Cmd {
	return func() tea.Msg {
		if err := nativeClipboard(s); err == nil {
			return copiedMsg{}
		}
		return oscCopiedMsg(s)
	}
}

func nativeClipboard(s string) error {
	commands := [][]string{
		{"wl-copy"},
		{"xclip", "-selection", "clipboard"},
		{"xsel", "--clipboard", "--input"},
		{"pbcopy"},
	}
	var lastErr error
	for _, entry := range commands {
		path, err := exec.LookPath(entry[0])
		if err != nil {
			continue
		}
		cmd := exec.Command(path, entry[1:]...)
		cmd.Stdin = strings.NewReader(s)
		if err := cmd.Run(); err != nil {
			lastErr = err
			continue
		}
		return nil
	}
	if lastErr != nil {
		return lastErr
	}
	return errors.New("no clipboard tool (wl-copy, xclip, xsel, pbcopy)")
}

type engineReadyMsg struct {
	eng      CallEngine
	invite   session.Invite
	incoming bool
	err      error
}

type engineEventMsg engine.Event

type callCommandMsg struct {
	err    error
	action pages.CallAction
	toMain bool
}

type manualCodeMsg struct {
	code  string
	stage pages.ManualStage
	err   error
}

type manualDoneMsg struct{ err error }

type manualEventMsg engine.Event

type savedSignalMsg string

func (m Model) Init() tea.Cmd {
	return nil
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.Window.Width, m.Window.Heigh = msg.Width, msg.Height
		var cmd tea.Cmd
		m.main, _, _, cmd = m.main.Update(msg)
		m.call, _, _ = m.call.Update(msg)
		m.manualUI, _, _, _ = m.manualUI.Update(msg)
		return m, cmd

	case tea.KeyPressMsg:
		if msg.String() == "ctrl+c" {
			m.closeEngine()
			return m, tea.Quit
		}

	case oscCopiedMsg:
		return m, tea.Sequence(tea.SetClipboard(string(msg)), func() tea.Msg {
			return copiedMsg{}
		})

	case updateCheckMsg:
		m.main.SetLatest(msg.tag, msg.err)
		return m, nil

	case updateInstallMsg:
		m.main.SetUpdating(false)
		if msg.err != nil {
			m.main.SetFailure("update failed: " + msg.err.Error())
			m.main.SetNotice("")
			return m, nil
		}
		m.main.SetLatest(msg.tag, nil)
		m.main.SetNotice("updated to " + msg.tag + " — restart worm")
		return m, nil

	case uninstallMsg:
		if msg.err != nil {
			m.main.SetFailure("uninstall failed: " + msg.err.Error())
			return m, nil
		}
		m.closeEngine()
		return m, tea.Quit

	case copiedMsg:
		if m.screen == ScreenManual {
			if msg.err != nil {
				m.manualUI.SetError("clipboard unavailable: " + msg.err.Error())
			} else {
				m.manualUI.SetNotice("code copied to clipboard")
			}
			return m, nil
		}
		if msg.err != nil {
			m.call.SetError("clipboard unavailable: " + msg.err.Error())
		} else {
			m.call.SetNoticeOK("invite code copied to clipboard")
		}
		return m, nil

	case engineReadyMsg:
		return m.handleEngineReady(msg)

	case engineEventMsg:
		return m.handleEngineEvent(engine.Event(msg))

	case callCommandMsg:
		return m.handleCallCommand(msg)

	case manualCodeMsg:
		return m.handleManualCode(msg)

	case manualDoneMsg:
		return m.handleManualDone(msg)

	case savedSignalMsg:
		if m.screen == ScreenManual {
			m.manualUI.SetNotice("saved to " + string(msg))
		}
		return m, nil

	case manualEventMsg:
		return m.handleManualEvent(engine.Event(msg))
	}

	switch m.screen {
	case ScreenCall:
		return m.updateCall(msg)
	case ScreenManual:
		return m.updateManual(msg)
	}
	return m.updateMain(msg)
}

func (m Model) updateMain(msg tea.Msg) (tea.Model, tea.Cmd) {
	var (
		action pages.MainMenuAction
		data   string
		cmd    tea.Cmd
	)

	m.main, action, data, cmd = m.main.Update(msg)

	switch action {
	case pages.ActionCreate:
		if strings.TrimSpace(m.server) == "" {
			m.main.SetFailure(client.ErrServerNotConfigured.Error())
			return m, nil
		}
		return m, m.startEngine(session.Invite{}, false)

	case pages.ActionJoin:
		invite, err := session.DecodeInvite(data)
		if err != nil {
			m.main.SetError("cannot join: " + err.Error())
			return m, cmd
		}
		server := strings.TrimSpace(invite.Server)
		if server == "" {
			server = strings.TrimSpace(m.server)
		}
		if server == "" {
			m.main.SetFailure(client.ErrServerNotConfigured.Error())
			return m, nil
		}
		m.server = server
		return m, m.startEngine(invite, true)

	case pages.ActionManual:
		return m.openManual()

	case pages.ActionRename:
		return m.rename(data), cmd

	case pages.ActionSetServer:
		return m.setServer(data), cmd

	case pages.ActionCheckUpdate:
		return m, checkUpdate()

	case pages.ActionInstallUpdate:
		m.main.SetUpdating(true)
		return m, installUpdate(data)

	case pages.ActionUninstall:
		return m, uninstallWorm()
	}

	return m, cmd
}

func (m Model) setServer(server string) Model {
	updated := m.cfg.WithServer(server)
	if err := updated.Save(); err != nil {
		m.main.SetFailure("cannot save " + config.FileName + ": " + err.Error())
		return m
	}

	m.cfg = updated
	m.server = server
	m.main.SetServer(server)
	if server == "" {
		m.main.SetNotice("server cleared — manual exchange works without one")
	} else {
		m.main.SetNotice("server saved to " + updated.Path())
	}
	return m
}

func (m Model) rename(name string) Model {
	if err := config.ValidateName(name); err != nil {
		m.main.SetError(err.Error())
		return m
	}

	updated := m.cfg.WithName(name)
	if err := updated.Save(); err != nil {
		m.main.SetError("cannot save " + config.FileName + ": " + err.Error())
		return m
	}

	m.cfg = updated
	m.selfName = updated.Name
	m.main.SetUser(updated.Name)
	m.main.SetNotice("name saved to " + updated.Path())
	return m
}

func (m Model) startEngine(invite session.Invite, incoming bool) tea.Cmd {
	server := strings.TrimSpace(m.server)
	if incoming {
		server = strings.TrimSpace(invite.Server)
	}
	name := m.selfName
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()
		return m.connectSession(ctx, server, name, invite, incoming)
	}
}

func (m Model) connectSession(ctx context.Context, server, name string, invite session.Invite, incoming bool) tea.Msg {
	sess, err := m.register(ctx, server, name)
	if err != nil {
		return engineReadyMsg{err: err}
	}

	eng := m.newEngine(engine.Config{
		Name:       sess.Name,
		ICEServers: sess.ICEServers,
		Audio:      audio.Default,
		Signaling: client.SignalingConfig{
			URL:   sess.WSURL,
			Token: sess.Token,
		},
	})

	if err := eng.Connect(ctx); err != nil {
		_ = eng.Close()
		return engineReadyMsg{err: err}
	}

	if !incoming {
		if err := eng.CreateCall(ctx); err != nil {
			_ = eng.Close()
			return engineReadyMsg{err: err}
		}
	}

	return engineReadyMsg{eng: eng, invite: invite, incoming: incoming}
}

func (m Model) openManual() (tea.Model, tea.Cmd) {
	m.closeEngine()
	m.manualUI = pages.NewManual(m.selfName)
	m.manualUI.W, m.manualUI.H = m.Window.Width, m.Window.Heigh
	m.manual = engine.NewManual(engine.Config{Name: m.selfName, Audio: audio.Default})
	m.screen = ScreenManual
	return m, waitManual(m.manual)
}

func (m Model) handleManualCode(msg manualCodeMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.manualUI.SetError(msg.err.Error())
		return m, nil
	}
	m.manualUI.SetCode(msg.code)
	m.manualUI.SetStage(msg.stage)
	if msg.stage == pages.ManualAwaitAnswer {
		m.manualUI.SetStatus("offer ready")
		m.manualUI.SetNotice("send your offer code to the peer, then paste the answer")
	} else {
		m.manualUI.SetStatus("answer ready")
		m.manualUI.SetNotice("send your answer code back to the caller")
	}
	return m, nil
}

func (m Model) handleManualDone(msg manualDoneMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.manualUI.SetError(msg.err.Error())
		return m, nil
	}
	m.manualUI.SetStatus("connecting")
	m.manualUI.SetNotice("answer applied, waiting for ICE")
	return m, nil
}

func (m Model) handleManualEvent(ev engine.Event) (tea.Model, tea.Cmd) {
	switch ev.Kind {
	case engine.EventMedia:
		if ev.Connected {
			m.manualUI.SetConnected()
		} else {
			m.manualUI.SetStatus(strings.TrimPrefix(ev.Message, "media "))
		}
	case engine.EventStats:
		m.manualUI.SetQuality(ev.Message)
	case engine.EventError:
		m.manualUI.SetError(ev.Message)
	case engine.EventNotice:
		m.manualUI.SetNotice(ev.Message)
	}
	return m, waitManual(m.manual)
}

func (m Model) updateManual(msg tea.Msg) (tea.Model, tea.Cmd) {
	var (
		action pages.ManualAction
		data   string
		cmd    tea.Cmd
	)

	m.manualUI, action, data, cmd = m.manualUI.Update(msg)
	if m.manual == nil {
		return m, cmd
	}

	manual := m.manual
	timeout := func() (context.Context, context.CancelFunc) {
		return context.WithTimeout(context.Background(), 30*time.Second)
	}

	switch action {
	case pages.ManualCreateOffer:
		return m, func() tea.Msg {
			ctx, cancel := timeout()
			defer cancel()
			sig, err := manual.Offer(ctx)
			if err != nil {
				return manualCodeMsg{err: err}
			}
			code, err := sig.Encode()
			return manualCodeMsg{code: code, stage: pages.ManualAwaitAnswer, err: err}
		}

	case pages.ManualAnswerOffer:
		return m, func() tea.Msg {
			offer, err := session.DecodeSignal(data)
			if err != nil {
				return manualCodeMsg{err: err}
			}
			ctx, cancel := timeout()
			defer cancel()
			answer, err := manual.Answer(ctx, offer)
			if err != nil {
				return manualCodeMsg{err: err}
			}
			code, err := answer.Encode()
			return manualCodeMsg{code: code, stage: pages.ManualAwaitConnect, err: err}
		}

	case pages.ManualApplyAnswer:
		return m, func() tea.Msg {
			answer, err := session.DecodeSignal(data)
			if err != nil {
				return manualDoneMsg{err: err}
			}
			return manualDoneMsg{err: manual.Connect(answer)}
		}

	case pages.ManualAddCandidates:
		return m, func() tea.Msg {
			code, err := session.DecodeSignal(data)
			if err != nil {
				return manualDoneMsg{err: err}
			}
			return manualDoneMsg{err: manual.AddCandidates(code)}
		}

	case pages.ManualToggleMute:
		m.manualUI.Muted = !m.manualUI.Muted
		manual.SetMuted(m.manualUI.Muted)
		return m, nil

	case pages.ManualCopy:
		if data == "" {
			return m, nil
		}
		return m, copyToClipboard(data)

	case pages.ManualSave:
		if data == "" {
			return m, nil
		}
		return m, saveSignalCode(data)

	case pages.ManualLeave:
		return m.leaveManual()
	}

	return m, cmd
}

func saveSignalCode(code string) tea.Cmd {
	return func() tea.Msg {
		dir, err := home.Dir()
		if err != nil {
			return manualDoneMsg{err: err}
		}
		path := filepath.Join(dir, "last-signal.txt")
		if err := os.WriteFile(path, []byte(code+"\n"), 0o600); err != nil {
			return manualDoneMsg{err: err}
		}
		return savedSignalMsg(path)
	}
}

func (m Model) leaveManual() (tea.Model, tea.Cmd) {
	if m.manual != nil {
		_ = m.manual.Close()
		m.manual = nil
	}
	m.manualUI = pages.ManualModel{}
	m.screen = ScreenMain
	m.main.SetNotice("")
	return m, nil
}

func (m Model) handleEngineReady(msg engineReadyMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.main.SetFailure(msg.err.Error())
		return m, nil
	}

	m.eng = msg.eng
	m.call = pages.NewCall(m.selfName)
	m.call.W, m.call.H = m.Window.Width, m.Window.Heigh
	m.screen = ScreenCall

	if msg.incoming {
		m.invite = msg.invite
		m.server = msg.invite.Server
		m.call.Room = shortRoom(msg.invite.CallID)
		m.call.State = "ringing"
		m.call.Role = "callee"
		m.call.Peer = msg.invite.Caller
		m.call.SetNoticeOK("incoming call — press a to accept")
	}

	return m, waitEngine(m.eng)
}

func (m Model) handleEngineEvent(ev engine.Event) (tea.Model, tea.Cmd) {
	switch ev.Kind {
	case engine.EventState:
		wasTerminal := m.call.Terminal()
		m.call.SetState(ev.Call)
		if ev.Call.Invite != "" && ev.Call.CallID != "" {
			code, err := session.Invite{
				Server: m.server,
				CallID: ev.Call.CallID,
				Token:  ev.Call.Invite,
				Caller: ev.Call.Self.Name,
			}.Encode()
			if err == nil {
				m.call.Invite = code
			}
		}
		switch ev.Call.State {
		case "connected":
			m.call.SetNoticeOK("audio connected")
		case "ringing":
			if ev.Call.Role == "caller" {
				m.call.SetNoticeOK("waiting for the other side")
			}
		}
		if m.call.Terminal() && !wasTerminal {
			reason := ev.Call.Reason
			if reason == "" {
				reason = ev.Call.State
			}
			m.call.SetNotice("", "call "+reason)
			m.closeEngine()
		}

	case engine.EventNotice:
		m.call.SetNoticeOK(ev.Message)

	case engine.EventStats:
		m.call.SetQuality(ev.Message)

	case engine.EventError:
		m.call.SetError(ev.Message)
	}

	return m, waitEngine(m.eng)
}

func (m Model) updateCall(msg tea.Msg) (tea.Model, tea.Cmd) {
	var action pages.CallAction
	m.call, action, _ = m.call.Update(msg)

	switch action {
	case pages.CallAccept:
		return m, m.callCommand(action, false, func(ctx context.Context) error {
			if m.eng == nil {
				return nil
			}
			return m.eng.Accept(ctx, m.invite.CallID, m.invite.Token)
		})

	case pages.CallReject:
		return m, m.callCommand(action, true, func(ctx context.Context) error {
			if m.eng == nil {
				return nil
			}
			return m.eng.Reject(ctx, m.invite.CallID, m.invite.Token, "declined")
		})

	case pages.CallLeave:
		return m.callLeave()

	case pages.CallToggleMute:
		if m.eng == nil {
			return m, nil
		}
		m.call.Muted = !m.call.Muted
		m.eng.SetMuted(m.call.Muted)
		return m, nil

	case pages.CallCopyInvite:
		if m.call.Invite == "" {
			return m, nil
		}
		return m, copyToClipboard(m.call.Invite)
	}

	return m, nil
}

func (m Model) callLeave() (tea.Model, tea.Cmd) {
	if m.eng == nil {
		return m.toMain(), nil
	}
	callID := m.invite.CallID
	if m.call.State == "ringing" && m.call.Role == "caller" {
		return m, m.callCommand(pages.CallLeave, true, func(ctx context.Context) error {
			if m.eng == nil {
				return nil
			}
			return m.eng.Cancel(ctx, callID)
		})
	}
	if !m.call.Terminal() && m.call.State != "" {
		return m, m.callCommand(pages.CallLeave, true, func(ctx context.Context) error {
			if m.eng == nil {
				return nil
			}
			return m.eng.End(ctx, callID, "hangup")
		})
	}
	return m.toMain(), nil
}

func (m Model) callCommand(action pages.CallAction, toMain bool, fn func(context.Context) error) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return callCommandMsg{action: action, toMain: toMain, err: fn(ctx)}
	}
}

func (m Model) handleCallCommand(msg callCommandMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.call.SetError(msg.err.Error())
		return m, nil
	}
	switch msg.action {
	case pages.CallReject:
		m.call.SetNoticeOK("call declined")
	case pages.CallLeave:
		m.call.SetNoticeOK("call ended")
	}
	if msg.toMain {
		return m.toMain(), nil
	}
	return m, nil
}

func (m Model) toMain() Model {
	m.closeEngine()
	m.screen = ScreenMain
	m.call = pages.CallModel{}
	m.invite = session.Invite{}
	m.main.SetNotice("")
	return m
}

func (m *Model) closeEngine() {
	if m.eng == nil {
		return
	}
	_ = m.eng.Close()
	m.eng = nil
}

func waitManual(manual *engine.Manual) tea.Cmd {
	if manual == nil {
		return nil
	}
	ch := manual.Events()
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return nil
		}
		return manualEventMsg(ev)
	}
}

func waitEngine(eng CallEngine) tea.Cmd {
	if eng == nil {
		return nil
	}
	ch := eng.Events()
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return nil
		}
		return engineEventMsg(ev)
	}
}

func shortRoom(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}

func (m Model) View() tea.View {
	var v tea.View
	v.AltScreen = true
	v.WindowTitle = "worm"

	if m.fatal != nil {
		v.AltScreen = false
		path := m.cfg.Path()
		if path == "" {
			path = config.FileName
		}
		v.SetContent(fmt.Sprintf("worm: cannot read %s: %v\n", path, m.fatal))
		return v
	}

	switch m.screen {
	case ScreenCall:
		v.SetContent(m.call.View())
	case ScreenManual:
		v.SetContent(m.manualUI.View())
	default:
		v.SetContent(m.main.View())
	}
	return v
}
