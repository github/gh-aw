//go:build !integration

package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/githubnext/gh-aw/pkg/campaign"
	"github.com/githubnext/gh-aw/pkg/workflow"
)

func TestGenerateAndCompileCampaignOrchestrator(t *testing.T) {
	tmpDir := t.TempDir()

	campaignSpecPath := filepath.Join(tmpDir, "test-campaign.campaign.md")

	spec := &campaign.CampaignSpec{
		ID:          "test-campaign",
		Name:        "Test Campaign",
		Description: "A test campaign",
		Workflows:   []string{"example-workflow"},
		MemoryPaths: []string{"memory/campaigns/test-campaign/**"},
	}

	// Compiler with auto-detected version and action mode
	compiler := workflow.NewCompiler(
		workflow.WithSkipValidation(true),
		workflow.WithNoEmit(false),
		workflow.WithStrictMode(false),
	)

	orchestratorPath, err := generateAndCompileCampaignOrchestrator(GenerateCampaignOrchestratorOptions{
		Compiler:             compiler,
		Spec:                 spec,
		CampaignSpecPath:     campaignSpecPath,
		Verbose:              false,
		NoEmit:               false,
		RunZizmorPerFile:     false,
		RunPoutinePerFile:    false,
		RunActionlintPerFile: false,
		Strict:               false,
		ValidateActionSHAs:   false,
	})
	if err != nil {
		t.Fatalf("generateAndCompileCampaignOrchestrator() error: %v", err)
	}

	expectedPath := strings.TrimSuffix(campaignSpecPath, ".campaign.md") + ".campaign.g.md"
	if orchestratorPath != expectedPath {
		t.Fatalf("unexpected orchestrator path: got %q, want %q", orchestratorPath, expectedPath)
	}

	if _, statErr := os.Stat(orchestratorPath); statErr != nil {
		t.Fatalf("expected orchestrator markdown to exist, stat error: %v", statErr)
	}

	// For campaign orchestrators (*.campaign.g.md), the lock file should be *.campaign.g.lock.yml
	lockPath := strings.TrimSuffix(orchestratorPath, ".md") + ".lock.yml"
	if _, statErr := os.Stat(lockPath); statErr != nil {
		t.Fatalf("expected orchestrator lock file to exist at %s, stat error: %v", lockPath, statErr)
	}

	// Verify that the generated orchestrator has the required permissions
	lockContent, readErr := os.ReadFile(lockPath)
	if readErr != nil {
		t.Fatalf("failed to read lock file: %v", readErr)
	}
	lockStr := string(lockContent)

	if !strings.Contains(lockStr, "engine_id: \"claude\"") {
		t.Errorf("expected lock file to use claude engine, got: %s", lockPath)
	}

	requiredPermissions := []string{
		"contents: read",
		"issues: read",
	}

	for _, perm := range requiredPermissions {
		if !strings.Contains(lockStr, perm) {
			t.Errorf("expected lock file to contain permission %q", perm)
		}
	}

	// Note: Issue/project write operations are handled via safe-outputs which mint
	// app tokens with appropriate permissions, not direct workflow permissions.

	// Read the generated markdown file to verify the Source comment contains a relative path
	mdContent, readErr := os.ReadFile(orchestratorPath)
	if readErr != nil {
		t.Fatalf("failed to read generated markdown file: %v", readErr)
	}
	mdStr := string(mdContent)

	if !strings.Contains(mdStr, "engine: claude") {
		t.Errorf("expected generated markdown to set engine: claude")
	}

	// Verify dispatch-workflow safe output is rendered (used for orchestration)
	if !strings.Contains(mdStr, "dispatch-workflow:") {
		t.Errorf("expected generated markdown to include dispatch-workflow safe output")
	}
	if !strings.Contains(mdStr, "example-workflow") {
		t.Errorf("expected generated markdown to include allowlisted workflow 'example-workflow'")
	}

	// Verify that the Source comment exists and contains a relative path (not absolute)
	if !strings.Contains(mdStr, "<!-- Source:") {
		t.Errorf("expected generated markdown to contain Source comment")
	}

	// Extract the source path from the comment
	if strings.Contains(mdStr, "<!-- Source:") {
		// Find the source path in the comment
		startIdx := strings.Index(mdStr, "<!-- Source:")
		endIdx := strings.Index(mdStr[startIdx:], "-->")
		if endIdx > 0 {
			sourceComment := mdStr[startIdx : startIdx+endIdx]
			// Verify it's not an absolute path (no leading / or drive letter)
			if strings.Contains(sourceComment, "<!-- Source: /") ||
				(len(sourceComment) > 15 && sourceComment[13] == ':' && sourceComment[14] == '\\') {
				t.Errorf("Source comment contains absolute path: %q", sourceComment)
			}
			// Verify it contains the campaign filename
			if !strings.Contains(sourceComment, "test-campaign.campaign.md") {
				t.Errorf("Source comment doesn't contain expected filename: %q", sourceComment)
			}
		}
	}
}

