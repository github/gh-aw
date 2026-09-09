//go:build !integration

package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsCopilotWorkflowContent(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{
			name:    "copilot engine",
			content: "---\nengine: copilot\n---\nbody\n",
			want:    true,
		},
		{
			name:    "different engine",
			content: "---\nengine: claude\n---\nbody\n",
			want:    false,
		},
		{
			name:    "default engine",
			content: "---\non: workflow_dispatch\n---\nbody\n",
			want:    true,
		},
		{
			name:    "no frontmatter",
			content: "body\n",
			want:    false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, isCopilotWorkflowContent(tt.content))
		})
	}
}
