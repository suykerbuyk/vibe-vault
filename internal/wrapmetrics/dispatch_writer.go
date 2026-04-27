// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

// File dispatch_writer.go is the host-local dispatch-metrics jsonl writer
// (Phase 4 of wrap-model-tiering). Each /wrap dispatch tier-attempt emits one
// DispatchLine to ~/.cache/vibe-vault/wrap-dispatch.jsonl, sibling to the
// existing wrap-metrics.jsonl drift log.
//
// Schema (Decision 14): per-dispatch shape carries iteration index,
// agent-definition fingerprint, the tier attempted, the resolved
// provider:model, duration, outcome (ok|escalate|error), token counts,
// and an optional escalate_reason. Each vv_wrap_dispatch invocation runs
// exactly ONE tier, so each call produces one line with len(TierAttempts)==1;
// `vv stats wrap` aggregates by `iter` to reconstruct the multi-tier picture.
//
// As with wrap-metrics.jsonl the file is host-local by design (no vault-side
// commit) — the multi-machine append-race that drove the original location
// choice applies here too.

package wrapmetrics

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// DispatchActiveFile is the filename of the active dispatch-metrics jsonl.
const DispatchActiveFile = "wrap-dispatch.jsonl"

// DispatchLine is one record per vv_wrap_dispatch invocation.
//
// Each TierAttempt slice has len 1 in v1 (one dispatch handler call drives
// one tier); the slice shape is preserved so future server-side multi-tier
// dispatch can reuse this writer without a schema break. EscalatedFrom and
// ModelUsed fields are populated by the orchestrator's natural-language
// ladder logic when it reconstructs the iteration's picture from the
// stream of single-tier lines.
type DispatchLine struct {
	Iter                   int           `json:"iter"`
	TS                     string        `json:"ts"` // RFC3339
	AgentDefinitionSha256  string        `json:"agent_definition_sha256"`
	AgentDefinitionVersion string        `json:"agent_definition_version"`
	TierAttempts           []TierAttempt `json:"tier_attempts"`
	ModelUsed              string        `json:"model_used"`     // tier name that succeeded (set on success line)
	EscalatedFrom          []string      `json:"escalated_from"` // tier names that failed before success
	TotalDurationMs        int64         `json:"total_duration_ms"`
}

// TierAttempt captures one tier's dispatch outcome.
type TierAttempt struct {
	Tier              string `json:"tier"`
	ProviderModel     string `json:"provider_model"`
	DurationMs        int64  `json:"duration_ms"`
	Outcome           string `json:"outcome"` // "ok" | "escalate" | "error"
	ExpectedMutations int    `json:"expected_mutations,omitempty"`
	ActualMutations   int    `json:"actual_mutations,omitempty"`
	InputTokens       int    `json:"input_tokens"`
	OutputTokens      int    `json:"output_tokens"`
	EscalateReason    string `json:"escalate_reason,omitempty"`
}

// dispatchWriteMu serialises concurrent writes from same-process goroutines.
// The OS-level append guarantee handles cross-process atomicity for short
// writes (<= PIPE_BUF, comfortably under 1 KiB lines), but the in-process
// mutex keeps line ordering predictable when multiple goroutines invoke
// WriteDispatchLine concurrently (the dispatch handler itself is currently
// single-flight per tool call, but tests exercise concurrent writes
// directly).
var dispatchWriteMu sync.Mutex

// DispatchPath returns the absolute path of the active dispatch-metrics
// file. Returns the same parent directory as the existing
// wrap-metrics.jsonl writer.
func DispatchPath() (string, error) {
	cacheDir, err := CacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cacheDir, DispatchActiveFile), nil
}

// WriteDispatchLine marshals line and appends it to the active dispatch
// file, creating parent directories as needed. The mutex + O_APPEND
// combination is concurrent-safe within a single process; cross-process
// atomicity relies on the OS append guarantee for sub-PIPE_BUF writes.
func WriteDispatchLine(line DispatchLine) error {
	dispatchWriteMu.Lock()
	defer dispatchWriteMu.Unlock()

	cacheDir, cdErr := CacheDir()
	if cdErr != nil {
		return cdErr
	}
	if mkdirErr := os.MkdirAll(cacheDir, 0o755); mkdirErr != nil {
		return fmt.Errorf("create cache dir %q: %w", cacheDir, mkdirErr)
	}

	path := filepath.Join(cacheDir, DispatchActiveFile)
	data, mErr := json.Marshal(line)
	if mErr != nil {
		return fmt.Errorf("marshal dispatch line: %w", mErr)
	}
	data = append(data, '\n')

	f, oErr := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if oErr != nil {
		return fmt.Errorf("open dispatch file %q: %w", path, oErr)
	}
	_, wErr := f.Write(data)
	cErr := f.Close()
	if wErr != nil {
		return fmt.Errorf("write dispatch line: %w", wErr)
	}
	if cErr != nil {
		return fmt.Errorf("close dispatch file: %w", cErr)
	}
	return nil
}

// ReadDispatchLines returns up to limit most-recent DispatchLine records
// from the active dispatch file. limit <= 0 returns all lines. Lines that
// fail to parse (e.g. a half-flushed final line interrupted by SIGKILL)
// are skipped silently — the caller should not crash on a single corrupt
// record. Returns nil and no error when the file does not yet exist.
func ReadDispatchLines(limit int) ([]DispatchLine, error) {
	path, pErr := DispatchPath()
	if pErr != nil {
		return nil, pErr
	}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("open dispatch file: %w", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	// Allow generous buffer for token counts + future fields; drift
	// metrics show 4-5 KiB lines is a comfortable upper bound today.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var all []DispatchLine
	for scanner.Scan() {
		raw := scanner.Bytes()
		if len(raw) == 0 {
			continue
		}
		var line DispatchLine
		if jerr := json.Unmarshal(raw, &line); jerr != nil {
			// Skip malformed lines (typically a torn final line from a
			// crashed prior run). Log-free per Decision 14: surface the
			// count via a future health endpoint if needed.
			continue
		}
		all = append(all, line)
	}
	if sErr := scanner.Err(); sErr != nil {
		return all, fmt.Errorf("scan dispatch file: %w", sErr)
	}

	if limit <= 0 || limit >= len(all) {
		return all, nil
	}
	return all[len(all)-limit:], nil
}
