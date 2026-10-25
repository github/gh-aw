//go:build !integration

package cli

import (
	"context"
	"slices"
	"testing"

	"github.com/fsnotify/fsnotify"
)

func TestCompileWatchDebouncerTakeModifiedFiles(t *testing.T) {
	d := &compileWatchDebouncer{modifiedFiles: map[string]struct{}{
		"one.md": {}, "two.md": {},
	}}
	files := d.takeModifiedFiles()
	slices.Sort(files)
	if !slices.Equal(files, []string{"one.md", "two.md"}) {
		t.Fatalf("unexpected modified files: %v", files)
	}
	if d.modifiedFiles == nil || len(d.modifiedFiles) != 0 {
		t.Fatalf("expected a fresh empty modified-file map: %v", d.modifiedFiles)
	}
	d.modifiedFiles["next.md"] = struct{}{}
	if next := d.takeModifiedFiles(); !slices.Equal(next, []string{"next.md"}) {
		t.Fatalf("unexpected next batch: %v", next)
	}
	if empty := d.takeModifiedFiles(); empty == nil || len(empty) != 0 {
		t.Fatalf("expected non-nil empty batch: %v", empty)
	}
}

func TestCompileWatchIgnoresUnrelatedEvents(t *testing.T) {
	d := &compileWatchDebouncer{modifiedFiles: make(map[string]struct{})}
	for _, event := range []fsnotify.Event{
		{Name: "selected.md", Op: fsnotify.Chmod},
		{Name: "selected.lock.yml", Op: fsnotify.Write},
		{Name: "other.md", Op: fsnotify.Write},
	} {
		handleCompileWatchEvent(context.Background(), event, nil, nil, d, "selected.md", false)
	}
	if d.timer != nil || len(d.modifiedFiles) != 0 {
		t.Fatal("ignored events must not schedule compilation")
	}
}

func TestCompileWatchLoopTermination(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	d := &compileWatchDebouncer{}
	if err := runCompileWatchLoop(ctx, &fsnotify.Watcher{}, nil, nil, d, "", false); err != nil {
		t.Fatalf("cancelled watch loop: %v", err)
	}
	events := make(chan fsnotify.Event)
	close(events)
	watcher := &fsnotify.Watcher{Events: events}
	err := runCompileWatchLoop(context.Background(), watcher, nil, nil, d, "", false)
	if err == nil || err.Error() != "watcher channel closed" {
		t.Fatalf("unexpected closed-channel error: %v", err)
	}
}
