// Package safeoutput defines delivery and adoption semantics for safe outputs.
package safeoutput

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"slices"
	"sync"
	"time"
)

const AdoptionRegistryVersion = "gh-aw-v1.2.3-adoption-v1"

type EffectClass string

const (
	EffectProposal    EffectClass = "proposal"
	EffectPublication EffectClass = "publication"
	EffectMutation    EffectClass = "mutation"
	EffectDelegation  EffectClass = "delegation"
	EffectInfra       EffectClass = "infra"
)

type DeliveryState string

const (
	DeliveryDelivered DeliveryState = "delivered"
)

type AdoptionState string

const (
	AdoptionAccepted      AdoptionState = "accepted"
	AdoptionRejected      AdoptionState = "rejected"
	AdoptionPending       AdoptionState = "pending"
	AdoptionLifecycle     AdoptionState = "lifecycle"
	AdoptionNotApplicable AdoptionState = "not_applicable"
	AdoptionUnknown       AdoptionState = "unknown"
)

type Rule struct {
	Type                string      `json:"type"`
	EffectClass         EffectClass `json:"effectClass"`
	FallbackToDelivery  bool        `json:"fallbackToDelivery"`
	AttributionEligible bool        `json:"attributionEligible"`
}

type registryDocument struct {
	Version string `json:"version"`
	Rules   []Rule `json:"rules"`
}

type DeliveryEvaluation struct {
	State  DeliveryState `json:"state"`
	Signal string        `json:"signal"`
	At     string        `json:"at,omitempty"`
	Source string        `json:"source"`
}

type AdoptionEvaluation struct {
	State               AdoptionState `json:"state"`
	Signal              string        `json:"signal"`
	Source              string        `json:"source"`
	RuleVersion         string        `json:"rule_version"`
	EffectClass         EffectClass   `json:"effect_class,omitempty"`
	FallbackToDelivery  bool          `json:"fallback_to_delivery"`
	AttributionEligible bool          `json:"attribution_eligible"`
	SurvivalRequired    bool          `json:"survival_required"`
	AdoptionAt          string        `json:"adoption_at,omitempty"`
	AcceptanceKind      string        `json:"acceptance_kind,omitempty"`
}

type ObservedAdoption struct {
	Status string
	Signal string
	At     string
}

//go:embed adoption_rules.json
var adoptionRulesJSON []byte

var adoptionRegistry = sync.OnceValue(func() registryDocument {
	var document registryDocument
	if err := json.Unmarshal(adoptionRulesJSON, &document); err != nil {
		panic(fmt.Sprintf("BUG: cannot parse embedded safe-output adoption registry: %v", err))
	}
	if document.Version != AdoptionRegistryVersion {
		panic(fmt.Sprintf("BUG: safe-output adoption registry version is %q, expected %q", document.Version, AdoptionRegistryVersion))
	}
	seen := make(map[string]struct{}, len(document.Rules))
	for _, rule := range document.Rules {
		if rule.Type == "" || !validEffectClass(rule.EffectClass) {
			panic(fmt.Sprintf("BUG: invalid safe-output adoption rule for %q", rule.Type))
		}
		if _, exists := seen[rule.Type]; exists {
			panic(fmt.Sprintf("BUG: duplicate safe-output adoption rule for %q", rule.Type))
		}
		seen[rule.Type] = struct{}{}
	}
	if !slices.IsSortedFunc(document.Rules, func(a, b Rule) int {
		switch {
		case a.Type < b.Type:
			return -1
		case a.Type > b.Type:
			return 1
		default:
			return 0
		}
	}) {
		panic("BUG: safe-output adoption registry must be sorted by type")
	}
	return document
})

func validEffectClass(class EffectClass) bool {
	return class == EffectProposal || class == EffectPublication || class == EffectMutation ||
		class == EffectDelegation || class == EffectInfra
}

func RuleFor(outputType string) (Rule, bool) {
	for _, rule := range adoptionRegistry().Rules {
		if rule.Type == outputType {
			return rule, true
		}
	}
	return Rule{}, false
}

func Rules() []Rule {
	return slices.Clone(adoptionRegistry().Rules)
}

func EvaluateDelivery(outputType, deliveredAt string) (DeliveryEvaluation, AdoptionEvaluation) {
	delivery := DeliveryEvaluation{
		State: DeliveryDelivered, Signal: "safe_output.result",
		At: canonicalTimestamp(deliveredAt), Source: "safe-output-manifest",
	}
	rule, ok := RuleFor(outputType)
	if !ok {
		return delivery, AdoptionEvaluation{
			State: AdoptionUnknown, Signal: "unsupported_safe_output",
			Source: "adoption-registry", RuleVersion: AdoptionRegistryVersion,
		}
	}
	adoption := adoptionFromRule(rule)
	switch {
	case !rule.AttributionEligible:
		adoption.State = AdoptionNotApplicable
		adoption.Signal = "no_repository_value_effect"
		adoption.Source = "adoption-registry"
	case rule.EffectClass == EffectProposal && !rule.FallbackToDelivery:
		adoption.State = AdoptionPending
		adoption.Signal = "human_decision_pending"
		adoption.Source = "adoption-registry"
	case rule.FallbackToDelivery:
		adoption.State = AdoptionAccepted
		adoption.Signal = "delivered"
		adoption.Source = "delivery-rule"
		adoption.AdoptionAt = delivery.At
		adoption.AcceptanceKind = "fallback_delivery"
	default:
		adoption.State = AdoptionUnknown
		adoption.Signal = "missing_acceptance_rule"
		adoption.Source = "adoption-registry"
	}
	return delivery, adoption
}

func EvaluateAdoption(outputType, deliveredAt string, observed ObservedAdoption) (DeliveryEvaluation, AdoptionEvaluation) {
	delivery, adoption := EvaluateDelivery(outputType, deliveredAt)
	signal := observed.Signal
	if signal == "" {
		signal = observed.Status
	}
	decisionAt := canonicalTimestamp(observed.At)
	switch observed.Status {
	case "accepted":
		adoption.State = AdoptionAccepted
		adoption.Signal = signal
		adoption.Source = "adoption-evaluator"
		adoption.AdoptionAt = decisionAt
		adoption.AcceptanceKind = "observed_adoption"
	case "rejected":
		adoption.State = AdoptionRejected
		adoption.Signal = signal
		adoption.Source = "adoption-evaluator"
		adoption.AdoptionAt = ""
		adoption.AcceptanceKind = ""
	case "lifecycle":
		adoption.State = AdoptionLifecycle
		adoption.Signal = signal
		adoption.Source = "adoption-evaluator"
		adoption.AdoptionAt = ""
		adoption.AcceptanceKind = ""
	case "pending":
		if adoption.State == AdoptionPending {
			adoption.Signal = signal
			adoption.Source = "adoption-evaluator"
		}
	}
	return delivery, adoption
}

func adoptionFromRule(rule Rule) AdoptionEvaluation {
	return AdoptionEvaluation{
		RuleVersion: AdoptionRegistryVersion, EffectClass: rule.EffectClass,
		FallbackToDelivery: rule.FallbackToDelivery, AttributionEligible: rule.AttributionEligible,
		SurvivalRequired: rule.AttributionEligible,
	}
}

func canonicalTimestamp(value string) string {
	if value == "" {
		return ""
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return ""
	}
	return parsed.UTC().Format(time.RFC3339Nano)
}
