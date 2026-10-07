//go:build !integration

package workflow

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFrontmatterConfigGitHubAppJSON(t *testing.T) {
	var config FrontmatterConfig
	err := json.Unmarshal([]byte(`{"github-app":{"client-id":"client","private-key":"key","ignore-if-missing":true}}`), &config)
	require.NoError(t, err)
	require.NotNil(t, config.GitHubApp)
	assert.Equal(t, "client", config.GitHubApp.AppID)
	assert.Equal(t, "key", config.GitHubApp.PrivateKey)
	assert.True(t, config.GitHubApp.IgnoreIfMissing)

	data, err := json.Marshal(config)
	require.NoError(t, err)
	assert.JSONEq(t, `{"github-app":{"client-id":"client","private-key":"key","ignore-if-missing":true}}`, string(data))
}

func TestParseFrontmatterConfigMaxTurnsAndMaxRuns(t *testing.T) {
	config, err := ParseFrontmatterConfig(map[string]any{
		"name":      "test-workflow",
		"max-turns": 15,
		"max-runs":  "${{ inputs.max-runs }}",
	})
	require.NoError(t, err)
	require.NotNil(t, config.MaxTurns)
	assert.Equal(t, 15, config.MaxTurns.IntValue())
	require.NotNil(t, config.MaxRuns)
	assert.True(t, config.MaxRuns.IsExpression())
	assert.Equal(t, "${{ inputs.max-runs }}", config.MaxRuns.String())
}

