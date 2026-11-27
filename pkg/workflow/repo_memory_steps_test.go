//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRepoMemoryStepsGeneration tests that repo-memory steps are generated correctly
func TestRepoMemoryStepsGeneration(t *testing.T) {
	config := &RepoMemoryConfig{
		Memories: []RepoMemoryEntry{
			{
				ID:           "default",
				BranchName:   "memory/default",
				MaxFileSize:  10240,
				MaxFileCount: 100,
				CreateOrphan: true,
			},
		},
	}

	data := &WorkflowData{
		RepoMemoryConfig: config,
	}

	var builder strings.Builder
	NewCompiler().generateRepoMemorySteps(&builder, data, "agent")

	output := builder.String()

	// Check for clone step
	if !strings.Contains(output, "Clone repo-memory branch (default)") {
		t.Error("Expected clone step for repo-memory")
	}

	// Check for script call
	if !strings.Contains(output, "bash \"${RUNNER_TEMP}/gh-aw/actions/clone_repo_memory_branch.sh\"") {
		t.Error("Expected clone_repo_memory_branch.sh script call")
	}

	// Check for environment variables
	if !strings.Contains(output, "BRANCH_NAME: memory/default") {
		t.Error("Expected BRANCH_NAME environment variable")
	}

	if !strings.Contains(output, "CREATE_ORPHAN: true") {
		t.Error("Expected CREATE_ORPHAN environment variable")
	}

	if !strings.Contains(output, "MEMORY_DIR: /tmp/gh-aw/repo-memory/default") {
		t.Error("Expected MEMORY_DIR environment variable")
	}

	// Check for memory directory creation
	if !strings.Contains(output, "/tmp/gh-aw/repo-memory/default") {
		t.Error("Expected memory directory path")
	}
}

// TestGenerateRepoMemoryArtifactUpload tests that the artifact upload steps are generated correctly,
// including the sanitize step that appears before each upload step.
func TestGenerateRepoMemoryArtifactUpload(t *testing.T) {
	tests := []struct {
		name              string
		memory            RepoMemoryEntry
		wantSanitizeLabel string
	}{
		{
			name: "non-wiki memory",
			memory: RepoMemoryEntry{
				ID:         "default",
				BranchName: "memory/default",
				Wiki:       false,
			},
			wantSanitizeLabel: "repo-memory",
		},
		{
			name: "wiki memory",
			memory: RepoMemoryEntry{
				ID:         "notes",
				BranchName: "memory/notes",
				Wiki:       true,
			},
			wantSanitizeLabel: "wiki-memory",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &RepoMemoryConfig{
				Memories: []RepoMemoryEntry{tt.memory},
			}
			data := &WorkflowData{
				RepoMemoryConfig: config,
			}

			var builder strings.Builder
			generateRepoMemoryArtifactUpload(&builder, data, getActionPin)
			output := builder.String()

			sanitizeName := "Sanitize " + tt.wantSanitizeLabel + " filenames (" + tt.memory.ID + ")"
			uploadName := "Upload " + tt.wantSanitizeLabel + " artifact (" + tt.memory.ID + ")"

			// Both steps must be present
			assert.Contains(t, output, sanitizeName, "Should contain sanitize step")
			assert.Contains(t, output, uploadName, "Should contain upload step")

			// Sanitize step must appear before the upload step
			sanitizePos := strings.Index(output, sanitizeName)
			uploadPos := strings.Index(output, uploadName)
			assert.Less(t, sanitizePos, uploadPos, "Sanitize step should appear before upload step")

			// Sanitize step must have continue-on-error: true so a rename failure
			// does not block the artifact upload. Verify it appears between
			// "if: always()" and "env:" (correct position in YAML step structure).
			sanitizeSection := output[sanitizePos:uploadPos]
			ifPos := strings.Index(sanitizeSection, "if: always()")
			continuePos := strings.Index(sanitizeSection, "continue-on-error: true")
			envPos := strings.Index(sanitizeSection, "env:")
			require.Greater(t, continuePos, -1, "Sanitize step should have continue-on-error: true")
			assert.Greater(t, continuePos, ifPos,
				"continue-on-error should appear after if: always()")
			assert.Less(t, continuePos, envPos,
				"continue-on-error should appear before env:")

			// Sanitize step must set MEMORY_DIR and call the script
			expectedDir := "/tmp/gh-aw/repo-memory/" + tt.memory.ID
			assert.Contains(t, sanitizeSection, "MEMORY_DIR: "+expectedDir,
				"Sanitize step should set MEMORY_DIR env var")
			assert.Contains(t, sanitizeSection, "sanitize_repo_memory_filenames.sh",
				"Sanitize step should call sanitize_repo_memory_filenames.sh")
		})
	}
}

