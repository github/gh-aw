//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestChaosPRBundleFuzzerSinglePRContract(t *testing.T) {
	path := filepath.Join("..", "..", ".github", "workflows", "chaos-pr-bundle-fuzzer.md")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	workflow := string(content)
	for _, required := range []string{
		"max: 1",
		"exactly one scenario pull request",
		"one randomized persona",
		"Record the selected scenario and summary stats in cache-memory before requesting the PR",
		"`create_pull_request` exactly once",
		"After calling `create_pull_request`, stop without making another PR call.",
	} {
		if !strings.Contains(workflow, required) {
			t.Errorf("fuzzer workflow must contain %q", required)
		}
	}
	for _, obsolete := range []string{"five scenario pull requests", "5 PRs per run", "generate 5 PR scenarios"} {
		if strings.Contains(workflow, obsolete) {
			t.Errorf("fuzzer workflow must not contain obsolete instruction %q", obsolete)
		}
	}
}
