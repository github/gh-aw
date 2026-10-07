//go:build !integration

package cli

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDeleteLegacyAgentFiles tests deletion of old agent files.
func TestDeleteLegacyAgentFiles(t *testing.T) {
	tests := []struct {
		name            string
		filesToCreate   []string // Paths relative to git root
		expectedDeleted []string // Files that should be deleted
	}{
		{
			name: "deletes legacy agent files from .github/agents including dispatcher",
			filesToCreate: []string{
				".github/agents/agentic-workflows.agent.md",
				".github/agents/create-agentic-workflow.agent.md",
				".github/agents/debug-agentic-workflow.agent.md",
				".github/agents/create-shared-agentic-workflow.agent.md",
			},
			expectedDeleted: []string{
				".github/agents/agentic-workflows.agent.md",
				".github/agents/create-agentic-workflow.agent.md",
				".github/agents/debug-agentic-workflow.agent.md",
				".github/agents/create-shared-agentic-workflow.agent.md",
			},
		},
		{
			name: "deletes singular upgrade-agentic-workflow.md from .github/aw",
			filesToCreate: []string{
				".github/aw/upgrade-agentic-workflow.md",
			},
			expectedDeleted: []string{
				".github/aw/upgrade-agentic-workflow.md",
			},
		},
		{
			name: "deletes both agent and aw files",
			filesToCreate: []string{
				".github/agents/create-agentic-workflow.agent.md",
				".github/aw/upgrade-agentic-workflow.md",
			},
			expectedDeleted: []string{
				".github/agents/create-agentic-workflow.agent.md",
				".github/aw/upgrade-agentic-workflow.md",
			},
		},
		{
			name: "deletes old non-.agent.md files from .github/agents",
			filesToCreate: []string{
				".github/agents/create-agentic-workflow.md",
				".github/agents/create-shared-agentic-workflow.md",
				".github/agents/setup-agentic-workflows.md",
				".github/agents/update-agentic-workflows.md",
				".github/agents/upgrade-agentic-workflows.md",
			},
			expectedDeleted: []string{
				".github/agents/create-agentic-workflow.md",
				".github/agents/create-shared-agentic-workflow.md",
				".github/agents/setup-agentic-workflows.md",
				".github/agents/update-agentic-workflows.md",
				".github/agents/upgrade-agentic-workflows.md",
			},
		},
		{
			name: "deletes all old agent files together",
			filesToCreate: []string{
				".github/agents/create-agentic-workflow.agent.md",
				".github/agents/debug-agentic-workflow.agent.md",
				".github/agents/create-shared-agentic-workflow.agent.md",
				".github/agents/agentic-workflows.agent.md",
				".github/agents/create-agentic-workflow.md",
				".github/agents/create-shared-agentic-workflow.md",
				".github/agents/setup-agentic-workflows.md",
				".github/agents/update-agentic-workflows.md",
				".github/agents/upgrade-agentic-workflows.md",
				".github/aw/upgrade-agentic-workflow.md",
			},
			expectedDeleted: []string{
				".github/agents/create-agentic-workflow.agent.md",
				".github/agents/debug-agentic-workflow.agent.md",
				".github/agents/create-shared-agentic-workflow.agent.md",
				".github/agents/agentic-workflows.agent.md",
				".github/agents/create-agentic-workflow.md",
				".github/agents/create-shared-agentic-workflow.md",
				".github/agents/setup-agentic-workflows.md",
				".github/agents/update-agentic-workflows.md",
				".github/agents/upgrade-agentic-workflows.md",
				".github/aw/upgrade-agentic-workflow.md",
			},
		},
		{
			name:            "handles no files to delete",
			filesToCreate:   []string{},
			expectedDeleted: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a temporary directory for testing
			tempDir := testutil.TempDir(t, "test-*")

			// Change to temp directory and initialize git repo
			oldWd, _ := os.Getwd()
			defer func() {
				_ = os.Chdir(oldWd)
			}()
			err := os.Chdir(tempDir)
			if err != nil {
				t.Fatalf("Failed to change directory: %v", err)
			}

			// Initialize git repo
			if err := exec.Command("git", "init").Run(); err != nil {
				t.Fatalf("Failed to init git repo: %v", err)
			}

			// Create test files
			for _, filePath := range tt.filesToCreate {
				fullPath := filepath.Join(tempDir, filePath)
				dir := filepath.Dir(fullPath)
				if err := os.MkdirAll(dir, 0755); err != nil {
					t.Fatalf("Failed to create directory %s: %v", dir, err)
				}
				if err := os.WriteFile(fullPath, []byte("test content"), 0644); err != nil {
					t.Fatalf("Failed to create file %s: %v", fullPath, err)
				}
			}

			// Call deleteLegacyAgentFiles
			err = deleteLegacyAgentFiles(false)
			if err != nil {
				t.Fatalf("deleteLegacyAgentFiles() returned error: %v", err)
			}

			// Verify expected files were deleted
			for _, filePath := range tt.expectedDeleted {
				fullPath := filepath.Join(tempDir, filePath)
				if _, err := os.Stat(fullPath); !os.IsNotExist(err) {
					t.Errorf("Expected file %s to be deleted, but it still exists", filePath)
				}
			}

			// Verify other files weren't affected (if any exist)
			// For example, the plural form should not be deleted
			pluralPath := filepath.Join(tempDir, ".github/aw/upgrade-agentic-workflows.md")
			if _, err := os.Stat(pluralPath); err == nil {
				// If it existed, it should still exist
				t.Logf("Correctly preserved .github/aw/upgrade-agentic-workflows.md (plural)")
			}
		})
	}
}

