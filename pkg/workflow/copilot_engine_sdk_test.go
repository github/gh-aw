//go:build !integration

package workflow

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/github/gh-aw/pkg/constants"
)

func containsEnvValue(stepContent, key, value string) bool {
	return strings.Contains(stepContent, key+": "+value) ||
		strings.Contains(stepContent, key+`: "`+value+`"`)
}

func TestCopilotSDKNpmInstallCooldownInMultilineStep(t *testing.T) {
	spec := copilotSDKInstallSpec{
		stepName: "Install test SDK",
		runLines: []string{"npm install --ignore-scripts --no-save example-sdk@1.2.3"},
	}

	step := strings.Join(specToInstallStep(spec, 7), "\n")
	if !strings.Contains(step, "NPM_CONFIG_MIN_RELEASE_AGE: '7'") ||
		strings.Contains(step, "npm --version") {
		t.Fatalf("Expected multiline npm SDK installs to use npm's built-in cooldown without a version-check script, got:\n%s", step)
	}
}

func TestCopilotSDKNpmInstallCooldownInSingleLineStep(t *testing.T) {
	spec := copilotSDKInstallSpec{
		stepName: "Install test SDK",
		command:  "npm install --ignore-scripts --no-save example-sdk@1.2.3",
	}
	for _, days := range []int{0, 3, 7} {
		step := strings.Join(specToInstallStep(spec, days), "\n")
		if !strings.Contains(step, "run: "+spec.command) ||
			!strings.Contains(step, "NPM_CONFIG_MIN_RELEASE_AGE: '"+strconv.Itoa(days)+"'") ||
			strings.Contains(step, "npm --version") {
			t.Fatalf("Expected single-line npm SDK install with native cooldown configuration, got:\n%s", step)
		}
	}
}

func TestCopilotSDKJavaInstallUsesMavenNotNpm(t *testing.T) {
	spec := getInlineCopilotSDKInstallSpec("java")
	step := strings.Join(specToInstallStep(spec, 7), "\n")
	if !strings.Contains(step, "mvn -q dependency:build-classpath") {
		t.Fatalf("Expected Java SDK installation to use Maven, got:\n%s", step)
	}
	if strings.Contains(step, "NPM_CONFIG_MIN_RELEASE_AGE") {
		t.Fatalf("Java/Maven installation must not receive npm-specific cooldown configuration, got:\n%s", step)
	}
}

