package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
	return deleteFilesCmd([]file{f})
}

func deleteFilesCmd(files []file) tea.Cmd {
	return func() tea.Msg {
		var errors []string
		for _, f := range files {
			var err error
			if f.IsDir {
				err = os.RemoveAll(f.Path)
			} else {
				err = os.Remove(f.Path)
			}
			if err != nil {
				errors = append(errors, fmt.Sprintf("%s: %v", f.Name, err))
			}
		}
		if len(errors) > 0 {
			return fileDeletedMsg{err: fmt.Errorf("failed to delete: %s", strings.Join(errors, ", "))}
		}
		return fileDeletedMsg{err: nil}
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