func TestParseFrontmatterConfig(t *testing.T) {
	t.Run("parses minimal workflow config", func(t *testing.T) {
		frontmatter := map[string]any{
			"name":   "test-workflow",
			"engine": "claude",
		}

		config, err := ParseFrontmatterConfig(frontmatter)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if config.Name != "test-workflow" {
			t.Errorf("Name = %q, want %q", config.Name, "test-workflow")
		}

		if config.Engine != "claude" {
			t.Errorf("Engine = %q, want %q", config.Engine, "claude")
		}
	})

	t.Run("parses labels array", func(t *testing.T) {
		frontmatter := map[string]any{
			"name":   "test-workflow",
			"engine": "copilot",
			"labels": []any{"automation", "ci", "diagnostics"},
		}

		config, err := ParseFrontmatterConfig(frontmatter)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(config.Labels) != 3 {
			t.Fatalf("expected 3 labels, got %d", len(config.Labels))
		}

		expectedLabels := []string{"automation", "ci", "diagnostics"}
		for i, expected := range expectedLabels {
			if config.Labels[i] != expected {
				t.Errorf("Labels[%d] = %q, want %q", i, config.Labels[i], expected)
			}
		}
	})

	t.Run("parses skills array", func(t *testing.T) {
		frontmatter := map[string]any{
			"skills": []any{
				"githubnext/skills@1f181b37d3fe5862ab590648f25a292e345b5de6",
				"githubnext/skills/review/security@1f181b37d3fe5862ab590648f25a292e345b5de6",
			},
		}

		config, err := ParseFrontmatterConfig(frontmatter)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		require.Equal(t, []any{
			"githubnext/skills@1f181b37d3fe5862ab590648f25a292e345b5de6",
			"githubnext/skills/review/security@1f181b37d3fe5862ab590648f25a292e345b5de6",
		}, config.Skills)
		require.Equal(t, []SkillReference{
			{Skill: "githubnext/skills@1f181b37d3fe5862ab590648f25a292e345b5de6"},
			{Skill: "githubnext/skills/review/security@1f181b37d3fe5862ab590648f25a292e345b5de6"},
		}, config.SkillReferences)
	})

	t.Run("parses object-form skill entries", func(t *testing.T) {
		frontmatter := map[string]any{
			"skills": []any{
				map[string]any{
					"skill":        "githubnext/skills@1f181b37d3fe5862ab590648f25a292e345b5de6",
					"github-token": "${{ secrets.SOME_TOKEN }}",
				},
			},
		}

		config, err := ParseFrontmatterConfig(frontmatter)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		require.Len(t, config.SkillReferences, 1)
		require.Equal(t, "githubnext/skills@1f181b37d3fe5862ab590648f25a292e345b5de6", config.SkillReferences[0].Skill)
		require.Equal(t, "${{ secrets.SOME_TOKEN }}", config.SkillReferences[0].GitHubToken)
	})

	t.Run("parses top-level ambient-folders", func(t *testing.T) {
		frontmatter := map[string]any{
			"ambient-folders": []any{" docs ", "src\\lib", "docs"},
		}

		config, err := ParseFrontmatterConfig(frontmatter)
		require.NoError(t, err)

		require.Equal(t, []string{"docs", "src/lib"}, config.AmbientFolders)
		require.Equal(t, []string{"docs", "src/lib"}, config.ToMap()["ambient-folders"])

		roundTripped, err := ParseFrontmatterConfig(config.ToMap())
		require.NoError(t, err)
		require.Equal(t, config.AmbientFolders, roundTripped.AmbientFolders)
	})

	t.Run("rejects invalid top-level ambient-folders", func(t *testing.T) {
		config, err := ParseFrontmatterConfig(map[string]any{
			"ambient-folders": []any{"../docs"},
		})

		require.ErrorContains(t, err, "is not a relative path within the repository")
		assert.Nil(t, config)
	})

	t.Run("parses top-level github-app", func(t *testing.T) {
		frontmatter := map[string]any{
			"github-app": map[string]any{
				"client-id":         "${{ vars.APP_ID }}",
				"private-key":       "${{ secrets.APP_PRIVATE_KEY }}",
				"ignore-if-missing": true,
				"owner":             "github",
				"repositories":      []any{"gh-aw"},
				"permissions": map[string]any{
					"members": "read",
				},
			},
		}

		config, err := ParseFrontmatterConfig(frontmatter)
		require.NoError(t, err)
		require.NotNil(t, config.GitHubApp)

		assert.Equal(t, "${{ vars.APP_ID }}", config.GitHubApp.AppID)
		assert.Equal(t, "${{ secrets.APP_PRIVATE_KEY }}", config.GitHubApp.PrivateKey)
		assert.True(t, config.GitHubApp.IgnoreIfMissing)
		assert.Equal(t, "github", config.GitHubApp.Owner)
		assert.Equal(t, []string{"gh-aw"}, config.GitHubApp.Repositories)
		assert.Equal(t, map[string]string{"members": "read"}, config.GitHubApp.Permissions)

		reconstructedApp, ok := config.ToMap()["github-app"].(map[string]any)
		require.True(t, ok, "github-app should round-trip as a map")
		assert.Equal(t, "${{ vars.APP_ID }}", reconstructedApp["client-id"])
		assert.Equal(t, "${{ secrets.APP_PRIVATE_KEY }}", reconstructedApp["private-key"])
		assert.Equal(t, true, reconstructedApp["ignore-if-missing"])
		assert.Equal(t, "github", reconstructedApp["owner"])
		assert.Equal(t, []any{"gh-aw"}, reconstructedApp["repositories"])
		assert.Equal(t, map[string]any{"members": "read"}, reconstructedApp["permissions"])

		roundTripped, err := ParseFrontmatterConfig(config.ToMap())
		require.NoError(t, err)
		require.NotNil(t, roundTripped.GitHubApp)
		assert.Equal(t, config.GitHubApp, roundTripped.GitHubApp)

		roundTripped.GitHubApp.Repositories[0] = "other"
		roundTripped.GitHubApp.Permissions["members"] = "write"
		assert.Equal(t, []string{"gh-aw"}, config.GitHubApp.Repositories)
		assert.Equal(t, map[string]string{"members": "read"}, config.GitHubApp.Permissions)
	})

	t.Run("drops incomplete top-level github-app like compiler fallback", func(t *testing.T) {
		config, err := ParseFrontmatterConfig(map[string]any{
			"github-app": map[string]any{
				"client-id":         "${{ vars.APP_ID }}",
				"ignore-if-missing": true,
			},
		})

		require.NoError(t, err)
		assert.Nil(t, config.GitHubApp)
		assert.NotContains(t, config.ToMap(), "github-app")
	})

	t.Run("parses string map top-level github-app", func(t *testing.T) {
		config, err := ParseFrontmatterConfig(map[string]any{
			"github-app": map[string]string{
				"client-id":   "client-id",
				"private-key": "private-key",
			},
		})

		require.NoError(t, err)
		require.NotNil(t, config.GitHubApp)
		assert.Equal(t, "client-id", config.GitHubApp.AppID)
		assert.Equal(t, "private-key", config.GitHubApp.PrivateKey)
	})

	t.Run("rejects unsupported top-level github-app type", func(t *testing.T) {
		config, err := ParseFrontmatterConfig(map[string]any{"github-app": "invalid"})

		require.ErrorContains(t, err, "github-app has an unsupported type")
		assert.Nil(t, config)
	})

	t.Run("parses top-level github-app app-id alias", func(t *testing.T) {
		frontmatter := map[string]any{
			"github-app": map[string]any{
				"app-id":      "${{ vars.APP_ID }}",
				"private-key": "${{ secrets.APP_PRIVATE_KEY }}",
			},
		}

		config, err := ParseFrontmatterConfig(frontmatter)
		require.NoError(t, err)
		require.NotNil(t, config.GitHubApp)
		assert.Equal(t, "${{ vars.APP_ID }}", config.GitHubApp.AppID)

		reconstructedApp, ok := config.ToMap()["github-app"].(map[string]any)
		require.True(t, ok, "github-app should round-trip as a map")
		assert.Equal(t, "${{ vars.APP_ID }}", reconstructedApp["client-id"])
		assert.NotContains(t, reconstructedApp, "app-id")
	})

	t.Run("parses complete workflow config", func(t *testing.T) {
		frontmatter := map[string]any{
			"name":        "full-workflow",
			"description": "A complete workflow",
			"metadata": map[string]any{
				"docs": "https://docs.example.com/full-workflow",
			},
			"engine":     "copilot",
			"source":     "owner/repo/path@main",
			"redirect":   "owner/repo/new-path@main",
			"tracker-id": "test-tracker-123",
			"tools": map[string]any{
				"bash": map[string]any{
					"enabled": true,
				},
			},
			"mcp-servers": map[string]any{
				"github": map[string]any{
					"mode": "remote",
				},
			},
			"safe-outputs": map[string]any{
				"create-issue": map[string]any{
					"enabled": true,
				},
			},
		}

		config, err := ParseFrontmatterConfig(frontmatter)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Check core fields
		if config.Name != "full-workflow" {
			t.Errorf("Name = %q, want %q", config.Name, "full-workflow")
		}

		if config.Description != "A complete workflow" {
			t.Errorf("Description = %q, want %q", config.Description, "A complete workflow")
		}
		if config.Metadata["docs"] != "https://docs.example.com/full-workflow" {
			t.Errorf("Metadata docs = %q, want documentation URL", config.Metadata["docs"])
		}

		if config.Engine != "copilot" {
			t.Errorf("Engine = %q, want %q", config.Engine, "copilot")
		}

		if config.Source != "owner/repo/path@main" {
			t.Errorf("Source = %q, want %q", config.Source, "owner/repo/path@main")
		}
		if config.Redirect != "owner/repo/new-path@main" {
			t.Errorf("Redirect = %q, want %q", config.Redirect, "owner/repo/new-path@main")
		}

		if config.TrackerID != "test-tracker-123" {
			t.Errorf("TrackerID = %q, want %q", config.TrackerID, "test-tracker-123")
		}

		// Check nested configuration sections
		if config.Tools == nil {
			t.Error("Tools should not be nil")
		}

		if config.MCPServers == nil {
			t.Error("MCPServers should not be nil")
		}

		if config.SafeOutputs == nil {
			t.Error("SafeOutputs should not be nil")
		}
	})

	t.Run("parses on.needs config", func(t *testing.T) {
		frontmatter := map[string]any{
			"on": map[string]any{
				"workflow_dispatch": map[string]any{},
				"needs":             []any{"secrets_fetcher", "config_loader"},
			},
		}

		config, err := ParseFrontmatterConfig(frontmatter)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(config.OnNeeds) != 2 {
			t.Fatalf("expected 2 on.needs entries, got %d", len(config.OnNeeds))
		}
		if config.OnNeeds[0] != "secrets_fetcher" || config.OnNeeds[1] != "config_loader" {
			t.Fatalf("unexpected on.needs entries: %#v", config.OnNeeds)
		}
	})

	t.Run("handles timeout-minutes as int", func(t *testing.T) {
		frontmatter := map[string]any{
			"timeout-minutes": 60,
		}

		config, err := ParseFrontmatterConfig(frontmatter)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if config.TimeoutMinutes == nil {
			t.Fatal("TimeoutMinutes should not be nil")
		}
		if config.TimeoutMinutes.IntValue() != 60 {
			t.Errorf("TimeoutMinutes.IntValue() = %d, want 60", config.TimeoutMinutes.IntValue())
		}
		if config.TimeoutMinutes.IsExpression() {
			t.Error("TimeoutMinutes.IsExpression() should be false for an integer literal")
		}
	})

	t.Run("handles timeout-minutes as expression string", func(t *testing.T) {
		frontmatter := map[string]any{
			"timeout-minutes": "${{ inputs.timeout }}",
		}

		config, err := ParseFrontmatterConfig(frontmatter)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if config.TimeoutMinutes == nil {
			t.Fatal("TimeoutMinutes should not be nil")
		}
		if config.TimeoutMinutes.String() != "${{ inputs.timeout }}" {
			t.Errorf("TimeoutMinutes.String() = %q, want %q", config.TimeoutMinutes.String(), "${{ inputs.timeout }}")
		}
		if !config.TimeoutMinutes.IsExpression() {
			t.Error("TimeoutMinutes.IsExpression() should be true for an expression")
		}
	})

	t.Run("rejects timeout-minutes as float", func(t *testing.T) {
		frontmatter := map[string]any{
			"timeout-minutes": 5.5,
		}

		_, err := ParseFrontmatterConfig(frontmatter)
		if err == nil {
			t.Error("expected error for float timeout-minutes, got nil")
		}
	})

	t.Run("rejects timeout-minutes as free-form string", func(t *testing.T) {
		frontmatter := map[string]any{
			"timeout-minutes": "not-an-expression",
		}

		_, err := ParseFrontmatterConfig(frontmatter)
		if err == nil {
			t.Error("expected error for non-expression string timeout-minutes, got nil")
			return
		}
		if !strings.Contains(err.Error(), "must be an integer or a GitHub Actions expression") {
			t.Errorf("error message should describe the integer/expression requirement, got: %v", err)
		}
	})

	t.Run("handles empty frontmatter", func(t *testing.T) {
		frontmatter := map[string]any{}

		config, err := ParseFrontmatterConfig(frontmatter)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if config.Name != "" {
			t.Errorf("Name should be empty, got %q", config.Name)
		}

		if config.Tools != nil {
			t.Errorf("Tools should be nil for empty frontmatter, got %v", config.Tools)
		}
	})

	t.Run("handles network configuration", func(t *testing.T) {
		frontmatter := map[string]any{
			"network": map[string]any{
				"allowed": []any{"github.com", "api.github.com"},
				"firewall": map[string]any{
					"enabled": true,
				},
			},
		}

		config, err := ParseFrontmatterConfig(frontmatter)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if config.Network == nil {
			t.Fatal("Network should not be nil")
		}

		// Check that allowed domains are present
		if len(config.Network.Allowed) != 2 {
			t.Errorf("expected 2 allowed domains, got %d", len(config.Network.Allowed))
		}
	})

	t.Run("handles sandbox configuration", func(t *testing.T) {
		frontmatter := map[string]any{
			"sandbox": map[string]any{
				"agent": map[string]any{
					"type":     "awf",
					"platform": "ghes",
				},
			},
		}

		config, err := ParseFrontmatterConfig(frontmatter)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if config.Sandbox == nil {
			t.Fatal("Sandbox should not be nil")
		}
		if config.Sandbox.Agent == nil {
			t.Fatal("Sandbox.Agent should not be nil")
		}
		if config.Sandbox.Agent.Platform != "ghes" {
			t.Fatalf("Sandbox.Agent.Platform = %q, want %q", config.Sandbox.Agent.Platform, "ghes")
		}
	})

	t.Run("handles observability configuration", func(t *testing.T) {
		frontmatter := map[string]any{
			"observability": map[string]any{
				"otlp": map[string]any{
					"endpoint": "https://traces.example.com",
				},
			},
		}

		config, err := ParseFrontmatterConfig(frontmatter)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if config.Observability == nil {
			t.Fatal("Observability should not be nil")
		}

		if config.Observability.OTLP == nil {
			t.Fatal("OTLP should not be nil")
		}
	})

	t.Run("handles jobs configuration", func(t *testing.T) {
		frontmatter := map[string]any{
			"jobs": map[string]any{
				"test-job": map[string]any{
					"runs-on": "ubuntu-latest",
					"steps": []any{
						map[string]any{
							"run": "echo hello",
						},
					},
				},
			},
		}

		config, err := ParseFrontmatterConfig(frontmatter)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if config.Jobs == nil {
			t.Fatal("Jobs should not be nil")
		}

		if _, ok := config.Jobs["test-job"]; !ok {
			t.Error("test-job should exist in Jobs")
		}
	})

	t.Run("handles top-level runs-on forms", func(t *testing.T) {
		tests := []struct {
			name      string
			runsOn    any
			assertion func(t *testing.T, got any)
		}{
			{
				name:   "string form",
				runsOn: "ubuntu-latest",
				assertion: func(t *testing.T, got any) {
					parsed, ok := got.(string)
					if !ok {
						t.Fatalf("RunsOn should be preserved as a string, got %T", got)
					}
					if parsed != "ubuntu-latest" {
						t.Errorf("RunsOn = %v, want ubuntu-latest", parsed)
					}
				},
			},
			{
				name:   "array form",
				runsOn: []any{"self-hosted", "linux"},
				assertion: func(t *testing.T, got any) {
					parsed, ok := got.([]any)
					if !ok {
						t.Fatalf("RunsOn should be preserved as a slice, got %T", got)
					}
					if len(parsed) != 2 || parsed[0] != "self-hosted" || parsed[1] != "linux" {
						t.Errorf("RunsOn = %v, want [self-hosted linux]", parsed)
					}
				},
			},
			{
				name: "object form",
				runsOn: map[string]any{
					"group":  "arc-custom",
					"labels": []any{"self-hosted", "linux"},
				},
				assertion: func(t *testing.T, got any) {
					parsed, ok := got.(map[string]any)
					if !ok {
						t.Fatalf("RunsOn should be preserved as a map, got %T", got)
					}
					if parsed["group"] != "arc-custom" {
						t.Errorf("RunsOn group = %v, want arc-custom", parsed["group"])
					}
				},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				frontmatter := map[string]any{
					"runs-on": tt.runsOn,
				}

				config, err := ParseFrontmatterConfig(frontmatter)
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}

				tt.assertion(t, config.RunsOn)

				reconstructed := config.ToMap()
				tt.assertion(t, reconstructed["runs-on"])

				config2, err := ParseFrontmatterConfig(reconstructed)
				require.NoError(t, err)
				tt.assertion(t, config2.RunsOn)

				reconstructed2 := config2.ToMap()
				assert.Equal(t, reconstructed, reconstructed2)
			})

			t.Run("handles safe-outputs runs-on forms", func(t *testing.T) {
				tests := []struct {
					name           string
					safeOutputs    any
					expectedRunsOn string
				}{
					{
						name: "safe-outputs.runs-on string form",
						safeOutputs: map[string]any{
							"runs-on": "ubuntu-latest",
						},
						expectedRunsOn: "runs-on: ubuntu-latest",
					},
					{
						name: "safe-outputs.runs-on array form",
						safeOutputs: map[string]any{
							"runs-on": []any{"self-hosted", "linux"},
						},
						expectedRunsOn: "runs-on:\n  - self-hosted\n  - linux",
					},
				}

				for _, tt := range tests {
					t.Run(tt.name, func(t *testing.T) {
						config, err := ParseFrontmatterConfig(map[string]any{
							"safe-outputs": tt.safeOutputs,
						})
						require.NoError(t, err)
						require.NotNil(t, config.SafeOutputs)
						assert.Equal(t, tt.expectedRunsOn, config.SafeOutputs.RunsOn)
					})
				}
			})
		}
	})

	t.Run("rejects invalid top-level runs-on forms", func(t *testing.T) {
		tests := []struct {
			name        string
			runsOn      any
			errContains string
		}{
			{
				name:        "number form",
				runsOn:      42,
				errContains: "expected a string, array of strings, or object",
			},
			{
				name:        "array contains non-string",
				runsOn:      []any{"self-hosted", 42},
				errContains: "expected a string label",
			},
			{
				name:        "object contains unknown key",
				runsOn:      map[string]any{"unknown": "value"},
				errContains: "is not supported, expected 'group' or 'labels'",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				_, err := ParseFrontmatterConfig(map[string]any{
					"runs-on": tt.runsOn,
				})
				require.Error(t, err)
				require.ErrorContains(t, err, tt.errContains)
			})
		}
	})

	t.Run("omits typed-nil top-level runs-on during serialization", func(t *testing.T) {
		var runsOn *string
		config := &FrontmatterConfig{
			RunsOn: runsOn,
		}

		reconstructed := config.ToMap()
		_, ok := reconstructed["runs-on"]
		assert.False(t, ok)
	})

	t.Run("preserves complex nested structures", func(t *testing.T) {
		frontmatter := map[string]any{
			"safe-outputs": map[string]any{
				"jobs": map[string]any{
					"custom-job": map[string]any{
						"conditions": []any{
							map[string]any{
								"field": "status",
								"value": "success",
							},
						},
					},
				},
			},
		}

		config, err := ParseFrontmatterConfig(frontmatter)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if config.SafeOutputs == nil {
			t.Fatal("SafeOutputs should not be nil")
		}

		if config.SafeOutputs.Jobs == nil {
			t.Fatal("SafeOutputs.Jobs should not be nil")
		}

		customJob, ok := config.SafeOutputs.Jobs["custom-job"]
		if !ok {
			t.Fatal("custom-job should exist in SafeOutputs.Jobs")
		}

		if customJob == nil {
			t.Fatal("custom-job should not be nil")
		}
	})
}

