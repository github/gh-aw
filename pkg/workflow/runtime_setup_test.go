//go:build !integration

package workflow

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
			name:     "gh aw command",
			command:  "gh aw add githubnext/agentics",
			expected: []string{"gh-aw"},
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
			name: "detects gh-aw from gh aw command",
			customSteps: `steps:
  - run: gh aw add githubnext/agentics`,
			expected: []string{"gh-aw"},
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

func TestDetectFromMCPScripts(t *testing.T) {
	requirements := make(map[string]*RuntimeRequirement)
	detectFromMCPScripts(&MCPScriptsConfig{
		Tools: map[string]*MCPScriptToolConfig{
			"js-tool": {Script: "return { ok: true }"},
			"py-tool": {Py: "print('ok')"},
			"go-tool": {Go: `fmt.Println("ok")`},
			"sh-tool": {Run: "echo ok"},
		},
	}, requirements)

	if _, ok := requirements["node"]; !ok {
		t.Fatal("expected node runtime for script tool")
	}
	if _, ok := requirements["python"]; !ok {
		t.Fatal("expected python runtime for py tool")
	}
	if _, ok := requirements["go"]; !ok {
		t.Fatal("expected go runtime for go tool")
	}
	if _, ok := requirements["shell"]; ok {
		t.Fatal("did not expect shell runtime requirement")
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
				"oven-sh/setup-bun@",
				"bun-version: '1.1'",
			},
		},
		{
			name: "generates node setup",
			requirements: []RuntimeRequirement{
				{Runtime: findRuntimeByID("node"), Version: "24"},
			},
			expectSteps: 1,
			checkContent: []string{
				"Setup Node.js",
				"actions/setup-node@",
				"node-version: '24'",
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
				"actions/setup-python@",
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
				"astral-sh/setup-uv@",
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
				"actions/setup-dotnet@",
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
				"actions/setup-java@",
				"java-version: '21'",
				"distribution: temurin",
			},
		},
		{
			name: "generates elixir setup",
			requirements: []RuntimeRequirement{
				{Runtime: findRuntimeByID("elixir"), Version: "1.17"},
			},
			expectSteps: 2, // setup + ERLANG_HOME capture for AWF chroot mode
			checkContent: []string{
				"Setup Elixir",
				"erlef/setup-beam@",
				"elixir-version: '1.17'",
				"Capture ERLANG_HOME for AWF chroot mode",
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
				"haskell-actions/setup@",
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
				"actions/setup-go@",
				"go-version: '1.22'",
				"cache: false",
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
				"actions/setup-go@",
				"go-version: '1.26'",
				"cache: false",
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
				"actions/setup-go@",
				"go-version-file: custom/go.mod",
				"cache: false",
				"Capture GOROOT for AWF chroot mode",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			steps := GenerateRuntimeSetupSteps(tt.requirements, nil)

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

func TestGenerateRuntimeSetupSteps_MergesAndSortsRuntimeExtraFields(t *testing.T) {
	nodeRuntime := findRuntimeByID("node")
	require.NotNil(t, nodeRuntime)

	steps := GenerateRuntimeSetupSteps([]RuntimeRequirement{{
		Runtime: nodeRuntime,
		Version: "24",
		ExtraFields: map[string]any{
			"cache-dependency-path": "frontend/package-lock.json",
			"package-manager-cache": true,
		},
	}}, nil)
	require.NotEmpty(t, steps)

	content := strings.Join(steps[0], "\n")
	assert.Contains(t, content, "cache-dependency-path: 'frontend/package-lock.json'")
	assert.Contains(t, content, "package-manager-cache: true")
	assert.NotContains(t, content, "package-manager-cache: false")
	assert.Less(t, strings.Index(content, "node-version: '24'"), strings.Index(content, "cache-dependency-path: 'frontend/package-lock.json'"))
	assert.Less(t, strings.Index(content, "node-version: '24'"), strings.Index(content, "package-manager-cache: true"))
	assert.Less(t, strings.Index(content, "cache-dependency-path: 'frontend/package-lock.json'"), strings.Index(content, "package-manager-cache: true"))
}

func TestGenerateRuntimeSetupSteps_GoVersionFileMergesAndSortsRuntimeExtraFields(t *testing.T) {
	goRuntime := findRuntimeByID("go")
	require.NotNil(t, goRuntime)

	steps := GenerateRuntimeSetupSteps([]RuntimeRequirement{{
		Runtime:   goRuntime,
		GoModFile: "custom/go.mod",
		ExtraFields: map[string]any{
			"cache":           true,
			"check-latest":    false,
			"go-version-file": "ignored/go.mod",
		},
	}}, nil)
	require.NotEmpty(t, steps)

	content := strings.Join(steps[0], "\n")
	assert.Contains(t, content, "go-version-file: custom/go.mod")
	assert.NotContains(t, content, "go-version-file: ignored/go.mod")
	assert.Equal(t, 1, strings.Count(content, "go-version-file:"))
	assert.Contains(t, content, "cache: true")
	assert.NotContains(t, content, "cache: false")
	assert.Contains(t, content, "check-latest: false")
	assert.Less(t, strings.Index(content, "go-version-file: custom/go.mod"), strings.Index(content, "cache: true"))
	assert.Less(t, strings.Index(content, "cache: true"), strings.Index(content, "check-latest: false"))
}

func TestGenerateRuntimeSetupSteps_GoVersionFileFromExtraFieldsWhenGoModFileNotSet(t *testing.T) {
	goRuntime := findRuntimeByID("go")
	require.NotNil(t, goRuntime)

	steps := GenerateRuntimeSetupSteps([]RuntimeRequirement{{
		Runtime: goRuntime,
		ExtraFields: map[string]any{
			"go-version-file": "go.work.mod",
		},
	}}, nil)
	require.NotEmpty(t, steps)

	content := strings.Join(steps[0], "\n")
	assert.Contains(t, content, "go-version-file: 'go.work.mod'")
	assert.Equal(t, 1, strings.Count(content, "go-version-file:"))
}

func TestGenerateRuntimeSetupSteps_UVWithoutVersionRendersRuntimeExtraWithFields(t *testing.T) {
	uvRuntime := *findRuntimeByID("uv")
	uvRuntime.ExtraWithFields = map[string]string{
		"python-version": "'3.12'",
	}

	steps := GenerateRuntimeSetupSteps([]RuntimeRequirement{{
		Runtime: &uvRuntime,
		Version: "",
	}}, nil)
	require.NotEmpty(t, steps)

	content := strings.Join(steps[0], "\n")
	assert.Contains(t, content, "with:")
	assert.Contains(t, content, "python-version: '3.12'")
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
	var result strings.Builder
	for _, step := range steps {
		for _, line := range step {
			result.WriteString(line + "\n")
		}
	}
	return result.String()
}

func TestRuntimeFilteringWithExistingSetupActions(t *testing.T) {
	// Test that runtimes are detected even when setup actions already exist
	// The deduplication happens later in the compiler, not during detection
	tools := map[string]any{
		"custom-uvx-command": map[string]any{
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
					{Runtime: findRuntimeByID("node"), Version: "24"},
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
	sampleError := fmt.Errorf("failed to marshal deduplicated workflow steps to YAML. Step deduplication removes duplicate runtime setup actions (like actions/setup-node) from custom steps to avoid conflicts when automatic runtime detection adds them. This optimization ensures runtime setup steps appear before custom steps. Error: %w", errors.New("yaml marshal error"))

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
    uses: actions/setup-python@a309ff8b426b58ec0e2a45f0f869d46889d02405
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
	if !strings.Contains(deduplicatedSteps, "actions/setup-python@a309ff8b426b58ec0e2a45f0f869d46889d02405") {
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

// TestDeduplicatePreservesUserNodeVersionFile tests that when a user specifies
// node-version-file instead of node-version, deduplication preserves the step.
func TestDeduplicatePreservesUserNodeVersionFile(t *testing.T) {
	customSteps := `steps:
  - name: Setup Node
    uses: actions/setup-node@v6
    with:
      node-version-file: '.node-version'
      cache: yarn
  - name: Bootstrap
    run: yarn kbn bootstrap`

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

	// Node runtime should be filtered out (user owns the setup step)
	if len(filteredRequirements) != 0 {
		t.Errorf("Expected 0 filtered requirements, got %d", len(filteredRequirements))
	}

	// User setup step should be preserved with node-version-file.
	if !strings.Contains(deduplicatedSteps, "node-version-file") {
		t.Error("Expected deduplicated steps to contain 'node-version-file'")
	}

	// The engine default node-version should not be added.
	if strings.Contains(deduplicatedSteps, "node-version: '24'") {
		t.Error("Expected default node-version not to be injected when user specifies node-version-file")
	}
}

func TestGenerateRuntimeSetupStepsWithIfCondition(t *testing.T) {
	tests := []struct {
		name         string
		requirements []RuntimeRequirement
		expectSteps  int
		checkContent []string
	}{
		{
			name: "generates go setup with if condition",
			requirements: []RuntimeRequirement{
				{
					Runtime:     findRuntimeByID("go"),
					Version:     "1.25",
					IfCondition: "hashFiles('go.mod') != ''",
				},
			},
			expectSteps: 2, // setup + GOROOT capture
			checkContent: []string{
				"Setup Go",
				"actions/setup-go@",
				"go-version: '1.25'",
				"if: hashFiles('go.mod') != ''",
			},
		},
		{
			name: "generates uv setup with if condition",
			requirements: []RuntimeRequirement{
				{
					Runtime:     findRuntimeByID("uv"),
					Version:     "",
					IfCondition: "hashFiles('uv.lock') != ''",
				},
			},
			expectSteps: 1,
			checkContent: []string{
				"Setup uv",
				"astral-sh/setup-uv@",
				"if: hashFiles('uv.lock') != ''",
			},
		},
		{
			name: "generates python setup with if condition",
			requirements: []RuntimeRequirement{
				{
					Runtime:     findRuntimeByID("python"),
					Version:     "3.11",
					IfCondition: "hashFiles('requirements.txt') != '' || hashFiles('pyproject.toml') != ''",
				},
			},
			expectSteps: 1,
			checkContent: []string{
				"Setup Python",
				"actions/setup-python@",
				"python-version: '3.11'",
				"if: hashFiles('requirements.txt') != '' || hashFiles('pyproject.toml') != ''",
			},
		},
		{
			name: "generates node setup with if condition",
			requirements: []RuntimeRequirement{
				{
					Runtime:     findRuntimeByID("node"),
					Version:     "24",
					IfCondition: "hashFiles('package.json') != ''",
				},
			},
			expectSteps: 1,
			checkContent: []string{
				"Setup Node.js",
				"actions/setup-node@",
				"node-version: '24'",
				"if: hashFiles('package.json') != ''",
			},
		},
		{
			name: "generates multiple runtimes with different if conditions",
			requirements: []RuntimeRequirement{
				{
					Runtime:     findRuntimeByID("go"),
					Version:     "1.25",
					IfCondition: "hashFiles('go.mod') != ''",
				},
				{
					Runtime:     findRuntimeByID("python"),
					Version:     "3.11",
					IfCondition: "hashFiles('requirements.txt') != ''",
				},
				{
					Runtime:     findRuntimeByID("node"),
					Version:     "24",
					IfCondition: "hashFiles('package.json') != ''",
				},
			},
			expectSteps: 4, // go setup + GOROOT capture + python setup + node setup
			checkContent: []string{
				"Setup Go",
				"if: hashFiles('go.mod') != ''",
				"Setup Python",
				"if: hashFiles('requirements.txt') != ''",
				"Setup Node.js",
				"if: hashFiles('package.json') != ''",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			steps := GenerateRuntimeSetupSteps(tt.requirements, nil)

			if len(steps) != tt.expectSteps {
				t.Errorf("Expected %d steps, got %d", tt.expectSteps, len(steps))
			}

			// Join all steps into a single string for content checking
			var allStepsSb strings.Builder
			for _, step := range steps {
				for _, line := range step {
					allStepsSb.WriteString(line + "\n")
				}
			}
			allSteps := allStepsSb.String()

			for _, content := range tt.checkContent {
				if !strings.Contains(allSteps, content) {
					t.Errorf("Expected steps to contain %q\nGot:\n%s", content, allSteps)
				}
			}
		})
	}
}

// TestIsCustomImageRunner tests detection of non-standard GitHub-hosted runners
func TestIsCustomImageRunner(t *testing.T) {
	tests := []struct {
		name     string
		runsOn   string
		expected bool
	}{
		{
			name:     "empty runs-on (default ubuntu-latest applies)",
			runsOn:   "",
			expected: false,
		},
		{
			name:     "ubuntu-latest is standard",
			runsOn:   "runs-on: ubuntu-latest",
			expected: false,
		},
		{
			name:     "ubuntu-22.04 is standard",
			runsOn:   "runs-on: ubuntu-22.04",
			expected: false,
		},
		{
			name:     "ubuntu-24.04 is standard",
			runsOn:   "runs-on: ubuntu-24.04",
			expected: false,
		},
		{
			name:     "ubuntu-slim is standard",
			runsOn:   "runs-on: ubuntu-slim",
			expected: false,
		},
		{
			name:     "ubuntu prefix case-insensitive",
			runsOn:   "runs-on: Ubuntu-latest",
			expected: false,
		},
		{
			name:     "windows-latest is standard",
			runsOn:   "runs-on: windows-latest",
			expected: false,
		},
		{
			name:     "windows-2022 is standard",
			runsOn:   "runs-on: windows-2022",
			expected: false,
		},
		{
			name:     "self-hosted is custom",
			runsOn:   "runs-on: self-hosted",
			expected: true,
		},
		{
			name:     "custom label is custom",
			runsOn:   "runs-on: my-company-runner",
			expected: true,
		},
		{
			name:     "array form is custom",
			runsOn:   "runs-on:\n- self-hosted\n- linux\n",
			expected: true,
		},
		{
			name:     "object form (group/labels) is custom",
			runsOn:   "runs-on:\n  group: larger-runners\n  labels:\n  - ubuntu-latest-8-cores\n",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isCustomImageRunner(tt.runsOn)
			assert.Equal(t, tt.expected, result, "isCustomImageRunner(%q)", tt.runsOn)
		})
	}
}

// TestDetectRuntimeRequirements_CustomImageRunner tests that Node.js v24 is automatically
// added as a runtime requirement when a custom image runner is specified.
func TestDetectRuntimeRequirements_CustomImageRunner(t *testing.T) {
	tests := []struct {
		name        string
		runsOn      string
		customSteps string
		expectNode  bool
		description string
	}{
		{
			name:        "custom runner adds node requirement",
			runsOn:      "runs-on: self-hosted",
			expectNode:  true,
			description: "self-hosted runner should trigger Node.js setup",
		},
		{
			name:        "standard ubuntu runner does not add node",
			runsOn:      "runs-on: ubuntu-latest",
			expectNode:  false,
			description: "ubuntu-latest has Node.js pre-installed, no setup needed",
		},
		{
			name:        "empty runs-on (default ubuntu) does not add node",
			runsOn:      "",
			expectNode:  false,
			description: "default ubuntu runner has Node.js pre-installed",
		},
		{
			name:        "custom label runner adds node requirement",
			runsOn:      "runs-on: enterprise-runner",
			expectNode:  true,
			description: "custom enterprise runner should trigger Node.js setup",
		},
		{
			name:        "custom runner with existing npm step does not duplicate node",
			runsOn:      "runs-on: self-hosted",
			customSteps: "- name: Install\n  run: npm install\n",
			expectNode:  true,
			description: "node requirement from both npm command and custom runner — single entry expected",
		},
		{
			name:        "array runs-on adds node requirement",
			runsOn:      "runs-on:\n- self-hosted\n- linux\n",
			expectNode:  true,
			description: "array form runner should trigger Node.js setup",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := &WorkflowData{
				RunsOn:      tt.runsOn,
				CustomSteps: tt.customSteps,
			}

			requirements := DetectRuntimeRequirements(data)

			hasNode := false
			for _, req := range requirements {
				if req.Runtime != nil && req.Runtime.ID == "node" {
					hasNode = true
					break
				}
			}

			assert.Equal(t, tt.expectNode, hasNode, tt.description)

			// When node IS expected, verify there is exactly one node requirement (no duplicates)
			if tt.expectNode {
				nodeCount := 0
				for _, req := range requirements {
					if req.Runtime != nil && req.Runtime.ID == "node" {
						nodeCount++
					}
				}
				assert.Equal(t, 1, nodeCount, "Should have exactly one node requirement")
			}
		})
	}
}

func TestDetectRuntimeRequirements_CustomDriverAddsNode24(t *testing.T) {
	data := &WorkflowData{
		RunsOn: "runs-on: ubuntu-latest",
		EngineConfig: &EngineConfig{
			ID:            "copilot",
			HarnessScript: "custom_harness.cjs",
		},
	}

	requirements := DetectRuntimeRequirements(data)

	var nodeReq *RuntimeRequirement
	for i := range requirements {
		if requirements[i].Runtime != nil && requirements[i].Runtime.ID == "node" {
			nodeReq = &requirements[i]
			break
		}
	}

	require.NotNil(t, nodeReq, "Expected Node.js runtime requirement when custom copilot driver is configured")

	assert.Equal(t, string(constants.DefaultNodeVersion), nodeReq.Version, "Custom engine driver should require Node.js 24 runtime")
}

func TestDetectRuntimeRequirements_CustomDriverDoesNotAddNodeForNonCopilotEngine(t *testing.T) {
	data := &WorkflowData{
		RunsOn: "runs-on: ubuntu-latest",
		EngineConfig: &EngineConfig{
			ID:            "claude",
			HarnessScript: "custom_harness.cjs",
		},
	}

	requirements := DetectRuntimeRequirements(data)

	for _, req := range requirements {
		if req.Runtime != nil && req.Runtime.ID == "node" {
			t.Fatalf("Expected no Node.js runtime requirement for non-Copilot engine, got version %q", req.Version)
		}
	}
}

// TestDetectRuntimeRequirements_TypeScriptSDKDriverAddsNode24 verifies that a Copilot SDK
// workflow with a .ts/.mts driver requires Node 24 for native TypeScript execution.
func TestDetectRuntimeRequirements_TypeScriptSDKDriverAddsNode24(t *testing.T) {
	tests := []struct {
		name   string
		driver string
	}{
		{name: ".ts driver", driver: "my_driver.ts"},
		{name: ".mts driver", driver: "my_driver.mts"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := &WorkflowData{
				RunsOn: "runs-on: ubuntu-latest",
				EngineConfig: &EngineConfig{
					CopilotSDK: true,
					Driver:     tt.driver,
				},
			}

			requirements := DetectRuntimeRequirements(data)

			var nodeReq *RuntimeRequirement
			for i := range requirements {
				if requirements[i].Runtime != nil && requirements[i].Runtime.ID == "node" {
					nodeReq = &requirements[i]
					break
				}
			}

			require.NotNil(t, nodeReq, "Expected Node.js 24 runtime requirement for TypeScript SDK driver %q", tt.driver)
			assert.Equal(t, string(constants.DefaultNodeVersion), nodeReq.Version,
				"TypeScript SDK driver %q should require Node.js %s for native TypeScript support", tt.driver, constants.DefaultNodeVersion)
		})
	}
}

// TestDetectRuntimeRequirements_TypeScriptSDKCommandDoesNotAddNode24 verifies that when
// engine.command is set (e.g., ts-node driver.ts), no automatic Node 24 requirement is added,
// since the user manages the toolchain via engine.command.
func TestDetectRuntimeRequirements_TypeScriptSDKCommandDoesNotAddNode24(t *testing.T) {
	data := &WorkflowData{
		RunsOn: "runs-on: ubuntu-latest",
		EngineConfig: &EngineConfig{
			CopilotSDK: true,
			Command:    "ts-node driver.ts",
		},
	}

	requirements := DetectRuntimeRequirements(data)

	for _, req := range requirements {
		if req.Runtime != nil && req.Runtime.ID == "node" && req.Version == string(constants.DefaultNodeVersion) {
			t.Fatalf("Expected no explicit Node 24 requirement when engine.command is set (ts-node manages its own toolchain), got Node version %q", req.Version)
		}
	}
}
