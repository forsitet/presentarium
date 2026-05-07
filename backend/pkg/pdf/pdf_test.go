package pdf

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
	"time"
)

func TestUTF8ToCP1251_ASCII(t *testing.T) {
	got := utf8ToCP1251("hello")
	if string(got) != "hello" {
		t.Errorf("ascii passthrough failed: %q", got)
	}
}

func TestUTF8ToCP1251_Cyrillic(t *testing.T) {
	got := utf8ToCP1251("Привет")
	if len(got) != 6 {
		t.Errorf("expected 6 bytes (1 per Cyrillic char), got %d", len(got))
	}
	// 'П' (U+041F) maps to 0xCF in CP1251.
	if got[0] != 0xCF {
		t.Errorf("first byte = 0x%X, want 0xCF", got[0])
	}
}

func TestUTF8ToCP1251_Unmappable(t *testing.T) {
	// Hiragana — not in CP1251, should become '?'.
	got := utf8ToCP1251("あ")
	if len(got) != 1 || got[0] != '?' {
		t.Errorf("expected '?' for unmappable rune, got %v", got)
	}
}

func TestPdfEscape(t *testing.T) {
	in := []byte("a(b)c\\d")
	got := pdfEscape(in)
	want := `a\(b\)c\\d`
	if got != want {
		t.Errorf("pdfEscape = %q, want %q", got, want)
	}
}

func TestPdfEscape_NoSpecials(t *testing.T) {
	in := []byte("plain")
	if got := pdfEscape(in); got != "plain" {
		t.Errorf("pdfEscape passthrough failed: %q", got)
	}
}

func TestLocalizeType(t *testing.T) {
	cases := map[string]string{
		"single_choice":   "Один вариант",
		"multiple_choice": "Несколько вариантов",
		"open_text":       "Открытый ответ",
		"image_choice":    "Выбор изображения",
		"word_cloud":      "Облако слов",
		"brainstorm":      "Брейншторм",
		"unknown_type":    "unknown_type",
	}
	for in, want := range cases {
		if got := localizeType(in); got != want {
			t.Errorf("localizeType(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDoc_NewAndWriteEmpty(t *testing.T) {
	d := New()
	d.NewPage().Finalize()
	var buf bytes.Buffer
	if err := d.WriteTo(&buf); err != nil {
		t.Fatalf("WriteTo error: %v", err)
	}
	out := buf.String()
	if !strings.HasPrefix(out, "%PDF-1.4") {
		t.Error("missing PDF header")
	}
	if !strings.Contains(out, "%%EOF") {
		t.Error("missing PDF EOF trailer")
	}
	if !strings.Contains(out, "/Type /Catalog") {
		t.Error("missing catalog object")
	}
}

func TestPage_TextHelpers(t *testing.T) {
	d := New()
	pg := d.NewPage()
	pg.SetFontBold(14)
	pg.Text("Заголовок")
	pg.SetFontNormal(11)
	pg.TextAt(50, "слева")
	pg.TextRight("справа")
	pg.HRule()
	pg.DrawBarFull(100, 50, 10, 0.5)
	pg.Ln(2)
	pg.Finalize()

	var buf bytes.Buffer
	if err := d.WriteTo(&buf); err != nil {
		t.Fatalf("WriteTo error: %v", err)
	}
	if !bytes.Contains(buf.Bytes(), []byte("BT ")) {
		t.Error("expected at least one text-block (BT) operator")
	}
}

func TestPage_NeedBreak(t *testing.T) {
	d := New()
	pg := d.NewPage()
	if pg.NeedBreak() {
		t.Error("fresh page should not need break")
	}
	// Force y to bottom margin
	for i := 0; i < 100; i++ {
		pg.Ln(2)
	}
	if !pg.NeedBreak() {
		t.Error("after many newlines page should need break")
	}
}

// makePNGBase64 builds a tiny in-memory PNG and returns its base64 string.
func makePNGBase64(t *testing.T, w, h int) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 50), G: uint8(y * 50), B: 100, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png encode: %v", err)
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes())
}

func TestAddImagePNG_Success(t *testing.T) {
	d := New()
	b64 := makePNGBase64(t, 4, 3)
	name, err := d.AddImagePNG(b64)
	if err != nil {
		t.Fatalf("AddImagePNG: %v", err)
	}
	if name == "" {
		t.Error("expected non-empty image name")
	}
	if len(d.images) != 1 {
		t.Errorf("expected 1 image registered, got %d", len(d.images))
	}
	// Use the image on a page.
	pg := d.NewPage()
	pg.Image(name, 100, 100)
	pg.Finalize()

	var buf bytes.Buffer
	if err := d.WriteTo(&buf); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	if !bytes.Contains(buf.Bytes(), []byte("/Subtype /Image")) {
		t.Error("output should contain image XObject")
	}
}

