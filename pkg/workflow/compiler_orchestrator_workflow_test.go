//go:build !integration

package workflow

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/testutil"
	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProtectedSessionCaptureRejectsEvals(t *testing.T) {
	definition := &EngineDefinition{
		ID: "protected",
		Behaviors: &EngineBehaviorDefinition{
			Execution: &EngineExecutionDefinition{ProtectedSessionCapture: true},
			LogParser: `function parseLog() { return { markdown: "", logEntries: [] }; }`,
		},
	}
	engine, err := NewBehaviorDefinedEngine(definition)
	require.NoError(t, err)

	evals := &EvalsConfig{Questions: []EvalDefinition{{ID: "review", Question: "Does the answer match?"}}}
	ctx := &workflowBuildContext{
		cleanPath:    "protected.md",
		engineSetup:  &engineSetupResult{agenticEngine: engine},
		workflowData: &WorkflowData{Evals: evals},
	}
	err = NewCompiler().validateWorkflowBuildContext(ctx)
	require.ErrorContains(t, err, "secondary engine invocation that reads evals.log")
	assert.Contains(t, err.Error(), "protected-session-capture: false")

	require.NoError(t, validateProtectedSessionCaptureEvals(engine, nil))
	definition.Behaviors.Execution.ProtectedSessionCapture = false
	unprotectedEngine, err := NewBehaviorDefinedEngine(definition)
	require.NoError(t, err)
	require.NoError(t, validateProtectedSessionCaptureEvals(unprotectedEngine, evals))
}

// TestBuildInitialWorkflowData_BasicFields tests that buildInitialWorkflowData correctly populates basic fields
func TestBuildInitialWorkflowData_BasicFields(t *testing.T) {
	compiler := NewCompiler()

	// Mock frontmatter result
	frontmatterResult := &parser.FrontmatterResult{
		Frontmatter:      map[string]any{"description": "Test workflow", "metadata": map[string]any{"docs": "https://docs.example.com/test-workflow"}, "source": "test-source"},
		FrontmatterLines: []string{"description: Test workflow", "metadata:", "  docs: https://docs.example.com/test-workflow", "source: test-source"},
		Markdown:         "# Test\n\nContent",
	}

	// Mock tools processing result
	toolsResult := &toolsProcessingResult{
		workflowName:         "Test Workflow",
		frontmatterName:      "Test Frontmatter Name",
		trackerID:            "TRACKER-123",
		importedMarkdown:     "Imported content",
		importPaths:          []string{"/path/to/import"},
		mainWorkflowMarkdown: "Main markdown",
		allIncludedFiles:     []string{"/file1", "/file2"},
		markdownContent:      "Full markdown content",
		tools:                map[string]any{"bash": []string{"echo"}},
		runtimes:             map[string]any{"node": "18"},
		toolsTimeout:         "300",
		toolsStartupTimeout:  "60",
		needsTextOutput:      true,
		safeOutputs:          &SafeOutputsConfig{},
		secretMasking:        &SecretMaskingConfig{},
		parsedFrontmatter:    &FrontmatterConfig{},
	}

	// Mock engine setup result
	engineSetup := &engineSetupResult{
		engineSetting:      "copilot",
		engineConfig:       &EngineConfig{ID: "copilot"},
		networkPermissions: &NetworkPermissions{Allowed: []string{"defaults"}},
		sandboxConfig:      &SandboxConfig{},
		importsResult: &parser.ImportsResult{
			ImportedFiles:   []string{"/imported/file"},
			ImportInputs:    map[string]any{"test": map[string]any{"key": "value"}},
			AgentFile:       "agent.md",
			AgentImportSpec: "agent.md",
		},
	}

	// Call buildInitialWorkflowData
	workflowData, err := compiler.buildInitialWorkflowData(frontmatterResult, toolsResult, engineSetup, engineSetup.importsResult)
	require.NoError(t, err)

	// Verify all fields are populated correctly
	assert.Equal(t, "Test Workflow", workflowData.Name)
	assert.Equal(t, "Test Frontmatter Name", workflowData.FrontmatterName)
	assert.Equal(t, "Test workflow", workflowData.Description)
	assert.Equal(t, "https://docs.example.com/test-workflow", workflowData.Docs)
	assert.Equal(t, "test-source", workflowData.Source)
	assert.Equal(t, "TRACKER-123", workflowData.TrackerID)
	assert.Equal(t, []string{"/imported/file"}, workflowData.ImportedFiles)
	assert.Equal(t, "Imported content", workflowData.ImportedMarkdown)
	assert.Equal(t, []string{"/path/to/import"}, workflowData.ImportPaths)
	assert.Equal(t, "Main markdown", workflowData.MainWorkflowMarkdown)
	assert.Equal(t, []string{"/file1", "/file2"}, workflowData.IncludedFiles)
	assert.Equal(t, "Full markdown content", workflowData.MarkdownContent)
	assert.Equal(t, "copilot", workflowData.AI)
	assert.NotNil(t, workflowData.EngineConfig)
	assert.NotNil(t, workflowData.ParsedTools)
	assert.NotNil(t, workflowData.NetworkPermissions)
	assert.NotNil(t, workflowData.SandboxConfig)
	assert.Equal(t, "300", workflowData.ToolsTimeout)
	assert.Equal(t, "60", workflowData.ToolsStartupTimeout)
	assert.True(t, workflowData.NeedsTextOutput)
	assert.Equal(t, "agent.md", workflowData.AgentFile)
}

// TestBuildInitialWorkflowData_EmptyFields tests buildInitialWorkflowData with minimal/empty fields
func TestBuildInitialWorkflowData_EmptyFields(t *testing.T) {
	compiler := NewCompiler()

	frontmatterResult := &parser.FrontmatterResult{
		Frontmatter:      map[string]any{},
		FrontmatterLines: []string{},
	}

	toolsResult := &toolsProcessingResult{
		tools:             map[string]any{},
		runtimes:          map[string]any{},
		parsedFrontmatter: &FrontmatterConfig{},
	}

	engineSetup := &engineSetupResult{
		engineSetting:      "copilot",
		engineConfig:       &EngineConfig{},
		networkPermissions: &NetworkPermissions{},
		importsResult:      &parser.ImportsResult{},
	}

	workflowData, err := compiler.buildInitialWorkflowData(frontmatterResult, toolsResult, engineSetup, engineSetup.importsResult)
	require.NoError(t, err)

	// Should not panic and should create valid structure
	assert.NotNil(t, workflowData)
	assert.Empty(t, workflowData.Name)
	assert.Empty(t, workflowData.Description)
	assert.Empty(t, workflowData.ImportedFiles)
}

// TestExtractYAMLSections_AllSections tests extraction of all YAML sections
func TestExtractYAMLSections_AllSections(t *testing.T) {
	compiler := NewCompiler()
	workflowData := &WorkflowData{}

	frontmatter := map[string]any{
		"on": map[string]any{
			"push": map[string]any{
				"branches": []string{"main"},
			},
		},
		"permissions": map[string]any{
			"contents": "read",
			"issues":   "write",
		},
		"network": map[string]any{
			"allowed": []string{"github.com"},
		},
		"concurrency": map[string]any{
			"group":              "ci-${{ github.ref }}",
			"cancel-in-progress": true,
		},
		"run-name":        "Test Run ${{ github.run_id }}",
		"env":             map[string]any{"NODE_ENV": "production"},
		"features":        map[string]any{"mcp-scripts": true},
		"if":              "github.event_name == 'push'",
		"timeout-minutes": 30,
		"runs-on":         "ubuntu-latest",
		"environment":     "production",
		"container":       "node:18",
		"cache": []any{
			map[string]any{
				"key":  "${{ runner.os }}-node",
				"path": "node_modules",
			},
		},
	}

	compiler.extractYAMLSections(frontmatter, workflowData)

	// Verify all sections were extracted
	assert.NotEmpty(t, workflowData.On)
	assert.Contains(t, workflowData.On, "push")
	assert.NotEmpty(t, workflowData.Permissions)
	assert.Contains(t, workflowData.Permissions, "contents")
	assert.NotEmpty(t, workflowData.Network)
	assert.Contains(t, workflowData.Network, "github.com")
	assert.NotEmpty(t, workflowData.Concurrency)
	assert.Contains(t, workflowData.Concurrency, "group")
	assert.NotEmpty(t, workflowData.RunName)
	assert.Contains(t, workflowData.RunName, "Test Run")
	assert.NotEmpty(t, workflowData.Env)
	assert.Contains(t, workflowData.Env, "NODE_ENV")
	assert.NotEmpty(t, workflowData.Features)
	assert.Contains(t, workflowData.Features, "mcp-scripts")
	assert.NotEmpty(t, workflowData.If)
	assert.Contains(t, workflowData.If, "github.event_name")
	assert.NotEmpty(t, workflowData.TimeoutMinutes)
	assert.Contains(t, workflowData.TimeoutMinutes, "30")
	assert.NotEmpty(t, workflowData.RunsOn)
	assert.Contains(t, workflowData.RunsOn, "ubuntu-latest")
	assert.NotEmpty(t, workflowData.Environment)
	assert.Contains(t, workflowData.Environment, "production")
	assert.NotEmpty(t, workflowData.Container)
	assert.Contains(t, workflowData.Container, "node:18")
	assert.NotEmpty(t, workflowData.Cache)
	assert.Contains(t, workflowData.Cache, "runner.os")
}

// TestExtractYAMLSections_MissingSections tests extraction when sections are missing
func TestExtractYAMLSections_MissingSections(t *testing.T) {
	compiler := NewCompiler()
	workflowData := &WorkflowData{}

	// Empty frontmatter
	frontmatter := map[string]any{}

	compiler.extractYAMLSections(frontmatter, workflowData)

	// All fields should be empty strings when not present
	assert.Empty(t, workflowData.On)
	assert.Empty(t, workflowData.Permissions)
	assert.Empty(t, workflowData.Network)
	assert.Empty(t, workflowData.Concurrency)
	assert.Empty(t, workflowData.RunName)
	assert.Empty(t, workflowData.Env)
	assert.Empty(t, workflowData.Features)
	assert.Empty(t, workflowData.If)
	assert.Empty(t, workflowData.TimeoutMinutes)
	assert.Empty(t, workflowData.RunsOn)
	assert.Empty(t, workflowData.Environment)
	assert.Empty(t, workflowData.Container)
	assert.Empty(t, workflowData.Cache)
}

func TestExtractYAMLSections_EmptyRunsOnSlimTreatedAsUnset(t *testing.T) {
	compiler := NewCompiler()

	tests := []struct {
		name  string
		value any
	}{
		{
			name:  "empty string",
			value: "",
		},
		{
			name:  "empty array",
			value: []any{},
		},
		{
			name:  "empty object",
			value: map[string]any{},
		},
		{
			name:  "object with empty group and labels",
			value: map[string]any{"group": "", "labels": []any{}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workflowData := &WorkflowData{}
			frontmatter := map[string]any{
				"runs-on-slim": tt.value,
			}

			compiler.extractYAMLSections(frontmatter, workflowData)

			assert.Empty(t, workflowData.RunsOnSlim)
		})
	}
}

