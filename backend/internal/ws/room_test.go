package ws

import (
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
)

// newTestRoom creates a Room for unit testing (bypasses Hub).
func newTestRoom() *Room {
	return newRoom("TEST01", uuid.New())
}

// newTestClient creates a minimal Client for testing without a real WebSocket.
func newTestClient(role ClientRole) *Client {
	return &Client{
		role: role,
		send: make(chan []byte, sendBufSize),
	}
}

// --- State machine ---

func TestRoom_InitialState(t *testing.T) {
	r := newTestRoom()
	if r.State() != StateWaiting {
		t.Errorf("initial state: want %q, got %q", StateWaiting, r.State())
	}
}

func TestRoom_SetState(t *testing.T) {
	r := newTestRoom()
	states := []RoomState{StateActive, StateShowingQuestion, StateShowingResults, StateFinished}
	for _, s := range states {
		r.SetState(s)
		if got := r.State(); got != s {
			t.Errorf("SetState(%q): got %q", s, got)
		}
	}
}

func TestRoom_TryFinishQuestion_Success(t *testing.T) {
	r := newTestRoom()
	qID := uuid.New()
	r.SetCurrentQuestion(&ActiveQuestion{ID: qID})
	r.SetState(StateShowingQuestion)

	if !r.TryFinishQuestion(qID) {
		t.Error("TryFinishQuestion: expected true for matching question ID")
	}
	if r.State() != StateShowingResults {
		t.Errorf("after finish: want state %q, got %q", StateShowingResults, r.State())
	}
	if r.CurrentQuestion() != nil {
		t.Error("after finish: currentQuestion should be nil")
	}
}

func TestRoom_TryFinishQuestion_WrongID(t *testing.T) {
	r := newTestRoom()
	r.SetCurrentQuestion(&ActiveQuestion{ID: uuid.New()})

	if r.TryFinishQuestion(uuid.New()) {
		t.Error("TryFinishQuestion: expected false for mismatched question ID")
	}
}

func TestRoom_TryFinishQuestion_NoActiveQuestion(t *testing.T) {
	r := newTestRoom()
	if r.TryFinishQuestion(uuid.New()) {
		t.Error("TryFinishQuestion: expected false when no active question")
	}
}

func TestRoom_TryFinishQuestion_DoubleFin(t *testing.T) {
	r := newTestRoom()
	qID := uuid.New()
	r.SetCurrentQuestion(&ActiveQuestion{ID: qID})

	if !r.TryFinishQuestion(qID) {
		t.Fatal("first TryFinishQuestion: expected true")
	}
	// Second call should return false — prevents double-broadcast.
	if r.TryFinishQuestion(qID) {
		t.Error("second TryFinishQuestion: expected false (double-finish prevented)")
	}
}

// --- Answer count ---

func TestRoom_AnswerCount(t *testing.T) {
	r := newTestRoom()
	if r.AnswerCount() != 0 {
		t.Error("initial answer count should be 0")
	}
	n1, avg1 := r.IncrementAnswerCount(1000)
	if n1 != 1 {
		t.Errorf("after first increment: want 1, got %d", n1)
	}
	if avg1 != 1000 {
		t.Errorf("avg after first increment: want 1000, got %d", avg1)
	}
	n2, avg2 := r.IncrementAnswerCount(3000)
	if n2 != 2 {
		t.Errorf("after second increment: want 2, got %d", n2)
	}
	if avg2 != 2000 {
		t.Errorf("avg after second increment: want 2000, got %d", avg2)
	}
}

func TestRoom_ResetAnswerCount(t *testing.T) {
	r := newTestRoom()
	r.IncrementAnswerCount(500)
	r.IncrementAnswerCount(1500)
	r.ResetAnswerCount()
	if r.AnswerCount() != 0 {
		t.Errorf("after reset: want 0, got %d", r.AnswerCount())
	}
}

// --- Word cloud ---

// addPhrases is a small test helper that mirrors what conduct_service does:
// caller computes a normalized key (here: just lowercased) and passes it
// alongside the original display.
func addPhrases(r *Room, phrases ...string) {
	for _, p := range phrases {
		key := strings.ToLower(strings.TrimSpace(p))
		r.AddWordCloudPhrase(key, strings.TrimSpace(p))
	}
}

