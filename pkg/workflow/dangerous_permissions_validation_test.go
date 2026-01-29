//go:build !integration

package workflow

import (
	"strings"
	"testing"
)

func TestValidateDangerousPermissions(t *testing.T) {
	tests := []struct {
		name          string
		permissions   string
		features      map[string]any
		shouldError   bool
		errorContains string
	}{
		{
			name:        "no permissions - should pass",
			permissions: "",
			shouldError: false,
		},
		{
			name:        "read permissions only - should pass",
			permissions: "permissions:\n  contents: read\n  issues: read",
			shouldError: false,
		},
		{
			name:          "write permission without feature flag - should error",
			permissions:   "permissions:\n  contents: write",
			shouldError:   true,
			errorContains: "Write permissions are not allowed",
		},
		{
			name:          "multiple write permissions without feature flag - should error",
			permissions:   "permissions:\n  contents: write\n  issues: write",
			shouldError:   true,
			errorContains: "Write permissions are not allowed",
		},
		{
			name:        "write permission with feature flag enabled - should pass",
			permissions: "permissions:\n  contents: write",
			features: map[string]any{
				"dangerous-permissions-write": true,
			},
			shouldError: false,
		},
		{
			name:        "write permission with feature flag disabled - should error",
			permissions: "permissions:\n  contents: write",
			features: map[string]any{
				"dangerous-permissions-write": false,
			},
			shouldError:   true,
			errorContains: "Write permissions are not allowed",
		},
		{
			name:        "shorthand read-all - should pass",
			permissions: "permissions: read-all",
			shouldError: false,
		},
		{
			name:          "shorthand write-all without feature flag - should error",
			permissions:   "permissions: write-all",
			shouldError:   true,
			errorContains: "Write permissions are not allowed",
		},
		{
			name:        "shorthand write-all with feature flag - should pass",
			permissions: "permissions: write-all",
			features: map[string]any{
				"dangerous-permissions-write": true,
			},
			shouldError: false,
		},
		{
			name:        "mixed read and write with feature flag - should pass",
			permissions: "permissions:\n  contents: read\n  issues: write\n  pull-requests: read",
			features: map[string]any{
				"dangerous-permissions-write": true,
			},
			shouldError: false,
		},
		{
			name:          "mixed read and write without feature flag - should error",
			permissions:   "permissions:\n  contents: read\n  issues: write\n  pull-requests: read",
			shouldError:   true,
			errorContains: "issues: write",
		},
		{
			name:        "id-token write without feature flag - should pass (id-token is safe)",
			permissions: "permissions:\n  id-token: write",
			shouldError: false,
		},
		{
			name:        "id-token write with other read permissions - should pass",
			permissions: "permissions:\n  contents: read\n  id-token: write\n  issues: read",
			shouldError: false,
		},
		{
			name:          "id-token write with other write permissions - should error on other permissions",
			permissions:   "permissions:\n  contents: write\n  id-token: write",
			shouldError:   true,
			errorContains: "contents: write",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workflowData := &WorkflowData{
				Permissions: tt.permissions,
				Features:    tt.features,
			}

			err := validateDangerousPermissions(workflowData)

			if tt.shouldError {
				if err == nil {
					t.Errorf("Expected error but got none")
					return
				}
				if tt.errorContains != "" && !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("Expected error to contain %q, but got: %v", tt.errorContains, err)
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error but got: %v", err)
				}
			}
		})
	}
}

