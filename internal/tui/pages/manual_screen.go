package pages

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

type ManualStage int

const (
	ManualChoose ManualStage = iota
	ManualPasteOffer
	ManualAwaitAnswer
	ManualAwaitConnect
	ManualConnected
)

type ManualAction int

const (
	ManualNone ManualAction = iota
	ManualCreateOffer
	ManualAnswerOffer
	ManualApplyAnswer
	ManualAddCandidates
	ManualCopy
	ManualSave
	ManualToggleMute
	ManualLeave
)

type manualButton struct {
	label  string
	kind   string
	action ManualAction
}

type ManualModel struct {
	Stage   ManualStage
	Self    string
	Code    string
	Status  string
	Quality string
	Err     string
	Notice  string
	Muted   bool

	W, H int

	input codeInput
	focus int
}

func NewManual(self string) ManualModel {
	return ManualModel{
		Stage:  ManualChoose,
		Self:   self,
		Status: "no peer yet",
		input:  newCodeInput("paste peer code", 400_000),
	}
}

func (m *ManualModel) SetStage(stage ManualStage) {
	m.Stage = stage
	if m.acceptsInput() {
		_ = m.input.Focus()
	} else {
		m.input.Blur()
	}
}

func (m *ManualModel) SetCode(code string) {
	m.Code = code
}

func (m *ManualModel) SetStatus(status string) {
	m.Status = status
}

func (m *ManualModel) SetQuality(q string) {
	m.Quality = q
}

func (m *ManualModel) SetNotice(msg string) {
	m.Notice = msg
	m.Err = ""
}

func (m *ManualModel) SetError(msg string) {
	m.Err = msg
	m.Notice = ""
}

func (m *ManualModel) SetConnected() {
	m.SetStage(ManualConnected)
	m.Status = "connected"
	m.SetNotice("media connected")
}

func (m ManualModel) acceptsInput() bool {
	switch m.Stage {
	case ManualPasteOffer, ManualAwaitAnswer, ManualAwaitConnect:
		return true
	}
	return false
}

func (m ManualModel) buttons() []manualButton {
	switch m.Stage {
	case ManualChoose:
		return []manualButton{
			{"Create offer", "ok", ManualCreateOffer},
			{"Answer an offer", "default", ManualNone},
			{"Back to main", "default", ManualLeave},
		}
	case ManualPasteOffer:
		return []manualButton{
			{"Generate answer", "ok", ManualAnswerOffer},
			{"Back", "default", ManualLeave},
		}
	case ManualAwaitAnswer:
		return []manualButton{
			{"Copy my offer", "ok", ManualCopy},
			{"Connect", "warn", ManualApplyAnswer},
			{"Cancel", "danger", ManualLeave},
		}
	case ManualAwaitConnect:
		return []manualButton{
			{"Copy my answer", "ok", ManualCopy},
			{"Add candidates", "warn", ManualAddCandidates},
			{"Cancel", "danger", ManualLeave},
		}
	default:
		mute := "Mute mic"
		if m.Muted {
			mute = "Unmute mic"
		}
		return []manualButton{
			{mute, "warn", ManualToggleMute},
			{"End call", "danger", ManualLeave},
		}
	}
}

func (m ManualModel) Update(msg tea.Msg) (ManualModel, ManualAction, string, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.W, m.H = msg.Width, msg.Height
		m.input.SetWidth(max(20, min(90, msg.Width-16)))
		return m, ManualNone, "", nil

	case tea.KeyPressMsg:
		buttons := m.buttons()
		switch msg.String() {
		case "left", "shift+tab":
			m.focus = (m.focus - 1 + len(buttons)) % len(buttons)
			return m, ManualNone, "", nil
		case "right", "tab":
			m.focus = (m.focus + 1) % len(buttons)
			return m, ManualNone, "", nil
		case "c":
			if m.Code != "" {
				return m, ManualCopy, m.Code, nil
			}
			return m, ManualNone, "", nil
		case "s":
			if m.Code != "" {
				return m, ManualSave, m.Code, nil
			}
			return m, ManualNone, "", nil
		case "m":
			if m.Stage == ManualConnected {
				return m, ManualToggleMute, "", nil
			}
		case "esc":
			if m.Stage == ManualPasteOffer {
				m.SetStage(ManualChoose)
				return m, ManualNone, "", nil
			}
			return m, ManualLeave, "", nil
		case "q":
			if !m.acceptsInput() {
				return m, ManualLeave, "", nil
			}
		case "enter":
			if m.focus < len(buttons) {
				btn := buttons[m.focus]
				if btn.action == ManualNone {
					m.SetStage(ManualPasteOffer)
					return m, ManualNone, "", nil
				}
				return m, btn.action, strings.TrimSpace(m.input.Value()), nil
			}
		}

		if m.acceptsInput() {
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			return m, ManualNone, "", cmd
		}
	}

	if m.acceptsInput() {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, ManualNone, "", cmd
	}
	return m, ManualNone, "", nil
}

