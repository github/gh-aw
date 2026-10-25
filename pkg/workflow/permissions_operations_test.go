//go:build !integration

package workflow

import (
	"strings"
	"testing"
)

func TestNewPermissions(t *testing.T) {
	p := NewPermissions()
	if p == nil {
		t.Fatal("NewPermissions() returned nil")
	}
	if p.shorthand != "" {
		t.Errorf("expected empty shorthand, got %q", p.shorthand)
	}
	if p.permissions == nil {
		t.Error("expected permissions map to be initialized")
	}
	if len(p.permissions) != 0 {
		t.Errorf("expected empty permissions map, got %d entries", len(p.permissions))
	}
}

func TestNewPermissionsShorthand(t *testing.T) {
	tests := []struct {
		name      string
		fn        func() *Permissions
		shorthand string
	}{
		{"read-all", NewPermissionsReadAll, "read-all"},
		{"write-all", NewPermissionsWriteAll, "write-all"},
		{"none", NewPermissionsNone, "none"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := tt.fn()
			if p.shorthand != tt.shorthand {
				t.Errorf("expected shorthand %q, got %q", tt.shorthand, p.shorthand)
			}
		})
	}
}

func TestNewPermissionsFromMap(t *testing.T) {
	perms := map[PermissionScope]PermissionLevel{
		PermissionContents: PermissionRead,
		PermissionIssues:   PermissionWrite,
	}

	p := NewPermissionsFromMap(perms)
	if p.shorthand != "" {
		t.Errorf("expected empty shorthand, got %q", p.shorthand)
	}
	if len(p.permissions) != 2 {
		t.Errorf("expected 2 permissions, got %d", len(p.permissions))
	}

	level, exists := p.Get(PermissionContents)
	if !exists || level != PermissionRead {
		t.Errorf("expected contents: read, got %v (exists: %v)", level, exists)
	}

	level, exists = p.Get(PermissionIssues)
	if !exists || level != PermissionWrite {
		t.Errorf("expected issues: write, got %v (exists: %v)", level, exists)
	}
}

func TestPermissionsSet(t *testing.T) {
	p := NewPermissions()
	p.Set(PermissionContents, PermissionRead)

	level, exists := p.Get(PermissionContents)
	if !exists || level != PermissionRead {
		t.Errorf("expected contents: read, got %v (exists: %v)", level, exists)
	}

	// Test setting on shorthand converts to map
	p2 := NewPermissionsReadAll()
	p2.Set(PermissionIssues, PermissionWrite)
	if p2.shorthand != "" {
		t.Error("expected shorthand to be cleared after Set")
	}
	level, exists = p2.Get(PermissionIssues)
	if !exists || level != PermissionWrite {
		t.Errorf("expected issues: write, got %v (exists: %v)", level, exists)
	}

	p3 := NewPermissionsAllRead()
	p3.Set(PermissionCopilotRequests, PermissionWrite)
	if _, exists := p3.Get(PermissionIdToken); exists {
		t.Error("expected id-token to be excluded when converting all: read to explicit map")
	}
	if yaml := p3.RenderToYAML(); strings.Contains(yaml, "id-token: read") {
		t.Errorf("RenderToYAML() should not contain id-token: read, got:\n%s", yaml)
	}
}

// TestPermissionsSetPreservesShorthandPermissions verifies that calling Set() on a Permissions
// with a shorthand value (read-all, write-all, none) preserves the shorthand-implied permissions
// instead of discarding them. This is the regression test for the bug where adding
// copilot-requests: write to a read-all workflow dropped all other read permissions.
func TestPermissionsSetPreservesShorthandPermissions(t *testing.T) {
	tests := []struct {
		name            string
		base            *Permissions
		setScope        PermissionScope
		setLevel        PermissionLevel
		checkScope      PermissionScope
		wantPreserved   PermissionLevel
		wantPreservedOK bool
	}{
		{
			name:            "read-all: adding write scope preserves contents: read",
			base:            NewPermissionsReadAll(),
			setScope:        PermissionCopilotRequests,
			setLevel:        PermissionWrite,
			checkScope:      PermissionContents,
			wantPreserved:   PermissionRead,
			wantPreservedOK: true,
		},
		{
			name:            "read-all: adding write scope preserves issues: read",
			base:            NewPermissionsReadAll(),
			setScope:        PermissionCopilotRequests,
			setLevel:        PermissionWrite,
			checkScope:      PermissionIssues,
			wantPreserved:   PermissionRead,
			wantPreservedOK: true,
		},
		{
			name:            "read-all: adding write scope preserves pull-requests: read",
			base:            NewPermissionsReadAll(),
			setScope:        PermissionCopilotRequests,
			setLevel:        PermissionWrite,
			checkScope:      PermissionPullRequests,
			wantPreserved:   PermissionRead,
			wantPreservedOK: true,
		},
		{
			name:            "write-all: adding extra write scope preserves contents: write",
			base:            NewPermissionsWriteAll(),
			setScope:        PermissionCopilotRequests,
			setLevel:        PermissionWrite,
			checkScope:      PermissionContents,
			wantPreserved:   PermissionWrite,
			wantPreservedOK: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.base.Set(tt.setScope, tt.setLevel)

			// Verify the newly set scope
			gotNew, existsNew := tt.base.Get(tt.setScope)
			if !existsNew || gotNew != tt.setLevel {
				t.Errorf("Set scope %s: got %v (exists=%v), want %v", tt.setScope, gotNew, existsNew, tt.setLevel)
			}

			// Verify the preserved scope
			gotPrev, existsPrev := tt.base.Get(tt.checkScope)
			if existsPrev != tt.wantPreservedOK || gotPrev != tt.wantPreserved {
				t.Errorf("Preserved scope %s: got %v (exists=%v), want %v (exists=%v)",
					tt.checkScope, gotPrev, existsPrev, tt.wantPreserved, tt.wantPreservedOK)
			}
		})
	}
}

