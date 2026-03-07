package watcher

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Source describes a set of directories to watch for a specific launcher.
// Adding support for a new launcher means adding a new Source.
type Source struct {
	Name   string
	Dirs   []string
	Filter func(filename string) bool // return true if this file should trigger a sync
}

// Run watches all sources and calls onChange (debounced by the given duration)
// whenever a matching file event occurs. Blocks until a fatal watcher error.
func Run(sources []Source, debounce time.Duration, onChange func()) error {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("creating watcher: %w", err)
	}
	defer w.Close()

	dirSource := make(map[string]string)
	for _, src := range sources {
		for _, dir := range src.Dirs {
			if err := w.Add(dir); err != nil {
				fmt.Printf("Warning: could not watch %s (%s): %v\n", dir, src.Name, err)
				continue
			}
			dirSource[dir] = src.Name
			fmt.Printf("Watching %s: %s\n", src.Name, dir)
		}
	}

	matchesAny := func(eventPath string) bool {
		dir := filepath.Dir(eventPath)
		name := filepath.Base(eventPath)
		for _, src := range sources {
			for _, d := range src.Dirs {
				if d == dir && src.Filter(name) {
					return true
				}
			}
		}
		return false
	}

	timer := time.NewTimer(debounce)
	timer.Stop()

	for {
		select {
		case event, ok := <-w.Events:
			if !ok {
				return fmt.Errorf("watcher channel closed")
			}
			if matchesAny(event.Name) {
				// Reset debounce window — coalesce rapid changes (e.g. Steam
				// writing multiple ACF files during a single install).
				timer.Reset(debounce)
			}
		case err, ok := <-w.Errors:
			if !ok {
				return fmt.Errorf("watcher error channel closed")
			}
			fmt.Printf("Watcher error: %v\n", err)
		case <-timer.C:
			onChange()
		}
	}
}
