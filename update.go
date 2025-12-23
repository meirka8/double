package main

import (
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// Update handles messages and updates the model.
func (m *model) processOverwriteConflicts() tea.Cmd {
	if m.skipAll {
		m.overwriteConflicts = nil // Skip all remaining
	}

	if len(m.overwriteConflicts) == 0 {
		m.isConfirmingOverwrite = false
		m.overwriteAll = false
		m.skipAll = false
		return nil
	}

	var filesToOperate []file
	for _, conflict := range m.overwriteConflicts {
		filesToOperate = append(filesToOperate, conflict.Source)
	}

	if m.isMoving {
		return moveFilesCmd(filesToOperate, filepath.Dir(m.overwriteConflicts[0].Destination), true)
	}
	return copyFilesCmd(filesToOperate, filepath.Dir(m.overwriteConflicts[0].Destination), true)
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
				m.isDeleting = false
				cmd = deleteFileCmd(m.fileToDelete)
				m.fileToDelete = file{} // Clear file to delete
				return m, cmd
			case "n", "N", "esc":
				m.isDeleting = false
				m.fileToDelete = file{} // Clear file to delete
				return m, nil
			}
		}
	} else if m.isConfirmingOverwrite {
		switch msg := msg.(type) {
		case tea.KeyMsg:
			switch msg.String() {
			case "y", "Y":
				// Overwrite the current file and process the rest
				conflict := m.overwriteConflicts[0]
				m.overwriteConflicts = m.overwriteConflicts[1:]
				var operationCmd tea.Cmd
				if m.isMoving {
					operationCmd = moveFilesCmd([]file{conflict.Source}, filepath.Dir(conflict.Destination), true)
				} else {
					operationCmd = copyFilesCmd([]file{conflict.Source}, filepath.Dir(conflict.Destination), true)
				}
				return m, tea.Sequence(operationCmd, m.processOverwriteConflicts())

			case "n", "N":
				// Skip the current file and process the rest
				m.overwriteConflicts = m.overwriteConflicts[1:]
				return m, m.processOverwriteConflicts()

			case "a", "A":
				m.overwriteAll = true
				return m, m.processOverwriteConflicts()

			case "s", "S": // Skip All
				m.skipAll = true
				return m, m.processOverwriteConflicts()

			case "esc":
				m.isConfirmingOverwrite = false
				m.overwriteConflicts = nil
				m.overwriteAll = false
				m.skipAll = false
				return m, nil
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
					m.isMoving = false                              // It's a copy operation
					sourcePane.selected = make(map[string]struct{}) // Clear selection
					return m, copyFilesCmd(files, destPane.path, false)
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
					m.isMoving = true                               // It's a move operation
					sourcePane.selected = make(map[string]struct{}) // Clear selection
					return m, moveFilesCmd(files, destPane.path, false)
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
				if len(activePane.files) > 0 {
					m.isDeleting = true
					m.fileToDelete = activePane.files[activePane.cursor]
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
			// Reload directory in active pane
			if m.leftPane.active {
				return m, m.leftPane.loadDirectoryCmd("")
			} else {
				return m, m.rightPane.loadDirectoryCmd("")
			}
		}
		return m, nil
	case fileDeletedMsg:
		if msg.err != nil {
			m.err = msg.err
		} else {
			// Adjust cursor if it's out of bounds after deletion
			activePane := &m.leftPane
			if m.rightPane.active {
				activePane = &m.rightPane
			}
			if activePane.cursor >= len(activePane.files)-1 && activePane.cursor > 0 {
				activePane.cursor--
			}

			// Reload directory in active pane
			if m.leftPane.active {
				return m, m.leftPane.loadDirectoryCmd("")
			} else {
				return m, m.rightPane.loadDirectoryCmd("")
			}
		}
		return m, nil
	case fileConflictMsg:
		m.isConfirmingOverwrite = true
		m.overwriteConflicts = msg.Conflicts
		return m, nil
	case fileOperationMsg: // For copy/move operations
		if msg.err != nil {
			m.err = msg.err
		} else {
			// Reload both source and destination panes
			cmds := []tea.Cmd{m.leftPane.loadDirectoryCmd(""), m.rightPane.loadDirectoryCmd("")}
			return m, tea.Batch(cmds...)
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
	if !m.isCreatingFolder && !m.isDeleting && !m.isConfirmingOverwrite && !m.isPreviewing {
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
			p.cursor = len(p.files) - 1
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
		case "insert", "alt+i":
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
				lowerSearchQuery := strings.ToLower(p.searchQuery)

				for i, f := range p.files {
					if strings.HasPrefix(strings.ToLower(f.Name), lowerSearchQuery) {
						p.cursor = i
						break
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
