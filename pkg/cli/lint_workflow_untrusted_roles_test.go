//go:build !integration

package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	workflowlint "github.com/github/gh-aw/pkg/workflow/lint"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWorkflowLintUntrustedRolesReadsLockManifestDetectionPolicy(t *testing.T) {
	const jobs = "jobs:\n  agent:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo agent\n" +
		"  detection:\n    needs: agent\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo detect\n" +
		"  safe_outputs:\n    needs: [agent, detection]\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo publish\n"
	for _, tt := range []struct {
		name, mode   string
		wantFindings int
	}{
		{name: "conditional", mode: "conditional", wantFindings: 1},
		{name: "enabled", mode: "enabled"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			source := "---\non:\n  issues:\n    types: [opened]\n  roles: all\nstrict: false\nimports:\n  - shared/outputs.md\n---\nTriage the issue.\n"
			require.NoError(t, os.WriteFile(filepath.Join(dir, "test.md"), []byte(source), 0o600))
			lock := "# gh-aw-manifest: {\"version\":1,\"secrets\":[],\"actions\":[],\"mcp_servers\":[],\"threat_detection\":{\"mode\":\"" + tt.mode + "\"},\"admission\":false,\"agent_metadata\":{}}\n" +
				"name: test\non:\n  issues:\n    types: [opened]\n" + jobs
			file := filepath.Join(dir, "test.lock.yml")
			require.NoError(t, os.WriteFile(file, []byte(lock), 0o600))
			runner, err := workflowlint.Default().Select("untrusted-roles-detection")
			require.NoError(t, err)
			var output bytes.Buffer
			require.NoError(t, runWorkflowLint(t.Context(), []string{file}, runner, &output, true))
			var diagnostics []workflowLintDiagnostic
			require.NoError(t, json.Unmarshal(output.Bytes(), &diagnostics))
			require.Len(t, diagnostics, tt.wantFindings)
			for _, diagnostic := range diagnostics {
				assert.Contains(t, diagnostic.Message, "imported safe-outputs configuration gates threat detection")
			}
		})
	}
}
