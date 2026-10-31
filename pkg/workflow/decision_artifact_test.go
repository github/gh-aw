//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDecisionArtifactStep(t *testing.T) {
	for _, reusable := range []bool{false, true} {
		data := &WorkflowData{On: "on:\n  issues:\n"}
		if reusable {
			data.On = "on:\n  workflow_call:\n"
		}
		step := strings.Join(NewCompiler().appendDecisionArtifactStep(data, nil), "")
		assert.Contains(t, step, "if: always()")
		assert.Contains(t, step, "continue-on-error: true")
		assert.Contains(t, step, "path: ${{ runner.temp }}/gh-aw/decision-gate.json")
		assert.Contains(t, step, "if-no-files-found: ignore")
		if reusable {
			assert.Contains(t, step, "name: ${{ steps.setup.outputs.span-id }}-decision-gate")
		} else {
			assert.Contains(t, step, "name: decision-gate\n")
		}
	}
}
