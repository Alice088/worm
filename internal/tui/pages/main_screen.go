package pages

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/Alice088/worm/internal/config"
	"github.com/Alice088/worm/internal/session"
)

// MainMenuAction identifies what the user picked on the main screen.
type MainMenuAction int

const (
	// ActionNone means "nothing to do yet".
	ActionNone MainMenuAction = iota
	// ActionCreate asks the app to host a new room.
	ActionCreate
	// ActionJoin asks the app to join the room described by the payload.
	ActionJoin
	// ActionRename asks the app to persist a new display name (payload).
	ActionRename
	// ActionManual opens the offline SDP/ICE exchange screen.
	ActionManual
	// ActionCheckUpdate asks the app to fetch the latest release tag.
	ActionCheckUpdate
	// ActionInstallUpdate asks the app to install the latest release.
	ActionInstallUpdate
	// ActionUninstall asks the app to remove the installed program.
	ActionUninstall
)

// MainMode is the input mode of the main screen.
type MainMode int

const (
	// ModeMenu is the menu.
	ModeMenu MainMode = iota
	// ModeJoin is the "paste connect-data" form.
	ModeJoin
	// ModeName is the "change display name" form.
	ModeName
	// ModeVersion shows the installed version and update controls.
	ModeVersion
	// ModeUninstall asks for confirmation before removing the program.
	ModeUninstall
)

// menuIndex values. Create is the primary call to action, so it is on top.
const (
	menuCreate = iota
	menuJoin
	menuManual
	menuRename
	menuVersion
	menuUninstall
	menuCount
)

// MainModel is the "Main screen": create a call, join one, or rename yourself.
type MainModel struct {
	AppName  string
	Version  string
	UserName string
	Server   string

	W, H int

	mode      MainMode
	index     int
	joinIn    codeInput
	nameIn    textinput.Model
	err       string
	notice    string
	noticeErr string
	savePath  string
	latest    string
	latestErr string
	checking  bool
	busy      bool
	selfPath  string
}

// NewMain builds the main screen model.
func NewMain(appName, version, userName, savePath string) MainModel {
	join := newCodeInput("hex invite code", 4096)

	name := textinput.New()
	name.Placeholder = "your display name"
	name.Prompt = "✎ "
	name.CharLimit = 32
	name.SetValue(userName)

	return MainModel{
		AppName:  appName,
		Version:  version,
		UserName: userName,
		mode:     ModeMenu,
		index:    menuCreate,
		joinIn:   join,
		nameIn:   name,
		savePath: savePath,
	}
}

// Update handles messages for the main screen, returning the resulting action
// and its payload (connect-data for ActionJoin, the new name for ActionRename).
func (m MainModel) Update(msg tea.Msg) (MainModel, MainMenuAction, string, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.W, m.H = msg.Width, msg.Height
		w := max(16, min(64, msg.Width-20))
		m.joinIn.SetWidth(w)
		m.nameIn.SetWidth(w)
		return m, ActionNone, "", nil

	case tea.KeyPressMsg:
		switch m.mode {
		case ModeJoin:
			return m.updateJoin(msg)
		case ModeName:
			return m.updateName(msg)
		case ModeVersion:
			return m.updateVersion(msg)
		case ModeUninstall:
			return m.updateUninstall(msg)
		default:
			return m.updateMenu(msg)
		}
	}

	// Let the focused text input consume other messages (cursor blink, paste).
	switch m.mode {
	case ModeJoin:
		var cmd tea.Cmd
		m.joinIn, cmd = m.joinIn.Update(msg)
		return m, ActionNone, "", cmd
	case ModeName:
		var cmd tea.Cmd
		m.nameIn, cmd = m.nameIn.Update(msg)
		return m, ActionNone, "", cmd
	}
	return m, ActionNone, "", nil
}

