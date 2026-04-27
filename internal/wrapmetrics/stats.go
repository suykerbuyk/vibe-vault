// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

// File stats.go aggregates the wrap-dispatch.jsonl + wrap-metrics.jsonl
// jsonl files into the human-readable report rendered by `vv stats wrap`.
// The aggregation lives in this package (rather than under internal/stats)
// to keep the schema definitions co-located with their writers and the
// stats package unaware of the per-jsonl shapes.

package wrapmetrics

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// WrapStats holds the aggregate output of `vv stats wrap`.
type WrapStats struct {
	TotalDispatches      int                      // total DispatchLine records considered
	IterationCount       int                      // unique iter values
	MedianDurationByTier map[string]TierDurStats  // tier -> duration stats
	EscalationRate       float64                  // 0..1, dispatches with outcome != "ok"
	EscalateCount        int
	TopEscalateReasons   []ReasonCount            // sorted, longest first
	DriftMedians         map[string]int           // field -> median drift_bytes
	DispatchLineCount    int                      // raw count of jsonl lines read
	DriftLineCount       int                      // raw count of drift jsonl lines read
}

// TierDurStats holds duration aggregates for a single tier.
type TierDurStats struct {
	Count          int
	MedianMs       int64
}

// ReasonCount pairs an escalate reason with its occurrence count.
type ReasonCount struct {
	Reason string
	Count  int
}

// ComputeWrapStats aggregates dispatch lines and drift lines into a
// WrapStats report. Both inputs may be nil/empty; the returned struct
// reports zeros where data is absent.
func ComputeWrapStats(dispatch []DispatchLine, drift []Line) WrapStats {
	out := WrapStats{
		MedianDurationByTier: map[string]TierDurStats{},
		DriftMedians:         map[string]int{},
		DispatchLineCount:    len(dispatch),
		DriftLineCount:       len(drift),
	}
	if len(dispatch) > 0 {
		out.TotalDispatches = len(dispatch)
		iterSeen := map[int]struct{}{}
		// Group durations by tier.
		durByTier := map[string][]int64{}
		reasonCounts := map[string]int{}
		var escalations int
		for _, d := range dispatch {
			iterSeen[d.Iter] = struct{}{}
			for _, att := range d.TierAttempts {
				durByTier[att.Tier] = append(durByTier[att.Tier], att.DurationMs)
				if att.Outcome != "ok" {
					escalations++
					reason := att.EscalateReason
					if reason == "" {
						reason = "(no reason)"
					}
					reasonCounts[reason]++
				}
			}
		}
		out.IterationCount = len(iterSeen)
		out.EscalateCount = escalations
		if out.TotalDispatches > 0 {
			out.EscalationRate = float64(escalations) / float64(out.TotalDispatches)
		}
		for tier, durs := range durByTier {
			out.MedianDurationByTier[tier] = TierDurStats{
				Count:    len(durs),
				MedianMs: medianInt64(durs),
			}
		}
		out.TopEscalateReasons = sortReasonCounts(reasonCounts)
	}

	if len(drift) > 0 {
		driftByField := map[string][]int{}
		for _, ln := range drift {
			driftByField[ln.Field] = append(driftByField[ln.Field], ln.DriftBytes)
		}
		for field, bytes := range driftByField {
			out.DriftMedians[field] = medianInt(bytes)
		}
	}

	return out
}

// medianInt64 returns the median of the slice. Empty slice returns 0.
func medianInt64(xs []int64) int64 {
	if len(xs) == 0 {
		return 0
	}
	sorted := append([]int64(nil), xs...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	mid := len(sorted) / 2
	if len(sorted)%2 == 1 {
		return sorted[mid]
	}
	return (sorted[mid-1] + sorted[mid]) / 2
}

func medianInt(xs []int) int {
	if len(xs) == 0 {
		return 0
	}
	sorted := append([]int(nil), xs...)
	sort.Ints(sorted)
	mid := len(sorted) / 2
	if len(sorted)%2 == 1 {
		return sorted[mid]
	}
	return (sorted[mid-1] + sorted[mid]) / 2
}

// sortReasonCounts converts a reason->count map into a slice sorted by
// count descending (ties broken alphabetically for determinism).
func sortReasonCounts(m map[string]int) []ReasonCount {
	out := make([]ReasonCount, 0, len(m))
	for r, c := range m {
		out = append(out, ReasonCount{Reason: r, Count: c})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Reason < out[j].Reason
	})
	return out
}

// FormatWrapStats renders a WrapStats as the human-readable text shown by
// `vv stats wrap`. The format is a stable contract for tests to assert
// against (key headlines + exact sentinel strings on edge cases).
func FormatWrapStats(s WrapStats) string {
	var b strings.Builder
	if s.DispatchLineCount == 0 && s.DriftLineCount == 0 {
		fmt.Fprintln(&b, "wrap dispatch stats: no data yet")
		return b.String()
	}
	if s.DispatchLineCount == 0 {
		fmt.Fprintln(&b, "wrap dispatch stats: no dispatch data yet")
	} else {
		fmt.Fprintf(&b, "wrap dispatch stats (%d dispatches across %d iterations):\n",
			s.TotalDispatches, s.IterationCount)
		fmt.Fprintln(&b, "  median duration per tier:")
		// Stable order: alphabetical by tier name.
		tiers := make([]string, 0, len(s.MedianDurationByTier))
		for tier := range s.MedianDurationByTier {
			tiers = append(tiers, tier)
		}
		sort.Strings(tiers)
		for _, tier := range tiers {
			d := s.MedianDurationByTier[tier]
			fmt.Fprintf(&b, "    %-8s %d ms  (n=%d)\n", tier+":", d.MedianMs, d.Count)
		}
		fmt.Fprintf(&b, "  escalation rate: %.1f%% (%d/%d dispatches escalated)\n",
			s.EscalationRate*100, s.EscalateCount, s.TotalDispatches)
		if len(s.TopEscalateReasons) > 0 {
			fmt.Fprintln(&b, "  top escalation reasons:")
			limit := len(s.TopEscalateReasons)
			if limit > 5 {
				limit = 5
			}
			for i := 0; i < limit; i++ {
				rc := s.TopEscalateReasons[i]
				fmt.Fprintf(&b, "    %d. %s (n=%d)\n", i+1, rc.Reason, rc.Count)
			}
		}
	}

	if s.DriftLineCount > 0 {
		fmt.Fprintln(&b, "")
		fmt.Fprintf(&b, "wrap drift trends (%d field-records):\n", s.DriftLineCount)
		fmt.Fprintln(&b, "  median drift_bytes per field:")
		fields := make([]string, 0, len(s.DriftMedians))
		for f := range s.DriftMedians {
			fields = append(fields, f)
		}
		sort.Strings(fields)
		for _, f := range fields {
			fmt.Fprintf(&b, "    %-30s %d bytes\n", f+":", s.DriftMedians[f])
		}
	}
	return b.String()
}

// ReadDriftLines is a small wrapper around the existing ReadActiveLines
// helper that decodes each jsonl line into a Line struct. Returns nil
// without error when the file is missing or empty.
func ReadDriftLines() ([]Line, error) {
	cacheDir, err := CacheDir()
	if err != nil {
		return nil, err
	}
	raw, err := ReadActiveLines(cacheDir)
	if err != nil {
		return nil, err
	}
	out := make([]Line, 0, len(raw))
	for _, r := range raw {
		var ln Line
		if jerr := json.Unmarshal([]byte(r), &ln); jerr != nil {
			continue // skip malformed lines (parity with dispatch reader)
		}
		out = append(out, ln)
	}
	return out, nil
}
