//go:build !integration

package workflow

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRepoMemoryPathConsistencyAcrossLayers validates that all layers use consistent path patterns
// This test ensures the memory directory path, artifact name, branch name, and prompt path
// are constructed consistently across the Go, JavaScript, and shell script layers.
func TestRepoMemoryPathConsistencyAcrossLayers(t *testing.T) {
	tests := []struct {
		name                 string
		memoryID             string
		branchName           string
		expectedMemoryDir    string
		expectedPromptPath   string
		expectedArtifactName string
	}{
		{
			name:                 "default memory",
			memoryID:             "default",
			branchName:           "memory/default",
			expectedMemoryDir:    "/tmp/gh-aw/repo-memory/default",
			expectedPromptPath:   "/tmp/gh-aw/repo-memory/default/",
			expectedArtifactName: "repo-memory-default",
		},
		{
			name:                 "custom memory ID",
			memoryID:             "session",
			branchName:           "memory/session",
			expectedMemoryDir:    "/tmp/gh-aw/repo-memory/session",
			expectedPromptPath:   "/tmp/gh-aw/repo-memory/session/",
			expectedArtifactName: "repo-memory-session",
		},
		{
			name:                 "campaigns memory",
			memoryID:             "campaigns",
			branchName:           "memory/campaigns",
			expectedMemoryDir:    "/tmp/gh-aw/repo-memory/campaigns",
			expectedPromptPath:   "/tmp/gh-aw/repo-memory/campaigns/",
			expectedArtifactName: "repo-memory-campaigns",
		},
		{
			name:                 "hyphenated memory ID",
			memoryID:             "code-metrics",
			branchName:           "memory/code-metrics",
			expectedMemoryDir:    "/tmp/gh-aw/repo-memory/code-metrics",
			expectedPromptPath:   "/tmp/gh-aw/repo-memory/code-metrics/",
			expectedArtifactName: "repo-memory-codemetrics", // Sanitized: hyphens removed
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &RepoMemoryConfig{
				Memories: []RepoMemoryEntry{
					{
						ID:           tt.memoryID,
						BranchName:   tt.branchName,
						MaxFileSize:  10240,
						MaxFileCount: 100,
						CreateOrphan: true,
					},
				},
			}

			data := &WorkflowData{
				RepoMemoryConfig: config,
			}

			// Test 1: Validate prompt section uses correct memory path (with trailing slash)
			section := buildRepoMemoryPromptSection(config)
			require.NotNil(t, section, "Should have a prompt section")
			assert.True(t, section.IsFile, "Should use template file")

			// For single default memory, the path is in GH_AW_MEMORY_DIR
			// For non-default or multiple memories, the path is in GH_AW_MEMORY_LIST
			if config.Memories[0].ID == "default" && len(config.Memories) == 1 {
				assert.Equal(t, tt.expectedPromptPath, section.EnvVars["GH_AW_MEMORY_DIR"],
					"Prompt should contain memory path with trailing slash")
			} else {
				assert.Contains(t, section.EnvVars["GH_AW_MEMORY_LIST"], tt.expectedPromptPath,
					"Prompt list should contain memory path with trailing slash")
			}

			// Test 2: Validate artifact upload path (no trailing slash)
			var artifactUploadBuilder strings.Builder
			generateRepoMemoryArtifactUpload(&artifactUploadBuilder, data, getActionPin)
			artifactUploadOutput := artifactUploadBuilder.String()

			assert.Contains(t, artifactUploadOutput,
				"name: "+tt.expectedArtifactName,
				"Artifact upload should use correct artifact name")

			assert.Contains(t, artifactUploadOutput,
				"path: "+tt.expectedMemoryDir,
				"Artifact upload should use correct memory directory path (no trailing slash)")

			// Verify no trailing slash in artifact path
			assert.NotContains(t, artifactUploadOutput,
				fmt.Sprintf("path: %s/", tt.expectedMemoryDir),
				"Artifact path should not have trailing slash")

			// Test 3: Validate clone steps
			var cloneStepsBuilder strings.Builder
			NewCompiler().generateRepoMemorySteps(&cloneStepsBuilder, data, "agent")
			cloneStepsOutput := cloneStepsBuilder.String()

			assert.Contains(t, cloneStepsOutput,
				"MEMORY_DIR: "+tt.expectedMemoryDir,
				"Clone step should use correct memory directory path")

			assert.Contains(t, cloneStepsOutput,
				"BRANCH_NAME: "+tt.branchName,
				"Clone step should use correct branch name")

			// Test 4: Validate push job artifact download
			compiler := NewCompiler()
			pushJob, err := compiler.buildPushRepoMemoryJob(data, false)
			require.NoError(t, err, "Should successfully build push job")
			require.NotNil(t, pushJob, "Push job should not be nil")

			pushJobOutput := strings.Join(pushJob.Steps, "\n")

			assert.Contains(t, pushJobOutput,
				"name: "+tt.expectedArtifactName,
				"Push job should download correct artifact")

			assert.Contains(t, pushJobOutput,
				"path: "+tt.expectedMemoryDir,
				"Push job should download to correct memory directory")

			assert.Contains(t, pushJobOutput,
				"ARTIFACT_DIR: "+tt.expectedMemoryDir,
				"Push job should pass correct artifact directory to JavaScript")

			assert.Contains(t, pushJobOutput,
				"MEMORY_ID: "+tt.memoryID,
				"Push job should pass correct memory ID to JavaScript")

			assert.Contains(t, pushJobOutput,
				"BRANCH_NAME: "+tt.branchName,
				"Push job should pass correct branch name to JavaScript")
		})
	}
}

