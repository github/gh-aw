//go:build !integration

package workflow

import (
	"strings"
	"testing"
)

func TestRenderStepMapFieldMultilineModes(t *testing.T) {
	for _, tt := range []struct {
		name           string
		field          string
		allowMultiline bool
		want           string
	}{
		{"ordered name stays inline", "name", false, "name: first\nsecond\n"},
		{"run uses block scalar", "run", true, "run: |\n        first\n        second\n"},
		{"extra field uses block scalar", "custom", true, "custom: |\n        first\n        second\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var out strings.Builder
			renderStepMapField(&out, tt.field, "first\nsecond", "    ", tt.allowMultiline)
			if got := out.String(); got != tt.want {
				t.Fatalf("renderStepMapField() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRenderStepMapFieldMapOrderingAndEnvQuoting(t *testing.T) {
	var out strings.Builder
	renderStepMapField(&out, "env", map[string]any{"Z": "z", "A": "value: text"}, "  ", false)
	want := "env:\n      A: \"value: text\"\n      Z: z\n"
	if got := out.String(); got != want {
		t.Fatalf("renderStepMapField() = %q, want %q", got, want)
	}
}
