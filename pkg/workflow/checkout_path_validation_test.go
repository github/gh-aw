//go:build !integration

package workflow

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDeriveAndWarnCrossRepoCheckoutPaths verifies that cross-repo checkout entries
// without an explicit path: get a warning and an auto-derived path.
func TestDeriveAndWarnCrossRepoCheckoutPaths(t *testing.T) {
	const markdownPath = "test-workflow.md"

	t.Run("no checkout configs produces no warning", func(t *testing.T) {
		c := NewCompiler()
		c.deriveAndWarnCrossRepoCheckoutPaths(nil, markdownPath)
		assert.Equal(t, 0, c.GetWarningCount())
	})

	t.Run("default-only checkout (no repository) produces no warning", func(t *testing.T) {
		c := NewCompiler()
		configs := []*CheckoutConfig{{Path: "."}}
		c.deriveAndWarnCrossRepoCheckoutPaths(configs, markdownPath)
		assert.Equal(t, 0, c.GetWarningCount())
		assert.Equal(t, ".", configs[0].Path, "path must not be modified")
	})

	t.Run("cross-repo checkout with explicit path produces no warning", func(t *testing.T) {
		c := NewCompiler()
		configs := []*CheckoutConfig{
			{Repository: "owner/repo", Path: "my-repo"},
		}
		c.deriveAndWarnCrossRepoCheckoutPaths(configs, markdownPath)
		assert.Equal(t, 0, c.GetWarningCount())
		assert.Equal(t, "my-repo", configs[0].Path, "explicit path must not be changed")
	})

	t.Run("cross-repo checkout with no path gets auto-derived path and warning", func(t *testing.T) {
		c := NewCompiler()
		configs := []*CheckoutConfig{
			{Repository: "githubnext/gh-aw-side-repo"},
		}
		c.deriveAndWarnCrossRepoCheckoutPaths(configs, markdownPath)
		assert.Equal(t, 1, c.GetWarningCount())
		assert.Equal(t, "gh-aw-side-repo", configs[0].Path, "path must be auto-derived from repo name")
	})

	t.Run("auto-derived path is only the repo-name portion of owner/repo", func(t *testing.T) {
		c := NewCompiler()
		configs := []*CheckoutConfig{
			{Repository: "some-org/my-awesome-tool"},
		}
		c.deriveAndWarnCrossRepoCheckoutPaths(configs, markdownPath)
		require.Equal(t, 1, c.GetWarningCount())
		assert.Equal(t, "my-awesome-tool", configs[0].Path)
	})

	t.Run("multiple cross-repo checkouts without path each get a warning and derived path", func(t *testing.T) {
		c := NewCompiler()
		configs := []*CheckoutConfig{
			{Repository: "owner/alpha"},
			{Repository: "owner/beta", Path: "explicit-beta"}, // already has path — unchanged
			{Repository: "owner/gamma"},
		}
		c.deriveAndWarnCrossRepoCheckoutPaths(configs, markdownPath)
		assert.Equal(t, 2, c.GetWarningCount())
		assert.Equal(t, "alpha", configs[0].Path)
		assert.Equal(t, "explicit-beta", configs[1].Path, "pre-set path must not be changed")
		assert.Equal(t, "gamma", configs[2].Path)
	})

	t.Run("nil checkout entry in slice is skipped without panic", func(t *testing.T) {
		c := NewCompiler()
		configs := []*CheckoutConfig{nil}
		require.NotPanics(t, func() { c.deriveAndWarnCrossRepoCheckoutPaths(configs, markdownPath) })
		assert.Equal(t, 0, c.GetWarningCount())
	})

	t.Run("dynamic repository expression is skipped (no warning, no path change)", func(t *testing.T) {
		c := NewCompiler()
		configs := []*CheckoutConfig{
			{Repository: "${{ github.event.inputs.repo }}"},
		}
		c.deriveAndWarnCrossRepoCheckoutPaths(configs, markdownPath)
		assert.Equal(t, 0, c.GetWarningCount(), "dynamic expressions cannot be determined as cross-repo at compile time")
		assert.Empty(t, configs[0].Path, "path must not be modified for dynamic repos")
	})

	t.Run("github.repository expression (same-repo trusted checkout) is skipped", func(t *testing.T) {
		c := NewCompiler()
		configs := []*CheckoutConfig{
			{Repository: "${{ github.repository }}"},
		}
		c.deriveAndWarnCrossRepoCheckoutPaths(configs, markdownPath)
		assert.Equal(t, 0, c.GetWarningCount(), "github.repository is a same-repo pattern used in pull_request_target")
		assert.Empty(t, configs[0].Path)
	})

	t.Run("warning message for static repo contains suggested path", func(t *testing.T) {
		path, stderrMsg, warnCount := warnCrossRepoPath(t, "acme/widget-service")
		assert.Equal(t, "widget-service", path)
		assert.Equal(t, 1, warnCount)
		assert.Contains(t, stderrMsg, `checkout: repository "acme/widget-service" has no explicit path: field.`)
		assert.Contains(t, stderrMsg, "path: widget-service")
	})

	t.Run("explicit root path remains opt-in when normalized to empty string", func(t *testing.T) {
		c := NewCompiler()
		configs := []*CheckoutConfig{
			{Repository: "owner/repo", PathExplicit: true},
		}
		c.deriveAndWarnCrossRepoCheckoutPaths(configs, markdownPath)
		assert.Equal(t, 0, c.GetWarningCount(), "explicit root path must not trigger auto-derivation")
		assert.Empty(t, configs[0].Path, "explicit root path should stay at workspace root")
	})
}

