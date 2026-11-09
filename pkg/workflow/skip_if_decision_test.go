//go:build !integration

package workflow

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/require"
)

func TestParseSkipIfDecision(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  map[string]any
		fail bool
	}{
		{"absent", map[string]any{}, false},
		{"empty", map[string]any{"skip-if-decision": []any{}}, false},
		{"questions", map[string]any{"skip-if-decision": []any{"Is @context/task.md done?", "Is further execution unnecessary?"}}, false},
		{"null", map[string]any{"skip-if-decision": nil}, true},
		{"string", map[string]any{"skip-if-decision": "Done?"}, true},
		{"non-string", map[string]any{"skip-if-decision": []any{true}}, true},
		{"blank", map[string]any{"skip-if-decision": []any{" \n"}}, true},
		{"expression", map[string]any{"skip-if-decision": []any{"Is ${{ secrets.TOKEN }} complete?"}}, true},
		{"too-many", map[string]any{"skip-if-decision": make([]any, 65)}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseSkipIfDecision(tc.raw)
			if (err != nil) != tc.fail {
				t.Fatalf("parseSkipIfDecision error = %v, want failure %v", err, tc.fail)
			}
		})
	}
}

func TestSkipIfDecisionQuestionLimit(t *testing.T) {
	for _, count := range []int{64, 65} {
		questions := make([]any, count)
		for i := range questions {
			questions[i] = "Is the task complete?"
		}
		parsed, parseErr := parseSkipIfDecision(map[string]any{"skip-if-decision": questions})
		encoded, err := json.Marshal(questions)
		require.NoError(t, err)
		filename := filepath.Join(testutil.TempDir(t, "skip-decision-limit"), "decision.md")
		source := "---\non: workflow_dispatch\nengine: copilot\nskip-if-decision: " + string(encoded) + "\n---\nDo the task.\n"
		require.NoError(t, os.WriteFile(filename, []byte(source), 0600))
		compileErr := NewCompiler().CompileWorkflow(filename)
		if count == 64 {
			require.NoError(t, parseErr)
			require.Len(t, parsed, 64)
			require.NoError(t, compileErr)
		} else {
			require.ErrorContains(t, parseErr, "at most 64 questions")
			require.Error(t, compileErr)
			_, lockErr := os.Stat(stringutil.MarkdownToLockFile(filename))
			require.True(t, os.IsNotExist(lockErr))
		}
	}
}

func TestSkipIfDecisionEngineEnv(t *testing.T) {
	for _, phase := range []string{"agent", "detection", "evals"} {
		t.Run(phase, func(t *testing.T) {
			data := &WorkflowData{
				SkipIfDecision: []string{"Is @reports/progress.md complete?"},
				IsDetectionRun: phase == "detection",
				IsEvalsRun:     phase == "evals",
			}
			env := map[string]string{}
			applyEngineCwdEnv(env, data)
			applyEngineAndAgentEnv(env, data, nil)
			if phase != "agent" {
				if _, exists := env["GH_AW_SKIP_IF_DECISION"]; exists {
					t.Fatal("decision gate must not suppress detection or evals")
				}
				return
			}
			var questions []string
			if err := json.Unmarshal([]byte(env["GH_AW_SKIP_IF_DECISION"]), &questions); err != nil || len(questions) != 1 {
				t.Fatalf("invalid sandbox question env: %q, error %v", env["GH_AW_SKIP_IF_DECISION"], err)
			}
			if env["AWF_REFLECT_ENABLED"] != "1" {
				t.Fatal("all configured engines must discover Decisions through AWF")
			}
		})
	}
}

