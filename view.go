package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// View renders the application UI.
func (m model) View() string {
	if m.quitting {
		return "Exiting Double Manager. Goodbye!\n"
	}

	if m.isPreviewing {
		var finalView string
		// Calculate inner dimensions for content
		// Border (2) + Padding (4) = 6 horizontal overhead
		// Border (2) + Padding (2) = 4 vertical overhead
		innerWidth := m.previewWidth - 6
		innerHeight := m.previewHeight - 4

		// Wrap content to fit width first
		wrappedLines := calculateWrappedLines(m.previewContent, innerWidth)

		// Truncate to fit height with scrolling
		contentLines := wrappedLines
		maxScroll := len(contentLines) - innerHeight
		if maxScroll < 0 {
			maxScroll = 0
		}

		if m.previewScrollY > maxScroll {
			m.previewScrollY = maxScroll
		}
		if m.previewScrollY < 0 {
			m.previewScrollY = 0
		}

		start := m.previewScrollY
		end := start + innerHeight
		if end > len(contentLines) {
			end = len(contentLines)
		}

		visibleLines := contentLines[start:end]

		previewView := previewStyle.Width(m.previewWidth).Height(m.previewHeight).Render(strings.Join(visibleLines, "\n"))
		if m.leftPane.active {
			finalView = lipgloss.JoinHorizontal(lipgloss.Top, previewView, paneView(m.rightPane))
		} else {
			finalView = lipgloss.JoinHorizontal(lipgloss.Top, paneView(m.leftPane), previewView)
		}
		return lipgloss.JoinVertical(lipgloss.Left, finalView, m.statusBarView())
	}

	leftView := paneView(m.leftPane)
	rightView := paneView(m.rightPane)

	return lipgloss.JoinVertical(lipgloss.Left,
		lipgloss.JoinHorizontal(lipgloss.Top, leftView, rightView),
		m.statusBarView(),
		m.hintsView(),
		m.progressView(),
	)
}

func (m model) statusBarView() string {
	if m.isCreatingFolder {
		return inputPromptStyle.Render("Create folder: " + m.folderNameInput)
	}

	if m.isDeleting {
		return confirmPromptStyle.Render(fmt.Sprintf("Delete %s? (y/n)", m.fileToDelete.Name))
	}

	if m.isConfirmingOverwrite {
		if len(m.overwriteConflicts) > 0 {
			return overwritePromptStyle.Render(fmt.Sprintf("Overwrite %s? (y/n/A/s)", m.overwriteConflicts[0].Source.Name))
		}
	}

	activePane := m.leftPane
	if m.rightPane.active {
		activePane = m.rightPane
	}

	var search string
	if activePane.searchQuery != "" {
		search = "Search: " + activePane.searchQuery
	}

	if len(activePane.files) == 0 || activePane.cursor >= len(activePane.files) {
		return statusBar.Render(search)
	}

	f := activePane.files[activePane.cursor]
	status := fmt.Sprintf("%s | %s | %s", f.Name, f.Mode.String(), f.ModTime.Format("2006-01-02 15:04:05"))

	// Calculate available space for the status, leaving room for the search query
	w := lipgloss.Width
	statusWidth := w(status)
	searchWidth := w(search)
	availableWidth := m.leftPane.width + m.rightPane.width + 2 - searchWidth
	if availableWidth < statusWidth {
		status = status[:availableWidth]
	}

	return lipgloss.JoinHorizontal(lipgloss.Left,
		statusBarActive.Render(search),
		statusBar.Render(status),
	)
}

func paneView(p pane) string {
	var s strings.Builder
	s.WriteString(p.path + "\n")

	for i := p.viewportY; i < len(p.files) && i < p.viewportY+p.height-2; i++ {
		f := p.files[i]
		line := " " + f.Name
		if f.IsDir {
			line = " " + dirStyle.Render(f.Name)
		}

		_, isSelected := p.selected[f.Path]

		if i == p.cursor {
			s.WriteString(cursorStyle.Render(line))
		} else if isSelected {
			s.WriteString(selectionStyle.Render(line))
		} else {
			s.WriteString(line)
		}
		s.WriteString("\n")
	}

	style := inactiveStyle
	if p.active {
		style = activeStyle
	}

	return style.Width(p.width).Height(p.height).Render(s.String())
}

func (m model) hintsView() string {
	// Modifiers
	// We primarily care about Alt as per user request
	altStyle := altChipInactiveStyle
	if m.modifierState.Alt {
		altStyle = altChipStyle
	}

	// Check other modifiers just in case we need to show them or they affect hints
	// But user asked to "leave only alt".
	// We will just show "Alt" chip, highlighted if pressed.

	modifiers := altStyle.Render("Alt")

	// Hints
	var hints []string
	targetModifier := "alt" // Default fallback
	if m.modifierState.Ctrl {
		targetModifier = "ctrl"
	} else if m.modifierState.Alt {
		targetModifier = "alt"
	} else if m.modifierState.Shift {
		targetModifier = "shift"
	}

	for _, shortcut := range m.keyMap.GetShortcuts() {
		if shortcut.Modifier == targetModifier {
			hint := hintCardStyle.Render(
				lipgloss.JoinHorizontal(lipgloss.Left,
					hintKeyStyle.Render(shortcut.DisplayKey),
					hintDescStyle.Render(shortcut.Action),
				),
			)
			hints = append(hints, hint)
		}
	}

	return lipgloss.JoinHorizontal(lipgloss.Center, // Alignment check
		modifiers,
		// No spacer needed if margins are handled by styles
		lipgloss.JoinHorizontal(lipgloss.Left, hints...),
	)
}

func (m model) progressView() string {
	if !m.progressState.IsActive {
		return ""
	}

	width := m.leftPane.width + m.rightPane.width

	// Calculate progress
	var percent float64
	if m.progressState.TotalBytes > 0 {
		percent = float64(m.progressState.WrittenBytes) / float64(m.progressState.TotalBytes)
	} else if m.progressState.TotalFiles > 0 {
		// Fallback to file count if bytes not available or 0
		percent = float64(m.progressState.ProcessedFiles) / float64(m.progressState.TotalFiles)
	}

	if percent > 1.0 {
		percent = 1.0
	}

	// Calculate speed
	duration := time.Since(m.progressState.StartTime)
	var speed string
	if duration.Seconds() > 0 {
		bytesPerSec := float64(m.progressState.WrittenBytes) / duration.Seconds()
		speed = fmt.Sprintf("%s/s", formatBytes(int64(bytesPerSec)))
	}

	// Format status text
	var statusText string
	if m.progressState.TotalFiles > 1 {
		statusText = fmt.Sprintf("Copying %d/%d files (%s) - %s", m.progressState.ProcessedFiles, m.progressState.TotalFiles, speed, m.progressState.CurrentFile)
	} else {
		statusText = fmt.Sprintf("Copying %s (%s)", m.progressState.CurrentFile, speed)
	}

	// Create progress bar
	barWidth := 20
	filledWidth := int(percent * float64(barWidth))
	if filledWidth > barWidth {
		filledWidth = barWidth
	}

	bar := progressBarStyle.Render(strings.Repeat(" ", filledWidth)) +
		progressTrackStyle.Render(strings.Repeat(" ", barWidth-filledWidth))

	content := lipgloss.JoinVertical(lipgloss.Right,
		statusText,
		bar,
	)

	return progressContainerStyle.Width(width).Render(content)
}

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
