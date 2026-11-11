package connectors

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConnectorPhaseSnapshots(t *testing.T) {
	t.Parallel()
	full := fixturePlan(t, "https://api.example.com")
	for phase, expected := range map[string]string{"agent": "READ_TOKEN", "context": "CONTEXT_TOKEN", "safe-output": "WRITE_TOKEN"} {
		t.Run(phase, func(t *testing.T) {
			restricted, err := full.ForPhase(phase)
			require.NoError(t, err)
			assert.Equal(t, []string{expected}, restricted.Secrets())
			encoded, err := restricted.Encode()
			require.NoError(t, err)
			copy, err := DecodePlan(encoded)
			require.NoError(t, err)
			assert.Equal(t, restricted.Digest, copy.Digest)
			again, err := copy.ForPhase(phase)
			require.NoError(t, err)
			assert.Equal(t, restricted.Digest, again.Digest, "phase restriction must be idempotent")
			for _, other := range []string{"agent", "context", "safe-output"} {
				if other != phase {
					_, err := copy.ForPhase(other)
					assert.Error(t, err)
				}
			}
		})
	}
}

func TestConnectorFrozenSnapshotRejectsTamper(t *testing.T) {
	t.Parallel()
	plan := fixturePlan(t, "https://api.example.com")
	encoded, err := plan.Encode()
	require.NoError(t, err)
	raw, err := base64.StdEncoding.DecodeString(encoded)
	require.NoError(t, err)
	var document map[string]any
	require.NoError(t, json.Unmarshal(raw, &document))
	document["digest"] = strings.Repeat("0", 64)
	raw, err = json.Marshal(document)
	require.NoError(t, err)
	_, err = DecodePlan(base64.StdEncoding.EncodeToString(raw))
	require.ErrorContains(t, err, "digest mismatch")
	_, err = DecodePlan(strings.Repeat("a", MaxEncodedPlanBytes+1))
	require.ErrorContains(t, err, "byte bound")
	_, err = DecodePlan("not-base64")
	require.ErrorContains(t, err, "base64")
}

func TestConnectorPhaseKeepsResponseOnlyGrants(t *testing.T) {
	t.Parallel()
	plan := fixturePlan(t, "https://api.example.com")
	op := plan.spec.Context["issue-details"]
	op.Resources = nil
	op.ResponseResources = map[string]string{"/id": "issues"}
	plan.spec.Context["issue-details"] = op
	restricted, err := plan.ForPhase("context")
	require.NoError(t, err)
	assert.Equal(t, []string{"ISSUE-1"}, restricted.connection.Resources["issues"])
	delete(plan.connection.Resources, "issues")
	err = validateSelection(plan.spec, plan.binding, plan.connection)
	require.ErrorContains(t, err, "allowlist")
}

func TestConnectorHTTPMCPPhaseSnapshot(t *testing.T) {
	t.Parallel()
	plan := fixturePlan(t, "https://api.example.com")
	name := strings.Repeat("a", 63)
	plan.spec.HTTPMCP = map[string]HTTPMCP{name: {
		URL: "https://mcp.example.com/mcp", Credential: "reader", ReadOnly: true, MaxResponseBytes: 4096,
		Tools: map[string]MCPTool{"get": {Inputs: map[string]Input{}}},
	}}
	plan.binding.MCP = nil
	plan.binding.HTTPMCP = []string{name}
	restricted, err := plan.ForPhase("agent")
	require.NoError(t, err)
	assert.Equal(t, []string{"https://mcp.example.com"}, restricted.Origins())
	encoded, err := restricted.Encode()
	require.NoError(t, err)
	copy, err := DecodePlan(encoded)
	require.NoError(t, err)
	assert.Equal(t, restricted.Digest, copy.Digest)
}

func TestConnectorSnapshotPreservesStructuredContract(t *testing.T) {
	t.Parallel()
	plan := fixturePlan(t, "https://api.example.com")
	op := plan.spec.SafeOutputs["add-comment"]
	op.Request.Body = map[string]Value{"fields": {Object: map[string]Value{
		"description": {Input: "text", Encoding: "jira-adf"},
		"empty-array": {Array: []Value{}}, "empty-object": {Object: map[string]Value{}},
		"values": {Array: []Value{{Literal: false}, {Literal: 0}, {Literal: json.Number("1.0")}, {Literal: json.Number("9007199254740993")}}},
	}}}
	op.ResponseChecks = map[string]any{"/id": json.Number("9007199254740993")}
	op.ResponseInputs = map[string]string{"/issue": "issue"}
	op.ResponseTypes = map[string]string{"/id": "number"}
	op.ResponseResources = map[string]string{"/issue": "issues"}
	op.ResponseRequired = []string{"/id"}
	op.Project = map[string]string{"id": "/id"}
	plan.spec.SafeOutputs["add-comment"] = op
	restricted, err := plan.ForPhase("safe-output")
	require.NoError(t, err)
	encoded, err := restricted.Encode()
	require.NoError(t, err)
	restored, err := DecodePlan(encoded)
	require.NoError(t, err)
	before, err := json.Marshal(op)
	require.NoError(t, err)
	after, err := json.Marshal(restored.spec.SafeOutputs["add-comment"])
	require.NoError(t, err)
	assert.JSONEq(t, string(before), string(after))
	assert.Contains(t, string(after), "9007199254740993")
	assert.Contains(t, string(after), "1.0")
	request, err := restored.PrepareSafeOutput("add-comment", map[string]any{"issue": "ISSUE-1", "text": "<script>"})
	require.NoError(t, err)
	assert.Equal(t, restored.Digest, request.Digest)
	body, _, err := requestBody(restored.spec.SafeOutputs["add-comment"].Request, request.Arguments)
	require.NoError(t, err)
	assert.Contains(t, string(body), `"empty-array":[]`)
	assert.Contains(t, string(body), `"empty-object":{}`)
	assert.Contains(t, string(body), `"values":[false,0,1.0,9007199254740993]`)
}

func TestConnectorSnapshotRejectsUnknownFieldsAndTrailingJSON(t *testing.T) {
	t.Parallel()
	plan := fixturePlan(t, "https://api.example.com")
	encoded, err := plan.Encode()
	require.NoError(t, err)
	raw, err := base64.StdEncoding.DecodeString(encoded)
	require.NoError(t, err)
	_, err = DecodePlan(base64.StdEncoding.EncodeToString(append(raw, []byte("\n{}")...)))
	require.ErrorContains(t, err, "exactly one document")
	var document map[string]any
	require.NoError(t, decodeJSON(raw, &document))
	document["untrusted"] = true
	raw, err = json.Marshal(document)
	require.NoError(t, err)
	_, err = DecodePlan(base64.StdEncoding.EncodeToString(raw))
	require.ErrorContains(t, err, "unknown field")
}
