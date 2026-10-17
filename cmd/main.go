package main

import (
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"
	"github.com/Alice088/worm/internal/tui"
)

func main() {
	// e := engine.NewEngine()

	p := tea.NewProgram(tui.InitialModel("v1"))
	if _, err := p.Run(); err != nil {
		fmt.Printf("Alas, there's been an error: %v", err)
		os.Exit(1)
	}
	
}
