package lifecycle

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"time"
)

type Identity struct {
	Host             string `json:"host"`
	Repository       string `json:"repository"`
	Workflow         string `json:"workflow,omitempty"`
	RunID            int64  `json:"run_id"`
	RunAttempt       int    `json:"run_attempt"`
	InvocationID     string `json:"invocation_id"`
	EvidenceID       string `json:"evidence_id"`
	Type             string `json:"type"`
	TargetURL        string `json:"target_url,omitempty"`
	TargetRepository string `json:"target_repository,omitempty"`
}

type Delivery struct {
	State      string    `json:"state"`
	Signal     string    `json:"signal"`
	At         time.Time `json:"at"`
	ObservedAt time.Time `json:"observed_at"`
	Source     string    `json:"source"`
}

// Actor is the provider-reported account that resolved the adopted resource.
type Actor struct {
	Login string `json:"login"`
	Type  string `json:"type,omitempty"`
}

type Adoption struct {
	State          string    `json:"state"`
	Signal         string    `json:"signal"`
	At             time.Time `json:"at,omitzero"`
	ObservedAt     time.Time `json:"observed_at"`
	Source         string    `json:"source"`
	RuleVersion    string    `json:"rule_version"`
	AcceptanceKind string    `json:"acceptance_kind,omitempty"`
	Actor          Actor     `json:"actor,omitzero"`
	HumanVerified  bool      `json:"human_verified"`
	Coverage       string    `json:"coverage"`
	Error          string    `json:"error,omitempty"`
}

type Survival struct {
	State      string    `json:"state"`
	Signal     string    `json:"signal,omitempty"`
	ObservedAt time.Time `json:"observed_at,omitzero"`
	Coverage   string    `json:"coverage"`
	Error      string    `json:"error,omitempty"`
}

func (a Adoption) Equal(b Adoption) bool {
	a.At, b.At = a.At.UTC(), b.At.UTC()
	a.ObservedAt, b.ObservedAt = a.ObservedAt.UTC(), b.ObservedAt.UTC()
	return a == b
}

func (a Survival) Equal(b Survival) bool {
	a.ObservedAt, b.ObservedAt = a.ObservedAt.UTC(), b.ObservedAt.UTC()
	return a == b
}

type Record struct {
	ID string `json:"id"`
	Identity
	Delivery       Delivery   `json:"delivery"`
	Adoption       Adoption   `json:"adoption"`
	Survival       Survival   `json:"survival"`
	ReversalSignal string     `json:"reversal_signal,omitempty"`
	ReversedAt     time.Time  `json:"reversed_at,omitzero"`
	Execution      *Execution `json:"execution,omitempty"`
}

// Execution retains the selectors needed to recheck an effect after artifacts expire.
type Execution struct {
	Number       int            `json:"number,omitempty"`
	LabelsBefore []string       `json:"labels_before"`
	LabelsAdded  []string       `json:"labels_added,omitempty"`
	Metadata     map[string]any `json:"metadata,omitempty"`
	BeforeState  map[string]any `json:"before_state,omitempty"`
	AfterState   map[string]any `json:"after_state,omitempty"`
}

