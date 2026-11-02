//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	bareInlineSkillReportingHint = "Use the `reporting` skill when producing a report, issue, or PR comment to follow the formatting guidelines below."
	bareInlineSkillAmbientName   = "ambient-helper"
	// bareInlineSkillArgsFragment expands at run time to the --skill arguments pi_runtime.cjs
	// derives from the inline skills activation extracted and staged.
	bareInlineSkillArgsFragment = "$(cat /tmp/gh-aw/pi-agent-dir/workflow-skill-args 2>/dev/null)"
)

// bareInlineSkillReportingImport mirrors .github/workflows/shared/reporting.md: the skill-use
// hint lives outside the inline skill block, so extraction leaves it in the prompt.
const bareInlineSkillReportingImport = "---\n# Report formatting guidelines exposed as an inline skill.\n---\n\n" +
	bareInlineSkillReportingHint + "\n\n" +
	"## skill: `reporting`\n" +
	"---\ndescription: Report formatting guidelines for gh-aw workflows\n---\n\n" +
	"- Use `###` (or lower) headers only.\n\n" +
	"## end skill: `reporting`\n"

// bareInlineSkillWrapperImport is a frontmatter-importable file whose body pulls in the
// reporting skill through a nested runtime import.
const bareInlineSkillWrapperImport = "---\n# Wrapper importing reporting at run time.\n---\n\n{{#runtime-import .github/workflows/shared/reporting.md}}\n"

var bareInlineSkillPiFlags = []string{"--no-context-files", "--no-skills", "--no-prompt-templates", "--no-extensions", "--no-themes"}

// setupBareInlineSkillRepo writes a repository fixture containing the reporting import and an
// unrelated ambient pi skill (negative control), then writes the workflow and returns its path.
func setupBareInlineSkillRepo(t *testing.T, workflowName, workflowContent string) string {
	t.Helper()
	root := testutil.TempDir(t, "pi-bare-inline-skills-*")
	sharedDir := filepath.Join(root, ".github", "workflows", "shared")
	require.NoError(t, os.MkdirAll(sharedDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(sharedDir, "reporting.md"), []byte(bareInlineSkillReportingImport), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(sharedDir, "wrapper.md"), []byte(bareInlineSkillWrapperImport), 0o644))
	ambientDir := filepath.Join(root, ".pi", "skills", bareInlineSkillAmbientName)
	require.NoError(t, os.MkdirAll(ambientDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(ambientDir, "SKILL.md"), []byte("---\ndescription: ambient\n---\nAmbient.\n"), 0o644))
	workflowPath := filepath.Join(root, ".github", "workflows", workflowName+".md")
	require.NoError(t, os.WriteFile(workflowPath, []byte(workflowContent), 0o644))
	return workflowPath
}

// compileBareInlineSkillWorkflow compiles the workflow and returns the lock file content
// together with the parsed workflow data.
func compileBareInlineSkillWorkflow(t *testing.T, workflowPath string) (string, *WorkflowData) {
	t.Helper()
	compiler := NewCompiler()
	require.NoError(t, compiler.CompileWorkflow(workflowPath), "workflow should compile")
	lock, err := os.ReadFile(strings.TrimSuffix(workflowPath, ".md") + ".lock.yml")
	require.NoError(t, err)
	data, err := NewCompiler().ParseWorkflowFile(workflowPath)
	require.NoError(t, err)
	return string(lock), data
}

// assertBareRuntimeSkillLoading checks that bare pi keeps every discovery-disabling flag and
// loads workflow skills only through the runtime list derived from activation output.
func assertBareRuntimeSkillLoading(t *testing.T, lock string) {
	t.Helper()
	args := compiledPiArgs(t, lock)
	assert.Contains(t, args, strings.Join(bareInlineSkillPiFlags, " "), "bare pi should keep all five discovery-disabling flags")
	assert.Contains(t, args, bareInlineSkillArgsFragment, "bare pi should load the workflow skills activation staged")
	assert.Equal(t, 1, strings.Count(lock, bareInlineSkillArgsFragment), "the runtime skill list should be expanded once")
	assert.NotContains(t, lock, "--skill", "no compile-time skill list should be emitted")
	assert.Contains(t, lock, `"--no-themes"]`, "pi sub-agents should inherit bare mode and receive the runtime skill list")
	assert.Contains(t, lock, `GH_AW_SKILL_DIR: ".pi/skills"`, "inline skills should be restored from the activation artifact")
	assert.NotContains(t, lock, bareInlineSkillAmbientName, "ambient repository skills must not be loaded")
}