// TestExtractYAMLSections_NonEmptyRunsOnSlimRenderedSnippet pins the rendered
// snippet format that extractYAMLSections stores in WorkflowData.RunsOnSlim for
// non-empty values. Downstream helpers (indentYAMLLines, formatRunsOnSnippetForInlineValue)
// depend on these exact forms, so changes to the rendering path are intentional.
func TestExtractYAMLSections_NonEmptyRunsOnSlimRenderedSnippet(t *testing.T) {
	compiler := NewCompiler()

	tests := []struct {
		name            string
		value           any
		expectedSnippet string
	}{
		{
			name:            "string value produces plain string snippet",
			value:           "self-hosted",
			expectedSnippet: "runs-on: self-hosted",
		},
		{
			// Array branch uses standard yaml.Marshal (no DefaultMarshalOptions),
			// which produces zero-indented list items.
			name:            "array value produces zero-indented list snippet",
			value:           []any{"self-hosted", "ubuntu2404"},
			expectedSnippet: "runs-on:\n- self-hosted\n- ubuntu2404",
		},
		{
			// Object branch uses DefaultMarshalOptions (yaml.Indent(2)), so
			// map continuation lines carry exactly 2-space indentation.
			// formatRunsOnSnippetForInlineValue's TrimPrefix("  ") and
			// indentYAMLLines rely on this exact form.
			name: "group+labels object produces 2-space-indented map snippet",
			value: map[string]any{
				"group":  "runner-group",
				"labels": []any{"ubuntu2404", "x64"},
			},
			expectedSnippet: "runs-on:\n  group: runner-group\n  labels:\n  - ubuntu2404\n  - x64",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workflowData := &WorkflowData{}
			frontmatter := map[string]any{
				"runs-on-slim": tt.value,
			}

			compiler.extractYAMLSections(frontmatter, workflowData)

			assert.Equal(t, tt.expectedSnippet, workflowData.RunsOnSlim)
		})
	}
}

func TestValidateWorkflowEngineSettings_PreservesLegacyErrorOrder(t *testing.T) {
	compiler := NewCompiler()
	compiler.strictMode = true

	workflowData := &WorkflowData{
		RunInstallScripts: true,
		EngineConfig: &EngineConfig{
			HarnessScript: "invalid/path.js",
		},
	}

	err := compiler.validateWorkflowEngineSettings("workflow.md", workflowData)
	require.Error(t, err)
	require.ErrorContains(t, err, "workflow.md: strict mode: run-install-scripts: true is set")
	assert.NotContains(t, err.Error(), "engine.harness")
}

func TestValidateWorkflowEngineSettings_LSPRequiresCopilot(t *testing.T) {
	compiler := NewCompiler()
	workflowData := &WorkflowData{
		AI: "codex",
		LSP: map[string]LSPServerConfig{
			"go": {
				Command: "gopls",
				Args:    []string{"serve"},
				FileExtensions: map[string]string{
					".go": "go",
				},
			},
		},
	}

	err := compiler.validateWorkflowEngineSettings("workflow.md", workflowData)
	require.Error(t, err)
	require.ErrorContains(t, err, "workflow.md: lsp is currently only supported for engine: copilot")
}

func TestMergeRawOTLPEndpoints_DedupesAndCountsSources(t *testing.T) {
	mainObs := map[string]any{
		"otlp": map[string]any{
			"endpoint": []any{
				map[string]any{"url": "https://main.example/otlp"},
				map[string]any{"url": "https://main.example/otlp"},
				map[string]any{"url": "https://shared.example/otlp"},
			},
		},
	}
	importedObs := map[string]any{
		"otlp": map[string]any{
			"endpoint": []any{
				map[string]any{"url": "https://shared.example/otlp"},
				map[string]any{"url": "https://import.example/otlp"},
				map[string]any{"url": "https://import.example/otlp"},
			},
		},
	}

	mergedEndpoints, mainCount, importAdded := mergeRawOTLPEndpoints(mainObs, importedObs)

	require.Len(t, mergedEndpoints, 3)
	assert.Equal(t, 2, mainCount)
	assert.Equal(t, 1, importAdded)
	assert.Equal(t, "https://main.example/otlp", mergedEndpoints[0].(map[string]any)["url"])
	assert.Equal(t, "https://shared.example/otlp", mergedEndpoints[1].(map[string]any)["url"])
	assert.Equal(t, "https://import.example/otlp", mergedEndpoints[2].(map[string]any)["url"])
}

func TestMergeImportedObservability_MergesResourceAttributesWithMainPrecedence(t *testing.T) {
	importedObsJSON, err := json.Marshal(map[string]any{
		"otlp": map[string]any{
			"resource-attributes": map[string]any{
				"shared.key":      "from-import",
				"import.only.key": "import-value",
			},
		},
	})
	require.NoError(t, err)

	workflowData := &WorkflowData{
		RawFrontmatter: map[string]any{
			"observability": map[string]any{
				"otlp": map[string]any{
					"resource-attributes": map[string]any{
						"shared.key":    "from-main",
						"main.only.key": "main-value",
					},
				},
			},
		},
	}

	NewCompiler().mergeImportedObservability(workflowData, string(importedObsJSON))

	obs := workflowData.RawFrontmatter["observability"].(map[string]any)
	otlp := obs["otlp"].(map[string]any)
	assert.Equal(t, map[string]string{
		"shared.key":      "from-main",
		"main.only.key":   "main-value",
		"import.only.key": "import-value",
	}, otlp["resource-attributes"])
}

func TestMergeImportedObservabilityResolvesRootMetadataReferences(t *testing.T) {
	importedObsJSON, err := json.Marshal(map[string]any{
		"metadata": map[string]any{"version": "import-version"},
		"otlp": map[string]any{
			"attributes": map[string]any{
				"catalogue.version": "${{ github.aw.metadata.version }}",
			},
			"resource-attributes": map[string]any{
				"catalogue.category": "${{ github.aw.metadata.category }}",
			},
		},
	})
	require.NoError(t, err)

	workflowData := &WorkflowData{
		RawFrontmatter: map[string]any{
			"metadata": map[string]any{
				"version":  "root-version",
				"category": "automation",
			},
		},
	}

	ctx := &workflowBuildContext{
		cleanPath:   "workflow.md",
		frontmatter: &parser.FrontmatterResult{Frontmatter: workflowData.RawFrontmatter},
		engineSetup: &engineSetupResult{
			importsResult: &parser.ImportsResult{MergedObservability: string(importedObsJSON)},
		},
		workflowData: workflowData,
	}
	require.NoError(t, NewCompiler().mergeImportedWorkflowConfiguration(ctx))

	obs := workflowData.RawFrontmatter["observability"].(map[string]any)
	otlp := obs["otlp"].(map[string]any)
	assert.Equal(t, map[string]any{"catalogue.version": "root-version"}, otlp["attributes"])
	assert.Equal(t, map[string]any{"catalogue.category": "automation"}, otlp["resource-attributes"])
	assert.Equal(t, map[string]string{"catalogue.version": "root-version"}, collectOTLPCustomAttributes(workflowData.RawFrontmatter))
	assert.Equal(t, map[string]string{"catalogue.category": "automation"}, collectOTLPResourceAttributes(workflowData.RawFrontmatter))
	assert.Contains(t, workflowData.Env, `"catalogue.version":"root-version"`)
	assert.Contains(t, workflowData.Env, "catalogue.category=automation")
}

func TestBuildMergedEnvSources_MainWorkflowWins(t *testing.T) {
	mergedEnv := map[string]any{
		"MAIN_ONLY":   "1",
		"IMPORT_ONLY": "2",
		"BOTH":        "3",
	}
	topEnv := map[string]any{
		"MAIN_ONLY": "1",
		"BOTH":      "3",
	}
	importedSources := map[string]string{
		"IMPORT_ONLY": "imports/shared.md",
		"BOTH":        "imports/overridden.md",
	}

	envSources := buildMergedEnvSources(mergedEnv, topEnv, importedSources)

	assert.Equal(t, map[string]string{
		"MAIN_ONLY":   "(main workflow)",
		"IMPORT_ONLY": "imports/shared.md",
		"BOTH":        "(main workflow)",
	}, envSources)
}

func TestSetMainWorkflowEnvSources_OnlyTracksPresentKeys(t *testing.T) {
	workflowData := &WorkflowData{}

	setMainWorkflowEnvSources(workflowData, map[string]any{
		"FOO": "1",
		"BAR": "2",
	})

	assert.Equal(t, map[string]string{
		"FOO": "(main workflow)",
		"BAR": "(main workflow)",
	}, workflowData.EnvSources)
}

func TestMergeWorkflowEnv_InlinesImportedEnvReferences(t *testing.T) {
	compiler := NewCompiler()
	workflowData := &WorkflowData{}
	importsResult := &parser.ImportsResult{
		MergedEnv: `{"REVIEW_OUTPUT_REPO":"${{ github.event.inputs.safe_output_repo || vars.CENTRAL_AGENTIC_OPS_REVIEW_REPO || '' }}","SAFE_OUTPUT_REPO":"${{ (github.event.inputs.safe_output_mode || vars.CENTRAL_AGENTIC_OPS_MODE || 'preview') == 'review' && env.REVIEW_OUTPUT_REPO || '' }}"}`,
		MergedEnvSources: map[string]string{
			"REVIEW_OUTPUT_REPO": "shared/control.md",
			"SAFE_OUTPUT_REPO":   "shared/control.md",
		},
	}

	err := compiler.mergeWorkflowEnv(map[string]any{}, workflowData, importsResult)
	require.NoError(t, err)
	assert.Contains(t, workflowData.Env, "SAFE_OUTPUT_REPO: ${{ (github.event.inputs.safe_output_mode || vars.CENTRAL_AGENTIC_OPS_MODE || 'preview') == 'review' && (github.event.inputs.safe_output_repo || vars.CENTRAL_AGENTIC_OPS_REVIEW_REPO || '') || '' }}")
	assert.NotContains(t, workflowData.Env, "env.REVIEW_OUTPUT_REPO")
	assert.Equal(t, "shared/control.md", workflowData.EnvSources["SAFE_OUTPUT_REPO"])
}

// TestProcessAndMergeSteps_NoSteps tests processAndMergeSteps with no steps
func TestProcessAndMergeSteps_NoSteps(t *testing.T) {
	compiler := NewCompiler()
	workflowData := &WorkflowData{}
	frontmatter := map[string]any{}
	importsResult := &parser.ImportsResult{}

	require.NoError(t, compiler.processAndMergeSteps(frontmatter, workflowData, importsResult))

	// CustomSteps should be empty when no steps are defined
	assert.Empty(t, workflowData.CustomSteps)
}