func TestRoom_AddWordCloudPhrase_Basic(t *testing.T) {
	r := newTestRoom()
	addPhrases(r, "hello", "world", "hello")
	words := r.WordCloudTopWords(10)

	freq := map[string]int{}
	for _, w := range words {
		freq[w.Text] = w.Count
	}
	if freq["hello"] != 2 {
		t.Errorf("phrase 'hello': want count 2, got %d", freq["hello"])
	}
	if freq["world"] != 1 {
		t.Errorf("phrase 'world': want count 1, got %d", freq["world"])
	}
}

func TestRoom_AddWordCloudPhrase_KeepsMultiWordPhrasesIntact(t *testing.T) {
	r := newTestRoom()
	addPhrases(r, "искусственный интеллект", "машинное обучение", "искусственный интеллект")
	words := r.WordCloudTopWords(10)

	if len(words) != 2 {
		t.Fatalf("expected 2 phrases, got %d (%v)", len(words), words)
	}
	freq := map[string]int{}
	for _, w := range words {
		freq[w.Text] = w.Count
	}
	if freq["искусственный интеллект"] != 2 {
		t.Errorf("phrase 'искусственный интеллект': want count 2, got %d", freq["искусственный интеллект"])
	}
	if freq["машинное обучение"] != 1 {
		t.Errorf("phrase 'машинное обучение': want count 1, got %d", freq["машинное обучение"])
	}
}

func TestRoom_AddWordCloudPhrase_CaseInsensitiveAggregation(t *testing.T) {
	r := newTestRoom()
	// Different casings of the same word should aggregate, and the FIRST
	// submission's display is the one that wins.
	r.AddWordCloudPhrase("go", "Go")
	r.AddWordCloudPhrase("go", "GO")
	r.AddWordCloudPhrase("go", "go")

	words := r.WordCloudTopWords(10)
	if len(words) != 1 {
		t.Fatalf("expected 1 aggregated phrase, got %d (%v)", len(words), words)
	}
	if words[0].Text != "Go" {
		t.Errorf("display: want first-seen 'Go', got %q", words[0].Text)
	}
	if words[0].Count != 3 {
		t.Errorf("count: want 3, got %d", words[0].Count)
	}
}

func TestRoom_WordCloudTopWords_Order(t *testing.T) {
	r := newTestRoom()
	addPhrases(r, "a", "b", "b", "c", "c", "c")
	words := r.WordCloudTopWords(10)

	if len(words) != 3 {
		t.Fatalf("expected 3 words, got %d", len(words))
	}
	// Should be sorted by count descending: c(3), b(2), a(1).
	if words[0].Text != "c" || words[0].Count != 3 {
		t.Errorf("top word: want c(3), got %s(%d)", words[0].Text, words[0].Count)
	}
	if words[1].Text != "b" || words[1].Count != 2 {
		t.Errorf("second word: want b(2), got %s(%d)", words[1].Text, words[1].Count)
	}
	if words[2].Text != "a" || words[2].Count != 1 {
		t.Errorf("third word: want a(1), got %s(%d)", words[2].Text, words[2].Count)
	}
}

func TestRoom_WordCloudTopWords_Limit(t *testing.T) {
	r := newTestRoom()
	addPhrases(r, "a", "b", "b", "c", "c", "c", "d", "d", "d", "d", "e", "e", "e", "e", "e")
	words := r.WordCloudTopWords(3)
	if len(words) != 3 {
		t.Errorf("top 3 limit: want 3, got %d", len(words))
	}
}

func TestRoom_WordCloudTopWords_Empty(t *testing.T) {
	r := newTestRoom()
	words := r.WordCloudTopWords(10)
	if len(words) != 0 {
		t.Errorf("empty word cloud: want 0 words, got %d", len(words))
	}
}

func TestRoom_ResetAnswerCount_ClearsWordCloud(t *testing.T) {
	r := newTestRoom()
	addPhrases(r, "hello", "world")
	r.ResetAnswerCount() // should also reset wordCloudPhrases
	words := r.WordCloudTopWords(10)
	if len(words) != 0 {
		t.Errorf("after reset: word cloud should be empty, got %d words", len(words))
	}
}

