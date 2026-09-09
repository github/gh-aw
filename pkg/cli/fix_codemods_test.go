//go:build !integration

package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCodemodTypes(t *testing.T) {
	t.Parallel()
	// Test that the Codemod type has all required fields
	codemod := Codemod{
		ID:           "test-id",
		Name:         "Test Name",
		Description:  "Test Description",
		IntroducedIn: "1.0.0",
		Apply: func(content string, frontmatter map[string]any) (string, bool, error) {
			return content, false, nil
		},
	}

	assert.Equal(t, "test-id", codemod.ID, "ID should be set")
	assert.Equal(t, "Test Name", codemod.Name, "Name should be set")
	assert.Equal(t, "Test Description", codemod.Description, "Description should be set")
	assert.Equal(t, "1.0.0", codemod.IntroducedIn, "IntroducedIn should be set")
	require.NotNil(t, codemod.Apply, "Apply function should be set")
}

func TestCodemodResultType(t *testing.T) {
	t.Parallel()
	// Test that the CodemodResult type has all required fields
	result := CodemodResult{
		Applied: true,
		Message: "Test message",
	}

	assert.True(t, result.Applied, "Applied should be true")
	assert.Equal(t, "Test message", result.Message, "Message should be set")
}

func TestGetAllCodemods_ReturnsAllCodemods(t *testing.T) {
	t.Parallel()
	codemods := GetAllCodemods()

	// Verify we have the expected number of codemods
	expectedCount := len(expectedCodemodOrder())
	assert.Len(t, codemods, expectedCount, "Should return all %d codemods", expectedCount)

	// Verify all codemods have required fields
	for i, codemod := range codemods {
		assert.NotEmpty(t, codemod.ID, "Codemod %d should have an ID", i)
		assert.NotEmpty(t, codemod.Name, "Codemod %d should have a Name", i)
		assert.NotEmpty(t, codemod.Description, "Codemod %d should have a Description", i)
		assert.NotEmpty(t, codemod.IntroducedIn, "Codemod %d should have IntroducedIn version", i)
		require.NotNil(t, codemod.Apply, "Codemod %d should have an Apply function", i)
	}
}

func TestGetAllCodemods_ContainsExpectedCodemods(t *testing.T) {
	t.Parallel()
	codemods := GetAllCodemods()

	// Build a map of codemod IDs
	codemodIDs := make(map[string]bool)
	for _, codemod := range codemods {
		codemodIDs[codemod.ID] = true
	}

	// Verify all expected codemods are present
	expectedIDs := []string{
		"timeout-minutes-migration",
		"network-firewall-migration",
		"command-to-slash-command-migration",
		"workflow-dispatch-required-false-with-slash-command",
		"mcp-scripts-mode-removal",
		"upload-assets-to-upload-asset-migration",
		"write-permissions-to-read-migration",
		"permissions-read-to-read-all",
		"agent-task-to-agent-session-migration",
		"sandbox-false-to-agent-false",
		"schedule-at-to-around-migration",
		"delete-schema-file",
		"grep-tool-removal",
		"mcp-network-to-top-level-migration",
		"add-comment-discussion-removal",
		"discussion-trigger-categories-lowercase",
		"mcp-mode-to-type-migration",
		"install-script-url-migration",
		"bash-anonymous-removal",
		"activation-outputs-to-sanitized-step",
		"roles-to-on-roles",
		"bots-to-on-bots",
		"engine-steps-to-top-level",
		"assign-to-agent-default-agent-to-name",
		"playwright-allowed-domains-migration",
		"playwright-cli-mode-removal",
		"expires-integer-to-string",
		"app-to-github-app",
		"github-app-app-id-to-client-id",
		"safe-output-title-prefix-to-required-title-prefix",
		"safe-output-merge-pr-constraints",
		"safe-output-add-reviewer-allowlists",
		"safe-output-dispatch-repository-key",
		"safe-job-runner-to-runs-on",
		"safe-inputs-to-mcp-scripts",
		"rate-limit-to-user-rate-limit",
		"effective-tokens-to-ai-credits",
		"messages-effective-tokens-suffix-to-ai-credits-suffix",
		"engine-max-runs-to-top-level",
		"max-runs-to-max-turns",
		"engine-max-turns-to-top-level",
		"steps-run-secrets-to-env",
		"engine-env-secrets-to-engine-config",
		"top-level-env-secrets-guided-error",
		"serena-tools-to-shared-import",
		"workflow-run-branches-default",
		"checkout-persist-credentials-false",
		"pull-request-target-checkout-false",
		"dependabot-toolset-permissions",
		"github-repos-to-allowed-repos",
		"toolset-singular-to-toolsets",
		"allowed-repos-current-to-github-repository",
		"features-copilot-requests-to-permissions",
		"features-byok-copilot-removal",
		"features-inline-agents-removal",
		"features-cli-proxy-to-tools-github-mode",
		"features-difc-proxy-to-tools-github",
		"mount-as-clis-to-cli-proxy",
		"cli-proxy-false-when-bash-disabled",
		"sandbox-mcp-container-removal",
		"sandbox-mcp-version-removal",
		"bash-single-quoted-args-rewrite",
		"bash-allowlist-unsupported-engine-guided-error",
		"infer-to-disable-model-invocation",
		"run-install-scripts-to-runtimes-node",
		"mentions-allow-team-members-to-allowed-collaborators",
		"engine-copilot-sdk-driver-to-driver",
	}

	for _, expectedID := range expectedIDs {
		assert.True(t, codemodIDs[expectedID], "Expected codemod with ID %s to be present", expectedID)
	}
}

