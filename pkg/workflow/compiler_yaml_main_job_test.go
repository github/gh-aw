//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParseRepositoryImportSpec tests parsing of repository import specifications
func TestParseRepositoryImportSpec(t *testing.T) {
	tests := []struct {
		name         string
		importSpec   string
		wantOwner    string
		wantRepo     string
		wantRef      string
		shouldBeZero bool
	}{
		{
			name:       "full spec with ref",
			importSpec: "owner/repo@branch",
			wantOwner:  "owner",
			wantRepo:   "repo",
			wantRef:    "branch",
		},
		{
			name:       "spec without ref defaults to main",
			importSpec: "owner/repo",
			wantOwner:  "owner",
			wantRepo:   "repo",
			wantRef:    "main",
		},
		{
			name:       "spec with section reference",
			importSpec: "owner/repo@v1.0.0#Section",
			wantOwner:  "owner",
			wantRepo:   "repo",
			wantRef:    "v1.0.0",
		},
		{
			name:       "spec with sha ref",
			importSpec: "owner/repo@abc123def456",
			wantOwner:  "owner",
			wantRepo:   "repo",
			wantRef:    "abc123def456",
		},
		{
			name:         "invalid spec without slash",
			importSpec:   "invalid-spec",
			shouldBeZero: true,
		},
		{
			name:         "invalid spec with too many slashes",
			importSpec:   "owner/repo/extra/path@ref",
			shouldBeZero: true,
		},
		{
			name:       "spec with branch name containing slash",
			importSpec: "owner/repo@feature/branch-name",
			wantOwner:  "owner",
			wantRepo:   "repo",
			wantRef:    "feature/branch-name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner, repo, ref := parseRepositoryImportSpec(tt.importSpec)

			if tt.shouldBeZero {
				assert.Empty(t, owner, "owner should be empty for invalid spec")
				assert.Empty(t, repo, "repo should be empty for invalid spec")
			} else {
				assert.Equal(t, tt.wantOwner, owner, "owner mismatch")
				assert.Equal(t, tt.wantRepo, repo, "repo mismatch")
				assert.Equal(t, tt.wantRef, ref, "ref mismatch")
			}
		})
	}
}

// TestSanitizeRefForPath tests sanitization of git refs for use in file paths
func TestSanitizeRefForPath(t *testing.T) {
	tests := []struct {
		name     string
		ref      string
		expected string
	}{
		{
			name:     "simple branch name",
			ref:      "main",
			expected: "main",
		},
		{
			name:     "branch with slashes",
			ref:      "feature/my-feature",
			expected: "feature-my-feature",
		},
		{
			name:     "ref with colons",
			ref:      "refs:heads:main",
			expected: "refs-heads-main",
		},
		{
			name:     "ref with backslashes",
			ref:      "branch\\name",
			expected: "branch-name",
		},
		{
			name:     "complex ref with multiple special chars",
			ref:      "feature/test:branch\\name",
			expected: "feature-test-branch-name",
		},
		{
			name:     "tag ref",
			ref:      "v1.0.0",
			expected: "v1.0.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sanitizeRefForPath(tt.ref)
			assert.Equal(t, tt.expected, result, "sanitized ref mismatch")
		})
	}
}

// TestGenerateRepositoryImportCheckouts tests generation of checkout steps for repository imports
func TestGenerateRepositoryImportCheckouts(t *testing.T) {
	tests := []struct {
		name              string
		repositoryImports []string
		expectInOutput    []string
		notInOutput       []string
	}{
		{
			name:              "single repository import",
			repositoryImports: []string{"github/example@main"},
			expectInOutput: []string{
				"- name: Checkout repository import github/example@main",
				"repository: github/example",
				"ref: main",
				"path: .github/aw/imports/github-example-main",
				"sparse-checkout: |",
				".github/",
				"persist-credentials: false",
			},
		},
		{
			name:              "multiple repository imports",
			repositoryImports: []string{"owner1/repo1@v1.0", "owner2/repo2@main"},
			expectInOutput: []string{
				"Checkout repository import owner1/repo1@v1.0",
				"repository: owner1/repo1",
				"ref: v1.0",
				"path: .github/aw/imports/owner1-repo1-v1.0",
				"Checkout repository import owner2/repo2@main",
				"repository: owner2/repo2",
				"ref: main",
				"path: .github/aw/imports/owner2-repo2-main",
			},
		},
		{
			name:              "repository with branch containing slash",
			repositoryImports: []string{"org/repo@feature/test-branch"},
			expectInOutput: []string{
				"Checkout repository import org/repo@feature/test-branch",
				"repository: org/repo",
				"ref: feature/test-branch",
				"path: .github/aw/imports/org-repo-feature-test-branch",
			},
		},
		{
			name:              "empty imports list",
			repositoryImports: []string{},
			expectInOutput:    []string{},
			notInOutput:       []string{"Checkout repository import"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()
			var yaml strings.Builder

			compiler.generateRepositoryImportCheckouts(&yaml, tt.repositoryImports)
			result := yaml.String()

			for _, expected := range tt.expectInOutput {
				assert.Contains(t, result, expected, "output should contain %q", expected)
			}

			for _, notExpected := range tt.notInOutput {
				assert.NotContains(t, result, notExpected, "output should not contain %q", notExpected)
			}
		})
	}
}

