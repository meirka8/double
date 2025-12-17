package main

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

type clipboardCopiedMsg struct {
	err error
}
