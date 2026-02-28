package trends

import (
	"math"
	"strings"
	"testing"

	"github.com/johns/vibe-vault/internal/index"
)

func makeEntry(id, project, date string, friction, corrections, duration, tokensIn, tokensOut int, files []string) index.SessionEntry {
	return index.SessionEntry{
		SessionID:     id,
		Project:       project,
		Date:          date,
		FrictionScore: friction,
		Corrections:   corrections,
		Duration:      duration,
		TokensIn:      tokensIn,
		TokensOut:     tokensOut,
		FilesChanged:  files,
	}
}

func TestComputeEmpty(t *testing.T) {
	r := Compute(nil, "", 12)
	if r.TotalSessions != 0 {
		t.Errorf("expected 0 sessions, got %d", r.TotalSessions)
	}
	if len(r.Metrics) != 0 {
		t.Errorf("expected no metrics, got %d", len(r.Metrics))
	}
}

func TestComputeEmptyMap(t *testing.T) {
	r := Compute(map[string]index.SessionEntry{}, "", 12)
	if r.TotalSessions != 0 {
		t.Errorf("expected 0 sessions, got %d", r.TotalSessions)
	}
}

func TestComputeSingleSession(t *testing.T) {
	entries := map[string]index.SessionEntry{
		"s1": makeEntry("s1", "proj", "2025-02-10", 25, 2, 30, 5000, 3000, []string{"a.go", "b.go"}),
	}
	r := Compute(entries, "", 12)
	if r.TotalSessions != 1 {
		t.Errorf("expected 1 session, got %d", r.TotalSessions)
	}
	if r.TotalWeeks != 1 {
		t.Errorf("expected 1 week, got %d", r.TotalWeeks)
	}
	if len(r.Metrics) != 4 {
		t.Errorf("expected 4 metrics, got %d", len(r.Metrics))
	}
}

func TestComputeTwoWeeks(t *testing.T) {
	entries := map[string]index.SessionEntry{
		"s1": makeEntry("s1", "proj", "2025-02-03", 20, 1, 25, 4000, 2000, []string{"a.go"}),
		"s2": makeEntry("s2", "proj", "2025-02-10", 30, 3, 40, 6000, 4000, []string{"b.go"}),
	}
	r := Compute(entries, "", 12)
	if r.TotalWeeks != 2 {
		t.Errorf("expected 2 weeks, got %d", r.TotalWeeks)
	}
}

func TestComputeProjectFilter(t *testing.T) {
	entries := map[string]index.SessionEntry{
		"s1": makeEntry("s1", "alpha", "2025-02-03", 20, 1, 25, 4000, 2000, []string{"a.go"}),
		"s2": makeEntry("s2", "beta", "2025-02-10", 30, 3, 40, 6000, 4000, []string{"b.go"}),
	}
	r := Compute(entries, "alpha", 12)
	if r.TotalSessions != 1 {
		t.Errorf("expected 1 session for alpha, got %d", r.TotalSessions)
	}
	if r.Project != "alpha" {
		t.Errorf("expected project=alpha, got %q", r.Project)
	}
}

func TestComputeFrictionAverage(t *testing.T) {
	// Two sessions in same week with different friction scores
	entries := map[string]index.SessionEntry{
		"s1": makeEntry("s1", "proj", "2025-02-03", 20, 0, 0, 0, 0, nil),
		"s2": makeEntry("s2", "proj", "2025-02-04", 40, 0, 0, 0, 0, nil),
	}
	r := Compute(entries, "", 12)

	// Find friction metric
	var friction MetricTrend
	for _, m := range r.Metrics {
		if m.Name == "friction" {
			friction = m
			break
		}
	}

	if len(friction.Points) != 1 {
		t.Fatalf("expected 1 point, got %d", len(friction.Points))
	}
	// Average of 20 and 40 = 30
	if friction.Points[0].Value != 30 {
		t.Errorf("expected friction value 30, got %.1f", friction.Points[0].Value)
	}
}

