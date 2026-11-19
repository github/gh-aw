//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestMCPDebugLoggingOnDemand(t *testing.T) {
	for _, engine := range []string{"copilot", "claude", "codex"} {
		t.Run(engine, func(t *testing.T) {
			workflowPath := filepath.Join(t.TempDir(), "debug.md")
			content := `---
on: workflow_dispatch
strict: false
engine: ` + engine + `
tools:
  agentic-workflows: true
mcp-scripts:
  hello:
    description: Say hello
    script: return "hello";
safe-outputs:
  noop:
---
Test on-demand MCP debug logging.
`
			if err := os.WriteFile(workflowPath, []byte(content), 0600); err != nil {
				t.Fatal(err)
			}
			compiler := NewCompiler()
			compiler.SetSkipValidation(true)
			data, err := compiler.ParseWorkflowFile(workflowPath)
			if err != nil {
				t.Fatal(err)
			}
			output, _, _, err := compiler.generateYAML(data, workflowPath)
			if err != nil {
				t.Fatal(err)
			}
			expected := "DEBUG: ${{ env.DEBUG || runner.debug == '1' && '*' || '' }}"
			if strings.Count(output, expected) != 2 {
				t.Error("Expected gateway and MCP scripts to honor explicit DEBUG and GitHub Actions debug mode")
			}
			for _, forced := range []string{`export DEBUG="*"`, `DEBUG: '*'`, `"DEBUG": "*"`} {
				if strings.Contains(output, forced) {
					t.Errorf("Unexpected forced debug logging: %s", forced)
				}
			}
			if strings.Count(output, `"DEBUG": "\${DEBUG}"`) != 2 {
				t.Error("Expected safe outputs and agentic-workflows servers to inherit DEBUG")
			}
		})
	}
}

// TestMCPServersCompilation verifies that mcp-servers configuration is properly compiled into workflows
// TestMCPEnvVarsAlphabeticallySorted verifies that env vars in MCP configs are sorted alphabetically
func TestMCPEnvVarsAlphabeticallySorted(t *testing.T) {
	// Create a temporary markdown file with mcp-servers configuration containing env vars
	workflowContent := `---
on:
  workflow_dispatch:
strict: false
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: copilot
mcp-servers:
  test-server:
    container: example/test:latest
    env:
      ZEBRA_VAR: "z"
      ALPHA_VAR: "a"
      BETA_VAR: "b"
---

# Test MCP Env Var Sorting

This workflow tests that MCP server env vars are sorted alphabetically.
`

	// Create temporary file
	tmpFile, err := os.CreateTemp("", "test-env-sort-*.md")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	// Write content to file
	if _, err := tmpFile.WriteString(workflowContent); err != nil {
		t.Fatalf("Failed to write to temp file: %v", err)
	}
	tmpFile.Close()

	// Create compiler and compile workflow
	compiler := NewCompiler()
	compiler.SetSkipValidation(true)

	// Generate YAML
	workflowData, err := compiler.ParseWorkflowFile(tmpFile.Name())
	if err != nil {
		t.Fatalf("Failed to parse workflow file: %v", err)
	}

	yamlContent, _, _, err := compiler.generateYAML(workflowData, tmpFile.Name())
	if err != nil {
		t.Fatalf("Failed to generate YAML: %v", err)
	}

	// Find the test-server env section in the generated YAML
	// Look for "test-server" first, then find the env section after it
	testServerIndex := strings.LastIndex(yamlContent, `"test-server"`)
	if testServerIndex == -1 {
		t.Fatalf("Could not find test-server section in generated YAML")
	}

	// Find env section after test-server
	envIndex := strings.Index(yamlContent[testServerIndex:], `"env": {`)
	if envIndex == -1 {
		t.Fatalf("Could not find env section for test-server in generated YAML")
	}

	// Adjust envIndex to be relative to the full yamlContent
	envIndex += testServerIndex

	// Extract a portion of YAML starting from env section (next 300 chars should be enough)
	envSection := yamlContent[envIndex : envIndex+300]

	// Verify that ALPHA_VAR appears before BETA_VAR and ZEBRA_VAR
	alphaIndex := strings.Index(envSection, `"ALPHA_VAR"`)
	betaIndex := strings.Index(envSection, `"BETA_VAR"`)
	zebraIndex := strings.Index(envSection, `"ZEBRA_VAR"`)

	if alphaIndex == -1 || betaIndex == -1 || zebraIndex == -1 {
		t.Fatalf("Could not find all env vars in generated YAML. Section: %s", envSection)
	}

	// Verify alphabetical order
	if alphaIndex >= betaIndex {
		t.Errorf("Expected ALPHA_VAR to appear before BETA_VAR, but ALPHA_VAR is at %d and BETA_VAR is at %d", alphaIndex, betaIndex)
	}
	if betaIndex >= zebraIndex {
		t.Errorf("Expected BETA_VAR to appear before ZEBRA_VAR, but BETA_VAR is at %d and ZEBRA_VAR is at %d", betaIndex, zebraIndex)
	}
	if alphaIndex >= zebraIndex {
		t.Errorf("Expected ALPHA_VAR to appear before ZEBRA_VAR, but ALPHA_VAR is at %d and ZEBRA_VAR is at %d", alphaIndex, zebraIndex)
	}
}

