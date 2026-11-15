package workflow

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFirstForbiddenCharInModelToken(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		s    string
		want rune
	}{
		{name: "empty string returns zero", s: "", want: 0},
		{name: "all lowercase letters allowed", s: "abcxyz", want: 0},
		{name: "all uppercase letters allowed", s: "ABCXYZ", want: 0},
		{name: "digits allowed", s: "0123456789", want: 0},
		{name: "underscore allowed", s: "a_b", want: 0},
		{name: "dot allowed", s: "a.b", want: 0},
		{name: "hyphen allowed", s: "a-b", want: 0},
		{name: "slash allowed", s: "a/b", want: 0},
		{name: "mixed allowed characters", s: "gpt-4.1_turbo", want: 0},
		{name: "space is forbidden", s: "a b", want: ' '},
		{name: "colon is forbidden", s: "a:b", want: ':'},
		{name: "at sign is forbidden as first char", s: "@abc", want: '@'},
		{name: "forbidden char after allowed prefix", s: "model-v1@latest", want: '@'},
		{name: "unicode letter is forbidden", s: "modèle", want: 'è'},
		{name: "unicode emoji is forbidden", s: "abc🚀", want: '🚀'},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := firstForbiddenCharInModelToken(tt.s)

			assert.Equal(t, tt.want, got)
		})
	}
}

func FuzzFirstForbiddenCharInModelToken(f *testing.F) {
	seeds := []string{
		"",
		"abcXYZ019_.-",
		"a b",
		"a/b",
		"modèle",
		"abc🚀",
		"gpt-4.1_turbo",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, s string) {
		got := firstForbiddenCharInModelToken(s)
		if got == 0 {
			// No forbidden char found: every rune must be alnum, '_', '.', '/', or '-'.
			for _, r := range s {
				if !isAlpha(r) && !isDigit(r) && r != '_' && r != '.' && r != '/' && r != '-' {
					t.Fatalf("firstForbiddenCharInModelToken(%q) = 0, but rune %q is forbidden", s, r)
				}
			}
			return
		}
		// A forbidden rune must actually be present in s.
		found := false
		for _, r := range s {
			if r == got {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("firstForbiddenCharInModelToken(%q) = %q, which is not a rune in s", s, got)
		}
	})
}
