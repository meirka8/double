package main

import (
	"context"
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

// fileProgress lets the recursive helpers below report incremental progress
// without knowing anything about the queue or Bubble Tea. The zero value
// reports nothing, which is what callers that only care about the final error
// should pass.
type fileProgress struct {
	onBytes    func(n int64)     // n bytes were written since the last call
	onFile     func(name string) // work started on a new file
	onFileDone func()            // a file was finished
}

func (p fileProgress) bytes(n int64) {
	if p.onBytes != nil {
		p.onBytes(n)
	}
}

func (p fileProgress) file(name string) {
	if p.onFile != nil {
		p.onFile(name)
	}
}

func (p fileProgress) fileDone() {
	if p.onFileDone != nil {
		p.onFileDone()
	}
}

// copyBufSize is the chunk size for progress-reporting copies. Large enough to
// keep syscall overhead negligible, small enough that the bar still moves on a
// slow device.
const copyBufSize = 256 * 1024

// isCrossDevice reports whether err is the EXDEV that os.Rename returns when
// source and destination live on different filesystems.
func isCrossDevice(err error) bool {
	return errors.Is(err, syscall.EXDEV)
}

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

// copyPath copies a file or a whole directory tree, reporting progress as it
// goes and aborting as soon as ctx is cancelled.
func copyPath(ctx context.Context, src file, dst string, p fileProgress) error {
	if src.IsDir {
		return copyDir(ctx, src.Path, dst, p)
	}
	return copyFile(ctx, src.Path, dst, p)
}

// copyFile copies a single file from src to dst.
func copyFile(ctx context.Context, src, dst string, p fileProgress) error {
	p.file(filepath.Base(src))

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

	// io.Copy would be shorter, but the whole point is to observe the transfer
	// as it happens, so the loop stays explicit.
	buf := make([]byte, copyBufSize)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		n, readErr := sourceFile.Read(buf)
		if n > 0 {
			if _, err := destFile.Write(buf[:n]); err != nil {
				return err
			}
			p.bytes(int64(n))
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
	}

	sourceInfo, err := os.Stat(src)
	if err != nil {
		return err
	}
	if err := os.Chmod(dst, sourceInfo.Mode()); err != nil {
		return err
	}

	p.fileDone()
	return nil
}

// copyDir recursively copies a directory from src to dst.
func copyDir(ctx context.Context, src, dst string, p fileProgress) error {
	if err := ctx.Err(); err != nil {
		return err
	}

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
			err = copyDir(ctx, srcPath, dstPath, p)
			if err != nil {
				return err
			}
		} else {
			err = copyFile(ctx, srcPath, dstPath, p)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// removeTree deletes a file, or a directory and everything under it, reporting
// one unit of progress per entry removed. It is the progress-aware counterpart
// to os.RemoveAll, which finishes silently and tells us nothing on the way.
func removeTree(ctx context.Context, path string, isDir bool, p fileProgress) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if isDir {
		entries, err := os.ReadDir(path)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if err := removeTree(ctx, filepath.Join(path, entry.Name()), entry.IsDir(), p); err != nil {
				return err
			}
		}
	}

	// Only leaf entries are counted, so that doneFiles stays comparable with the
	// total scanSources produced (which likewise ignores directories).
	if !isDir {
		p.file(filepath.Base(path))
	}
	if err := os.Remove(path); err != nil {
		return err
	}
	if !isDir {
		p.fileDone()
	}
	return nil
}

// scanSources measures a set of sources up front so a progress bar has a
// denominator. Directories are walked; the returned count excludes directories
// themselves, matching what the copy and delete loops actually report.
func scanSources(ctx context.Context, files []file) (totalBytes int64, totalFiles int64, err error) {
	for _, f := range files {
		if err := ctx.Err(); err != nil {
			return 0, 0, err
		}

		if !f.IsDir {
			totalBytes += f.Size
			totalFiles++
			continue
		}

		walkErr := filepath.WalkDir(f.Path, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			totalBytes += info.Size()
			totalFiles++
			return nil
		})
		if walkErr != nil {
			return 0, 0, walkErr
		}
	}
	return totalBytes, totalFiles, nil
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

// getMountedDrives returns a list of mounted drive paths on Linux systems.
func getMountedDrives() []string {
	content, err := os.ReadFile("/proc/mounts")
	if err != nil {
		return nil
	}

	var drives []string
	lines := strings.Split(string(content), "\n")
	seen := make(map[string]struct{})

	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		mountPoint := fields[1]
		// Unescape octal sequences (like \040 for space)
		mountPoint = strings.ReplaceAll(mountPoint, "\\040", " ")
		mountPoint = strings.ReplaceAll(mountPoint, "\\011", "\t")

		// Filter for common external drive mount points
		if strings.HasPrefix(mountPoint, "/media") ||
			strings.HasPrefix(mountPoint, "/mnt") ||
			strings.HasPrefix(mountPoint, "/run/media") {
			if _, ok := seen[mountPoint]; !ok {
				seen[mountPoint] = struct{}{}
				drives = append(drives, mountPoint)
			}
		}
	}
	sort.Strings(drives)
	return drives
}

// getStandardPaths returns common user directories.
func getStandardPaths() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return []string{}
	}

	paths := []string{home}
	dirs := []string{"Documents", "Downloads", "Desktop", "Pictures", "Music", "Videos"}

	for _, d := range dirs {
		p := filepath.Join(home, d)
		if _, err := os.Stat(p); err == nil {
			paths = append(paths, p)
		}
	}
	return paths
}
