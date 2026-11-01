//go:build !integration

package workflow

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/workflow/compilerenv"
	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func evalsTestData() *WorkflowData {
	return &WorkflowData{Evals: &EvalsConfig{Questions: []EvalDefinition{
		{ID: "builds", Question: "Does it build?"},
		{ID: "tests", Question: "Did tests pass?"},
	}}}
}

func TestEvalsDecisionsIndependentOfAgentEngine(t *testing.T) {
	compiler := NewCompiler()
	for _, engine := range []string{"copilot", "claude", "codex", "pi", "custom"} {
		t.Run(engine, func(t *testing.T) {
			data := evalsTestData()
			data.AI = engine
			data.Model = "unrelated-agent-model"
			data.EngineConfig = &EngineConfig{ID: engine, Command: "custom-command", HarnessScript: "custom.cjs", Agent: "custom-agent"}
			steps := strings.Join(compiler.buildEvalsJobSteps(data), "")
			assert.Contains(t, steps, "name: Execute BinEval Decisions")
			assert.Contains(t, steps, "id: evals_decisions_execution")
			assert.Contains(t, steps, `GH_AW_EVALS_MODEL: "gpt-6-luna"`)
			assert.Contains(t, steps, "GH_AW_DECISIONS_BASE_URL: http://api-proxy:10002")
			assert.Contains(t, steps, "GH_AW_PHASE: evals")
			assert.Contains(t, steps, "run_evals.cjs")
			assert.Contains(t, steps, "--env-all")
			for _, excluded := range []string{"copilot_harness", "claude_harness", "codex_harness", "shell_harness", "custom-command", "custom-agent", "custom.cjs", "unrelated-agent-model", "Install GitHub Copilot", "Install Claude", "Install Codex", "Setup BinEval", "Ensure evals directory", "Download container images"} {
				assert.NotContains(t, steps, excluded)
			}
			assert.Equal(t, "custom-command", data.EngineConfig.Command)
		})
	}
}

func TestEvalsDecisionsQuestionsAreYAMLData(t *testing.T) {
	data := evalsTestData()
	data.Evals.Questions = []EvalDefinition{{ID: `id'"\\;$(touch /tmp/pwned)`, Question: "line one\nline two; `echo unsafe`"}}
	var steps []struct {
		Env map[string]string `yaml:"env"`
	}
	require.NoError(t, yaml.Unmarshal([]byte(strings.Join(NewCompiler().buildEvalsDecisionSteps(data), "")), &steps))
	var decoded []struct{ ID, Question string }
	for _, step := range steps {
		if raw, ok := step.Env["GH_AW_EVALS_QUESTIONS"]; ok {
			require.NoError(t, json.Unmarshal([]byte(raw), &decoded))
		}
	}
	require.Len(t, decoded, 1)
	assert.Equal(t, data.Evals.Questions[0].ID, decoded[0].ID)
	assert.Equal(t, data.Evals.Questions[0].Question, decoded[0].Question)
}

func TestEvalsDecisionsModelAndProvider(t *testing.T) {
	compiler := NewCompiler()
	data := evalsTestData()
	data.Model = "claude-sonnet-4.6"
	assert.Equal(t, "gpt-6-luna", compiler.resolveEvalsExecutionModel(data))
	t.Setenv(compilerenv.DefaultEvalsModel, "copilot/decision-model")
	assert.Equal(t, "copilot/decision-model", compiler.resolveEvalsExecutionModel(data))
	data.Evals.Model = "openai/gpt-6-luna"
	assert.Equal(t, data.Evals.Model, compiler.resolveEvalsExecutionModel(data))
	steps := strings.Join(compiler.buildEvalsDecisionSteps(data), "")
	assert.Contains(t, steps, "GH_AW_DECISIONS_BASE_URL: http://api-proxy:10000")
	assert.Contains(t, steps, "secrets.CODEX_API_KEY || secrets.OPENAI_API_KEY")
	assert.NotContains(t, steps, "COPILOT_GITHUB_TOKEN:")
}

