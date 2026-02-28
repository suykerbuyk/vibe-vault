package trends

import (
	"math"
	"sort"
	"time"

	"github.com/johns/vibe-vault/internal/index"
)

// WeekBucket accumulates raw values for a single ISO week.
type WeekBucket struct {
	Year, Week int
	Start      time.Time // Monday of the ISO week

	FrictionScores []float64
	TokensPerFile  []float64
	Corrections    []float64
	Durations      []float64

	Sessions int
}

// TrendPoint is a single data point in a metric time series.
type TrendPoint struct {
	WeekLabel  string  // "Jan 06", "Feb 17", etc.
	Value      float64 // per-week average
	RollingAvg float64 // 4-week rolling average (0 if < 4 weeks of data)
	Anomaly    bool    // >1.5 stddev from rolling avg
}

// MetricTrend holds the full time series for one metric.
type MetricTrend struct {
	Name      string
	Points    []TrendPoint // most recent first
	OverallAvg float64
	Direction  string // "improving", "worsening", "stable"
	DeltaPct   float64 // percent change (negative = improving for friction/corrections)
}

// Result holds the complete trends analysis.
type Result struct {
	TotalSessions int
	TotalWeeks    int
	DisplayWeeks  int
	Project       string
	Metrics       []MetricTrend
}

// Compute builds trend analysis from index entries.
func Compute(entries map[string]index.SessionEntry, project string, displayWeeks int) Result {
	if displayWeeks <= 0 {
		displayWeeks = 12
	}

	// Filter and collect entries with valid dates
	var valid []index.SessionEntry
	for _, e := range entries {
		if project != "" && e.Project != project {
			continue
		}
		if len(e.Date) < 10 {
			continue
		}
		if _, err := time.Parse("2006-01-02", e.Date); err != nil {
			continue
		}
		valid = append(valid, e)
	}

	if len(valid) == 0 {
		return Result{Project: project, DisplayWeeks: displayWeeks}
	}

	// Bucket into ISO weeks
	bucketMap := make(map[[2]int]*WeekBucket)
	for _, e := range valid {
		t, _ := time.Parse("2006-01-02", e.Date)
		year, week := t.ISOWeek()
		key := [2]int{year, week}

		b, ok := bucketMap[key]
		if !ok {
			b = &WeekBucket{Year: year, Week: week, Start: isoWeekStart(year, week)}
			bucketMap[key] = b
		}
		b.Sessions++

		if e.FrictionScore > 0 {
			b.FrictionScores = append(b.FrictionScores, float64(e.FrictionScore))
		}

		filesChanged := len(e.FilesChanged)
		totalTokens := e.TokensIn + e.TokensOut
		if filesChanged > 0 && totalTokens > 0 {
			b.TokensPerFile = append(b.TokensPerFile, float64(totalTokens)/float64(filesChanged))
		}

		b.Corrections = append(b.Corrections, float64(e.Corrections))

		if e.Duration > 0 {
			b.Durations = append(b.Durations, float64(e.Duration))
		}
	}

	// Sort buckets chronologically (oldest first for rolling avg computation)
	buckets := make([]*WeekBucket, 0, len(bucketMap))
	for _, b := range bucketMap {
		buckets = append(buckets, b)
	}
	sort.Slice(buckets, func(i, j int) bool {
		if buckets[i].Year != buckets[j].Year {
			return buckets[i].Year < buckets[j].Year
		}
		return buckets[i].Week < buckets[j].Week
	})

	// Build metric trends
	frictionPts := buildPoints(buckets, func(b *WeekBucket) (float64, bool) { return avg(b.FrictionScores) })
	tokensPts := buildPoints(buckets, func(b *WeekBucket) (float64, bool) { return avg(b.TokensPerFile) })
	correctionsPts := buildPoints(buckets, func(b *WeekBucket) (float64, bool) { return avg(b.Corrections) })
	durationPts := buildPoints(buckets, func(b *WeekBucket) (float64, bool) { return avg(b.Durations) })

	metrics := []MetricTrend{
		buildMetric("friction", frictionPts, displayWeeks, true),
		buildMetric("tokens/file", tokensPts, displayWeeks, true),
		buildMetric("corrections", correctionsPts, displayWeeks, true),
		buildMetric("duration", durationPts, displayWeeks, true),
	}

	return Result{
		TotalSessions: len(valid),
		TotalWeeks:    len(buckets),
		DisplayWeeks:  displayWeeks,
		Project:       project,
		Metrics:       metrics,
	}
}

