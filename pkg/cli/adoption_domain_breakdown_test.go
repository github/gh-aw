//go:build !integration

package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestComputeDomainBreakdowns_SortsByValueThenLabel(t *testing.T) {
	t.Parallel()
	reports := []AdoptionReport{
		{
			AdoptionEvaluation: AdoptionEvaluation{AdoptionStatus: AdoptionStatusAccepted},
			ObjectiveValue:     20,
			ObjectiveLabels:    []string{"beta"},
		},
		{
			AdoptionEvaluation: AdoptionEvaluation{AdoptionStatus: AdoptionStatusAccepted},
			ObjectiveValue:     20,
			ObjectiveLabels:    []string{"alpha"},
		},
		{
			AdoptionEvaluation: AdoptionEvaluation{AdoptionStatus: AdoptionStatusRejected},
			ObjectiveLabels:    []string{"gamma"},
		},
	}

	breakdowns := ComputeDomainBreakdowns(reports)
	require.Len(t, breakdowns, 3)
	assert.Equal(t, []string{"alpha", "beta", "gamma"}, []string{breakdowns[0].Label, breakdowns[1].Label, breakdowns[2].Label})
	assert.Equal(t, []int{20, 20, 0}, []int{breakdowns[0].TotalObjectiveValue, breakdowns[1].TotalObjectiveValue, breakdowns[2].TotalObjectiveValue})
}