func TestComputeTokensPerFile(t *testing.T) {
	entries := map[string]index.SessionEntry{
		"s1": makeEntry("s1", "proj", "2025-02-03", 0, 0, 0, 3000, 1000, []string{"a.go", "b.go"}),
	}
	r := Compute(entries, "", 12)

	var tokMetric MetricTrend
	for _, m := range r.Metrics {
		if m.Name == "tokens/file" {
			tokMetric = m
			break
		}
	}

	if len(tokMetric.Points) != 1 {
		t.Fatalf("expected 1 point, got %d", len(tokMetric.Points))
	}
	// (3000+1000) / 2 files = 2000
	if tokMetric.Points[0].Value != 2000 {
		t.Errorf("expected tokens/file value 2000, got %.1f", tokMetric.Points[0].Value)
	}
}

func TestComputeSkipsZeroFriction(t *testing.T) {
	entries := map[string]index.SessionEntry{
		"s1": makeEntry("s1", "proj", "2025-02-03", 0, 0, 0, 1000, 500, []string{"a.go"}),
	}
	r := Compute(entries, "", 12)

	var friction MetricTrend
	for _, m := range r.Metrics {
		if m.Name == "friction" {
			friction = m
			break
		}
	}

	if len(friction.Points) != 0 {
		t.Errorf("expected 0 friction points (skips zero), got %d", len(friction.Points))
	}
}

func TestComputeSkipsZeroDuration(t *testing.T) {
	entries := map[string]index.SessionEntry{
		"s1": makeEntry("s1", "proj", "2025-02-03", 10, 0, 0, 1000, 500, []string{"a.go"}),
	}
	r := Compute(entries, "", 12)

	var duration MetricTrend
	for _, m := range r.Metrics {
		if m.Name == "duration" {
			duration = m
			break
		}
	}

	if len(duration.Points) != 0 {
		t.Errorf("expected 0 duration points (skips zero), got %d", len(duration.Points))
	}
}

func TestComputeRollingAverage(t *testing.T) {
	// 5 weeks of data — the 4th and 5th points should have rolling averages
	entries := map[string]index.SessionEntry{
		"s1": makeEntry("s1", "proj", "2025-01-06", 10, 0, 0, 0, 0, nil),
		"s2": makeEntry("s2", "proj", "2025-01-13", 20, 0, 0, 0, 0, nil),
		"s3": makeEntry("s3", "proj", "2025-01-20", 30, 0, 0, 0, 0, nil),
		"s4": makeEntry("s4", "proj", "2025-01-27", 40, 0, 0, 0, 0, nil),
		"s5": makeEntry("s5", "proj", "2025-02-03", 50, 0, 0, 0, 0, nil),
	}
	r := Compute(entries, "", 12)

	var friction MetricTrend
	for _, m := range r.Metrics {
		if m.Name == "friction" {
			friction = m
			break
		}
	}

	if len(friction.Points) != 5 {
		t.Fatalf("expected 5 points, got %d", len(friction.Points))
	}

	// Points are most-recent-first. Point[0] = week 5 (val=50)
	// Rolling avg for week 5 = avg(20,30,40,50) = 35
	if math.Abs(friction.Points[0].RollingAvg-35) > 0.1 {
		t.Errorf("expected rolling avg ~35 for most recent, got %.1f", friction.Points[0].RollingAvg)
	}

	// Point[1] = week 4 (val=40), rolling avg = avg(10,20,30,40) = 25
	if math.Abs(friction.Points[1].RollingAvg-25) > 0.1 {
		t.Errorf("expected rolling avg ~25 for second point, got %.1f", friction.Points[1].RollingAvg)
	}

	// First 3 points (indices 2,3,4 = weeks 3,2,1) should have no rolling avg
	if friction.Points[2].RollingAvg != 0 {
		t.Errorf("expected no rolling avg for week 3, got %.1f", friction.Points[2].RollingAvg)
	}
}

