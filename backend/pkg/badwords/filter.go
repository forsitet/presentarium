// Package badwords provides content filtering functionality.
package badwords

import (
	"strings"
	"sync"

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

// Filter checks text against the badwords dictionary.
// Detected words are replaced with "***".
// Returns the filtered text and whether any bad words were found.
func Filter(text string) (filtered string, hasBadWords bool) {
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
