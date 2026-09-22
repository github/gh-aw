//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEmitCustomStepsDoesNotRestoreSharedLogsCache(t *testing.T) {
	data := &WorkflowData{
		On:          "schedule: daily",
		CustomSteps: "steps:\n  - name: Download logs\n    run: gh aw logs",
	}
	var yaml strings.Builder

	(&Compiler{}).emitCustomSteps(&yaml, data, false, nil)

	assert.NotContains(t, yaml.String(), "Restore shared agentic logs cache")
}