// TestHasMCPConfigDetection verifies that hasMCPConfig properly detects MCP configurations
func TestHasMCPConfigDetection(t *testing.T) {
	testCases := []struct {
		name     string
		config   map[string]any
		expected bool
		mcpType  string
	}{
		{
			name: "explicit stdio type",
			config: map[string]any{
				"type":    "stdio",
				"command": "npx",
			},
			expected: true,
			mcpType:  "stdio",
		},
		{
			name: "explicit http type",
			config: map[string]any{
				"type": "http",
				"url":  "https://example.com",
			},
			expected: true,
			mcpType:  "http",
		},
		{
			name: "inferred stdio from command",
			config: map[string]any{
				"command": "npx",
				"args":    []any{"-y", "@microsoft/markitdown"},
			},
			expected: true,
			mcpType:  "stdio",
		},
		{
			name: "inferred http from url",
			config: map[string]any{
				"url": "https://example.com/mcp",
			},
			expected: true,
			mcpType:  "http",
		},
		{
			name: "inferred stdio from container",
			config: map[string]any{
				"container": "example/mcp:latest",
			},
			expected: true,
			mcpType:  "stdio",
		},
		{
			name: "not MCP config",
			config: map[string]any{
				"allowed": []any{"some_tool"},
			},
			expected: false,
			mcpType:  "",
		},
		{
			name: "markitdown-like config",
			config: map[string]any{
				"registry": "https://api.mcp.github.com/v0/servers/microsoft/markitdown",
				"command":  "npx",
				"args":     []any{"-y", "@microsoft/markitdown"},
			},
			expected: true,
			mcpType:  "stdio",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			hasMcp, mcpType := hasMCPConfig(tc.config)
			if hasMcp != tc.expected {
				t.Errorf("Expected hasMCPConfig to return %v, got %v", tc.expected, hasMcp)
			}
			if mcpType != tc.mcpType {
				t.Errorf("Expected MCP type %q, got %q", tc.mcpType, mcpType)
			}
		})
	}
}

