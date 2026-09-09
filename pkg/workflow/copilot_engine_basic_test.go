//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
)

func TestCopilotEngine(t *testing.T) {
	engine := NewCopilotEngine()

	// Test basic properties
	if engine.GetID() != "copilot" {
		t.Errorf("Expected copilot engine ID, got '%s'", engine.GetID())
	}

	if engine.GetDisplayName() != "GitHub Copilot CLI" {
		t.Errorf("Expected 'GitHub Copilot CLI' display name, got '%s'", engine.GetDisplayName())
	}

	if engine.IsExperimental() {
		t.Error("Expected copilot engine to not be experimental")
	}

	capabilities := engine.GetCapabilities()

	if !capabilities.ToolsAllowlist {
		t.Error("Expected copilot engine to support tools allowlist")
	}

	if !capabilities.MaxTurns {
		t.Error("Expected copilot engine to support max-turns")
	}

	// Test declared output files (session files are copied to logs folder)
	outputFiles := engine.GetDeclaredOutputFiles()
	if len(outputFiles) != 1 {
		t.Errorf("Expected 1 declared output file, got %d", len(outputFiles))
	}

	if outputFiles[0] != "/tmp/gh-aw/sandbox/agent/logs/" {
		t.Errorf("Expected declared output file to be logs folder, got %s", outputFiles[0])
	}
}

func TestCopilotEngineDefaultDetectionModel(t *testing.T) {
	engine := NewCopilotEngine()

	// CopilotEngine does not hardcode a detection model - it falls through to the
	// BaseEngine default (empty string), allowing the Copilot CLI to use its native
	// default model (currently claude-sonnet-4.6), matching the main agent behavior.
	defaultModel := engine.GetDefaultDetectionModel()
	if defaultModel != "" {
		t.Errorf("Expected empty default detection model (native CLI default), got '%s'", defaultModel)
	}
}

func TestOtherEnginesNoDefaultDetectionModel(t *testing.T) {
	// Test that other engines return empty string for GetDefaultDetectionModel
	engines := []CodingAgentEngine{
		NewClaudeEngine(),
		NewCodexEngine(),
	}

	for _, engine := range engines {
		defaultModel := engine.GetDefaultDetectionModel()
		if defaultModel != "" {
			t.Errorf("Expected engine '%s' to return empty default detection model, got '%s'", engine.GetID(), defaultModel)
		}
	}
}

func TestCopilotEngineInstallationSteps(t *testing.T) {
	engine := NewCopilotEngine()

	// Test with no version (firewall feature disabled by default)
	workflowData := &WorkflowData{}
	steps := engine.GetInstallationSteps(workflowData)
	// Secret validation is now in the activation job; installation only installs Copilot CLI.
	if len(steps) != 1 {
		t.Errorf("Expected 1 installation step, got %d", len(steps))
	}

	// Test with version (firewall feature disabled by default)
	workflowDataWithVersion := &WorkflowData{
		EngineConfig: &EngineConfig{Version: "1.0.0"},
	}
	stepsWithVersion := engine.GetInstallationSteps(workflowDataWithVersion)
	if len(stepsWithVersion) != 1 {
		t.Errorf("Expected 1 installation step with version, got %d", len(stepsWithVersion))
	}

	workflowDataWithSDK := &WorkflowData{
		EngineConfig: &EngineConfig{CopilotSDK: true},
	}
	stepsWithSDK := engine.GetInstallationSteps(workflowDataWithSDK)
	if len(stepsWithSDK) != 2 {
		t.Fatalf("Expected 2 installation steps with copilot-sdk enabled, got %d", len(stepsWithSDK))
	}
	sdkInstallStep := strings.Join(stepsWithSDK[1], "\n")
	if !strings.Contains(sdkInstallStep, "name: Install GitHub Copilot SDK (Node.js)") {
		t.Fatalf("Expected SDK install step name, got:\n%s", sdkInstallStep)
	}
	expectedSDKInstall := "cd \"${GITHUB_WORKSPACE}\" && npm install --ignore-scripts --no-save @github/copilot-sdk@" + string(constants.DefaultCopilotSDKVersion)
	if !strings.Contains(sdkInstallStep, expectedSDKInstall) {
		t.Fatalf("Expected SDK install command %q, got:\n%s", expectedSDKInstall, sdkInstallStep)
	}
	if !strings.Contains(sdkInstallStep, copilotSDKWebFetchDependency) {
		t.Fatalf("Expected SDK install command to include %q, got:\n%s", copilotSDKWebFetchDependency, sdkInstallStep)
	}
}

