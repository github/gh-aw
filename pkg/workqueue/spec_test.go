//go:build !integration

// Package workqueue_test contains black-box tests derived from the package README.
package workqueue_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/github/gh-aw/pkg/workqueue"
)

// TestSpec_Constants_LedgerContract validates the ledger constants documented
// in the package README.
func TestSpec_Constants_LedgerContract(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		actual   any
		expected any
	}{
		{name: "ledger filename", actual: workqueue.FileName, expected: "work-queue.jsonl"},
		{name: "default branch", actual: workqueue.DefaultBranch, expected: "work-queue"},
		{name: "protocol version", actual: workqueue.Version, expected: 3},
		{name: "maximum timestamp", actual: workqueue.MaxTimestamp, expected: int64(9007199254740991)},
		{name: "maximum enqueue timestamp", actual: workqueue.MaxEnqueued, expected: int64(9007199254740991)},
		{name: "maximum claims per dispatch", actual: workqueue.MaxClaimsPerDispatch, expected: 16},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, tt.actual, "%s should match the documented ledger contract", tt.name)
		})
	}
}

// TestSpec_PublicAPI_Canonical validates Public API / Functions: the canonical
// JSON profile is integer-only and preserves values.
// SPEC_AMBIGUITY: The README does not define exact canonical byte ordering or
// numeric bounds, so this test checks values rather than an invented wire format.
func TestSpec_PublicAPI_Canonical(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{name: "integer payload", input: `{"count":42,"ready":true}`},
		{name: "nested integer payload", input: `{"items":[1,2],"detail":{"count":3}}`},
		{name: "fractional number", input: `{"count":1.5}`, wantErr: true},
		{name: "nested fractional number", input: `{"items":[1,2.5]}`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			encoded, err := workqueue.Canonical([]byte(tt.input))
			if tt.wantErr {
				require.Error(t, err, "the integer-only profile should reject fractional values")
				return
			}
			require.NoError(t, err, "integer-valued JSON should be canonicalizable")
			assert.JSONEq(t, tt.input, string(encoded), "canonicalization should preserve JSON values")
			again, err := workqueue.Canonical(encoded)
			require.NoError(t, err, "canonical JSON should remain valid input")
			assert.Equal(t, encoded, again, "canonicalization should be idempotent")
		})
	}
}

// TestSpec_PublicAPI_CanonicalValue validates Public API / Functions: typed
// JSON encoding must preserve numeric values without repairing invalid strings.
func TestSpec_PublicAPI_CanonicalValue(t *testing.T) {
	t.Parallel()
	t.Run("large exact integer", func(t *testing.T) {
		const value int64 = 9007199254740991
		input := struct {
			Count int64 `json:"count"`
		}{Count: value}
		encoded, err := workqueue.CanonicalValue(input)
		require.NoError(t, err, "encoding should preserve an integer within the documented timestamp bound")
		var decoded struct {
			Count int64 `json:"count"`
		}
		require.NoError(t, json.Unmarshal(encoded, &decoded), "typed JSON should decode without a float conversion")
		assert.Equal(t, value, decoded.Count, "typed canonicalization should retain the exact integer")
	})
	t.Run("invalid UTF-8 string", func(t *testing.T) {
		_, err := workqueue.CanonicalValue(map[string]string{"text": string([]byte{0xff})})
		require.Error(t, err, "typed JSON must reject a string that would require repair")
	})
}

// TestSpec_PublicAPI_IndependentGraphID validates Public API / Functions:
// graph identity is computed from the canonical payload.
func TestSpec_PublicAPI_IndependentGraphID(t *testing.T) {
	t.Parallel()
	first, err := workqueue.IndependentGraphID([]byte(`{"name":"task","count":1}`))
	require.NoError(t, err, "an integer-valued task payload should have a graph ID")
	second, err := workqueue.IndependentGraphID([]byte("{ \"count\": 1, \"name\": \"task\" }\n"))
	require.NoError(t, err, "equivalent task JSON should have a graph ID")
	assert.NotEmpty(t, first, "a task payload should yield a graph identity")
	assert.Equal(t, first, second, "equivalent canonical payloads should have the same graph identity")
}

// TestSpec_PublicAPI_DispatchParameters validates Public API / Methods:
// marshaling preserves an explicitly empty worker-profile list.
func TestSpec_PublicAPI_DispatchParameters(t *testing.T) {
	t.Parallel()
	encoded, err := json.Marshal(workqueue.DispatchParameters{WorkerProfiles: []string{}})
	require.NoError(t, err, "dispatch parameters should support JSON marshaling")
	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(encoded, &fields), "dispatch JSON should decode into fields")
	profiles, exists := fields["worker_profiles"]
	require.True(t, exists, "an explicitly empty profile list should remain present")
	assert.JSONEq(t, "[]", string(profiles), "an explicitly empty profile list should remain an empty array")
}

