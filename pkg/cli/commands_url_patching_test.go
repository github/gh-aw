//go:build !integration

package cli

import (
	"testing"
)

func TestPatchAgentFileURLs(t *testing.T) {
	tests := []struct {
		name           string
		content        string
		ref            string
		expectedOutput string
	}{
		{
			name:           "converts local paths to GitHub URLs with main ref",
			content:        "**Prompt file**: `.github/aw/create-agentic-workflow.md`",
			ref:            "main",
			expectedOutput: "**Prompt file**: `https://github.com/github/gh-aw/blob/main/.github/aw/create-agentic-workflow.md`",
		},
		{
			name:           "converts local paths to GitHub URLs with release ref",
			content:        "**Prompt file**: `.github/aw/create-agentic-workflow.md`",
			ref:            "v1.2.3",
			expectedOutput: "**Prompt file**: `https://github.com/github/gh-aw/blob/v1.2.3/.github/aw/create-agentic-workflow.md`",
		},
		{
			name:           "patches existing main URLs to release version",
			content:        "**Prompt file**: https://github.com/github/gh-aw/blob/main/.github/aw/create-agentic-workflow.md",
			ref:            "v1.2.3",
			expectedOutput: "**Prompt file**: https://github.com/github/gh-aw/blob/v1.2.3/.github/aw/create-agentic-workflow.md",
		},
		{
			name:           "does not patch main URLs when ref is main",
			content:        "**Prompt file**: https://github.com/github/gh-aw/blob/main/.github/aw/create-agentic-workflow.md",
			ref:            "main",
			expectedOutput: "**Prompt file**: https://github.com/github/gh-aw/blob/main/.github/aw/create-agentic-workflow.md",
		},
		{
			name: "handles multiple URLs in content",
			content: `**Prompt file**: ` + "`.github/aw/create-agentic-workflow.md`" + `

Other content

**Prompt file**: ` + "`.github/aw/update-agentic-workflow.md`",
			ref: "v2.0.0",
			expectedOutput: `**Prompt file**: ` + "`https://github.com/github/gh-aw/blob/v2.0.0/.github/aw/create-agentic-workflow.md`" + `

Other content

**Prompt file**: ` + "`https://github.com/github/gh-aw/blob/v2.0.0/.github/aw/update-agentic-workflow.md`",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := patchAgentFileURLs(tt.content, tt.ref)
			if result != tt.expectedOutput {
				t.Errorf("Expected:\n%s\n\nGot:\n%s", tt.expectedOutput, result)
			}
		})
	}
}
