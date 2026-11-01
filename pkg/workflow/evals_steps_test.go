//go:build !integration

package workflow

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/workflow/compilerenv"
	"github.com/goccy/go-yaml"
)

func TestBuildSetupEvalsStepEscapesQuestionData(t *testing.T) {
	questions := []EvalDefinition{
		{ID: `id'"\\;$(touch /tmp/pwned)`, Question: "line one\nline two; `echo unsafe`"},
	}
	data := &WorkflowData{Evals: &EvalsConfig{Questions: questions}}

	var steps []struct {
		Env map[string]string `yaml:"env"`
	}
	if err := yaml.Unmarshal([]byte(strings.Join(NewCompiler().buildSetupEvalsStep(data), "")), &steps); err != nil {
		t.Fatalf("generated evals step should be valid YAML: %v", err)
	}
	if len(steps) != 1 {
		t.Fatalf("expected one setup step, got %d", len(steps))
	}

	var decoded []struct {
		ID       string `json:"id"`
		Question string `json:"question"`
	}
	if err := json.Unmarshal([]byte(steps[0].Env["GH_AW_EVALS_QUESTIONS"]), &decoded); err != nil {
		t.Fatalf("evals question environment value should be valid JSON: %v", err)
	}
	if len(decoded) != 1 || decoded[0].ID != questions[0].ID || decoded[0].Question != questions[0].Question {
		t.Fatalf("evals question data changed during serialization: got %#v", decoded)
	}
}

func TestEvalsBackendRuntimeContract(t *testing.T) {
	for _, backend := range []string{"", "auto", "llm", "decisions"} {
		for _, model := range []string{"", "gpt-6-luna"} {
			t.Run(backend+"/"+model, func(t *testing.T) {
				data := &WorkflowData{
					AI: "copilot", Model: "workflow-model",
					Evals: &EvalsConfig{Backend: backend, Model: model, Questions: []EvalDefinition{
						{ID: "q1", Question: "Correct?", Model: "gpt-6-luna"},
						{ID: "q2", Question: "Safe?"},
					}},
				}
				var steps []struct {
					Name string            `yaml:"name"`
					ID   string            `yaml:"id"`
					If   string            `yaml:"if"`
					Env  map[string]string `yaml:"env"`
				}
				text := strings.Join(NewCompiler().buildEvalsJobSteps(data), "")
				if err := yaml.Unmarshal([]byte(text), &steps); err != nil {
					t.Fatalf("invalid generated YAML: %v", err)
				}
				setup := -1
				cleanup := -1
				for i, step := range steps {
					if step.Name == "Clean stale firewall files from agent artifact" {
						cleanup = i
						if step.If != "" {
							t.Fatal("downloaded agent firewall cleanup must not depend on backend routing")
						}
					}
					if step.ID == "evals_setup" {
						setup = i
						if step.Env["GH_AW_EVALS_BACKEND"] != evalsBackend(data.Evals) ||
							step.Env["GH_AW_EVALS_DEFAULT_MODEL"] != model {
							t.Fatalf("incorrect setup routing environment: %#v", step.Env)
						}
						if backend == "llm" {
							if _, exists := step.Env["OPENAI_API_KEY"]; exists {
								t.Fatal("llm backend must not receive the Decisions credential")
							}
						} else if step.Env["OPENAI_API_KEY"] != "${{ secrets.OPENAI_API_KEY }}" {
							t.Fatal("auto/decisions setup requires the Decisions credential")
						}
					}
					if step.ID != "evals_setup" && step.Env["OPENAI_API_KEY"] == "${{ secrets.OPENAI_API_KEY }}" {
						t.Fatalf("Decisions credential leaked into %s", step.Name)
					}
					if step.Name == "Setup BinEval evaluations" || step.Name == "Parse BinEval results" {
						var questions []map[string]string
						if err := json.Unmarshal([]byte(step.Env["GH_AW_EVALS_QUESTIONS"]), &questions); err != nil {
							t.Fatal(err)
						}
						if questions[0]["model"] != "gpt-6-luna" {
							t.Fatal("per-question model lost in transport")
						}
						if _, ok := questions[1]["model"]; ok {
							t.Fatal("empty model should be omitted")
						}
					}
					if strings.Contains(step.If, "steps.evals_setup.outputs.run_llm") && (setup < 0 || i <= setup) {
						t.Fatal("LLM step precedes backend routing")
					}
				}
				if setup < 0 {
					t.Fatal("missing evals_setup")
				}
				if cleanup < 0 || cleanup >= setup {
					t.Fatal("agent firewall cleanup must precede backend routing for every backend")
				}
				if backend == "decisions" && (strings.Contains(text, "evals_agentic_execution") ||
					strings.Contains(text, "Download container images") || strings.Contains(text, "Install AWF binary")) {
					t.Fatal("explicit decisions must omit LLM installation/execution")
				}
			})
		}
	}
}

func TestGateEvalsLLMStepsPreservesConditions(t *testing.T) {
	input := []string{
		"      - name: First\n", "        if: ${{ always() && steps.previous.outcome == 'success' }}\n", "        run: echo first\n",
		"      - name: Second\n", "        run: echo second\n",
	}
	text := strings.Join(gateEvalsLLMSteps(input), "")
	if !strings.Contains(text, "if: (always() && steps.previous.outcome == 'success') && steps.evals_setup.outputs.run_llm == 'true'") {
		t.Fatal("existing condition was not preserved")
	}
	if strings.Count(text, "steps.evals_setup.outputs.run_llm == 'true'") != 2 ||
		strings.Count(text, "        if:") != 2 {
		t.Fatal("all steps must have exactly one routing condition")
	}
}

