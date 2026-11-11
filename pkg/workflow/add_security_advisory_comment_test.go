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

func TestAddSecurityAdvisoryCommentCompile(t *testing.T) {
	for _, tt := range []struct {
		name      string
		config    string
		wantError string
		wantToken string
	}{
		{name: "requires credentials", config: "  add-security-advisory-comment:\n", wantError: "requires an external github-token"},
		{name: "rejects Actions token", config: "  add-security-advisory-comment:\n    github-token: ${{ secrets.GITHUB_TOKEN }}\n", wantError: "requires an external"},
		{name: "handler token", config: "  add-security-advisory-comment:\n    max: 2\n    footer: false\n    github-token: ${{ secrets.ADVISORY_TOKEN }}\n", wantToken: "${{ secrets.ADVISORY_TOKEN }}"},
		{name: "global token", config: "  github-token: ${{ secrets.ADVISORY_TOKEN }}\n  add-security-advisory-comment:\n", wantToken: "${{ secrets.ADVISORY_TOKEN }}"},
		{name: "handler App", config: "  add-security-advisory-comment:\n    github-app:\n      client-id: ${{ vars.APP_ID }}\n      private-key: ${{ secrets.APP_KEY }}\n", wantError: "GitHub App authentication is unsupported"},
		{name: "global App", config: "  github-app:\n    client-id: ${{ vars.APP_ID }}\n    private-key: ${{ secrets.APP_KEY }}\n  add-security-advisory-comment:\n", wantError: "GitHub App authentication is unsupported"},
		{name: "staged without credentials", config: "  add-security-advisory-comment:\n    staged: true\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "advisory-comment.md")
			content := "---\non: workflow_dispatch\nstrict: false\nengine: copilot\nsafe-outputs:\n  create-issue: false\n" + tt.config + "---\nComment on a security advisory.\n"
			require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
			compiler := NewCompiler(WithVersion("test"))
			err := compiler.CompileWorkflow(path)
			if tt.wantError != "" {
				require.ErrorContains(t, err, tt.wantError)
				return
			}
			require.NoError(t, err)
			data, err := compiler.ParseWorkflowFile(path)
			require.NoError(t, err)
			require.NotNil(t, data.SafeOutputs.AddSecurityAdvisoryComment)
			assert.Nil(t, data.SafeOutputs.CreateIssues)
			assert.Contains(t, computeEnabledToolNames(data), "add_security_advisory_comment")
			assert.True(t, hasHandlerManagerTypes(data))
			agentConfig, err := generateSafeOutputsConfig(data)
			require.NoError(t, err)
			assert.NotContains(t, agentConfig, "github-token")
			assert.NotContains(t, agentConfig, "outputs.token")
			assert.NotContains(t, agentConfig, "secrets.")
			handlerConfig := handlerRegistry["add_security_advisory_comment"](data.SafeOutputs)
			if tt.wantToken != "" {
				assert.Equal(t, tt.wantToken, handlerConfig["github-token"])
			}
			if tt.name == "handler token" {
				assert.Equal(t, 2, handlerConfig["max"])
				assert.Equal(t, false, handlerConfig["footer"])
			}
			lock, err := os.ReadFile(strings.TrimSuffix(path, ".md") + ".lock.yml")
			require.NoError(t, err)
			compiled := string(lock)
			assert.Contains(t, compiled, `"add_security_advisory_comment"`)
			assert.NotContains(t, compiled, "      repository-advisories: write")
			assert.NotContains(t, compiled, "permission-repository-advisories:")
		})
	}
}