func TestSkipIfDecisionCompilation(t *testing.T) {
	for _, engine := range []string{"copilot", "claude", "codex", "gemini", "pi", "agy", "declarative", "copilot-custom", "claude-custom", "codex-custom"} {
		t.Run(engine, func(t *testing.T) {
			dir := testutil.TempDir(t, "skip-decision-compile")
			filename := filepath.Join(dir, "decision.md")
			selection := "engine: " + engine + "\n"
			if id, custom := strings.CutSuffix(engine, "-custom"); custom {
				selection = "engine:\n  id: " + id + "\n  harness: custom_harness.cjs\n"
			}
			if engine == "declarative" {
				definition := "---\nengine:\n  id: declarative\n  display-name: Declarative\n  behaviors:\n    execution:\n      command-name: declarative-cli\n      step-name: Execute Declarative\n---\nShared engine.\n"
				require.NoError(t, os.WriteFile(filepath.Join(dir, "engine.md"), []byte(definition), 0600))
				selection = "engine:\n  id: declarative\nimports: [engine.md]\n"
			}
			source := "---\non: workflow_dispatch\n" + selection + "skip-if-decision:\n  - Is there no actionable work in @context/task.md?\n---\nPerform remaining work.\n"
			if err := os.WriteFile(filename, []byte(source), 0600); err != nil {
				t.Fatal(err)
			}
			compiler := NewCompiler()
			if err := compiler.CompileWorkflow(filename); err != nil {
				t.Fatal(err)
			}
			lock, err := os.ReadFile(stringutil.MarkdownToLockFile(filename))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(lock), "GH_AW_SKIP_IF_DECISION:") || !strings.Contains(string(lock), "@context/task.md") || !strings.Contains(string(lock), "AWF_REFLECT_ENABLED:") {
				t.Fatal("compiled workflow must pass unresolved questions to the AWF agent harness")
			}
			if engine == "declarative" || strings.HasSuffix(engine, "-custom") {
				require.Contains(t, string(lock), "shell_harness.cjs")
			}
		})
	}
}

func TestSkipIfDecisionCompileValidation(t *testing.T) {
	for _, tc := range []struct {
		name, config string
		fail         bool
	}{
		{"absent", "", false},
		{"empty", "skip-if-decision: []\n", false},
		{"null", "skip-if-decision: null\n", true},
		{"object", "skip-if-decision:\n  question: Done?\n", true},
		{"non-string", "skip-if-decision: [42]\n", true},
		{"whitespace", "skip-if-decision: ['  ']\n", true},
		{"disabled-sandbox", "sandbox:\n  agent: false\nskip-if-decision: ['Done?']\n", true},
		{"expression", "skip-if-decision: ['Is ${{ secrets.TOKEN }} complete?']\n", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			filename := filepath.Join(testutil.TempDir(t, "skip-decision-validation"), "decision.md")
			if err := os.WriteFile(filename, []byte("---\non: workflow_dispatch\nengine: copilot\n"+tc.config+"---\nDo the task.\n"), 0600); err != nil {
				t.Fatal(err)
			}
			err := NewCompiler().CompileWorkflow(filename)
			if (err != nil) != tc.fail {
				t.Fatalf("compile error = %v, want failure %v", err, tc.fail)
			}
			if !tc.fail {
				lock, err := os.ReadFile(stringutil.MarkdownToLockFile(filename))
				if err != nil {
					t.Fatal(err)
				}
				if strings.Contains(string(lock), "GH_AW_SKIP_IF_DECISION:") {
					t.Fatal("absent or empty questions must preserve existing compiled behavior")
				}
			}
		})
	}
}

