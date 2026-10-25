//go:build !integration

package workflow

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCustomJobDetectionGuardExpressionContract(t *testing.T) {
	for _, condition := range []string{
		"always()", "${{always()}}", " \n${{ \n always() \t }} \t",
		"contains('${{ text }}', '}}')", "${{ contains('${{ text }}', '}}') }}",
	} {
		t.Run(condition, func(t *testing.T) {
			job := &Job{Name: "report", Needs: []string{"agent"}, If: condition}
			require.NoError(t, addCustomJobDetectionGuard(job, false))
			assert.Contains(t, job.Needs, "detection")
			assert.Contains(t, job.If, "needs.detection.outputs.detection_success == 'true'")
			assert.NotContains(t, job.If, "(${{")
		})
	}
	for _, condition := range []string{
		"${{ }}", "${{}}", "${{ always()", "always() }}",
		"${{ always() }} trailing", "${{ false }}${{ true }}",
		"${{ always() }} && needs.detection.result == 'success' }}",
	} {
		t.Run(condition, func(t *testing.T) {
			job := &Job{Name: "report", Needs: []string{"agent"}, If: condition}
			require.ErrorContains(t, addCustomJobDetectionGuard(job, false), "jobs.report.if")
			assert.Equal(t, condition, job.If)
			assert.Equal(t, []string{"agent"}, job.Needs)
		})
	}
}
