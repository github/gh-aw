//go:build !integration

package cli

import (
	"bytes"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/setutil"
	"github.com/github/gh-aw/pkg/testutil"
	"github.com/github/gh-aw/pkg/workflow"
)

func TestDisplaySecretsSummaryTable_UnicodeNameAlignment(t *testing.T) {
	oldStderr := os.Stderr
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stderr = w
	t.Cleanup(func() {
		os.Stderr = oldStderr
	})

	displaySecretsSummaryTable([]SecretRequirement{
		{Name: "名称", WhenNeeded: "needed"},
		{Name: "LONGEST", WhenNeeded: "needed"},
	}, nil)

	require.NoError(t, w.Close())
	var output bytes.Buffer
	_, err = output.ReadFrom(r)
	require.NoError(t, err)

	assert.Contains(t, output.String(), "名称    - needed")
	assert.Contains(t, output.String(), "LONGEST - needed")
}

func TestGetRequiredSecretsForEngine(t *testing.T) {
	tests := []struct {
		name                 string
		engine               string
		includeSystemSecrets bool
		includeOptional      bool
		wantSecretNames      []string
		wantMinCount         int
		wantMaxCount         int
	}{
		{
			name:                 "copilot engine without system secrets",
			engine:               string(constants.CopilotEngine),
			includeSystemSecrets: false,
			includeOptional:      false,
			wantSecretNames:      []string{"COPILOT_GITHUB_TOKEN"},
			wantMinCount:         1,
			wantMaxCount:         1,
		},
		{
			name:                 "copilot engine with system secrets",
			engine:               string(constants.CopilotEngine),
			includeSystemSecrets: true,
			includeOptional:      false,
			wantSecretNames:      []string{"COPILOT_GITHUB_TOKEN"}, // No system secrets since all are optional
			wantMinCount:         1,
			wantMaxCount:         1,
		},
		{
			name:                 "copilot engine with optional secrets",
			engine:               string(constants.CopilotEngine),
			includeSystemSecrets: true,
			includeOptional:      true,
			wantSecretNames:      []string{"COPILOT_GITHUB_TOKEN", "GH_AW_GITHUB_TOKEN"},
			wantMinCount:         3, // At least 3 (required system + optional system + engine)
			wantMaxCount:         10,
		},
		{
			name:                 "claude engine",
			engine:               string(constants.ClaudeEngine),
			includeSystemSecrets: false,
			includeOptional:      false,
			wantSecretNames:      []string{"ANTHROPIC_API_KEY"},
			wantMinCount:         1,
			wantMaxCount:         1,
		},
		{
			name:                 "codex engine",
			engine:               string(constants.CodexEngine),
			includeSystemSecrets: false,
			includeOptional:      false,
			wantSecretNames:      []string{"OPENAI_API_KEY"},
			wantMinCount:         1,
			wantMaxCount:         1,
		},
		{
			name:                 "empty engine returns only system secrets when requested",
			engine:               "",
			includeSystemSecrets: true,
			includeOptional:      true, // Changed to true to include optional system secrets
			wantSecretNames:      []string{"GH_AW_GITHUB_TOKEN"},
			wantMinCount:         1,
			wantMaxCount:         5,
		},
		{
			name:                 "unknown engine returns no engine secrets",
			engine:               "unknown-engine",
			includeSystemSecrets: false,
			includeOptional:      false,
			wantSecretNames:      []string{},
			wantMinCount:         0,
			wantMaxCount:         0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requirements := getSecretRequirementsForEngine(tt.engine, tt.includeSystemSecrets, tt.includeOptional)

			assert.GreaterOrEqual(t, len(requirements), tt.wantMinCount,
				"Should have at least %d requirements", tt.wantMinCount)
			assert.LessOrEqual(t, len(requirements), tt.wantMaxCount,
				"Should have at most %d requirements", tt.wantMaxCount)

			// Check that expected secrets are present
			secretNames := make(map[string]struct{})
			for _, req := range requirements {
				secretNames[req.Name] = struct{}{}
			}
			for _, wantName := range tt.wantSecretNames {
				assert.Contains(t, secretNames, wantName,
					"Should include secret %s", wantName)
			}
		})
	}
}

