package friction

import (
	"fmt"
	"sort"
	"strings"

	"github.com/johns/vibe-vault/internal/index"
)

// ComputeProjectFriction computes aggregate friction metrics from index entries.
func ComputeProjectFriction(entries map[string]index.SessionEntry, project string) []ProjectFriction {
	projectMap := make(map[string]*ProjectFriction)

	for _, e := range entries {
		if project != "" && e.Project != project {
			continue
		}
		if e.FrictionScore == 0 && e.Corrections == 0 {
			continue
		}

		pf, ok := projectMap[e.Project]
		if !ok {
			pf = &ProjectFriction{Project: e.Project}
			projectMap[e.Project] = pf
		}
		pf.Sessions++
		pf.TotalCorrections += e.Corrections
		pf.AvgScore += float64(e.FrictionScore)
		if e.FrictionScore > pf.MaxScore {
			pf.MaxScore = e.FrictionScore
		}
		if e.FrictionScore >= 40 {
			pf.HighFriction++
		}
	}

	var results []ProjectFriction
	for _, pf := range projectMap {
		if pf.Sessions > 0 {
			pf.AvgScore = pf.AvgScore / float64(pf.Sessions)
		}
		results = append(results, *pf)
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].AvgScore != results[j].AvgScore {
			return results[i].AvgScore > results[j].AvgScore
		}
		return results[i].Project < results[j].Project
	})

	return results
}

// Format renders friction data for terminal output.
func Format(projects []ProjectFriction, totalEntries int, projectFilter string) string {
	var b strings.Builder

	if projectFilter != "" {
		b.WriteString(fmt.Sprintf("Friction Analysis: %s\n", projectFilter))
	} else {
		b.WriteString("Friction Analysis\n")
	}
	b.WriteString(strings.Repeat("=", 40) + "\n\n")

	if len(projects) == 0 {
		b.WriteString("No friction data available.\n")
		b.WriteString("Run `vv reprocess` to generate friction scores.\n")
		return b.String()
	}

	// Global summary
	var totalCorrections, totalHighFriction, totalSessions int
	var totalScore float64
	for _, pf := range projects {
		totalCorrections += pf.TotalCorrections
		totalHighFriction += pf.HighFriction
		totalSessions += pf.Sessions
		totalScore += pf.AvgScore * float64(pf.Sessions)
	}

	globalAvg := 0.0
	if totalSessions > 0 {
		globalAvg = totalScore / float64(totalSessions)
	}

	b.WriteString("Overview\n")
	b.WriteString(fmt.Sprintf("  Sessions with data   %d / %d\n", totalSessions, totalEntries))
	b.WriteString(fmt.Sprintf("  Avg friction score   %.0f / 100\n", globalAvg))
	b.WriteString(fmt.Sprintf("  Total corrections    %d\n", totalCorrections))
	b.WriteString(fmt.Sprintf("  High friction (≥40)  %d sessions\n\n", totalHighFriction))

	// Per-project breakdown
	if len(projects) > 1 || projectFilter == "" {
		b.WriteString("Projects\n")
		for _, pf := range projects {
			indicator := " "
			if pf.MaxScore >= 40 {
				indicator = "⚡"
			}
			b.WriteString(fmt.Sprintf("  %s %-20s avg:%3.0f  max:%3d  corrections:%d  (%d sessions)\n",
				indicator, pf.Project, pf.AvgScore, pf.MaxScore, pf.TotalCorrections, pf.Sessions))
		}
		b.WriteString("\n")
	}

	return b.String()
}
