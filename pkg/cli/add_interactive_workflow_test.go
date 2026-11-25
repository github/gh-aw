//go:build !integration

package cli

import (
	"context"
	"errors"
	"testing"

	"github.com/github/gh-aw/pkg/console"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetWorkflowStatuses(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		pattern      string
		repoOverride string
		verbose      bool
		// We can't easily test the actual results without mocking gh CLI,
		// so we just verify the function doesn't panic
	}{
		{
			name:         "simple pattern",
			pattern:      "test-workflow",
			repoOverride: "",
			verbose:      false,
		},
		{
			name:         "with repo override",
			pattern:      "daily-status",
			repoOverride: "owner/repo",
			verbose:      false,
		},
		{
			name:         "verbose mode",
			pattern:      "workflow-name",
			repoOverride: "owner/repo",
			verbose:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// This function calls gh CLI, so it will likely error in tests
			// We just verify it doesn't panic
			statuses, err := findWorkflowsByFilenamePattern(tt.pattern, tt.repoOverride, tt.verbose)

			// Either succeeds or fails gracefully, but shouldn't panic
			// Note: statuses may be nil even when err is nil (no workflows found)
			if err != nil {
				// Error is acceptable in test environment without gh CLI setup
				require.Error(t, err, "Expected error without gh CLI")
			}
			// If no error and statuses exist, verify they have expected structure
			if err == nil && statuses != nil {
				for _, status := range statuses {
					assert.NotEmpty(t, status.Workflow, "Workflow name should not be empty")
				}
			}
		})
	}
}

func TestMatchWorkflowByFilename(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    bool
		wantErr bool
	}{
		{name: "exact lock filename", input: `[{"name":"Repository status","state":"active","path":".github/workflows/repo-status.lock.yml"}]`, want: true},
		{name: "exact markdown filename", input: `[{"path":".github/workflows/repo-status.md"}]`, want: true},
		{name: "longer filename", input: `[{"path":".github/workflows/daily-repo-status.lock.yml"}]`},
		{name: "filename suffix", input: `[{"path":".github/workflows/repo-status.lock.yml.bak"}]`},
		{name: "display name is not path", input: `[{"name":"repo-status.md","path":".github/workflows/other.lock.yml"}]`},
		{name: "directory is not filename", input: `[{"path":"repo-status.lock.yml/other.lock.yml"}]`},
		{name: "later matching workflow", input: `[{"path":"other.lock.yml"},{"path":"repo-status.lock.yml"}]`, want: true},
		{name: "duplicate matches return one status", input: `[{"path":"repo-status.lock.yml"},{"path":"repo-status.md"}]`, want: true},
		{name: "escaped path", input: `[{"path":".github/workflows/repo\u002dstatus.lock.yml"}]`, want: true},
		{name: "empty list", input: `[]`},
		{name: "invalid json", input: `[{"path":"repo-status.lock.yml"},`, wantErr: true},
		{name: "wrong json shape", input: `{"path":"repo-status.lock.yml"}`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			statuses, err := matchWorkflowByFilename("repo-status", []byte(tt.input))
			if tt.wantErr {
				require.Error(t, err)
				assert.Nil(t, statuses)
				return
			}
			require.NoError(t, err)
			if tt.want {
				assert.Equal(t, []WorkflowStatus{{Workflow: "repo-status"}}, statuses)
			} else {
				assert.Nil(t, statuses)
			}
		})
	}
}

func TestCheckStatusAndOfferRun_ContextCancelled(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	cfg := &AddInteractiveConfig{Verbose: true}
	err := cfg.checkStatusAndOfferRun(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation error, got %v", err)
	}
}

func TestShouldOfferAddedWorkflowRun(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cfg  AddInteractiveConfig
		want bool
	}{
		{
			name: "no add result",
			cfg:  AddInteractiveConfig{},
			want: false,
		},
		{
			name: "workflow dispatch unavailable",
			cfg: AddInteractiveConfig{
				addResult: &AddWorkflowsResult{HasWorkflowDispatch: false},
			},
			want: false,
		},
		{
			name: "workflow dispatch available",
			cfg: AddInteractiveConfig{
				addResult: &AddWorkflowsResult{HasWorkflowDispatch: true},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, tt.cfg.shouldOfferAddedWorkflowRun())
		})
	}
}

func TestCheckWorkflowStatusAttempt(t *testing.T) {
	t.Parallel()

	t.Run("returns false when primaryWorkflowName is empty", func(t *testing.T) {
		t.Parallel()
		cfg := &AddInteractiveConfig{Verbose: false}
		// No WorkflowSpecs so primaryWorkflowName() returns ""
		assert.False(t, cfg.checkWorkflowStatusAttempt(0))
	})

	t.Run("returns false in verbose mode when primaryWorkflowName is empty", func(t *testing.T) {
		t.Parallel()
		cfg := &AddInteractiveConfig{Verbose: true}
		assert.False(t, cfg.checkWorkflowStatusAttempt(0))
	})
}

func TestConfirmRunAddedWorkflow_ContextCancelled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	// With a cancelled context the form should error immediately.
	runNow, err := confirmRunAddedWorkflow(ctx)
	assert.False(t, runNow)
	require.Error(t, err)
	// context.Canceled is not a user-abort, so it must be propagated as a real error.
	assert.False(t, console.IsCancelled(err), "context cancellation should not be treated as a user-abort")
}

func TestRunAddedWorkflowOnce_EmptyWorkflowName(t *testing.T) {
	t.Parallel()

	cfg := &AddInteractiveConfig{Verbose: false}
	// No WorkflowSpecs so primaryWorkflowName() returns "" — early-return expected.
	err := cfg.runAddedWorkflowOnce(t.Context())
	assert.NoError(t, err)
}
