package cli

import (
	"testing"

	"github.com/github/gh-aw/pkg/safeoutput"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFinalizeAdoptionEvaluation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name           string
		itemType       string
		observed       AdoptionEvaluation
		state          AdoptionStatus
		source         string
		strength       EvidenceStrength
		acceptanceKind string
	}{
		{
			name: "accepted proposal", itemType: "create_issue",
			observed: func() AdoptionEvaluation {
				evaluation := adoptionEvidence(AdoptionStatusAccepted, EvidenceStrong, "completed")
				evaluation.AdoptionAt = "2026-11-16T13:00:00Z"
				return evaluation
			}(),
			state: AdoptionStatusAccepted, source: "adoption-evaluator",
			strength: EvidenceStrong, acceptanceKind: "observed_adoption",
		},
		{
			name: "pending proposal", itemType: "create_issue",
			observed: adoptionEvidence(AdoptionStatusPending, EvidenceMedium, "open"),
			state:    AdoptionStatusPending, source: "adoption-evaluator",
			strength: EvidenceMedium,
		},
		{
			name: "unavailable proposal observation", itemType: "create_issue",
			observed: adoptionEvidence(AdoptionStatusUnknown, EvidenceWeak, "evaluation_error"),
			state:    AdoptionStatusPending, source: "adoption-registry",
			strength: EvidenceStrong,
		},
		{
			name: "pending publication uses delivery fallback", itemType: "add_comment",
			observed: adoptionEvidence(AdoptionStatusPending, EvidenceMedium, "pending"),
			state:    AdoptionStatusAccepted, source: "delivery-rule",
			strength: EvidenceStrong, acceptanceKind: "fallback_delivery",
		},
		{
			name: "failed delegation", itemType: "dispatch_workflow",
			observed: adoptionEvidence(AdoptionStatusRejected, EvidenceStrong, "workflow_failed"),
			state:    AdoptionStatusRejected, source: "adoption-evaluator",
			strength: EvidenceStrong,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report := AdoptionReport{AdoptionEvaluation: tt.observed}
			finalizeAdoptionEvaluation(&report, CreatedItemReport{
				Type: tt.itemType, Timestamp: "2026-11-16T12:00:00Z",
			})
			require.NotNil(t, report.Delivery)
			assert.Equal(t, safeoutput.DeliveryDelivered, report.Delivery.State)
			assert.Equal(t, tt.state, report.AdoptionStatus)
			assert.Equal(t, tt.source, report.Source)
			assert.Equal(t, tt.strength, report.EvidenceStrength)
			assert.Equal(t, tt.acceptanceKind, report.AcceptanceKind)
			assert.Equal(t, safeoutput.AdoptionRegistryVersion, report.RuleVersion)
			if tt.name == "accepted proposal" {
				assert.Equal(t, "2026-11-16T13:00:00Z", report.AdoptionAt)
			}
		})
	}
}
