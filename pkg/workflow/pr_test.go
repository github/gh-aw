//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestShouldGeneratePRCheckoutStep_PullRequestDisabled(t *testing.T) {
	disabled := false
	data := &WorkflowData{
		Permissions: "contents: read",
		CheckoutConfigs: []*CheckoutConfig{
			{PullRequest: &disabled},
		},
	}

	assert.False(t, ShouldGeneratePRCheckoutStep(data))
}

func TestShouldGeneratePRCheckoutStep_MultiCheckout(t *testing.T) {
	tests := []struct {
		name      string
		checkouts []*CheckoutConfig
		want      bool
	}{
		{
			name: "external repository at root and workflow repository in subpath",
			checkouts: []*CheckoutConfig{
				{Repository: "${{ inputs.target_repo }}", Path: "."},
				{Repository: "${{ github.repository }}", Path: "target"},
			},
		},
		{
			name: "literal external repository at root",
			checkouts: []*CheckoutConfig{
				{Repository: "other/project", Path: ".", PathExplicit: true},
			},
		},
		{
			name: "external repository at root with Windows path separators",
			checkouts: []*CheckoutConfig{
				{Repository: "other/project", Path: ".\\"},
			},
		},
		{
			name: "wiki repository at root",
			checkouts: []*CheckoutConfig{
				{Repository: "${{ github.repository }}", Wiki: true},
			},
		},
		{
			name: "workflow repository at root and external repository in subpath",
			checkouts: []*CheckoutConfig{
				{Repository: "${{ github.repository }}", Path: "."},
				{Repository: "other/project", Path: "target"},
			},
			want: true,
		},
		{
			name: "implicit workflow repository at root",
			checkouts: []*CheckoutConfig{
				{Path: "."},
				{Repository: "other/project", Path: "target"},
			},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := &WorkflowData{Permissions: "contents: read", CheckoutConfigs: tt.checkouts}
			assert.Equal(t, tt.want, ShouldGeneratePRCheckoutStep(data))
		})
	}
}

func TestPRCheckoutRestoreWithMultiCheckout(t *testing.T) {
	for _, tt := range []struct {
		name       string
		rootRepo   string
		wantPRStep bool
	}{
		{name: "target repository at root", rootRepo: "${{ inputs.target_repo }}"},
		{name: "workflow repository at root", rootRepo: "${{ github.repository }}", wantPRStep: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			source := `---
on:
  workflow_dispatch:
    inputs:
      target_repo:
        type: string
        required: true
permissions:
  contents: read
checkout:
  - repository: ` + tt.rootRepo + `
    path: .
  - repository: ${{ github.repository }}
    path: target
strict: false
---
Test workflow.
`
			path := filepath.Join(dir, "test.md")
			if err := os.WriteFile(path, []byte(source), 0644); err != nil {
				t.Fatal(err)
			}
			if err := NewCompiler().CompileWorkflow(path); err != nil {
				t.Fatal(err)
			}
			lock, err := os.ReadFile(filepath.Join(dir, "test.lock.yml"))
			if err != nil {
				t.Fatal(err)
			}
			for _, step := range []string{
				"- name: Checkout PR branch",
				"- name: Restore agent config folders from base branch",
			} {
				assert.Equal(t, tt.wantPRStep, strings.Contains(string(lock), step), step)
			}
		})
	}
}

func TestCompileWorkflowWithDynamicWorkflowsDisabled(t *testing.T) {
	dir := t.TempDir()
	workflowPath := filepath.Join(dir, "claude.md")
	source := `---
on:
  workflow_dispatch:
engine:
  id: claude
  dynamic-workflows: false
strict: false
---
Run the workflow.
`
	require.NoError(t, os.WriteFile(workflowPath, []byte(source), 0644))
	require.NoError(t, NewCompiler().CompileWorkflow(workflowPath))

	lock, err := os.ReadFile(filepath.Join(dir, "claude.lock.yml"))
	require.NoError(t, err)
	assert.NotContains(t, string(lock), "Restore Claude workflows from activation artifact")
}