// TestMCPServersAllowedToolFilterCompilation verifies that the allowed tool filter in
// mcp-servers section is properly compiled into the "tools" field in the output.
func TestMCPServersAllowedToolFilterCompilation(t *testing.T) {
	tests := []struct {
		name               string
		workflowContent    string
		serverName         string
		expectedContent    []string
		unexpectedInServer []string
	}{
		{
			name: "copilot - http mcp server with specific allowed tools",
			workflowContent: `---
on:
  workflow_dispatch:
strict: false
permissions:
  contents: read
engine: copilot
mcp-servers:
  my-api:
    type: http
    url: https://api.example.com/mcp
    allowed:
      - get_data
      - list_items
---

Test workflow.
`,
			serverName:      `"my-api"`,
			expectedContent: []string{`"get_data"`, `"list_items"`},
			// Regex: "tools" key whose value array starts with "*" (ignores whitespace/indentation).
			// guard-policies "accept": ["*"] has a different key, so it is never matched.
			unexpectedInServer: []string{`"tools"\s*:\s*\[\s*"\*"`},
		},
		{
			name: "copilot - stdio mcp server with specific allowed tools",
			workflowContent: `---
on:
  workflow_dispatch:
strict: false
permissions:
  contents: read
engine: copilot
mcp-servers:
  my-tool:
    container: example/tool:latest
    allowed:
      - run_query
      - fetch_results
---

Test workflow.
`,
			serverName:      `"my-tool"`,
			expectedContent: []string{`"run_query"`, `"fetch_results"`},
			// Regex: "tools" key whose value array starts with "*" (ignores whitespace/indentation).
			// guard-policies "accept": ["*"] has a different key, so it is never matched.
			unexpectedInServer: []string{`"tools"\s*:\s*\[\s*"\*"`},
		},
		{
			name: "copilot - mcp server with no allowed field defaults to wildcard",
			workflowContent: `---
on:
  workflow_dispatch:
strict: false
permissions:
  contents: read
engine: copilot
mcp-servers:
  my-api:
    type: http
    url: https://api.example.com/mcp
---

Test workflow.
`,
			serverName:         `"my-api"`,
			expectedContent:    []string{`"*"`},
			unexpectedInServer: []string{},
		},
		{
			name: "claude - http mcp server with specific allowed tools passes through",
			workflowContent: `---
on:
  workflow_dispatch:
strict: false
permissions:
  contents: read
engine: claude
mcp-servers:
  my-api:
    type: http
    url: https://api.example.com/mcp
    allowed:
      - get_data
      - list_items
---

Test workflow.
`,
			serverName:      `"my-api"`,
			expectedContent: []string{`"get_data"`, `"list_items"`},
			// Regex: "tools" key whose value array starts with "*" (ignores whitespace/indentation).
			// guard-policies "accept": ["*"] has a different key, so it is never matched.
			unexpectedInServer: []string{`"tools"\s*:\s*\[\s*"\*"`},
		},
		{
			name: "claude - http mcp server with no allowed field has no tools filter",
			workflowContent: `---
on:
  workflow_dispatch:
strict: false
permissions:
  contents: read
engine: claude
mcp-servers:
  my-api:
    type: http
    url: https://api.example.com/mcp
---

Test workflow.
`,
			serverName:         `"my-api"`,
			expectedContent:    []string{`"url":`},
			unexpectedInServer: []string{`"tools":`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpFile, err := os.CreateTemp("", "test-allowed-filter-*.md")
			if err != nil {
				t.Fatalf("Failed to create temp file: %v", err)
			}
			defer os.Remove(tmpFile.Name())

			if _, err := tmpFile.WriteString(tt.workflowContent); err != nil {
				t.Fatalf("Failed to write to temp file: %v", err)
			}
			tmpFile.Close()

			compiler := NewCompiler()

			workflowData, err := compiler.ParseWorkflowFile(tmpFile.Name())
			if err != nil {
				t.Fatalf("Failed to parse workflow file: %v", err)
			}

			yamlContent, _, _, err := compiler.generateYAML(workflowData, tmpFile.Name())
			if err != nil {
				t.Fatalf("Failed to generate YAML: %v", err)
			}

			// Find the server-specific block in the YAML
			serverIndex := strings.LastIndex(yamlContent, tt.serverName)
			if serverIndex == -1 {
				t.Fatalf("Could not find server %s in generated YAML", tt.serverName)
			}

			// Extract the server block (next 500 chars should be sufficient)
			endIdx := min(serverIndex+500, len(yamlContent))
			serverBlock := yamlContent[serverIndex:endIdx]

			for _, content := range tt.expectedContent {
				if !strings.Contains(serverBlock, content) {
					t.Errorf("Expected %q in server block for %s, but not found.\nServer block:\n%s",
						content, tt.serverName, serverBlock)
				}
			}

			for _, pattern := range tt.unexpectedInServer {
				matched, err := regexp.MatchString(pattern, serverBlock)
				if err != nil {
					t.Fatalf("Invalid regex pattern %q: %v", pattern, err)
				}
				if matched {
					t.Errorf("Unexpected pattern %q matched in server block for %s.\nServer block:\n%s",
						pattern, tt.serverName, serverBlock)
				}
			}
		})
	}
}

