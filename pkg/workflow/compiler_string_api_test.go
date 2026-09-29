//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/workflow/compilerenv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseWorkflowString_BasicParsing(t *testing.T) {
	markdown := `---
name: hello-world
description: A simple hello world workflow
on:
  workflow_dispatch:
engine: copilot
---

# Mission

Say hello to the world!
`

	compiler := NewCompiler(
		WithNoEmit(true),
		WithSkipValidation(true),
	)

	wd, err := compiler.ParseWorkflowString(markdown, "workflow.md")
	require.NoError(t, err)
	assert.NotNil(t, wd)
	assert.Equal(t, "hello-world", wd.Name)
}

func TestParseWorkflowString_MissingFrontmatter(t *testing.T) {
	markdown := `# Just a heading

No frontmatter here.
`

	compiler := NewCompiler(
		WithNoEmit(true),
		WithSkipValidation(true),
	)

	_, err := compiler.ParseWorkflowString(markdown, "workflow.md")
	require.Error(t, err)
	require.ErrorContains(t, err, "frontmatter")
}

func TestParseWorkflowString_InvalidFrontmatterYAML(t *testing.T) {
	markdown := `---
name: [invalid yaml
on: {{{
---

# Broken
`

	compiler := NewCompiler(
		WithNoEmit(true),
		WithSkipValidation(true),
	)

	_, err := compiler.ParseWorkflowString(markdown, "workflow.md")
	require.Error(t, err)
}

func TestParseWorkflowString_SharedWorkflowDetection(t *testing.T) {
	// Shared workflows have no 'on' trigger field
	markdown := `---
name: shared-tools
description: shared component
tools:
  bash: ["echo"]
---

# Shared tools component
`

	compiler := NewCompiler(
		WithNoEmit(true),
		WithSkipValidation(true),
	)

	_, err := compiler.ParseWorkflowString(markdown, "shared/tools.md")
	require.Error(t, err)

	var sharedErr *SharedWorkflowError
	assert.ErrorAs(t, err, &sharedErr, "expected SharedWorkflowError")
}

func TestParseWorkflowString_CommentOnlyFrontmatter_IsSharedWorkflow(t *testing.T) {
	markdown := `---
# Shared workflow guidance
# comment-only frontmatter
---

# Shared tools component
`

	compiler := NewCompiler(
		WithNoEmit(true),
		WithSkipValidation(true),
	)

	_, err := compiler.ParseWorkflowString(markdown, "shared/comment-only.md")
	require.Error(t, err)

	var sharedErr *SharedWorkflowError
	assert.ErrorAs(t, err, &sharedErr, "expected SharedWorkflowError")
}

func TestParseWorkflowString_WhitespaceOnlyFrontmatter_IsMissing(t *testing.T) {
	markdown := `---
   
	
---

# Shared tools component
`

	compiler := NewCompiler(
		WithNoEmit(true),
		WithSkipValidation(true),
	)

	_, err := compiler.ParseWorkflowString(markdown, "shared/whitespace-only.md")
	require.Error(t, err)
	require.ErrorContains(t, err, "no frontmatter found")
}

func TestParseWorkflowString_RedirectOnlyDetection(t *testing.T) {
	// Redirect-only workflows have a redirect field but no 'on' trigger field.
	// ParseWorkflowString should return RedirectOnlyWorkflowError, not SharedWorkflowError.
	markdown := `---
redirect: "githubnext/agentics/workflows/repo-status.md@main"
source: githubnext/agentics/workflows/daily-repo-status.md@c7d030cd6d4607b90d9ac3ffc8b24aff4f251632
---
`

	compiler := NewCompiler(
		WithNoEmit(true),
		WithSkipValidation(true),
	)

	_, err := compiler.ParseWorkflowString(markdown, "daily-repo-status.md")
	require.Error(t, err)

	var redirectErr *RedirectOnlyWorkflowError
	require.ErrorAs(t, err, &redirectErr, "expected RedirectOnlyWorkflowError, not SharedWorkflowError")
	assert.Equal(t, "githubnext/agentics/workflows/repo-status.md@main", redirectErr.Target)

	// The error message should NOT contain a duplicate info icon
	assert.NotContains(t, err.Error(), "ℹ", "error string should not contain icon prefix (caller adds it)")
}

func TestParseWorkflowString_VirtualPathBehavior(t *testing.T) {
	markdown := `---
name: path-test
on: push
engine: copilot
---

# Path test
`

	compiler := NewCompiler(
		WithNoEmit(true),
		WithSkipValidation(true),
	)

	// The virtual path should be cleaned and used for workflow ID derivation
	wd, err := compiler.ParseWorkflowString(markdown, "some/nested/../workflow.md")
	require.NoError(t, err)
	assert.NotNil(t, wd)
}