func TestPermissionsScopeExpansion(t *testing.T) {
	shorthands := map[PermissionLevel]string{
		PermissionRead:  "read-all",
		PermissionWrite: "write-all",
		PermissionNone:  "none",
	}
	operations := []struct {
		name   string
		expand func(*Permissions, PermissionLevel)
	}{
		{"set shorthand", func(p *Permissions, level PermissionLevel) {
			p.shorthand = shorthands[level]
			p.Set(PermissionCopilotRequests, PermissionWrite)
		}},
		{"set all", func(p *Permissions, level PermissionLevel) {
			p.hasAll, p.allLevel = true, level
			p.Set(PermissionCopilotRequests, PermissionWrite)
		}},
		{"merge receiver all", func(p *Permissions, level PermissionLevel) {
			p.hasAll, p.allLevel = true, level
			p.Merge(NewPermissions())
		}},
		{"merge other all", func(p *Permissions, level PermissionLevel) {
			p.Merge(&Permissions{hasAll: true, allLevel: level})
		}},
		{"merge other shorthand", func(p *Permissions, level PermissionLevel) {
			p.Merge(&Permissions{shorthand: shorthands[level]})
		}},
	}

	for _, operation := range operations {
		for _, level := range []PermissionLevel{PermissionRead, PermissionWrite, PermissionNone} {
			t.Run(operation.name+"/"+string(level), func(t *testing.T) {
				p := NewPermissionsFromMap(map[PermissionScope]PermissionLevel{
					PermissionContents: PermissionNone,
					PermissionIssues:   PermissionWrite,
				})
				operation.expand(p, level)

				if p.shorthand != "" || p.hasAll || p.allLevel != "" {
					t.Errorf("expected explicit permissions, got shorthand=%q hasAll=%v allLevel=%q", p.shorthand, p.hasAll, p.allLevel)
				}
				for _, scope := range GetAllPermissionScopes() {
					want, wantExists := level, true
					switch scope {
					case PermissionContents:
						if operation.name != "merge other all" {
							want = PermissionNone
						}
					case PermissionIssues:
						want = PermissionWrite
					default:
						if (scope == PermissionIdToken && level == PermissionRead) ||
							(operation.name == "merge other shorthand" && level == PermissionNone) {
							want, wantExists = "", false
						}
					}
					if got, exists := p.GetExplicit(scope); got != want || exists != wantExists {
						t.Errorf("%s: got %q (exists=%v), want %q (exists=%v)", scope, got, exists, want, wantExists)
					}
				}
			})
		}
	}

	t.Run("preserve explicit id-token during read expansion", func(t *testing.T) {
		for _, operation := range operations {
			t.Run(operation.name, func(t *testing.T) {
				p := NewPermissionsFromMap(map[PermissionScope]PermissionLevel{
					PermissionIdToken: PermissionWrite,
				})
				operation.expand(p, PermissionRead)
				if got, exists := p.GetExplicit(PermissionIdToken); !exists || got != PermissionWrite {
					t.Errorf("expected explicit id-token: write, got %q (exists=%v)", got, exists)
				}
			})
		}
	})
}