func TestFrontmatterConfigFieldExtraction(t *testing.T) {
	t.Run("extracts tools using struct", func(t *testing.T) {
		frontmatter := map[string]any{
			"tools": map[string]any{
				"bash": map[string]any{
					"enabled": true,
				},
				"playwright": map[string]any{
					"version": "v1.41.0",
				},
			},
		}

		config, err := ParseFrontmatterConfig(frontmatter)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Verify tools can be accessed via ToMap()
		if config.Tools == nil {
			t.Fatal("Tools should not be nil")
		}

		toolsMap := config.Tools.ToMap()
		if len(toolsMap) < 2 {
			t.Errorf("expected at least 2 tools, got %d", len(toolsMap))
		}

		if _, ok := toolsMap["bash"]; !ok {
			t.Error("bash tool should exist")
		}

		if _, ok := toolsMap["playwright"]; !ok {
			t.Error("playwright tool should exist")
		}
	})

	t.Run("extracts mcp-servers using struct", func(t *testing.T) {
		frontmatter := map[string]any{
			"mcp-servers": map[string]any{
				"github": map[string]any{
					"mode":     "remote",
					"toolsets": []any{"repos", "issues"},
				},
			},
		}

		config, err := ParseFrontmatterConfig(frontmatter)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(config.MCPServers) != 1 {
			t.Errorf("expected 1 mcp server, got %d", len(config.MCPServers))
		}

		if _, ok := config.MCPServers["github"]; !ok {
			t.Error("github mcp server should exist")
		}
	})

	t.Run("extracts runtimes using struct", func(t *testing.T) {
		frontmatter := map[string]any{
			"runtimes": map[string]any{
				"node": map[string]any{
					"version": "24",
				},
			},
		}

		config, err := ParseFrontmatterConfig(frontmatter)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(config.Runtimes) != 1 {
			t.Errorf("expected 1 runtime, got %d", len(config.Runtimes))
		}

		if _, ok := config.Runtimes["node"]; !ok {
			t.Error("node runtime should exist")
		}
	})

	t.Run("parses object-form experiments without unmarshal error", func(t *testing.T) {
		frontmatter := map[string]any{
			"experiments": map[string]any{
				// Object form: must not cause json.Unmarshal to fail.
				"prompt_style": map[string]any{
					"variants": []any{"concise", "verbose"},
					"weight":   []any{70.0, 30.0},
				},
				// Bare-array form: must still work alongside the object form.
				"caveman": []any{"yes", "no"},
			},
		}

		config, err := ParseFrontmatterConfig(frontmatter)
		if err != nil {
			t.Fatalf("ParseFrontmatterConfig should not fail on object-form experiments: %v", err)
		}

		if config.ExperimentConfigs == nil {
			t.Fatal("ExperimentConfigs should be populated")
		}
		if len(config.ExperimentConfigs) != 2 {
			t.Errorf("expected 2 experiment configs, got %d", len(config.ExperimentConfigs))
		}
		ps := config.ExperimentConfigs["prompt_style"]
		if ps == nil {
			t.Fatal("prompt_style config should exist")
		}
		if len(ps.Variants) != 2 || ps.Variants[0] != "concise" {
			t.Errorf("unexpected variants: %v", ps.Variants)
		}
		if len(ps.Weight) != 2 || ps.Weight[0] != 70 {
			t.Errorf("unexpected weight: %v", ps.Weight)
		}
	})
}