func TestGetRequiredSecretsForEngineAttributes(t *testing.T) {
	t.Run("copilot secret has correct attributes", func(t *testing.T) {
		requirements := getSecretRequirementsForEngine(string(constants.CopilotEngine), false, false)
		require.Len(t, requirements, 1, "Should have exactly one requirement")

		req := requirements[0]
		assert.Equal(t, "COPILOT_GITHUB_TOKEN", req.Name, "Secret name should match")
		assert.True(t, req.IsEngineSecret, "Should be marked as engine secret")
		assert.Equal(t, string(constants.CopilotEngine), req.EngineName, "Engine name should match")
		assert.False(t, req.Optional, "Copilot token should be required")
		assert.NotEmpty(t, req.KeyURL, "Should have a key URL")
		assert.NotEmpty(t, req.Description, "Should have a description")
	})

	t.Run("claude secret has no alternative env vars", func(t *testing.T) {
		requirements := getSecretRequirementsForEngine(string(constants.ClaudeEngine), false, false)
		require.Len(t, requirements, 1, "Should have exactly one requirement")

		req := requirements[0]
		assert.Equal(t, "ANTHROPIC_API_KEY", req.Name, "Secret name should match")
		assert.Empty(t, req.AlternativeEnvVars, "Should have no alternative env vars")
	})

	t.Run("system secrets are not engine secrets", func(t *testing.T) {
		requirements := getSecretRequirementsForEngine("", true, true)

		for _, req := range requirements {
			if req.Name == "GH_AW_GITHUB_TOKEN" {
				assert.False(t, req.IsEngineSecret, "System secret should not be marked as engine secret")
				assert.Empty(t, req.EngineName, "System secret should have empty engine name")
			}
		}
	})
}

func TestBuildCopilotPATCreationURL(t *testing.T) {
	t.Run("defaults to public github", func(t *testing.T) {
		// Clear all host env vars so the remote-detection fallback is exercised in
		// isolation.  Higher-priority variables (GITHUB_SERVER_URL, etc.) are set
		// by GitHub Actions and would otherwise override the expected default.
		t.Setenv("GITHUB_SERVER_URL", "")
		t.Setenv("GITHUB_ENTERPRISE_HOST", "")
		t.Setenv("GITHUB_HOST", "")
		t.Setenv("GH_HOST", "")

		// Run outside any git checkout so that getHostFromOriginRemote() has no
		// remote to detect, guaranteeing the github.com default is returned.
		tmpDir := testutil.TempDir(t, "copilot-pat-url-default-*")
		t.Chdir(tmpDir)

		rawURL := buildCopilotPATCreationURL()
		parsed, err := url.Parse(rawURL)
		require.NoError(t, err)

		assert.Equal(t, "https", parsed.Scheme)
		assert.Equal(t, "github.com", parsed.Host)
		assert.Equal(t, "/settings/personal-access-tokens/new", parsed.Path)
		assert.Equal(t, constants.CopilotGitHubToken, parsed.Query().Get("name"), "token name must be prefilled to COPILOT_GITHUB_TOKEN")
		assert.Equal(t, "read", parsed.Query().Get("user_copilot_requests"))
		assert.Empty(t, parsed.Query().Get("contents"), "Copilot PAT setup URL should not request unrelated repository permissions")
		assert.Empty(t, parsed.Query().Get("issues"), "Copilot PAT setup URL should not request unrelated issue permissions")
		assert.Empty(t, parsed.Query().Get("pull_requests"), "Copilot PAT setup URL should not request unrelated pull request permissions")
	})

	t.Run("uses GH_HOST when gh auth is configured for enterprise", func(t *testing.T) {
		// Clear higher-priority variables first so GH_HOST (the lowest-priority
		// consumer in getGitHubHost) is actually honoured.
		t.Setenv("GITHUB_SERVER_URL", "")
		t.Setenv("GITHUB_ENTERPRISE_HOST", "")
		t.Setenv("GITHUB_HOST", "")
		t.Setenv("GH_HOST", "ghe.example.com")

		rawURL := buildCopilotPATCreationURL()
		parsed, err := url.Parse(rawURL)
		require.NoError(t, err)

		assert.Equal(t, "https", parsed.Scheme)
		assert.Equal(t, "ghe.example.com", parsed.Host)
		assert.Equal(t, "/settings/personal-access-tokens/new", parsed.Path)
	})

	t.Run("falls back to origin remote host when GH_HOST is unset", func(t *testing.T) {
		t.Setenv("GH_HOST", "")
		t.Setenv("GITHUB_HOST", "")
		t.Setenv("GITHUB_ENTERPRISE_HOST", "")
		t.Setenv("GITHUB_SERVER_URL", "")

		tmpDir := testutil.TempDir(t, "copilot-pat-url-host-*")
		require.NoError(t, initTestGitRepo(tmpDir))
		require.NoError(t, addOriginRemoteToTestRepo(tmpDir, "https://ghes.example.com/org/repo.git"))
		t.Chdir(tmpDir)

		rawURL := buildCopilotPATCreationURL()
		parsed, err := url.Parse(rawURL)
		require.NoError(t, err)

		assert.Equal(t, "https", parsed.Scheme)
		assert.Equal(t, "ghes.example.com", parsed.Host)
		assert.Equal(t, "/settings/personal-access-tokens/new", parsed.Path)
	})
}

