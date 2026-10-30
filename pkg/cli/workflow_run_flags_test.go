//go:build !integration

package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWorkflowRunFlagsDispatchArgs(t *testing.T) {
	tests := []struct {
		name string
		repo string
		ref  string
	}{
		{name: "remote default branch", repo: "owner/repo"},
		{name: "remote feature branch", repo: "owner/repo", ref: "feature/my-change"},
		{name: "remote tag", repo: "owner/repo", ref: "v1.0.0"},
		{name: "local feature branch", ref: "feature/my-change"},
		{name: "local tag", ref: "v1.0.0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := RunOptions{
				WorkflowRunFlags: WorkflowRunFlags{
					RepoOverride: tt.repo,
					RefOverride:  tt.ref,
					DryRun:       true,
				},
				Inputs: []string{"name=value", "message=hello world"},
			}
			expected := []string{"workflow", "run", "test.lock.yml"}
			if tt.repo != "" {
				expected = append(expected, "--repo", tt.repo)
			}
			if tt.ref != "" {
				expected = append(expected, "--ref", tt.ref)
			}
			expected = append(expected, "-f", "name=value", "-f", "message=hello world")

			args, resolvedRef := buildWorkflowRunArgs("test.lock.yml", opts)
			assert.Equal(t, expected, args)
			assert.Equal(t, tt.ref, resolvedRef)
		})
	}
}
