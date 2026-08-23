package main

import (
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// queueLinger is how long a finished operation stays on screen before the
// progress widget clears itself. Failures are exempt: they wait to be dismissed
// from the queue panel.
const queueLinger = 3 * time.Second

// ensureCursorInBounds validates and adjusts cursor position to be within file list bounds
func ensureCursorInBounds(p *pane) {
	if p.cursor >= len(p.files) {
		if len(p.files) > 0 {
			p.cursor = len(p.files) - 1
		} else {
			p.cursor = 0
		}
	}
}

// enqueueOp appends an operation to the queue and returns whatever commands are
// needed to get it moving.
func (m *model) enqueueOp(kind opKind, sources []file, destPath string) tea.Cmd {
	if len(sources) == 0 {
		return nil
	}
	op := newFileOp(m.nextOpID, kind, sources, destPath)
	m.nextOpID++
	m.queue = append(m.queue, op)
	return m.pumpQueue()
}

// pumpQueue starts the next operation if nothing is running and makes sure the
// progress tick is scheduled. It is the only place that dispatches work, which
// is what keeps execution strictly serial.
func (m *model) pumpQueue() tea.Cmd {
	var cmds []tea.Cmd

	if m.runningOp() == nil {
		if next := m.nextQueuedOp(); next != nil {
			next.startedAt = time.Now()
			next.lastSample = next.startedAt
			cmds = append(cmds, runOpCmd(next))
		}
	}

	if !m.queueTicking && m.queueNeedsTick(time.Now()) {
		m.queueTicking = true
		cmds = append(cmds, progressTickCmd())
	}

	return tea.Batch(cmds...)
}

// queueNeedsTick reports whether the progress widget still changes on its own.
// A failed operation does not: it sits there until dismissed, and the frame
// already on screen shows it, so there is nothing left to repaint.
func (m model) queueNeedsTick(now time.Time) bool {
	for _, op := range m.queue {
		state := opState(op.state.Load())
		if !state.terminal() {
			return true
		}
		if state != opFailed && !op.finishedAt.IsZero() && now.Sub(op.finishedAt) <= queueLinger {
			return true
		}
	}
	return false
}

// sampleProgress refreshes the throughput estimate for the running operation.
// Speed is an exponentially weighted moving average rather than a plain
// total/elapsed average, so the figure follows the device slowing down or
// speeding up instead of being anchored to how the transfer started.
func (m *model) sampleProgress(now time.Time) {
	op := m.runningOp()
	if op == nil {
		return
	}

	elapsed := now.Sub(op.lastSample).Seconds()
	if elapsed <= 0 {
		return
	}

	done := op.doneBytes.Load()
	instant := float64(done-op.lastBytes) / elapsed
	if instant < 0 {
		// A cross-device move re-scans and resets doneBytes partway through.
		instant = 0
	}

	const alpha = 0.3
	if op.speed == 0 {
		op.speed = instant
	} else {
		op.speed = alpha*instant + (1-alpha)*op.speed
	}
	op.lastBytes = done
	op.lastSample = now
}

// pruneQueue drops finished operations once they have lingered long enough to
// have been noticed. Failures stay until the user dismisses them.
func (m *model) pruneQueue(now time.Time) {
	kept := make([]*fileOp, 0, len(m.queue))
	for _, op := range m.queue {
		state := opState(op.state.Load())
		expired := state.terminal() && state != opFailed &&
			!op.finishedAt.IsZero() && now.Sub(op.finishedAt) > queueLinger
		if !expired {
			kept = append(kept, op)
		}
	}
	m.queue = kept

	if m.queueCursor >= len(m.queue) {
		m.queueCursor = len(m.queue) - 1
	}
	if m.queueCursor < 0 {
		m.queueCursor = 0
	}
}

// queueOpAt returns the operation under the queue panel cursor, if any.
func (m model) queueOpAt(i int) *fileOp {
	if i < 0 || i >= len(m.queue) {
		return nil
	}
	return m.queue[i]
}

// cancelOp stops an operation, whether or not it has started running.
func (m *model) cancelOp(op *fileOp) {
	if opState(op.state.Load()).terminal() {
		return
	}
	if op.startedAt.IsZero() {
		// Never dispatched, so no worker exists to observe the cancelled
		// context and publish a terminal state. Update has to do it here.
		op.markCancelled()
		op.finishedAt = time.Now()
		return
	}
	op.cancel()
}

// finishOverwritePrompt closes the overwrite prompt and queues everything the
// user approved as a single operation.
func (m *model) finishOverwritePrompt() tea.Cmd {
	m.isConfirmingOverwrite = false
	m.overwriteConflicts = nil

	sources, dest := m.pendingApproved, m.pendingDest
	m.pendingApproved, m.pendingDest = nil, ""

	kind := opCopy
	if m.isMoving {
		kind = opMove
	}
	return m.enqueueOp(kind, sources, dest)
}

// abandonOverwritePrompt cancels the whole pending operation, including the
// files that had no conflict.
func (m *model) abandonOverwritePrompt() {
	m.isConfirmingOverwrite = false
	m.overwriteConflicts = nil
	m.pendingApproved = nil
	m.pendingDest = ""
}

// Update handles messages and updates the model.
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	// Track modifiers (basic implementation) - REMOVED FUNCTIONALITY
	// switch msg := msg.(type) {
	// case tea.KeyMsg:
	// ...
	// }
	// User requested to remove this functionality for now.

	// Handle operations that take precedence over normal key presses
	if m.isCreatingFolder {
		switch msg := msg.(type) {
		case tea.KeyMsg:
			key := msg.String()
			if mapKey, ok := m.aliasMap[key]; ok {
				key = mapKey
			}
			switch key {
			case "enter":
				activePane := &m.leftPane
				if m.rightPane.active {
					activePane = &m.rightPane
				}
				m.isCreatingFolder = false
				cmd = createFolderCmd(filepath.Join(activePane.path, m.folderNameInput))
				m.folderNameInput = ""
				return m, cmd
			case "esc":
				m.isCreatingFolder = false
				m.folderNameInput = ""
				return m, nil
			case "backspace":
				if len(m.folderNameInput) > 0 {
					m.folderNameInput = m.folderNameInput[:len(m.folderNameInput)-1]
				}
				return m, nil
			default:
				if len(msg.String()) == 1 {
					m.folderNameInput += msg.String()
				}
				return m, nil
			}
		}
	} else if m.isDeleting {
		switch msg := msg.(type) {
		case tea.KeyMsg:
			switch msg.String() {
			case "y", "Y":
				activePane := &m.leftPane
				if m.rightPane.active {
					activePane = &m.rightPane
				}
				m.isDeleting = false
				files := m.filesToDelete
				m.filesToDelete = nil                           // Clear files to delete
				activePane.selected = make(map[string]struct{}) // Clear selection
				return m, m.enqueueOp(opDelete, files, "")
			case "n", "N", "esc":
				m.isDeleting = false
				m.filesToDelete = nil // Clear files to delete
				return m, nil
			}
		}
	} else if m.isConfirmingOverwrite {
		if len(m.overwriteConflicts) == 0 {
			// Nothing left to ask about; queue whatever was approved.
			return m, m.finishOverwritePrompt()
		}
		switch msg := msg.(type) {
		case tea.KeyMsg:
			switch msg.String() {
			case "y", "Y": // Overwrite this one
				m.pendingApproved = append(m.pendingApproved, m.overwriteConflicts[0].Source)
				m.overwriteConflicts = m.overwriteConflicts[1:]
				if len(m.overwriteConflicts) == 0 {
					return m, m.finishOverwritePrompt()
				}
				return m, nil

			case "n", "N": // Skip this one
				m.overwriteConflicts = m.overwriteConflicts[1:]
				if len(m.overwriteConflicts) == 0 {
					return m, m.finishOverwritePrompt()
				}
				return m, nil

			case "a", "A": // Overwrite all remaining
				for _, conflict := range m.overwriteConflicts {
					m.pendingApproved = append(m.pendingApproved, conflict.Source)
				}
				return m, m.finishOverwritePrompt()

			case "s", "S": // Skip all remaining
				return m, m.finishOverwritePrompt()

			case "esc": // Abandon the operation entirely
				m.abandonOverwritePrompt()
				return m, nil
			}
		}
	} else if m.isQueueOpen {
		switch msg := msg.(type) {
		case tea.KeyMsg:
			switch msg.String() {
			case "esc", "q", m.keyMap.Queue.Key:
				m.isQueueOpen = false
				return m, nil
			case "up", "k":
				if m.queueCursor > 0 {
					m.queueCursor--
				}
				return m, nil
			case "down", "j":
				if m.queueCursor < len(m.queue)-1 {
					m.queueCursor++
				}
				return m, nil
			case "home":
				m.queueCursor = 0
				return m, nil
			case "end":
				if len(m.queue) > 0 {
					m.queueCursor = len(m.queue) - 1
				}
				return m, nil
			case "c", "C", "delete": // Cancel the highlighted operation
				if op := m.queueOpAt(m.queueCursor); op != nil {
					m.cancelOp(op)
					return m, m.pumpQueue()
				}
				return m, nil
			case "x", "X": // Dismiss a finished entry
				if op := m.queueOpAt(m.queueCursor); op != nil && opState(op.state.Load()).terminal() {
					kept := make([]*fileOp, 0, len(m.queue))
					for _, other := range m.queue {
						if other.id != op.id {
							kept = append(kept, other)
						}
					}
					m.queue = kept
					if m.queueCursor >= len(m.queue) {
						m.queueCursor = len(m.queue) - 1
					}
					if m.queueCursor < 0 {
						m.queueCursor = 0
					}
				}
				return m, nil
			}
		}
	} else if m.isFavoritesOpen {
		if m.isConfirmingUnmount {
			switch msg := msg.(type) {
			case tea.KeyMsg:
				switch msg.String() {
				case "y", "Y":
					m.isConfirmingUnmount = false
					path := m.driveToUnmount
					m.driveToUnmount = ""
					return m, unmountDriveCmd(path)
				case "n", "N", "esc":
					m.isConfirmingUnmount = false
					m.driveToUnmount = ""
					return m, nil
				}
			}
		} else if m.isConfirmingRemoveFav {
			switch msg := msg.(type) {
			case tea.KeyMsg:
				switch msg.String() {
				case "y", "Y":
					if m.favToRemove >= 0 && m.favToRemove < len(m.favorites) {
						// Remove favorite
						m.favorites = append(m.favorites[:m.favToRemove], m.favorites[m.favToRemove+1:]...)

						// Adjust cursor if out of bounds (but account for drives)
						totalItems := len(m.favorites) + len(m.drives)
						if m.favoritesCursor >= totalItems && totalItems > 0 {
							m.favoritesCursor = totalItems - 1
						}
						// If user deleted the last favorite and there are drives below,
						// the cursor (which was at favToRemove) now points to the drive that shifted up.
						// We don't need to decrement unless we were at the very end of the *entire* list.
					}
					m.isConfirmingRemoveFav = false
					m.favToRemove = -1
					return m, nil
				case "n", "N", "esc":
					m.isConfirmingRemoveFav = false
					m.favToRemove = -1
					return m, nil
				}
			}
		} else {
			switch msg := msg.(type) {
			case tea.KeyMsg:
				switch msg.String() {
				case "esc", "q", "alt+f":
					m.isFavoritesOpen = false
					return m, nil
				case "up", "k":
					if m.favoritesCursor > 0 {
						m.favoritesCursor--
					}
				case "down", "j":
					totalItems := len(m.favorites) + len(m.drives)
					if m.favoritesCursor < totalItems-1 {
						m.favoritesCursor++
					}
				case "home":
					m.favoritesCursor = 0
				case "end":
					totalItems := len(m.favorites) + len(m.drives)
					if totalItems > 0 {
						m.favoritesCursor = totalItems - 1
					}
				case "enter":
					totalItems := len(m.favorites) + len(m.drives)
					if totalItems > 0 && m.favoritesCursor < totalItems {
						var selectedPath string
						if m.favoritesCursor < len(m.favorites) {
							selectedPath = m.favorites[m.favoritesCursor]
						} else {
							selectedPath = m.drives[m.favoritesCursor-len(m.favorites)]
						}
						m.isFavoritesOpen = false
						if m.leftPane.active {
							m.leftPane.path = selectedPath
							m.leftPane.cursor = 0
							m.leftPane.viewportY = 0
							return m, m.leftPane.loadDirectoryCmd("")
						} else {
							m.rightPane.path = selectedPath
							m.rightPane.cursor = 0
							m.rightPane.viewportY = 0
							return m, m.rightPane.loadDirectoryCmd("")
						}
					}
				case "delete", "d":
					if len(m.favorites) > 0 && m.favoritesCursor < len(m.favorites) {
						// Remove from favorites
						m.isConfirmingRemoveFav = true
						m.favToRemove = m.favoritesCursor
					} else if m.favoritesCursor >= len(m.favorites) && len(m.drives) > 0 {
						// Unmount drive
						driveIdx := m.favoritesCursor - len(m.favorites)
						if driveIdx < len(m.drives) {
							m.isConfirmingUnmount = true
							m.driveToUnmount = m.drives[driveIdx]
						}
					}
				}
			}
		}
	} else if m.isPreviewing {
		switch msg := msg.(type) {
		case tea.KeyMsg:
			switch msg.String() {
			case "esc", "q":
				m.isPreviewing = false
				m.previewContent = ""
				m.previewFilePath = ""
				m.previewScrollY = 0
				return m, nil
			case "up", "k":
				if m.previewScrollY > 0 {
					m.previewScrollY--
				}
				return m, nil
			case "down", "j":
				// Calculate max scroll
				innerWidth := m.previewWidth - 6
				innerHeight := m.previewHeight - 4
				wrappedLines := calculateWrappedLines(m.previewContent, innerWidth)
				maxScroll := len(wrappedLines) - innerHeight
				if maxScroll < 0 {
					maxScroll = 0
				}

				if m.previewScrollY < maxScroll {
					m.previewScrollY++
				}
				return m, nil
			case "pgup":
				m.previewScrollY -= m.previewHeight
				if m.previewScrollY < 0 {
					m.previewScrollY = 0
				}
				return m, nil
			case "pgdown":
				// Calculate max scroll
				innerWidth := m.previewWidth - 6
				innerHeight := m.previewHeight - 4
				wrappedLines := calculateWrappedLines(m.previewContent, innerWidth)
				maxScroll := len(wrappedLines) - innerHeight
				if maxScroll < 0 {
					maxScroll = 0
				}

				m.previewScrollY += m.previewHeight
				if m.previewScrollY > maxScroll {
					m.previewScrollY = maxScroll
				}
				return m, nil
			case "home", "g":
				m.previewScrollY = 0
				return m, nil
			case "end", "G":
				// Calculate max scroll
				innerWidth := m.previewWidth - 6
				innerHeight := m.previewHeight - 4
				wrappedLines := calculateWrappedLines(m.previewContent, innerWidth)
				maxScroll := len(wrappedLines) - innerHeight
				if maxScroll < 0 {
					maxScroll = 0
				}
				m.previewScrollY = maxScroll
				return m, nil
			}
		}
	} else { // Normal operation mode
		switch msg := msg.(type) {
		case tea.KeyMsg:
			key := msg.String()
			if mapKey, ok := m.aliasMap[key]; ok {
				key = mapKey
			}
			switch key {
			case m.keyMap.Quit.Key: // Quit
				m.quitting = true
				return m, tea.Quit
			case m.keyMap.ForceQuit.Key: // Force Quit
				m.quitting = true
				return m, tea.Quit
			case m.keyMap.SwitchPane.Key:
				m.leftPane.active = !m.leftPane.active
				m.rightPane.active = !m.rightPane.active
				return m, nil
			case m.keyMap.Preview.Key: // Preview
				activePane := &m.leftPane
				if m.rightPane.active {
					activePane = &m.rightPane
				}
				if len(activePane.files) > 0 {
					selectedFile := activePane.files[activePane.cursor]
					if !selectedFile.IsDir {
						m.isPreviewing = true
						m.previewFilePath = selectedFile.Path
						m.previewWidth = activePane.width
						m.previewHeight = activePane.height
						m.previewScrollY = 0
						return m, previewFileCmd(selectedFile.Path)
					}
				}
				return m, nil
			case m.keyMap.Copy.Key: // Copy
				sourcePane := &m.leftPane
				destPane := &m.rightPane
				if m.rightPane.active {
					sourcePane = &m.rightPane
					destPane = &m.leftPane
				}
				files := getFilesFromSelected(*sourcePane)
				if len(files) == 0 && len(sourcePane.files) > 0 { // Nothing selected, use focused file
					files = []file{sourcePane.files[sourcePane.cursor]}
				}
				if len(files) > 0 {
					sourcePane.selected = make(map[string]struct{}) // Clear selection
					return m, probeConflictsCmd(files, destPane.path, false)
				}
				return m, nil
			case m.keyMap.Move.Key: // Move
				sourcePane := &m.leftPane
				destPane := &m.rightPane
				if m.rightPane.active {
					sourcePane = &m.rightPane
					destPane = &m.leftPane
				}
				files := getFilesFromSelected(*sourcePane)
				if len(files) == 0 && len(sourcePane.files) > 0 { // Nothing selected, use focused file
					files = []file{sourcePane.files[sourcePane.cursor]}
				}
				if len(files) > 0 {
					sourcePane.selected = make(map[string]struct{}) // Clear selection
					return m, probeConflictsCmd(files, destPane.path, true)
				}
				return m, nil
			case m.keyMap.NewFolder.Key: // New Folder
				m.isCreatingFolder = true
				return m, nil
			case m.keyMap.Delete.Key: // Delete
				activePane := &m.leftPane
				if m.rightPane.active {
					activePane = &m.rightPane
				}
				files := getFilesFromSelected(*activePane)
				if len(files) == 0 && len(activePane.files) > 0 { // Nothing selected, use focused file
					files = []file{activePane.files[activePane.cursor]}
				}
				if len(files) > 0 {
					m.isDeleting = true
					m.filesToDelete = files
				}
				return m, nil
			case m.keyMap.CopyPath.Key:
				activePane := &m.leftPane
				if m.rightPane.active {
					activePane = &m.rightPane
				}
				files := getFilesFromSelected(*activePane)
				if len(files) == 0 && len(activePane.files) > 0 {
					files = []file{activePane.files[activePane.cursor]}
				}
				if len(files) > 0 {
					var paths []string
					for _, f := range files {
						paths = append(paths, f.Path)
					}
					return m, copyToClipboardCmd(strings.Join(paths, "\n"))
				}
				return m, nil
			case m.keyMap.Favorites.Key:
				m.isFavoritesOpen = true
				m.favoritesCursor = 0
				return m, nil
			case m.keyMap.Queue.Key:
				m.isQueueOpen = true
				m.queueCursor = 0
				return m, nil
			case m.keyMap.AddToFavorites.Key:
				activePane := &m.leftPane
				if m.rightPane.active {
					activePane = &m.rightPane
				}

				pathToAdd := activePane.path
				if len(activePane.files) > 0 {
					f := activePane.files[activePane.cursor]
					if f.IsDir && f.Name != ".." {
						pathToAdd = f.Path
					}
				}

				// Check for duplicates
				exists := false
				for _, fav := range m.favorites {
					if fav == pathToAdd {
						exists = true
						break
					}
				}
				if !exists {
					m.favorites = append(m.favorites, pathToAdd)
				}
			case m.keyMap.SyncPanes.Key:
				activePane := &m.leftPane
				inactivePane := &m.rightPane
				if m.rightPane.active {
					activePane = &m.rightPane
					inactivePane = &m.leftPane
				}
				inactivePane.path = activePane.path
				// Reset cursor and viewport for the inactive pane
				inactivePane.cursor = 0
				inactivePane.viewportY = 0
				return m, inactivePane.loadDirectoryCmd("")
			case m.keyMap.OpenInOther.Key:
				activePane := &m.leftPane
				inactivePane := &m.rightPane
				if m.rightPane.active {
					activePane = &m.rightPane
					inactivePane = &m.leftPane
				}
				if len(activePane.files) > 0 {
					selectedFile := activePane.files[activePane.cursor]
					if selectedFile.IsDir {
						inactivePane.path = selectedFile.Path
						// Reset cursor and viewport for the inactive pane
						inactivePane.cursor = 0
						inactivePane.viewportY = 0
						return m, inactivePane.loadDirectoryCmd("")
					}
				}
				return m, nil
			}
		}
	}

	// Handle messages that are always processed
	switch msg := msg.(type) {
	case directoryLoadedMsg:
		if msg.paneID == m.leftPane.id {
			m.leftPane.files = msg.files
			m.leftPane.err = msg.err
			if msg.focusPath != "" {
				for i, f := range m.leftPane.files {
					if f.Path == msg.focusPath {
						m.leftPane.cursor = i
						// Adjust viewport to make cursor visible
						if m.leftPane.cursor >= m.leftPane.viewportY+m.leftPane.height-2 {
							m.leftPane.viewportY = m.leftPane.cursor - m.leftPane.height + 3
						}
						break
					}
				}
			}
			// Ensure cursor is within bounds after directory reload
			ensureCursorInBounds(&m.leftPane)
		} else if msg.paneID == m.rightPane.id {
			m.rightPane.files = msg.files
			m.rightPane.err = msg.err
			if msg.focusPath != "" {
				for i, f := range m.rightPane.files {
					if f.Path == msg.focusPath {
						m.rightPane.cursor = i
						// Adjust viewport to make cursor visible
						if m.rightPane.cursor >= m.rightPane.viewportY+m.rightPane.height-2 {
							m.rightPane.viewportY = m.rightPane.cursor - m.rightPane.height + 3
						}
						break
					}
				}
			}
			// Ensure cursor is within bounds after directory reload
			ensureCursorInBounds(&m.rightPane)
		}
		return m, nil
	case tea.WindowSizeMsg:
		// Handle window resizing
		// Height includes:
		// - Status bar (1 line)
		// - Hint panel (approx 3 lines: 1 text + 2 border)
		// - Borders (2 lines for top/bottom of pane?)
		// Let's account for 4 lines of overhead separate from pane borders.
		paneHeight := msg.Height - 1 - 4 // Adjust for status bar (1) and hints (3) and maybe some breathing room?
		// Previously it was -1 -2. 1 for status, 2 for borders?
		// If we have top border and bottom border on panes, that's inside paneView rendering usually or accounted for here.
		// Let's try reducing height by 5 total to be safe: 1 (status) + 3 (hints).
		// Wait, original was `msg.Height - 1 - 2`.
		// If hints are new, we need to subtract their height.
		// Hint panel height = 3 (1 text + 2 border).
		// So we need to subtract 3 more than before.
		// New calculation: msg.Height - 1 (status) - 3 (hints) - 2 (pane borders overhead if any, previously 2)
		// Total subtraction: 6.
		paneHeight = msg.Height - 6
		paneWidth := msg.Width/2 - 2
		m.width = msg.Width
		m.height = msg.Height
		m.leftPane.height = paneHeight
		m.rightPane.height = paneHeight
		m.leftPane.width = paneWidth
		m.rightPane.width = paneWidth
		return m, nil
	case fileOpenedMsg:
		if msg.err != nil {
			m.err = msg.err
		}
		return m, nil
	case folderCreatedMsg:
		if msg.err != nil {
			m.err = msg.err
		} else {
			// Reload directory in active pane and focus on the newly created folder
			if m.leftPane.active {
				return m, m.leftPane.loadDirectoryCmd(msg.folderPath)
			} else {
				return m, m.rightPane.loadDirectoryCmd(msg.folderPath)
			}
		}
		return m, nil
	case conflictProbeMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.isMoving = msg.moving
		m.pendingDest = msg.dest
		m.pendingApproved = msg.approved
		if len(msg.conflicts) == 0 {
			// Nothing to ask about; queue it straight away.
			return m, m.finishOverwritePrompt()
		}
		m.overwriteConflicts = msg.conflicts
		m.isConfirmingOverwrite = true
		return m, nil
	case opFinishedMsg:
		now := time.Now()
		for _, op := range m.queue {
			if op.id != msg.id {
				continue
			}
			op.finishedAt = now
			if opState(op.state.Load()) == opFailed {
				m.err = op.snapshot().err
			}
			break
		}
		// Either pane can be the source or the destination, so refresh both.
		return m, tea.Batch(
			m.leftPane.loadDirectoryCmd(""),
			m.rightPane.loadDirectoryCmd(""),
			m.pumpQueue(),
		)
	case progressTickMsg:
		now := time.Time(msg)
		m.queueTicking = false
		m.sampleProgress(now)
		m.pruneQueue(now)
		if m.queueNeedsTick(now) {
			m.queueTicking = true
			return m, progressTickCmd()
		}
		return m, nil
	case driveUnmountedMsg:
		if msg.err != nil {
			m.err = msg.err
		} else {
			// Refresh drives list after successful unmount
			m.drives = getMountedDrives()
			totalItems := len(m.favorites) + len(m.drives)
			if m.favoritesCursor >= totalItems && totalItems > 0 {
				m.favoritesCursor = totalItems - 1
			}
		}
		return m, nil
	case previewReadyMsg:
		m.previewContent = msg.Content
		if msg.Err != nil {
			m.err = msg.Err
		}
		return m, nil
	default:
		// logDebug("Unknown message: %T", msg)
	}

	// Delegate updates to active pane only if not in an operation mode
	if !m.isCreatingFolder && !m.isDeleting && !m.isConfirmingOverwrite && !m.isPreviewing && !m.isQueueOpen {
		if m.leftPane.active {
			m.leftPane, cmd = m.leftPane.update(msg)
		} else {
			m.rightPane, cmd = m.rightPane.update(msg)
		}
	}
	return m, cmd
}

