//go:build !integration

package workflow

import (
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
				PermissionContents:         PermissionWrite, // preserved
				PermissionActions:          PermissionRead,  // added
				PermissionAttestations:     PermissionRead,
				PermissionChecks:           PermissionRead,
				PermissionDeployments:      PermissionRead,
				PermissionDiscussions:      PermissionRead,
				PermissionIssues:           PermissionRead,
				PermissionMetadata:         PermissionRead,
				PermissionPackages:         PermissionRead,
				PermissionPages:            PermissionRead,
				PermissionPullRequests:     PermissionRead,
				PermissionRepositoryProj:   PermissionRead,
				PermissionOrganizationProj: PermissionRead,
				PermissionSecurityEvents:   PermissionRead,
				PermissionStatuses:         PermissionRead,
				PermissionModels:           PermissionRead,
				// Note: id-token is NOT included because it doesn't support read level
			},
		},
		{
			name:  "merge write-all shorthand into map - adds all missing scopes as write",
			base:  NewPermissionsFromMap(map[PermissionScope]PermissionLevel{PermissionContents: PermissionRead}),
			merge: NewPermissionsWriteAll(),
			want: map[PermissionScope]PermissionLevel{
				PermissionContents:         PermissionRead, // preserved (not overwritten)
				PermissionActions:          PermissionWrite,
				PermissionAttestations:     PermissionWrite,
				PermissionChecks:           PermissionWrite,
				PermissionDeployments:      PermissionWrite,
				PermissionDiscussions:      PermissionWrite,
				PermissionIdToken:          PermissionWrite, // id-token supports write
				PermissionIssues:           PermissionWrite,
				PermissionMetadata:         PermissionWrite,
				PermissionPackages:         PermissionWrite,
				PermissionPages:            PermissionWrite,
				PermissionPullRequests:     PermissionWrite,
				PermissionRepositoryProj:   PermissionWrite,
				PermissionOrganizationProj: PermissionWrite,
				PermissionSecurityEvents:   PermissionWrite,
				PermissionStatuses:         PermissionWrite,
				PermissionModels:           PermissionWrite,
			},
		},
		{
			name:  "merge read shorthand into map - adds all missing scopes as read",
			base:  NewPermissionsFromMap(map[PermissionScope]PermissionLevel{PermissionContents: PermissionWrite}),
			merge: NewPermissionsReadAll(),
			want: map[PermissionScope]PermissionLevel{
				PermissionContents:         PermissionWrite,
				PermissionActions:          PermissionRead,
				PermissionAttestations:     PermissionRead,
				PermissionChecks:           PermissionRead,
				PermissionDeployments:      PermissionRead,
				PermissionDiscussions:      PermissionRead,
				PermissionIssues:           PermissionRead,
				PermissionMetadata:         PermissionRead,
				PermissionPackages:         PermissionRead,
				PermissionPages:            PermissionRead,
				PermissionPullRequests:     PermissionRead,
				PermissionRepositoryProj:   PermissionRead,
				PermissionOrganizationProj: PermissionRead,
				PermissionSecurityEvents:   PermissionRead,
				PermissionStatuses:         PermissionRead,
				PermissionModels:           PermissionRead,
				// Note: id-token is NOT included because it doesn't support read level
			},
		},
		{
			name:  "merge write shorthand into map - adds all missing scopes as write",
			base:  NewPermissionsFromMap(map[PermissionScope]PermissionLevel{PermissionIssues: PermissionRead}),
			merge: NewPermissionsWriteAll(),
			want: map[PermissionScope]PermissionLevel{
				PermissionIssues:           PermissionRead,
				PermissionActions:          PermissionWrite,
				PermissionAttestations:     PermissionWrite,
				PermissionChecks:           PermissionWrite,
				PermissionContents:         PermissionWrite,
				PermissionDeployments:      PermissionWrite,
				PermissionDiscussions:      PermissionWrite,
				PermissionIdToken:          PermissionWrite, // id-token supports write
				PermissionMetadata:         PermissionWrite,
				PermissionPackages:         PermissionWrite,
				PermissionPages:            PermissionWrite,
				PermissionPullRequests:     PermissionWrite,
				PermissionRepositoryProj:   PermissionWrite,
				PermissionOrganizationProj: PermissionWrite,
				PermissionSecurityEvents:   PermissionWrite,
				PermissionStatuses:         PermissionWrite,
				PermissionModels:           PermissionWrite,
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
