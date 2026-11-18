//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/github/gh-aw/pkg/testutil"
	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================================
// End-to-end compilation tests for call-workflow safe output
//
// Each test writes one or more markdown workflow files, calls CompileWorkflow, reads the
// generated .lock.yml, and asserts on the compiled YAML content.
// =============================================================================================

// workerWith creates a minimal worker .lock.yml (or .yml) with workflow_call trigger and
// optional typed inputs. Used as a helper in compilation tests.
func workerLockYML(withInputs string) string {
	if withInputs == "" {
		return `name: Worker
on:
  workflow_call:
    inputs:
      payload:
        type: string
        required: false
jobs:
  work:
    runs-on: ubuntu-latest
    steps:
      - run: echo "Working"
`
	}
	return `name: Worker
on:
  workflow_call:
    inputs:
` + withInputs + `
jobs:
  work:
    runs-on: ubuntu-latest
    steps:
      - run: echo "Working"
`
}

func TestSafeOutputAuthorizationWorkflowLaunchCompilation(t *testing.T) {
	dir := filepath.Join(testutil.TempDir(t, "authorized-call-workflow"), ".github", "workflows")
	require.NoError(t, os.MkdirAll(dir, 0755))
	createWorker(t, dir, "worker", "")
	lock := compileAndReadLock(t, filepath.Join(dir, "gateway.md"), `---
on: workflow_dispatch
engine: copilot
permissions:
  contents: read
safe-outputs:
  authorization:
    endpoint: https://authority.example/authorize
    audience: gh-aw
  call-workflow:
    workflows: [worker]
---
Call the reviewed worker.
`)
	var workflow struct {
		Jobs map[string]struct {
			If          string
			Needs       any
			Uses        string
			Permissions map[string]string
		}
	}
	require.NoError(t, yaml.Unmarshal([]byte(lock), &workflow))
	launch := workflow.Jobs["call-worker"]
	require.Equal(t, "./.github/workflows/worker.lock.yml", launch.Uses)
	require.Contains(t, launch.Needs, "authorize-call-worker")
	require.Contains(t, launch.If, "authorized")
	require.Equal(t, "write", workflow.Jobs["authorize-call-worker"].Permissions["id-token"])
	require.NotEqual(t, "write", launch.Permissions["id-token"])
	require.Contains(t, lock, "call_workflow:launch:worker")
}

// createWorker writes a worker lock file to the .github/workflows directory.
func createWorker(t *testing.T, workflowsDir, name, inputs string) {
	t.Helper()
	content := workerLockYML(inputs)
	err := os.WriteFile(filepath.Join(workflowsDir, name+".lock.yml"), []byte(content), 0644)
	require.NoError(t, err, "Failed to write worker %s", name)
}

// compileAndReadLock writes a markdown gateway file, compiles it, and returns the lock file content.
func compileAndReadLock(t *testing.T, gatewayFile, markdown string) string {
	t.Helper()
	err := os.WriteFile(gatewayFile, []byte(markdown), 0644)
	require.NoError(t, err, "Failed to write gateway markdown")

	compiler := NewCompiler(WithVersion("1.0.0"))
	err = compiler.CompileWorkflow(gatewayFile)
	require.NoError(t, err, "Compilation should succeed")

	lockFile := stringutil.MarkdownToLockFile(gatewayFile)
	content, err := os.ReadFile(lockFile)
	require.NoError(t, err, "Failed to read lock file")
	return string(content)
}

// extractWorkflowCallSecretsFromParsedLock parses the compiled lock file YAML and returns
// the secret names declared under on.workflow_call.secrets. Used by tests to assert on the
// exact contents of the secrets section without relying on fragile string searches.
func extractWorkflowCallSecretsFromParsedLock(t *testing.T, lockContent string) []string {
	t.Helper()
	var workflow map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(lockContent), &workflow), "Should parse lock file YAML")
	return extractWorkflowCallSecretsFromParsed(workflow)
}

// TestCallWorkflowCompile_ArrayFormat tests compilation with the shorthand array format
// (call-workflow: [worker-a, worker-b])
func TestCallWorkflowCompile_ArrayFormat(t *testing.T) {
	tmpDir := testutil.TempDir(t, "call-workflow-array")
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	require.NoError(t, os.MkdirAll(workflowsDir, 0755))

	createWorker(t, workflowsDir, "worker-a", "")
	createWorker(t, workflowsDir, "worker-b", "")

	gatewayMD := `---
on: workflow_dispatch
engine: copilot
permissions:
  contents: read
safe-outputs:
  call-workflow:
    - worker-a
    - worker-b
---

# Gateway (Array Format)

Select the appropriate worker and call it.
`
	yaml := compileAndReadLock(t, filepath.Join(workflowsDir, "gateway.md"), gatewayMD)

	assert.Contains(t, yaml, "call-worker-a:", "Should contain call-worker-a job")
	assert.Contains(t, yaml, "call-worker-b:", "Should contain call-worker-b job")
	assert.Contains(t, yaml, "uses: ./.github/workflows/worker-a.lock.yml")
	assert.Contains(t, yaml, "uses: ./.github/workflows/worker-b.lock.yml")
	assert.Contains(t, yaml, "secrets: inherit")
	assert.Contains(t, yaml, "call_workflow_name == 'worker-a'")
	assert.Contains(t, yaml, "call_workflow_name == 'worker-b'")
}

