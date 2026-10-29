package pages

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
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
	// ActionSetServer asks the app to persist a signaling server URL.
	ActionSetServer
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
	// ModeServer is the "set signaling server" form.
	ModeServer
)

// menuIndex values. Create is the primary call to action, so it is on top.
const (
	menuCreate = iota
	menuJoin
	menuManual
	menuRename
	menuServer
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

	mode       MainMode
	index      int
	joinIn     codeInput
	nameIn     textinput.Model
	serverIn   textinput.Model
	err        string
	notice     string
	noticeErr  string
	savePath   string
	latest     string
	notes      string
	latestErr  string
	noReleases bool
	checking   bool
	busy       bool
	progStage  string
	progDone   int64
	progTotal  int64
	selfPath   string
}

// NewMain builds the main screen model.
func NewMain(appName, version, userName, savePath string) MainModel {
	join := newCodeInput("invite code", 4096)

	name := textinput.New()
	name.Placeholder = "display name"
	name.Prompt = "✎ "
	name.CharLimit = 32
	name.SetValue(userName)

	server := textinput.New()
	server.Placeholder = "server URL (optional)"
	server.Prompt = "⇄ "
	server.CharLimit = 256

	return MainModel{
		AppName:  appName,
		Version:  version,
		UserName: userName,
		mode:     ModeMenu,
		index:    menuCreate,
		joinIn:   join,
		nameIn:   name,
		serverIn: server,
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
		case ModeServer:
			return m.updateServer(msg)
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
	case ModeServer:
		var cmd tea.Cmd
		m.serverIn, cmd = m.serverIn.Update(msg)
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
	case "6", "s":
		return m.openServer()
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
		case menuServer:
			return m.openServer()
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
	m.notes = ""
	m.latestErr = ""
	m.noReleases = false
	m.checking = true
	m.noticeErr = ""
	return m, ActionCheckUpdate, "", nil
}

func (m MainModel) openServer() (MainModel, MainMenuAction, string, tea.Cmd) {
	m.mode = ModeServer
	m.err = ""
	m.notice = ""
	m.serverIn.SetValue(m.Server)
	m.serverIn.CursorEnd()
	return m, ActionNone, "", m.serverIn.Focus()
}

func (m MainModel) updateServer(msg tea.KeyPressMsg) (MainModel, MainMenuAction, string, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = ModeMenu
		m.err = ""
		m.serverIn.Blur()
		return m, ActionNone, "", nil
	case "enter":
		server := strings.TrimSpace(m.serverIn.Value())
		if err := validateServer(server); err != nil {
			m.err = err.Error()
			return m, ActionNone, "", nil
		}
		m.mode = ModeMenu
		m.serverIn.Blur()
		return m, ActionSetServer, server, nil
	}

	var cmd tea.Cmd
	m.serverIn, cmd = m.serverIn.Update(msg)
	m.err = ""
	return m, ActionNone, "", cmd
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
	if m.serverIn.Value() != server {
		m.serverIn.SetValue(server)
	}
}

// SetNotice shows a transient success line on the menu.
func (m *MainModel) SetNotice(msg string) {
	m.notice = msg
}

func (m *MainModel) SetNoReleases() {
	m.checking = false
	m.noReleases = true
	m.latest = ""
	m.notes = ""
	m.latestErr = ""
}

// SetLatest records the newest release tag and its notes fetched from GitHub.
func (m *MainModel) SetLatest(tag, notes string, err error) {
	m.checking = false
	m.noReleases = false
	m.latest = tag
	m.notes = notes
	if err != nil {
		m.latestErr = err.Error()
		return
	}
	m.latestErr = ""
}

// SetUpdating reports whether an update is running.
func (m *MainModel) SetUpdating(busy bool) {
	m.busy = busy
	if !busy {
		m.progStage = ""
		m.progDone = 0
		m.progTotal = 0
	}
}