func TestCopilotEngineExecutionStepsWithCopilotSDK(t *testing.T) {
	engine := NewCopilotEngine()
	workflowData := &WorkflowData{
		Name: "test-workflow",
		EngineConfig: &EngineConfig{
			CopilotSDK: true,
		},
	}

	steps := engine.GetExecutionSteps(workflowData, "/tmp/gh-aw/test.log")
	if len(steps) != 1 {
		t.Fatalf("Expected 1 execution step, got %d", len(steps))
	}

	stepContent := strings.Join([]string(steps[0]), "\n")

	if strings.Contains(stepContent, "--transport http") {
		t.Fatalf("Expected main copilot command to avoid --transport http when copilot-sdk is enabled, got:\n%s", stepContent)
	}

	// SDK URI env var must be set so the driver and SDK client can locate the sidecar.
	expectedURI := constants.CopilotSDKURIEnvVar + ": http://127.0.0.1:" + strconv.Itoa(constants.DefaultCopilotSDKPort)
	if !strings.Contains(stepContent, expectedURI) {
		t.Fatalf("Expected %s in step env, got:\n%s", expectedURI, stepContent)
	}
	expectedMaxToolDenials := constants.EnvVarMaxToolDenials + ": " + strconv.Itoa(constants.DefaultMaxToolDenials)
	if !strings.Contains(stepContent, expectedMaxToolDenials) {
		t.Fatalf("Expected %s in step env, got:\n%s", expectedMaxToolDenials, stepContent)
	}
	defaultTimeoutMinutes := strconv.Itoa(int(constants.DefaultAgenticWorkflowTimeout / time.Minute))
	expectedTimeoutEnv := "GH_AW_TIMEOUT_MINUTES: " + defaultTimeoutMinutes
	if !containsEnvValue(stepContent, "GH_AW_TIMEOUT_MINUTES", defaultTimeoutMinutes) {
		t.Fatalf("Expected %s in step env, got:\n%s", expectedTimeoutEnv, stepContent)
	}
	if !strings.Contains(stepContent, `npm root -g`) || !strings.Contains(stepContent, `export NODE_PATH=`) {
		t.Fatalf("Expected SDK mode command to configure NODE_PATH from npm global root, got:\n%s", stepContent)
	}
	if !strings.Contains(stepContent, `${GITHUB_WORKSPACE:-$PWD}/node_modules`) {
		t.Fatalf("Expected SDK mode command to configure NODE_PATH from workspace node_modules, got:\n%s", stepContent)
	}
	if !strings.Contains(stepContent, `${NODE_PATH:+:${NODE_PATH}}`) {
		t.Fatalf("Expected SDK mode command to preserve existing NODE_PATH entries, got:\n%s", stepContent)
	}

	// Driver mode: GH_AW_COPILOT_SDK_DRIVER must be set so the harness delegates to the driver.
	if !strings.Contains(stepContent, constants.CopilotSDKDriverEnvVar+": 1") {
		t.Fatalf("Expected %s: 1 in step env, got:\n%s", constants.CopilotSDKDriverEnvVar, stepContent)
	}

	// GH_AW_COPILOT_SDK_SERVER_ARGS must carry the JSON-encoded server arg list.
	if !strings.Contains(stepContent, constants.CopilotSDKServerArgsEnvVar+":'") &&
		!strings.Contains(stepContent, constants.CopilotSDKServerArgsEnvVar+": '") {
		// Try the plain (no-quotes) form too — YAML scalar style varies.
		if !strings.Contains(stepContent, constants.CopilotSDKServerArgsEnvVar+":") {
			t.Fatalf("Expected %s to be set in step env, got:\n%s", constants.CopilotSDKServerArgsEnvVar, stepContent)
		}
	}
	// The server args value must include the headless sidecar control flags.
	if !strings.Contains(stepContent, `"--headless"`) {
		t.Fatalf("Expected GH_AW_COPILOT_SDK_SERVER_ARGS to include --headless, got:\n%s", stepContent)
	}
	if !strings.Contains(stepContent, `"--port"`) {
		t.Fatalf("Expected GH_AW_COPILOT_SDK_SERVER_ARGS to include --port, got:\n%s", stepContent)
	}
	if strings.Contains(stepContent, `"--host"`) {
		t.Fatalf("Expected default SDK server args to omit --host, got:\n%s", stepContent)
	}
	if !strings.Contains(stepContent, `"--disable-builtin-mcps"`) {
		t.Fatalf("Expected GH_AW_COPILOT_SDK_SERVER_ARGS to include --disable-builtin-mcps, got:\n%s", stepContent)
	}
	if !strings.Contains(stepContent, `"--no-ask-user"`) {
		t.Fatalf("Expected GH_AW_COPILOT_SDK_SERVER_ARGS to include --no-ask-user, got:\n%s", stepContent)
	}

	// Driver mode: the harness command must reference copilot_sdk_driver.cjs.
	if !strings.Contains(stepContent, "copilot_sdk_driver.cjs") {
		t.Fatalf("Expected SDK driver mode command to include copilot_sdk_driver.cjs, got:\n%s", stepContent)
	}
	if strings.Contains(stepContent, `cd "${GITHUB_WORKSPACE}" &&`) {
		t.Fatalf("Expected SDK driver mode command to not use shell cd prefix (harness sets cwd via spawn options), got:\n%s", stepContent)
	}

	// No stdin pipe: configuration is in env vars, not piped JSON.
	if strings.Contains(stepContent, "| { ") {
		t.Fatalf("Expected SDK driver mode to not use stdin pipe (| { ... }), got:\n%s", stepContent)
	}

	// --prompt-file must never appear: the driver reads the prompt via GH_AW_PROMPT.
	if strings.Contains(stepContent, "--prompt-file") {
		t.Fatalf("Expected SDK mode to omit --prompt-file CLI arg (prompt is read via GH_AW_PROMPT env var), got:\n%s", stepContent)
	}

	// The promptFile JSON field must not appear (old stdin-payload format is gone).
	if strings.Contains(stepContent, `"promptFile"`) {
		t.Fatalf("Expected SDK driver mode to not embed promptFile JSON (old stdin format), got:\n%s", stepContent)
	}
}

