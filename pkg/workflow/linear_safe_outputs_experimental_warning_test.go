//go:build !integration

package workflow

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLinearSafeOutputsExperimentalWarning(t *testing.T) {
	const warning = "Using experimental feature: Linear safe outputs"

	tests := []struct {
		name        string
		safeOutputs *SafeOutputsConfig
		expect      bool
	}{
		{name: "create issue", safeOutputs: &SafeOutputsConfig{LinearCreateIssue: &LinearCreateIssueConfig{}}, expect: true},
		{name: "add comment", safeOutputs: &SafeOutputsConfig{LinearAddComment: &LinearTargetConfig{}}, expect: true},
		{name: "update issue", safeOutputs: &SafeOutputsConfig{LinearUpdateIssue: &LinearUpdateIssueConfig{}}, expect: true},
		{name: "non-Linear output", safeOutputs: &SafeOutputsConfig{CreateIssues: &CreateIssuesConfig{}}},
		{name: "no safe outputs"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()
			var output bytes.Buffer
			compiler.emitExperimentalFeatureWarningsTo(&WorkflowData{SafeOutputs: tt.safeOutputs}, &output)

			if tt.expect {
				assert.Contains(t, output.String(), warning)
				assert.Equal(t, 1, compiler.GetWarningCount())
			} else {
				assert.NotContains(t, output.String(), warning)
			}
		})
	}
}