// TestCampaignSourceCommentStability verifies that the source path in the generated
// campaign orchestrator is stable regardless of the current working directory
func TestCampaignSourceCommentStability(t *testing.T) {
	// Create a temporary git repository structure with .github directory
	tmpDir := t.TempDir()
	githubDir := filepath.Join(tmpDir, ".github", "workflows")
	if err := os.MkdirAll(githubDir, 0755); err != nil {
		t.Fatalf("failed to create .github/workflows directory: %v", err)
	}

	// Create a campaign spec in the .github/workflows directory
	campaignSpecPath := filepath.Join(githubDir, "test-campaign.campaign.md")

	spec := &campaign.CampaignSpec{
		ID:          "test-campaign",
		Name:        "Test Campaign",
		Description: "A test campaign for path stability",
		Workflows:   []string{"example-workflow"},
		MemoryPaths: []string{"memory/campaigns/test-campaign/**"},
	}

	compiler := workflow.NewCompiler(
		workflow.WithSkipValidation(true),
		workflow.WithNoEmit(false),
		workflow.WithStrictMode(false),
	)

	// Save original working directory
	originalWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get current working directory: %v", err)
	}
	defer func() {
		// Restore original working directory
		if err := os.Chdir(originalWd); err != nil {
			t.Logf("warning: failed to restore working directory: %v", err)
		}
	}()

	// Test 1: Generate from the tmp directory root
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to change to tmp directory: %v", err)
	}

	orchestratorPath1, err := generateAndCompileCampaignOrchestrator(GenerateCampaignOrchestratorOptions{
		Compiler:             compiler,
		Spec:                 spec,
		CampaignSpecPath:     campaignSpecPath,
		Verbose:              false,
		NoEmit:               false,
		RunZizmorPerFile:     false,
		RunPoutinePerFile:    false,
		RunActionlintPerFile: false,
		Strict:               false,
		ValidateActionSHAs:   false,
	})
	if err != nil {
		t.Fatalf("first generation error: %v", err)
	}

	mdContent1, err := os.ReadFile(orchestratorPath1)
	if err != nil {
		t.Fatalf("failed to read first generated file: %v", err)
	}
	sourcePath1 := extractSourcePath(t, string(mdContent1))

	// Test 2: Generate from a subdirectory
	pkgDir := filepath.Join(tmpDir, "pkg", "cli")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatalf("failed to create pkg/cli directory: %v", err)
	}
	if err := os.Chdir(pkgDir); err != nil {
		t.Fatalf("failed to change to pkg/cli directory: %v", err)
	}

	// Remove the previously generated file to regenerate it
	if err := os.Remove(orchestratorPath1); err != nil {
		t.Fatalf("failed to remove first generated file: %v", err)
	}

	orchestratorPath2, err := generateAndCompileCampaignOrchestrator(GenerateCampaignOrchestratorOptions{
		Compiler:             compiler,
		Spec:                 spec,
		CampaignSpecPath:     campaignSpecPath,
		Verbose:              false,
		NoEmit:               false,
		RunZizmorPerFile:     false,
		RunPoutinePerFile:    false,
		RunActionlintPerFile: false,
		Strict:               false,
		ValidateActionSHAs:   false,
	})
	if err != nil {
		t.Fatalf("second generation error: %v", err)
	}

	mdContent2, err := os.ReadFile(orchestratorPath2)
	if err != nil {
		t.Fatalf("failed to read second generated file: %v", err)
	}
	sourcePath2 := extractSourcePath(t, string(mdContent2))

	// Verify both paths are identical and stable
	if sourcePath1 != sourcePath2 {
		t.Errorf("source paths differ based on working directory:\n  from root: %q\n  from subdir: %q", sourcePath1, sourcePath2)
	}

	// Verify the path is normalized to .github/workflows/...
	expectedPath := ".github/workflows/test-campaign.campaign.md"
	if sourcePath1 != expectedPath {
		t.Errorf("expected stable path %q, got %q", expectedPath, sourcePath1)
	}
}

// extractSourcePath extracts the source path from a markdown file's source comment
func extractSourcePath(t *testing.T, content string) string {
	t.Helper()

	startMarker := "<!-- Source: "
	endMarker := " -->"

	startIdx := strings.Index(content, startMarker)
	if startIdx == -1 {
		t.Fatalf("source comment not found in content")
	}

	startIdx += len(startMarker)
	endIdx := strings.Index(content[startIdx:], endMarker)
	if endIdx == -1 {
		t.Fatalf("source comment end marker not found")
	}

	return strings.TrimSpace(content[startIdx : startIdx+endIdx])
}
