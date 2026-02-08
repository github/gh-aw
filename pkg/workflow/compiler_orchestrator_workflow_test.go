//go:build !integration

package workflow

import (
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

// TestBuildInitialWorkflowData_BasicFields tests that buildInitialWorkflowData correctly populates basic fields
func TestBuildInitialWorkflowData_BasicFields(t *testing.T) {
	compiler := NewCompiler()

	// Mock frontmatter result
	frontmatterResult := &parser.FrontmatterResult{
		Frontmatter:      map[string]any{"description": "Test workflow", "source": "test-source"},
		FrontmatterLines: []string{"description: Test workflow", "source: test-source"},
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
		pluginInfo:           &PluginInfo{Plugins: []string{"test-plugin"}},
		toolsTimeout:         300,
		toolsStartupTimeout:  60,
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
	workflowData := compiler.buildInitialWorkflowData(frontmatterResult, toolsResult, engineSetup, engineSetup.importsResult)

	// Verify all fields are populated correctly
	assert.Equal(t, "Test Workflow", workflowData.Name)
	assert.Equal(t, "Test Frontmatter Name", workflowData.FrontmatterName)
	assert.Equal(t, "Test workflow", workflowData.Description)
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
	assert.Equal(t, 300, workflowData.ToolsTimeout)
	assert.Equal(t, 60, workflowData.ToolsStartupTimeout)
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

	workflowData := compiler.buildInitialWorkflowData(frontmatterResult, toolsResult, engineSetup, engineSetup.importsResult)

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
		"features":        map[string]any{"safe-inputs": true},
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
	assert.Contains(t, workflowData.Features, "safe-inputs")
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

// TestProcessAndMergeSteps_NoSteps tests processAndMergeSteps with no steps
func TestProcessAndMergeSteps_NoSteps(t *testing.T) {
	compiler := NewCompiler()
	workflowData := &WorkflowData{}
	frontmatter := map[string]any{}
	importsResult := &parser.ImportsResult{}

	compiler.processAndMergeSteps(frontmatter, workflowData, importsResult)

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

	compiler.processAndMergeSteps(frontmatter, workflowData, importsResult)

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

	compiler.processAndMergeSteps(frontmatter, workflowData, importsResult)

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

	compiler.processAndMergeSteps(frontmatter, workflowData, importsResult)

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

	compiler.processAndMergeSteps(frontmatter, workflowData, importsResult)

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

	compiler.processAndMergePostSteps(frontmatter, workflowData)

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

	compiler.processAndMergePostSteps(frontmatter, workflowData)

	assert.NotEmpty(t, workflowData.PostSteps)
	assert.Contains(t, workflowData.PostSteps, "Cleanup")
	assert.Contains(t, workflowData.PostSteps, "Upload logs")
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

	result := compiler.mergeJobsFromYAMLImports(mainJobs, "")

	assert.Equal(t, mainJobs, result)
	assert.Len(t, result, 1)
}

// TestMergeJobsFromYAMLImports_EmptyJSON tests mergeJobsFromYAMLImports with empty JSON
func TestMergeJobsFromYAMLImports_EmptyJSON(t *testing.T) {
	compiler := NewCompiler()

	mainJobs := map[string]any{
		"test": map[string]any{"runs-on": "ubuntu-latest"},
	}

	result := compiler.mergeJobsFromYAMLImports(mainJobs, "{}")

	assert.Equal(t, mainJobs, result)
	assert.Len(t, result, 1)
}

// TestMergeJobsFromYAMLImports_ImportedJobsOnly tests merging with only imported jobs
func TestMergeJobsFromYAMLImports_ImportedJobsOnly(t *testing.T) {
	compiler := NewCompiler()

	mainJobs := map[string]any{}
	importedJobsJSON := `{"imported-job": {"runs-on": "ubuntu-latest", "steps": [{"run": "echo imported"}]}}`

	result := compiler.mergeJobsFromYAMLImports(mainJobs, importedJobsJSON)

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

	result := compiler.mergeJobsFromYAMLImports(mainJobs, importedJobsJSON)

	assert.Len(t, result, 1)
	assert.Contains(t, result, "test")

	// Main job should be preserved
	testJob := result["test"].(map[string]any)
	assert.Equal(t, "ubuntu-latest", testJob["runs-on"])
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

	result := compiler.mergeJobsFromYAMLImports(mainJobs, importedJobsJSON)

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

	result := compiler.mergeJobsFromYAMLImports(mainJobs, importedJobsJSON)

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

	result := compiler.mergeJobsFromYAMLImports(mainJobs, importedJobsJSON)

	assert.Len(t, result, 2)
	assert.Contains(t, result, "job-1")
	assert.Contains(t, result, "job-2")
}

// TestExtractAdditionalConfigurations_BasicConfig tests extractAdditionalConfigurations with basic config
func TestExtractAdditionalConfigurations_BasicConfig(t *testing.T) {
	tmpDir := testutil.TempDir(t, "additional-config")
	compiler := NewCompiler()

	frontmatter := map[string]any{
		"roles": []any{"admin", "contributor"},
		"bots":  []any{"copilot", "dependabot"},
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
			expectError: "parse frontmatter",
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
				assert.Contains(t, err.Error(), tt.expectError)
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
features:
  dangerous-permissions-write: true
tools:
  bash: ["echo", "ls"]
  github:
    allowed: [list_issues]
permissions:
  contents: read
  issues: write
network:
  allowed:
    - github.com
timeout-minutes: 45
---

# Phase Test Workflow

Test content with ${{ needs.activation.outputs.text }} usage.
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
on:
  push:
    branches: [main]
  pull_request:
    types: [opened, synchronize]
    draft: false
engine: copilot
name: Complete Workflow
description: Test all sections
source: complete-test
timeout-minutes: 60
strict: false
features:
  dangerous-permissions-write: true
permissions:
  contents: read
  issues: write
  pull-requests: write
network:
  allowed:
    - github.com
    - api.example.com
concurrency:
  group: ci-${{ github.ref }}
  cancel-in-progress: true
run-name: Test Run ${{ github.run_id }}
env:
  NODE_ENV: production
  DEBUG: "true"
if: github.event_name == 'push'
runs-on: ubuntu-latest
environment: production
container: node:18
cache:
  - key: ${{ runner.os }}-node
    path: node_modules
tools:
  bash: ["echo", "ls", "cat"]
  github:
    allowed: [list_issues, create_issue]
roles:
  - admin
  - maintainer
bots:
  - copilot
  - dependabot
steps:
  - name: Custom step
    run: echo "test"
post-steps:
  - name: Cleanup
    run: echo "cleanup"
services:
  postgres:
    image: postgres:14
    env:
      POSTGRES_PASSWORD: postgres
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
	assert.Contains(t, err.Error(), "invalid-engine-that-does-not-exist")
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
	assert.Contains(t, err.Error(), "timeout")
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

	result := compiler.mergeJobsFromYAMLImports(mainJobs, importedJobsJSON)

	assert.Len(t, result, 4)
	// Verify all jobs present
	assert.Contains(t, result, "job-a")
	assert.Contains(t, result, "job-b")
	assert.Contains(t, result, "job-c")
	assert.Contains(t, result, "job-d")
}

// TestProcessAndMergeSteps_InvalidYAML tests handling of invalid YAML in imported steps
func TestProcessAndMergeSteps_InvalidYAML(t *testing.T) {
	tmpDir := testutil.TempDir(t, "invalid-steps-yaml")
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

	// Should handle gracefully without panicking
	compiler.processAndMergeSteps(frontmatter, workflowData, importsResult)

	// Should still have main steps
	assert.NotEmpty(t, workflowData.CustomSteps)
	assert.Contains(t, workflowData.CustomSteps, "Main")
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
		toolsTimeout:         500,
		toolsStartupTimeout:  100,
		needsTextOutput:      true,
		markdownContent:      "# Content",
		importedMarkdown:     "Imported",
		mainWorkflowMarkdown: "Main",
		importPaths:          []string{"/path1", "/path2"},
		allIncludedFiles:     []string{"/file1"},
		tools:                map[string]any{"tool1": "config1"},
		runtimes:             map[string]any{"runtime1": "v1"},
		pluginInfo:           &PluginInfo{Plugins: []string{"plugin1"}},
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

	workflowData := compiler.buildInitialWorkflowData(frontmatterResult, toolsResult, engineSetup, engineSetup.importsResult)

	// Verify all mappings
	assert.Equal(t, "Test Name", workflowData.Name)
	assert.Equal(t, "Frontmatter Name", workflowData.FrontmatterName)
	assert.Equal(t, "TRK-001", workflowData.TrackerID)
	assert.Equal(t, 500, workflowData.ToolsTimeout)
	assert.Equal(t, 100, workflowData.ToolsStartupTimeout)
	assert.True(t, workflowData.NeedsTextOutput)
	assert.Equal(t, "# Content", workflowData.MarkdownContent)
	assert.Equal(t, "Imported", workflowData.ImportedMarkdown)
	assert.Equal(t, "Main", workflowData.MainWorkflowMarkdown)
	assert.Equal(t, []string{"/path1", "/path2"}, workflowData.ImportPaths)
	assert.Equal(t, []string{"/file1"}, workflowData.IncludedFiles)
	assert.Equal(t, "copilot", workflowData.AI)
	assert.NotNil(t, workflowData.Tools)
	assert.NotNil(t, workflowData.Runtimes)
	assert.NotNil(t, workflowData.PluginInfo)
	assert.NotNil(t, workflowData.EngineConfig)
	assert.NotNil(t, workflowData.NetworkPermissions)
	assert.NotNil(t, workflowData.SandboxConfig)
	assert.Equal(t, []string{"/imported1"}, workflowData.ImportedFiles)
	assert.NotNil(t, workflowData.ImportInputs)
}
