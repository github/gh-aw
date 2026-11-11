package connectors

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConnectorLiteralMetadataHeaders(t *testing.T) {
	for _, headers := range []map[string]string{
		{"Notion-Version": "2026-03-11", "Content-Type": "application/json"},
		{"Accept": "application/vnd.pagerduty+json;version=2", "From": "automation@example.test"},
	} {
		require.NoError(t, validateRequestHeaders(Request{Headers: headers}))
	}
	for _, headers := range []map[string]string{
		{"Authorization": "Bearer cannot-override"},
		{"AUTHORIZATION": "Bearer cannot-override"},
		{"Host": "other.example"},
		{"Cookie": "value"},
		{"Connection": "keep-alive"},
		{"Content-Length": "1"},
		{"DD-API-Key": "not-a-metadata-value"},
		{"PRIVATE-TOKEN": "not-a-metadata-value"},
		{"X-Test": "safe", "x-test": "duplicate"},
		{"Invalid Header": "value"},
		{"Notion-Version": "${{ secrets.VALUE }}"},
		{"From": "value\r\nInjected: true"},
		{"X-Long": strings.Repeat("x", 1025)},
		{"Content-Type": "text/plain"},
	} {
		require.Error(t, validateRequestHeaders(Request{Headers: headers}))
	}
	plan := fixturePlan(t, "https://example.test")
	op := plan.spec.MCP["get-issue"].Operation
	op.Request.Headers = map[string]string{"Notion-Version": "2026-03-11", "Content-Type": "application/json"}
	request, err := plan.buildRequest(t.Context(), op, map[string]any{"issue": "ISSUE-1"})
	require.NoError(t, err)
	require.NoError(t, plan.authenticate(context.Background(), testRuntime(nil, "https://example.test", "agent"), op.Credential, request))
	require.Equal(t, "2026-03-11", request.Header.Get("Notion-Version"))
	require.Equal(t, "application/json", request.Header.Get("Content-Type"))
	require.Equal(t, "Bearer agent-only", request.Header.Get("Authorization"))
	require.Equal(t, http.MethodGet, request.Method)
}

func TestConnectorOptionalEmptyResponseMetadata(t *testing.T) {
	for _, test := range []struct {
		document string
		valid    bool
	}{
		{`{}`, true}, {`{"cursor":null}`, true}, {`{"cursor":""}`, true},
		{`{"cursor":"next"}`, false}, {`{"cursor":" "}`, false},
		{`{"cursor":false}`, false}, {`{"cursor":0}`, false},
		{`{"cursor":[]}`, false}, {`{"cursor":{}}`, false},
	} {
		var document any
		require.NoError(t, decodeJSON([]byte(test.document), &document))
		err := checkResponseEvidence(document, Operation{ResponseOptionalEmpty: []string{"/cursor"}})
		if test.valid {
			require.NoError(t, err)
		} else {
			require.Error(t, err)
		}
	}
}

func TestConnectorResponseResourceIdentity(t *testing.T) {
	for _, test := range []struct {
		actual, expected string
		valid            bool
	}{
		{`42`, "42", true}, {`"42"`, "42", true}, {`"APP-1"`, "APP-1", true},
		{`42`, "042", false}, {`42.0`, "42", false}, {`4.2e1`, "42", false},
		{`-42`, "-42", false}, {`false`, "false", false},
		{`9007199254740991`, "9007199254740991", true},
		{`9007199254740992`, "9007199254740992", false},
		{`9007199254740991.1`, "9007199254740991", false},
	} {
		var document any
		require.NoError(t, decodeJSON([]byte(`{"id":`+test.actual+`}`), &document))
		err := checkResponseBindings(document, Operation{ResponseResourceInputs: map[string]string{"/id": "id"}}, map[string]any{"id": test.expected})
		if test.valid {
			require.NoError(t, err)
		} else {
			require.Error(t, err)
		}
	}
	var document any
	require.NoError(t, decodeJSON([]byte(`{"id":42}`), &document))
	require.Error(t, checkResponseBindings(document, Operation{ResponseInputs: map[string]string{"/id": "id"}}, map[string]any{"id": "42"}), "strict typed equality must not be weakened")
}