var piCommandArgsPattern = regexp.MustCompile(` pi (--print [^']*?) --append-system-prompt`)

// compiledPiArgs returns the pi CLI argument string from the compiled execution command.
func compiledPiArgs(t *testing.T, lock string) string {
	t.Helper()
	match := piCommandArgsPattern.FindStringSubmatch(lock)
	require.NotNil(t, match, "compiled lock should invoke pi")
	return match[1]
}

// assertPromptContract checks that the runtime-imported source keeps the skill-use hint after
// inline-skill extraction and materializes exactly the reporting skill.
func assertPromptContract(t *testing.T, lock, runtimeImportPath, sourcePath string, authorSource bool) {
	t.Helper()
	directive := "runtime-import"
	if authorSource {
		directive = "author-prompt"
	}
	assert.Contains(t, promptRenderPlan(t, lock), "{{#"+directive+" "+runtimeImportPath+"}}", "prompt should load the selected skill source at runtime")
	raw, err := os.ReadFile(sourcePath)
	require.NoError(t, err)
	body, err := parser.ExtractMarkdownContent(string(raw))
	require.NoError(t, err)
	prompt, skills, err := parser.ExtractInlineSkills(body)
	require.NoError(t, err)
	assert.Contains(t, prompt, bareInlineSkillReportingHint, "effective prompt should keep the reporting hint")
	assert.NotContains(t, prompt, "## skill:", "inline skill body should be extracted from the prompt")
	require.Len(t, skills, 1)
	assert.Equal(t, "reporting", skills[0].Name, "extraction should materialize reporting/SKILL.md")
	assert.Equal(t, "/SKILL.md", engineInlineSkillExt("pi"))
}

func TestCompileBarePiInlineSkillsExplicitlyLoaded(t *testing.T) {
	tests := []struct {
		name              string
		workflow          string
		runtimeImportPath string
		sourceFile        string
	}{
		{
			name: "imported",
			workflow: `---
on: workflow_dispatch
permissions:
  contents: read
engine:
  id: pi
  bare: true
imports:
  - shared/reporting.md
tools:
  bash: false
  cli-proxy: false
---

# Bare pi imported skill

Say hello.
`,
			runtimeImportPath: ".github/workflows/shared/reporting.md",
			sourceFile:        "shared/reporting.md",
		},
		{
			name: "directly declared",
			workflow: `---
on: workflow_dispatch
permissions:
  contents: read
engine:
  id: pi
  bare: true
tools:
  bash: false
  cli-proxy: false
---

# Bare pi declared skill

` + bareInlineSkillReportingHint + `

## skill: ` + "`reporting`" + `
---
description: Report formatting guidelines for gh-aw workflows
---

- Use ` + "`###`" + ` (or lower) headers only.

## end skill: ` + "`reporting`" + `
`,
			runtimeImportPath: ".github/workflows/bare-pi-skill.md",
			sourceFile:        "bare-pi-skill.md",
		},
		{
			name: "body-level runtime import",
			workflow: `---
on: workflow_dispatch
permissions:
  contents: read
engine:
  id: pi
  bare: true
tools:
  bash: false
  cli-proxy: false
---

# Bare pi runtime-imported skill

{{#runtime-import .github/workflows/shared/reporting.md}}

Say hello.
`,
			runtimeImportPath: ".github/workflows/shared/reporting.md",
			sourceFile:        "shared/reporting.md",
		},
		{
			name: "runtime import nested in frontmatter import",
			workflow: `---
on: workflow_dispatch
permissions:
  contents: read
engine:
  id: pi
  bare: true
imports:
  - shared/wrapper.md
tools:
  bash: false
  cli-proxy: false
---

# Bare pi nested runtime-imported skill

Say hello.
`,
			runtimeImportPath: ".github/workflows/shared/wrapper.md",
			sourceFile:        "shared/reporting.md",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workflowPath := setupBareInlineSkillRepo(t, "bare-pi-skill", tt.workflow)
			lock, _ := compileBareInlineSkillWorkflow(t, workflowPath)
			assertBareRuntimeSkillLoading(t, lock)
			assertPromptContract(t, lock, tt.runtimeImportPath, filepath.Join(filepath.Dir(workflowPath), tt.sourceFile), tt.name == "directly declared")
		})
	}
}