// TestGenerateLegacyAgentImportCheckout tests generation of checkout steps for legacy agent imports
// Note: The parser extracts only owner/repo from the spec, ignoring path components
func TestGenerateLegacyAgentImportCheckout(t *testing.T) {
	tests := []struct {
		name            string
		agentImportSpec string
		expectInOutput  []string
		shouldGenerate  bool
	}{
		{
			name:            "basic legacy agent import - valid format",
			agentImportSpec: "github/example@main",
			expectInOutput: []string{
				"- name: Checkout agent import github/example@main",
				"repository: github/example",
				"ref: main",
				"path: /tmp/gh-aw/repo-imports/github-example-main",
				"sparse-checkout: |",
				".github/",
				"persist-credentials: false",
			},
			shouldGenerate: true,
		},
		{
			name:            "legacy import with complex ref",
			agentImportSpec: "org/repo@feature/branch",
			expectInOutput: []string{
				"Checkout agent import org/repo@feature/branch",
				"repository: org/repo",
				"ref: feature/branch",
				"path: /tmp/gh-aw/repo-imports/org-repo-feature-branch",
			},
			shouldGenerate: true,
		},
		{
			name:            "legacy import with path components - only owner/repo extracted",
			agentImportSpec: "github/example/agents/test.md@main",
			expectInOutput:  []string{},
			shouldGenerate:  false, // Parser rejects specs with more than 2 slash parts
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()
			var yaml strings.Builder

			compiler.generateLegacyAgentImportCheckout(&yaml, tt.agentImportSpec)
			result := yaml.String()

			if tt.shouldGenerate {
				for _, expected := range tt.expectInOutput {
					assert.Contains(t, result, expected, "output should contain %q", expected)
				}
			} else {
				assert.Empty(t, result, "should not generate output for invalid spec")
			}
		})
	}
}

// TestGenerateDevModeCLIBuildSteps tests generation of dev mode CLI build steps
func TestGenerateDevModeCLIBuildSteps(t *testing.T) {
	compiler := NewCompiler()
	var yaml strings.Builder

	compiler.generateDevModeCLIBuildSteps(&yaml)
	result := yaml.String()

	// Verify all required build steps are present
	expectedSteps := []string{
		"- name: Setup Go for CLI build",
		"uses: actions/setup-go@",
		"go-version-file: go.mod",
		"cache: true",
		"- name: Build gh-aw CLI",
		"mkdir -p dist",
		"CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build",
		"-o dist/gh-aw-linux-amd64",
		"./cmd/gh-aw",
		"- name: Setup Docker Buildx",
		"uses: docker/setup-buildx-action@",
		"- name: Build gh-aw Docker image",
		"uses: docker/build-push-action@",
		"context: .",
		"platforms: linux/amd64",
		"push: false",
		"load: true",
		"tags: localhost/gh-aw:dev",
		"BINARY=dist/gh-aw-linux-amd64",
	}

	for _, expected := range expectedSteps {
		assert.Contains(t, result, expected, "dev mode build should contain %q", expected)
	}
}

