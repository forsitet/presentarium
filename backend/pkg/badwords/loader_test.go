package badwords_test

import (
	"os"
	"path/filepath"
	"testing"

	"presentarium/pkg/badwords"
)

func TestLoadFromFile_Success(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "words.json")
	if err := os.WriteFile(path, []byte(`["foo","bar"]`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := badwords.LoadFromFile(path); err != nil {
		t.Fatalf("LoadFromFile error: %v", err)
	}
	// Restore the package-level dictionary used by the other tests.
	t.Cleanup(func() { badwords.Load([]string{"нецензурное", "shit", "fuck"}) })

	_, hasBad := badwords.Filter("foo and bar")
	if !hasBad {
		t.Error("expected the freshly-loaded words to be filtered")
	}
}

func TestLoadFromFile_MissingFile(t *testing.T) {
	t.Cleanup(func() { badwords.Load([]string{"нецензурное", "shit", "fuck"}) })
	if err := badwords.LoadFromFile(filepath.Join(t.TempDir(), "nope.json")); err == nil {
		t.Error("expected error reading missing file")
	}
}

func TestLoadFromFile_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { badwords.Load([]string{"нецензурное", "shit", "fuck"}) })
	if err := badwords.LoadFromFile(path); err == nil {
		t.Error("expected error parsing invalid JSON")
	}
}