// Delivered accepts execution evidence, never a request or a job conclusion.
func Delivered(identity Identity, at, observedAt time.Time, source string) (Record, error) {
	if identity.Host == "" || identity.Repository == "" || identity.RunID <= 0 ||
		identity.RunAttempt <= 0 || identity.InvocationID == "" || identity.EvidenceID == "" ||
		identity.Type == "" || at.IsZero() || observedAt.IsZero() || source == "" {
		return Record{}, errors.New("delivery requires complete execution identity and event clocks")
	}
	if identity.TargetURL != "" {
		target, err := url.Parse(identity.TargetURL)
		if err != nil || target.Scheme != "https" || target.Host != identity.Host ||
			target.User != nil || target.RawQuery != "" {
			return Record{}, errors.New("delivery target must be an exact HTTPS resource on the execution host")
		}
	}
	key, err := json.Marshal(identity)
	if err != nil {
		return Record{}, err
	}
	digest := sha256.Sum256(key)
	record := Record{
		ID: "delivery-sha256-" + hex.EncodeToString(digest[:]), Identity: identity,
		Delivery: Delivery{"delivered", "effect_applied", at.UTC(), observedAt.UTC(), source},
		Adoption: Adoption{
			State: "unknown", Signal: "unsupported_safe_output", ObservedAt: observedAt.UTC(),
			Source: "adoption-registry", RuleVersion: RuleVersion, Coverage: "unsupported",
		},
		Survival: Survival{State: "unknown", Coverage: "unobserved"},
	}
	rule, registered := RuleFor(identity.Type)
	if !registered {
		return record, nil
	}
	record.Adoption.Coverage = "available"
	switch {
	case !rule.ValueEligible:
		record.Adoption.State, record.Adoption.Signal = "not_applicable", "no_repository_value_effect"
	case rule.FallbackToDelivery:
		record.Adoption.State, record.Adoption.Signal = "accepted", "delivered"
		record.Adoption.At, record.Adoption.Source = at.UTC(), "delivery-rule"
		record.Adoption.AcceptanceKind = "fallback_delivery"
	default:
		record.Adoption.State, record.Adoption.Signal = "pending", "human_decision_pending"
		record.Adoption.Coverage = "unobserved"
	}
	return record, nil
}

type Observation struct {
	State          string
	Signal         string
	At             time.Time
	ObservedAt     time.Time
	AcceptanceKind string
	Actor          Actor
	HumanVerified  bool
	Error          string
}

func Observe(record Record, observation Observation) (Record, error) {
	if observation.ObservedAt.IsZero() {
		return Record{}, errors.New("adoption observation requires an observation clock")
	}
	if observation.Error != "" {
		record.Adoption.Coverage, record.Adoption.Error = "unavailable", observation.Error
		record.Adoption.ObservedAt = observation.ObservedAt.UTC()
		return record, nil
	}
	switch observation.State {
	case "pending", "accepted", "rejected", "lifecycle", "not_applicable", "unknown":
	default:
		return Record{}, errors.New("invalid adoption state")
	}
	if observation.State == "accepted" && (observation.At.IsZero() ||
		observation.AcceptanceKind != "explicit_human" && observation.AcceptanceKind != "fallback_delivery" &&
			observation.AcceptanceKind != "automation" ||
		observation.AcceptanceKind == "explicit_human" && !observation.HumanVerified) {
		return Record{}, errors.New("adoption requires a verified decision, an automation decision, or explicit delivery fallback")
	}
	if observation.AcceptanceKind == "automation" && (observation.HumanVerified || observation.Actor.Login == "") {
		return Record{}, errors.New("automation decision requires its actor and cannot claim human verification")
	}
	if observation.State == "accepted" {
		rule, ok := RuleFor(record.Type)
		if !ok || !rule.ValueEligible {
			return Record{}, errors.New("adoption requires a registered value-eligible output type")
		}
		if observation.AcceptanceKind == "fallback_delivery" &&
			(!rule.FallbackToDelivery || observation.HumanVerified || !observation.At.Equal(record.Delivery.At)) {
			return Record{}, errors.New("delivery fallback is not permitted by this adoption rule")
		}
	}
	if !observation.At.IsZero() && observation.At.Before(record.Delivery.At) {
		return Record{}, errors.New("adoption decision precedes delivery")
	}
	previous := record.Adoption
	record.Adoption = Adoption{
		State: observation.State, Signal: observation.Signal, At: observation.At.UTC(),
		ObservedAt: observation.ObservedAt.UTC(), Source: "github-api", RuleVersion: RuleVersion,
		AcceptanceKind: observation.AcceptanceKind, Actor: observation.Actor,
		HumanVerified: observation.HumanVerified, Coverage: "available",
	}
	if observation.State == "accepted" {
		record.Survival = Survival{State: "observed", Signal: "resource_still_accepted", ObservedAt: observation.ObservedAt.UTC(), Coverage: "snapshot"}
	} else if previous.State == "accepted" && observation.State != "unknown" {
		record.Survival = Survival{State: "reversed", Signal: observation.Signal, ObservedAt: observation.ObservedAt.UTC(), Coverage: "snapshot"}
		record.ReversalSignal = observation.Signal
		record.ReversedAt = observation.At.UTC()
	}
	return record, nil
}

