//go:build !integration

package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCodemodTypes(t *testing.T) {
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
	// Test that the CodemodResult type has all required fields
	result := CodemodResult{
		Applied: true,
		Message: "Test message",
	}

	assert.True(t, result.Applied, "Applied should be true")
	assert.Equal(t, "Test message", result.Message, "Message should be set")
}

func TestGetAllCodemods_ReturnsAllCodemods(t *testing.T) {
	codemods := GetAllCodemods()

	// Verify we have the expected number of codemods
	expectedCount := 17
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
		"safe-inputs-mode-removal",
		"upload-assets-to-upload-asset-migration",
		"write-permissions-to-read-migration",
		"permissions-read-to-read-all",
		"agent-task-to-agent-session-migration",
		"sandbox-agent-false-removal",
		"schedule-at-to-around-migration",
		"delete-schema-file",
		"grep-tool-removal",
		"mcp-network-to-top-level-migration",
	}

	for _, expectedID := range expectedIDs {
		assert.True(t, codemodIDs[expectedID], "Expected codemod with ID %s to be present", expectedID)
	}
}

func TestGetAllCodemods_NoduplicateIDs(t *testing.T) {
	codemods := GetAllCodemods()

	// Check for duplicate IDs
	seenIDs := make(map[string]bool)
	for _, codemod := range codemods {
		assert.False(t, seenIDs[codemod.ID], "Duplicate codemod ID found: %s", codemod.ID)
		seenIDs[codemod.ID] = true
	}
}

func TestGetAllCodemods_InExpectedOrder(t *testing.T) {
	codemods := GetAllCodemods()

	// Verify codemods are returned in the expected order
	// This is important for consistent behavior
	expectedOrder := []string{
		"timeout-minutes-migration",
		"network-firewall-migration",
		"command-to-slash-command-migration",
		"safe-inputs-mode-removal",
		"upload-assets-to-upload-asset-migration",
		"write-permissions-to-read-migration",
		"permissions-read-to-read-all",
		"agent-task-to-agent-session-migration",
		"sandbox-agent-false-removal",
		"schedule-at-to-around-migration",
		"delete-schema-file",
		"grep-tool-removal",
		"mcp-network-to-top-level-migration",
		"add-comment-discussion-removal",
		"mcp-mode-to-type-migration",
		"install-script-url-migration",
		"bash-anonymous-removal",
	}

	require.Len(t, codemods, len(expectedOrder), "Should have expected number of codemods")

	for i, expectedID := range expectedOrder {
		assert.Equal(t, expectedID, codemods[i].ID, "Codemod at position %d should have ID %s", i, expectedID)
	}
}
