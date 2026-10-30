//go:build !integration

package workflow

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/require"
)

func TestMergeJobsFromYAMLImports_Outputs(t *testing.T) {
	tests := []struct {
		name     string
		mainJobs map[string]any
		imports  string
		want     map[string]any
	}{
		{
			name:    "imported job only",
			imports: `{"build":{"outputs":{"imported":"imported"}}}`,
			want:    map[string]any{"imported": "imported"},
		},
		{
			name:     "main job without outputs",
			mainJobs: map[string]any{"build": map[string]any{"runs-on": "ubuntu-latest"}},
			imports:  `{"build":{"outputs":{"imported":"imported"}}}`,
			want:     map[string]any{"imported": "imported"},
		},
		{
			name: "main outputs take precedence",
			mainJobs: map[string]any{"build": map[string]any{
				"outputs": map[string]any{"main": "main", "shared": ""},
			}},
			imports: `{"build":{"outputs":{"imported":"imported","shared":"imported"}}}`,
			want:    map[string]any{"main": "main", "imported": "imported", "shared": ""},
		},
		{
			name:     "empty main outputs",
			mainJobs: map[string]any{"build": map[string]any{"outputs": map[string]any{}}},
			imports:  `{"build":{"outputs":{"imported":"imported"}}}`,
			want:     map[string]any{"imported": "imported"},
		},
		{
			name:     "null main outputs",
			mainJobs: map[string]any{"build": map[string]any{"outputs": nil}},
			imports:  `{"build":{"outputs":{"imported":"imported"}}}`,
			want:     map[string]any{"imported": "imported"},
		},
		{
			name:     "null imported outputs",
			mainJobs: map[string]any{"build": map[string]any{"outputs": map[string]any{"main": "main"}}},
			imports:  `{"build":{"outputs":null}}`,
			want:     map[string]any{"main": "main"},
		},
		{
			name:     "main outputs only",
			mainJobs: map[string]any{"build": map[string]any{"outputs": map[string]any{"main": "main"}}},
			imports:  `{"build":{"runs-on":"macos-latest"}}`,
			want:     map[string]any{"main": "main"},
		},
		{
			name: "multiple imports without main",
			imports: `{"build":{"outputs":{"first":"first","shared":"first"}}}
{"build":{"outputs":{"second":"second","shared":"second"}}}`,
			want: map[string]any{"first": "first", "second": "second", "shared": "second"},
		},
		{
			name: "null first import outputs",
			imports: `{"build":{"outputs":null}}
{"build":{"outputs":{"second":"second"}}}`,
			want: map[string]any{"second": "second"},
		},
		{
			name: "null later import outputs",
			imports: `{"build":{"outputs":{"first":"first"}}}
{"build":{"outputs":null}}`,
			want: map[string]any{"first": "first"},
		},
		{
			name:     "multiple imports with main",
			mainJobs: map[string]any{"build": map[string]any{"outputs": map[string]any{"main": "main", "shared": "main"}}},
			imports: `{"build":{"outputs":{"first":"first","shared":"first"}}}
{"build":{"outputs":{"second":"second","shared":"second"}}}`,
			want: map[string]any{"first": "first", "second": "second", "main": "main", "shared": "main"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			original, err := json.Marshal(tt.mainJobs)
			require.NoError(t, err)

			result, err := (&Compiler{}).mergeJobsFromYAMLImports(tt.mainJobs, tt.imports)
			require.NoError(t, err)
			require.Equal(t, tt.want, result["build"].(map[string]any)["outputs"])

			after, err := json.Marshal(tt.mainJobs)
			require.NoError(t, err)
			require.JSONEq(t, string(original), string(after), "main job must not be mutated")
		})
	}
}

func TestMergeJobOutputField_YAMLValues(t *testing.T) {
	for _, tt := range []struct {
		name string
		yaml string
		ok   bool
	}{
		{name: "absent", yaml: "{}\n", ok: true},
		{name: "null", yaml: "outputs: null\n", ok: true},
		{name: "empty declaration", yaml: "outputs:\n", ok: true},
		{name: "empty mapping", yaml: "outputs: {}\n", ok: true},
		{name: "invalid string", yaml: "outputs: invalid\n"},
		{name: "invalid boolean", yaml: "outputs: false\n"},
		{name: "invalid sequence", yaml: "outputs: []\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var job map[string]any
			require.NoError(t, yaml.Unmarshal([]byte(tt.yaml), &job))
			outputsJob := map[string]any{"outputs": map[string]any{"value": "value"}}
			for _, jobs := range [][2]any{{job, outputsJob}, {outputsJob, job}} {
				outputs, ok := mergeJobOutputField(jobs[0], jobs[1])
				require.Equal(t, tt.ok, ok)
				if tt.ok {
					require.Equal(t, map[string]any{"value": "value"}, outputs)
				} else {
					require.Nil(t, outputs)
				}
			}
		})
	}
}

