package pages

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/Alice088/worm/internal/session"
)

// CallAction is what the user asked for on the call screen.
type CallAction int

const (
	// CallNone means "stay in the call".
	CallNone CallAction = iota
	// CallLeave hangs up and returns to the main screen.
	CallLeave
	// CallToggleMute flips the local microphone.
	CallToggleMute
	// CallCopyConnectData copies the connect-data blob to the clipboard.
	CallCopyConnectData
)

// Participant is one member of the call.
type Participant struct {
	Name  string
	Self  bool
	Muted bool
	// Speaking marks the audio-active peer, if known.
	Speaking bool
	// Connection is a short status label, e.g. "connected", "connecting".
	Connection string
}

// control identifies a focusable control on the call screen.
type control int

const (
	ctrlMute control = iota
	ctrlCopy
	ctrlLeave
	ctrlCount
)

// CallModel is the "Call screen".
type CallModel struct {
	RoomName  string
	Self      string
	Muted     bool
	Members   []Participant
	Connect   string // hex connect-data blob
	Connected bool

	W, H int

	focus     control
	copied    bool
	noticeOK  string
	noticeErr string
}

// NewCall builds the call screen model.
func NewCall(room, self, connectData string, connected bool) CallModel {
	return CallModel{
		RoomName:  room,
		Self:      self,
		Members:   []Participant{{Name: self, Self: true, Connection: "connected"}},
		Connect:   connectData,
		Connected: connected,
		focus:     ctrlMute,
	}
}

// SetNotice records a status message shown under the controls. Exactly one of
// ok/err should be non-empty; ok is rendered as success, err as an error.
func (m *CallModel) SetNotice(ok, errMsg string) {
	m.noticeOK = ok
	m.noticeErr = errMsg
	m.copied = ok != ""
}

// Update handles messages for the call screen.
func (m CallModel) Update(msg tea.Msg) (CallModel, CallAction, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.W, m.H = msg.Width, msg.Height
		return m, CallNone, nil

	case tea.KeyPressMsg:
		switch msg.String() {
		case "left", "shift+tab", "h":
			m.focus = (m.focus - 1 + ctrlCount) % ctrlCount
			return m, CallNone, nil
		case "right", "tab", "l":
			m.focus = (m.focus + 1) % ctrlCount
			return m, CallNone, nil
		case "m":
			return m, CallToggleMute, nil
		case "c":
			return m, CallCopyConnectData, nil
		case "q":
			return m, CallLeave, nil
		case "enter", "space":
			return m.activate()
		}
	}
	return m, CallNone, nil
}

func (m CallModel) activate() (CallModel, CallAction, tea.Cmd) {
	switch m.focus {
	case ctrlLeave:
		return m, CallLeave, nil
	case ctrlCopy:
		return m, CallCopyConnectData, nil
	default:
		return m, CallToggleMute, nil
	}
}

// View renders the call screen.
func (m CallModel) View() string {
	w := m.W
	if w <= 0 {
		w = 80
	}
	contentW := max(40, min(96, w-4))
	colW := (contentW - 3) / 2

	header := lipgloss.JoinHorizontal(lipgloss.Left,
		StyleTitle.Render(" CALL "),
		" ",
		lipgloss.NewStyle().Bold(true).Foreground(ColText).Render(m.RoomName),
		"  ",
		m.statusBadge(),
	)

	participants := m.panel("PARTICIPANTS  ("+fmt.Sprint(len(m.Members))+")", m.viewMembers(), colW)
	connect := m.panel("CONNECT-DATA", m.viewConnect(colW-4), colW)

	body := lipgloss.JoinHorizontal(lipgloss.Top, participants, " ", connect)

	parts := []string{header, "", body, "", m.viewControls()}
	switch {
	case m.noticeErr != "":
		parts = append(parts, "", StyleError.Render("✗ "+m.noticeErr))
	case m.noticeOK != "":
		parts = append(parts, "", StyleOK.Render("✓ "+m.noticeOK))
	}
	parts = append(parts, "", StyleHint.Render("←/→ move · enter activate · m mute · c copy · q leave"))

	view := lipgloss.JoinVertical(lipgloss.Left, parts...)
	if m.W > 0 && m.H > 0 {
		return lipgloss.Place(m.W, m.H, lipgloss.Left, lipgloss.Center, view)
	}
	return view
}

