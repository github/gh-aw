//go:build !integration

package campaign

import "testing"

func TestNormalizeProjectOwner(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "plain login unchanged",
			in:   "mnkiefer",
			want: "mnkiefer",
		},
		{
			name: "strip leading at",
			in:   "@mnkiefer",
			want: "mnkiefer",
		},
		{
			name: "keep special @me",
			in:   "@me",
			want: "@me",
		},
		{
			name: "trim whitespace",
			in:   "  @mnkiefer  ",
			want: "mnkiefer",
		},
		{
			name: "empty stays empty",
			in:   "",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeProjectOwner(tt.in)
			if got != tt.want {
				t.Fatalf("normalizeProjectOwner(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