func TestMergeJobImportFields_OutputsDoNotMutateInputs(t *testing.T) {
	mainOutputs := map[string]any{"main": "main", "shared": "main"}
	importedOutputs := map[string]any{"imported": "imported", "shared": "imported"}
	mainJob := map[string]any{"outputs": mainOutputs, "runs-on": "ubuntu-latest"}
	importedJob := map[string]any{"outputs": importedOutputs, "runs-on": "macos-latest"}

	merged, ok := mergeJobImportFields("build", mainJob, importedJob)
	require.True(t, ok)
	require.Equal(t, "ubuntu-latest", merged["runs-on"])
	merged["outputs"].(map[string]any)["shared"] = "changed"
	require.Equal(t, map[string]any{"main": "main", "shared": "main"}, mainOutputs)
	require.Equal(t, map[string]any{"imported": "imported", "shared": "imported"}, importedOutputs)
}

func TestMergeJobsFromYAMLImports_OutputsPreserveImportedJob(t *testing.T) {
	for _, laterConfig := range []string{
		`{"build":{"outputs":{"second":"second"}}}`,
		`{"build":{"runs-on":"macos-latest","steps":[{"run":"echo second"}],"outputs":{"second":"second"}}}`,
	} {
		t.Run(laterConfig, func(t *testing.T) {
			imports := `{"build":{"runs-on":"ubuntu-latest","steps":[{"run":"echo first"}],"outputs":{"first":"first"}}}` + "\n" + laterConfig
			result, err := (&Compiler{}).mergeJobsFromYAMLImports(nil, imports)
			require.NoError(t, err)
			job := result["build"].(map[string]any)
			require.Equal(t, "ubuntu-latest", job["runs-on"])
			require.Equal(t, []any{map[string]any{"run": "echo first"}}, job["steps"])
			require.Equal(t, map[string]any{"first": "first", "second": "second"}, job["outputs"])
		})
	}
}

func TestMergeJobsFromYAMLImports_OutputBodyPrecedence(t *testing.T) {
	first := map[string]any{
		"runs-on": "ubuntu-latest",
		"env":     map[string]any{"SOURCE": "first"},
		"needs":   []any{"prepare"},
		"steps":   []any{map[string]any{"id": "first", "run": "echo first"}},
		"outputs": map[string]any{"first": "first"},
	}
	for _, tt := range []struct {
		name        string
		injected    bool
		later       map[string]any
		laterBody   bool
		wantOutputs map[string]any
	}{
		{
			name:        "later nonstep fields do not change existing precedence",
			later:       map[string]any{"runs-on": "macos-latest", "env": map[string]any{"SOURCE": "second"}, "needs": []any{"other"}, "outputs": map[string]any{"second": "second"}},
			wantOutputs: map[string]any{"first": "first", "second": "second"},
		},
		{
			name:        "earlier pre-steps and later output-only preserve full job",
			injected:    true,
			later:       map[string]any{"outputs": map[string]any{"second": "second"}},
			wantOutputs: map[string]any{"first": "first", "second": "second"},
		},
		{
			name:        "earlier pre-steps and null output-only preserve full job",
			injected:    true,
			later:       map[string]any{"outputs": nil},
			wantOutputs: map[string]any{"first": "first"},
		},
		{
			name:        "injected merge retains existing later body precedence",
			injected:    true,
			later:       map[string]any{"runs-on": "macos-latest", "outputs": map[string]any{"second": "second"}},
			laterBody:   true,
			wantOutputs: map[string]any{"first": "first", "second": "second"},
		},
		{
			name:        "non-output later contribution retains existing later body precedence",
			injected:    true,
			later:       map[string]any{"runs-on": "macos-latest"},
			laterBody:   true,
			wantOutputs: map[string]any{"first": "first"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			earlier := maps.Clone(first)
			if tt.injected {
				earlier["pre-steps"] = []any{map[string]any{"id": "setup", "run": "echo setup"}}
			}
			before, err := json.Marshal(earlier)
			require.NoError(t, err)
			firstJSON, err := json.Marshal(map[string]any{"build": earlier})
			require.NoError(t, err)
			laterJSON, err := json.Marshal(map[string]any{"build": tt.later})
			require.NoError(t, err)
			result, err := (&Compiler{}).mergeJobsFromYAMLImports(nil, string(firstJSON)+"\n"+string(laterJSON))
			require.NoError(t, err)
			job := result["build"].(map[string]any)
			require.Equal(t, tt.wantOutputs, job["outputs"])
			if tt.laterBody {
				require.Equal(t, "macos-latest", job["runs-on"])
				require.NotContains(t, job, "env")
				require.NotContains(t, job, "needs")
				require.NotContains(t, job, "steps")
			} else {
				for _, field := range []string{"runs-on", "env", "needs", "steps"} {
					require.Equal(t, earlier[field], job[field], field)
				}
			}
			require.Equal(t, earlier["pre-steps"], job["pre-steps"])
			after, err := json.Marshal(earlier)
			require.NoError(t, err)
			require.JSONEq(t, string(before), string(after))
		})
	}
}

