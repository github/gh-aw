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

// TestGitHubMCPToolsPromptBoundsCodeScanningAlertQueries verifies that the
// github_mcp_tools_prompt.md file includes the required guardrail instructing
// agents to always bound list_code_scanning_alerts calls with state: open and
// severity: critical,high. Unbounded queries produce oversized responses that
// break downstream workflow runs.
func TestGitHubMCPToolsPromptBoundsCodeScanningAlertQueries(t *testing.T) {
	promptPath := filepath.Join("..", "..", "actions", "setup", "md", "github_mcp_tools_prompt.md")
	data, err := os.ReadFile(promptPath)
	require.NoError(t, err, "should be able to read github_mcp_tools_prompt.md")

	content := string(data)

	assert.Contains(t, content, "list_code_scanning_alerts",
		"prompt should mention list_code_scanning_alerts to set agent expectations")
	assert.Contains(t, content, "state: open",
		"prompt should require state: open to bound list_code_scanning_alerts queries")
	assert.Contains(t, content, "severity: critical,high",
		"prompt should require severity: critical,high to bound list_code_scanning_alerts queries")
}

// TestGitHubMCPToolsWithSafeOutputsPromptBoundsCodeScanningAlertQueries verifies that
// the github_mcp_tools_with_safeoutputs_prompt.md file includes the same guardrail.
func TestGitHubMCPToolsWithSafeOutputsPromptBoundsCodeScanningAlertQueries(t *testing.T) {
	promptPath := filepath.Join("..", "..", "actions", "setup", "md", "github_mcp_tools_with_safeoutputs_prompt.md")
	data, err := os.ReadFile(promptPath)
	require.NoError(t, err, "should be able to read github_mcp_tools_with_safeoutputs_prompt.md")

	content := string(data)

	assert.Contains(t, content, "list_code_scanning_alerts",
		"prompt should mention list_code_scanning_alerts to set agent expectations")
	assert.Contains(t, content, "state: open",
		"prompt should require state: open to bound list_code_scanning_alerts queries")
	assert.Contains(t, content, "severity: critical,high",
		"prompt should require severity: critical,high to bound list_code_scanning_alerts queries")
}

// TestGitHubMCPToolsPromptHasFieldSelectionGuidance verifies that both prompt files
// contain guidance about the fields parameter introduced in GitHub MCP server 1.6.0.
// Field selection allows agents to request only the fields they need, significantly
// reducing response size for list/search tools.
func TestGitHubMCPToolsPromptHasFieldSelectionGuidance(t *testing.T) {
	promptFiles := []string{
		filepath.Join("..", "..", "actions", "setup", "md", "github_mcp_tools_prompt.md"),
		filepath.Join("..", "..", "actions", "setup", "md", "github_mcp_tools_with_safeoutputs_prompt.md"),
	}

	for _, promptPath := range promptFiles {
		t.Run(filepath.Base(promptPath), func(t *testing.T) {
			data, err := os.ReadFile(promptPath)
			require.NoError(t, err, "should be able to read %s", promptPath)

			content := string(data)

			assert.Contains(t, content, "fields",
				"prompt should mention fields parameter for response size reduction")
			assert.Contains(t, content, "list_pull_requests",
				"prompt should reference list_pull_requests as a tool that supports fields")
			assert.Contains(t, content, "1.6.0",
				"prompt should reference the GitHub MCP server version that introduced fields")
		})
	}
}

// TestGitHubMCPToolsPromptHasPartialFileReadGuidance verifies that both prompt
// files steer agents away from unbounded single-file get_file_contents calls.
func TestGitHubMCPToolsPromptHasPartialFileReadGuidance(t *testing.T) {
	promptFiles := []string{
		filepath.Join("..", "..", "actions", "setup", "md", "github_mcp_tools_prompt.md"),
		filepath.Join("..", "..", "actions", "setup", "md", "github_mcp_tools_with_safeoutputs_prompt.md"),
	}

	for _, promptPath := range promptFiles {
		t.Run(filepath.Base(promptPath), func(t *testing.T) {
			data, err := os.ReadFile(promptPath)
			require.NoError(t, err, "should be able to read %s", promptPath)

			content := string(data)

			assert.Contains(t, content, "`fields` only reduces directory listings",
				"prompt should clarify get_file_contents fields do not reduce single-file content")
			assert.Contains(t, content, "fields: [name, type, size, path]",
				"prompt should recommend metadata-only directory listings before file reads")
			assert.Contains(t, content, "Partial file reads",
				"prompt should include explicit partial-content guidance")
			assert.Contains(t, content, "bounded excerpt",
				"prompt should recommend bounded excerpts for headers or sections")
		})
	}
}

