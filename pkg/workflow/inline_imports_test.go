//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/parser"
	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestInlinedImports_FrontmatterHashInline_SameBodySameHash verifies determinism.
func TestInlinedImports_FrontmatterHashInline_SameBodySameHash(t *testing.T) {
	tmpDir := t.TempDir()
	content := `---
name: test
on:
  workflow_dispatch:
inlined-imports: true
engine: copilot
---

# Same body content
`
	file1 := filepath.Join(tmpDir, "a.md")
	file2 := filepath.Join(tmpDir, "b.md")
	require.NoError(t, os.WriteFile(file1, []byte(content), 0o644))
	require.NoError(t, os.WriteFile(file2, []byte(content), 0o644))

	cache := parser.NewImportCache(tmpDir)
	hash1, err := parser.ComputeFrontmatterHashFromFile(file1, cache)
	require.NoError(t, err)
	hash2, err := parser.ComputeFrontmatterHashFromFile(file2, cache)
	require.NoError(t, err)

	assert.Equal(t, hash1, hash2, "same content should produce the same hash")
}

func TestInlinedImports_PreservesInlineAgentsAndSkillsInPrompt(t *testing.T) {
	for _, mode := range []string{"inlined-imports", "inline-prompt"} {
		for _, message := range []string{"hello", "${{ inputs.message }}"} {
			t.Run(mode+"/"+message, func(t *testing.T) {
				tmpDir := t.TempDir()
				workflowPath := filepath.Join(tmpDir, "inline-agent-repro.md")
				workflowContent := `---
name: inline-agent-repro
on:
  workflow_dispatch:
    inputs:
      message:
        type: string
engine: copilot
strict: false
permissions:
  contents: read
tools:
  github: false
inlined-imports: ` + map[bool]string{true: "true", false: "false"}[mode == "inlined-imports"] + `
---
# Main Workflow

Delegate to the greeter agent.

## agent: ` + "`greeter`" + `
---
description: Greets the user
---
Say hello from greeter.
Say ` + message + `.
Use github(get_issue).
## end agent: ` + "`greeter`" + `

## skill: ` + "`greeting`" + `
---
description: Greets users
---
Use the greeting skill.
Skill says ` + message + `.
## end skill: ` + "`greeting`" + `
`
				require.NoError(t, os.WriteFile(workflowPath, []byte(workflowContent), 0600))

				compiler := NewCompiler()
				compiler.SetQuiet(true)
				compiler.inlinePrompt = mode == "inline-prompt"
				data, err := compiler.ParseWorkflowFile(workflowPath)
				require.NoError(t, err)
				assert.NotContains(t, data.MarkdownContent, "Say hello from greeter.")
				assert.NotContains(t, data.MainWorkflowMarkdown, "Say hello from greeter.")
				assert.Contains(t, data.PromptMarkdown, "Say hello from greeter.")
				warnings := compiler.GetWarningCount()
				compiler.validatePromptTools(data, workflowPath)
				assert.Equal(t, warnings, compiler.GetWarningCount(), "inline definitions must not affect main-prompt tool validation")
				require.NoError(t, compiler.CompileWorkflow(workflowPath))

				lockContent, err := os.ReadFile(filepath.Join(tmpDir, "inline-agent-repro.lock.yml"))
				require.NoError(t, err)
				var workflow struct {
					Jobs map[string]struct {
						Steps []struct {
							Name string            `yaml:"name"`
							Env  map[string]string `yaml:"env"`
						} `yaml:"steps"`
					} `yaml:"jobs"`
				}
				require.NoError(t, yaml.Unmarshal(lockContent, &workflow))
				var promptContent strings.Builder
				var creationSteps, interpolationSteps int
				for _, step := range workflow.Jobs["activation"].Steps {
					switch step.Name {
					case "Create prompt with built-in context":
						creationSteps++
						for key, value := range step.Env {
							if strings.HasPrefix(key, "GH_AW_PROMPT_CONTENT_") {
								promptContent.WriteString(value)
							}
						}
					case "Interpolate variables and render templates":
						interpolationSteps++
						if strings.Contains(message, "${{") {
							assert.Equal(t, "${{ inputs.message }}", step.Env["GH_AW_INPUTS_MESSAGE"])
						}
					}
				}
				require.Equal(t, 1, creationSteps)
				assert.Equal(t, 1, interpolationSteps, "definitions alone must trigger extraction")
				if strings.Contains(message, "${{") {
					assert.Contains(t, promptContent.String(), "Say __GH_AW_INPUTS_MESSAGE__.")
					assert.Contains(t, promptContent.String(), "Skill says __GH_AW_INPUTS_MESSAGE__.")
				}
				for _, expected := range []string{
					"## agent: `greeter`",
					"Say hello from greeter.",
					"## end agent: `greeter`",
					"## skill: `greeting`",
					"Use the greeting skill.",
					"## end skill: `greeting`",
				} {
					assert.Equal(t, 1, strings.Count(promptContent.String(), expected), "prompt must contain exactly one %q", expected)
				}
			})
		}
	}
}