func TestCopilotEngineExecutionStepsWithCopilotSDKCloudHypervisorBindHost(t *testing.T) {
	engine := NewCopilotEngine()
	workflowData := &WorkflowData{
		Name: "test-workflow",
		EngineConfig: &EngineConfig{
			CopilotSDK: true,
		},
		SandboxConfig: &SandboxConfig{Agent: &AgentSandboxConfig{Runtime: AgentRuntimeCloudHypervisor}},
	}

	steps := engine.GetExecutionSteps(workflowData, "/tmp/gh-aw/test.log")
	if len(steps) != 1 {
		t.Fatalf("Expected 1 execution step, got %d", len(steps))
	}

	stepContent := strings.Join([]string(steps[0]), "\n")
	if !strings.Contains(stepContent, `"--host","0.0.0.0","--port","`+strconv.Itoa(constants.DefaultCopilotSDKPort)+`"`) {
		t.Fatalf("Expected Cloud Hypervisor SDK server args to bind on all guest interfaces before port, got:\n%s", stepContent)
	}
	expectedURI := constants.CopilotSDKURIEnvVar + ": http://127.0.0.1:" + strconv.Itoa(constants.DefaultCopilotSDKPort)
	if !strings.Contains(stepContent, expectedURI) {
		t.Fatalf("Expected SDK client URI to remain loopback (%s), got:\n%s", expectedURI, stepContent)
	}
}

func TestCopilotEngineExecutionStepsWithCopilotSDKTimeoutExpression(t *testing.T) {
	engine := NewCopilotEngine()
	timeoutExpr := TemplatableInt32("${{ inputs.timeout }}")
	workflowData := &WorkflowData{
		Name: "test-workflow",
		ParsedFrontmatter: &FrontmatterConfig{
			TimeoutMinutes: &timeoutExpr,
		},
		EngineConfig: &EngineConfig{
			CopilotSDK: true,
		},
	}

	steps := engine.GetExecutionSteps(workflowData, "/tmp/gh-aw/test.log")
	if len(steps) != 1 {
		t.Fatalf("Expected 1 execution step, got %d", len(steps))
	}

	stepContent := strings.Join([]string(steps[0]), "\n")
	if !strings.Contains(stepContent, "timeout-minutes: ${{ inputs.timeout }}") {
		t.Fatalf("Expected timeout-minutes expression in step, got:\n%s", stepContent)
	}
	if !containsEnvValue(stepContent, "GH_AW_TIMEOUT_MINUTES", "${{ inputs.timeout }}") {
		t.Fatalf("Expected GH_AW_TIMEOUT_MINUTES expression in step env, got:\n%s", stepContent)
	}
}