// TestSafeOutputsPromptDoesNotRequireCLI verifies that the base safe-output
// guidance remains transport-neutral. Some workflows expose safeoutputs as MCP
// tools rather than CLI commands, so CLI-only guidance can cause agents to
// finish without emitting any safe-output items.
func TestSafeOutputsPromptDoesNotRequireCLI(t *testing.T) {
	promptPath := filepath.Join("..", "..", "actions", "setup", "md", safeOutputsMCPTransportPromptFile)
	data, err := os.ReadFile(promptPath)
	require.NoError(t, err, "should be able to read the MCP safe-output transport prompt")

	content := string(data)

	assert.Contains(t, content, "Call the tool names listed in `<safe-output-tools>` directly",
		"safe-output prompt should instruct agents to call configured safe-output tools")
	assert.Contains(t, content, "if a separate `<mcp-clis>` section says `safeoutputs` is available on `PATH`, you may use that CLI form instead",
		"safe-output prompt should describe CLI usage as optional because CLI mounting is optional")
}

// TestComposedPromptSafeOutputsGuidanceStaysTransportNeutral verifies that
// later prompt fragments (like mcp_cli_tools_prompt.md) do not override direct
// safe-output tool guidance in the final composed prompt.
func TestComposedPromptSafeOutputsGuidanceStaysTransportNeutral(t *testing.T) {
	compiler := NewCompiler()
	data := &WorkflowData{
		SafeOutputs: &SafeOutputsConfig{
			NoOp: &NoOpConfig{},
		},
	}

	sections := compiler.collectPromptSections(data)
	require.NotEmpty(t, sections, "should collect prompt sections")

	wd, err := os.Getwd()
	require.NoError(t, err)
	promptDir := filepath.Clean(filepath.Join(wd, "..", "..", "actions", "setup", "md"))

	var composed strings.Builder
	for _, section := range sections {
		content := section.Content
		if section.IsFile {
			fileBytes, readErr := os.ReadFile(filepath.Clean(filepath.Join(promptDir, section.Content)))
			require.NoError(t, readErr, "should read prompt fragment %s", section.Content)
			content = string(fileBytes)
		}

		for key, value := range section.EnvVars {
			content = strings.ReplaceAll(content, "__"+key+"__", value)
		}

		composed.WriteString(content)
		composed.WriteString("\n")
	}

	finalPrompt := composed.String()
	assert.Contains(t, finalPrompt, "Call the tool names listed in `<safe-output-tools>` directly",
		"final composed prompt should preserve direct safe-output tool guidance")
	assert.NotContains(t, finalPrompt, "For `safeoutputs` and `mcpscripts`, always use the CLI commands above.",
		"final composed prompt should not require safeoutputs CLI usage unconditionally")
	assert.Contains(t, finalPrompt, "if a separate `<mcp-clis>` section says `safeoutputs` is available on `PATH`, you may use that CLI form instead",
		"final composed prompt should keep safeoutputs CLI guidance optional when safe-output tools are available")
}

// TestComposedPromptForNonMCPEngineStatesCLIIsOnlyTransport verifies that engines
// without native MCP tool-calling (EngineCapabilities.MCP == false, e.g. `pi`) are
// told the safeoutputs CLI is the only transport, instead of being told the CLI is
// an optional alternative to a direct tool call that does not exist for them.
func TestComposedPromptForNonMCPEngineStatesCLIIsOnlyTransport(t *testing.T) {
	compiler := NewCompiler()
	data := &WorkflowData{
		EngineConfig: &EngineConfig{ID: "pi"},
		SafeOutputs: &SafeOutputsConfig{
			NoOp: &NoOpConfig{},
		},
	}

	sections := compiler.collectPromptSections(data)
	require.NotEmpty(t, sections, "should collect prompt sections")

	wd, err := os.Getwd()
	require.NoError(t, err)
	promptDir := filepath.Clean(filepath.Join(wd, "..", "..", "actions", "setup", "md"))

	var composed strings.Builder
	for _, section := range sections {
		content := section.Content
		if section.IsFile {
			fileBytes, readErr := os.ReadFile(filepath.Clean(filepath.Join(promptDir, section.Content)))
			require.NoError(t, readErr, "should read prompt fragment %s", section.Content)
			content = string(fileBytes)
		}

		for key, value := range section.EnvVars {
			content = strings.ReplaceAll(content, "__"+key+"__", value)
		}

		composed.WriteString(content)
		composed.WriteString("\n")
	}

	finalPrompt := composed.String()
	assert.NotContains(t, finalPrompt, "optional equivalent transport",
		"engines without MCP must not be told the safeoutputs CLI is optional")
	assert.NotContains(t, finalPrompt, "you may use that CLI form instead",
		"engines without MCP must not be told the safeoutputs CLI is an alternative")
	assert.Contains(t, finalPrompt, "the `safeoutputs` CLI on `PATH` is the ONLY way to invoke the tools listed in `<safe-output-tools>`",
		"engines without MCP must be told the safeoutputs CLI is the only transport")
}

