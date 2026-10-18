package pages

import (
	"charm.land/lipgloss/v2"
)

type WelcomeData struct {
	AppName     string
	WindowWidth int
}

func WelcomeScreen(data WelcomeData) string {
	var style = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#FAFAFA")).
		Background(lipgloss.Color("#7D56F4")).
		Width(data.WindowWidth)

	return style.Render(data.AppName)
}
