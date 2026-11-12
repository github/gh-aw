//go:build !integration

package workflow

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateAtomicExternalStoragePolicy(t *testing.T) {
	for _, test := range []struct {
		name        string
		engine      string
		value       string
		strict      bool
		wantError   bool
		wantWarning bool
	}{
		{name: "unset", strict: false},
		{name: "off", value: "off", strict: true},
		{name: "on warns in non-strict mode", value: "on", strict: false, wantWarning: true},
		{name: "on errors in strict mode", value: "on", strict: true, wantError: true},
		{name: "case-insensitive on errors in strict mode", value: " ON ", strict: true, wantError: true},
		{name: "other engine ignores Atomic setting", engine: "copilot", value: "on", strict: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			compiler := NewCompiler()
			compiler.strictMode = test.strict
			initialWarnings := compiler.GetWarningCount()
			engineID := test.engine
			if engineID == "" {
				engineID = "atomic"
			}
			err := compiler.validateAtomicExternalStoragePolicy(&WorkflowData{
				RawFrontmatter: map[string]any{"strict": test.strict},
				EngineConfig:   &EngineConfig{ID: engineID, Env: map[string]string{"ATOMIC_STORAGE": test.value}},
			})

			if test.wantError {
				require.Error(t, err)
				assert.ErrorContains(t, err, "engine.env.ATOMIC_STORAGE")
				assert.ErrorContains(t, err, "strict mode")
				assert.ErrorContains(t, err, "before post-run threat detection")
			} else {
				require.NoError(t, err)
			}
			if test.wantWarning {
				assert.Equal(t, initialWarnings+1, compiler.GetWarningCount())
			} else {
				assert.Equal(t, initialWarnings, compiler.GetWarningCount())
			}
		})
	}
}