// TestRepoMemoryFilterStepGatesUpload verifies that when allowed-extensions/file-glob
// filtering is configured, the "Filter ... files" step has an id and the subsequent
// custom-validation and upload-artifact steps require that step's outcome to be
// 'success'. This ensures a filter-step failure (e.g. an fs error while removing
// ineligible files) can never result in an unfiltered directory being validated or
// uploaded — regression for a "fail open" review finding.
func TestRepoMemoryFilterStepGatesUpload(t *testing.T) {
	config := &RepoMemoryConfig{
		Memories: []RepoMemoryEntry{
			{
				ID:                "default",
				BranchName:        "memory/default",
				AllowedExtensions: []string{".json"},
				FileGlob:          []string{"*.json", "notes/*.md"},
				Validation: &MemoryValidationConfig{
					Script: "// noop",
				},
			},
		},
	}
	data := &WorkflowData{RepoMemoryConfig: config}

	var builder strings.Builder
	generateRepoMemoryArtifactUpload(&builder, data, getActionPin)
	output := builder.String()

	filterStepID := repoMemoryFilterStepID("default")
	validationStepID := repoMemoryValidationStepID("default")

	assert.Contains(t, output, "id: "+filterStepID, "Filter step must have an id")

	sanitizeNamePos := strings.Index(output, "Sanitize repo-memory filenames (default)")
	filterNamePos := strings.Index(output, "Filter repo-memory files (default)")
	filterIDPos := strings.Index(output, "id: "+filterStepID)
	validationNamePos := strings.Index(output, "Validate repo-memory domain content (default)")
	uploadNamePos := strings.Index(output, "Upload repo-memory artifact (default)")
	require.Greater(t, sanitizeNamePos, -1)
	require.Greater(t, filterNamePos, -1)
	require.Greater(t, validationNamePos, -1)
	require.Greater(t, uploadNamePos, -1)
	assert.Less(t, sanitizeNamePos, filterNamePos, "Sanitize step must appear before filter step")
	assert.Less(t, filterNamePos, validationNamePos, "Filter step must appear before validation step")
	assert.Less(t, validationNamePos, uploadNamePos, "Validation step must appear before upload step")

	filterSection := output[filterNamePos:validationNamePos]
	assert.Contains(t, filterSection, "id: "+filterStepID, "Filter step id must appear within the filter step")
	assert.Contains(t, filterSection, `ALLOWED_EXTENSIONS: '[".json"]'`, "Filter step must set ALLOWED_EXTENSIONS to the JSON-encoded extensions list")
	assert.Contains(t, filterSection, `FILE_GLOB_FILTER: "*.json notes/*.md"`, "Filter step must set FILE_GLOB_FILTER to the space-joined glob patterns")
	assert.Greater(t, filterIDPos, filterNamePos, "Filter step id must appear after its name")

	validationSection := output[validationNamePos:uploadNamePos]
	assert.Contains(t, validationSection, "steps."+filterStepID+".outcome == 'success'",
		"Validation step must be gated on the filter step's success")

	uploadSection := output[uploadNamePos:]
	assert.Contains(t, uploadSection, "steps."+filterStepID+".outcome == 'success'",
		"Upload step must be gated on the filter step's success")
	assert.Contains(t, uploadSection, "steps."+validationStepID+".outcome == 'success'",
		"Upload step must also be gated on the validation step's success")
}