func TestCopilotEngineInstallationSteps_WithLSPConfig(t *testing.T) {
	engine := NewCopilotEngine()
	workflowData := &WorkflowData{
		Name: "test-workflow",
		LSP: map[string]LSPServerConfig{
			"python": {
				Command: "pyright-langserver",
				Args:    []string{"--stdio"},
				FileExtensions: map[string]string{
					".py": "python",
				},
			},
		},
	}

	steps := engine.GetInstallationSteps(workflowData)
	var allLines strings.Builder
	for _, step := range steps {
		allLines.WriteString(strings.Join(step, "\n"))
		allLines.WriteByte('\n')
	}
	allLinesStr := allLines.String()
	if !strings.Contains(allLinesStr, "Install Python LSP dependencies") {
		t.Fatalf("Expected Python LSP install step, got:\n%s", allLinesStr)
	}
	if !strings.Contains(allLinesStr, "npm install -g --ignore-scripts pyright") {
		t.Fatalf("Expected pyright install command, got:\n%s", allLinesStr)
	}
}

func TestCopilotEngineGetLogParserScript(t *testing.T) {
	engine := NewCopilotEngine()
	script := engine.GetLogParserScriptId()

	if script != "parse_copilot_log" {
		t.Errorf("Expected 'parse_copilot_log', got '%s'", script)
	}
}

func TestCopilotEngineGetLogFileForParsing(t *testing.T) {
	engine := NewCopilotEngine()
	logFile := engine.GetLogFileForParsing()

	expected := "/tmp/gh-aw/sandbox/agent/logs/"
	if logFile != expected {
		t.Errorf("Expected '%s', got '%s'", expected, logFile)
	}
}

func TestCopilotEngineSkipInstallationWithCommand(t *testing.T) {
	engine := NewCopilotEngine()

	// Test with custom command - should skip installation
	workflowData := &WorkflowData{
		EngineConfig: &EngineConfig{Command: "/usr/local/bin/custom-copilot"},
	}
	steps := engine.GetInstallationSteps(workflowData)

	if len(steps) != 0 {
		t.Errorf("Expected 0 installation steps when command is specified, got %d", len(steps))
	}

	// Test with custom command + firewall - should still install AWF runtime
	workflowData = &WorkflowData{
		EngineConfig: &EngineConfig{Command: "/usr/local/bin/custom-copilot"},
		NetworkPermissions: &NetworkPermissions{
			Firewall: &FirewallConfig{Enabled: true},
		},
	}
	steps = engine.GetInstallationSteps(workflowData)

	if len(steps) == 0 {
		t.Fatal("Expected installation steps when firewall is enabled with custom command")
	}

	installContent := strings.Join([]string(steps[0]), "\n")
	if !strings.Contains(installContent, "Install AWF binary") {
		t.Errorf("Expected AWF installation step when firewall is enabled with custom command, got:\n%s", installContent)
	}
}

