// Package scoring implements the quiz scoring rules defined in the PRD.
//
// Three rules are supported:
//   - none          — no points are awarded
//   - correct_answer — base points for a correct answer, 0 for wrong
//   - speed_bonus   — fractional base points proportional to remaining time,
//     with a guaranteed minimum of 10 % of base points
package scoring

// CalculateScore returns the score earned for a single answer.
//
//   - basePoints:     question.points
//   - timeLimitSec:  question.time_limit_seconds
//   - responseTimeMs: time elapsed from question start to answer (server-measured)
//   - scoringRule:    poll.scoring_rule (none | correct_answer | speed_bonus)
//   - isCorrect:      nil means the question has no correct/incorrect concept
func CalculateScore(basePoints, timeLimitSec, responseTimeMs int, scoringRule string, isCorrect *bool) int {
	if scoringRule == "none" {
		return 0
	}

	// For questions without a correct/incorrect concept (open_text, word_cloud, brainstorm)
	// no score is awarded in any mode.
	if isCorrect == nil || !*isCorrect {
		return 0
	}

	switch scoringRule {
	case "correct_answer":
		return basePoints

	case "speed_bonus":
		timeLimitMs := timeLimitSec * 1000
		if timeLimitMs <= 0 {
			return basePoints
		}
		timeRemainingMs := timeLimitMs - responseTimeMs
		if timeRemainingMs < 0 {
			timeRemainingMs = 0
		}
		score := basePoints * timeRemainingMs / timeLimitMs
		// Minimum 10 % of base points.
		minScore := basePoints / 10
		if minScore < 1 {
			minScore = 1
		}
		if score < minScore {
			return minScore
		}
		return score
	}

	return 0
}