func TestMergeJobsFromYAMLImports_MainFieldsWin(t *testing.T) {
	main := map[string]any{
		"runs-on": "ubuntu-latest",
		"env":     map[string]any{"SOURCE": "main"},
		"needs":   []any{"prepare"},
		"steps":   []any{map[string]any{"id": "main", "run": "echo main"}},
		"outputs": map[string]any{"result": "${{ steps.main.outputs.value }}"},
	}
	imports := `{"build":{"runs-on":"macos-latest","env":{"SOURCE":"imported"},"needs":["other"],"steps":[{"id":"imported","run":"echo imported"}],"outputs":{"result":"${{ steps.imported.outputs.value }}","extra":"extra"}}}`
	result, err := (&Compiler{}).mergeJobsFromYAMLImports(map[string]any{"build": main}, imports)
	require.NoError(t, err)
	job := result["build"].(map[string]any)
	for _, field := range []string{"runs-on", "env", "needs", "steps"} {
		require.Equal(t, main[field], job[field], field)
	}
	require.Equal(t, map[string]any{
		"result": "${{ steps.main.outputs.value }}",
		"extra":  "extra",
	}, job["outputs"])
}

func TestMergeJobsFromYAMLImports_DiscardedOutputSteps(t *testing.T) {
	for _, tt := range []struct {
		name     string
		mainJobs map[string]any
		imports  string
		wantErr  string
	}{
		{
			name: "later conflict references discarded regular step",
			imports: `{"build":{"steps":[{"id":"a","run":"echo a"}],"outputs":{"result":"${{ steps.a.outputs.value }}"}}}
{"build":{"steps":[{"id":"b","run":"echo b"}],"outputs":{"result":"${{ steps.b.outputs.value }}"}}}`,
			wantErr: `jobs.build.outputs.result references discarded regular step "b"`,
		},
		{
			name: "distinct later output references discarded step with bracket syntax",
			imports: `{"build":{"steps":[{"id":"a","run":"echo a"}],"outputs":{"first":"${{ steps.a.outputs.value }}"}}}
{"build":{"steps":[{"id":"b","run":"echo b"}],"outputs":{"second":"${{ steps['b'].outputs.value }}"}}}`,
			wantErr: `jobs.build.outputs.second references discarded regular step "b"`,
		},
		{
			name: "injected merge discards earlier regular body",
			imports: `{"build":{"steps":[{"id":"a","run":"echo a"}],"pre-steps":[{"run":"echo setup"}],"outputs":{"first":"${{ steps.a.outputs.value }}"}}}
{"build":{"steps":[{"id":"b","run":"echo b"}],"outputs":{"second":"${{ steps.b.outputs.value }}"}}}`,
			wantErr: `jobs.build.outputs.first references discarded regular step "a"`,
		},
		{
			name: "main job discards imported regular body",
			mainJobs: map[string]any{"build": map[string]any{
				"steps": []any{map[string]any{"id": "main", "run": "echo main"}},
			}},
			imports: `{"build":{"steps":[{"id":"imported","run":"echo imported"}],"outputs":{"result":"${{ steps.imported.outputs.value }}"}}}`,
			wantErr: `jobs.build.outputs.result references discarded regular step "imported"`,
		},
		{
			name: "main output override removes unsafe intermediate conflict",
			mainJobs: map[string]any{"build": map[string]any{
				"outputs": map[string]any{"result": "main"},
			}},
			imports: `{"build":{"steps":[{"id":"a","run":"echo a"}],"outputs":{"result":"${{ steps.a.outputs.value }}"}}}
{"build":{"steps":[{"id":"b","run":"echo b"}],"outputs":{"result":"${{ steps.b.outputs.value }}"}}}`,
		},
		{
			name: "retained regular producer with same ID remains valid",
			imports: `{"build":{"steps":[{"id":"a","run":"echo first"}],"outputs":{"result":"first"}}}
{"build":{"steps":[{"id":"a","run":"echo second"}],"outputs":{"result":"${{ steps.a.outputs.value }}"}}}`,
		},
		{
			name: "retained injected producer supports later output",
			imports: `{"build":{"steps":[{"id":"a","run":"echo a"}],"pre-steps":[{"id":"b","run":"echo setup"}],"outputs":{"first":"${{ steps.a.outputs.value }}"}}}
{"build":{"outputs":{"second":"${{ steps.b.outputs.value }}"}}}`,
		},
		{
			name: "main injected producer resolves discarded import producer",
			mainJobs: map[string]any{"build": map[string]any{
				"setup-steps": []any{map[string]any{"id": "b", "run": "echo b"}},
			}},
			imports: `{"build":{"steps":[{"id":"a","run":"echo a"}],"outputs":{"result":"first"}}}
{"build":{"steps":[{"id":"b","run":"echo b"}],"outputs":{"result":"${{ STEPS['B'].outputs.value }}"}}}`,
		},
		{
			name: "activation regular steps are concatenated",
			imports: `{"activation":{"steps":[{"id":"a","run":"echo a"}],"outputs":{"first":"${{ steps.a.outputs.value }}"}}}
{"activation":{"steps":[{"id":"b","run":"echo b"}],"outputs":{"second":"${{ steps.b.outputs.value }}"}}}`,
		},
		{
			name: "quoted step syntax is not a reference",
			imports: `{"build":{"steps":[{"id":"a","run":"echo a"}]}}
{"build":{"steps":[{"id":"b","run":"echo b"}],"outputs":{"result":"${{ 'steps.b.outputs.value' }}"}}}`,
		},
		{
			name: "unrelated preexisting missing reference is outside merge validation",
			imports: `{"build":{"steps":[{"id":"a","run":"echo a"}]}}
{"build":{"steps":[{"id":"b","run":"echo b"}],"outputs":{"result":"${{ steps.unknown.outputs.value }}"}}}`,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			result, err := (&Compiler{}).mergeJobsFromYAMLImports(tt.mainJobs, tt.imports)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				require.ErrorContains(t, err, "pre-steps")
				require.Nil(t, result)
			} else {
				require.NoError(t, err)
				require.NotEmpty(t, result)
			}
		})
	}
}