func TestEnsureSecretAvailable_ExistingCopilotSecret(t *testing.T) {
	copilotReq := SecretRequirement{
		Name:           constants.CopilotGitHubToken,
		IsEngineSecret: true,
		EngineName:     string(constants.CopilotEngine),
	}
	claudeReq := SecretRequirement{
		Name:           "ANTHROPIC_API_KEY",
		IsEngineSecret: true,
		EngineName:     string(constants.ClaudeEngine),
	}

	t.Run("uses existing Copilot secret when confirmed", func(t *testing.T) {
		promptCalled := false
		origConfirm := engineSecretsConfirmExistingFn
		origPrompt := engineSecretsPromptFn
		t.Cleanup(func() {
			engineSecretsConfirmExistingFn = origConfirm
			engineSecretsPromptFn = origPrompt
		})
		engineSecretsConfirmExistingFn = func(secretName string, _ EngineSecretConfig) (bool, error) {
			assert.Equal(t, constants.CopilotGitHubToken, secretName)
			return true, nil
		}
		engineSecretsPromptFn = func(_ SecretRequirement, _ EngineSecretConfig) error {
			promptCalled = true
			return nil
		}

		cfg := EngineSecretConfig{
			ExistingSecrets: map[string]struct{}{constants.CopilotGitHubToken: {}},
		}
		require.NoError(t, ensureSecretAvailable(copilotReq, cfg))
		assert.False(t, promptCalled, "confirmed existing secret must not trigger a token prompt")
	})

	t.Run("replaces existing Copilot secret when requested", func(t *testing.T) {
		var capturedConfig EngineSecretConfig
		origConfirm := engineSecretsConfirmExistingFn
		origPrompt := engineSecretsPromptFn
		t.Cleanup(func() {
			engineSecretsConfirmExistingFn = origConfirm
			engineSecretsPromptFn = origPrompt
		})
		engineSecretsConfirmExistingFn = func(_ string, _ EngineSecretConfig) (bool, error) {
			return false, nil
		}
		engineSecretsPromptFn = func(req SecretRequirement, config EngineSecretConfig) error {
			capturedConfig = config
			return nil
		}

		cfg := EngineSecretConfig{
			ExistingSecrets: map[string]struct{}{constants.CopilotGitHubToken: {}},
		}
		require.NoError(t, ensureSecretAvailable(copilotReq, cfg))
		assert.True(t, capturedConfig.OverwriteExistingSecret, "replacement prompt must overwrite the existing secret")
	})

	t.Run("existing non-Copilot secret skips prompt", func(t *testing.T) {
		called := false
		orig := engineSecretsPromptFn
		t.Cleanup(func() { engineSecretsPromptFn = orig })
		engineSecretsPromptFn = func(_ SecretRequirement, _ EngineSecretConfig) error {
			called = true
			return nil
		}

		cfg := EngineSecretConfig{
			ExistingSecrets: map[string]struct{}{"ANTHROPIC_API_KEY": {}},
		}
		require.NoError(t, ensureSecretAvailable(claudeReq, cfg))
		assert.False(t, called, "non-Copilot existing secret must not trigger a prompt")
	})
}

func TestEnsureSecretAvailableClaudeOAuthToken(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "private-oauth-token")
	req := SecretRequirement{
		Name:           "ANTHROPIC_API_KEY",
		IsEngineSecret: true,
		EngineName:     string(constants.ClaudeEngine),
	}
	originalPrompt := engineSecretsPromptFn
	t.Cleanup(func() { engineSecretsPromptFn = originalPrompt })
	prompted := false
	engineSecretsPromptFn = func(_ SecretRequirement, _ EngineSecretConfig) error {
		prompted = true
		return nil
	}

	err := ensureSecretAvailable(req, EngineSecretConfig{})
	require.ErrorIs(t, err, errUnsupportedClaudeOAuthToken)
	assert.NotContains(t, err.Error(), "private-oauth-token")
	assert.False(t, prompted)

	t.Run("existing repository secret is accepted", func(t *testing.T) {
		err := ensureSecretAvailable(req, EngineSecretConfig{
			ExistingSecrets: map[string]struct{}{"ANTHROPIC_API_KEY": {}},
		})
		require.NoError(t, err)
	})
}

