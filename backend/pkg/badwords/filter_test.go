package badwords_test

import (
	"testing"

	"presentarium/pkg/badwords"
)

func init() {
	// Load a small test dictionary.
	badwords.Load([]string{"нецензурное", "shit", "fuck"})
}

func TestFilter_CleanText(t *testing.T) {
	filtered, hasBad := badwords.Filter("чистый текст")
	if hasBad {
		t.Error("expected hasBadWords=false for clean text")
	}
	if filtered != "чистый текст" {
		t.Errorf("expected unchanged text, got %q", filtered)
	}
}

func TestFilter_BadWord(t *testing.T) {
	filtered, hasBad := badwords.Filter("нецензурное слово")
	if !hasBad {
		t.Error("expected hasBadWords=true")
	}
	if filtered != "*** слово" {
		t.Errorf("expected '*** слово', got %q", filtered)
	}
}

func TestFilter_EnglishBadWord(t *testing.T) {
	filtered, hasBad := badwords.Filter("this is shit")
	if !hasBad {
		t.Error("expected hasBadWords=true")
	}
	if filtered != "this is ***" {
		t.Errorf("expected 'this is ***', got %q", filtered)
	}
}

func TestFilter_CaseInsensitive(t *testing.T) {
	_, hasBad := badwords.Filter("SHIT")
	if !hasBad {
		t.Error("expected hasBadWords=true for uppercase bad word")
	}
}

func TestFilter_EmptyText(t *testing.T) {
	filtered, hasBad := badwords.Filter("")
	if hasBad {
		t.Error("expected hasBadWords=false for empty string")
	}
	if filtered != "" {
		t.Errorf("expected empty string, got %q", filtered)
	}
}
