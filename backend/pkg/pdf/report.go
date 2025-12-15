package pdf

import (
	"fmt"
	"io"
	"sort"
	"time"
)

// SessionReport holds all data needed to generate the PDF report.
type SessionReport struct {
	PollTitle        string
	StartedAt        *time.Time
	FinishedAt       *time.Time
	ParticipantCount int
	AverageScore     float64
	Questions        []QuestionReport
	Leaderboard      []LeaderboardEntry
	// ChartImages maps question index (0-based) → base64-encoded PNG/JPEG chart image.
	ChartImages map[int]string
}

// QuestionReport holds per-question stats.
type QuestionReport struct {
	Text         string
	Type         string
	TotalAnswers int
	Distribution map[string]int // option text → count
	CorrectKey   string         // if non-empty, which option key is correct
}

// LeaderboardEntry is one row in the final ranking.
type LeaderboardEntry struct {
	Rank  int
	Name  string
	Score int
}

// GenerateReport writes a PDF session report to w.
func GenerateReport(w io.Writer, r SessionReport) error {
	doc := New()

	// Pre-embed chart images
	chartNames := make(map[int]string, len(r.ChartImages))
	for qi, b64 := range r.ChartImages {
		name, err := doc.AddImagePNG(b64)
		if err == nil {
			chartNames[qi] = name
		}
	}

	// ─── PAGE 1: Title ───────────────────────────────────────────────────────
	pg := doc.NewPage()

	// Big title
	pg.y = A4H - 100
	pg.SetFontBold(22)
	pg.Text("Отчёт о сессии опроса")

	pg.Ln(0.5)
	pg.SetFontBold(16)
	truncate := func(s string, n int) string {
		runes := []rune(s)
		if len(runes) > n {
			return string(runes[:n]) + "..."
		}
		return s
	}
	pg.Text(truncate(r.PollTitle, 60))

	pg.Ln(1)
	pg.SetFontNormal(11)

	if r.StartedAt != nil {
		pg.Text(fmt.Sprintf("Дата проведения: %s", r.StartedAt.Format("02.01.2006 15:04")))
	}
	if r.FinishedAt != nil {
		pg.Text(fmt.Sprintf("Завершено: %s", r.FinishedAt.Format("02.01.2006 15:04")))
	}

	pg.Ln(0.5)
	pg.HRule()
	pg.Ln(0.5)

	// Stats grid
	pg.SetFontBold(12)
	pg.Text("Общая статистика")
	pg.Ln(0.3)
	pg.SetFontNormal(11)
	pg.Text(fmt.Sprintf("Участников:        %d", r.ParticipantCount))
	pg.Text(fmt.Sprintf("Средний балл:      %.1f", r.AverageScore))
	pg.Text(fmt.Sprintf("Вопросов:          %d", len(r.Questions)))

	// Total answers and correct percentage
	totalAnswers := 0
	for _, q := range r.Questions {
		totalAnswers += q.TotalAnswers
	}
	pg.Text(fmt.Sprintf("Всего ответов:     %d", totalAnswers))

	pg.Finalize()

	// ─── PAGE 2+: Questions ───────────────────────────────────────────────────
	pg = doc.NewPage()
	pg.SetFontBold(14)
	pg.Text("Результаты по вопросам")
	pg.Ln(0.3)

	for qi, q := range r.Questions {
		if pg.NeedBreak() {
			pg.Finalize()
			pg = doc.NewPage()
		}

		pg.SetFontBold(11)
		pg.Text(fmt.Sprintf("%d. %s", qi+1, truncate(q.Text, 70)))
		pg.SetFontNormal(10)
		pg.Text(fmt.Sprintf("   Тип: %s   |   Ответили: %d", localizeType(q.Type), q.TotalAnswers))
		pg.Ln(0.2)

		// Inline bar chart from distribution
		if len(q.Distribution) > 0 {
			// Sort keys for consistent rendering
			keys := make([]string, 0, len(q.Distribution))
			for k := range q.Distribution {
				keys = append(keys, k)
			}
			sort.Strings(keys)

			maxCount := 0
			for _, cnt := range q.Distribution {
				if cnt > maxCount {
					maxCount = cnt
				}
			}

			if chartName, ok := chartNames[qi]; ok {
				// Use provided chart image
				if pg.NeedBreak() {
					pg.Finalize()
					pg = doc.NewPage()
				}
				pg.Image(chartName, A4W-pg.margin*2, 120)
			} else {
				// Draw simple inline bars
				barMaxW := (A4W - pg.margin*2 - 80) * 0.6
				for _, key := range keys {
					if pg.NeedBreak() {
						pg.Finalize()
						pg = doc.NewPage()
					}
					cnt := q.Distribution[key]
					label := truncate(key, 25)
					barW := 0.0
					if maxCount > 0 {
						barW = float64(cnt) / float64(maxCount) * barMaxW
					}
					shade := 0.4
					if key == q.CorrectKey {
						shade = 0.2 // darker = correct
					}
					// Label
					pg.SetFontNormal(9)
					pg.TextAt(pg.margin, label)
					// Bar
					if barW > 1 {
						pg.DrawBar(pg.margin+100, barW, 10, shade)
					}
					// Count
					pg.TextAt(pg.margin+110+barMaxW, fmt.Sprintf("%d", cnt))
					pg.y -= 13
				}
			}
		}

		pg.Ln(0.5)
	}
	pg.Finalize()

	// ─── PAGE N: Leaderboard ──────────────────────────────────────────────────
	if len(r.Leaderboard) > 0 {
		pg = doc.NewPage()
		pg.SetFontBold(14)
		pg.Text("Итоговый рейтинг (топ-10)")
		pg.Ln(0.5)
		pg.HRule()
		pg.Ln(0.3)

		top := r.Leaderboard
		if len(top) > 10 {
			top = top[:10]
		}

		for _, entry := range top {
			if pg.NeedBreak() {
				pg.Finalize()
				pg = doc.NewPage()
			}
			medal := ""
			switch entry.Rank {
			case 1:
				medal = "1. "
			case 2:
				medal = "2. "
			case 3:
				medal = "3. "
			default:
				medal = fmt.Sprintf("%d. ", entry.Rank)
			}
			pg.SetFontNormal(11)
			pg.TextAt(pg.margin, medal+truncate(entry.Name, 35))
			pg.TextAt(pg.margin+320, fmt.Sprintf("%d очков", entry.Score))
			pg.y -= pg.lineH
		}
		pg.Finalize()
	}

	return doc.WriteTo(w)
}

func localizeType(t string) string {
	switch t {
	case "single_choice":
		return "Один вариант"
	case "multiple_choice":
		return "Несколько вариантов"
	case "open_text":
		return "Открытый ответ"
	case "image_choice":
		return "Выбор изображения"
	case "word_cloud":
		return "Облако слов"
	case "brainstorm":
		return "Брейншторм"
	default:
		return t
	}
}
