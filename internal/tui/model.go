package tui

import (
	"fmt"
	"net"
	"os"

	tea "charm.land/bubbletea/v2"
	"github.com/Alice088/worm/internal/tui/pages"
)

type Model struct {
	Window  Window
	Version string
	Name    string
}

type PeerClient struct {
	Addr net.Addr
	Name string
}

type Window struct {
	Heigh int
	Width int
}

func InitialModel(v string) Model {
	b, err := os.ReadFile("./assets/worm")
	if err != nil {
		fmt.Println("failed read worm.txt")
		os.Exit(1)
	}

	return Model{
		Version: v,
		Name:    string(b),
	}
}

func (m Model) Init() tea.Cmd {
	// Just return `nil`, which means "no I/O right now, please."
	return nil
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg: // ← обязательно ловить
		m.Window.Width, m.Window.Heigh = msg.Width, msg.Height
	case tea.KeyPressMsg:
		switch msg.String() {
		case "enter", "ctrl+c", "esc":
			return m, tea.Quit
		}
	}

	return m, nil
}

func (m Model) View() tea.View {
	return tea.NewView(pages.WelcomeScreen(pages.WelcomeData{
		AppName: m.Name,
		WindowWidth: m.Window.Width,
	}))
}