// TestAddCustomStepsAsIs tests adding custom steps without modification
func TestAddCustomStepsAsIs(t *testing.T) {
	tests := []struct {
		name           string
		customSteps    string
		expectInOutput []string
		notInOutput    []string
	}{
		{
			name: "basic custom steps",
			customSteps: `steps:
  - name: Test Step
    run: echo "test"
  - name: Another Step
    run: ls -la`,
			expectInOutput: []string{
				"- name: Test Step",
				`run: echo "test"`,
				"- name: Another Step",
				"run: ls -la",
			},
			notInOutput: []string{
				"steps:", // Should be removed
			},
		},
		{
			name: "custom steps with empty lines",
			customSteps: `steps:
  - name: Step One
    run: command1

  - name: Step Two
    run: command2`,
			expectInOutput: []string{
				"- name: Step One",
				"- name: Step Two",
			},
		},
		{
			name:           "empty custom steps",
			customSteps:    "",
			expectInOutput: []string{},
		},
		{
			name: "single line after steps",
			customSteps: `steps:
  - run: echo "hello"`,
			expectInOutput: []string{
				`- run: echo "hello"`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()
			var yaml strings.Builder

			compiler.addCustomStepsAsIs(&yaml, tt.customSteps)
			result := yaml.String()

			for _, expected := range tt.expectInOutput {
				assert.Contains(t, result, expected, "output should contain %q", expected)
			}

			for _, notExpected := range tt.notInOutput {
				assert.NotContains(t, result, notExpected, "output should not contain %q", notExpected)
			}
		})
	}
}

// TestAddCustomStepsWithRuntimeInsertion tests adding custom steps with runtime insertion
func TestAddCustomStepsWithRuntimeInsertion(t *testing.T) {
	tests := []struct {
		name              string
		customSteps       string
		runtimeSetupSteps []GitHubActionStep
		tools             *ToolsConfig
		expectInOutput    []string
		expectStepOrder   []string
		notInOutput       []string
		insertionHappened bool
	}{
		{
			name: "insert runtime after checkout",
			customSteps: `steps:
  - name: Checkout code
    uses: actions/checkout@v4
    with:
      token: ${{ secrets.GITHUB_TOKEN }}
  - name: Build
    run: make build`,
			runtimeSetupSteps: []GitHubActionStep{
				{"      - name: Setup Node.js", "        uses: actions/setup-node@v4", "        with:", "          node-version: '20'"},
			},
			tools: &ToolsConfig{},
			expectInOutput: []string{
				"- name: Checkout code",
				"uses: actions/checkout@v4",
				"- name: Setup Node.js",
				"uses: actions/setup-node@v4",
				"node-version: '20'",
				"- name: Build",
				"run: make build",
			},
			expectStepOrder: []string{
				"Checkout code",
				"Setup Node.js",
				"Build",
			},
			insertionHappened: true,
		},
		{
			name: "no insertion when checkout not found",
			customSteps: `steps:
  - name: Build
    run: make build
  - name: Test
    run: make test`,
			runtimeSetupSteps: []GitHubActionStep{
				{"      - name: Setup Node.js", "        uses: actions/setup-node@v4"},
			},
			tools: &ToolsConfig{},
			expectInOutput: []string{
				"- name: Build",
				"- name: Test",
			},
			notInOutput: []string{
				"Setup Node.js", // Should not be inserted since no checkout
			},
			insertionHappened: false,
		},
		{
			name: "checkout with uses on same line - not detected as checkout",
			customSteps: `steps:
  - uses: actions/checkout@v4
  - name: Build
    run: make build`,
			runtimeSetupSteps: []GitHubActionStep{
				{"      - name: Setup Go", "        uses: actions/setup-go@v5"},
			},
			tools: &ToolsConfig{},
			expectInOutput: []string{
				"- uses: actions/checkout@v4",
				"- name: Build",
			},
			notInOutput: []string{
				"Setup Go", // Won't be inserted - function doesn't detect "- uses:" as checkout
			},
			insertionHappened: false,
		},
		{
			name: "multiple runtime steps inserted",
			customSteps: `steps:
  - name: Checkout
    uses: actions/checkout@v4
  - name: Deploy
    run: deploy.sh`,
			runtimeSetupSteps: []GitHubActionStep{
				{"      - name: Setup Node.js", "        uses: actions/setup-node@v4"},
				{"      - name: Setup Python", "        uses: actions/setup-python@v5"},
			},
			tools: &ToolsConfig{},
			expectInOutput: []string{
				"- name: Checkout",
				"- name: Setup Node.js",
				"- name: Setup Python",
				"- name: Deploy",
			},
			expectStepOrder: []string{
				"Checkout",
				"Setup Node.js",
				"Setup Python",
				"Deploy",
			},
			insertionHappened: true,
		},
		{
			name: "empty runtime steps",
			customSteps: `steps:
  - name: Checkout
    uses: actions/checkout@v4
  - name: Build
    run: make build`,
			runtimeSetupSteps: []GitHubActionStep{},
			tools:             &ToolsConfig{},
			expectInOutput: []string{
				"- name: Checkout",
				"- name: Build",
			},
			insertionHappened: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()
			var yaml strings.Builder

			compiler.addCustomStepsWithRuntimeInsertion(&yaml, tt.customSteps, tt.runtimeSetupSteps, tt.tools)
			result := yaml.String()

			for _, expected := range tt.expectInOutput {
				assert.Contains(t, result, expected, "output should contain %q", expected)
			}

			for _, notExpected := range tt.notInOutput {
				assert.NotContains(t, result, notExpected, "output should not contain %q", notExpected)
			}

			// Verify step ordering if specified
			if len(tt.expectStepOrder) > 0 {
				lastPos := -1
				for _, step := range tt.expectStepOrder {
					pos := strings.Index(result, step)
					if tt.insertionHappened {
						assert.NotEqual(t, -1, pos, "step %q should be present", step)
						if lastPos != -1 {
							assert.Greater(t, pos, lastPos, "step %q should appear after previous step", step)
						}
						lastPos = pos
					}
				}
			}
		})
	}
}