func TestCopilotEngineExecutionStepsWithCopilotSDKCustomDriver(t *testing.T) {
	engine := NewCopilotEngine()
	workflowData := &WorkflowData{
		Name: "test-workflow",
		EngineConfig: &EngineConfig{
			CopilotSDK: true,
			Driver:     ".github/drivers/custom_copilot_sdk_driver.cjs",
		},
	}

	steps := engine.GetExecutionSteps(workflowData, "/tmp/gh-aw/test.log")
	if len(steps) != 1 {
		t.Fatalf("Expected 1 execution step, got %d", len(steps))
	}

	stepContent := strings.Join([]string(steps[0]), "\n")
	if !strings.Contains(stepContent, "custom_copilot_sdk_driver.cjs") {
		t.Fatalf("Expected SDK driver mode command to include custom_copilot_sdk_driver.cjs, got:\n%s", stepContent)
	}
	if !strings.Contains(stepContent, `${GITHUB_WORKSPACE}/.github/drivers/custom_copilot_sdk_driver.cjs`) {
		t.Fatalf("Expected custom SDK driver to resolve as ${GITHUB_WORKSPACE}/<path>, got:\n%s", stepContent)
	}
	if strings.Contains(stepContent, "/actions/copilot_sdk_driver.cjs") {
		t.Fatalf("Expected built-in SDK driver to be replaced, got:\n%s", stepContent)
	}
}

func TestCopilotEngineExecutionStepsWithCopilotSDKMaxToolDenialsOverride(t *testing.T) {
	engine := NewCopilotEngine()
	workflowData := &WorkflowData{
		Name: "test-workflow",
		EngineConfig: &EngineConfig{
			CopilotSDK:     true,
			MaxToolDenials: "${{ inputs.max-tool-denials }}",
		},
	}

	steps := engine.GetExecutionSteps(workflowData, "/tmp/gh-aw/test.log")
	if len(steps) != 1 {
		t.Fatalf("Expected 1 execution step, got %d", len(steps))
	}

	stepContent := strings.Join([]string(steps[0]), "\n")
	if !strings.Contains(stepContent, constants.EnvVarMaxToolDenials+": ${{ inputs.max-tool-denials }}") &&
		!strings.Contains(stepContent, constants.EnvVarMaxToolDenials+`: "${{ inputs.max-tool-denials }}"`) {
		t.Fatalf("Expected %s to include workflow expression override, got:\n%s", constants.EnvVarMaxToolDenials, stepContent)
	}
}

func TestCopilotEngineExecutionStepsWithCopilotSDKPythonDriver(t *testing.T) {
	engine := NewCopilotEngine()
	workflowData := &WorkflowData{
		Name: "test-workflow",
		EngineConfig: &EngineConfig{
			CopilotSDK: true,
			Driver:     "my_driver.py",
		},
	}

	steps := engine.GetExecutionSteps(workflowData, "/tmp/gh-aw/test.log")
	if len(steps) != 1 {
		t.Fatalf("Expected 1 execution step, got %d", len(steps))
	}

	stepContent := strings.Join([]string(steps[0]), "\n")
	if !strings.Contains(stepContent, "python3") {
		t.Fatalf("Expected Python SDK driver mode to use python3 runtime, got:\n%s", stepContent)
	}
	if !strings.Contains(stepContent, "my_driver.py") {
		t.Fatalf("Expected SDK driver mode to include my_driver.py, got:\n%s", stepContent)
	}
}

func TestCopilotEngineExecutionStepsWithCopilotSDKTypeScriptDriver(t *testing.T) {
	engine := NewCopilotEngine()
	workflowData := &WorkflowData{
		Name: "test-workflow",
		EngineConfig: &EngineConfig{
			CopilotSDK: true,
			Driver:     "my_driver.ts",
		},
	}

	steps := engine.GetExecutionSteps(workflowData, "/tmp/gh-aw/test.log")
	if len(steps) != 1 {
		t.Fatalf("Expected 1 execution step, got %d", len(steps))
	}

	stepContent := strings.Join([]string(steps[0]), "\n")
	// The harness is invoked as: <outer-node> copilot_harness.cjs <runtime-cmd> <driver> <copilot-binary>
	// Verify the runtime argument passed to the harness is GH_AW_NODE_EXEC (native Node, not ts-node).
	if !strings.Contains(stepContent, `copilot_harness.cjs" "$GH_AW_NODE_EXEC"`) {
		t.Fatalf("Expected TypeScript SDK driver to pass GH_AW_NODE_EXEC as runtime to harness, got:\n%s", stepContent)
	}
	if strings.Contains(stepContent, "ts-node") {
		t.Fatalf("Expected TypeScript SDK driver to NOT use ts-node (Node 24 runs TS natively), got:\n%s", stepContent)
	}
	if !strings.Contains(stepContent, "my_driver.ts") {
		t.Fatalf("Expected SDK driver mode to include my_driver.ts, got:\n%s", stepContent)
	}
}

