// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package effectiveness

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/suykerbuyk/vibe-vault/internal/index"
)

// Cohort groups sessions by context depth.
type Cohort struct {
	Label          string  `json:"label"`
	Sessions       int     `json:"sessions"`
	AvgFriction    float64 `json:"avg_friction"`
	AvgCorrections float64 `json:"avg_corrections"`
	AvgDuration    float64 `json:"avg_duration_minutes"`
}

// ProjectReport holds analysis for one project.
type ProjectReport struct {
	Project       string   `json:"project"`
	TotalSessions int      `json:"total_sessions"`
	WithContext   int      `json:"with_context"`
	Cohorts       []Cohort `json:"cohorts"`
	Correlation   float64  `json:"correlation"`
	Confidence    string   `json:"confidence"`
	Summary       string   `json:"summary"`
}

// Result holds analysis across projects.
type Result struct {
	Projects []ProjectReport `json:"projects"`
}

// Analyze computes effectiveness from index entries.
// Filters by project if non-empty. Only considers entries with non-nil Context.
func Analyze(entries map[string]index.SessionEntry, project string) Result {
	// Group entries by project
	byProject := make(map[string][]index.SessionEntry)
	for _, e := range entries {
		if project != "" && e.Project != project {
			continue
		}
		byProject[e.Project] = append(byProject[e.Project], e)
	}

	var reports []ProjectReport
	for proj, projEntries := range byProject {
		reports = append(reports, analyzeProject(proj, projEntries))
	}

	sort.Slice(reports, func(i, j int) bool {
		return strings.ToLower(reports[i].Project) < strings.ToLower(reports[j].Project)
	})

	return Result{Projects: reports}
}

func analyzeProject(project string, entries []index.SessionEntry) ProjectReport {
	report := ProjectReport{
		Project:       project,
		TotalSessions: len(entries),
	}

	// Filter to entries with context data
	var withCtx []index.SessionEntry
	for _, e := range entries {
		if e.Context != nil {
			withCtx = append(withCtx, e)
		}
	}
	report.WithContext = len(withCtx)

	if len(withCtx) == 0 {
		report.Confidence = confidenceLevel(0)
		report.Summary = "No context data available. Run: vv reprocess --backfill-context"
		return report
	}

	// Assign to cohorts
	type cohortData struct {
		friction    []float64
		corrections []float64
		duration    []float64
	}
	cohorts := map[string]*cohortData{
		"none (0)":         {},
		"early (1-10)":     {},
		"building (11-30)": {},
		"mature (30+)":     {},
	}

	var xs, ys []float64 // for Pearson: HistorySessions vs FrictionScore

	for _, e := range withCtx {
		label := cohortLabel(e.Context.HistorySessions)
		cd := cohorts[label]
		if e.FrictionScore > 0 {
			cd.friction = append(cd.friction, float64(e.FrictionScore))
			xs = append(xs, float64(e.Context.HistorySessions))
			ys = append(ys, float64(e.FrictionScore))
		}
		if e.Corrections > 0 {
			cd.corrections = append(cd.corrections, float64(e.Corrections))
		}
		if e.Duration > 0 {
			cd.duration = append(cd.duration, float64(e.Duration))
		}
	}

	// Build cohort results in order
	labels := []string{"none (0)", "early (1-10)", "building (11-30)", "mature (30+)"}
	for _, label := range labels {
		cd := cohorts[label]
		sessions := max(len(cd.friction), max(len(cd.corrections), len(cd.duration)))
		if sessions == 0 {
			// Count entries in this cohort even without metrics
			for _, e := range withCtx {
				if cohortLabel(e.Context.HistorySessions) == label {
					sessions++
				}
			}
		}
		if sessions == 0 {
			continue
		}
		report.Cohorts = append(report.Cohorts, Cohort{
			Label:          label,
			Sessions:       sessions,
			AvgFriction:    avg(cd.friction),
			AvgCorrections: avg(cd.corrections),
			AvgDuration:    avg(cd.duration),
		})
	}

	report.Correlation = pearsonR(xs, ys)
	report.Confidence = confidenceLevel(len(withCtx))
	report.Summary = summarize(report)

	return report
}

