package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// progressWidgetWidth is the content width of the bottom-right progress widget,
// excluding its one-cell horizontal padding.
const progressWidgetWidth = 34

// View renders the application UI.
func (m model) View() string {
	if m.quitting {
		return "Exiting Twin Manager. Goodbye!\n"
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

	if m.isFavoritesOpen || m.isQueueOpen {
		var centerContent string
		switch {
		case m.isQueueOpen:
			centerContent = m.queueView()
		case m.isConfirmingRemoveFav:
			centerContent = m.favoritesConfirmView()
		case m.isConfirmingUnmount:
			centerContent = m.favoritesUnmountConfirmView()
		default:
			centerContent = m.favoritesView()
		}

		var finalView string
		if m.leftPane.active {
			finalView = lipgloss.JoinHorizontal(lipgloss.Top, centerContent, paneView(m.rightPane))
		} else {
			finalView = lipgloss.JoinHorizontal(lipgloss.Top, paneView(m.leftPane), centerContent)
		}
		return lipgloss.JoinVertical(lipgloss.Left,
			finalView,
			m.statusBarView(),
			m.bottomRowView(),
		)
	}

	leftView := paneView(m.leftPane)
	rightView := paneView(m.rightPane)

	return lipgloss.JoinVertical(lipgloss.Left,
		lipgloss.JoinHorizontal(lipgloss.Top, leftView, rightView),
		m.statusBarView(),
		m.bottomRowView(),
	)
}

// contentWidth is the full width the bottom chrome may occupy.
func (m model) contentWidth() int {
	if m.width > 0 {
		return m.width
	}
	// Before the first WindowSizeMsg arrives, fall back to the pane arithmetic
	// (each pane renders its width plus two border columns).
	return m.leftPane.width + m.rightPane.width + 4
}

// bottomRowView lays out the hints bar with the progress widget pinned to the
// bottom-right corner. The widget is exactly as tall as the hint cards, so it
// costs no extra vertical space and never shifts the panes.
func (m model) bottomRowView() string {
	total := m.contentWidth()

	widget := m.progressWidgetView()
	if widget == "" {
		return m.hintsView(total)
	}

	widgetWidth := lipgloss.Width(widget)
	hints := m.hintsView(total - widgetWidth)

	gap := total - lipgloss.Width(hints) - widgetWidth
	if gap < 0 {
		gap = 0
	}
	return lipgloss.JoinHorizontal(lipgloss.Bottom, hints, strings.Repeat(" ", gap), widget)
}

func (m model) statusBarView() string {
	if m.isCreatingFolder {
		return inputPromptStyle.Render("Create folder: " + m.folderNameInput)
	}

	if m.isDeleting {
		if len(m.filesToDelete) == 1 {
			return confirmPromptStyle.Render(fmt.Sprintf("Delete %s? (y/n)", m.filesToDelete[0].Name))
		}
		return confirmPromptStyle.Render(fmt.Sprintf("Delete %d items? (y/n)", len(m.filesToDelete)))
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
		// truncate is rune-aware and copes with a non-positive budget; slicing
		// the string directly would panic on a narrow terminal.
		status = truncate(status, availableWidth)
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

// hintsView renders the shortcut hints, using at most maxWidth columns.
func (m model) hintsView(maxWidth int) string {
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
	targetModifier := "alt" // Default fallback
	if m.modifierState.Ctrl {
		targetModifier = "ctrl"
	} else if m.modifierState.Alt {
		targetModifier = "alt"
	} else if m.modifierState.Shift {
		targetModifier = "shift"
	}

	// Cards are appended only while they still fit, so a narrow terminal — or
	// the progress widget claiming the right-hand end — drops trailing hints
	// instead of wrapping the bottom row onto another line.
	blocks := []string{modifiers}
	used := lipgloss.Width(modifiers)

	for _, shortcut := range m.keyMap.GetShortcuts() {
		if shortcut.Modifier != targetModifier {
			continue
		}
		hint := hintCardStyle.Render(
			lipgloss.JoinHorizontal(lipgloss.Left,
				hintKeyStyle.Render(shortcut.DisplayKey),
				hintDescStyle.Render(shortcut.Action),
			),
		)
		width := lipgloss.Width(hint)
		if used+width > maxWidth {
			break
		}
		used += width
		blocks = append(blocks, hint)
	}

	return lipgloss.JoinHorizontal(lipgloss.Center, blocks...)
}

// progressWidgetView renders the compact operation indicator in the bottom-right
// corner. It returns "" when there is nothing to report, so the hints bar simply
// gets the full width back.
func (m model) progressWidgetView() string {
	if len(m.queue) == 0 {
		return ""
	}

	inner := progressWidgetWidth
	if avail := m.contentWidth()/2 - 2; avail < inner {
		inner = avail
	}
	if inner < 20 {
		return "" // Too narrow to say anything useful
	}

	op := m.widgetOp()
	snap := op.snapshot()

	pending := 0
	for _, other := range m.queue {
		if other.id != snap.id && !opState(other.state.Load()).terminal() {
			pending++
		}
	}

	// Line 1: what is happening, to what.
	titleStyle := progressTitleStyle
	if snap.state == opFailed {
		titleStyle = progressFailedStyle
	}
	title := titleStyle.Render(snap.kind.String())
	name := snap.label
	if snap.currentName != "" && snap.state == opRunning {
		name = snap.currentName
	}
	head := lipgloss.JoinHorizontal(lipgloss.Left,
		title,
		progressLabelStyle.Render(" "+truncate(name, inner-lipgloss.Width(title)-1)),
	)

	// Line 2: the bar itself, with the percentage pinned to the right.
	percent := snap.percent()
	barWidth := inner - 5 // one space plus a right-aligned "100%"
	filled := int(percent * float64(barWidth))
	if filled > barWidth {
		filled = barWidth
	}
	if filled < 0 {
		filled = 0
	}
	bar := lipgloss.JoinHorizontal(lipgloss.Left,
		progressBarFillStyle.Render(strings.Repeat("█", filled)),
		progressBarTrackStyle.Render(strings.Repeat("░", barWidth-filled)),
		progressDetailStyle.Render(fmt.Sprintf(" %3d%%", int(percent*100))),
	)

	// Line 3: throughput, ETA, and anything waiting behind this operation.
	detail := opDetail(op, snap)
	if pending > 0 {
		detail = fmt.Sprintf("%s (+%d)", detail, pending)
	}
	foot := progressDetailStyle.Render(truncate(detail, inner))

	return progressWidgetStyle.
		Width(inner + 2). // Width covers the one-cell padding on each side
		Render(lipgloss.JoinVertical(lipgloss.Left, head, bar, foot))
}

// widgetOp picks which operation the corner widget reports on: whatever is
// running, else a failure that still wants attention, else the newest entry
// (which is what keeps a just-finished transfer visible while it lingers).
func (m model) widgetOp() *fileOp {
	if op := m.runningOp(); op != nil {
		return op
	}
	for i := len(m.queue) - 1; i >= 0; i-- {
		if opState(m.queue[i].state.Load()) == opFailed {
			return m.queue[i]
		}
	}
	return m.queue[len(m.queue)-1]
}

// opDetail renders the throughput/ETA summary for an operation.
func opDetail(op *fileOp, snap opSnapshot) string {
	switch snap.state {
	case opQueued:
		return "queued"
	case opScanning:
		return "scanning…"
	case opDone:
		return "done"
	case opCancelled:
		return "cancelled"
	case opFailed:
		return "failed — alt+j for details"
	}

	var parts []string
	switch {
	case snap.totalBytes > 0:
		parts = append(parts, fmt.Sprintf("%s/%s", formatBytes(snap.doneBytes), formatBytes(snap.totalBytes)))
		parts = append(parts, formatRate(op.speed))
		if remaining := snap.totalBytes - snap.doneBytes; remaining > 0 && op.speed > 0 {
			if secs := float64(remaining) / op.speed; secs < 24*3600 {
				parts = append(parts, formatDuration(time.Duration(secs*float64(time.Second))))
			}
		}
	case snap.totalFiles > 0:
		parts = append(parts, fmt.Sprintf("%d/%d files", snap.doneFiles, snap.totalFiles))
	default:
		parts = append(parts, "working…")
	}
	return strings.Join(parts, " · ")
}

// queueView renders the operation queue panel.
func (m model) queueView() string {
	activePane := m.leftPane
	if m.rightPane.active {
		activePane = m.rightPane
	}

	// The border (2) and padding (4) have to come out of the pane width before
	// anything is drawn, or the panel spills over the neighbouring pane —
	// lipgloss.Place does not clip content that is wider than its box.
	inner := activePane.width - 6
	if inner < 24 {
		inner = 24
	}
	if inner > 72 {
		inner = 72
	}

	// Fixed columns for the cursor, the kind and the label; whatever is left
	// belongs to the progress detail.
	const cursorWidth, kindWidth = 2, 7
	labelWidth := inner / 3
	detailWidth := inner - cursorWidth - kindWidth - labelWidth - 2

	var s strings.Builder
	s.WriteString("Operation Queue\n\n")

	if len(m.queue) == 0 {
		s.WriteString(queueQueuedStyle.Render("Nothing queued."))
		s.WriteString("\n")
	}

	for i, op := range m.queue {
		snap := op.snapshot()

		cursor := "  "
		if i == m.queueCursor {
			cursor = "> "
		}

		line := fmt.Sprintf("%s%-*s %-*s %s",
			cursor,
			kindWidth, snap.kind.String(),
			labelWidth, truncate(snap.label, labelWidth),
			truncate(opDetail(op, snap), detailWidth),
		)
		s.WriteString(queueStateStyle(snap.state).Render(truncate(line, inner)))
		s.WriteString("\n")

		if snap.state == opFailed && snap.err != nil {
			s.WriteString(queueFailedStyle.Render("    " + truncate(snap.err.Error(), inner-4)))
			s.WriteString("\n")
		}
	}

	s.WriteString("\n" + truncate("[c] Cancel  [x] Dismiss  [Esc] Close", inner))

	// Width covers the horizontal padding but not the border, so inner+4 lands
	// the whole panel at inner+6 = the pane width.
	content := popupStyle.Width(inner + 4).Render(s.String())

	return lipgloss.Place(activePane.width, activePane.height, lipgloss.Center, lipgloss.Center, content)
}

func queueStateStyle(s opState) lipgloss.Style {
	switch s {
	case opScanning, opRunning:
		return queueRunningStyle
	case opDone:
		return queueDoneStyle
	case opFailed:
		return queueFailedStyle
	case opCancelled:
		return queueCancelledStyle
	}
	return queueQueuedStyle
}

func (m model) favoritesView() string {
	var s strings.Builder
	s.WriteString("Favorites\n\n")

	renderItem := func(i int, name string, isSelected bool) {
		cursor := "  "
		if isSelected {
			cursor = "> "
		}
		line := cursor + name
		if isSelected {
			s.WriteString(selectionStyle.Render(line))
		} else {
			s.WriteString(line)
		}
		s.WriteString("\n")
	}

	for i, fav := range m.favorites {
		renderItem(i, fav, i == m.favoritesCursor)
	}

	if len(m.drives) > 0 {
		s.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render("──────────────────────"))
		s.WriteString("\n")
		for i, drive := range m.drives {
			// Offset cursor logic for drives
			globalIndex := len(m.favorites) + i
			renderItem(globalIndex, drive, globalIndex == m.favoritesCursor)
		}
	}

	deleteHint := "[Delete/d] Remove"
	if m.favoritesCursor >= len(m.favorites) {
		deleteHint = "[d] Unmount"
	}
	s.WriteString("\n[Enter] Go  [Esc] Close  " + deleteHint)

	content := popupStyle.Render(s.String())

	activePane := m.leftPane
	if m.rightPane.active {
		activePane = m.rightPane
	}

	return lipgloss.Place(activePane.width, activePane.height, lipgloss.Center, lipgloss.Center, content)
}

func (m model) favoritesConfirmView() string {
	fav := ""
	if m.favToRemove >= 0 && m.favToRemove < len(m.favorites) {
		fav = m.favorites[m.favToRemove]
	}

	var s strings.Builder
	s.WriteString(fmt.Sprintf("Remove '%s' from favorites?\n\n", fav))
	s.WriteString("[y] Yes  [n] No")

	content := popupConfirmStyle.Render(s.String())

	activePane := m.leftPane
	if m.rightPane.active {
		activePane = m.rightPane
	}

	return lipgloss.Place(activePane.width, activePane.height, lipgloss.Center, lipgloss.Center, content)
}

func (m model) favoritesUnmountConfirmView() string {
	var s strings.Builder
	s.WriteString(fmt.Sprintf("Unmount '%s'?\n\n", m.driveToUnmount))
	s.WriteString("[y] Yes  [n] No")

	content := popupConfirmStyle.Render(s.String())

	activePane := m.leftPane
	if m.rightPane.active {
		activePane = m.rightPane
	}

	return lipgloss.Place(activePane.width, activePane.height, lipgloss.Center, lipgloss.Center, content)
}
