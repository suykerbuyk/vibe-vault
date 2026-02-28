package trends

import (
	"fmt"
	"strings"
)

// Format renders a Result as aligned terminal output.
func Format(r Result) string {
	if r.TotalSessions == 0 {
		if r.Project != "" {
			return fmt.Sprintf("vv trends --project %s\n\n  No sessions found for project %q.\n", r.Project, r.Project)
		}
		return "vv trends\n\n  No sessions found. Run `vv backfill` or `vv process` first.\n"
	}

	var b strings.Builder

	if r.Project != "" {
		fmt.Fprintf(&b, "vv trends --project %s\n", r.Project)
	} else {
		b.WriteString("vv trends\n")
	}

	// Overview
	fmt.Fprintf(&b, "\nOverview (%d sessions, %d weeks)\n", r.TotalSessions, r.TotalWeeks)
	for _, m := range r.Metrics {
		arrow := directionArrow(m.Direction)
		detail := ""
		if m.Direction != "stable" && m.DeltaPct != 0 {
			detail = fmt.Sprintf(" (%+.0f%%)", m.DeltaPct)
		}
		avgStr := formatMetricValue(m.Name, m.OverallAvg)
		fmt.Fprintf(&b, "  %-16s %8s avg  %s %s%s\n", m.Name, avgStr, arrow, m.Direction, detail)
	}

	// Per-metric week tables
	for _, m := range r.Metrics {
		if len(m.Points) == 0 {
			continue
		}
		fmt.Fprintf(&b, "\n%s\n", metricTitle(m.Name))
		fmt.Fprintf(&b, "  %-10s %8s %8s\n", "Week", "Value", "Avg")
		for _, p := range m.Points {
			valStr := formatMetricValue(m.Name, p.Value)
			avgStr := ""
			if p.RollingAvg > 0 {
				avgStr = formatMetricValue(m.Name, p.RollingAvg)
			}
			marker := ""
			if p.Anomaly {
				if p.RollingAvg > 0 && p.Value > p.RollingAvg {
					marker = "  ^ spike"
				} else {
					marker = "  v dip"
				}
			}
			fmt.Fprintf(&b, "  %-10s %8s %8s%s\n", p.WeekLabel, valStr, avgStr, marker)
		}
	}

	// Anomalies section
	var anomalies []string
	for _, m := range r.Metrics {
		for _, p := range m.Points {
			if p.Anomaly {
				kind := "spike"
				if p.RollingAvg > 0 && p.Value < p.RollingAvg {
					kind = "dip"
				}
				avgStr := formatMetricValue(m.Name, p.RollingAvg)
				valStr := formatMetricValue(m.Name, p.Value)
				anomalies = append(anomalies, fmt.Sprintf("  %-10s %-14s %s (avg %s)  %s",
					p.WeekLabel, m.Name, valStr, avgStr, kind))
			}
		}
	}
	if len(anomalies) > 0 {
		b.WriteString("\nAnomalies\n")
		for _, a := range anomalies {
			b.WriteString(a)
			b.WriteByte('\n')
		}
	}

	return b.String()
}

func directionArrow(dir string) string {
	switch dir {
	case "improving":
		return "↓"
	case "worsening":
		return "↑"
	default:
		return "→"
	}
}

func metricTitle(name string) string {
	switch name {
	case "friction":
		return "Friction Score"
	case "tokens/file":
		return "Tokens per File"
	case "corrections":
		return "Corrections per Session"
	case "duration":
		return "Duration (minutes)"
	default:
		return name
	}
}

func formatMetricValue(metric string, val float64) string {
	switch metric {
	case "duration":
		return formatDuration(int(val + 0.5))
	case "tokens/file":
		return formatTokens(int(val + 0.5))
	default:
		return formatInt(int(val + 0.5))
	}
}

// formatTokens formats a token count for display.
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
