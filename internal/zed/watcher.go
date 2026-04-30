// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package zed

import (
	"context"
	"log"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

// WatcherConfig configures the Zed threads database watcher.
type WatcherConfig struct {
	DBPath   string        // path to threads.db
	Debounce time.Duration // quiet period before firing (default 5m)
	Logger   *log.Logger
	Clock    Clock
}

// Watch monitors the Zed threads database directory for WAL writes.
// It calls onChange when no writes occur for the debounce period.
// Blocks until ctx is cancelled.
func Watch(ctx context.Context, cfg WatcherConfig, onChange func()) error {
	if cfg.Debounce == 0 {
		cfg.Debounce = 5 * time.Minute
	}
	if cfg.Logger == nil {
		cfg.Logger = log.Default()
	}
	if cfg.Clock == nil {
		cfg.Clock = realClock{}
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer watcher.Close()

	dir := filepath.Dir(cfg.DBPath)
	if err := watcher.Add(dir); err != nil {
		return err
	}

	walBase := filepath.Base(cfg.DBPath) + "-wal"

	var timer Stoppable
	defer func() {
		// interface-nil: timer is nil iff zero interface (no AfterFunc call yet)
		if timer != nil {
			timer.Stop()
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case event, ok := <-watcher.Events:
			if !ok {
				return nil
			}
			if !event.Has(fsnotify.Write) {
				continue
			}
			if !strings.HasSuffix(event.Name, walBase) {
				continue
			}

			// Reset debounce timer
			if timer != nil {
				timer.Stop()
			}
			timer = cfg.Clock.AfterFunc(cfg.Debounce, onChange)

		case err, ok := <-watcher.Errors:
			if !ok {
				return nil
			}
			cfg.Logger.Printf("watcher error: %v", err)
		}
	}
}
