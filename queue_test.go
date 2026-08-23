package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
)

func writeFile(t *testing.T, path string, size int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, size)
	for i := range buf {
		buf[i] = byte(i % 251)
	}
	if err := os.WriteFile(path, buf, 0644); err != nil {
		t.Fatal(err)
	}
}

func mkFile(t *testing.T, path string) file {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return file{Name: info.Name(), Path: path, Size: info.Size(), IsDir: info.IsDir(), Mode: info.Mode()}
}

func TestScanSources(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "tree", "a.bin"), 1000)
	writeFile(t, filepath.Join(root, "tree", "sub", "b.bin"), 2000)
	writeFile(t, filepath.Join(root, "loose.bin"), 500)

	bytes, files, err := scanSources(context.Background(), []file{
		mkFile(t, filepath.Join(root, "tree")),
		mkFile(t, filepath.Join(root, "loose.bin")),
	})
	if err != nil {
		t.Fatal(err)
	}
	if bytes != 3500 {
		t.Errorf("bytes = %d, want 3500", bytes)
	}
	if files != 3 {
		t.Errorf("files = %d, want 3", files)
	}
}

func TestCopyOpEndToEnd(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	writeFile(t, filepath.Join(src, "tree", "a.bin"), 300_000)
	writeFile(t, filepath.Join(src, "tree", "sub", "b.bin"), 120_000)

	op := newFileOp(1, opCopy, []file{mkFile(t, filepath.Join(src, "tree"))}, dst)
	op.run()

	if got := opState(op.state.Load()); got != opDone {
		t.Fatalf("state = %v, err = %v", got, op.snapshot().err)
	}
	if op.doneBytes.Load() != op.totalBytes.Load() {
		t.Errorf("doneBytes %d != totalBytes %d", op.doneBytes.Load(), op.totalBytes.Load())
	}
	if op.doneFiles.Load() != op.totalFiles.Load() || op.totalFiles.Load() != 2 {
		t.Errorf("files %d/%d, want 2/2", op.doneFiles.Load(), op.totalFiles.Load())
	}
	if got := op.snapshot().percent(); got != 1 {
		t.Errorf("percent = %v, want 1", got)
	}
	// The tree actually landed.
	if _, err := os.Stat(filepath.Join(dst, "tree", "sub", "b.bin")); err != nil {
		t.Error(err)
	}
}

func TestMoveOpSameFilesystem(t *testing.T) {
	root := t.TempDir()
	src, dst := filepath.Join(root, "src"), filepath.Join(root, "dst")
	writeFile(t, filepath.Join(src, "a.bin"), 5000)
	if err := os.MkdirAll(dst, 0755); err != nil {
		t.Fatal(err)
	}

	op := newFileOp(1, opMove, []file{mkFile(t, filepath.Join(src, "a.bin"))}, dst)
	op.run()

	if got := opState(op.state.Load()); got != opDone {
		t.Fatalf("state = %v, err = %v", got, op.snapshot().err)
	}
	if _, err := os.Stat(filepath.Join(src, "a.bin")); !os.IsNotExist(err) {
		t.Error("source still present after move")
	}
	if _, err := os.Stat(filepath.Join(dst, "a.bin")); err != nil {
		t.Error(err)
	}
	if got := op.snapshot().percent(); got != 1 {
		t.Errorf("percent = %v, want 1 (file-count fallback)", got)
	}
}

func TestDeleteOpRemovesTree(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "tree", "a.bin"), 10)
	writeFile(t, filepath.Join(root, "tree", "sub", "b.bin"), 10)

	op := newFileOp(1, opDelete, []file{mkFile(t, filepath.Join(root, "tree"))}, "")
	op.run()

	if got := opState(op.state.Load()); got != opDone {
		t.Fatalf("state = %v, err = %v", got, op.snapshot().err)
	}
	if _, err := os.Stat(filepath.Join(root, "tree")); !os.IsNotExist(err) {
		t.Error("tree still present after delete")
	}
	if op.doneFiles.Load() != 2 || op.totalFiles.Load() != 2 {
		t.Errorf("files %d/%d, want 2/2", op.doneFiles.Load(), op.totalFiles.Load())
	}
	if op.totalBytes.Load() != 0 {
		t.Errorf("delete should not report bytes, got %d", op.totalBytes.Load())
	}
}

