//go:build !integration

package workflow

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/workflow/compilerenv"
	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadRepoConfigArtifactRetentionDays(t *testing.T) {
	for _, value := range []string{"1", "7", "90", "400", `"${{ vars.RETENTION || '7' }}"`, `"${{ inputs.retention }}"`} {
		t.Run(value, func(t *testing.T) {
			dir := t.TempDir()
			writeAWJSON(t, dir, `{"artifact_retention_days":`+value+`}`)
			config, err := LoadRepoConfig(dir)
			require.NoError(t, err)
			require.NotNil(t, config.ArtifactRetentionDays)
			encoded, err := json.Marshal(config.ArtifactRetentionDays)
			require.NoError(t, err)
			assert.JSONEq(t, value, string(encoded))
		})
	}
	for _, value := range []string{"0", "-1", "401", "1.5", "null", "true", "[]", "{}", `"7"`, `""`, `"${{ }}"`, `"${{ vars.X }}\nrun: bad"`, `"${{ vars.X }}${{ vars.Y }}"`} {
		t.Run("reject "+value, func(t *testing.T) {
			dir := t.TempDir()
			writeAWJSONRaw(t, dir, `{"artifact_retention_days":`+value+`}`)
			_, err := LoadRepoConfig(dir)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "artifact_retention_days")
		})
	}
}

func TestApplyArtifactRetention(t *testing.T) {
	const content = `name: Retention
on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - name: Script is not an action
        run: |
          echo 'uses: actions/upload-artifact@v4'
          echo 'retention-days: 1'
      - uses: actions/upload-artifact@v3
        with:
          name: staging
          path: |
            one
            two
          retention-days: 1
      - uses: internal/artifacts@v4 # mirror
        with: {name: logs, path: log.txt}
      - uses: actions/upload-artifact@v7
      - uses: actions/download-artifact@v4
        with:
          name: staging
`
	for _, config := range []*RepoConfig{{}, {ArtifactRetentionDays: new(TemplatableInt32("7"))}, {ArtifactRetentionDays: new(TemplatableInt32("${{ vars.RETENTION || '14' }}"))}} {
		config.ActionPins = map[string]string{"actions/upload-artifact@v4": "internal/artifacts@v4"}
		output, err := applyArtifactRetention(content, config)
		require.NoError(t, err)
		assert.Contains(t, output, "        run: |\n          echo 'uses: actions/upload-artifact@v4'\n          echo 'retention-days: 1'\n")
		steps := retentionTestSteps(t, output)
		require.Len(t, steps, 5)
		assert.Equal(t, "one\ntwo\n", steps[1]["with"].(map[string]any)["path"])
		for _, index := range []int{1, 2, 3} {
			want := any("${{ vars.GH_AW_DEFAULT_ARTIFACT_RETENTION_DAYS || '0' }}")
			if index == 1 {
				want = "${{ vars.GH_AW_DEFAULT_ARTIFACT_RETENTION_DAYS || '1' }}"
			}
			if config.ArtifactRetentionDays != nil {
				want = config.ArtifactRetentionDays.ToValue()
			}
			assert.EqualValues(t, want, steps[index]["with"].(map[string]any)["retention-days"])
		}
		assert.NotContains(t, steps[4]["with"], "retention-days")
	}
}

func TestArtifactRetentionPreservesExpressionFallback(t *testing.T) {
	assert.Equal(t, "${{ vars.GH_AW_DEFAULT_ARTIFACT_RETENTION_DAYS || (inputs.retention) }}",
		artifactRetentionDays(nil, "${{ inputs.retention }}"))
	t.Setenv(compilerenv.DefaultArtifactRetentionDays, "5")
	assert.Equal(t, "${{ vars.GH_AW_DEFAULT_ARTIFACT_RETENTION_DAYS || '30' }}", artifactRetentionDays(nil, "30"),
		"enterprise default must be resolved at runtime, not from the compiler environment")
}