func TestCopilotEngineInstallationWithCommandAndCopilotSDK(t *testing.T) {
	engine := NewCopilotEngine()

	tests := []struct {
		name          string
		command       string
		expectedName  string
		expectedRun   string
		withFirewall  bool
		expectedSteps int
	}{
		{
			name:          "node command uses npm sdk install",
			command:       "node ./agent.js",
			expectedName:  "name: Install GitHub Copilot SDK (Node.js)",
			expectedRun:   "npm install --ignore-scripts --no-save @github/copilot-sdk@" + string(constants.DefaultCopilotSDKVersion),
			expectedSteps: 1,
		},
		{
			name:          "python command uses pip sdk install",
			command:       "python3 main.py",
			expectedName:  "name: Install GitHub Copilot SDK (Python)",
			expectedRun:   "python3 -m pip install --disable-pip-version-check --target \"${GITHUB_WORKSPACE}/.gh-aw/copilot-sdk/python\" github-copilot-sdk==" + string(constants.DefaultCopilotSDKVersion),
			expectedSteps: 1,
		},
		{
			name:          "go command uses go get sdk install",
			command:       "go run ./cmd/agent",
			expectedName:  "name: Install GitHub Copilot SDK (Go)",
			expectedRun:   "go get github.com/github/copilot-sdk/go@v" + string(constants.DefaultCopilotSDKVersion),
			expectedSteps: 1,
		},
		{
			name:          "rust command uses cargo sdk install",
			command:       "cargo run --bin agent",
			expectedName:  "name: Install GitHub Copilot SDK (Rust)",
			expectedRun:   "cargo add github-copilot-sdk@" + string(constants.DefaultCopilotSDKVersion),
			expectedSteps: 1,
		},
		{
			name:          "dotnet command uses nuget sdk install",
			command:       "dotnet run --project src/Agent",
			expectedName:  "name: Install GitHub Copilot SDK (.NET)",
			expectedRun:   "dotnet add package GitHub.Copilot.SDK --version " + string(constants.DefaultCopilotSDKVersion),
			expectedSteps: 1,
		},
		{
			name:          "java command uses maven sdk install",
			command:       "mvn test",
			expectedName:  "name: Install GitHub Copilot SDK (Java)",
			expectedRun:   "mvn -q org.apache.maven.plugins:maven-dependency-plugin:3.8.1:get -Dartifact=com.github:copilot-sdk-java:" + string(constants.DefaultCopilotSDKVersion),
			expectedSteps: 1,
		},
		{
			name:          "runtime manager java command uses java sdk install",
			command:       "gradle test",
			expectedName:  "name: Install GitHub Copilot SDK (Java)",
			expectedRun:   "mvn -q org.apache.maven.plugins:maven-dependency-plugin:3.8.1:get -Dartifact=com.github:copilot-sdk-java:" + string(constants.DefaultCopilotSDKVersion),
			expectedSteps: 1,
		},
		{
			name:          "unsupported runtime falls back to node sdk install",
			command:       "bun run agent.ts",
			expectedName:  "name: Install GitHub Copilot SDK (Node.js)",
			expectedRun:   "npm install --ignore-scripts --no-save @github/copilot-sdk@" + string(constants.DefaultCopilotSDKVersion),
			expectedSteps: 1,
		},
		{
			name:          "ts-node command installs ts-node and typescript alongside sdk",
			command:       "ts-node driver.ts",
			expectedName:  "name: Install GitHub Copilot SDK (TypeScript)",
			expectedRun:   "npm install --ignore-scripts --no-save @github/copilot-sdk@" + string(constants.DefaultCopilotSDKVersion) + " " + copilotSDKWebFetchDependency + " ts-node typescript",
			expectedSteps: 1,
		},
		{
			name:          "env wrapper command is detected",
			command:       "env FOO=bar python script.py",
			expectedName:  "name: Install GitHub Copilot SDK (Python)",
			expectedRun:   "python3 -m pip install --disable-pip-version-check --target \"${GITHUB_WORKSPACE}/.gh-aw/copilot-sdk/python\" github-copilot-sdk==" + string(constants.DefaultCopilotSDKVersion),
			expectedSteps: 1,
		},
		{
			name:          "custom command with firewall keeps awf and sdk installs",
			command:       "python script.py",
			expectedName:  "name: Install GitHub Copilot SDK (Python)",
			expectedRun:   "python3 -m pip install --disable-pip-version-check --target \"${GITHUB_WORKSPACE}/.gh-aw/copilot-sdk/python\" github-copilot-sdk==" + string(constants.DefaultCopilotSDKVersion),
			withFirewall:  true,
			expectedSteps: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workflowData := &WorkflowData{
				EngineConfig: &EngineConfig{
					Command:    tt.command,
					CopilotSDK: true,
				},
			}
			if tt.withFirewall {
				workflowData.NetworkPermissions = &NetworkPermissions{
					Firewall: &FirewallConfig{Enabled: true},
				}
			}

			steps := engine.GetInstallationSteps(workflowData)
			if len(steps) != tt.expectedSteps {
				t.Fatalf("Expected %d installation steps, got %d", tt.expectedSteps, len(steps))
			}

			sdkStepContent := strings.Join(steps[0], "\n")
			if !strings.Contains(sdkStepContent, tt.expectedName) {
				t.Fatalf("Expected SDK install step name %q, got:\n%s", tt.expectedName, sdkStepContent)
			}
			if !strings.Contains(sdkStepContent, tt.expectedRun) {
				t.Fatalf("Expected SDK install command %q, got:\n%s", tt.expectedRun, sdkStepContent)
			}

			if tt.withFirewall {
				awfStepContent := strings.Join(steps[1], "\n")
				if !strings.Contains(awfStepContent, "Install AWF binary") {
					t.Fatalf("Expected AWF installation step with firewall enabled, got:\n%s", awfStepContent)
				}
			}
		})
	}
}