func TestInlinedImports_ValidatesPreservedDefinitions(t *testing.T) {
	for _, kind := range []string{"agent", "skill"} {
		for _, expression := range []string{"${{ toJSON(github) }}", "${{ toJSON(secrets) }}"} {
			for _, source := range []string{"body", "include", "runtime-import"} {
				t.Run(kind+"/"+expression+"/"+source, func(t *testing.T) {
					tmpDir := t.TempDir()
					body := expression
					switch source {
					case "include":
						require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "definition.md"), []byte("## end "+kind+": `helper`\n"+expression), 0600))
						body = "@include definition.md"
					case "runtime-import":
						require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, ".github", "workflows"), 0700))
						require.NoError(t, os.WriteFile(filepath.Join(tmpDir, ".github", "workflows", "definition.md"), []byte(expression), 0600))
						body = "{{#runtime-import .github/workflows/definition.md}}"
					}
					workflowPath := filepath.Join(tmpDir, "workflow.md")
					workflowContent := "---\non: workflow_dispatch\nengine: copilot\ninlined-imports: true\n---\n# Main\n\n## " + kind + ": `helper`\n" + body + "\n"
					require.NoError(t, os.WriteFile(workflowPath, []byte(workflowContent), 0600))
					compiler := NewCompiler()
					compiler.SetQuiet(true)
					err := compiler.CompileWorkflow(workflowPath)
					require.Error(t, err)
					if strings.Contains(expression, "secrets") && source != "runtime-import" {
						assert.Contains(t, err.Error(), "secrets serialization")
					} else {
						assert.Contains(t, err.Error(), strings.TrimSuffix(strings.TrimPrefix(expression, "${{ "), " }}"))
					}
				})
			}
		}
	}
}

func TestInlinedImports_RuntimeImportExpressionsInDefinitions(t *testing.T) {
	for _, kind := range []string{"agent", "skill"} {
		t.Run(kind, func(t *testing.T) {
			tmpDir := t.TempDir()
			workflowDir := filepath.Join(tmpDir, ".github", "workflows")
			require.NoError(t, os.MkdirAll(workflowDir, 0700))
			imported := "Say ${{ inputs.message }}.\nUse github(get_issue)."
			require.NoError(t, os.WriteFile(filepath.Join(workflowDir, "definition.md"), []byte(imported), 0600))
			workflowPath := filepath.Join(workflowDir, "workflow.md")
			source := "---\non: workflow_dispatch\nengine: copilot\nstrict: false\ntools:\n  github: false\ninlined-imports: true\n---\n# Main\n\n## " + kind + ": `helper`\n{{#runtime-import .github/workflows/definition.md}}\n"
			require.NoError(t, os.WriteFile(workflowPath, []byte(source), 0600))
			compiler := NewCompiler()
			compiler.SetQuiet(true)
			data, err := compiler.ParseWorkflowFile(workflowPath)
			require.NoError(t, err)
			compiler.markdownPath = workflowPath
			assert.NotContains(t, compiler.collectRuntimeImportMarkdownForCompilerAnalysis(data), imported)
			assert.Contains(t, compiler.collectRuntimeImportMarkdownForPromptExpressions(data), imported)
			require.NoError(t, compiler.CompileWorkflow(workflowPath))
			lockContent, err := os.ReadFile(filepath.Join(workflowDir, "workflow.lock.yml"))
			require.NoError(t, err)
			assert.Contains(t, string(lockContent), "GH_AW_INPUTS_MESSAGE: ${{ inputs.message }}")
		})
	}
}

