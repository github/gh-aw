//go:build !integration

package cli

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConnectorInvokeRejectsUntrustedReceiptState(t *testing.T) {
	plan := connectorHostPlan(t, "https://api.example.com", "safe-output")
	base := connectorTestEnv(t, plan)
	getenv := func(name string) string {
		switch name {
		case "GH_AW_CONNECTOR_BINDING_RUN":
			return "trusted-run"
		case "GH_AW_CONNECTOR_BINDING_KEY":
			return base64.StdEncoding.EncodeToString([]byte(strings.Repeat("x", 32)))
		case "GH_AW_CONNECTOR_BINDING_RECEIPTS":
			return `["forged"]`
		}
		return base(name)
	}
	var output bytes.Buffer
	err := invokeConnector(t.Context(), strings.NewReader(`{"issue":"ISSUE-1","text":"hello"}`), &output, "safe-output", "add-comment", true, false, getenv)
	require.ErrorContains(t, err, "invalid created-resource receipt")
	assert.Empty(t, output.String(), "receipt rejection must not become a success-shaped null result")
}