func TestPermissionsGet(t *testing.T) {
	tests := []struct {
		name        string
		permissions *Permissions
		scope       PermissionScope
		wantLevel   PermissionLevel
		wantExists  bool
	}{
		{
			name:        "read-all shorthand",
			permissions: NewPermissionsReadAll(),
			scope:       PermissionContents,
			wantLevel:   PermissionRead,
			wantExists:  true,
		},
		{
			name:        "write-all shorthand",
			permissions: NewPermissionsWriteAll(),
			scope:       PermissionIssues,
			wantLevel:   PermissionWrite,
			wantExists:  true,
		},
		{
			name:        "none shorthand",
			permissions: NewPermissionsNone(),
			scope:       PermissionContents,
			wantLevel:   PermissionNone,
			wantExists:  true,
		},
		{
			name: "specific permission",
			permissions: NewPermissionsFromMap(map[PermissionScope]PermissionLevel{
				PermissionContents: PermissionRead,
			}),
			scope:      PermissionContents,
			wantLevel:  PermissionRead,
			wantExists: true,
		},
		{
			name:        "non-existent permission",
			permissions: NewPermissions(),
			scope:       PermissionContents,
			wantLevel:   "",
			wantExists:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			level, exists := tt.permissions.Get(tt.scope)
			if exists != tt.wantExists {
				t.Errorf("Get() exists = %v, want %v", exists, tt.wantExists)
			}
			if level != tt.wantLevel {
				t.Errorf("Get() level = %v, want %v", level, tt.wantLevel)
			}
		})
	}
}

