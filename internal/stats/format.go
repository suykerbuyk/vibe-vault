package stats

import (
	"fmt"
	"strings"
)

// Format renders a Summary as aligned terminal output.
func Format(s Summary, project string) string {
	if s.TotalSessions == 0 {
		if project != "" {
			return fmt.Sprintf("vv stats --project %s\n\n  No sessions found for project %q.\n", project, project)
		}
		return "vv stats\n\n  No sessions found. Run `vv backfill` or `vv process` first.\n"
	}

	var b strings.Builder

	if project != "" {
		fmt.Fprintf(&b, "vv stats --project %s\n", project)
	} else {
		b.WriteString("vv stats\n")
	}

	// Overview
	b.WriteString("\nOverview\n")
	fmt.Fprintf(&b, "  %-20s %d\n", "sessions", s.TotalSessions)
	if project == "" {
		fmt.Fprintf(&b, "  %-20s %d\n", "projects", s.ActiveProjects)
	}
	fmt.Fprintf(&b, "  %-20s %d\n", "models", len(s.Models))
	fmt.Fprintf(&b, "  %-20s %s\n", "total duration", formatDuration(s.TotalDuration))
	fmt.Fprintf(&b, "  %-20s %s in / %s out\n", "total tokens", formatTokens(s.TotalTokensIn), formatTokens(s.TotalTokensOut))

	// Averages
	b.WriteString("\nAverages\n")
	fmt.Fprintf(&b, "  %-20s %s in / %s out\n", "tokens/message", formatFloat(s.AvgTokensInPerMsg), formatFloat(s.AvgTokensOutPerMsg))
	fmt.Fprintf(&b, "  %-20s %.1f\n", "tools/session", s.AvgToolsPerSession)
	fmt.Fprintf(&b, "  %-20s %s\n", "duration", formatDuration(int(s.AvgDuration)))

	// Projects (omit when filtered by project)
	if project == "" && len(s.Projects) > 0 {
		b.WriteString("\nProjects\n")
		limit := 5
		if len(s.Projects) < limit {
			limit = len(s.Projects)
		}
		for _, p := range s.Projects[:limit] {
			fmt.Fprintf(&b, "  %-24s %3d sessions   %6s in   %s\n",
				p.Name, p.Sessions, formatTokens(p.TokensIn), formatDuration(p.Duration))
		}
		if len(s.Projects) > 5 {
			fmt.Fprintf(&b, "  ... and %d more\n", len(s.Projects)-5)
		}
	}

	// Models
	if len(s.Models) > 0 {
		b.WriteString("\nModels\n")
		for _, m := range s.Models {
			tokPerMsg := "-"
			if m.TokPerMsg > 0 {
				tokPerMsg = formatFloat(m.TokPerMsg) + " tok/msg"
			}
			fmt.Fprintf(&b, "  %-24s %3d sessions   %s\n", m.Name, m.Sessions, tokPerMsg)
		}
	}

	// Tags
	if len(s.Tags) > 0 {
		b.WriteString("\nActivity Tags\n")
		for _, t := range s.Tags {
			fmt.Fprintf(&b, "  %-24s %3d (%d%%)\n", t.Name, t.Count, int(t.Percent))
		}
	}

	// Monthly Trend
	if len(s.Monthly) > 0 {
		b.WriteString("\nMonthly Trend\n")
		for _, m := range s.Monthly {
			fmt.Fprintf(&b, "  %-12s %3d sessions   %6s in / %6s out\n",
				m.Month, m.Sessions, formatTokens(m.TokensIn), formatTokens(m.TokensOut))
		}
	}

	// Top Files
	if len(s.TopFiles) > 0 {
		b.WriteString("\nTop Files\n")
		limit := 10
		if len(s.TopFiles) < limit {
			limit = len(s.TopFiles)
		}
		for _, f := range s.TopFiles[:limit] {
			fmt.Fprintf(&b, "  %-48s %3d sessions\n", f.Path, f.Sessions)
		}
	}

	return b.String()
}

// formatTokens formats a token count for display.
// <10K: plain with commas, >=10K: X.XK, >=1M: X.XM
func formatTokens(n int) string {
	if n < 0 {
		return "0"
	}
	if n >= 1_000_000 {
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	}
	if n >= 10_000 {
		return fmt.Sprintf("%.1fK", float64(n)/1_000)
	}
	return formatInt(n)
}

// formatFloat formats a float for display with commas.
func formatFloat(f float64) string {
	return formatInt(int(f + 0.5))
}

// formatInt formats an integer with comma separators.
func formatInt(n int) string {
	if n < 0 {
		return "0"
	}
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	var result []byte
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			result = append(result, ',')
		}
		result = append(result, byte(c))
	}
	return string(result)
}

// formatDuration formats minutes as "Xh Ym".
func formatDuration(minutes int) string {
	if minutes <= 0 {
		return "0m"
	}
	h := minutes / 60
	m := minutes % 60
	if h == 0 {
		return fmt.Sprintf("%dm", m)
	}
	if m == 0 {
		return fmt.Sprintf("%dh", h)
	}
	return fmt.Sprintf("%dh %dm", h, m)
}