// TestRepoMemoryFilterStepEmptyBothFieldsSkipsFilter verifies that the "no filter step"
// branch is only reachable when a caller explicitly leaves both AllowedExtensions and
// FileGlob empty — i.e. it's not accidentally triggered by a nil vs. empty-slice distinction.
func TestRepoMemoryFilterStepEmptyBothFieldsSkipsFilter(t *testing.T) {
	var builder strings.Builder
	stepID := generateRepoMemoryFilterFilesStep(&builder, RepoMemoryEntry{
		ID:                "default",
		AllowedExtensions: []string{},
		FileGlob:          []string{},
	}, "/tmp/gh-aw/repo-memory/default", "repo-memory", "")

	assert.Empty(t, stepID, "No filter step id should be returned when both fields are empty slices")
	assert.Empty(t, builder.String(), "No filter step should be emitted when both fields are empty slices")
}

// TestRepoMemoryNoFilterStepWhenNoFilterConfigured verifies that when no
// allowed-extensions/file-glob is configured, no filter step id is emitted and the
// upload step is not gated on a nonexistent filter step.
func TestRepoMemoryNoFilterStepWhenNoFilterConfigured(t *testing.T) {
	config := &RepoMemoryConfig{
		Memories: []RepoMemoryEntry{
			{
				ID:         "default",
				BranchName: "memory/default",
			},
		},
	}
	data := &WorkflowData{RepoMemoryConfig: config}

	var builder strings.Builder
	generateRepoMemoryArtifactUpload(&builder, data, getActionPin)
	output := builder.String()

	assert.NotContains(t, output, "Filter repo-memory files (default)", "No filter step should be emitted")
	assert.NotContains(t, output, repoMemoryFilterStepID("default"), "No filter step id should be referenced")
}

// TestRepoMemoryPromptGeneration tests that prompt section is generated correctly
func TestRepoMemoryPromptGeneration(t *testing.T) {
	config := &RepoMemoryConfig{
		Memories: []RepoMemoryEntry{
			{
				ID:          "default",
				BranchName:  "memory/default",
				Description: "Persistent memory for agent state",
			},
		},
	}

	section := buildRepoMemoryPromptSection(config)

	require.NotNil(t, section, "Expected non-nil prompt section")
	assert.True(t, section.IsFile, "Should use template file")
	assert.Equal(t, repoMemoryPromptFile, section.Content, "Should reference repo memory prompt file")
	require.NotNil(t, section.EnvVars, "Should have environment variables")

	// Check for prompt header key
	assert.Equal(t, "/tmp/gh-aw/repo-memory/default/", section.EnvVars["GH_AW_MEMORY_DIR"], "Should have correct memory directory")

	// Check for description
	assert.Equal(t, " Persistent memory for agent state", section.EnvVars["GH_AW_MEMORY_DESCRIPTION"], "Expected custom description with leading space")

	// Check for key information
	assert.Equal(t, "memory/default", section.EnvVars["GH_AW_MEMORY_BRANCH_NAME"], "Should have correct branch name")
	assert.Equal(t, " of the current repository", section.EnvVars["GH_AW_MEMORY_TARGET_REPO"], "Should default to current repository")
}

// TestRepoMemoryWikiStepsGeneration tests that wiki:true appends .wiki to TARGET_REPO
func TestRepoMemoryWikiStepsGeneration(t *testing.T) {
	config := &RepoMemoryConfig{
		Memories: []RepoMemoryEntry{
			{
				ID:         "default",
				BranchName: "master",
				Wiki:       true,
			},
		},
	}

	data := &WorkflowData{
		RepoMemoryConfig: config,
	}

	var builder strings.Builder
	NewCompiler().generateRepoMemorySteps(&builder, data, "agent")

	output := builder.String()
	assert.Contains(t, output, "TARGET_REPO: ${{ github.repository }}.wiki",
		"Wiki mode should append .wiki to TARGET_REPO")
	assert.Contains(t, output, "- name: Clone wiki-memory branch (default)",
		"Wiki mode should use wiki-memory step name")
}

