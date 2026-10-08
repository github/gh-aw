//go:build integration

package workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/stringutil"

	"github.com/github/gh-aw/pkg/testutil"
)

func TestRuntimeSetupUVHostEnvIntegration(t *testing.T) {
	topologies := []struct {
		name        string
		frontmatter string
		marker      string
	}{
		{name: "docker"},
		{
			name:        "cloud-hypervisor",
			frontmatter: "sandbox:\n  agent:\n    runtime: cloud-hypervisor\n",
			marker:      "--container-runtime cloud-hypervisor",
		},
		{
			name:        "arc-dind",
			frontmatter: "runs-on: arc-scale-set\nrunner:\n  topology: arc-dind\n",
			marker:      "patch_awf_chroot_config.cjs",
		},
	}
	scenarios := []struct {
		name        string
		frontmatter string
		steps       string
		wantCache   bool
		wantPython  bool
		wantSetup   bool
	}{
		{
			name:      "generated",
			steps:     "  - run: uv sync\n",
			wantCache: true, wantPython: true, wantSetup: true,
		},
		{
			name:      "custom without detected uv",
			steps:     "  - uses: astral-sh/setup-uv@v5\n    with:\n      version: '0.8.0'\n",
			wantCache: true, wantPython: true, wantSetup: true,
		},
		{
			name:      "custom preserved after deduplication",
			steps:     "  - uses: astral-sh/setup-uv@v5\n    with:\n      version: '0.8.0'\n  - run: uv sync\n",
			wantCache: true, wantPython: true, wantSetup: true,
		},
		{
			name:      "custom replaced after deduplication",
			steps:     "  - uses: astral-sh/setup-uv@v5\n  - run: uv sync\n",
			wantCache: true, wantPython: true, wantSetup: true,
		},
		{
			name:        "custom pre-step",
			frontmatter: "pre-steps:\n  - uses: astral-sh/setup-uv@v5\n",
			steps:       "  - run: echo ready\n",
			wantCache:   true, wantPython: true, wantSetup: true,
		},
		{
			name:        "custom pre-agent step",
			frontmatter: "pre-agent-steps:\n  - uses: astral-sh/setup-uv@v5\n",
			steps:       "  - run: echo ready\n",
			wantCache:   true, wantPython: true, wantSetup: true,
		},
		{
			name:        "frontmatter override",
			frontmatter: "env:\n  UV_CACHE_DIR: /cache\n",
			steps:       "  - run: uv sync\n",
			wantPython:  true, wantSetup: true,
		},
		{
			name:        "engine override",
			frontmatter: "engine:\n  id: copilot\n  env:\n    UV_PYTHON_INSTALL_DIR: /python\n",
			steps:       "  - run: uv sync\n",
			wantCache:   true, wantSetup: true,
		},
		{
			name:        "explicit exclusion beats override",
			frontmatter: "env:\n  UV_CACHE_DIR: /cache\nexcluded-env:\n  - UV_CACHE_DIR\n",
			steps:       "  - run: uv sync\n",
			wantCache:   true, wantPython: true, wantSetup: true,
		},
		{
			name:        "frontmatter secret remains excluded",
			frontmatter: "env:\n  UV_CACHE_DIR: ${{ secrets.CACHE }}\n",
			steps:       "  - run: uv sync\n",
			wantCache:   true, wantPython: true, wantSetup: true,
		},
		{
			name:        "engine secret remains excluded",
			frontmatter: "engine:\n  id: copilot\n  env:\n    UV_CACHE_DIR: ${{ secrets.CACHE }}\n",
			steps:       "  - run: uv sync\n",
			wantCache:   true, wantPython: true, wantSetup: true,
		},
		{
			name:  "impostor action",
			steps: "  - uses: astral-sh/setup-uv-impostor@v5\n",
		},
		{
			name:  "no setup",
			steps: "  - run: echo ready\n",
		},
	}
	for _, topology := range topologies {
		for _, scenario := range scenarios {
			t.Run(topology.name+"/"+scenario.name, func(t *testing.T) {
				engine := "engine: copilot\n"
				if strings.HasPrefix(scenario.frontmatter, "engine:") {
					engine = ""
				}
				markdown := "---\non:\n  workflow_dispatch:\nstrict: false\n" + engine +
					topology.frontmatter + scenario.frontmatter + "steps:\n" + scenario.steps + "---\n\n# Test uv host paths\n"
				lock, agentRun := compileUVHostEnvWorkflow(t, markdown)
				require.Contains(t, agentRun, "--env-all")
				for name, excluded := range map[string]bool{
					"UV_CACHE_DIR": scenario.wantCache, "UV_PYTHON_INSTALL_DIR": scenario.wantPython,
				} {
					assert.Equal(t, excluded, strings.Contains(agentRun, "--exclude-env "+name), name)
					assert.LessOrEqual(t, strings.Count(agentRun, "--exclude-env "+name), 1, name)
				}
				if scenario.wantSetup {
					assert.Equal(t, 1, countInNonCommentLines(lock, "uses: astral-sh/setup-uv@"))
				} else {
					assert.Zero(t, countInNonCommentLines(lock, "uses: astral-sh/setup-uv@"))
				}
				if topology.marker != "" {
					assert.Contains(t, lock, topology.marker)
				}
				if topology.name == "cloud-hypervisor" {
					assert.Contains(t, agentRun, `/workspace/.awf-home`)
					assert.NotContains(t, agentRun, "--exclude-env HOME")
				}
			})
		}
	}
}