// TestCallWorkflowCompile_MapFormat tests compilation with the full map format
// (call-workflow: { workflows: [...], max: 1 })
func TestCallWorkflowCompile_MapFormat(t *testing.T) {
	tmpDir := testutil.TempDir(t, "call-workflow-map")
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	require.NoError(t, os.MkdirAll(workflowsDir, 0755))

	createWorker(t, workflowsDir, "spring-boot-bugfix", "")
	createWorker(t, workflowsDir, "frontend-dep-upgrade", "")

	gatewayMD := `---
on: workflow_dispatch
engine: copilot
permissions:
  contents: read
safe-outputs:
  call-workflow:
    workflows:
      - spring-boot-bugfix
      - frontend-dep-upgrade
    max: 1
---

# Gateway (Map Format)

Dispatch to the selected worker.
`
	yaml := compileAndReadLock(t, filepath.Join(workflowsDir, "gateway.md"), gatewayMD)

	assert.Contains(t, yaml, "call-spring-boot-bugfix:", "Should contain call-spring-boot-bugfix job")
	assert.Contains(t, yaml, "call-frontend-dep-upgrade:", "Should contain call-frontend-dep-upgrade job")
	assert.Contains(t, yaml, "uses: ./.github/workflows/spring-boot-bugfix.lock.yml")
	assert.Contains(t, yaml, "uses: ./.github/workflows/frontend-dep-upgrade.lock.yml")
	assert.Contains(t, yaml, "call_workflow_name == 'spring-boot-bugfix'")
	assert.Contains(t, yaml, "call_workflow_name == 'frontend-dep-upgrade'")
}

// TestCallWorkflowCompile_SingleWorker tests compilation with a single worker in the list
func TestCallWorkflowCompile_SingleWorker(t *testing.T) {
	tmpDir := testutil.TempDir(t, "call-workflow-single")
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	require.NoError(t, os.MkdirAll(workflowsDir, 0755))

	createWorker(t, workflowsDir, "worker-a", "")

	gatewayMD := `---
on: workflow_dispatch
engine: copilot
permissions:
  contents: read
safe-outputs:
  call-workflow:
    workflows:
      - worker-a
---

# Single Worker Gateway

Call worker-a for all requests.
`
	yaml := compileAndReadLock(t, filepath.Join(workflowsDir, "gateway.md"), gatewayMD)

	assert.Contains(t, yaml, "call-worker-a:", "Should contain call-worker-a job")
	assert.Contains(t, yaml, "uses: ./.github/workflows/worker-a.lock.yml")
	assert.Contains(t, yaml, "secrets: inherit")
	assert.Contains(t, yaml, "payload: ${{ needs.safe_outputs.outputs.call_workflow_payload }}")
}

// TestCallWorkflowCompile_WorkerWithTypedInputs tests compilation where worker has
// workflow_call inputs that are transformed into typed MCP tool parameters
func TestCallWorkflowCompile_WorkerWithTypedInputs(t *testing.T) {
	tmpDir := testutil.TempDir(t, "call-workflow-typed")
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	require.NoError(t, os.MkdirAll(workflowsDir, 0755))

	typedInputs := `      environment:
        description: Target environment
        type: choice
        options:
          - dev
          - staging
          - production
        required: true
      dry_run:
        description: Run without applying changes
        type: boolean
        required: false
        default: false
      version:
        description: Package version
        type: string
        required: false
      timeout:
        description: Timeout in seconds
        type: number
        required: false
`
	createWorker(t, workflowsDir, "deploy", typedInputs)

	gatewayMD := `---
on: workflow_dispatch
engine: copilot
permissions:
  contents: read
safe-outputs:
  call-workflow:
    workflows:
      - deploy
---

# Typed Inputs Gateway

Deploy with the specified configuration.
`
	yaml := compileAndReadLock(t, filepath.Join(workflowsDir, "gateway.md"), gatewayMD)

	// The compiled YAML should contain the MCP tool JSON with typed parameters
	assert.Contains(t, yaml, "call-deploy:", "Should contain call-deploy job")
	assert.Contains(t, yaml, "uses: ./.github/workflows/deploy.lock.yml")

	// The safe_outputs_tools section should contain the deploy tool with typed parameters
	assert.Contains(t, yaml, "deploy", "Should reference deploy tool")
	assert.Contains(t, yaml, "environment", "Should include environment parameter from worker inputs")
}

// TestCallWorkflowCompile_WithAdditionalSafeOutputs tests that call-workflow works
// alongside other safe output types (add-comment, create-pull-request)
func TestCallWorkflowCompile_WithAdditionalSafeOutputs(t *testing.T) {
	tmpDir := testutil.TempDir(t, "call-workflow-combined")
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	require.NoError(t, os.MkdirAll(workflowsDir, 0755))

	createWorker(t, workflowsDir, "worker-a", "")

	gatewayMD := `---
on:
  issues:
    types: [opened]
engine: copilot
permissions:
  contents: read
safe-outputs:
  add-comment:
    max: 1
  call-workflow:
    workflows:
      - worker-a
    max: 1
---

# Gateway with Multiple Safe Outputs

Comment on the issue and then call the worker.
`
	yaml := compileAndReadLock(t, filepath.Join(workflowsDir, "gateway.md"), gatewayMD)

	// Both safe output types should be present in the compiled output
	assert.Contains(t, yaml, "add_comment", "Should include add_comment tool/step")
	assert.Contains(t, yaml, "call-worker-a:", "Should contain call-worker-a fan-out job")

	// safe_outputs job should declare call_workflow outputs
	assert.Contains(t, yaml, "call_workflow_name:", "Should declare call_workflow_name output")
	assert.Contains(t, yaml, "call_workflow_payload:", "Should declare call_workflow_payload output")
}