func TestComputeAnomalyDetection(t *testing.T) {
	// 4 normal weeks + 1 spike
	entries := map[string]index.SessionEntry{
		"s1": makeEntry("s1", "proj", "2025-01-06", 20, 0, 0, 0, 0, nil),
		"s2": makeEntry("s2", "proj", "2025-01-13", 22, 0, 0, 0, 0, nil),
		"s3": makeEntry("s3", "proj", "2025-01-20", 21, 0, 0, 0, 0, nil),
		"s4": makeEntry("s4", "proj", "2025-01-27", 23, 0, 0, 0, 0, nil),
		"s5": makeEntry("s5", "proj", "2025-02-03", 80, 0, 0, 0, 0, nil), // spike
	}
	r := Compute(entries, "", 12)

	var friction MetricTrend
	for _, m := range r.Metrics {
		if m.Name == "friction" {
			friction = m
			break
		}
	}

	// Most recent point (index 0) should be anomaly
	if len(friction.Points) < 1 {
		t.Fatal("no friction points")
	}
	if !friction.Points[0].Anomaly {
		t.Error("expected spike at most recent week to be flagged as anomaly")
	}
}

func TestComputeDirection(t *testing.T) {
	// 8 weeks: first 4 high friction, last 4 low = improving
	entries := make(map[string]index.SessionEntry)
	dates := []string{
		"2025-01-06", "2025-01-13", "2025-01-20", "2025-01-27",
		"2025-02-03", "2025-02-10", "2025-02-17", "2025-02-24",
	}
	for i, d := range dates {
		score := 50
		if i >= 4 {
			score = 20
		}
		id := string(rune('a'+i)) + "1"
		entries[id] = makeEntry(id, "proj", d, score, 0, 0, 0, 0, nil)
	}

	r := Compute(entries, "", 12)

	var friction MetricTrend
	for _, m := range r.Metrics {
		if m.Name == "friction" {
			friction = m
			break
		}
	}

	if friction.Direction != "improving" {
		t.Errorf("expected direction=improving, got %q", friction.Direction)
	}
}

func TestComputeDisplayWeeksLimit(t *testing.T) {
	entries := make(map[string]index.SessionEntry)
	// 10 weeks of data
	dates := []string{
		"2025-01-06", "2025-01-13", "2025-01-20", "2025-01-27",
		"2025-02-03", "2025-02-10", "2025-02-17", "2025-02-24",
		"2025-03-03", "2025-03-10",
	}
	for i, d := range dates {
		id := string(rune('a'+i)) + "1"
		entries[id] = makeEntry(id, "proj", d, 20+i, 0, 0, 0, 0, nil)
	}

	r := Compute(entries, "", 5)

	var friction MetricTrend
	for _, m := range r.Metrics {
		if m.Name == "friction" {
			friction = m
			break
		}
	}

	if len(friction.Points) > 5 {
		t.Errorf("expected at most 5 displayed points, got %d", len(friction.Points))
	}
	if r.TotalWeeks != 10 {
		t.Errorf("expected 10 total weeks, got %d", r.TotalWeeks)
	}
}

func TestComputeSkipsBadDates(t *testing.T) {
	entries := map[string]index.SessionEntry{
		"s1": makeEntry("s1", "proj", "bad-date", 20, 0, 0, 0, 0, nil),
		"s2": makeEntry("s2", "proj", "2025", 20, 0, 0, 0, 0, nil),
		"s3": makeEntry("s3", "proj", "", 20, 0, 0, 0, 0, nil),
	}
	r := Compute(entries, "", 12)
	if r.TotalSessions != 0 {
		t.Errorf("expected 0 sessions (all bad dates), got %d", r.TotalSessions)
	}
}

// --- Format tests ---

func TestFormatEmpty(t *testing.T) {
	r := Result{Project: "", DisplayWeeks: 12}
	out := Format(r)
	if !strings.Contains(out, "No sessions found") {
		t.Error("expected 'No sessions found' message")
	}
}

func TestFormatEmptyProject(t *testing.T) {
	r := Result{Project: "myproj", DisplayWeeks: 12}
	out := Format(r)
	if !strings.Contains(out, "myproj") {
		t.Error("expected project name in empty output")
	}
}

