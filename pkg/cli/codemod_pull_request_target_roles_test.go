//go:build !integration

package cli

import (
	"testing"

	"github.com/github/gh-aw/pkg/workflow/aw"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPullRequestTargetRolesCodemodMetadata(t *testing.T) {
	t.Parallel()
	mod := getPullRequestTargetRolesCodemod()
	assert.Equal(t, "pull-request-target-restrict-roles", mod.ID)
	assert.NotEmpty(t, mod.Name)
	assert.NotEmpty(t, mod.Description)
	assert.Equal(t, "1.5.0", mod.IntroducedIn)
	assert.False(t, mod.Guided)
	assert.NotNil(t, mod.Apply)
}

func TestPullRequestTargetRolesCodemod(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		fields string
		want   []string
	}{
		{"read scalar", "on: {pull_request_target: null, roles: read}", []string{"admin", "maintainer", "write"}},
		{"all scalar", "on: {pull_request_target: null, roles: all}", []string{"admin", "maintainer", "write"}},
		{"unsafe only", "on: {pull_request_target: null, roles: [read, all]}", []string{"admin", "maintainer", "write"}},
		{"narrow list", "on: {pull_request_target: null, roles: [admin, read]}", []string{"admin"}},
		{"mixed list preserves order", "on: {pull_request_target: null, roles: [write, read, admin, all]}", []string{"write", "admin"}},
		{"other supported roles", "on: {pull_request_target: null, roles: [maintain, triage, read]}", []string{"maintain", "triage"}},
		{"block list", "on:\n  pull_request_target:\n  roles:\n    - read\n    - write", []string{"write"}},
		{"mixed triggers", "on: {pull_request_target: null, workflow_dispatch: null, roles: read}", []string{"admin", "maintainer", "write"}},
		{"acknowledgment cannot bypass", "on: {pull_request_target: {acknowledge-risk: true}, roles: read}\ncheckout: false", []string{"admin", "maintainer", "write"}},
		{"trusted list", "on: {pull_request_target: null, roles: [admin, write]}", nil},
		{"trusted scalar", "on: {pull_request_target: null, roles: write}", nil},
		{"unknown role without restriction", "on: {pull_request_target: null, roles: [admin, maintainers]}", nil},
		{"unknown scalar without restriction", "on: {pull_request_target: null, roles: maintainers}", nil},
		{"nonstring entry without restriction", "on: {pull_request_target: null, roles: [admin, true]}", nil},
		{"mapping entry without restriction", "on: {pull_request_target: null, roles: [admin, {key: value}]}", nil},
		{"null without restriction", "on: {pull_request_target: null, roles: null}", nil},
		{"boolean without restriction", "on: {pull_request_target: null, roles: true}", nil},
		{"mapping without restriction", "on: {pull_request_target: null, roles: {key: value}}", nil},
		{"default roles", "on: {pull_request_target: null}", nil},
		{"non target", "on: {pull_request: null, roles: read}", nil},
		{"exact trigger", "on: {pull_request_target_staging: null, roles: all}", nil},
		{"string trigger", "on: pull_request_target", nil},
		{"list trigger", "on: [push, pull_request_target]", nil},
		{"nonstrict", "strict: false\non: {pull_request_target: null, roles: read}", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			content := "---\n" + tt.fields + "\n---\n\n# Prompt\nKeep this body.\n"
			document, err := aw.Parse(content)
			require.NoError(t, err)
			mod := getPullRequestTargetRolesCodemod()
			result, applied, err := mod.Apply(content, document.Frontmatter)
			require.NoError(t, err)
			assert.Equal(t, tt.want != nil, applied)
			if tt.want == nil {
				assert.Equal(t, content, result)
				return
			}
			updated, err := aw.Parse(result)
			require.NoError(t, err)
			on := updated.Frontmatter["on"].(map[string]any)
			var expected []any
			for _, role := range tt.want {
				expected = append(expected, role)
			}
			assert.Equal(t, expected, on["roles"])
			assert.Equal(t, document.Markdown, updated.Markdown)
			again, applied, err := mod.Apply(result, updated.Frontmatter)
			require.NoError(t, err)
			assert.False(t, applied)
			assert.Equal(t, result, again)
		})
	}
}

func TestPullRequestTargetRolesCodemodPreservesSource(t *testing.T) {
	t.Parallel()
	content := "---\n# Trigger comment\non:\n  pull_request_target: # event comment\n  roles: read # role comment\ncheckout: false # checkout comment\n---\n\n# Prompt\nUntouched.\n"
	document, err := aw.Parse(content)
	require.NoError(t, err)
	result, applied, err := getPullRequestTargetRolesCodemod().Apply(content, document.Frontmatter)
	require.NoError(t, err)
	assert.True(t, applied)
	for _, preserved := range []string{"# Trigger comment", "  pull_request_target: # event comment", "# role comment", "checkout: false # checkout comment", "\n---\n\n# Prompt\nUntouched.\n"} {
		assert.Contains(t, result, preserved)
	}
}

func TestPullRequestTargetRolesCodemodMalformedRoles(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{"[admin, {key: value}, read]", "[unknown, read]", "[admin, true, all]", "[read, null]", "[read, 1]", "[read, maintainers]"} {
		t.Run(raw, func(t *testing.T) {
			t.Parallel()
			content := "---\non: {pull_request_target: null, roles: " + raw + "}\n---\n"
			document, err := aw.Parse(content)
			require.NoError(t, err)
			result, applied, err := getPullRequestTargetRolesCodemod().Apply(content, document.Frontmatter)
			require.ErrorContains(t, err, "on.roles")
			assert.False(t, applied)
			assert.Equal(t, content, result)
		})
	}
}