func TestEvalsLLMEngineStepsAreAllGated(t *testing.T) {
	for _, engineID := range []string{"copilot", "claude", "codex", "pi"} {
		t.Run(engineID, func(t *testing.T) {
			data := &WorkflowData{AI: engineID, EngineConfig: &EngineConfig{ID: engineID}}
			var steps []struct {
				Name string `yaml:"name"`
				If   string `yaml:"if"`
			}

			text := strings.Join(NewCompiler().buildEvalsEngineSteps(data), "")
			if err := yaml.Unmarshal([]byte(text), &steps); err != nil {
				t.Fatalf("invalid YAML: %v", err)
			}
			for _, step := range steps {
				if !strings.Contains(step.If, "steps.evals_setup.outputs.run_llm == 'true'") {
					t.Errorf("ungated LLM step %q", step.Name)
				}
			}
		})
	}
}
func TestPrepareEvalsFilesPreservesExecutionTrace(t *testing.T) {
	steps := strings.Join(buildPrepareEvalsFilesStep(), "")
	for _, expected := range []string{
		"cp /tmp/gh-aw/agent_output.json /tmp/gh-aw/evals/agent_output.json",
		"cp /tmp/gh-aw/agent-session.jsonl /tmp/gh-aw/evals/agent-session.jsonl 2>/dev/null || true",
		"cp /tmp/gh-aw/aw-prompts/prompt.txt /tmp/gh-aw/evals/prompt.txt",
	} {
		if !strings.Contains(steps, expected) {
			t.Errorf("missing evals context preparation: %s", expected)
		}
	}
	if strings.Contains(steps, "usage/aw_session.jsonl") {
		t.Fatal("evals must not depend on conclusion-generated unified session")
	}
}

func TestEvalsMixedRoutingExecutionModel(t *testing.T) {
	for _, tt := range []struct {
		name    string
		backend string
		model   string
		want    string
	}{
		{name: "default auto", want: "small"},
		{name: "explicit auto", backend: "auto", want: "small"},
		{name: "decisions default", backend: "auto", model: "gpt-6-luna", want: "small"},
		{name: "other explicit default preserved", backend: "auto", model: "other-model", want: "other-model"},
		{name: "llm unchanged", backend: "llm", model: "gpt-6-luna", want: "gpt-6-luna"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			data := &WorkflowData{
				AI: "copilot", Model: "workflow-model",
				Evals: &EvalsConfig{Backend: tt.backend, Model: tt.model, Questions: []EvalDefinition{
					{ID: "decisions", Question: "Correct?", Model: "gpt-6-luna"},
					{ID: "llm", Question: "Safe?", Model: "small"},
					{ID: "later", Question: "Focused?", Model: "other-model"},
				}},
			}
			compiler := NewCompiler()
			if got := compiler.resolveEvalsExecutionModel(data); got != tt.want {
				t.Fatalf("execution model = %q, want %q", got, tt.want)
			}
			for _, steps := range [][]string{compiler.buildSetupEvalsStep(data), compiler.buildParseEvalsResultsStep(data)} {
				if !strings.Contains(strings.Join(steps, ""), fmt.Sprintf("GH_AW_EVALS_MODEL: %q", tt.want)) {
					t.Fatal("setup/parse model must match the LLM execution model")
				}
			}
		})
	}
}

func TestEvalsAutoPreservesDefaultWithoutLLMQuestionOverride(t *testing.T) {
	data := &WorkflowData{
		AI: "copilot",
		Evals: &EvalsConfig{Model: "other-model", Questions: []EvalDefinition{
			{ID: "decisions", Question: "Correct?", Model: "gpt-6-luna"},
			{ID: "llm", Question: "Safe?"},
		}},
	}
	if got := NewCompiler().resolveEvalsExecutionModel(data); got != "other-model" {
		t.Fatalf("default model must be preserved without a LLM question override, got %q", got)
	}
}

func TestEvalsCollectorRequiresLLMExecution(t *testing.T) {
	var steps []struct {
		Name string `yaml:"name"`
		If   string `yaml:"if"`
	}
	text := strings.Join(NewCompiler().buildUploadEvalsArtifactStep(&WorkflowData{}), "")
	if err := yaml.Unmarshal([]byte(text), &steps); err != nil {
		t.Fatal(err)
	}
	if len(steps) == 0 || steps[0].Name != "Collect evals token usage" ||
		steps[0].If != "always() && steps.evals_setup.outputs.run_llm == 'true'" {
		t.Fatal("Decisions-only evaluations must not collect downloaded agent firewall usage")
	}
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
}

func TestEvalsExecutionEvidence(t *testing.T) {
	compiler := NewCompiler()
	data := &WorkflowData{
		AI: "copilot",
		Evals: &EvalsConfig{
			Questions: []EvalDefinition{{ID: "example", Question: "Is the result valid?"}},
		},
	}

	steps := strings.Join(compiler.buildEvalsJobSteps(data), "")
	initialize := strings.Index(steps, "name: Initialize evals execution evidence")
	install := strings.Index(steps, "name: Install AWF binary")
	execution := strings.Index(steps, "id: evals_agentic_execution")
	started := strings.Index(steps, `"state":"started"`)
	if initialize < 0 || install <= initialize || execution <= install || started <= execution {
		t.Fatalf("expected evals execution evidence to distinguish setup failures from started execution:\n%s", steps)
	}
	if !strings.Contains(steps[initialize:install], "if: always()") {
		t.Fatalf("expected evals execution initializer to run after earlier failures:\n%s", steps[initialize:install])
	}
}

