//go:build !integration

package cli

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCheckWorkflowHasDispatchFromContent(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{name: "mapping", content: "---\non:\n  workflow_dispatch:\n---\n", want: true},
		{name: "string", content: "---\non: workflow_dispatch\n---\n", want: true},
		{name: "list", content: "---\non:\n  - push\n  - workflow_dispatch\n---\n", want: true},
		{name: "no dispatch", content: "---\non: push\n---\n", want: false},
		{name: "invalid frontmatter", content: "not frontmatter", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, checkWorkflowHasDispatchFromContent(tt.content))
		})
	}
}
