package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

// opKind identifies what a queued operation does.
type opKind int

const (
	opCopy opKind = iota
	opMove
	opDelete
)

func (k opKind) String() string {
	switch k {
	case opCopy:
		return "Copy"
	case opMove:
		return "Move"
	case opDelete:
		return "Delete"
	}
	return "Op"
}

// opState is the lifecycle of a queued operation.
type opState int32

const (
	opQueued opState = iota
	opScanning
	opRunning
	opDone
	opFailed
	opCancelled
)

// terminal reports whether the operation will never make progress again.
func (s opState) terminal() bool { return s >= opDone }

func (s opState) String() string {
	switch s {
	case opQueued:
		return "queued"
	case opScanning:
		return "scanning"
	case opRunning:
		return "running"
	case opDone:
		return "done"
	case opFailed:
		return "failed"
	case opCancelled:
		return "cancelled"
	}
	return "?"
}

// fileOp is one entry in the operation queue.
//
// Field ownership matters here, because run() executes on Bubble Tea's command
// goroutine while Update reads the same struct on the event loop:
//
//   - the atomics and the mutex-guarded fields are written by run() and read by
//     Update only through snapshot();
//   - the trailing view-side fields are touched exclusively by Update;
//   - cancel is created up front (not inside run) so Update can cancel an
//     operation that has not started yet.
//
// The queue holds *fileOp rather than fileOp so that copying the model — Update
// has a value receiver — never copies the mutex.
type fileOp struct {
	id       int
	kind     opKind
	label    string
	sources  []file
	destPath string

	ctx    context.Context
	cancel context.CancelFunc

	state      atomic.Int32
	totalBytes atomic.Int64
	doneBytes  atomic.Int64
	totalFiles atomic.Int64
	doneFiles  atomic.Int64

	mu          sync.Mutex
	currentName string
	err         error

	// View-side fields, owned by Update.
	startedAt  time.Time
	finishedAt time.Time
	lastSample time.Time
	lastBytes  int64
	speed      float64 // EWMA of bytes/sec
}

func newFileOp(id int, kind opKind, sources []file, destPath string) *fileOp {
	ctx, cancel := context.WithCancel(context.Background())
	op := &fileOp{
		id:       id,
		kind:     kind,
		label:    describeSources(sources),
		sources:  sources,
		destPath: destPath,
		ctx:      ctx,
		cancel:   cancel,
	}
	op.state.Store(int32(opQueued))
	return op
}

func describeSources(sources []file) string {
	switch len(sources) {
	case 0:
		return "nothing"
	case 1:
		return sources[0].Name
	default:
		return fmt.Sprintf("%d items", len(sources))
	}
}

// opSnapshot is a consistent, lock-free copy of an operation's progress for the
// view to render.
type opSnapshot struct {
	id          int
	kind        opKind
	label       string
	state       opState
	totalBytes  int64
	doneBytes   int64
	totalFiles  int64
	doneFiles   int64
	currentName string
	err         error
}

func (o *fileOp) snapshot() opSnapshot {
	// Load state first: run() publishes err before storing a terminal state, so
	// reading in this order guarantees a visible err whenever state is opFailed.
	s := opState(o.state.Load())

	o.mu.Lock()
	current, err := o.currentName, o.err
	o.mu.Unlock()

	return opSnapshot{
		id:          o.id,
		kind:        o.kind,
		label:       o.label,
		state:       s,
		totalBytes:  o.totalBytes.Load(),
		doneBytes:   o.doneBytes.Load(),
		totalFiles:  o.totalFiles.Load(),
		doneFiles:   o.doneFiles.Load(),
		currentName: current,
		err:         err,
	}
}

// percent returns completion in [0,1]. Byte progress is preferred; operations
// with no byte total (deletes, and moves that are pure renames) fall back to
// the file count.
func (s opSnapshot) percent() float64 {
	switch {
	case s.totalBytes > 0:
		return clampUnit(float64(s.doneBytes) / float64(s.totalBytes))
	case s.totalFiles > 0:
		return clampUnit(float64(s.doneFiles) / float64(s.totalFiles))
	}
	return 0
}

// progress builds the reporter that the fs helpers write through.
func (o *fileOp) progress() fileProgress {
	return fileProgress{
		onBytes: func(n int64) { o.doneBytes.Add(n) },
		onFile: func(name string) {
			o.mu.Lock()
			o.currentName = name
			o.mu.Unlock()
		},
		onFileDone: func() { o.doneFiles.Add(1) },
	}
}

