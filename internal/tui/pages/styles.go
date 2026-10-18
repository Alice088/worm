package pages

import "charm.land/lipgloss/v2"

// Palette shared by every screen.
var (
	ColAccent  = lipgloss.Color("#7D56F4")
	ColAccent2 = lipgloss.Color("#B49BFF")
	ColText    = lipgloss.Color("#FAFAFA")
	ColMuted   = lipgloss.Color("#8B8B93")
	ColDanger  = lipgloss.Color("#FF5F87")
	ColOK      = lipgloss.Color("#5FD787")
	ColWarn    = lipgloss.Color("#FFD75F")
)

// Common styles.
var (
	// StyleTitle is the big screen heading.
	StyleTitle = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColText).
			Background(ColAccent).
			Padding(0, 1)

	// StyleSubtitle is the muted line under a title.
	StyleSubtitle = lipgloss.NewStyle().
			Foreground(ColMuted)

	// StyleCard is the bordered container used for panels.
	StyleCard = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColAccent).
			Padding(1, 2)

	// StyleCardFocused is the highlighted variant of StyleCard.
	StyleCardFocused = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(ColAccent2).
				Padding(1, 2)

	// StyleMuted renders secondary information.
	StyleMuted = lipgloss.NewStyle().Foreground(ColMuted)

	// StyleHint renders the footer help line.
	StyleHint = lipgloss.NewStyle().Foreground(ColMuted).Italic(true)

	// StyleError renders an inline error.
	StyleError = lipgloss.NewStyle().Foreground(ColDanger).Bold(true)

	// StyleOK renders a success flash.
	StyleOK = lipgloss.NewStyle().Foreground(ColOK).Bold(true)

	// StyleLabel renders small uppercase field labels.
	StyleLabel = lipgloss.NewStyle().Foreground(ColMuted).Bold(true)
)

// button renders a clickable-looking action label.
func button(label string, focused bool, kind string) string {
	style := lipgloss.NewStyle().Padding(0, 1).Foreground(ColText)
	if focused {
		style = style.Bold(true)
	}

	switch kind {
	case "danger":
		style = style.Background(ColDanger)
		if !focused {
			style = style.Background(lipgloss.Color("#4A2430"))
		}
	case "warn":
		style = style.Background(ColWarn).Foreground(lipgloss.Color("#1A1A1A"))
		if !focused {
			style = style.Foreground(ColWarn).Background(lipgloss.Color("#3D3520"))
		}
	case "ok":
		style = style.Background(ColOK).Foreground(lipgloss.Color("#10210F"))
		if !focused {
			style = style.Foreground(ColOK).Background(lipgloss.Color("#1E3325"))
		}
	default:
		style = style.Background(ColAccent)
		if !focused {
			style = style.Foreground(ColAccent2).Background(lipgloss.Color("#2A2340"))
		}
	}

	if focused {
		style = style.Underline(true)
	}
	return style.Render(label)
}
