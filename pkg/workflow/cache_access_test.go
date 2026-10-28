//go:build !integration

package workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/workflow/actions"
	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompiledCacheAccessBoundaries(t *testing.T) {
	for _, trigger := range []string{
		"pull_request_target",
		"issue_comment",
		"{workflow_run: {workflows: [CI], branches: [main]}}",
		"{push: {}, issue_comment: {}}",
		"{repository_dispatch: {types: [refresh]}}",
		"push",
		"workflow_dispatch",
	} {
		for _, detection := range []string{"true", "false", `"${{ vars.DETECT == 'true' }}"`} {
			t.Run(trigger+"/"+detection, func(t *testing.T) {
				compiled, content := compileCacheAccessFixture(t, trigger, detection, false, false, false)
				assert.NotContains(t, compiled, "cache-mode", "do not impose a workflow-wide cap on the publisher")
				jobs := compiled["jobs"].(map[string]any)
				agent := jobs["agent"].(map[string]any)
				agentSteps := fmt.Sprint(agent["steps"])
				assert.NotContains(t, jobs["custom"].(map[string]any), "cache-mode", "authored jobs retain their own trust boundary")
				if detection == "false" {
					assert.NotContains(t, agent, "cache-mode", "preserve same-job cache-memory writes without detection")
					assert.Contains(t, agentSteps, "actions/cache@")
					assert.NotContains(t, jobs, "update_cache_memory")
					return
				}
				assert.Equal(t, "read", agent["cache-mode"])
				assert.NotContains(t, agentSteps, "actions/cache@")
				assert.NotContains(t, agentSteps, "actions/cache/save@")
				assert.Contains(t, agentSteps, "actions/cache/restore@")
				publisher := jobs["update_cache_memory"].(map[string]any)
				assert.Equal(t, "write-only", publisher["cache-mode"])
				assert.Equal(t, map[string]any{"actions": "write", "contents": "read"}, publisher["permissions"])
				condition := strings.Join(strings.Fields(publisher["if"].(string)), " ")
				assert.Equal(t, "always() && needs.detection.result == 'success' && needs.detection.outputs.detection_success == 'true' && needs.agent.result == 'success'", condition)
				assert.ElementsMatch(t, []any{"activation", "agent", "detection"}, publisher["needs"])
				publisherSteps := fmt.Sprint(publisher["steps"])
				assert.Contains(t, publisherSteps, "actions/cache/save@")
				assert.NotContains(t, publisherSteps, "actions/cache/restore@")
				native, err := actions.Parse([]byte(content))
				require.NoError(t, err, "generated job cache modes must remain readable by native lint")
				for _, job := range native.Jobs {
					if string(job.ID) == "agent" {
						assert.Equal(t, "read", job.CacheMode)
					}
				}
			})
		}
	}
}

func TestCompiledCacheAccessDryRunAndGHES(t *testing.T) {
	for _, ghes := range []bool{false, true} {
		for _, dryRun := range []bool{false, true} {
			for _, restoreOnly := range []bool{false, true} {
				t.Run(fmt.Sprintf("ghes=%t/dry-run=%t/restore-only=%t", ghes, dryRun, restoreOnly), func(t *testing.T) {
					compiled, content := compileCacheAccessFixture(t, "issue_comment", "true", ghes, dryRun, restoreOnly)
					jobs := compiled["jobs"].(map[string]any)
					agent := jobs["agent"].(map[string]any)
					if ghes {
						assert.NotContains(t, content, "cache-mode:")
					} else {
						assert.Equal(t, "read", agent["cache-mode"])
					}
					if dryRun || restoreOnly {
						assert.NotContains(t, jobs, "update_cache_memory")
						assert.NotContains(t, content, "actions/cache/save@")
					} else {
						publisher := jobs["update_cache_memory"].(map[string]any)
						if ghes {
							assert.NotContains(t, publisher, "cache-mode")
						} else {
							assert.Equal(t, "write-only", publisher["cache-mode"])
						}
					}
					if dryRun && !ghes {
						assert.Equal(t, "read", compiled["cache-mode"])
					} else {
						assert.NotContains(t, compiled, "cache-mode")
					}
				})
			}
		}
	}
}

func TestCachePublisherRequiresDetectionAcceptance(t *testing.T) {
	data := &WorkflowData{
		CacheMemoryConfig: &CacheMemoryConfig{Caches: []CacheMemoryEntry{{ID: "default"}}},
	}
	compiler := NewCompiler()
	job, err := compiler.buildUpdateCacheMemoryJob(data, true)
	require.NoError(t, err)
	require.NotNil(t, job)
	node, err := ParseExpression(stripExpressionWrapper(job.If))
	require.NoError(t, err)
	for _, agent := range []string{"success", "failure", "cancelled", "skipped", ""} {
		for _, detection := range []string{"success", "failure", "cancelled", "skipped", ""} {
			for _, accepted := range []string{"true", "false", ""} {
				values := map[string]string{
					"needs.agent.result":                        agent,
					"needs.detection.result":                    detection,
					"needs.detection.outputs.detection_success": accepted,
				}
				assert.Equal(t, agent == "success" && detection == "success" && accepted == "true",
					evaluateDetectionRedactionGate(t, node, values),
					"agent=%s detection=%s detection_success=%s", agent, detection, accepted)
			}
		}
	}
}

func compileCacheAccessFixture(t *testing.T, trigger, detection string, ghes, dryRun, restoreOnly bool) (map[string]any, string) {
	t.Helper()
	source := fmt.Sprintf(`---
on: %s
strict: false
engine: copilot
permissions:
  contents: read
tools:
  github: false
  cache-memory:
    restore-only: %t
cache:
  key: dependencies
  path: node_modules
safe-outputs:
  create-issue:
  threat-detection: %s
jobs:
  custom:
    runs-on: ubuntu-latest
    steps:
      - run: echo custom
---
Test cache access separation.
`, trigger, restoreOnly, detection)
	dir := t.TempDir()
	path := filepath.Join(dir, "cache.md")
	require.NoError(t, os.WriteFile(path, []byte(source), 0600))
	compiler := NewCompiler()
	compiler.gitRoot = dir
	compiler.SetSkipValidation(true)
	compiler.SetGHESCompat(ghes)
	compiler.SetDryRun(dryRun)
	require.NoError(t, compiler.CompileWorkflow(path))
	content, err := os.ReadFile(filepath.Join(dir, "cache.lock.yml"))
	require.NoError(t, err)
	var compiled map[string]any
	require.NoError(t, yaml.Unmarshal(content, &compiled))
	require.NoError(t, compiler.validateGitHubActionsSchemaFromParsed(compiled))
	return compiled, string(content)
}