func TestArtifactRetentionMatchesOnlyMappedMirrorRef(t *testing.T) {
	const sha = "0123456789012345678901234567890123456789"
	config := &RepoConfig{ActionPins: map[string]string{"actions/upload-artifact@v4": "internal/actions@upload"}}
	cache := NewActionCache(t.TempDir())
	cache.Set("internal/actions", "upload", sha)
	data := &WorkflowData{
		ActionPinMappings: config.ActionPins,
		ActionCache:       cache,
		ActionResolver:    NewActionResolver(cache),
	}
	resolved, err := resolvedArtifactRetentionConfig(config, data)
	require.NoError(t, err)
	assert.Equal(t, "internal/actions@upload", config.ActionPins["actions/upload-artifact@v4"], "do not mutate repository config")
	assert.Equal(t, "internal/actions@"+sha, resolved.ActionPins["actions/upload-artifact@v4"])
	source := `jobs:
  test:
    steps:
      - uses: internal/actions@` + sha + `
        with:
          path: report.txt
      - uses: internal/actions@download
        with:
          name: report
`
	output, err := applyArtifactRetention(source, resolved)
	require.NoError(t, err)
	steps := retentionTestSteps(t, output)
	assert.Contains(t, steps[0]["with"], "retention-days")
	assert.NotContains(t, steps[1]["with"], "retention-days", "a different action ref in the same mirror repository must not be rewritten")
}

func TestArtifactRetentionPreservesCommentsAndMultilineNames(t *testing.T) {
	const source = `jobs:
  test:
    steps:
      - uses: actions/upload-artifact@v4
        # Keep this input comment exactly once.
        with:
          path: report.txt
          retention-days: ${{ inputs.days }}
      - name: |
          A multiline
          step name
        uses: actions/upload-artifact@v4
`
	output, err := applyArtifactRetention(source, nil)
	require.NoError(t, err)
	steps := retentionTestSteps(t, output)
	require.Len(t, steps, 2)
	assert.Equal(t, 1, strings.Count(output, "# Keep this input comment exactly once."))
	inputs := steps[0]["with"].(map[string]any)
	assert.Equal(t, "report.txt", inputs["path"])
	assert.Equal(t, "${{ vars.GH_AW_DEFAULT_ARTIFACT_RETENTION_DAYS || (inputs.days) }}", inputs["retention-days"])
	assert.Equal(t, "A multiline\nstep name\n", steps[1]["name"])
	assert.Contains(t, steps[1]["with"], "retention-days")
}

func TestArtifactRetentionErrors(t *testing.T) {
	for _, source := range []string{
		"jobs:\n  test:\n    steps:\n      - uses: actions/upload-artifact@v4\n        with: invalid\n",
	} {
		_, err := applyArtifactRetention(source, nil)
		require.Error(t, err)
	}
	for _, source := range []string{"", "name: No jobs\n", "jobs:\n  reusable:\n    uses: org/repo/.github/workflows/test.yml@main\n"} {
		output, err := applyArtifactRetention(source, nil)
		require.NoError(t, err)
		assert.Equal(t, source, output)
	}
}

func TestArtifactRetentionDoesNotParseUnrelatedSteps(t *testing.T) {
	const source = `jobs:
  test:
    steps:
      - name: Validation is handled elsewhere
        env:
          LEGACY_NAME: a workflow: with a colon
        run: echo hello
      - uses: actions/upload-artifact@v4
        with:
          path: report.txt
`
	output, err := applyArtifactRetention(source, nil)
	require.NoError(t, err)
	assert.Contains(t, output, "          LEGACY_NAME: a workflow: with a colon\n")
	assert.Contains(t, output, "retention-days: ${{ vars.GH_AW_DEFAULT_ARTIFACT_RETENTION_DAYS || '0' }}")
}

