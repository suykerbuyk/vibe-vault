package knowledge

import (
	"github.com/johns/vibe-vault/internal/friction"
	"github.com/johns/vibe-vault/internal/prose"
)

// PairCorrections matches friction Correction entries (which have TurnIndex into
// the prose Dialogue) with the assistant response that followed, producing
// structured CorrectionPairs for knowledge extraction.
func PairCorrections(fr *friction.Result, d *prose.Dialogue) []CorrectionPair {
	if fr == nil || d == nil || len(fr.Corrections) == 0 {
		return nil
	}

	// Flatten all user and assistant turns from the dialogue with their indices.
	// TurnIndex in friction counts user turns sequentially across all sections.
	type turnInfo struct {
		role string
		text string
	}
	var turns []turnInfo

	for _, sec := range d.Sections {
		for _, elem := range sec.Elements {
			if elem.Turn != nil {
				turns = append(turns, turnInfo{role: elem.Turn.Role, text: elem.Turn.Text})
			} else if elem.Marker != nil {
				turns = append(turns, turnInfo{role: "marker", text: elem.Marker.Text})
			}
		}
	}

	// Build a map of user turn index → position in the flat turns slice.
	// Friction's TurnIndex counts user turns only (matching detect.go's counter).
	userTurnIdx := 0
	userTurnPositions := make(map[int]int) // friction TurnIndex → flat position
	for i, t := range turns {
		if t.role == "user" {
			userTurnPositions[userTurnIdx] = i
			userTurnIdx++
		}
	}

	var pairs []CorrectionPair
	for _, corr := range fr.Corrections {
		pos, ok := userTurnPositions[corr.TurnIndex]
		if !ok {
			continue
		}

		// Gather the resolution: collect assistant text and markers that follow
		// until the next user turn or end of turns.
		var resolution string
		for j := pos + 1; j < len(turns); j++ {
			if turns[j].role == "user" {
				break
			}
			if turns[j].text != "" {
				if resolution != "" {
					resolution += "\n"
				}
				resolution += turns[j].text
			}
		}

		pairs = append(pairs, CorrectionPair{
			UserText:   corr.Text,
			Resolution: truncate(resolution, 500),
			Pattern:    corr.Pattern,
		})
	}

	return pairs
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