// TestProcessAndMergeSteps_MainStepsOnly tests processAndMergeSteps with only main workflow steps
func TestProcessAndMergeSteps_MainStepsOnly(t *testing.T) {
	tmpDir := testutil.TempDir(t, "steps-main-only")
	compiler := NewCompiler()
	actionCache := NewActionCache(tmpDir)
	actionResolver := NewActionResolver(actionCache)
	workflowData := &WorkflowData{
		ActionCache:    actionCache,
		ActionResolver: actionResolver,
	}

	frontmatter := map[string]any{
		"steps": []any{
			map[string]any{
				"name": "Test step",
				"run":  "echo 'test'",
			},
		},
	}
	importsResult := &parser.ImportsResult{}

	require.NoError(t, compiler.processAndMergeSteps(frontmatter, workflowData, importsResult))

	// CustomSteps should contain the main workflow steps
	assert.NotEmpty(t, workflowData.CustomSteps)
	assert.Contains(t, workflowData.CustomSteps, "Test step")
	assert.Contains(t, workflowData.CustomSteps, "echo 'test'")
}

// TestProcessAndMergeSteps_WithImportedSteps tests step merging with imported steps
func TestProcessAndMergeSteps_WithImportedSteps(t *testing.T) {
	tmpDir := testutil.TempDir(t, "steps-with-imports")
	compiler := NewCompiler()
	actionCache := NewActionCache(tmpDir)
	actionResolver := NewActionResolver(actionCache)
	workflowData := &WorkflowData{
		ActionCache:    actionCache,
		ActionResolver: actionResolver,
	}

	frontmatter := map[string]any{
		"steps": []any{
			map[string]any{
				"name": "Main step",
				"run":  "echo 'main'",
			},
		},
	}

	// Imported steps in YAML format (without 'steps:' wrapper)
	importedSteps := []any{
		map[string]any{
			"name": "Imported step",
			"run":  "echo 'imported'",
		},
	}
	importedStepsYAML, _ := yaml.Marshal(importedSteps)

	importsResult := &parser.ImportsResult{
		MergedSteps: string(importedStepsYAML),
	}

	require.NoError(t, compiler.processAndMergeSteps(frontmatter, workflowData, importsResult))

	// CustomSteps should contain both imported and main steps
	assert.NotEmpty(t, workflowData.CustomSteps)
	assert.Contains(t, workflowData.CustomSteps, "Imported step")
	assert.Contains(t, workflowData.CustomSteps, "Main step")

	// Imported step should come before main step
	importedIndex := strings.Index(workflowData.CustomSteps, "Imported step")
	mainIndex := strings.Index(workflowData.CustomSteps, "Main step")
	assert.Less(t, importedIndex, mainIndex, "Imported steps should come before main steps")
}

// TestProcessAndMergeSteps_WithCopilotSetupSteps tests step merging with copilot-setup steps
func TestProcessAndMergeSteps_WithCopilotSetupSteps(t *testing.T) {
	tmpDir := testutil.TempDir(t, "steps-copilot-setup")
	compiler := NewCompiler()
	actionCache := NewActionCache(tmpDir)
	actionResolver := NewActionResolver(actionCache)
	workflowData := &WorkflowData{
		ActionCache:    actionCache,
		ActionResolver: actionResolver,
	}

	frontmatter := map[string]any{
		"steps": []any{
			map[string]any{
				"name": "Main step",
				"run":  "echo 'main'",
			},
		},
	}

	copilotSetupSteps := []any{
		map[string]any{
			"name": "Setup Copilot",
			"run":  "echo 'setup'",
		},
	}
	copilotSetupYAML, _ := yaml.Marshal(copilotSetupSteps)

	importsResult := &parser.ImportsResult{
		CopilotSetupSteps: string(copilotSetupYAML),
	}

	require.NoError(t, compiler.processAndMergeSteps(frontmatter, workflowData, importsResult))

	// CustomSteps should contain both copilot-setup and main steps
	assert.NotEmpty(t, workflowData.CustomSteps)
	assert.Contains(t, workflowData.CustomSteps, "Setup Copilot")
	assert.Contains(t, workflowData.CustomSteps, "Main step")

	// Copilot setup should come before main step
	setupIndex := strings.Index(workflowData.CustomSteps, "Setup Copilot")
	mainIndex := strings.Index(workflowData.CustomSteps, "Main step")
	assert.Less(t, setupIndex, mainIndex, "Copilot setup steps should come before main steps")
}

// TestProcessAndMergeSteps_AllStepTypes tests merging of all step types in correct order
func TestProcessAndMergeSteps_AllStepTypes(t *testing.T) {
	tmpDir := testutil.TempDir(t, "steps-all-types")
	compiler := NewCompiler()
	actionCache := NewActionCache(tmpDir)
	actionResolver := NewActionResolver(actionCache)
	workflowData := &WorkflowData{
		ActionCache:    actionCache,
		ActionResolver: actionResolver,
	}

	frontmatter := map[string]any{
		"steps": []any{
			map[string]any{
				"name": "Main step",
				"run":  "echo 'main'",
			},
		},
	}

	copilotSetupSteps := []any{
		map[string]any{"name": "Copilot setup", "run": "echo 'copilot'"},
	}
	copilotSetupYAML, _ := yaml.Marshal(copilotSetupSteps)

	otherSteps := []any{
		map[string]any{"name": "Other imported", "run": "echo 'other'"},
	}
	otherStepsYAML, _ := yaml.Marshal(otherSteps)

	importsResult := &parser.ImportsResult{
		CopilotSetupSteps: string(copilotSetupYAML),
		MergedSteps:       string(otherStepsYAML),
	}

	require.NoError(t, compiler.processAndMergeSteps(frontmatter, workflowData, importsResult))

	// All steps should be present
	assert.Contains(t, workflowData.CustomSteps, "Copilot setup")
	assert.Contains(t, workflowData.CustomSteps, "Other imported")
	assert.Contains(t, workflowData.CustomSteps, "Main step")

	// Verify correct order: copilot-setup → other imported → main
	copilotIndex := strings.Index(workflowData.CustomSteps, "Copilot setup")
	otherIndex := strings.Index(workflowData.CustomSteps, "Other imported")
	mainIndex := strings.Index(workflowData.CustomSteps, "Main step")

	assert.Less(t, copilotIndex, otherIndex, "Copilot setup should come before other imported steps")
	assert.Less(t, otherIndex, mainIndex, "Other imported steps should come before main steps")
}

// TestProcessAndMergePostSteps_NoPostSteps tests processAndMergePostSteps with no post-steps
func TestProcessAndMergePostSteps_NoPostSteps(t *testing.T) {
	compiler := NewCompiler()
	workflowData := &WorkflowData{}
	frontmatter := map[string]any{}
	importsResult := &parser.ImportsResult{}

	require.NoError(t, compiler.processAndMergePostSteps(frontmatter, workflowData, importsResult))

	assert.Empty(t, workflowData.PostSteps)
}

// TestProcessAndMergePostSteps_WithPostSteps tests processAndMergePostSteps with post-steps defined
func TestProcessAndMergePostSteps_WithPostSteps(t *testing.T) {
	tmpDir := testutil.TempDir(t, "post-steps-defined")
	compiler := NewCompiler()
	actionCache := NewActionCache(tmpDir)
	actionResolver := NewActionResolver(actionCache)
	workflowData := &WorkflowData{
		ActionCache:    actionCache,
		ActionResolver: actionResolver,
	}

	frontmatter := map[string]any{
		"post-steps": []any{
			map[string]any{
				"name": "Cleanup",
				"run":  "echo 'cleanup'",
			},
			map[string]any{
				"name": "Upload logs",
				"run":  "echo 'upload'",
			},
		},
	}
	importsResult := &parser.ImportsResult{}

	require.NoError(t, compiler.processAndMergePostSteps(frontmatter, workflowData, importsResult))

	assert.NotEmpty(t, workflowData.PostSteps)
	assert.Contains(t, workflowData.PostSteps, "Cleanup")
	assert.Contains(t, workflowData.PostSteps, "Upload logs")
}

// TestProcessAndMergePostSteps_WithImportedPostSteps tests that imported post-steps are appended
func TestProcessAndMergePostSteps_WithImportedPostSteps(t *testing.T) {
	compiler := NewCompiler()
	workflowData := &WorkflowData{}

	frontmatter := map[string]any{
		"post-steps": []any{
			map[string]any{"name": "Main post step", "run": "echo 'main'"},
		},
	}

	importedPostStepsYAML, err := yaml.Marshal([]any{
		map[string]any{"name": "Imported post step", "run": "echo 'imported'"},
	})
	require.NoError(t, err, "yaml.Marshal should not fail for well-formed post-steps")
	importsResult := &parser.ImportsResult{
		MergedPostSteps: string(importedPostStepsYAML),
	}

	require.NoError(t, compiler.processAndMergePostSteps(frontmatter, workflowData, importsResult))

	assert.Contains(t, workflowData.PostSteps, "Main post step")
	assert.Contains(t, workflowData.PostSteps, "Imported post step")

	// Main workflow's post-steps should come before imported ones
	mainIdx := strings.Index(workflowData.PostSteps, "Main post step")
	importedIdx := strings.Index(workflowData.PostSteps, "Imported post step")
	assert.Less(t, mainIdx, importedIdx, "Main post-steps should come before imported ones")
}

// TestProcessAndMergePreSteps_NoPreSteps tests processAndMergePreSteps with no pre-steps
func TestProcessAndMergePreSteps_NoPreSteps(t *testing.T) {
	compiler := NewCompiler()
	workflowData := &WorkflowData{}
	frontmatter := map[string]any{}
	importsResult := &parser.ImportsResult{}

	require.NoError(t, compiler.processAndMergePreSteps(frontmatter, workflowData, importsResult))

	assert.Empty(t, workflowData.PreSteps)
}

// TestProcessAndMergePreSteps_WithPreSteps tests processAndMergePreSteps with pre-steps defined
func TestProcessAndMergePreSteps_WithPreSteps(t *testing.T) {
	compiler := NewCompiler()
	workflowData := &WorkflowData{}

	frontmatter := map[string]any{
		"pre-steps": []any{
			map[string]any{"name": "Mint token", "run": "echo 'minting'"},
		},
	}
	importsResult := &parser.ImportsResult{}

	require.NoError(t, compiler.processAndMergePreSteps(frontmatter, workflowData, importsResult))

	assert.NotEmpty(t, workflowData.PreSteps)
	assert.Contains(t, workflowData.PreSteps, "Mint token")
}

// TestProcessAndMergePreSteps_WithImportedPreSteps tests that imported pre-steps are prepended
func TestProcessAndMergePreSteps_WithImportedPreSteps(t *testing.T) {
	compiler := NewCompiler()
	workflowData := &WorkflowData{}

	frontmatter := map[string]any{
		"pre-steps": []any{
			map[string]any{"name": "Main pre step", "run": "echo 'main'"},
		},
	}

	importedPreStepsYAML, err := yaml.Marshal([]any{
		map[string]any{"name": "Imported pre step", "run": "echo 'imported'"},
	})
	require.NoError(t, err, "yaml.Marshal should not fail for well-formed pre-steps")
	importsResult := &parser.ImportsResult{
		MergedPreSteps: string(importedPreStepsYAML),
	}

	require.NoError(t, compiler.processAndMergePreSteps(frontmatter, workflowData, importsResult))

	assert.Contains(t, workflowData.PreSteps, "Main pre step")
	assert.Contains(t, workflowData.PreSteps, "Imported pre step")

	// Imported pre-steps should come before the main workflow's pre-steps
	importedIdx := strings.Index(workflowData.PreSteps, "Imported pre step")
	mainIdx := strings.Index(workflowData.PreSteps, "Main pre step")
	assert.Less(t, importedIdx, mainIdx, "Imported pre-steps should come before main pre-steps")
}