func TestPermissionsMerge(t *testing.T) {
	tests := []struct {
		name   string
		base   *Permissions
		merge  *Permissions
		want   map[PermissionScope]PermissionLevel
		wantSH string
	}{
		// Map-to-Map merges
		{
			name:  "merge two maps - write overrides read",
			base:  NewPermissionsFromMap(map[PermissionScope]PermissionLevel{PermissionContents: PermissionRead}),
			merge: NewPermissionsFromMap(map[PermissionScope]PermissionLevel{PermissionContents: PermissionWrite}),
			want:  map[PermissionScope]PermissionLevel{PermissionContents: PermissionWrite},
		},
		{
			name:  "merge two maps - read doesn't override write",
			base:  NewPermissionsFromMap(map[PermissionScope]PermissionLevel{PermissionContents: PermissionWrite}),
			merge: NewPermissionsFromMap(map[PermissionScope]PermissionLevel{PermissionContents: PermissionRead}),
			want:  map[PermissionScope]PermissionLevel{PermissionContents: PermissionWrite},
		},
		{
			name:  "merge two maps - different scopes",
			base:  NewPermissionsFromMap(map[PermissionScope]PermissionLevel{PermissionContents: PermissionRead}),
			merge: NewPermissionsFromMap(map[PermissionScope]PermissionLevel{PermissionIssues: PermissionWrite}),
			want: map[PermissionScope]PermissionLevel{
				PermissionContents: PermissionRead,
				PermissionIssues:   PermissionWrite,
			},
		},
		{
			name: "merge two maps - multiple scopes with conflicts",
			base: NewPermissionsFromMap(map[PermissionScope]PermissionLevel{
				PermissionContents:     PermissionRead,
				PermissionIssues:       PermissionWrite,
				PermissionPullRequests: PermissionRead,
			}),
			merge: NewPermissionsFromMap(map[PermissionScope]PermissionLevel{
				PermissionContents:    PermissionWrite,
				PermissionIssues:      PermissionRead,
				PermissionDiscussions: PermissionWrite,
			}),
			want: map[PermissionScope]PermissionLevel{
				PermissionContents:     PermissionWrite, // write wins
				PermissionIssues:       PermissionWrite, // write preserved
				PermissionPullRequests: PermissionRead,  // kept from base
				PermissionDiscussions:  PermissionWrite, // added from merge
			},
		},
		{
			name:  "merge two maps - none overrides read",
			base:  NewPermissionsFromMap(map[PermissionScope]PermissionLevel{PermissionContents: PermissionRead}),
			merge: NewPermissionsFromMap(map[PermissionScope]PermissionLevel{PermissionContents: PermissionNone}),
			want:  map[PermissionScope]PermissionLevel{PermissionContents: PermissionRead},
		},
		{
			name:  "merge two maps - none overrides none",
			base:  NewPermissionsFromMap(map[PermissionScope]PermissionLevel{PermissionContents: PermissionNone}),
			merge: NewPermissionsFromMap(map[PermissionScope]PermissionLevel{PermissionContents: PermissionNone}),
			want:  map[PermissionScope]PermissionLevel{PermissionContents: PermissionNone},
		},
		{
			name:  "merge two maps - write overrides none",
			base:  NewPermissionsFromMap(map[PermissionScope]PermissionLevel{PermissionContents: PermissionNone}),
			merge: NewPermissionsFromMap(map[PermissionScope]PermissionLevel{PermissionContents: PermissionWrite}),
			want:  map[PermissionScope]PermissionLevel{PermissionContents: PermissionWrite},
		},
		{
			name: "merge two maps - all permission scopes",
			base: NewPermissionsFromMap(map[PermissionScope]PermissionLevel{
				PermissionActions:     PermissionRead,
				PermissionChecks:      PermissionRead,
				PermissionContents:    PermissionRead,
				PermissionDeployments: PermissionRead,
				PermissionDiscussions: PermissionRead,
				PermissionIssues:      PermissionRead,
				PermissionPackages:    PermissionRead,
			}),
			merge: NewPermissionsFromMap(map[PermissionScope]PermissionLevel{
				PermissionPages:          PermissionWrite,
				PermissionPullRequests:   PermissionWrite,
				PermissionRepositoryProj: PermissionWrite,
				PermissionSecurityEvents: PermissionWrite,
				PermissionStatuses:       PermissionWrite,
				PermissionModels:         PermissionWrite,
			}),
			want: map[PermissionScope]PermissionLevel{
				PermissionActions:        PermissionRead,
				PermissionChecks:         PermissionRead,
				PermissionContents:       PermissionRead,
				PermissionDeployments:    PermissionRead,
				PermissionDiscussions:    PermissionRead,
				PermissionIssues:         PermissionRead,
				PermissionPackages:       PermissionRead,
				PermissionPages:          PermissionWrite,
				PermissionPullRequests:   PermissionWrite,
				PermissionRepositoryProj: PermissionWrite,
				PermissionSecurityEvents: PermissionWrite,
				PermissionStatuses:       PermissionWrite,
				PermissionModels:         PermissionWrite,
			},
		},

		// Shorthand-to-Shorthand merges
		{
			name:   "merge shorthand - write-all wins over read-all",
			base:   NewPermissionsReadAll(),
			merge:  NewPermissionsWriteAll(),
			wantSH: "write-all",
		},
		{
			name:   "merge shorthand - write-all wins over read",
			base:   NewPermissionsReadAll(),
			merge:  NewPermissionsWriteAll(),
			wantSH: "write-all",
		},
		{
			name:   "merge shorthand - write-all wins over write",
			base:   NewPermissionsWriteAll(),
			merge:  NewPermissionsWriteAll(),
			wantSH: "write-all",
		},
		{
			name:   "merge shorthand - write-all wins over none",
			base:   NewPermissionsNone(),
			merge:  NewPermissionsWriteAll(),
			wantSH: "write-all",
		},
		{
			name:   "merge shorthand - write-all wins over read-all",
			base:   NewPermissionsReadAll(),
			merge:  NewPermissionsWriteAll(),
			wantSH: "write-all",
		},
		{
			name:   "merge shorthand - write-all wins over read-all (duplicate for coverage)",
			base:   NewPermissionsReadAll(),
			merge:  NewPermissionsWriteAll(),
			wantSH: "write-all",
		},
		{
			name:   "merge shorthand - write-all wins over none",
			base:   NewPermissionsNone(),
			merge:  NewPermissionsWriteAll(),
			wantSH: "write-all",
		},
		{
			name:   "merge shorthand - read-all wins over read-all",
			base:   NewPermissionsReadAll(),
			merge:  NewPermissionsReadAll(),
			wantSH: "read-all",
		},
		{
			name:   "merge shorthand - read-all wins over none",
			base:   NewPermissionsNone(),
			merge:  NewPermissionsReadAll(),
			wantSH: "read-all",
		},
		{
			name:   "merge shorthand - read-all wins over none (duplicate for coverage)",
			base:   NewPermissionsNone(),
			merge:  NewPermissionsReadAll(),
			wantSH: "read-all",
		},
		{
			name:   "merge shorthand - read-all preserved when merging read",
			base:   NewPermissionsReadAll(),
			merge:  NewPermissionsReadAll(),
			wantSH: "read-all",
		},
		{
			name:   "merge shorthand - write-all preserved when merging write",
			base:   NewPermissionsWriteAll(),
			merge:  NewPermissionsWriteAll(),
			wantSH: "write-all",
		},
		{
			name:   "merge shorthand - same shorthand preserved (read-all)",
			base:   NewPermissionsReadAll(),
			merge:  NewPermissionsReadAll(),
			wantSH: "read-all",
		},
		{
			name:   "merge shorthand - same shorthand preserved (write-all)",
			base:   NewPermissionsWriteAll(),
			merge:  NewPermissionsWriteAll(),
			wantSH: "write-all",
		},
		{
			name:   "merge shorthand - same shorthand preserved (none)",
			base:   NewPermissionsNone(),
			merge:  NewPermissionsNone(),
			wantSH: "none",
		},

		// Shorthand-to-Map merges
		{
			name:  "merge read-all shorthand into map - adds all missing scopes as read",
			base:  NewPermissionsFromMap(map[PermissionScope]PermissionLevel{PermissionContents: PermissionWrite}),
			merge: NewPermissionsReadAll(),
			want: map[PermissionScope]PermissionLevel{
				PermissionContents:            PermissionWrite, // preserved
				PermissionActions:             PermissionRead,  // added
				PermissionAttestations:        PermissionRead,
				PermissionChecks:              PermissionRead,
				PermissionCodeQuality:         PermissionRead,
				PermissionDeployments:         PermissionRead,
				PermissionDiscussions:         PermissionRead,
				PermissionDrives:              PermissionRead,
				PermissionIssues:              PermissionRead,
				PermissionMetadata:            PermissionRead,
				PermissionPackages:            PermissionRead,
				PermissionPages:               PermissionRead,
				PermissionPullRequests:        PermissionRead,
				PermissionRepositoryProj:      PermissionRead,
				PermissionSecurityEvents:      PermissionRead,
				PermissionStatuses:            PermissionRead,
				PermissionModels:              PermissionRead,
				PermissionVulnerabilityAlerts: PermissionRead,
				// Note: id-token is NOT included because it doesn't support read level
				// Note: organization-projects is NOT included because it's a GitHub App-only scope
			},
		},
		{
			name:  "merge write-all shorthand into map - adds all missing scopes as write",
			base:  NewPermissionsFromMap(map[PermissionScope]PermissionLevel{PermissionContents: PermissionRead}),
			merge: NewPermissionsWriteAll(),
			want: map[PermissionScope]PermissionLevel{
				PermissionContents:            PermissionRead, // preserved (not overwritten)
				PermissionActions:             PermissionWrite,
				PermissionAttestations:        PermissionWrite,
				PermissionChecks:              PermissionWrite,
				PermissionCodeQuality:         PermissionWrite,
				PermissionDeployments:         PermissionWrite,
				PermissionDiscussions:         PermissionWrite,
				PermissionDrives:              PermissionWrite,
				PermissionIdToken:             PermissionWrite, // id-token supports write
				PermissionIssues:              PermissionWrite,
				PermissionMetadata:            PermissionWrite,
				PermissionPackages:            PermissionWrite,
				PermissionPages:               PermissionWrite,
				PermissionPullRequests:        PermissionWrite,
				PermissionRepositoryProj:      PermissionWrite,
				PermissionSecurityEvents:      PermissionWrite,
				PermissionStatuses:            PermissionWrite,
				PermissionModels:              PermissionWrite,
				PermissionVulnerabilityAlerts: PermissionWrite,
				// Note: organization-projects is NOT included because it's a GitHub App-only scope
			},
		},
		{
			name:  "merge read shorthand into map - adds all missing scopes as read",
			base:  NewPermissionsFromMap(map[PermissionScope]PermissionLevel{PermissionContents: PermissionWrite}),
			merge: NewPermissionsReadAll(),
			want: map[PermissionScope]PermissionLevel{
				PermissionContents:            PermissionWrite,
				PermissionActions:             PermissionRead,
				PermissionAttestations:        PermissionRead,
				PermissionChecks:              PermissionRead,
				PermissionCodeQuality:         PermissionRead,
				PermissionDeployments:         PermissionRead,
				PermissionDiscussions:         PermissionRead,
				PermissionDrives:              PermissionRead,
				PermissionIssues:              PermissionRead,
				PermissionMetadata:            PermissionRead,
				PermissionPackages:            PermissionRead,
				PermissionPages:               PermissionRead,
				PermissionPullRequests:        PermissionRead,
				PermissionRepositoryProj:      PermissionRead,
				PermissionSecurityEvents:      PermissionRead,
				PermissionStatuses:            PermissionRead,
				PermissionModels:              PermissionRead,
				PermissionVulnerabilityAlerts: PermissionRead,
				// Note: id-token is NOT included because it doesn't support read level
				// Note: organization-projects is NOT included because it's a GitHub App-only scope
			},
		},
		{
			name:  "merge write shorthand into map - adds all missing scopes as write",
			base:  NewPermissionsFromMap(map[PermissionScope]PermissionLevel{PermissionIssues: PermissionRead}),
			merge: NewPermissionsWriteAll(),
			want: map[PermissionScope]PermissionLevel{
				PermissionIssues:              PermissionRead,
				PermissionActions:             PermissionWrite,
				PermissionAttestations:        PermissionWrite,
				PermissionChecks:              PermissionWrite,
				PermissionCodeQuality:         PermissionWrite,
				PermissionContents:            PermissionWrite,
				PermissionDeployments:         PermissionWrite,
				PermissionDiscussions:         PermissionWrite,
				PermissionDrives:              PermissionWrite,
				PermissionIdToken:             PermissionWrite, // id-token supports write
				PermissionMetadata:            PermissionWrite,
				PermissionPackages:            PermissionWrite,
				PermissionPages:               PermissionWrite,
				PermissionPullRequests:        PermissionWrite,
				PermissionRepositoryProj:      PermissionWrite,
				PermissionSecurityEvents:      PermissionWrite,
				PermissionStatuses:            PermissionWrite,
				PermissionModels:              PermissionWrite,
				PermissionVulnerabilityAlerts: PermissionWrite,
			},
		},
		{
			name:  "merge none shorthand into map - no change",
			base:  NewPermissionsFromMap(map[PermissionScope]PermissionLevel{PermissionContents: PermissionRead}),
			merge: NewPermissionsNone(),
			want:  map[PermissionScope]PermissionLevel{PermissionContents: PermissionRead},
		},

		// Map-to-Shorthand merges (shorthand converts to map)
		{
			name:  "merge map into read-all shorthand - shorthand cleared, map created",
			base:  NewPermissionsReadAll(),
			merge: NewPermissionsFromMap(map[PermissionScope]PermissionLevel{PermissionIssues: PermissionWrite}),
			want:  map[PermissionScope]PermissionLevel{PermissionIssues: PermissionWrite},
		},
		{
			name:  "merge map into write-all shorthand - shorthand cleared, map created",
			base:  NewPermissionsWriteAll(),
			merge: NewPermissionsFromMap(map[PermissionScope]PermissionLevel{PermissionContents: PermissionRead}),
			want:  map[PermissionScope]PermissionLevel{PermissionContents: PermissionRead},
		},
		{
			name:  "merge map into none shorthand - shorthand cleared, map created",
			base:  NewPermissionsNone(),
			merge: NewPermissionsFromMap(map[PermissionScope]PermissionLevel{PermissionIssues: PermissionWrite}),
			want:  map[PermissionScope]PermissionLevel{PermissionIssues: PermissionWrite},
		},
		{
			name: "merge complex map into read shorthand",
			base: NewPermissionsReadAll(),
			merge: NewPermissionsFromMap(map[PermissionScope]PermissionLevel{
				PermissionContents:     PermissionWrite,
				PermissionIssues:       PermissionRead,
				PermissionPullRequests: PermissionWrite,
			}),
			want: map[PermissionScope]PermissionLevel{
				PermissionContents:     PermissionWrite,
				PermissionIssues:       PermissionRead,
				PermissionPullRequests: PermissionWrite,
			},
		},

		// Nil and edge cases
		{
			name:  "merge nil into map - no change",
			base:  NewPermissionsFromMap(map[PermissionScope]PermissionLevel{PermissionContents: PermissionRead}),
			merge: nil,
			want:  map[PermissionScope]PermissionLevel{PermissionContents: PermissionRead},
		},
		{
			name:   "merge nil into shorthand - no change",
			base:   NewPermissionsReadAll(),
			merge:  nil,
			wantSH: "read-all",
		},
		{
			name:  "merge empty map into map - no change",
			base:  NewPermissionsFromMap(map[PermissionScope]PermissionLevel{PermissionContents: PermissionRead}),
			merge: NewPermissionsFromMap(map[PermissionScope]PermissionLevel{}),
			want:  map[PermissionScope]PermissionLevel{PermissionContents: PermissionRead},
		},
		{
			name:  "merge map into empty map - scopes added",
			base:  NewPermissionsFromMap(map[PermissionScope]PermissionLevel{}),
			merge: NewPermissionsFromMap(map[PermissionScope]PermissionLevel{PermissionIssues: PermissionWrite}),
			want:  map[PermissionScope]PermissionLevel{PermissionIssues: PermissionWrite},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.base.Merge(tt.merge)

			if tt.wantSH != "" {
				if tt.base.shorthand != tt.wantSH {
					t.Errorf("after merge, shorthand = %q, want %q", tt.base.shorthand, tt.wantSH)
				}
				return
			}

			if len(tt.want) != len(tt.base.permissions) {
				t.Errorf("after merge, got %d permissions, want %d", len(tt.base.permissions), len(tt.want))
			}

			for scope, wantLevel := range tt.want {
				gotLevel, exists := tt.base.Get(scope)
				if !exists {
					t.Errorf("after merge, scope %s not found", scope)
					continue
				}
				if gotLevel != wantLevel {
					t.Errorf("after merge, scope %s = %v, want %v", scope, gotLevel, wantLevel)
				}
			}
		})
	}
}

