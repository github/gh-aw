//go:build integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSafeInputsStepCodeGenerationStability verifies that the MCP setup step code generation
// for safe-inputs produces stable, deterministic output when called multiple times.
// This test ensures that tools are sorted before generating cat commands.
func TestSafeInputsStepCodeGenerationStability(t *testing.T) {
	// Create a config with multiple tools to ensure sorting is tested
	safeInputsConfig := &SafeInputsConfig{
		Tools: map[string]*SafeInputToolConfig{
			"zebra-shell": {
				Name:        "zebra-shell",
				Description: "A shell tool that starts with Z",
				Run:         "echo zebra",
			},
			"alpha-js": {
				Name:        "alpha-js",
				Description: "A JS tool that starts with A",
				Script:      "return 'alpha';",
			},
			"middle-shell": {
				Name:        "middle-shell",
				Description: "A shell tool in the middle",
				Run:         "echo middle",
			},
			"beta-js": {
				Name:        "beta-js",
				Description: "A JS tool that starts with B",
				Script:      "return 'beta';",
			},
		},
	}

	workflowData := &WorkflowData{
		SafeInputs: safeInputsConfig,
		Tools:      make(map[string]any),
		Features: map[string]any{
			"safe-inputs": true, // Feature flag is optional now
		},
	}

	// Generate MCP setup code multiple times using the actual compiler method
	iterations := 10
	outputs := make([]string, iterations)
	compiler := &Compiler{}

	// Create a mock engine that does nothing for MCP config
	mockEngine := &CustomEngine{}

	for i := 0; i < iterations; i++ {
		var yaml strings.Builder
		compiler.generateMCPSetup(&yaml, workflowData.Tools, mockEngine, workflowData)
		outputs[i] = yaml.String()
	}

	// All iterations should produce identical output
	for i := 1; i < iterations; i++ {
		if outputs[i] != outputs[0] {
			t.Errorf("generateMCPSetup produced different output on iteration %d", i+1)
			// Find first difference for debugging
			for j := 0; j < len(outputs[0]) && j < len(outputs[i]); j++ {
				if outputs[0][j] != outputs[i][j] {
					start := j - 100
					if start < 0 {
						start = 0
					}
					end := j + 100
					if end > len(outputs[0]) {
						end = len(outputs[0])
					}
					if end > len(outputs[i]) {
						end = len(outputs[i])
					}
					t.Errorf("First difference at position %d:\n  Expected: %q\n  Got: %q", j, outputs[0][start:end], outputs[i][start:end])
					break
				}
			}
		}
	}

	// Verify tools appear in sorted order in the output
	// All tools are sorted alphabetically regardless of type (JavaScript or shell):
	// alpha-js, beta-js, middle-shell, zebra-shell
	alphaPos := strings.Index(outputs[0], "alpha-js")
	betaPos := strings.Index(outputs[0], "beta-js")
	middlePos := strings.Index(outputs[0], "middle-shell")
	zebraPos := strings.Index(outputs[0], "zebra-shell")

	if alphaPos == -1 || betaPos == -1 || middlePos == -1 || zebraPos == -1 {
		t.Error("Output should contain all tool names")
	}

	// Verify alphabetical sorting: alpha < beta < middle < zebra
	if alphaPos >= betaPos || betaPos >= middlePos || middlePos >= zebraPos {
		t.Errorf("Tools should be sorted alphabetically in step code: alpha(%d) < beta(%d) < middle(%d) < zebra(%d)",
			alphaPos, betaPos, middlePos, zebraPos)
	}
}