func TestRuntimeSetupUVSandboxEnvOverrideIntegration(t *testing.T) {
	for _, runtime := range []string{"docker", "cloud-hypervisor", "arc-dind"} {
		t.Run(runtime, func(t *testing.T) {
			runner := ""
			agentRuntime := runtime
			if runtime == "arc-dind" {
				runner = "runs-on: arc-scale-set\nrunner:\n  topology: arc-dind\n"
				agentRuntime = "docker"
			}
			markdown := "---\non:\n  workflow_dispatch:\nstrict: false\nengine: copilot\nsandbox:\n  agent:\n    runtime: " +
				agentRuntime + "\n    env:\n      UV_CACHE_DIR: /cache\n      UV_PYTHON_INSTALL_DIR: /python\n" + runner +
				"steps:\n  - run: uv sync\n---\n\n# Test uv overrides\n"
			_, run := compileUVHostEnvWorkflow(t, markdown)
			assert.NotContains(t, run, "--exclude-env UV_CACHE_DIR")
			assert.NotContains(t, run, "--exclude-env UV_PYTHON_INSTALL_DIR")
		})
	}
}

func TestRuntimeSetupUVExcludeEnvVersionGateIntegration(t *testing.T) {
	require.Equal(t, "v0.25.3", string(constants.AWFExcludeEnvMinVersion))
	for _, version := range []string{"v0.25.2", "v0.25.3"} {
		t.Run(version, func(t *testing.T) {
			markdown := "---\non:\n  workflow_dispatch:\nstrict: false\nengine: copilot\nsandbox:\n  agent:\n    version: " +
				version + "\nsteps:\n  - run: uv sync\n---\n\n# Test uv version gate\n"
			_, run := compileUVHostEnvWorkflow(t, markdown)
			for _, name := range []string{"UV_CACHE_DIR", "UV_PYTHON_INSTALL_DIR"} {
				assert.Equal(t, version == "v0.25.3", strings.Contains(run, "--exclude-env "+name))
			}
		})
	}
}