func TestPermissions_AllRead(t *testing.T) {
	tests := []struct {
		name     string
		perms    *Permissions
		scope    PermissionScope
		expected PermissionLevel
		exists   bool
	}{
		{
			name:     "all: read returns read for contents",
			perms:    NewPermissionsAllRead(),
			scope:    PermissionContents,
			expected: PermissionRead,
			exists:   true,
		},
		{
			name:     "all: read returns read for issues",
			perms:    NewPermissionsAllRead(),
			scope:    PermissionIssues,
			expected: PermissionRead,
			exists:   true,
		},
		{
			name: "all: read with explicit override",
			perms: func() *Permissions {
				p := NewPermissionsAllRead()
				p.Set(PermissionContents, PermissionWrite)
				return p
			}(),
			scope:    PermissionContents,
			expected: PermissionWrite,
			exists:   true,
		},
		{
			name:     "all: read does not include id-token (not supported at read level)",
			perms:    NewPermissionsAllRead(),
			scope:    PermissionIdToken,
			expected: "",    // Should be empty since the permission doesn't exist
			exists:   false, // Should not exist because id-token doesn't support read
		},
		{
			name: "all: read with explicit id-token: write override",
			perms: func() *Permissions {
				p := NewPermissionsAllRead()
				p.Set(PermissionIdToken, PermissionWrite)
				return p
			}(),
			scope:    PermissionIdToken,
			expected: PermissionWrite,
			exists:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			level, exists := tt.perms.Get(tt.scope)
			if exists != tt.exists {
				t.Errorf("Get(%s) exists = %v, want %v", tt.scope, exists, tt.exists)
			}
			if level != tt.expected {
				t.Errorf("Get(%s) = %v, want %v", tt.scope, level, tt.expected)
			}
		})
	}
}

