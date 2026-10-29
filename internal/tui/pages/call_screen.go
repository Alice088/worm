package pages

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/Alice088/worm/internal/protocol"
	"github.com/Alice088/worm/internal/session"
)

type CallAction int

const (
	CallNone CallAction = iota
	CallAccept
	CallReject
	CallLeave
	CallToggleMute
	CallCopyInvite
)

type CallModel struct {
	Room   string
	Self   string
	Peer   string
	State  string
	Role   string
	Invite string
	Muted  bool

	Quality   string
	Connected time.Time

	W, H int

	focus     int
	noticeOK  string
	noticeErr string
}

func NewCall(self string) CallModel {
	return CallModel{Self: self, State: "starting"}
}

func (m *CallModel) SetState(call protocol.CallStatePayload) {
	m.Room = shortCallID(call.CallID)
	m.State = call.State
	m.Role = call.Role
	m.Self = call.Self.Name
	if call.Peer.Name != "" {
		m.Peer = call.Peer.Name
	}
	if call.Invite != "" {
		m.Invite = call.Invite
	}
	if call.Connected != nil {
		m.Connected = *call.Connected
	}
	if m.focus >= len(m.controls()) {
		m.focus = 0
	}
}

func (m *CallModel) SetQuality(text string) {
	m.Quality = text
}

func (m *CallModel) SetNotice(ok, errMsg string) {
	m.noticeOK = ok
	m.noticeErr = errMsg
}

func (m *CallModel) SetError(errMsg string) {
	m.noticeErr = errMsg
	m.noticeOK = ""
}

func (m *CallModel) SetNoticeOK(msg string) {
	m.noticeOK = msg
	m.noticeErr = ""
}

func (m CallModel) Terminal() bool {
	switch m.State {
	case "ended", "rejected", "cancelled", "timeout", "failed":
		return true
	}
	return false
}

type control struct {
	label  string
	kind   string
	action CallAction
}

func (m CallModel) controls() []control {
	if m.Terminal() {
		return []control{{"Back to main", "default", CallLeave}}
	}

	switch m.State {
	case "", "starting":
		return []control{{"Cancel", "danger", CallLeave}}
	case "ringing":
		if m.Role == "callee" {
			return []control{
				{"Accept", "ok", CallAccept},
				{"Reject", "danger", CallReject},
			}
		}
		return []control{
			{"Mute mic", "warn", CallToggleMute},
			{"Copy invite", "ok", CallCopyInvite},
			{"Cancel", "danger", CallLeave},
		}
	case "accepted", "connecting", "connected":
		mute := "Mute mic"
		if m.Muted {
			mute = "Unmute mic"
		}
		list := []control{{mute, "warn", CallToggleMute}}
		if m.Invite != "" {
			list = append(list, control{"Copy invite", "ok", CallCopyInvite})
		}
		list = append(list, control{"End call", "danger", CallLeave})
		return list
	}

	return []control{{"Back to main", "default", CallLeave}}
}

func (m CallModel) Update(msg tea.Msg) (CallModel, CallAction, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.W, m.H = msg.Width, msg.Height
		return m, CallNone, nil

	case tea.KeyPressMsg:
		controls := m.controls()
		switch msg.String() {
		case "left", "shift+tab", "h":
			m.focus = (m.focus - 1 + len(controls)) % len(controls)
			return m, CallNone, nil
		case "right", "tab", "l":
			m.focus = (m.focus + 1) % len(controls)
			return m, CallNone, nil
		case "m":
			return m, m.actionFor(CallToggleMute), nil
		case "c":
			return m, m.actionFor(CallCopyInvite), nil
		case "a":
			return m, m.actionFor(CallAccept), nil
		case "r":
			return m, m.actionFor(CallReject), nil
		case "q", "esc":
			return m, m.actionFor(CallLeave), nil
		case "enter", "space":
			if len(controls) == 0 {
				return m, CallNone, nil
			}
			return m, controls[m.focus].action, nil
		}
	}
	return m, CallNone, nil
}

func (m CallModel) actionFor(action CallAction) CallAction {
	for _, c := range m.controls() {
		if c.action == action {
			return action
		}
	}
	return CallNone
}

