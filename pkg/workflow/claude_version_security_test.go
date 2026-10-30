//go:build !integration

package workflow

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompileBlocksClaudeVersion(t *testing.T) {
	for _, tt := range []struct {
		name   string
		config string
		field  string
	}{
		{"main engine", "engine:\n  id: claude\n  version: 2.1.313\n", "engine.version"},
		{"threat detection", "engine: copilot\nsafe-outputs:\n  create-issue:\n  threat-detection:\n    engine:\n      id: claude\n      version: 2.1.313\n", "safe-outputs.threat-detection.engine.version"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "blocked.md")
			require.NoError(t, os.WriteFile(path, []byte("---\non: workflow_dispatch\nstrict: false\n"+tt.config+"---\nInspect the repository.\n"), 0o644))
			err := NewCompiler(WithSkipValidation(true)).CompileWorkflow(path)
			require.ErrorContains(t, err, "Claude Code 2.1.313 is blocked")
			assert.Contains(t, err.Error(), tt.field)
			_, err = os.Stat(strings.TrimSuffix(path, ".md") + ".lock.yml")
			assert.True(t, os.IsNotExist(err), "blocked version must not produce an executable workflow")
		})
	}
}

func TestClaudeVersionGuardInstallation(t *testing.T) {
	for _, version := range []string{"2.1.313", "v2.1.313", "02.1.313", "latest", "^2.1.312", "*", ">=2.1.312", "${{ inputs.version }}"} {
		t.Run(version, func(t *testing.T) {
			steps := GenerateNpmInstallSteps("@anthropic-ai/claude-code", version, "Install Claude", "claude",
				NPMInstallOptions{RunInstallScripts: true})
			script := strings.Join(steps[0], "\n")
			assert.Contains(t, script, claudeNpmVersionGuard)
			assert.Contains(t, script, `npm install -g @anthropic-ai/claude-code@"${ENGINE_VERSION}"`)
			assert.Less(t, strings.Index(script, "npm view"), strings.Index(script, "npm install"))
			envVersion := version
			if !ExpressionPattern.MatchString(version) {
				envVersion = strconv.Quote(version)
			}
			assert.Contains(t, script, "ENGINE_VERSION: "+envVersion)
			var parsed []struct {
				Run string
				Env map[string]string
			}
			require.NoError(t, yaml.Unmarshal([]byte(script), &parsed))
			require.Len(t, parsed, 1)
			assert.Equal(t, version, parsed[0].Env["ENGINE_VERSION"])

			script = strings.Join(GenerateMicroVMNpmCLIInstallStep("@anthropic-ai/claude-code", version, "Install Claude", "claude", true, false), "\n")
			assert.Contains(t, script, claudeNpmVersionGuard)
			assert.Less(t, strings.Index(script, "npm view"), strings.Index(script, "npm install"))
		})
	}
	require.False(t, needsClaudeNpmVersionGuard("@anthropic-ai/claude-code", string(constants.DefaultClaudeCodeVersion)))
	require.False(t, needsClaudeNpmVersionGuard("@openai/codex", "latest"))
}

func TestClaudeVersionGuardRejectsBeforeInstall(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is required to exercise the generated version guard")
	}
	for _, tt := range []struct {
		name     string
		resolved string
		want     string
		blocked  bool
	}{
		{"safe release", `"2.1.312"`, "2.1.312", false},
		{"blocked release", `"2.1.313"`, "", true},
		{"range selects blocked release", `["2.1.312","2.1.313"]`, "", true},
		{"range selects unaffected release", `["2.1.312","2.1.313","2.1.314"]`, "2.1.314", false},
		{"blocked build metadata", `"2.1.313+build"`, "", true},
		{"invalid registry result", `{}`, "", true},
		{"empty registry result", `[]`, "", true},
		{"malformed registry result", `not json`, "", true},
		{"shell injection", `"$(touch /tmp/should-not-exist)"`, "", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			script := "npm() { printf '%s' \"$MOCK_NPM_VERSION\"; }\n" + claudeNpmVersionGuard + "\nprintf '%s' \"$ENGINE_VERSION\""
			cmd := exec.Command("bash", "-e", "-c", script)
			cmd.Env = append(os.Environ(), "ENGINE_VERSION=latest", "MOCK_NPM_VERSION="+tt.resolved)
			output, err := cmd.Output()
			if tt.blocked {
				require.Error(t, err)
				assert.Empty(t, output)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.want, string(output))
			}
		})
	}
}
