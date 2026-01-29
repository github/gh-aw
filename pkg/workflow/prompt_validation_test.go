//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGeneratedWorkflowsValidatePromptStep tests that all generated workflows
// include the prompt validation step
func TestGeneratedWorkflowsValidatePromptStep(t *testing.T) {
	// Get the workflows directory
	workflowsDir := filepath.Join("..", "..", ".github", "workflows")

	// Check if directory exists
	if _, err := os.Stat(workflowsDir); os.IsNotExist(err) {
		t.Skip("Workflows directory not found, skipping test")
	}

	// Read all .lock.yml files
	files, err := filepath.Glob(filepath.Join(workflowsDir, "*.lock.yml"))
	require.NoError(t, err, "Should be able to list lock files")

	if len(files) == 0 {
		t.Skip("No lock files found, skipping test")
	}

	// Check each workflow
	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			content, err := os.ReadFile(file)
			require.NoError(t, err, "Should be able to read lock file")

			lockStr := string(content)

			// Skip workflows that don't have agent jobs (some workflows might not need prompts)
			if !strings.Contains(lockStr, "name: agent") {
				t.Skip("Workflow doesn't have agent job")
			}

			// Check for the validation step
			assert.Contains(t, lockStr, "Validate prompt placeholders",
				"Workflow should include prompt validation step")

			// Check that validation script is called
			assert.Contains(t, lockStr, "validate_prompt_placeholders.sh",
				"Workflow should call validation script")

			// Verify the validation step comes after interpolation
			interpolatePos := strings.Index(lockStr, "Interpolate variables and render templates")
			validatePos := strings.Index(lockStr, "Validate prompt placeholders")

			if interpolatePos != -1 && validatePos != -1 {
				assert.Less(t, interpolatePos, validatePos,
					"Validation should come after interpolation")
			}

			// Verify validation comes before print
			printPos := strings.Index(lockStr, "Print prompt")
			if validatePos != -1 && printPos != -1 {
				assert.Less(t, validatePos, printPos,
					"Validation should come before print")
			}
		})
	}
}

// TestGeneratedWorkflowsPromptStructure tests that generated workflows
// have proper prompt structure with system tags and ordering
func TestGeneratedWorkflowsPromptStructure(t *testing.T) {
	workflowsDir := filepath.Join("..", "..", ".github", "workflows")

	if _, err := os.Stat(workflowsDir); os.IsNotExist(err) {
		t.Skip("Workflows directory not found, skipping test")
	}

	files, err := filepath.Glob(filepath.Join(workflowsDir, "*.lock.yml"))
	require.NoError(t, err)

	if len(files) == 0 {
		t.Skip("No lock files found")
	}

	// Sample a few workflows to test
	sampleSize := 5
	if len(files) > sampleSize {
		files = files[:sampleSize]
	}

	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			content, err := os.ReadFile(file)
			require.NoError(t, err)

			lockStr := string(content)

			// Skip workflows without agent jobs
			if !strings.Contains(lockStr, "name: agent") {
				t.Skip("Workflow doesn't have agent job")
			}

			// Check for system tags in the prompt creation
			if strings.Contains(lockStr, "Create prompt with built-in context") {
				// Should have opening system tag
				assert.Contains(t, lockStr, "<system>",
					"Workflow should have opening system tag")

				// Should have closing system tag
				assert.Contains(t, lockStr, "</system>",
					"Workflow should have closing system tag")

				// Verify system tags come in order
				systemOpenPos := strings.Index(lockStr, "<system>")
				systemClosePos := strings.Index(lockStr, "</system>")

				if systemOpenPos != -1 && systemClosePos != -1 {
					assert.Less(t, systemOpenPos, systemClosePos,
						"Opening system tag should come before closing tag")
				}
			}
		})
	}
}

// TestGeneratedWorkflowsPlaceholderFormat tests that placeholders in generated
// workflows follow the correct format and are in appropriate locations
func TestGeneratedWorkflowsPlaceholderFormat(t *testing.T) {
	workflowsDir := filepath.Join("..", "..", ".github", "workflows")

	if _, err := os.Stat(workflowsDir); os.IsNotExist(err) {
		t.Skip("Workflows directory not found, skipping test")
	}

	files, err := filepath.Glob(filepath.Join(workflowsDir, "*.lock.yml"))
	require.NoError(t, err)

	if len(files) == 0 {
		t.Skip("No lock files found")
	}

	// Sample one workflow for detailed check
	file := files[0]
	content, err := os.ReadFile(file)
	require.NoError(t, err)

	lockStr := string(content)

	// Skip if no agent job
	if !strings.Contains(lockStr, "name: agent") {
		t.Skip("Workflow doesn't have agent job")
	}

	// Find all __GH_AW_*__ placeholders
	// These should only appear in:
	// 1. Heredoc content (between cat << 'PROMPT_EOF' and PROMPT_EOF)
	// 2. Environment variable values

	// Count placeholders
	placeholderCount := strings.Count(lockStr, "__GH_AW_")
	if placeholderCount > 0 {
		t.Logf("Found %d placeholder occurrences in %s", placeholderCount, filepath.Base(file))

		// This is expected - placeholders should be in the heredoc content
		// They will be replaced at runtime by the substitution step

		// Verify that these placeholders are NOT in step names or other critical areas
		assert.NotContains(t, lockStr, "name: __GH_AW_",
			"Placeholders should not be in step names")
		assert.NotContains(t, lockStr, "uses: __GH_AW_",
			"Placeholders should not be in action uses")
	}
}
