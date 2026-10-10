package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeGraderRunWorkflow(t *testing.T, content string) string {
	t.Helper()
	root := t.TempDir()
	workflowDir := filepath.Join(root, ".github", "workflows")
	require.NoError(t, os.MkdirAll(workflowDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(workflowDir, "test.md"), []byte(content), 0o600))
	previous, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(root))
	t.Cleanup(func() { require.NoError(t, os.Chdir(previous)) })
	return "test"
}

func TestRunGraderFromStdin(t *testing.T) {
	workflowID := writeGraderRunWorkflow(t, "---\ngraders: {}\n---\n")
	var output bytes.Buffer
	err := runGrader(context.Background(), graderRunConfig{
		Workflow: workflowID,
		GraderID: "loops",
		Input: bytes.NewBufferString(`{
			"toolCalls":[
				{"name":"view","arguments":{"path":"a"}},
				{"name":"view","arguments":{"path":"a"}}
			],
			"tokenUsageEntries":[],
			"retryEvents":[],
			"artifacts":[]
		}`),
		Output: &output,
	})
	require.NoError(t, err)
	assert.JSONEq(t, `{
		"id":"loops",
		"name":"Loops",
		"value":1,
		"unit":"count",
		"passed":true,
		"status":"pass",
		"source":"builtin",
		"implementation":{"id":"gh-aw/graders","version":1}
	}`, output.String())
}

func TestRunInlineScriptGraderFromStdin(t *testing.T) {
	workflowID := writeGraderRunWorkflow(t, `---
graders:
  custom-score:
    script: |
      return { value: trace.score, message: "computed" }
    unit: ratio
    direction: higher_is_better
    threshold: 0.5
---
`)
	var output bytes.Buffer
	err := runGrader(context.Background(), graderRunConfig{
		Workflow: workflowID,
		GraderID: "custom-score",
		Input:    bytes.NewBufferString(`{"score":0.75}`),
		Output:   &output,
	})
	require.NoError(t, err)
	assert.JSONEq(t, `{
		"id":"custom-score",
		"name":"custom-score",
		"value":0.75,
		"unit":"ratio",
		"passed":true,
		"status":"pass",
		"source":"inline",
		"implementation":{
			"id":"gh-aw/graders",
			"version":1,
			"digest":"518c37ee83a83874d2added478398c3b391bdbaa7b92f6ea9567a016dd888640"
		},
		"message":"computed"
	}`, output.String())
}

func TestRunInlineScriptGraderCannotAccessProcessOrRequire(t *testing.T) {
	workflowID := writeGraderRunWorkflow(t, `---
graders:
  custom-score:
    script: |
      return typeof process === "undefined" && typeof require === "undefined" ? 1 : 0
---
`)
	var output bytes.Buffer
	err := runGrader(context.Background(), graderRunConfig{
		Workflow: workflowID,
		GraderID: "custom-score",
		Input:    bytes.NewBufferString(`{}`),
		Output:   &output,
	})
	require.NoError(t, err)
	var result struct {
		Value float64 `json:"value"`
	}
	require.NoError(t, json.Unmarshal(output.Bytes(), &result))
	require.InDelta(t, float64(1), result.Value, 0)
}

func TestRunInlineScriptGraderNonFiniteValues(t *testing.T) {
	for _, script := range []string{"return NaN", "return Infinity", "return -Infinity", "return { value: NaN }"} {
		t.Run(script, func(t *testing.T) {
			workflowID := writeGraderRunWorkflow(t, fmt.Sprintf("---\ngraders:\n  custom-score:\n    script: |\n      %s\n---\n", script))
			var output bytes.Buffer
			require.NoError(t, runGrader(context.Background(), graderRunConfig{
				Workflow: workflowID,
				GraderID: "custom-score",
				Input:    bytes.NewBufferString(`{}`),
				Output:   &output,
			}))
			var result struct {
				Value  *float64 `json:"value"`
				Status string   `json:"status"`
				Error  string   `json:"error"`
			}
			require.NoError(t, json.Unmarshal(output.Bytes(), &result))
			assert.Nil(t, result.Value)
			assert.Equal(t, "error", result.Status)
			assert.Contains(t, result.Error, "non-finite value")
		})
	}
}