// TestMCPGatewayVersionFromFrontmatter tests that sandbox.mcp.version specified in frontmatter
// is correctly used in both the docker predownload step and the MCP gateway setup command
func TestMCPGatewayVersionFromFrontmatter(t *testing.T) {
	tests := []struct {
		name            string
		sandboxConfig   *SandboxConfig
		expectedVersion string
		description     string
	}{
		{
			name: "custom version specified in frontmatter",
			sandboxConfig: &SandboxConfig{
				MCP: &MCPGatewayRuntimeConfig{
					Container: constants.DefaultMCPGatewayContainer,
					Version:   "v0.0.5",
					Port:      8080,
				},
			},
			expectedVersion: "v0.0.5",
			description:     "should use custom version v0.0.5",
		},
		{
			name: "no version specified - should use default",
			sandboxConfig: &SandboxConfig{
				MCP: &MCPGatewayRuntimeConfig{
					Container: constants.DefaultMCPGatewayContainer,
					Port:      8080,
				},
			},
			expectedVersion: string(constants.DefaultMCPGatewayVersion),
			description:     "should use default version when not specified",
		},
		{
			name: "empty version string - should use default",
			sandboxConfig: &SandboxConfig{
				MCP: &MCPGatewayRuntimeConfig{
					Container: constants.DefaultMCPGatewayContainer,
					Version:   "",
					Port:      8080,
				},
			},
			expectedVersion: string(constants.DefaultMCPGatewayVersion),
			description:     "should use default version when version is empty string",
		},
		{
			name: "version 'latest' preserved",
			sandboxConfig: &SandboxConfig{
				MCP: &MCPGatewayRuntimeConfig{
					Container: constants.DefaultMCPGatewayContainer,
					Version:   "latest",
					Port:      8080,
				},
			},
			expectedVersion: "latest",
			description:     "should preserve 'latest' version as specified by user",
		},
		{
			name: "custom version with different format",
			sandboxConfig: &SandboxConfig{
				MCP: &MCPGatewayRuntimeConfig{
					Container: constants.DefaultMCPGatewayContainer,
					Version:   "1.2.3",
					Port:      8080,
				},
			},
			expectedVersion: "1.2.3",
			description:     "should use custom version 1.2.3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workflowData := &WorkflowData{
				SandboxConfig: tt.sandboxConfig,
				Tools:         map[string]any{"github": map[string]any{}},
			}

			// Ensure MCP gateway config is applied (includes normalization of "latest")
			ensureDefaultMCPGatewayConfig(workflowData)

			// After normalization, verify the version matches expected
			require.NotNil(t, workflowData.SandboxConfig, "SandboxConfig should not be nil")
			require.NotNil(t, workflowData.SandboxConfig.MCP, "MCP gateway config should not be nil")

			actualVersion := workflowData.SandboxConfig.MCP.Version
			assert.Equal(t, tt.expectedVersion, actualVersion,
				"Version after normalization should be %s (%s)", tt.expectedVersion, tt.description)

			// Test 1: Verify docker image collection uses the correct version
			dockerImages := collectDockerImages(workflowData.Tools, workflowData, ActionModeRelease)
			expectedImage := constants.DefaultMCPGatewayContainer + ":" + tt.expectedVersion

			found := false
			for _, img := range dockerImages {
				if strings.Contains(img, constants.DefaultMCPGatewayContainer) {
					assert.Equal(t, expectedImage, img,
						"Docker image should include correct version (%s)", tt.description)
					found = true
					break
				}
			}
			assert.True(t, found, "MCP gateway container should be in docker images list")

			// Test 2: Verify MCP gateway setup command uses the correct version
			compiler := &Compiler{}
			var yaml strings.Builder
			mockEngine := &CustomEngine{}

			compiler.generateMCPSetup(&yaml, workflowData.Tools, mockEngine, workflowData)
			setupOutput := yaml.String()

			// The setup output should contain the container image with the correct version
			assert.Contains(t, setupOutput, expectedImage,
				"MCP gateway setup should use correct container version (%s)", tt.description)
		})
	}
}