// TestDevModeAgenticWorkflowsContainer verifies that the agentic-workflows MCP server
// uses the locally built Docker image in dev mode instead of alpine:latest
func TestDevModeAgenticWorkflowsContainer(t *testing.T) {
	tests := []struct {
		name              string
		actionMode        ActionMode
		expectedContainer string
	}{
		{
			name:              "dev mode uses local image",
			actionMode:        ActionModeDev,
			expectedContainer: "localhost/gh-aw:dev",
		},
		{
			name:              "release mode uses alpine",
			actionMode:        ActionModeRelease,
			expectedContainer: "alpine:latest",
		},
		{
			name:              "script mode uses alpine",
			actionMode:        ActionModeScript,
			expectedContainer: "alpine:latest",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a workflow with agentic-workflows tool
			workflowContent := `---
on:
  workflow_dispatch:
strict: false
permissions:
  contents: read
  actions: read
engine: copilot
tools:
  agentic-workflows:
---

# Test Agentic Workflows Dev Mode

This workflow tests that agentic-workflows uses the correct container in dev mode.
`

			// Create temporary file
			tmpFile, err := os.CreateTemp("", "test-dev-mode-*.md")
			if err != nil {
				t.Fatalf("Failed to create temp file: %v", err)
			}
			defer os.Remove(tmpFile.Name())

			// Write content to file
			if _, err := tmpFile.WriteString(workflowContent); err != nil {
				t.Fatalf("Failed to write to temp file: %v", err)
			}
			tmpFile.Close()

			// Compile the workflow with the specified action mode
			compiler := NewCompiler()
			compiler.SetActionMode(tt.actionMode)

			if err := compiler.CompileWorkflow(tmpFile.Name()); err != nil {
				t.Fatalf("Failed to compile workflow: %v", err)
			}

			// Read the compiled lock file
			lockFile := strings.TrimSuffix(tmpFile.Name(), ".md") + ".lock.yml"
			defer os.Remove(lockFile)

			lockContent, err := os.ReadFile(lockFile)
			if err != nil {
				t.Fatalf("Failed to read lock file: %v", err)
			}

			// Check that the container image is correct
			if !strings.Contains(string(lockContent), `"container": "`+tt.expectedContainer+`"`) {
				t.Errorf("Expected container %q in lock file, but not found. Lock file content:\n%s",
					tt.expectedContainer, string(lockContent))
			}

			// In dev mode, verify dev-specific configuration
			if tt.actionMode == ActionModeDev {
				// Check for build steps
				requiredSteps := []string{
					"Setup Go for CLI build",
					"Build gh-aw CLI",
					"Setup Docker Buildx",
					"Build gh-aw Docker image",
					"tags: localhost/gh-aw:dev",
				}

				for _, step := range requiredSteps {
					if !strings.Contains(string(lockContent), step) {
						t.Errorf("Expected build step %q in dev mode lock file, but not found", step)
					}
				}

				// Verify binary copy step for user-defined steps that execute ./gh-aw
				if !strings.Contains(string(lockContent), "cp dist/gh-aw-linux-amd64 ./gh-aw") {
					t.Error("Expected 'cp dist/gh-aw-linux-amd64 ./gh-aw' in dev mode build step")
				}
				if !strings.Contains(string(lockContent), "chmod +x ./gh-aw") {
					t.Error("Expected 'chmod +x ./gh-aw' in dev mode build step")
				}

				// Verify NO release-mode agenticworkflows entrypoint (uses container's default ENTRYPOINT in dev mode)
				// Note: safeoutputs section always has entrypoint = "sh" — only agenticworkflows omits it in dev mode
				if strings.Contains(string(lockContent), `"entrypoint": "${RUNNER_TEMP}/gh-aw/gh-aw"`) {
					t.Error("Did not expect release-mode agenticworkflows entrypoint in dev mode")
				}

				// Verify NO release-mode agenticworkflows entrypointArgs (uses container's default CMD in dev mode)
				if strings.Contains(string(lockContent), `"entrypointArgs": ["mcp-server", "--validate-actor"]`) {
					t.Error("Did not expect release-mode agenticworkflows entrypointArgs in dev mode")
				}

				// Verify no --cmd argument
				if strings.Contains(string(lockContent), `"--cmd"`) {
					t.Error("Did not expect --cmd argument in dev mode")
				}

				// Verify the private setup root is always mounted read-only for security
				// (protects gh-aw infrastructure files from direct agent writes in all modes)
				if !strings.Contains(string(lockContent), `${GH_AW_SETUP_ROOT}:${GH_AW_SETUP_ROOT}:ro`) {
					t.Error("Expected GH_AW_SETUP_ROOT to be mounted read-only in AWF for security")
				}
				if strings.Contains(string(lockContent), `/usr/bin/gh:/usr/bin/gh:ro`) {
					t.Error("Did not expect /usr/bin/gh mount in dev mode (gh CLI is in image)")
				}

				// Verify DEBUG and GITHUB_TOKEN are present
				if !strings.Contains(string(lockContent), `"DEBUG": "\${DEBUG}"`) {
					t.Error("Expected DEBUG forwarded from the environment in dev mode")
				}
				if !strings.Contains(string(lockContent), `"GITHUB_TOKEN"`) {
					t.Error("Expected GITHUB_TOKEN in dev mode env vars")
				}

				// Verify working directory args are present
				if !strings.Contains(string(lockContent), `"args": ["--network", "host", "-w", "\${GITHUB_WORKSPACE}"]`) {
					t.Error("Expected args with network access and working directory in dev mode")
				}
			}
		})
	}
}