func TestRuntimeSetupUVCacheValidationIntegration(t *testing.T) {
	scenarios := []struct {
		name       string
		with       string
		runCommand string
	}{
		{
			name:       "custom version preserved after deduplication",
			with:       "      version: '0.8.0'\n",
			runCommand: "  - run: uv sync\n",
		},
		{
			name: "custom version without detected uv",
			with: "      version: '0.8.0'\n",
		},
		{name: "setup action without detected uv"},
	}
	const cacheDiagnostic = "Actions caches can save agent-written files"
	for _, scenario := range scenarios {
		for _, strict := range []bool{true, false} {
			for _, disabled := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/strict=%t/cache-disabled=%t", scenario.name, strict, disabled), func(t *testing.T) {
					with := scenario.with
					if disabled {
						with += "      enable-cache: false\n"
					}
					steps := "steps:\n  - name: Custom uv setup\n    uses: astral-sh/setup-uv@v5\n"
					if with != "" {
						steps += "    with:\n" + with
					}
					steps += scenario.runCommand
					markdown := fmt.Sprintf("---\non:\n  workflow_dispatch:\nengine: copilot\nstrict: %t\n%s---\n\n# Test uv cache validation\n", strict, steps)
					dir := testutil.TempDir(t, "uv-cache-validation-*")
					path := filepath.Join(dir, "workflow.md")
					require.NoError(t, os.WriteFile(path, []byte(markdown), 0o644))
					compiler := NewCompiler()
					var compileErr error
					stderr := testutil.CaptureStderr(t, func() {
						compileErr = compiler.CompileWorkflow(path)
					})
					if strict && !disabled {
						require.ErrorContains(t, compileErr, "strict mode:")
						assert.ErrorContains(t, compileErr, cacheDiagnostic)
						assert.ErrorContains(t, compileErr, "Custom uv setup")
						assert.ErrorContains(t, compileErr, "enable-cache: false")
						_, err := os.Stat(stringutil.MarkdownToLockFile(path))
						assert.True(t, os.IsNotExist(err), "rejected workflow must not produce a lock file")
						return
					}
					require.NoError(t, compileErr)
					if disabled {
						assert.NotContains(t, stderr, cacheDiagnostic)
					} else {
						assert.Contains(t, stderr, cacheDiagnostic)
						assert.Contains(t, stderr, "Custom uv setup")
						assert.Contains(t, stderr, "enable-cache: false")
						assert.Positive(t, compiler.GetWarningCount())
					}
					content, err := os.ReadFile(stringutil.MarkdownToLockFile(path))
					require.NoError(t, err)
					assert.Equal(t, 1, countInNonCommentLines(string(content), "uses: astral-sh/setup-uv@"))
					if scenario.with != "" {
						assert.Contains(t, string(content), "0.8.0", "preserved custom setup must retain its version")
					}
				})
			}
		}
	}
}

