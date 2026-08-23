package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
	"unicode/utf8"

	"github.com/atotto/clipboard"
	"github.com/aymanbagabas/go-osc52/v2"
	tea "github.com/charmbracelet/bubbletea"
)

// progressTickInterval is how often the progress widget refreshes while the
// queue is busy. Fast enough to feel live, slow enough to stay off the CPU.
const progressTickInterval = 100 * time.Millisecond

// Commands
func (p pane) loadDirectoryCmd(focusPath string) tea.Cmd {
	return func() tea.Msg {
		files, err := readDirectory(p.path)
		return directoryLoadedMsg{paneID: p.id, files: files, err: err, focusPath: focusPath}
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
		return folderCreatedMsg{err: err, folderPath: path}
	}
}

// probeConflictsCmd checks which of sourceFiles already exist at destPath.
// It only looks — the actual work is queued afterwards — so that the user can
// answer the overwrite prompt before anything starts, and so that files with no
// conflict are never held hostage by the ones that do have a conflict.
func probeConflictsCmd(sourceFiles []file, destPath string, moving bool) tea.Cmd {
	return func() tea.Msg {
		msg := conflictProbeMsg{dest: destPath, moving: moving}

		for _, srcFile := range sourceFiles {
			destFilePath := filepath.Join(destPath, srcFile.Name)
			if _, err := os.Stat(destFilePath); err == nil {
				msg.conflicts = append(msg.conflicts, fileConflict{Source: srcFile, Destination: destFilePath})
			} else if os.IsNotExist(err) {
				msg.approved = append(msg.approved, srcFile)
			} else {
				msg.err = fmt.Errorf("checking %s: %w", destFilePath, err)
				return msg
			}
		}
		return msg
	}
}

// runOpCmd executes a queued operation. Bubble Tea runs every command on its
// own goroutine, so op.run() can block for the whole transfer; the UI keeps
// moving because progress is published through op's atomics and picked up by
// the progress tick, not by this command's return value.
func runOpCmd(op *fileOp) tea.Cmd {
	return func() tea.Msg {
		op.run()
		return opFinishedMsg{id: op.id}
	}
}

// progressTickCmd schedules the next repaint of the progress widget.
func progressTickCmd() tea.Cmd {
	return tea.Tick(progressTickInterval, func(t time.Time) tea.Msg {
		return progressTickMsg(t)
	})
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

func unmountDriveCmd(path string) tea.Cmd {
	return func() tea.Msg {
		cmd := exec.Command("umount", path)
		err := cmd.Run()
		return driveUnmountedMsg{err: err, drivePath: path}
	}
}

func copyToClipboardCmd(text string) tea.Cmd {
	return func() tea.Msg {
		err := clipboard.WriteAll(text)
		if err != nil {
			// Fallback to OSC 52
			osc52.New(text).WriteTo(os.Stderr)
			// We don't return the error if OSC 52 "succeeds" (it just writes to stderr)
			// But strictly speaking we don't know if the terminal handled it.
			// However, it's better than failing.
			return clipboardCopiedMsg{err: nil}
		}
		return clipboardCopiedMsg{err: err}
	}
}