// TestNpxCommandAutoContainerization verifies that `command: "npx"` with args is auto-converted to a
// containerized stdio server without duplicating the command in entrypointArgs.
// Regression test: the auto-containerization previously prepended the command (e.g. "npx") to
// entrypointArgs, which caused Docker to run "npx npx @sentry/mcp-server" instead of the correct
// "npx @sentry/mcp-server", resulting in the MCP server exposing 0 tools.
func TestNpxCommandAutoContainerization(t *testing.T) {
	tests := []struct {
		name         string
		command      string
		args         string
		wantImage    string
		wantEntry    string
		wantFirstArg string
	}{
		{
			name:         "npx command with package arg",
			command:      "npx",
			args:         `["@sentry/mcp-server@0.33.0"]`,
			wantImage:    "node:lts-alpine",
			wantEntry:    "npx",
			wantFirstArg: "@sentry/mcp-server@0.33.0",
		},
		{
			name:         "npx command with -y flag and package",
			command:      "npx",
			args:         `["-y", "@modelcontextprotocol/server-memory"]`,
			wantImage:    "node:lts-alpine",
			wantEntry:    "npx",
			wantFirstArg: "-y",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workflowContent := `---
on:
  workflow_dispatch:
strict: false
permissions:
  contents: read
engine: copilot
mcp-servers:
  my-server:
    command: "` + tt.command + `"
    args: ` + tt.args + `
---

# Test workflow
`
			tmpFile, err := os.CreateTemp("", "test-npx-container-*.md")
			if err != nil {
				t.Fatalf("Failed to create temp file: %v", err)
			}
			defer os.Remove(tmpFile.Name())

			if _, err := tmpFile.WriteString(workflowContent); err != nil {
				t.Fatalf("Failed to write workflow content: %v", err)
			}
			tmpFile.Close()

			compiler := NewCompiler()
			compiler.SetSkipValidation(true)

			workflowData, err := compiler.ParseWorkflowFile(tmpFile.Name())
			if err != nil {
				t.Fatalf("Failed to parse workflow file: %v", err)
			}

			yamlContent, _, _, err := compiler.generateYAML(workflowData, tmpFile.Name())
			if err != nil {
				t.Fatalf("Failed to generate YAML: %v", err)
			}

			serverIdx := strings.LastIndex(yamlContent, `"my-server"`)
			if serverIdx == -1 {
				t.Fatal("Could not find my-server block in generated YAML")
			}
			endIdx := min(serverIdx+800, len(yamlContent))
			serverBlock := yamlContent[serverIdx:endIdx]

			// container must be auto-assigned
			if !strings.Contains(serverBlock, `"container": "`+tt.wantImage+`"`) {
				t.Errorf("Expected container=%q in server block; got:\n%s", tt.wantImage, serverBlock)
			}

			// entrypoint must be the command
			if !strings.Contains(serverBlock, `"entrypoint": "`+tt.wantEntry+`"`) {
				t.Errorf("Expected entrypoint=%q in server block; got:\n%s", tt.wantEntry, serverBlock)
			}

			// entrypointArgs must NOT start with the command itself (no double "npx"/"uvx")
			badPattern := `"entrypointArgs":\s*\[\s*"` + tt.command + `"`
			if matched, _ := regexp.MatchString(badPattern, serverBlock); matched {
				t.Errorf("entrypointArgs must NOT begin with %q (the command is already the entrypoint and must not be duplicated); got:\n%s",
					tt.command, serverBlock)
			}

			// entrypointArgs must start with the first actual arg
			if !strings.Contains(serverBlock, `"`+tt.wantFirstArg+`"`) {
				t.Errorf("Expected first entrypointArg %q in server block; got:\n%s", tt.wantFirstArg, serverBlock)
			}
		})
	}
}

