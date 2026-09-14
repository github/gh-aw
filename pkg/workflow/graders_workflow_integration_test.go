//go:build integration

package workflow

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/github/gh-aw/pkg/testutil"
)

func TestGradersWorkflowIntegration_SyntaxParserSchema(t *testing.T) {
	tmpDir := testutil.TempDir(t, "graders-workflow-integration")
	workflowPath := filepath.Join(tmpDir, "workflow.md")

	content := `---
on: workflow_dispatch
engine: copilot
strict: false
permissions:
  contents: read
graders:
  retries: null
  custom-score:
    name: Custom Score
    description: Measures custom score
    unit: ratio
    direction: higher_is_better
    threshold: 0.7
    min: 0.0
    max: 1.0
    config:
      window: 5
    script: |
      return helpers.clamp(trace.toolCalls.length / 10, 0, 1)
experiments:
  prompt_style:
    variants: [control, candidate]
    metric: grader:retries
  model_mix:
    variants: [baseline, tuned]
    metric: graders.custom-score.value
---

# Integration test workflow
`
	require.NoError(t, os.WriteFile(workflowPath, []byte(content), 0o644))

	compiler := NewCompiler(WithVersion("dev"))
	require.NoError(t, compiler.CompileWorkflow(workflowPath))

	compiledPath := stringutil.MarkdownToLockFile(workflowPath)
	compiled, err := os.ReadFile(compiledPath)
	require.NoError(t, err)
	yaml := string(compiled)

	assert.Contains(t, yaml, "Run graders")
	assert.Contains(t, yaml, "trace_graders.cjs")
	assert.Contains(t, yaml, "grader:retries")
	assert.Contains(t, yaml, "graders.custom-score.value")

	match := regexp.MustCompile(`await main\('([^']+)', '([^']+)'\);`).FindStringSubmatch(yaml)
	require.Len(t, match, 3, "expected encoded manifest and exec spec in generated script")

	manifestJSON, err := base64.StdEncoding.DecodeString(match[1])
	require.NoError(t, err)

	var manifest graderManifest
	require.NoError(t, json.Unmarshal(manifestJSON, &manifest))
	require.Equal(t, 1, manifest.Version)

	entriesByID := map[string]graderManifestEntry{}
	for _, entry := range manifest.Graders {
		entriesByID[entry.ID] = entry
	}

	retries, ok := entriesByID["retries"]
	require.True(t, ok, "expected retries grader in manifest")
	assert.Equal(t, "builtin", retries.Source)
	assert.True(t, retries.Enabled)

	custom, ok := entriesByID["custom-score"]
	require.True(t, ok, "expected custom grader in manifest")
	assert.Equal(t, "inline", custom.Source)
	assert.Equal(t, "Custom Score", custom.Name)
	assert.Equal(t, "ratio", custom.Unit)
	assert.Equal(t, "higher_is_better", custom.Direction)
	require.NotNil(t, custom.Threshold)
	assert.InDelta(t, 0.7, *custom.Threshold, 0.000001)
	require.NotNil(t, custom.Config)
	assert.Equal(t, float64(5), custom.Config["window"])

	execJSON, err := base64.StdEncoding.DecodeString(match[2])
	require.NoError(t, err)

	var execEntries []graderExecEntry
	require.NoError(t, json.Unmarshal(execJSON, &execEntries))
	require.Len(t, execEntries, 1)
	assert.Equal(t, "custom-score", execEntries[0].ID)
	assert.Contains(t, execEntries[0].Script, "helpers.clamp")
}

func TestGradersWorkflowIntegration_InlineOperationalValue(t *testing.T) {
	tmpDir := testutil.TempDir(t, "graders-inline-operational-value")
	gitInit := exec.Command("git", "-C", tmpDir, "init")
	output, err := gitInit.CombinedOutput()
	require.NoErrorf(t, err, "git init should succeed: %s", output)
	workflowPath := filepath.Join(tmpDir, "workflow.md")
	evaluator := "#!/usr/bin/env bash\nset -euo pipefail\nrequest=$(cat)\nprintf '%s\\n' '[{\"id\":\"goal-attained\",\"value\":1}]'\n"

	content := `---
on: workflow_dispatch
engine: copilot
strict: false
permissions:
  contents: read
graders:
  operational-value:
    script: |
      #!/usr/bin/env bash
      set -euo pipefail
      request=$(cat)
      printf '%s\n' '[{"id":"goal-attained","value":1}]'
---

# Inline operational value
`
	require.NoError(t, os.WriteFile(workflowPath, []byte(content), 0o644))

	compiler := NewCompiler(WithVersion("dev"))
	require.NoError(t, compiler.CompileWorkflow(workflowPath))

	compiled, err := os.ReadFile(stringutil.MarkdownToLockFile(workflowPath))
	require.NoError(t, err)
	match := regexp.MustCompile(`await main\('([^']+)', '([^']+)'\);`).FindStringSubmatch(string(compiled))
	require.Len(t, match, 3, "expected encoded manifest and exec spec in generated script")

	manifestJSON, err := base64.StdEncoding.DecodeString(match[1])
	require.NoError(t, err)
	var manifest graderManifest
	require.NoError(t, json.Unmarshal(manifestJSON, &manifest))
	require.Len(t, manifest.Graders, 1)
	entry := manifest.Graders[0]
	assert.Equal(t, "operational-value", entry.ID)
	assert.Equal(t, "operational-value", entry.Source)
	assert.Empty(t, entry.Run)
	assert.Equal(t, "workflow.md", entry.Inline)
	assert.Equal(t, fmt.Sprintf("%x", sha256.Sum256([]byte(evaluator))), entry.Digest)

	execJSON, err := base64.StdEncoding.DecodeString(match[2])
	require.NoError(t, err)
	var execEntries []graderExecEntry
	require.NoError(t, json.Unmarshal(execJSON, &execEntries))
	require.Len(t, execEntries, 1)
	assert.Equal(t, evaluator, execEntries[0].Run)
	assert.Empty(t, execEntries[0].Script)
}

func TestGradersWorkflowIntegration_SchemaRejectsUnknownGraderField(t *testing.T) {
	tmpDir := testutil.TempDir(t, "graders-schema-rejects-unknown-field")
	workflowPath := filepath.Join(tmpDir, "workflow.md")

	content := `---
on: workflow_dispatch
engine: copilot
graders:
  retries:
    unsupported: true
---

# Integration test workflow
`
	require.NoError(t, os.WriteFile(workflowPath, []byte(content), 0o644))

	compiler := NewCompiler(WithVersion("dev"))
	err := compiler.CompileWorkflow(workflowPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "graders")
	if !regexp.MustCompile(`additionalProperties|unknown field|Unknown property`).MatchString(err.Error()) {
		t.Fatalf("expected schema/parser unknown-field error, got: %v", err)
	}
}

func TestGradersWorkflowIntegration_ExperimentsMetricRequiresDeclaredGrader(t *testing.T) {
	tmpDir := testutil.TempDir(t, "experiments-grader-metric-reference")
	workflowPath := filepath.Join(tmpDir, "workflow.md")

	content := `---
on: workflow_dispatch
engine: copilot
strict: false
experiments:
  prompt_style:
    variants: [control, candidate]
    metric: grader:loops
---

# Integration test workflow
`
	require.NoError(t, os.WriteFile(workflowPath, []byte(content), 0o644))

	compiler := NewCompiler(WithVersion("dev"))
	err := compiler.CompileWorkflow(workflowPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `references grader "loops" but no graders are declared`)
}
