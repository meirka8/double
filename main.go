package main

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	// Styles
	docStyle             = lipgloss.NewStyle().Margin(1, 2)
	activeStyle          = lipgloss.NewStyle().Border(lipgloss.NormalBorder(), true).BorderForeground(lipgloss.Color("63"))
	inactiveStyle        = lipgloss.NewStyle().Border(lipgloss.NormalBorder(), true).BorderForeground(lipgloss.Color("240"))
	cursorStyle          = lipgloss.NewStyle().Background(lipgloss.Color("63")).Foreground(lipgloss.Color("255"))
	selectionStyle       = lipgloss.NewStyle().Background(lipgloss.Color("220")).Foreground(lipgloss.Color("0"))
	dirStyle             = lipgloss.NewStyle().Foreground(lipgloss.Color("33"))
	fileStyle            = lipgloss.NewStyle().Foreground(lipgloss.Color("255"))
	statusBar            = lipgloss.NewStyle().Background(lipgloss.Color("235")).Foreground(lipgloss.Color("250")).Padding(0, 1)
	statusBarActive      = lipgloss.NewStyle().Background(lipgloss.Color("63")).Foreground(lipgloss.Color("255")).Padding(0, 1)
	inputPromptStyle     = lipgloss.NewStyle().Background(lipgloss.Color("235")).Foreground(lipgloss.Color("255")).Padding(0, 1)
	confirmPromptStyle   = lipgloss.NewStyle().Background(lipgloss.Color("166")).Foreground(lipgloss.Color("255")).Padding(0, 1)
	overwritePromptStyle = lipgloss.NewStyle().Background(lipgloss.Color("202")).Foreground(lipgloss.Color("0")).Padding(0, 1)
	previewStyle         = lipgloss.NewStyle().Border(lipgloss.DoubleBorder(), true).BorderForeground(lipgloss.Color("205")).Padding(1, 2)
)

// file represents a file or directory entry.
type file struct {
	Name    string
	Path    string
	Size    int64
	Mode    fs.FileMode
	ModTime time.Time
	IsDir   bool
}

// fileConflict represents a file that already exists at the destination.
type fileConflict struct {
	Source      file
	Destination string
}

// pane represents one of the two file listing panels.
type pane struct {
	id          int
	path        string
	files       []file
	selected    map[string]struct{} // Paths of selected files
	cursor      int
	active      bool
	viewportY   int // Top of the visible area in the file list
	height      int // Height of the pane's display area
	width       int // Width of the pane's display area
	searchQuery string
	err         error // Error encountered during directory loading
}

// model is the main application model.
type model struct {
	leftPane              pane
	rightPane             pane
	quitting              bool
	err                   error
	isCreatingFolder      bool
	folderNameInput       string
	isDeleting            bool
	fileToDelete          file
	isConfirmingOverwrite bool
	overwriteConflicts    []fileConflict
	overwriteAll          bool
	skipAll               bool
	isMoving              bool // To know if the operation is a move or copy
	isPreviewing          bool
	previewContent        string
	previewFilePath       string
	previewWidth          int
	previewHeight         int
}

// initialModel creates a new model with default state.
func initialModel() model {
	cwd, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}

	return model{
		leftPane: pane{
			id:       0,
			path:     cwd,
			active:   true,
			selected: make(map[string]struct{}),
		},
		rightPane: pane{
			id:       1,
			path:     cwd,
			active:   false,
			selected: make(map[string]struct{}),
		},
	}
}

