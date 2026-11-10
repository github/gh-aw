//go:build !integration

// Package workqueue_test contains black-box tests derived from the package README.
package workqueue_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

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
