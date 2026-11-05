//go:build !integration

package workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnvironmentDeploymentCompilation(t *testing.T) {
	for _, location := range []string{"top-level", "safe-outputs", "override"} {
		t.Run(location, func(t *testing.T) {
			environment := map[string]any{
				"name":       "protected",
				"url":        "https://example.com",
				"deployment": false,
			}
			fields := map[string]any{
				"safe-outputs": map[string]any{
					"add-comment": nil,
					"jobs": map[string]any{
						"notify": map[string]any{
							"runs-on": "ubuntu-latest",
							"steps":   []any{map[string]any{"run": "echo testing"}},
						},
					},
				},
				"jobs": map[string]any{
					"custom": map[string]any{
						"runs-on":     "ubuntu-latest",
						"environment": environment,
						"steps":       []any{map[string]any{"run": "echo testing"}},
					},
				},
			}
			if location == "top-level" {
				fields["environment"] = environment
			} else {
				fields["safe-outputs"].(map[string]any)["environment"] = environment
				if location == "override" {
					fields["environment"] = map[string]any{"name": "agent-environment", "deployment": true}
				}
			}
			frontmatter, err := yaml.Marshal(fields)
			require.NoError(t, err)
			source := fmt.Sprintf(`---
on:
  workflow_dispatch:
  roles: [admin]
strict: false
engine: copilot
tools:
  cache-memory: true
%s---
Test environment deployment settings.
`, frontmatter)
			dir := t.TempDir()
			path := filepath.Join(dir, "test.md")
			require.NoError(t, os.WriteFile(path, []byte(source), 0600))
			require.NoError(t, NewCompiler().CompileWorkflow(path))

			content, err := os.ReadFile(filepath.Join(dir, "test.lock.yml"))
			require.NoError(t, err)
			var compiled struct {
				Jobs map[string]map[string]any `yaml:"jobs"`
			}
			require.NoError(t, yaml.Unmarshal(content, &compiled))
			safeJobs := []string{"pre_activation", "safe_outputs", "conclusion", "notify", "custom"}
			agentJobs := []string{"activation", "agent", "detection"}
			require.Contains(t, compiled.Jobs, "update_cache_memory")
			assert.NotContains(t, compiled.Jobs["update_cache_memory"], "environment")
			for _, name := range safeJobs {
				require.Contains(t, compiled.Jobs, name)
				assert.Equal(t, environment, compiled.Jobs[name]["environment"], "job %s", name)
			}
			for _, name := range agentJobs {
				require.Contains(t, compiled.Jobs, name)
				switch location {
				case "top-level":
					assert.Equal(t, environment, compiled.Jobs[name]["environment"], "job %s", name)
				case "override":
					assert.Equal(t, fields["environment"], compiled.Jobs[name]["environment"], "job %s", name)
				default:
					assert.NotContains(t, compiled.Jobs[name], "environment", "job %s", name)
				}
			}
		})
	}
}
