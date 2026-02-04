//go:build !integration

package workflow

import (
	"strings"
	"testing"
)

func TestPermissionsRenderToYAML(t *testing.T) {
	tests := []struct {
		name        string
		permissions *Permissions
		want        string
	}{
		{
			name:        "nil permissions",
			permissions: nil,
			want:        "",
		},
		{
			name:        "read-all shorthand",
			permissions: NewPermissionsReadAll(),
			want:        "permissions: read-all",
		},
		{
			name:        "write-all shorthand",
			permissions: NewPermissionsWriteAll(),
			want:        "permissions: write-all",
		},
		{
			name:        "empty permissions",
			permissions: NewPermissions(),
			want:        "",
		},
		{
			name: "single permission",
			permissions: NewPermissionsFromMap(map[PermissionScope]PermissionLevel{
				PermissionContents: PermissionRead,
			}),
			want: "permissions:\n      contents: read",
		},
		{
			name: "multiple permissions - sorted",
			permissions: NewPermissionsFromMap(map[PermissionScope]PermissionLevel{
				PermissionIssues:       PermissionWrite,
				PermissionContents:     PermissionRead,
				PermissionPullRequests: PermissionWrite,
			}),
			want: "permissions:\n      contents: read\n      issues: write\n      pull-requests: write",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.permissions.RenderToYAML()
			if got != tt.want {
				t.Errorf("RenderToYAML() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPermissions_AllReadWithIdTokenWrite(t *testing.T) {
	// Test that "all: read" with "id-token: write" works as expected
	// id-token is special because it only supports write and none, not read

	// Create permissions with all: read and id-token: write
	perms := &Permissions{
		hasAll:   true,
		allLevel: PermissionRead,
		permissions: map[PermissionScope]PermissionLevel{
			PermissionIdToken: PermissionWrite,
		},
	}

	// Test that all normal scopes have read access
	normalScopes := []PermissionScope{
		PermissionActions, PermissionAttestations, PermissionChecks, PermissionContents,
		PermissionDeployments, PermissionDiscussions, PermissionIssues, PermissionPackages,
		PermissionPages, PermissionPullRequests, PermissionRepositoryProj,
		PermissionSecurityEvents, PermissionStatuses, PermissionModels,
	}

	for _, scope := range normalScopes {
		level, allowed := perms.Get(scope)
		if !allowed || level != PermissionRead {
			t.Errorf("scope %s should have read access, got allowed=%v, level=%s", scope, allowed, level)
		}
	}

	// Test that id-token has write access (explicit override)
	level, allowed := perms.Get(PermissionIdToken)
	if !allowed || level != PermissionWrite {
		t.Errorf("id-token should have write access, got allowed=%v, level=%s", allowed, level)
	}

	// Test that id-token does NOT get read access from all: read
	// This should return false because id-token doesn't support read
	if level, allowed := perms.Get(PermissionIdToken); allowed && level == PermissionRead {
		t.Errorf("id-token should NOT have read access from all: read")
	}

	// Test YAML rendering excludes id-token: read but includes id-token: write
	yaml := perms.RenderToYAML()

	// Should contain all normal scopes with read access
	expectedLines := []string{
		"      actions: read",
		"      attestations: read",
		"      checks: read",
		"      contents: read",
		"      deployments: read",
		"      discussions: read",
		"      issues: read",
		"      packages: read",
		"      pages: read",
		"      pull-requests: read",
		"      repository-projects: read",
		"      security-events: read",
		"      statuses: read",
		"      models: read",
		"      id-token: write", // explicit override
	}

	for _, expected := range expectedLines {
		if !strings.Contains(yaml, expected) {
			t.Errorf("YAML should contain %q, but got:\n%s", expected, yaml)
		}
	}

	// Should NOT contain id-token: read
	if strings.Contains(yaml, "id-token: read") {
		t.Errorf("YAML should NOT contain 'id-token: read', but got:\n%s", yaml)
	}
}

func TestPermissions_AllReadRenderToYAML(t *testing.T) {
	tests := []struct {
		name        string
		perms       *Permissions
		contains    []string // Check that output contains these lines
		notContains []string // Check that output does NOT contain these lines
	}{
		{
			name:  "all: read expands to individual permissions",
			perms: NewPermissionsAllRead(),
			contains: []string{
				"permissions:",
				"      actions: read",
				"      attestations: read",
				"      checks: read",
				"      contents: read",
				"      deployments: read",
				"      discussions: read",
				"      issues: read",
				"      models: read",
				"      packages: read",
				"      pages: read",
				"      pull-requests: read",
				"      repository-projects: read",
				"      security-events: read",
				"      statuses: read",
			},
		},
		{
			name: "all: read with explicit override - write overrides read",
			perms: func() *Permissions {
				p := NewPermissionsAllRead()
				p.Set(PermissionContents, PermissionWrite)
				return p
			}(),
			contains: []string{
				"permissions:",
				"      actions: read",
				"      contents: write", // Overridden to write
				"      issues: read",
			},
			notContains: []string{
				"      contents: read", // Should NOT contain contents: read when explicitly set to write
			},
		},
		{
			name: "all: read with multiple explicit overrides",
			perms: func() *Permissions {
				p := NewPermissionsAllRead()
				p.Set(PermissionContents, PermissionWrite)
				p.Set(PermissionIssues, PermissionWrite)
				return p
			}(),
			contains: []string{
				"permissions:",
				"      actions: read",
				"      contents: write",
				"      issues: write",
				"      packages: read",
			},
			notContains: []string{
				"      contents: read", // Should NOT contain contents: read
				"      issues: read",   // Should NOT contain issues: read
			},
		},
		{
			name: "all: read with id-token: write - id-token should be excluded from all: read expansion but included when explicitly set to write",
			perms: func() *Permissions {
				p := NewPermissionsAllRead()
				p.Set(PermissionIdToken, PermissionWrite)
				return p
			}(),
			contains: []string{
				"permissions:",
				"      actions: read",
				"      contents: read",
				"      id-token: write", // Explicitly set to write
				"      issues: read",
			},
			notContains: []string{
				"      id-token: read", // Should NOT contain id-token: read (not supported)
			},
		},
		{
			name:  "all: read excludes id-token since it doesn't support read level",
			perms: NewPermissionsAllRead(),
			contains: []string{
				"permissions:",
				"      actions: read",
				"      attestations: read",
				"      checks: read",
				"      contents: read",
				"      deployments: read",
				"      discussions: read",
				"      issues: read",
				"      models: read",
				"      packages: read",
				"      pages: read",
				"      pull-requests: read",
				"      repository-projects: read",
				"      security-events: read",
				"      statuses: read",
			},
			notContains: []string{
				"      id-token: read", // Should NOT be included since id-token doesn't support read
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.perms.RenderToYAML()
			for _, expected := range tt.contains {
				if !strings.Contains(result, expected) {
					t.Errorf("RenderToYAML() should contain %q, but got:\n%s", expected, result)
				}
			}
			for _, notExpected := range tt.notContains {
				if strings.Contains(result, notExpected) {
					t.Errorf("RenderToYAML() should NOT contain %q, but got:\n%s", notExpected, result)
				}
			}
		})
	}
}

func TestPermissions_MetadataExcluded(t *testing.T) {
	tests := []struct {
		name        string
		perms       *Permissions
		contains    []string
		notContains []string
	}{
		{
			name: "metadata permission should be excluded from YAML output",
			perms: NewPermissionsFromMap(map[PermissionScope]PermissionLevel{
				PermissionContents: PermissionRead,
				PermissionMetadata: PermissionRead,
				PermissionIssues:   PermissionWrite,
			}),
			contains: []string{
				"permissions:",
				"      contents: read",
				"      issues: write",
			},
			notContains: []string{
				"metadata",
			},
		},
		{
			name:  "all: read should expand without metadata",
			perms: NewPermissionsAllRead(),
			contains: []string{
				"permissions:",
				"      contents: read",
				"      issues: read",
			},
			notContains: []string{
				"metadata",
			},
		},
		{
			name: "metadata: write should also be excluded",
			perms: NewPermissionsFromMap(map[PermissionScope]PermissionLevel{
				PermissionContents: PermissionRead,
				PermissionMetadata: PermissionWrite,
			}),
			contains: []string{
				"permissions:",
				"      contents: read",
			},
			notContains: []string{
				"metadata",
			},
		},
		{
			name: "only metadata permission should render empty permissions",
			perms: NewPermissionsFromMap(map[PermissionScope]PermissionLevel{
				PermissionMetadata: PermissionRead,
			}),
			contains:    []string{},
			notContains: []string{"metadata"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.perms.RenderToYAML()
			for _, expected := range tt.contains {
				if !strings.Contains(result, expected) {
					t.Errorf("RenderToYAML() should contain %q, but got:\n%s", expected, result)
				}
			}
			for _, notExpected := range tt.notContains {
				if strings.Contains(result, notExpected) {
					t.Errorf("RenderToYAML() should NOT contain %q, but got:\n%s", notExpected, result)
				}
			}
		})
	}
}