func TestGetAllCodemods_NoduplicateIDs(t *testing.T) {
	t.Parallel()
	codemods := GetAllCodemods()

	// Check for duplicate IDs
	seenIDs := make(map[string]bool)
	for _, codemod := range codemods {
		assert.False(t, seenIDs[codemod.ID], "Duplicate codemod ID found: %s", codemod.ID)
		seenIDs[codemod.ID] = true
	}
}

func TestGetCodemods_DisablesRequestedCodemods(t *testing.T) {
	t.Parallel()
	codemods, err := GetCodemods([]string{"timeout-minutes-migration", "network-firewall-migration"})
	require.NoError(t, err)

	var ids []string
	for _, codemod := range codemods {
		ids = append(ids, codemod.ID)
	}

	assert.NotContains(t, ids, "timeout-minutes-migration")
	assert.NotContains(t, ids, "network-firewall-migration")
	assert.Contains(t, ids, "command-to-slash-command-migration")
}

func TestGetCodemods_UnknownDisabledCodemodReturnsError(t *testing.T) {
	t.Parallel()
	codemods, err := GetCodemods([]string{"not-a-real-codemod"})
	require.Error(t, err)
	assert.Nil(t, codemods)
	require.ErrorContains(t, err, "unknown codemod ID(s): not-a-real-codemod")
}

func TestGetAllCodemods_InExpectedOrder(t *testing.T) {
	t.Parallel()
	codemods := GetAllCodemods()

	// Verify codemods are returned in the expected order
	// This is important for consistent behavior
	expectedOrder := expectedCodemodOrder()
	require.Len(t, codemods, len(expectedOrder), "Should have expected number of codemods")

	for i, expectedID := range expectedOrder {
		assert.Equal(t, expectedID, codemods[i].ID, "Codemod at position %d should have ID %s", i, expectedID)
	}
}

func expectedCodemodOrder() []string {
	return []string{
		"timeout-minutes-migration",
		"network-firewall-migration",
		"command-to-slash-command-migration",
		"workflow-dispatch-required-false-with-slash-command",
		"mcp-scripts-mode-removal",
		"upload-assets-to-upload-asset-migration",
		"write-permissions-to-read-migration",
		"permissions-read-to-read-all",
		"agent-task-to-agent-session-migration",
		"sandbox-false-to-agent-false",
		"schedule-at-to-around-migration",
		"delete-schema-file",
		"grep-tool-removal",
		"mcp-network-to-top-level-migration",
		"add-comment-discussion-removal",
		"discussion-trigger-categories-lowercase",
		"mcp-mode-to-type-migration",
		"install-script-url-migration",
		"bash-anonymous-removal",
		"bash-single-quoted-args-rewrite",
		"bash-allowlist-unsupported-engine-guided-error",
		"activation-outputs-to-sanitized-step",
		"roles-to-on-roles",
		"bots-to-on-bots",
		"engine-steps-to-top-level",
		"engine-max-runs-to-top-level",
		"max-runs-to-max-turns",
		"engine-max-turns-to-top-level",
		"steps-run-secrets-to-env",
		"engine-env-secrets-to-engine-config",
		"top-level-env-secrets-guided-error",
		"assign-to-agent-default-agent-to-name",
		"playwright-allowed-domains-migration",
		"playwright-cli-mode-removal",
		"expires-integer-to-string",
		"app-to-github-app",
		"github-app-app-id-to-client-id",
		"safe-output-title-prefix-to-required-title-prefix",
		"safe-output-merge-pr-constraints",
		"safe-output-add-reviewer-allowlists",
		"safe-output-dispatch-repository-key",
		"safe-job-runner-to-runs-on",
		"safe-inputs-to-mcp-scripts",
		"rate-limit-to-user-rate-limit",
		"effective-tokens-to-ai-credits",
		"messages-effective-tokens-suffix-to-ai-credits-suffix",
		"serena-mcp-location-migration",
		"serena-tools-to-shared-import",
		"workflow-run-branches-default",
		"checkout-persist-credentials-false",
		"pull-request-target-checkout-false",
		"dependabot-toolset-permissions",
		"github-repos-to-allowed-repos",
		"toolset-singular-to-toolsets",
		"allowed-repos-current-to-github-repository",
		"features-copilot-requests-to-permissions",
		"features-byok-copilot-removal",
		"features-inline-agents-removal",
		"features-cli-proxy-to-tools-github-mode",
		"features-difc-proxy-to-tools-github",
		"mount-as-clis-to-cli-proxy",
		"min-integrity-none-requires-bash",
		"cli-proxy-false-when-bash-disabled",
		"sandbox-mcp-container-removal",
		"sandbox-mcp-version-removal",
		"sandbox-runtime-profiles",
		"infer-to-disable-model-invocation",
		"run-install-scripts-to-runtimes-node",
		"mentions-allow-team-members-to-allowed-collaborators",
		"engine-copilot-sdk-driver-to-driver",
	}
}