// TestRepoMemoryWikiStepsWithTargetRepo tests wiki mode with explicit target-repo
func TestRepoMemoryWikiStepsWithTargetRepo(t *testing.T) {
	config := &RepoMemoryConfig{
		Memories: []RepoMemoryEntry{
			{
				ID:         "default",
				BranchName: "master",
				TargetRepo: "myorg/myrepo",
				Wiki:       true,
			},
		},
	}

	data := &WorkflowData{
		RepoMemoryConfig: config,
	}

	var builder strings.Builder
	NewCompiler().generateRepoMemorySteps(&builder, data, "agent")

	output := builder.String()
	assert.Contains(t, output, "TARGET_REPO: myorg/myrepo.wiki",
		"Wiki mode should append .wiki to explicit target-repo")
	assert.Contains(t, output, "- name: Clone wiki-memory branch (default)",
		"Wiki mode should use wiki-memory step name")
}

// TestRepoMemoryWikiPromptSection tests that wiki mode injects wiki note into prompt
func TestRepoMemoryWikiPromptSection(t *testing.T) {
	config := &RepoMemoryConfig{
		Memories: []RepoMemoryEntry{
			{
				ID:         "default",
				BranchName: "master",
				Wiki:       true,
			},
		},
	}

	section := buildRepoMemoryPromptSection(config)

	require.NotNil(t, section, "Expected non-nil prompt section")
	require.NotNil(t, section.EnvVars, "Expected env vars")

	wikiNote := section.EnvVars["GH_AW_WIKI_NOTE"]
	assert.NotEmpty(t, wikiNote, "Wiki mode should set GH_AW_WIKI_NOTE")
	assert.Contains(t, wikiNote, "GitHub Wiki", "Wiki note should mention GitHub Wiki")
	assert.Contains(t, wikiNote, "Markdown", "Wiki note should mention Markdown syntax")
}

// TestRepoMemoryNonWikiPromptSection tests that non-wiki mode substitutes an empty wiki note.
func TestRepoMemoryNonWikiPromptSection(t *testing.T) {
	config := &RepoMemoryConfig{
		Memories: []RepoMemoryEntry{
			{
				ID:         "default",
				BranchName: "memory/default",
			},
		},
	}

	section := buildRepoMemoryPromptSection(config)

	require.NotNil(t, section, "Expected non-nil prompt section")
	require.NotNil(t, section.EnvVars, "Expected env vars")

	require.Contains(t, section.EnvVars, "GH_AW_WIKI_NOTE")
	assert.Empty(t, section.EnvVars["GH_AW_WIKI_NOTE"])
	var prompt strings.Builder
	mappings := NewCompiler().generateUnifiedPromptCreationStep(&prompt, []PromptSection{*section}, nil, nil, &WorkflowData{})
	assert.NotContains(t, prompt.String(), "GH_AW_WIKI_NOTE:")
	assert.Contains(t, mappings, &ExpressionMapping{EnvVar: "GH_AW_WIKI_NOTE", Content: "", IsLiteral: true})
}

func TestRepoMemoryNonWikiCompilation(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "repo-memory.md")
	source := `---
on:
  workflow_dispatch:
  admission: true
engine: copilot
permissions:
  contents: read
tools:
  github: false
  repo-memory:
    branch-name: memory/repo-memory
---
Maintain the repository memory.
`
	require.NoError(t, os.WriteFile(path, []byte(source), 0o600))
	compiler := NewCompiler(WithLint(true))
	compiler.gitRoot = dir
	compiler.SetReadOnly(true)
	compiler.SetSkipValidation(true)
	require.NoError(t, compiler.CompileWorkflow(path))
	require.Zero(t, compiler.GetWarningCount())
	lock, err := os.ReadFile(filepath.Join(dir, "repo-memory.lock.yml"))
	require.NoError(t, err)
	assert.NotContains(t, string(lock), "GH_AW_WIKI_NOTE: ${{ '' }}")
	assert.Contains(t, string(lock), `GH_AW_WIKI_NOTE: ""`)
}

