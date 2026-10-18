package tui

import (
	"fmt"
	"net"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/Alice088/worm/internal/config"
	"github.com/Alice088/worm/internal/session"
	"github.com/Alice088/worm/internal/tui/pages"
)

// Screen identifies which screen is currently shown.
type Screen int

const (
	// ScreenMain is the main screen (join / create a call).
	ScreenMain Screen = iota
	// ScreenCall is the in-call screen.
	ScreenCall
)

// Model is the root application model that routes between screens.
type Model struct {
	Window  Window
	Version string
	Name    string

	screen Screen
	main   pages.MainModel
	call   pages.CallModel

	// cfg is the persisted user configuration (name etc.).
	cfg config.Config

	// selfName is the display name of this client in a call.
	selfName string

	// fatal holds an unrecoverable startup error to show before quitting.
	fatal error
}

// PeerClient describes another peer of the call.
type PeerClient struct {
	Addr net.Addr
	Name string
}

// Window is the terminal size.
type Window struct {
	Heigh int
	Width int
}

// InitialModel loads branding assets and builds the root model.
func InitialModel(v string) Model {
	b, err := os.ReadFile("./assets/worm")
	if err != nil {
		fmt.Println("failed read worm.txt")
		os.Exit(1)
	}

	return newModel(v, string(b), config.DefaultPath())
}

// newModel builds the root model bound to a specific config path.
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

	// Fall back to a derived name when the config has none yet.
	self := strings.TrimSpace(cfg.Name)
	if self == "" {
		self = defaultName()
	}

	appName := strings.TrimRight(branding, "\n")

	return Model{
		Version:  version,
		Name:     branding,
		screen:   ScreenMain,
		selfName: self,
		cfg:      cfg,
		fatal:    loadErr,
		main:     pages.NewMain(appName, version, self, cfg.Path()),
	}
}

// defaultName derives a short local display name.
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

// copiedMsg reports the outcome of a clipboard write.
type copiedMsg struct{ err error }

// copyToClipboard copies s to the system clipboard via OSC52.
func copyToClipboard(s string) tea.Cmd {
	return tea.Sequence(tea.SetClipboard(s), func() tea.Msg {
		return copiedMsg{}
	})
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd {
	return nil
}

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.Window.Width, m.Window.Heigh = msg.Width, msg.Height
		var cmd tea.Cmd
		m.main, _, _, cmd = m.main.Update(msg)
		m.call, _, _ = m.call.Update(msg)
		return m, cmd

	case tea.KeyPressMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}

	case copiedMsg:
		if msg.err != nil {
			m.call.SetNotice("", "clipboard unavailable: "+msg.err.Error())
		} else {
			m.call.SetNotice("connect-data copied to clipboard", "")
		}
		return m, nil
	}

	switch m.screen {
	case ScreenCall:
		return m.updateCall(msg)
	default:
		return m.updateMain(msg)
	}
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
		return m.startCall(""), cmd

	case pages.ActionJoin:
		cd, err := session.Decode(data)
		if err != nil {
			m.main.SetError("cannot join: " + err.Error())
			return m, cmd
		}
		m.selfName = strings.TrimSpace(m.cfg.Name)
		if m.selfName == "" {
			m.selfName = defaultName()
		}
		return m.startCall(cd.Room), cmd
	case pages.ActionRename:
		return m.rename(data), cmd
	}

	return m, cmd
}

// rename persists a new display name to the config file.
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

	// Keep an open call in sync with the new identity.
	if m.screen == ScreenCall {
		m.call.Self = updated.Name
	}
	return m
}

// startCall switches to the call screen for the given room.
func (m Model) startCall(room string) Model {
	if room == "" {
		room = "room-" + session.RandomID(3)
	}

	blob, err := session.Encode(session.ConnectData{Room: room, Name: m.selfName})
	if err != nil {
		blob = ""
	}

	m.call = pages.NewCall(room, m.selfName, blob, true)
	m.call.W, m.call.H = m.Window.Width, m.Window.Heigh
	m.screen = ScreenCall
	return m
}

func (m Model) updateCall(msg tea.Msg) (tea.Model, tea.Cmd) {
	var action pages.CallAction
	m.call, action, _ = m.call.Update(msg)

	switch action {
	case pages.CallLeave:
		m.screen = ScreenMain
		return m, nil

	case pages.CallToggleMute:
		m.call.Muted = !m.call.Muted
		return m, nil

	case pages.CallCopyConnectData:
		if m.call.Connect == "" {
			return m, nil
		}
		return m, copyToClipboard(m.call.Connect)
	}

	return m, nil
}

// View implements tea.Model.
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
	default:
		v.SetContent(m.main.View())
	}
	return v
}