// TestProcessAndMergePreAgentSteps_NoPreAgentSteps tests processAndMergePreAgentSteps with no pre-agent-steps
func TestProcessAndMergePreAgentSteps_NoPreAgentSteps(t *testing.T) {
	compiler := NewCompiler()
	workflowData := &WorkflowData{}
	frontmatter := map[string]any{}
	importsResult := &parser.ImportsResult{}

	require.NoError(t, compiler.processAndMergePreAgentSteps(frontmatter, workflowData, importsResult))

	assert.Empty(t, workflowData.PreAgentSteps)
}

// TestProcessAndMergePreAgentSteps_WithPreAgentSteps tests processAndMergePreAgentSteps with pre-agent-steps defined
func TestProcessAndMergePreAgentSteps_WithPreAgentSteps(t *testing.T) {
	compiler := NewCompiler()
	workflowData := &WorkflowData{}

	frontmatter := map[string]any{
		"pre-agent-steps": []any{
			map[string]any{"name": "Prepare final context", "run": "echo 'prepare'"},
		},
	}
	importsResult := &parser.ImportsResult{}

	require.NoError(t, compiler.processAndMergePreAgentSteps(frontmatter, workflowData, importsResult))

	assert.NotEmpty(t, workflowData.PreAgentSteps)
	assert.Contains(t, workflowData.PreAgentSteps, "Prepare final context")
}

// TestProcessAndMergePreAgentSteps_WithImportedPreAgentSteps tests that imported pre-agent-steps are prepended
func TestProcessAndMergePreAgentSteps_WithImportedPreAgentSteps(t *testing.T) {
	compiler := NewCompiler()
	workflowData := &WorkflowData{}

	frontmatter := map[string]any{
		"pre-agent-steps": []any{
			map[string]any{"name": "Main pre-agent step", "run": "echo 'main'"},
		},
	}

	importedPreAgentStepsYAML, err := yaml.Marshal([]any{
		map[string]any{"name": "Imported pre-agent step", "run": "echo 'imported'"},
	})
	require.NoError(t, err, "yaml.Marshal should not fail for well-formed pre-agent-steps")
	importsResult := &parser.ImportsResult{
		MergedPreAgentSteps: string(importedPreAgentStepsYAML),
	}

	require.NoError(t, compiler.processAndMergePreAgentSteps(frontmatter, workflowData, importsResult))

	assert.Contains(t, workflowData.PreAgentSteps, "Main pre-agent step")
	assert.Contains(t, workflowData.PreAgentSteps, "Imported pre-agent step")

	importedIdx := strings.Index(workflowData.PreAgentSteps, "Imported pre-agent step")
	mainIdx := strings.Index(workflowData.PreAgentSteps, "Main pre-agent step")
	assert.Less(t, importedIdx, mainIdx, "Imported pre-agent-steps should come before main pre-agent-steps")
}

// TestProcessAndMergeServices_NoServices tests processAndMergeServices with no services
func TestProcessAndMergeServices_NoServices(t *testing.T) {
	compiler := NewCompiler()
	workflowData := &WorkflowData{}
	frontmatter := map[string]any{}
	importsResult := &parser.ImportsResult{}

	compiler.processAndMergeServices(frontmatter, workflowData, importsResult)

	assert.Empty(t, workflowData.Services)
}

// TestProcessAndMergeServices_MainServicesOnly tests processAndMergeServices with only main workflow services
func TestProcessAndMergeServices_MainServicesOnly(t *testing.T) {
	compiler := NewCompiler()
	workflowData := &WorkflowData{}

	frontmatter := map[string]any{
		"services": map[string]any{
			"postgres": map[string]any{
				"image": "postgres:14",
				"env": map[string]any{
					"POSTGRES_PASSWORD": "postgres",
				},
			},
		},
	}
	importsResult := &parser.ImportsResult{}

	compiler.processAndMergeServices(frontmatter, workflowData, importsResult)

	assert.NotEmpty(t, workflowData.Services)
	assert.Contains(t, workflowData.Services, "postgres")
	assert.Contains(t, workflowData.Services, "postgres:14")
}

// TestProcessAndMergeServices_WithImportedServices tests service merging with imported services
func TestProcessAndMergeServices_WithImportedServices(t *testing.T) {
	compiler := NewCompiler()
	workflowData := &WorkflowData{}

	frontmatter := map[string]any{
		"services": map[string]any{
			"postgres": map[string]any{
				"image": "postgres:14",
			},
		},
	}

	importedServices := map[string]any{
		"redis": map[string]any{
			"image": "redis:7",
		},
		"postgres": map[string]any{
			"image": "postgres:13", // Should be overridden by main
		},
	}
	importedServicesYAML, _ := yaml.Marshal(importedServices)

	importsResult := &parser.ImportsResult{
		MergedServices: string(importedServicesYAML),
	}

	compiler.processAndMergeServices(frontmatter, workflowData, importsResult)

	assert.NotEmpty(t, workflowData.Services)
	// Main workflow postgres should take precedence
	assert.Contains(t, workflowData.Services, "postgres:14")
	assert.NotContains(t, workflowData.Services, "postgres:13")
	// Imported redis should be included
	assert.Contains(t, workflowData.Services, "redis")
}

// TestProcessAndMergeServices_ImportedServicesOnly tests processAndMergeServices with only imported services
func TestProcessAndMergeServices_ImportedServicesOnly(t *testing.T) {
	compiler := NewCompiler()
	workflowData := &WorkflowData{}
	frontmatter := map[string]any{} // No main services

	importedServices := map[string]any{
		"redis": map[string]any{
			"image": "redis:7",
		},
	}
	importedServicesYAML, _ := yaml.Marshal(importedServices)

	importsResult := &parser.ImportsResult{
		MergedServices: string(importedServicesYAML),
	}

	compiler.processAndMergeServices(frontmatter, workflowData, importsResult)

	assert.NotEmpty(t, workflowData.Services)
	assert.Contains(t, workflowData.Services, "redis")
	assert.Contains(t, workflowData.Services, "redis:7")
}

// TestMergeJobsFromYAMLImports_NoImportedJobs tests mergeJobsFromYAMLImports with no imported jobs
func TestMergeJobsFromYAMLImports_NoImportedJobs(t *testing.T) {
	compiler := NewCompiler()

	mainJobs := map[string]any{
		"test": map[string]any{
			"runs-on": "ubuntu-latest",
			"steps": []any{
				map[string]any{"run": "echo test"},
			},
		},
	}

	result, err := compiler.mergeJobsFromYAMLImports(mainJobs, "")
	require.NoError(t, err)

	assert.Equal(t, mainJobs, result)
	assert.Len(t, result, 1)
}

// TestMergeJobsFromYAMLImports_EmptyJSON tests mergeJobsFromYAMLImports with empty JSON
func TestMergeJobsFromYAMLImports_EmptyJSON(t *testing.T) {
	compiler := NewCompiler()

	mainJobs := map[string]any{
		"test": map[string]any{"runs-on": "ubuntu-latest"},
	}

	result, err := compiler.mergeJobsFromYAMLImports(mainJobs, "{}")
	require.NoError(t, err)

	assert.Equal(t, mainJobs, result)
	assert.Len(t, result, 1)
}

// TestMergeJobsFromYAMLImports_ImportedJobsOnly tests merging with only imported jobs
func TestMergeJobsFromYAMLImports_ImportedJobsOnly(t *testing.T) {
	compiler := NewCompiler()

	mainJobs := map[string]any{}
	importedJobsJSON := `{"imported-job": {"runs-on": "ubuntu-latest", "steps": [{"run": "echo imported"}]}}`

	result, err := compiler.mergeJobsFromYAMLImports(mainJobs, importedJobsJSON)
	require.NoError(t, err)

	assert.Len(t, result, 1)
	assert.Contains(t, result, "imported-job")
}

// TestMergeJobsFromYAMLImports_MainJobTakesPrecedence tests that main jobs override imported jobs
func TestMergeJobsFromYAMLImports_MainJobTakesPrecedence(t *testing.T) {
	compiler := NewCompiler()

	mainJobs := map[string]any{
		"test": map[string]any{
			"runs-on": "ubuntu-latest",
			"steps": []any{
				map[string]any{"run": "echo main"},
			},
		},
	}

	// Imported job with same name "test"
	importedJobsJSON := `{"test": {"runs-on": "macos-latest", "steps": [{"run": "echo imported"}]}}`

	result, err := compiler.mergeJobsFromYAMLImports(mainJobs, importedJobsJSON)
	require.NoError(t, err)

	assert.Len(t, result, 1)
	assert.Contains(t, result, "test")

	// Main job should be preserved
	testJob := result["test"].(map[string]any)
	assert.Equal(t, "ubuntu-latest", testJob["runs-on"])
}

// TestMergeJobsFromYAMLImports_MergesPreStepsOnConflict tests deterministic merging of
// jobs.<job-id>.pre-steps when main and imported workflows define the same job.
func TestMergeJobsFromYAMLImports_MergesPreStepsOnConflict(t *testing.T) {
	compiler := NewCompiler()

	mainJobs := map[string]any{
		"test": map[string]any{
			"runs-on": "ubuntu-latest",
			"pre-steps": []any{
				map[string]any{"name": "main pre", "run": "echo main"},
			},
			"steps": []any{
				map[string]any{"run": "echo main job"},
			},
		},
	}

	importedJobsJSON := `{"test": {"runs-on": "macos-latest", "pre-steps": [{"name": "import pre", "run": "echo import"}], "steps": [{"run": "echo imported job"}]}}`
	result, err := compiler.mergeJobsFromYAMLImports(mainJobs, importedJobsJSON)
	require.NoError(t, err)

	assert.Len(t, result, 1)
	testJob := result["test"].(map[string]any)

	// Main job fields still take precedence.
	assert.Equal(t, "ubuntu-latest", testJob["runs-on"])

	preSteps, ok := testJob["pre-steps"].([]any)
	require.True(t, ok, "Expected merged pre-steps array")
	require.Len(t, preSteps, 2, "Expected imported+main pre-steps to be merged")

	first := preSteps[0].(map[string]any)
	second := preSteps[1].(map[string]any)
	assert.Equal(t, "import pre", first["name"], "Imported pre-steps should run first")
	assert.Equal(t, "main pre", second["name"], "Main workflow pre-steps should run after imported pre-steps")
}

