//go:build integration

package workflow

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestThreatDetectionCombinations is the integration counterpart to the unit tests in
// safe_jobs_threat_detection_test.go. It compiles complete workflow files (mirroring the
// fixtures in pkg/cli/workflows/) and asserts that the generated lock YAML contains the
// correct job set and job conditions for every combination of:
//
//   - safe_outputs       — always present when safe-outputs is configured
//   - detection          — present when threat-detection is enabled (boolean or expression)
//   - push_repo_memory   — present when tools.repo-memory + detection enabled/conditional
//   - update_cache_memory — present when tools.cache-memory + detection enabled/conditional
//   - custom safe-jobs   — present when safe-outputs.jobs is configured
//
// Detection modes tested:
//
//	disabled   = explicit threat-detection: false
//	enabled    = threat-detection: true
//	expression = threat-detection: ${{ inputs.enable-threat-detection }}
func TestThreatDetectionCombinations(t *testing.T) {
	tests := []struct {
		name        string
		frontmatter string
		wantJobs    []string            // job keys that must appear in compiled YAML
		wantNotJobs []string            // job keys that must NOT appear
		wantInJobIf map[string][]string // job → strings that must appear in its if: block
	}{
		{
			name: "safe-outputs only, no explicit detection (auto-enabled)",
			frontmatter: `---
on: workflow_dispatch
permissions: read-all
engine: copilot
safe-outputs:
  create-issue:
    title-prefix: "[bot] "
---
Test workflow.
`,
			wantJobs:    []string{"safe_outputs", "detection"},
			wantNotJobs: []string{"push_repo_memory", "update_cache_memory"},
		},
		{
			name: "safe-outputs + threat-detection: true (explicit)",
			frontmatter: `---
on: workflow_dispatch
permissions: read-all
engine: copilot
safe-outputs:
  create-issue:
    title-prefix: "[bot] "
  threat-detection: true
---
Test workflow.
`,
			wantJobs:    []string{"safe_outputs", "detection"},
			wantNotJobs: []string{"push_repo_memory", "update_cache_memory"},
		},
		{
			name: "safe-outputs + threat-detection: false (disabled)",
			frontmatter: `---
on: workflow_dispatch
permissions: read-all
engine: copilot
safe-outputs:
  create-issue:
    title-prefix: "[bot] "
  threat-detection: false
---
Test workflow.
`,
			wantJobs:    []string{"safe_outputs"},
			wantNotJobs: []string{"detection", "push_repo_memory", "update_cache_memory"},
		},
		{
			name: "safe-outputs + expression threat-detection (conditional)",
			frontmatter: `---
on:
  workflow_call:
    inputs:
      enable-threat-detection:
        type: boolean
        default: true
permissions: read-all
engine: copilot
safe-outputs:
  create-issue:
    title-prefix: "[bot] "
  threat-detection: ${{ inputs.enable-threat-detection }}
---
Test workflow.
`,
			wantJobs:    []string{"safe_outputs", "detection"},
			wantNotJobs: []string{"push_repo_memory", "update_cache_memory"},
			wantInJobIf: map[string][]string{
				// detection job must reference the caller expression
				"detection": {"inputs.enable-threat-detection"},
				// safe_outputs must use always() and accept skipped detection
				"safe_outputs": {"always()", "'skipped'"},
			},
		},
		{
			// repo-memory without explicit safe-outputs (no detection) → push_repo_memory with no detection dep
			name: "repo-memory, no safe-outputs → push_repo_memory present, no detection",
			frontmatter: `---
on: workflow_dispatch
engine: copilot
tools:
  repo-memory: true
---
Test workflow.
`,
			wantJobs:    []string{"push_repo_memory"},
			wantNotJobs: []string{"detection", "update_cache_memory"},
		},
		{
			name: "repo-memory + threat-detection: true → push_repo_memory depends on detection",
			frontmatter: `---
on: workflow_dispatch
engine: copilot
tools:
  repo-memory: true
safe-outputs:
  create-issue:
    title-prefix: "[bot] "
  threat-detection: true
---
Test workflow.
`,
			wantJobs:    []string{"safe_outputs", "detection", "push_repo_memory"},
			wantNotJobs: []string{"update_cache_memory"},
			wantInJobIf: map[string][]string{
				"push_repo_memory": {"always()", "'skipped'"},
			},
		},
		{
			name: "repo-memory + threat-detection: false → push_repo_memory present, no detection",
			frontmatter: `---
on: workflow_dispatch
engine: copilot
tools:
  repo-memory: true
safe-outputs:
  create-issue:
    title-prefix: "[bot] "
  threat-detection: false
---
Test workflow.
`,
			wantJobs:    []string{"safe_outputs", "push_repo_memory"},
			wantNotJobs: []string{"detection", "update_cache_memory"},
		},
		{
			name: "repo-memory + expression detection → push_repo_memory condition accepts skipped",
			frontmatter: `---
on:
  workflow_call:
    inputs:
      enable-threat-detection:
        type: boolean
        default: true
engine: copilot
tools:
  repo-memory: true
safe-outputs:
  create-issue:
    title-prefix: "[bot] "
  threat-detection: ${{ inputs.enable-threat-detection }}
---
Test workflow.
`,
			wantJobs:    []string{"safe_outputs", "detection", "push_repo_memory"},
			wantNotJobs: []string{"update_cache_memory"},
			wantInJobIf: map[string][]string{
				"detection":        {"inputs.enable-threat-detection"},
				"push_repo_memory": {"always()", "'skipped'"},
			},
		},
		{
			name: "cache-memory + threat-detection: true → update_cache_memory requires detection success",
			frontmatter: `---
on: workflow_dispatch
permissions: read-all
engine: copilot
tools:
  cache-memory: true
safe-outputs:
  create-issue:
    title-prefix: "[bot] "
  threat-detection: true
---
Test workflow.
`,
			wantJobs:    []string{"safe_outputs", "detection", "update_cache_memory"},
			wantNotJobs: []string{"push_repo_memory"},
			wantInJobIf: map[string][]string{
				"update_cache_memory": {"always()", "needs.detection.result == 'success'"},
			},
		},
		{
			name: "cache-memory + threat-detection: false → no update_cache_memory job",
			frontmatter: `---
on: workflow_dispatch
permissions: read-all
engine: copilot
tools:
  cache-memory: true
safe-outputs:
  create-issue:
    title-prefix: "[bot] "
  threat-detection: false
---
Test workflow.
`,
			wantJobs:    []string{"safe_outputs"},
			wantNotJobs: []string{"detection", "update_cache_memory", "push_repo_memory"},
		},
		{
			name: "cache-memory + expression detection → update_cache_memory requires detection success",
			frontmatter: `---
on:
  workflow_call:
    inputs:
      enable-threat-detection:
        type: boolean
        default: true
permissions: read-all
engine: copilot
tools:
  cache-memory: true
safe-outputs:
  create-issue:
    title-prefix: "[bot] "
  threat-detection: ${{ inputs.enable-threat-detection }}
---
Test workflow.
`,
			wantJobs:    []string{"safe_outputs", "detection", "update_cache_memory"},
			wantNotJobs: []string{"push_repo_memory"},
			wantInJobIf: map[string][]string{
				"detection":           {"inputs.enable-threat-detection"},
				"update_cache_memory": {"always()", "needs.detection.result == 'success'"},
			},
		},
		{
			// safe-jobs (custom) are named by their job ID in the lock YAML, not "safe_jobs"
			name: "safe-jobs (custom) + expression detection → custom job condition accepts skipped",
			frontmatter: `---
on:
  workflow_call:
    inputs:
      enable-threat-detection:
        type: boolean
        default: true
permissions: read-all
engine: copilot
safe-outputs:
  threat-detection: ${{ inputs.enable-threat-detection }}
  jobs:
    summarize:
      runs-on: ubuntu-latest
      steps:
        - name: Print output
          run: echo done
---
Test workflow.
`,
			wantJobs:    []string{"detection", "summarize"},
			wantNotJobs: []string{"push_repo_memory", "update_cache_memory"},
			wantInJobIf: map[string][]string{
				"detection": {"inputs.enable-threat-detection"},
				"summarize": {"always()", "'skipped'"},
			},
		},
		{
			name: "repo-memory + cache-memory + expression detection (all memory jobs present)",
			frontmatter: `---
on:
  workflow_call:
    inputs:
      enable-threat-detection:
        type: boolean
        default: true
engine: copilot
tools:
  repo-memory: true
  cache-memory: true
safe-outputs:
  create-issue:
    title-prefix: "[bot] "
  threat-detection: ${{ inputs.enable-threat-detection }}
---
Test workflow.
`,
			wantJobs:    []string{"safe_outputs", "detection", "push_repo_memory", "update_cache_memory"},
			wantNotJobs: []string{},
			wantInJobIf: map[string][]string{
				"detection":           {"inputs.enable-threat-detection"},
				"push_repo_memory":    {"always()", "'skipped'"},
				"update_cache_memory": {"always()", "needs.detection.result == 'success'"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := testutil.TempDir(t, "threat-detection-combo-*")
			mdPath := filepath.Join(tmpDir, "test.md")
			lockPath := stringutil.MarkdownToLockFile(mdPath)

			require.NoError(t, os.WriteFile(mdPath, []byte(tt.frontmatter), 0o644))

			compiler := NewCompiler()
			require.NoError(t, compiler.CompileWorkflow(mdPath), "CompileWorkflow must succeed")

			rawBytes, err := os.ReadFile(lockPath)
			require.NoError(t, err, "lock file must be readable")
			yaml := string(rawBytes)

			// Verify expected jobs are present
			for _, job := range tt.wantJobs {
				assert.Contains(t, yaml, "  "+job+":",
					"compiled YAML should contain job %q", job)
			}

			// Verify absent jobs are not present
			for _, job := range tt.wantNotJobs {
				assert.NotContains(t, yaml, "  "+job+":",
					"compiled YAML should NOT contain job %q", job)
			}

			// Verify job if: conditions
			for job, wantSubstrings := range tt.wantInJobIf {
				jobSection := extractJobSection(yaml, job)
				require.NotEmpty(t, jobSection, "job section %q must be findable in compiled YAML", job)
				for _, sub := range wantSubstrings {
					assert.Contains(t, jobSection, sub,
						"job %q if: condition should contain %q", job, sub)
				}
			}

			if slices.Contains(tt.wantJobs, "detection") {
				detectionSection := extractJobSection(yaml, "detection")
				require.Equal(t, 1, strings.Count(detectionSection, "- name: Setup Node.js"),
					"detection job must contain exactly one Node.js setup step")
				assert.Less(t,
					strings.Index(detectionSection, "- name: Setup Node.js"),
					strings.Index(detectionSection, "- name: Install GitHub Copilot CLI"),
					"Node.js setup must precede Copilot installation")
			}
		})
	}
}

// TestWorkflowFilesCompile compiles each cli/workflows fixture that exercises
// threat detection variants and verifies no compilation error occurs.
// This ensures the .md files in pkg/cli/workflows/ are kept in sync with the compiler.
func TestWorkflowFilesCompile(t *testing.T) {
	workflowsDir := filepath.Join("..", "cli", "workflows")

	threatFiles := []string{
		"test-copilot-threat-detection-expression.md",
		"test-copilot-threat-detection-environment.md",
		"test-copilot-repo-memory-threat-detection.md",
		"test-copilot-repo-memory-threat-detection-expression.md",
		"test-copilot-cache-memory-threat-detection.md",
		"test-copilot-cache-memory-threat-detection-expression.md",
		"test-copilot-safe-jobs-threat-detection-expression.md",
	}

	for _, filename := range threatFiles {
		t.Run(filename, func(t *testing.T) {
			srcPath := filepath.Join(workflowsDir, filename)

			content, err := os.ReadFile(srcPath)
			require.NoError(t, err, "fixture %s must be readable", filename)

			// Compile into a temp dir so we don't modify the source tree
			tmpDir := testutil.TempDir(t, "compile-fixture-*")
			tmpMd := filepath.Join(tmpDir, filename)
			require.NoError(t, os.WriteFile(tmpMd, content, 0o644))

			compiler := NewCompiler()
			err = compiler.CompileWorkflow(tmpMd)
			require.NoError(t, err, "fixture %s must compile without errors", filename)

			lockPath := stringutil.MarkdownToLockFile(tmpMd)
			rawBytes, err := os.ReadFile(lockPath)
			require.NoError(t, err)
			yaml := string(rawBytes)

			// Expression-controlled detection files must produce a detection job
			if strings.Contains(filename, "expression") {
				assert.Contains(t, yaml, "  detection:",
					"file %s should produce a detection job", filename)

				// Detection job must reference the workflow_call input
				detectionSection := extractJobSection(yaml, string(constants.DetectionJobName))
				assert.Contains(t, detectionSection, "inputs.enable-threat-detection",
					"detection job in %s must reference the input expression", filename)
			}

			// Explicit threat-detection files (non-expression) must produce a detection job too
			if strings.Contains(filename, "threat-detection") && !strings.Contains(filename, "expression") {
				assert.Contains(t, yaml, "  detection:",
					"file %s should produce a detection job", filename)
			}

			// Environment fixture must propagate top-level environment to detection.
			if filename == "test-copilot-threat-detection-environment.md" {
				detectionSection := extractJobSection(yaml, string(constants.DetectionJobName))
				require.NotEmpty(t, detectionSection, "detection job section should be present")
				assert.Contains(t, detectionSection, "environment: production",
					"detection job in %s should inherit top-level environment", filename)
			}

			// Repo-memory files should produce push_repo_memory job
			if strings.Contains(filename, "repo-memory") {
				assert.Contains(t, yaml, "  push_repo_memory:",
					"file %s should produce a push_repo_memory job", filename)
			}

			// Cache-memory + threat-detection files should produce update_cache_memory job
			if strings.Contains(filename, "cache-memory") && strings.Contains(filename, "threat-detection") {
				assert.Contains(t, yaml, "  update_cache_memory:",
					"file %s should produce an update_cache_memory job", filename)
			}
		})
	}
}

// TestRepoMemoryWithThreatDetectionNeedsAndConditions tests push_repo_memory job
// graph position across all three detection modes.
// It must depend on detection and use always() so it still runs when detection
// is skipped at runtime (expression-controlled).
func TestRepoMemoryWithThreatDetectionNeedsAndConditions(t *testing.T) {
	cases := []struct {
		name              string
		threatDetection   string // value for safe-outputs.threat-detection
		wantDetectionJob  bool
		wantDetectionDep  bool // push_repo_memory should need detection
		wantAlwaysInCond  bool // push_repo_memory if: should use always()
		wantSkippedInCond bool // push_repo_memory if: should accept 'skipped'
	}{
		{
			name:              "boolean true",
			threatDetection:   "true",
			wantDetectionJob:  true,
			wantDetectionDep:  true,
			wantAlwaysInCond:  true,
			wantSkippedInCond: true,
		},
		{
			name:              "boolean false",
			threatDetection:   "false",
			wantDetectionJob:  false,
			wantDetectionDep:  false,
			wantAlwaysInCond:  true,
			wantSkippedInCond: false,
		},
		{
			name:              "expression",
			threatDetection:   "${{ inputs.enable-threat-detection }}",
			wantDetectionJob:  true,
			wantDetectionDep:  true,
			wantAlwaysInCond:  true,
			wantSkippedInCond: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			frontmatter := `---
on:
  workflow_call:
    inputs:
      enable-threat-detection:
        type: boolean
        default: true
engine: copilot
tools:
  repo-memory: true
safe-outputs:
  create-issue:
    title-prefix: "[bot] "
  threat-detection: ` + tc.threatDetection + `
---
Test workflow.
`
			tmpDir := testutil.TempDir(t, "repo-memory-td-*")
			mdPath := filepath.Join(tmpDir, "test.md")
			lockPath := stringutil.MarkdownToLockFile(mdPath)

			require.NoError(t, os.WriteFile(mdPath, []byte(frontmatter), 0o644))

			compiler := NewCompiler()
			require.NoError(t, compiler.CompileWorkflow(mdPath))

			rawBytes, err := os.ReadFile(lockPath)
			require.NoError(t, err)
			yaml := string(rawBytes)

			// Verify detection job presence
			assert.Equal(t, tc.wantDetectionJob, strings.Contains(yaml, "  detection:"),
				"detection job presence mismatch for threat-detection=%s", tc.threatDetection)

			// push_repo_memory must always be present (repo-memory is configured)
			assert.Contains(t, yaml, "  push_repo_memory:", "push_repo_memory job must always be present")

			pushSection := extractJobSection(yaml, "push_repo_memory")
			require.NotEmpty(t, pushSection)

			// Check detection in needs list
			hasDetectionDep := slices.Contains(
				extractJobNeeds(pushSection),
				string(constants.DetectionJobName),
			)
			assert.Equal(t, tc.wantDetectionDep, hasDetectionDep,
				"push_repo_memory detection dependency mismatch for threat-detection=%s", tc.threatDetection)

			// always() must be present in the condition
			if tc.wantAlwaysInCond {
				assert.Contains(t, pushSection, "always()",
					"push_repo_memory if: should use always() for threat-detection=%s", tc.threatDetection)
			}

			if tc.wantSkippedInCond {
				assert.Contains(t, pushSection, "'skipped'",
					"push_repo_memory if: should accept skipped detection for threat-detection=%s", tc.threatDetection)
			} else {
				assert.NotContains(t, pushSection, "'skipped'",
					"push_repo_memory if: should NOT reference skipped when detection disabled for threat-detection=%s", tc.threatDetection)
			}
		})
	}
}

