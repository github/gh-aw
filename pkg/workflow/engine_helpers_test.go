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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveEngineID(t *testing.T) {
	tests := []struct {
		name         string
		workflowData *WorkflowData
		want         string
	}{
		{
			name:         "nil workflow data",
			workflowData: nil,
			want:         "",
		},
		{
			name: "engine config id",
			workflowData: &WorkflowData{
				EngineConfig: &EngineConfig{ID: "copilot"},
				AI:           "claude",
			},
			want: "copilot",
		},
		{
			name: "fallback to ai",
			workflowData: &WorkflowData{
				AI: "claude",
			},
			want: "claude",
		},
		{
			name:         "empty when unavailable",
			workflowData: &WorkflowData{},
			want:         "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, ResolveEngineID(tt.workflowData))
		})
	}
}

func TestApplyEngineHarnessRetryEnv(t *testing.T) {
	t.Run("injects harness policy env vars including watchdog timeout", func(t *testing.T) {
		env := map[string]string{}
		workflowData := &WorkflowData{
			EngineConfig: &EngineConfig{
				HarnessMaxRetries:        "6",
				HarnessInitialDelayMs:    "10000",
				HarnessBackoffMultiplier: "2",
				HarnessMaxDelayMs:        "180000",
				HarnessWatchdogTimeoutMs: "120000",
			},
		}

		applyEngineHarnessRetryEnv(env, workflowData)

		assert.Equal(t, "6", env["GH_AW_HARNESS_MAX_RETRIES"])
		assert.Equal(t, "10000", env["GH_AW_HARNESS_INITIAL_DELAY_MS"])
		assert.Equal(t, "2", env["GH_AW_HARNESS_BACKOFF_MULTIPLIER"])
		assert.Equal(t, "180000", env["GH_AW_HARNESS_MAX_DELAY_MS"])
		assert.Equal(t, "120000", env["GH_AW_HARNESS_WATCHDOG_TIMEOUT_MS"])
	})

	t.Run("engine.env override still wins after harness policy env injection", func(t *testing.T) {
		env := map[string]string{}
		workflowData := &WorkflowData{
			EngineConfig: &EngineConfig{
				HarnessWatchdogTimeoutMs: "120000",
				Env: map[string]string{
					"GH_AW_HARNESS_WATCHDOG_TIMEOUT_MS": "90000",
				},
			},
		}

		applyEngineHarnessRetryEnv(env, workflowData)
		applyEngineAndAgentEnv(env, workflowData, nil)

		assert.Equal(t, "90000", env["GH_AW_HARNESS_WATCHDOG_TIMEOUT_MS"])
	})
}

func TestBuildStandardNpmEngineInstallStepsNoCooldown(t *testing.T) {
	steps := BuildStandardNpmEngineInstallStepsNoCooldown(
		"@github/copilot",
		string(constants.DefaultCopilotVersion),
		"Install GitHub Copilot CLI",
		"copilot",
		&WorkflowData{},
	)

	var content strings.Builder
	for _, step := range steps {
		content.WriteString(strings.Join(step, "\n"))
		content.WriteString("\n")
	}
	if strings.Contains(content.String(), "NPM_CONFIG_MIN_RELEASE_AGE") {
		t.Fatalf("expected npm cooldown env to be omitted when no-cooldown helper is used")
	}
}

func TestShellEscapeArgWithFullyQuotedAgentPath(t *testing.T) {
	// After the bypass removal, a double-quoted string is treated as any other argument
	// containing special characters and gets properly single-quoted.
	agentPath := "\"${GITHUB_WORKSPACE}/.github/agents/test-agent.md\""

	result := shellEscapeArg(agentPath)

	// Should be single-quoted (the bypass was removed for security)
	if !strings.HasPrefix(result, "'") {
		t.Errorf("shellEscapeArg should single-quote the agent path after bypass removal, got: %s", result)
	}

	// Should NOT be left as-is (that was the vulnerable bypass behavior)
	if result == agentPath {
		t.Errorf("shellEscapeArg should not leave double-quoted agent path unchanged (bypass removed), got: %s", result)
	}
}