// TestCustomMCPEnvSecretSingleEscape is a regression test for the double-escape bug
// introduced in v0.81.2 (PR #41038). Custom MCP server env secrets must be rendered
// with a single backslash (\${VAR}) in the generated lock file, NOT double-escaped
// (\\${VAR}). The unquoted heredoc in the generated workflow passes the content
// through bash, which collapses \$ → $, giving the MCP gateway the literal ${VAR}
// string it then expands from its own environment. Double backslashes cause bash to
// produce \<secret-value>, which is an invalid JSON escape character.
func TestCustomMCPEnvSecretSingleEscape(t *testing.T) {
	workflowContent := `---
on:
  workflow_dispatch:
strict: false
permissions:
  contents: read
engine: copilot
mcp-servers:
  my-server:
    container: "example/my-server:latest"
    env:
      MY_API_TOKEN: "${{ secrets.MY_API_TOKEN }}"
      PLAIN_VALUE: "hello"
---

# Test env secret escaping

Do nothing.
`

	tmpFile, err := os.CreateTemp("", "test-env-escape-*.md")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(workflowContent); err != nil {
		t.Fatalf("Failed to write to temp file: %v", err)
	}
	tmpFile.Close()

	compiler := NewCompiler()
	compiler.SetSkipValidation(true)

	workflowData, err := compiler.ParseWorkflowFile(tmpFile.Name())
	if err != nil {
		t.Fatalf("Failed to parse workflow file: %v", err)
	}

	yamlContent, _, _, err := compiler.generateYAML(workflowData, tmpFile.Name())
	if err != nil {
		t.Fatalf("Failed to generate YAML: %v", err)
	}

	// Locate the my-server block.
	serverIdx := strings.LastIndex(yamlContent, `"my-server"`)
	if serverIdx == -1 {
		t.Fatal("Could not find my-server block in generated YAML")
	}
	serverBlock := yamlContent[serverIdx:min(serverIdx+1000, len(yamlContent))]

	// The secret placeholder must appear with a SINGLE backslash.
	// In a Go raw string literal, \$ is the two-character sequence backslash-dollar,
	// which is exactly what should be in the generated lock file.
	if !strings.Contains(serverBlock, `"\${MY_API_TOKEN}"`) {
		t.Errorf("expected single-backslash placeholder \"\\${MY_API_TOKEN}\" in generated env section; got server block:\n%s", serverBlock)
	}

	// There must be NO double-escaped form — that is the regression we are guarding against.
	if strings.Contains(serverBlock, `"\\${MY_API_TOKEN}"`) {
		t.Errorf("double-escaped placeholder \"\\\\${MY_API_TOKEN}\" found in generated env section (regression); got server block:\n%s", serverBlock)
	}

	// Plain (non-secret) env values must be rendered as-is.
	if !strings.Contains(serverBlock, `"PLAIN_VALUE": "hello"`) {
		t.Errorf("expected plain env value \"PLAIN_VALUE\": \"hello\" in generated env section; got server block:\n%s", serverBlock)
	}
}

