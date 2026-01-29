//go:build !integration

package campaign

import "testing"

func TestParseProjectURL(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantScope string
		wantOwner string
		wantNum   int
		wantErr   bool
	}{
		{
			name:      "org project",
			input:     "https://github.com/orgs/githubnext/projects/123",
			wantScope: "orgs",
			wantOwner: "githubnext",
			wantNum:   123,
		},
		{
			name:      "user project",
			input:     "https://github.com/users/mnkiefer/projects/7",
			wantScope: "users",
			wantOwner: "mnkiefer",
			wantNum:   7,
		},
		{
			name:    "invalid url",
			input:   "https://github.com/githubnext/projects/123",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseProjectURL(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("parseProjectURL error: %v", err)
			}
			if got.scope != tt.wantScope || got.ownerLogin != tt.wantOwner || got.projectNumber != tt.wantNum {
				t.Fatalf("parseProjectURL(%q) = %+v, want scope=%q owner=%q number=%d", tt.input, got, tt.wantScope, tt.wantOwner, tt.wantNum)
			}
		})
	}
}