func TestStringContainsSecretName(t *testing.T) {
	tests := []struct {
		name       string
		output     string
		secretName string
		want       bool
	}{
		{
			name:       "exact match single line",
			output:     "GH_AW_GITHUB_TOKEN",
			secretName: "GH_AW_GITHUB_TOKEN",
			want:       true,
		},
		{
			name:       "match with tab separator",
			output:     "GH_AW_GITHUB_TOKEN\t2024-01-01",
			secretName: "GH_AW_GITHUB_TOKEN",
			want:       true,
		},
		{
			name:       "match with space separator",
			output:     "GH_AW_GITHUB_TOKEN 2024-01-01",
			secretName: "GH_AW_GITHUB_TOKEN",
			want:       true,
		},
		{
			name:       "match in multiline output",
			output:     "SOME_SECRET\t2024-01-01\nGH_AW_GITHUB_TOKEN\t2024-02-01\nOTHER_SECRET\t2024-03-01",
			secretName: "GH_AW_GITHUB_TOKEN",
			want:       true,
		},
		{
			name:       "no match - different secret",
			output:     "SOME_OTHER_TOKEN\t2024-01-01",
			secretName: "GH_AW_GITHUB_TOKEN",
			want:       false,
		},
		{
			name:       "no match - prefix only",
			output:     "GH_AW_GITHUB_TOKEN_EXTENDED\t2024-01-01",
			secretName: "GH_AW_GITHUB_TOKEN",
			want:       false,
		},
		{
			name:       "no match - empty output",
			output:     "",
			secretName: "GH_AW_GITHUB_TOKEN",
			want:       false,
		},
		{
			name:       "no match - secret longer than line",
			output:     "SHORT",
			secretName: "GH_AW_GITHUB_TOKEN",
			want:       false,
		},
		{
			name:       "match copilot token",
			output:     "COPILOT_GITHUB_TOKEN\t2024-01-15\nANTHROPIC_API_KEY\t2024-01-20",
			secretName: "COPILOT_GITHUB_TOKEN",
			want:       true,
		},
		{
			name:       "match anthropic key in mixed output",
			output:     "COPILOT_GITHUB_TOKEN\t2024-01-15\nANTHROPIC_API_KEY\t2024-01-20",
			secretName: "ANTHROPIC_API_KEY",
			want:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stringContainsSecretName(tt.output, tt.secretName)
			assert.Equal(t, tt.want, got,
				"stringContainsSecretName(%q, %q) = %v, want %v",
				tt.output, tt.secretName, got, tt.want)
		})
	}
}

func TestUploadSecretToRepo_UsesStdinForSecretValue(t *testing.T) {
	fakeBinDir := t.TempDir()
	fakeGH := filepath.Join(fakeBinDir, "gh")
	argsLog := filepath.Join(fakeBinDir, "gh-args.log")
	stdinLog := filepath.Join(fakeBinDir, "gh-stdin.log")
	script := "#!/bin/sh\n" +
		"printf '%s\\n' \"$*\" >> \"" + argsLog + "\"\n" +
		"if [ \"$1\" = \"secret\" ] && [ \"$2\" = \"list\" ]; then\n" +
		"  exit 0\n" +
		"fi\n" +
		"if [ \"$1\" = \"secret\" ] && [ \"$2\" = \"set\" ]; then\n" +
		"  cat > \"" + stdinLog + "\"\n" +
		"  exit 0\n" +
		"fi\n" +
		"exit 1\n"
	require.NoError(t, os.WriteFile(fakeGH, []byte(script), 0o755))
	t.Setenv("PATH", fakeBinDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	err := uploadSecretToRepo(t.Context(), "TEST_SECRET", "super-secret-value", "owner/repo", false, true)
	require.NoError(t, err)

	argsBytes, readArgsErr := os.ReadFile(argsLog)
	require.NoError(t, readArgsErr)
	args := string(argsBytes)
	assert.Contains(t, args, "secret set TEST_SECRET --repo owner/repo")
	assert.NotContains(t, args, "--body")
	assert.NotContains(t, args, "super-secret-value")

	stdinBytes, readStdinErr := os.ReadFile(stdinLog)
	require.NoError(t, readStdinErr)
	assert.Equal(t, "super-secret-value", strings.TrimSpace(string(stdinBytes)))
}

func TestGetEngineSecretDescription(t *testing.T) {
	tests := []struct {
		name         string
		engineValue  string
		wantContains string
	}{
		{
			name:         "copilot engine description",
			engineValue:  string(constants.CopilotEngine),
			wantContains: "Fine-grained PAT",
		},
		{
			name:         "claude engine description",
			engineValue:  string(constants.ClaudeEngine),
			wantContains: "Anthropic Console",
		},
		{
			name:         "codex engine description",
			engineValue:  string(constants.CodexEngine),
			wantContains: "OpenAI",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opt := constants.GetEngineOption(tt.engineValue)
			require.NotNil(t, opt, "Engine option should exist for %s", tt.engineValue)

			desc := getEngineSecretDescription(opt)
			assert.Contains(t, desc, tt.wantContains,
				"Description should contain %q", tt.wantContains)
		})
	}
}

func TestSecretRequirementStructure(t *testing.T) {
	t.Run("SecretRequirement has all required fields", func(t *testing.T) {
		req := SecretRequirement{
			Name:               "TEST_SECRET",
			WhenNeeded:         "When testing",
			Description:        "Test description",
			Optional:           false,
			AlternativeEnvVars: []string{"ALT_SECRET"},
			KeyURL:             "https://example.com/keys",
			IsEngineSecret:     true,
			EngineName:         "test-engine",
		}

		assert.Equal(t, "TEST_SECRET", req.Name)
		assert.Equal(t, "When testing", req.WhenNeeded)
		assert.Equal(t, "Test description", req.Description)
		assert.False(t, req.Optional)
		assert.Contains(t, req.AlternativeEnvVars, "ALT_SECRET")
		assert.Equal(t, "https://example.com/keys", req.KeyURL)
		assert.True(t, req.IsEngineSecret)
		assert.Equal(t, "test-engine", req.EngineName)
	})
}