func TestRoom_SetWordCloudPhrases_RebuildsFromSnapshot(t *testing.T) {
	r := newTestRoom()
	r.SetWordCloudPhrases(map[string]struct {
		Display string
		Count   int
	}{
		"go": {Display: "Go", Count: 3},
		"ai": {Display: "AI", Count: 1},
	})
	words := r.WordCloudTopWords(10)
	if len(words) != 2 {
		t.Fatalf("rebuild: want 2 phrases, got %d", len(words))
	}
	if words[0].Text != "Go" || words[0].Count != 3 {
		t.Errorf("rebuild order/display: want Go(3) first, got %s(%d)", words[0].Text, words[0].Count)
	}
}

// --- Brainstorm state ---

func TestRoom_InitBrainstorm(t *testing.T) {
	r := newTestRoom()
	r.InitBrainstorm()
	if r.BrainstormPhase() != "collecting" {
		t.Errorf("after InitBrainstorm: want phase 'collecting', got %q", r.BrainstormPhase())
	}
}

func TestRoom_SetBrainstormPhase(t *testing.T) {
	r := newTestRoom()
	r.InitBrainstorm()

	phases := []string{"collecting", "voting", "results"}
	for _, p := range phases {
		r.SetBrainstormPhase(p)
		if got := r.BrainstormPhase(); got != p {
			t.Errorf("SetBrainstormPhase(%q): got %q", p, got)
		}
	}
}

func TestRoom_BrainstormIdeaCount(t *testing.T) {
	r := newTestRoom()
	r.InitBrainstorm()
	pid := uuid.New()

	if r.BrainstormIdeaCount(pid) != 0 {
		t.Error("initial idea count should be 0")
	}
	n1 := r.IncrementBrainstormIdeaCount(pid)
	if n1 != 1 {
		t.Errorf("after first increment: want 1, got %d", n1)
	}
	n2 := r.IncrementBrainstormIdeaCount(pid)
	if n2 != 2 {
		t.Errorf("after second increment: want 2, got %d", n2)
	}

	// Different participant → independent counter.
	other := uuid.New()
	if r.BrainstormIdeaCount(other) != 0 {
		t.Error("different participant should have 0 ideas")
	}
}

func TestRoom_BrainstormVoteCount(t *testing.T) {
	r := newTestRoom()
	r.InitBrainstorm()
	pid := uuid.New()

	if r.BrainstormVoteCount(pid) != 0 {
		t.Error("initial vote count should be 0")
	}
	for i := 1; i <= 3; i++ {
		n := r.IncrementBrainstormVoteCount(pid)
		if n != i {
			t.Errorf("vote count after %d increments: want %d, got %d", i, i, n)
		}
	}
}

// --- Client and participant count ---

func TestRoom_ParticipantCount(t *testing.T) {
	r := newTestRoom()
	org := newTestClient(RoleOrganizer)
	p1 := newTestClient(RoleParticipant)
	p2 := newTestClient(RoleParticipant)

	r.AddClient(org)
	r.AddClient(p1)
	r.AddClient(p2)

	if got := r.ParticipantCount(); got != 2 {
		t.Errorf("ParticipantCount: want 2, got %d", got)
	}
	if got := r.ClientCount(); got != 3 {
		t.Errorf("ClientCount: want 3, got %d", got)
	}
}

func TestRoom_RemoveClient(t *testing.T) {
	r := newTestRoom()
	p := newTestClient(RoleParticipant)
	r.AddClient(p)
	r.RemoveClient(p)

	if r.ClientCount() != 0 {
		t.Errorf("after remove: want 0 clients, got %d", r.ClientCount())
	}
}

func TestRoom_OrganizerTracked(t *testing.T) {
	r := newTestRoom()
	org := newTestClient(RoleOrganizer)
	r.AddClient(org)

	if r.Organizer() != org {
		t.Error("Organizer() should return the organizer client")
	}

	r.RemoveClient(org)
	if r.Organizer() != nil {
		t.Error("after removing organizer: Organizer() should return nil")
	}
}

// --- Concurrency safety ---

func TestRoom_ConcurrentAnswerCount(t *testing.T) {
	r := newTestRoom()
	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			r.IncrementAnswerCount(100)
		}()
	}
	wg.Wait()
	if got := r.AnswerCount(); got != goroutines {
		t.Errorf("concurrent increment: want %d, got %d", goroutines, got)
	}
}

func TestRoom_ConcurrentWordCloud(t *testing.T) {
	r := newTestRoom()
	const goroutines = 20
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			r.AddWordCloudPhrase("concurrent", "concurrent")
		}()
	}
	wg.Wait()

	words := r.WordCloudTopWords(10)
	if len(words) != 1 || words[0].Count != goroutines {
		t.Errorf("concurrent word cloud: want count=%d, got words=%v", goroutines, words)
	}
}