// TestInlinedImports_AgentFileError verifies that when inlined-imports: true and a custom agent
// file is imported, ParseWorkflowFile returns a compilation error.
// Agent files require runtime access and will not be resolved without sources.
func TestInlinedImports_AgentFileError(t *testing.T) {
	tmpDir := t.TempDir()

	// Create the .github/agents directory and agent file
	agentsDir := filepath.Join(tmpDir, ".github", "agents")
	require.NoError(t, os.MkdirAll(agentsDir, 0o755))
	agentFile := filepath.Join(agentsDir, "my-agent.md")
	require.NoError(t, os.WriteFile(agentFile, []byte("# Agent\nDo things.\n"), 0o644))

	// Create the workflow file with inlined-imports: true importing the agent file
	workflowDir := filepath.Join(tmpDir, ".github", "workflows")
	require.NoError(t, os.MkdirAll(workflowDir, 0o755))
	workflowFile := filepath.Join(workflowDir, "test-workflow.md")
	workflowContent := `---
name: inlined-agent-test
on:
  workflow_dispatch:
permissions:
  contents: read
engine: copilot
inlined-imports: true
imports:
  - ../../.github/agents/my-agent.md
---

# Main Workflow

Do something.
`
	require.NoError(t, os.WriteFile(workflowFile, []byte(workflowContent), 0o644))

	compiler := NewCompiler(
		WithNoEmit(true),
		WithSkipValidation(true),
	)

	_, err := compiler.ParseWorkflowFile(workflowFile)
	require.Error(t, err, "should return an error when inlined-imports is used with an agent file")
	require.ErrorContains(t, err, "inlined-imports cannot be used with agent file imports",
		"error message should explain the conflict")
	require.ErrorContains(t, err, "my-agent.md",
		"error message should include the agent file path")
}

// TestInlinedImports_AgentFileCleared verifies that buildInitialWorkflowData clears the AgentFile
// field when inlined-imports is true. Note: ParseWorkflowFile will error before this state is used.
func TestInlinedImports_AgentFileCleared(t *testing.T) {
	compiler := NewCompiler()

	frontmatterResult := &parser.FrontmatterResult{
		Frontmatter: map[string]any{
			"name":            "agent-test",
			"engine":          "copilot",
			"inlined-imports": true,
		},
		FrontmatterLines: []string{
			"name: agent-test",
			"engine: copilot",
			"inlined-imports: true",
		},
	}

	toolsResult := &toolsProcessingResult{
		workflowName:         "agent-test",
		frontmatterName:      "agent-test",
		parsedFrontmatter:    &FrontmatterConfig{Name: "agent-test", Engine: "copilot", InlinedImports: true},
		tools:                map[string]any{},
		importPaths:          []string{".github/agents/my-agent.md"},
		mainWorkflowMarkdown: "# Main",
	}

	engineSetup := &engineSetupResult{
		engineSetting: "copilot",
		engineConfig:  &EngineConfig{ID: "copilot"},
		sandboxConfig: &SandboxConfig{},
	}

	importsResult := &parser.ImportsResult{
		AgentFile:       ".github/agents/my-agent.md",
		AgentImportSpec: ".github/agents/my-agent.md",
	}

	wd, err := compiler.buildInitialWorkflowData(frontmatterResult, toolsResult, engineSetup, importsResult)
	require.NoError(t, err)

	// InlinedImports should be true in WorkflowData
	assert.True(t, wd.InlinedImports, "InlinedImports should be true in WorkflowData")

	// AgentFile should be cleared (content inlined via ImportPaths instead)
	assert.Empty(t, wd.AgentFile, "AgentFile should be cleared when inlined-imports is true")
	assert.Empty(t, wd.AgentImportSpec, "AgentImportSpec should be cleared when inlined-imports is true")
}
