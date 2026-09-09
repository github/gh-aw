//go:build !integration

package cli

import (
	"testing"

	"charm.land/huh/v2"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/stretchr/testify/assert"
)

func TestApplyCopilotAuthMethodChoice(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name            string
		authMethod      string
		wantCopilotReqs bool
		wantCopilotPAT  bool
	}{
		{
			name:            "copilot-requests sets UseCopilotRequests true",
			authMethod:      "copilot-requests",
			wantCopilotReqs: true,
			wantCopilotPAT:  false,
		},
		{
			name:            "pat sets UseCopilotRequests false",
			authMethod:      "pat",
			wantCopilotReqs: false,
			wantCopilotPAT:  true,
		},
		{
			name:            "empty value (form cancelled) sets UseCopilotRequests false",
			authMethod:      "",
			wantCopilotReqs: false,
			wantCopilotPAT:  false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := &AddInteractiveConfig{}
			cfg.applyCopilotAuthMethodChoice(tc.authMethod)
			assert.Equal(t, tc.wantCopilotReqs, cfg.UseCopilotRequests)
			assert.Equal(t, tc.wantCopilotPAT, cfg.UseCopilotPAT)
		})
	}
}

func TestApplyCopilotAuthMethodChoice_ReEntryClearsOldValue(t *testing.T) {
	t.Parallel()
	cfg := &AddInteractiveConfig{}

	// First selection: copilot-requests
	cfg.applyCopilotAuthMethodChoice("copilot-requests")
	assert.True(t, cfg.UseCopilotRequests)

	// User changes selection to PAT — old value must not persist
	cfg.applyCopilotAuthMethodChoice("pat")
	assert.False(t, cfg.UseCopilotRequests)
	assert.True(t, cfg.UseCopilotPAT)
}

func TestCopilotAuthMethodDescription(t *testing.T) {
	t.Parallel()

	t.Run("bullets both authentication methods", func(t *testing.T) {
		description := copilotAuthMethodDescription(orgCopilotBillingProbeResult{}, "")
		assert.Equal(t, "• PAT: Create or use a COPILOT_GITHUB_TOKEN repository secret.\n• copilot-requests: Use the org's Copilot billing seat; no PAT required.", description)
	})

	t.Run("describes an existing organization secret precisely", func(t *testing.T) {
		description := copilotAuthMethodDescription(orgCopilotBillingProbeResult{}, secretSourceOrganizationSelected)
		assert.Contains(t, description, "Reuse the existing COPILOT_GITHUB_TOKEN organization (selected repository) secret.")
	})

	t.Run("includes inconclusive billing note in copilot-requests bullet", func(t *testing.T) {
		description := copilotAuthMethodDescription(orgCopilotBillingProbeResult{InfoNote: copilotBillingInconclusiveNote}, secretSourceRepository)
		assert.Equal(t, "• PAT: Reuse the existing COPILOT_GITHUB_TOKEN repository secret.\n• copilot-requests: Use the org's Copilot billing seat; no PAT required.\n  (NOTE: Could not confirm org Copilot CLI billing.\n   Check with your org admin if you want to use this option.)", description)
	})
}

func TestPrioritizeEngineOption(t *testing.T) {
	t.Parallel()
	options := []huh.Option[string]{
		huh.NewOption("B", "b"),
		huh.NewOption("A", "a"),
	}

	prioritizeEngineOption(options, "a")
	assert.Equal(t, "a", options[0].Value)
	assert.Equal(t, "b", options[1].Value)

	prioritizeEngineOption(options, "missing")
	assert.Equal(t, "a", options[0].Value)
	assert.Equal(t, "b", options[1].Value)
}

func TestDetermineDefaultEngine(t *testing.T) {
	t.Parallel()
	makeSecrets := func(keys ...string) map[string]struct{} {
		m := make(map[string]struct{}, len(keys))
		for _, k := range keys {
			m[k] = struct{}{}
		}
		return m
	}

	tests := []struct {
		name                    string
		engineOverride          string
		existingSecrets         map[string]struct{}
		workflowSpecifiedEngine string
		want                    string
	}{
		{
			name:                    "engine override takes priority over everything",
			engineOverride:          "codex",
			existingSecrets:         makeSecrets(constants.AnthropicAPIKey),
			workflowSpecifiedEngine: "claude",
			want:                    "codex",
		},
		{
			name:                    "non-default secret overrides workflow preference",
			existingSecrets:         makeSecrets(constants.AnthropicAPIKey),
			workflowSpecifiedEngine: "codex",
			want:                    string(constants.ClaudeEngine),
		},
		{
			name:                    "default-engine (Copilot) secret falls through to workflow preference",
			existingSecrets:         makeSecrets(constants.CopilotGitHubToken),
			workflowSpecifiedEngine: string(constants.ClaudeEngine),
			want:                    string(constants.ClaudeEngine),
		},
		{
			name:                    "default-engine secret with no workflow preference stays default",
			existingSecrets:         makeSecrets(constants.CopilotGitHubToken),
			workflowSpecifiedEngine: "",
			want:                    string(constants.DefaultEngine),
		},
		{
			name:                    "no secret defers to workflow preference",
			existingSecrets:         nil,
			workflowSpecifiedEngine: string(constants.ClaudeEngine),
			want:                    string(constants.ClaudeEngine),
		},
		{
			name:                    "no secret no workflow returns default engine",
			existingSecrets:         nil,
			workflowSpecifiedEngine: "",
			want:                    string(constants.DefaultEngine),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := &AddInteractiveConfig{
				EngineOverride:  tc.engineOverride,
				existingSecrets: tc.existingSecrets,
			}
			got := cfg.determineDefaultEngine(tc.workflowSpecifiedEngine)
			assert.Equal(t, tc.want, got)
		})
	}
}
