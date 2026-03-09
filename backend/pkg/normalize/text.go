// Package normalize provides text normalization utilities.
package normalize

import (
	"strings"
	"unicode"
)

// Text normalizes input text: trims whitespace, lowercases,
// and removes punctuation characters.
func Text(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ToLower(s)

	var b strings.Builder
	b.Grow(len(s))
	prevSpace := false
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			prevSpace = false
		} else if unicode.IsSpace(r) {
			if !prevSpace && b.Len() > 0 {
				b.WriteByte(' ')
			}
			prevSpace = true
		}
		// skip punctuation
	}
	return strings.TrimRight(b.String(), " ")
}
