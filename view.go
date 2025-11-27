package main

import (
	"fmt"
	"strings"

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
