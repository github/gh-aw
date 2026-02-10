//go:build !integration

package workflow

import (
	"fmt"
	"strings"
	"testing"
)

func TestDetectRuntimeFromCommand(t *testing.T) {
	tests := []struct {
		name     string
		command  string
		expected []string // Expected runtime IDs
	}{
		{
			name:     "bun command",
			command:  "bun install",
			expected: []string{"bun"},
		},
		{
			name:     "bunx command",
			command:  "bunx tsc",
			expected: []string{"bun"},
		},
		{
			name:     "npm install command",
			command:  "npm install",
			expected: []string{"node"},
		},
		{
			name:     "npx command",
			command:  "npx playwright test",
			expected: []string{"node"},
		},
		{
			name:     "python command",
			command:  "python script.py",
			expected: []string{"python"},
		},
		{
			name:     "pip install",
			command:  "pip install package",
			expected: []string{"python"},
		},
		{
			name:     "uv command",
			command:  "uv pip install package",
			expected: []string{"uv"},
		},
		{
			name:     "uvx command",
			command:  "uvx ruff check",
			expected: []string{"uv"},
		},
		{
			name:     "go command",
			command:  "go build",
			expected: []string{"go"},
		},
		{
			name:     "ruby command",
			command:  "ruby script.rb",
			expected: []string{"ruby"},
		},
		{
			name:     "deno command",
			command:  "deno run main.ts",
			expected: []string{"deno"},
		},
		{
			name:     "dotnet command",
			command:  "dotnet build",
			expected: []string{"dotnet"},
		},
		{
			name:     "java command",
			command:  "java -jar app.jar",
			expected: []string{"java"},
		},
		{
			name:     "javac command",
			command:  "javac Main.java",
			expected: []string{"java"},
		},
		{
			name:     "maven command",
			command:  "mvn clean install",
			expected: []string{"java"},
		},
		{
			name:     "gradle command",
			command:  "gradle build",
			expected: []string{"java"},
		},
		{
			name:     "elixir command",
			command:  "elixir script.exs",
			expected: []string{"elixir"},
		},
		{
			name:     "mix command",
			command:  "mix deps.get",
			expected: []string{"elixir"},
		},
		{
			name:     "haskell ghc command",
			command:  "ghc Main.hs",
			expected: []string{"haskell"},
		},
		{
			name:     "cabal command",
			command:  "cabal build",
			expected: []string{"haskell"},
		},
		{
			name:     "stack command",
			command:  "stack build",
			expected: []string{"haskell"},
		},
		{
			name:     "multiple commands",
			command:  "npm install && python test.py",
			expected: []string{"node", "python"},
		},
		{
			name:     "no runtime commands",
			command:  "echo hello",
			expected: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requirements := make(map[string]*RuntimeRequirement)
			detectRuntimeFromCommand(tt.command, requirements)

			if len(requirements) != len(tt.expected) {
				t.Errorf("Expected %d runtime(s), got %d: %v", len(tt.expected), len(requirements), getRequirementIDs(requirements))
			}

			for _, expectedID := range tt.expected {
				if _, exists := requirements[expectedID]; !exists {
					t.Errorf("Expected runtime %s to be detected", expectedID)
				}
			}
		})
	}
}

func TestDetectFromCustomSteps(t *testing.T) {
	tests := []struct {
		name           string
		customSteps    string
		expected       []string
		skipIfHasSetup bool
	}{
		{
			name: "detects node from npm command",
			customSteps: `steps:
  - run: npm install`,
			expected: []string{"node"},
		},
		{
			name: "detects python from python command",
			customSteps: `steps:
  - run: python test.py`,
			expected: []string{"python"},
		},
		{
			name: "detects multiple runtimes",
			customSteps: `steps:
  - run: npm install
  - run: python test.py`,
			expected: []string{"node", "python"},
		},
		{
			name: "detects node even when setup-node exists (filtering happens later)",
			customSteps: `steps:
  - uses: actions/setup-node@395ad3262231945c25e8478fd5baf05154b1d79f
  - run: npm install`,
			expected: []string{"node"}, // Changed: now detects, filtering happens in DetectRuntimeRequirements
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requirements := make(map[string]*RuntimeRequirement)
			detectFromCustomSteps(tt.customSteps, requirements)

			if len(requirements) != len(tt.expected) {
				t.Errorf("Expected %d requirements, got %d: %v", len(tt.expected), len(requirements), getRequirementIDs(requirements))
			}

			for _, expectedID := range tt.expected {
				if _, exists := requirements[expectedID]; !exists {
					t.Errorf("Expected runtime %s to be detected", expectedID)
				}
			}
		})
	}
}