// TestGenerateMainJobSteps tests the main job steps generation orchestration
func TestGenerateMainJobSteps(t *testing.T) {
	tests := []struct {
		name           string
		setupData      func() *WorkflowData
		setupCompiler  func(*Compiler)
		expectInOutput []string
		shouldError    bool
		errorContains  string
	}{
		{
			name: "basic workflow with copilot engine",
			setupData: func() *WorkflowData {
				return &WorkflowData{
					Name:            "Test Workflow",
					AI:              "copilot",
					MarkdownContent: "Test prompt",
					EngineConfig: &EngineConfig{
						ID: "copilot",
					},
					ParsedTools: &ToolsConfig{},
				}
			},
			setupCompiler: func(c *Compiler) {
				// Basic setup
			},
			expectInOutput: []string{
				"- name: Checkout repository",
				"persist-credentials: false",
				"- name: Create gh-aw temp directory",
				"run: bash /opt/gh-aw/actions/create_gh_aw_tmp_dir.sh",
			},
			shouldError: false,
		},
		{
			name: "workflow with repository imports",
			setupData: func() *WorkflowData {
				return &WorkflowData{
					Name:              "Test Workflow",
					AI:                "copilot",
					MarkdownContent:   "Test prompt",
					RepositoryImports: []string{"github/example@main"},
					EngineConfig: &EngineConfig{
						ID: "copilot",
					},
					ParsedTools: &ToolsConfig{},
				}
			},
			expectInOutput: []string{
				"- name: Checkout repository",
				"- name: Checkout repository import github/example@main",
				"repository: github/example",
				"- name: Merge remote .github folder",
				"GH_AW_REPOSITORY_IMPORTS:",
			},
			shouldError: false,
		},
		{
			name: "workflow with custom steps",
			setupData: func() *WorkflowData {
				return &WorkflowData{
					Name:            "Test Workflow",
					AI:              "copilot",
					MarkdownContent: "Test prompt",
					CustomSteps: `steps:
  - name: Custom Build
    run: make build`,
					EngineConfig: &EngineConfig{
						ID: "copilot",
					},
					ParsedTools: &ToolsConfig{},
				}
			},
			expectInOutput: []string{
				"- name: Checkout repository",
				"- name: Custom Build",
				"run: make build",
			},
			shouldError: false,
		},
		{
			name: "workflow with safe outputs",
			setupData: func() *WorkflowData {
				return &WorkflowData{
					Name:            "Test Workflow",
					AI:              "copilot",
					MarkdownContent: "Test prompt",
					SafeOutputs: &SafeOutputsConfig{
						CreatePullRequests: &CreatePullRequestsConfig{},
					},
					EngineConfig: &EngineConfig{
						ID: "copilot",
					},
					ParsedTools: &ToolsConfig{},
				}
			},
			expectInOutput: []string{
				"- name: Checkout repository",
				"/tmp/gh-aw/aw.patch", // Git patch path should be collected
			},
			shouldError: false,
		},
		{
			name: "workflow with legacy agent import",
			setupData: func() *WorkflowData {
				return &WorkflowData{
					Name:            "Test Workflow",
					AI:              "copilot",
					MarkdownContent: "Test prompt",
					AgentFile:       "test-agent.md",
					AgentImportSpec: "github/example@main", // Parser only handles owner/repo format
					EngineConfig: &EngineConfig{
						ID: "copilot",
					},
					ParsedTools: &ToolsConfig{},
				}
			},
			expectInOutput: []string{
				"- name: Checkout agent import github/example@main",
				"path: /tmp/gh-aw/repo-imports/github-example-main",
				"- name: Merge remote .github folder",
				"GH_AW_AGENT_FILE:",
				"GH_AW_AGENT_IMPORT_SPEC:",
			},
			shouldError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()
			compiler.stepOrderTracker = NewStepOrderTracker()

			if tt.setupCompiler != nil {
				tt.setupCompiler(compiler)
			}

			data := tt.setupData()
			var yaml strings.Builder

			err := compiler.generateMainJobSteps(&yaml, data)

			if tt.shouldError {
				require.Error(t, err, "expected error but got none")
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains, "error message mismatch")
				}
			} else {
				require.NoError(t, err, "unexpected error: %v", err)
				result := yaml.String()

				for _, expected := range tt.expectInOutput {
					assert.Contains(t, result, expected, "output should contain %q", expected)
				}
			}
		})
	}
}