// TestMCPGatewayVersionParsedFromSource tests that sandbox.mcp.version is correctly parsed
// from markdown frontmatter and used in the compiled workflow output
func TestMCPGatewayVersionParsedFromSource(t *testing.T) {
	tests := []struct {
		name                  string
		frontmatter           string
		expectedVersion       string
		shouldHaveGateway     bool
		shouldContainInDocker bool
		shouldContainInSetup  bool
	}{
		{
			name: "custom version v0.0.5 specified in frontmatter",
			frontmatter: `---
on: issues
engine: claude
sandbox:
  mcp:
    container: ghcr.io/github/gh-aw-mcpg
    version: v0.0.5
tools:
  github:
---

# Test Workflow
Test workflow with custom sandbox.mcp.version.`,
			expectedVersion:       "v0.0.5",
			shouldHaveGateway:     true,
			shouldContainInDocker: true,
			shouldContainInSetup:  true,
		},
		{
			name: "no version specified - should use default v0.0.12",
			frontmatter: `---
on: issues
engine: claude
tools:
  github:
---

# Test Workflow
Test workflow without sandbox.mcp.version specified.`,
			expectedVersion:       string(constants.DefaultMCPGatewayVersion),
			shouldHaveGateway:     true,
			shouldContainInDocker: true,
			shouldContainInSetup:  true,
		},
		{
			name: "version latest should be preserved",
			frontmatter: `---
on: issues
engine: claude
sandbox:
  mcp:
    container: ghcr.io/github/gh-aw-mcpg
    version: latest
tools:
  github:
---

# Test Workflow
Test workflow with version: latest.`,
			expectedVersion:       "latest",
			shouldHaveGateway:     true,
			shouldContainInDocker: true,
			shouldContainInSetup:  true,
		},
		{
			name: "custom version 1.2.3 specified in frontmatter",
			frontmatter: `---
on: issues
engine: claude
sandbox:
  mcp:
    container: ghcr.io/github/gh-aw-mcpg
    version: "1.2.3"
tools:
  github:
---

# Test Workflow
Test workflow with version 1.2.3.`,
			expectedVersion:       "1.2.3",
			shouldHaveGateway:     true,
			shouldContainInDocker: true,
			shouldContainInSetup:  true,
		},
		{
			name: "custom container and version specified",
			frontmatter: `---
on: issues
engine: claude
sandbox:
  mcp:
    container: ghcr.io/custom/gateway
    version: v2.0.0
tools:
  github:
---

# Test Workflow
Test workflow with custom container and version.`,
			expectedVersion:       "v2.0.0",
			shouldHaveGateway:     true,
			shouldContainInDocker: true,
			shouldContainInSetup:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary directory for test files
			tmpDir := testutil.TempDir(t, "mcp-version-test")

			// Write test workflow file
			testFile := filepath.Join(tmpDir, "test-workflow.md")
			err := os.WriteFile(testFile, []byte(tt.frontmatter), 0644)
			require.NoError(t, err, "Failed to write test workflow file")

			// Compile the workflow
			compiler := NewCompiler()
			err = compiler.CompileWorkflow(testFile)
			require.NoError(t, err, "Failed to compile workflow")

			// Read generated lock file
			lockFile := stringutil.MarkdownToLockFile(testFile)
			yamlContent, err := os.ReadFile(lockFile)
			require.NoError(t, err, "Failed to read lock file")

			yamlStr := string(yamlContent)

			// Test 1: Check if docker predownload step contains the correct version
			if tt.shouldContainInDocker {
				dockerStep := strings.Contains(yamlStr, "Download container images")
				assert.True(t, dockerStep, "Should have docker predownload step")

				// Extract container name (handle both default and custom)
				var expectedContainer string
				if strings.Contains(tt.frontmatter, "container: ghcr.io/custom/gateway") {
					expectedContainer = "ghcr.io/custom/gateway"
				} else {
					expectedContainer = constants.DefaultMCPGatewayContainer
				}

				expectedImage := expectedContainer + ":" + tt.expectedVersion
				assert.Contains(t, yamlStr, expectedImage,
					"Docker predownload step should contain image with version %s", tt.expectedVersion)
			}

			// Test 2: Check if MCP gateway setup contains the correct version
			if tt.shouldContainInSetup {
				setupStep := strings.Contains(yamlStr, "Start MCP gateway")
				assert.True(t, setupStep, "Should have MCP setup step")

				// The setup step should use the docker run command with the correct version
				// Extract container name (handle both default and custom)
				var expectedContainer string
				if strings.Contains(tt.frontmatter, "container: ghcr.io/custom/gateway") {
					expectedContainer = "ghcr.io/custom/gateway"
				} else {
					expectedContainer = constants.DefaultMCPGatewayContainer
				}

				expectedImage := expectedContainer + ":" + tt.expectedVersion
				assert.Contains(t, yamlStr, expectedImage,
					"MCP setup should use container image with version %s", tt.expectedVersion)
			}

			// Test 3: Verify version is NOT missing or using wrong default
			if tt.shouldHaveGateway {
				// Should not have untagged container references
				var containerName string
				if strings.Contains(tt.frontmatter, "container: ghcr.io/custom/gateway") {
					containerName = "ghcr.io/custom/gateway"
				} else {
					containerName = constants.DefaultMCPGatewayContainer
				}

				// Check that we don't have the container without any tag
				// (This would be a bug - every container reference should have a version)
				untaggerdPattern := "docker run.*" + strings.ReplaceAll(containerName, "/", "\\/") + "\\s"
				assert.NotRegexp(t, untaggerdPattern, yamlStr,
					"Container should always have a version tag, never be used untagged")
			}
		})
	}
}