// TestSpec_PublicAPI_Receipts validates Public API / Methods: receipts retain
// identity while omitting work payloads and result references.
func TestSpec_PublicAPI_Receipts(t *testing.T) {
	t.Parallel()
	claim := workqueue.AssignmentClaim{
		Handle:  "claim-handle",
		ClaimID: "claim-id",
		WorkID:  "work-id",
		Work:    json.RawMessage(`{"task":"private-payload-marker"}`),
		ResultRefs: []workqueue.ResultReference{{
			WorkID: "parent-work", ResultCommitID: "result-commit",
			Descriptor: json.RawMessage(`{"detail":"result-descriptor-marker"}`),
		}},
	}
	assignment := workqueue.Assignment{
		DispatchID: "dispatch-id", RequestID: "request-id", CommitID: "commit-id",
		Claims: []workqueue.AssignmentClaim{claim},
	}
	receipt := assignment.ToReceipt()
	assert.Equal(t, assignment.DispatchID, receipt.DispatchID, "receipt should retain dispatch identity")
	assert.Equal(t, assignment.RequestID, receipt.RequestID, "receipt should retain request identity")
	assert.Equal(t, assignment.CommitID, receipt.CommitID, "receipt should retain commit identity")
	require.Len(t, receipt.Claims, 1, "assignment receipt should project its claim identity")
	for _, projected := range []workqueue.ClaimReceipt{claim.ToReceipt(), receipt.Claims[0]} {
		assert.Equal(t, claim.Handle, projected.Handle, "claim receipt should retain its handle")
		assert.Equal(t, claim.ClaimID, projected.ClaimID, "claim receipt should retain claim identity")
		assert.Equal(t, claim.WorkID, projected.WorkID, "claim receipt should retain work identity")
	}
	for _, tt := range []struct {
		name  string
		value any
	}{
		{name: "assignment", value: receipt},
		{name: "claim", value: claim.ToReceipt()},
	} {
		t.Run(tt.name, func(t *testing.T) {
			encoded, err := json.Marshal(tt.value)
			require.NoError(t, err, "receipt should be serializable")
			assert.NotContains(t, string(encoded), "private-payload-marker", "receipt should omit work payloads")
			assert.NotContains(t, string(encoded), "result-descriptor-marker", "receipt should omit result descriptors")
			assert.NotContains(t, string(encoded), "result-commit", "receipt should omit result references")
		})
	}
}

// TestSpec_Types_PublicContracts verifies that the core types documented in the
// package README remain part of the exported API.
func TestSpec_Types_PublicContracts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value any
	}{
		{name: "Actor", value: workqueue.Actor{}},
		{name: "Request", value: workqueue.Request{}},
		{name: "QueueCommit", value: workqueue.QueueCommit{}},
		{name: "Operation", value: workqueue.Operation{}},
		{name: "Policy", value: workqueue.Policy{}},
		{name: "WorkDefinition", value: workqueue.WorkDefinition{}},
		{name: "Projection", value: workqueue.Projection{}},
		{name: "WorkState", value: workqueue.WorkState{}},
		{name: "ClaimState", value: workqueue.ClaimState{}},
		{name: "DispatchState", value: workqueue.DispatchState{}},
		{name: "Assignment", value: workqueue.Assignment{}},
		{name: "Evidence", value: workqueue.Evidence{}},
		{name: "Selection", value: workqueue.Selection{}},
		{name: "Decision", value: workqueue.Decision{}},
		{name: "Settings", value: workqueue.Settings{}},
		{name: "Branch", value: workqueue.Branch{}},
		{name: "NativeDeliveryHost", value: workqueue.NativeDeliveryHost{}},
		{name: "LedgerSession", value: workqueue.LedgerSession{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.NotNil(t, tt.value, "%s should remain an exported public type", tt.name)
		})
	}
}

// TestSpec_PublicAPI_ProtocolErrorFormatting validates the stable error-code
// and message format documented in the package README.
func TestSpec_PublicAPI_ProtocolErrorFormatting(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		code     string
		message  string
		expected string
	}{
		{name: "code and rejection message", code: "invalid_request", message: "request was rejected", expected: "invalid_request: request was rejected"},
		{name: "empty message preserves separator", code: "resource_limit", message: "", expected: "resource_limit: "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := &workqueue.ProtocolError{Code: tt.code, Message: tt.message}
			assert.Equal(t, tt.expected, err.Error(), "ProtocolError should format the documented stable code and message")
		})
	}
}
