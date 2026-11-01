//go:build !integration

package workflow

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEngineCooldownParsingAndValidation(t *testing.T) {
	for _, inline := range []bool{false, true} {
		for _, test := range []struct {
			name  string
			value any
			valid bool
			days  int
		}{
			{"zero", 0, true, 0}, {"positive", 7, true, 7},
			{"yaml-integer", uint64(5), true, 5}, {"json-integer", float64(3), true, 3},
			{"negative", -1, false, 0}, {"boolean", false, false, 0},
			{"string", "3", false, 0}, {"fraction", 1.5, false, 0},
			{"null", nil, false, 0}, {"array", []any{3}, false, 0},
			{"object", map[string]any{}, false, 0}, {"overflow", uint64(math.MaxUint64), false, 0},
		} {
			t.Run(fmt.Sprintf("%t/%s", inline, test.name), func(t *testing.T) {
				obj := map[string]any{"id": "codex", "cooldown": test.value}
				if inline {
					delete(obj, "id")
					obj["runtime"] = map[string]any{"id": "codex"}
				}
				_, config, _ := NewCompiler().ExtractEngineConfig(map[string]any{"engine": obj})
				err := validateEngineCooldown(config, false)
				if !test.valid {
					require.Error(t, err)
					require.Error(t, validateEngineCooldown(config, true))
					return
				}
				require.NoError(t, err)
				require.NotNil(t, config.Cooldown)
				assert.Equal(t, test.days, *config.Cooldown)
				if test.days == 0 {
					require.Error(t, validateEngineCooldown(config, true))
				} else {
					require.NoError(t, validateEngineCooldown(config, true))
				}
			})
		}
	}
	for _, frontmatter := range []map[string]any{
		{}, {"engine": "codex"}, {"engine": map[string]any{"id": "codex"}},
		{"engine": map[string]any{"runtime": map[string]any{"id": "codex"}}},
	} {
		_, config, _ := NewCompiler().ExtractEngineConfig(frontmatter)
		if config != nil {
			assert.Nil(t, config.Cooldown)
		}
		assert.Equal(t, 3, resolveEngineCooldownDays(&WorkflowData{EngineConfig: config}))
	}
}

func TestEngineCooldownInstalls(t *testing.T) {
	def := &EngineDefinition{
		ID: "testcooldown", Version: "1.2.3",
		Behaviors: &EngineBehaviorDefinition{
			Installation: &EngineInstallationDefinition{
				PackageManager: "npm", PackageName: "testcooldown", BinaryName: "testcooldown",
				StepName: "Install testcooldown",
			},
		},
	}
	behavior, err := NewBehaviorDefinedEngine(def)
	require.NoError(t, err)
	engines := []CodingAgentEngine{NewClaudeEngine(), NewCodexEngine(), NewPiEngine(), NewGeminiEngine(), behavior}
	for _, name := range []string{"crush", "deepseek-harness", "factory", "opencode"} {
		content, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "shared", name+".md"))
		require.NoError(t, err)
		frontmatter, err := extractMarkdownFrontmatterYAML(content)
		require.NoError(t, err)
		var definition engineDefinitionFile
		require.NoError(t, yaml.Unmarshal(frontmatter, &definition))
		engine, err := NewBehaviorDefinedEngine(&definition.Engine)
		require.NoError(t, err)
		engines = append(engines, engine)
	}
	for _, engine := range engines {
		for _, days := range []int{0, 3, 7} {
			for _, version := range []string{"1.2.3", "${{ inputs.engine-version }}"} {
				t.Run(fmt.Sprintf("%s/%d/%s", engine.GetID(), days, version), func(t *testing.T) {
					data := &WorkflowData{
						Name: "cooldown", EngineConfig: &EngineConfig{ID: engine.GetID(), Version: version, Cooldown: &days},
						SandboxConfig: &SandboxConfig{Agent: &AgentSandboxConfig{Type: SandboxTypeAWF, Runtime: AgentRuntimeNVX}},
						Runtimes:      map[string]any{"node": map[string]any{"cooldown": false}},
					}
					if days == 3 {
						data.EngineConfig.Cooldown = nil
					}
					steps := engine.GetInstallationSteps(data)
					var installs int
					for _, step := range steps {
						content := strings.Join(step, "\n")
						if strings.Contains(content, "npm install ") {
							installs++
							assert.Contains(t, content, fmt.Sprintf("NPM_CONFIG_MIN_RELEASE_AGE: '%d'", days))
							if strings.Contains(version, "${{") {
								assert.Contains(t, content, `@"${ENGINE_VERSION}"`)
								assert.Contains(t, content, "ENGINE_VERSION: "+version)
							}
						}
					}
					assert.Equal(t, 2, installs, "host and microVM installs must share the age policy")
				})
			}
		}
	}
	for _, days := range []int{0, 7} {
		data := &WorkflowData{
			EngineConfig: &EngineConfig{ID: "copilot", Version: "${{ inputs.engine-version }}", Cooldown: &days, CopilotSDK: true},
			Runtimes:     map[string]any{"node": map[string]any{"cooldown": false}},
		}
		content := stepsToString(NewCopilotEngine().GetInstallationSteps(data))
		assert.Contains(t, content, fmt.Sprintf("--cooldown-days %d", days))
		assert.Contains(t, content, `"${ENGINE_VERSION}"`)
		assert.Contains(t, content, fmt.Sprintf("NPM_CONFIG_MIN_RELEASE_AGE: '%d'", days), "SDK npm install also follows the engine policy")
	}
}