func TestArtifactRetentionPreservesEmbeddedWorkflowYAML(t *testing.T) {
	for _, scalar := range []string{"|", "|-", "|+", ">", ">-", ">+", "|2", ">2-"} {
		t.Run(scalar, func(t *testing.T) {
			const embedded = `          cat <<'DOC'
          jobs:
            example:
              steps:
                - uses: actions/upload-artifact@v4
                  with:
                    path: embedded.txt
                    retention-days: 13
          DOC
`
			source := "jobs:\n  test:\n    steps:\n      - name: Print embedded YAML\n        run: " + scalar + "\n" + embedded + `      - uses: actions/github-script@v9
        with:
          script: |
            const example = ` + "`" + `
                steps:
                  - uses: actions/upload-artifact@v4
            ` + "`" + `;
            core.info(example);
      - uses: actions/upload-artifact@v4
        with:
          path: real.txt
`
			original := retentionTestSteps(t, source)
			output, err := applyArtifactRetention(source, &RepoConfig{ArtifactRetentionDays: new(TemplatableInt32("7"))})
			require.NoError(t, err)
			steps := retentionTestSteps(t, output)
			require.Len(t, steps, 3)
			assert.Equal(t, original[0], steps[0], "run scalar must remain unchanged")
			assert.Equal(t, original[1], steps[1], "script scalar must remain unchanged")
			assert.Contains(t, output, embedded, "heredoc text must remain byte-for-byte intact")
			assert.EqualValues(t, 7, steps[2]["with"].(map[string]any)["retention-days"])
			assert.Equal(t, 1, strings.Count(output, "retention-days: 7\n"))
		})
	}
}

func TestArtifactRetentionReviewReproductionIsInvalidYAML(t *testing.T) {
	const source = `jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - name: Print some embedded yaml
        run: |
          cat <<'DOC'
    steps:
      - uses: actions/upload-artifact@v4
          DOC
`
	var workflow map[string]any
	require.Error(t, yaml.Unmarshal([]byte(source), &workflow), "dedenting steps ends the run scalar and introduces a duplicate mapping key")
}

func TestWorkflowDataWithArtifactRetention(t *testing.T) {
	for _, retention := range []*string{nil, new("14"), new("${{ inputs.retention }}")} {
		upload := &UploadArtifactConfig{RetentionDays: retention, MaxUploads: 3}
		data := &WorkflowData{Name: "test", SafeOutputs: &SafeOutputsConfig{UploadArtifact: upload}}
		for _, days := range []string{"7", "30", "${{ vars.RETENTION }}"} {
			t.Run(days, func(t *testing.T) {
				t.Parallel()
				renderData := workflowDataWithArtifactRetention(data, &RepoConfig{ArtifactRetentionDays: new(TemplatableInt32(days))})
				assert.NotSame(t, data, renderData)
				assert.NotSame(t, data.SafeOutputs, renderData.SafeOutputs)
				assert.NotSame(t, upload, renderData.SafeOutputs.UploadArtifact)
				assert.Equal(t, days, *renderData.SafeOutputs.UploadArtifact.RetentionDays)
				assert.Equal(t, upload.MaxUploads, renderData.SafeOutputs.UploadArtifact.MaxUploads)
				assert.Same(t, upload, data.SafeOutputs.UploadArtifact)
				assert.Equal(t, retention, data.SafeOutputs.UploadArtifact.RetentionDays)
			})
		}
	}
	for _, data := range []*WorkflowData{{}, {SafeOutputs: &SafeOutputsConfig{}}} {
		assert.Same(t, data, workflowDataWithArtifactRetention(data, nil), "do not copy workflows without artifact safe outputs")
	}
}