// TestDeleteOldTemplateFiles tests deletion of old template files
func TestDeleteOldTemplateFiles(t *testing.T) {
	tests := []struct {
		name             string
		filesToCreate    []string // Files to create in pkg/cli/templates/
		expectedDeleted  []string // Files that should be deleted
		expectDirRemoved bool     // Whether the templates directory should be removed
	}{
		{
			name: "deletes all old template files including agent file and removes directory",
			filesToCreate: []string{
				"agentic-workflows.agent.md",
				"create-agentic-workflow.md",
				"github-agentic-workflows.md",
			},
			expectedDeleted: []string{
				"agentic-workflows.agent.md",
				"create-agentic-workflow.md",
				"github-agentic-workflows.md",
			},
			expectDirRemoved: true,
		},
		{
			name: "deletes all template files",
			filesToCreate: []string{
				"agentic-workflows.agent.md",
				"create-agentic-workflow.md",
				"create-shared-agentic-workflow.md",
				"debug-agentic-workflow.md",
				"github-agentic-workflows.md",
				"serena-tool.md",
				"update-agentic-workflow.md",
				"upgrade-agentic-workflows.md",
			},
			expectedDeleted: []string{
				"agentic-workflows.agent.md",
				"create-agentic-workflow.md",
				"create-shared-agentic-workflow.md",
				"debug-agentic-workflow.md",
				"github-agentic-workflows.md",
				"serena-tool.md",
				"update-agentic-workflow.md",
				"upgrade-agentic-workflows.md",
			},
			expectDirRemoved: true,
		},
		{
			name:             "handles no files to delete",
			filesToCreate:    []string{},
			expectedDeleted:  []string{},
			expectDirRemoved: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a temporary directory for testing
			tempDir := testutil.TempDir(t, "test-*")

			// Change to temp directory and initialize git repo
			oldWd, _ := os.Getwd()
			defer func() {
				_ = os.Chdir(oldWd)
			}()
			err := os.Chdir(tempDir)
			if err != nil {
				t.Fatalf("Failed to change directory: %v", err)
			}

			// Initialize git repo
			if err := exec.Command("git", "init").Run(); err != nil {
				t.Fatalf("Failed to init git repo: %v", err)
			}

			// Create templates directory and files
			templatesDir := filepath.Join(tempDir, "pkg", "cli", "templates")
			if len(tt.filesToCreate) > 0 {
				if err := os.MkdirAll(templatesDir, 0755); err != nil {
					t.Fatalf("Failed to create templates directory: %v", err)
				}

				for _, file := range tt.filesToCreate {
					path := filepath.Join(templatesDir, file)
					if err := os.WriteFile(path, []byte("# Test template content"), 0644); err != nil {
						t.Fatalf("Failed to create file %s: %v", file, err)
					}
				}
			}

			// Call deleteOldTemplateFiles
			err = deleteOldTemplateFiles(false)
			if err != nil {
				t.Fatalf("deleteOldTemplateFiles() returned error: %v", err)
			}

			// Check that expected files were deleted
			for _, file := range tt.expectedDeleted {
				path := filepath.Join(templatesDir, file)
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Errorf("Expected file %s to be deleted, but it still exists", file)
				}
			}

			// Check if directory was removed
			if tt.expectDirRemoved {
				if _, err := os.Stat(templatesDir); !os.IsNotExist(err) {
					t.Errorf("Expected templates directory to be removed, but it still exists")
				}
			}
		})
	}
}