func TestAddImagePNG_BadBase64(t *testing.T) {
	d := New()
	_, err := d.AddImagePNG("not-base64!!!")
	if err == nil {
		t.Error("expected error for invalid base64")
	}
}

func TestAddImagePNG_NotAnImage(t *testing.T) {
	d := New()
	junk := base64.StdEncoding.EncodeToString([]byte("not an image"))
	_, err := d.AddImagePNG(junk)
	if err == nil {
		t.Error("expected error decoding non-image bytes")
	}
}

func TestPage_ImageNotFound(t *testing.T) {
	// Calling Image with an unknown name should be a no-op (no panic).
	d := New()
	pg := d.NewPage()
	pg.Image("does-not-exist", 100, 100)
	pg.Finalize()
	var buf bytes.Buffer
	if err := d.WriteTo(&buf); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
}

func TestGenerateReport_FullScenario(t *testing.T) {
	started := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	finished := started.Add(15 * time.Minute)
	r := SessionReport{
		PollTitle:        "Тест опрос",
		StartedAt:        &started,
		FinishedAt:       &finished,
		ParticipantCount: 5,
		AverageScore:     42.5,
		Questions: []QuestionReport{
			{
				Text:         "Какой ваш любимый цвет?",
				Type:         "single_choice",
				TotalAnswers: 5,
				Distribution: map[string]int{"Красный": 3, "Синий": 2},
				CorrectKey:   "Красный",
			},
			{
				Text:         "Open ended q",
				Type:         "open_text",
				TotalAnswers: 3,
				Distribution: map[string]int{"yes": 2, "no": 1},
			},
		},
		Leaderboard: []LeaderboardEntry{
			{Rank: 1, Name: "Alice", Score: 100},
			{Rank: 2, Name: "Bob", Score: 80},
			{Rank: 3, Name: "Charlie", Score: 70},
		},
	}

	var buf bytes.Buffer
	if err := GenerateReport(&buf, r); err != nil {
		t.Fatalf("GenerateReport: %v", err)
	}
	out := buf.Bytes()
	if !bytes.HasPrefix(out, []byte("%PDF-1.4")) {
		t.Error("output missing PDF header")
	}
	if !bytes.Contains(out, []byte("%%EOF")) {
		t.Error("output missing EOF marker")
	}
	if buf.Len() < 200 {
		t.Errorf("PDF output suspiciously small: %d bytes", buf.Len())
	}
}

func TestGenerateReport_WithChartImage(t *testing.T) {
	r := SessionReport{
		PollTitle: "with chart",
		Questions: []QuestionReport{
			{
				Text:         "Q",
				Type:         "single_choice",
				TotalAnswers: 1,
				Distribution: map[string]int{"a": 1},
			},
		},
		ChartImages: map[int]string{0: makePNGBase64(t, 10, 10)},
	}
	var buf bytes.Buffer
	if err := GenerateReport(&buf, r); err != nil {
		t.Fatalf("GenerateReport: %v", err)
	}
	if buf.Len() < 200 {
		t.Errorf("output too small: %d bytes", buf.Len())
	}
}

func TestGenerateReport_LeaderboardTruncatedToTen(t *testing.T) {
	board := make([]LeaderboardEntry, 15)
	for i := range board {
		board[i] = LeaderboardEntry{Rank: i + 1, Name: "P", Score: 100 - i}
	}
	r := SessionReport{PollTitle: "x", Leaderboard: board}
	var buf bytes.Buffer
	if err := GenerateReport(&buf, r); err != nil {
		t.Fatalf("GenerateReport: %v", err)
	}
}

func TestGenerateReport_LongTitleTruncated(t *testing.T) {
	long := strings.Repeat("ы", 200)
	r := SessionReport{PollTitle: long}
	var buf bytes.Buffer
	if err := GenerateReport(&buf, r); err != nil {
		t.Fatalf("GenerateReport: %v", err)
	}
}

func TestGenerateReport_EmptyMinimal(t *testing.T) {
	var buf bytes.Buffer
	if err := GenerateReport(&buf, SessionReport{PollTitle: "empty"}); err != nil {
		t.Fatalf("GenerateReport: %v", err)
	}
}
