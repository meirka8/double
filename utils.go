package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// clampUnit constrains a ratio to [0,1], and maps NaN (0/0) to 0.
func clampUnit(v float64) float64 {
	if v != v || v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// formatBytes renders a byte count in the largest unit that keeps it readable.
func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

// formatRate renders a throughput in bytes per second.
func formatRate(bytesPerSec float64) string {
	if bytesPerSec <= 0 {
		return "--"
	}
	return formatBytes(int64(bytesPerSec)) + "/s"
}

// formatDuration renders a short m:ss (or h:mm:ss) ETA.
func formatDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	total := int(d.Round(time.Second).Seconds())
	h, m, s := total/3600, (total/60)%60, total%60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}

// truncate shortens s to at most width display cells, using an ellipsis when it
// has to cut. It is rune-aware, unlike slicing a string directly.
func truncate(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= width {
		return s
	}
	if width == 1 {
		return "…"
	}
	runes := []rune(s)
	for len(runes) > 0 {
		candidate := string(runes) + "…"
		if lipgloss.Width(candidate) <= width {
			return candidate
		}
		runes = runes[:len(runes)-1]
	}
	return "…"
}

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