func TestCompileEngineCooldown(t *testing.T) {
	for _, inline := range []bool{false, true} {
		for _, strict := range []bool{false, true} {
			for _, value := range []string{"0", "7", "-1", "false", `"3"`, "1.5", "null"} {
				t.Run(fmt.Sprintf("%t/%t/%s", inline, strict, value), func(t *testing.T) {
					engine := "  id: codex\n"
					if inline {
						engine = "  runtime:\n    id: codex\n"
					}
					path := filepath.Join(t.TempDir(), "cooldown.md")
					content := fmt.Sprintf("---\non: workflow_dispatch\nstrict: %t\nnetwork: defaults\nengine:\n%s  cooldown: %s\n---\nInspect the repository.\n", strict, engine, value)
					require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
					err := NewCompiler(WithSkipValidation(true)).CompileWorkflow(path)
					if value != "7" && (value != "0" || strict) {
						require.Error(t, err)
						assert.Contains(t, err.Error(), "cooldown")
						return
					}
					require.NoError(t, err)
					result, err := os.ReadFile(strings.TrimSuffix(path, ".md") + ".lock.yml")
					require.NoError(t, err)
					assert.Contains(t, string(result), "NPM_CONFIG_MIN_RELEASE_AGE: '"+value+"'")
				})
			}
		}
	}
}

func TestEngineCooldownSecondaryJobInheritance(t *testing.T) {
	data := releasePinnedWorkflowData()
	days := 7
	data.EngineConfig.Cooldown = &days
	c := NewCompiler()
	assert.Contains(t, strings.Join(c.buildDetectionEngineExecutionStep(data), ""), "--cooldown-days 7")
	assert.Contains(t, strings.Join(c.buildInstallDetectionEngineForExternalDetectorStep(data), ""), "--cooldown-days 7")
	assert.Contains(t, strings.Join(c.buildEvalsEngineSteps(data), ""), "--cooldown-days 7")
	cloned := cloneThreatDetectionEngineConfig("copilot", &EngineConfig{ID: "pi", Cooldown: &days})
	assert.Equal(t, &days, cloned.Cooldown, "age policy is not specific to an engine identity")
	zero := 0
	data.SafeOutputs.ThreatDetection.EngineConfig = &EngineConfig{ID: "copilot", Cooldown: &zero}
	assert.Contains(t, strings.Join(c.buildInstallDetectionEngineForExternalDetectorStep(data), ""), "--cooldown-days 0")
}

func TestEngineCooldownInheritedByCrossEngineDetection(t *testing.T) {
	days := 7
	data := &WorkflowData{
		AI: "pi",
		EngineConfig: &EngineConfig{
			ID:          "pi",
			LLMProvider: LLMProviderGitHub,
			Cooldown:    &days,
		},
		SafeOutputs: &SafeOutputsConfig{ThreatDetection: &ThreatDetectionConfig{}},
	}
	compiler := NewCompiler()
	detectionConfig := resolveExternalDetectorEngineConfig(data, "copilot")
	require.NotNil(t, detectionConfig.Cooldown)
	assert.Equal(t, days, *detectionConfig.Cooldown)

	for name, build := range map[string]func(*WorkflowData) []string{
		"inline":   compiler.buildDetectionEngineExecutionStep,
		"external": compiler.buildInstallDetectionEngineForExternalDetectorStep,
	} {
		t.Run(name, func(t *testing.T) {
			assert.Contains(t, strings.Join(build(data), ""), "--cooldown-days 7")
		})
	}
}