func cohortLabel(historySessions int) string {
	switch {
	case historySessions == 0:
		return "none (0)"
	case historySessions <= 10:
		return "early (1-10)"
	case historySessions <= 30:
		return "building (11-30)"
	default:
		return "mature (30+)"
	}
}

func avg(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	sum := 0.0
	for _, x := range xs {
		sum += x
	}
	return sum / float64(len(xs))
}

func pearsonR(xs, ys []float64) float64 {
	n := len(xs)
	if n < 3 || len(ys) != n {
		return 0
	}

	mx := avg(xs)
	my := avg(ys)

	var num, dx2, dy2 float64
	for i := 0; i < n; i++ {
		dx := xs[i] - mx
		dy := ys[i] - my
		num += dx * dy
		dx2 += dx * dx
		dy2 += dy * dy
	}

	denom := math.Sqrt(dx2 * dy2)
	if denom == 0 {
		return 0
	}
	return num / denom
}

func confidenceLevel(n int) string {
	switch {
	case n < 20:
		return "insufficient"
	case n < 50:
		return "low"
	case n < 100:
		return "medium"
	default:
		return "high"
	}
}

func summarize(r ProjectReport) string {
	if r.WithContext == 0 {
		return "No context data available."
	}
	if r.Confidence == "insufficient" {
		return fmt.Sprintf("Only %d sessions with context data — need 20+ for meaningful analysis.", r.WithContext)
	}

	direction := "no clear"
	if r.Correlation < -0.1 {
		direction = "negative"
	} else if r.Correlation > 0.1 {
		direction = "positive"
	}

	switch direction {
	case "negative":
		return fmt.Sprintf("Context shows %s correlation (r=%.2f) with friction — more context tends to reduce friction.", direction, r.Correlation)
	case "positive":
		return fmt.Sprintf("Context shows %s correlation (r=%.2f) with friction — unexpected; may reflect project complexity growth.", direction, r.Correlation)
	default:
		return fmt.Sprintf("No clear correlation (r=%.2f) between context depth and friction.", r.Correlation)
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// Format renders a Result as human-readable CLI text.
func Format(r Result) string {
	if len(r.Projects) == 0 {
		return "No projects found.\n"
	}

	var sb strings.Builder
	sb.WriteString("Context Effectiveness Analysis\n")
	sb.WriteString(strings.Repeat("=", 50) + "\n\n")

	for _, p := range r.Projects {
		fmt.Fprintf(&sb, "Project: %s (%d sessions, %d with context data)\n", p.Project, p.TotalSessions, p.WithContext)
		fmt.Fprintf(&sb, "Confidence: %s\n", p.Confidence)

		if len(p.Cohorts) > 0 {
			fmt.Fprintf(&sb, "\n  %-18s %8s %10s %12s %10s\n", "Cohort", "Sessions", "Friction", "Corrections", "Duration")
			fmt.Fprintf(&sb, "  %-18s %8s %10s %12s %10s\n", strings.Repeat("-", 18), "--------", "----------", "------------", "----------")
			for _, c := range p.Cohorts {
				fmt.Fprintf(&sb, "  %-18s %8d %10.1f %12.1f %8.0f min\n", c.Label, c.Sessions, c.AvgFriction, c.AvgCorrections, c.AvgDuration)
			}
		}

		if p.Correlation != 0 {
			fmt.Fprintf(&sb, "\nCorrelation (HistorySessions vs Friction): r = %.3f\n", p.Correlation)
		}
		fmt.Fprintf(&sb, "Summary: %s\n\n", p.Summary)
	}

	return sb.String()
}