func TestCompilePiRoutedClaudeWithReportingSkill(t *testing.T) {
	workflowPath := setupBareInlineSkillRepo(t, "smoke-pi-routed-claude", `---
on: workflow_dispatch
permissions:
  contents: read
  copilot-requests: write
engine:
  id: pi
  bare: true
  model-routing:
    goal: cost
    mode: robust
    allowed-models: [claude-sonnet-5]
imports:
  - shared/reporting.md
max-tool-calls: 10
max-tool-denials: 3
tools:
  bash: false
  cli-proxy: false
  edit: false
safe-outputs:
  create-issue:
sandbox:
  agent:
    id: awf
---

# Smoke Test: Pi Model Routing to Claude

Answer the question "what is 6 × 7?" in one word.
`)
	lock, _ := compileBareInlineSkillWorkflow(t, workflowPath)

	args := compiledPiArgs(t, lock)
	assert.Contains(t, args, "--exclude-tools bash", "bash must stay disabled")
	assertBareRuntimeSkillLoading(t, lock)
	assert.Contains(t, args, `--model "aw-gateway/$(cat /tmp/gh-aw/pi-routing-model)"`, "model routing should be unchanged")
	assert.Contains(t, lock, "GH_AW_MAX_TOOL_DENIALS: 3", "denial budget should be unchanged")
	assert.Contains(t, lock, "claude-sonnet-5", "routing pool should be unchanged")
	assertPromptContract(t, lock, ".github/workflows/shared/reporting.md", filepath.Join(filepath.Dir(workflowPath), "shared", "reporting.md"), false)
}

// TestCompileBarePiDriversLoadWorkflowSkills covers bare pi with the SDK and RPC drivers, which
// bypass the CLI command line: each driver loads the runtime skill list from preparePiRuntime
// (additionalSkillPaths for the SDK resource loader, --skill arguments for the RPC child).
func TestCompileBarePiDriversLoadWorkflowSkills(t *testing.T) {
	for _, driver := range []string{"pi_agent_core_driver.cjs", "pi_rpc_driver.cjs"} {
		t.Run(driver, func(t *testing.T) {
			workflowPath := setupBareInlineSkillRepo(t, "bare-pi-driver-skill", `---
on: workflow_dispatch
permissions:
  contents: read
engine:
  id: pi
  bare: true
  driver: `+driver+`
imports:
  - shared/reporting.md
tools:
  bash: false
  cli-proxy: false
---

# Bare pi driver skill

Say hello.
`)
			lock, _ := compileBareInlineSkillWorkflow(t, workflowPath)

			assert.Contains(t, lock, `/`+driver, "the configured driver should run the main session")
			assert.Contains(t, lock, "GH_AW_PI_BARE: true", "the driver should run the session in bare mode")
			assert.Contains(t, lock, `"${RUNNER_TEMP}/gh-aw/actions/pi_runtime.cjs"`, "pi_runtime.cjs should derive the runtime skill list before the driver starts")
			assert.NotContains(t, lock, bareInlineSkillArgsFragment, "drivers load the runtime skill list themselves")
			assert.NotContains(t, lock, "--skill", "no compile-time skill list should be emitted")
			assert.Contains(t, lock, `"--no-themes"]`, "pi sub-agents should inherit bare mode and receive the runtime skill list")
			assert.Contains(t, lock, `GH_AW_SKILL_DIR: ".pi/skills"`, "inline skills should be restored from the activation artifact")
			assert.NotContains(t, lock, bareInlineSkillAmbientName, "ambient repository skills must not be loaded")
			if driver == "pi_rpc_driver.cjs" {
				assert.Contains(t, lock, "GH_AW_PI_ARGS:", "the RPC driver should receive the bare arguments")
			}
			assertPromptContract(t, lock, ".github/workflows/shared/reporting.md", filepath.Join(filepath.Dir(workflowPath), "shared", "reporting.md"), false)
		})
	}
}

