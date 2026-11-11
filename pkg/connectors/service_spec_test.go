package connectors

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConnectorInlineServiceIsConfinedAndCopied(t *testing.T) {
	t.Parallel()
	original := fixturePlan(t, "https://api.example.com")
	root := t.TempDir()
	data, err := json.Marshal(original.connection)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(root, "connection.json"), data, 0600))
	service, err := LoadServiceSpec(root, original.spec, "connection.json")
	require.NoError(t, err)
	original.spec.Endpoints["api"] = "https://attacker.example"
	plan, err := service.Select(Selection{MCP: []string{"get-issue"}})
	require.NoError(t, err)
	assert.Equal(t, "https://api.example.com", plan.spec.Endpoints["api"])
	_, err = LoadServiceSpec(root, plan.spec, "../connection.json")
	require.ErrorContains(t, err, "repository-relative")
	_, err = LoadServiceSpec(root, plan.spec, "${{ secrets.FILE }}")
	require.ErrorContains(t, err, "without expressions")
}