func TestEvalsOTLPEnvExcludedFromSandbox(t *testing.T) {
	for _, engineID := range []string{"copilot", "claude", "codex", "pi"} {
		for _, tt := range []struct {
			name   string
			env    string
			dryRun bool
			want   bool
		}{
			{name: "configured endpoint", env: "env:\n  OTEL_EXPORTER_OTLP_ENDPOINT: https://traces.example.com\n", want: true},
			{name: "multiple endpoints", env: "env:\n  GH_AW_OTLP_ENDPOINTS: '[{\"url\":\"https://traces.example.com\",\"headers\":\"Authorization=synthetic-token\"}]'\n  GH_AW_OTLP_ALL_HEADERS: Authorization=synthetic-token\n", want: true},
			{name: "no endpoint", env: "env:\n  OTHER: value\n"},
			{name: "dry run", env: "env:\n  OTEL_EXPORTER_OTLP_ENDPOINT: https://traces.example.com\n", dryRun: true},
		} {
			t.Run(engineID+"/"+tt.name, func(t *testing.T) {
				data := &WorkflowData{
					AI:           engineID,
					EngineConfig: &EngineConfig{ID: engineID},
					Env:          tt.env,
					DryRun:       tt.dryRun,
				}
				steps := strings.Join(NewCompiler().buildEvalsEngineSteps(data), "")
				if !strings.Contains(steps, "--env-all") {
					t.Fatal("evals must execute through AWF")
				}
				for _, name := range []string{
					"OTEL_EXPORTER_OTLP_ENDPOINT",
					"OTEL_EXPORTER_OTLP_HEADERS",
					"GH_AW_OTLP_ENDPOINTS",
					"GH_AW_OTLP_ALL_HEADERS",
				} {
					wantCount := 0
					if tt.want {
						wantCount = 1
					}
					if got := strings.Count(steps, "--exclude-env "+name); got != wantCount {
						t.Errorf("expected %d exclusions for %s, got %d", wantCount, name, got)
					}
				}
				if strings.Contains(steps, "https://traces.example.com") {
					t.Fatal("evals steps must not copy the host OTLP endpoint")
				}
				if data.ExcludedEnv != nil {
					t.Fatal("evals must not mutate the parent workflow exclusions")
				}
			})
		}
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

// TestBuildEvalsEngineStepsHonorsNodeActionOverride verifies that the evals job's
// Setup Node.js step honors runtimes.node.action-repo / action-version.
func TestBuildEvalsEngineStepsHonorsNodeActionOverride(t *testing.T) {
	compiler := NewCompiler()
	const customRef = "myorg/myrepo/.github/actions/setup-node@main"

	for _, engineID := range []string{"copilot", "claude"} {
		t.Run(engineID, func(t *testing.T) {
			data := &WorkflowData{
				AI: engineID,
				Runtimes: map[string]any{
					"node": map[string]any{
						"action-repo":    "myorg/myrepo/.github/actions/setup-node",
						"action-version": "main",
					},
				},
				Evals: &EvalsConfig{
					Questions: []EvalDefinition{
						{ID: "test", Question: "Does the code work?"},
					},
				},
			}

			allSteps := strings.Join(compiler.buildEvalsEngineSteps(data), "")
			if !strings.Contains(allSteps, "uses: "+customRef) {
				t.Errorf("expected Setup Node.js to use %q;\ngot:\n%s", customRef, allSteps)
			}
			if strings.Contains(allSteps, "uses: actions/setup-node@") {
				t.Errorf("expected no default actions/setup-node reference;\ngot:\n%s", allSteps)
			}
		})
	}
}

func TestBuildEvalsEngineStepsHonorsNodeVersion(t *testing.T) {
	compiler := NewCompiler()
	for _, engineID := range []string{"copilot", "claude", "codex"} {
		t.Run(engineID, func(t *testing.T) {
			data := &WorkflowData{
				AI:       engineID,
				Runtimes: map[string]any{"node": map[string]any{"version": "24.21.0"}},
				Evals: &EvalsConfig{
					Questions: []EvalDefinition{{ID: "test", Question: "Does the code work?"}},
				},
			}
			steps := strings.Join(compiler.buildEvalsEngineSteps(data), "")
			if count := strings.Count(steps, "- name: Setup Node.js"); count != 1 {
				t.Fatalf("expected one Setup Node.js step, got %d:\n%s", count, steps)
			}
			if !strings.Contains(steps, "node-version: '24.21.0'") {
				t.Errorf("expected pinned Node version in evals steps:\n%s", steps)
			}
		})
	}
}

// TestBuildEvalsEngineStepsArcDindTopology verifies that the evals job
// correctly propagates arc-dind runner topology from the main workflow data.
// Regression: before the fix, RunnerConfig was not propagated to evalsData,
// so isArcDindTopology(evalsData) was always false — the Copilot staging step
// was never emitted and the engine was spawned as /usr/local/bin/copilot (ENOENT inside
// the AWF chroot which uses the dind daemon's filesystem).
func TestBuildEvalsEngineStepsArcDindTopology(t *testing.T) {
	compiler := NewCompiler()

	t.Run("arc-dind: emits daemon-visible staging step and uses RUNNER_TEMP copilot path", func(t *testing.T) {
		data := &WorkflowData{
			AI: "copilot",
			RunnerConfig: &RunnerConfig{
				Topology: RunnerTopologyArcDind,
			},
			Evals: &EvalsConfig{
				Questions: []EvalDefinition{
					{ID: "test", Question: "Does the code work?"},
				},
			},
		}

		steps := compiler.buildEvalsEngineSteps(data)
		if len(steps) == 0 {
			t.Fatal("expected non-empty steps")
		}
		allSteps := strings.Join(steps, "")

		// The staging step copies the Copilot CLI to a daemon-visible path.
		if !strings.Contains(allSteps, "Copy Copilot CLI to daemon-visible path") {
			t.Errorf("expected 'Copy Copilot CLI to daemon-visible path' step in evals job for arc-dind;\ngot:\n%s", allSteps)
		}

		// The copilot_harness.cjs invocation must use the daemon-visible path specifically.
		// Note: constants.GhAwRootDirShell+"/bin/copilot" also appears in the staging step's
		// copy command ("cp /usr/local/bin/copilot ..."), so checking the harness line
		// directly avoids a false positive from the staging step.
		harnessArcDindPath := `copilot_harness.cjs" "` + constants.GhAwRootDirShell + `/bin/copilot"`
		if !strings.Contains(allSteps, harnessArcDindPath) {
			t.Errorf("expected copilot_harness.cjs to be invoked with daemon-visible path %q for arc-dind;\ngot:\n%s", harnessArcDindPath, allSteps)
		}
		if strings.Contains(allSteps, `copilot_harness.cjs" `+constants.CopilotBinaryPath) {
			t.Errorf("copilot_harness.cjs must NOT be invoked with %q for arc-dind (ENOENT inside chroot);\ngot:\n%s", constants.CopilotBinaryPath, allSteps)
		}
		mount := `--mount "${RUNNER_TEMP}/gh-aw:${RUNNER_TEMP}/gh-aw:ro"`
		if !strings.Contains(allSteps, mount) {
			t.Errorf("expected ARC/DinD execution to mount the staged Copilot CLI directory;\ngot:\n%s", allSteps)
		}
	})

	t.Run("non-arc-dind: resolves activated Copilot CLI binary", func(t *testing.T) {
		data := &WorkflowData{
			AI: "copilot",
			// RunnerConfig is nil → default topology
			Evals: &EvalsConfig{
				Questions: []EvalDefinition{
					{ID: "test", Question: "Does the code work?"},
				},
			},
		}

		steps := compiler.buildEvalsEngineSteps(data)
		if len(steps) == 0 {
			t.Fatal("expected non-empty steps")
		}
		allSteps := strings.Join(steps, "")

		// No daemon-visible staging step for standard runners.
		if strings.Contains(allSteps, "Copy Copilot CLI to daemon-visible path") {
			t.Errorf("unexpected 'Copy Copilot CLI to daemon-visible path' step for non-arc-dind evals job;\ngot:\n%s", allSteps)
		}

		if !strings.Contains(allSteps, `GH_AW_COPILOT_SRC="$(command -v copilot 2>/dev/null || true)"`) {
			t.Errorf("expected evals execution to resolve the activated Copilot CLI binary;\ngot:\n%s", allSteps)
		}
		if !strings.Contains(allSteps, `cp "$GH_AW_COPILOT_SRC" "$GH_AW_COPILOT_BIN"`) {
			t.Errorf("expected evals execution to stage the Copilot CLI binary in its mounted directory;\ngot:\n%s", allSteps)
		}
		mountedCopilotPath := `copilot_harness.cjs" "` + constants.GhAwRootDirShell + `/bin/copilot"`
		if !strings.Contains(allSteps, mountedCopilotPath) {
			t.Errorf("expected evals harness to use mounted Copilot CLI path %q;\ngot:\n%s", mountedCopilotPath, allSteps)
		}
		if strings.Contains(allSteps, `copilot_harness.cjs" `+constants.CopilotBinaryPath) {
			t.Errorf("expected evals harness to avoid fixed path %q;\ngot:\n%s", constants.CopilotBinaryPath, allSteps)
		}
	})
}

// TestBuildEvalsJobStepsRenderSummary verifies that the evals job includes the
// "Render evals results to step summary" step and that it runs after the redact step.
func TestBuildEvalsJobStepsRenderSummary(t *testing.T) {
	compiler := NewCompiler()

	data := &WorkflowData{
		AI: "copilot",
		Evals: &EvalsConfig{
			Questions: []EvalDefinition{
				{ID: "builds", Question: "Does the code build?"},
			},
		},
	}

	steps := compiler.buildEvalsJobSteps(data)
	if len(steps) == 0 {
		t.Fatal("expected non-empty steps")
	}
	allSteps := strings.Join(steps, "")

	// The render summary step must be present.
	if !strings.Contains(allSteps, "- name: Render evals results to step summary") {
		t.Errorf("expected 'Render evals results to step summary' step in evals job;\ngot:\n%s", allSteps)
	}
	if !strings.Contains(allSteps, "id: redact_evals_results") {
		t.Errorf("expected redact step id in evals job;\ngot:\n%s", allSteps)
	}
	if !strings.Contains(allSteps, "redact_evals_results.cjs") {
		t.Errorf("expected redact_evals_results.cjs reference in evals job steps;\ngot:\n%s", allSteps)
	}

	// The render summary step must call render_evals_summary.cjs.
	if !strings.Contains(allSteps, "render_evals_summary.cjs") {
		t.Errorf("expected render_evals_summary.cjs reference in evals job steps;\ngot:\n%s", allSteps)
	}
	const successPathCondition = "if: always() && steps.redact_evals_results.outcome == 'success'"
	if strings.Count(allSteps, successPathCondition) != 2 {
		t.Errorf("expected redact outcome gating for both render and upload steps;\ngot:\n%s", allSteps)
	}

	// The render step must appear after the redact step (redact before publish to step summary).
	redactIdx := strings.Index(allSteps, "- name: Redact secrets in evals results")
	renderIdx := strings.Index(allSteps, "- name: Render evals results to step summary")
	uploadIdx := strings.Index(allSteps, "- name: Upload evals results")
	if redactIdx < 0 {
		t.Error("expected 'Redact secrets in evals results' step")
	}
	if renderIdx < 0 {
		t.Error("expected 'Render evals results to step summary' step")
	}
	if uploadIdx < 0 {
		t.Error("expected 'Upload evals results' step")
	}
	if redactIdx >= 0 && renderIdx >= 0 && renderIdx <= redactIdx {
		t.Errorf("expected render step to appear after redact step; redactIdx=%d renderIdx=%d", redactIdx, renderIdx)
	}
	if renderIdx >= 0 && uploadIdx >= 0 && renderIdx >= uploadIdx {
		t.Errorf("expected render step to appear before upload step; renderIdx=%d uploadIdx=%d", renderIdx, uploadIdx)
	}

}

func TestBuildEvalsJobStepsRedactionUsesEvalsSecretReferences(t *testing.T) {
	compiler := NewCompiler()

	data := &WorkflowData{
		AI: "copilot",
		Evals: &EvalsConfig{
			Model: "${{ secrets.EVALS_MODEL_SECRET }}",
			Questions: []EvalDefinition{
				{ID: "builds", Question: "Does it use ${{ secrets.EVALS_PROMPT_SECRET }}?"},
			},
		},
	}

	steps := strings.Join(compiler.buildRedactEvalsSecretsStep(data), "")
	if !strings.Contains(steps, "GH_AW_SECRET_NAMES") {
		t.Fatalf("expected GH_AW_SECRET_NAMES in redact step:\n%s", steps)
	}
	if !strings.Contains(steps, "SECRET_EVALS_MODEL_SECRET: ${{ secrets.EVALS_MODEL_SECRET }}") {
		t.Errorf("expected model secret env binding in redact step:\n%s", steps)
	}
	if !strings.Contains(steps, "SECRET_EVALS_PROMPT_SECRET: ${{ secrets.EVALS_PROMPT_SECRET }}") {
		t.Errorf("expected prompt secret env binding in redact step:\n%s", steps)
	}
}

func TestBuildParseEvalsResultsStepUsesResolvedExecutionModel(t *testing.T) {
	compiler := NewCompiler()

	data := &WorkflowData{
		AI:    "claude",
		Model: "claude-sonnet-4.6",
		EngineConfig: &EngineConfig{
			ID: "claude",
		},
		Evals: &EvalsConfig{
			Questions: []EvalDefinition{
				{ID: "builds", Question: "Does it build?"},
			},
		},
	}

	steps := strings.Join(compiler.buildParseEvalsResultsStep(data), "")
	if !strings.Contains(steps, `GH_AW_EVALS_MODEL: "claude-sonnet-4.6"`) {
		t.Errorf("expected parse step to record resolved execution model; got:\n%s", steps)
	}
	if strings.Contains(steps, `GH_AW_EVALS_MODEL: "small"`) {
		t.Errorf("expected parse step to avoid default 'small' when engine model is resolved; got:\n%s", steps)
	}
	if !strings.Contains(steps, "GITHUB_RUN_ID: ${{ github.run_id }}") {
		t.Errorf("expected parse step to pass github.run_id through to the eval record writer; got:\n%s", steps)
	}
}

func TestBuildParseEvalsResultsStepUsesExpressionModelAndFallbackEnv(t *testing.T) {
	compiler := NewCompiler()
	tests := []struct {
		name          string
		engineID      string
		modelEnvVar   string
		defaultEnvVar string
		defaultModel  string
	}{
		{
			name:          "copilot evals expression model fallback",
			engineID:      "copilot",
			modelEnvVar:   constants.EnvVarModelEvalsCopilot,
			defaultEnvVar: compilerenv.DefaultModelCopilot,
			defaultModel:  constants.CopilotBYOKDefaultModel,
		},
		{
			name:          "claude evals expression model fallback",
			engineID:      "claude",
			modelEnvVar:   constants.EnvVarModelEvalsClaude,
			defaultEnvVar: compilerenv.DefaultModelClaude,
			defaultModel:  constants.SonnetDefaultModel,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := &WorkflowData{
				AI:    tt.engineID,
				Model: "${{ inputs.model }}",
				EngineConfig: &EngineConfig{
					ID: tt.engineID,
				},
				Evals: &EvalsConfig{
					Questions: []EvalDefinition{
						{ID: "builds", Question: "Does it build?"},
					},
				},
			}

			steps := strings.Join(compiler.buildParseEvalsResultsStep(data), "")
			if !strings.Contains(steps, `GH_AW_EVALS_MODEL: ${{ inputs.model }}`) {
				t.Errorf("expected parse step to preserve expression-backed evals model; got:\n%s", steps)
			}
			expectedFallbackEnvLine := fmt.Sprintf(
				"%s: ${{ vars.%s || vars.%s || '%s' }}",
				constants.EnvVarModelFallback,
				tt.modelEnvVar,
				tt.defaultEnvVar,
				tt.defaultModel,
			)
			if !strings.Contains(steps, expectedFallbackEnvLine) {
				t.Errorf("expected parse step to expose evals model fallback env %q; got:\n%s", expectedFallbackEnvLine, steps)
			}
		})
	}
}

// TestBuildEvalsJobStepsCodexNoDuplicateContainerDownload verifies that a codex
// workflow with evals does not generate a duplicate "Download container images" step.
// Regression: buildEvalsJobSteps previously called buildPullAWFContainersStep
// unconditionally while buildEvalsEngineSteps → generateMCPSetup also emitted the
// step for codex, producing two identical steps and tripping duplicate-step validation.
func TestBuildEvalsJobStepsCodexNoDuplicateContainerDownload(t *testing.T) {
	compiler := NewCompiler()

	data := &WorkflowData{
		AI: "codex",
		SandboxConfig: &SandboxConfig{
			Agent: &AgentSandboxConfig{
				Type: SandboxTypeAWF,
			},
		},
		Evals: &EvalsConfig{
			Questions: []EvalDefinition{
				{ID: "task-completed", Question: "Did the agent complete the assigned task?"},
			},
		},
	}

	steps := compiler.buildEvalsJobSteps(data)
	allSteps := strings.Join(steps, "")

	count := strings.Count(allSteps, "name: Download container images")
	if count != 1 {
		t.Errorf("expected exactly one 'Download container images' step for codex evals job, got %d\n%s", count, allSteps)
	}
}

// TestBuildEvalsJobStepsNonCodexIncludesContainerDownload verifies that non-codex
// engines still get the "Download container images" step in the evals job.
// Unlike codex, non-codex engines do not call generateMCPSetup in buildEvalsEngineSteps,
// so buildPullAWFContainersStep must still be called for them.
func TestBuildEvalsJobStepsNonCodexIncludesContainerDownload(t *testing.T) {
	compiler := NewCompiler()

	for _, engine := range []string{"copilot", "claude"} {
		t.Run(engine, func(t *testing.T) {
			data := &WorkflowData{
				AI: engine,
				SandboxConfig: &SandboxConfig{
					Agent: &AgentSandboxConfig{
						Type: SandboxTypeAWF,
					},
				},
				Evals: &EvalsConfig{
					Questions: []EvalDefinition{
						{ID: "task-completed", Question: "Did the agent complete the assigned task?"},
					},
				},
			}

			steps := compiler.buildEvalsJobSteps(data)
			allSteps := strings.Join(steps, "")

			if !strings.Contains(allSteps, "Download container images") {
				t.Errorf("expected 'Download container images' step in evals job for %s engine\ngot:\n%s", engine, allSteps)
			}
		})
	}
}

// TestBuildEvalsEngineStepsCodexNoDetectionSchemaOrResultPath verifies evals
// Codex execution does not reuse detection-only structured output settings.
// Regression: when evals set IsDetectionRun=true, the generated command included
// --output-schema /tmp/gh-aw/threat-detection/detection_schema.json and
// -o /tmp/gh-aw/threat-detection/detection_result.json, which caused eval logs to
// capture detection JSON payloads instead of BinEval question answers.
func TestBuildEvalsEngineStepsCodexNoDetectionSchemaOrResultPath(t *testing.T) {
	compiler := NewCompiler()

	data := &WorkflowData{
		AI: "codex",
		SandboxConfig: &SandboxConfig{
			Agent: &AgentSandboxConfig{
				Type: SandboxTypeAWF,
			},
		},
		Evals: &EvalsConfig{
			Questions: []EvalDefinition{
				{ID: "evals_data_analyzed", Question: "Did the agent analyze evals data?"},
			},
		},
	}

	steps := strings.Join(compiler.buildEvalsEngineSteps(data), "")
	if strings.Contains(steps, "--output-schema") {
		t.Fatalf("evals Codex execution must not include detection structured-output flags; got:\n%s", steps)
	}
	if strings.Contains(steps, "/tmp/gh-aw/threat-detection/detection_schema.json") {
		t.Fatalf("evals Codex execution must not reference detection schema path; got:\n%s", steps)
	}
	if strings.Contains(steps, "/tmp/gh-aw/threat-detection/detection_result.json") {
		t.Fatalf("evals Codex execution must not reference detection result path; got:\n%s", steps)
	}
}

func TestBuildEvalsEngineStepsUsesEvalsPhase(t *testing.T) {
	compiler := NewCompiler()
	for _, engine := range []string{"copilot", "claude", "codex"} {
		t.Run(engine, func(t *testing.T) {
			data := &WorkflowData{
				AI: engine,
				SandboxConfig: &SandboxConfig{
					Agent: &AgentSandboxConfig{
						Type: SandboxTypeAWF,
					},
				},
				Evals: &EvalsConfig{
					Questions: []EvalDefinition{
						{ID: "evals_data_analyzed", Question: "Did the agent analyze evals data?"},
					},
				},
			}

			steps := strings.Join(compiler.buildEvalsEngineSteps(data), "")
			if !strings.Contains(steps, "GH_AW_PHASE: evals") {
				t.Fatalf("expected evals engine steps to set GH_AW_PHASE=evals; got:\n%s", steps)
			}
		})
	}
}

func TestBuildEvalsEngineStepsOmitsMainAgent(t *testing.T) {
	compiler := NewCompiler()
	data := &WorkflowData{
		AI: "copilot",
		EngineConfig: &EngineConfig{
			ID:    "copilot",
			Agent: "my-agent",
		},
		Evals: &EvalsConfig{
			Questions: []EvalDefinition{
				{ID: "check", Question: "Did the agent complete the task?"},
			},
		},
	}

	steps := strings.Join(compiler.buildEvalsEngineSteps(data), "")
	if strings.Contains(steps, "--agent") {
		t.Fatalf("evals engine steps must not contain the main job's --agent flag; got:\n%s", steps)
	}
	if data.EngineConfig.Agent != "my-agent" {
		t.Fatalf("original EngineConfig.Agent was mutated; expected %q, got %q", "my-agent", data.EngineConfig.Agent)
	}
}

func TestBuildEvalsEngineStepsUsesEvalsMaxAICreditsDefault(t *testing.T) {
	compiler := NewCompiler()
	data := &WorkflowData{
		AI: "codex",
		SandboxConfig: &SandboxConfig{
			Agent: &AgentSandboxConfig{
				Type: SandboxTypeAWF,
			},
		},
		Evals: &EvalsConfig{
			Questions: []EvalDefinition{
				{ID: "evals_data_analyzed", Question: "Did the agent analyze evals data?"},
			},
		},
	}

	steps := strings.Join(compiler.buildEvalsEngineSteps(data), "")
	if !strings.Contains(steps, "vars.GH_AW_DEFAULT_EVALS_MAX_AI_CREDITS") {
		t.Fatalf("expected evals engine steps to use GH_AW_DEFAULT_EVALS_MAX_AI_CREDITS; got:\n%s", steps)
	}
	if strings.Contains(steps, "vars.GH_AW_DEFAULT_DETECTION_MAX_AI_CREDITS") {
		t.Fatalf("evals engine steps must not use detection max-ai-credits default; got:\n%s", steps)
	}
}

// TestBuildEvalsEngineStepsModelMappingsPropagated verifies that model alias mappings
// from the parent WorkflowData are propagated to the evals engine steps so that the
// AWF config JSON includes the apiProxy.models section.
// Regression: before the fix, ModelMappings was not propagated to evalsData, so alias
// model names (e.g. "small") could not be resolved by the AWF — causing the error
// "model 'small' is unsupported or unrecognized by this AWF version".
func TestBuildEvalsEngineStepsModelMappingsPropagated(t *testing.T) {
	compiler := NewCompiler()

	t.Run("model mappings included in evals AWF config when set on parent WorkflowData", func(t *testing.T) {
		data := &WorkflowData{
			AI:            "copilot",
			ModelMappings: MergeImportedModelAliases(nil, nil), // builtin aliases
			Evals: &EvalsConfig{
				Questions: []EvalDefinition{
					{ID: "check", Question: "Did the agent complete the task?"},
				},
			},
		}

		steps := strings.Join(compiler.buildEvalsEngineSteps(data), "")

		// The AWF config JSON written in the run step must contain the "models" key
		// (shell-escaped as \"models\" inside the printf string) so the AWF can resolve
		// alias model names (e.g. "small") to concrete IDs.
		if !strings.Contains(steps, `\"models\"`) {
			t.Errorf("expected evals engine steps to include AWF config with \"models\" key when ModelMappings is set;\ngot:\n%s", steps)
		}

	})

	t.Run("model mappings absent from evals AWF config when nil on parent WorkflowData", func(t *testing.T) {
		data := &WorkflowData{
			AI:            "copilot",
			ModelMappings: nil,
			Evals: &EvalsConfig{
				Questions: []EvalDefinition{
					{ID: "check", Question: "Did the agent complete the task?"},
				},
			},
		}

		steps := strings.Join(compiler.buildEvalsEngineSteps(data), "")

		// Without ModelMappings, the models section should not appear in the AWF config.
		if strings.Contains(steps, `\"models\"`) {
			t.Errorf("expected evals engine steps to exclude \"models\" key from AWF config when ModelMappings is nil;\ngot:\n%s", steps)
		}
	})
}

func TestBuildEvalsEngineStepsModelCostsProvidersPropagated(t *testing.T) {
	compiler := NewCompiler()

	data := &WorkflowData{
		AI:    "claude",
		Model: "accounts/fireworks/models/minimax-m3",
		EngineConfig: &EngineConfig{
			ID: "claude",
		},
		ModelCosts: map[string]any{
			"providers": map[string]any{
				"anthropic": map[string]any{
					"models": map[string]any{
						"accounts/fireworks/models/minimax-m3": map[string]any{
							"cost": map[string]any{
								"input":  "3e-07",
								"output": "1.5e-06",
							},
						},
					},
				},
			},
		},
		Evals: &EvalsConfig{
			Questions: []EvalDefinition{
				{ID: "check", Question: "Did the agent complete the task?"},
			},
		},
		NetworkPermissions: &NetworkPermissions{
			Firewall: &FirewallConfig{Enabled: true, Version: string(constants.AWFAPIProxyProvidersMinVersion)},
		},
		SandboxConfig: &SandboxConfig{
			Agent: &AgentSandboxConfig{
				Version: string(constants.AWFAPIProxyProvidersMinVersion),
			},
		},
	}

	steps := strings.Join(compiler.buildEvalsEngineSteps(data), "")
	if !strings.Contains(steps, "providers") {
		t.Errorf("expected evals awf-config.json to contain apiProxy.providers; got:\n%s", steps)
	}
	if !strings.Contains(steps, "accounts/fireworks/models/minimax-m3") {
		t.Errorf("expected evals awf-config.json to contain custom model pricing key; got:\n%s", steps)
	}
}

// compileClaudeEvalsWorkflow compiles a Claude workflow shaped like
// claude-code-user-docs-review / copilot-session-insights with the given engine
// block and evals, and returns the agent and evals job sections.
func compileClaudeEvalsWorkflow(t *testing.T, engineYAML string) (string, string) {
	t.Helper()
	workflowPath := filepath.Join(t.TempDir(), "claude-evals.md")
	content := `---
on:
  workflow_dispatch:
permissions:
  contents: read
  issues: read
` + engineYAML + `strict: true
network:
  allowed:
    - defaults
    - github
safe-outputs:
  create-discussion:
evals:
  - id: docs_reviewed
    question: Did the agent review the project documentation from the perspective of a Claude Code user?
  - id: feedback_produced
    question: Was feedback or a report produced identifying documentation gaps for non-Copilot users?
---
Review the documentation.
`
	if err := os.WriteFile(workflowPath, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	if err := NewCompiler().CompileWorkflow(workflowPath); err != nil {
		t.Fatal(err)
	}
	result, err := os.ReadFile(strings.TrimSuffix(workflowPath, ".md") + ".lock.yml")
	if err != nil {
		t.Fatal(err)
	}
	compiled := string(result)
	agent := extractJobSection(compiled, "agent")
	evals := extractJobSection(compiled, "evals")
	if agent == "" || evals == "" {
		t.Fatal("expected both agent and evals jobs in the compiled workflow")
	}
	return agent, evals
}

func TestEvalsClaudeOnCopilotModelRoutingUsesAPIProxyGateway(t *testing.T) {
	agent, evals := compileClaudeEvalsWorkflow(t, `engine:
  id: claude
  model-provider: github
  model-routing:
    goal: cost
    mode: economy
`)
	const want = "ANTHROPIC_BASE_URL: http://api-proxy:10002\n"
	if got := strings.Count(evals, want); got != 1 {
		t.Fatalf("expected evals job to set %q exactly once, got %d", strings.TrimSpace(want), got)
	}
	if strings.Contains(evals, "host.docker.internal:10002") {
		t.Fatal("evals job must not use the unreachable host gateway URL")
	}
	if got := strings.Count(agent, want); got != 1 {
		t.Fatalf("expected agent job to keep %q exactly once, got %d", strings.TrimSpace(want), got)
	}
	if strings.Contains(agent, "host.docker.internal:10002") {
		t.Fatal("agent job must not use the host gateway URL")
	}
}

func TestEvalsClaudeOnCopilotWithoutModelRoutingUsesAPIProxyGateway(t *testing.T) {
	_, evals := compileClaudeEvalsWorkflow(t, `engine:
  id: claude
  model-provider: github
`)
	if got := strings.Count(evals, "ANTHROPIC_BASE_URL: http://api-proxy:10002\n"); got != 1 {
		t.Fatalf("expected evals job to set ANTHROPIC_BASE_URL to the api-proxy gateway exactly once, got %d", got)
	}
	if strings.Contains(evals, "host.docker.internal:10002") {
		t.Fatal("evals job must not use the unreachable host gateway URL")
	}
}

func TestEvalsClaudeAnthropicProviderHasNoCompilerBaseURL(t *testing.T) {
	_, evals := compileClaudeEvalsWorkflow(t, `engine:
  id: claude
`)
	if strings.Contains(evals, "ANTHROPIC_BASE_URL:") {
		t.Fatal("Anthropic-provider evals job must not set a compiler ANTHROPIC_BASE_URL")
	}
}

// TestEvalsAndDetectionEngineStepNamesFollowAuditContract verifies that the compiler
// enforces the engine execution step naming contract that gh aw audit uses to find
// the evals and detection engine steps (the jobs API does not expose step ids):
// a custom engine step name that already follows it is kept, one that does not
// is prefixed, and an omitted one falls back to the engine display name.
func TestEvalsAndDetectionEngineStepNamesFollowAuditContract(t *testing.T) {
	for _, tc := range []struct{ name, displayName, stepName, want string }{
		{name: "contract name", stepName: "Execute DeepSeek Harness", want: "Execute DeepSeek Harness"},
		{name: "other name", stepName: "Run DeepSeek Harness", want: "Execute Run DeepSeek Harness"},
		{name: "omitted name", want: "Execute Harness Engine"},
		{name: "omitted name with YAML-special display name", displayName: "Harness: Local #1 it's", want: "Execute Harness: Local #1 it's"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			sharedDir := filepath.Join(tmpDir, "shared")
			if err := os.MkdirAll(sharedDir, 0o755); err != nil {
				t.Fatal(err)
			}
			stepNameLine := ""
			if tc.stepName != "" {
				stepNameLine = "\n      step-name: " + tc.stepName
			}
			displayName := "Harness Engine"
			if tc.displayName != "" {
				displayName = strconv.Quote(tc.displayName)
			}
			shared := `---
engine:
  id: harness-engine
  display-name: ` + displayName + `
  behaviors:
    secret-strategy: universal-llm-consumer
    execution:
      command-name: harness-engine` + stepNameLine + `
---

# Shared Engine Definition
`
			if err := os.WriteFile(filepath.Join(sharedDir, "engine.md"), []byte(shared), 0o600); err != nil {
				t.Fatal(err)
			}
			workflowPath := filepath.Join(tmpDir, "test.md")
			content := `---
on: push
engine:
  id: harness-engine
  model: copilot/gpt-5
imports:
  - shared/engine.md
safe-outputs:
  create-issue:
evals:
  - id: completed
    question: Did the agent complete the task?
---

# Test Workflow
`
			if err := os.WriteFile(workflowPath, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := NewCompiler().CompileWorkflow(workflowPath); err != nil {
				t.Fatal(err)
			}
			result, err := os.ReadFile(strings.TrimSuffix(workflowPath, ".md") + ".lock.yml")
			if err != nil {
				t.Fatal(err)
			}
			var lock struct {
				Jobs map[string]struct {
					Steps []struct {
						Name string `yaml:"name"`
						ID   string `yaml:"id"`
					} `yaml:"steps"`
				} `yaml:"jobs"`
			}
			if err := yaml.Unmarshal(result, &lock); err != nil {
				t.Fatal(err)
			}
			engineSteps := map[string]string{
				string(constants.EvalsJobName):     "evals_agentic_execution",
				string(constants.DetectionJobName): "detection_agentic_execution",
			}
			for jobName, engineStepID := range engineSteps {
				job, ok := lock.Jobs[jobName]
				if !ok {
					t.Fatalf("expected %s job in the compiled workflow", jobName)
				}
				found := false
				for _, step := range job.Steps {
					if step.ID == engineStepID {
						found = true
						if !constants.IsEngineExecutionStepName(step.Name) {
							t.Errorf("%s engine step %q must satisfy constants.IsEngineExecutionStepName", jobName, step.Name)
						}
						if jobName == string(constants.EvalsJobName) && step.Name != tc.want {
							t.Errorf("evals engine step name = %q, want %q", step.Name, tc.want)
						}
						continue
					}
					if constants.IsEngineExecutionStepName(step.Name) {
						t.Errorf("%s step %q (id %q) is not the engine step but satisfies constants.IsEngineExecutionStepName", jobName, step.Name, step.ID)
					}
				}
				if !found {
					t.Errorf("expected %s job to contain step id %s", jobName, engineStepID)
				}
			}
		})
	}
}

// TestEnforceEngineExecutionStepNameQuotesFallback verifies that the fallback name for
// a blank engine step name is emitted as a quoted YAML scalar, so display names
// containing ": ", "#" or quotes stay valid YAML whether or not the blank name was quoted.
func TestEnforceEngineExecutionStepNameQuotesFallback(t *testing.T) {
	engine := NewClaudeEngine()
	engine.displayName = `Harness: Local #1 "it's"`
	for _, nameLine := range []string{`      - name: `, `      - name: ""`, `      - name: ''`} {
		t.Run(nameLine, func(t *testing.T) {
			step := enforceEngineExecutionStepName(GitHubActionStep{nameLine, "        id: agentic_execution"}, engine)
			var parsed []struct {
				Name string `yaml:"name"`
				ID   string `yaml:"id"`
			}
			if err := yaml.Unmarshal([]byte(strings.Join(step, "\n")), &parsed); err != nil {
				t.Fatalf("rewritten step is not valid YAML: %v\n%s", err, strings.Join(step, "\n"))
			}
			if len(parsed) != 1 || parsed[0].Name != `Execute Harness: Local #1 "it's"` || parsed[0].ID != "agentic_execution" {
				t.Errorf("parsed step = %+v, want name %q", parsed, `Execute Harness: Local #1 "it's"`)
			}
		})
	}
}