func TestCancelMidCopyLeavesSourceIntact(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	// Big enough that the copy loop gets several chunks in.
	writeFile(t, filepath.Join(src, "big.bin"), 40*1024*1024)

	op := newFileOp(1, opCopy, []file{mkFile(t, filepath.Join(src, "big.bin"))}, dst)
	go func() {
		time.Sleep(5 * time.Millisecond)
		op.cancel()
	}()
	op.run()

	if got := opState(op.state.Load()); got != opCancelled {
		t.Fatalf("state = %v, want cancelled (err = %v)", got, op.snapshot().err)
	}
	if _, err := os.Stat(filepath.Join(src, "big.bin")); err != nil {
		t.Error("source damaged by cancelled copy:", err)
	}
}

func TestFailedOpPublishesErrorBeforeState(t *testing.T) {
	dst := t.TempDir()
	missing := file{Name: "nope.bin", Path: filepath.Join(dst, "does-not-exist"), Size: 1}

	op := newFileOp(1, opCopy, []file{missing}, dst)
	op.run()

	snap := op.snapshot()
	if snap.state != opFailed {
		t.Fatalf("state = %v, want failed", snap.state)
	}
	if snap.err == nil {
		t.Error("failed op exposed a nil error")
	}
}

func TestFormatHelpers(t *testing.T) {
	cases := []struct{ got, want string }{
		{formatBytes(512), "512 B"},
		{formatBytes(2048), "2.0 KB"},
		{formatBytes(5 * 1024 * 1024), "5.0 MB"},
		{formatRate(0), "--"},
		{formatDuration(9 * time.Second), "0:09"},
		{formatDuration(125 * time.Second), "2:05"},
		{formatDuration(3725 * time.Second), "1:02:05"},
		{truncate("hello", 10), "hello"},
		{truncate("hello world", 8), "hello w…"},
		{truncate("hello", 0), ""},
		{truncate("hello", -3), ""},
	}
	for i, c := range cases {
		if c.got != c.want {
			t.Errorf("case %d: got %q, want %q", i, c.got, c.want)
		}
	}
	if clampUnit(2) != 1 || clampUnit(-1) != 0 {
		t.Error("clampUnit failed to clamp")
	}
}

// TestQueueDispatchIsSerial covers the reason startedAt exists: pumpQueue must
// not hand the same operation to Bubble Tea twice before its worker has had a
// chance to publish a running state.
func TestQueueDispatchIsSerial(t *testing.T) {
	var m model
	src, dst := t.TempDir(), t.TempDir()
	writeFile(t, filepath.Join(src, "a.bin"), 100)
	f := mkFile(t, filepath.Join(src, "a.bin"))

	if cmd := m.enqueueOp(opCopy, []file{f}, dst); cmd == nil {
		t.Fatal("first enqueue returned no command")
	}
	first := m.runningOp()
	if first == nil {
		t.Fatal("nothing marked running after enqueue")
	}

	// Second enqueue while the first has not reported anything yet.
	m.enqueueOp(opCopy, []file{f}, dst)
	if got := m.runningOp(); got != first {
		t.Error("second enqueue displaced the running operation")
	}
	if m.nextQueuedOp() == nil {
		t.Error("second operation was not left queued")
	}
	if len(m.queue) != 2 {
		t.Fatalf("queue length = %d, want 2", len(m.queue))
	}

	// Cancelling the un-dispatched entry must terminate it without a worker.
	pending := m.nextQueuedOp()
	m.cancelOp(pending)
	if got := opState(pending.state.Load()); got != opCancelled {
		t.Errorf("pending op state = %v, want cancelled", got)
	}
	if m.nextQueuedOp() != nil {
		t.Error("cancelled op is still offered for dispatch")
	}
}