// TestMergeJobsFromYAMLImports_MergesSetupStepsOnConflict tests deterministic merging of
// jobs.<job-id>.setup-steps when main and imported workflows define the same job.
func TestMergeJobsFromYAMLImports_MergesSetupStepsOnConflict(t *testing.T) {
	compiler := NewCompiler()

	mainJobs := map[string]any{
		"test": map[string]any{
			"runs-on": "ubuntu-latest",
			"setup-steps": []any{
				map[string]any{"name": "main setup", "run": "echo main"},
			},
			"steps": []any{
				map[string]any{"run": "echo main job"},
			},
		},
	}

	importedJobsJSON := `{"test": {"runs-on": "macos-latest", "setup-steps": [{"name": "import setup", "run": "echo import"}], "steps": [{"run": "echo imported job"}]}}`
	result, err := compiler.mergeJobsFromYAMLImports(mainJobs, importedJobsJSON)
	require.NoError(t, err)

	assert.Len(t, result, 1)
	testJob := result["test"].(map[string]any)

	// Main job fields still take precedence.
	assert.Equal(t, "ubuntu-latest", testJob["runs-on"])

	setupSteps, ok := testJob["setup-steps"].([]any)
	require.True(t, ok, "Expected merged setup-steps array")
	require.Len(t, setupSteps, 2, "Expected imported+main setup-steps to be merged")

	first := setupSteps[0].(map[string]any)
	second := setupSteps[1].(map[string]any)
	assert.Equal(t, "import setup", first["name"], "Imported setup-steps should run first")
	assert.Equal(t, "main setup", second["name"], "Main workflow setup-steps should run after imported setup-steps")
}

func TestMergeJobsFromYAMLImports_MergesSetupAndPreStepsIndependentlyOnConflict(t *testing.T) {
	compiler := NewCompiler()

	mainJobs := map[string]any{
		"test": map[string]any{
			"runs-on": "ubuntu-latest",
			"setup-steps": []any{
				map[string]any{"name": "main setup", "run": "echo main"},
			},
			"pre-steps": []any{
				map[string]any{"name": "main pre", "run": "echo main pre"},
			},
			"steps": []any{
				map[string]any{"run": "echo main job"},
			},
		},
	}

	importedJobsJSON := `{"test": {"runs-on": "macos-latest", "setup-steps": [{"name": "import setup", "run": "echo import setup"}], "pre-steps": [{"name": "import pre", "run": "echo import pre"}], "steps": [{"run": "echo imported job"}]}}`
	result, err := compiler.mergeJobsFromYAMLImports(mainJobs, importedJobsJSON)
	require.NoError(t, err)

	assert.Len(t, result, 1)
	testJob := result["test"].(map[string]any)

	setupSteps, ok := testJob["setup-steps"].([]any)
	require.True(t, ok, "Expected merged setup-steps array")
	require.Len(t, setupSteps, 2, "Expected imported+main setup-steps to be merged")

	preSteps, ok := testJob["pre-steps"].([]any)
	require.True(t, ok, "Expected merged pre-steps array")
	require.Len(t, preSteps, 2, "Expected imported+main pre-steps to be merged")

	first := setupSteps[0].(map[string]any)
	second := setupSteps[1].(map[string]any)
	assert.Equal(t, "import setup", first["name"], "Imported setup-steps should run first")
	assert.Equal(t, "main setup", second["name"], "Main workflow setup-steps should run after imported setup-steps")

	firstPre := preSteps[0].(map[string]any)
	secondPre := preSteps[1].(map[string]any)
	assert.Equal(t, "import pre", firstPre["name"], "Imported pre-steps should run first")
	assert.Equal(t, "main pre", secondPre["name"], "Main workflow pre-steps should run after imported pre-steps")
}

func TestMergeJobsFromYAMLImports_MergesActivationStepsOnConflict(t *testing.T) {
	compiler := NewCompiler()

	mainJobs := map[string]any{
		"activation": map[string]any{
			"steps": []any{
				map[string]any{"name": "main activation", "run": "echo main"},
			},
		},
	}

	importedJobsJSON := `{"activation": {"steps": [{"name": "import activation", "run": "echo import"}]}}`
	result, err := compiler.mergeJobsFromYAMLImports(mainJobs, importedJobsJSON)
	require.NoError(t, err)

	assert.Len(t, result, 1)
	activationJob := result["activation"].(map[string]any)

	steps, ok := activationJob["steps"].([]any)
	require.True(t, ok, "Expected merged activation steps array")
	require.Len(t, steps, 2, "Expected imported+main activation steps to be merged")

	first := steps[0].(map[string]any)
	second := steps[1].(map[string]any)
	assert.Equal(t, "import activation", first["name"], "Imported activation steps should run first")
	assert.Equal(t, "main activation", second["name"], "Main workflow activation steps should run after imported activation steps")
}

func TestMergeJobsFromYAMLImports_DoesNotMergeRegularStepsForCustomJobConflict(t *testing.T) {
	compiler := NewCompiler()

	mainJobs := map[string]any{
		"test": map[string]any{
			"runs-on": "ubuntu-latest",
			"steps": []any{
				map[string]any{"name": "main", "run": "echo main"},
			},
		},
	}

	importedJobsJSON := `{"test": {"runs-on": "macos-latest", "steps": [{"name": "import", "run": "echo import"}]}}`
	result, err := compiler.mergeJobsFromYAMLImports(mainJobs, importedJobsJSON)
	require.NoError(t, err)

	testJob := result["test"].(map[string]any)
	steps, ok := testJob["steps"].([]any)
	require.True(t, ok, "Expected main custom job steps array")
	require.Len(t, steps, 1, "Custom job conflicts should preserve main regular steps")
	assert.Equal(t, "main", steps[0].(map[string]any)["name"])
}

// TestMergeJobsFromYAMLImports_MultipleImportedJobs tests merging multiple imported jobs
func TestMergeJobsFromYAMLImports_MultipleImportedJobs(t *testing.T) {
	compiler := NewCompiler()

	mainJobs := map[string]any{
		"main-job": map[string]any{"runs-on": "ubuntu-latest"},
	}

	// Multiple JSON objects separated by newlines
	importedJobsJSON := `{"imported-1": {"runs-on": "ubuntu-latest"}}
{"imported-2": {"runs-on": "macos-latest"}}`

	result, err := compiler.mergeJobsFromYAMLImports(mainJobs, importedJobsJSON)
	require.NoError(t, err)

	assert.Len(t, result, 3)
	assert.Contains(t, result, "main-job")
	assert.Contains(t, result, "imported-1")
	assert.Contains(t, result, "imported-2")
}

// TestMergeJobsFromYAMLImports_MalformedJSON tests error handling with malformed JSON
func TestMergeJobsFromYAMLImports_MalformedJSON(t *testing.T) {
	compiler := NewCompiler()

	mainJobs := map[string]any{
		"test": map[string]any{"runs-on": "ubuntu-latest"},
	}

	// Malformed JSON should be skipped
	importedJobsJSON := `{"malformed": "unclosed`

	result, err := compiler.mergeJobsFromYAMLImports(mainJobs, importedJobsJSON)
	require.NoError(t, err)

	// Should return only main jobs, skipping malformed
	assert.Len(t, result, 1)
	assert.Contains(t, result, "test")
}

// TestMergeJobsFromYAMLImports_EmptyLines tests handling of empty lines in imported JSON
func TestMergeJobsFromYAMLImports_EmptyLines(t *testing.T) {
	compiler := NewCompiler()

	mainJobs := map[string]any{}

	// JSON with empty lines and empty objects
	importedJobsJSON := `
{}

{"job-1": {"runs-on": "ubuntu-latest"}}

{}
{"job-2": {"runs-on": "macos-latest"}}
`

	result, err := compiler.mergeJobsFromYAMLImports(mainJobs, importedJobsJSON)
	require.NoError(t, err)

	assert.Len(t, result, 2)
	assert.Contains(t, result, "job-1")
	assert.Contains(t, result, "job-2")
}

// TestExtractAdditionalConfigurations_BasicConfig tests extractAdditionalConfigurations with basic config
func TestExtractAdditionalConfigurations_BasicConfig(t *testing.T) {
	tmpDir := testutil.TempDir(t, "additional-config")
	compiler := NewCompiler()

	frontmatter := map[string]any{
		"on": map[string]any{
			"roles": []any{"admin", "contributor"},
			"bots":  []any{"copilot", "dependabot"},
		},
	}

	tools := map[string]any{
		"bash": []string{"echo", "ls"},
	}

	workflowData := &WorkflowData{}
	importsResult := &parser.ImportsResult{}

	err := compiler.extractAdditionalConfigurations(
		frontmatter,
		tools,
		tmpDir,
		workflowData,
		importsResult,
		"# Test\n\nContent",
		nil, // safeOutputs
	)

	require.NoError(t, err)
	assert.NotEmpty(t, workflowData.Roles)
	assert.NotEmpty(t, workflowData.Bots)
}

// TestExtractAdditionalConfigurations_WithSafeOutputs tests safe-outputs extraction
func TestExtractAdditionalConfigurations_WithSafeOutputs(t *testing.T) {
	tmpDir := testutil.TempDir(t, "safe-outputs-config")
	compiler := NewCompiler()

	frontmatter := map[string]any{}
	tools := map[string]any{}

	safeOutputs := &SafeOutputsConfig{
		CreateIssues: &CreateIssuesConfig{},
		AddComments:  &AddCommentsConfig{},
	}

	workflowData := &WorkflowData{}
	importsResult := &parser.ImportsResult{}

	err := compiler.extractAdditionalConfigurations(
		frontmatter,
		tools,
		tmpDir,
		workflowData,
		importsResult,
		"# Test\n\nContent",
		safeOutputs,
	)

	require.NoError(t, err)
	assert.NotNil(t, workflowData.SafeOutputs)
	assert.Equal(t, safeOutputs, workflowData.SafeOutputs)
}

// TestExtractAdditionalConfigurations_WithMergedJobs tests job merging in extractAdditionalConfigurations
func TestExtractAdditionalConfigurations_WithMergedJobs(t *testing.T) {
	tmpDir := testutil.TempDir(t, "merged-jobs-config")
	compiler := NewCompiler()

	frontmatter := map[string]any{
		"jobs": map[string]any{
			"main-job": map[string]any{"runs-on": "ubuntu-latest"},
		},
	}

	tools := map[string]any{}
	workflowData := &WorkflowData{}

	mergedJobsJSON := `{"imported-job": {"runs-on": "macos-latest"}}`
	importsResult := &parser.ImportsResult{
		MergedJobs: mergedJobsJSON,
	}

	err := compiler.extractAdditionalConfigurations(
		frontmatter,
		tools,
		tmpDir,
		workflowData,
		importsResult,
		"# Test\n\nContent",
		nil,
	)

	require.NoError(t, err)
	assert.Len(t, workflowData.Jobs, 2)
	assert.Contains(t, workflowData.Jobs, "main-job")
	assert.Contains(t, workflowData.Jobs, "imported-job")
}

// TestProcessOnSectionAndFilters_BasicFilters tests processOnSectionAndFilters with basic configuration
func TestProcessOnSectionAndFilters_BasicFilters(t *testing.T) {
	tmpDir := testutil.TempDir(t, "on-filters")
	compiler := NewCompiler()

	frontmatter := map[string]any{
		"on": map[string]any{
			"pull_request": map[string]any{
				"types": []string{"opened", "synchronize"},
			},
		},
	}

	workflowData := &WorkflowData{
		ParsedTools: NewTools(map[string]any{}),
	}

	testFile := filepath.Join(tmpDir, "test-workflow.md")
	err := compiler.processOnSectionAndFilters(frontmatter, workflowData, testFile)

	require.NoError(t, err)
	// Basic validation that processing succeeded
	assert.NotNil(t, workflowData)
}

