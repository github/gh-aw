//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"
)

func TestImportedCacheGeneratesStepsAndEnablesCacheWrites(t *testing.T) {
	tmpDir := t.TempDir()
	sharedDir := filepath.Join(tmpDir, ".github", "workflows", "shared")
	if err := os.MkdirAll(sharedDir, 0755); err != nil {
		t.Fatal(err)
	}

	sharedWorkflow := `---
cache:
  key: probe-${{ runner.os }}-v1
  path:
    - ~/probe-dir
---
`
	if err := os.WriteFile(filepath.Join(sharedDir, "cache.md"), []byte(sharedWorkflow), 0644); err != nil {
		t.Fatal(err)
	}

	mainWorkflow := `---
on: workflow_dispatch
permissions:
  contents: read
safe-outputs:
  add-comment:
imports:
  - shared/cache.md
cache:
  key: local-${{ runner.os }}-v1
  path: ./local-cache
engine: claude
strict: false
---
Say hello in a comment.
`
	mainPath := filepath.Join(tmpDir, ".github", "workflows", "imported.md")
	if err := os.MkdirAll(filepath.Dir(mainPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mainPath, []byte(mainWorkflow), 0644); err != nil {
		t.Fatal(err)
	}

	if err := NewCompiler().CompileWorkflow(mainPath); err != nil {
		t.Fatalf("CompileWorkflow() error = %v", err)
	}
	lockContent, err := os.ReadFile(stringutil.MarkdownToLockFile(mainPath))
	if err != nil {
		t.Fatal(err)
	}

	lock := string(lockContent)
	if got := strings.Count(lock, "uses: actions/cache"); got != 4 {
		t.Errorf("expected cache and restore steps for both configurations, got %d actions/cache steps", got)
	}
	if !strings.Contains(lock, "key: probe-${{ runner.os }}-v1") {
		t.Error("compiled workflow is missing the imported cache key")
	}
	if !strings.Contains(lock, "key: local-${{ runner.os }}-v1") {
		t.Error("compiled workflow is missing the main workflow cache key")
	}
	if strings.Contains(lock, "cache-mode: read") {
		t.Error("agent job should not remain read-only when a dependency cache is configured")
	}
}