// TestHTTPMCPHeaderSecretSingleEscape is a regression test for the double-escape bug
// applied to HTTP MCP server header secrets.  When a header value contains a secret
// expression (e.g. "${{ secrets.DD_API_KEY }}"), the compiled lock file must render
// it as "\${DD_API_KEY}" (single backslash) so that the unquoted heredoc in bash
// produces "${DD_API_KEY}" for the MCP gateway — not "\\${DD_API_KEY}" which would
// expand to "\<secret-value>" (an invalid JSON escape character).
func TestHTTPMCPHeaderSecretSingleEscape(t *testing.T) {
	workflowContent := `---
on:
  workflow_dispatch:
strict: false
permissions:
  contents: read
engine: copilot
mcp-servers:
  datadog:
    type: http
    url: https://mcp.datadoghq.com/api/mcp
    headers:
      DD_API_KEY: "${{ secrets.DD_API_KEY }}"
      X-Static-Header: "static-value"
---

# Test HTTP header secret escaping

Do nothing.
`

	tmpFile, err := os.CreateTemp("", "test-http-header-escape-*.md")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(workflowContent); err != nil {
		t.Fatalf("Failed to write to temp file: %v", err)
	}
	tmpFile.Close()

	compiler := NewCompiler()
	compiler.SetSkipValidation(true)

	workflowData, err := compiler.ParseWorkflowFile(tmpFile.Name())
	if err != nil {
		t.Fatalf("Failed to parse workflow file: %v", err)
	}

	yamlContent, _, _, err := compiler.generateYAML(workflowData, tmpFile.Name())
	if err != nil {
		t.Fatalf("Failed to generate YAML: %v", err)
	}

	serverIdx := strings.LastIndex(yamlContent, `"datadog"`)
	if serverIdx == -1 {
		t.Fatal("Could not find datadog block in generated YAML")
	}
	serverBlock := yamlContent[serverIdx:min(serverIdx+1000, len(yamlContent))]

	// The header secret must appear with a SINGLE backslash in the headers section.
	if !strings.Contains(serverBlock, `"DD_API_KEY": "\${DD_API_KEY}"`) {
		t.Errorf("expected single-backslash placeholder in headers section; got server block:\n%s", serverBlock)
	}

	// The env passthrough section must also use a single backslash.
	if !strings.Contains(serverBlock, `"DD_API_KEY": "\${DD_API_KEY}"`) {
		t.Errorf("expected single-backslash placeholder in env passthrough section; got server block:\n%s", serverBlock)
	}

	// There must be NO double-escaped form in either section.
	if strings.Contains(serverBlock, `"\\${DD_API_KEY}"`) {
		t.Errorf("double-escaped placeholder found in generated server block (regression); got:\n%s", serverBlock)
	}

	// Static header values must be rendered verbatim (no escaping applied).
	if !strings.Contains(serverBlock, `"X-Static-Header": "static-value"`) {
		t.Errorf("expected static header value to be rendered verbatim; got server block:\n%s", serverBlock)
	}
}

// TestMCPSecretEscapingAcrossConfigurations is a table-driven regression test covering
// several MCP server configurations that all involve secret expressions. For each case
// the compiled lock file must render placeholders with a SINGLE backslash (\${VAR}) and
// must NOT contain double-escaped placeholders (\\${VAR}).
func TestMCPSecretEscapingAcrossConfigurations(t *testing.T) {
	tests := []struct {
		name            string
		workflowContent string
		serverName      string
		wantSingle      []string // must be present (single backslash form)
		wantAbsent      []string // must not be present (double backslash form)
	}{
		{
			name: "container env secret - copilot engine",
			workflowContent: `---
on:
  workflow_dispatch:
strict: false
permissions:
  contents: read
engine: copilot
mcp-servers:
  api-server:
    container: "example/api:latest"
    env:
      API_TOKEN: "${{ secrets.API_TOKEN }}"
---

Do nothing.
`,
			serverName: `"api-server"`,
			wantSingle: []string{`"\${API_TOKEN}"`},
			wantAbsent: []string{`"\\${API_TOKEN}"`},
		},
		{
			name: "HTTP server with header secret - copilot engine",
			workflowContent: `---
on:
  workflow_dispatch:
strict: false
permissions:
  contents: read
engine: copilot
mcp-servers:
  api-http:
    type: http
    url: https://api.example.com/mcp
    headers:
      Authorization: "${{ secrets.API_TOKEN }}"
---

Do nothing.
`,
			serverName: `"api-http"`,
			wantSingle: []string{`"\${API_TOKEN}"`},
			wantAbsent: []string{`"\\${API_TOKEN}"`},
		},
		{
			name: "multiple env secrets - copilot engine",
			workflowContent: `---
on:
  workflow_dispatch:
strict: false
permissions:
  contents: read
engine: copilot
mcp-servers:
  multi-secret:
    container: "example/multi:latest"
    env:
      TOKEN_A: "${{ secrets.TOKEN_A }}"
      TOKEN_B: "${{ secrets.TOKEN_B }}"
      PLAIN: "no-secret"
---

Do nothing.
`,
			serverName: `"multi-secret"`,
			wantSingle: []string{`"\${TOKEN_A}"`, `"\${TOKEN_B}"`, `"PLAIN": "no-secret"`},
			wantAbsent: []string{`"\\${TOKEN_A}"`, `"\\${TOKEN_B}"`},
		},
		// Regression test: GitHub remote MCP Copilot Authorization header must use a single
		// backslash (\${GITHUB_PERSONAL_ACCESS_TOKEN}) in the generated lock file so that the
		// unquoted bash heredoc emits ${GITHUB_PERSONAL_ACCESS_TOKEN} (a valid JSON string that
		// the gateway expands at runtime). Double-escaping (\\${...}) causes bash to emit
		// \<token-value>, which is an invalid JSON escape sequence and makes JSON.parse fail.
		{
			name: "GitHub remote MCP Authorization header - copilot engine single-escape",
			workflowContent: `---
on:
  workflow_dispatch:
strict: false
permissions:
  contents: read
engine: copilot
tools:
  github:
    mode: remote
---

Do nothing.
`,
			serverName: `"github"`,
			wantSingle: []string{`"Authorization": "Bearer \${GITHUB_PERSONAL_ACCESS_TOKEN}"`},
			wantAbsent: []string{`"Authorization": "Bearer \\${GITHUB_PERSONAL_ACCESS_TOKEN}"`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpFile, err := os.CreateTemp("", "test-mcp-escape-*.md")
			if err != nil {
				t.Fatalf("Failed to create temp file: %v", err)
			}
			defer os.Remove(tmpFile.Name())

			if _, err := tmpFile.WriteString(tt.workflowContent); err != nil {
				t.Fatalf("Failed to write to temp file: %v", err)
			}
			tmpFile.Close()

			compiler := NewCompiler()
			compiler.SetSkipValidation(true)

			workflowData, err := compiler.ParseWorkflowFile(tmpFile.Name())
			if err != nil {
				t.Fatalf("Failed to parse workflow file: %v", err)
			}

			yamlContent, _, _, err := compiler.generateYAML(workflowData, tmpFile.Name())
			if err != nil {
				t.Fatalf("Failed to generate YAML: %v", err)
			}

			serverIdx := strings.LastIndex(yamlContent, tt.serverName)
			if serverIdx == -1 {
				t.Fatalf("Could not find server %s in generated YAML", tt.serverName)
			}
			serverBlock := yamlContent[serverIdx:min(serverIdx+1500, len(yamlContent))]

			for _, want := range tt.wantSingle {
				if !strings.Contains(serverBlock, want) {
					t.Errorf("expected %q in server block but not found; block:\n%s", want, serverBlock)
				}
			}
			for _, absent := range tt.wantAbsent {
				if strings.Contains(serverBlock, absent) {
					t.Errorf("unexpected double-escaped placeholder %q found (regression); block:\n%s", absent, serverBlock)
				}
			}
		})
	}
}