// demoModel builds a laid-out model without going through Bubble Tea, so the
// view helpers can be measured directly.
func demoModel(width int) model {
	km := DefaultKeyMap()
	return model{
		width:     width,
		height:    30,
		keyMap:    km,
		aliasMap:  km.GetAliasMap(),
		leftPane:  pane{id: 0, width: width/2 - 2, height: 24, active: true, selected: map[string]struct{}{}},
		rightPane: pane{id: 1, width: width/2 - 2, height: 24, selected: map[string]struct{}{}},
	}
}

func runningDemoOp() *fileOp {
	op := newFileOp(1, opCopy, []file{{Name: "ubuntu-24.04-desktop-amd64.iso"}}, "/media/usb")
	op.state.Store(int32(opRunning))
	op.totalBytes.Store(2 * 1024 * 1024 * 1024)
	op.doneBytes.Store(1300 * 1024 * 1024)
	op.totalFiles.Store(1)
	op.currentName = "ubuntu-24.04-desktop-amd64.iso"
	op.speed = 88 * 1024 * 1024
	op.startedAt = time.Now()
	return op
}

// TestBottomRowFitsTerminal is the guard on the one piece of this feature that
// has no other safety net: the progress widget shares the bottom row with the
// hints bar, and overflowing it would wrap the row and shove the panes around.
func TestBottomRowFitsTerminal(t *testing.T) {
	for _, width := range []int{120, 90, 60, 40} {
		m := demoModel(width)
		m.queue = []*fileOp{runningDemoOp()}

		row := m.bottomRowView()
		if got := lipgloss.Width(row); got > width {
			t.Errorf("width %d: bottom row overflows to %d", width, got)
		}
		if got := lipgloss.Height(row); got != 3 {
			t.Errorf("width %d: bottom row is %d lines, want 3", width, got)
		}
	}
}

// TestQueuePanelFitsPane covers the same hazard for the overlay: lipgloss.Place
// does not clip, so an oversized panel would spill onto the other pane.
func TestQueuePanelFitsPane(t *testing.T) {
	failed := newFileOp(3, opCopy, []file{{Name: "report.pdf"}}, "/media/usb")
	failed.state.Store(int32(opFailed))
	failed.err = fmt.Errorf("copy report.pdf: no space left on device")
	failed.finishedAt = time.Now()

	for _, width := range []int{120, 90, 64} {
		m := demoModel(width)
		m.queue = []*fileOp{runningDemoOp(), failed, newFileOp(5, opCopy, []file{{Name: "photos"}}, "/media/usb")}

		if got := lipgloss.Width(m.queueView()); got > m.leftPane.width {
			t.Errorf("width %d: panel is %d wide, pane is %d", width, got, m.leftPane.width)
		}
	}
}

// TestWidgetPrefersFailureOverQueued keeps a failure on screen even when a
// newer entry is sitting behind it.
func TestWidgetPrefersFailureOverQueued(t *testing.T) {
	m := demoModel(120)

	failed := newFileOp(1, opCopy, []file{{Name: "report.pdf"}}, "/media/usb")
	failed.state.Store(int32(opFailed))
	failed.err = fmt.Errorf("no space left on device")
	failed.finishedAt = time.Now()

	queued := newFileOp(2, opCopy, []file{{Name: "photos"}}, "/media/usb")
	m.queue = []*fileOp{failed, queued}

	if got := m.widgetOp(); got != failed {
		t.Errorf("widgetOp picked op %d, want the failed one", got.id)
	}
}

func TestPruneQueueKeepsFailures(t *testing.T) {
	var m model
	old := time.Now().Add(-time.Hour)

	done := newFileOp(1, opCopy, []file{{Name: "a"}}, "")
	done.state.Store(int32(opDone))
	done.finishedAt = old

	failed := newFileOp(2, opCopy, []file{{Name: "b"}}, "")
	failed.state.Store(int32(opFailed))
	failed.finishedAt = old

	m.queue = []*fileOp{done, failed}
	m.pruneQueue(time.Now())

	if len(m.queue) != 1 || m.queue[0].id != 2 {
		t.Fatalf("queue = %v, want only the failed op", m.queue)
	}
	if m.queueNeedsTick(time.Now()) {
		t.Error("a lone failed op should not keep the tick running")
	}
}