// TestGenerateMainJobStepsWithDevMode tests dev mode CLI build steps integration
func TestGenerateMainJobStepsWithDevMode(t *testing.T) {
	compiler := NewCompiler()
	compiler.actionMode = ActionModeDev
	compiler.stepOrderTracker = NewStepOrderTracker()

	data := &WorkflowData{
		Name:            "Test Workflow",
		AI:              "copilot",
		MarkdownContent: "Test prompt",
		Tools: map[string]any{
			"agentic-workflows": map[string]any{},
		},
		EngineConfig: &EngineConfig{
			ID: "copilot",
		},
		ParsedTools: &ToolsConfig{},
	}

	var yaml strings.Builder
	err := compiler.generateMainJobSteps(&yaml, data)

	require.NoError(t, err, "generateMainJobSteps should not error in dev mode")
	result := yaml.String()

	// Verify dev mode build steps are included
	expectedDevModeSteps := []string{
		"- name: Setup Go for CLI build",
		"- name: Build gh-aw CLI",
		"- name: Setup Docker Buildx",
		"- name: Build gh-aw Docker image",
		"tags: localhost/gh-aw:dev",
	}

	for _, expected := range expectedDevModeSteps {
		assert.Contains(t, result, expected, "dev mode should include %q", expected)
	}
}

// TestGenerateMainJobStepsStepOrdering tests step ordering validation
func TestGenerateMainJobStepsStepOrdering(t *testing.T) {
	compiler := NewCompiler()
	compiler.stepOrderTracker = NewStepOrderTracker()

	data := &WorkflowData{
		Name:            "Test Workflow",
		AI:              "copilot",
		MarkdownContent: "Test prompt",
		EngineConfig: &EngineConfig{
			ID: "copilot",
		},
		ParsedTools: &ToolsConfig{},
	}

	var yaml strings.Builder
	err := compiler.generateMainJobSteps(&yaml, data)

	// Should not error - step ordering should be valid
	require.NoError(t, err, "step ordering validation should pass")
}

// TestShouldAddCheckoutStepEdgeCases tests edge cases for checkout step detection
func TestShouldAddCheckoutStepEdgeCases(t *testing.T) {
	tests := []struct {
		name           string
		setupData      func() *WorkflowData
		setupCompiler  func(*Compiler)
		expectCheckout bool
	}{
		{
			name: "checkout needed in dev mode (default)",
			setupData: func() *WorkflowData {
				return &WorkflowData{
					Name: "Test",
				}
			},
			expectCheckout: true,
		},
		{
			name: "no checkout in release mode without agent file",
			setupData: func() *WorkflowData {
				return &WorkflowData{
					Name: "Test",
				}
			},
			setupCompiler: func(c *Compiler) {
				c.actionMode = ActionModeRelease
			},
			expectCheckout: false,
		},
		{
			name: "checkout required when agent file specified",
			setupData: func() *WorkflowData {
				return &WorkflowData{
					Name:      "Test",
					AgentFile: "custom-agent.md",
				}
			},
			setupCompiler: func(c *Compiler) {
				c.actionMode = ActionModeRelease
			},
			expectCheckout: true,
		},
		{
			name: "no checkout when custom steps contain checkout",
			setupData: func() *WorkflowData {
				return &WorkflowData{
					Name: "Test",
					CustomSteps: `steps:
  - name: Checkout
    uses: actions/checkout@v4`,
				}
			},
			expectCheckout: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()
			if tt.setupCompiler != nil {
				tt.setupCompiler(compiler)
			}

			data := tt.setupData()
			result := compiler.shouldAddCheckoutStep(data)

			assert.Equal(t, tt.expectCheckout, result, "checkout detection mismatch")
		})
	}
}