func TestRunInlineScriptGraderCannotEscapeAndExecuteCommand(t *testing.T) {
	markerPath := filepath.Join(t.TempDir(), "executed")
	workflowID := writeGraderRunWorkflow(t, fmt.Sprintf(`---
graders:
  custom-score:
    script: |
      try {
        const escapedProcess = trace.constructor.constructor("return process")()
        escapedProcess.getBuiltinModule("node:child_process").execSync("touch %s")
      } catch {}
      return 1
---
`, markerPath))
	var output bytes.Buffer
	err := runGrader(context.Background(), graderRunConfig{
		Workflow: workflowID,
		GraderID: "custom-score",
		Input:    bytes.NewBufferString(`{}`),
		Output:   &output,
	})
	require.NoError(t, err)
	var result struct {
		Value float64 `json:"value"`
	}
	require.NoError(t, json.Unmarshal(output.Bytes(), &result))
	require.InDelta(t, float64(1), result.Value, 0)
	_, err = os.Stat(markerPath)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestRunInlineOperationalValueGraderFromStdin(t *testing.T) {
	workflowID := writeGraderRunWorkflow(t, `---
graders:
  operational-value:
    script: |
      #!/usr/bin/env bash
      set -euo pipefail
      [[ $# -eq 0 ]]
      request=$(cat)
      value=$(printf '%s' "$request" | jq -r '.score')
      printf '[{"id":"score","value":%s}]\n' "$value"
---
`)
	var output bytes.Buffer
	err := runGrader(context.Background(), graderRunConfig{
		Workflow: workflowID,
		GraderID: "operational-value",
		Input:    bytes.NewBufferString(`{"score":0.75}`),
		Output:   &output,
	})
	require.NoError(t, err)
	assert.JSONEq(t, `[{"id":"score","value":0.75}]`, output.String())
}

func TestRunScriptFileGraderFromStdin(t *testing.T) {
	workflowID := writeGraderRunWorkflow(t, `---
graders:
  operational-value:
    run: .github/graders/test-operational-value.sh
---
`)
	require.NoError(t, os.Mkdir(".git", 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(".github", "graders"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(".github", "graders", "test-operational-value.sh"), []byte(`#!/usr/bin/env bash
set -euo pipefail
[[ $# -eq 0 ]]
[[ "${GH_HOST:-}" == "ghe.example" ]]
payload=$(cat)
[[ "$payload" == '{"score":0.8}' ]]
printf '%s\n' '[{"id":"score","value":0.8},{"id":"evidence-available","value":1}]'
`), 0o700))

	var output bytes.Buffer
	err := runGrader(context.Background(), graderRunConfig{
		Workflow: workflowID,
		GraderID: "operational-value",
		Repo:     "ghe.example/example/repo",
		Input:    bytes.NewBufferString(`{"score":0.8}`),
		Output:   &output,
	})
	require.NoError(t, err)
	assert.JSONEq(t, `[{"id":"score","value":0.8},{"id":"evidence-available","value":1}]`, output.String())
}

func TestRunOperationalValueGraderRejectsHistoricalPayload(t *testing.T) {
	workflowID := writeGraderRunWorkflow(t, `---
graders:
  operational-value:
    script: |
      #!/usr/bin/env bash
      cat
---
`)
	err := runGrader(context.Background(), graderRunConfig{
		Workflow: workflowID,
		GraderID: "operational-value",
		RunID:    123,
		Output:   &bytes.Buffer{},
	})
	require.ErrorContains(t, err, "historical replay is not supported")
}

func TestReadGraderPayloadValidation(t *testing.T) {
	_, err := readGraderPayload(bytes.NewBufferString("not-json"), "standard input")
	require.ErrorContains(t, err, "not valid JSON")

	_, err = parseGraderRunID("0")
	require.ErrorContains(t, err, "positive integer")
}

func TestGraderNodePermissionFlag(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX shell fixture")
	}
	for _, test := range []struct {
		name    string
		output  string
		flag    string
		wantErr string
	}{
		{name: "stable", output: "--permission", flag: "--permission"},
		{name: "experimental Node 20", output: "--experimental-permission", flag: "--experimental-permission"},
		{name: "unsupported", wantErr: "install Node.js 20 or newer"},
		{name: "unexpected output", output: "--allow-fs-read=*", wantErr: "permission support is required"},
	} {
		t.Run(test.name, func(t *testing.T) {
			nodePath := filepath.Join(t.TempDir(), "node")
			require.NoError(t, os.WriteFile(nodePath, []byte(fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' '%s'\n", test.output)), 0o700))
			flag, err := graderNodePermissionFlag(context.Background(), nodePath)
			if test.wantErr != "" {
				require.ErrorContains(t, err, test.wantErr)
				assert.Empty(t, flag)
			} else {
				require.NoError(t, err)
				assert.Equal(t, test.flag, flag)
			}
		})
	}
}

func TestGraderNodePermissionFlagProbeFailure(t *testing.T) {
	_, err := graderNodePermissionFlag(context.Background(), filepath.Join(t.TempDir(), "missing-node"))
	require.ErrorContains(t, err, "failed to detect Node.js permission support")
}

func TestValidateOperationalValueEvaluatorSource(t *testing.T) {
	valid := []byte("#!/usr/bin/env bash\nprintf '%s\\n' '[]'\n")
	actual, err := validateOperationalValueEvaluatorSource(valid)
	require.NoError(t, err)
	assert.Equal(t, valid, actual)

	_, err = validateOperationalValueEvaluatorSource([]byte("echo invalid\n"))
	require.ErrorContains(t, err, "Bash shebang")

	_, err = validateOperationalValueEvaluatorSource(append([]byte("#!/usr/bin/env bash\n"), bytes.Repeat([]byte("x"), maxOperationalValueEvaluatorBytes)...))
	require.ErrorContains(t, err, "65536-byte limit")
}

func TestValidateOperationalValueMetrics(t *testing.T) {
	require.NoError(t, validateOperationalValueMetrics([]byte(`[{"id":"primary","value":2.5},{"id":"cost","value":-3.25},{"id":"diagnostic","value":null}]`)))

	for _, invalid := range []string{
		`[]`,
		`[{"id":"score","value":1,"message":"invalid"}]`,
		`[{"id":"score","value":1},{"id":"score","value":0}]`,
		`[{"id":"score","value":"1"}]`,
	} {
		require.Error(t, validateOperationalValueMetrics([]byte(invalid)), invalid)
	}
}