// TestRepoMemoryPromptPathTrailingSlash validates that prompt paths always have trailing slash
// to clearly indicate they are directories for agent file operations
func TestRepoMemoryPromptPathTrailingSlash(t *testing.T) {
	tests := []struct {
		name             string
		memoryID         string
		branchName       string
		checkExamplePath bool // Only check example path for single default memory
	}{
		{
			name:             "default memory",
			memoryID:         "default",
			branchName:       "memory/default",
			checkExamplePath: true,
		},
		{
			name:       "custom memory",
			memoryID:   "session",
			branchName: "memory/session",
			// Multiple memories use generic example paths, not memory-specific
			checkExamplePath: false,
		},
		{
			name:       "campaigns memory",
			memoryID:   "campaigns",
			branchName: "memory/campaigns",
			// Multiple memories use generic example paths, not memory-specific
			checkExamplePath: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &RepoMemoryConfig{
				Memories: []RepoMemoryEntry{
					{
						ID:         tt.memoryID,
						BranchName: tt.branchName,
					},
				},
			}

			section := buildRepoMemoryPromptSection(config)
			require.NotNil(t, section, "Should have a prompt section")
			assert.True(t, section.IsFile, "Should use template file")

			expectedPath := fmt.Sprintf("/tmp/gh-aw/repo-memory/%s/", tt.memoryID)

			if tt.memoryID == "default" {
				// Single default memory uses GH_AW_MEMORY_DIR
				assert.Equal(t, expectedPath, section.EnvVars["GH_AW_MEMORY_DIR"],
					"Prompt must show memory path with trailing slash")

				// Check example paths are memory-specific for default memory
				if tt.checkExamplePath {
					assert.Equal(t, repoMemoryPromptFile, section.Content,
						"Default memory should use single memory template")
				}
			} else {
				// Non-default single memory uses multi template with GH_AW_MEMORY_LIST
				assert.Contains(t, section.EnvVars["GH_AW_MEMORY_LIST"], expectedPath,
					"Prompt list must show memory path with trailing slash")
			}
		})
	}
}

// TestRepoMemoryArtifactPathNoTrailingSlash validates that artifact paths never have trailing slash
// for proper YAML path specification
func TestRepoMemoryArtifactPathNoTrailingSlash(t *testing.T) {
	config := &RepoMemoryConfig{
		Memories: []RepoMemoryEntry{
			{
				ID:           "test",
				BranchName:   "memory/test",
				MaxFileSize:  10240,
				MaxFileCount: 100,
			},
		},
	}

	data := &WorkflowData{
		RepoMemoryConfig: config,
	}

	// Test artifact upload
	var uploadBuilder strings.Builder
	generateRepoMemoryArtifactUpload(&uploadBuilder, data, getActionPin)
	uploadOutput := uploadBuilder.String()

	// Should have path without trailing slash
	assert.Contains(t, uploadOutput, "path: /tmp/gh-aw/repo-memory/test\n",
		"Artifact upload path should not have trailing slash")

	// Should not have path with trailing slash
	assert.NotContains(t, uploadOutput, "path: /tmp/gh-aw/repo-memory/test/",
		"Artifact upload path must not have trailing slash")

	// Test artifact download in push job
	compiler := NewCompiler()
	pushJob, err := compiler.buildPushRepoMemoryJob(data, false)
	require.NoError(t, err)
	require.NotNil(t, pushJob)

	pushJobOutput := strings.Join(pushJob.Steps, "\n")

	assert.Contains(t, pushJobOutput, "path: /tmp/gh-aw/repo-memory/test\n",
		"Artifact download path should not have trailing slash")
}