// TestProcessOnSectionAndFilters_DraftFilter tests draft filter application
func TestProcessOnSectionAndFilters_DraftFilter(t *testing.T) {
	tmpDir := testutil.TempDir(t, "draft-filter")
	compiler := NewCompiler()

	frontmatter := map[string]any{
		"on": map[string]any{
			"pull_request": map[string]any{
				"types": []string{"opened"},
				"draft": false,
			},
		},
	}

	workflowData := &WorkflowData{
		ParsedTools: NewTools(map[string]any{}),
	}

	testFile := filepath.Join(tmpDir, "draft-workflow.md")
	err := compiler.processOnSectionAndFilters(frontmatter, workflowData, testFile)

	require.NoError(t, err)
	// Verify draft filter was processed
	assert.NotNil(t, workflowData)
}

// TestProcessOnSectionAndFilters_LabelFilter tests label filter application
func TestProcessOnSectionAndFilters_LabelFilter(t *testing.T) {
	tmpDir := testutil.TempDir(t, "label-filter")
	compiler := NewCompiler()

	frontmatter := map[string]any{
		"on": map[string]any{
			"issues": map[string]any{
				"types":  []string{"labeled"},
				"labels": []string{"bug", "enhancement"},
			},
		},
	}

	workflowData := &WorkflowData{
		ParsedTools: NewTools(map[string]any{}),
	}

	testFile := filepath.Join(tmpDir, "label-workflow.md")
	err := compiler.processOnSectionAndFilters(frontmatter, workflowData, testFile)

	require.NoError(t, err)
	assert.NotNil(t, workflowData)
}

// TestProcessOnSectionAndFilters_ForkFilter tests fork filter application
func TestProcessOnSectionAndFilters_ForkFilter(t *testing.T) {
	tmpDir := testutil.TempDir(t, "fork-filter")
	compiler := NewCompiler()

	frontmatter := map[string]any{
		"on": map[string]any{
			"pull_request": map[string]any{
				"types": []string{"opened"},
				"forks": "ignore",
			},
		},
	}

	workflowData := &WorkflowData{
		ParsedTools: NewTools(map[string]any{}),
	}

	testFile := filepath.Join(tmpDir, "fork-workflow.md")
	err := compiler.processOnSectionAndFilters(frontmatter, workflowData, testFile)

	require.NoError(t, err)
	assert.NotNil(t, workflowData)
}

// TestParseWorkflowFile_PhaseExecutionOrder tests that ParseWorkflowFile executes phases in correct order
func TestParseWorkflowFile_PhaseExecutionOrder(t *testing.T) {
	tmpDir := testutil.TempDir(t, "phase-order")

	// Create a complete workflow file
	testContent := `---
on: push
engine: copilot
permissions:
  contents: read
---

# Test Workflow

This tests phase execution order.
`

	testFile := filepath.Join(tmpDir, "phase-test.md")
	require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0644))

	compiler := NewCompiler()
	workflowData, err := compiler.ParseWorkflowFile(testFile)

	require.NoError(t, err)
	require.NotNil(t, workflowData)

	// Verify all phases completed successfully by checking resulting data
	assert.NotEmpty(t, workflowData.MarkdownContent, "Markdown should be processed")
	assert.NotEmpty(t, workflowData.AI, "Engine should be set")
	assert.NotNil(t, workflowData.ParsedTools, "Tools should be initialized")
	assert.NotNil(t, workflowData.NetworkPermissions, "Network permissions should be set")
	assert.NotEmpty(t, workflowData.Permissions, "Permissions should be extracted")
}

// TestParseWorkflowFile_ErrorPropagation tests error propagation through phases
func TestParseWorkflowFile_ErrorPropagation(t *testing.T) {
	tmpDir := testutil.TempDir(t, "error-propagation")

	tests := []struct {
		name        string
		content     string
		expectError string
	}{
		{
			name: "invalid frontmatter",
			content: `---
on: [invalid: yaml
---

# Workflow
`,
			expectError: "sequence end token", // Check for actual YAML error message instead of "parse frontmatter"
		},
		{
			name: "no markdown content for main workflow",
			content: `---
on: push
engine: copilot
---
`,
			expectError: "markdown content",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testFile := filepath.Join(tmpDir, tt.name+".md")
			require.NoError(t, os.WriteFile(testFile, []byte(tt.content), 0644))

			compiler := NewCompiler()
			workflowData, err := compiler.ParseWorkflowFile(testFile)

			require.Error(t, err, "Should error for %s", tt.name)
			assert.Nil(t, workflowData)
			if tt.expectError != "" {
				require.ErrorContains(t, err, tt.expectError)
			}
		})
	}
}

// TestParseWorkflowFile_WorkflowIDGeneration tests WorkflowID generation from file path
func TestParseWorkflowFile_WorkflowIDGeneration(t *testing.T) {
	tmpDir := testutil.TempDir(t, "workflow-id")

	tests := []struct {
		filename       string
		expectedPrefix string
	}{
		{
			filename:       "my-workflow.md",
			expectedPrefix: "my-workflow",
		},
		{
			filename:       "test_workflow_with_underscores.md",
			expectedPrefix: "test_workflow_with_underscores",
		},
		{
			filename:       "simple.md",
			expectedPrefix: "simple",
		},
	}

	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			testContent := `---
on: push
engine: copilot
---

# Test Workflow
`
			testFile := filepath.Join(tmpDir, tt.filename)
			require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0644))

			compiler := NewCompiler()
			workflowData, err := compiler.ParseWorkflowFile(testFile)

			require.NoError(t, err)
			require.NotNil(t, workflowData)
			assert.Equal(t, tt.expectedPrefix, workflowData.WorkflowID,
				"WorkflowID should be derived from filename without .md extension")
		})
	}
}

// TestParseWorkflowFile_PhaseDataFlow tests that data flows correctly between phases
func TestParseWorkflowFile_PhaseDataFlow(t *testing.T) {
	tmpDir := testutil.TempDir(t, "phase-data-flow")

	testContent := `---
on: push
engine: copilot
name: Phase Test Workflow
description: Tests phase data flow
source: test-source
strict: false
tools:
  bash: ["echo", "ls"]
  github:
    allowed: [list_issues]
permissions:
  contents: read
  issues: read
network:
  allowed:
    - github.com
timeout-minutes: 45
---

# Phase Test Workflow

Test content with ${{ steps.sanitized.outputs.text }} usage.
`

	testFile := filepath.Join(tmpDir, "phase-flow.md")
	require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0644))

	compiler := NewCompiler()
	workflowData, err := compiler.ParseWorkflowFile(testFile)

	require.NoError(t, err)
	require.NotNil(t, workflowData)

	// Verify data from frontmatter phase
	assert.Equal(t, "Phase Test Workflow", workflowData.FrontmatterName)
	assert.Equal(t, "Tests phase data flow", workflowData.Description)
	assert.Equal(t, "test-source", workflowData.Source)

	// Verify data from engine setup phase
	assert.Equal(t, "copilot", workflowData.AI)
	assert.NotNil(t, workflowData.EngineConfig)
	assert.NotNil(t, workflowData.NetworkPermissions)
	assert.Contains(t, workflowData.NetworkPermissions.Allowed, "github.com")

	// Verify data from tools processing phase
	assert.NotNil(t, workflowData.ParsedTools)
	assert.NotNil(t, workflowData.Tools)
	assert.True(t, workflowData.NeedsTextOutput)
	assert.NotEmpty(t, workflowData.MarkdownContent)

	// Verify data from YAML extraction phase
	assert.NotEmpty(t, workflowData.Permissions)
	assert.Contains(t, workflowData.Permissions, "contents")
	assert.NotEmpty(t, workflowData.TimeoutMinutes)
	assert.Contains(t, workflowData.TimeoutMinutes, "45")

	// Verify WorkflowID was generated
	assert.Equal(t, "phase-flow", workflowData.WorkflowID)
}

// TestParseWorkflowFile_BashToolValidationBeforeDefaults tests bash validation occurs before defaults
func TestParseWorkflowFile_BashToolValidationBeforeDefaults(t *testing.T) {
	tmpDir := testutil.TempDir(t, "bash-validation")

	// Test that bash validation happens before applyDefaults
	testContent := `---
on: push
engine: copilot
tools:
  bash: []
  cli-proxy: false
---

# Test Workflow
`

	testFile := filepath.Join(tmpDir, "bash-test.md")
	require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0644))

	compiler := NewCompiler()
	workflowData, err := compiler.ParseWorkflowFile(testFile)

	// Empty bash array should be valid (nil bash would be converted to defaults)
	require.NoError(t, err)
	require.NotNil(t, workflowData)
}

// TestParseWorkflowFile_CompleteWorkflowWithAllSections tests a workflow with all possible sections
func TestParseWorkflowFile_CompleteWorkflowWithAllSections(t *testing.T) {
	tmpDir := testutil.TempDir(t, "complete-workflow")

	testContent := `---
name: Complete Workflow
description: Test all sections
source: complete-test
on:
  push:
    branches: [main]
  pull_request:
    types: [opened, synchronize]
    draft: false
  roles:
    - admin
    - maintainer
  bots:
    - copilot
    - dependabot
permissions:
  contents: read
  issues: read
network:
  allowed:
    - defaults
concurrency: test-concurrency
run-name: Test Run
env:
  TEST_VAR: value
features:
  test-feature: true
if: github.actor != 'bot'
timeout-minutes: 30
runs-on: ubuntu-latest
environment: production
container:
  image: node:18
cache:
  key: test-cache
  path: ~/.npm
services:
  postgres:
    image: postgres:14
    env:
      POSTGRES_PASSWORD: postgres
engine: copilot
steps:
  - name: Custom step
    run: echo "test"
post-steps:
  - name: Cleanup
    run: echo "cleanup"
jobs:
  custom-job:
    runs-on: ubuntu-latest
    steps:
      - run: echo "custom"
---

# Complete Workflow

This workflow tests all sections.
`

	testFile := filepath.Join(tmpDir, "complete.md")
	require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0644))

	compiler := NewCompiler()
	workflowData, err := compiler.ParseWorkflowFile(testFile)

	require.NoError(t, err)
	require.NotNil(t, workflowData)

	// Verify all sections were processed
	assert.Equal(t, "Complete Workflow", workflowData.FrontmatterName)
	assert.Equal(t, "Test all sections", workflowData.Description)
	assert.Equal(t, "complete-test", workflowData.Source)
	assert.Equal(t, "copilot", workflowData.AI)
	assert.NotEmpty(t, workflowData.On)
	assert.NotEmpty(t, workflowData.Permissions)
	assert.NotEmpty(t, workflowData.Network)
	assert.NotEmpty(t, workflowData.Concurrency)
	assert.NotEmpty(t, workflowData.RunName)
	assert.NotEmpty(t, workflowData.Env)
	assert.NotEmpty(t, workflowData.Features)
	assert.NotEmpty(t, workflowData.If)
	assert.NotEmpty(t, workflowData.TimeoutMinutes)
	assert.NotEmpty(t, workflowData.RunsOn)
	assert.NotEmpty(t, workflowData.Environment)
	assert.NotEmpty(t, workflowData.Container)
	assert.NotEmpty(t, workflowData.Cache)
	assert.NotEmpty(t, workflowData.CustomSteps)
	assert.NotEmpty(t, workflowData.PostSteps)
	assert.NotEmpty(t, workflowData.Services)
	assert.NotEmpty(t, workflowData.Roles)
	assert.NotEmpty(t, workflowData.Bots)
	assert.NotEmpty(t, workflowData.Jobs)
	assert.NotNil(t, workflowData.NetworkPermissions)
	assert.NotNil(t, workflowData.ParsedTools)
}

