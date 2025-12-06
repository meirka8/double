package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	// Enable Kitty Keyboard Protocol
	fmt.Print("\x1b[>15u")
	defer fmt.Print("\x1b[<u")

	p := tea.NewProgram(initialModel(), tea.WithAltScreen(), tea.WithInput(debugReader{r: os.Stdin}))
	if _, err := p.Run(); err != nil {
		fmt.Printf("Alas, there's been an error: %v", err)
		os.Exit(1)
	}
}