func TestGetNpmBinPathSetup(t *testing.T) {
	pathSetup := GetNpmBinPathSetup()

	// Should require RUNNER_TOOL_CACHE instead of guessing fallback paths.
	if !strings.Contains(pathSetup, "RUNNER_TOOL_CACHE must be set") {
		t.Errorf("PATH setup should require RUNNER_TOOL_CACHE, got: %s", pathSetup)
	}
	if strings.Contains(pathSetup, ":-/opt/hostedtoolcache") {
		t.Errorf("PATH setup should not fall back to /opt/hostedtoolcache, got: %s", pathSetup)
	}
	if strings.Contains(pathSetup, "/home/runner/work/_tool") {
		t.Errorf("PATH setup should not hard-code /home/runner/work/_tool, got: %s", pathSetup)
	}

	// Should search for bin directories
	if !strings.Contains(pathSetup, "-name bin") {
		t.Errorf("PATH setup should search for bin directories, got: %s", pathSetup)
	}
	if !strings.Contains(pathSetup, "-maxdepth 5") {
		t.Errorf("PATH setup should search deep enough for setup-node toolcache bins, got: %s", pathSetup)
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
	findIdx := strings.Index(pathSetup, `find "$GH_AW_TOOL_CACHE"`)
	gorootIdx := strings.Index(pathSetup, "$GOROOT")
	if findIdx < 0 {
		t.Errorf("PATH setup should search GH_AW_TOOL_CACHE, got: %s", pathSetup)
	}
	if gorootIdx < findIdx {
		t.Errorf("GOROOT re-prepend should come after find command, got: %s", pathSetup)
	}

	// Should re-prepend ERLANG_HOME/bin so erl is reachable when erlef/setup-beam is used.
	// setup-beam installs OTP to ${RUNNER_TEMP}/.setup-beam/otp/ which is NOT under
	// RUNNER_TOOL_CACHE, so the find above would miss it without this explicit re-prepend.
	if !strings.Contains(pathSetup, "$ERLANG_HOME") {
		t.Errorf("PATH setup should re-prepend ERLANG_HOME/bin for Elixir/OTP support, got: %s", pathSetup)
	}

	// ERLANG_HOME re-prepend should come AFTER the find command
	erlangIdx := strings.Index(pathSetup, "$ERLANG_HOME")
	if erlangIdx < findIdx {
		t.Errorf("ERLANG_HOME re-prepend should come after find command, got: %s", pathSetup)
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
		`export GOROOT=%q; export PATH="$(find %q -maxdepth 5 -type d -name bin 2>/dev/null | tr '\n' ':')$PATH"; [ -n "$GOROOT" ] && export PATH="$GOROOT/bin:$PATH" || true; go version`,
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

func TestGetNpmBinPathSetup_PreservesSelectedRuby(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Skipping shell-based test on non-Linux platform")
	}

	cacheRoot := filepath.Join(t.TempDir(), "toolcache")
	selectedRubyBin := filepath.Join(t.TempDir(), "selected-ruby", "bin")
	cachedRubyBin := filepath.Join(cacheRoot, "Ruby", "3.2.11", "x64", "bin")
	require.NoError(t, os.MkdirAll(selectedRubyBin, 0o755))
	require.NoError(t, os.MkdirAll(cachedRubyBin, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(selectedRubyBin, "ruby"), []byte("#!/bin/sh\necho 'ruby 3.4.8'\n"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(cachedRubyBin, "ruby"), []byte("#!/bin/sh\necho 'ruby 3.2.11'\n"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(cachedRubyBin, "npm-agent"), []byte("#!/bin/sh\necho 'npm agent found'\n"), 0o755))

	shellCmd := fmt.Sprintf(
		`unset GOROOT ERLANG_HOME; export RUNNER_TOOL_CACHE=%q; export PATH=%q:/usr/bin:/bin; %s; ruby --version; npm-agent`,
		cacheRoot,
		selectedRubyBin,
		GetNpmBinPathSetup(),
	)

	output, err := exec.Command("bash", "-c", shellCmd).Output()
	if err != nil {
		t.Fatalf("Failed to execute shell command: %v", err)
	}
	assert.Equal(t, "ruby 3.4.8\nnpm agent found\n", string(output))
}

func TestDockerSudoIptablesPreservesSelectedRubyPath(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Skipping shell-based test on non-Linux platform")
	}

	root := t.TempDir()
	cacheRoot := filepath.Join(root, "toolcache")
	selectedRubyBin := filepath.Join(root, "selected-ruby", "bin")
	cachedRubyBin := filepath.Join(cacheRoot, "Ruby", "3.2.11", "x64", "bin")
	secureBin := filepath.Join(root, "secure-bin")
	commandBin := filepath.Join(root, "command-bin")
	for _, dir := range []string{selectedRubyBin, cachedRubyBin, secureBin, commandBin} {
		require.NoError(t, os.MkdirAll(dir, 0o755))
	}

	require.NoError(t, os.WriteFile(filepath.Join(selectedRubyBin, "ruby"), []byte("#!/bin/sh\necho 'ruby 3.4.8'\n"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(cachedRubyBin, "ruby"), []byte("#!/bin/sh\necho 'ruby 3.2.11'\n"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(cachedRubyBin, "npm-agent"), []byte("#!/bin/sh\necho 'npm agent found'\n"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(secureBin, "ruby"), []byte("#!/bin/sh\necho 'ruby 3.2.3'\n"), 0o755))

	fakeAWF := filepath.Join(secureBin, "awf")
	awfScript := fmt.Sprintf("#!/bin/bash\nset -e\nunset GOROOT ERLANG_HOME\n%s\nruby --version\nnpm-agent\n", GetNpmBinPathSetup())
	require.NoError(t, os.WriteFile(fakeAWF, []byte(awfScript), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(commandBin, "sudo"), []byte(`#!/bin/sh
if [ "$1" = "-E" ]; then
  shift
fi
export PATH="$GH_AW_TEST_SECURE_PATH"
exec "$@"
`), 0o755))

	workflowData := &WorkflowData{
		SandboxConfig: &SandboxConfig{
			Agent: &AgentSandboxConfig{Runtime: AgentRuntimeDockerSudoIptables},
		},
	}
	awfCommand := strings.Replace(GetAWFCommandPrefix(workflowData), "/usr/local/bin/awf", fakeAWF, 1)
	hostPath := strings.Join([]string{selectedRubyBin, commandBin, "/usr/bin", "/bin"}, ":")
	securePath := strings.Join([]string{secureBin, "/usr/bin", "/bin"}, ":")
	shellCmd := fmt.Sprintf(
		`export PATH=%q GH_AW_TEST_SECURE_PATH=%q RUNNER_TOOL_CACHE=%q; %s`,
		hostPath,
		securePath,
		cacheRoot,
		awfCommand,
	)

	output, err := exec.Command("/bin/bash", "-c", shellCmd).CombinedOutput()
	require.NoError(t, err, "privileged AWF command failed: %s", output)
	assert.Equal(t, "ruby 3.4.8\nnpm agent found\n", string(output))
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
	shellCmd := fmt.Sprintf(`unset GOROOT; export RUNNER_TOOL_CACHE=%q; %s && echo "chain-continued"`, t.TempDir(), GetNpmBinPathSetup())

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

// TestGetNpmBinPathSetup_ErlangHomeOrdering verifies that ERLANG_HOME/bin takes precedence
// over any Erlang/OTP bins that might appear in RUNNER_TOOL_CACHE, ensuring that the erl
// binary installed by erlef/setup-beam (in ${RUNNER_TEMP}/.setup-beam/otp/) is found.
func TestGetNpmBinPathSetup_ErlangHomeOrdering(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Skipping shell-based test on non-Linux platform")
	}

	// Simulate ERLANG_HOME pointing to a fake OTP installation
	tmpDir := t.TempDir()
	erlBinDir := filepath.Join(tmpDir, "otp", "bin")
	os.MkdirAll(erlBinDir, 0o755)

	// Write a fake erl script that reports its version
	os.WriteFile(filepath.Join(erlBinDir, "erl"), []byte("#!/bin/bash\necho 'fake-otp-27'\n"), 0o755)

	erlangHome := filepath.Join(tmpDir, "otp")

	// Simulate the PATH setup with ERLANG_HOME set (as erlef/setup-beam would do)
	shellCmd := fmt.Sprintf(
		`export ERLANG_HOME=%q; export RUNNER_TOOL_CACHE=%q; %s && erl`,
		erlangHome,
		t.TempDir(),
		GetNpmBinPathSetup(),
	)

	cmd := exec.Command("bash", "-c", shellCmd)
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("Failed to execute shell command: %v", err)
	}

	result := strings.TrimSpace(string(output))
	if !strings.Contains(result, "fake-otp-27") {
		t.Errorf("Expected ERLANG_HOME/bin/erl to be in PATH, but got: %s", result)
	}
}

// TestGetNpmBinPathSetup_NoErlangHomeDoesNotBreakChain verifies that when ERLANG_HOME is
// not set (non-Elixir workflows), the command chain continues.
func TestGetNpmBinPathSetup_NoErlangHomeDoesNotBreakChain(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Skipping shell-based test on non-Linux platform")
	}

	// When ERLANG_HOME is empty, [ -n "$ERLANG_HOME" ] is false. Without || true,
	// the && chain short-circuits and subsequent commands are never run.
	shellCmd := fmt.Sprintf(`unset ERLANG_HOME; export RUNNER_TOOL_CACHE=%q; %s && echo "chain-continued"`, t.TempDir(), GetNpmBinPathSetup())

	cmd := exec.Command("bash", "-c", shellCmd)
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("Command chain should not fail when ERLANG_HOME is empty: %v", err)
	}

	result := strings.TrimSpace(string(output))
	if !strings.Contains(result, "chain-continued") {
		t.Errorf("Expected command chain to continue when ERLANG_HOME is empty, got: %q", result)
	}
}

func TestYamlStringValue(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "plain string unchanged",
			input:    "hello world",
			expected: "hello world",
		},
		{
			name:     "empty string unchanged",
			input:    "",
			expected: "",
		},
		{
			name:     "github actions expression unchanged",
			input:    "${{ secrets.TOKEN }}",
			expected: "${{ secrets.TOKEN }}",
		},
		{
			name:     "json object gets single-quoted",
			input:    `{"key":"value"}`,
			expected: `'{"key":"value"}'`,
		},
		{
			name:     "json array gets single-quoted",
			input:    `["a","b"]`,
			expected: `'["a","b"]'`,
		},
		{
			name:     "json object with embedded single quote gets escaped",
			input:    `{"key":"it's"}`,
			expected: `'{"key":"it''s"}'`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := yamlStringValue(tt.input)
			if result != tt.expected {
				t.Errorf("yamlStringValue(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestFormatStepWithCommandAndEnvYAMLSafe(t *testing.T) {
	t.Run("json env var is single-quoted for valid YAML", func(t *testing.T) {
		stepLines := []string{"      - name: Test step"}
		env := map[string]string{
			"MY_JSON": `{"key":"value","nested":{"a":1}}`,
		}
		result := FormatStepWithCommandAndEnv(stepLines, "echo test", env)
		output := strings.Join(result, "\n")

		// The JSON value must be single-quoted so YAML treats it as a string
		if !strings.Contains(output, `MY_JSON: '{"key":"value","nested":{"a":1}}'`) {
			t.Errorf("Expected single-quoted JSON in env, got:\n%s", output)
		}
	})

	t.Run("github expression env var is not quoted", func(t *testing.T) {
		stepLines := []string{"      - name: Test step"}
		env := map[string]string{
			"MY_TOKEN": "${{ secrets.TOKEN }}",
		}
		result := FormatStepWithCommandAndEnv(stepLines, "echo test", env)
		output := strings.Join(result, "\n")

		// GitHub Actions expressions should not be wrapped in extra quotes
		if !strings.Contains(output, "MY_TOKEN: ${{ secrets.TOKEN }}") {
			t.Errorf("Expected unquoted github expression in env, got:\n%s", output)
		}
	})

	t.Run("multi-line env var emitted as literal block scalar", func(t *testing.T) {
		stepLines := []string{"      - name: Test step"}
		// Continuation lines have 4-space leading whitespace (as produced by goccy/go-yaml
		// when parsing a >- block scalar with extra-indented continuation lines).
		multiLineValue := "${{ secrets.PAT_1 != '' && secrets.PAT_1 ||\n    secrets.PAT_2 != '' && secrets.PAT_2 ||\n    secrets.PAT_3 }}"
		env := map[string]string{
			"COPILOT_GITHUB_TOKEN": multiLineValue,
		}
		result := FormatStepWithCommandAndEnv(stepLines, "echo test", env)
		output := strings.Join(result, "\n")

		// Multi-line value must be emitted as a literal block scalar
		if !strings.Contains(output, "          COPILOT_GITHUB_TOKEN: |") {
			t.Errorf("Expected literal block scalar indicator, got:\n%s", output)
		}
		if !strings.Contains(output, "            ${{ secrets.PAT_1 != '' && secrets.PAT_1 ||") {
			t.Errorf("Expected first line of multi-line value, got:\n%s", output)
		}
		// Continuation lines have 4-space prefix preserved, so total indentation is 12+4=16 spaces.
		if !strings.Contains(output, "                secrets.PAT_3 }}") {
			t.Errorf("Expected last line of multi-line value with preserved continuation indentation (16 spaces), got:\n%s", output)
		}
	})

	t.Run("trailing newline in env var is trimmed", func(t *testing.T) {
		stepLines := []string{"      - name: Test step"}
		env := map[string]string{
			"MY_TOKEN": "${{ secrets.TOKEN }}\n",
		}
		result := FormatStepWithCommandAndEnv(stepLines, "echo test", env)
		output := strings.Join(result, "\n")

		// Trailing newline should be trimmed; value should be emitted inline
		if !strings.Contains(output, "MY_TOKEN: ${{ secrets.TOKEN }}") {
			t.Errorf("Expected trailing newline to be trimmed, got:\n%s", output)
		}
		// Should not emit a block scalar for a value that only had a trailing newline
		if strings.Contains(output, "MY_TOKEN: |") {
			t.Errorf("Expected inline emission (not block scalar) after trimming trailing newline, got:\n%s", output)
		}
	})
}

func TestAppendEnvVarLine(t *testing.T) {
	tests := []struct {
		name            string
		key             string
		value           string
		expectedContent []string
		notExpected     []string
	}{
		{
			name:  "single-line value emitted inline",
			key:   "MY_VAR",
			value: "simple value",
			expectedContent: []string{
				"          MY_VAR: simple value",
			},
		},
		{
			name:  "github expression emitted inline without extra quoting",
			key:   "TOKEN",
			value: "${{ secrets.TOKEN }}",
			expectedContent: []string{
				"          TOKEN: ${{ secrets.TOKEN }}",
			},
		},
		{
			name:  "json value gets single-quoted inline",
			key:   "CONFIG",
			value: `{"key":"value"}`,
			expectedContent: []string{
				`          CONFIG: '{"key":"value"}'`,
			},
		},
		{
			name: "multi-line value emitted as literal block scalar",
			key:  "COPILOT_GITHUB_TOKEN",
			// Continuation lines have 4-space leading whitespace (as produced by goccy/go-yaml
			// when parsing a >- block scalar with extra-indented continuation lines).
			value: "${{ secrets.PAT_1 != '' && secrets.PAT_1 ||\n    secrets.PAT_2 }}",
			expectedContent: []string{
				"          COPILOT_GITHUB_TOKEN: |",
				"            ${{ secrets.PAT_1 != '' && secrets.PAT_1 ||",
				// Continuation line has 4-space prefix preserved: 12 base + 4 continuation = 16 spaces total.
				"                secrets.PAT_2 }}",
			},
			notExpected: []string{
				"COPILOT_GITHUB_TOKEN: ${{ secrets.PAT_1",
			},
		},
		{
			name:  "trailing newline is trimmed before deciding inline vs block",
			key:   "TRIMMED",
			value: "${{ secrets.TOKEN }}\n",
			expectedContent: []string{
				"          TRIMMED: ${{ secrets.TOKEN }}",
			},
			notExpected: []string{
				"TRIMMED: |",
			},
		},
		{
			name:  "only one trailing newline is trimmed (not multiple)",
			key:   "MULTI_NEWLINE",
			value: "line one\nline two\n\n",
			expectedContent: []string{
				"          MULTI_NEWLINE: |",
				"            line one",
				"            line two",
				// The second trailing newline becomes an empty line in the block scalar.
				"            ",
			},
		},
		{
			name:  "multi-line value with trailing newline trimmed",
			key:   "MULTI",
			value: "line one\nline two\n",
			expectedContent: []string{
				"          MULTI: |",
				"            line one",
				"            line two",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := appendEnvVarLine([]string{}, tt.key, tt.value)
			output := strings.Join(result, "\n")

			for _, expected := range tt.expectedContent {
				if !strings.Contains(output, expected) {
					t.Errorf("Expected result to contain %q\nGot:\n%s", expected, output)
				}
			}
			for _, notExp := range tt.notExpected {
				if strings.Contains(output, notExp) {
					t.Errorf("Expected result NOT to contain %q\nGot:\n%s", notExp, output)
				}
			}
		})
	}
}

func TestNormalizeBashCommand(t *testing.T) {
	tests := []struct {
		name            string
		input           string
		expectedCmd     string
		expectedChanged bool
	}{
		{
			name:            "plain command unchanged",
			input:           "jq",
			expectedCmd:     "jq",
			expectedChanged: false,
		},
		{
			name:            "command with space-star suffix is stripped",
			input:           "jq *",
			expectedCmd:     "jq",
			expectedChanged: true,
		},
		{
			name:            "multi-word command with space-star suffix is stripped",
			input:           "gh issue list *",
			expectedCmd:     "gh issue list",
			expectedChanged: true,
		},
		{
			name:            "command with arguments but no wildcard unchanged",
			input:           "jq . /tmp/file.json",
			expectedCmd:     "jq . /tmp/file.json",
			expectedChanged: false,
		},
		{
			name:            "lone star is not stripped (handled as full-wildcard sentinel)",
			input:           "*",
			expectedCmd:     "*",
			expectedChanged: false,
		},
		{
			name:            "colon-star is not stripped (handled as full-wildcard sentinel)",
			input:           ":*",
			expectedCmd:     ":*",
			expectedChanged: false,
		},
		{
			name:            "sed with space-star suffix is stripped",
			input:           "sed *",
			expectedCmd:     "sed",
			expectedChanged: true,
		},
		{
			name:            "awk with space-star suffix is stripped",
			input:           "awk *",
			expectedCmd:     "awk",
			expectedChanged: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotCmd, gotChanged := normalizeBashCommand(tt.input)
			if gotCmd != tt.expectedCmd {
				t.Errorf("normalizeBashCommand(%q) cmd = %q, want %q", tt.input, gotCmd, tt.expectedCmd)
			}
			if gotChanged != tt.expectedChanged {
				t.Errorf("normalizeBashCommand(%q) changed = %v, want %v", tt.input, gotChanged, tt.expectedChanged)
			}
		})
	}
}

func TestApplyEngineVersionEnv(t *testing.T) {
	tests := []struct {
		name         string
		workflowData *WorkflowData
		wantVersion  string
		wantSet      bool
	}{
		{
			name:         "nil workflow data",
			workflowData: nil,
			wantSet:      false,
		},
		{
			name:         "nil engine config",
			workflowData: &WorkflowData{},
			wantSet:      false,
		},
		{
			name: "empty version",
			workflowData: &WorkflowData{
				EngineConfig: &EngineConfig{ID: "goose"},
			},
			wantSet: false,
		},
		{
			name: "explicit version string",
			workflowData: &WorkflowData{
				EngineConfig: &EngineConfig{ID: "goose", Version: "1.0.0"},
			},
			wantVersion: "1.0.0",
			wantSet:     true,
		},
		{
			name: "expression version",
			workflowData: &WorkflowData{
				EngineConfig: &EngineConfig{ID: "goose", Version: "${{ inputs.engine-version }}"},
			},
			wantVersion: "${{ inputs.engine-version }}",
			wantSet:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := map[string]string{}
			applyEngineVersionEnv(env, tt.workflowData)
			got, set := env["GH_AW_ENGINE_VERSION"]
			if set != tt.wantSet {
				t.Errorf("expected GH_AW_ENGINE_VERSION to be set=%v, got set=%v", tt.wantSet, set)
			}
			if tt.wantSet && got != tt.wantVersion {
				t.Errorf("expected GH_AW_ENGINE_VERSION=%q, got %q", tt.wantVersion, got)
			}
		})
	}
}