func TestImportedJobOutputsRejectDiscardedProducer(t *testing.T) {
	dir := testutil.TempDir(t, "discarded-output-producer")
	for _, id := range []string{"a", "b"} {
		component := fmt.Sprintf(`---
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - id: %s
        run: echo "value=%s" >> "$GITHUB_OUTPUT"
    outputs:
      result: ${{ steps.%s.outputs.value }}
---
`, id, id, id)
		require.NoError(t, os.WriteFile(filepath.Join(dir, id+".md"), []byte(component), 0o644))
	}
	workflow := `---
on: workflow_dispatch
engine: copilot
strict: false
permissions:
  contents: read
imports:
  - ./a.md
  - ./b.md
---
Test discarded output producer.
`
	path := filepath.Join(dir, "workflow.md")
	require.NoError(t, os.WriteFile(path, []byte(workflow), 0o644))
	err := NewCompiler().CompileWorkflow(path)
	require.ErrorContains(t, err, `jobs.build.outputs.result references discarded regular step "b"`)
	require.NoFileExists(t, filepath.Join(dir, "workflow.lock.yml"))
}

func TestImportedJobOutputsCompile(t *testing.T) {
	for _, jobName := range []string{"build", "pre_activation"} {
		t.Run(jobName, func(t *testing.T) {
			tmpDir := testutil.TempDir(t, "imported-job-outputs")
			for _, name := range []string{"first", "second"} {
				component := fmt.Sprintf(`---
jobs:
  %s:
    outputs:
      %s: ${{ steps.%s.outputs.value }}
      shared: %s
    pre-steps:
      - id: %s
        run: echo "value=%s" >> "$GITHUB_OUTPUT"
---
`, jobName, name, name, name, name, name)
				require.NoError(t, os.WriteFile(filepath.Join(tmpDir, name+".md"), []byte(component), 0o644))
			}
			customJobFields := ""
			if jobName == "build" {
				customJobFields = "    runs-on: ubuntu-latest\n"
			}
			workflow := fmt.Sprintf(`---
on: workflow_dispatch
engine: copilot
strict: false
permissions:
  contents: read
imports:
  - ./first.md
  - ./second.md
jobs:
  %s:
%s    outputs:
      main: ${{ steps.main.outputs.value }}
      shared: main
    pre-steps:
      - id: main
        run: echo "value=main" >> "$GITHUB_OUTPUT"
---
Test imported job outputs.
`, jobName, customJobFields)
			workflowPath := filepath.Join(tmpDir, "workflow.md")
			require.NoError(t, os.WriteFile(workflowPath, []byte(workflow), 0o644))
			require.NoError(t, NewCompiler().CompileWorkflow(workflowPath))
			lockContent, err := os.ReadFile(filepath.Join(tmpDir, "workflow.lock.yml"))
			require.NoError(t, err)
			var compiled struct {
				Jobs map[string]struct {
					Outputs map[string]string `yaml:"outputs"`
					Steps   []map[string]any  `yaml:"steps"`
				} `yaml:"jobs"`
			}
			require.NoError(t, yaml.Unmarshal(lockContent, &compiled))
			job := compiled.Jobs[jobName]
			for _, name := range []string{"first", "second", "main"} {
				require.Equal(t, "${{ steps."+name+".outputs.value }}", job.Outputs[name])
			}
			require.Equal(t, "main", job.Outputs["shared"])
			var stepIDs []string
			for _, step := range job.Steps {
				if id, ok := step["id"].(string); ok && (id == "first" || id == "second" || id == "main") {
					stepIDs = append(stepIDs, id)
				}
			}
			require.Equal(t, []string{"first", "second", "main"}, stepIDs)
		})
	}
}