// Init initializes the application.
func (m model) Init() tea.Cmd {
	return tea.Batch(m.leftPane.loadDirectoryCmd(), m.rightPane.loadDirectoryCmd())
}

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

	// Handle operations that take precedence over normal key presses
	if m.isCreatingFolder {
		switch msg := msg.(type) {
		case tea.KeyMsg:
			switch msg.String() {
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
				return m, nil
			}
		}
	} else { // Normal operation mode
		switch msg := msg.(type) {
		case tea.KeyMsg:
			switch msg.String() {
			case "alt+q", "f10", "\x1b[21~": // alt+q, f10
				m.quitting = true
				return m, tea.Quit
			case "ctrl+c": // Always allow ctrl+c to quit
				m.quitting = true
				return m, tea.Quit
			case "tab":
				m.leftPane.active = !m.leftPane.active
				m.rightPane.active = !m.rightPane.active
				return m, nil
			case "alt+v", "f3", "\x1b[13~": // alt+v, f3
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
						return m, previewFileCmd(selectedFile.Path)
					}
				}
				return m, nil
			case "alt+c", "f5", "\x1b[15~": // alt+c, f5
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
					m.isMoving = false // It's a copy operation
					sourcePane.selected = make(map[string]struct{}) // Clear selection
					return m, copyFilesCmd(files, destPane.path, false)
				}
				return m, nil
			case "alt+m", "f6", "\x1b[17~": // alt+m, f6
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
					m.isMoving = true // It's a move operation
					sourcePane.selected = make(map[string]struct{}) // Clear selection
					return m, moveFilesCmd(files, destPane.path, false)
				}
				return m, nil
			case "alt+n", "f7", "\x1b[18~": // alt+n, f7
				m.isCreatingFolder = true
				return m, nil
			case "alt+d", "f8", "\x1b[19~": // alt+d, f8
				activePane := &m.leftPane
				if m.rightPane.active {
					activePane = &m.rightPane
				}
				if len(activePane.files) > 0 {
					m.isDeleting = true
					m.fileToDelete = activePane.files[activePane.cursor]
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
		} else if msg.paneID == m.rightPane.id {
			m.rightPane.files = msg.files
			m.rightPane.err = msg.err
		}
		return m, nil
	case tea.WindowSizeMsg:
		// Handle window resizing
		paneHeight := msg.Height - 1 // Adjust for status bar
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
				return m, m.leftPane.loadDirectoryCmd()
			} else {
				return m, m.rightPane.loadDirectoryCmd()
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
				return m, m.leftPane.loadDirectoryCmd()
			} else {
				return m, m.rightPane.loadDirectoryCmd()
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
			cmds := []tea.Cmd{m.leftPane.loadDirectoryCmd(), m.rightPane.loadDirectoryCmd()}
			return m, tea.Batch(cmds...)
		}
		return m, nil
	case previewReadyMsg:
		m.previewContent = msg.Content
		if msg.Err != nil {
			m.err = msg.Err
		}
		return m, nil
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

func main() {
	p := tea.NewProgram(initialModel(), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Printf("Alas, there's been an error: %v", err)
		os.Exit(1)
	}
}

// Messages
type directoryLoadedMsg struct {
	paneID int
	files  []file
	err    error
}

type fileOpenedMsg struct {
	err error
}

type folderCreatedMsg struct {
	err error
}

type fileDeletedMsg struct {
	err error
}

type fileOperationMsg struct { // For copy/move
	err error
}

type fileConflictMsg struct {
	Conflicts []fileConflict
}

type previewReadyMsg struct {
	Content string
	Err     error
}

// Commands
func (p pane) loadDirectoryCmd() tea.Cmd {
	return func() tea.Msg {
		files, err := readDirectory(p.path)
		return directoryLoadedMsg{paneID: p.id, files: files, err: err}
	}
}

func openFileCmd(path string) tea.Cmd {
	return func() tea.Msg {
		cmd := exec.Command("xdg-open", path)
		err := cmd.Run()
		return fileOpenedMsg{err: err}
	}
}

func createFolderCmd(path string) tea.Cmd {
	return func() tea.Msg {
		err := os.Mkdir(path, 0755)
		return folderCreatedMsg{err: err}
	}
}

func deleteFileCmd(f file) tea.Cmd {
	return func() tea.Msg {
		var err error
		if f.IsDir {
			err = os.RemoveAll(f.Path)
		} else {
			err = os.Remove(f.Path)
		}
		return fileDeletedMsg{err: err}
	}
}

func copyFilesCmd(sourceFiles []file, destPath string, force bool) tea.Cmd {
	return func() tea.Msg {
		if !force {
			var conflicts []fileConflict
			for _, srcFile := range sourceFiles {
				destFilePath := filepath.Join(destPath, srcFile.Name)
				if _, err := os.Stat(destFilePath); !os.IsNotExist(err) {
					conflicts = append(conflicts, fileConflict{Source: srcFile, Destination: destFilePath})
				}
			}
			if len(conflicts) > 0 {
				return fileConflictMsg{Conflicts: conflicts}
			}
		}

		for _, srcFile := range sourceFiles {
			destFilePath := filepath.Join(destPath, srcFile.Name)
			if srcFile.IsDir {
				err := copyDir(srcFile.Path, destFilePath)
				if err != nil {
					return fileOperationMsg{err: fmt.Errorf("failed to copy directory %s: %w", srcFile.Name, err)}
				}
			} else {
				err := copyFile(srcFile.Path, destFilePath)
				if err != nil {
					return fileOperationMsg{err: fmt.Errorf("failed to copy file %s: %w", srcFile.Name, err)}
				}
			}
		}
		return fileOperationMsg{err: nil}
	}
}

