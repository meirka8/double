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
		previewView := previewStyle.Width(m.previewWidth).Height(m.previewHeight).Render(m.previewContent)
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

	// Ensure viewport is within bounds
	if p.cursor < p.viewportY {
		p.viewportY = p.cursor
	}
	if p.cursor >= p.viewportY+p.height-1 {
		p.viewportY = p.cursor - p.height + 2
	}

	for i := p.viewportY; i < len(p.files) && i < p.viewportY+p.height-1; i++ {
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