func (m CallModel) View() string {
	w := m.W
	if w <= 0 {
		w = 80
	}
	contentW := max(40, min(96, w-4))
	colW := (contentW - 3) / 2

	header := lipgloss.JoinHorizontal(lipgloss.Left,
		lipgloss.NewStyle().Bold(true).Foreground(ColText).Render(m.Room),
		"   ",
		m.badge(),
	)

	participants := m.panel("PARTICIPANTS", m.viewMembers(), colW)
	invite := m.panel("INVITE", m.viewInvite(colW-4), colW)
	body := lipgloss.JoinHorizontal(lipgloss.Top, participants, " ", invite)

	parts := []string{header, "", body, "", m.viewControls()}
	if m.Quality != "" {
		parts = append(parts, "", StyleMuted.Render(m.Quality))
	}
	switch {
	case m.noticeErr != "":
		parts = append(parts, "", StyleError.Render(m.noticeErr))
	case m.noticeOK != "":
		parts = append(parts, "", StyleOK.Render(m.noticeOK))
	}
	parts = append(parts, "", StyleHint.Render(m.hint()))

	view := lipgloss.JoinVertical(lipgloss.Left, parts...)
	if m.W > 0 && m.H > 0 {
		return lipgloss.Place(m.W, m.H, lipgloss.Left, lipgloss.Center, view)
	}
	return view
}

func (m CallModel) hint() string {
	switch m.State {
	case "ringing":
		if m.Role == "callee" {
			return "a · r · q"
		}
		return "c copy · q cancel"
	case "accepted", "connecting", "connected":
		return "←→ · enter · m · q"
	default:
		return "q"
	}
}

func (m CallModel) badge() string {
	label := m.State
	if label == "" {
		label = "starting"
	}
	var color = ColWarn
	switch m.State {
	case "connected":
		color = ColOK
	case "accepted", "connecting":
		color = ColWarn
	case "ended", "rejected", "cancelled", "timeout", "failed":
		color = ColDanger
	case "ringing":
		color = ColAccent2
	}
	return lipgloss.NewStyle().Foreground(color).Render("● " + label)
}

func (m CallModel) panel(title, body string, w int) string {
	return lipgloss.NewStyle().
		Width(max(10, w)).
		Render(lipgloss.JoinVertical(lipgloss.Left,
			StyleLabel.Render(title),
			"",
			body,
		))
}

func (m CallModel) viewMembers() string {
	rows := []string{m.memberRow(m.Self, true)}

	switch m.State {
	case "ringing", "accepted", "connecting", "connected", "ended":
		peer := m.Peer
		if peer == "" {
			peer = "waiting for peer…"
		}
		rows = append(rows, m.memberRow(peer, false))
	default:
		if m.Peer != "" {
			rows = append(rows, m.memberRow(m.Peer, false))
		}
	}
	return lipgloss.JoinVertical(lipgloss.Left, rows...)
}

func (m CallModel) memberRow(name string, self bool) string {
	mic := lipgloss.NewStyle().Foreground(ColOK).Render("🎙")
	if self && m.Muted {
		mic = lipgloss.NewStyle().Foreground(ColDanger).Render("🔇")
	}

	label := StyleText.Render(name)
	suffix := ""
	if self {
		suffix = StyleMuted.Render("  you")
	}
	return mic + "  " + label + suffix
}

func (m CallModel) viewInvite(width int) string {
	if m.Invite == "" {
		if m.State == "ringing" && m.Role == "callee" {
			return StyleMuted.Render("waiting for caller")
		}
		return StyleMuted.Render("—")
	}

	code := m.Invite
	grouped := session.Group(code, groupBlock(width))
	wrapped := wrap(grouped, max(16, width))

	return lipgloss.JoinVertical(lipgloss.Left,
		lipgloss.NewStyle().Foreground(ColAccent2).Render(wrapped),
		"",
		StyleMuted.Render(fmt.Sprintf("%d bytes", len(code))),
	)
}

func (m CallModel) viewControls() string {
	controls := m.controls()
	rendered := make([]string, 0, len(controls))
	for i, c := range controls {
		rendered = append(rendered, button(c.label, m.focus == i, c.kind))
	}
	return lipgloss.JoinHorizontal(lipgloss.Center, strings.Join(rendered, " "))
}

func groupBlock(width int) int {
	for _, n := range []int{16, 8, 4} {
		if n <= width {
			return n
		}
	}
	return 2
}

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

func shortCallID(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}
