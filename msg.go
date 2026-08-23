package main

import "time"

// Messages
type directoryLoadedMsg struct {
	paneID    int
	files     []file
	err       error
	focusPath string
}

type fileOpenedMsg struct {
	err error
}

type folderCreatedMsg struct {
	err        error
	folderPath string
}

// conflictProbeMsg reports the result of checking a copy/move destination
// before anything is queued. Sources are split so that files with no conflict
// can proceed regardless of how the user answers about the rest.
type conflictProbeMsg struct {
	approved  []file
	conflicts []fileConflict
	dest      string
	moving    bool
	err       error
}

// opFinishedMsg is returned by the command that ran an operation to completion,
// whatever its outcome; the outcome itself lives on the fileOp.
type opFinishedMsg struct {
	id int
}

// progressTickMsg drives the periodic re-render of the progress widget while
// the queue is busy.
type progressTickMsg time.Time

type previewReadyMsg struct {
	Content string
	Err     error
}

type clipboardCopiedMsg struct {
	err error
}

type driveUnmountedMsg struct {
	err       error
	drivePath string
}
