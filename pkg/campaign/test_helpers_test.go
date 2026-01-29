//go:build !integration

package campaign

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func withTempGitRepoWithInstalledCampaignPrompts(t *testing.T, run func(repoRoot string)) {
	t.Helper()

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get current directory: %v", err)
	}

	repoRoot := t.TempDir()

	if err := os.MkdirAll(filepath.Join(repoRoot, ".github", "aw"), 0o755); err != nil {
		t.Fatalf("failed to create .github/aw directory: %v", err)
	}

	srcTemplatesDir := filepath.Clean(filepath.Join(originalDir, "..", "cli", "templates"))
	installed := map[string]string{
		"generate-agentic-campaign.md":         "generate-agentic-campaign.md",
		"orchestrate-agentic-campaign.md":      "orchestrate-agentic-campaign.md",
		"execute-agentic-campaign-workflow.md": "execute-agentic-campaign-workflow.md",
		"update-agentic-campaign-project.md":   "update-agentic-campaign-project.md",
		"close-agentic-campaign.md":            "close-agentic-campaign.md",
	}

	for srcName, dstName := range installed {
		srcPath := filepath.Join(srcTemplatesDir, srcName)
		dstPath := filepath.Join(repoRoot, ".github", "aw", dstName)
		content, err := os.ReadFile(srcPath)
		if err != nil {
			t.Fatalf("failed to read template %s: %v", srcPath, err)
		}
		if err := os.WriteFile(dstPath, content, 0o644); err != nil {
			t.Fatalf("failed to write installed prompt %s: %v", dstPath, err)
		}
	}

	cmd := exec.Command("git", "init")
	cmd.Dir = repoRoot
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to init git repo: %v (output: %s)", err, string(out))
	}

	if err := os.Chdir(repoRoot); err != nil {
		t.Fatalf("failed to chdir to temp repo: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(originalDir); err != nil {
			panic(fmt.Sprintf("failed to restore working dir: %v", err))
		}
	})

	run(repoRoot)
}
