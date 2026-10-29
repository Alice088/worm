package main

import (
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"
	"github.com/Alice088/worm/internal/tui"
)

var version = "dev"

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "-v") {
		fmt.Println("worm " + version)
		return
	}

	p := tea.NewProgram(tui.InitialModel(version))
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "worm: %v\n", err)
		os.Exit(1)
	}
}