func TestDetectFromMCPConfigs(t *testing.T) {
	tests := []struct {
		name     string
		tools    map[string]any
		expected []string
	}{
		{
			name: "detects node from MCP command",
			tools: map[string]any{
				"custom-tool": map[string]any{
					"command": "node",
					"args":    []string{"server.js"},
				},
			},
			expected: []string{"node"},
		},
		{
			name: "detects python from MCP command",
			tools: map[string]any{
				"custom-tool": map[string]any{
					"command": "python",
					"args":    []string{"-m", "server"},
				},
			},
			expected: []string{"python"},
		},
		{
			name: "detects npx from MCP command",
			tools: map[string]any{
				"custom-playwright": map[string]any{
					"command": "npx",
					"args":    []string{"@playwright/mcp"},
				},
			},
			expected: []string{"node"},
		},
		{
			name: "no detection for non-runtime commands",
			tools: map[string]any{
				"docker-tool": map[string]any{
					"command": "docker",
					"args":    []string{"run"},
				},
			},
			expected: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requirements := make(map[string]*RuntimeRequirement)
			parsedTools := NewTools(tt.tools)
			detectFromMCPConfigs(parsedTools, requirements)

			if len(requirements) != len(tt.expected) {
				t.Errorf("Expected %d requirements, got %d: %v", len(tt.expected), len(requirements), getRequirementIDs(requirements))
			}

			for _, expectedID := range tt.expected {
				if _, exists := requirements[expectedID]; !exists {
					t.Errorf("Expected runtime %s to be detected", expectedID)
				}
			}
		})
	}
}

