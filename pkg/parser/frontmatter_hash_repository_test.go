//go:build !integration

package parser

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAllRepositoryWorkflowHashes validates that all workflows in the repository
// can have their hashes computed successfully and produces a reference list
func TestAllRepositoryWorkflowHashes(t *testing.T) {
	// Find repository root
	repoRoot := findRepoRoot(t)
	workflowsDir := filepath.Join(repoRoot, ".github", "workflows")

	// Check if workflows directory exists
	if _, err := os.Stat(workflowsDir); os.IsNotExist(err) {
		t.Skip("Workflows directory not found, skipping test")
		return
	}

	// Find all workflow markdown files
	files, err := filepath.Glob(filepath.Join(workflowsDir, "*.md"))
	require.NoError(t, err, "Should list workflow files")

	if len(files) == 0 {
		t.Skip("No workflow files found")
		return
	}

	cache := NewImportCache(repoRoot)
	hashMap := make(map[string]string)

	t.Logf("Computing hashes for %d workflows:", len(files))

	for _, file := range files {
		workflowName := filepath.Base(file)

		hash, err := ComputeFrontmatterHashFromFile(file, cache)
		if err != nil {
			t.Logf("  ✗ %s: ERROR - %v", workflowName, err)
			continue
		}

		assert.Len(t, hash, 64, "Hash should be 64 characters for %s", workflowName)
		assert.Regexp(t, "^[a-f0-9]{64}$", hash, "Hash should be lowercase hex for %s", workflowName)

		hashMap[workflowName] = hash
		t.Logf("  ✓ %s: %s", workflowName, hash)

		// Verify determinism - compute again
		hash2, err := ComputeFrontmatterHashFromFile(file, cache)
		require.NoError(t, err, "Should compute hash again for %s", workflowName)
		assert.Equal(t, hash, hash2, "Hash should be deterministic for %s", workflowName)
	}

	t.Logf("\nSuccessfully computed hashes for %d workflows", len(hashMap))

	// Write hash reference file for cross-language validation
	referenceFile := filepath.Join(repoRoot, "tmp", "workflow-hashes-reference.txt")
	tmpDir := filepath.Dir(referenceFile)
	if err := os.MkdirAll(tmpDir, 0755); err == nil {
		f, err := os.Create(referenceFile)
		if err == nil {
			defer f.Close()
			for name, hash := range hashMap {
				f.WriteString(name + ": " + hash + "\n")
			}
			t.Logf("\nWrote hash reference to: %s", referenceFile)
		}
	}
}

// TestHashConsistencyAcrossLockFiles validates that hashes in lock files
// match the computed hashes from source markdown files
func TestHashConsistencyAcrossLockFiles(t *testing.T) {
	repoRoot := findRepoRoot(t)
	workflowsDir := filepath.Join(repoRoot, ".github", "workflows")

	// Check if workflows directory exists
	if _, err := os.Stat(workflowsDir); os.IsNotExist(err) {
		t.Skip("Workflows directory not found, skipping test")
		return
	}

	// Find all workflow markdown files
	mdFiles, err := filepath.Glob(filepath.Join(workflowsDir, "*.md"))
	require.NoError(t, err, "Should list workflow files")

	if len(mdFiles) == 0 {
		t.Skip("No workflow files found")
		return
	}

	cache := NewImportCache(repoRoot)
	checkedCount := 0

	for _, mdFile := range mdFiles {
		lockFile := mdFile[:len(mdFile)-3] + ".lock.yml"

		// Check if lock file exists
		if _, err := os.Stat(lockFile); os.IsNotExist(err) {
			continue // Skip if no lock file
		}

		// Compute hash from markdown
		computedHash, err := ComputeFrontmatterHashFromFile(mdFile, cache)
		require.NoError(t, err, "Should compute hash for %s", filepath.Base(mdFile))

		// Read hash from lock file
		lockContent, err := os.ReadFile(lockFile)
		require.NoError(t, err, "Should read lock file for %s", filepath.Base(lockFile))

		// Extract hash from lock file comment
		lockHash := extractHashFromLockFile(string(lockContent))

		if lockHash == "" {
			t.Logf("  ⚠ %s: No hash in lock file (may need recompilation)", filepath.Base(mdFile))
			continue
		}

		// Compare hashes
		if computedHash != lockHash {
			t.Errorf("  ✗ %s: Hash mismatch!\n    Computed: %s\n    Lock file: %s",
				filepath.Base(mdFile), computedHash, lockHash)
		} else {
			t.Logf("  ✓ %s: Hash matches", filepath.Base(mdFile))
		}

		checkedCount++
	}

	t.Logf("\nVerified hash consistency for %d workflows", checkedCount)
}

// extractHashFromLockFile extracts the frontmatter-hash from a lock file
func extractHashFromLockFile(content string) string {
	// Look for: # frontmatter-hash: <hash>
	lines := splitLines(content)
	for _, line := range lines {
		if len(line) > 20 && line[:20] == "# frontmatter-hash: " {
			return line[20:]
		}
	}
	return ""
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}
