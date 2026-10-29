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
	ManualOfferReady
	ManualAwaitAnswer
	ManualAnswerReady
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
	ManualPasteAnswer
	ManualPasteCandidates
	ManualBack
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
	m.focus = 0
	switch stage {
	case ManualPasteOffer:
		m.input.SetPlaceholder("paste offer code")
	case ManualAwaitAnswer:
		m.input.SetPlaceholder("paste answer code")
	case ManualAwaitConnect:
		m.input.SetPlaceholder("paste candidates")
	}
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
			{"Back", "default", ManualBack},
		}
	case ManualOfferReady:
		return []manualButton{
			{"Copy offer", "ok", ManualCopy},
			{"Save offer", "default", ManualSave},
			{"Paste answer", "warn", ManualPasteAnswer},
			{"Cancel", "danger", ManualLeave},
		}
	case ManualAwaitAnswer:
		return []manualButton{
			{"Apply answer", "ok", ManualApplyAnswer},
			{"Copy offer", "default", ManualCopy},
			{"Back", "default", ManualBack},
		}
	case ManualAnswerReady:
		return []manualButton{
			{"Copy answer", "ok", ManualCopy},
			{"Save answer", "default", ManualSave},
			{"Paste candidates", "warn", ManualPasteCandidates},
			{"Cancel", "danger", ManualLeave},
		}
	case ManualAwaitConnect:
		return []manualButton{
			{"Apply candidates", "ok", ManualAddCandidates},
			{"Copy answer", "default", ManualCopy},
			{"Back", "default", ManualBack},
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
			if !m.acceptsInput() && m.Code != "" {
				return m, ManualCopy, m.Code, nil
			}
		case "s":
			if !m.acceptsInput() && m.Code != "" {
				return m, ManualSave, m.Code, nil
			}
		case "m":
			if m.Stage == ManualConnected {
				return m, ManualToggleMute, "", nil
			}
		case "esc":
			switch m.Stage {
			case ManualPasteOffer:
				m.SetStage(ManualChoose)
			case ManualAwaitAnswer:
				m.SetStage(ManualOfferReady)
			case ManualAwaitConnect:
				m.SetStage(ManualAnswerReady)
			default:
				return m, ManualLeave, "", nil
			}
			return m, ManualNone, "", nil
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
				data := strings.TrimSpace(m.input.Value())
				if btn.action == ManualCopy || btn.action == ManualSave {
					data = m.Code
				}
				return m, btn.action, data, nil
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

	statusColor := ColMuted
	if m.Stage == ManualConnected {
		statusColor = ColOK
	}
	header := lipgloss.JoinHorizontal(lipgloss.Left,
		StyleTitle.Render("manual"),
		"   ",
		lipgloss.NewStyle().Foreground(statusColor).Render("● "+m.Status),
	)

	parts := []string{
		header, "",
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
		parts = append(parts, StyleError.Render(m.Err), "")
	case m.Notice != "":
		parts = append(parts, StyleOK.Render(m.Notice), "")
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
	case ManualPasteOffer, ManualOfferReady:
		return "OFFER"
	case ManualAwaitAnswer, ManualAnswerReady:
		return "ANSWER"
	case ManualAwaitConnect:
		return "WAITING"
	case ManualConnected:
		return "CONNECTED"
	default:
		return "CHOOSE"
	}
}

func (m ManualModel) codeBlock(width int) string {
	preview := m.Code
	if len(preview) > 30 {
		preview = preview[:30] + "…"
	}
	body := lipgloss.JoinVertical(lipgloss.Left,
		lipgloss.NewStyle().Foreground(ColAccent2).Render(preview),
		StyleMuted.Render(fmt.Sprintf("%d bytes", len(m.Code))),
		"",
		StyleHint.Render(m.copyHint()),
	)
	return lipgloss.NewStyle().
		Width(max(20, width)).
		Render(body)
}

func (m ManualModel) copyHint() string {
	if m.acceptsInput() {
		return "esc back · copies above"
	}
	return "c copy · s save"
}

func (m ManualModel) fieldBlock(width int) string {
	label := "PASTE OFFER CODE"
	switch m.Stage {
	case ManualAwaitAnswer:
		label = "PASTE ANSWER CODE"
	case ManualAwaitConnect:
		label = "MORE CANDIDATES"
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		StyleLabel.Render(label),
		lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColLine).
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
	return lipgloss.JoinHorizontal(lipgloss.Center, strings.Join(rendered, " "))
}

func (m ManualModel) hint() string {
	switch m.Stage {
	case ManualPasteOffer, ManualAwaitAnswer, ManualAwaitConnect:
		return "enter apply · esc back"
	case ManualOfferReady, ManualAnswerReady:
		return "c · s · enter"
	case ManualConnected:
		return "m · q"
	default:
		return "↑↓ · enter"
	}
}