func (m CallModel) statusBadge() string {
	if m.Connected {
		return lipgloss.NewStyle().Foreground(ColOK).Render("● connected")
	}
	return lipgloss.NewStyle().Foreground(ColWarn).Render("◌ connecting…")
}

func (m CallModel) panel(title, body string, w int) string {
	titleLine := StyleLabel.Render(title)
	inner := lipgloss.JoinVertical(lipgloss.Left, titleLine, "", body)
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColAccent).
		Padding(0, 1).
		Width(max(10, w)).
		Render(inner)
}

func (m CallModel) viewMembers() string {
	if len(m.Members) == 0 {
		return StyleMuted.Render("nobody here yet")
	}

	rows := make([]string, 0, len(m.Members))
	for _, p := range m.Members {
		mic := lipgloss.NewStyle().Foreground(ColOK).Render("🎙")
		if p.Muted {
			mic = lipgloss.NewStyle().Foreground(ColDanger).Render("🔇")
		}

		name := p.Name
		if p.Speaking {
			name = lipgloss.NewStyle().Bold(true).Foreground(ColAccent2).Render(name)
		} else {
			name = lipgloss.NewStyle().Foreground(ColText).Render(name)
		}

		suffix := ""
		if p.Self {
			suffix = StyleMuted.Render(" (you)")
		}

		status := ""
		if p.Connection != "" && !p.Self {
			status = " " + StyleMuted.Render("· "+p.Connection)
		}

		rows = append(rows, " "+mic+" "+name+suffix+status)
	}
	return lipgloss.JoinVertical(lipgloss.Left, rows...)
}

func (m CallModel) viewConnect(width int) string {
	if m.Connect == "" {
		return StyleMuted.Render("no connect-data yet —\nwaiting for the peer connection")
	}

	grouped := session.Group(m.Connect, groupBlock(width))
	// Soft-wrap inside the panel so long blobs stay readable.
	wrapped := wrap(grouped, max(16, width))

	lines := []string{
		StyleMuted.Render("share with another client to join:"),
		"",
		lipgloss.NewStyle().Foreground(ColAccent2).Render(wrapped),
		"",
	}
	if m.copied {
		lines = append(lines, StyleOK.Render("copied to clipboard"))
	} else {
		lines = append(lines, StyleMuted.Render(fmt.Sprintf("%d bytes", len(m.Connect))))
	}
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func (m CallModel) viewControls() string {
	muteLabel := "Mute mic"
	if m.Muted {
		muteLabel = "Unmute mic"
	}

	btns := []struct {
		label string
		kind  string
		idx   control
	}{
		{muteLabel, "warn", ctrlMute},
		{"Copy connect-data", "ok", ctrlCopy},
		{"Leave", "danger", ctrlLeave},
	}

	rendered := make([]string, 0, len(btns))
	for _, b := range btns {
		rendered = append(rendered, button(b.label, m.focus == b.idx, b.kind))
	}
	return lipgloss.JoinHorizontal(lipgloss.Center, rendered[0], "  ", rendered[1], "  ", rendered[2])
}

// groupBlock picks a hex group size that fits the available width.
func groupBlock(width int) int {
	for _, n := range []int{16, 8, 4} {
		if n <= width {
			return n
		}
	}
	return 2
}

// wrap word-wraps s into lines no wider than width, breaking on spaces. The
// connect-data blob uses spaces solely as visual group separators, so breaking
// there keeps the hex readable and still decodable.
func wrap(s string, width int) string {
	if width <= 0 {
		return s
	}

	words := strings.Fields(s)
	var b strings.Builder
	col := 0
	for i, w := range words {
		if i > 0 {
			if col+1+len(w) > width {
				b.WriteByte('\n')
				col = 0
			} else {
				b.WriteByte(' ')
				col++
			}
		}
		// A single word longer than the line is hard-broken.
		for len(w) > width {
			b.WriteString(w[:width])
			b.WriteByte('\n')
			w = w[width:]
			col = 0
		}
		b.WriteString(w)
		col += len(w)
	}
	return b.String()
}