func TestWarnLegacyBehaviorInstallationCooldown(t *testing.T) {
	disabled := false
	compiler := NewCompiler()
	data := &WorkflowData{
		EngineConfig: &EngineConfig{ID: "legacy"},
		engineDefinition: &EngineDefinition{
			ID: "legacy",
			Behaviors: &EngineBehaviorDefinition{
				Installation: &EngineInstallationDefinition{Cooldown: &disabled},
			},
		},
	}

	compiler.warnLegacyBehaviorInstallationCooldown(data, "workflow.md")
	assert.Equal(t, 1, compiler.GetWarningCount())

	compiler.ResetWarningCount()
	data.engineDefinition.Behaviors.Installation.Cooldown = nil
	compiler.warnLegacyBehaviorInstallationCooldown(data, "workflow.md")
	assert.Zero(t, compiler.GetWarningCount())
}

func TestCompileEngineCooldownDetection(t *testing.T) {
	for _, strict := range []bool{false, true} {
		for _, value := range []string{"0", "7", "-1", "false", `"3"`} {
			t.Run(fmt.Sprintf("%t/%s", strict, value), func(t *testing.T) {
				path := filepath.Join(t.TempDir(), "detection.md")
				content := fmt.Sprintf(`---
on: workflow_dispatch
strict: %t
network: defaults
safe-outputs:
  create-issue:
  threat-detection:
    engine:
      id: copilot
      cooldown: %s
---
Inspect the repository.
`, strict, value)
				require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
				err := NewCompiler(WithSkipValidation(true)).CompileWorkflow(path)
				if value != "7" && (value != "0" || strict) {
					require.Error(t, err)
					assert.Contains(t, err.Error(), "cooldown")
					return
				}
				require.NoError(t, err)
				result, err := os.ReadFile(strings.TrimSuffix(path, ".md") + ".lock.yml")
				require.NoError(t, err)
				assert.Contains(t, string(result), "--cooldown-days "+value)
				assert.Contains(t, string(result), "--cooldown-days 3", "main engine retains default age")
			})
		}
	}
}

func TestEngineCooldownImportOverrides(t *testing.T) {
	for _, strict := range []bool{false, true} {
		c := NewCompiler()
		c.strictMode = strict
		for _, value := range []string{"0", "7", "-1", "false", `"3"`} {
			config, _, err := c.extractEngineConfigFromJSON(`{"id":"codex","cooldown":` + value + `}`)
			if value != "7" && value != "0" {
				require.Error(t, err)
				continue
			}
			require.NoError(t, err)
			assert.NotNil(t, config.Cooldown)
		}
	}
	importedDays, overrideDays := 7, 0
	imported := &EngineConfig{Cooldown: &importedDays}
	applyMainWorkflowEngineOverrides(imported, &EngineConfig{Cooldown: &overrideDays})
	assert.Equal(t, &overrideDays, imported.Cooldown, "explicit zero must not be lost when overriding an import")
}

func TestCompileEngineCooldownImport(t *testing.T) {
	for _, strict := range []bool{false, true} {
		t.Run(strconv.FormatBool(strict), func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, "shared.md"), []byte("---\nengine:\n  id: codex\n  cooldown: 0\n---\n"), 0o644))
			path := filepath.Join(dir, "main.md")
			content := fmt.Sprintf("---\non: workflow_dispatch\nstrict: %t\nnetwork: defaults\nimports:\n  - shared.md\n---\nInspect the repository.\n", strict)
			require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
			err := NewCompiler(WithSkipValidation(true)).CompileWorkflow(path)
			if strict {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "cooldown")
				return
			}
			require.NoError(t, err)
			result, err := os.ReadFile(strings.TrimSuffix(path, ".md") + ".lock.yml")
			require.NoError(t, err)
			assert.Contains(t, string(result), "NPM_CONFIG_MIN_RELEASE_AGE: '0'")
		})
	}
}