// TestParseWorkflowFile_ErrorPropagationFromEngineSetup tests error propagation from engine setup phase
func TestParseWorkflowFile_ErrorPropagationFromEngineSetup(t *testing.T) {
	tmpDir := testutil.TempDir(t, "engine-error")

	testContent := `---
on: push
engine: invalid-engine-that-does-not-exist
---

# Test Workflow
`

	testFile := filepath.Join(tmpDir, "invalid-engine.md")
	require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0644))

	compiler := NewCompiler()
	workflowData, err := compiler.ParseWorkflowFile(testFile)

	require.Error(t, err, "Should error with invalid engine")
	assert.Nil(t, workflowData)
	require.ErrorContains(t, err, "invalid-engine-that-does-not-exist")
}

// TestParseWorkflowFile_ErrorPropagationFromToolsProcessing tests error propagation from tools phase
func TestParseWorkflowFile_ErrorPropagationFromToolsProcessing(t *testing.T) {
	tmpDir := testutil.TempDir(t, "tools-error")

	// Create main workflow with invalid tool timeout
	testContent := `---
on: push
engine: copilot
tools:
  timeout: -10
  bash: ["echo"]
---

# Test Workflow
`

	testFile := filepath.Join(tmpDir, "invalid-timeout.md")
	require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0644))

	compiler := NewCompiler()
	workflowData, err := compiler.ParseWorkflowFile(testFile)

	require.Error(t, err, "Should error with invalid tools timeout")
	assert.Nil(t, workflowData)
	require.ErrorContains(t, err, "timeout")
}

// TestParseWorkflowFile_ActionCacheAndResolverSetup tests action cache and resolver are properly set
func TestParseWorkflowFile_ActionCacheAndResolverSetup(t *testing.T) {
	tmpDir := testutil.TempDir(t, "action-cache")

	testContent := `---
on: push
engine: copilot
steps:
  - uses: actions/checkout@v3
    name: Checkout
    with:
      persist-credentials: false
---

# Test Workflow
`

	testFile := filepath.Join(tmpDir, "action-test.md")
	require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0644))

	compiler := NewCompiler()
	workflowData, err := compiler.ParseWorkflowFile(testFile)

	require.NoError(t, err)
	require.NotNil(t, workflowData)

	// Verify action cache and resolver are set
	assert.NotNil(t, workflowData.ActionCache, "ActionCache should be set")
	assert.NotNil(t, workflowData.ActionResolver, "ActionResolver should be set")
}

// TestExtractYAMLSections_PartialSections tests extraction with only some sections present
func TestExtractYAMLSections_PartialSections(t *testing.T) {
	compiler := NewCompiler()
	workflowData := &WorkflowData{}

	frontmatter := map[string]any{
		"on": map[string]any{
			"push": map[string]any{
				"branches": []string{"main"},
			},
		},
		"permissions": map[string]any{
			"contents": "read",
		},
		"timeout-minutes": 30,
		// Missing: network, concurrency, run-name, env, features, if, runs-on, environment, container, cache
	}

	compiler.extractYAMLSections(frontmatter, workflowData)

	// Verify present sections were extracted
	assert.NotEmpty(t, workflowData.On)
	assert.NotEmpty(t, workflowData.Permissions)
	assert.NotEmpty(t, workflowData.TimeoutMinutes)

	// Verify missing sections are empty
	assert.Empty(t, workflowData.Network)
	assert.Empty(t, workflowData.Concurrency)
	assert.Empty(t, workflowData.RunName)
	assert.Empty(t, workflowData.Env)
	assert.Empty(t, workflowData.Features)
	assert.Empty(t, workflowData.If)
	assert.Empty(t, workflowData.RunsOn)
	assert.Empty(t, workflowData.Environment)
	assert.Empty(t, workflowData.Container)
	assert.Empty(t, workflowData.Cache)
}

// TestMergeJobsFromYAMLImports_PreservesJobOrder tests job merge preserves main job definitions
func TestMergeJobsFromYAMLImports_PreservesJobOrder(t *testing.T) {
	compiler := NewCompiler()

	mainJobs := map[string]any{
		"job-a": map[string]any{"runs-on": "ubuntu-latest"},
		"job-b": map[string]any{"runs-on": "ubuntu-latest"},
	}

	importedJobsJSON := `{"job-c": {"runs-on": "ubuntu-latest"}}
{"job-d": {"runs-on": "macos-latest"}}`

	result, err := compiler.mergeJobsFromYAMLImports(mainJobs, importedJobsJSON)
	require.NoError(t, err)

	assert.Len(t, result, 4)
	// Verify all jobs present
	assert.Contains(t, result, "job-a")
	assert.Contains(t, result, "job-b")
	assert.Contains(t, result, "job-c")
	assert.Contains(t, result, "job-d")
}

// TestProcessAndMergeSteps_InvalidYAML_MergedSteps tests that malformed MergedSteps YAML returns an error
func TestProcessAndMergeSteps_InvalidYAML_MergedSteps(t *testing.T) {
	tmpDir := testutil.TempDir(t, "invalid-merged-steps-yaml")
	compiler := NewCompiler()
	actionCache := NewActionCache(tmpDir)
	actionResolver := NewActionResolver(actionCache)
	workflowData := &WorkflowData{
		ActionCache:    actionCache,
		ActionResolver: actionResolver,
	}

	frontmatter := map[string]any{
		"steps": []any{
			map[string]any{"name": "Main", "run": "echo main"},
		},
	}

	// Invalid YAML for imported steps
	importsResult := &parser.ImportsResult{
		MergedSteps: "invalid: [yaml",
	}

	// Malformed MergedSteps YAML must propagate as an error
	err := compiler.processAndMergeSteps(frontmatter, workflowData, importsResult)
	require.Error(t, err, "malformed MergedSteps YAML should return an error")
	require.ErrorContains(t, err, "imported steps YAML is not recognized")
}

// TestProcessAndMergePreSteps_InvalidYAML tests that malformed imported pre-steps YAML returns an error
func TestProcessAndMergePreSteps_InvalidYAML(t *testing.T) {
	tmpDir := testutil.TempDir(t, "invalid-pre-steps-yaml")
	compiler := NewCompiler()
	actionCache := NewActionCache(tmpDir)
	actionResolver := NewActionResolver(actionCache)
	workflowData := &WorkflowData{
		ActionCache:    actionCache,
		ActionResolver: actionResolver,
	}

	frontmatter := map[string]any{}
	importsResult := &parser.ImportsResult{
		MergedPreSteps: "invalid: [yaml",
	}

	err := compiler.processAndMergePreSteps(frontmatter, workflowData, importsResult)
	require.Error(t, err, "malformed imported pre-steps YAML should return an error")
	require.ErrorContains(t, err, "imported pre-steps YAML is not recognized")
}

// TestProcessAndMergePreAgentSteps_InvalidYAML tests that malformed imported pre-agent-steps YAML returns an error
func TestProcessAndMergePreAgentSteps_InvalidYAML(t *testing.T) {
	tmpDir := testutil.TempDir(t, "invalid-pre-agent-steps-yaml")
	compiler := NewCompiler()
	actionCache := NewActionCache(tmpDir)
	actionResolver := NewActionResolver(actionCache)
	workflowData := &WorkflowData{
		ActionCache:    actionCache,
		ActionResolver: actionResolver,
	}

	frontmatter := map[string]any{}
	importsResult := &parser.ImportsResult{
		MergedPreAgentSteps: "invalid: [yaml",
	}

	err := compiler.processAndMergePreAgentSteps(frontmatter, workflowData, importsResult)
	require.Error(t, err, "malformed imported pre-agent-steps YAML should return an error")
	require.ErrorContains(t, err, "imported pre-agent-steps YAML is not recognized")
}

// TestProcessAndMergePostSteps_InvalidYAML tests that malformed imported post-steps YAML returns an error
func TestProcessAndMergePostSteps_InvalidYAML(t *testing.T) {
	tmpDir := testutil.TempDir(t, "invalid-post-steps-yaml")
	compiler := NewCompiler()
	actionCache := NewActionCache(tmpDir)
	actionResolver := NewActionResolver(actionCache)
	workflowData := &WorkflowData{
		ActionCache:    actionCache,
		ActionResolver: actionResolver,
	}

	frontmatter := map[string]any{}
	importsResult := &parser.ImportsResult{
		MergedPostSteps: "invalid: [yaml",
	}

	err := compiler.processAndMergePostSteps(frontmatter, workflowData, importsResult)
	require.Error(t, err, "malformed imported post-steps YAML should return an error")
	require.ErrorContains(t, err, "imported post-steps YAML is not recognized")
}

// TestProcessAndMergeServices_EmptyImportedServices tests handling of empty imported services
func TestProcessAndMergeServices_EmptyImportedServices(t *testing.T) {
	compiler := NewCompiler()
	workflowData := &WorkflowData{}

	frontmatter := map[string]any{
		"services": map[string]any{
			"postgres": map[string]any{"image": "postgres:14"},
		},
	}

	// Empty YAML for imported services
	importsResult := &parser.ImportsResult{
		MergedServices: "",
	}

	compiler.processAndMergeServices(frontmatter, workflowData, importsResult)

	// Should only have main services
	assert.NotEmpty(t, workflowData.Services)
	assert.Contains(t, workflowData.Services, "postgres")
}