func TestWorkflowCallCompile_InjectsNetworkAllowedInputWhenEnabled(t *testing.T) {
	tmpDir := testutil.TempDir(t, "workflow-call-network-allowed")
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	require.NoError(t, os.MkdirAll(workflowsDir, 0755))

	workflowMD := `---
on: workflow_call
engine: copilot
permissions:
  contents: read
network:
  allowed:
    - defaults
  allowed-input: true
---

# Reusable worker

Compile with an optional workflow_call network input.
`
	lockYAML := compileAndReadLock(t, filepath.Join(workflowsDir, "worker.md"), workflowMD)

	onLockSection := extractWorkflowCallSection(lockYAML)
	workflowCallIdx := strings.Index(onLockSection, "workflow_call:")
	require.GreaterOrEqual(t, workflowCallIdx, 0, "on section should contain workflow_call:")
	workflowCallBlock := onLockSection[workflowCallIdx:]

	var compiled map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(lockYAML), &compiled), "compiled workflow should remain valid YAML")
	onMap, ok := compiled["on"].(map[string]any)
	require.True(t, ok, "compiled workflow should include an on map")
	workflowCallMap, ok := onMap["workflow_call"].(map[string]any)
	require.True(t, ok, "compiled workflow should include on.workflow_call")
	inputsMap, ok := workflowCallMap["inputs"].(map[string]any)
	require.True(t, ok, "compiled workflow should include on.workflow_call.inputs")
	networkAllowedMap, ok := inputsMap[NetworkAllowedInputName].(map[string]any)
	require.True(t, ok, "compiled workflow should include on.workflow_call.inputs.network_allowed")
	assert.Equal(t, networkAllowedInputDescription, networkAllowedMap["description"], "network_allowed description should round-trip as YAML")

	assert.Contains(t, workflowCallBlock, "network_allowed:", "workflow_call inputs should include network_allowed when opt-in is enabled")
	assert.Contains(t, workflowCallBlock, "type: string", "network_allowed workflow_call input should be typed as string")
	assert.Contains(t, lockYAML, "GH_AW_WORKFLOW_CALL_NETWORK_ALLOWED:", "engine step env should expose the workflow_call input at runtime")
	assert.Contains(t, lockYAML, "index.crates.io", "runtime network expansion should include known ecosystem domains")
}

func TestWorkflowCallCompile_DoesNotInjectNetworkAllowedInputByDefault(t *testing.T) {
	tmpDir := testutil.TempDir(t, "workflow-call-network-default")
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	require.NoError(t, os.MkdirAll(workflowsDir, 0755))

	workflowMD := `---
on: workflow_call
engine: copilot
permissions:
  contents: read
network:
  allowed:
    - defaults
---

# Reusable worker

Compile without the optional workflow_call network input.
`
	lockYAML := compileAndReadLock(t, filepath.Join(workflowsDir, "worker.md"), workflowMD)

	onLockSection := extractWorkflowCallSection(lockYAML)
	workflowCallIdx := strings.Index(onLockSection, "workflow_call:")
	require.GreaterOrEqual(t, workflowCallIdx, 0, "on section should contain workflow_call:")
	workflowCallBlock := onLockSection[workflowCallIdx:]

	assert.NotContains(t, workflowCallBlock, "network_allowed:", "workflow_call inputs should not include network_allowed unless opted in")
	assert.NotContains(t, lockYAML, "GH_AW_WORKFLOW_CALL_NETWORK_ALLOWED:", "engine step env should not reference network_allowed unless opted in")
}

// TestCallWorkflowCompile_WorkflowCallGateway tests compilation when the gateway itself
// is triggered via workflow_call (cross-repo use case)
func TestCallWorkflowCompile_WorkflowCallGateway(t *testing.T) {
	tmpDir := testutil.TempDir(t, "call-workflow-gateway-trigger")
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	require.NoError(t, os.MkdirAll(workflowsDir, 0755))

	createWorker(t, workflowsDir, "worker-a", "")
	createWorker(t, workflowsDir, "worker-b", "")

	gatewayMD := `---
on:
  workflow_call:
    inputs:
      issue_number:
        type: number
        required: true
engine: copilot
permissions:
  contents: read
safe-outputs:
  add-comment:
    max: 1
  call-workflow:
    workflows:
      - worker-a
      - worker-b
    max: 1
---

# Cross-Repo Gateway

Triggered via workflow_call from consumer repos.
Select the appropriate worker workflow.
`
	yaml := compileAndReadLock(t, filepath.Join(workflowsDir, "gateway.md"), gatewayMD)

	// Gateway should compile with workflow_call trigger AND call-workflow fan-out
	assert.Contains(t, yaml, "workflow_call", "Should contain workflow_call trigger")
	assert.Contains(t, yaml, "call-worker-a:", "Should contain call-worker-a job")
	assert.Contains(t, yaml, "call-worker-b:", "Should contain call-worker-b job")
	assert.Contains(t, yaml, "secrets: inherit", "Workers should inherit secrets")
}