func TestFormatSectionsPresent(t *testing.T) {
	entries := make(map[string]index.SessionEntry)
	dates := []string{
		"2025-01-06", "2025-01-13", "2025-01-20", "2025-01-27",
		"2025-02-03",
	}
	for i, d := range dates {
		id := string(rune('a'+i)) + "1"
		entries[id] = makeEntry(id, "proj", d, 20+i*5, i+1, 30+i*5, 5000, 3000, []string{"a.go"})
	}

	r := Compute(entries, "", 12)
	out := Format(r)

	assertContains(t, out, "Overview")
	assertContains(t, out, "sessions")
	assertContains(t, out, "weeks")
	assertContains(t, out, "Friction Score")
	assertContains(t, out, "Tokens per File")
	assertContains(t, out, "Corrections per Session")
	assertContains(t, out, "Duration")
	assertContains(t, out, "Week")
	assertContains(t, out, "Value")
	assertContains(t, out, "Avg")
}

func TestFormatAnomalyMarker(t *testing.T) {
	entries := make(map[string]index.SessionEntry)
	dates := []string{
		"2025-01-06", "2025-01-13", "2025-01-20", "2025-01-27",
		"2025-02-03",
	}
	scores := []int{20, 22, 21, 23, 80}
	for i, d := range dates {
		id := string(rune('a'+i)) + "1"
		entries[id] = makeEntry(id, "proj", d, scores[i], 0, 0, 0, 0, nil)
	}

	r := Compute(entries, "", 12)
	out := Format(r)

	if !strings.Contains(out, "spike") {
		t.Error("expected anomaly spike marker in output")
	}
	assertContains(t, out, "Anomalies")
}

func TestFormatProjectInHeader(t *testing.T) {
	entries := map[string]index.SessionEntry{
		"s1": makeEntry("s1", "myproj", "2025-02-03", 20, 1, 30, 5000, 3000, []string{"a.go"}),
	}
	r := Compute(entries, "myproj", 12)
	out := Format(r)

	if !strings.Contains(out, "vv trends --project myproj") {
		t.Error("expected project in header")
	}
}

// --- Helper tests ---

func TestRollingAvg(t *testing.T) {
	values := []float64{10, 20, 30, 40, 50}
	got := rollingAvg(values, 3, 4)
	want := 25.0 // avg(10,20,30,40)
	if math.Abs(got-want) > 0.01 {
		t.Errorf("rollingAvg = %.2f, want %.2f", got, want)
	}
}

func TestRollingAvgShortWindow(t *testing.T) {
	values := []float64{10, 20}
	got := rollingAvg(values, 1, 4)
	want := 15.0 // avg(10,20) — window clamped
	if math.Abs(got-want) > 0.01 {
		t.Errorf("rollingAvg short = %.2f, want %.2f", got, want)
	}
}

func TestMetricDirection(t *testing.T) {
	// Decreasing values → improving (lower is better)
	values := []float64{50, 48, 52, 49, 20, 22, 18, 21}
	dir, _ := metricDirection(values, true)
	if dir != "improving" {
		t.Errorf("expected improving, got %q", dir)
	}

	// Increasing values → worsening (lower is better)
	values2 := []float64{20, 22, 18, 21, 50, 48, 52, 49}
	dir2, _ := metricDirection(values2, true)
	if dir2 != "worsening" {
		t.Errorf("expected worsening, got %q", dir2)
	}
}

func TestMetricDirectionStable(t *testing.T) {
	// Too few values for direction
	values := []float64{20, 22, 18}
	dir, _ := metricDirection(values, true)
	if dir != "stable" {
		t.Errorf("expected stable for short data, got %q", dir)
	}
}

func TestISOWeekStart(t *testing.T) {
	// 2025-W06 starts on Monday Feb 3
	got := isoWeekStart(2025, 6)
	if got.Month() != 2 || got.Day() != 3 {
		t.Errorf("expected Feb 3, got %s", got.Format("Jan 02"))
	}
}

func TestWeekLabel(t *testing.T) {
	tm := isoWeekStart(2025, 6)
	label := weekLabel(tm)
	if label != "Feb 03" {
		t.Errorf("expected 'Feb 03', got %q", label)
	}
}

// assertContains is a test helper for string containment.
func assertContains(t *testing.T, s, substr string) {
	t.Helper()
	if !strings.Contains(s, substr) {
		t.Errorf("expected output to contain %q, got:\n%s", substr, s)
	}
}