func (m MainModel) updateMenu(msg tea.KeyPressMsg) (MainModel, MainMenuAction, string, tea.Cmd) {
	// Any navigation clears stale feedback.
	switch msg.String() {
	case "up", "k", "shift+tab":
		m.index = (m.index - 1 + menuCount) % menuCount
		m.notice = ""
	case "down", "j", "tab":
		m.index = (m.index + 1) % menuCount
		m.notice = ""
	case "1":
		return m, ActionCreate, "", nil
	case "2":
		return m.openJoin()
	case "3", "r":
		return m.openRename()
	case "4", "u":
		return m.openVersion()
	case "5", "x":
		return m.openUninstall()
	case "m":
		return m, ActionManual, "", nil
	case "n":
		return m, ActionCreate, "", nil
	case "enter", "space":
		switch m.index {
		case menuCreate:
			return m, ActionCreate, "", nil
		case menuJoin:
			return m.openJoin()
		case menuManual:
			return m, ActionManual, "", nil
		case menuRename:
			return m.openRename()
		case menuVersion:
			return m.openVersion()
		default:
			return m.openUninstall()
		}
	}
	return m, ActionNone, "", nil
}

func (m MainModel) openVersion() (MainModel, MainMenuAction, string, tea.Cmd) {
	m.mode = ModeVersion
	m.latest = ""
	m.latestErr = ""
	m.checking = true
	m.noticeErr = ""
	return m, ActionCheckUpdate, "", nil
}

func (m MainModel) openUninstall() (MainModel, MainMenuAction, string, tea.Cmd) {
	m.mode = ModeUninstall
	m.noticeErr = ""
	return m, ActionNone, "", nil
}

func (m MainModel) updateVersion(msg tea.KeyPressMsg) (MainModel, MainMenuAction, string, tea.Cmd) {
	if m.busy {
		return m, ActionNone, "", nil
	}
	switch msg.String() {
	case "esc", "q":
		m.mode = ModeMenu
		return m, ActionNone, "", nil
	case "r":
		m.latest = ""
		m.latestErr = ""
		m.checking = true
		return m, ActionCheckUpdate, "", nil
	case "enter":
		if m.updateAvailable() {
			m.busy = true
			return m, ActionInstallUpdate, m.latest, nil
		}
	}
	return m, ActionNone, "", nil
}

func (m MainModel) updateUninstall(msg tea.KeyPressMsg) (MainModel, MainMenuAction, string, tea.Cmd) {
	switch msg.String() {
	case "esc", "n", "q":
		m.mode = ModeMenu
		return m, ActionNone, "", nil
	case "y":
		return m, ActionUninstall, "", nil
	}
	return m, ActionNone, "", nil
}

// openJoin switches to the paste form and focuses its input.
func (m MainModel) openJoin() (MainModel, MainMenuAction, string, tea.Cmd) {
	m.mode = ModeJoin
	m.err = ""
	m.notice = ""
	return m, ActionNone, "", m.joinIn.Focus()
}

// openRename switches to the name form, pre-filled with the current name.
func (m MainModel) openRename() (MainModel, MainMenuAction, string, tea.Cmd) {
	m.mode = ModeName
	m.err = ""
	m.notice = ""
	m.nameIn.SetValue(m.UserName)
	m.nameIn.CursorEnd()
	return m, ActionNone, "", m.nameIn.Focus()
}

func (m MainModel) updateJoin(msg tea.KeyPressMsg) (MainModel, MainMenuAction, string, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = ModeMenu
		m.err = ""
		m.joinIn.Blur()
		return m, ActionNone, "", nil
	case "enter":
		code := strings.TrimSpace(m.joinIn.Value())
		if code == "" {
			m.err = "invite code is empty"
			return m, ActionNone, "", nil
		}
		if _, err := session.DecodeInvite(code); err != nil {
			if errors.Is(err, session.ErrInvalid) {
				m.err = "not a valid invite code"
			} else {
				m.err = err.Error()
			}
			return m, ActionNone, "", nil
		}
		return m, ActionJoin, code, nil
	}

	var cmd tea.Cmd
	m.joinIn, cmd = m.joinIn.Update(msg)
	m.err = ""
	return m, ActionNone, "", cmd
}