func TestEngineSecretConfigStructure(t *testing.T) {
	t.Run("EngineSecretConfig has all required fields", func(t *testing.T) {
		config := EngineSecretConfig{
			RepoSlug:             "owner/repo",
			Engine:               "copilot",
			Verbose:              true,
			ExistingSecrets:      map[string]struct{}{"SECRET1": {}},
			IncludeSystemSecrets: true,
			IncludeOptional:      false,
		}

		assert.Equal(t, "owner/repo", config.RepoSlug)
		assert.Equal(t, "copilot", config.Engine)
		assert.True(t, config.Verbose)
		assert.True(t, setutil.Contains(config.ExistingSecrets, "SECRET1"))
		assert.True(t, config.IncludeSystemSecrets)
		assert.False(t, config.IncludeOptional)
	})
}

func TestGetEngineSecretNameAndValue(t *testing.T) {
	// Save current env and restore after test
	oldCopilotToken := os.Getenv("COPILOT_GITHUB_TOKEN")
	oldAnthropicKey := os.Getenv("ANTHROPIC_API_KEY")
	oldOpenAIKey := os.Getenv("OPENAI_API_KEY")
	oldCodexKey := os.Getenv("CODEX_API_KEY")
	defer func() {
		if oldCopilotToken != "" {
			os.Setenv("COPILOT_GITHUB_TOKEN", oldCopilotToken)
		} else {
			os.Unsetenv("COPILOT_GITHUB_TOKEN")
		}
		if oldAnthropicKey != "" {
			os.Setenv("ANTHROPIC_API_KEY", oldAnthropicKey)
		} else {
			os.Unsetenv("ANTHROPIC_API_KEY")
		}
		if oldOpenAIKey != "" {
			os.Setenv("OPENAI_API_KEY", oldOpenAIKey)
		} else {
			os.Unsetenv("OPENAI_API_KEY")
		}
		if oldCodexKey != "" {
			os.Setenv("CODEX_API_KEY", oldCodexKey)
		} else {
			os.Unsetenv("CODEX_API_KEY")
		}
	}()

	t.Run("secret exists in repository", func(t *testing.T) {
		existingSecrets := map[string]struct {
		}{
			"COPILOT_GITHUB_TOKEN": {},
		}

		name, value, existsInRepo, err := GetEngineSecretNameAndValue("copilot", existingSecrets)

		require.NoError(t, err, "Should not error when secret exists in repo")
		assert.Equal(t, "COPILOT_GITHUB_TOKEN", name)
		assert.Empty(t, value, "Value should be empty when secret exists in repo")
		assert.True(t, existsInRepo, "Should indicate secret exists in repo")
	})

	t.Run("secret found in environment", func(t *testing.T) {
		os.Setenv("ANTHROPIC_API_KEY", "test-api-key-12345")
		defer os.Unsetenv("ANTHROPIC_API_KEY")

		existingSecrets := map[string]struct {
		}{}

		name, value, existsInRepo, err := GetEngineSecretNameAndValue("claude", existingSecrets)

		require.NoError(t, err, "Should not error when secret in environment")
		assert.Equal(t, "ANTHROPIC_API_KEY", name)
		assert.Equal(t, "test-api-key-12345", value)
		assert.False(t, existsInRepo, "Should indicate secret does not exist in repo")
	})

	t.Run("secret not in repo or environment", func(t *testing.T) {
		os.Unsetenv("OPENAI_API_KEY")
		os.Unsetenv("CODEX_API_KEY")

		existingSecrets := map[string]struct {
		}{}

		name, value, existsInRepo, err := GetEngineSecretNameAndValue("codex", existingSecrets)

		require.NoError(t, err, "Should not error even when secret not found")
		assert.Equal(t, "OPENAI_API_KEY", name)
		assert.Empty(t, value, "Value should be empty when not found in env")
		assert.False(t, existsInRepo, "Should indicate secret does not exist in repo")
	})

	t.Run("unknown engine returns error", func(t *testing.T) {
		existingSecrets := map[string]struct {
		}{}

		_, _, _, err := GetEngineSecretNameAndValue("unknown-engine", existingSecrets)

		require.Error(t, err, "Should error for unknown engine")
		require.ErrorContains(t, err, "unknown engine", "Error should mention unknown engine")
	})

	t.Run("no alternative secret in repo", func(t *testing.T) {
		existingSecrets := map[string]struct {
		}{}

		name, value, existsInRepo, err := GetEngineSecretNameAndValue("claude", existingSecrets)

		require.NoError(t, err, "Should not error")
		assert.Equal(t, "ANTHROPIC_API_KEY", name)
		assert.Empty(t, value, "Value should be empty when not found")
		assert.False(t, existsInRepo, "Should indicate secret does not exist")
	})

	t.Run("prefers primary secret over environment", func(t *testing.T) {
		os.Setenv("COPILOT_GITHUB_TOKEN", "test-token-from-env")
		defer os.Unsetenv("COPILOT_GITHUB_TOKEN")

		existingSecrets := map[string]struct {
		}{
			"COPILOT_GITHUB_TOKEN": {},
		}

		name, value, existsInRepo, err := GetEngineSecretNameAndValue("copilot", existingSecrets)

		require.NoError(t, err, "Should not error")
		assert.Equal(t, "COPILOT_GITHUB_TOKEN", name)
		assert.Empty(t, value, "Should prefer existing repo secret over environment")
		assert.True(t, existsInRepo, "Should indicate secret exists in repo")
	})
}

