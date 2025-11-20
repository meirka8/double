package main

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
