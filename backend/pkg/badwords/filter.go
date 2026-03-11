// Package badwords provides content filtering functionality.
package badwords

import (
	"encoding/json"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"presentarium/pkg/normalize"
)

var (
	mu         sync.RWMutex
	dictionary = map[string]struct{}{}
)

// Load replaces the current dictionary with the provided word list.
func Load(words []string) {
	mu.Lock()
	defer mu.Unlock()
	dictionary = make(map[string]struct{}, len(words))
	for _, w := range words {
		dictionary[normalize.Text(w)] = struct{}{}
	}
}

// LoadFromFile reads a JSON array of words from the given file path and loads them.
// Returns an error if the file cannot be read or parsed.
func LoadFromFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var words []string
	if err := json.Unmarshal(data, &words); err != nil {
		return err
	}
	Load(words)
	return nil
}

// WatchFile starts a background goroutine that polls the file at path every interval.
// When the file's modification time changes, the dictionary is reloaded automatically.
func WatchFile(path string, interval time.Duration) {
	info, err := os.Stat(path)
	var lastMod time.Time
	if err == nil {
		lastMod = info.ModTime()
	}

	go func() {
		for {
			time.Sleep(interval)
			info, err := os.Stat(path)
			if err != nil {
				continue
			}
			if info.ModTime().After(lastMod) {
				lastMod = info.ModTime()
				if err := LoadFromFile(path); err != nil {
					slog.Warn("badwords hot reload failed", "error", err)
				} else {
					slog.Info("badwords dictionary reloaded")
				}
			}
		}
	}()
}

// Filter checks text against the badwords dictionary.
// Detected words are replaced with "***".
// Returns the filtered text and whether any bad words were found.
// Also detects bypass attempts where letters are separated by spaces.
func Filter(text string) (filtered string, hasBadWords bool) {
	// Bypass detection: check if stripping all spaces produces a single bad word.
	stripped := normalize.Text(strings.ReplaceAll(text, " ", ""))
	mu.RLock()
	_, isBypass := dictionary[stripped]
	mu.RUnlock()
	if isBypass {
		return "***", true
	}

	words := strings.Fields(text)
	mu.RLock()
	defer mu.RUnlock()

	result := make([]string, 0, len(words))
	for _, w := range words {
		normalized := normalize.Text(w)
		if _, bad := dictionary[normalized]; bad {
			result = append(result, "***")
			hasBadWords = true
		} else {
			result = append(result, w)
		}
	}
	return strings.Join(result, " "), hasBadWords
}