func TestComposedPromptForCompilerRegisteredNonMCPEngineStatesCLIIsOnlyTransport(t *testing.T) {
	compiler := NewCompiler()
	compiler.engineCatalog.Register(&EngineDefinition{
		ID:        "custom-no-mcp",
		RuntimeID: "pi",
	})
	data := &WorkflowData{
		EngineConfig: &EngineConfig{ID: "custom-no-mcp"},
		SafeOutputs: &SafeOutputsConfig{
			NoOp: &NoOpConfig{},
		},
	}

	sections := compiler.collectPromptSections(data)
	require.NotEmpty(t, sections, "should collect prompt sections")

	wd, err := os.Getwd()
	require.NoError(t, err)
	promptDir := filepath.Clean(filepath.Join(wd, "..", "..", "actions", "setup", "md"))

	var composed strings.Builder
	for _, section := range sections {
		content := section.Content
		if section.IsFile {
			fileBytes, readErr := os.ReadFile(filepath.Clean(filepath.Join(promptDir, section.Content)))
			require.NoError(t, readErr, "should read prompt fragment %s", section.Content)
			content = string(fileBytes)
		}

		for key, value := range section.EnvVars {
			content = strings.ReplaceAll(content, "__"+key+"__", value)
		}

		composed.WriteString(content)
		composed.WriteString("\n")
	}

	finalPrompt := composed.String()
	assert.NotContains(t, finalPrompt, "optional equivalent transport",
		"compiler-registered engines without MCP must not describe the safeoutputs CLI as optional")
	assert.Contains(t, finalPrompt, "the `safeoutputs` CLI on `PATH` is the ONLY way to invoke the tools listed in `<safe-output-tools>`",
		"compiler-registered engines without MCP must be told the safeoutputs CLI is the only transport")
}

// TestGitHubMCPToolsPromptIncludedForCodeSecurityToolset verifies that when a
// workflow uses the code_security GitHub toolset the generated lock file references
// one of the github_mcp_tools prompt files (which carry the list_code_scanning_alerts
// guardrail). This is a regression test for unbounded alert queries.
func TestGitHubMCPToolsPromptIncludedForCodeSecurityToolset(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "gh-aw-code-scanning-prompt-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	testFile := filepath.Join(tmpDir, "test-workflow.md")
	testContent := `---
on: push
engine: claude
permissions:
  security-events: read
tools:
  github:
    toolsets: [code_security]
---

# Test Workflow with Code Security Toolset

List open critical code scanning alerts.
`
	require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0644))

	compiler := NewCompiler()
	require.NoError(t, compiler.CompileWorkflow(testFile))

	lockFile := strings.TrimSuffix(testFile, ".md") + ".lock.yml"
	lockContent, err := os.ReadFile(lockFile)
	require.NoError(t, err)

	lockStr := string(lockContent)

	// The generated workflow must reference one of the github_mcp_tools prompt files
	// (plain or with_safeoutputs variant) so the list_code_scanning_alerts guardrail
	// is injected at runtime. Both variants carry the same guardrail.
	hasGitHubMCPPrompt := strings.Contains(lockStr, githubMCPToolsPromptFile) ||
		strings.Contains(lockStr, githubMCPToolsWithSafeOutputsPromptFile)
	assert.True(t, hasGitHubMCPPrompt,
		"lock file should reference a github_mcp_tools prompt file when code_security toolset is active")
}

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