// TestCopilotEngineInstallationWithCopilotSDKDriver tests that using copilot-sdk-driver
// with various language extensions triggers the correct SDK install step.
func TestCopilotEngineInstallationWithCopilotSDKDriver(t *testing.T) {
	engine := NewCopilotEngine()

	tests := []struct {
		name         string
		driver       string
		expectedName string
		expectedRun  string
	}{
		{
			name:         "js driver uses npm sdk install",
			driver:       "my_driver.cjs",
			expectedName: "name: Install GitHub Copilot SDK (Node.js)",
			expectedRun:  "npm install --ignore-scripts --no-save @github/copilot-sdk@" + string(constants.DefaultCopilotSDKVersion),
		},
		{
			name:         "python driver uses pip sdk install",
			driver:       "my_driver.py",
			expectedName: "name: Install GitHub Copilot SDK (Python)",
			expectedRun:  "python3 -m pip install --disable-pip-version-check --target \"${GITHUB_WORKSPACE}/.gh-aw/copilot-sdk/python\" github-copilot-sdk==" + string(constants.DefaultCopilotSDKVersion),
		},
		{
			name:         "typescript driver uses node sdk install (node 24 native ts support)",
			driver:       "my_driver.ts",
			expectedName: "name: Install GitHub Copilot SDK (Node.js)",
			expectedRun:  "npm install --ignore-scripts --no-save @github/copilot-sdk@" + string(constants.DefaultCopilotSDKVersion),
		},
		{
			name:         "ruby driver uses npm sdk install fallback",
			driver:       "my_driver.rb",
			expectedName: "name: Install GitHub Copilot SDK (Node.js)",
			expectedRun:  "npm install --ignore-scripts --no-save @github/copilot-sdk@" + string(constants.DefaultCopilotSDKVersion),
		},
		{
			name:         "arbitrary command driver uses npm sdk install fallback",
			driver:       "my-copilot-driver",
			expectedName: "name: Install GitHub Copilot SDK (Node.js)",
			expectedRun:  "npm install --ignore-scripts --no-save @github/copilot-sdk@" + string(constants.DefaultCopilotSDKVersion),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workflowData := &WorkflowData{
				EngineConfig: &EngineConfig{
					CopilotSDK: true,
					Driver:     tt.driver,
				},
			}

			steps := engine.GetInstallationSteps(workflowData)
			if len(steps) != 2 {
				t.Fatalf("Expected 2 installation steps (Copilot CLI + SDK), got %d", len(steps))
			}

			sdkStepContent := strings.Join(steps[1], "\n")
			if !strings.Contains(sdkStepContent, tt.expectedName) {
				t.Fatalf("Expected SDK install step name %q, got:\n%s", tt.expectedName, sdkStepContent)
			}
			if !strings.Contains(sdkStepContent, tt.expectedRun) {
				t.Fatalf("Expected SDK install command %q, got:\n%s", tt.expectedRun, sdkStepContent)
			}
		})
	}
}