func TestEvalsDecisionsRuntimeOverridesAndTopology(t *testing.T) {
	for _, topology := range []RunnerTopology{"", RunnerTopologyArcDind} {
		t.Run(string(topology), func(t *testing.T) {
			data := evalsTestData()
			data.RunnerConfig = &RunnerConfig{Topology: topology}
			data.Runtimes = map[string]any{"node": map[string]any{
				"action-repo": "myorg/setup-node", "action-version": "main", "version": "24.21.0",
			}}
			steps := strings.Join(NewCompiler().buildEvalsDecisionSteps(data), "")
			assert.Equal(t, 1, strings.Count(steps, "- name: Setup Node.js"))
			assert.Contains(t, steps, "uses: myorg/setup-node@main")
			assert.Contains(t, steps, "node-version: '24.21.0'")
			if topology == RunnerTopologyArcDind {
				assert.Contains(t, steps, `--mount "${RUNNER_TEMP}/gh-aw:${RUNNER_TEMP}/gh-aw:ro"`)
			}
			assert.NotContains(t, steps, "Copy Copilot CLI")
		})
	}
}

func TestEvalsDecisionsFailureAndPublicationContract(t *testing.T) {
	data := evalsTestData()
	data.Evals.Model = "${{ inputs.model }}"
	steps := strings.Join(NewCompiler().buildEvalsJobSteps(data), "")
	assert.Contains(t, steps, "GH_AW_EVALS_MODEL: ${{ inputs.model }}")
	assert.Contains(t, steps, "GH_AW_MODEL_FALLBACK: ${{ vars.GH_AW_DEFAULT_EVALS_MODEL || 'gpt-6-luna' }}")
	assert.Contains(t, steps, "GH_AW_EVALS_EXECUTION_OUTCOME: ${{ steps.evals_decisions_execution.outcome }}")
	assert.NotContains(t, steps, "steps.evals_decisions_execution.conclusion")
	initialize := strings.Index(steps, "name: Initialize evals execution evidence")
	install := strings.Index(steps, "name: Install AWF binary")
	execute := strings.Index(steps, "id: evals_decisions_execution")
	started := strings.Index(steps, `"state":"started"`)
	require.Greater(t, install, initialize)
	require.Greater(t, execute, install)
	require.Greater(t, started, execute)
	redact := strings.Index(steps, "name: Redact secrets")
	render := strings.Index(steps, "name: Render evals results")
	upload := strings.Index(steps, "name: Upload evals results")
	require.Greater(t, render, redact)
	require.Greater(t, upload, render)
	assert.Equal(t, 2, strings.Count(steps, "if: always() && steps.redact_evals_results.outcome == 'success'"))
	for _, expected := range []string{
		evalsExecutionEvidencePath, evalsResultsPath, evalsStatusPath, "/tmp/gh-aw/evals_token_usage.jsonl",
		`cp "$AWF_TOKEN_USAGE_LOG" /tmp/gh-aw/evals_token_usage.jsonl`,
		"name: Upload evals accounting after failure", "vars.GH_AW_DEFAULT_EVALS_MAX_AI_CREDITS",
	} {
		assert.Contains(t, steps, expected)
	}
	assert.NotContains(t, steps, "vars.GH_AW_DEFAULT_DETECTION_MAX_AI_CREDITS")
}

func TestEvalsDecisionsSecretAndOTLPIsolation(t *testing.T) {
	data := evalsTestData()
	data.Env = "env:\n  OTEL_EXPORTER_OTLP_ENDPOINT: https://traces.example.com\n"
	data.Evals.Model = "${{ secrets.EVAL_MODEL }}"
	data.Evals.Questions[0].Question = "Does it use ${{ secrets.EVAL_PROMPT }}?"
	compiler := NewCompiler()
	execution := strings.Join(compiler.buildEvalsDecisionSteps(data), "")
	for _, key := range append(otlpSandboxExcludedEnvVarNames, "COPILOT_GITHUB_TOKEN") {
		assert.Contains(t, execution, "--exclude-env "+key)
	}
	assert.Nil(t, data.ExcludedEnv)
	redaction := strings.Join(compiler.buildRedactEvalsSecretsStep(data), "")
	assert.Contains(t, redaction, "SECRET_EVAL_MODEL: ${{ secrets.EVAL_MODEL }}")
	assert.Contains(t, redaction, "SECRET_EVAL_PROMPT: ${{ secrets.EVAL_PROMPT }}")
}

