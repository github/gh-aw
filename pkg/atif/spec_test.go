//go:build !integration

package atif_test

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/github/gh-aw/pkg/atif"
)

// TestSpec_Constants_ATIFSchema validates the schema version and input bound
// documented in the README "Constants" table.
func TestSpec_Constants_ATIFSchema(t *testing.T) {
	tests := []struct {
		name     string
		actual   any
		expected any
	}{
		{name: "schema version", actual: atif.SchemaVersion, expected: "ATIF-v1.8"},
		{name: "maximum session bytes", actual: atif.MaxSessionBytes, expected: 64 * 1024 * 1024},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.actual, "%s must match the README specification", tt.name)
		})
	}
}

// TestSpec_Types_PublicTypes verifies that every struct documented in the
// README "Types" table is part of the public package surface.
func TestSpec_Types_PublicTypes(t *testing.T) {
	tests := []struct {
		name  string
		value any
	}{
		{name: "Trajectory", value: atif.Trajectory{}},
		{name: "Agent", value: atif.Agent{}},
		{name: "Step", value: atif.Step{}},
		{name: "ToolCall", value: atif.ToolCall{}},
		{name: "Observation", value: atif.Observation{}},
		{name: "ObservationResult", value: atif.ObservationResult{}},
		{name: "FinalMetrics", value: atif.FinalMetrics{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.NotNil(t, tt.value, "%s must remain an exported struct as documented", tt.name)
		})
	}
}

// TestSpec_PublicAPI_ConvertRejectsInvalidInput validates the README contract
// that invalid unified-session JSONL is returned as an error.
func TestSpec_PublicAPI_ConvertRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{name: "malformed JSON", input: "{"},
		{name: "non-object JSON", input: "[]\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			trajectory, err := atif.Convert(context.Background(), strings.NewReader(tt.input))
			assert.Error(t, err, "Convert should reject %s per the README error-handling contract", tt.name)
			assert.Nil(t, trajectory, "Convert should not return a trajectory for %s", tt.name)
		})
	}
}

// TestSpec_PublicAPI_ConvertEnforcesInputLimit validates the documented 64 MiB
// bound without retaining the full input in memory.
func TestSpec_PublicAPI_ConvertEnforcesInputLimit(t *testing.T) {
	input := io.LimitReader(&repeatingReader{pattern: []byte(" \n")}, int64(atif.MaxSessionBytes)+1)

	trajectory, err := atif.Convert(context.Background(), input)

	assert.Error(t, err, "Convert should reject input larger than MaxSessionBytes")
	assert.Nil(t, trajectory, "Convert should not return a trajectory for oversized input")
}

// TestSpec_PublicAPI_ValidateRejectsInvalidTrajectory validates the README
// contract that invalid trajectories are reported as errors.
func TestSpec_PublicAPI_ValidateRejectsInvalidTrajectory(t *testing.T) {
	tests := []struct {
		name       string
		trajectory *atif.Trajectory
	}{
		{name: "nil trajectory", trajectory: nil},
		{name: "empty trajectory", trajectory: &atif.Trajectory{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Error(t, tt.trajectory.Validate(), "%s should fail README-specified trajectory validation", tt.name)
		})
	}
}

// TestSpec_PublicAPI_EncodeValidatesBeforeWriting validates the README contract
// that Encode rejects an invalid trajectory instead of silently emitting it.
func TestSpec_PublicAPI_EncodeValidatesBeforeWriting(t *testing.T) {
	tests := []struct {
		name       string
		trajectory *atif.Trajectory
	}{
		{name: "nil trajectory", trajectory: nil},
		{name: "empty trajectory", trajectory: &atif.Trajectory{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output bytes.Buffer
			err := atif.Encode(&output, tt.trajectory)
			require.Error(t, err, "Encode should reject %s", tt.name)
			assert.Empty(t, output.Bytes(), "Encode should not emit an invalid trajectory for %s", tt.name)
		})
	}
}

type repeatingReader struct {
	pattern []byte
	offset  int
}

func (r *repeatingReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = r.pattern[r.offset]
		r.offset = (r.offset + 1) % len(r.pattern)
	}
	return len(p), nil
}
