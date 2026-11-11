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

func TestCreateSecurityAdvisoryAuthentication(t *testing.T) {
	app := &GitHubAppConfig{AppID: "${{ vars.APP_ID }}", PrivateKey: "${{ secrets.APP_KEY }}"}
	for _, tt := range []struct {
		name        string
		handler     BaseSafeOutputConfig
		globalToken string
		globalApp   *GitHubAppConfig
		staged      *TemplatableBool
		wantToken   string
		wantError   bool
	}{
		{name: "missing credentials", wantError: true},
		{name: "handler PAT", handler: BaseSafeOutputConfig{GitHubToken: "${{ secrets.ADVISORY_TOKEN }}"}, wantToken: "${{ secrets.ADVISORY_TOKEN }}"},
		{name: "global PAT", globalToken: "${{ secrets.GLOBAL_TOKEN }}", wantToken: "${{ secrets.GLOBAL_TOKEN }}"},
		{name: "handler App", handler: BaseSafeOutputConfig{GitHubApp: app}, wantError: true},
		{name: "global App", globalApp: app, wantError: true},
		{name: "handler overrides global", handler: BaseSafeOutputConfig{GitHubToken: "${{ secrets.ADVISORY_TOKEN }}"}, globalApp: app, wantToken: "${{ secrets.ADVISORY_TOKEN }}"},
		{name: "Actions token", handler: BaseSafeOutputConfig{GitHubToken: "${{ github.token }}"}, wantError: true},
		{name: "Actions secret", globalToken: "${{ secrets.GITHUB_TOKEN }}", wantError: true},
		{name: "bracket notation", globalToken: "${{ secrets['GITHUB_TOKEN'] }}", wantError: true},
		{name: "Actions fallback", globalToken: "${{ secrets.PAT || github.token }}", wantError: true},
		{name: "blank token", globalToken: " ", wantError: true},
		{name: "handler staged", handler: BaseSafeOutputConfig{Staged: templatableBoolPtr("true")}},
		{name: "global staged", staged: templatableBoolPtr("true")},
		{name: "dynamic staged still needs credentials", staged: templatableBoolPtr("${{ inputs.preview }}"), wantError: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			config := &SafeOutputsConfig{
				CreateSecurityAdvisory: &CreateSecurityAdvisoryConfig{BaseSafeOutputConfig: tt.handler},
				GitHubToken:            tt.globalToken, GitHubApp: tt.globalApp, Staged: tt.staged,
			}
			err := validateSafeOutputsCreateSecurityAdvisory(config)
			if tt.wantError {
				require.ErrorContains(t, err, "requires an external github-token")
				return
			}
			require.NoError(t, err)
			if tt.wantToken == "" {
				assert.Empty(t, handlerRegistry["create_security_advisory"](config)["github-token"])
			} else {
				assert.Equal(t, tt.wantToken, handlerRegistry["create_security_advisory"](config)["github-token"])
			}
		})
	}
}

func TestCreateSecurityAdvisoryCompile(t *testing.T) {
	for _, tt := range []struct {
		name      string
		config    string
		wantError string
		wantToken string
	}{
		{name: "requires credentials", config: "  create-security-advisory:\n", wantError: "requires an external github-token"},
		{name: "rejects Actions token", config: "  create-security-advisory:\n    github-token: ${{ secrets.GITHUB_TOKEN }}\n", wantError: "requires an external"},
		{name: "handler token", config: "  create-security-advisory:\n    max: 2\n    github-token: ${{ secrets.ADVISORY_TOKEN }}\n", wantToken: "${{ secrets.ADVISORY_TOKEN }}"},
		{name: "global token", config: "  github-token: ${{ secrets.ADVISORY_TOKEN }}\n  create-security-advisory:\n", wantToken: "${{ secrets.ADVISORY_TOKEN }}"},
		{name: "handler App", config: "  create-security-advisory:\n    github-app:\n      client-id: ${{ vars.APP_ID }}\n      private-key: ${{ secrets.APP_KEY }}\n", wantError: "GitHub App authentication is unsupported"},
		{name: "global App", config: "  github-app:\n    client-id: ${{ vars.APP_ID }}\n    private-key: ${{ secrets.APP_KEY }}\n  create-security-advisory:\n", wantError: "GitHub App authentication is unsupported"},
		{name: "staged without credentials", config: "  create-security-advisory:\n    staged: true\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "advisory.md")
			content := "---\non: workflow_dispatch\nstrict: false\nengine: copilot\nsafe-outputs:\n  create-issue: false\n" + tt.config + "---\nCreate a draft advisory for review.\n"
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
			require.NotNil(t, data.SafeOutputs.CreateSecurityAdvisory)
			assert.Contains(t, computeEnabledToolNames(data), "create_security_advisory")
			assert.True(t, hasHandlerManagerTypes(data))
			agentConfig, err := generateSafeOutputsConfig(data)
			require.NoError(t, err)
			assert.NotContains(t, agentConfig, "github-token")
			assert.NotContains(t, agentConfig, "outputs.token")
			assert.NotContains(t, agentConfig, "secrets.")
			lock, err := os.ReadFile(strings.TrimSuffix(path, ".md") + ".lock.yml")
			require.NoError(t, err)
			compiled := string(lock)
			assert.Contains(t, compiled, `"create_security_advisory"`)
			assert.NotContains(t, compiled, "      repository-advisories: write")
			if tt.wantToken != "" {
				assert.Equal(t, tt.wantToken, handlerRegistry["create_security_advisory"](data.SafeOutputs)["github-token"])
			}
			assert.NotContains(t, compiled, "permission-repository-advisories:")
		})
	}
}
