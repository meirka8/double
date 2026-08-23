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
	width                 int // Full terminal width
	height                int // Full terminal height
	leftPane              pane
	rightPane             pane
	quitting              bool
	err                   error
	isCreatingFolder      bool
	folderNameInput       string
	isDeleting            bool
	filesToDelete         []file
	isConfirmingOverwrite bool
	overwriteConflicts    []fileConflict // Still to be asked about, one at a time
	pendingApproved       []file         // Cleared to proceed once the prompt is done
	pendingDest           string
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
	favorites             []string
	drives                []string
	isFavoritesOpen       bool
	favoritesCursor       int
	isConfirmingRemoveFav bool
	favToRemove           int
	isConfirmingUnmount   bool
	driveToUnmount        string

	// Operation queue. Entries run strictly one at a time, oldest first.
	queue        []*fileOp
	nextOpID     int
	queueTicking bool // A progressTickMsg is already in flight
	isQueueOpen  bool
	queueCursor  int
}

// startedAt doubles as the dispatch marker for the two lookups below. It is set
// on the event loop the instant the worker command is handed to Bubble Tea,
// which is what stops pumpQueue from starting the same operation twice in the
// window before its goroutine gets to publish a running state.

// runningOp returns the dispatched operation that has not finished yet, or nil
// when nothing is running. Only one operation ever executes at a time.
func (m model) runningOp() *fileOp {
	for _, op := range m.queue {
		if !op.startedAt.IsZero() && !opState(op.state.Load()).terminal() {
			return op
		}
	}
	return nil
}

// nextQueuedOp returns the oldest operation that has not been dispatched yet.
func (m model) nextQueuedOp() *fileOp {
	for _, op := range m.queue {
		if op.startedAt.IsZero() && !opState(op.state.Load()).terminal() {
			return op
		}
	}
	return nil
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

	favorites := getStandardPaths()
	drives := getMountedDrives()

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
		keyMap:    km,
		aliasMap:  km.GetAliasMap(),
		favorites: favorites,
		drives:    drives,
	}
}

// Init initializes the application.
func (m model) Init() tea.Cmd {
	return tea.Batch(m.leftPane.loadDirectoryCmd(""), m.rightPane.loadDirectoryCmd(""))
}
