package stats

import (
	"sort"
	"strings"

	"github.com/johns/vibe-vault/internal/index"
)

// Summary holds aggregate metrics computed from the session index.
type Summary struct {
	TotalSessions  int
	TotalTokensIn  int
	TotalTokensOut int
	TotalMessages  int
	TotalToolUses  int
	TotalDuration  int // minutes
	ActiveProjects int

	AvgTokensInPerMsg  float64
	AvgTokensOutPerMsg float64
	AvgToolsPerSession float64
	AvgDuration        float64 // minutes

	Projects []ProjectStats
	Models   []ModelStats
	Tags     []TagStats
	TopFiles []FileStats
	Monthly  []MonthStats
}

// ProjectStats holds per-project aggregate metrics.
type ProjectStats struct {
	Name     string
	Sessions int
	TokensIn int
	Duration int // minutes
}

// ModelStats holds per-model aggregate metrics.
type ModelStats struct {
	Name       string
	Sessions   int
	TokensIn   int
	Messages   int
	TokPerMsg  float64
}

// TagStats holds per-tag counts.
type TagStats struct {
	Name    string
	Count   int
	Percent float64
}

// FileStats holds per-file session counts.
type FileStats struct {
	Path     string
	Sessions int
}

// MonthStats holds per-month aggregate metrics.
type MonthStats struct {
	Month    string // YYYY-MM
	Sessions int
	TokensIn int
	TokensOut int
}

// Compute builds a Summary from index entries, optionally filtered by project.
func Compute(entries map[string]index.SessionEntry, project string) Summary {
	var s Summary

	projectMap := make(map[string]*ProjectStats)
	modelMap := make(map[string]*ModelStats)
	tagMap := make(map[string]int)
	fileMap := make(map[string]int)
	monthMap := make(map[string]*MonthStats)

	for _, e := range entries {
		if project != "" && e.Project != project {
			continue
		}

		s.TotalSessions++
		s.TotalTokensIn += e.TokensIn
		s.TotalTokensOut += e.TokensOut
		s.TotalMessages += e.Messages
		s.TotalToolUses += e.ToolUses
		s.TotalDuration += e.Duration

		// Project breakdown
		ps, ok := projectMap[e.Project]
		if !ok {
			ps = &ProjectStats{Name: e.Project}
			projectMap[e.Project] = ps
		}
		ps.Sessions++
		ps.TokensIn += e.TokensIn
		ps.Duration += e.Duration

		// Model breakdown
		model := e.Model
		if model == "" {
			model = "unknown"
		}
		ms, ok := modelMap[model]
		if !ok {
			ms = &ModelStats{Name: model}
			modelMap[model] = ms
		}
		ms.Sessions++
		ms.TokensIn += e.TokensIn
		ms.Messages += e.Messages

		// Tag breakdown
		if e.Tag != "" {
			tagMap[e.Tag]++
		}

		// File breakdown
		for _, f := range e.FilesChanged {
			fileMap[f]++
		}

		// Monthly breakdown
		if len(e.Date) >= 7 {
			month := e.Date[:7]
			mm, ok := monthMap[month]
			if !ok {
				mm = &MonthStats{Month: month}
				monthMap[month] = mm
			}
			mm.Sessions++
			mm.TokensIn += e.TokensIn
			mm.TokensOut += e.TokensOut
		}
	}

	s.ActiveProjects = len(projectMap)

	// Averages (guard division by zero)
	if s.TotalMessages > 0 {
		s.AvgTokensInPerMsg = float64(s.TotalTokensIn) / float64(s.TotalMessages)
		s.AvgTokensOutPerMsg = float64(s.TotalTokensOut) / float64(s.TotalMessages)
	}
	if s.TotalSessions > 0 {
		s.AvgToolsPerSession = float64(s.TotalToolUses) / float64(s.TotalSessions)
		s.AvgDuration = float64(s.TotalDuration) / float64(s.TotalSessions)
	}

	// Model tok/msg
	for _, ms := range modelMap {
		if ms.Messages > 0 {
			ms.TokPerMsg = float64(ms.TokensIn) / float64(ms.Messages)
		}
	}

	// Sort projects by sessions desc
	for _, ps := range projectMap {
		s.Projects = append(s.Projects, *ps)
	}
	sort.Slice(s.Projects, func(i, j int) bool {
		if s.Projects[i].Sessions != s.Projects[j].Sessions {
			return s.Projects[i].Sessions > s.Projects[j].Sessions
		}
		return strings.ToLower(s.Projects[i].Name) < strings.ToLower(s.Projects[j].Name)
	})

	// Sort models by sessions desc
	for _, ms := range modelMap {
		s.Models = append(s.Models, *ms)
	}
	sort.Slice(s.Models, func(i, j int) bool {
		if s.Models[i].Sessions != s.Models[j].Sessions {
			return s.Models[i].Sessions > s.Models[j].Sessions
		}
		return strings.ToLower(s.Models[i].Name) < strings.ToLower(s.Models[j].Name)
	})

	// Sort tags by count desc
	taggedSessions := 0
	for _, count := range tagMap {
		taggedSessions += count
	}
	for name, count := range tagMap {
		pct := 0.0
		if taggedSessions > 0 {
			pct = float64(count) / float64(taggedSessions) * 100
		}
		s.Tags = append(s.Tags, TagStats{Name: name, Count: count, Percent: pct})
	}
	sort.Slice(s.Tags, func(i, j int) bool {
		if s.Tags[i].Count != s.Tags[j].Count {
			return s.Tags[i].Count > s.Tags[j].Count
		}
		return strings.ToLower(s.Tags[i].Name) < strings.ToLower(s.Tags[j].Name)
	})

	// Sort files by sessions desc, apply threshold
	threshold := 10
	if project != "" {
		threshold = 3
	}
	for path, count := range fileMap {
		if count >= threshold {
			s.TopFiles = append(s.TopFiles, FileStats{Path: path, Sessions: count})
		}
	}
	sort.Slice(s.TopFiles, func(i, j int) bool {
		if s.TopFiles[i].Sessions != s.TopFiles[j].Sessions {
			return s.TopFiles[i].Sessions > s.TopFiles[j].Sessions
		}
		return s.TopFiles[i].Path < s.TopFiles[j].Path
	})

	// Sort months recent-first, cap at 6
	for _, mm := range monthMap {
		s.Monthly = append(s.Monthly, *mm)
	}
	sort.Slice(s.Monthly, func(i, j int) bool {
		return s.Monthly[i].Month > s.Monthly[j].Month
	})
	if len(s.Monthly) > 6 {
		s.Monthly = s.Monthly[:6]
	}

	return s
}