// TestDeriveAndWarnCrossRepoCheckoutPathsViaCompiler verifies that the warning and path
// auto-derivation are triggered end-to-end when compiling a workflow.
func TestDeriveAndWarnCrossRepoCheckoutPathsViaCompiler(t *testing.T) {
	t.Run("compiler emits warning for cross-repo checkout without path", func(t *testing.T) {
		c := NewCompiler()

		workflowData := minimalWorkflowData()
		workflowData.CheckoutConfigs = []*CheckoutConfig{
			{Repository: "owner/side-repo"},
		}

		markdownPath := "test.md"
		err := c.validateWorkflowData(workflowData, markdownPath)
		require.NoError(t, err, "missing path should be a warning, not an error")
		assert.Equal(t, 1, c.GetWarningCount(), "expected one warning for missing path")
		// Confirm auto-derivation happened
		assert.Equal(t, "side-repo", workflowData.CheckoutConfigs[0].Path)
	})

	t.Run("compiler emits no warning when path is explicitly set", func(t *testing.T) {
		c := NewCompiler()

		workflowData := minimalWorkflowData()
		workflowData.CheckoutConfigs = []*CheckoutConfig{
			{Repository: "owner/side-repo", Path: "side-repo"},
		}

		markdownPath := "test.md"
		err := c.validateWorkflowData(workflowData, markdownPath)
		require.NoError(t, err)
		assert.Equal(t, 0, c.GetWarningCount())
	})
}

// minimalWorkflowData returns a WorkflowData suitable for use in compiler validation tests.
func minimalWorkflowData() *WorkflowData {
	return &WorkflowData{
		Name:            "Test Workflow",
		MarkdownContent: "Test content.",
		On:              "push:",
		Permissions:     "contents: read",
	}
}

// warnCrossRepoPath returns the derived path, emitted warning text, and warning count.
func warnCrossRepoPath(t *testing.T, repository string) (path string, stderrMsg string, warnCount int) {
	t.Helper()
	origStderr := os.Stderr
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stderr = w
	defer func() { os.Stderr = origStderr }()

	c := NewCompiler()
	cfg := &CheckoutConfig{Repository: repository}
	c.deriveAndWarnCrossRepoCheckoutPaths([]*CheckoutConfig{cfg}, "test.md")

	require.NoError(t, w.Close())
	var buf bytes.Buffer
	_, err = io.Copy(&buf, r)
	require.NoError(t, err)
	require.NoError(t, r.Close())

	return cfg.Path, buf.String(), c.GetWarningCount()
}