func (m *MainModel) SetProgress(stage string, done, total int64) {
	m.progStage = stage
	m.progDone = done
	m.progTotal = total
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
	case ModeServer:
		body = m.viewServer()
	default:
		body = m.viewMenu()
	}

	meta := m.UserName
	if m.Server != "" {
		meta += " · " + m.Server
	}

	card := StyleCard.Render(lipgloss.JoinVertical(lipgloss.Left,
		StyleTitle.Render(m.AppName),
		StyleSubtitle.Render(m.Version+" · "+meta),
		"",
		body,
	))

	if m.W > 0 && m.H > 0 {
		return clipHeight(lipgloss.Place(m.W, m.H, lipgloss.Center, lipgloss.Center, card), m.H)
	}
	return card
}

func (m MainModel) viewMenu() string {
	entries := [menuCount]struct {
		label string
		key   string
	}{
		{"Create a call", "1"},
		{"Join a call", "2"},
		{"Manual exchange", "m"},
		{"Change my name", "3"},
		{"Set signaling server", "6"},
		{"Version & updates", "4"},
		{"Uninstall worm", "5"},
	}

	labelW := 0
	for _, e := range entries {
		labelW = max(labelW, lipgloss.Width(e.label))
	}

	var rows []string
	for i, e := range entries {
		if i == menuRename {
			rows = append(rows, "")
		}
		if i == menuVersion {
			rows = append(rows, "")
		}

		cursor := "  "
		label := StyleText.Render(e.label)
		if i == m.index {
			cursor = lipgloss.NewStyle().Foreground(ColAccent).Render("▌ ")
			label = lipgloss.NewStyle().Bold(true).Foreground(ColAccent).Render(e.label)
		}

		pad := strings.Repeat(" ", labelW-lipgloss.Width(e.label)+2)
		rows = append(rows, cursor+label+StyleMuted.Render(pad+e.key))
	}

	rows = append(rows, "")
	switch {
	case m.noticeErr != "":
		rows = append(rows, StyleError.Render(m.noticeErr))
	case m.notice != "":
		rows = append(rows, StyleOK.Render(m.notice))
	default:
		rows = append(rows, StyleHint.Render("↑↓ · enter"))
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
		StyleHint.Render("enter · esc"),
	)
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func (m MainModel) viewName() string {
	lines := []string{
		StyleLabel.Render("DISPLAY NAME"),
		m.fieldBox(m.nameIn.View()),
	}
	if m.err != "" {
		lines = append(lines, StyleError.Render("✗ "+m.err))
	}
	lines = append(lines,
		"",
		StyleHint.Render("enter · esc"),
	)
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func (m MainModel) viewVersion() string {
	lines := []string{
		StyleLabel.Render("INSTALLED"),
		StyleText.Render(m.Version),
		"",
		StyleLabel.Render("LATEST"),
	}
	switch {
	case m.checking:
		lines = append(lines, StyleMuted.Render("checking github…"))
	case m.noReleases:
		lines = append(lines, StyleMuted.Render("no releases yet"))
	case m.latestErr != "":
		lines = append(lines, StyleError.Render(m.latestErr))
	case m.updateAvailable():
		lines = append(lines, StyleOK.Render(m.latest+" · update available"))
	default:
		lines = append(lines, StyleMuted.Render(m.latest+" · up to date"))
	}

	if notes := m.releaseNotes(); m.updateAvailable() && len(notes) > 0 {
		lines = append(lines, "", StyleLabel.Render("WHAT'S NEW"), "")
		lines = append(lines, StyleMuted.Render(strings.Join(notes, "\n")))
	}

	lines = append(lines, "")
	switch {
	case m.busy:
		lines = append(lines, m.progressLines()...)
	case m.noReleases:
		lines = append(lines, StyleHint.Render("r · esc"))
	case m.updateAvailable():
		lines = append(lines, StyleHint.Render("enter update · r · esc"))
	default:
		lines = append(lines, StyleHint.Render("r · esc"))
	}
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func (m MainModel) viewServer() string {
	lines := []string{
		StyleLabel.Render("SIGNALING SERVER"),
		m.fieldBox(m.serverIn.View()),
	}
	if m.err != "" {
		lines = append(lines, StyleError.Render("✗ "+m.err))
	}
	lines = append(lines,
		"",
		StyleHint.Render("enter · esc"),
	)
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func (m MainModel) progressLines() []string {
	if m.progStage == "" {
		return []string{StyleHint.Render("installing…")}
	}
	label := map[string]string{
		"download":  "downloading",
		"checksums": "downloading checksums",
		"verify":    "verifying checksum",
		"extract":   "extracting",
		"install":   "installing",
		"done":      "done",
	}[m.progStage]
	if label == "" {
		label = m.progStage
	}
	if m.progTotal <= 0 {
		if m.progDone > 0 {
			return []string{StyleMuted.Render(label + " · " + formatBytes(m.progDone))}
		}
		return []string{StyleMuted.Render(label + "…")}
	}
	percent := min(100, int(m.progDone*100/m.progTotal))
	width := min(24, max(8, m.W-40))
	filled := width * percent / 100
	bar := lipgloss.NewStyle().Foreground(ColAccent).Render(strings.Repeat("━", filled)) +
		StyleMuted.Render(strings.Repeat("━", width-filled))
	return []string{
		StyleMuted.Render(label),
		bar + " " + StyleText.Render(fmt.Sprintf("%d%%", percent)),
		StyleMuted.Render(fmt.Sprintf("%s / %s", formatBytes(m.progDone), formatBytes(m.progTotal))),
	}
}

func formatBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	value := float64(n)
	for _, suffix := range []string{"KB", "MB", "GB"} {
		value /= unit
		if value < unit {
			return fmt.Sprintf("%.1f %s", value, suffix)
		}
	}
	return fmt.Sprintf("%.1f TB", value/unit)
}

func (m MainModel) releaseNotes() []string {
	width, limit := 64, 8
	if m.W > 0 {
		width = min(width, m.W-24)
	}
	if m.H > 0 {
		limit = min(limit, max(2, m.H-18))
	}
	if width < 24 {
		width = 24
	}
	return notesLines(m.notes, width, limit)
}

func notesLines(body string, width, limit int) []string {
	body = strings.ReplaceAll(body, "\r\n", "\n")
	var out []string
	for _, raw := range strings.Split(body, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "<!--") {
			continue
		}
		if strings.Contains(strings.ToLower(line), "full changelog") {
			continue
		}
		line = releasePrefix.ReplaceAllString(line, "")
		line = strings.NewReplacer("**", "", "`", "", "__", "").Replace(line)
		line = releaseLink.ReplaceAllString(line, "$1")
		line = releaseByline.ReplaceAllString(line, "")
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		for _, wrapped := range strings.Split(wrap(line, width), "\n") {
			out = append(out, wrapped)
			if len(out) >= limit {
				return out
			}
		}
	}
	return out
}

var (
	releaseLink   = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
	releasePrefix = regexp.MustCompile(`^[#>*+~-]+\s*`)
	releaseByline = regexp.MustCompile(`\s+by @\S+ in \S+\s*$`)
)

func validateServer(server string) error {
	if server == "" {
		return nil
	}
	parsed, err := url.Parse(server)
	if err != nil {
		return errors.New("not a valid URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return errors.New("URL must start with http:// or https://")
	}
	if parsed.Host == "" {
		return errors.New("URL has no host")
	}
	return nil
}

func (m MainModel) viewUninstall() string {
	target := m.selfPath
	if target == "" {
		target = "worm"
	}
	lines := []string{
		StyleLabel.Render("UNINSTALL"),
		StyleMuted.Render(target),
		StyleMuted.Render("config kept in ~/.worm"),
		"",
		StyleHint.Render("y · esc"),
	}
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func (m MainModel) updateAvailable() bool {
	return m.latest != "" && m.latest != m.Version
}

func (m MainModel) fieldBox(content string) string {
	return lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(ColAccent2).
		Padding(0, 1).
		Width(max(16, min(64, m.W-20))).
		Render(content)
}
