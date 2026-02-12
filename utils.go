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

// fuzzyMatch checks if the characters in source appear in target in order.
func fuzzyMatch(source, target string) bool {
	sourceRunes := []rune(source)
	targetRunes := []rune(target)

	sIdx := 0
	for _, tR := range targetRunes {
		if sIdx < len(sourceRunes) && tR == sourceRunes[sIdx] {
			sIdx++
		}
	}
	return sIdx == len(sourceRunes)
}