// update handles messages for a pane.
func (p pane) update(msg tea.Msg) (pane, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "home":
			p.cursor = 0
		case "end":
			if len(p.files) > 0 {
				p.cursor = len(p.files) - 1
			} else {
				p.cursor = 0
			}
		case "up":
			p.searchQuery = "" // Clear search on navigation
			if p.cursor > 0 {
				p.cursor--
			}
		case "down":
			p.searchQuery = "" // Clear search on navigation
			if p.cursor < len(p.files)-1 {
				p.cursor++
			}
		case "pgup":
			p.searchQuery = "" // Clear search on navigation
			p.cursor -= p.height
			if p.cursor < 0 {
				p.cursor = 0
			}
		case "pgdown":
			p.searchQuery = "" // Clear search on navigation
			if len(p.files) > 0 {
				p.cursor += p.height
				if p.cursor >= len(p.files) {
					p.cursor = len(p.files) - 1
				}
			}
		case "enter":
			p.searchQuery = "" // Clear search on navigation
			if len(p.files) > 0 {
				selectedFile := p.files[p.cursor]
				if selectedFile.IsDir {
					// Check if it's the parent directory entry ".."
					if selectedFile.Name == ".." {
						currentPath := p.path
						p.path = selectedFile.Path
						p.cursor = 0
						return p, p.loadDirectoryCmd(currentPath)
					}

					p.path = selectedFile.Path
					p.cursor = 0    // Reset cursor when entering a new directory
					p.viewportY = 0 // Reset viewport when entering a new directory
					return p, p.loadDirectoryCmd("")
				} else {
					return p, openFileCmd(selectedFile.Path)
				}
			}
		case "esc":
			p.searchQuery = "" // Clear search explicitly
		case "insert":
			if len(p.files) > 0 {
				filePath := p.files[p.cursor].Path
				if _, ok := p.selected[filePath]; ok {
					delete(p.selected, filePath)
				} else {
					p.selected[filePath] = struct{}{}
				}
				// Move cursor down after selection/deselection
				if p.cursor < len(p.files)-1 {
					p.cursor++
				}
			}
		default:
			// Handle active search
			if len(msg.String()) == 1 { // Only process single character inputs
				p.searchQuery += msg.String()
				lowerQuery := strings.ToLower(p.searchQuery)

				// Priority 1: Prefix match
				found := false
				for i, f := range p.files {
					if strings.HasPrefix(strings.ToLower(f.Name), lowerQuery) {
						p.cursor = i
						found = true
						break
					}
				}

				// Priority 2: Fuzzy match
				if !found {
					for i, f := range p.files {
						if fuzzyMatch(p.searchQuery, f.Name) {
							p.cursor = i
							break
						}
					}
				}
			}
		}
	}

	// Ensure viewport is within bounds
	if p.cursor < p.viewportY {
		p.viewportY = p.cursor
	}
	if p.cursor >= p.viewportY+p.height-2 {
		p.viewportY = p.cursor - p.height + 3
	}

	return p, nil
}
