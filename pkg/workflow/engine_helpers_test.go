//go:build !integration

package workflow

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
)

func TestBuildStandardNpmEngineInstallSteps(t *testing.T) {
	tests := []struct {
		name           string
		workflowData   *WorkflowData
		expectedSteps  int // Number of steps expected (Node.js setup + npm install)
		expectedInStep string
	}{
		{
			name:           "with default version",
			workflowData:   &WorkflowData{},
			expectedSteps:  2, // Node.js setup + npm install
			expectedInStep: string(constants.DefaultCopilotVersion),
		},
		{
			name: "with custom version from engine config",
			workflowData: &WorkflowData{
				EngineConfig: &EngineConfig{
					Version: "1.2.3",
				},
			},
			expectedSteps:  2,
			expectedInStep: "1.2.3",
		},
		{
			name: "with empty version in engine config (use default)",
			workflowData: &WorkflowData{
				EngineConfig: &EngineConfig{
					Version: "",
				},
			},
			expectedSteps:  2,
			expectedInStep: string(constants.DefaultCopilotVersion),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			steps := BuildStandardNpmEngineInstallSteps(
				"@github/copilot",
				string(constants.DefaultCopilotVersion),
				"Install GitHub Copilot CLI",
				"copilot",
				tt.workflowData,
			)

			if len(steps) != tt.expectedSteps {
				t.Errorf("Expected %d steps, got %d", tt.expectedSteps, len(steps))
			}

			// Verify that the expected version appears in the steps
			found := false
			for _, step := range steps {
				for _, line := range step {
					if strings.Contains(line, tt.expectedInStep) {
						found = true
						break
					}
				}
			}

			if !found {
				t.Errorf("Expected version %s not found in steps", tt.expectedInStep)
			}
		})
	}
}

func TestBuildStandardNpmEngineInstallSteps_AllEngines(t *testing.T) {
	tests := []struct {
		name           string
		packageName    string
		defaultVersion string
		stepName       string
		cacheKeyPrefix string
	}{
		{
			name:           "copilot engine",
			packageName:    "@github/copilot",
			defaultVersion: string(constants.DefaultCopilotVersion),
			stepName:       "Install GitHub Copilot CLI",
			cacheKeyPrefix: "copilot",
		},
		{
			name:           "codex engine",
			packageName:    "@openai/codex",
			defaultVersion: string(constants.DefaultCodexVersion),
			stepName:       "Install Codex",
			cacheKeyPrefix: "codex",
		},
		{
			name:           "claude engine",
			packageName:    "@anthropic-ai/claude-code",
			defaultVersion: string(constants.DefaultClaudeCodeVersion),
			stepName:       "Install Claude Code CLI",
			cacheKeyPrefix: "claude",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workflowData := &WorkflowData{}

			steps := BuildStandardNpmEngineInstallSteps(
				tt.packageName,
				tt.defaultVersion,
				tt.stepName,
				tt.cacheKeyPrefix,
				workflowData,
			)

			if len(steps) < 1 {
				t.Errorf("Expected at least 1 step, got %d", len(steps))
			}

			// Verify package name appears in steps
			found := false
			for _, step := range steps {
				for _, line := range step {
					if strings.Contains(line, tt.packageName) {
						found = true
						break
					}
				}
			}

			if !found {
				t.Errorf("Expected package name %s not found in steps", tt.packageName)
			}
		})
	}
}