// --- Active presentation ---

func makeActivePresentation(slideCount int) *ActivePresentation {
	slides := make([]SlideInfo, slideCount)
	for i := 0; i < slideCount; i++ {
		slides[i] = SlideInfo{
			ID:       uuid.New(),
			Position: i + 1,
			ImageURL: "https://cdn.example/slide.webp",
			Width:    1920,
			Height:   1080,
		}
	}
	return &ActivePresentation{
		ID:              uuid.New(),
		Title:           "Deck",
		SlideCount:      slideCount,
		CurrentPosition: 1,
		Slides:          slides,
	}
}

func TestRoom_ActivePresentation_InitiallyNil(t *testing.T) {
	r := newTestRoom()
	if r.ActivePresentation() != nil {
		t.Error("expected nil active presentation on a fresh room")
	}
}

func TestRoom_SetActivePresentation_Roundtrip(t *testing.T) {
	r := newTestRoom()
	active := makeActivePresentation(3)
	r.SetActivePresentation(active)

	got := r.ActivePresentation()
	if got == nil {
		t.Fatal("ActivePresentation returned nil after SetActivePresentation")
	}
	if got.ID != active.ID {
		t.Errorf("ID mismatch: got %s want %s", got.ID, active.ID)
	}
	if got.SlideCount != 3 {
		t.Errorf("SlideCount=%d want 3", got.SlideCount)
	}
	if len(got.Slides) != 3 {
		t.Errorf("len(Slides)=%d want 3", len(got.Slides))
	}
}

func TestRoom_SetActivePresentation_DefensiveCopy(t *testing.T) {
	r := newTestRoom()
	active := makeActivePresentation(2)
	r.SetActivePresentation(active)

	// Mutating the caller's slice must not affect stored state.
	active.Slides[0].Position = 999
	got := r.ActivePresentation()
	if got.Slides[0].Position != 1 {
		t.Errorf("room state leaked through input slice: got Position=%d", got.Slides[0].Position)
	}

	// Mutating the returned snapshot must not affect stored state either.
	got.Slides[1].Position = 777
	got2 := r.ActivePresentation()
	if got2.Slides[1].Position != 2 {
		t.Errorf("room state leaked through output slice: got Position=%d", got2.Slides[1].Position)
	}
}

func TestRoom_ClearActivePresentation(t *testing.T) {
	r := newTestRoom()
	r.SetActivePresentation(makeActivePresentation(1))
	r.ClearActivePresentation()
	if r.ActivePresentation() != nil {
		t.Error("expected nil after ClearActivePresentation")
	}
}

func TestRoom_SetActivePresentation_NilClears(t *testing.T) {
	r := newTestRoom()
	r.SetActivePresentation(makeActivePresentation(1))
	r.SetActivePresentation(nil)
	if r.ActivePresentation() != nil {
		t.Error("SetActivePresentation(nil) should clear")
	}
}

func TestRoom_SetSlidePosition_ValidBounds(t *testing.T) {
	r := newTestRoom()
	r.SetActivePresentation(makeActivePresentation(5))

	cases := []int{1, 3, 5}
	for _, pos := range cases {
		if !r.SetSlidePosition(pos) {
			t.Errorf("SetSlidePosition(%d): want true", pos)
			continue
		}
		if got := r.ActivePresentation().CurrentPosition; got != pos {
			t.Errorf("CurrentPosition=%d want %d", got, pos)
		}
	}
}

func TestRoom_SetSlidePosition_OutOfRange(t *testing.T) {
	r := newTestRoom()
	r.SetActivePresentation(makeActivePresentation(3))

	for _, bad := range []int{0, -1, 4, 100} {
		if r.SetSlidePosition(bad) {
			t.Errorf("SetSlidePosition(%d): want false (out of range)", bad)
		}
	}
	// Position must stay at the initial 1.
	if got := r.ActivePresentation().CurrentPosition; got != 1 {
		t.Errorf("CurrentPosition changed despite invalid inputs: got %d", got)
	}
}

func TestRoom_SetSlidePosition_NoActivePresentation(t *testing.T) {
	r := newTestRoom()
	if r.SetSlidePosition(1) {
		t.Error("SetSlidePosition should return false when no presentation is active")
	}
}