func authority(source string) int {
	if source == "github-api" {
		return 2
	}
	return 1
}

// Merge never lets replayed delivery roll back a later provider observation.
func Merge(previous, incoming Record) (Record, error) {
	expected, err := Delivered(incoming.Identity, incoming.Delivery.At, incoming.Delivery.ObservedAt, incoming.Delivery.Source)
	if err != nil {
		return Record{}, err
	}
	if expected.ID != incoming.ID || incoming.Delivery.State != "delivered" ||
		incoming.Adoption.RuleVersion != RuleVersion {
		return Record{}, errors.New("invalid lifecycle identity, delivery state, or rule version")
	}
	if _, err := Observe(expected, Observation{
		State: incoming.Adoption.State, Signal: incoming.Adoption.Signal,
		At: incoming.Adoption.At, ObservedAt: incoming.Adoption.ObservedAt,
		AcceptanceKind: incoming.Adoption.AcceptanceKind, Actor: incoming.Adoption.Actor,
		HumanVerified: incoming.Adoption.HumanVerified,
	}); err != nil {
		return Record{}, err
	}
	if previous.ID == "" {
		return incoming, nil
	}
	if previous.ID != incoming.ID || previous.Identity != incoming.Identity ||
		!previous.Delivery.At.Equal(incoming.Delivery.At) ||
		previous.Adoption.RuleVersion != incoming.Adoption.RuleVersion {
		return Record{}, errors.New("conflicting lifecycle identity or delivery evidence")
	}
	if previous.Execution != nil {
		if incoming.Execution != nil {
			oldExecution, err := json.Marshal(previous.Execution)
			if err != nil {
				return Record{}, err
			}
			newExecution, err := json.Marshal(incoming.Execution)
			if err != nil {
				return Record{}, err
			}
			if !bytes.Equal(oldExecution, newExecution) {
				return Record{}, errors.New("conflicting retained execution evidence")
			}
		}
		incoming.Execution = previous.Execution
	}
	incoming.Delivery.Source = previous.Delivery.Source
	old, next := previous.Adoption, incoming.Adoption
	if authority(old.Source) > authority(next.Source) ||
		authority(old.Source) == authority(next.Source) && old.ObservedAt.After(next.ObservedAt) {
		incoming.Adoption = old
	} else if old.Source == next.Source && old.ObservedAt.Equal(next.ObservedAt) &&
		!old.Equal(next) {
		return Record{}, errors.New("conflicting equal-clock adoption evidence")
	}
	if previous.Survival.ObservedAt.After(incoming.Survival.ObservedAt) {
		incoming.Survival = previous.Survival
	}
	if previous.ReversalSignal != "" && incoming.ReversalSignal == "" ||
		previous.ReversedAt.After(incoming.ReversedAt) {
		incoming.ReversedAt, incoming.ReversalSignal = previous.ReversedAt, previous.ReversalSignal
	}
	return incoming, nil
}

func IsHuman(actorType, login string, publishers []string) bool {
	if actorType != "User" || login == "" || strings.HasSuffix(strings.ToLower(login), "[bot]") {
		return false
	}
	for _, publisher := range publishers {
		if strings.EqualFold(login, publisher) {
			return false
		}
	}
	return true
}