func TestSkipIfDecisionProtectedCaptureCompilation(t *testing.T) {
	for _, harness := range []bool{false, true} {
		for _, durable := range []bool{false, true} {
			for _, questions := range []string{"absent", "empty", "enabled"} {
				t.Run(questions+"/"+map[bool]string{false: "command", true: "harness"}[harness]+"/"+map[bool]string{false: "fresh", true: "durable"}[durable], func(t *testing.T) {
					dir := testutil.TempDir(t, "skip-decision-protected")
					harnessSource := ""
					if harness {
						harnessSource = "    harness-script: |\n      require('node:process').execve('/usr/bin/recorder', ['/usr/bin/recorder'], process.env);\n"
					}
					durableSource := ""
					engineSelection := "engine:\n  id: protected-test\n"
					if durable {
						durableSource = "    durable:\n      files: ['$GH_AW_DURABLE_CHECKPOINT']\n      execution:\n        command-name: recorder\n        protected-session-capture: true\n"
						if harness {
							durableSource += "      harness-script: |\n        require('node:process').execve('/usr/bin/recorder', ['/usr/bin/recorder'], process.env);\n"
						}
						engineSelection += "  durable: {}\n"
					}
					definition := "---\nengine:\n  id: protected-test\n  display-name: Protected test\n  behaviors:\n" + harnessSource +
						"    log-parser: |\n      function parseLog() { return { markdown: '', logEntries: [] }; }\n" +
						"    execution:\n      command-name: recorder\n      protected-session-capture: true\n" + durableSource + "---\nProtected engine.\n"
					require.NoError(t, os.WriteFile(filepath.Join(dir, "engine.md"), []byte(definition), 0600))
					config := ""
					switch questions {
					case "empty":
						config = "skip-if-decision: []\n"
					case "enabled":
						config = "skip-if-decision: ['Done?']\n"
					}
					filename := filepath.Join(dir, "decision.md")
					require.NoError(t, os.WriteFile(filename, []byte("---\non: workflow_dispatch\n"+engineSelection+"imports: [engine.md]\n"+config+"---\nDo the task.\n"), 0600))
					err := NewCompiler().CompileWorkflow(filename)
					if questions == "enabled" {
						require.ErrorContains(t, err, "skip-if-decision is incompatible with protected-session-capture")
						require.ErrorContains(t, err, "remove skip-if-decision")
						_, lockErr := os.Stat(stringutil.MarkdownToLockFile(filename))
						require.True(t, os.IsNotExist(lockErr), "unsafe combination must not produce a lock")
						return
					}
					require.NoError(t, err)
					lock, err := os.ReadFile(stringutil.MarkdownToLockFile(filename))
					require.NoError(t, err)
					require.NotContains(t, string(lock), "shell_harness.cjs")
					require.NotContains(t, string(lock), "GH_AW_SKIP_IF_DECISION:")
					require.Contains(t, string(lock), "--capture-command-output")
					if harness {
						require.Contains(t, string(lock), `exec "$GH_AW_NODE_EXEC"`)
					} else {
						require.Contains(t, string(lock), "exec recorder")
					}
				})
			}
		}
	}
}

func TestSkipIfDecisionDeclarativeExecution(t *testing.T) {
	for _, harness := range []bool{false, true} {
		for _, durable := range []bool{false, true} {
			for _, phase := range []string{"absent", "empty", "agent", "detection", "evals"} {
				name := phase
				if harness {
					name += "/custom-harness"
				}
				if durable {
					name += "/durable"
				}
				t.Run(name, func(t *testing.T) {
					definition := newHarnessEngineDefinition()
					if !harness {
						definition.Behaviors.HarnessScript = ""
					}
					if durable {
						definition.Behaviors.Durable = &EngineDurableDefinition{
							Execution:     definition.Behaviors.Execution,
							HarnessScript: definition.Behaviors.HarnessScript,
							Files:         []string{"$GH_AW_DURABLE_CHECKPOINT"},
						}
					}
					engine, err := NewBehaviorDefinedEngine(definition)
					require.NoError(t, err)
					data := &WorkflowData{
						Name:           "declarative",
						WorkflowID:     "declarative",
						EngineConfig:   &EngineConfig{ID: definition.ID},
						IsDetectionRun: phase == "detection",
						IsEvalsRun:     phase == "evals",
						NetworkPermissions: &NetworkPermissions{
							Allowed:  []string{"defaults"},
							Firewall: &FirewallConfig{Enabled: true},
						},
					}
					if phase == "empty" {
						data.SkipIfDecision = []string{}
					} else if phase != "absent" {
						data.SkipIfDecision = []string{"Is @context/task.md complete?"}
					}
					steps := engine.GetExecutionSteps(data, "/tmp/test.log")
					if durable {
						data.EngineConfig.Durable = &DurableConfig{RetentionDays: 7}
						plan, err := engine.GetDurableExecutionPlan(data, "/tmp/test.log")
						require.NoError(t, err)
						steps = plan.ExecutionSteps
					}
					require.NotEmpty(t, steps)
					execution := strings.Join(steps[len(steps)-1], "\n")
					if phase == "agent" {
						require.Contains(t, execution, "shell_harness.cjs")
						require.Contains(t, execution, "GH_AW_SKIP_IF_DECISION:")
						require.Contains(t, execution, "AWF_REFLECT_ENABLED: 1")
						require.Contains(t, execution, "awf --config")
						installation := engine.GetInstallationSteps(data)
						require.Contains(t, strings.Join(installation[0], "\n"), "Setup Node.js")
					} else {
						require.NotContains(t, execution, "shell_harness.cjs")
						require.NotContains(t, execution, "GH_AW_SKIP_IF_DECISION:")
					}
				})
			}
		}
	}
}