// TestCallWorkflowCompile_SafeOutputsJobHasCallWorkflowOutputs checks that the
// compiled safe_outputs job declares call_workflow_name and call_workflow_payload outputs
func TestCallWorkflowCompile_SafeOutputsJobHasCallWorkflowOutputs(t *testing.T) {
	tmpDir := testutil.TempDir(t, "call-workflow-outputs")
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	require.NoError(t, os.MkdirAll(workflowsDir, 0755))

	createWorker(t, workflowsDir, "worker-a", "")

	gatewayMD := `---
on: workflow_dispatch
engine: copilot
permissions:
  contents: read
safe-outputs:
  add-comment:
    max: 1
  call-workflow:
    workflows:
      - worker-a
---

# Gateway

Route to the worker.
`
	yaml := compileAndReadLock(t, filepath.Join(workflowsDir, "gateway.md"), gatewayMD)

	// The safe_outputs job section should declare both call_workflow outputs
	assert.Contains(t, yaml, "call_workflow_name:", "safe_outputs job should declare call_workflow_name")
	assert.Contains(t, yaml, "call_workflow_payload:", "safe_outputs job should declare call_workflow_payload")
	// Both outputs should reference process_safe_outputs step
	assert.Contains(t, yaml, "steps.process_safe_outputs.outputs.call_workflow_name",
		"call_workflow_name should come from process_safe_outputs step")
	assert.Contains(t, yaml, "steps.process_safe_outputs.outputs.call_workflow_payload",
		"call_workflow_payload should come from process_safe_outputs step")
}

// TestCallWorkflowCompile_ConclusionJobDependsOnWorkers tests that the conclusion job
// lists all call-* jobs in its needs clause
func TestCallWorkflowCompile_ConclusionJobDependsOnWorkers(t *testing.T) {
	tmpDir := testutil.TempDir(t, "call-workflow-conclusion")
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	require.NoError(t, os.MkdirAll(workflowsDir, 0755))

	createWorker(t, workflowsDir, "worker-a", "")
	createWorker(t, workflowsDir, "worker-b", "")
	createWorker(t, workflowsDir, "worker-c", "")

	gatewayMD := `---
on:
  issues:
    types: [opened]
engine: copilot
permissions:
  contents: read
safe-outputs:
  add-comment:
    max: 1
  call-workflow:
    workflows:
      - worker-a
      - worker-b
      - worker-c
---

# Three-Worker Gateway

Route to one of three workers.
`
	yaml := compileAndReadLock(t, filepath.Join(workflowsDir, "gateway.md"), gatewayMD)

	// Verify all three conditional jobs are generated
	assert.Contains(t, yaml, "call-worker-a:")
	assert.Contains(t, yaml, "call-worker-b:")
	assert.Contains(t, yaml, "call-worker-c:")

	// Conclusion job should depend on all worker jobs (check the conclusion section)
	conclusionIdx := strings.Index(yaml, "conclusion:")
	if conclusionIdx >= 0 {
		conclusionSection := yaml[conclusionIdx:]
		// Check that the needs list for conclusion includes all call-* jobs
		assert.Contains(t, conclusionSection, "call-worker-a", "Conclusion should need call-worker-a")
		assert.Contains(t, conclusionSection, "call-worker-b", "Conclusion should need call-worker-b")
		assert.Contains(t, conclusionSection, "call-worker-c", "Conclusion should need call-worker-c")
	}
}

// TestCallWorkflowCompile_WorkerWithUnderscoresInName tests compilation when worker
// names contain underscores, which get converted to hyphens in job names
func TestCallWorkflowCompile_WorkerWithUnderscoresInName(t *testing.T) {
	tmpDir := testutil.TempDir(t, "call-workflow-underscores")
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	require.NoError(t, os.MkdirAll(workflowsDir, 0755))

	createWorker(t, workflowsDir, "spring_boot_bugfix", "")

	gatewayMD := `---
on: workflow_dispatch
engine: copilot
permissions:
  contents: read
safe-outputs:
  call-workflow:
    workflows:
      - spring_boot_bugfix
---

# Underscore Name Gateway

Worker name with underscores should produce hyphenated job name.
`
	yaml := compileAndReadLock(t, filepath.Join(workflowsDir, "gateway.md"), gatewayMD)

	// Job name should use hyphens even if workflow name has underscores
	assert.Contains(t, yaml, "call-spring-boot-bugfix:", "Job name should use hyphens")
	assert.Contains(t, yaml, "spring_boot_bugfix", "If condition should reference original name")
}

// TestCallWorkflowCompile_ValidationFails_MissingWorkflowCall verifies that compilation
// fails with a clear error when a worker does not have the workflow_call trigger
func TestCallWorkflowCompile_ValidationFails_MissingWorkflowCall(t *testing.T) {
	tmpDir := testutil.TempDir(t, "call-workflow-missing-trigger")
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	require.NoError(t, os.MkdirAll(workflowsDir, 0755))

	// Worker has workflow_dispatch (not workflow_call)
	noCallContent := `name: Worker without workflow_call
on:
  workflow_dispatch:
jobs:
  work:
    runs-on: ubuntu-latest
    steps:
      - run: echo "Working"
`
	err := os.WriteFile(filepath.Join(workflowsDir, "no-call-worker.lock.yml"), []byte(noCallContent), 0644)
	require.NoError(t, err)

	gatewayMD := `---
on: workflow_dispatch
engine: copilot
permissions:
  contents: read
safe-outputs:
  call-workflow:
    workflows:
      - no-call-worker
---

# Gateway referencing a worker without workflow_call
`
	gatewayFile := filepath.Join(workflowsDir, "gateway.md")
	require.NoError(t, os.WriteFile(gatewayFile, []byte(gatewayMD), 0644))

	compiler := NewCompiler(WithVersion("1.0.0"))
	err = compiler.CompileWorkflow(gatewayFile)
	require.Error(t, err, "Should fail when worker lacks workflow_call trigger")
	require.ErrorContains(t, err, "workflow_call", "Error should mention workflow_call")
	require.ErrorContains(t, err, "no-call-worker", "Error should mention the workflow name")
}

