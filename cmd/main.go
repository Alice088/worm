package main

import (
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"
	"github.com/Alice088/worm/internal/tui"
)

const version = "v1"

func main() {
	p := tea.NewProgram(tui.InitialModel(version))
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "worm: %v\n", err)
		os.Exit(1)
	}
}