func TestParseWorkflowString_InvalidEngineReportedBeforeSchemaErrors(t *testing.T) {
	markdown := `---
on: push
engine: copiilot
bogus-field: true
---

# Test
`

	compiler := NewCompiler(
		WithNoEmit(true),
		WithSkipValidation(true),
	)

	_, err := compiler.ParseWorkflowString(markdown, "virtual/workflow.md")
	require.Error(t, err)

	errorStr := err.Error()
	assert.Contains(t, errorStr, "invalid engine: copiilot")
	assert.Contains(t, errorStr, "Did you mean: copilot?")
	assert.NotContains(t, errorStr, "Unknown property: bogus-field")
	assert.Contains(t, errorStr, "virtual/workflow.md:3:1: error:")
}

func TestParseWorkflowString_WhitespaceOnlyEngineReportedBeforeSchemaErrors(t *testing.T) {
	markdown := `---
on: push
engine: "   "
bogus-field: true
---

# Test
`

	compiler := NewCompiler(
		WithNoEmit(true),
		WithSkipValidation(true),
	)

	_, err := compiler.ParseWorkflowString(markdown, "virtual/workflow.md")
	require.Error(t, err)

	errorStr := err.Error()
	assert.Contains(t, errorStr, "invalid engine:")
	assert.NotContains(t, errorStr, "Unknown property: bogus-field")
	assert.Contains(t, errorStr, "virtual/workflow.md:3:1: error:")
}

func TestParseWorkflowString_JobsInputsValidationReportedBeforeSchemaErrors(t *testing.T) {
	markdown := `---
name: jobs-inputs-test
on: push
engine: copilot
jobs:
  my-job:
    runs-on: ubuntu-latest
    inputs:
      greeting:
        description: Greeting
    steps:
      - run: echo hi
---

# Test
`

	compiler := NewCompiler(
		WithNoEmit(true),
		WithSkipValidation(true),
	)

	_, err := compiler.ParseWorkflowString(markdown, "virtual/workflow.md")
	require.Error(t, err)

	errorStr := err.Error()
	assert.Contains(t, errorStr, "jobs.my-job.inputs: inputs are not supported on jobs")
	assert.NotContains(t, errorStr, "Unknown property: inputs")
}

func TestCompileToYAML_BasicCompilation(t *testing.T) {
	markdown := `---
name: compile-test
on:
  workflow_dispatch:
engine: copilot
---

# Mission

Greet the user warmly.
`

	compiler := NewCompiler(
		WithNoEmit(true),
		WithSkipValidation(true),
	)

	wd, err := compiler.ParseWorkflowString(markdown, "workflow.md")
	require.NoError(t, err)

	yaml, err := compiler.CompileToYAML(wd, "workflow.md")
	require.NoError(t, err)
	assert.NotEmpty(t, yaml)

	// The generated YAML should contain standard GitHub Actions structure
	assert.Contains(t, yaml, "name:")
	assert.Contains(t, yaml, "on:")
	assert.Contains(t, yaml, "jobs:")
}

func TestCompileToYAML_RequiresSelfHostedRunners(t *testing.T) {
	t.Setenv(compilerenv.RequireSelfHostedRunners, "true")
	markdown := `---
name: compile-runner-policy-test
on:
  workflow_dispatch:
engine: copilot
runs-on: self-hosted
---

# Mission

Greet the user warmly.
`

	compiler := NewCompiler(
		WithNoEmit(true),
		WithSkipValidation(true),
	)
	workflowData, err := compiler.ParseWorkflowString(markdown, "workflow.md")
	require.NoError(t, err)

	_, err = compiler.CompileToYAML(workflowData, "workflow.md")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ubuntu-slim")
	assert.Contains(t, err.Error(), "self-hosted runner")

	markdown = strings.Replace(markdown, "runs-on: self-hosted\n", "runs-on: self-hosted\nruns-on-slim: self-hosted\n", 1)
	compiler = NewCompiler(
		WithNoEmit(true),
		WithSkipValidation(true),
	)
	workflowData, err = compiler.ParseWorkflowString(markdown, "workflow.md")
	require.NoError(t, err)

	compiledYAML, err := compiler.CompileToYAML(workflowData, "workflow.md")
	require.NoError(t, err)
	assert.Contains(t, compiledYAML, "runs-on: self-hosted")
}

