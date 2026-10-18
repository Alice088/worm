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
)

// menuIndex values. Create is the primary call to action, so it is on top.
const (
	menuCreate = iota
	menuJoin
	menuRename
	menuCount
)

// MainModel is the "Main screen": create a call, join one, or rename yourself.
type MainModel struct {
	AppName  string
	Version  string
	UserName string

	W, H int

	mode     MainMode
	index    int
	joinIn   textinput.Model
	nameIn   textinput.Model
	err      string
	notice   string
	savePath string
}

// NewMain builds the main screen model.
func NewMain(appName, version, userName, savePath string) MainModel {
	join := textinput.New()
	join.Placeholder = "hex connect-data"
	join.Prompt = "› "
	join.CharLimit = 4096

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
	case "n":
		return m, ActionCreate, "", nil
	case "enter", "space":
		switch m.index {
		case menuCreate:
			return m, ActionCreate, "", nil
		case menuJoin:
			return m.openJoin()
		default:
			return m.openRename()
		}
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
			m.err = "connect-data is empty"
			return m, ActionNone, "", nil
		}
		if _, err := session.Decode(code); err != nil {
			if errors.Is(err, session.ErrInvalid) {
				m.err = "not a valid connect-data blob"
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
	msg = strings.TrimPrefix(msg, "invalid connect data: ")
	return msg
}

// SetUser applies a new display name to the screen.
func (m *MainModel) SetUser(name string) {
	m.UserName = name
	m.nameIn.SetValue(name)
}

// SetNotice shows a transient success line on the menu.
func (m *MainModel) SetNotice(msg string) {
	m.notice = msg
}

// SetError shows a validation error on the join form and opens it, so the
// user can fix the input.
func (m *MainModel) SetError(msg string) {
	m.mode = ModeJoin
	m.err = msg
	m.joinIn.Focus()
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
	default:
		body = m.viewMenu()
	}

	subtitle := fmt.Sprintf("worm %s · you: %s", m.Version, m.UserName)
	if m.savePath != "" {
		subtitle += StyleMuted.Render("  · name from " + shortPath(m.savePath))
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
		{"Create a call", "host a new room and share connect-data", "1"},
		{"Join a call", "paste connect-data from another client", "2"},
		{"Change my name", "stored in " + shortPath(m.savePath) + " as name=…", "3"},
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
	if m.notice != "" {
		rows = append(rows, StyleOK.Render("✓ "+m.notice))
	} else {
		rows = append(rows, StyleHint.Render("↑/↓ select · enter activate · ctrl+c quit"))
	}
	return lipgloss.JoinVertical(lipgloss.Left, rows...)
}

func (m MainModel) viewJoin() string {
	lines := []string{
		StyleLabel.Render("CONNECT-DATA"),
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
