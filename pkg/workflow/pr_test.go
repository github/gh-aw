//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestShouldGeneratePRCheckoutStep(t *testing.T) {
	tests := []struct {
		name        string
		permissions string
		expected    bool
	}{
		{
			name:        "with contents read permission",
			permissions: "contents: read",
			expected:    true,
		},
		{
			name:        "with contents write permission",
			permissions: "contents: write",
			expected:    true,
		},
		{
			name:        "without contents permission",
			permissions: "issues: read",
			expected:    false,
		},
		{
			name:        "with read-all shorthand",
			permissions: "read-all",
			expected:    true,
		},
		{
			name:        "with write-all shorthand",
			permissions: "write-all",
			expected:    true,
		},
		{
			name:        "with none shorthand",
			permissions: "none",
			expected:    false,
		},
		{
			name:        "with all: read",
			permissions: `all: read`,
			expected:    true,
		},
		{
			name: "multiple permissions including contents",
			permissions: `contents: read
issues: write
pull-requests: read`,
			expected: true,
		},
		{
			name: "multiple permissions without contents",
			permissions: `issues: write
pull-requests: read`,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := &WorkflowData{
				Permissions: tt.permissions,
			}
			result := ShouldGeneratePRCheckoutStep(data)
			assert.Equal(t, tt.expected, result, "ShouldGeneratePRCheckoutStep() result mismatch")
		})
	}
}

func TestShouldGeneratePRCheckoutStep_CheckoutDisabled(t *testing.T) {
	t.Run("returns false when CheckoutDisabled is true even with contents read", func(t *testing.T) {
		data := &WorkflowData{
			Permissions:      "contents: read",
			CheckoutDisabled: true,
		}
		result := ShouldGeneratePRCheckoutStep(data)
		assert.False(t, result, "ShouldGeneratePRCheckoutStep() should return false when CheckoutDisabled is true")
	})

	t.Run("returns true when CheckoutDisabled is false with contents read", func(t *testing.T) {
		data := &WorkflowData{
			Permissions:      "contents: read",
			CheckoutDisabled: false,
		}
		result := ShouldGeneratePRCheckoutStep(data)
		assert.True(t, result, "ShouldGeneratePRCheckoutStep() should return true when CheckoutDisabled is false and permissions allow")
	})

	t.Run("returns false when IsPullRequestTarget is true even with contents read and explicit checkout", func(t *testing.T) {
		data := &WorkflowData{
			Permissions:         "contents: read",
			CheckoutDisabled:    false,
			IsPullRequestTarget: true,
		}
		result := ShouldGeneratePRCheckoutStep(data)
		assert.False(t, result, "ShouldGeneratePRCheckoutStep() should return false for pull_request_target workflows to prevent refs/pull/<n>/head checkout")
	})
}

func TestResolveAgentManifestPaths(t *testing.T) {
	t.Run("includes defaults and only the selected engine", func(t *testing.T) {
		folders, files := resolveAgentManifestPaths(NewEngineRegistry(), &WorkflowData{
			EngineConfig: &EngineConfig{ID: "claude"},
		})

		assert.Equal(t, []string{".agents", ".claude", ".github"}, folders)
		assert.Equal(t, []string{"AGENTS.md", "CLAUDE.md"}, files)
	})

	t.Run("includes ambient folders without duplicates", func(t *testing.T) {
		folders, files := resolveAgentManifestPaths(NewEngineRegistry(), &WorkflowData{
			EngineConfig:   &EngineConfig{ID: "codex"},
			AmbientFolders: []string{".squad", ".agents"},
		})

		assert.Equal(t, []string{".agents", ".codex", ".github", ".squad"}, folders)
		assert.Equal(t, []string{"AGENTS.md"}, files)
	})

	t.Run("resolves engine via prefix alias", func(t *testing.T) {
		folders, files := resolveAgentManifestPaths(NewEngineRegistry(), &WorkflowData{
			EngineConfig: &EngineConfig{ID: "codex-experimental"},
		})

		assert.Equal(t, []string{".agents", ".codex", ".github"}, folders)
		assert.Equal(t, []string{"AGENTS.md"}, files)
	})
}

func TestGeneratePRReadyForReviewCheckout_IncludesWorkflowDispatchIssueCommentContext(t *testing.T) {
	compiler := NewCompiler()
	var yaml strings.Builder
	data := &WorkflowData{
		Permissions: "contents: read",
	}

	compiler.generatePRReadyForReviewCheckout(&yaml, data)
	rendered := yaml.String()

	assert.Contains(t, rendered, "github.event.pull_request")
	assert.Contains(t, rendered, "github.event.issue.pull_request")
	assert.Contains(t, rendered, "github.event_name == 'workflow_dispatch'")
	assert.NotContains(t, rendered, "fromJSON(")
	assert.Contains(t, rendered, "github.event.pull_request || github.event.issue.pull_request || github.event_name == 'workflow_dispatch'")
}

func TestFrontmatterHasTrigger(t *testing.T) {
	tests := []struct {
		name     string
		onVal    any
		trigger  string
		expected bool
	}{
		// scalar string form: on: pull_request_target
		{name: "scalar matches", onVal: "pull_request_target", trigger: "pull_request_target", expected: true},
		{name: "scalar no match", onVal: "push", trigger: "pull_request_target", expected: false},
		// sequence form: on: [pull_request_target, push]
		{name: "slice matches first", onVal: []any{"pull_request_target", "push"}, trigger: "pull_request_target", expected: true},
		{name: "slice matches second", onVal: []any{"push", "pull_request_target"}, trigger: "pull_request_target", expected: true},
		{name: "slice no match", onVal: []any{"push", "schedule"}, trigger: "pull_request_target", expected: false},
		// mapping form: on:\n  pull_request_target:\n    types: [closed]
		{name: "map matches", onVal: map[string]any{"pull_request_target": map[string]any{"types": []any{"closed"}}}, trigger: "pull_request_target", expected: true},
		{name: "map no match", onVal: map[string]any{"push": nil}, trigger: "pull_request_target", expected: false},
		// nil / unknown
		{name: "nil returns false", onVal: nil, trigger: "pull_request_target", expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := frontmatterHasTrigger(tt.onVal, tt.trigger)
			assert.Equal(t, tt.expected, got)
		})
	}
}