// TestCallWorkflowCompile_ValidationFails_SelfReference verifies that compilation fails
// when a gateway tries to call itself
func TestCallWorkflowCompile_ValidationFails_SelfReference(t *testing.T) {
	tmpDir := testutil.TempDir(t, "call-workflow-self-ref")
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	require.NoError(t, os.MkdirAll(workflowsDir, 0755))

	gatewayMD := `---
on: workflow_dispatch
engine: copilot
permissions:
  contents: read
safe-outputs:
  call-workflow:
    workflows:
      - gateway
---

# Self-referencing Gateway (should fail)
`
	gatewayFile := filepath.Join(workflowsDir, "gateway.md")
	require.NoError(t, os.WriteFile(gatewayFile, []byte(gatewayMD), 0644))

	compiler := NewCompiler(WithVersion("1.0.0"))
	err := compiler.CompileWorkflow(gatewayFile)
	require.Error(t, err, "Should fail for self-reference")
	require.ErrorContains(t, err, "self-reference", "Error should mention self-reference")
}

// TestCallWorkflowCompile_ValidationFails_WorkerNotFound verifies compilation fails
// when a listed worker file does not exist
func TestCallWorkflowCompile_ValidationFails_WorkerNotFound(t *testing.T) {
	tmpDir := testutil.TempDir(t, "call-workflow-not-found")
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	require.NoError(t, os.MkdirAll(workflowsDir, 0755))

	gatewayMD := `---
on: workflow_dispatch
engine: copilot
permissions:
  contents: read
safe-outputs:
  call-workflow:
    workflows:
      - nonexistent-worker
---

# Gateway referencing missing worker
`
	gatewayFile := filepath.Join(workflowsDir, "gateway.md")
	require.NoError(t, os.WriteFile(gatewayFile, []byte(gatewayMD), 0644))

	compiler := NewCompiler(WithVersion("1.0.0"))
	err := compiler.CompileWorkflow(gatewayFile)
	require.Error(t, err, "Should fail for missing worker")
	require.ErrorContains(t, err, "not found", "Error should mention not found")
	require.ErrorContains(t, err, "nonexistent-worker", "Error should name the missing workflow")
}

// TestCallWorkflowCompile_ValidationFails_DuplicateWorkflow verifies that compilation
// fails with a clear error when a workflow name appears more than once in the list
func TestCallWorkflowCompile_ValidationFails_DuplicateWorkflow(t *testing.T) {
	tmpDir := testutil.TempDir(t, "call-workflow-duplicate")
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	require.NoError(t, os.MkdirAll(workflowsDir, 0755))

	// Write a valid worker so the only failure is the duplicate, not "not found"
	require.NoError(t, os.WriteFile(filepath.Join(workflowsDir, "worker-a.lock.yml"),
		[]byte(workerLockYML("")), 0644))

	gatewayMD := `---
on: workflow_dispatch
engine: copilot
permissions:
  contents: read
safe-outputs:
  call-workflow:
    workflows:
      - worker-a
      - worker-a
---

# Gateway with duplicate worker name
`
	gatewayFile := filepath.Join(workflowsDir, "gateway.md")
	require.NoError(t, os.WriteFile(gatewayFile, []byte(gatewayMD), 0644))

	compiler := NewCompiler(WithVersion("1.0.0"))
	err := compiler.CompileWorkflow(gatewayFile)
	require.Error(t, err, "Should fail for duplicate workflow name")
	require.ErrorContains(t, err, "duplicate", "Error should mention duplicate")
	require.ErrorContains(t, err, "worker-a", "Error should name the duplicate workflow")
}

// TestCallWorkflowCompile_MDSourceWorker tests that compilation succeeds when the
// worker only exists as a .md source (same-batch compilation target)
func TestCallWorkflowCompile_MDSourceWorker(t *testing.T) {
	tmpDir := testutil.TempDir(t, "call-workflow-md-source")
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	require.NoError(t, os.MkdirAll(workflowsDir, 0755))

	// Worker exists only as .md (not yet compiled)
	workerMD := `---
on:
  workflow_call:
    inputs:
      payload:
        type: string
        required: false
engine: copilot
permissions:
  contents: read
---

# Worker (MD source only)

Does some work.
`
	err := os.WriteFile(filepath.Join(workflowsDir, "worker-md-only.md"), []byte(workerMD), 0644)
	require.NoError(t, err)

	gatewayMD := `---
on: workflow_dispatch
engine: copilot
permissions:
  contents: read
safe-outputs:
  add-comment:
    max: 1
  call-workflow:
    workflows:
      - worker-md-only
---

# Gateway referencing .md-only worker
`
	yaml := compileAndReadLock(t, filepath.Join(workflowsDir, "gateway.md"), gatewayMD)

	// The fan-out job should target the expected .lock.yml path (batch compilation will create it)
	assert.Contains(t, yaml, "call-worker-md-only:", "Should generate fan-out job for .md-only worker")
	assert.Contains(t, yaml, "uses: ./.github/workflows/worker-md-only.lock.yml",
		"Should reference .lock.yml even when only .md exists")
}