func TestFilterJobLevelPermissions(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expectEmpty bool
		contains    []string
		excludes    []string
	}{
		{
			name:        "empty input returns empty",
			input:       "",
			expectEmpty: true,
			contains:    []string{},
			excludes:    []string{},
		},
		{
			name:  "standard permissions are preserved",
			input: "permissions:\n  contents: read\n  issues: write",
			contains: []string{
				"permissions:",
				"  contents: read",
				"  issues: write",
			},
			excludes: []string{},
		},
		{
			name:  "vulnerability-alerts is preserved (GITHUB_TOKEN scope)",
			input: "permissions:\n  contents: read\n  pull-requests: read\n  security-events: read\n  vulnerability-alerts: read",
			contains: []string{
				"permissions:",
				"  contents: read",
				"  pull-requests: read",
				"  security-events: read",
				"  vulnerability-alerts: read",
			},
			excludes: []string{},
		},
		{
			name:  "multiple GitHub App-only scopes are filtered out but vulnerability-alerts is preserved",
			input: "permissions:\n  contents: read\n  issues: write\n  administration: read\n  members: read\n  vulnerability-alerts: read",
			contains: []string{
				"permissions:",
				"  contents: read",
				"  issues: write",
				"  vulnerability-alerts: read",
			},
			excludes: []string{"administration", "members"},
		},
		{
			name:        "only GitHub App-only scopes returns empty string",
			input:       "permissions:\n  members: read",
			expectEmpty: true,
			contains:    []string{},
			excludes:    []string{"members"},
		},
		{
			name:     "shorthand read-all is preserved unchanged",
			input:    "permissions: read-all",
			contains: []string{"permissions: read-all"},
			excludes: []string{},
		},
		{
			name:     "shorthand write-all is preserved unchanged",
			input:    "permissions: write-all",
			contains: []string{"permissions: write-all"},
			excludes: []string{},
		},
		{
			name:     "shorthand none is preserved unchanged",
			input:    "permissions: none",
			contains: []string{"permissions: none"},
			excludes: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := filterJobLevelPermissions(tt.input)
			if tt.expectEmpty && result != "" {
				t.Errorf("filterJobLevelPermissions() should return empty string, but got:\n%q", result)
			}
			for _, expected := range tt.contains {
				if !strings.Contains(result, expected) {
					t.Errorf("filterJobLevelPermissions() should contain %q, but got:\n%q", expected, result)
				}
			}
			for _, excluded := range tt.excludes {
				if strings.Contains(result, excluded) {
					t.Errorf("filterJobLevelPermissions() should NOT contain %q, but got:\n%q", excluded, result)
				}
			}
		})
	}
}

