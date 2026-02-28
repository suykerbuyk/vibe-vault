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
