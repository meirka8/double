package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"unicode/utf8"

	"github.com/atotto/clipboard"
	"github.com/aymanbagabas/go-osc52/v2"
	tea "github.com/charmbracelet/bubbletea"
)

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

// waitForProgressMsg waits for a progress message from the channel.
func waitForProgressMsg(sub chan progressMsg) tea.Cmd {
	return func() tea.Msg {
		return <-sub
	}
}

func copyFilesCmd(sourceFiles []file, destPath string, force bool, progressChan chan<- progressMsg) tea.Cmd {
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

		// Calculate total size and files
		totalBytes, totalFiles, err := calculateTotalSize(sourceFiles)
		if err != nil {
			return copyStartedMsg{err: err}
		}

		go func() {
			currentBytes := int64(0)
			processedFiles := 0

			progressChan <- progressMsg{
				TotalBytes:   totalBytes,
				CurrentBytes: 0,
				TotalFiles:   totalFiles,
				Done:         false,
			}

			for _, srcFile := range sourceFiles {
				destFilePath := filepath.Join(destPath, srcFile.Name)

				progressChan <- progressMsg{
					TotalBytes:   totalBytes,
					CurrentBytes: currentBytes,
					TotalFiles:   totalFiles,
					CurrentFile:  srcFile.Name,
					Done:         false,
				}

				var err error
				if srcFile.IsDir {
					err = copyDir(srcFile.Path, destFilePath, func(n int64) {
						currentBytes += n
						progressChan <- progressMsg{
							TotalBytes:     totalBytes,
							CurrentBytes:   currentBytes,
							TotalFiles:     totalFiles,
							ProcessedFiles: processedFiles,
							CurrentFile:    srcFile.Name, // Ideally show subfile
							Done:           false,
						}
					})
				} else {
					err = copyFile(srcFile.Path, destFilePath, func(n int64) {
						currentBytes += n
						progressChan <- progressMsg{
							TotalBytes:     totalBytes,
							CurrentBytes:   currentBytes,
							TotalFiles:     totalFiles,
							ProcessedFiles: processedFiles,
							CurrentFile:    srcFile.Name,
							Done:           false,
						}
					})
				}

				if err != nil {
					progressChan <- progressMsg{Err: fmt.Errorf("failed to copy %s: %w", srcFile.Name, err), Done: true}
					return
				}
				processedFiles++
			}
			progressChan <- progressMsg{Done: true}
		}()

		// Return a nil message effectively, but we might want to signal start?
		// But the goroutine sends the first progressMsg immediately.
		// The caller will run waitForProgressMsg.
		return copyStartedMsg{}
	}
}

type copyStartedMsg struct {
	err error
}

func moveFilesCmd(sourceFiles []file, destPath string, force bool, progressChan chan<- progressMsg) tea.Cmd {
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

		// Check for cross-device move?
		// For now we assume os.Rename works or fails.
		// If we wanted to support robust move (copy+del), we would need similar logic to copyFilesCmd.
		// Let's implement basic progress reporting for Rename.

		totalFiles := len(sourceFiles)

		go func() {
			for i, srcFile := range sourceFiles {
				progressChan <- progressMsg{
					TotalFiles:     totalFiles,
					ProcessedFiles: i,
					CurrentFile:    srcFile.Name,
					Done:           false,
				}

				destFilePath := filepath.Join(destPath, srcFile.Name)
				err := os.Rename(srcFile.Path, destFilePath)
				if err != nil {
					// Todo: handle cross-device link error by falling back to copy+delete?
					// For now, report error.
					progressChan <- progressMsg{Err: fmt.Errorf("failed to move %s: %w", srcFile.Name, err), Done: true}
					return
				}
			}
			progressChan <- progressMsg{Done: true}
		}()

		return copyStartedMsg{} // Reusing copyStartedMsg or we can rename it to operationStartedMsg
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