// TestMCPServerRequiredFalseCompilation verifies that a server marked with
// required: false is accepted by validation and rendered into the gateway
// configuration so its startup failure can be degraded to a warning.
func TestMCPServerRequiredFalseCompilation(t *testing.T) {
	workflowContent := `---
on:
  workflow_dispatch:
strict: false
permissions:
  contents: read
engine: copilot
mcp-servers:
  optional-http:
    type: http
    url: "https://example.com/mcp"
    required: false
  critical-http:
    type: http
    url: "https://example.com/other-mcp"
---

# Test MCP Required Flag

This workflow tests that non-critical MCP servers are marked as such.
`

	tmpFile, err := os.CreateTemp("", "test-mcp-required-*.md")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(workflowContent); err != nil {
		t.Fatalf("Failed to write to temp file: %v", err)
	}
	tmpFile.Close()

	compiler := NewCompiler()
	compiler.SetSkipValidation(true)

	workflowData, err := compiler.ParseWorkflowFile(tmpFile.Name())
	if err != nil {
		t.Fatalf("Failed to parse workflow file: %v", err)
	}

	yamlContent, _, _, err := compiler.generateYAML(workflowData, tmpFile.Name())
	if err != nil {
		t.Fatalf("Failed to generate YAML: %v", err)
	}

	optionalIndex := strings.Index(yamlContent, `"optional-http"`)
	if optionalIndex == -1 {
		t.Fatalf("Could not find optional-http server in generated YAML")
	}
	if !strings.Contains(yamlContent, `"critical-http"`) {
		t.Fatalf("Could not find critical-http server in generated YAML")
	}

	// The required flag must be emitted only for the non-critical server.
	requiredIndex := strings.Index(yamlContent, `"required": false`)
	if requiredIndex == -1 {
		t.Fatalf("Expected \"required\": false to be rendered for optional-http")
	}
	if strings.Count(yamlContent, `"required": false`) != 1 {
		t.Errorf("Expected exactly one \"required\": false entry, got %d", strings.Count(yamlContent, `"required": false`))
	}
	if requiredIndex < optionalIndex {
		t.Errorf("Expected \"required\": false to be rendered inside the optional-http server block")
	}
}
