//go:build !integration

package cli

import (
	"bytes"
	"io"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/workflow"
	"github.com/stretchr/testify/require"
)

func TestLinearDevelopmentShorthandStaging(t *testing.T) {
	root, err := filepath.Abs("../..")
	require.NoError(t, err)
	grants := map[string][]string{
		"issues": {"22222222-2222-4222-8222-222222222222"}, "issue-references": {"ENG-123"},
		"teams": {"33333333-3333-4333-8333-333333333333"}, "projects": {"44444444-4444-4444-8444-444444444444"},
		"organizations": {"11111111-1111-4111-8111-111111111111"},
	}
	declaration, err := workflow.LoadDevelopmentConnector(root, "linear", "", map[string]any{"writer-secret": "OVERRIDDEN_WRITER"}, grants)
	require.NoError(t, err)
	options := developmentOptions("safe-output", "add-comment", `{"issue-id":"ENG-123","body":"A staged shorthand comment"}`)
	options.nodeRuntime = "../../actions/setup/js/connector_runtime.cjs"
	noSecrets := func(string) string {
		t.Fatal("shorthand staging must not inspect credentials")
		return ""
	}
	client := &http.Client{Transport: developmentRoundTripper(func(*http.Request) (*http.Response, error) {
		t.Fatal("shorthand staging must not contact a provider")
		return nil, nil
	})}
	var output bytes.Buffer
	require.NoError(t, testDevelopmentConnector(t.Context(), &output, declaration, options, noSecrets, client))
	require.Contains(t, output.String(), "Unresolved targets: issue-id")
	require.Contains(t, output.String(), "remain unverified")
	require.NotContains(t, output.String(), "ENG-123")
	output.Reset()
	require.NoError(t, setupDevelopmentConnector(&output, "linear", declaration, "safe-output"))
	require.Contains(t, output.String(), "--grant issue-references=")
	require.Contains(t, output.String(), "--grant team-references=")
	require.Contains(t, output.String(), "provider Read permissions")
	options.arguments = `{"issue-id":"ENG-999","body":"A staged shorthand comment"}`
	require.Error(t, testDevelopmentConnector(t.Context(), io.Discard, declaration, options, noSecrets, client))
}