func TestFrontmatterConfigBackwardCompatibility(t *testing.T) {
	// This test ensures that the new structured types work with existing
	// frontmatter extraction patterns used throughout the codebase

	t.Run("compatible with ExtractMapField pattern", func(t *testing.T) {
		frontmatter := map[string]any{
			"tools": map[string]any{
				"bash": map[string]any{
					"enabled": true,
				},
			},
		}

		// Old pattern: ExtractMapField
		oldResult := ExtractMapField(frontmatter, "tools")

		// New pattern: Parse then access field
		config, err := ParseFrontmatterConfig(frontmatter)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Convert tools back to map for comparison
		newResult := config.Tools.ToMap()

		// Both should have the same tool
		if _, oldOk := oldResult["bash"]; !oldOk {
			t.Error("old pattern missing bash tool")
		}
		if _, newOk := newResult["bash"]; !newOk {
			t.Error("new pattern missing bash tool")
		}
	})

	t.Run("supports round-trip conversion", func(t *testing.T) {
		originalFrontmatter := map[string]any{
			"name":            "test-workflow",
			"engine":          "copilot",
			"description":     "A test workflow",
			"tracker-id":      "test-tracker-12345678",
			"timeout-minutes": 30,
			"on": map[string]any{
				"issues": map[string]any{
					"types": []any{"opened", "labeled"},
				},
			},
			"env": map[string]string{
				"MY_VAR": "value",
			},
		}

		// Parse to struct
		config, err := ParseFrontmatterConfig(originalFrontmatter)
		if err != nil {
			t.Fatalf("parse error: %v", err)
		}

		// Convert back to map
		reconstructed := config.ToMap()

		// Verify key fields are preserved
		if reconstructed["name"] != "test-workflow" {
			t.Errorf("name mismatch: got %v", reconstructed["name"])
		}
		if reconstructed["engine"] != "copilot" {
			t.Errorf("engine mismatch: got %v", reconstructed["engine"])
		}
		if reconstructed["description"] != "A test workflow" {
			t.Errorf("description mismatch: got %v", reconstructed["description"])
		}
		if reconstructed["tracker-id"] != "test-tracker-12345678" {
			t.Errorf("tracker-id mismatch: got %v", reconstructed["tracker-id"])
		}
		if reconstructed["timeout-minutes"] != 30 {
			t.Errorf("timeout-minutes mismatch: got %v", reconstructed["timeout-minutes"])
		}

		// Verify nested structures
		if reconstructed["on"] == nil {
			t.Error("on should not be nil")
		}
		if reconstructed["env"] == nil {
			t.Error("env should not be nil")
		}
	})

	t.Run("round-trip preserves timeout-minutes expression", func(t *testing.T) {
		frontmatter := map[string]any{
			"name":            "test-workflow",
			"engine":          "copilot",
			"timeout-minutes": "${{ inputs.timeout }}",
		}

		config, err := ParseFrontmatterConfig(frontmatter)
		if err != nil {
			t.Fatalf("parse error: %v", err)
		}

		reconstructed := config.ToMap()

		if reconstructed["timeout-minutes"] != "${{ inputs.timeout }}" {
			t.Errorf("timeout-minutes expression mismatch: got %v (%T)", reconstructed["timeout-minutes"], reconstructed["timeout-minutes"])
		}
	})

	t.Run("round-trip preserves labels", func(t *testing.T) {
		frontmatter := map[string]any{
			"name":   "test-workflow",
			"engine": "copilot",
			"labels": []any{"automation", "ci", "security"},
		}

		// Parse to struct
		config, err := ParseFrontmatterConfig(frontmatter)
		if err != nil {
			t.Fatalf("parse error: %v", err)
		}

		// Convert back to map
		reconstructed := config.ToMap()

		// Verify labels are preserved
		labels, ok := reconstructed["labels"].([]string)
		if !ok {
			t.Fatalf("labels should be []string, got %T", reconstructed["labels"])
		}

		if len(labels) != 3 {
			t.Fatalf("expected 3 labels, got %d", len(labels))
		}

		expectedLabels := []string{"automation", "ci", "security"}
		for i, expected := range expectedLabels {
			if labels[i] != expected {
				t.Errorf("labels[%d] = %q, want %q", i, labels[i], expected)
			}
		}
	})

	t.Run("preserves strongly-typed fields", func(t *testing.T) {
		frontmatter := map[string]any{
			"network": map[string]any{
				"allowed": []any{"github.com", "api.github.com"},
			},
			"sandbox": map[string]any{
				"agent": map[string]any{
					"type": "awf",
				},
			},
			"safe-outputs": map[string]any{
				"create-issue": map[string]any{
					"max": 5,
				},
			},
			"mcp-scripts": map[string]any{
				"mode": "http",
			},
		}

		config, err := ParseFrontmatterConfig(frontmatter)
		if err != nil {
			t.Fatalf("parse error: %v", err)
		}

		// Verify strongly-typed fields are populated
		if config.Network == nil {
			t.Error("Network should be strongly typed")
		}
		if config.Sandbox == nil {
			t.Error("Sandbox should be strongly typed")
		}
		if config.SafeOutputs == nil {
			t.Error("SafeOutputs should be strongly typed")
		}
		if config.MCPScripts == nil {
			t.Error("MCPScripts should be strongly typed")
		}

		// Convert back and verify they're preserved
		reconstructed := config.ToMap()
		if reconstructed["network"] == nil {
			t.Error("network should be preserved in ToMap")
		}
		if reconstructed["sandbox"] == nil {
			t.Error("sandbox should be preserved in ToMap")
		}
		if reconstructed["safe-outputs"] == nil {
			t.Error("safe-outputs should be preserved in ToMap")
		}
		if reconstructed["mcp-scripts"] == nil {
			t.Error("mcp-scripts should be preserved in ToMap")
		}
	})
}