func TestCompileToYAML_ManagedSelfHostedRunnerDefault(t *testing.T) {
	t.Setenv(compilerenv.RequireSelfHostedRunners, "")
	t.Setenv(compilerenv.DefaultRequireSelfHostedRunners, "true")
	markdown := `---
name: compile-runner-default-test
on:
  workflow_dispatch:
engine: copilot
runs-on: self-hosted
---

# Mission

Greet the user warmly.
`
	compiler := NewCompiler(WithNoEmit(true), WithSkipValidation(true))
	workflowData, err := compiler.ParseWorkflowString(markdown, "workflow.md")
	require.NoError(t, err)

	_, err = compiler.CompileToYAML(workflowData, "workflow.md")
	require.ErrorContains(t, err, "self-hosted runner")

	t.Setenv(compilerenv.RequireSelfHostedRunners, "false")
	_, err = compiler.CompileToYAML(workflowData, "workflow.md")
	require.NoError(t, err)
}

func TestCompileToYAML_RequiresSelfHostedRunnersFromRepoConfig(t *testing.T) {
	t.Setenv(compilerenv.RequireSelfHostedRunners, "")
	gitRoot := t.TempDir()
	writeAWJSON(t, gitRoot, `{"require_self_hosted_runners": true}`)

	markdown := `---
name: compile-runner-repo-config-test
on:
  workflow_dispatch:
engine: copilot
runs-on: self-hosted
---

# Mission

Greet the user warmly.
`

	compiler := NewCompiler(
		WithNoEmit(true),
		WithSkipValidation(true),
	)
	compiler.gitRoot = gitRoot
	workflowData, err := compiler.ParseWorkflowString(markdown, "workflow.md")
	require.NoError(t, err)

	_, err = compiler.CompileToYAML(workflowData, "workflow.md")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ubuntu-slim")
	assert.Contains(t, err.Error(), "self-hosted runner enforcement")
}

func TestCompileToYAML_AllowsMissingRepoConfig(t *testing.T) {
	t.Setenv(compilerenv.RequireSelfHostedRunners, "")
	gitRoot := t.TempDir()

	markdown := `---
name: compile-runner-missing-repo-config-test
on:
  workflow_dispatch:
engine: copilot
runs-on: self-hosted
---

# Mission

Greet the user warmly.
`

	compiler := NewCompiler(
		WithNoEmit(true),
		WithSkipValidation(true),
	)
	compiler.gitRoot = gitRoot
	workflowData, err := compiler.ParseWorkflowString(markdown, "workflow.md")
	require.NoError(t, err)

	compiledYAML, err := compiler.CompileToYAML(workflowData, "workflow.md")
	require.NoError(t, err)
	assert.Contains(t, compiledYAML, "compile-runner-missing-repo-config-test")
}

func TestCompileToYAML_RequiresValidRepoConfig(t *testing.T) {
	t.Setenv(compilerenv.RequireSelfHostedRunners, "")
	gitRoot := t.TempDir()
	writeAWJSON(t, gitRoot, `{"require_self_hosted_runners":}`)

	markdown := `---
name: compile-runner-invalid-repo-config-test
on:
  workflow_dispatch:
engine: copilot
runs-on: self-hosted
---

# Mission

Greet the user warmly.
`

	compiler := NewCompiler(
		WithNoEmit(true),
		WithSkipValidation(true),
	)
	compiler.gitRoot = gitRoot
	workflowData, err := compiler.ParseWorkflowString(markdown, "workflow.md")
	require.NoError(t, err)

	_, err = compiler.CompileToYAML(workflowData, "workflow.md")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load aw.json")
}

func TestCompileToYAML_GHESCompatPinsStringAPI(t *testing.T) {
	markdown := `---
name: ghes-string-api
on:
  workflow_dispatch:
permissions:
  contents: read
engine: copilot
strict: false
steps:
  - name: Upload test artifact
    uses: actions/upload-artifact@v7
    with:
      name: test
      path: test.txt
---

# GHES string API pins
`

	compiler := NewCompiler()
	compiler.SetGHESCompat(true)
	wd, err := compiler.ParseWorkflowString(markdown, "workflow.md")
	require.NoError(t, err)

	yaml, err := compiler.CompileToYAML(wd, "workflow.md")
	require.NoError(t, err)
	assert.Contains(t, yaml, "actions/upload-artifact@c6a366c94c3e0affe28c06c8df20a878f24da3cf # v3.2.2")
	assert.Contains(t, yaml, "actions/download-artifact@a9bc5e6ef2cb54c177f32aa5726adaa15e7e2d59 # v3.1.0")
	assert.NotContains(t, yaml, "actions/upload-artifact@043fb46d1a93c77aae656e7c1c64a875d1fc6a0a # v7")
	assert.NotContains(t, yaml, "actions/download-artifact@3e5f45b2cfb9172054b4087a40e8e0b5a5461e7c # v8")
}