func TestPermissions_HasContentsReadAccess(t *testing.T) {
	tests := []struct {
		name     string
		perms    *Permissions
		expected bool
	}{
		{
			name:     "nil permissions returns false",
			perms:    nil,
			expected: false,
		},
		{
			name:     "read-all shorthand grants contents read",
			perms:    NewPermissionsReadAll(),
			expected: true,
		},
		{
			name:     "write-all shorthand grants contents read",
			perms:    NewPermissionsWriteAll(),
			expected: true,
		},
		{
			name:     "none shorthand denies contents read",
			perms:    NewPermissionsNone(),
			expected: false,
		},
		{
			name:     "empty permissions denies contents read",
			perms:    NewPermissions(),
			expected: false,
		},
		{
			name:     "contents: read grants access",
			perms:    NewPermissionsContentsRead(),
			expected: true,
		},
		{
			name:     "contents: write grants access (write implies read)",
			perms:    NewPermissionsContentsWrite(),
			expected: true,
		},
		{
			name: "no contents permission denies access",
			perms: NewPermissionsFromMap(map[PermissionScope]PermissionLevel{
				PermissionIssues: PermissionWrite,
			}),
			expected: false,
		},
		{
			name: "contents: none denies access",
			perms: NewPermissionsFromMap(map[PermissionScope]PermissionLevel{
				PermissionContents: PermissionNone,
			}),
			expected: false,
		},
		{
			name:     "all: read grants contents read",
			perms:    NewPermissionsAllRead(),
			expected: true,
		},
		{
			name: "all: write grants contents read (write implies read)",
			perms: func() *Permissions {
				p := NewPermissions()
				p.hasAll = true
				p.allLevel = PermissionWrite
				return p
			}(),
			expected: true,
		},
		{
			name: "all: read with explicit contents: none denies access",
			perms: func() *Permissions {
				p := NewPermissionsAllRead()
				p.Set(PermissionContents, PermissionNone)
				return p
			}(),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.perms.HasContentsReadAccess()
			if result != tt.expected {
				t.Errorf("HasContentsReadAccess() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

func TestPermissions_HasCopilotRequestsWrite(t *testing.T) {
	tests := []struct {
		name     string
		perms    *Permissions
		expected bool
	}{
		{
			name:     "nil permissions returns false",
			perms:    nil,
			expected: false,
		},
		{
			name:     "write-all grants copilot requests write",
			perms:    NewPermissionsWriteAll(),
			expected: true,
		},
		{
			name:     "read-all does not grant copilot requests write",
			perms:    NewPermissionsReadAll(),
			expected: false,
		},
		{
			name: "explicit copilot requests write grants access",
			perms: NewPermissionsFromMap(map[PermissionScope]PermissionLevel{
				PermissionCopilotRequests: PermissionWrite,
			}),
			expected: true,
		},
		{
			name: "explicit copilot requests none denies access",
			perms: NewPermissionsFromMap(map[PermissionScope]PermissionLevel{
				PermissionCopilotRequests: PermissionNone,
			}),
			expected: false,
		},
		{
			name: "all write with explicit copilot requests none denies access",
			perms: func() *Permissions {
				p := NewPermissions()
				p.hasAll = true
				p.allLevel = PermissionWrite
				p.Set(PermissionCopilotRequests, PermissionNone)
				return p
			}(),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.perms.HasCopilotRequestsWrite()
			if result != tt.expected {
				t.Errorf("HasCopilotRequestsWrite() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

func TestFilterJobLevelPermissionsWithCache(t *testing.T) {
	// Verify that passing a pre-parsed *Permissions produces the same result
	// as parsing from the raw YAML string.
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "standard permissions with cache",
			input: "permissions:\n  contents: read\n  issues: write",
		},
		{
			name:  "shorthand read-all with cache",
			input: "permissions: read-all",
		},
		{
			name:  "shorthand none with cache",
			input: "permissions: none",
		},
		{
			name:  "App-only scopes filtered with cache",
			input: "permissions:\n  contents: read\n  members: read",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Compute expected result without cache
			withoutCache := filterJobLevelPermissions(tt.input)

			// Compute result using cached permissions
			cached := NewPermissionsParser(tt.input).ToPermissions()
			withCache := filterJobLevelPermissions(tt.input, cached)

			if withoutCache != withCache {
				t.Errorf("filterJobLevelPermissions() with cache produced different result:\nwithout cache: %q\nwith cache:    %q", withoutCache, withCache)
			}
		})
	}
}

func TestPermissions_Clone(t *testing.T) {
	t.Run("clone is independent - mutations do not affect original", func(t *testing.T) {
		original := NewPermissionsContentsRead()
		clone := original.Clone()

		// Mutate the clone
		clone.Set(PermissionIssues, PermissionWrite)

		// Original must be unchanged
		if _, exists := original.permissions[PermissionIssues]; exists {
			t.Error("Clone.Set() mutated the original Permissions object")
		}
	})

	t.Run("clone of shorthand is independent", func(t *testing.T) {
		original := NewPermissionsReadAll()
		clone := original.Clone()

		if clone.shorthand != original.shorthand {
			t.Errorf("Clone shorthand mismatch: got %q, want %q", clone.shorthand, original.shorthand)
		}

		// Mutating clone should not affect original
		clone.shorthand = "none"
		if original.shorthand != "read-all" {
			t.Errorf("Clone mutation affected original shorthand: %q", original.shorthand)
		}
	})

	t.Run("clone of nil returns empty permissions", func(t *testing.T) {
		var p *Permissions
		clone := p.Clone()
		if clone == nil {
			t.Fatal("Clone of nil should return non-nil empty Permissions")
		}
		if len(clone.permissions) != 0 || clone.shorthand != "" {
			t.Error("Clone of nil should return empty Permissions")
		}
	})

	t.Run("clone of all:read preserves hasAll flag", func(t *testing.T) {
		original := NewPermissionsAllRead()
		clone := original.Clone()
		if !clone.hasAll || clone.allLevel != PermissionRead {
			t.Errorf("Clone of all:read did not preserve hasAll/allLevel: hasAll=%v allLevel=%q", clone.hasAll, clone.allLevel)
		}
	})
}