// TestFrontmatterConfigTypeSafety demonstrates compile-time type safety benefits
func TestFrontmatterConfigTypeSafety(t *testing.T) {
	t.Run("strongly-typed Tools field provides compile-time safety", func(t *testing.T) {
		frontmatter := map[string]any{
			"tools": map[string]any{
				"github": map[string]any{
					"mode":     "remote",
					"toolsets": []any{"repos", "issues"},
				},
			},
		}

		config, err := ParseFrontmatterConfig(frontmatter)
		if err != nil {
			t.Fatalf("parse error: %v", err)
		}

		// Strong typing allows direct access without type assertions
		if config.Tools != nil {
			// Tools is *ToolsConfig, not map[string]any
			// This provides compile-time validation
			toolsMap := config.Tools.ToMap()
			if _, ok := toolsMap["github"]; !ok {
				t.Error("github tool should exist")
			}
		} else {
			t.Error("Tools should not be nil")
		}
	})

	t.Run("strongly-typed Network field eliminates type assertions", func(t *testing.T) {
		frontmatter := map[string]any{
			"network": map[string]any{
				"allowed": []any{"github.com", "api.github.com"},
			},
		}

		config, err := ParseFrontmatterConfig(frontmatter)
		if err != nil {
			t.Fatalf("parse error: %v", err)
		}

		// Old pattern required: networkMap, ok := config["network"].(map[string]any)
		// New pattern: direct access to strongly-typed field
		if config.Network == nil {
			t.Fatal("Network should not be nil")
		}

		// Access allowed domains directly without type assertion
		if len(config.Network.Allowed) != 2 {
			t.Errorf("expected 2 allowed domains, got %d", len(config.Network.Allowed))
		}

		if config.Network.Allowed[0] != "github.com" {
			t.Errorf("first domain should be github.com, got %s", config.Network.Allowed[0])
		}
	})

	t.Run("strongly-typed SafeOutputs eliminates nested type assertions", func(t *testing.T) {
		frontmatter := map[string]any{
			"safe-outputs": map[string]any{
				"staged": true,
			},
		}

		config, err := ParseFrontmatterConfig(frontmatter)
		if err != nil {
			t.Fatalf("parse error: %v", err)
		}

		// Strong typing provides direct access to nested config
		if config.SafeOutputs == nil {
			t.Fatal("SafeOutputs should not be nil")
		}

		// Access staged flag directly
		if !templatableBoolIsTrue(config.SafeOutputs.Staged) {
			t.Error("Staged should be true")
		}
	})

	t.Run("Env field is type-safe map[string]string", func(t *testing.T) {
		frontmatter := map[string]any{
			"env": map[string]any{
				"VAR1": "value1",
				"VAR2": "value2",
			},
		}

		config, err := ParseFrontmatterConfig(frontmatter)
		if err != nil {
			t.Fatalf("parse error: %v", err)
		}

		// Env is map[string]string, not map[string]any
		if len(config.Env) != 2 {
			t.Errorf("expected 2 env vars, got %d", len(config.Env))
		}

		// Direct string access without type assertion
		if config.Env["VAR1"] != "value1" {
			t.Errorf("VAR1 should be 'value1', got %s", config.Env["VAR1"])
		}
	})
}

// TestFrontmatterConfigEdgeCases tests edge cases and error handling
func TestFrontmatterConfigEdgeCases(t *testing.T) {
	t.Run("handles nil values gracefully", func(t *testing.T) {
		frontmatter := map[string]any{
			"name":  "test",
			"tools": nil,
		}

		config, err := ParseFrontmatterConfig(frontmatter)
		if err != nil {
			t.Fatalf("parse error: %v", err)
		}

		if config.Name != "test" {
			t.Errorf("name should be 'test', got %s", config.Name)
		}

		// Nil tools should result in nil Tools field
		if config.Tools != nil {
			t.Error("Tools should be nil when frontmatter has nil tools")
		}
	})

	t.Run("handles missing optional fields", func(t *testing.T) {
		frontmatter := map[string]any{
			"name": "minimal-workflow",
		}

		config, err := ParseFrontmatterConfig(frontmatter)
		if err != nil {
			t.Fatalf("parse error: %v", err)
		}

		// All optional fields should be nil/empty
		if config.Tools != nil {
			t.Error("Tools should be nil for minimal config")
		}
		if config.Network != nil {
			t.Error("Network should be nil for minimal config")
		}
		if config.SafeOutputs != nil {
			t.Error("SafeOutputs should be nil for minimal config")
		}
	})

	t.Run("handles Strict pointer correctly", func(t *testing.T) {
		// Test with explicit false
		frontmatter1 := map[string]any{
			"strict": false,
		}
		config1, err := ParseFrontmatterConfig(frontmatter1)
		if err != nil {
			t.Fatalf("parse error: %v", err)
		}
		if config1.Strict == nil {
			t.Error("Strict should not be nil when explicitly set to false")
		}
		if *config1.Strict != false {
			t.Error("Strict should be false")
		}

		// Test with explicit true
		frontmatter2 := map[string]any{
			"strict": true,
		}
		config2, err := ParseFrontmatterConfig(frontmatter2)
		if err != nil {
			t.Fatalf("parse error: %v", err)
		}
		if config2.Strict == nil {
			t.Error("Strict should not be nil when explicitly set to true")
		}
		if *config2.Strict != true {
			t.Error("Strict should be true")
		}

		// Test with missing field (should be nil)
		frontmatter3 := map[string]any{
			"name": "test",
		}
		config3, err := ParseFrontmatterConfig(frontmatter3)
		if err != nil {
			t.Fatalf("parse error: %v", err)
		}
		if config3.Strict != nil {
			t.Error("Strict should be nil when not specified")
		}
	})
}

