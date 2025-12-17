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

type clipboardCopiedMsg struct {
	err error
}

type progressMsg struct {
	TotalBytes     int64
	CurrentBytes   int64
	TotalFiles     int
	ProcessedFiles int
	CurrentFile    string
	Done           bool
	Err            error
}
