package main

import (
	"io/fs"
	"log"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
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
	previewScrollY        int
	keyMap                KeyMap
	modifierState         ModifierState
	aliasMap              map[string]string
}

// ModifierState tracks the state of modifier keys.
type ModifierState struct {
	Ctrl  bool
	Alt   bool
	Shift bool
}

// initialModel creates a new model with default state.
func initialModel() model {
	cwd, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}

	km := DefaultKeyMap()
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
		keyMap:   km,
		aliasMap: km.GetAliasMap(),
	}
}

// Init initializes the application.
func (m model) Init() tea.Cmd {
	return tea.Batch(m.leftPane.loadDirectoryCmd(""), m.rightPane.loadDirectoryCmd(""))
}