// TestFrontmatterConfigIntegration tests integration with existing extraction functions
func TestFrontmatterConfigIntegration(t *testing.T) {
	t.Run("integrates with ExtractMapField helper", func(t *testing.T) {
		frontmatter := map[string]any{
			"tools": map[string]any{
				"github": map[string]any{
					"mode": "remote",
				},
			},
			"runtimes": map[string]any{
				"node": map[string]any{
					"version": "24",
				},
			},
		}

		// Old pattern still works for backward compatibility
		tools := ExtractMapField(frontmatter, "tools")
		if len(tools) == 0 {
			t.Error("ExtractMapField should return tools")
		}

		// New pattern with strong typing
		config, err := ParseFrontmatterConfig(frontmatter)
		if err != nil {
			t.Fatalf("parse error: %v", err)
		}

		if config.Tools == nil {
			t.Error("Tools should be parsed")
		}
		if config.Runtimes == nil {
			t.Error("Runtimes should be parsed")
		}
	})

	t.Run("ToMap produces compatible output for legacy code", func(t *testing.T) {
		frontmatter := map[string]any{
			"name":        "test-workflow",
			"description": "A test",
			"engine":      "copilot",
			"network": map[string]any{
				"allowed": []any{"github.com"},
			},
		}

		config, err := ParseFrontmatterConfig(frontmatter)
		if err != nil {
			t.Fatalf("parse error: %v", err)
		}

		// Convert back to map
		reconstructed := config.ToMap()

		// Legacy code can use this map
		if name, ok := reconstructed["name"].(string); !ok || name != "test-workflow" {
			t.Errorf("name should be reconstructed correctly")
		}

		if network := reconstructed["network"]; network == nil {
			t.Error("network should be reconstructed")
		}
	})

	t.Run("ToMap preserves network allowed-input with defaults shorthand", func(t *testing.T) {
		config := &FrontmatterConfig{
			Network: &NetworkPermissions{
				Allowed:      []string{"defaults"},
				AllowedInput: true,
			},
		}

		reconstructed := config.ToMap()
		networkMap, ok := reconstructed["network"].(map[string]any)
		require.True(t, ok, "network should remain a map when allowed-input is enabled")
		assert.Equal(t, true, networkMap["allowed-input"], "allowed-input should be preserved")
		assert.Equal(t, []string{"defaults"}, networkMap["allowed"], "allowed list should be preserved")
	})

	t.Run("ToMap omits empty runs-on-slim", func(t *testing.T) {
		config := &FrontmatterConfig{
			RunsOnSlim: "",
		}

		reconstructed := config.ToMap()
		_, ok := reconstructed["runs-on-slim"]
		assert.False(t, ok, "empty runs-on-slim should be omitted")
	})
}

// TestRuntimesConfigTyping tests the new typed RuntimesConfig field
func TestRuntimesConfigTyping(t *testing.T) {
	t.Run("parses node runtime with string version", func(t *testing.T) {
		frontmatter := map[string]any{
			"runtimes": map[string]any{
				"node": map[string]any{
					"version": "24",
				},
			},
		}

		config, err := ParseFrontmatterConfig(frontmatter)
		if err != nil {
			t.Fatalf("parse error: %v", err)
		}

		// Check that RuntimesTyped is populated
		if config.RuntimesTyped == nil {
			t.Fatal("RuntimesTyped should not be nil")
		}

		if config.RuntimesTyped.Node == nil {
			t.Fatal("RuntimesTyped.Node should not be nil")
		}

		if config.RuntimesTyped.Node.Version != "24" {
			t.Errorf("Node version = %s, want 24", config.RuntimesTyped.Node.Version)
		}

		// Legacy field should still be populated
		if config.Runtimes == nil {
			t.Error("Legacy Runtimes field should still be populated")
		}
	})

	t.Run("parses python runtime with numeric version", func(t *testing.T) {
		frontmatter := map[string]any{
			"runtimes": map[string]any{
				"python": map[string]any{
					"version": 3.11,
				},
			},
		}

		config, err := ParseFrontmatterConfig(frontmatter)
		if err != nil {
			t.Fatalf("parse error: %v", err)
		}

		if config.RuntimesTyped == nil {
			t.Fatal("RuntimesTyped should not be nil")
		}

		if config.RuntimesTyped.Python == nil {
			t.Fatal("RuntimesTyped.Python should not be nil")
		}

		if config.RuntimesTyped.Python.Version != "3.11" {
			t.Errorf("Python version = %s, want 3.11", config.RuntimesTyped.Python.Version)
		}
	})

	t.Run("parses multiple runtimes", func(t *testing.T) {
		frontmatter := map[string]any{
			"runtimes": map[string]any{
				"node": map[string]any{
					"version": "24",
				},
				"python": map[string]any{
					"version": "3.11",
				},
				"go": map[string]any{
					"version": "1.21",
				},
			},
		}

		config, err := ParseFrontmatterConfig(frontmatter)
		if err != nil {
			t.Fatalf("parse error: %v", err)
		}

		if config.RuntimesTyped == nil {
			t.Fatal("RuntimesTyped should not be nil")
		}

		if config.RuntimesTyped.Node == nil {
			t.Error("Node runtime should be set")
		}
		if config.RuntimesTyped.Python == nil {
			t.Error("Python runtime should be set")
		}
		if config.RuntimesTyped.Go == nil {
			t.Error("Go runtime should be set")
		}
	})

	t.Run("converts RuntimesTyped back to map", func(t *testing.T) {
		frontmatter := map[string]any{
			"name": "test-workflow",
			"runtimes": map[string]any{
				"node": map[string]any{
					"version": "24",
				},
			},
		}

		config, err := ParseFrontmatterConfig(frontmatter)
		if err != nil {
			t.Fatalf("parse error: %v", err)
		}

		// Convert back to map
		reconstructed := config.ToMap()

		// Check that runtimes is preserved
		runtimes, ok := reconstructed["runtimes"].(map[string]any)
		if !ok {
			t.Fatal("runtimes should be in reconstructed map")
		}

		node, ok := runtimes["node"].(map[string]any)
		if !ok {
			t.Fatal("node runtime should be in runtimes map")
		}

		if node["version"] != "24" {
			t.Errorf("node version = %v, want 24", node["version"])
		}
	})

	t.Run("handles all runtime types", func(t *testing.T) {
		frontmatter := map[string]any{
			"runtimes": map[string]any{
				"node":   map[string]any{"version": "24"},
				"python": map[string]any{"version": "3.11"},
				"go":     map[string]any{"version": "1.21"},
				"uv":     map[string]any{"version": "0.1.0"},
				"bun":    map[string]any{"version": "1.0.0"},
				"deno":   map[string]any{"version": "1.40"},
			},
		}

		config, err := ParseFrontmatterConfig(frontmatter)
		if err != nil {
			t.Fatalf("parse error: %v", err)
		}

		if config.RuntimesTyped == nil {
			t.Fatal("RuntimesTyped should not be nil")
		}

		// Check all runtimes are parsed
		if config.RuntimesTyped.Node == nil {
			t.Error("Node should be set")
		}
		if config.RuntimesTyped.Python == nil {
			t.Error("Python should be set")
		}
		if config.RuntimesTyped.Go == nil {
			t.Error("Go should be set")
		}
		if config.RuntimesTyped.UV == nil {
			t.Error("UV should be set")
		}
		if config.RuntimesTyped.Bun == nil {
			t.Error("Bun should be set")
		}
		if config.RuntimesTyped.Deno == nil {
			t.Error("Deno should be set")
		}
	})
}