func TestBuildAgenticWorkflowsAgentContent(t *testing.T) {
	t.Parallel()
	tempDir := testutil.TempDir(t, "test-*")

	content, err := buildAgenticWorkflowsAgentContent(tempDir)
	if err != nil {
		t.Fatalf("buildAgenticWorkflowsAgentContent() returned error: %v", err)
	}

	expected := agenticWorkflowsAgentTemplate
	if content != expected {
		t.Fatalf("Expected exact agent content:\n%s\ngot:\n%s", expected, content)
	}
	if strings.Contains(content, ".github/skills/agentic-workflows/SKILL.md") {
		t.Fatalf("expected generated agent content to avoid skill cross-references:\n%s", content)
	}
	assert.Contains(t, content, ".github/aw/instructions.md")
	assert.Contains(t, content, "repository overlay instructions override defaults in this agent when they conflict")
}

func TestCheckedInAgenticWorkflowsAgentMatchesGeneratedContent(t *testing.T) {
	t.Parallel()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("Failed to locate test file")
	}

	gitRoot := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	expected, err := buildAgenticWorkflowsAgentContent(gitRoot)
	if err != nil {
		t.Fatalf("buildAgenticWorkflowsAgentContent() returned error: %v", err)
	}

	actual, err := os.ReadFile(filepath.Join(gitRoot, ".github", "agents", "agentic-workflows.md"))
	if err != nil {
		t.Fatalf("Failed to read checked-in agent file: %v", err)
	}

	if strings.TrimSpace(string(actual)) != strings.TrimSpace(expected) {
		t.Fatalf("Checked-in agent file is out of sync with generated content\nexpected:\n%s\nactual:\n%s", expected, string(actual))
	}
}

func TestBuildAgenticWorkflowsSkillContent(t *testing.T) {
	withMockAWMarkdownFileList(t, []string{"workflow-z.md", "workflow-a.md"}, nil)

	content, err := buildAgenticWorkflowsSkillContent()
	if err != nil {
		t.Fatalf("buildAgenticWorkflowsSkillContent() returned error: %v", err)
	}

	expected := strings.Replace(
		agenticWorkflowsSkillTemplate,
		agenticWorkflowsSkillFileListPlaceholder,
		"- `.github/aw/workflow-a.md`\n- `.github/aw/workflow-z.md`\n",
		1,
	)
	if content != expected {
		t.Fatalf("Expected exact skill content:\n%s\ngot:\n%s", expected, content)
	}
	if strings.Contains(content, ".github/agents/agentic-workflows") {
		t.Fatalf("expected generated skill content to avoid agent cross-references:\n%s", content)
	}
	assert.Contains(t, content, "Design workflows from scratch via interview: `.github/aw/designer.md`")
	assert.Contains(t, content, "Diagnose, patch, audit, or actively debug workflows: `.github/aw/debug-agentic-workflow.md`")
	assert.NotContains(t, content, "local-debug.md")
	assert.NotContains(t, content, "Debug, audit, or investigate workflows: `.github/aw/debug-agentic-workflow.md`")
	assert.Contains(t, content, agenticWorkflowsOTELSkillParagraph)
}

