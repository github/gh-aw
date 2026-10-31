package taint

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFastSameSite(t *testing.T) {
	t.Parallel()

	site := func(path string) Site {
		return Site{Kind: Kind("run"), Path: path, Line: 12}
	}
	direct := map[string][]string{"authored": {"compiled"}}
	siblings := map[string][]string{
		"jobs.source.steps[2]": {"jobs.destination.steps[5]"},
	}

	tests := []struct {
		name      string
		compiled  Site
		authored  Site
		locations map[string][]string
		want      bool
	}{
		{
			name:      "different site kinds do not match",
			compiled:  Site{Kind: Kind("script"), Path: "compiled", Line: 12},
			authored:  site("authored"),
			locations: direct,
		},
		{
			name:      "different source lines do not match",
			compiled:  Site{Kind: Kind("run"), Path: "compiled", Line: 13},
			authored:  site("authored"),
			locations: direct,
		},
		{
			name: "zero values without locations do not match",
		},
		{
			name:      "empty locations do not match",
			compiled:  site("compiled"),
			authored:  site("authored"),
			locations: map[string][]string{},
		},
		{
			name:      "mapping without target locations does not match",
			compiled:  site("compiled"),
			authored:  site("authored"),
			locations: map[string][]string{"authored": nil},
		},
		{
			name:      "exact mapped paths match",
			compiled:  site("compiled"),
			authored:  site("authored"),
			locations: direct,
			want:      true,
		},
		{
			name:      "javascript suffix is preserved by the mapping",
			compiled:  site("compiled.javascript"),
			authored:  site("authored.javascript"),
			locations: direct,
			want:      true,
		},
		{
			name:      "nested unicode javascript suffix is preserved",
			compiled:  site("compiled.javascript[2].入力"),
			authored:  site("authored.javascript[2].入力"),
			locations: direct,
			want:      true,
		},
		{
			name:      "other suffixes are rejected even when mapped paths agree",
			compiled:  site("compiled.run"),
			authored:  site("authored.run"),
			locations: direct,
		},
		{
			name:      "authored path must start with the source mapping",
			compiled:  site("compiled"),
			authored:  site("unrelated"),
			locations: direct,
		},
		{
			name:      "compiled path must agree with the mapped target",
			compiled:  site("unrelated"),
			authored:  site("authored"),
			locations: direct,
		},
		{
			name:      "later target can match after an unrelated target",
			compiled:  site("compiled"),
			authored:  site("authored"),
			locations: map[string][]string{"authored": {"unrelated", "compiled"}},
			want:      true,
		},
		{
			name:      "empty paths can map to each other",
			locations: map[string][]string{"": {""}},
			want:      true,
		},
		{
			name:      "site metadata beyond kind and line does not affect matching",
			compiled:  Site{Kind: Kind("run"), Path: "compiled", Line: 12, Uses: "compiled-action", Input: "compiled-input"},
			authored:  Site{Kind: Kind("run"), Path: "authored", Line: 12, Uses: "authored-action", Input: "authored-input"},
			locations: direct,
			want:      true,
		},
		{
			name:      "sibling steps match when offsets and suffixes agree",
			compiled:  site("jobs.destination.steps[8].run"),
			authored:  site("jobs.source.steps[5].run"),
			locations: siblings,
			want:      true,
		},
		{
			name:      "sibling steps can precede their mapped anchor",
			compiled:  site("jobs.destination.steps[3].run"),
			authored:  site("jobs.source.steps[0].run"),
			locations: siblings,
			want:      true,
		},
		{
			name:      "sibling steps with different offsets do not match",
			compiled:  site("jobs.destination.steps[9].run"),
			authored:  site("jobs.source.steps[5].run"),
			locations: siblings,
		},
		{
			name:      "sibling steps with different suffixes do not match",
			compiled:  site("jobs.destination.steps[8].env.VALUE"),
			authored:  site("jobs.source.steps[5].run"),
			locations: siblings,
		},
		{
			name:      "compiled sibling must belong to the target job",
			compiled:  site("jobs.unrelated.steps[8].run"),
			authored:  site("jobs.source.steps[5].run"),
			locations: siblings,
		},
		{
			name:      "authored sibling must belong to the source job",
			compiled:  site("jobs.destination.steps[8].run"),
			authored:  site("jobs.unrelated.steps[5].run"),
			locations: siblings,
		},
		{
			name:      "compiled sibling requires a valid step index",
			compiled:  site("jobs.destination.steps[invalid].run"),
			authored:  site("jobs.source.steps[5].run"),
			locations: siblings,
		},
		{
			name:      "authored sibling requires a closing step bracket",
			compiled:  site("jobs.destination.steps[8].run"),
			authored:  site("jobs.source.steps[5.run"),
			locations: siblings,
		},
		{
			name:      "target anchor requires a step location",
			compiled:  site("jobs.destination.steps[8].run"),
			authored:  site("jobs.source.steps[5].run"),
			locations: map[string][]string{"jobs.source.steps[2]": {"jobs.destination"}},
		},
		{
			name:      "source anchor requires a valid step index",
			compiled:  site("jobs.destination.steps[8].run"),
			authored:  site("jobs.source.steps[5].run"),
			locations: map[string][]string{"jobs.source.steps[invalid]": {"jobs.destination.steps[5]"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, fastSameSite(tt.compiled, tt.authored, tt.locations))
		})
	}
}