func TestWarnCrossRepoPath_RepoNameExtraction(t *testing.T) {
	tests := []struct {
		repository   string
		expectedPath string
		expectedWarn int
	}{
		{"owner/repo", "repo", 1},
		{"github/copilot", "copilot", 1},
		{"githubnext/gh-aw-side-repo", "gh-aw-side-repo", 1},
		{"a/b/c", "c", 1},                               // only the last segment
		{"just-repo-no-owner", "just-repo-no-owner", 1}, // no slash: use full string
		{"owner/", "owner", 1},                          // trailing slash is trimmed before derivation
		{"   ", "", 0},                                  // whitespace-only repository is ignored
	}
	for _, tt := range tests {
		t.Run(tt.repository, func(t *testing.T) {
			path, _, count := warnCrossRepoPath(t, tt.repository)
			assert.Equal(t, tt.expectedPath, path)
			assert.Equal(t, tt.expectedWarn, count)
		})
	}
}

// TestCrossRepoCheckoutPathAppearsInManifestStep confirms that after auto-derivation the
// CheckoutManager emits a non-empty GH_AW_CHECKOUT_PATH_N in the manifest step.
func TestCrossRepoCheckoutPathAppearsInManifestStep(t *testing.T) {
	getActionPin := func(action string) string { return action + "@pin" }

	t.Run("auto-derived path appears in manifest step", func(t *testing.T) {
		// Simulate what the compiler does: validate configs first (derives path), then pass to manager.
		configs := []*CheckoutConfig{
			{Repository: "githubnext/gh-aw-side-repo"},
		}
		c := NewCompiler()
		c.deriveAndWarnCrossRepoCheckoutPaths(configs, "test.md")

		cm := NewCheckoutManager(configs)
		steps := cm.GenerateCheckoutManifestStep(getActionPin)
		require.Len(t, steps, 1)
		out := steps[0]
		assert.Contains(t, out, `GH_AW_CHECKOUT_REPO_0: "githubnext/gh-aw-side-repo"`)
		assert.Contains(t, out, `GH_AW_CHECKOUT_PATH_0: "gh-aw-side-repo"`)
		assert.NotContains(t, out, `GH_AW_CHECKOUT_PATH_0: ""`, "path must not be empty after auto-derivation")
	})
}

// TestCrossRepoCheckoutPathAppearsInCheckoutStep confirms that the checkout step emits
// a path: field after auto-derivation.
func TestCrossRepoCheckoutPathAppearsInCheckoutStep(t *testing.T) {
	getActionPin := func(action string) string { return action + "@pin" }

	configs := []*CheckoutConfig{
		{Repository: "acme/my-lib"},
	}
	c := NewCompiler()
	c.deriveAndWarnCrossRepoCheckoutPaths(configs, "test.md")

	cm := NewCheckoutManager(configs)
	lines := cm.GenerateAdditionalCheckoutSteps(getActionPin)
	combined := strings.Join(lines, "")
	assert.Contains(t, combined, "repository: acme/my-lib")
	assert.Contains(t, combined, "path: my-lib", "checkout step must include the auto-derived path")
}

// TestCheckoutPathRejectsWorkspaceEscape verifies that checkout paths escaping the
// workspace root are rejected at parse time instead of being silently normalized.
func TestCheckoutPathRejectsWorkspaceEscape(t *testing.T) {
	t.Run("parent segment", func(t *testing.T) {
		_, err := checkoutConfigFromMap(map[string]any{"path": "../../etc"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "must not contain")
	})

	t.Run("nested parent segment", func(t *testing.T) {
		_, err := checkoutConfigFromMap(map[string]any{"path": "external/../../etc"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "must not contain")
	})

	t.Run("absolute path", func(t *testing.T) {
		_, err := checkoutConfigFromMap(map[string]any{"path": "/etc"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "relative path")
	})

	t.Run("valid relative path", func(t *testing.T) {
		cfg, err := checkoutConfigFromMap(map[string]any{"path": "external/repo"})
		require.NoError(t, err)
		assert.Equal(t, "external/repo", cfg.Path)
	})
}
