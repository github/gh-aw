//go:build !integration

package workflow

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewPermissionsBuilder(t *testing.T) {
	builder := NewPermissionsBuilder()

	require.NotNil(t, builder, "NewPermissionsBuilder should return non-nil builder")
	require.NotNil(t, builder.perms, "Builder should have non-nil permissions")
	assert.Empty(t, builder.perms.permissions, "Builder should start with empty permissions map")
}

func TestPermissionsBuilder_SinglePermission(t *testing.T) {
	tests := []struct {
		name     string
		builder  func() *PermissionsBuilder
		scope    PermissionScope
		expected PermissionLevel
	}{
		{
			name:     "contents read",
			builder:  func() *PermissionsBuilder { return NewPermissionsBuilder().WithContents(PermissionRead) },
			scope:    PermissionContents,
			expected: PermissionRead,
		},
		{
			name:     "issues write",
			builder:  func() *PermissionsBuilder { return NewPermissionsBuilder().WithIssues(PermissionWrite) },
			scope:    PermissionIssues,
			expected: PermissionWrite,
		},
		{
			name:     "pull-requests write",
			builder:  func() *PermissionsBuilder { return NewPermissionsBuilder().WithPullRequests(PermissionWrite) },
			scope:    PermissionPullRequests,
			expected: PermissionWrite,
		},
		{
			name:     "discussions write",
			builder:  func() *PermissionsBuilder { return NewPermissionsBuilder().WithDiscussions(PermissionWrite) },
			scope:    PermissionDiscussions,
			expected: PermissionWrite,
		},
		{
			name:     "actions write",
			builder:  func() *PermissionsBuilder { return NewPermissionsBuilder().WithActions(PermissionWrite) },
			scope:    PermissionActions,
			expected: PermissionWrite,
		},
		{
			name:     "security-events write",
			builder:  func() *PermissionsBuilder { return NewPermissionsBuilder().WithSecurityEvents(PermissionWrite) },
			scope:    PermissionSecurityEvents,
			expected: PermissionWrite,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			perms := tt.builder().Build()
			require.NotNil(t, perms, "Build should return non-nil Permissions")

			level, exists := perms.Get(tt.scope)
			assert.True(t, exists, "Permission scope should exist")
			assert.Equal(t, tt.expected, level, "Permission level should match expected")
		})
	}
}

func TestPermissionsBuilder_MultiplePermissions(t *testing.T) {
	perms := NewPermissionsBuilder().
		WithContents(PermissionRead).
		WithIssues(PermissionWrite).
		WithPullRequests(PermissionWrite).
		Build()

	require.NotNil(t, perms, "Build should return non-nil Permissions")
	assert.Len(t, perms.permissions, 3, "Should have 3 permissions set")

	// Verify each permission
	level, exists := perms.Get(PermissionContents)
	assert.True(t, exists, "Contents permission should exist")
	assert.Equal(t, PermissionRead, level, "Contents should be read")

	level, exists = perms.Get(PermissionIssues)
	assert.True(t, exists, "Issues permission should exist")
	assert.Equal(t, PermissionWrite, level, "Issues should be write")

	level, exists = perms.Get(PermissionPullRequests)
	assert.True(t, exists, "Pull requests permission should exist")
	assert.Equal(t, PermissionWrite, level, "Pull requests should be write")
}

func TestPermissionsBuilder_AllScopes(t *testing.T) {
	// Test all available permission scopes
	perms := NewPermissionsBuilder().
		WithActions(PermissionWrite).
		WithAttestations(PermissionRead).
		WithChecks(PermissionRead).
		WithContents(PermissionRead).
		WithDeployments(PermissionRead).
		WithDiscussions(PermissionWrite).
		WithIdToken(PermissionWrite).
		WithIssues(PermissionWrite).
		WithMetadata(PermissionRead).
		WithModels(PermissionRead).
		WithPackages(PermissionRead).
		WithPages(PermissionRead).
		WithPullRequests(PermissionWrite).
		WithRepositoryProjects(PermissionRead).
		WithOrganizationProjects(PermissionWrite).
		WithSecurityEvents(PermissionWrite).
		WithStatuses(PermissionRead).
		Build()

	require.NotNil(t, perms, "Build should return non-nil Permissions")

	// Verify a few key permissions
	level, exists := perms.Get(PermissionActions)
	assert.True(t, exists, "Actions permission should exist")
	assert.Equal(t, PermissionWrite, level, "Actions should be write")

	level, exists = perms.Get(PermissionContents)
	assert.True(t, exists, "Contents permission should exist")
	assert.Equal(t, PermissionRead, level, "Contents should be read")
}

