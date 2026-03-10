package normalize_test

import (
	"testing"

	"presentarium/pkg/normalize"
)

func TestText(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "trim and lowercase",
			input: "  Привет, мир!  ",
			want:  "привет мир",
		},
		{
			name:  "same result for different cases",
			input: "МОСКВА",
			want:  "москва",
		},
		{
			name:  "lowercase also normalizes",
			input: "москва",
			want:  "москва",
		},
		{
			name:  "removes punctuation",
			input: "Hello, world!",
			want:  "hello world",
		},
		{
			name:  "collapses multiple spaces",
			input: "one   two   three",
			want:  "one two three",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:  "only punctuation",
			input: "!@#$%",
			want:  "",
		},
		{
			name:  "mixed cyrillic and latin",
			input: "Привет World!",
			want:  "привет world",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := normalize.Text(tc.input)
			if got != tc.want {
				t.Errorf("normalize.Text(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestTextCaseEquality(t *testing.T) {
	// МОСКВА and москва should normalize to the same string.
	upper := normalize.Text("МОСКВА")
	lower := normalize.Text("москва")
	mixed := normalize.Text("Москва")
	if upper != lower || lower != mixed {
		t.Errorf("case-insensitive normalization failed: %q %q %q", upper, lower, mixed)
	}
}