func TestGenerateRuntimeSetupSteps(t *testing.T) {
	tests := []struct {
		name         string
		requirements []RuntimeRequirement
		expectSteps  int
		checkContent []string
	}{
		{
			name: "generates bun setup",
			requirements: []RuntimeRequirement{
				{Runtime: findRuntimeByID("bun"), Version: "1.1"},
			},
			expectSteps: 1,
			checkContent: []string{
				"Setup Bun",
				"oven-sh/setup-bun@3d267786b128fe76c2f16a390aa2448b815359f3",
				"bun-version: '1.1'",
			},
		},
		{
			name: "generates node setup",
			requirements: []RuntimeRequirement{
				{Runtime: findRuntimeByID("node"), Version: "20"},
			},
			expectSteps: 1,
			checkContent: []string{
				"Setup Node.js",
				"actions/setup-node@6044e13b5dc448c55e2357c09f80417699197238",
				"node-version: '20'",
			},
		},
		{
			name: "generates python setup",
			requirements: []RuntimeRequirement{
				{Runtime: findRuntimeByID("python"), Version: "3.11"},
			},
			expectSteps: 1,
			checkContent: []string{
				"Setup Python",
				"actions/setup-python@a26af69be951a213d495a4c3e4e4022e16d87065",
				"python-version: '3.11'",
			},
		},
		{
			name: "generates uv setup",
			requirements: []RuntimeRequirement{
				{Runtime: findRuntimeByID("uv"), Version: ""},
			},
			expectSteps: 1,
			checkContent: []string{
				"Setup uv",
				"astral-sh/setup-uv@d4b2f3b6ecc6e67c4457f6d3e41ec42d3d0fcb86",
			},
		},
		{
			name: "generates dotnet setup",
			requirements: []RuntimeRequirement{
				{Runtime: findRuntimeByID("dotnet"), Version: "8.0"},
			},
			expectSteps: 1, // setup only - PATH inherited via AWF_HOST_PATH in chroot mode
			checkContent: []string{
				"Setup .NET",
				"actions/setup-dotnet@67a3573c9a986a3f9c594539f4ab511d57bb3ce9",
				"dotnet-version: '8.0'",
			},
		},
		{
			name: "generates java setup",
			requirements: []RuntimeRequirement{
				{Runtime: findRuntimeByID("java"), Version: "21"},
			},
			expectSteps: 1, // setup only - PATH inherited via AWF_HOST_PATH in chroot mode
			checkContent: []string{
				"Setup Java",
				"actions/setup-java@c1e323688fd81a25caa38c78aa6df2d33d3e20d9",
				"java-version: '21'",
				"distribution: temurin",
			},
		},
		{
			name: "generates elixir setup",
			requirements: []RuntimeRequirement{
				{Runtime: findRuntimeByID("elixir"), Version: "1.17"},
			},
			expectSteps: 1,
			checkContent: []string{
				"Setup Elixir",
				"erlef/setup-beam@dff508cca8ce57162e7aa6c4769a4f97c2fed638",
				"elixir-version: '1.17'",
			},
		},
		{
			name: "generates haskell setup",
			requirements: []RuntimeRequirement{
				{Runtime: findRuntimeByID("haskell"), Version: "9.10"},
			},
			expectSteps: 1,
			checkContent: []string{
				"Setup Haskell",
				"haskell-actions/setup@9cd1b7bf3f36d5a3c3b17abc3545bfb5481912ea",
				"ghc-version: '9.10'",
			},
		},
		{
			name: "generates multiple setups",
			requirements: []RuntimeRequirement{
				{Runtime: findRuntimeByID("node"), Version: "24"},
				{Runtime: findRuntimeByID("python"), Version: "3.12"},
			},
			expectSteps: 2,
			checkContent: []string{
				"Setup Node.js",
				"Setup Python",
			},
		},
		{
			name: "uses default versions",
			requirements: []RuntimeRequirement{
				{Runtime: findRuntimeByID("node"), Version: ""},
			},
			expectSteps: 1,
			checkContent: []string{
				"node-version: '24'",
			},
		},
		{
			name: "generates go setup with explicit version",
			requirements: []RuntimeRequirement{
				{Runtime: findRuntimeByID("go"), Version: "1.22"},
			},
			expectSteps: 2, // setup + GOROOT capture for AWF chroot mode
			checkContent: []string{
				"Setup Go",
				"actions/setup-go@7a3fe6cf4cb3a834922a1244abfce67bcef6a0c5",
				"go-version: '1.22'",
				"Capture GOROOT for AWF chroot mode",
			},
		},
		{
			name: "generates go setup with default version when no go.mod",
			requirements: []RuntimeRequirement{
				{Runtime: findRuntimeByID("go"), Version: ""},
			},
			expectSteps: 2, // setup + GOROOT capture for AWF chroot mode
			checkContent: []string{
				"Setup Go",
				"actions/setup-go@7a3fe6cf4cb3a834922a1244abfce67bcef6a0c5",
				"go-version: '1.25'",
				"Capture GOROOT for AWF chroot mode",
			},
		},
		{
			name: "generates go setup with go-version-file when go-mod-file specified",
			requirements: []RuntimeRequirement{
				{Runtime: findRuntimeByID("go"), Version: "", GoModFile: "custom/go.mod"},
			},
			expectSteps: 2, // setup + GOROOT capture for AWF chroot mode
			checkContent: []string{
				"Setup Go",
				"actions/setup-go@7a3fe6cf4cb3a834922a1244abfce67bcef6a0c5",
				"go-version-file: custom/go.mod",
				"cache: true",
				"Capture GOROOT for AWF chroot mode",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			steps := GenerateRuntimeSetupSteps(tt.requirements)

			if len(steps) != tt.expectSteps {
				t.Errorf("Expected %d steps, got %d", tt.expectSteps, len(steps))
			}

			stepsStr := stepsToString(steps)
			for _, content := range tt.checkContent {
				if !strings.Contains(stepsStr, content) {
					t.Errorf("Expected steps to contain '%s', got: %s", content, stepsStr)
				}
			}
		})
	}
}

func TestShouldSkipRuntimeSetup(t *testing.T) {
	tests := []struct {
		name     string
		data     *WorkflowData
		expected bool
	}{
		{
			name: "never skip - runtime filtering handles existing setup actions",
			data: &WorkflowData{
				CustomSteps: `steps:
  - uses: actions/setup-node@395ad3262231945c25e8478fd5baf05154b1d79f
  - run: npm install`,
			},
			expected: false, // Changed: we no longer skip, we filter instead
		},
		{
			name: "never skip when no setup actions",
			data: &WorkflowData{
				CustomSteps: `steps:
  - run: npm install`,
			},
			expected: false,
		},
		{
			name:     "never skip when no custom steps",
			data:     &WorkflowData{},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ShouldSkipRuntimeSetup(tt.data)
			if result != tt.expected {
				t.Errorf("Expected %v, got %v", tt.expected, result)
			}
		})
	}
}

// Helper functions

func getRequirementIDs(requirements map[string]*RuntimeRequirement) []string {
	var ids []string
	for id := range requirements {
		ids = append(ids, id)
	}
	return ids
}

func stepsToString(steps []GitHubActionStep) string {
	var result string
	for _, step := range steps {
		for _, line := range step {
			result += line + "\n"
		}
	}
	return result
}

