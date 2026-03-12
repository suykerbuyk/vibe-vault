// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package mcp

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/johns/vibe-vault/internal/config"
	"github.com/johns/vibe-vault/internal/zed"
)

// AutoCaptureConfig configures the background auto-capture goroutine.
type AutoCaptureConfig struct {
	DBPath   string
	Debounce time.Duration
	Logger   *log.Logger
	Cfg      config.Config
}

// StartAutoCapture launches a background goroutine that watches the Zed
// threads database and auto-captures new threads. Returns a channel that
// receives the watcher error when it exits. If DBPath is empty, returns
// a closed channel (graceful no-op).
func StartAutoCapture(ctx context.Context, acCfg AutoCaptureConfig) <-chan error {
	errCh := make(chan error, 1)

	dbPath := acCfg.DBPath
	if dbPath == "" {
		dbPath = zed.DefaultDBPath()
	}
	if dbPath == "" {
		if acCfg.Logger != nil {
			acCfg.Logger.Printf("auto-capture: no Zed DB path found, disabling")
		}
		close(errCh)
		return errCh
	}

	// Validate that the DB directory exists (watcher needs it)
	if _, err := os.Stat(filepath.Dir(dbPath)); err != nil {
		if acCfg.Logger != nil {
			acCfg.Logger.Printf("auto-capture: DB directory not found (%s), disabling", filepath.Dir(dbPath))
		}
		close(errCh)
		return errCh
	}

	debounce := acCfg.Debounce
	if debounce == 0 {
		debounce = 5 * time.Minute
	}

	logger := acCfg.Logger
	if logger == nil {
		logger = log.Default()
	}

	var mu sync.Mutex

	go func() {
		defer close(errCh)

		logger.Printf("auto-capture: watching %s (debounce %s)", dbPath, debounce)

		err := zed.Watch(ctx, zed.WatcherConfig{
			DBPath:   dbPath,
			Debounce: debounce,
			Logger:   logger,
		}, func() {
			mu.Lock()
			defer mu.Unlock()

			window := debounce + time.Minute
			since := time.Now().Add(-window)

			threads, err := zed.ParseDB(dbPath, zed.ParseOpts{Since: since})
			if err != nil {
				logger.Printf("auto-capture: error reading threads: %v", err)
				return
			}

			if len(threads) == 0 {
				return
			}

			result := zed.BatchCapture(zed.BatchCaptureOpts{
				Threads:      threads,
				DBPath:       dbPath,
				AutoCaptured: true,
				Cfg:          acCfg.Cfg,
				Logger:       logger,
			})

			if result.Processed > 0 || result.Errors > 0 {
				logger.Printf("auto-capture: captured=%d skipped=%d errors=%d",
					result.Processed, result.Skipped, result.Errors)
			}
		})

		if err != nil {
			errCh <- err
		}
	}()

	return errCh
}