func (m MainModel) updateName(msg tea.KeyPressMsg) (MainModel, MainMenuAction, string, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = ModeMenu
		m.err = ""
		m.nameIn.Blur()
		return m, ActionNone, "", nil
	case "enter":
		name := strings.TrimSpace(m.nameIn.Value())
		if err := config.ValidateName(name); err != nil {
			m.err = trimErrPrefix(err)
			return m, ActionNone, "", nil
		}
		m.mode = ModeMenu
		m.nameIn.Blur()
		return m, ActionRename, name, nil
	}

	var cmd tea.Cmd
	m.nameIn, cmd = m.nameIn.Update(msg)
	m.err = ""
	return m, ActionNone, "", cmd
}

// trimErrPrefix makes validation errors read nicely inline.
func trimErrPrefix(err error) string {
	msg := err.Error()
	msg = strings.TrimPrefix(msg, "no name configured: ")
	msg = strings.TrimPrefix(msg, "invalid invite code: ")
	return msg
}

// SetUser applies a new display name to the screen.
func (m *MainModel) SetUser(name string) {
	m.UserName = name
	m.nameIn.SetValue(name)
}

// SetServer records the signaling server shown in the header.
func (m *MainModel) SetServer(server string) {
	m.Server = server
}

// SetNotice shows a transient success line on the menu.
func (m *MainModel) SetNotice(msg string) {
	m.notice = msg
}

// SetLatest records the newest release tag fetched from GitHub.
func (m *MainModel) SetLatest(tag string, err error) {
	m.checking = false
	m.latest = tag
	if err != nil {
		m.latestErr = err.Error()
		return
	}
	m.latestErr = ""
}

// SetUpdating reports whether an update is running.
func (m *MainModel) SetUpdating(busy bool) {
	m.busy = busy
}

// SetSelfPath records where the running binary lives.
func (m *MainModel) SetSelfPath(path string) {
	m.selfPath = path
}

// SetFailure shows an error banner on the menu without changing the mode.
func (m *MainModel) SetFailure(msg string) {
	m.noticeErr = msg
	m.notice = ""
}

// SetError shows a validation error on the join form and opens it, so the
// user can fix the input.
func (m *MainModel) SetError(msg string) {
	m.mode = ModeJoin
	m.err = msg
	m.noticeErr = ""
	_ = m.joinIn.Focus()
}

// Mode reports the current input mode. Useful for tests and parents.
func (m MainModel) Mode() MainMode { return m.mode }

// Err reports the current validation error, if any.
func (m MainModel) Err() string { return m.err }

// View renders the main screen.
func (m MainModel) View() string {
	var body string
	switch m.mode {
	case ModeJoin:
		body = m.viewJoin()
	case ModeName:
		body = m.viewName()
	case ModeVersion:
		body = m.viewVersion()
	case ModeUninstall:
		body = m.viewUninstall()
	default:
		body = m.viewMenu()
	}

	subtitle := fmt.Sprintf("worm %s · you: %s", m.Version, m.UserName)
	if m.Server != "" {
		subtitle += StyleMuted.Render("  · server " + m.Server)
	} else {
		subtitle += StyleMuted.Render("  · no server configured")
	}

	card := StyleCard.Render(lipgloss.JoinVertical(lipgloss.Left,
		StyleTitle.Render(" "+m.AppName+" "),
		StyleSubtitle.Render(subtitle),
		"",
		body,
	))

	if m.W > 0 && m.H > 0 {
		return lipgloss.Place(m.W, m.H, lipgloss.Center, lipgloss.Center, card)
	}
	return card
}