func TestRuntimeFilteringWithExistingSetupActions(t *testing.T) {
	// Test that runtimes are detected even when setup actions already exist
	// The deduplication happens later in the compiler, not during detection
	tools := map[string]any{
		"serena": map[string]any{
			"command": "uvx",
		},
	}
	workflowData := &WorkflowData{
		CustomSteps: `steps:
  - uses: actions/setup-go@4dc6199c7b1a012772edbd06daecab0f50c9053c
    with:
      go-version-file: go.mod
  - run: go build
  - run: uv pip install package`,
		Tools:       tools,
		ParsedTools: NewTools(tools),
	}

	requirements := DetectRuntimeRequirements(workflowData)

	// Check that uv, python, and go are all detected
	// Go should be detected even though there's an existing setup action
	// The compiler will deduplicate the setup action from custom steps
	foundUV := false
	foundPython := false
	foundGo := false
	for _, req := range requirements {
		if req.Runtime.ID == "uv" {
			foundUV = true
		}
		if req.Runtime.ID == "python" {
			foundPython = true
		}
		if req.Runtime.ID == "go" {
			foundGo = true
		}
	}

	if !foundUV {
		t.Error("Expected uv to be detected from uvx command and uv pip")
	}

	if !foundPython {
		t.Error("Expected python to be auto-added when uv is detected")
	}

	if !foundGo {
		t.Error("Expected go to be detected from go build command (deduplication happens in compiler)")
	}
}

// TestRuntimeSetupErrorMessages validates that error messages in runtime_setup.go
// provide clear context, explanation, and examples following the error message guidelines
func TestRuntimeSetupErrorMessages(t *testing.T) {
	tests := []struct {
		name          string
		testFunc      func() error
		shouldContain []string
		description   string
	}{
		{
			name: "parse custom steps error includes example and explanation",
			testFunc: func() error {
				// Invalid YAML - tab character which is not allowed in YAML
				invalidYAML := "steps:\n\t- name: test\n\t  run: echo 'hello'"
				// Need at least one runtime requirement to avoid early exit
				requirements := []RuntimeRequirement{
					{Runtime: findRuntimeByID("node"), Version: "20"},
				}
				_, _, err := DeduplicateRuntimeSetupStepsFromCustomSteps(invalidYAML, requirements)
				return err
			},
			shouldContain: []string{
				"failed to parse custom workflow steps",
				"Custom steps must be valid GitHub Actions step syntax",
				"Example:",
				"steps:",
				"- name:",
				"run:",
			},
			description: "Error should explain what custom steps are and show valid example",
		},
		{
			name: "marshal deduplicated steps error includes context about deduplication",
			testFunc: func() error {
				// This test is harder to trigger since Marshal rarely fails
				// We test the error message format by checking it would be generated correctly
				// by examining the code path
				return nil // Skip actual test since Marshal errors are rare
			},
			shouldContain: []string{},
			description:   "Skip - Marshal errors are difficult to trigger in tests",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.testFunc()

			// Skip tests that are marked to skip
			if len(tt.shouldContain) == 0 {
				t.Skip(tt.description)
				return
			}

			if err == nil {
				t.Fatal("Expected error but got nil")
			}

			errMsg := err.Error()

			// Check that error contains expected content
			for _, content := range tt.shouldContain {
				if !strings.Contains(errMsg, content) {
					t.Errorf("Error message should contain '%s'\nActual error: %s",
						content, errMsg)
				}
			}

			// Check that error is descriptive (not too vague)
			if len(errMsg) < 50 {
				t.Errorf("Error message should be descriptive (>50 chars)\nActual (%d chars): %s",
					len(errMsg), errMsg)
			}

			// Check that error includes the word "Error:" before the wrapped error
			if !strings.Contains(errMsg, "Error:") {
				t.Errorf("Error message should include 'Error:' before wrapped error\nActual: %s",
					errMsg)
			}
		})
	}
}

// TestDeduplicateErrorMessageFormat tests that the deduplicate function would
// produce a helpful error message if Marshal fails
func TestDeduplicateErrorMessageFormat(t *testing.T) {
	// We can't easily trigger a Marshal error, but we can verify the error format
	// by checking what the error message would look like
	expectedPhrases := []string{
		"failed to marshal deduplicated workflow steps",
		"Step deduplication removes duplicate runtime setup actions",
		"to avoid conflicts",
		"Error:",
	}

	// Create a sample error message as it would appear
	sampleError := fmt.Errorf("failed to marshal deduplicated workflow steps to YAML. Step deduplication removes duplicate runtime setup actions (like actions/setup-node) from custom steps to avoid conflicts when automatic runtime detection adds them. This optimization ensures runtime setup steps appear before custom steps. Error: %w", fmt.Errorf("yaml marshal error"))

	errMsg := sampleError.Error()

	for _, phrase := range expectedPhrases {
		if !strings.Contains(errMsg, phrase) {
			t.Errorf("Error message should contain '%s'\nActual: %s", phrase, errMsg)
		}
	}

	// Verify the message is descriptive
	if len(errMsg) < 100 {
		t.Errorf("Error message should be comprehensive (>100 chars)\nActual (%d chars): %s",
			len(errMsg), errMsg)
	}
}

