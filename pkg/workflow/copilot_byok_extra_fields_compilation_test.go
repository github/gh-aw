//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/require"
)

// TestCopilotBYOKExtraFieldsInCompiledWorkflow is an end-to-end compilation test
// verifying that sandbox.agent.targets.copilot.extra-headers, extra-body-fields, and
// session-id in frontmatter flow all the way through compilation into the AWF config
// JSON embedded in the generated .lock.yml file.
//
// This test covers the full path: frontmatter schema validation → WorkflowData parsing
// → BuildAWFConfigJSON → lock file printf script.
func TestCopilotBYOKExtraFieldsInCompiledWorkflow(t *testing.T) {
	workflow := `---
on: push
permissions:
  contents: read
engine:
  id: copilot
  env:
    COPILOT_PROVIDER_BASE_URL: "https://openrouter.ai/api/v1"
    COPILOT_PROVIDER_API_KEY: "${{ secrets.OPENROUTER_API_KEY }}"
sandbox:
  agent:
    targets:
      copilot:
        extra-headers:
          x-openrouter-title: my-workflow
          http-referer: https://github.com/org/repo
        extra-body-fields:
          custom-field: custom-value
        session-id: "${{ github.run_id }}"
strict: false
safe-outputs:
  create-issue:
---

# BYOK workflow with extraHeaders, extraBodyFields, and sessionId
`

	tmpDir := testutil.TempDir(t, "copilot-byok-extra-fields")
	testFile := filepath.Join(tmpDir, "byok-extra-fields.md")
	require.NoError(t, os.WriteFile(testFile, []byte(workflow), 0644), "failed to write workflow file")

	compiler := NewCompiler()
	require.NoError(t, compiler.CompileWorkflow(testFile), "failed to compile workflow")

	lockContent, err := os.ReadFile(stringutil.MarkdownToLockFile(testFile))
	require.NoError(t, err, "failed to read compiled lock file")
	lockStr := string(lockContent)

	// extraHeaders should appear in the AWF config JSON (either escaped or unescaped,
	// depending on whether shell-escaping was applied to the printf argument).
	if !strings.Contains(lockStr, `"extraHeaders"`) && !strings.Contains(lockStr, `\"extraHeaders\"`) {
		t.Error("expected extraHeaders to be present in compiled AWF config JSON")
	}
	if !strings.Contains(lockStr, `"x-openrouter-title"`) && !strings.Contains(lockStr, `\"x-openrouter-title\"`) {
		t.Error("expected x-openrouter-title header key in compiled AWF config JSON")
	}
	if !strings.Contains(lockStr, `"my-workflow"`) && !strings.Contains(lockStr, `\"my-workflow\"`) {
		t.Error("expected my-workflow header value in compiled AWF config JSON")
	}

	// extraBodyFields should appear in the AWF config JSON.
	if !strings.Contains(lockStr, `"extraBodyFields"`) && !strings.Contains(lockStr, `\"extraBodyFields\"`) {
		t.Error("expected extraBodyFields to be present in compiled AWF config JSON")
	}
	if !strings.Contains(lockStr, `"custom-field"`) && !strings.Contains(lockStr, `\"custom-field\"`) {
		t.Error("expected custom-field body field key in compiled AWF config JSON")
	}
	if !strings.Contains(lockStr, `"custom-value"`) && !strings.Contains(lockStr, `\"custom-value\"`) {
		t.Error("expected custom-value body field value in compiled AWF config JSON")
	}

	// sessionId should appear in the AWF config JSON with the full expression preserved.
	if !strings.Contains(lockStr, `"sessionId"`) && !strings.Contains(lockStr, `\"sessionId\"`) {
		t.Error("expected sessionId to be present in compiled AWF config JSON")
	}
	// The ${{ github.run_id }} GitHub Actions expression must be preserved verbatim so
	// it is evaluated at runtime. Check for the full expression syntax.
	if !strings.Contains(lockStr, `${{ github.run_id }}`) {
		t.Error("expected full '${{ github.run_id }}' expression in compiled AWF config JSON sessionId")
	}
}