// TestCallWorkflowCompile_ConfigInSafeOutputsJSON tests that the call_workflow configuration
// is included in the safe_outputs JSON config embedded in the compiled lock file
func TestCallWorkflowCompile_ConfigInSafeOutputsJSON(t *testing.T) {
	tmpDir := testutil.TempDir(t, "call-workflow-config-json")
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	require.NoError(t, os.MkdirAll(workflowsDir, 0755))

	createWorker(t, workflowsDir, "worker-a", "")

	gatewayMD := `---
on: workflow_dispatch
engine: copilot
permissions:
  contents: read
safe-outputs:
  add-comment:
    max: 1
  call-workflow:
    workflows:
      - worker-a
    max: 1
---

# Gateway - Config JSON test

Verify that the safe_outputs configuration includes call_workflow config.
`
	yaml := compileAndReadLock(t, filepath.Join(workflowsDir, "gateway.md"), gatewayMD)

	// The compiled YAML should contain the call_workflow config in the safe_outputs JSON block
	assert.Contains(t, yaml, "call_workflow", "Lock file should contain call_workflow config key")
	assert.Contains(t, yaml, "worker-a", "Lock file should contain the allowed worker name")
}

// TestCallWorkflowCompile_IfConditionFormat tests that the generated if: condition
// uses the correct format for GitHub Actions expression evaluation
func TestCallWorkflowCompile_IfConditionFormat(t *testing.T) {
	tmpDir := testutil.TempDir(t, "call-workflow-if-condition")
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	require.NoError(t, os.MkdirAll(workflowsDir, 0755))

	createWorker(t, workflowsDir, "worker-a", "")

	gatewayMD := `---
on: workflow_dispatch
engine: copilot
permissions:
  contents: read
safe-outputs:
  add-comment:
    max: 1
  call-workflow:
    workflows:
      - worker-a
---

# If Condition Test

Verify the exact format of the if condition in the compiled YAML.
`
	yaml := compileAndReadLock(t, filepath.Join(workflowsDir, "gateway.md"), gatewayMD)

	// Verify the if condition references safe_outputs outputs
	assert.Contains(t, yaml, "needs.safe_outputs.outputs.call_workflow_name",
		"If condition should reference safe_outputs.call_workflow_name")
	assert.Contains(t, yaml, "worker-a", "If condition should include workflow name")

	// The with block should pass the payload
	assert.Contains(t, yaml, "needs.safe_outputs.outputs.call_workflow_payload",
		"With block should reference call_workflow_payload")
}

// TestCallWorkflowCompile_NoOtherSafeOutputs tests compilation with call-workflow as the
// only safe output type (no add-comment etc.)
func TestCallWorkflowCompile_NoOtherSafeOutputs(t *testing.T) {
	tmpDir := testutil.TempDir(t, "call-workflow-only")
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	require.NoError(t, os.MkdirAll(workflowsDir, 0755))

	createWorker(t, workflowsDir, "worker-a", "")

	// Note: call-workflow alone won't create a safe_outputs job by itself
	// because the handler needs to be part of the consolidated job. We add
	// add-comment to ensure the safe_outputs job is created.
	gatewayMD := `---
on: workflow_dispatch
engine: copilot
permissions:
  contents: read
safe-outputs:
  add-comment:
    max: 1
  call-workflow:
    workflows:
      - worker-a
---

# Minimal Gateway

Only calls a worker, no other safe outputs.
`
	yaml := compileAndReadLock(t, filepath.Join(workflowsDir, "gateway.md"), gatewayMD)

	assert.Contains(t, yaml, "call-worker-a:", "Should contain the worker fan-out job")
	assert.Contains(t, yaml, "safe_outputs:", "Should contain safe_outputs job")
}

// TestCallWorkflowCompile_WorkerNeedsPayloadInput tests that when a worker declares a
// 'payload' input in workflow_call.inputs, compilation succeeds (the JSON envelope pattern)
func TestCallWorkflowCompile_WorkerNeedsPayloadInput(t *testing.T) {
	tmpDir := testutil.TempDir(t, "call-workflow-payload-input")
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	require.NoError(t, os.MkdirAll(workflowsDir, 0755))

	// Worker that explicitly declares payload input (the recommended pattern)
	payloadInputs := `      payload:
        description: JSON-encoded input parameters
        type: string
        required: false
`
	createWorker(t, workflowsDir, "worker-a", payloadInputs)

	gatewayMD := `---
on: workflow_dispatch
engine: copilot
permissions:
  contents: read
safe-outputs:
  add-comment:
    max: 1
  call-workflow:
    workflows:
      - worker-a
---

# Gateway using JSON envelope pattern

Worker declares explicit payload input.
`
	yaml := compileAndReadLock(t, filepath.Join(workflowsDir, "gateway.md"), gatewayMD)

	assert.Contains(t, yaml, "call-worker-a:", "Should contain call-worker-a job")
	assert.Contains(t, yaml, "payload:", "Should wire payload in with block")
	assert.Contains(t, yaml, "call_workflow_payload", "Should reference call_workflow_payload output")
}

