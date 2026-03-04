package friction

import "testing"

func TestScore_ZeroSignals(t *testing.T) {
	s := Signals{}
	score := Score(s)
	if score != 0 {
		t.Errorf("expected 0, got %d", score)
	}
}

func TestScore_MaxSignals(t *testing.T) {
	s := Signals{
		CorrectionDensity: 0.30,
		TokensPerFile:     50000,
		FileRetryDensity:  0.50,
		ErrorCycleDensity: 0.20,
		RecurringThreads:  true,
	}
	score := Score(s)
	if score != 100 {
		t.Errorf("expected 100, got %d", score)
	}
}

func TestScore_CorrectionDensityOnly(t *testing.T) {
	s := Signals{CorrectionDensity: 0.30}
	score := Score(s)
	if score != weightCorrectionDensity {
		t.Errorf("expected %d, got %d", weightCorrectionDensity, score)
	}
}

func TestScore_TokenEfficiencyOnly(t *testing.T) {
	s := Signals{TokensPerFile: 50000}
	score := Score(s)
	if score != weightTokenEfficiency {
		t.Errorf("expected %d, got %d", weightTokenEfficiency, score)
	}
}

func TestScore_FileRetryOnly(t *testing.T) {
	s := Signals{FileRetryDensity: 0.50}
	score := Score(s)
	if score != weightFileRetryDensity {
		t.Errorf("expected %d, got %d", weightFileRetryDensity, score)
	}
}

func TestScore_ErrorCycleOnly(t *testing.T) {
	s := Signals{ErrorCycleDensity: 0.20}
	score := Score(s)
	if score != weightErrorCycleDensity {
		t.Errorf("expected %d, got %d", weightErrorCycleDensity, score)
	}
}

func TestScore_RecurringThreadsOnly(t *testing.T) {
	s := Signals{RecurringThreads: true}
	score := Score(s)
	if score != weightRecurringThreads {
		t.Errorf("expected %d, got %d", weightRecurringThreads, score)
	}
}

func TestScore_HalfThresholds(t *testing.T) {
	s := Signals{
		CorrectionDensity: 0.15,
		TokensPerFile:     25000,
		FileRetryDensity:  0.25,
		ErrorCycleDensity: 0.10,
	}
	score := Score(s)
	expected := 45 // half of 30+25+20+15 = 45
	if score != expected {
		t.Errorf("expected %d, got %d", expected, score)
	}
}

func TestScore_ClampAtMax(t *testing.T) {
	s := Signals{
		CorrectionDensity: 1.0, // way over threshold
		TokensPerFile:     200000,
		FileRetryDensity:  1.0,
		ErrorCycleDensity: 1.0,
		RecurringThreads:  true,
	}
	score := Score(s)
	if score != 100 {
		t.Errorf("expected 100 (clamped), got %d", score)
	}
}

func TestScore_PartialCombination(t *testing.T) {
	// 3 corrections in 10 user turns = 0.30 density → full weight
	// + recurring threads
	s := Signals{
		CorrectionDensity: 0.30,
		RecurringThreads:  true,
	}
	score := Score(s)
	expected := weightCorrectionDensity + weightRecurringThreads
	if score != expected {
		t.Errorf("expected %d, got %d", expected, score)
	}
}

func TestTopContributors_AllAtThreshold(t *testing.T) {
	s := Signals{
		CorrectionDensity: 0.30,
		TokensPerFile:     50000,
		FileRetryDensity:  0.50,
		ErrorCycleDensity: 0.20,
		RecurringThreads:  true,
	}
	top := TopContributors(s, 2)
	if len(top) != 2 {
		t.Fatalf("expected 2 contributors, got %d", len(top))
	}
	// corrections (30), tokens/file (25) are the top two
	if top[0].Name != "corrections" || top[0].Weight != 30 {
		t.Errorf("top[0] = %+v, want corrections:30", top[0])
	}
	if top[1].Name != "tokens/file" || top[1].Weight != 25 {
		t.Errorf("top[1] = %+v, want tokens/file:25", top[1])
	}
}

func TestTopContributors_SingleSignal(t *testing.T) {
	s := Signals{CorrectionDensity: 0.30}
	top := TopContributors(s, 3)
	if len(top) != 1 {
		t.Fatalf("expected 1 contributor, got %d", len(top))
	}
	if top[0].Name != "corrections" {
		t.Errorf("top[0].Name = %q, want corrections", top[0].Name)
	}
}

func TestTopContributors_ZeroSignals(t *testing.T) {
	s := Signals{}
	top := TopContributors(s, 2)
	if len(top) != 0 {
		t.Errorf("expected 0 contributors, got %d", len(top))
	}
}

func TestClamp(t *testing.T) {
	tests := []struct {
		v    float64
		want float64
	}{
		{-0.5, 0},
		{0, 0},
		{0.5, 0.5},
		{1.0, 1.0},
		{1.5, 1.0},
	}
	for _, tc := range tests {
		got := clamp(tc.v)
		if got != tc.want {
			t.Errorf("clamp(%v) = %v, want %v", tc.v, got, tc.want)
		}
	}
}