func TestCompileToYAML_OutputContainsWorkflowName(t *testing.T) {
	markdown := `---
name: my-unique-workflow
on: push
engine: copilot
---

# Do something
`

	compiler := NewCompiler(
		WithNoEmit(true),
		WithSkipValidation(true),
	)

	wd, err := compiler.ParseWorkflowString(markdown, "workflow.md")
	require.NoError(t, err)

	yaml, err := compiler.CompileToYAML(wd, "workflow.md")
	require.NoError(t, err)
	assert.Contains(t, yaml, "my-unique-workflow")
}

func TestParseWorkflowString_EmptyContent(t *testing.T) {
	compiler := NewCompiler(
		WithNoEmit(true),
		WithSkipValidation(true),
	)

	_, err := compiler.ParseWorkflowString("", "workflow.md")
	require.Error(t, err)
}

func TestParseWorkflowString_FrontmatterOnly(t *testing.T) {
	markdown := `---
name: no-body
on: push
engine: copilot
---
`

	compiler := NewCompiler(
		WithNoEmit(true),
		WithSkipValidation(true),
	)

	// Should parse successfully even without markdown body
	wd, err := compiler.ParseWorkflowString(markdown, "workflow.md")
	require.NoError(t, err)
	assert.NotNil(t, wd)
}

func TestCompileToYAML_EndToEnd(t *testing.T) {
	// Full round trip: markdown string -> parse -> compile -> YAML string
	markdown := `---
name: e2e-test
description: End-to-end string API test
on:
  issues:
    types: [opened]
engine: copilot
---

# Mission

When a new issue is opened, add a welcome comment.
`

	compiler := NewCompiler(
		WithNoEmit(true),
		WithSkipValidation(true),
	)

	wd, err := compiler.ParseWorkflowString(markdown, "workflow.md")
	require.NoError(t, err)

	yaml, err := compiler.CompileToYAML(wd, "workflow.md")
	require.NoError(t, err)

	// Verify the YAML is valid-looking
	// CompileToYAML skips the ASCII art header (wasm/editor mode), so YAML starts with the name
	assert.NotContains(t, yaml, "Agentic", "compiled YAML should not contain ASCII art header")
	assert.Contains(t, yaml, "e2e-test")
	assert.Contains(t, yaml, "issues")
}

func TestCompileToYAML_PromptContentInlined(t *testing.T) {
	// Verify that the markdown prompt content is inlined in the compiled YAML
	// when using ParseWorkflowString (the Wasm/browser path).
	// This is the key regression test for the wasm live editor prompt issue.
	markdown := `---
name: hello-world
on:
  workflow_dispatch:
engine: copilot
---

# Mission

Say hello to the world!
`

	compiler := NewCompiler(
		WithNoEmit(true),
		WithSkipValidation(true),
	)

	wd, err := compiler.ParseWorkflowString(markdown, "workflow.md")
	require.NoError(t, err)

	yamlOutput, err := compiler.CompileToYAML(wd, "workflow.md")
	require.NoError(t, err)

	// The prompt content should be inlined in the YAML, not behind a runtime-import macro
	assert.Contains(t, yamlOutput, "# Mission", "compiled YAML should contain the markdown heading")
	assert.Contains(t, yamlOutput, "Say hello to the world!", "compiled YAML should contain the markdown body")
	assert.NotContains(t, yamlOutput, "{{#runtime-import", "compiled YAML from string API should not contain runtime-import macros")
}

func TestCompileToYAML_PromptContentInlinedWithExpressions(t *testing.T) {
	// Verify that GitHub expressions in the markdown prompt are properly handled
	// when inlined (expressions should be extracted and replaced with env vars)
	markdown := `---
name: expr-test
on:
  issues:
    types: [opened]
engine: copilot
---

# Mission

Handle issue ${{ github.event.issue.number }} in repo ${{ github.repository }}.
`

	compiler := NewCompiler(
		WithNoEmit(true),
		WithSkipValidation(true),
	)

	wd, err := compiler.ParseWorkflowString(markdown, "workflow.md")
	require.NoError(t, err)

	yamlOutput, err := compiler.CompileToYAML(wd, "workflow.md")
	require.NoError(t, err)

	// The prompt content should be present (with expressions replaced by env var references)
	assert.Contains(t, yamlOutput, "# Mission", "compiled YAML should contain the markdown heading")
	assert.Contains(t, yamlOutput, "Handle issue", "compiled YAML should contain the prompt text")
	assert.NotContains(t, yamlOutput, "{{#runtime-import", "compiled YAML from string API should not contain runtime-import macros")
}