// TestCallWorkflowCompile_WorkerFoundInWorkflowsDir tests that workers are found in
// .github/workflows regardless of gateway location
func TestCallWorkflowCompile_WorkerFoundInWorkflowsDir(t *testing.T) {
	tmpDir := testutil.TempDir(t, "call-workflow-dir-search")
	awDir := filepath.Join(tmpDir, ".github", "aw")
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	require.NoError(t, os.MkdirAll(awDir, 0755))
	require.NoError(t, os.MkdirAll(workflowsDir, 0755))

	// Worker is in .github/workflows
	createWorker(t, workflowsDir, "worker-a", "")

	// Gateway is in .github/aw (different directory)
	gatewayMD := `---
on: workflow_dispatch
engine: copilot
permissions:
  contents: read
safe-outputs:
  add-comment:
    max: 1
  call-workflow:
    workflows:
      - worker-a
---

# Gateway in .github/aw

Worker is in .github/workflows - should be found via directory discovery.
`
	yaml := compileAndReadLock(t, filepath.Join(awDir, "gateway.md"), gatewayMD)

	assert.Contains(t, yaml, "call-worker-a:", "Should find and generate fan-out job for worker in workflows dir")
	assert.Contains(t, yaml, ".github/workflows/worker-a.lock.yml", "Should reference worker in workflows dir")
}

// TestCallWorkflowCompile_ForwardsTypedInputsAlongsidePayload tests that the compiler
// emits fromJSON-derived with: entries for each declared workflow_call input (except payload)
// alongside the canonical payload entry in the generated fan-out job.
func TestCallWorkflowCompile_ForwardsTypedInputsAlongsidePayload(t *testing.T) {
	tmpDir := testutil.TempDir(t, "call-workflow-forward-inputs")
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	require.NoError(t, os.MkdirAll(workflowsDir, 0755))

	typedInputs := `      payload:
        type: string
        required: false
      source_repo:
        description: Repository to check out
        type: string
        required: false
      issue_number:
        description: Issue number to process
        type: string
        required: false
`
	createWorker(t, workflowsDir, "worker-a", typedInputs)

	gatewayMD := `---
on: workflow_dispatch
engine: copilot
permissions:
  contents: read
safe-outputs:
  add-comment:
    max: 1
  call-workflow:
    workflows:
      - worker-a
---

# Gateway — typed input forwarding
`
	yaml := compileAndReadLock(t, filepath.Join(workflowsDir, "gateway.md"), gatewayMD)

	// Canonical payload transport must always be present
	assert.Contains(t, yaml, "payload: ${{ needs.safe_outputs.outputs.call_workflow_payload }}",
		"Should always forward canonical payload")

	// Typed inputs must be derived from the payload via fromJSON
	assert.Contains(t, yaml, "fromJSON(needs.safe_outputs.outputs.call_workflow_payload).source_repo",
		"Should forward source_repo via fromJSON")
	assert.Contains(t, yaml, "fromJSON(needs.safe_outputs.outputs.call_workflow_payload).issue_number",
		"Should forward issue_number via fromJSON")

	// payload must not appear as a fromJSON entry (it must remain the raw string)
	assert.NotContains(t, yaml, "fromJSON(needs.safe_outputs.outputs.call_workflow_payload).payload",
		"payload itself must not be duplicated as a fromJSON entry")
}

// TestCallWorkflowCompile_ForwardsHyphenatedInputs tests that the compiler correctly emits
// fromJSON-derived with: entries for inputs whose names contain hyphens (e.g. "task-description"),
// which are not valid Go/YAML identifiers but are valid GitHub Actions input names.
func TestCallWorkflowCompile_ForwardsHyphenatedInputs(t *testing.T) {
	tmpDir := testutil.TempDir(t, "call-workflow-hyphen-inputs")
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	require.NoError(t, os.MkdirAll(workflowsDir, 0755))

	typedInputs := `      payload:
        type: string
        required: false
      task-description:
        description: Human-readable description of the task to perform
        type: string
        required: false
`
	createWorker(t, workflowsDir, "worker-a", typedInputs)

	gatewayMD := `---
on: workflow_dispatch
engine: copilot
permissions:
  contents: read
safe-outputs:
  add-comment:
    max: 1
  call-workflow:
    workflows:
      - worker-a
---

# Gateway — hyphenated input forwarding
`
	yaml := compileAndReadLock(t, filepath.Join(workflowsDir, "gateway.md"), gatewayMD)

	// Canonical payload transport must always be present
	assert.Contains(t, yaml, "payload: ${{ needs.safe_outputs.outputs.call_workflow_payload }}",
		"Should always forward canonical payload")

	// Hyphenated input must use bracket access because dot access is invalid for `-`.
	assert.Contains(t, yaml, "fromJSON(needs.safe_outputs.outputs.call_workflow_payload)['task-description']",
		"Should forward task-description (hyphenated) via bracket access")
	assert.NotContains(t, yaml, "fromJSON(needs.safe_outputs.outputs.call_workflow_payload).task-description",
		"Hyphenated input must not use dot access")

	// payload must not appear as a fromJSON entry
	assert.NotContains(t, yaml, "fromJSON(needs.safe_outputs.outputs.call_workflow_payload).payload",
		"payload itself must not be duplicated as a fromJSON entry")
}

// workerLockYMLWithSecrets creates a worker lock file that declares on.workflow_call.secrets,
// simulating a worker compiled with the new secrets-inference feature.
func workerLockYMLWithSecrets(secrets []string) string {
	var secretsYAML strings.Builder
	for _, s := range secrets {
		secretsYAML.WriteString("      " + s + ":\n        required: false\n")
	}
	return `name: Worker
"on":
  workflow_call:
    inputs:
      payload:
        type: string
        required: false
    secrets:
` + secretsYAML.String() + `jobs:
  work:
    runs-on: ubuntu-latest
    steps:
      - run: echo "Working"
`
}

// createWorkerWithSecrets writes a worker lock file that includes on.workflow_call.secrets.
func createWorkerWithSecrets(t *testing.T, workflowsDir, name string, secrets []string) {
	t.Helper()
	content := workerLockYMLWithSecrets(secrets)
	err := os.WriteFile(filepath.Join(workflowsDir, name+".lock.yml"), []byte(content), 0644)
	require.NoErrorf(t, err, "Failed to write worker %s with secrets", name)
}

