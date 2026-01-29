//go:build !integration

package campaign

import "testing"

func TestParseRepoNameWithOwner(t *testing.T) {
	tests := []struct {
		name      string
		in        string
		wantOwner string
		wantRepo  string
		wantErr   bool
	}{
		{
			name:      "basic",
			in:        "githubnext/gh-aw",
			wantOwner: "githubnext",
			wantRepo:  "gh-aw",
		},
		{
			name:      "trims whitespace",
			in:        "  githubnext / gh-aw  ",
			wantOwner: "githubnext",
			wantRepo:  "gh-aw",
		},
		{
			name:      "strips leading at on owner",
			in:        "@mnkiefer/gh-aw",
			wantOwner: "mnkiefer",
			wantRepo:  "gh-aw",
		},
		{
			name:    "missing slash",
			in:      "githubnext",
			wantErr: true,
		},
		{
			name:    "empty owner",
			in:      "/repo",
			wantErr: true,
		},
		{
			name:    "empty repo",
			in:      "owner/",
			wantErr: true,
		},
		{
			name:    "empty",
			in:      "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner, repo, err := parseRepoNameWithOwner(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseRepoNameWithOwner(%q) expected error", tt.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseRepoNameWithOwner(%q) unexpected error: %v", tt.in, err)
			}
			if owner != tt.wantOwner || repo != tt.wantRepo {
				t.Fatalf("parseRepoNameWithOwner(%q) = (%q, %q), want (%q, %q)", tt.in, owner, repo, tt.wantOwner, tt.wantRepo)
			}
		})
	}
}