func TestCompileArtifactRetention(t *testing.T) {
	for _, value := range []string{"7", `"${{ vars.RETENTION || '14' }}"`, ""} {
		t.Run(value, func(t *testing.T) {
			dir := t.TempDir()
			if value != "" {
				extra := ""
				if value == "7" {
					extra = `,"ghes":true,"action_pins":{"actions/upload-artifact@v4":"internal/artifacts@0123456789012345678901234567890123456789"}`
				}
				writeAWJSON(t, dir, `{"artifact_retention_days":`+value+extra+`}`)
			}
			source := filepath.Join(dir, "retention.md")
			require.NoError(t, os.WriteFile(source, []byte(`---
on: workflow_dispatch
engine: copilot
strict: false
permissions:
  contents: read
tools:
  cache-memory:
    retention-days: 5
safe-outputs:
  upload-artifact:
    retention-days: 14
  upload-asset:
  create-pull-request:
post-steps:
  - uses: actions/upload-artifact@v4
    with:
      name: custom
      path: report.txt
      retention-days: 90
---
Test retention.
`), 0o600))
			compiler := NewCompiler()
			compiler.gitRoot = dir
			require.NoError(t, compiler.CompileWorkflow(source))
			data, err := os.ReadFile(filepath.Join(dir, "retention.lock.yml"))
			require.NoError(t, err)
			output := string(data)
			var workflow map[string]any
			require.NoError(t, yaml.Unmarshal(data, &workflow))
			count := 0
			for _, rawJob := range workflow["jobs"].(map[string]any) {
				job := rawJob.(map[string]any)
				for _, rawStep := range job["steps"].([]any) {
					step := rawStep.(map[string]any)
					uses, _ := step["uses"].(string)
					if strings.HasPrefix(uses, "actions/upload-artifact@") || strings.HasPrefix(uses, "internal/artifacts@") {
						count++
						retention := step["with"].(map[string]any)["retention-days"]
						require.NotNil(t, retention, "every upload must have retention-days")
						switch value {
						case "7":
							assert.EqualValues(t, 7, retention)
						case "":
							assert.Contains(t, retention, "vars.GH_AW_DEFAULT_ARTIFACT_RETENTION_DAYS")
						default:
							assert.Equal(t, "${{ vars.RETENTION || '14' }}", retention)
						}
					}
				}
			}
			assert.GreaterOrEqual(t, count, 7, "cover activation, agent, staging, cache, safe-output, and custom artifacts")
			assert.Contains(t, output, `\"retention-days\":`)
			switch value {
			case "7":
				assert.Contains(t, output, `\"retention-days\":7`, "safe-output handler must use the global policy")
				assert.Contains(t, output, "uses: internal/artifacts@0123456789012345678901234567890123456789", "custom mirrored uploads must retain the policy")
				assert.Contains(t, output, "# v3.2.2", "GHES uploads must retain the policy")
			case "":
				assert.Contains(t, output, "vars.GH_AW_DEFAULT_ARTIFACT_RETENTION_DAYS || '14'", "safe-output default must preserve the configured retention")
				assert.Contains(t, output, "vars.GH_AW_DEFAULT_ARTIFACT_RETENTION_DAYS || '5'", "cache fallback must be preserved")
				assert.Contains(t, output, "vars.GH_AW_DEFAULT_ARTIFACT_RETENTION_DAYS || '90'", "custom upload fallback must be preserved")
			default:
				assert.Contains(t, output, `\"retention-days\":\"${{ vars.RETENTION || '14' }}\"`)
			}
		})
	}
}

func TestMaintenanceArtifactRetention(t *testing.T) {
	ledger, err := parseLedgerToolConfig(map[string]any{"findings": map[string]any{}})
	require.NoError(t, err)
	for _, config := range []*RepoConfig{nil, {ArtifactRetentionDays: new(TemplatableInt32("7"))}} {
		dir := t.TempDir()
		require.NoError(t, GenerateMaintenanceWorkflow(context.Background(), GenerateMaintenanceWorkflowOptions{
			WorkflowDataList: []*WorkflowData{{Name: "test", WorkflowID: "test", LedgerConfig: ledger}},
			WorkflowDir:      dir,
			Version:          "v1.0.0",
			ActionMode:       ActionModeDev,
			RepoConfig:       config,
		}))
		data, err := os.ReadFile(filepath.Join(dir, "agentics-maintenance.yml"))
		require.NoError(t, err)
		if config == nil {
			assert.Contains(t, string(data), "retention-days: ${{ vars.GH_AW_DEFAULT_ARTIFACT_RETENTION_DAYS || '30' }}")
			assert.Contains(t, string(data), "retention-days: ${{ vars.GH_AW_DEFAULT_ARTIFACT_RETENTION_DAYS || '1' }}")
		} else {
			assert.Contains(t, string(data), "retention-days: 7")
			assert.NotContains(t, string(data), "retention-days: 30")
			assert.NotContains(t, string(data), "retention-days: 1")
		}
	}
}

func retentionTestSteps(t *testing.T, content string) []map[string]any {
	t.Helper()
	var workflow struct {
		Jobs map[string]struct {
			Steps []map[string]any `yaml:"steps"`
		} `yaml:"jobs"`
	}
	require.NoError(t, yaml.Unmarshal([]byte(content), &workflow))
	return workflow.Jobs["test"].Steps
}
