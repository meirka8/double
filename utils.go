package main

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// calculateWrappedLines wraps the content to fit the given width and returns the lines.
func calculateWrappedLines(content string, width int) []string {
	if width <= 0 {
		return []string{}
	}
	wrappedContent := lipgloss.NewStyle().Width(width).Render(content)
	return strings.Split(wrappedContent, "\n")
}