// run executes the operation to completion. It is invoked from a tea.Cmd, so it
// already owns a goroutine: Update learns about progress by polling the atomics
// on a tick, and about completion from the opFinishedMsg the command returns.
func (o *fileOp) run() {
	defer o.cancel()

	var err error
	switch o.kind {
	case opCopy:
		err = o.runCopy()
	case opMove:
		err = o.runMove()
	case opDelete:
		err = o.runDelete()
	}
	o.finish(err)
}

func (o *fileOp) runCopy() error {
	if err := o.scan(o.sources); err != nil {
		return err
	}
	o.state.Store(int32(opRunning))

	p := o.progress()
	for _, src := range o.sources {
		if err := o.ctx.Err(); err != nil {
			return err
		}
		if err := copyPath(o.ctx, src, filepath.Join(o.destPath, src.Name), p); err != nil {
			return fmt.Errorf("copy %s: %w", src.Name, err)
		}
	}
	return nil
}

// runMove renames first and only falls back to copy-then-delete for the sources
// that turn out to live on another filesystem. Renaming up front means a
// same-filesystem move stays instant and never pays for a tree walk.
func (o *fileOp) runMove() error {
	o.totalFiles.Store(int64(len(o.sources)))
	o.state.Store(int32(opRunning))
	p := o.progress()

	var crossDevice []file
	for _, src := range o.sources {
		if err := o.ctx.Err(); err != nil {
			return err
		}
		p.file(src.Name)

		err := os.Rename(src.Path, filepath.Join(o.destPath, src.Name))
		switch {
		case err == nil:
			p.fileDone()
		case isCrossDevice(err):
			crossDevice = append(crossDevice, src)
		default:
			return fmt.Errorf("move %s: %w", src.Name, err)
		}
	}

	if len(crossDevice) == 0 {
		return nil
	}

	// The remaining sources need a real copy, so switch the operation over to
	// byte-based progress for what is left.
	o.doneFiles.Store(0)
	if err := o.scan(crossDevice); err != nil {
		return err
	}
	o.state.Store(int32(opRunning))

	for _, src := range crossDevice {
		if err := o.ctx.Err(); err != nil {
			return err
		}
		dst := filepath.Join(o.destPath, src.Name)
		if err := copyPath(o.ctx, src, dst, p); err != nil {
			return fmt.Errorf("move %s: %w", src.Name, err)
		}
		// Only unlink the source once the copy landed, so a failed or cancelled
		// move never destroys the original.
		if err := removeTree(o.ctx, src.Path, src.IsDir, fileProgress{}); err != nil {
			return fmt.Errorf("move %s: removing source: %w", src.Name, err)
		}
	}
	return nil
}

func (o *fileOp) runDelete() error {
	if err := o.scan(o.sources); err != nil {
		return err
	}
	// Deletes are measured in files, not bytes: removing a 4 GB file costs the
	// same as removing an empty one.
	o.totalBytes.Store(0)
	o.state.Store(int32(opRunning))

	p := o.progress()
	for _, src := range o.sources {
		if err := o.ctx.Err(); err != nil {
			return err
		}
		if err := removeTree(o.ctx, src.Path, src.IsDir, p); err != nil {
			return fmt.Errorf("delete %s: %w", src.Name, err)
		}
	}
	return nil
}

// scan sizes the work up front so the bar has a denominator.
func (o *fileOp) scan(sources []file) error {
	o.state.Store(int32(opScanning))
	bytes, count, err := scanSources(o.ctx, sources)
	if err != nil {
		return err
	}
	o.totalBytes.Store(bytes)
	o.totalFiles.Store(count)
	o.doneBytes.Store(0)
	return nil
}

// finish publishes the terminal state. err is stored before the state so that
// snapshot(), which loads state first, never observes opFailed with a nil err.
func (o *fileOp) finish(err error) {
	switch {
	case err == nil:
		o.state.Store(int32(opDone))
	case errors.Is(err, context.Canceled):
		o.state.Store(int32(opCancelled))
	default:
		o.mu.Lock()
		o.err = err
		o.mu.Unlock()
		o.state.Store(int32(opFailed))
	}
}

// markCancelled is the Update-side counterpart to finish() for an operation
// that is cancelled before it ever starts running. It is safe precisely because
// no worker exists yet to race with.
func (o *fileOp) markCancelled() {
	o.cancel()
	o.state.Store(int32(opCancelled))
}