func TestGetEngineSecretNameAndValueClaudeOAuthToken(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "private-oauth-token")

	t.Run("unsupported token alone", func(t *testing.T) {
		_, _, _, err := GetEngineSecretNameAndValue("claude", nil)
		require.Error(t, err)
		require.ErrorContains(t, err, "CLAUDE_CODE_OAUTH_TOKEN")
		require.ErrorContains(t, err, "ANTHROPIC_API_KEY")
		require.ErrorContains(t, err, "Workload Identity Federation")
		assert.NotContains(t, err.Error(), "private-oauth-token")
	})

	t.Run("API key in environment takes precedence", func(t *testing.T) {
		t.Setenv("ANTHROPIC_API_KEY", "supported-api-key")
		name, value, exists, err := GetEngineSecretNameAndValue("claude", nil)
		require.NoError(t, err)
		assert.Equal(t, "ANTHROPIC_API_KEY", name)
		assert.Equal(t, "supported-api-key", value)
		assert.False(t, exists)
	})

	t.Run("repository secret takes precedence", func(t *testing.T) {
		name, value, exists, err := GetEngineSecretNameAndValue("claude", map[string]struct{}{"ANTHROPIC_API_KEY": {}})
		require.NoError(t, err)
		assert.Equal(t, "ANTHROPIC_API_KEY", name)
		assert.Empty(t, value)
		assert.True(t, exists)
	})
}

func TestMustValidateExistingSecretValue(t *testing.T) {
	t.Run("copilot engine secret requires revalidation", func(t *testing.T) {
		req := SecretRequirement{
			Name:           "COPILOT_GITHUB_TOKEN",
			IsEngineSecret: true,
			EngineName:     string(constants.CopilotEngine),
		}

		assert.True(t, mustValidateExistingSecretValue(req))
	})

	t.Run("non-copilot engine secret does not require revalidation", func(t *testing.T) {
		req := SecretRequirement{
			Name:           "ANTHROPIC_API_KEY",
			IsEngineSecret: true,
			EngineName:     string(constants.ClaudeEngine),
		}

		assert.False(t, mustValidateExistingSecretValue(req))
	})

	t.Run("system secret does not require revalidation", func(t *testing.T) {
		req := SecretRequirement{
			Name:           "GH_AW_GITHUB_TOKEN",
			IsEngineSecret: false,
		}

		assert.False(t, mustValidateExistingSecretValue(req))
	})
}