func (m ManualModel) View() string {
	w := m.W
	if w <= 0 {
		w = 80
	}
	contentW := max(40, min(100, w-4))

	header := lipgloss.JoinHorizontal(lipgloss.Left,
		StyleTitle.Render(" MANUAL "),
		" ",
		lipgloss.NewStyle().Bold(true).Foreground(ColText).Render("SDP + ICE"),
		"  ",
		lipgloss.NewStyle().Foreground(ColOK).Render("● "+m.Status),
	)

	parts := []string{
		header, "",
		StyleMuted.Render("No server needed: exchange one code at a time."),
		"",
		StyleLabel.Render(m.stepTitle()),
		"",
	}

	if m.Code != "" {
		parts = append(parts, m.codeBlock(contentW), "")
	}
	if m.acceptsInput() {
		parts = append(parts, m.fieldBlock(contentW), "")
	}

	switch {
	case m.Err != "":
		parts = append(parts, StyleError.Render("✗ "+m.Err), "")
	case m.Notice != "":
		parts = append(parts, StyleOK.Render("✓ "+m.Notice), "")
	}
	if m.Quality != "" {
		parts = append(parts, StyleMuted.Render(m.Quality), "")
	}

	parts = append(parts, m.viewButtons(), "", StyleHint.Render(m.hint()))

	view := lipgloss.JoinVertical(lipgloss.Left, parts...)
	if m.W > 0 && m.H > 0 {
		view = lipgloss.Place(m.W, m.H, lipgloss.Left, lipgloss.Center, view)
		return clipHeight(view, m.H)
	}
	return view
}

func clipHeight(s string, height int) string {
	if height <= 0 {
		return s
	}
	lines := strings.Split(s, "\n")
	if len(lines) > height {
		lines = lines[:height]
	}
	return strings.Join(lines, "\n")
}

func (m ManualModel) stepTitle() string {
	switch m.Stage {
	case ManualPasteOffer:
		return "STEP 2 — paste the caller's offer code"
	case ManualAwaitAnswer:
		return "STEP 2 — send your offer, paste the answer back"
	case ManualAwaitConnect:
		return "STEP 3 — send your answer and wait for the caller"
	case ManualConnected:
		return "CONNECTED"
	default:
		return "STEP 1 — choose your side"
	}
}

func (m ManualModel) codeBlock(width int) string {
	preview := m.Code
	if len(preview) > 30 {
		preview = preview[:30] + "…"
	}
	body := lipgloss.JoinVertical(lipgloss.Left,
		StyleMuted.Render(fmt.Sprintf("%d bytes · preview", len(m.Code))),
		"",
		lipgloss.NewStyle().Foreground(ColAccent2).Render(preview),
		"",
		StyleHint.Render("c — copy · s — save to file"),
	)
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColAccent).
		Padding(0, 1).
		Width(max(20, width)).
		Render(body)
}

func (m ManualModel) fieldBlock(width int) string {
	label := "PASTE OFFER CODE"
	switch m.Stage {
	case ManualAwaitAnswer:
		label = "PASTE ANSWER CODE"
	case ManualAwaitConnect:
		label = "PASTE MORE CANDIDATES (OPTIONAL)"
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		StyleLabel.Render(label),
		lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(ColAccent2).
			Padding(0, 1).
			Width(max(20, width-2)).
			Render(m.input.View()),
	)
}

func (m ManualModel) viewButtons() string {
	buttons := m.buttons()
	rendered := make([]string, 0, len(buttons))
	for i, b := range buttons {
		rendered = append(rendered, button(b.label, m.focus == i, b.kind))
	}
	return lipgloss.JoinHorizontal(lipgloss.Center, strings.Join(rendered, "  "))
}

func (m ManualModel) hint() string {
	switch m.Stage {
	case ManualPasteOffer:
		return "enter generate · ctrl+v paste · esc back"
	case ManualAwaitAnswer:
		return "c copy · s save · paste answer · enter connect"
	case ManualAwaitConnect:
		return "c copy · s save · paste extra candidates if needed"
	case ManualConnected:
		return "m mute · q end call"
	default:
		return "↑/↓ move · enter activate · q quit"
	}
}