func TestPermissionsBuilder_ComplexPatterns(t *testing.T) {
	tests := []struct {
		name          string
		builder       func() *PermissionsBuilder
		expectedCount int
		checks        []struct {
			scope PermissionScope
			level PermissionLevel
		}
	}{
		{
			name: "contents read + issues write",
			builder: func() *PermissionsBuilder {
				return NewPermissionsBuilder().
					WithContents(PermissionRead).
					WithIssues(PermissionWrite)
			},
			expectedCount: 2,
			checks: []struct {
				scope PermissionScope
				level PermissionLevel
			}{
				{PermissionContents, PermissionRead},
				{PermissionIssues, PermissionWrite},
			},
		},
		{
			name: "contents read + issues write + pr write",
			builder: func() *PermissionsBuilder {
				return NewPermissionsBuilder().
					WithContents(PermissionRead).
					WithIssues(PermissionWrite).
					WithPullRequests(PermissionWrite)
			},
			expectedCount: 3,
			checks: []struct {
				scope PermissionScope
				level PermissionLevel
			}{
				{PermissionContents, PermissionRead},
				{PermissionIssues, PermissionWrite},
				{PermissionPullRequests, PermissionWrite},
			},
		},
		{
			name: "contents read + issues write + pr write + discussions write",
			builder: func() *PermissionsBuilder {
				return NewPermissionsBuilder().
					WithContents(PermissionRead).
					WithIssues(PermissionWrite).
					WithPullRequests(PermissionWrite).
					WithDiscussions(PermissionWrite)
			},
			expectedCount: 4,
			checks: []struct {
				scope PermissionScope
				level PermissionLevel
			}{
				{PermissionContents, PermissionRead},
				{PermissionIssues, PermissionWrite},
				{PermissionPullRequests, PermissionWrite},
				{PermissionDiscussions, PermissionWrite},
			},
		},
		{
			name: "actions write + contents write + issues write + pr write",
			builder: func() *PermissionsBuilder {
				return NewPermissionsBuilder().
					WithActions(PermissionWrite).
					WithContents(PermissionWrite).
					WithIssues(PermissionWrite).
					WithPullRequests(PermissionWrite)
			},
			expectedCount: 4,
			checks: []struct {
				scope PermissionScope
				level PermissionLevel
			}{
				{PermissionActions, PermissionWrite},
				{PermissionContents, PermissionWrite},
				{PermissionIssues, PermissionWrite},
				{PermissionPullRequests, PermissionWrite},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			perms := tt.builder().Build()
			require.NotNil(t, perms, "Build should return non-nil Permissions")
			assert.Len(t, perms.permissions, tt.expectedCount, "Should have expected number of permissions")

			for _, check := range tt.checks {
				level, exists := perms.Get(check.scope)
				assert.True(t, exists, "Permission scope %s should exist", check.scope)
				assert.Equal(t, check.level, level, "Permission level for %s should match", check.scope)
			}
		})
	}
}

func TestPermissionsBuilder_RenderToYAML(t *testing.T) {
	tests := []struct {
		name     string
		builder  func() *PermissionsBuilder
		contains []string
	}{
		{
			name: "single permission",
			builder: func() *PermissionsBuilder {
				return NewPermissionsBuilder().WithContents(PermissionRead)
			},
			contains: []string{"permissions:", "contents: read"},
		},
		{
			name: "multiple permissions",
			builder: func() *PermissionsBuilder {
				return NewPermissionsBuilder().
					WithContents(PermissionRead).
					WithIssues(PermissionWrite).
					WithPullRequests(PermissionWrite)
			},
			contains: []string{
				"permissions:",
				"contents: read",
				"issues: write",
				"pull-requests: write",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			perms := tt.builder().Build()
			yaml := perms.RenderToYAML()

			for _, substr := range tt.contains {
				assert.Contains(t, yaml, substr, "YAML should contain %q", substr)
			}
		})
	}
}

func TestPermissionsBuilder_BackwardCompatibility(t *testing.T) {
	// Test that builder produces same result as existing factory functions
	tests := []struct {
		name        string
		builder     func() *PermissionsBuilder
		factoryFunc func() *Permissions
	}{
		{
			name: "contents read",
			builder: func() *PermissionsBuilder {
				return NewPermissionsBuilder().WithContents(PermissionRead)
			},
			factoryFunc: NewPermissionsContentsRead,
		},
		{
			name: "contents read + issues write",
			builder: func() *PermissionsBuilder {
				return NewPermissionsBuilder().
					WithContents(PermissionRead).
					WithIssues(PermissionWrite)
			},
			factoryFunc: NewPermissionsContentsReadIssuesWrite,
		},
		{
			name: "contents read + issues write + pr write",
			builder: func() *PermissionsBuilder {
				return NewPermissionsBuilder().
					WithContents(PermissionRead).
					WithIssues(PermissionWrite).
					WithPullRequests(PermissionWrite)
			},
			factoryFunc: NewPermissionsContentsReadIssuesWritePRWrite,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builderPerms := tt.builder().Build()
			factoryPerms := tt.factoryFunc()

			builderYAML := builderPerms.RenderToYAML()
			factoryYAML := factoryPerms.RenderToYAML()

			assert.Equal(t, factoryYAML, builderYAML, "Builder and factory should produce identical YAML")
		})
	}
}

func TestPermissionsBuilder_Chaining(t *testing.T) {
	// Test that builder methods return the builder for chaining
	builder := NewPermissionsBuilder()

	result := builder.WithContents(PermissionRead)
	assert.Same(t, builder, result, "WithContents should return same builder instance")

	result = builder.WithIssues(PermissionWrite)
	assert.Same(t, builder, result, "WithIssues should return same builder instance")

	result = builder.WithPullRequests(PermissionWrite)
	assert.Same(t, builder, result, "WithPullRequests should return same builder instance")
}

func TestPermissionsBuilder_OverwritePermission(t *testing.T) {
	// Test that setting a permission twice overwrites the first value
	perms := NewPermissionsBuilder().
		WithContents(PermissionRead).
		WithContents(PermissionWrite).
		Build()

	level, exists := perms.Get(PermissionContents)
	assert.True(t, exists, "Contents permission should exist")
	assert.Equal(t, PermissionWrite, level, "Contents should be write (last set value)")
}