// buildPoints creates TrendPoints from buckets using an extractor function.
// Points are returned oldest-first for rolling avg computation.
func buildPoints(buckets []*WeekBucket, extract func(*WeekBucket) (float64, bool)) []TrendPoint {
	var pts []TrendPoint
	for _, b := range buckets {
		val, ok := extract(b)
		if !ok {
			continue
		}
		pts = append(pts, TrendPoint{
			WeekLabel: weekLabel(b.Start),
			Value:     val,
		})
	}
	return pts
}

// buildMetric computes rolling averages, anomalies, and direction for a metric.
// lowerIsBetter controls direction interpretation.
func buildMetric(name string, pts []TrendPoint, displayWeeks int, lowerIsBetter bool) MetricTrend {
	m := MetricTrend{Name: name}

	if len(pts) == 0 {
		m.Direction = "stable"
		return m
	}

	// Compute rolling average and stddev, detect anomalies
	values := make([]float64, len(pts))
	for i := range pts {
		values[i] = pts[i].Value
	}

	for i := range pts {
		if i >= 3 { // need at least 4 points for rolling avg
			ra := rollingAvg(values, i, 4)
			pts[i].RollingAvg = ra

			sd := rollingStddev(values, i, 4)
			if sd > 0 && math.Abs(pts[i].Value-ra) > 1.5*sd {
				pts[i].Anomaly = true
			}
		}
	}

	// Overall average
	var sum float64
	for _, v := range values {
		sum += v
	}
	m.OverallAvg = sum / float64(len(values))

	// Direction: compare last 4 weeks vs previous 4 weeks
	m.Direction, m.DeltaPct = metricDirection(values, lowerIsBetter)

	// Reverse to most-recent-first, then trim to displayWeeks
	reversed := make([]TrendPoint, len(pts))
	for i, p := range pts {
		reversed[len(pts)-1-i] = p
	}

	if len(reversed) > displayWeeks {
		reversed = reversed[:displayWeeks]
	}
	m.Points = reversed

	return m
}

// metricDirection compares the last 4 values vs the previous 4.
// Returns direction string and delta percentage.
func metricDirection(values []float64, lowerIsBetter bool) (string, float64) {
	n := len(values)
	if n < 8 {
		return "stable", 0
	}

	// Last 4 weeks average
	recent := rollingAvg(values, n-1, 4)
	// Previous 4 weeks average
	prev := rollingAvg(values, n-5, 4)

	if prev == 0 {
		return "stable", 0
	}

	delta := (recent - prev) / prev * 100

	if math.Abs(delta) < 10 {
		return "stable", delta
	}

	if lowerIsBetter {
		if delta < 0 {
			return "improving", delta
		}
		return "worsening", delta
	}

	// Higher is better
	if delta > 0 {
		return "improving", delta
	}
	return "worsening", delta
}

// --- Helpers ---

// isoWeekStart returns the Monday of the given ISO year/week.
func isoWeekStart(year, week int) time.Time {
	// Jan 4 is always in week 1
	jan4 := time.Date(year, time.January, 4, 0, 0, 0, 0, time.UTC)
	// Find Monday of week 1
	weekday := jan4.Weekday()
	if weekday == time.Sunday {
		weekday = 7
	}
	monday := jan4.AddDate(0, 0, -int(weekday-time.Monday))
	// Add (week-1) * 7 days
	return monday.AddDate(0, 0, (week-1)*7)
}

// weekLabel formats a date as "Jan 06".
func weekLabel(t time.Time) string {
	return t.Format("Jan 02")
}

// avg computes the arithmetic mean. Returns (0, false) if slice is empty.
func avg(vals []float64) (float64, bool) {
	if len(vals) == 0 {
		return 0, false
	}
	var sum float64
	for _, v := range vals {
		sum += v
	}
	return sum / float64(len(vals)), true
}

// rollingAvg computes the average of the `window` values ending at index `end` (inclusive).
func rollingAvg(values []float64, end, window int) float64 {
	start := end - window + 1
	if start < 0 {
		start = 0
	}
	var sum float64
	count := 0
	for i := start; i <= end; i++ {
		sum += values[i]
		count++
	}
	if count == 0 {
		return 0
	}
	return sum / float64(count)
}

// rollingStddev computes the standard deviation of the `window` values ending at index `end`.
func rollingStddev(values []float64, end, window int) float64 {
	start := end - window + 1
	if start < 0 {
		start = 0
	}
	mean := rollingAvg(values, end, window)
	var sumSq float64
	count := 0
	for i := start; i <= end; i++ {
		diff := values[i] - mean
		sumSq += diff * diff
		count++
	}
	if count < 2 {
		return 0
	}
	return math.Sqrt(sumSq / float64(count))
}
