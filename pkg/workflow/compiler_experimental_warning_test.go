//go:build !integration

package workflow

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExperimentalFeatureNoticeClassification(t *testing.T) {
	t.Parallel()
	for _, batch := range []bool{false, true} {
		compiler := NewCompiler()
		compiler.SetBatchMode(batch)
		data := &WorkflowData{
			LSP:         map[string]LSPServerConfig{"typescript": {}},
			SafeOutputs: &SafeOutputsConfig{Steer: true},
		}
		var output bytes.Buffer
		compiler.emitExperimentalFeatureWarningsTo(data, &output)
		assert.Equal(t, 2, compiler.GetWarningCount())
		assert.Equal(t, 2, compiler.GetExperimentalWarningCount())
		if batch {
			assert.Equal(t, 1, compiler.GetExperimentalFeatureUsage()["Using experimental feature: lsp"])
			assert.Equal(t, 1, compiler.GetExperimentalFeatureUsage()["Using experimental feature: safe-outputs steer"])
		} else {
			assert.Contains(t, output.String(), "Using experimental feature: lsp")
			assert.Contains(t, output.String(), "Using experimental feature: safe-outputs steer")
		}
	}
}

func TestExperimentalWarningAccounting(t *testing.T) {
	t.Parallel()
	for _, batch := range []bool{false, true} {
		compiler := NewCompiler()
		compiler.SetBatchMode(batch)
		compiler.IncrementWarningCount()
		assert.Zero(t, compiler.GetExperimentalWarningCount())
		compiler.IncrementExperimentalWarningCount()
		assert.Equal(t, 2, compiler.GetWarningCount())
		assert.Equal(t, 1, compiler.GetExperimentalWarningCount())
		compiler.ResetWarningCount()
		assert.Zero(t, compiler.GetWarningCount())
		assert.Zero(t, compiler.GetExperimentalWarningCount())
	}

}