func TestGetMissingRequiredSecrets(t *testing.T) {
	t.Run("all secrets missing", func(t *testing.T) {
		requirements := []SecretRequirement{
			{Name: "SECRET1", Optional: false},
			{Name: "SECRET2", Optional: false},
			{Name: "SECRET3", Optional: false},
		}
		existingSecrets := map[string]struct {
		}{}

		missing := getMissingRequiredSecrets(requirements, existingSecrets)

		assert.Len(t, missing, 3, "Should have 3 missing secrets")
		assert.Equal(t, "SECRET1", missing[0].Name)
		assert.Equal(t, "SECRET2", missing[1].Name)
		assert.Equal(t, "SECRET3", missing[2].Name)
	})

	t.Run("all secrets exist", func(t *testing.T) {
		requirements := []SecretRequirement{
			{Name: "SECRET1", Optional: false},
			{Name: "SECRET2", Optional: false},
		}
		existingSecrets := map[string]struct {
		}{
			"SECRET1": {},
			"SECRET2": {},
		}

		missing := getMissingRequiredSecrets(requirements, existingSecrets)

		assert.Empty(t, missing, "Should have no missing secrets")
	})

	t.Run("some secrets missing", func(t *testing.T) {
		requirements := []SecretRequirement{
			{Name: "SECRET1", Optional: false},
			{Name: "SECRET2", Optional: false},
			{Name: "SECRET3", Optional: false},
		}
		existingSecrets := map[string]struct {
		}{
			"SECRET1": {},
		}

		missing := getMissingRequiredSecrets(requirements, existingSecrets)

		assert.Len(t, missing, 2, "Should have 2 missing secrets")
		assert.Equal(t, "SECRET2", missing[0].Name)
		assert.Equal(t, "SECRET3", missing[1].Name)
	})

	t.Run("optional secrets are skipped", func(t *testing.T) {
		requirements := []SecretRequirement{
			{Name: "REQUIRED1", Optional: false},
			{Name: "OPTIONAL1", Optional: true},
			{Name: "REQUIRED2", Optional: false},
			{Name: "OPTIONAL2", Optional: true},
		}
		existingSecrets := map[string]struct {
		}{}

		missing := getMissingRequiredSecrets(requirements, existingSecrets)

		assert.Len(t, missing, 2, "Should only include required secrets")
		assert.Equal(t, "REQUIRED1", missing[0].Name)
		assert.Equal(t, "REQUIRED2", missing[1].Name)
	})

	t.Run("alternative secret names work", func(t *testing.T) {
		requirements := []SecretRequirement{
			{
				Name:               "PRIMARY_SECRET",
				Optional:           false,
				AlternativeEnvVars: []string{"ALT_SECRET1", "ALT_SECRET2"},
			},
			{Name: "OTHER_SECRET", Optional: false},
		}
		existingSecrets := map[string]struct {
		}{
			"ALT_SECRET1": { // Alternative exists
			},
		}

		missing := getMissingRequiredSecrets(requirements, existingSecrets)

		assert.Len(t, missing, 1, "Should have 1 missing secret")
		assert.Equal(t, "OTHER_SECRET", missing[0].Name, "Should not include PRIMARY_SECRET since alternative exists")
	})

	t.Run("alternative secret names - second alternative", func(t *testing.T) {
		requirements := []SecretRequirement{
			{
				Name:               "PRIMARY_SECRET",
				Optional:           false,
				AlternativeEnvVars: []string{"ALT_SECRET1", "ALT_SECRET2"},
			},
		}
		existingSecrets := map[string]struct {
		}{
			"ALT_SECRET2": { // Second alternative exists
			},
		}

		missing := getMissingRequiredSecrets(requirements, existingSecrets)

		assert.Empty(t, missing, "Should find second alternative")
	})

	t.Run("primary secret takes precedence over alternatives", func(t *testing.T) {
		requirements := []SecretRequirement{
			{
				Name:               "PRIMARY_SECRET",
				Optional:           false,
				AlternativeEnvVars: []string{"ALT_SECRET"},
			},
		}
		existingSecrets := map[string]struct {
		}{
			"PRIMARY_SECRET": {},
			"ALT_SECRET":     {},
		}

		missing := getMissingRequiredSecrets(requirements, existingSecrets)

		assert.Empty(t, missing, "Should not include secret when primary exists")
	})

	t.Run("empty requirements list", func(t *testing.T) {
		requirements := []SecretRequirement{}
		existingSecrets := map[string]struct {
		}{
			"SECRET1": {},
		}

		missing := getMissingRequiredSecrets(requirements, existingSecrets)

		assert.Empty(t, missing, "Should return empty list for empty requirements")
	})

	t.Run("empty existing secrets map", func(t *testing.T) {
		requirements := []SecretRequirement{
			{Name: "SECRET1", Optional: false},
			{Name: "SECRET2", Optional: false},
		}
		existingSecrets := map[string]struct {
		}{}

		missing := getMissingRequiredSecrets(requirements, existingSecrets)

		assert.Len(t, missing, 2, "Should return all required secrets as missing")
	})

	t.Run("nil existing secrets map", func(t *testing.T) {
		requirements := []SecretRequirement{
			{Name: "SECRET1", Optional: false},
		}
		var existingSecrets map[string]struct { // nil map
		}

		missing := getMissingRequiredSecrets(requirements, existingSecrets)

		assert.Len(t, missing, 1, "Should handle nil map and return all as missing")
	})

	t.Run("mixed required and optional with alternatives", func(t *testing.T) {
		requirements := []SecretRequirement{
			{
				Name:               "COPILOT_GITHUB_TOKEN",
				Optional:           false,
				IsEngineSecret:     true,
				AlternativeEnvVars: []string{"GITHUB_TOKEN"},
			},
			{
				Name:     "GH_AW_GITHUB_TOKEN",
				Optional: true,
			},
			{
				Name:               "ANTHROPIC_API_KEY",
				Optional:           false,
				IsEngineSecret:     true,
				AlternativeEnvVars: []string{"CLAUDE_API_KEY"},
			},
		}
		existingSecrets := map[string]struct {
		}{
			"GITHUB_TOKEN": { // Alternative for COPILOT_GITHUB_TOKEN
			},
		}

		missing := getMissingRequiredSecrets(requirements, existingSecrets)

		assert.Len(t, missing, 1, "Should have 1 missing required secret")
		assert.Equal(t, "ANTHROPIC_API_KEY", missing[0].Name, "Should only include ANTHROPIC_API_KEY")
	})
}