// TestPermissionsConfigTyping tests the new typed PermissionsConfig field
func TestPermissionsConfigTyping(t *testing.T) {
	t.Run("parses shorthand read-all permission", func(t *testing.T) {
		frontmatter := map[string]any{
			"permissions": map[string]any{
				"read-all": "read-all",
			},
		}

		config, err := ParseFrontmatterConfig(frontmatter)
		if err != nil {
			t.Fatalf("parse error: %v", err)
		}

		// Check that PermissionsTyped is populated
		if config.PermissionsTyped == nil {
			t.Fatal("PermissionsTyped should not be nil")
		}

		if config.PermissionsTyped.Shorthand != "read-all" {
			t.Errorf("Shorthand = %s, want read-all", config.PermissionsTyped.Shorthand)
		}

		// Legacy field should still be populated
		if config.Permissions == nil {
			t.Error("Legacy Permissions field should still be populated")
		}
	})

	t.Run("parses detailed permissions", func(t *testing.T) {
		frontmatter := map[string]any{
			"permissions": map[string]any{
				"contents":      "read",
				"issues":        "write",
				"pull-requests": "write",
			},
		}

		config, err := ParseFrontmatterConfig(frontmatter)
		if err != nil {
			t.Fatalf("parse error: %v", err)
		}

		if config.PermissionsTyped == nil {
			t.Fatal("PermissionsTyped should not be nil")
		}

		if config.PermissionsTyped.Contents != "read" {
			t.Errorf("Contents = %s, want read", config.PermissionsTyped.Contents)
		}
		if config.PermissionsTyped.Issues != "write" {
			t.Errorf("Issues = %s, want write", config.PermissionsTyped.Issues)
		}
		if config.PermissionsTyped.PullRequests != "write" {
			t.Errorf("PullRequests = %s, want write", config.PermissionsTyped.PullRequests)
		}
	})

	t.Run("parses all permission scopes", func(t *testing.T) {
		frontmatter := map[string]any{
			"permissions": map[string]any{
				"actions":               "read",
				"checks":                "write",
				"contents":              "write",
				"deployments":           "read",
				"id-token":              "write",
				"issues":                "write",
				"discussions":           "write",
				"packages":              "read",
				"pages":                 "write",
				"pull-requests":         "write",
				"repository-projects":   "write",
				"security-events":       "read",
				"statuses":              "write",
				"organization-projects": "read",
				"organization-packages": "read",
			},
		}

		config, err := ParseFrontmatterConfig(frontmatter)
		if err != nil {
			t.Fatalf("parse error: %v", err)
		}

		if config.PermissionsTyped == nil {
			t.Fatal("PermissionsTyped should not be nil")
		}

		// Check all scopes are parsed
		if config.PermissionsTyped.Actions != "read" {
			t.Error("Actions should be read")
		}
		if config.PermissionsTyped.Checks != "write" {
			t.Error("Checks should be write")
		}
		if config.PermissionsTyped.Contents != "write" {
			t.Error("Contents should be write")
		}
		if config.PermissionsTyped.Deployments != "read" {
			t.Error("Deployments should be read")
		}
		if config.PermissionsTyped.IDToken != "write" {
			t.Error("IDToken should be write")
		}
	})

	t.Run("converts PermissionsTyped back to map", func(t *testing.T) {
		frontmatter := map[string]any{
			"name": "test-workflow",
			"permissions": map[string]any{
				"contents": "read",
				"issues":   "write",
			},
		}

		config, err := ParseFrontmatterConfig(frontmatter)
		if err != nil {
			t.Fatalf("parse error: %v", err)
		}

		// Convert back to map
		reconstructed := config.ToMap()

		// Check that permissions is preserved
		permissions, ok := reconstructed["permissions"].(map[string]any)
		if !ok {
			t.Fatal("permissions should be in reconstructed map")
		}

		if permissions["contents"] != "read" {
			t.Errorf("contents = %v, want read", permissions["contents"])
		}
		if permissions["issues"] != "write" {
			t.Errorf("issues = %v, want write", permissions["issues"])
		}
	})

	t.Run("converts shorthand permissions back to map", func(t *testing.T) {
		frontmatter := map[string]any{
			"permissions": map[string]any{
				"write-all": "write-all",
			},
		}

		config, err := ParseFrontmatterConfig(frontmatter)
		if err != nil {
			t.Fatalf("parse error: %v", err)
		}

		// Convert back to map
		reconstructed := config.ToMap()

		// Check that shorthand is preserved
		permissions, ok := reconstructed["permissions"].(map[string]any)
		if !ok {
			t.Fatal("permissions should be in reconstructed map")
		}

		if permissions["write-all"] != "write-all" {
			t.Errorf("shorthand permission not preserved correctly: %v", permissions)
		}
	})
}

// TestTypedConfigsBackwardCompatibility tests backward compatibility of typed configs
func TestTypedConfigsBackwardCompatibility(t *testing.T) {
	t.Run("legacy Runtimes field still accessible", func(t *testing.T) {
		frontmatter := map[string]any{
			"runtimes": map[string]any{
				"node": map[string]any{
					"version": "24",
				},
			},
		}

		config, err := ParseFrontmatterConfig(frontmatter)
		if err != nil {
			t.Fatalf("parse error: %v", err)
		}

		// Both typed and legacy fields should be accessible
		if config.RuntimesTyped == nil {
			t.Error("RuntimesTyped should be set")
		}
		if config.Runtimes == nil {
			t.Error("Legacy Runtimes should still be set")
		}

		// Legacy field should have the same data
		if node, ok := config.Runtimes["node"].(map[string]any); ok {
			if node["version"] != "24" {
				t.Error("Legacy Runtimes field should have version 24")
			}
		} else {
			t.Error("Legacy Runtimes should have node entry")
		}
	})

	t.Run("legacy Permissions field still accessible", func(t *testing.T) {
		frontmatter := map[string]any{
			"permissions": map[string]any{
				"contents": "read",
				"issues":   "write",
			},
		}

		config, err := ParseFrontmatterConfig(frontmatter)
		if err != nil {
			t.Fatalf("parse error: %v", err)
		}

		// Both typed and legacy fields should be accessible
		if config.PermissionsTyped == nil {
			t.Error("PermissionsTyped should be set")
		}
		if config.Permissions == nil {
			t.Error("Legacy Permissions should still be set")
		}

		// Legacy field should have the same data
		if contents, ok := config.Permissions["contents"].(string); ok {
			if contents != "read" {
				t.Error("Legacy Permissions should have contents: read")
			}
		} else {
			t.Error("Legacy Permissions should have contents entry")
		}
	})

	t.Run("ToMap prefers typed fields over legacy", func(t *testing.T) {
		frontmatter := map[string]any{
			"runtimes": map[string]any{
				"node": map[string]any{
					"version": "24",
				},
			},
			"permissions": map[string]any{
				"contents": "read",
			},
		}

		config, err := ParseFrontmatterConfig(frontmatter)
		if err != nil {
			t.Fatalf("parse error: %v", err)
		}

		// Convert to map
		reconstructed := config.ToMap()

		// Check that runtimes and permissions are present
		if reconstructed["runtimes"] == nil {
			t.Error("runtimes should be in reconstructed map")
		}
		if reconstructed["permissions"] == nil {
			t.Error("permissions should be in reconstructed map")
		}
	})
}