func TestEvalsDecisionsAccountingTopology(t *testing.T) {
	data := evalsTestData()
	data.RunnerConfig = &RunnerConfig{Topology: RunnerTopologyArcDind}
	steps := strings.Join(NewCompiler().buildUploadEvalsArtifactStep(data), "")
	assert.Contains(t, steps, "${RUNNER_TEMP}/gh-aw/sandbox/firewall/logs")
	assert.Contains(t, steps, "${RUNNER_TEMP}/gh-aw/sandbox/firewall/audit")
	assert.NotContains(t, steps, "/tmp/gh-aw/sandbox/firewall")
	assert.Contains(t, strings.Join(buildUsageArtifactUploadSteps("", true, "", false, func(action string) string { return action }), ""), "/tmp/gh-aw/usage/evals/token_usage.jsonl")
}

func TestEvalsDecisionsPermissions(t *testing.T) {
	data := evalsTestData()
	data.Permissions = "permissions:\n  contents: read\n  copilot-requests: write"
	compiler := NewCompiler()
	_, permissions := compiler.buildEvalsJobPermissions(data)
	assert.Contains(t, permissions, "copilot-requests: write")
	data.Evals.Model = "openai/gpt-6-luna"
	_, permissions = compiler.buildEvalsJobPermissions(data)
	assert.NotContains(t, permissions, "copilot-requests")
	assert.Contains(t, permissions, "contents: read")
	assert.NotContains(t, permissions, "id-token")
}

func compileClaudeEvalsWorkflow(t *testing.T, engineYAML string) (string, string) {
	t.Helper()
	file := filepath.Join(t.TempDir(), "claude-evals.md")
	source := "---\non: workflow_dispatch\npermissions:\n  contents: read\n" + engineYAML +
		"safe-outputs:\n  add-comment:\nevals:\n  - id: docs\n    question: Were the docs reviewed?\n---\nReview documentation.\n"
	require.NoError(t, os.WriteFile(file, []byte(source), 0o600))
	require.NoError(t, NewCompiler().CompileWorkflow(file))
	content, err := os.ReadFile(strings.TrimSuffix(file, ".md") + ".lock.yml")
	require.NoError(t, err)
	return extractJobSection(string(content), "agent"), extractJobSection(string(content), "evals")
}

func TestEvalsDecisionsCompiledJob(t *testing.T) {
	agent, evals := compileClaudeEvalsWorkflow(t, "engine:\n  id: claude\n")
	assert.Contains(t, agent, "claude_harness.cjs")
	assert.Contains(t, evals, "Execute BinEval Decisions")
	assert.Contains(t, evals, "run_evals.cjs")
	assert.Contains(t, evals, `GH_AW_EVALS_MODEL: "gpt-6-luna"`)
	assert.NotContains(t, evals, "claude_harness.cjs")
	assert.NotContains(t, evals, "copilot_harness.cjs")
	assert.NotContains(t, evals, "Install Claude")
	assert.NotContains(t, evals, "Parse MCP Gateway logs")
	assert.Contains(t, evals, "id: parse_evals_token_usage")
	assert.Contains(t, evals, "${{ steps.parse_evals_token_usage.outputs.aic }}")
	assert.Contains(t, evals, "summarizeUsageMain")
}