// TestResolveAgentFilePath tests the shared agent file path resolution helper
func TestResolveAgentFilePath(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "basic agent file path",
			input:    ".github/agents/test-agent.md",
			expected: "\"${GITHUB_WORKSPACE}/.github/agents/test-agent.md\"",
		},
		{
			name:     "path with spaces",
			input:    ".github/agents/my agent file.md",
			expected: "\"${GITHUB_WORKSPACE}/.github/agents/my agent file.md\"",
		},
		{
			name:     "deeply nested path",
			input:    ".github/copilot/instructions/deep/nested/agent.md",
			expected: "\"${GITHUB_WORKSPACE}/.github/copilot/instructions/deep/nested/agent.md\"",
		},
		{
			name:     "simple filename",
			input:    "agent.md",
			expected: "\"${GITHUB_WORKSPACE}/agent.md\"",
		},
		{
			name:     "path with special characters",
			input:    ".github/agents/test-agent_v2.0.md",
			expected: "\"${GITHUB_WORKSPACE}/.github/agents/test-agent_v2.0.md\"",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ResolveAgentFilePath(tt.input)
			if result != tt.expected {
				t.Errorf("ResolveAgentFilePath(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

// TestResolveAgentFilePathFormat tests that the output format is consistent
func TestResolveAgentFilePathFormat(t *testing.T) {
	input := ".github/agents/test.md"
	result := ResolveAgentFilePath(input)

	// Verify it starts with opening quote, GITHUB_WORKSPACE variable, and forward slash
	expectedPrefix := "\"${GITHUB_WORKSPACE}/"
	if !strings.HasPrefix(result, expectedPrefix) {
		t.Errorf("Expected path to start with %q, got: %s", expectedPrefix, result)
	}

	// Verify it ends with the input path and a closing quote
	expectedSuffix := input + "\""
	if !strings.HasSuffix(result, expectedSuffix) {
		t.Errorf("Expected path to end with %q, got: %q", expectedSuffix, result)
	}

	// Verify the complete expected format
	expected := "\"${GITHUB_WORKSPACE}/" + input + "\""
	if result != expected {
		t.Errorf("Expected %q, got: %q", expected, result)
	}
}

// TestExtractAgentIdentifier tests extracting agent identifier from file paths
func TestExtractAgentIdentifier(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "basic agent file path",
			input:    ".github/agents/test-agent.md",
			expected: "test-agent",
		},
		{
			name:     "path with spaces",
			input:    ".github/agents/my agent file.md",
			expected: "my agent file",
		},
		{
			name:     "deeply nested path",
			input:    ".github/copilot/instructions/deep/nested/agent.md",
			expected: "agent",
		},
		{
			name:     "simple filename",
			input:    "agent.md",
			expected: "agent",
		},
		{
			name:     "path with special characters",
			input:    ".github/agents/test-agent_v2.0.md",
			expected: "test-agent_v2.0",
		},
		{
			name:     "cli-consistency-checker example",
			input:    ".github/agents/cli-consistency-checker.md",
			expected: "cli-consistency-checker",
		},
		{
			name:     "path without extension",
			input:    ".github/agents/test-agent",
			expected: "test-agent",
		},
		{
			name:     "custom agent file simple path",
			input:    ".github/agents/test-agent.agent.md",
			expected: "test-agent",
		},
		{
			name:     "custom agent file with path",
			input:    "../agents/technical-doc-writer.agent.md",
			expected: "technical-doc-writer",
		},
		{
			name:     "custom agent file with underscores",
			input:    ".github/agents/my_custom_agent.agent.md",
			expected: "my_custom_agent",
		},
		{
			name:     "agent file with only .agent extension",
			input:    ".github/agents/test-agent.agent",
			expected: "test-agent",
		},
		{
			name:     "agent file with .agent extension in path",
			input:    "../agents/my-agent.agent",
			expected: "my-agent",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExtractAgentIdentifier(tt.input)
			if result != tt.expected {
				t.Errorf("ExtractAgentIdentifier(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

// TestShellVariableExpansionInAgentPath tests that agent paths allow shell variable expansion
func TestShellVariableExpansionInAgentPath(t *testing.T) {
	agentFile := ".github/agents/test-agent.md"
	result := ResolveAgentFilePath(agentFile)

	// The result should be fully wrapped in double quotes (not single quotes)
	// Format: "${GITHUB_WORKSPACE}/.github/agents/test-agent.md"
	expected := "\"${GITHUB_WORKSPACE}/.github/agents/test-agent.md\""

	if result != expected {
		t.Errorf("ResolveAgentFilePath(%q) = %q, want %q", agentFile, result, expected)
	}

	// Verify it's properly quoted for shell variable expansion
	// Should start with double quote (not single quote)
	if !strings.HasPrefix(result, "\"") {
		t.Errorf("Agent path should start with double quote for variable expansion, got: %s", result)
	}

	// Should end with double quote (not single quote)
	if !strings.HasSuffix(result, "\"") {
		t.Errorf("Agent path should end with double quote for variable expansion, got: %s", result)
	}

	// Should NOT contain single quotes around the double-quoted section
	// Old broken format was: '"${GITHUB_WORKSPACE}"/.github/agents/test.md'
	if strings.Contains(result, "'\"") || strings.Contains(result, "\"'") {
		t.Errorf("Agent path should not mix single and double quotes, got: %s", result)
	}

	// Should contain the variable placeholder without internal quotes
	// Correct: "${GITHUB_WORKSPACE}/path"
	// Incorrect: "${GITHUB_WORKSPACE}"/path
	if strings.Contains(result, "\"/") && !strings.HasSuffix(result, "\"/\"") {
		t.Errorf("Variable should be inside the double quotes with path, got: %s", result)
	}
}

// TestShellEscapeArgWithFullyQuotedAgentPath tests that fully quoted agent paths are not re-escaped
func TestShellEscapeArgWithFullyQuotedAgentPath(t *testing.T) {
	// This simulates what happens when ResolveAgentFilePath output goes through shellEscapeArg
	agentPath := "\"${GITHUB_WORKSPACE}/.github/agents/test-agent.md\""

	result := shellEscapeArg(agentPath)

	// Should be left as-is because it's already fully double-quoted
	if result != agentPath {
		t.Errorf("shellEscapeArg should leave fully quoted path as-is, got: %s, want: %s", result, agentPath)
	}

	// Should NOT wrap it in additional single quotes
	if strings.HasPrefix(result, "'") {
		t.Errorf("shellEscapeArg should not add single quotes to already double-quoted string, got: %s", result)
	}
}

// TestGetHostedToolcachePathSetup tests the hostedtoolcache PATH setup helper
func TestGetHostedToolcachePathSetup(t *testing.T) {
	pathSetup := GetHostedToolcachePathSetup()

	// Should use find command to locate bin directories in hostedtoolcache
	if !strings.Contains(pathSetup, "/opt/hostedtoolcache") {
		t.Errorf("PATH setup should reference /opt/hostedtoolcache, got: %s", pathSetup)
	}

	// Should look for bin directories
	if !strings.Contains(pathSetup, "-name bin") {
		t.Errorf("PATH setup should search for bin directories, got: %s", pathSetup)
	}

	// Should use maxdepth 4 to reach /opt/hostedtoolcache/<tool>/<version>/<arch>/bin
	if !strings.Contains(pathSetup, "-maxdepth 4") {
		t.Errorf("PATH setup should use -maxdepth 4, got: %s", pathSetup)
	}

	// Should suppress errors with 2>/dev/null
	if !strings.Contains(pathSetup, "2>/dev/null") {
		t.Errorf("PATH setup should suppress errors with 2>/dev/null, got: %s", pathSetup)
	}

	// Should source the sanitize_path.sh script
	if !strings.Contains(pathSetup, "source /opt/gh-aw/actions/sanitize_path.sh") {
		t.Errorf("PATH setup should source sanitize_path.sh script, got: %s", pathSetup)
	}

	// Should preserve existing PATH by including $PATH in the raw path
	if !strings.Contains(pathSetup, "$PATH") {
		t.Errorf("PATH setup should include $PATH to preserve existing PATH, got: %s", pathSetup)
	}
}

// TestGetHostedToolcachePathSetup_Consistency verifies the PATH setup produces consistent output
func TestGetHostedToolcachePathSetup_Consistency(t *testing.T) {
	// Call multiple times to ensure consistent output
	first := GetHostedToolcachePathSetup()
	second := GetHostedToolcachePathSetup()

	if first != second {
		t.Errorf("GetHostedToolcachePathSetup should return consistent results, got:\n%s\nvs:\n%s", first, second)
	}
}

// TestGetHostedToolcachePathSetup_UsesToolBins verifies that GetHostedToolcachePathSetup
// uses $GH_AW_TOOL_BINS to get specific tool paths computed by GetToolBinsSetup.
func TestGetHostedToolcachePathSetup_UsesToolBins(t *testing.T) {
	pathSetup := GetHostedToolcachePathSetup()

	// Should use $GH_AW_TOOL_BINS for specific tool paths
	if !strings.Contains(pathSetup, "$GH_AW_TOOL_BINS") {
		t.Errorf("PATH setup should use $GH_AW_TOOL_BINS, got: %s", pathSetup)
	}

	// Verify ordering: $GH_AW_TOOL_BINS should come BEFORE the find command
	toolBinsIdx := strings.Index(pathSetup, "$GH_AW_TOOL_BINS")
	findIdx := strings.Index(pathSetup, "find /opt/hostedtoolcache")
	if toolBinsIdx > findIdx {
		t.Errorf("$GH_AW_TOOL_BINS should come before find command in PATH setup, got: %s", pathSetup)
	}
}

// TestGetToolBinsSetup verifies that GetToolBinsSetup computes specific tool paths
// for all supported runtimes (Go, Java, Rust, Conda, Ruby, pipx, Swift, .NET).
func TestGetToolBinsSetup(t *testing.T) {
	toolBinsSetup := GetToolBinsSetup()

	// Should use `go env GOROOT` for Go (actions/setup-go doesn't export GOROOT env var)
	if !strings.Contains(toolBinsSetup, "command -v go") || !strings.Contains(toolBinsSetup, "$(go env GOROOT)/bin") {
		t.Errorf("GetToolBinsSetup should use `go env GOROOT` for Go, got: %s", toolBinsSetup)
	}

	// Should check JAVA_HOME for Java
	if !strings.Contains(toolBinsSetup, "JAVA_HOME") || !strings.Contains(toolBinsSetup, "$JAVA_HOME/bin") {
		t.Errorf("GetToolBinsSetup should handle JAVA_HOME, got: %s", toolBinsSetup)
	}

	// Should check CARGO_HOME for Rust
	if !strings.Contains(toolBinsSetup, "CARGO_HOME") || !strings.Contains(toolBinsSetup, "$CARGO_HOME/bin") {
		t.Errorf("GetToolBinsSetup should handle CARGO_HOME, got: %s", toolBinsSetup)
	}

	// Should check CONDA for Conda
	if !strings.Contains(toolBinsSetup, `"$CONDA"`) || !strings.Contains(toolBinsSetup, "$CONDA/bin") {
		t.Errorf("GetToolBinsSetup should handle CONDA, got: %s", toolBinsSetup)
	}

	// Should check GEM_HOME for Ruby
	if !strings.Contains(toolBinsSetup, "GEM_HOME") || !strings.Contains(toolBinsSetup, "$GEM_HOME/bin") {
		t.Errorf("GetToolBinsSetup should handle GEM_HOME, got: %s", toolBinsSetup)
	}

	// Should check PIPX_BIN_DIR for pipx (no /bin suffix)
	if !strings.Contains(toolBinsSetup, "PIPX_BIN_DIR") || !strings.Contains(toolBinsSetup, "$PIPX_BIN_DIR:") {
		t.Errorf("GetToolBinsSetup should handle PIPX_BIN_DIR, got: %s", toolBinsSetup)
	}

	// Should check SWIFT_PATH for Swift (no /bin suffix)
	if !strings.Contains(toolBinsSetup, "SWIFT_PATH") || !strings.Contains(toolBinsSetup, "$SWIFT_PATH:") {
		t.Errorf("GetToolBinsSetup should handle SWIFT_PATH, got: %s", toolBinsSetup)
	}

	// Should check DOTNET_ROOT for .NET (no /bin suffix)
	if !strings.Contains(toolBinsSetup, "DOTNET_ROOT") || !strings.Contains(toolBinsSetup, "$DOTNET_ROOT:") {
		t.Errorf("GetToolBinsSetup should handle DOTNET_ROOT, got: %s", toolBinsSetup)
	}

	// Should export GH_AW_TOOL_BINS
	if !strings.Contains(toolBinsSetup, "export GH_AW_TOOL_BINS") {
		t.Errorf("GetToolBinsSetup should export GH_AW_TOOL_BINS, got: %s", toolBinsSetup)
	}
}

// TestGetToolBinsEnvArg verifies that GetToolBinsEnvArg returns the correct AWF argument.
func TestGetToolBinsEnvArg(t *testing.T) {
	envArg := GetToolBinsEnvArg()

	if len(envArg) != 2 {
		t.Errorf("GetToolBinsEnvArg should return 2 elements (--env and value), got: %d", len(envArg))
	}

	if envArg[0] != "--env" {
		t.Errorf("First element should be --env, got: %s", envArg[0])
	}

	if envArg[1] != "\"GH_AW_TOOL_BINS=$GH_AW_TOOL_BINS\"" {
		t.Errorf("Second element should be \"GH_AW_TOOL_BINS=$GH_AW_TOOL_BINS\" (with outer double quotes), got: %s", envArg[1])
	}
}

// TestGetSanitizedPATHExport verifies that GetSanitizedPATHExport produces correct shell commands.
func TestGetSanitizedPATHExport(t *testing.T) {
	result := GetSanitizedPATHExport("/usr/bin:/usr/local/bin")

	// Should source the sanitize_path.sh script from /opt/gh-aw/actions/
	if !strings.Contains(result, "source /opt/gh-aw/actions/sanitize_path.sh") {
		t.Errorf("GetSanitizedPATHExport should source sanitize_path.sh, got: %s", result)
	}

	// Should include the raw path as an argument
	if !strings.Contains(result, "/usr/bin:/usr/local/bin") {
		t.Errorf("GetSanitizedPATHExport should include the raw path, got: %s", result)
	}
}

// TestGetSanitizedPATHExport_ShellExecution tests that the sanitize_path.sh script
// correctly sanitizes various malformed PATH inputs when executed in a real shell.
// This test uses the script directly from actions/setup/sh/ since /opt/gh-aw/actions/
// only exists at runtime.
func TestGetSanitizedPATHExport_ShellExecution(t *testing.T) {
	// Get the path to the sanitize_path.sh script relative to this test file
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("Failed to get current file path")
	}
	// Navigate from pkg/workflow/ to actions/setup/sh/
	scriptPath := filepath.Join(filepath.Dir(thisFile), "..", "..", "actions", "setup", "sh", "sanitize_path.sh")
	if _, err := os.Stat(scriptPath); os.IsNotExist(err) {
		t.Fatalf("sanitize_path.sh script not found at %s", scriptPath)
	}

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "already clean PATH",
			input:    "/usr/bin:/usr/local/bin",
			expected: "/usr/bin:/usr/local/bin",
		},
		{
			name:     "leading colon",
			input:    ":/usr/bin:/usr/local/bin",
			expected: "/usr/bin:/usr/local/bin",
		},
		{
			name:     "trailing colon",
			input:    "/usr/bin:/usr/local/bin:",
			expected: "/usr/bin:/usr/local/bin",
		},
		{
			name:     "multiple leading colons",
			input:    ":::/usr/bin:/usr/local/bin",
			expected: "/usr/bin:/usr/local/bin",
		},
		{
			name:     "multiple trailing colons",
			input:    "/usr/bin:/usr/local/bin:::",
			expected: "/usr/bin:/usr/local/bin",
		},
		{
			name:     "internal empty elements",
			input:    "/usr/bin::/usr/local/bin",
			expected: "/usr/bin:/usr/local/bin",
		},
		{
			name:     "multiple internal empty elements",
			input:    "/usr/bin:::/usr/local/bin",
			expected: "/usr/bin:/usr/local/bin",
		},
		{
			name:     "combined leading trailing and internal",
			input:    ":/usr/bin:::/usr/local/bin:",
			expected: "/usr/bin:/usr/local/bin",
		},
		{
			name:     "all colons",
			input:    ":::",
			expected: "",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "single path no colons",
			input:    "/usr/bin",
			expected: "/usr/bin",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Source the script directly with the input and echo the resulting PATH
			shellCmd := fmt.Sprintf(`source '%s' '%s' && echo "$PATH"`, scriptPath, tt.input)

			cmd := exec.Command("bash", "-c", shellCmd)
			output, err := cmd.Output()
			if err != nil {
				t.Fatalf("Failed to execute shell command: %v\nCommand: %s", err, shellCmd)
			}

			result := strings.TrimSpace(string(output))
			if result != tt.expected {
				t.Errorf("Sanitized PATH = %q, want %q\nShell command: %s", result, tt.expected, shellCmd)
			}
		})
	}
}

func TestGetNpmBinPathSetup(t *testing.T) {
	pathSetup := GetNpmBinPathSetup()

	// Should use find command to locate bin directories in hostedtoolcache
	if !strings.Contains(pathSetup, "/opt/hostedtoolcache") {
		t.Errorf("PATH setup should reference /opt/hostedtoolcache, got: %s", pathSetup)
	}

	// Should search for bin directories
	if !strings.Contains(pathSetup, "-name bin") {
		t.Errorf("PATH setup should search for bin directories, got: %s", pathSetup)
	}

	// Should preserve existing PATH
	if !strings.Contains(pathSetup, "$PATH") {
		t.Errorf("PATH setup should include $PATH, got: %s", pathSetup)
	}

	// Should re-prepend GOROOT/bin after the find to preserve correct Go version ordering
	// (find returns alphabetically, so go/1.23 can shadow go/1.25)
	if !strings.Contains(pathSetup, "$GOROOT") {
		t.Errorf("PATH setup should re-prepend GOROOT/bin after find, got: %s", pathSetup)
	}

	// GOROOT re-prepend should come AFTER the find command
	findIdx := strings.Index(pathSetup, "find /opt/hostedtoolcache")
	gorootIdx := strings.Index(pathSetup, "$GOROOT")
	if gorootIdx < findIdx {
		t.Errorf("GOROOT re-prepend should come after find command, got: %s", pathSetup)
	}
}

// TestGetNpmBinPathSetup_GorootOrdering verifies that GOROOT/bin takes precedence
// over alphabetically-ordered Go versions in hostedtoolcache.
func TestGetNpmBinPathSetup_GorootOrdering(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Skipping shell-based test on non-Linux platform")
	}

	// Create a temporary hostedtoolcache structure with two Go versions
	tmpDir := t.TempDir()
	goOld := filepath.Join(tmpDir, "go", "1.23.12", "x64", "bin")
	goNew := filepath.Join(tmpDir, "go", "1.25.0", "x64", "bin")
	os.MkdirAll(goOld, 0o755)
	os.MkdirAll(goNew, 0o755)

	// Write fake go scripts: old reports 1.23, new reports 1.25
	os.WriteFile(filepath.Join(goOld, "go"), []byte("#!/bin/bash\necho 'go version go1.23.12 linux/amd64'\n"), 0o755)
	os.WriteFile(filepath.Join(goNew, "go"), []byte("#!/bin/bash\necho 'go version go1.25.0 linux/amd64'\n"), 0o755)

	// Simulate the PATH setup with GOROOT pointing to the newer version
	shellCmd := fmt.Sprintf(
		`export GOROOT=%q; export PATH="$(find %q -maxdepth 4 -type d -name bin 2>/dev/null | tr '\n' ':')$PATH"; [ -n "$GOROOT" ] && export PATH="$GOROOT/bin:$PATH" || true; go version`,
		filepath.Join(tmpDir, "go", "1.25.0", "x64"),
		tmpDir,
	)

	cmd := exec.Command("bash", "-c", shellCmd)
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("Failed to execute shell command: %v", err)
	}

	result := strings.TrimSpace(string(output))
	if !strings.Contains(result, "go1.25.0") {
		t.Errorf("Expected go1.25.0 to take precedence, but got: %s", result)
	}
}

// TestGetNpmBinPathSetup_NoGorootDoesNotBreakChain verifies that when GOROOT is
// not set, the command chain continues (the || true prevents short-circuit).
func TestGetNpmBinPathSetup_NoGorootDoesNotBreakChain(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Skipping shell-based test on non-Linux platform")
	}

	// The full command pattern used by engines:
	//   GetNpmBinPathSetup() && INSTRUCTION="..." && codex exec ...
	// When GOROOT is empty, [ -n "$GOROOT" ] is false. Without || true,
	// the && chain short-circuits and INSTRUCTION is never set.
	shellCmd := `unset GOROOT; export PATH="$(find /opt/hostedtoolcache -maxdepth 4 -type d -name bin 2>/dev/null | tr '\n' ':')$PATH"; [ -n "$GOROOT" ] && export PATH="$GOROOT/bin:$PATH" || true && echo "chain-continued"`

	cmd := exec.Command("bash", "-c", shellCmd)
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("Command chain should not fail when GOROOT is empty: %v", err)
	}

	result := strings.TrimSpace(string(output))
	if !strings.Contains(result, "chain-continued") {
		t.Errorf("Expected command chain to continue when GOROOT is empty, got: %q", result)
	}
}
