package cli

import (
	"math"
	"testing"
)

func TestOutcomeMetadataInt(t *testing.T) {
	tests := []struct {
		name string
		raw  any
		want int
	}{
		{"integer", 4, 4},
		{"int64", int64(4), 4},
		{"float", float64(4), 4},
		{"numeric string", " 4 ", 4},
		{"numeric prefix", "4junk", 0},
		{"signed string", "+4", 0},
		{"fraction", 4.5, 0},
		{"fraction string", "4.5", 0},
		{"nan", math.NaN(), 0},
		{"infinity", math.Inf(1), 0},
		{"unsafe float", float64(9007199254740992), 0},
		{"unsafe int64", int64(9007199254740992), 0},
		{"out of range", "9223372036854775808", 0},
		{"zero", 0, 0},
		{"negative", -1, 0},
		{"missing", nil, 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := metadataInt(map[string]any{"id": test.raw}, "id")
			if got != test.want {
				t.Fatalf("metadataInt(%v) = %d, want %d", test.raw, got, test.want)
			}
		})
	}
	if metadataInt(nil, "id") != 0 {
		t.Fatal("missing metadata must not supply an execution identity")
	}
}