// TestRepoMemoryArtifactNameFormat validates artifact naming convention
func TestRepoMemoryArtifactNameFormat(t *testing.T) {
	tests := []struct {
		name         string
		memoryID     string
		expectedName string
	}{
		{
			name:         "default memory",
			memoryID:     "default",
			expectedName: "repo-memory-default",
		},
		{
			name:         "session memory",
			memoryID:     "session",
			expectedName: "repo-memory-session",
		},
		{
			name:         "campaigns memory",
			memoryID:     "campaigns",
			expectedName: "repo-memory-campaigns",
		},
		{
			name:         "hyphenated memory ID",
			memoryID:     "code-metrics",
			expectedName: "repo-memory-codemetrics", // Sanitized: hyphens removed
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &RepoMemoryConfig{
				Memories: []RepoMemoryEntry{
					{
						ID:         tt.memoryID,
						BranchName: "memory/" + tt.memoryID,
					},
				},
			}

			data := &WorkflowData{
				RepoMemoryConfig: config,
			}

			// Check artifact upload
			var uploadBuilder strings.Builder
			generateRepoMemoryArtifactUpload(&uploadBuilder, data, getActionPin)
			uploadOutput := uploadBuilder.String()

			assert.Contains(t, uploadOutput, "name: "+tt.expectedName,
				"Artifact upload should use correct naming convention")

			// Check artifact download in push job
			compiler := NewCompiler()
			pushJob, err := compiler.buildPushRepoMemoryJob(data, false)
			require.NoError(t, err)
			require.NotNil(t, pushJob)

			pushJobOutput := strings.Join(pushJob.Steps, "\n")

			assert.Contains(t, pushJobOutput, "name: "+tt.expectedName,
				"Artifact download should use same naming convention")
		})
	}
}

// TestRepoMemoryBranchNameGeneration validates default branch name generation
func TestRepoMemoryBranchNameGeneration(t *testing.T) {
	tests := []struct {
		name               string
		memoryID           string
		expectedBranchName string
	}{
		{
			name:               "default memory uses memory/default",
			memoryID:           "default",
			expectedBranchName: "memory/default",
		},
		{
			name:               "custom memory uses memory/{id}",
			memoryID:           "session",
			expectedBranchName: "memory/session",
		},
		{
			name:               "campaigns uses memory/campaigns",
			memoryID:           "campaigns",
			expectedBranchName: "memory/campaigns",
		},
		{
			name:               "hyphenated ID preserves hyphens",
			memoryID:           "code-metrics",
			expectedBranchName: "memory/code-metrics",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			branchName := generateDefaultBranchName(tt.memoryID, "memory")
			assert.Equal(t, tt.expectedBranchName, branchName,
				"Generated branch name should match expected pattern")
		})
	}
}