func TestPRCheckoutRestoreWithCustomCheckout(t *testing.T) {
	dir := t.TempDir()
	source := `---
on:
  workflow_dispatch:
permissions:
  contents: read
steps:
  - name: Checkout target repository
    uses: actions/checkout@v4
    with:
      repository: other/project
strict: false
---
Test workflow.
`
	path := filepath.Join(dir, "test.md")
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	if err := NewCompiler().CompileWorkflow(path); err != nil {
		t.Fatal(err)
	}
	lock, err := os.ReadFile(filepath.Join(dir, "test.lock.yml"))
	if err != nil {
		t.Fatal(err)
	}
	assert.NotContains(t, string(lock), "- name: Checkout PR branch")
	assert.NotContains(t, string(lock), "- name: Restore agent config folders from base branch")
}

func TestPRCheckoutRestoreCanBeDisabled(t *testing.T) {
	dir := t.TempDir()
	source := `---
on:
  workflow_dispatch:
permissions:
  contents: read
checkout:
  pull-request: false
strict: false
---
Test workflow.
`
	path := filepath.Join(dir, "test.md")
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	if err := NewCompiler().CompileWorkflow(path); err != nil {
		t.Fatal(err)
	}
	lock, err := os.ReadFile(filepath.Join(dir, "test.lock.yml"))
	if err != nil {
		t.Fatal(err)
	}
	rendered := string(lock)
	assert.Contains(t, rendered, "uses: actions/checkout@")
	assert.NotContains(t, rendered, "- name: Checkout PR branch")
	assert.NotContains(t, rendered, "- name: Save agent config folders for base branch restoration")
	assert.NotContains(t, rendered, "- name: Restore agent config folders from base branch")
}

func TestRestoreClaudeWorkflowsStep(t *testing.T) {
	var yaml strings.Builder
	generateRestoreClaudeWorkflowsStep(&yaml)
	step := yaml.String()
	assert.Contains(t, step, "src=/tmp/gh-aw/base/.claude")
	assert.Contains(t, step, "dst=\"$GITHUB_WORKSPACE/.claude\"")
	assert.Contains(t, step, "cp -a \"$src\" \"$dst\"")
	assert.Contains(t, step, "rm -rf \"$dst\"")
}

func TestCanRestoreClaudeWorkflows(t *testing.T) {
	tests := []struct {
		name string
		data *WorkflowData
		want bool
	}{
		{name: "default checkout", data: &WorkflowData{}, want: true},
		{name: "checkout disabled", data: &WorkflowData{CheckoutDisabled: true}, want: true},
		{name: "same repository root", data: &WorkflowData{CheckoutConfigs: []*CheckoutConfig{{Repository: "${{ github.repository }}"}}}, want: true},
		{name: "different repository root", data: &WorkflowData{CheckoutConfigs: []*CheckoutConfig{{Repository: "example/other"}}}},
		{name: "different repository subdirectory", data: &WorkflowData{CheckoutConfigs: []*CheckoutConfig{{Repository: "example/other", Path: "other"}}}, want: true},
		{name: "custom checkout", data: &WorkflowData{CustomSteps: "      - uses: actions/checkout@v4\n"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, canRestoreClaudeWorkflows(tt.data))
		})
	}
}

func TestClaudeWorkflowRestorePrecedesAgentSteps(t *testing.T) {
	var yaml strings.Builder
	data := &WorkflowData{AI: "claude", Permissions: "contents: read"}
	_, err := NewCompiler().generateEngineInstallAndPreAgentSteps(&yaml, data, false)
	require.NoError(t, err)
	steps := yaml.String()
	assert.Contains(t, steps, "Restore Claude workflows from activation artifact")
	assert.Less(t, strings.Index(steps, "Checkout PR branch"), strings.Index(steps, "Restore Claude workflows from activation artifact"))

	yaml.Reset()
	data.AI = "copilot"
	_, err = NewCompiler().generateEngineInstallAndPreAgentSteps(&yaml, data, false)
	require.NoError(t, err)
	assert.NotContains(t, yaml.String(), "Restore Claude workflows from activation artifact")

	yaml.Reset()
	disabled := false
	data = &WorkflowData{
		AI:           "claude",
		EngineConfig: &EngineConfig{DynamicWorkflows: &disabled},
	}
	_, err = NewCompiler().generateEngineInstallAndPreAgentSteps(&yaml, data, false)
	require.NoError(t, err)
	assert.NotContains(t, yaml.String(), "Restore Claude workflows from activation artifact")
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