func TestParseRuntimesConfigWithIfCondition(t *testing.T) {
	tests := []struct {
		name     string
		runtimes map[string]any
		expected map[string]RuntimeConfig
	}{
		{
			name: "runtime with if condition",
			runtimes: map[string]any{
				"go": map[string]any{
					"version": "1.25",
					"if":      "hashFiles('go.mod') != ''",
				},
			},
			expected: map[string]RuntimeConfig{
				"go": {
					Version: "1.25",
					If:      "hashFiles('go.mod') != ''",
				},
			},
		},
		{
			name: "runtime with only if condition",
			runtimes: map[string]any{
				"uv": map[string]any{
					"if": "hashFiles('uv.lock') != ''",
				},
			},
			expected: map[string]RuntimeConfig{
				"uv": {
					Version: "",
					If:      "hashFiles('uv.lock') != ''",
				},
			},
		},
		{
			name: "multiple runtimes with if conditions",
			runtimes: map[string]any{
				"go": map[string]any{
					"version": "1.25",
					"if":      "hashFiles('go.mod') != ''",
				},
				"python": map[string]any{
					"version": "3.11",
					"if":      "hashFiles('requirements.txt') != ''",
				},
				"node": map[string]any{
					"version": "24",
					"if":      "hashFiles('package.json') != ''",
				},
			},
			expected: map[string]RuntimeConfig{
				"go": {
					Version: "1.25",
					If:      "hashFiles('go.mod') != ''",
				},
				"python": {
					Version: "3.11",
					If:      "hashFiles('requirements.txt') != ''",
				},
				"node": {
					Version: "24",
					If:      "hashFiles('package.json') != ''",
				},
			},
		},
		{
			name: "runtime without if condition",
			runtimes: map[string]any{
				"node": map[string]any{
					"version": "24",
				},
			},
			expected: map[string]RuntimeConfig{
				"node": {
					Version: "24",
					If:      "",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config, err := parseRuntimesConfig(tt.runtimes)
			if err != nil {
				t.Fatalf("parseRuntimesConfig failed: %v", err)
			}

			for runtimeID, expectedConfig := range tt.expected {
				var actualConfig *RuntimeConfig
				switch runtimeID {
				case "node":
					actualConfig = config.Node
				case "python":
					actualConfig = config.Python
				case "go":
					actualConfig = config.Go
				case "uv":
					actualConfig = config.UV
				case "bun":
					actualConfig = config.Bun
				case "deno":
					actualConfig = config.Deno
				}

				if actualConfig == nil {
					t.Errorf("Runtime %s not found in config", runtimeID)
					continue
				}

				if actualConfig.Version != expectedConfig.Version {
					t.Errorf("Runtime %s version: got %q, want %q", runtimeID, actualConfig.Version, expectedConfig.Version)
				}

				if actualConfig.If != expectedConfig.If {
					t.Errorf("Runtime %s if condition: got %q, want %q", runtimeID, actualConfig.If, expectedConfig.If)
				}
			}
		})
	}
}

func TestRuntimesConfigToMapWithIfCondition(t *testing.T) {
	config := &RuntimesConfig{
		Go: &RuntimeConfig{
			Version: "1.25",
			If:      "hashFiles('go.mod') != ''",
		},
		Python: &RuntimeConfig{
			Version: "3.11",
			If:      "hashFiles('requirements.txt') != ''",
		},
		Node: &RuntimeConfig{
			Version: "24",
		},
	}

	result := runtimesConfigToMap(config)

	// Check Go runtime
	goMap, ok := result["go"].(map[string]any)
	if !ok {
		t.Fatal("go runtime not found in result")
	}
	if goMap["version"] != "1.25" {
		t.Errorf("go version: got %v, want 1.25", goMap["version"])
	}
	if goMap["if"] != "hashFiles('go.mod') != ''" {
		t.Errorf("go if condition: got %v, want hashFiles('go.mod') != ''", goMap["if"])
	}

	// Check Python runtime
	pythonMap, ok := result["python"].(map[string]any)
	if !ok {
		t.Fatal("python runtime not found in result")
	}
	if pythonMap["version"] != "3.11" {
		t.Errorf("python version: got %v, want 3.11", pythonMap["version"])
	}
	if pythonMap["if"] != "hashFiles('requirements.txt') != ''" {
		t.Errorf("python if condition: got %v, want hashFiles('requirements.txt') != ''", pythonMap["if"])
	}

	// Check Node runtime (no if condition)
	nodeMap, ok := result["node"].(map[string]any)
	if !ok {
		t.Fatal("node runtime not found in result")
	}
	if nodeMap["version"] != "24" {
		t.Errorf("node version: got %v, want 24", nodeMap["version"])
	}
	if _, hasIf := nodeMap["if"]; hasIf {
		t.Error("node should not have if condition in map")
	}
}

func TestParseRuntimesConfig_Cooldown(t *testing.T) {
	runtimes := map[string]any{
		"node": map[string]any{
			"version":  "24",
			"cooldown": false,
		},
	}

	config, err := parseRuntimesConfig(runtimes)
	if err != nil {
		t.Fatalf("parseRuntimesConfig failed: %v", err)
	}
	if config.Node == nil {
		t.Fatal("node runtime not found in config")
	}
	if config.Node.Cooldown == nil {
		t.Fatal("node cooldown should be parsed")
	}
	if *config.Node.Cooldown {
		t.Fatal("expected node cooldown to be false")
	}
}

func TestRuntimesConfigToMap_Cooldown(t *testing.T) {
	disabled := false
	result := runtimesConfigToMap(&RuntimesConfig{
		Node: &RuntimeConfig{
			Version:  "24",
			Cooldown: &disabled,
		},
	})

	nodeMap, ok := result["node"].(map[string]any)
	if !ok {
		t.Fatal("node runtime not found in result")
	}
	if nodeMap["cooldown"] != false {
		t.Fatalf("expected cooldown=false in serialized runtime map, got %v", nodeMap["cooldown"])
	}
}

func TestParseFrontmatterConfigCheckoutDisabled(t *testing.T) {
	t.Run("checkout: false sets CheckoutDisabled", func(t *testing.T) {
		frontmatter := map[string]any{
			"name":     "test-workflow",
			"engine":   "claude",
			"checkout": false,
		}

		config, err := ParseFrontmatterConfig(frontmatter)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !config.CheckoutDisabled {
			t.Error("CheckoutDisabled should be true when checkout: false is set")
		}
		if len(config.CheckoutConfigs) != 0 {
			t.Errorf("CheckoutConfigs should be empty when checkout: false is set, got %d entries", len(config.CheckoutConfigs))
		}
	})

	t.Run("checkout: true is not a valid value (treated as object)", func(t *testing.T) {
		frontmatter := map[string]any{
			"name":     "test-workflow",
			"engine":   "claude",
			"checkout": true,
		}

		config, err := ParseFrontmatterConfig(frontmatter)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// checkout: true is not recognized as a disable flag; CheckoutDisabled stays false
		if config.CheckoutDisabled {
			t.Error("CheckoutDisabled should be false when checkout: true is set")
		}
	})

	t.Run("checkout object leaves CheckoutDisabled false", func(t *testing.T) {
		frontmatter := map[string]any{
			"name":   "test-workflow",
			"engine": "claude",
			"checkout": map[string]any{
				"fetch-depth": 0,
			},
		}

		config, err := ParseFrontmatterConfig(frontmatter)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if config.CheckoutDisabled {
			t.Error("CheckoutDisabled should be false when checkout is an object")
		}
		if len(config.CheckoutConfigs) == 0 {
			t.Error("CheckoutConfigs should be populated when checkout is an object")
		}
	})
}