func TestRepoMemoryValidationConfigAndGeneratedSteps(t *testing.T) {
	toolsMap := map[string]any{
		"repo-memory": []any{
			map[string]any{
				"id":                 "default",
				"branch-name":        "memory/notes",
				"allowed-extensions": []any{".json"},
				"validation": map[string]any{
					"script":          "if (!fs.existsSync(path.join(memoryRoot, 'state.json'))) throw new Error('missing state');",
					"timeout-minutes": 1,
				},
			},
			map[string]any{
				"id": "schema",
				"validation": map[string]any{
					"json-schemas": []any{
						map[string]any{"file": "state.json", "format": "json", "schema": map[string]any{"type": "object"}},
					},
				},
			},
		},
	}

	toolsConfig, err := ParseToolsConfig(toolsMap)
	require.NoError(t, err)

	compiler := NewCompiler()
	config, err := compiler.extractRepoMemoryConfig(toolsConfig, "")
	require.NoError(t, err)
	require.NotNil(t, config)
	require.Len(t, config.Memories, 2)
	require.NotNil(t, config.Memories[0].Validation)
	assert.Equal(t, 1, config.Memories[0].Validation.TimeoutMinutes)
	assert.Contains(t, config.Memories[0].Validation.Script, "missing state")
	require.NotNil(t, config.Memories[1].Validation)
	assert.Empty(t, config.Memories[1].Validation.Script)
	require.Len(t, config.Memories[1].Validation.JSONSchemas, 1)

	data := &WorkflowData{RepoMemoryConfig: config}
	var restore strings.Builder
	NewCompiler().generateRepoMemorySteps(&restore, data, "agent")
	assert.NotContains(t, restore.String(), "Validate repo-memory baseline (default)")
	generateRepoMemoryBaselineValidationSteps(&restore, data)
	restoreYAML := restore.String()
	assert.Less(t, strings.Index(restoreYAML, "Clone repo-memory branch (default)"), strings.Index(restoreYAML, "Validate repo-memory baseline (default)"))
	assert.Contains(t, restoreYAML, "validateRepoMemoryBaseline(core)")
	assert.NotContains(t, restoreYAML, "continue-on-error")

	var upload strings.Builder
	generateRepoMemoryArtifactUpload(&upload, data, getActionPin)
	uploadYAML := upload.String()
	assert.Contains(t, uploadYAML, "Validate repo-memory domain content (default)")
	assert.Contains(t, uploadYAML, "VALIDATION_SCRIPT_B64:")
	assert.Contains(t, uploadYAML, "MEMORY_JSON_SCHEMAS_REQUIRED: 'true'")
	assert.Contains(t, uploadYAML, "validate_memory_step.cjs")
	assert.Contains(t, uploadYAML, "steps."+repoMemoryValidationStepID("default")+".outcome == 'success'")
	assert.Contains(t, uploadYAML, "steps."+repoMemoryValidationStepID("schema")+".outcome == 'success'")
	skipCondition := "steps." + memoryValidationStepID("check_repo_memory_baseline", "default") + ".outputs.skip != 'true'"
	assert.Contains(t, uploadYAML, "checkRepoMemoryBaseline(core)")
	assert.Equal(t, 4, strings.Count(uploadYAML, skipCondition), "unchanged invalid memory must skip sanitize, filter, validation, and upload")

	pushJob, err := compiler.buildPushRepoMemoryJob(data, false)
	require.NoError(t, err)
	require.NotNil(t, pushJob)
	pushYAML := strings.Join(pushJob.Steps, "\n")
	assert.Contains(t, pushYAML, "VALIDATION_SCRIPT_B64:")
	assert.Contains(t, pushYAML, "GH_AW_MEMORY_SOURCE_JOB_RESULT: ${{ needs.agent.result }}")
	assert.Contains(t, pushYAML, "MEMORY_JSON_SCHEMAS_REQUIRED: 'true'")
	assert.Contains(t, pushYAML, "VALIDATION_TIMEOUT_SECONDS: 60")
}

func TestRepoMemoryValidationStepIDsDoNotCollide(t *testing.T) {
	hyphenID := repoMemoryValidationStepID("my-memory")
	underscoreID := repoMemoryValidationStepID("my_memory")

	assert.NotEqual(t, hyphenID, underscoreID)
	assert.Equal(t, "validate_repo_memory_6d792d6d656d6f7279", hyphenID)
	assert.Equal(t, "validate_repo_memory_6d795f6d656d6f7279", underscoreID)
}