func TestFindWritePermissions(t *testing.T) {
	tests := []struct {
		name               string
		permissions        *Permissions
		expectedWriteCount int
		expectedScopes     []PermissionScope
	}{
		{
			name:               "no permissions",
			permissions:        NewPermissions(),
			expectedWriteCount: 0,
			expectedScopes:     []PermissionScope{},
		},
		{
			name:               "only read permissions",
			permissions:        NewPermissionsContentsRead(),
			expectedWriteCount: 0,
			expectedScopes:     []PermissionScope{},
		},
		{
			name:               "single write permission",
			permissions:        NewPermissionsContentsWrite(),
			expectedWriteCount: 1,
			expectedScopes:     []PermissionScope{PermissionContents},
		},
		{
			name:               "multiple write permissions",
			permissions:        NewPermissionsContentsWriteIssuesWritePRWrite(),
			expectedWriteCount: 3,
			expectedScopes:     []PermissionScope{PermissionContents, PermissionIssues, PermissionPullRequests},
		},
		{
			name:               "write-all shorthand",
			permissions:        NewPermissionsWriteAll(),
			expectedWriteCount: 15,  // All permission scopes except id-token (which is excluded)
			expectedScopes:     nil, // Don't check specific scopes for shorthand
		},
		{
			name:               "mixed read and write",
			permissions:        NewPermissionsContentsReadIssuesWrite(),
			expectedWriteCount: 1,
			expectedScopes:     []PermissionScope{PermissionIssues},
		},
		{
			name: "id-token write is excluded from dangerous permissions",
			permissions: func() *Permissions {
				p := NewPermissions()
				p.Set(PermissionIdToken, PermissionWrite)
				return p
			}(),
			expectedWriteCount: 0, // id-token should be excluded
			expectedScopes:     []PermissionScope{},
		},
		{
			name: "id-token write with other write permissions - only other permissions counted",
			permissions: func() *Permissions {
				p := NewPermissions()
				p.Set(PermissionIdToken, PermissionWrite)
				p.Set(PermissionContents, PermissionWrite)
				return p
			}(),
			expectedWriteCount: 1, // Only contents, not id-token
			expectedScopes:     []PermissionScope{PermissionContents},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			writePerms := findWritePermissions(tt.permissions)

			if len(writePerms) != tt.expectedWriteCount {
				t.Errorf("Expected %d write permissions, got %d", tt.expectedWriteCount, len(writePerms))
			}

			if tt.expectedScopes != nil {
				// Check that all expected scopes are present
				for _, expectedScope := range tt.expectedScopes {
					found := false
					for _, scope := range writePerms {
						if scope == expectedScope {
							found = true
							break
						}
					}
					if !found {
						t.Errorf("Expected to find scope %s in write permissions", expectedScope)
					}
				}
			}
		})
	}
}

func TestFormatDangerousPermissionsError(t *testing.T) {
	tests := []struct {
		name               string
		writePermissions   []PermissionScope
		expectedContains   []string
		expectedNotContain []string
	}{
		{
			name: "single write permission",
			writePermissions: []PermissionScope{
				PermissionContents,
			},
			expectedContains: []string{
				"Write permissions are not allowed",
				"contents: write",
				"contents: read",
			},
			expectedNotContain: []string{
				"dangerous-permissions-write: true",
				"Option 2",
			},
		},
		{
			name: "multiple write permissions",
			writePermissions: []PermissionScope{
				PermissionContents,
				PermissionIssues,
			},
			expectedContains: []string{
				"Write permissions are not allowed",
				"contents: write",
				"issues: write",
				"contents: read",
				"issues: read",
			},
			expectedNotContain: []string{
				"dangerous-permissions-write: true",
				"Option 2",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := formatDangerousPermissionsError(tt.writePermissions)
			if err == nil {
				t.Fatal("Expected error but got nil")
			}

			errMsg := err.Error()

			for _, expected := range tt.expectedContains {
				if !strings.Contains(errMsg, expected) {
					t.Errorf("Expected error message to contain %q, but it didn't. Error: %s", expected, errMsg)
				}
			}

			for _, notExpected := range tt.expectedNotContain {
				if strings.Contains(errMsg, notExpected) {
					t.Errorf("Expected error message to NOT contain %q, but it did. Error: %s", notExpected, errMsg)
				}
			}
		})
	}
}