// TestCopilotSDKDriverExecArgs directly verifies the runtime command returned for each
// driver file extension, ensuring TypeScript uses native Node.js (not ts-node).
func TestCopilotSDKDriverExecArgs(t *testing.T) {
	tests := []struct {
		driver         string
		wantRuntime    string
		wantDriverArg  string
		wantNotRuntime string
	}{
		{driver: "agent.js", wantRuntime: `"$GH_AW_NODE_EXEC"`, wantDriverArg: "agent.js"},
		{driver: "agent.cjs", wantRuntime: `"$GH_AW_NODE_EXEC"`, wantDriverArg: "agent.cjs"},
		{driver: "agent.mjs", wantRuntime: `"$GH_AW_NODE_EXEC"`, wantDriverArg: "agent.mjs"},
		{driver: "agent.ts", wantRuntime: `"$GH_AW_NODE_EXEC"`, wantDriverArg: "agent.ts", wantNotRuntime: "ts-node"},
		{driver: "agent.mts", wantRuntime: `"$GH_AW_NODE_EXEC"`, wantDriverArg: "agent.mts", wantNotRuntime: "ts-node"},
		{driver: "agent.py", wantRuntime: "python3", wantDriverArg: "agent.py"},
		{driver: "agent.rb", wantRuntime: "ruby", wantDriverArg: "agent.rb"},
		{driver: "my-driver", wantRuntime: "my-driver", wantDriverArg: ""},
	}

	for _, tt := range tests {
		t.Run(tt.driver, func(t *testing.T) {
			runtime, driverArg := copilotSDKDriverExecArgs(tt.driver)
			if runtime != tt.wantRuntime {
				t.Errorf("copilotSDKDriverExecArgs(%q) runtime = %q, want %q", tt.driver, runtime, tt.wantRuntime)
			}
			if driverArg != tt.wantDriverArg {
				t.Errorf("copilotSDKDriverExecArgs(%q) driverArg = %q, want %q", tt.driver, driverArg, tt.wantDriverArg)
			}
			if tt.wantNotRuntime != "" && runtime == tt.wantNotRuntime {
				t.Errorf("copilotSDKDriverExecArgs(%q) runtime = %q, must NOT be %q", tt.driver, runtime, tt.wantNotRuntime)
			}
		})
	}
}

func TestCopilotEngineExecutionStepsWithCopilotSDKRubyDriver(t *testing.T) {
	engine := NewCopilotEngine()
	workflowData := &WorkflowData{
		Name: "test-workflow",
		EngineConfig: &EngineConfig{
			CopilotSDK: true,
			Driver:     "my_driver.rb",
		},
	}

	steps := engine.GetExecutionSteps(workflowData, "/tmp/gh-aw/test.log")
	if len(steps) != 1 {
		t.Fatalf("Expected 1 execution step, got %d", len(steps))
	}

	stepContent := strings.Join([]string(steps[0]), "\n")
	if !strings.Contains(stepContent, "ruby") {
		t.Fatalf("Expected Ruby SDK driver mode to use ruby runtime, got:\n%s", stepContent)
	}
	if !strings.Contains(stepContent, "my_driver.rb") {
		t.Fatalf("Expected SDK driver mode to include my_driver.rb, got:\n%s", stepContent)
	}
}