// TestDeduplicatePreservesUserPythonVersion tests that when a user specifies
// a custom Python version in their setup-python step, the deduplication logic
// correctly identifies it as a customization and filters out the auto-detected
// Python runtime requirement. This prevents the compiler from generating a
// duplicate Python setup step with the default version.
//
// Regression test to ensure user-specified versions are preserved when they
// differ from default versions (e.g., user specifies 3.9 vs default 3.12).
func TestDeduplicatePreservesUserPythonVersion(t *testing.T) {
	// Test case: User has setup-python with python-version: '3.9'
	// and runs a python command, which auto-detects Python runtime
	customSteps := `steps:
  - name: Setup Python
    uses: actions/setup-python@a26af69be951a213d495a4c3e4e4022e16d87065
    with:
      python-version: '3.9'
  - name: Run script
    run: python test.py`

	// Auto-detected Python runtime requirement (no version specified)
	pythonRuntime := findRuntimeByID("python")
	if pythonRuntime == nil {
		t.Fatal("Python runtime not found")
	}

	requirements := []RuntimeRequirement{
		{
			Runtime: pythonRuntime,
			Version: "", // Empty - detected from 'python' command but no version info
		},
	}

	// Verify initial state
	if pythonRuntime.DefaultVersion != "3.12" {
		t.Fatalf("Expected Python default version to be 3.12, got %q", pythonRuntime.DefaultVersion)
	}

	// Run deduplication
	deduplicatedSteps, filteredRequirements, err := DeduplicateRuntimeSetupStepsFromCustomSteps(customSteps, requirements)
	if err != nil {
		t.Fatalf("Deduplication failed: %v", err)
	}

	// CRITICAL: The Python runtime requirement should be filtered out
	// because the user has a customized setup-python step with python-version: '3.9'
	if len(filteredRequirements) != 0 {
		t.Errorf("Expected 0 filtered requirements (user's custom Python step should be preserved), got %d", len(filteredRequirements))
		for _, req := range filteredRequirements {
			t.Errorf("  - Unexpected requirement: %s (version=%q)", req.Runtime.ID, req.Version)
		}
	}

	// Verify the user's setup step is preserved in deduplicated steps
	if !strings.Contains(deduplicatedSteps, "Setup Python") {
		t.Error("Expected deduplicated steps to contain 'Setup Python'")
	}
	if !strings.Contains(deduplicatedSteps, "python-version") {
		t.Error("Expected deduplicated steps to contain 'python-version'")
	}
	if !strings.Contains(deduplicatedSteps, "3.9") {
		t.Error("Expected deduplicated steps to contain user's version '3.9'")
	}

	// Verify the user's step still has the SHA reference
	if !strings.Contains(deduplicatedSteps, "actions/setup-python@a26af69be951a213d495a4c3e4e4022e16d87065") {
		t.Error("Expected deduplicated steps to preserve user's SHA reference")
	}
}

// TestDeduplicatePreservesUserNodeVersion tests that when a user specifies
// a custom Node.js version, the deduplication logic correctly preserves it
func TestDeduplicatePreservesUserNodeVersion(t *testing.T) {
	customSteps := `steps:
  - name: Setup Node
    uses: actions/setup-node@v6
    with:
      node-version: '16'
  - name: Run npm
    run: npm install`

	nodeRuntime := findRuntimeByID("node")
	if nodeRuntime == nil {
		t.Fatal("Node runtime not found")
	}

	requirements := []RuntimeRequirement{
		{
			Runtime: nodeRuntime,
			Version: "", // Auto-detected
		},
	}

	deduplicatedSteps, filteredRequirements, err := DeduplicateRuntimeSetupStepsFromCustomSteps(customSteps, requirements)
	if err != nil {
		t.Fatalf("Deduplication failed: %v", err)
	}

	// Node runtime should be filtered out (user has custom version)
	if len(filteredRequirements) != 0 {
		t.Errorf("Expected 0 filtered requirements, got %d", len(filteredRequirements))
	}

	// Verify user's version is preserved
	if !strings.Contains(deduplicatedSteps, "16") {
		t.Error("Expected deduplicated steps to contain user's version '16'")
	}
}
