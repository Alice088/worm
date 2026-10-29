package pages

import "charm.land/lipgloss/v2"

// Palette shared by every screen: scarlet on black.
var (
	ColAccent  = lipgloss.Color("#FF2D2D")
	ColAccent2 = lipgloss.Color("#FF7A7A")
	ColText    = lipgloss.Color("#F2F2F2")
	ColMuted   = lipgloss.Color("#6C6C74")
	ColDanger  = lipgloss.Color("#FF2D2D")
	ColOK      = lipgloss.Color("#7BE495")
	ColWarn    = lipgloss.Color("#FFC24B")
	ColLine    = lipgloss.Color("#26262B")
	ColSurface = lipgloss.Color("#141416")
	ColInk     = lipgloss.Color("#0A0A0A")
)

// Common styles.
var (
	StyleTitle = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColAccent)

	StyleSubtitle = lipgloss.NewStyle().
			Foreground(ColMuted)

	StyleCard = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColLine).
			Padding(2, 4)

	StyleCardFocused = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(ColAccent).
				Padding(2, 4)

	StyleText = lipgloss.NewStyle().Foreground(ColText)

	StyleMuted = lipgloss.NewStyle().Foreground(ColMuted)

	StyleHint = lipgloss.NewStyle().Foreground(ColMuted)

	StyleError = lipgloss.NewStyle().Foreground(ColDanger)

	StyleOK = lipgloss.NewStyle().Foreground(ColOK)

	StyleLabel = lipgloss.NewStyle().Foreground(ColMuted).Bold(true)
)

// button renders a clickable-looking action label.
func button(label string, focused bool, kind string) string {
	style := lipgloss.NewStyle().Padding(0, 2)
	if focused {
		return style.Bold(true).Foreground(ColInk).Background(ColAccent).Render(label)
	}

	switch kind {
	case "danger":
		return style.Foreground(ColDanger).Render(label)
	case "warn":
		return style.Foreground(ColWarn).Render(label)
	case "ok":
		return style.Foreground(ColOK).Render(label)
	default:
		return style.Foreground(ColText).Render(label)
	}
}
