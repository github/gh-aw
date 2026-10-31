//go:build !integration

package workflow

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildDetectionAcceptedCondition(t *testing.T) {
	condition := RenderCondition(buildDetectionAcceptedCondition())
	assert.Equal(t, "needs.detection.result == 'success' && needs.detection.outputs.detection_success == 'true'", condition)
	node, err := ParseExpression(condition)
	require.NoError(t, err)
	for _, result := range []string{"success", "failure", "cancelled", "skipped", ""} {
		for _, accepted := range []string{"true", "false", ""} {
			values := map[string]string{
				"needs.detection.result":                    result,
				"needs.detection.outputs.detection_success": accepted,
			}
			assert.Equal(t, result == "success" && accepted == "true", evaluateDetectionRedactionGate(t, node, values),
				"result=%q detection_success=%q", result, accepted)
		}
	}
}
