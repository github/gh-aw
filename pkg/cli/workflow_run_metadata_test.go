//go:build !integration

package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBuildWorkflowRunAPIArgsPreservesExplicitHost(t *testing.T) {
	for _, tt := range []struct {
		name     string
		hostname string
		want     []string
	}{
		{
			name:     "ambient host",
			hostname: "",
			want:     []string{"api", "repos/octo/repo/actions/runs/42"},
		},
		{
			name:     "explicit public host",
			hostname: "github.com",
			want:     []string{"api", "--hostname", "github.com", "repos/octo/repo/actions/runs/42"},
		},
		{
			name:     "explicit enterprise host",
			hostname: "ghe.example.com",
			want:     []string{"api", "--hostname", "ghe.example.com", "repos/octo/repo/actions/runs/42"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, buildWorkflowRunAPIArgs(42, "octo", "repo", tt.hostname))
		})
	}
}