// TestMergeJobsFromYAMLImports_MultipleImportsPreserveOrder ensures that when
// more than one import defines the same built-in job (e.g. jobs.activation),
// their injected step fields are accumulated in import declaration order
// before the main workflow's steps are appended, rather than being reversed.
func TestMergeJobsFromYAMLImports_MultipleImportsPreserveOrder(t *testing.T) {
	c := &Compiler{}

	importA := map[string]any{
		"activation": map[string]any{
			"steps": []any{
				map[string]any{"name": "step-A"},
			},
		},
	}
	importB := map[string]any{
		"activation": map[string]any{
			"steps": []any{
				map[string]any{"name": "step-B"},
			},
		},
	}

	jsonA, err := json.Marshal(importA)
	require.NoError(t, err)
	jsonB, err := json.Marshal(importB)
	require.NoError(t, err)

	mergedJobsJSON := string(jsonA) + "\n" + string(jsonB)

	mainJobs := map[string]any{
		"activation": map[string]any{
			"steps": []any{
				map[string]any{"name": "step-main"},
			},
		},
	}

	result, err := c.mergeJobsFromYAMLImports(mainJobs, mergedJobsJSON)
	require.NoError(t, err)

	activationJob, ok := result["activation"].(map[string]any)
	require.True(t, ok, "expected activation job in merged result")

	stepsAny, ok := activationJob["steps"].([]any)
	require.True(t, ok, "expected steps field on activation job")
	require.Len(t, stepsAny, 3)

	var names []string
	for _, s := range stepsAny {
		stepMap, ok := s.(map[string]any)
		require.True(t, ok)
		name, ok := stepMap["name"].(string)
		require.True(t, ok)
		names = append(names, name)
	}

	require.Equal(t, []string{"step-A", "step-B", "step-main"}, names)
}