func (m MainModel) viewMenu() string {
	entries := [menuCount]struct {
		label string
		desc  string
		key   string
	}{
		{"Create a call", "start a call and share the invite code", "1"},
		{"Join a call", "paste an invite code from another client", "2"},
		{"Manual exchange", "swap SDP + ICE codes with a peer, no server", "m"},
		{"Change my name", "stored in " + shortPath(m.savePath) + " as name=…", "3"},
		{"Version & updates", "shows the installed version and installs the latest", "4"},
		{"Uninstall worm", "removes the program from this machine", "5"},
	}

	var rows []string
	for i, e := range entries {
		selected := i == m.index

		cursor := "  "
		if selected {
			cursor = "▸ "
		}

		label := lipgloss.NewStyle().Foreground(ColText).Render(e.label)
		if selected {
			label = button(e.label, true, "default")
		}

		rows = append(rows,
			cursor+StyleMuted.Render("["+e.key+"]")+" "+label,
			"     "+StyleMuted.Render(e.desc),
		)
	}

	rows = append(rows, "")
	switch {
	case m.noticeErr != "":
		rows = append(rows, StyleError.Render("✗ "+m.noticeErr))
	case m.notice != "":
		rows = append(rows, StyleOK.Render("✓ "+m.notice))
	default:
		rows = append(rows, StyleHint.Render("↑/↓ select · enter activate · ctrl+c quit"))
	}
	return lipgloss.JoinVertical(lipgloss.Left, rows...)
}

func (m MainModel) viewJoin() string {
	lines := []string{
		StyleLabel.Render("INVITE CODE"),
		m.fieldBox(m.joinIn.View()),
	}
	if m.err != "" {
		lines = append(lines, StyleError.Render("✗ "+m.err))
	}
	lines = append(lines,
		"",
		StyleHint.Render("enter join · esc back · ctrl+v paste · ctrl+c quit"),
	)
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func (m MainModel) viewName() string {
	lines := []string{
		StyleLabel.Render("DISPLAY NAME"),
		m.fieldBox(m.nameIn.View()),
		StyleMuted.Render("saved to " + m.savePath),
	}
	if m.err != "" {
		lines = append(lines, StyleError.Render("✗ "+m.err))
	}
	lines = append(lines,
		"",
		StyleHint.Render("enter save · esc cancel · ctrl+c quit"),
	)
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func (m MainModel) viewVersion() string {
	lines := []string{
		StyleLabel.Render("INSTALLED"),
		lipgloss.NewStyle().Foreground(ColAccent2).Render(m.Version),
		"",
		StyleLabel.Render("LATEST"),
	}
	switch {
	case m.checking:
		lines = append(lines, StyleMuted.Render("checking github…"))
	case m.latestErr != "":
		lines = append(lines, StyleError.Render("✗ "+m.latestErr))
	case m.updateAvailable():
		lines = append(lines, StyleOK.Render(m.latest+" — update available"))
	default:
		lines = append(lines, StyleOK.Render(m.latest+" — up to date"))
	}
	lines = append(lines, "")
	switch {
	case m.busy:
		lines = append(lines, StyleHint.Render("installing update…"))
	case m.updateAvailable():
		lines = append(lines, StyleHint.Render("enter update · r recheck · esc back"))
	default:
		lines = append(lines, StyleHint.Render("r recheck · esc back"))
	}
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func (m MainModel) viewUninstall() string {
	target := m.selfPath
	if target == "" {
		target = "worm"
	}
	lines := []string{
		StyleLabel.Render("UNINSTALL WORM"),
		StyleMuted.Render("removes " + target),
		StyleMuted.Render("config in ~/.worm is kept"),
		"",
		StyleError.Render("this cannot be undone"),
		"",
		StyleHint.Render("y remove · esc cancel"),
	}
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func (m MainModel) updateAvailable() bool {
	return m.latest != "" && m.latest != m.Version
}

// shortPath renders a path relative to the user's home as ~/… to keep the
// menu compact.
func shortPath(p string) string {
	if p == "" {
		return ""
	}

	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return p
	}

	rel, err := filepath.Rel(home, p)
	if err != nil || strings.HasPrefix(rel, "..") {
		return p
	}
	return "~" + string(filepath.Separator) + rel
}

func (m MainModel) fieldBox(content string) string {
	return lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(ColAccent2).
		Padding(0, 1).
		Width(max(16, min(64, m.W-20))).
		Render(content)
}