func compileUVHostEnvWorkflow(t *testing.T, markdown string) (string, string) {
	t.Helper()
	dir := testutil.TempDir(t, "uv-host-env-*")
	path := filepath.Join(dir, "workflow.md")
	require.NoError(t, os.WriteFile(path, []byte(markdown), 0o644))
	require.NoError(t, NewCompiler().CompileWorkflow(path))
	content, err := os.ReadFile(stringutil.MarkdownToLockFile(path))
	require.NoError(t, err)
	var workflow struct {
		Jobs map[string]struct {
			Steps []struct {
				Run string `yaml:"run"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	require.NoError(t, yaml.Unmarshal(content, &workflow))
	var agentRuns []string
	for _, step := range workflow.Jobs["agent"].Steps {
		if strings.Contains(step.Run, "--env-all") {
			agentRuns = append(agentRuns, step.Run)
		}
	}
	require.Len(t, agentRuns, 1)
	return string(content), agentRuns[0]
}

// countInNonCommentLines counts occurrences of a string in non-comment lines
// A comment line is one that starts with '#' (after trimming leading whitespace)
func countInNonCommentLines(content, search string) int {
	count := 0
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		trimmed := strings.TrimLeft(line, " \t")
		// Skip comment lines
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		count += strings.Count(line, search)
	}
	return count
}

// Note: indexInNonCommentLines is defined in compiler_test.go

func TestRuntimeSetupIntegration(t *testing.T) {
	tests := []struct {
		name             string
		workflowMarkdown string
		expectSetup      []string
		notExpectSetup   []string
	}{
		{
			name: "auto-detects node from npm command",
			workflowMarkdown: `---
on: push
engine: copilot
steps:
  - name: Install dependencies
    run: npm install
---

# Test workflow`,
			expectSetup: []string{
				"Setup Node.js",
				"uses: actions/setup-node@", // SHA varies
				"node-version: '24'",
			},
		},
		{
			name: "auto-detects python from python command",
			workflowMarkdown: `---
on: push
engine: copilot
steps:
  - name: Run script
    run: python test.py
---

# Test workflow`,
			expectSetup: []string{
				"Setup Python",
				"uses: actions/setup-python@", // SHA varies
				"python-version: '3.12'",
			},
		},
		{
			name: "auto-detects uv from uvx command",
			workflowMarkdown: `---
on: push
engine: copilot
steps:
  - name: Run ruff
    run: uvx ruff check
---

# Test workflow`,
			expectSetup: []string{
				"Setup uv",
				"uses: astral-sh/setup-uv@", // SHA varies
			},
		},
		{
			name: "auto-detects multiple runtimes",
			workflowMarkdown: `---
on: push
engine: copilot
steps:
  - name: Install
    run: npm install
  - name: Test
    run: python test.py
---

# Test workflow`,
			expectSetup: []string{
				"Setup Node.js",
				"Setup Python",
			},
		},
		{
			name: "skips auto-detection when setup action exists",
			workflowMarkdown: `---
on: push
engine: copilot
steps:
  - name: Setup Node.js
    uses: actions/setup-node@v4 # SHA will be pinned
    with:
      node-version: '24'
  - name: Install
    run: npm install
---

# Test workflow`,
			expectSetup: []string{
				"node-version:", // Should keep user's version (check for key)
				"24",            // Check for the value (regardless of quote type)
			},
			notExpectSetup: []string{
				// Should not add a second Node.js setup with different version
			},
		},
		{
			name: "detects runtime from MCP server config",
			workflowMarkdown: `---
on: push
engine: copilot
mcp-servers:
  custom-tool:
    command: python
    args: ["-m", "my_server"]
---

# Test workflow`,
			expectSetup: []string{
				"Setup Python",
				"uses: actions/setup-python@", // SHA varies
			},
		},
		{
			name: "no auto-detection for workflows without runtime commands in steps",
			workflowMarkdown: `---
on: push
engine:
  id: claude
steps:
  - name: Echo
    run: echo "Hello"
---

# Test workflow`,
			notExpectSetup: []string{
				"Setup Python",
				"Setup uv",
				"Setup Go",
				"Setup Ruby",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temp directory for test files
			tmpDir := testutil.TempDir(t, "test-*")
			testFile := tmpDir + "/test-workflow.md"

			// Write test workflow
			if err := os.WriteFile(testFile, []byte(tt.workflowMarkdown), 0644); err != nil {
				t.Fatalf("Failed to write test file: %v", err)
			}

			// Compile the workflow
			compiler := NewCompiler()
			if err := compiler.CompileWorkflow(testFile); err != nil {
				t.Fatalf("Failed to compile workflow: %v", err)
			}

			// Read the generated lock file
			lockFile := stringutil.MarkdownToLockFile(testFile)
			content, err := os.ReadFile(lockFile)
			if err != nil {
				t.Fatalf("Failed to read lock file: %v", err)
			}

			lockContent := string(content)

			// Check expected setup steps
			for _, expected := range tt.expectSetup {
				if !strings.Contains(lockContent, expected) {
					// Show a snippet of the lock file for context (first 100 lines)
					lines := strings.Split(lockContent, "\n")
					snippet := strings.Join(lines[:min(100, len(lines))], "\n")
					t.Errorf("Expected to find '%s' in lock file but didn't.\nFirst 100 lines:\n%s\n...(truncated)", expected, snippet)
				}
			}

			// Check that unwanted setup steps are not present
			for _, notExpected := range tt.notExpectSetup {
				if strings.Contains(lockContent, notExpected) {
					// Find the line containing the unexpected string for context
					lines := strings.Split(lockContent, "\n")
					var contextLines []string
					for i, line := range lines {
						if strings.Contains(line, notExpected) {
							start := max(0, i-3)
							end := min(len(lines), i+4)
							contextLines = append(contextLines, fmt.Sprintf("Lines %d-%d:", start+1, end))
							contextLines = append(contextLines, lines[start:end]...)
							break
						}
					}
					t.Errorf("Did not expect to find '%s' in lock file but it was present.\nContext:\n%s", notExpected, strings.Join(contextLines, "\n"))
				}
			}
		})
	}
}

func TestRuntimeSetupWithInlineCopilotDriver(t *testing.T) {
	workflowMarkdown := `---
on: workflow_dispatch
engine:
  id: copilot
  driver:
    python: |
      import sys
      print("hello from inline driver", file=sys.stderr)
---

# Test workflow`

	tmpDir := testutil.TempDir(t, "test-inline-driver-*")
	testFile := tmpDir + "/test-workflow.md"

	if err := os.WriteFile(testFile, []byte(workflowMarkdown), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	lockFile := stringutil.MarkdownToLockFile(testFile)
	content, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	lockContent := string(content)
	agentJobSection := extractJobSection(lockContent, "agent")

	expectedStrings := []string{
		"Setup Python",
		"Write Inline Copilot SDK Driver",
		"Install GitHub Copilot SDK (Python)",
		".gh-aw/copilot-sdk/inline-driver.py",
		`print("hello from inline driver", file=sys.stderr)`,
		".gh-aw/copilot-sdk/inline-driver",
		"PYTHONPATH: ${{ github.workspace }}/.gh-aw/copilot-sdk/python",
	}
	for _, expected := range expectedStrings {
		if !strings.Contains(lockContent, expected) {
			t.Errorf("Expected compiled workflow to contain %q", expected)
		}
	}

	setupPythonIndex := indexInNonCommentLines(agentJobSection, "Setup Python")
	writeDriverIndex := indexInNonCommentLines(agentJobSection, "Write Inline Copilot SDK Driver")
	installSDKIndex := indexInNonCommentLines(agentJobSection, "Install GitHub Copilot SDK (Python)")
	if setupPythonIndex == -1 || writeDriverIndex == -1 || installSDKIndex == -1 {
		t.Fatalf("Expected inline driver setup steps in agent job, got:\n%s", agentJobSection)
	}
	if !(setupPythonIndex < writeDriverIndex && writeDriverIndex < installSDKIndex) {
		t.Fatalf("Expected runtime setup, inline driver write, and SDK install ordering in agent job, got:\n%s", agentJobSection)
	}

	if !containsInNonCommentLines(agentJobSection, "${GITHUB_WORKSPACE}/.gh-aw/copilot-sdk/inline-driver") {
		t.Fatalf("Expected agent job to execute the generated inline driver wrapper, got:\n%s", agentJobSection)
	}
}

func TestRuntimeSetupWithEngineNode(t *testing.T) {
	// Test that auto-detected runtime setup works alongside engine requirements
	// Both the auto-detection and engine may add setup steps, which is acceptable
	workflowMarkdown := `---
on: push
engine: claude
steps:
  - name: Install dependencies
    run: npm install
---

# Test workflow`

	tmpDir := testutil.TempDir(t, "test-*")
	testFile := tmpDir + "/test-workflow.md"

	if err := os.WriteFile(testFile, []byte(workflowMarkdown), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	lockFile := stringutil.MarkdownToLockFile(testFile)
	content, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	lockContent := string(content)

	// Should have Node.js setup (from auto-detection or engine, or both)
	if !strings.Contains(lockContent, "Setup Node.js") {
		t.Error("Expected Node.js setup to be present")
	}

	// It's acceptable to have Node.js setup appear twice:
	// - Once from auto-detection for engine steps
	// - Once from engine requirements
	// This is not a problem as GitHub Actions will use the first setup
}

func TestRuntimeSetupPreservesUserVersions(t *testing.T) {
	// Test that when user specifies a version in setup action, we don't override it
	workflowMarkdown := `---
on: push
engine: copilot
steps:
  - name: Setup Python
    uses: actions/setup-python@v5 # SHA will be pinned
    with:
      python-version: '3.9'
  - name: Run script
    run: python test.py
---

# Test workflow`

	tmpDir := testutil.TempDir(t, "test-*")
	testFile := tmpDir + "/test-workflow.md"

	if err := os.WriteFile(testFile, []byte(workflowMarkdown), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	lockFile := stringutil.MarkdownToLockFile(testFile)
	content, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	lockContent := string(content)

	// Should preserve user's version (3.9) - check without quotes since YAML formatting may vary
	if !strings.Contains(lockContent, "python-version") || !strings.Contains(lockContent, "3.9") {
		t.Error("Expected to preserve user's Python version 3.9")
	}

	// Should not add default version (3.12) - check specifically for python-version to avoid
	// false positives from other version strings like AWF version "0.13.12"
	if strings.Contains(lockContent, `python-version: '3.12'`) || strings.Contains(lockContent, `python-version: "3.12"`) {
		t.Error("Should not override user's version with default version")
	}

	// Should only have one Python setup (excluding comment lines where frontmatter is embedded)
	count := countInNonCommentLines(lockContent, "Setup Python")
	if count > 1 {
		t.Errorf("Expected 'Setup Python' to appear once, but found %d occurrences", count)
	}
}

func TestUVDetectionAddsPythonDependency(t *testing.T) {
	// Test that when uv is detected via MCP server, Python is automatically added
	workflowMarkdown := `---
on: push
engine: copilot
mcp-servers:
  serena:
    command: "uvx"
    args:
      - "--from"
      - "git+https://github.com/oraios/serena"
      - "serena"
      - "start-mcp-server"
steps:
  - name: Verify uv
    run: uv --version
---

# Test workflow with uv`

	tmpDir := testutil.TempDir(t, "test-*")
	testFile := tmpDir + "/test-workflow.md"

	if err := os.WriteFile(testFile, []byte(workflowMarkdown), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	lockFile := stringutil.MarkdownToLockFile(testFile)
	content, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	lockContent := string(content)

	// Should have Python setup
	if !strings.Contains(lockContent, "Setup Python") {
		t.Error("Expected 'Setup Python' to be added as dependency for uv")
	}

	// Should have uv setup
	if !strings.Contains(lockContent, "Setup uv") {
		t.Error("Expected 'Setup uv' to be added")
	}

	// Python setup should come before uv setup (in non-comment lines)
	pythonIndex := indexInNonCommentLines(lockContent, "Setup Python")
	uvIndex := indexInNonCommentLines(lockContent, "Setup uv")
	if pythonIndex > uvIndex {
		t.Error("Setup Python should come before Setup uv (Python is a dependency of uv)")
	}

	// Both should come before "Verify uv" step (in non-comment lines)
	verifyIndex := indexInNonCommentLines(lockContent, "Verify uv")
	if pythonIndex > verifyIndex || uvIndex > verifyIndex {
		t.Error("Setup steps should come before 'Verify uv' step")
	}
}

// TestCustomImageRunnerNodeSetupIntegration verifies that Node.js is automatically
// set up in the agent job when a custom image runner is specified.
func TestCustomImageRunnerNodeSetupIntegration(t *testing.T) {
	tests := []struct {
		name              string
		workflowMarkdown  string
		expectNodeSetup   bool
		expectNodeVersion string // if non-empty, assert this node-version appears in the setup step
	}{
		{
			name: "self-hosted runner gets Node.js setup",
			workflowMarkdown: `---
on: push
engine: copilot
runs-on: self-hosted
---

# Test workflow`,
			expectNodeSetup: true,
		},
		{
			name: "custom enterprise runner gets Node.js setup",
			workflowMarkdown: `---
on: push
engine: copilot
runs-on: enterprise-custom-runner
---

# Test workflow`,
			expectNodeSetup: true,
		},
		{
			name: "ubuntu-latest does not get extra Node.js setup from runtime manager",
			workflowMarkdown: `---
on: push
engine: copilot
runs-on: ubuntu-latest
---

# Test workflow`,
			expectNodeSetup: false,
		},
		{
			name: "default runner (no runs-on) does not get extra Node.js setup",
			workflowMarkdown: `---
on: push
engine: copilot
---

# Test workflow`,
			expectNodeSetup: false,
		},
		{
			name: "custom runner with node version override uses overridden version",
			workflowMarkdown: `---
on: push
engine: copilot
runs-on: self-hosted
runtimes:
  node:
    version: '22'
---

# Test workflow`,
			expectNodeSetup:   true,
			expectNodeVersion: "22",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := testutil.TempDir(t, "test-custom-runner-*")
			testFile := tmpDir + "/test-workflow.md"

			if err := os.WriteFile(testFile, []byte(tt.workflowMarkdown), 0644); err != nil {
				t.Fatalf("Failed to write test file: %v", err)
			}

			compiler := NewCompiler()
			if err := compiler.CompileWorkflow(testFile); err != nil {
				t.Fatalf("Compilation failed: %v", err)
			}

			lockFile := stringutil.MarkdownToLockFile(testFile)
			content, err := os.ReadFile(lockFile)
			if err != nil {
				t.Fatalf("Failed to read lock file: %v", err)
			}
			lockContent := string(content)

			// For the agent job, check whether a Setup Node.js step appears
			// from the runtime manager (before engine installation steps).
			// The Copilot engine uses a binary installer (no npm), so any
			// "Setup Node.js" step in the agent job comes from the runtime manager.
			agentJobSection := extractJobSection(lockContent, "agent")

			hasNodeSetup := strings.Contains(agentJobSection, "Setup Node.js") &&
				strings.Contains(agentJobSection, "actions/setup-node@")

			if tt.expectNodeSetup {
				assert.True(t, hasNodeSetup, "Expected Node.js setup step in agent job for custom runner.\nAgent job:\n%s", agentJobSection)
				// If a specific node version is expected, verify it appears in the setup step.
				if tt.expectNodeVersion != "" {
					assert.Contains(t, agentJobSection, "node-version: '"+tt.expectNodeVersion+"'",
						"Expected node-version '%s' in Setup Node.js step.\nAgent job:\n%s", tt.expectNodeVersion, agentJobSection)
				}
			} else {
				assert.False(t, hasNodeSetup, "Did not expect Node.js setup step from runtime manager in agent job for standard runner.\nAgent job:\n%s", agentJobSection)
			}
		})
	}
}