// TestBuildInitialWorkflowData_FieldMapping tests correct field mapping in buildInitialWorkflowData
func TestBuildInitialWorkflowData_FieldMapping(t *testing.T) {
	compiler := NewCompiler()

	// Test that all fields from toolsResult and engineSetup are correctly mapped
	frontmatterResult := &parser.FrontmatterResult{
		Frontmatter:      map[string]any{},
		FrontmatterLines: []string{"test: value"},
	}

	toolsResult := &toolsProcessingResult{
		workflowName:         "Test Name",
		frontmatterName:      "Frontmatter Name",
		trackerID:            "TRK-001",
		toolsTimeout:         "500",
		toolsStartupTimeout:  "100",
		needsTextOutput:      true,
		markdownContent:      "# Content",
		importedMarkdown:     "Imported",
		mainWorkflowMarkdown: "Main",
		importPaths:          []string{"/path1", "/path2"},
		allIncludedFiles:     []string{"/file1"},
		tools:                map[string]any{"tool1": "config1"},
		runtimes:             map[string]any{"runtime1": "v1"},
		safeOutputs:          &SafeOutputsConfig{},
		secretMasking:        &SecretMaskingConfig{},
		parsedFrontmatter:    &FrontmatterConfig{},
	}

	engineSetup := &engineSetupResult{
		engineSetting:      "copilot",
		engineConfig:       &EngineConfig{ID: "copilot"},
		networkPermissions: &NetworkPermissions{Allowed: []string{"defaults"}},
		sandboxConfig:      &SandboxConfig{},
		importsResult: &parser.ImportsResult{
			ImportedFiles: []string{"/imported1"},
			ImportInputs:  map[string]any{"input1": "value1"},
		},
	}

	workflowData, err := compiler.buildInitialWorkflowData(frontmatterResult, toolsResult, engineSetup, engineSetup.importsResult)
	require.NoError(t, err)

	// Verify all mappings
	assert.Equal(t, "Test Name", workflowData.Name)
	assert.Equal(t, "Frontmatter Name", workflowData.FrontmatterName)
	assert.Equal(t, "TRK-001", workflowData.TrackerID)
	assert.Equal(t, "500", workflowData.ToolsTimeout)
	assert.Equal(t, "100", workflowData.ToolsStartupTimeout)
	assert.True(t, workflowData.NeedsTextOutput)
	assert.Equal(t, "# Content", workflowData.MarkdownContent)
	assert.Equal(t, "Imported", workflowData.ImportedMarkdown)
	assert.Equal(t, "Main", workflowData.MainWorkflowMarkdown)
	assert.Equal(t, []string{"/path1", "/path2"}, workflowData.ImportPaths)
	assert.Equal(t, []string{"/file1"}, workflowData.IncludedFiles)
	assert.Equal(t, "copilot", workflowData.AI)
	assert.NotNil(t, workflowData.Tools)
	assert.NotNil(t, workflowData.Runtimes)
	assert.NotNil(t, workflowData.EngineConfig)
	assert.NotNil(t, workflowData.NetworkPermissions)
	assert.NotNil(t, workflowData.SandboxConfig)
	assert.Equal(t, []string{"/imported1"}, workflowData.ImportedFiles)
	assert.NotNil(t, workflowData.ImportInputs)
}

// TestExtractDispatchItemNumber tests that the item_number presence is detected
// directly from the frontmatter map rather than from re-parsing YAML strings.
func TestExtractDispatchItemNumber(t *testing.T) {
	tests := []struct {
		name        string
		frontmatter map[string]any
		want        bool
	}{
		{
			name: "label trigger shorthand PR workflow has item_number",
			frontmatter: map[string]any{
				"on": map[string]any{
					"pull_request": map[string]any{"types": []any{"labeled"}},
					"workflow_dispatch": map[string]any{
						"inputs": map[string]any{
							"item_number": map[string]any{
								"description": "The number of the pull request",
								"required":    true,
								"type":        "string",
							},
						},
					},
				},
			},
			want: true,
		},
		{
			name: "label trigger shorthand issue workflow has item_number",
			frontmatter: map[string]any{
				"on": map[string]any{
					"issues": map[string]any{"types": []any{"labeled"}},
					"workflow_dispatch": map[string]any{
						"inputs": map[string]any{
							"item_number": map[string]any{
								"description": "The number of the issue",
								"required":    true,
								"type":        "string",
							},
						},
					},
				},
			},
			want: true,
		},
		{
			name: "plain workflow_dispatch without item_number",
			frontmatter: map[string]any{
				"on": map[string]any{
					"workflow_dispatch": nil,
				},
			},
			want: false,
		},
		{
			name: "workflow_dispatch with unrelated inputs does not match",
			frontmatter: map[string]any{
				"on": map[string]any{
					"workflow_dispatch": map[string]any{
						"inputs": map[string]any{
							"branch": map[string]any{"type": "string"},
						},
					},
				},
			},
			want: false,
		},
		{
			name: "no workflow_dispatch",
			frontmatter: map[string]any{
				"on": map[string]any{
					"pull_request": map[string]any{"types": []any{"opened"}},
				},
			},
			want: false,
		},
		{
			name:        "empty frontmatter",
			frontmatter: map[string]any{},
			want:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractDispatchItemNumber(tt.frontmatter)
			assert.Equal(t, tt.want, got, "extractDispatchItemNumber() mismatch")
		})
	}
}

// TestExtractYAMLSections_HasDispatchItemNumber verifies that extractYAMLSections
// populates WorkflowData.HasDispatchItemNumber from the frontmatter map.
func TestExtractYAMLSections_HasDispatchItemNumber(t *testing.T) {
	compiler := NewCompiler()

	t.Run("label trigger shorthand workflow sets HasDispatchItemNumber", func(t *testing.T) {
		workflowData := &WorkflowData{}
		frontmatter := map[string]any{
			"on": map[string]any{
				"pull_request": map[string]any{"types": []any{"labeled"}},
				"workflow_dispatch": map[string]any{
					"inputs": map[string]any{
						"item_number": map[string]any{
							"description": "The number of the pull request",
							"required":    true,
							"type":        "string",
						},
					},
				},
			},
		}
		compiler.extractYAMLSections(frontmatter, workflowData)
		assert.True(t, workflowData.HasDispatchItemNumber, "should detect item_number from label trigger shorthand")
	})

	t.Run("plain workflow does not set HasDispatchItemNumber", func(t *testing.T) {
		workflowData := &WorkflowData{}
		frontmatter := map[string]any{
			"on": map[string]any{
				"pull_request": map[string]any{"types": []any{"opened"}},
			},
		}
		compiler.extractYAMLSections(frontmatter, workflowData)
		assert.False(t, workflowData.HasDispatchItemNumber, "should not set HasDispatchItemNumber for plain workflow")
	})
}

// TestExtractConcurrencyJobDiscriminator tests extraction of job-discriminator from the concurrency block
func TestExtractConcurrencyJobDiscriminator(t *testing.T) {
	tests := []struct {
		name        string
		frontmatter map[string]any
		want        string
	}{
		{
			name: "job-discriminator present in concurrency object",
			frontmatter: map[string]any{
				"concurrency": map[string]any{
					"group":             "gh-aw-${{ github.workflow }}-${{ inputs.finding_id }}",
					"job-discriminator": "${{ inputs.finding_id }}",
				},
			},
			want: "${{ inputs.finding_id }}",
		},
		{
			name: "concurrency object without job-discriminator",
			frontmatter: map[string]any{
				"concurrency": map[string]any{
					"group":              "gh-aw-${{ github.workflow }}",
					"cancel-in-progress": false,
				},
			},
			want: "",
		},
		{
			name: "concurrency as string (no job-discriminator)",
			frontmatter: map[string]any{
				"concurrency": "gh-aw-${{ github.workflow }}",
			},
			want: "",
		},
		{
			name:        "no concurrency key",
			frontmatter: map[string]any{},
			want:        "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractConcurrencyJobDiscriminator(tt.frontmatter)
			assert.Equal(t, tt.want, got, "extractConcurrencyJobDiscriminator() mismatch")
		})
	}
}

// TestExtractConcurrencySection tests that job-discriminator is stripped from the serialized YAML
func TestExtractConcurrencySection(t *testing.T) {
	compiler := NewCompiler()

	t.Run("job-discriminator is stripped from serialized YAML", func(t *testing.T) {
		frontmatter := map[string]any{
			"concurrency": map[string]any{
				"group":             "gh-aw-${{ github.workflow }}-${{ inputs.finding_id }}",
				"queue":             "max",
				"job-discriminator": "${{ inputs.finding_id }}",
			},
		}
		result := compiler.extractConcurrencySection(frontmatter)
		assert.NotContains(t, result, "job-discriminator", "job-discriminator should be stripped from serialized concurrency YAML")
		assert.Contains(t, result, "group:", "group field should remain in serialized YAML")
		assert.Contains(t, result, "queue: max", "queue field should remain in serialized YAML")
		assert.Contains(t, result, "gh-aw-${{ github.workflow }}-${{ inputs.finding_id }}", "group value should be preserved")
	})

	t.Run("original frontmatter is not modified", func(t *testing.T) {
		frontmatter := map[string]any{
			"concurrency": map[string]any{
				"group":             "gh-aw-${{ github.workflow }}-${{ inputs.finding_id }}",
				"job-discriminator": "${{ inputs.finding_id }}",
			},
		}
		compiler.extractConcurrencySection(frontmatter)
		// Original frontmatter should still have job-discriminator
		concurrencyMap := frontmatter["concurrency"].(map[string]any)
		_, hasDiscriminator := concurrencyMap["job-discriminator"]
		assert.True(t, hasDiscriminator, "original frontmatter should not be modified")
	})

	t.Run("concurrency without job-discriminator passes through unchanged", func(t *testing.T) {
		frontmatter := map[string]any{
			"concurrency": map[string]any{
				"group":              "gh-aw-${{ github.workflow }}",
				"cancel-in-progress": false,
			},
		}
		result := compiler.extractConcurrencySection(frontmatter)
		assert.Contains(t, result, "group:", "group field should be present")
		assert.NotContains(t, result, "job-discriminator", "no job-discriminator should appear")
	})

	t.Run("job-discriminator only (no group) returns empty string", func(t *testing.T) {
		frontmatter := map[string]any{
			"concurrency": map[string]any{
				"job-discriminator": "${{ inputs.finding_id }}",
			},
		}
		result := compiler.extractConcurrencySection(frontmatter)
		assert.Empty(t, result, "when only job-discriminator is present the workflow-level concurrency should be empty (compiler generates defaults)")
	})
}

// TestExtractYAMLSections_ConcurrencyJobDiscriminator verifies that extractYAMLSections
// populates WorkflowData.ConcurrencyJobDiscriminator from the frontmatter.
func TestExtractYAMLSections_ConcurrencyJobDiscriminator(t *testing.T) {
	compiler := NewCompiler()

	t.Run("job-discriminator is extracted and stored", func(t *testing.T) {
		workflowData := &WorkflowData{}
		frontmatter := map[string]any{
			"on": map[string]any{"workflow_dispatch": nil},
			"concurrency": map[string]any{
				"group":             "gh-aw-${{ github.workflow }}-${{ inputs.finding_id }}",
				"job-discriminator": "${{ inputs.finding_id }}",
			},
		}
		compiler.extractYAMLSections(frontmatter, workflowData)
		assert.Equal(t, "${{ inputs.finding_id }}", workflowData.ConcurrencyJobDiscriminator,
			"ConcurrencyJobDiscriminator should be populated from frontmatter")
		assert.NotContains(t, workflowData.Concurrency, "job-discriminator",
			"Concurrency YAML should not include job-discriminator")
	})

	t.Run("no job-discriminator leaves field empty", func(t *testing.T) {
		workflowData := &WorkflowData{}
		frontmatter := map[string]any{
			"on": map[string]any{"workflow_dispatch": nil},
			"concurrency": map[string]any{
				"group": "gh-aw-${{ github.workflow }}",
			},
		}
		compiler.extractYAMLSections(frontmatter, workflowData)
		assert.Empty(t, workflowData.ConcurrencyJobDiscriminator,
			"ConcurrencyJobDiscriminator should be empty when not in frontmatter")
	})
}