// TestCacheMemoryWithThreatDetectionNeedsAndConditions tests update_cache_memory job
// graph position across all three detection modes.
// The job exists only when detection is enabled; its condition uses always()
// and requires detection success.
func TestCacheMemoryWithThreatDetectionNeedsAndConditions(t *testing.T) {
	cases := []struct {
		name              string
		threatDetection   string
		wantDetectionJob  bool
		wantCacheMemJob   bool // update_cache_memory only exists when detection is enabled/conditional
		wantDetectionDep  bool
		wantAlwaysInCond  bool
		wantSkippedInCond bool
	}{
		{
			name:              "boolean true",
			threatDetection:   "true",
			wantDetectionJob:  true,
			wantCacheMemJob:   true,
			wantDetectionDep:  true,
			wantAlwaysInCond:  true,
			wantSkippedInCond: false,
		},
		{
			name:             "boolean false",
			threatDetection:  "false",
			wantDetectionJob: false,
			wantCacheMemJob:  false,
		},
		{
			name:              "expression",
			threatDetection:   "${{ inputs.enable-threat-detection }}",
			wantDetectionJob:  true,
			wantCacheMemJob:   true,
			wantDetectionDep:  true,
			wantAlwaysInCond:  true,
			wantSkippedInCond: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			frontmatter := `---
on:
  workflow_call:
    inputs:
      enable-threat-detection:
        type: boolean
        default: true
permissions: read-all
engine: copilot
tools:
  cache-memory: true
safe-outputs:
  create-issue:
    title-prefix: "[bot] "
  threat-detection: ` + tc.threatDetection + `
---
Test workflow.
`
			tmpDir := testutil.TempDir(t, "cache-memory-td-*")
			mdPath := filepath.Join(tmpDir, "test.md")
			lockPath := stringutil.MarkdownToLockFile(mdPath)

			require.NoError(t, os.WriteFile(mdPath, []byte(frontmatter), 0o644))

			compiler := NewCompiler()
			require.NoError(t, compiler.CompileWorkflow(mdPath))

			rawBytes, err := os.ReadFile(lockPath)
			require.NoError(t, err)
			yaml := string(rawBytes)

			assert.Equal(t, tc.wantDetectionJob, strings.Contains(yaml, "  detection:"),
				"detection job presence mismatch for threat-detection=%s", tc.threatDetection)

			assert.Equal(t, tc.wantCacheMemJob, strings.Contains(yaml, "  update_cache_memory:"),
				"update_cache_memory presence mismatch for threat-detection=%s", tc.threatDetection)

			if !tc.wantCacheMemJob {
				return
			}

			cacheSection := extractJobSection(yaml, "update_cache_memory")
			require.NotEmpty(t, cacheSection)

			hasDetectionDep := slices.Contains(
				extractJobNeeds(cacheSection),
				string(constants.DetectionJobName),
			)
			assert.Equal(t, tc.wantDetectionDep, hasDetectionDep,
				"update_cache_memory detection dependency mismatch for threat-detection=%s", tc.threatDetection)

			if tc.wantAlwaysInCond {
				assert.Contains(t, cacheSection, "always()",
					"update_cache_memory if: should use always() for threat-detection=%s", tc.threatDetection)
			}
			if tc.wantSkippedInCond {
				assert.Contains(t, cacheSection, "'skipped'",
					"update_cache_memory if: should accept skipped detection for threat-detection=%s", tc.threatDetection)
			} else {
				assert.NotContains(t, cacheSection, "'skipped'",
					"update_cache_memory if: should not accept skipped detection for threat-detection=%s", tc.threatDetection)
			}
		})
	}
}

// extractJobNeeds parses the needs: list from a YAML job section string.
// It handles both single-line ("needs: [a, b]") and multi-line ("needs:\n  - a\n  - b") forms.
func extractJobNeeds(jobSection string) []string {
	var needs []string
	lines := strings.Split(jobSection, "\n")
	inNeeds := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		rest, ok := strings.CutPrefix(trimmed, "needs:")
		if ok {
			rest = strings.TrimSpace(rest)
			if strings.HasPrefix(rest, "[") {
				// Single-line form: needs: [a, b, c]
				rest = strings.Trim(rest, "[]")
				for part := range strings.SplitSeq(rest, ",") {
					needs = append(needs, strings.TrimSpace(part))
				}
			} else if rest != "" {
				// Single scalar: needs: agent
				needs = append(needs, rest)
			}
			inNeeds = true
			continue
		}
		if inNeeds {
			if item, hasPrefix := strings.CutPrefix(trimmed, "- "); hasPrefix {
				needs = append(needs, item)
			} else if trimmed != "" && !strings.HasPrefix(line, "    ") {
				inNeeds = false
			}
		}
	}
	return needs
}