func moveFilesCmd(sourceFiles []file, destPath string, force bool) tea.Cmd {
	return func() tea.Msg {
		if !force {
			var conflicts []fileConflict
			for _, srcFile := range sourceFiles {
				destFilePath := filepath.Join(destPath, srcFile.Name)
				if _, err := os.Stat(destFilePath); !os.IsNotExist(err) {
					conflicts = append(conflicts, fileConflict{Source: srcFile, Destination: destFilePath})
				}
			}
			if len(conflicts) > 0 {
				return fileConflictMsg{Conflicts: conflicts}
			}
		}

		for _, srcFile := range sourceFiles {
			destFilePath := filepath.Join(destPath, srcFile.Name)
			err := os.Rename(srcFile.Path, destFilePath)
			if err != nil {
				return fileOperationMsg{err: fmt.Errorf("failed to move %s: %w", srcFile.Name, err)}
			}
		}
		return fileOperationMsg{err: nil}
	}
}

func previewFileCmd(path string) tea.Cmd {
	return func() tea.Msg {
		content, err := os.ReadFile(path)
		if err != nil {
			return previewReadyMsg{Err: fmt.Errorf("could not read file: %w", err)}
		}

		// Basic check for binary content
		if !utf8.Valid(content) || bytes.Contains(content, []byte{0}) {
			return previewReadyMsg{Content: fmt.Sprintf("--- Binary file: %s ---", filepath.Base(path))}
		}

		// Limit preview size
		const maxPreviewSize = 1024 * 100 // 100KB
		if len(content) > maxPreviewSize {
			return previewReadyMsg{Content: fmt.Sprintf("--- File too large for preview (%s), showing first %d bytes ---\n%s", filepath.Base(path), maxPreviewSize, content[:maxPreviewSize])}
		}

		return previewReadyMsg{Content: string(content)}
	}
}

// Helper to get file structs from selected paths
func getFilesFromSelected(p pane) []file {
	var files []file
	for _, f := range p.files {
		if _, ok := p.selected[f.Path]; ok {
			files = append(files, f)
		}
	}
	return files
}

// copyFile copies a single file from src to dst.
func copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, sourceFile)
	if err != nil {
		return err
	}

	sourceInfo, err := os.Stat(src)
	if err != nil {
		return err
	}
	return os.Chmod(dst, sourceInfo.Mode())
}

// copyDir recursively copies a directory from src to dst.
func copyDir(src, dst string) error {
	sourceInfo, err := os.Stat(src)
	if err != nil {
		return err
	}

	err = os.MkdirAll(dst, sourceInfo.Mode())
	if err != nil {
		return err
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		if entry.IsDir() {
			err = copyDir(srcPath, dstPath)
			if err != nil {
				return err
			}
		} else {
			err = copyFile(srcPath, dstPath)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// readDirectory reads the contents of a directory and returns a sorted list of file structs.
func readDirectory(dirPath string) ([]file, error) {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return nil, err
	}

	var files []file
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			log.Printf("Error getting file info for %s: %v", filepath.Join(dirPath, entry.Name()), err)
			continue
		}

		files = append(files, file{
			Name:    entry.Name(),
			Path:    filepath.Join(dirPath, entry.Name()),
			Size:    info.Size(),
			Mode:    info.Mode(),
			ModTime: info.ModTime(),
			IsDir:   entry.IsDir(),
		})
	}

	// Sort files: directories first, then alphabetically
	sort.Slice(files, func(i, j int) bool {
		if files[i].IsDir != files[j].IsDir {
			return files[i].IsDir // Directories come before files
		}
		return files[i].Name < files[j].Name
	})

	return files, nil
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
		case "up", "k":
			p.searchQuery = "" // Clear search on navigation
			if p.cursor > 0 {
				p.cursor--
			}
		case "down", "j":
			p.searchQuery = "" // Clear search on navigation
			if p.cursor < len(p.files)-1 {
				p.cursor++
			}
		case "enter":
			p.searchQuery = "" // Clear search on navigation
			if len(p.files) > 0 {
				selectedFile := p.files[p.cursor]
				if selectedFile.IsDir {
					p.path = selectedFile.Path
					p.cursor = 0 // Reset cursor when entering a new directory
					return p, p.loadDirectoryCmd()
				} else {
					return p, openFileCmd(selectedFile.Path)
				}
			}
		case "backspace", "h": // Go up one directory
			p.searchQuery = "" // Clear search on navigation
			parentPath := filepath.Dir(p.path)
			if parentPath != p.path { // Ensure we don't go above root
				p.path = parentPath
				p.cursor = 0 // Reset cursor when going up
				return p, p.loadDirectoryCmd()
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
	return p, nil
}