func TestSecretRequirementsFromAuthDefinition(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		auth       *workflow.AuthDefinition
		engineName string
		want       []SecretRequirement
	}{
		{
			name:       "nil auth returns nil",
			auth:       nil,
			engineName: "custom",
			want:       nil,
		},
		{
			name: "oauth strategy with both client id and secret refs",
			auth: &workflow.AuthDefinition{
				Strategy:        workflow.AuthStrategyOAuthClientCreds,
				ClientIDRef:     "OAUTH_CLIENT_ID",
				ClientSecretRef: "OAUTH_CLIENT_SECRET",
			},
			engineName: "myengine",
			want: []SecretRequirement{
				{
					Name:           "OAUTH_CLIENT_ID",
					WhenNeeded:     "OAuth client ID for myengine engine",
					Description:    "GitHub Actions secret holding the OAuth 2.0 client ID used to obtain access tokens.",
					IsEngineSecret: true,
					EngineName:     "myengine",
				},
				{
					Name:           "OAUTH_CLIENT_SECRET",
					WhenNeeded:     "OAuth client secret for myengine engine",
					Description:    "GitHub Actions secret holding the OAuth 2.0 client secret used to obtain access tokens.",
					IsEngineSecret: true,
					EngineName:     "myengine",
				},
			},
		},
		{
			name: "oauth strategy with only client id ref",
			auth: &workflow.AuthDefinition{
				Strategy:    workflow.AuthStrategyOAuthClientCreds,
				ClientIDRef: "OAUTH_CLIENT_ID",
			},
			engineName: "myengine",
			want: []SecretRequirement{
				{
					Name:           "OAUTH_CLIENT_ID",
					WhenNeeded:     "OAuth client ID for myengine engine",
					Description:    "GitHub Actions secret holding the OAuth 2.0 client ID used to obtain access tokens.",
					IsEngineSecret: true,
					EngineName:     "myengine",
				},
			},
		},
		{
			name: "oauth strategy with only client secret ref",
			auth: &workflow.AuthDefinition{
				Strategy:        workflow.AuthStrategyOAuthClientCreds,
				ClientSecretRef: "OAUTH_CLIENT_SECRET",
			},
			engineName: "myengine",
			want: []SecretRequirement{
				{
					Name:           "OAUTH_CLIENT_SECRET",
					WhenNeeded:     "OAuth client secret for myengine engine",
					Description:    "GitHub Actions secret holding the OAuth 2.0 client secret used to obtain access tokens.",
					IsEngineSecret: true,
					EngineName:     "myengine",
				},
			},
		},
		{
			name: "oauth strategy with neither ref returns empty",
			auth: &workflow.AuthDefinition{
				Strategy: workflow.AuthStrategyOAuthClientCreds,
			},
			engineName: "myengine",
			want:       nil,
		},
		{
			name: "api-key strategy with secret",
			auth: &workflow.AuthDefinition{
				Strategy: workflow.AuthStrategyAPIKey,
				Secret:   "MY_API_KEY",
			},
			engineName: "myengine",
			want: []SecretRequirement{
				{
					Name:           "MY_API_KEY",
					WhenNeeded:     "API key or token for myengine engine",
					Description:    "GitHub Actions secret holding the API key or bearer token for provider authentication.",
					IsEngineSecret: true,
					EngineName:     "myengine",
				},
			},
		},
		{
			name: "bearer strategy with secret",
			auth: &workflow.AuthDefinition{
				Strategy: workflow.AuthStrategyBearer,
				Secret:   "MY_BEARER_TOKEN",
			},
			engineName: "myengine",
			want: []SecretRequirement{
				{
					Name:           "MY_BEARER_TOKEN",
					WhenNeeded:     "API key or token for myengine engine",
					Description:    "GitHub Actions secret holding the API key or bearer token for provider authentication.",
					IsEngineSecret: true,
					EngineName:     "myengine",
				},
			},
		},
		{
			name: "unset strategy with secret defaults to direct secret handling",
			auth: &workflow.AuthDefinition{
				Secret: "DEFAULT_SECRET",
			},
			engineName: "myengine",
			want: []SecretRequirement{
				{
					Name:           "DEFAULT_SECRET",
					WhenNeeded:     "API key or token for myengine engine",
					Description:    "GitHub Actions secret holding the API key or bearer token for provider authentication.",
					IsEngineSecret: true,
					EngineName:     "myengine",
				},
			},
		},
		{
			name: "default strategy branch with empty secret returns empty",
			auth: &workflow.AuthDefinition{
				Strategy: workflow.AuthStrategyAPIKey,
			},
			engineName: "myengine",
			want:       nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := secretRequirementsFromAuthDefinition(tt.auth, tt.engineName)

			assert.Equal(t, tt.want, got)
		})
	}
}