func TestCompileNonBarePiInlineSkillsUnchanged(t *testing.T) {
	for _, bare := range []string{"\n  bare: false", ""} {
		name := "omitted"
		if bare != "" {
			name = "false"
		}
		t.Run("bare "+name, func(t *testing.T) {
			workflowPath := setupBareInlineSkillRepo(t, "non-bare-pi-skill", `---
on: workflow_dispatch
permissions:
  contents: read
engine:
  id: pi`+bare+`
imports:
  - shared/reporting.md
tools:
  bash: false
  cli-proxy: false
---

# Non-bare pi skill

Say hello.
`)
			lock, _ := compileBareInlineSkillWorkflow(t, workflowPath)

			args := compiledPiArgs(t, lock)
			assert.Equal(t, "--print --mode json --no-session --no-approve --exclude-tools bash --model aw-gateway/auto", args,
				"non-bare pi arguments should match the pre-fix baseline")
			assert.NotContains(t, lock, "--skill", "non-bare pi should rely on normal discovery")
			assert.NotContains(t, lock, "workflow-skill-args", "non-bare pi should not load the runtime skill list")
			for _, flag := range bareInlineSkillPiFlags {
				assert.NotContains(t, lock, flag)
			}

			assertPromptContract(t, lock, ".github/workflows/shared/reporting.md", filepath.Join(filepath.Dir(workflowPath), "shared", "reporting.md"), false)
		})
	}
}

func TestCompileBarePiWithoutInlineSkillsUnchanged(t *testing.T) {
	workflowPath := setupBareInlineSkillRepo(t, "bare-pi-no-skill", `---
on: workflow_dispatch
permissions:
  contents: read
engine:
  id: pi
  bare: true
tools:
  bash: false
  cli-proxy: false
---

# Bare pi baseline

Say hello.
`)
	lock, _ := compileBareInlineSkillWorkflow(t, workflowPath)

	args := compiledPiArgs(t, lock)
	assert.Equal(t, "--print --mode json --no-session --no-approve --exclude-tools bash "+strings.Join(bareInlineSkillPiFlags, " ")+" --model aw-gateway/auto "+bareInlineSkillArgsFragment, args,
		"bare pi arguments should match the pre-fix baseline plus the runtime skill list, which is empty when activation stages no inline skills")
	assert.NotContains(t, lock, "--skill")
	assert.NotContains(t, lock, "reporting")
	assert.NotContains(t, lock, bareInlineSkillAmbientName, "ambient repository skills must not be loaded")

	raw, err := os.ReadFile(workflowPath)
	require.NoError(t, err)
	body, err := parser.ExtractMarkdownContent(string(raw))
	require.NoError(t, err)
	prompt, skills, err := parser.ExtractInlineSkills(body)
	require.NoError(t, err)
	assert.Empty(t, skills)
	assert.NotContains(t, prompt, "skill", "prompt should gain no skill-use hints")
}

func TestCompileBareInlineSkillsOtherEnginesUnchanged(t *testing.T) {
	for _, engineID := range []string{"claude", "copilot"} {
		t.Run(engineID, func(t *testing.T) {
			workflowPath := setupBareInlineSkillRepo(t, "bare-"+engineID+"-skill", `---
on: workflow_dispatch
permissions:
  contents: read
engine:
  id: `+engineID+`
  bare: true
imports:
  - shared/reporting.md
tools:
  bash: false
  cli-proxy: false
---

# Bare `+engineID+` skill

Say hello.
`)
			lock, _ := compileBareInlineSkillWorkflow(t, workflowPath)

			assert.NotContains(t, lock, "--skill", "pi skill flags must not leak into other engines")
			assert.NotContains(t, lock, "pi-agent-dir")
			assert.NotContains(t, lock, "workflow-skill-args")
			assert.NotContains(t, lock, "--no-skills")
			assert.NotContains(t, lock, bareInlineSkillAmbientName)
			assert.Contains(t, lock, `GH_AW_SKILL_DIR: "`+GetEngineSkillDir(engineID)+`"`, "existing skill restoration should be unchanged")

			assert.True(t, slices.ContainsFunc(strings.Split(lock, "\n"), func(line string) bool {
				return strings.Contains(line, "{{#runtime-import .github/workflows/shared/reporting.md}}")
			}), "prompt should still runtime-import the reporting skill source")
		})
	}
}
