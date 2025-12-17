package main

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"time"
)

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
func copyFile(src, dst string, onProgress func(int64)) error {
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

	// Use a small buffer and copy manually to track progress
	buf := make([]byte, 32*1024) // 32KB buffer
	for {
		n, err := sourceFile.Read(buf)
		if n > 0 {
			_, wErr := destFile.Write(buf[:n])
			if wErr != nil {
				return wErr
			}
			if onProgress != nil {
				onProgress(int64(n))
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
	}

	sourceInfo, err := os.Stat(src)
	if err != nil {
		return err
	}
	return os.Chmod(dst, sourceInfo.Mode())
}

// copyDir recursively copies a directory from src to dst.
func copyDir(src, dst string, onProgress func(int64)) error {
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
			err = copyDir(srcPath, dstPath, onProgress)
			if err != nil {
				return err
			}
		} else {
			err = copyFile(srcPath, dstPath, onProgress)
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

	// Add ".." entry if not root
	if filepath.Dir(dirPath) != dirPath {
		parent := file{
			Name:    "..",
			Path:    filepath.Dir(dirPath),
			IsDir:   true,
			Mode:    os.ModeDir,
			ModTime: time.Now(), // Dummy time
		}
		files = append([]file{parent}, files...)
	}

	return files, nil
}

// calculateTotalSize calculates the total size and file count of a list of files/directories.
func calculateTotalSize(files []file) (int64, int, error) {
	var totalSize int64
	var totalFiles int

	for _, f := range files {
		if f.IsDir {
			err := filepath.WalkDir(f.Path, func(path string, d os.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if !d.IsDir() {
					info, err := d.Info()
					if err != nil {
						return err
					}
					totalSize += info.Size()
					totalFiles++
				}
				return nil
			})
			if err != nil {
				return 0, 0, err
			}
		} else {
			totalSize += f.Size
			totalFiles++
		}
	}
	return totalSize, totalFiles, nil
}
