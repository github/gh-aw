//go:build !integration

package workflow

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDailySchemaAuditChecks(t *testing.T) {
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("jq is required to exercise the schema audit")
	}
	content, err := os.ReadFile("../../.github/workflows/daily-schema-audit-cursor.md")
	require.NoError(t, err)
	script := func(heading string) string {
		t.Helper()
		parts := strings.SplitN(string(content), heading, 2)
		require.Len(t, parts, 2)
		parts = strings.SplitN(parts[1], "```bash\n", 2)
		require.Len(t, parts, 2)
		parts = strings.SplitN(parts[1], "\n```", 2)
		require.Len(t, parts, 2)
		return parts[0]
	}
	references := script("## Step 2")
	required := script("## Step 3")
	workdir := t.TempDir()
	schemaPath := filepath.Join(workdir, "pkg/parser/schemas/main_workflow_schema.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(schemaPath), 0o755))
	run := func(command string) ([]byte, error) {
		t.Helper()
		cmd := exec.Command("bash", "-c", command)
		cmd.Dir = workdir
		return cmd.CombinedOutput()
	}
	require.NoError(t, os.WriteFile(schemaPath, []byte(`{
	  "properties": {
	    "direct": {"$ref": "#/$defs/direct"},
	    "nested": {"$ref": "#/$defs/a~1b/properties/field"}
	  },
	  "$defs": {
	    "direct": {"$ref": "#/$defs/transitive"},
	    "transitive": {"$ref": "#/$defs/direct"},
	    "a/b": {"properties": {"field": {"type": "string"}}},
	    "isolated1": {"$ref": "#/$defs/isolated2"},
	    "isolated2": {"$ref": "#/$defs/isolated1"},
	    "unused": {"description": "#/$defs/unused"}
	  }
	}`), 0o600))

	output, err := run(references)
	require.NoError(t, err, "%s", output)
	require.Equal(t, "isolated1\nisolated2\nunused\n", string(output))

	output, err = run(required)
	require.NoError(t, err, "%s", output)
	var paths [][]any
	require.NoError(t, json.Unmarshal(output, &paths))
	require.Equal(t, [][]any{{}, {"$defs", "a/b"}}, paths)

	require.NoError(t, os.WriteFile(schemaPath, []byte("{"), 0o600))
	for _, command := range []string{references, required} {
		output, err = run(command)
		require.Error(t, err, "%s", output)
	}
	require.NoError(t, os.Remove(schemaPath))
	for _, command := range []string{references, required} {
		output, err = run(command)
		require.Error(t, err, "%s", output)
	}
}