func TestDailyAICEvalsAccountingTransport(t *testing.T) {
	compiler := NewCompiler()
	steps := strings.Join(compiler.buildUploadEvalsArtifactStep(&WorkflowData{}), "")
	for _, expected := range []string{
		"name: Collect evals token usage",
		"if: always()",
		"/tmp/gh-aw/evals_token_usage.jsonl",
		evalsExecutionEvidencePath,
		"/tmp/gh-aw/evals.jsonl",
		"/tmp/gh-aw/evals_status.json",
		"if: always() && steps.redact_evals_results.outcome == 'success'",
		"name: Upload evals accounting after failure",
		"if: always() && steps.redact_evals_results.outcome != 'success'",
	} {
		if !strings.Contains(steps, expected) {
			t.Errorf("evals accounting transport missing %q", expected)
		}
	}
	usage := strings.Join(buildUsageArtifactUploadSteps("", true, "", false, func(action string) string { return action }), "")
	if !strings.Contains(usage, "/tmp/gh-aw/usage/evals/token_usage.jsonl") {
		t.Fatal("conclusion must publish evals token usage")
	}
	if !strings.Contains(usage, "/tmp/gh-aw/usage/evals/execution.json") {
		t.Fatal("conclusion must publish evals execution evidence")
	}
	if !strings.Contains(usage, "/tmp/gh-aw/usage/evals/evals_status.json") {
		t.Fatal("conclusion must publish the evals not-evaluated marker")
	}
	script, err := os.ReadFile("../../actions/setup/sh/collect_usage_artifact_files.sh")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(script), "cp /tmp/gh-aw/evals/evals_token_usage.jsonl /tmp/gh-aw/usage/evals/token_usage.jsonl") {
		t.Fatal("collector must retain evals accounting separately from evaluation results")
	}
	if !strings.Contains(string(script), "mkdir -p /tmp/gh-aw/usage/evals && cp /tmp/gh-aw/evals/execution.json /tmp/gh-aw/usage/evals/execution.json") {
		t.Fatal("collector must retain evals execution evidence")
	}
	if !strings.Contains(string(script), "cp /tmp/gh-aw/evals/evals_status.json /tmp/gh-aw/usage/evals/evals_status.json") {
		t.Fatal("collector must retain the evals not-evaluated marker beside execution evidence")
	}
	if strings.Contains(string(script), "evals_status.json /tmp/gh-aw/usage/evals.jsonl") {
		t.Fatal("collector must not publish the marker as an answer file")
	}
}

func TestDailyAICEvalsCollectorTopology(t *testing.T) {
	for _, topology := range []string{"default", "arc-dind"} {
		t.Run(topology, func(t *testing.T) {
			data := &WorkflowData{
				AI: "copilot",
				Evals: &EvalsConfig{
					Questions: []EvalDefinition{{ID: "example", Question: "Is the result valid?"}},
				},
			}
			root := "/tmp/gh-aw/sandbox/firewall"
			if topology == "arc-dind" {
				data.RunnerConfig = &RunnerConfig{Topology: RunnerTopologyArcDind}
				root = "${RUNNER_TEMP}/gh-aw/sandbox/firewall"
			}
			compiler := NewCompiler()
			for _, steps := range [][]string{
				compiler.buildUploadEvalsArtifactStep(data),
				compiler.buildEvalsJobSteps(data),
			} {
				text := strings.Join(steps, "")
				start := strings.Index(text, "      - name: Collect evals token usage\n")
				end := strings.Index(text, "      - name: Upload evals results\n")
				if start < 0 || end <= start {
					t.Fatal("missing evals collector or upload step")
				}
				collector := text[start:end]
				for _, expected := range []string{root + "/audit", root + "/logs", "cp \"$source\" /tmp/gh-aw/evals_token_usage.jsonl"} {
					if !strings.Contains(collector, expected) {
						t.Errorf("collector missing %q:\n%s", expected, collector)
					}
				}
				if strings.Contains(collector, "firewall-audit-logs") ||
					(topology == "arc-dind" && strings.Contains(collector, "/tmp/gh-aw/sandbox/firewall")) {
					t.Errorf("collector must not fall back to stale agent paths:\n%s", collector)
				}
			}
		})
	}
}