func TestUnifiedDebuggingInstructions(t *testing.T) {
	t.Parallel()
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok, "failed to locate test file")
	awDir := filepath.Join(filepath.Dir(file), "..", "..", ".github", "aw")
	content, err := os.ReadFile(filepath.Join(awDir, "debug-agentic-workflow.md"))
	require.NoError(t, err)
	for _, section := range []string{
		"## Choose the Mode",
		"## Strategy",
		"## Live Debug Loop",
		"## 403 and Codespaces Credential Triage",
		"## Development Compilation",
		"## Collect Existing Evidence",
		"## Identify the First Failing Boundary",
		"## Fix and Report",
	} {
		assert.Contains(t, string(content), section)
	}
	assert.Contains(t, string(content), "gh aw mcp inspect WORKFLOW --server NAME")
	assert.Contains(t, string(content), "gh aw audit RUN_ID RUN_ID_2 RUN_ID_3 --group --json")
	assert.Contains(t, string(content), "Explicit no-dispatch rules")
	assert.Contains(t, string(content), "No automatic dispatch retries")
	assert.Contains(t, string(content), "Docker unavailability does not block the gate")
	assert.Contains(t, string(content), "gh aw validate WORKFLOW")
	assert.Contains(t, string(content), "gh aw compile WORKFLOW --dry-run --zizmor --actionlint --poutine")
	_, err = os.Stat(filepath.Join(awDir, "local-debug.md"))
	assert.ErrorIs(t, err, os.ErrNotExist)
}

func TestBuildAgenticWorkflowsSkillContentEnsuresOTELParagraph(t *testing.T) {
	withMockAWMarkdownFileList(t, []string{"workflow-a.md"}, nil)

	originalTemplate := agenticWorkflowsSkillTemplate
	t.Cleanup(func() { agenticWorkflowsSkillTemplate = originalTemplate })
	agenticWorkflowsSkillTemplate = strings.ReplaceAll(agenticWorkflowsSkillTemplate, "\n"+agenticWorkflowsOTELSkillParagraph, "")

	content, err := buildAgenticWorkflowsSkillContent()
	require.NoError(t, err)
	assert.Contains(t, content, agenticWorkflowsOTELSkillParagraph)
}

func TestBuildAgenticWorkflowsSkillContentWithoutAWDirectory(t *testing.T) {
	withMockAWMarkdownFileList(t, []string{"workflow-a.md"}, nil)

	content, err := buildAgenticWorkflowsSkillContent()
	if err != nil {
		t.Fatalf("buildAgenticWorkflowsSkillContent() returned error: %v", err)
	}

	expected := strings.Replace(agenticWorkflowsSkillTemplate, agenticWorkflowsSkillFileListPlaceholder, "- `.github/aw/workflow-a.md`\n", 1)
	if content != expected {
		t.Fatalf("Expected exact skill content without .github/aw directory:\n%s\ngot:\n%s", expected, content)
	}
	if strings.Contains(content, agenticWorkflowsSkillFileListPlaceholder) {
		t.Fatalf("expected generated skill content to replace the file-list placeholder:\n%s", content)
	}
	if !strings.Contains(content, "- `.github/aw/workflow-a.md`") {
		t.Fatalf("expected generated skill content to include remotely sourced markdown files:\n%s", content)
	}
}

func TestBuildAgenticWorkflowsSkillContentFallsBackToEmbeddedFileList(t *testing.T) {
	withMockAWMarkdownFileList(t, nil, assert.AnError)

	content, err := buildAgenticWorkflowsSkillContent()
	require.NoError(t, err, "buildAgenticWorkflowsSkillContent() returned error")

	assert.NotContains(t, content, agenticWorkflowsSkillFileListPlaceholder, "expected generated skill content to replace the file-list placeholder")
	assert.Contains(t, content, "- `.github/aw/create-agentic-workflow.md`\n", "expected embedded fallback markdown file list to be used")
	assert.Contains(t, content, "- `.github/aw/designer.md`\n", "expected generated skill content to include designer instruction file")
	assert.Contains(t, content, "- `.github/aw/debug-agentic-workflow.md`\n", "expected unified debugging to remain discoverable without remote access")
	assert.NotContains(t, content, "local-debug.md")
}