// TestRepoMemoryMultipleMemoriesPathConsistency validates path consistency for multiple memories
func TestRepoMemoryMultipleMemoriesPathConsistency(t *testing.T) {
	config := &RepoMemoryConfig{
		Memories: []RepoMemoryEntry{
			{
				ID:           "session",
				BranchName:   "memory/session",
				MaxFileSize:  10240,
				MaxFileCount: 100,
			},
			{
				ID:           "logs",
				BranchName:   "memory/logs",
				MaxFileSize:  2097152,
				MaxFileCount: 500,
			},
		},
	}

	data := &WorkflowData{
		RepoMemoryConfig: config,
	}

	// Test prompt generation
	section := buildRepoMemoryPromptSection(config)
	require.NotNil(t, section, "Should have a prompt section")
	assert.True(t, section.IsFile, "Should use template file")
	assert.Equal(t, repoMemoryPromptMultiFile, section.Content, "Should use multi memory template")

	memoryList := section.EnvVars["GH_AW_MEMORY_LIST"]
	assert.Contains(t, memoryList, "/tmp/gh-aw/repo-memory/session/",
		"Memory list should contain session memory path")
	assert.Contains(t, memoryList, "/tmp/gh-aw/repo-memory/logs/",
		"Memory list should contain logs memory path")

	// Test artifact upload
	var uploadBuilder strings.Builder
	generateRepoMemoryArtifactUpload(&uploadBuilder, data, getActionPin)
	uploadOutput := uploadBuilder.String()

	assert.Contains(t, uploadOutput, "name: repo-memory-session",
		"Should upload session artifact")
	assert.Contains(t, uploadOutput, "path: /tmp/gh-aw/repo-memory/session",
		"Should use correct session path")
	assert.Contains(t, uploadOutput, "name: repo-memory-logs",
		"Should upload logs artifact")
	assert.Contains(t, uploadOutput, "path: /tmp/gh-aw/repo-memory/logs",
		"Should use correct logs path")

	// Test clone steps
	var cloneBuilder strings.Builder
	NewCompiler().generateRepoMemorySteps(&cloneBuilder, data, "agent")
	cloneOutput := cloneBuilder.String()

	assert.Contains(t, cloneOutput, "Clone repo-memory branch (session)",
		"Should have clone step for session")
	assert.Contains(t, cloneOutput, "MEMORY_DIR: /tmp/gh-aw/repo-memory/session",
		"Should use correct session directory")
	assert.Contains(t, cloneOutput, "Clone repo-memory branch (logs)",
		"Should have clone step for logs")
	assert.Contains(t, cloneOutput, "MEMORY_DIR: /tmp/gh-aw/repo-memory/logs",
		"Should use correct logs directory")

	// Test push job
	compiler := NewCompiler()
	pushJob, err := compiler.buildPushRepoMemoryJob(data, false)
	require.NoError(t, err)
	require.NotNil(t, pushJob)

	pushJobOutput := strings.Join(pushJob.Steps, "\n")

	assert.Contains(t, pushJobOutput, "name: repo-memory-session",
		"Should download session artifact")
	assert.Contains(t, pushJobOutput, "ARTIFACT_DIR: /tmp/gh-aw/repo-memory/session",
		"Should use correct session artifact directory")
	assert.Contains(t, pushJobOutput, "name: repo-memory-logs",
		"Should download logs artifact")
	assert.Contains(t, pushJobOutput, "ARTIFACT_DIR: /tmp/gh-aw/repo-memory/logs",
		"Should use correct logs artifact directory")
}

// TestRepoMemoryPathComponentIsolation validates that memory IDs don't leak into other path components
func TestRepoMemoryPathComponentIsolation(t *testing.T) {
	// Test that branch names and memory directories are properly isolated
	config := &RepoMemoryConfig{
		Memories: []RepoMemoryEntry{
			{
				ID:         "test",
				BranchName: "memory/test",
			},
		},
	}

	data := &WorkflowData{
		RepoMemoryConfig: config,
	}

	// Generate prompt section EnvVars (memory ID "test" uses multi template since it's not "default")
	section := buildRepoMemoryPromptSection(config)
	require.NotNil(t, section, "Should have a prompt section")
	memoryList := section.EnvVars["GH_AW_MEMORY_LIST"]

	var uploadBuilder strings.Builder
	generateRepoMemoryArtifactUpload(&uploadBuilder, data, getActionPin)

	var cloneBuilder strings.Builder
	NewCompiler().generateRepoMemorySteps(&cloneBuilder, data, "agent")

	compiler := NewCompiler()
	pushJob, err := compiler.buildPushRepoMemoryJob(data, false)
	require.NoError(t, err)
	require.NotNil(t, pushJob)
	pushJobOutput := strings.Join(pushJob.Steps, "\n")

	// Combine all output (using memory list from prompt section EnvVars)
	allOutput := memoryList + uploadBuilder.String() + cloneBuilder.String() + pushJobOutput

	// Verify consistent use of /tmp/gh-aw/repo-memory/test
	expectedMemoryDir := "/tmp/gh-aw/repo-memory/test"
	assert.Contains(t, allOutput, expectedMemoryDir,
		"All layers should use same memory directory base path")

	// Verify branch name is used for git operations only
	assert.Contains(t, allOutput, "memory/test",
		"Branch name should appear in git operations")

	// Verify artifact name follows convention
	assert.Contains(t, allOutput, "repo-memory-test",
		"Artifact name should follow naming convention")

	// Verify no path leakage (e.g., branch name in memory path)
	assert.NotContains(t, allOutput, "/tmp/gh-aw/repo-memory/memory/test",
		"Memory path should not include branch name prefix")

	// Verify no artifact name leakage (e.g., artifact prefix in paths)
	assert.NotContains(t, allOutput, "/tmp/gh-aw/repo-memory-test",
		"Memory path should not use artifact naming convention")
}