func TestDailyAICEvalsCollectorExportedLog(t *testing.T) {
	for _, topology := range []string{"default", "arc-dind"} {
		for _, tt := range []struct {
			name     string
			exported string
			files    map[string]string
			want     string
			missing  bool
		}{
			{
				name:     "exported log takes precedence",
				exported: "preserved logs/token-usage.jsonl",
				files: map[string]string{
					"preserved logs/token-usage.jsonl":                        `{"ai_credits_this_response":2}`,
					"sandbox/firewall/audit/api-proxy-logs/token-usage.jsonl": `{"ai_credits_this_response":9}`,
					"sandbox/firewall/logs/api-proxy-logs/token-usage.jsonl":  `{"ai_credits_this_response":10}`,
				},
				want: `{"ai_credits_this_response":2}`,
			},
			{
				name:     "empty exported log does not revive stale usage",
				exported: "preserved/token-usage.jsonl",
				files: map[string]string{
					"preserved/token-usage.jsonl":                            "",
					"sandbox/firewall/logs/api-proxy-logs/token-usage.jsonl": `{"ai_credits_this_response":10}`,
				},
			},
			{
				name:  "legacy audit log",
				files: map[string]string{"sandbox/firewall/audit/api-proxy-logs/token-usage.jsonl": `{"ai_credits_this_response":3}`},
				want:  `{"ai_credits_this_response":3}`,
			},
			{
				name:  "legacy proxy log",
				files: map[string]string{"sandbox/firewall/logs/api-proxy-logs/token-usage.jsonl": `{"ai_credits_this_response":4}`},
				want:  `{"ai_credits_this_response":4}`,
			},
			{
				name:     "missing exported log falls back",
				exported: "missing/token-usage.jsonl",
				files:    map[string]string{"sandbox/firewall/logs/api-proxy-logs/token-usage.jsonl": `{"ai_credits_this_response":4}`},
				want:     `{"ai_credits_this_response":4}`,
			},
			{name: "missing accounting stays missing", missing: true},
			{
				name:    "empty legacy log stays unresolved",
				files:   map[string]string{"sandbox/firewall/logs/api-proxy-logs/token-usage.jsonl": ""},
				missing: true,
			},
		} {
			t.Run(topology+"/"+tt.name, func(t *testing.T) {
				data := &WorkflowData{}
				if topology == "arc-dind" {
					data.RunnerConfig = &RunnerConfig{Topology: RunnerTopologyArcDind}
				}
				var steps []struct {
					Name string `yaml:"name"`
					Run  string `yaml:"run"`
				}
				if err := yaml.Unmarshal([]byte(strings.Join(NewCompiler().buildUploadEvalsArtifactStep(data), "")), &steps); err != nil {
					t.Fatal(err)
				}
				if len(steps) == 0 || steps[0].Name != "Collect evals token usage" || steps[0].Run == "" {
					t.Fatal("missing evals accounting collector")
				}
				directory := t.TempDir()
				for name, content := range tt.files {
					file := filepath.Join(directory, name)
					if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
						t.Fatal(err)
					}
				}
				exported := ""
				if tt.exported != "" {
					exported = filepath.Join(directory, tt.exported)
				}
				t.Setenv("AWF_TOKEN_USAGE_LOG", exported)
				t.Setenv("RUNNER_TEMP", directory)
				script := strings.NewReplacer("${RUNNER_TEMP}/gh-aw", directory, "/tmp/gh-aw", directory).Replace(steps[0].Run)
				output, err := exec.Command("bash", "-e", "-u", "-c", script).CombinedOutput()
				if err != nil {
					t.Fatalf("collector failed: %v\n%s", err, output)
				}
				content, err := os.ReadFile(filepath.Join(directory, "evals_token_usage.jsonl"))
				if tt.missing {
					if !os.IsNotExist(err) {
						t.Fatalf("unresolved accounting must not create a placeholder: %q, %v", content, err)
					}
				} else if err != nil || string(content) != tt.want {
					t.Fatalf("unexpected accounting: got %q, %v; want %q", content, err, tt.want)
				}
			})
		}
	}
}
