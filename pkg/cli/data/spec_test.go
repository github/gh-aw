//go:build !integration

package data_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/github/gh-aw/pkg/cli/data"
)

// TestSpec_PublicAPI_ModelsJSON enforces README.md's Public API contract:
// the exported bytes contain a bundled JSON catalogue keyed by provider.
func TestSpec_PublicAPI_ModelsJSON(t *testing.T) {
	catalogueBytes := data.ModelsJSON
	require.NotEmpty(t, catalogueBytes, "the bundled models.json catalogue must be available")

	var providers map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(catalogueBytes, &providers), "the catalogue must be a JSON object keyed by provider")
	assert.NotEmpty(t, providers, "the bundled catalogue must contain provider entries")
}
