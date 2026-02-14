//go:build integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCacheMemoryWithThreatDetection verifies that when threat detection is enabled,
// cache-memory uses actions/cache/restore instead of actions/cache and creates
// an update_cache_memory job to save the cache after detection succeeds
func TestCacheMemoryWithThreatDetection(t *testing.T) {
	tests := []struct {
		name              string
		frontmatter       string
		expectedInLock    []string
		notExpectedInLock []string
	}{
		{
			name: "cache-memory with threat detection enabled",
			frontmatter: `---
name: Test Cache Memory with Threat Detection
on: workflow_dispatch
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: claude
tools:
  cache-memory: true
  github:
    allowed: [get_file_contents]
safe-outputs:
  create-issue:
  threat-detection: true
---

Test workflow with cache-memory and threat detection enabled.`,
			expectedInLock: []string{
				// In agent job, should use actions/cache/restore instead of actions/cache
				"- name: Restore cache-memory file share data",
				"uses: actions/cache/restore@0057852bfaa89a56745cba8c7296529d2fc39830",
				"key: memory-${{ env.GH_AW_WORKFLOW_ID_SANITIZED }}-${{ github.run_id }}",
				// Should upload artifact with if: always()
				"- name: Upload cache-memory data as artifact",
				"uses: actions/upload-artifact@",
				"if: always()",
				"name: cache-memory",
				// Should have update_cache_memory job
				"update_cache_memory:",
				"- agent",
				"- detection",
				"if: always() && needs.detection.outputs.success == 'true'",
				"- name: Download cache-memory artifact (default)",
				"- name: Save cache-memory to cache (default)",
				"uses: actions/cache/save@0057852bfaa89a56745cba8c7296529d2fc39830",
			},
			notExpectedInLock: []string{
				// Should NOT use regular actions/cache in agent job
				"- name: Cache cache-memory file share data\n      uses: actions/cache@",
			},
		},
		{
			name: "cache-memory without threat detection",
			frontmatter: `---
name: Test Cache Memory without Threat Detection
on: workflow_dispatch
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: claude
tools:
  cache-memory: true
  github:
    allowed: [get_file_contents]
---

Test workflow with cache-memory but no threat detection.`,
			expectedInLock: []string{
				// Without threat detection, should use regular actions/cache
				"- name: Cache cache-memory file share data",
				"uses: actions/cache@0057852bfaa89a56745cba8c7296529d2fc39830",
				"key: memory-${{ env.GH_AW_WORKFLOW_ID_SANITIZED }}-${{ github.run_id }}",
			},
			notExpectedInLock: []string{
				// Should NOT upload artifact when detection is disabled
				"- name: Upload cache-memory data as artifact",
				// Should NOT have update_cache_memory job
				"update_cache_memory:",
				// Should NOT use restore action
				"uses: actions/cache/restore@",
			},
		},
		{
			name: "multiple cache-memory with threat detection",
			frontmatter: `---
name: Test Multiple Cache Memory with Threat Detection
on: workflow_dispatch
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: claude
tools:
  cache-memory:
    - id: default
      key: memory-default
    - id: session
      key: memory-session
  github:
    allowed: [get_file_contents]
safe-outputs:
  create-issue:
  threat-detection: true
---

Test workflow with multiple cache-memory and threat detection enabled.`,
			expectedInLock: []string{
				// Both caches should use restore
				"- name: Restore cache-memory file share data (default)",
				"uses: actions/cache/restore@0057852bfaa89a56745cba8c7296529d2fc39830",
				"key: memory-default-${{ github.run_id }}",
				"- name: Restore cache-memory file share data (session)",
				"key: memory-session-${{ github.run_id }}",
				// Should upload both artifacts with if: always()
				"- name: Upload cache-memory data as artifact (default)",
				"if: always()",
				"name: cache-memory-default",
				"- name: Upload cache-memory data as artifact (session)",
				"name: cache-memory-session",
				// Should have update_cache_memory job with both caches
				"update_cache_memory:",
				"- name: Download cache-memory artifact (default)",
				"- name: Save cache-memory to cache (default)",
				"- name: Download cache-memory artifact (session)",
				"- name: Save cache-memory to cache (session)",
			},
			notExpectedInLock: []string{
				// Should NOT use regular actions/cache
				"- name: Cache cache-memory file share data (default)",
			},
		},
		{
			name: "restore-only cache-memory with threat detection",
			frontmatter: `---
name: Test Restore-Only Cache Memory with Threat Detection
on: workflow_dispatch
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: claude
tools:
  cache-memory:
    - id: default
      key: memory-restore-only
      restore-only: true
  github:
    allowed: [get_file_contents]
safe-outputs:
  create-issue:
  threat-detection: true
---

Test workflow with restore-only cache-memory and threat detection enabled.`,
			expectedInLock: []string{
				// Should use restore for restore-only cache (no ID suffix for single default cache)
				"- name: Restore cache-memory file share data",
				"uses: actions/cache/restore@0057852bfaa89a56745cba8c7296529d2fc39830",
			},
			notExpectedInLock: []string{
				// Should NOT upload artifact for restore-only
				"- name: Upload cache-memory data as artifact",
				// Should NOT have update_cache_memory job for restore-only
				"update_cache_memory:",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a temporary directory for the test
			tmpDir := t.TempDir()
			mdPath := filepath.Join(tmpDir, "test.md")
			lockPath := filepath.Join(tmpDir, "test.lock.yml")

			// Write markdown to temp file
			if err := os.WriteFile(mdPath, []byte(tt.frontmatter), 0644); err != nil {
				t.Fatalf("Failed to write markdown file: %v", err)
			}

			// Compile the workflow
			compiler := NewCompiler()
			if err := compiler.CompileWorkflow(mdPath); err != nil {
				t.Fatalf("Failed to compile workflow: %v", err)
			}

			// Read the generated lock file
			lockYAML, err := os.ReadFile(lockPath)
			if err != nil {
				t.Fatalf("Failed to read lock file: %v", err)
			}
			lockContent := string(lockYAML)

			// Check expected strings
			for _, expected := range tt.expectedInLock {
				if !strings.Contains(lockContent, expected) {
					t.Errorf("Expected lock YAML to contain %q, but it didn't.\nGenerated YAML:\n%s", expected, lockContent)
				}
			}

			// Check not expected strings
			for _, notExpected := range tt.notExpectedInLock {
				if strings.Contains(lockContent, notExpected) {
					t.Errorf("Expected lock YAML NOT to contain %q, but it did.\nGenerated YAML:\n%s", notExpected, lockContent)
				}
			}
		})
	}
}