func TestCheckedInAgenticWorkflowsSkillMatchesGeneratedContent(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("Failed to locate test file")
	}

	gitRoot := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	awEntries, err := os.ReadDir(filepath.Join(gitRoot, ".github", "aw"))
	require.NoError(t, err, "failed to read .github/aw for test fixture")
	awFiles := make([]string, 0, len(awEntries))
	for _, entry := range awEntries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		awFiles = append(awFiles, entry.Name())
	}
	sort.Strings(awFiles)
	withMockAWMarkdownFileList(t, awFiles, nil)

	expected, err := buildAgenticWorkflowsSkillContent()
	if err != nil {
		t.Fatalf("buildAgenticWorkflowsSkillContent() returned error: %v", err)
	}

	skillPath := filepath.Join(gitRoot, ".github", "skills", "agentic-workflows", "SKILL.md")
	actual, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatalf("Failed to read checked-in skill file: %v", err)
	}

	if strings.TrimSpace(string(actual)) != strings.TrimSpace(expected) {
		if writeErr := os.WriteFile(skillPath, []byte(strings.TrimRight(expected, "\n")+"\n"), 0644); writeErr != nil {
			t.Fatalf("Checked-in skill file is out of sync and auto-update failed (%v)\nexpected:\n%s\nactual:\n%s", writeErr, expected, string(actual))
		}
		t.Fatalf("Checked-in skill file was out of sync and has been regenerated; commit %s and re-run", skillPath)
	}
}

func TestCheckedInInteractiveAgentDesignerMentionsRepoOverlay(t *testing.T) {
	t.Parallel()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("Failed to locate test file")
	}

	gitRoot := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	actual, err := os.ReadFile(filepath.Join(gitRoot, ".github", "agents", "interactive-agent-designer.agent.md"))
	require.NoError(t, err, "Failed to read checked-in interactive agent designer file")

	content := string(actual)
	assert.Contains(t, content, ".github/aw/instructions.md")
	assert.Contains(t, content, "repository overlay instructions override defaults in this agent when they conflict")
}

// TestFallbackAWFilesMatchesLocalAWDirectory validates that the embedded fallback file list
// matches the .md files actually present in .github/aw/. This ensures that when files are
// added or removed from .github/aw/, the fallback list is kept in sync so that offline
// compilation still produces an accurate SKILL.md.
func TestFallbackAWFilesMatchesLocalAWDirectory(t *testing.T) {
	t.Parallel()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("Failed to locate test file")
	}

	gitRoot := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	awEntries, err := os.ReadDir(filepath.Join(gitRoot, ".github", "aw"))
	require.NoError(t, err, "failed to read .github/aw directory")

	localFiles := make([]string, 0, len(awEntries))
	for _, entry := range awEntries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		localFiles = append(localFiles, entry.Name())
	}
	sort.Strings(localFiles)

	fallbackFiles := embeddedFallbackAWMarkdownFiles()
	sort.Strings(fallbackFiles)

	if !assert.Equal(t, localFiles, fallbackFiles,
		"embedded fallback file list (pkg/cli/data/agentic_workflows_fallback_aw_files.json) is out of sync with .github/aw/*.md") {
		// Auto-update the fallback JSON so the developer only needs to commit the change.
		fallbackPath := filepath.Join(gitRoot, "pkg", "cli", "data", "agentic_workflows_fallback_aw_files.json")
		updated, encErr := json.MarshalIndent(localFiles, "", "  ")
		if encErr != nil {
			t.Logf("Auto-update failed (could not encode JSON): %v", encErr)
			return
		}
		if writeErr := os.WriteFile(fallbackPath, append(updated, '\n'), 0644); writeErr != nil {
			t.Logf("Auto-update failed (could not write file): %v", writeErr)
			return
		}
		t.Logf("Auto-updated %s; commit the file and re-run tests", fallbackPath)
	}
}

func withMockAWMarkdownFileList(t *testing.T, files []string, err error) {
	t.Helper()
	previous := listAgenticWorkflowsMarkdownFiles
	listAgenticWorkflowsMarkdownFiles = func(context.Context) ([]string, error) {
		// Return a copy so tests can't mutate shared backing arrays across invocations.
		return append([]string(nil), files...), err
	}
	t.Cleanup(func() {
		listAgenticWorkflowsMarkdownFiles = previous
	})
}
