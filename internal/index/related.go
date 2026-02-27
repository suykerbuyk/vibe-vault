package index

import (
	"sort"
	"strings"
)

// RelatedSession holds a related session and its relevance score.
type RelatedSession struct {
	Entry SessionEntry
	Score int
}

// RelatedSessions finds sessions related to the candidate entry within the same project.
// It excludes self (by session ID) and the previous session (by note path).
// Returns at most 3 results with a minimum score of 5.
func (idx *Index) RelatedSessions(candidate SessionEntry, previousNotePath string) []RelatedSession {
	var results []RelatedSession

	for _, entry := range idx.Entries {
		// Same project only
		if entry.Project != candidate.Project {
			continue
		}
		// Exclude self
		if entry.SessionID == candidate.SessionID {
			continue
		}
		// Exclude previous session
		if previousNotePath != "" && entry.NotePath == previousNotePath {
			continue
		}

		score := computeScore(candidate, entry)
		if score >= 5 {
			results = append(results, RelatedSession{Entry: entry, Score: score})
		}
	}

	// Sort by score descending, then by date descending for ties
	sort.Slice(results, func(i, j int) bool {
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		return results[i].Entry.Date > results[j].Entry.Date
	})

	// Cap at 3
	if len(results) > 3 {
		results = results[:3]
	}

	return results
}

func computeScore(candidate, other SessionEntry) int {
	score := 0

	// Shared files changed: 3 per file, capped at 15
	fileScore := len(setIntersection(candidate.FilesChanged, other.FilesChanged)) * 3
	if fileScore > 15 {
		fileScore = 15
	}
	score += fileScore

	// Thread → resolution: open threads in one matching decisions in other
	score += threadMatchScore(candidate.OpenThreads, other.Decisions) * 10
	score += threadMatchScore(other.OpenThreads, candidate.Decisions) * 10

	// Same branch: 5 points (excludes main/master)
	if candidate.Branch != "" && candidate.Branch == other.Branch {
		if candidate.Branch != "main" && candidate.Branch != "master" {
			score += 5
		}
	}

	// Same tag: 2 points
	if candidate.Tag != "" && candidate.Tag == other.Tag {
		score += 2
	}

	return score
}

// threadMatchScore counts how many threads have significant word overlap with decisions.
func threadMatchScore(threads, decisions []string) int {
	matches := 0
	for _, thread := range threads {
		threadWords := significantWords(thread)
		if len(threadWords) == 0 {
			continue
		}
		for _, decision := range decisions {
			decisionWords := significantWords(decision)
			overlap := len(setIntersection(threadWords, decisionWords))
			if overlap >= 2 {
				matches++
				break // count each thread at most once
			}
		}
	}
	return matches
}

// significantWords extracts words >= 4 chars, lowercased, skipping stop words.
func significantWords(s string) []string {
	words := strings.Fields(strings.ToLower(s))
	var result []string
	for _, w := range words {
		// Strip punctuation from edges
		w = strings.Trim(w, ".,;:!?\"'`()[]{}—-")
		if len(w) >= 4 && !isStopWord(w) {
			result = append(result, w)
		}
	}
	return result
}

var stopWords = map[string]bool{
	"that": true, "this": true, "with": true, "from": true,
	"have": true, "been": true, "were": true, "will": true,
	"would": true, "could": true, "should": true, "what": true,
	"when": true, "where": true, "which": true, "their": true,
	"there": true, "these": true, "those": true, "them": true,
	"then": true, "than": true, "some": true, "also": true,
	"into": true, "each": true, "make": true, "like": true,
	"just": true, "over": true, "such": true, "only": true,
	"very": true, "more": true, "most": true, "other": true,
	"about": true, "after": true, "before": true, "being": true,
	"between": true, "does": true, "doing": true, "done": true,
}

func isStopWord(w string) bool {
	return stopWords[w]
}

// setIntersection returns elements present in both slices.
func setIntersection(a, b []string) []string {
	set := make(map[string]bool, len(a))
	for _, s := range a {
		set[s] = true
	}
	var result []string
	for _, s := range b {
		if set[s] {
			result = append(result, s)
			delete(set, s) // avoid duplicates
		}
	}
	return result
}
