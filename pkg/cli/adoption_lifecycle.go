package cli

import "github.com/github/gh-aw/pkg/safeoutput"

func attachAdoptionRuleMetadata(report *AdoptionReport, outputType string) {
	if report == nil {
		return
	}
	report.RuleVersion = safeoutput.AdoptionRegistryVersion
	if rule, ok := safeoutput.RuleFor(outputType); ok {
		report.EffectClass = rule.EffectClass
		report.FallbackToDelivery = rule.FallbackToDelivery
		report.AttributionEligible = rule.AttributionEligible
		report.SurvivalRequired = rule.AttributionEligible
	}
}

func finalizeAdoptionEvaluation(report *AdoptionReport, item CreatedItemReport) {
	if report == nil {
		return
	}
	observed := report.AdoptionEvaluation
	delivery, adoption := safeoutput.EvaluateAdoption(item.Type, item.Timestamp, safeoutput.ObservedAdoption{
		Status: string(observed.AdoptionStatus),
		Signal: observed.Signal,
		At:     observed.AdoptionAt,
	})
	report.Delivery = &delivery
	report.AdoptionStatus = AdoptionStatus(adoption.State)
	report.Signal = adoption.Signal
	report.Source = adoption.Source
	report.RuleVersion = adoption.RuleVersion
	report.EffectClass = adoption.EffectClass
	report.FallbackToDelivery = adoption.FallbackToDelivery
	report.AttributionEligible = adoption.AttributionEligible
	report.SurvivalRequired = adoption.SurvivalRequired
	report.AdoptionAt = adoption.AdoptionAt
	report.AcceptanceKind = adoption.AcceptanceKind
	if adoption.Source != "adoption-evaluator" {
		switch adoption.State {
		case safeoutput.AdoptionAccepted, safeoutput.AdoptionRejected, safeoutput.AdoptionPending, safeoutput.AdoptionLifecycle:
			report.EvidenceStrength = EvidenceStrong
		default:
			report.EvidenceStrength = EvidenceNone
		}
	}
}
