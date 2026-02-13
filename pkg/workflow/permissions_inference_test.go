//go:build !integration

package workflow

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestInferCompatibleToolsets(t *testing.T) {
	tests := []struct {
		name          string
		permissions   map[PermissionScope]PermissionLevel
		readOnly      bool
		expectedTools []string
		description   string
	}{
		{
			name: "all default permissions - read only",
			permissions: map[PermissionScope]PermissionLevel{
				PermissionContents:     PermissionRead,
				PermissionIssues:       PermissionRead,
				PermissionPullRequests: PermissionRead,
			},
			readOnly:      true,
			expectedTools: []string{"context", "repos", "issues", "pull_requests"},
			description:   "All default toolsets should be compatible when all required read permissions are granted",
		},
		{
			name: "missing pull-requests permission",
			permissions: map[PermissionScope]PermissionLevel{
				PermissionContents: PermissionRead,
				PermissionIssues:   PermissionRead,
				PermissionActions:  PermissionRead,
			},
			readOnly:      true,
			expectedTools: []string{"context", "repos", "issues"},
			description:   "pull_requests toolset should be excluded when pull-requests permission is missing",
		},
		{
			name: "only contents permission",
			permissions: map[PermissionScope]PermissionLevel{
				PermissionContents: PermissionRead,
			},
			readOnly:      true,
			expectedTools: []string{"context", "repos"},
			description:   "Only context and repos toolsets should be compatible with contents permission",
		},
		{
			name: "only issues permission",
			permissions: map[PermissionScope]PermissionLevel{
				PermissionIssues: PermissionRead,
			},
			readOnly:      true,
			expectedTools: []string{"context", "issues"},
			description:   "Only context and issues toolsets should be compatible with issues permission",
		},
		{
			name:          "no permissions",
			permissions:   map[PermissionScope]PermissionLevel{},
			readOnly:      true,
			expectedTools: []string{"context"},
			description:   "Only context toolset should be compatible when no permissions are granted",
		},
		{
			name: "write mode requires write permissions",
			permissions: map[PermissionScope]PermissionLevel{
				PermissionContents: PermissionRead,
				PermissionIssues:   PermissionRead,
			},
			readOnly:      false,
			expectedTools: []string{"context"},
			description:   "Only context toolset is compatible in write mode when only read permissions granted",
		},
		{
			name: "write mode with write permissions",
			permissions: map[PermissionScope]PermissionLevel{
				PermissionContents: PermissionWrite,
				PermissionIssues:   PermissionWrite,
			},
			readOnly:      false,
			expectedTools: []string{"context", "repos", "issues"},
			description:   "repos and issues toolsets compatible in write mode with write permissions",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create Permissions from map
			perms := NewPermissions()
			for scope, level := range tt.permissions {
				perms.Set(scope, level)
			}

			result := InferCompatibleToolsets(perms, tt.readOnly)

			assert.Equal(t, tt.expectedTools, result, tt.description)
		})
	}
}

func TestInferCompatibleToolsets_NilPermissions(t *testing.T) {
	result := InferCompatibleToolsets(nil, true)
	assert.Empty(t, result, "Should return empty slice for nil permissions")
}