// TestCallWorkflowCompile_ExplicitSecretsWhenWorkerDeclaresThem verifies that when the worker
// declares on.workflow_call.secrets, the orchestrator emits explicit secret mappings instead
// of secrets: inherit.
func TestCallWorkflowCompile_ExplicitSecretsWhenWorkerDeclaresThem(t *testing.T) {
	tmpDir := testutil.TempDir(t, "call-workflow-explicit-secrets")
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	require.NoError(t, os.MkdirAll(workflowsDir, 0755))

	createWorkerWithSecrets(t, workflowsDir, "worker-a", []string{"MY_TOKEN", "ANOTHER_SECRET"})

	gatewayMD := `---
on: workflow_dispatch
engine: copilot
permissions:
  contents: read
safe-outputs:
  call-workflow:
    - worker-a
---

# Gateway with explicit secrets
`
	yaml := compileAndReadLock(t, filepath.Join(workflowsDir, "gateway.md"), gatewayMD)

	assert.Contains(t, yaml, "call-worker-a:", "Should contain call-worker-a job")
	assert.NotContains(t, yaml, "secrets: inherit", "Should NOT use secrets: inherit when worker declares secrets")
	assert.Contains(t, yaml, "MY_TOKEN: ${{ secrets.MY_TOKEN }}", "Should map MY_TOKEN explicitly")
	assert.Contains(t, yaml, "ANOTHER_SECRET: ${{ secrets.ANOTHER_SECRET }}", "Should map ANOTHER_SECRET explicitly")
}

// TestCallWorkflowCompile_FallsBackToInheritWhenNoSecretsInWorker verifies that when the worker
// does NOT declare on.workflow_call.secrets, the orchestrator falls back to secrets: inherit.
func TestCallWorkflowCompile_FallsBackToInheritWhenNoSecretsInWorker(t *testing.T) {
	tmpDir := testutil.TempDir(t, "call-workflow-inherit-fallback")
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	require.NoError(t, os.MkdirAll(workflowsDir, 0755))

	// Worker with NO secrets declarations (legacy format)
	createWorker(t, workflowsDir, "legacy-worker", "")

	gatewayMD := `---
on: workflow_dispatch
engine: copilot
permissions:
  contents: read
safe-outputs:
  call-workflow:
    - legacy-worker
---

# Gateway with legacy worker
`
	yaml := compileAndReadLock(t, filepath.Join(workflowsDir, "gateway.md"), gatewayMD)

	assert.Contains(t, yaml, "call-legacy-worker:", "Should contain call-legacy-worker job")
	assert.Contains(t, yaml, "secrets: inherit", "Should fall back to secrets: inherit for legacy worker")
}

// TestCallWorkflowCompile_WorkerGetsSecretsDeclarationsOnCompilation verifies that compiling
// a reusable workflow with workflow_call trigger injects on.workflow_call.secrets declarations
// for the secrets it uses.
func TestCallWorkflowCompile_WorkerGetsSecretsDeclarationsOnCompilation(t *testing.T) {
	tmpDir := testutil.TempDir(t, "call-workflow-worker-secrets-injection")
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	require.NoError(t, os.MkdirAll(workflowsDir, 0755))

	// Worker markdown with workflow_call trigger that uses a custom secret
	workerMD := `---
name: My Worker
on:
  workflow_call:
    inputs:
      payload:
        type: string
        required: false
engine: copilot
permissions:
  contents: read
safe-outputs:
  add-comment:
    max: 1
---

# Worker that uses a custom token

Use MY_CUSTOM_TOKEN from secrets.MY_CUSTOM_TOKEN when making API calls.
`
	workerFile := filepath.Join(workflowsDir, "my-worker.md")
	err := os.WriteFile(workerFile, []byte(workerMD), 0644)
	require.NoError(t, err)

	compiler := NewCompiler(WithVersion("1.0.0"))
	err = compiler.CompileWorkflow(workerFile)
	require.NoError(t, err, "Worker compilation should succeed")

	lockFile := workerFile[:len(workerFile)-3] + ".lock.yml"
	content, err := os.ReadFile(lockFile)
	require.NoError(t, err, "Lock file should exist after compilation")
	lockContent := string(content)

	// The compiled worker should declare its secrets in on.workflow_call.secrets
	assert.Contains(t, lockContent, "workflow_call:", "Should have workflow_call trigger")
	assert.Contains(t, lockContent, "COPILOT_GITHUB_TOKEN", "Should declare COPILOT_GITHUB_TOKEN")
	assert.Contains(t, lockContent, "GH_AW_GITHUB_TOKEN", "Should declare GH_AW_GITHUB_TOKEN")

	// GITHUB_TOKEN should NOT be declared in on.workflow_call.secrets.
	// Parse the compiled YAML and inspect the secrets map directly to avoid
	// any ambiguity from the string search approach.
	declaredSecrets := extractWorkflowCallSecretsFromParsedLock(t, lockContent)
	assert.NotContains(t, declaredSecrets, "GITHUB_TOKEN",
		"GITHUB_TOKEN should not be in on.workflow_call.secrets (auto-provided by GitHub Actions)")
	assert.Contains(t, declaredSecrets, "COPILOT_GITHUB_TOKEN",
		"COPILOT_GITHUB_TOKEN should be declared in on.workflow_call.secrets")
}
