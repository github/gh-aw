//go:build !integration

package workflow

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEmitGitHubLockdownGuardPolicyWarningExactText validates that the
// compile-time lockdown/guard-policy conflict warning matches, byte-for-byte,
// the example message documented in
// scratchpad/github-mcp-access-control-specification.md §9.5.2.
func TestEmitGitHubLockdownGuardPolicyWarningExactText(t *testing.T) {
	tools := NewTools(map[string]any{
		"github": map[string]any{
			"lockdown":      true,
			"allowed-repos": "all",
			"min-integrity": "approved",
		},
	})
	require.NoError(t, validateGitHubGuardPolicy(tools, "test-workflow"))

	compiler := NewCompiler()
	stderrOutput := captureStderr(func() {
		emitGitHubLockdownGuardPolicyWarning(compiler, tools, "test-workflow.md")
	})

	const specExampleMessage = `'tools.github.lockdown: true' is set; GitHub guard policy fields ('allowed-repos', 'min-integrity', 'blocked-users', 'trusted-users', 'approval-labels') will be ignored.
Guard policies are only evaluated when lockdown is not active.`

	assert.Equal(t, specExampleMessage, githubLockdownGuardPolicyWarningMessage,
		"the implementation warning message must stay byte-identical to the §9.5.2 example")
	assert.Contains(t, stderrOutput, specExampleMessage,
		"the emitted warning must contain the exact §9.5.2 example message")
}

// TestHasGitHubLockdownGuardPolicyConflictDeprecatedReposField covers the
// deprecated 'repos' alias branch directly on GitHubToolConfig, which the
// frontmatter parser never populates because it normalizes 'repos' into
// 'allowed-repos'.
func TestHasGitHubLockdownGuardPolicyConflictDeprecatedReposField(t *testing.T) {
	github := &GitHubToolConfig{Lockdown: true, Repos: GitHubReposScope{"all"}}
	assert.True(t, hasGitHubGuardPolicyFields(github),
		"deprecated 'repos' alias must count as a configured guard-policy field")
	assert.True(t, hasGitHubLockdownGuardPolicyConflict(github),
		"lockdown combined with deprecated 'repos' alias must be reported as a conflict")

	tools := &Tools{GitHub: github}
	compiler := NewCompiler()
	stderrOutput := captureStderr(func() {
		emitGitHubLockdownGuardPolicyWarning(compiler, tools, "test-workflow.md")
	})

	assert.Contains(t, stderrOutput, githubLockdownGuardPolicyWarningMessage)
	assert.Equal(t, 1, compiler.GetWarningCount())
}

// TestValidateGitHubGuardPolicyNormalizesDeprecatedReposField verifies that
// validation treats a struct-literal 'repos' value identically to
// 'allowed-repos', including the min-integrity requirement.
func TestValidateGitHubGuardPolicyNormalizesDeprecatedReposField(t *testing.T) {
	tools := &Tools{GitHub: &GitHubToolConfig{Repos: GitHubReposScope{"all"}}}
	err := validateGitHubGuardPolicy(tools, "test-workflow")
	require.Error(t, err)
	require.ErrorContains(t, err, "'github.min-integrity' is required")

	tools = &Tools{GitHub: &GitHubToolConfig{Repos: GitHubReposScope{"not-a-valid-scope"}, MinIntegrity: GitHubIntegrityApproved}}
	err = validateGitHubGuardPolicy(tools, "test-workflow")
	require.Error(t, err)
	require.ErrorContains(t, err, "must be in format")

	tools = &Tools{GitHub: &GitHubToolConfig{Repos: GitHubReposScope{"all"}, MinIntegrity: GitHubIntegrityApproved}}
	require.NoError(t, validateGitHubGuardPolicy(tools, "test-workflow"))
	assert.Equal(t, GitHubReposScope{"all"}, tools.GitHub.AllowedRepos,
		"deprecated 'repos' alias must be normalized into 'allowed-repos'")
}

func TestGitHubAllowedReposTakesPrecedenceOverDeprecatedRepos(t *testing.T) {
	frontmatter := map[string]any{
		"tools": map[string]any{
			"github": map[string]any{
				"repos":         []any{"legacy/repo"},
				"allowed-repos": []any{"current/repo"},
				"min-integrity": "approved",
			},
		},
	}
	tools := NewTools(frontmatter["tools"].(map[string]any))
	require.NoError(t, validateGitHubGuardPolicy(tools, "test-workflow"))
	assert.Equal(t, GitHubReposScope{"current/repo"}, tools.GitHub.AllowedRepos)

	compiler := NewCompiler()
	stderrOutput := captureStderr(func() {
		compiler.warnDeprecatedFrontmatterFields(frontmatter)
	})

	assert.Contains(t, stderrOutput, "'tools.github.repos' is ignored")
	assert.Contains(t, stderrOutput, "'tools.github.allowed-repos' is set")
}

// TestEmitGitHubLockdownGuardPolicyWarningWhenValidationFails verifies that the
// lockdown warning is still emitted when guard-policy validation fails, so the
// warning and error paths are not mutually exclusive.
func TestEmitGitHubLockdownGuardPolicyWarningWhenValidationFails(t *testing.T) {
	tools := NewTools(map[string]any{
		"github": map[string]any{
			"lockdown":      true,
			"allowed-repos": "all",
		},
	})

	err := validateGitHubGuardPolicy(tools, "test-workflow")
	require.Error(t, err)
	require.ErrorContains(t, err, "'github.min-integrity' is required")

	compiler := NewCompiler()
	stderrOutput := captureStderr(func() {
		emitGitHubLockdownGuardPolicyWarning(compiler, tools, "test-workflow.md")
	})

	assert.Contains(t, stderrOutput, githubLockdownGuardPolicyWarningMessage)
	assert.Equal(t, 1, compiler.GetWarningCount())
}
