package scoring_test

import (
	"testing"

	"presentarium/pkg/scoring"
)

func boolPtr(b bool) *bool { return &b }

func TestCalculateScore_NoneRule(t *testing.T) {
	// none rule always returns 0, regardless of correctness or timing.
	if got := scoring.CalculateScore(100, 30, 0, "none", boolPtr(true)); got != 0 {
		t.Errorf("none rule with correct answer: want 0, got %d", got)
	}
	if got := scoring.CalculateScore(100, 30, 0, "none", boolPtr(false)); got != 0 {
		t.Errorf("none rule with wrong answer: want 0, got %d", got)
	}
	if got := scoring.CalculateScore(100, 30, 0, "none", nil); got != 0 {
		t.Errorf("none rule with nil isCorrect: want 0, got %d", got)
	}
}

func TestCalculateScore_CorrectAnswerRule(t *testing.T) {
	// correct_answer: full base points for correct, 0 for wrong.
	if got := scoring.CalculateScore(100, 30, 5000, "correct_answer", boolPtr(true)); got != 100 {
		t.Errorf("correct_answer with correct: want 100, got %d", got)
	}
	if got := scoring.CalculateScore(100, 30, 5000, "correct_answer", boolPtr(false)); got != 0 {
		t.Errorf("correct_answer with wrong: want 0, got %d", got)
	}
	if got := scoring.CalculateScore(100, 30, 5000, "correct_answer", nil); got != 0 {
		t.Errorf("correct_answer with nil isCorrect: want 0, got %d", got)
	}
}

func TestCalculateScore_SpeedBonus_Maximum(t *testing.T) {
	// Answering at the very start (responseTimeMs=0) → full base points.
	got := scoring.CalculateScore(100, 30, 0, "speed_bonus", boolPtr(true))
	if got != 100 {
		t.Errorf("speed_bonus at start: want 100, got %d", got)
	}
}

func TestCalculateScore_SpeedBonus_Minimum(t *testing.T) {
	// Answering exactly at the time limit → 0 remaining, should return minimum (10%).
	timeLimitMs := 30 * 1000
	got := scoring.CalculateScore(100, 30, timeLimitMs, "speed_bonus", boolPtr(true))
	want := 10 // 10% of 100
	if got != want {
		t.Errorf("speed_bonus at time limit: want %d, got %d", want, got)
	}
}

func TestCalculateScore_SpeedBonus_HalfTime(t *testing.T) {
	// Answering halfway through → ~50% of base points (but at least 10%).
	timeLimitMs := 30 * 1000
	halfTime := timeLimitMs / 2
	got := scoring.CalculateScore(100, 30, halfTime, "speed_bonus", boolPtr(true))
	// timeRemainingMs = 30000 - 15000 = 15000; score = 100 * 15000 / 30000 = 50
	if got != 50 {
		t.Errorf("speed_bonus at half time: want 50, got %d", got)
	}
}

func TestCalculateScore_SpeedBonus_WrongAnswer(t *testing.T) {
	// speed_bonus with wrong answer → 0.
	got := scoring.CalculateScore(100, 30, 0, "speed_bonus", boolPtr(false))
	if got != 0 {
		t.Errorf("speed_bonus with wrong answer: want 0, got %d", got)
	}
}

func TestCalculateScore_SpeedBonus_NilIsCorrect(t *testing.T) {
	// speed_bonus with nil isCorrect (open_text etc.) → 0.
	got := scoring.CalculateScore(100, 30, 0, "speed_bonus", nil)
	if got != 0 {
		t.Errorf("speed_bonus with nil isCorrect: want 0, got %d", got)
	}
}

func TestCalculateScore_SpeedBonus_MinimumFloor(t *testing.T) {
	// Over-time answer (responseTimeMs > timeLimitMs) → minimum 10%.
	got := scoring.CalculateScore(100, 30, 60000, "speed_bonus", boolPtr(true))
	want := 10
	if got != want {
		t.Errorf("speed_bonus over time: want %d, got %d", want, got)
	}
}

func TestCalculateScore_SpeedBonus_ZeroTimeLimit(t *testing.T) {
	// Zero time limit → returns base points to avoid division by zero.
	got := scoring.CalculateScore(100, 0, 0, "speed_bonus", boolPtr(true))
	if got != 100 {
		t.Errorf("speed_bonus with zero time limit: want 100, got %d", got)
	}
}

func TestCalculateScore_UnknownRule(t *testing.T) {
	// Unknown rule → 0.
	got := scoring.CalculateScore(100, 30, 0, "unknown_rule", boolPtr(true))
	if got != 0 {
		t.Errorf("unknown rule: want 0, got %d", got)
	}
}

func TestCalculateScore_SpeedBonus_MinimumFloor_SmallBase(t *testing.T) {
	// Small base where 10% would be 0 → minimum is at least 1.
	got := scoring.CalculateScore(5, 30, 30000, "speed_bonus", boolPtr(true))
	if got < 1 {
		t.Errorf("speed_bonus minimum floor: want at least 1, got %d", got)
	}
}
