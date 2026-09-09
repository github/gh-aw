//go:build integration

package workflow

import (
	"fmt"
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

// TestMCPScriptsStepCodeGenerationStability verifies that the MCP setup step code generation
// for mcp-scripts produces stable, deterministic output when called multiple times.
// This test ensures that tools are sorted before generating cat commands.
func TestMCPScriptsStepCodeGenerationStability(t *testing.T) {
	// Create a config with multiple tools to ensure sorting is tested
	mcpScriptsConfig := &MCPScriptsConfig{
		Tools: map[string]*MCPScriptToolConfig{
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
		MCPScripts:      mcpScriptsConfig,
		Tools:           make(map[string]any),
		FrontmatterHash: "stabletesthash1234567890abcdef",
		Features: map[string]any{
			"mcp-scripts": true, // Feature flag is optional now
		},
	}

	// Generate MCP setup code multiple times using the actual compiler method
	iterations := 10
	outputs := make([]string, iterations)
	compiler := &Compiler{}

	// Create a mock engine that does nothing for MCP config
	mockEngine := NewClaudeEngine()

	for i := 0; i < iterations; i++ {
		var yaml strings.Builder
		require.NoError(t, compiler.generateMCPSetup(&yaml, workflowData.Tools, mockEngine, workflowData))
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
			name: "dynamic enclave without explicit version uses default when it satisfies delegation minimum",
			sandboxConfig: &SandboxConfig{
				MCP: &MCPGatewayRuntimeConfig{
					Container: constants.DefaultMCPGatewayContainer,
					Version:   "",
					Port:      8080,
				},
			},
			expectedVersion: string(constants.DefaultMCPGatewayVersion),
			description:     "should use the default version when it satisfies the dynamic delegation minimum",
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
			if tt.name == "dynamic enclave without explicit version uses default when it satisfies delegation minimum" {
				workflowData = dynamicEnclaveWorkflowData()
				workflowData.SandboxConfig.MCP.Version = ""
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
			// collectDockerImages applies embedded container pins when available, so resolve
			// the pinned reference for the expected image if one exists.
			if pin, ok := lookupContainerPin(expectedImage, nil); ok && pin.PinnedImage != "" {
				expectedImage = pin.PinnedImage
			}

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
			mockEngine := NewClaudeEngine()

			require.NoError(t, compiler.generateMCPSetup(&yaml, workflowData.Tools, mockEngine, workflowData))
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
strict: false
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
strict: false
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
strict: false
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
strict: false
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
				setupStep := strings.Contains(yamlStr, "Start MCP Gateway")
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
mcp-servers:
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
		"TAVILY_API_KEY should be in the Start MCP Gateway step's env block")

	// Verify TAVILY_API_KEY is passed to the docker container via -e flag
	assert.Contains(t, yamlStr, "-e TAVILY_API_KEY",
		"TAVILY_API_KEY should be passed to gateway container via -e flag")

	// Verify the docker command includes the -e flag before the container image
	// This ensures proper docker run command structure
	dockerCmdPattern := `docker run.*-e TAVILY_API_KEY.*ghcr\.io/github/gh-aw-mcpg`
	assert.Regexp(t, dockerCmdPattern, yamlStr,
		"Docker command should include -e TAVILY_API_KEY before the container image")
}

// TestMCPGatewayDockerCommandUsesRunnerIdentityAndSocketGroup verifies the gateway docker command
// computes and uses runner UID/GID and docker socket group values in the generated command.
func TestMCPGatewayDockerCommandUsesRunnerIdentityAndSocketGroup(t *testing.T) {
	frontmatter := `---
on: workflow_dispatch
engine: copilot
tools:
  github:
    mode: remote
    toolsets: [repos]
---

# Test Docker Socket Group
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

	userSnippet := `--user '"${MCP_GATEWAY_UID}"':'"${MCP_GATEWAY_GID}"'`
	groupAddSnippet := `--group-add '"${DOCKER_SOCK_GID}"'`
	mountSnippet := `-v '"${DOCKER_SOCK_PATH}"':/var/run/docker.sock`
	defaultGatewayPortSnippet := `export MCP_GATEWAY_PORT="8080"`
	uidComputeSnippet := `MCP_GATEWAY_UID=$(id -u 2>/dev/null || echo '0')`
	runnerGIDComputeSnippet := `MCP_GATEWAY_GID=$(id -g 2>/dev/null || echo '0')`
	// Docker socket path and GID resolution has been refactored to a dedicated shell script.
	// The script handles override variables (GH_AW_DOCKER_SOCK_PATH, GH_AW_DOCKER_SOCK_GID),
	// DOCKER_HOST parsing, stat -Lc symlink following, and numeric validation.
	// See actions/setup/sh/resolve_docker_socket_gid.sh for implementation and comprehensive tests.
	resolveDockerSocketSnippet := `source "${RUNNER_TEMP}/gh-aw/actions/resolve_docker_socket_gid.sh"`
	dockerHostEnvSnippet := `-e DOCKER_HOST=unix:///var/run/docker.sock`
	containerNameSnippet := `--name awmg-mcpg`
	require.Contains(t, yamlStr, defaultGatewayPortSnippet,
		"Default MCP gateway port should be exported as 8080")
	require.Contains(t, yamlStr, uidComputeSnippet,
		"Shell should compute MCP_GATEWAY_UID before docker command")
	require.Contains(t, yamlStr, runnerGIDComputeSnippet,
		"Shell should compute MCP_GATEWAY_GID before docker command")
	// In bridge/network-isolation mode, --add-host host.docker.internal:127.0.0.1 is never
	// added (the container loopback differs from the host loopback). --add-host with
	// host-gateway is injected whenever the sandbox is active (shouldRewriteLocalhostToDocker)
	// so the gateway container can reach any host-side server (mcp-scripts, custom HTTP MCP
	// tools with localhost URLs) running on the runner host.
	require.NotContains(t, yamlStr, `--add-host host.docker.internal:127.0.0.1`,
		"Docker command should not add host.docker.internal:127.0.0.1 mapping in bridge mode")
	require.Contains(t, yamlStr, `--add-host host.docker.internal:host-gateway`,
		"Docker command should add host-gateway mapping in bridge mode when sandbox is active")
	require.Contains(t, yamlStr, `docker run -i --rm --network bridge`,
		"Docker command should use bridge networking in default network isolation mode")
	require.Contains(t, yamlStr, userSnippet,
		"Docker command should include runner UID/GID user mapping")
	require.Contains(t, yamlStr, resolveDockerSocketSnippet,
		"Shell should source the resolve_docker_socket_gid.sh script to set DOCKER_SOCK_PATH and DOCKER_SOCK_GID")
	require.Contains(t, yamlStr, groupAddSnippet,
		"Docker command should include docker socket supplementary group mapping")
	require.Contains(t, yamlStr, mountSnippet,
		"Docker command should mount the resolved Docker socket path")
	require.Contains(t, yamlStr, dockerHostEnvSnippet,
		"Docker command should set DOCKER_HOST to the fixed mount destination inside the gateway")
	require.Contains(t, yamlStr, containerNameSnippet,
		"Docker command should assign a well-known name to the gateway container for cleanup")
	require.Less(t, strings.Index(yamlStr, uidComputeSnippet), strings.Index(yamlStr, userSnippet),
		"MCP_GATEWAY_UID should be computed before it is used in the docker command")
	require.Less(t, strings.Index(yamlStr, runnerGIDComputeSnippet), strings.Index(yamlStr, userSnippet),
		"MCP_GATEWAY_GID should be computed before it is used in the docker command")
	require.Less(t, strings.Index(yamlStr, userSnippet), strings.Index(yamlStr, groupAddSnippet),
		"Docker command should include user mapping before supplementary group mapping")
	require.Less(t, strings.Index(yamlStr, resolveDockerSocketSnippet), strings.Index(yamlStr, groupAddSnippet),
		"Docker socket resolution script should be sourced before DOCKER_SOCK_GID is used in the docker command")
	require.Less(t, strings.Index(yamlStr, resolveDockerSocketSnippet), strings.Index(yamlStr, mountSnippet),
		"Docker socket resolution script should be sourced before DOCKER_SOCK_PATH is used in the docker command")
	require.Less(t, strings.Index(yamlStr, groupAddSnippet), strings.Index(yamlStr, mountSnippet),
		"Docker command should add supplementary group before mounting the Docker socket")
}

func TestMCPGatewaySetupCopiesGitHubEventPayloadForSafeOutputs(t *testing.T) {
	frontmatter := `---
on: workflow_dispatch
engine: copilot
tools:
  github:
    mode: remote
    toolsets: [repos]
---

# Test event payload forwarding
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

	require.Contains(t, yamlStr, `if [ -n "${GITHUB_EVENT_PATH:-}" ] && [ -r "${GITHUB_EVENT_PATH}" ]; then`,
		"Start MCP Gateway should conditionally copy the GitHub event payload when available")
	require.Contains(t, yamlStr, `GH_AW_SAFEOUTPUTS_EVENT_PATH="${RUNNER_TEMP}/gh-aw/safeoutputs/github_event.json"`,
		"Start MCP Gateway should write the copied payload under the safeoutputs mount")
	require.Contains(t, yamlStr, `cp "${GITHUB_EVENT_PATH}" "${GH_AW_SAFEOUTPUTS_EVENT_PATH}"`,
		"Start MCP Gateway should copy the GitHub event payload into the safeoutputs directory")
	require.Contains(t, yamlStr, `export GITHUB_EVENT_PATH="${GH_AW_SAFEOUTPUTS_EVENT_PATH}"`,
		"Start MCP Gateway should repoint GITHUB_EVENT_PATH to the mounted payload copy")
}

func TestMCPGatewayDockerCommandUsesBridgeInNetworkIsolationMode(t *testing.T) {
	frontmatter := `---
on: workflow_dispatch
engine: copilot
sandbox:
  agent:
tools:
  github:
    mode: remote
    toolsets: [repos]
---

# Test Docker Socket Group
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

	require.Contains(t, yamlStr, `docker run -i --rm --network bridge`,
		"Docker command should use bridge networking in network isolation mode")
	require.Contains(t, yamlStr, `-p 127.0.0.1:`,
		"Docker command should publish gateway port to host in network isolation mode")
	require.NotContains(t, yamlStr, `--network host`,
		"Docker command should not use host networking in network isolation mode")
	require.NotContains(t, yamlStr, `--add-host host.docker.internal:127.0.0.1`,
		"Docker command should not inject host.docker.internal:127.0.0.1 mapping in network isolation mode")
	require.Contains(t, yamlStr, `--add-host host.docker.internal:host-gateway`,
		"Docker command should inject host-gateway mapping in bridge mode when sandbox is active")
	require.Contains(t, yamlStr, `export MCP_GATEWAY_DOMAIN="awmg-mcpg"`,
		"MCP gateway domain should use the internal container name in network isolation mode")
	require.Contains(t, yamlStr, `export MCP_GATEWAY_HOST_DOMAIN="localhost"`,
		"MCP gateway host domain should be localhost in network isolation mode so host-side clients can connect")
}

func TestMCPGatewayDockerCommandUsesHostNetworkInHostAccessMode(t *testing.T) {
	frontmatter := `---
on: workflow_dispatch
engine: copilot
sandbox:
  agent:
    runtime: docker-sudo-iptables
tools:
  github:
    mode: remote
    toolsets: [repos]
---

# Test Host Access MCP Gateway
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

	require.Contains(t, yamlStr, `docker run -i --rm --network host`,
		"Docker command should use host networking in host-access mode")
	require.NotContains(t, yamlStr, `-p 127.0.0.1:`,
		"Docker command should not publish the gateway port in host-access mode")
	require.Contains(t, yamlStr, `export MCP_GATEWAY_DOMAIN="host.docker.internal"`,
		"MCP gateway domain should use host.docker.internal in host-access mode")
	require.Contains(t, yamlStr, `--enable-host-access`,
		"Agent sandbox should enable host access")
}

// TestMCPGatewayDockerCommandGeminiNetworkIsolationUsesTopologyHostname verifies that
// the Gemini engine uses the topology hostname (awmg-mcpg) as MCP_GATEWAY_HOST_DOMAIN
// under network isolation, instead of localhost. The Gemini CLI honors HTTP_PROXY but
// ignores NO_PROXY, so routing localhost:8080 through the squid proxy would be denied.
func TestMCPGatewayDockerCommandGeminiNetworkIsolationUsesTopologyHostname(t *testing.T) {
	frontmatter := `---
on: workflow_dispatch
engine: gemini
sandbox:
  agent:
tools:
  github:
    mode: remote
    toolsets: [repos]
---

# Test Gemini MCP Gateway Topology Hostname
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

	require.Contains(t, yamlStr, `export MCP_GATEWAY_DOMAIN="awmg-mcpg"`,
		"MCP gateway domain should use the topology container name in network isolation mode")
	require.Contains(t, yamlStr, `export MCP_GATEWAY_HOST_DOMAIN="awmg-mcpg"`,
		"Gemini MCP_GATEWAY_HOST_DOMAIN must use awmg-mcpg under network isolation so the Gemini CLI does not tunnel localhost through the squid egress proxy")
	require.NotContains(t, yamlStr, `export MCP_GATEWAY_HOST_DOMAIN="localhost"`,
		"Gemini MCP_GATEWAY_HOST_DOMAIN must not be localhost under network isolation")
}

// TestMCPGatewayDockerCommandUsesDockerSbxGatewayRouting verifies that docker-sbx workflows
// publish the gateway on 0.0.0.0 and export host.docker.internal for both container-side and
// microVM-side clients.
func TestMCPGatewayDockerCommandUsesDockerSbxGatewayRouting(t *testing.T) {
	frontmatter := `---
on: workflow_dispatch
engine: copilot
sandbox:
  agent:
    runtime: docker-sbx
    version: v0.28.0
tools:
  github:
    mode: remote
    toolsets: [repos]
---

# Test Docker SBX MCP Routing
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

	require.Contains(t, yamlStr, `docker run -i --rm --network bridge`,
		"Docker command should use bridge networking in docker-sbx mode")
	require.Contains(t, yamlStr, `-p 0.0.0.0:`,
		"Docker command should publish the gateway to 0.0.0.0 for docker-sbx")
	require.Contains(t, yamlStr, `export MCP_GATEWAY_DOMAIN="host.docker.internal"`,
		"MCP gateway domain should use host.docker.internal in docker-sbx mode")
	require.Contains(t, yamlStr, `export MCP_GATEWAY_HOST_DOMAIN="host.docker.internal"`,
		"MCP gateway host domain should use host.docker.internal in docker-sbx mode")
}

// TestMCPGatewayDockerCommandAddsHostGatewayForMCPScriptsInBridgeMode verifies that when
// mcp-scripts are configured in network-isolation (bridge) mode, the gateway container command
// includes --add-host host.docker.internal:host-gateway so the gateway can reach the
// mcp-scripts HTTP server running on the runner host.
func TestMCPGatewayDockerCommandAddsHostGatewayForMCPScriptsInBridgeMode(t *testing.T) {
	frontmatter := `---
on: workflow_dispatch
engine: copilot
sandbox:
  agent:
tools:
  github:
    mode: remote
    toolsets: [repos]
mcp-scripts:
  my-tool:
    description: "A test tool"
    inputs:
      text:
        type: string
        description: "Input text"
    run: echo "$INPUT_TEXT"
---

# Test MCP Scripts Host Gateway
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

	require.Contains(t, yamlStr, `docker run -i --rm --network bridge`,
		"Docker command should use bridge networking in network isolation mode")
	require.Contains(t, yamlStr, `--add-host host.docker.internal:host-gateway`,
		"Docker command must add host-gateway mapping in bridge mode when mcp-scripts are present so the gateway can reach the mcp-scripts server on the host")
	require.NotContains(t, yamlStr, `--add-host host.docker.internal:127.0.0.1`,
		"Docker command should not inject the host loopback mapping in bridge mode")
}

// TestMCPGatewayDockerCommandAddsHostGatewayForLocalhostHTTPMCPInBridgeMode verifies that when
// a custom HTTP MCP server with a localhost URL is configured in network-isolation (bridge) mode,
// the gateway container command includes --add-host host.docker.internal:host-gateway so the
// gateway can reach the rewritten host.docker.internal URL on the runner host. Without this
// mapping the gateway container silently fails to resolve host.docker.internal after
// rewriteLocalhostToDockerHost rewrites the URL.
func TestMCPGatewayDockerCommandAddsHostGatewayForLocalhostHTTPMCPInBridgeMode(t *testing.T) {
	frontmatter := `---
on: workflow_dispatch
engine: copilot
sandbox:
  agent:
tools:
  github:
    mode: remote
    toolsets: [repos]
mcp-servers:
  my-local-server:
    type: http
    url: "http://localhost:9000/mcp"
    allowed: ["*"]
---

# Test localhost HTTP MCP Host Gateway
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

	require.Contains(t, yamlStr, `docker run -i --rm --network bridge`,
		"Docker command should use bridge networking in network isolation mode")
	require.Contains(t, yamlStr, `--add-host host.docker.internal:host-gateway`,
		"Docker command must add host-gateway mapping in bridge mode with a localhost HTTP MCP server so the gateway can reach the rewritten host.docker.internal URL")
	require.NotContains(t, yamlStr, `--add-host host.docker.internal:127.0.0.1`,
		"Docker command should not inject the host loopback mapping in bridge mode")
}

// TestMultipleHTTPMCPSecretsPassedToGatewayContainer verifies that multiple HTTP MCP servers with
// different secrets all get their environment variables passed to the gateway container
func TestMultipleHTTPMCPSecretsPassedToGatewayContainer(t *testing.T) {
	frontmatter := `---
on: workflow_dispatch
engine: copilot
tools:
  github:
    mode: remote
    toolsets: [repos]
mcp-servers:
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

// TestSafeOutputsMCPContainerUsesRuntimePaths verifies that safe-outputs is configured as a
// containerized MCP server and still receives the same runtime paths used by downstream
// collection steps.
func TestSafeOutputsMCPContainerUsesRuntimePaths(t *testing.T) {
	frontmatter := `---
on: issues
engine: claude
safe-outputs:
  create-discussion: {}
  create-issue: {}
---

# Test Safe Outputs Output Path

Test that GH_AW_SAFE_OUTPUTS is passed to the HTTP server startup step.
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
	pinnedGhAwNodeImage := resolveMCPGatewayContainerImage(constants.DefaultGhAwNodeImage, nil)

	assert.Contains(t, yamlStr, `"safeoutputs": {`,
		"Should configure safeoutputs as an MCP server")
	assert.Contains(t, yamlStr, `"container": "`+pinnedGhAwNodeImage+`"`,
		"Safe outputs should run in the gh-aw node container")
	assert.Contains(t, yamlStr, `"GH_AW_SAFE_OUTPUTS": "\${GH_AW_SAFE_OUTPUTS}"`,
		"Safe outputs MCP server should receive the runtime output path")
	assert.Contains(t, yamlStr, `"GH_AW_SAFE_OUTPUTS_CONFIG_PATH": "\${GH_AW_SAFE_OUTPUTS_CONFIG_PATH}"`,
		"Safe outputs MCP server should receive the runtime config path")
	assert.Contains(t, yamlStr, `"GH_AW_SAFE_OUTPUTS_TOOLS_PATH": "\${GH_AW_SAFE_OUTPUTS_TOOLS_PATH}"`,
		"Safe outputs MCP server should receive the runtime tools path")
	assert.Contains(t, yamlStr, `"GH_AW_POLICY_ALLOW_CREATE_PULL_REQUEST": "\${GH_AW_POLICY_ALLOW_CREATE_PULL_REQUEST}"`,
		"Safe outputs MCP server should receive the runtime create-pull-request policy")
	for _, name := range []string{
		"GH_AW_PR_HEAD_BASE_BRANCH",
		"GH_AW_PR_HEAD_BASE_SHA",
		"GH_AW_PR_HEAD_BASE_REPO",
		"GH_AW_PR_HEAD_BASE_PR_NUMBER",
		"GH_AW_PR_HEAD_BASE_REF",
		"GH_AW_PR_HEAD_REPO",
	} {
		assert.Contains(t, yamlStr, " -e "+name,
			"MCP gateway container should receive PR head baseline metadata")
		assert.Contains(t, yamlStr, fmt.Sprintf(`"%s": "\${%s}"`, name, name),
			"Safe outputs MCP server should receive PR head baseline metadata")
	}
	assert.Contains(t, yamlStr, `"RUNNER_TEMP": "\${RUNNER_TEMP}"`,
		"Safe outputs MCP server should receive RUNNER_TEMP for staging helpers")
	assert.NotContains(t, yamlStr, "Start Safe Outputs MCP HTTP Server",
		"Should not launch safe outputs via a dedicated startup step")
	assert.NotContains(t, yamlStr, "safe-outputs-config.outputs.safe_outputs_port",
		"Should not depend on safe outputs port outputs")
	assert.NotContains(t, yamlStr, "safe-outputs-config.outputs.safe_outputs_api_key",
		"Should not depend on safe outputs API key outputs")
}

// TestOIDCEnvVarsPassedToGatewayContainer verifies that ACTIONS_ID_TOKEN_REQUEST_URL and
// ACTIONS_ID_TOKEN_REQUEST_TOKEN are passed to the MCP gateway container when an HTTP MCP server
// uses auth.type: "github-oidc". This is required for the gateway to mint OIDC tokens (spec §7.6.1).
func TestOIDCEnvVarsPassedToGatewayContainer(t *testing.T) {
	frontmatter := `---
on: workflow_dispatch
engine: copilot
permissions:
  id-token: write
tools:
  github:
    mode: remote
    toolsets: [repos]
mcp-servers:
  my-oidc-server:
    type: http
    url: "https://my-server.example.com/mcp"
    auth:
      type: github-oidc
      audience: "https://my-server.example.com"
    allowed: ["*"]
---

# Test OIDC Env Vars

Test that OIDC env vars are forwarded to the MCP gateway container.
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

	// Verify OIDC env vars are passed to the docker container via -e flags
	assert.Contains(t, yamlStr, "-e ACTIONS_ID_TOKEN_REQUEST_URL",
		"ACTIONS_ID_TOKEN_REQUEST_URL should be passed to gateway container via -e flag")
	assert.Contains(t, yamlStr, "-e ACTIONS_ID_TOKEN_REQUEST_TOKEN",
		"ACTIONS_ID_TOKEN_REQUEST_TOKEN should be passed to gateway container via -e flag")
	assert.NotContains(t, yamlStr, "-e ACTIONS_ID_TOKEN_REQUEST_URL=",
		"ACTIONS_ID_TOKEN_REQUEST_URL must be forwarded without embedding its value")
	assert.NotContains(t, yamlStr, "-e ACTIONS_ID_TOKEN_REQUEST_TOKEN=",
		"ACTIONS_ID_TOKEN_REQUEST_TOKEN must be forwarded without embedding its value")
	assert.Contains(t, yamlStr, "--exclude-env ACTIONS_ID_TOKEN_REQUEST_URL",
		"ACTIONS_ID_TOKEN_REQUEST_URL must be excluded from the AWF agent")
	assert.Contains(t, yamlStr, "--exclude-env ACTIONS_ID_TOKEN_REQUEST_TOKEN",
		"ACTIONS_ID_TOKEN_REQUEST_TOKEN must be excluded from the AWF agent")

	// Verify the docker command includes both -e flags before the container image
	dockerCmdPatternURL := `docker run.*-e ACTIONS_ID_TOKEN_REQUEST_URL.*ghcr\.io/github/gh-aw-mcpg`
	assert.Regexp(t, dockerCmdPatternURL, yamlStr,
		"Docker command should include -e ACTIONS_ID_TOKEN_REQUEST_URL before the container image")
	dockerCmdPatternToken := `docker run.*-e ACTIONS_ID_TOKEN_REQUEST_TOKEN.*ghcr\.io/github/gh-aw-mcpg`
	assert.Regexp(t, dockerCmdPatternToken, yamlStr,
		"Docker command should include -e ACTIONS_ID_TOKEN_REQUEST_TOKEN before the container image")
}

// TestOIDCEnvVarsNotPassedForEngineOIDCAuth verifies that engine WIF does not add OIDC env vars
// to the gateway command, while the AWF agent excludes them.
func TestOIDCEnvVarsNotPassedForEngineOIDCAuth(t *testing.T) {
	frontmatter := `---
on: workflow_dispatch
engine:
  id: claude
  auth:
    type: github-oidc
    provider: anthropic
    federation-rule-id: fr_01ABC
    organization-id: org_01XYZ
    service-account-id: sa_01DEF
    workspace-id: ws_01GHI
permissions:
  id-token: write
tools:
  github:
    mode: remote
    toolsets: [repos]
mcp-servers:
  tavily:
    type: http
    url: "https://mcp.tavily.com/mcp/"
    headers:
      Authorization: "Bearer ${{ secrets.TAVILY_API_KEY }}"
    allowed: ["*"]
---

# Test Engine OIDC

Test that only HTTP MCP OIDC adds OIDC env vars to the gateway.
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

	// Verify OIDC env vars are NOT in the docker command
	assert.NotContains(t, yamlStr, "-e ACTIONS_ID_TOKEN_REQUEST_URL",
		"ACTIONS_ID_TOKEN_REQUEST_URL should NOT be in docker command without HTTP MCP github-oidc auth")
	assert.NotContains(t, yamlStr, "-e ACTIONS_ID_TOKEN_REQUEST_TOKEN",
		"ACTIONS_ID_TOKEN_REQUEST_TOKEN should NOT be in docker command without HTTP MCP github-oidc auth")
	assert.Contains(t, yamlStr, "--exclude-env ACTIONS_ID_TOKEN_REQUEST_URL",
		"ACTIONS_ID_TOKEN_REQUEST_URL must always be excluded from the AWF agent")
	assert.Contains(t, yamlStr, "--exclude-env ACTIONS_ID_TOKEN_REQUEST_TOKEN",
		"ACTIONS_ID_TOKEN_REQUEST_TOKEN must always be excluded from the AWF agent")
}

// TestOTLPHeadersEnvVarPassedToGatewayContainer verifies that OTEL_EXPORTER_OTLP_HEADERS is
// passed to the MCP gateway container when observability.otlp is configured. This ensures
// that OTLP auth credentials are securely delivered via the container env rather than being
// embedded in the stdin JSON config pipe.
func TestOTLPHeadersEnvVarPassedToGatewayContainer(t *testing.T) {
	frontmatter := `---
on: workflow_dispatch
engine: copilot
tools:
  github:
    mode: remote
    toolsets: [repos]
observability:
  otlp:
    endpoint: "https://otel.example.com:4318"
    headers: "${{ secrets.OTEL_HEADERS }}"
---

# Test OTLP Headers Env Var

Test that OTEL_EXPORTER_OTLP_HEADERS is forwarded to the MCP gateway container.
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

	// Verify OTEL_EXPORTER_OTLP_HEADERS is passed to the docker container via -e flag
	assert.Contains(t, yamlStr, "-e OTEL_EXPORTER_OTLP_HEADERS",
		"OTEL_EXPORTER_OTLP_HEADERS should be passed to gateway container via -e flag")

	// Verify the docker command includes the -e flag before the container image
	dockerCmdPattern := `docker run.*-e OTEL_EXPORTER_OTLP_HEADERS.*ghcr\.io/github/gh-aw-mcpg`
	assert.Regexp(t, dockerCmdPattern, yamlStr,
		"Docker command should include -e OTEL_EXPORTER_OTLP_HEADERS before the container image")

	// Verify the headers value is NOT embedded in the JSON config pipe (security requirement)
	assert.NotContains(t, yamlStr, `"headers": "${OTEL_EXPORTER_OTLP_HEADERS}"`,
		"headers must not be embedded in the gateway JSON config")
}

// TestOTLPHeadersEnvVarNotPassedWithoutOTLP verifies that OTEL_EXPORTER_OTLP_HEADERS is
// still passed to the docker command when observability.otlp is not configured, since the
// compiler now falls back to the enterprise default OTLP endpoint/headers expressions
// (GH_AW_DEFAULT_OTLP_ENDPOINT / GH_AW_DEFAULT_OTLP_HEADERS) for every workflow.
func TestOTLPHeadersEnvVarNotPassedWithoutOTLP(t *testing.T) {
	frontmatter := `---
on: workflow_dispatch
engine: copilot
tools:
  github:
    mode: remote
    toolsets: [repos]
---

# Test No OTLP

Test that OTEL_EXPORTER_OTLP_HEADERS is passed through the enterprise default fallback
even when no explicit OTLP config is present.
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

	// Verify OTEL_EXPORTER_OTLP_HEADERS IS in the docker command, resolved via the
	// enterprise default secret expression rather than a literal value.
	assert.Contains(t, yamlStr, "-e OTEL_EXPORTER_OTLP_HEADERS",
		"OTEL_EXPORTER_OTLP_HEADERS should be passed through via the enterprise default fallback")
}
