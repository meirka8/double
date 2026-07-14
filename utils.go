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

// fuzzyMatch checks if the query characters appear in the target string in order.
func fuzzyMatch(query, target string) bool {
	if query == "" {
		return true
	}

	queryRunes := []rune(strings.ToLower(query))
	targetRunes := []rune(strings.ToLower(target))

	qIdx := 0
	for _, t := range targetRunes {
		if qIdx < len(queryRunes) && t == queryRunes[qIdx] {
			qIdx++
		}
	}
	return qIdx == len(queryRunes)
}