// TestHTTPMCPSecretsPassedToGatewayContainer verifies that secrets from HTTP MCP servers
// (like TAVILY_API_KEY) are correctly passed to the gateway container via -e flags
func TestHTTPMCPSecretsPassedToGatewayContainer(t *testing.T) {
	frontmatter := `---
on: workflow_dispatch
engine: copilot
tools:
  github:
    mode: remote
    toolsets: [repos, issues]
  tavily:
    type: http
    url: "https://mcp.tavily.com/mcp/"
    headers:
      Authorization: "Bearer ${{ secrets.TAVILY_API_KEY }}"
    allowed: ["*"]
---

# Test HTTP MCP Secrets

Test that TAVILY_API_KEY is passed to gateway container.
`

	compiler := NewCompiler()

	tmpDir := t.TempDir()
	inputFile := filepath.Join(tmpDir, "test.md")

	err := os.WriteFile(inputFile, []byte(frontmatter), 0644)
	require.NoError(t, err, "Failed to write test input file")

	err = compiler.CompileWorkflow(inputFile)
	require.NoError(t, err, "Compilation should succeed")

	outputFile := stringutil.MarkdownToLockFile(inputFile)
	content, err := os.ReadFile(outputFile)
	require.NoError(t, err, "Failed to read output file")
	yamlStr := string(content)

	// Verify TAVILY_API_KEY is in the step's env block
	assert.Contains(t, yamlStr, "TAVILY_API_KEY: ${{ secrets.TAVILY_API_KEY }}",
		"TAVILY_API_KEY should be in the Start MCP gateway step's env block")

	// Verify TAVILY_API_KEY is passed to the docker container via -e flag
	assert.Contains(t, yamlStr, "-e TAVILY_API_KEY",
		"TAVILY_API_KEY should be passed to gateway container via -e flag")

	// Verify the docker command includes the -e flag before the container image
	// This ensures proper docker run command structure
	dockerCmdPattern := `docker run.*-e TAVILY_API_KEY.*ghcr\.io/github/gh-aw-mcpg`
	assert.Regexp(t, dockerCmdPattern, yamlStr,
		"Docker command should include -e TAVILY_API_KEY before the container image")
}

// TestMultipleHTTPMCPSecretsPassedToGatewayContainer verifies that multiple HTTP MCP servers
// with different secrets all get their environment variables passed to the gateway container
func TestMultipleHTTPMCPSecretsPassedToGatewayContainer(t *testing.T) {
	frontmatter := `---
on: workflow_dispatch
engine: copilot
tools:
  github:
    mode: remote
    toolsets: [repos]
  tavily:
    type: http
    url: "https://mcp.tavily.com/mcp/"
    headers:
      Authorization: "Bearer ${{ secrets.TAVILY_API_KEY }}"
  datadog:
    type: http
    url: "https://api.datadoghq.com/mcp"
    headers:
      DD-API-KEY: "${{ secrets.DD_API_KEY }}"
      DD-APPLICATION-KEY: "${{ secrets.DD_APP_KEY }}"
---

# Test Multiple HTTP MCP Secrets

Test that multiple secrets are passed to gateway container.
`

	compiler := NewCompiler()

	tmpDir := t.TempDir()
	inputFile := filepath.Join(tmpDir, "test.md")

	err := os.WriteFile(inputFile, []byte(frontmatter), 0644)
	require.NoError(t, err, "Failed to write test input file")

	err = compiler.CompileWorkflow(inputFile)
	require.NoError(t, err, "Compilation should succeed")

	outputFile := stringutil.MarkdownToLockFile(inputFile)
	content, err := os.ReadFile(outputFile)
	require.NoError(t, err, "Failed to read output file")
	yamlStr := string(content)

	// Verify all secrets are in the step's env block
	assert.Contains(t, yamlStr, "TAVILY_API_KEY: ${{ secrets.TAVILY_API_KEY }}",
		"TAVILY_API_KEY should be in env block")
	assert.Contains(t, yamlStr, "DD_API_KEY: ${{ secrets.DD_API_KEY }}",
		"DD_API_KEY should be in env block")
	assert.Contains(t, yamlStr, "DD_APP_KEY: ${{ secrets.DD_APP_KEY }}",
		"DD_APP_KEY should be in env block")

	// Verify all secrets are passed to docker container
	assert.Contains(t, yamlStr, "-e TAVILY_API_KEY",
		"TAVILY_API_KEY should be passed to container")
	assert.Contains(t, yamlStr, "-e DD_API_KEY",
		"DD_API_KEY should be passed to container")
	assert.Contains(t, yamlStr, "-e DD_APP_KEY",
		"DD_APP_KEY should be passed to container")
}