func TestCopilotEngineExecutionStepsWithCopilotSDKArbitraryDriver(t *testing.T) {
	engine := NewCopilotEngine()
	workflowData := &WorkflowData{
		Name: "test-workflow",
		EngineConfig: &EngineConfig{
			CopilotSDK: true,
			Driver:     "my-copilot-driver",
		},
	}

	steps := engine.GetExecutionSteps(workflowData, "/tmp/gh-aw/test.log")
	if len(steps) != 1 {
		t.Fatalf("Expected 1 execution step, got %d", len(steps))
	}

	stepContent := strings.Join([]string(steps[0]), "\n")
	if !strings.Contains(stepContent, "my-copilot-driver") {
		t.Fatalf("Expected arbitrary SDK driver mode to include driver name, got:\n%s", stepContent)
	}
	// Arbitrary driver should NOT be prefixed with SetupActionDestinationShell path
	if strings.Contains(stepContent, SetupActionDestinationShell+"/my-copilot-driver") {
		t.Fatalf("Expected arbitrary SDK driver not to be prefixed with setup action path, got:\n%s", stepContent)
	}
}

func TestCopilotEngineExecutionStepsWithCopilotSDKPermissionConfig(t *testing.T) {
	engine := NewCopilotEngine()
	workflowData := &WorkflowData{
		Name: "test-workflow",
		EngineConfig: &EngineConfig{
			CopilotSDK: true,
		},
		Tools: map[string]any{
			"bash": []any{"git"},
			"edit": nil,
		},
	}

	steps := engine.GetExecutionSteps(workflowData, "/tmp/gh-aw/test.log")
	if len(steps) != 1 {
		t.Fatalf("Expected 1 execution step, got %d", len(steps))
	}

	stepContent := strings.Join([]string(steps[0]), "\n")
	if strings.Contains(stepContent, `"permissionConfig":{`) {
		t.Fatalf("Expected SDK driver mode to avoid legacy permissionConfig stdin JSON payload, got:\n%s", stepContent)
	}
	if !strings.Contains(stepContent, `"--allow-tool"`) ||
		!strings.Contains(stepContent, `"shell(git:*)"`) ||
		!strings.Contains(stepContent, `"write"`) {
		t.Fatalf("Expected GH_AW_COPILOT_SDK_SERVER_ARGS to include normalized allow-tool entries, got:\n%s", stepContent)
	}
}

func TestCopilotEngineCopilotSDKPythonDriverSetsPythonPath(t *testing.T) {
	engine := NewCopilotEngine()

	t.Run("python driver sets PYTHONPATH", func(t *testing.T) {
		workflowData := &WorkflowData{
			EngineConfig: &EngineConfig{
				CopilotSDK: true,
				Driver:     ".github/drivers/copilot_sdk_driver_sample_python.py",
			},
		}

		steps := engine.GetExecutionSteps(workflowData, "/tmp/gh-aw/test.log")
		if len(steps) != 1 {
			t.Fatalf("Expected 1 execution step, got %d", len(steps))
		}

		stepContent := strings.Join(steps[0], "\n")
		if !strings.Contains(stepContent, "PYTHONPATH: ${{ github.workspace }}/.gh-aw/copilot-sdk/python") {
			t.Fatalf("Expected PYTHONPATH to include python Copilot SDK target path, got:\n%s", stepContent)
		}
	})

	t.Run("node driver does not set PYTHONPATH", func(t *testing.T) {
		workflowData := &WorkflowData{
			EngineConfig: &EngineConfig{
				CopilotSDK: true,
				Driver:     ".github/drivers/copilot_sdk_driver_sample_node.cjs",
			},
		}

		steps := engine.GetExecutionSteps(workflowData, "/tmp/gh-aw/test.log")
		if len(steps) != 1 {
			t.Fatalf("Expected 1 execution step, got %d", len(steps))
		}

		stepContent := strings.Join(steps[0], "\n")
		if strings.Contains(stepContent, "PYTHONPATH: ${{ github.workspace }}/.gh-aw/copilot-sdk/python") {
			t.Fatalf("Did not expect PYTHONPATH override for node driver, got:\n%s", stepContent)
		}
	})
}
