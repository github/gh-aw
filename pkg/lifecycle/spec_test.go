//go:build !integration

package lifecycle_test

import (
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/github/gh-aw/pkg/lifecycle"
)

// These tests derive from the Public API, Usage Example, and Design Decisions
// sections of README.md. Only exported declarations were consulted in source.

func specIdentity() lifecycle.Identity {
	return lifecycle.Identity{
		Host: "github.com", Repository: "org/source", Workflow: "worker",
		RunID: 42, RunAttempt: 1, InvocationID: "worker:1",
		EvidenceID: "result:1", Type: "create_issue",
	}
}

func TestSpec_Constants_RuleVersion(t *testing.T) {
	assert.Equal(t, "gh-aw-adoption-v1", lifecycle.RuleVersion,
		"Public API documents the adoption rule version")
}

func TestSpec_PublicAPI_Delivered(t *testing.T) {
	at := time.Date(2026, 11, 18, 10, 0, 0, 0, time.UTC)
	identity := specIdentity()
	first, err := lifecycle.Delivered(identity, at, at, "safe-output-manifest")
	require.NoError(t, err, "the README usage example must produce a delivery")
	require.NotEmpty(t, first.ID, "a delivery must identify its effect")
	assert.Equal(t, identity, first.Identity, "a delivery must retain execution identity")
	assert.True(t, at.Equal(first.Delivery.At), "delivery must retain the event clock")
	assert.True(t, at.Equal(first.Delivery.ObservedAt), "delivery must retain the observation clock")
	assert.Equal(t, "safe-output-manifest", first.Delivery.Source, "delivery must retain its evidence source")

	for _, tt := range []struct {
		name   string
		at     time.Time
		source string
	}{
		{"repeated delivery", at, "safe-output-manifest"},
		{"later clocks", at.Add(time.Hour), "safe-output-manifest"},
		{"different evidence source", at, "replayed-manifest"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			replayed, err := lifecycle.Delivered(identity, tt.at, tt.at, tt.source)
			require.NoError(t, err, "valid delivery must succeed for %s", tt.name)
			assert.Equal(t, first.ID, replayed.ID, "record ID must be stable for the same identity")
		})
	}
}

func TestSpec_PublicAPI_DeliveredTargetURL(t *testing.T) {
	at := time.Date(2026, 11, 18, 10, 0, 0, 0, time.UTC)
	for _, tt := range []struct {
		name    string
		url     string
		wantErr bool
	}{
		{"optional target", "", false},
		{"HTTPS resource on execution host", "https://github.com/org/source/issues/1", false},
		{"HTTP resource", "http://github.com/org/source/issues/1", true},
		{"different host", "https://example.com/org/source/issues/1", true},
		{"user information", "https://someone@github.com/org/source/issues/1", true},
		{"query", "https://github.com/org/source/issues/1?view=all", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			identity := specIdentity()
			identity.TargetURL = tt.url
			_, err := lifecycle.Delivered(identity, at, at, "safe-output-manifest")
			if tt.wantErr {
				assert.Error(t, err, "Delivered must reject a target with %s", tt.name)
				return
			}
			require.NoError(t, err, "Delivered must accept %s", tt.name)
		})
	}
}

// TestSpec_DesignDecision_DeliveryIsSeparateFromHumanAcceptance derives from
// README.md's Design Decisions: delivery is not proof of human acceptance.
func TestSpec_DesignDecision_DeliveryIsSeparateFromHumanAcceptance(t *testing.T) {
	at := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
	record, err := lifecycle.Delivered(specIdentity(), at, at, "safe-output-manifest")
	require.NoError(t, err, "the documented usage example must produce a delivery")
	assert.False(t, record.Adoption.HumanVerified,
		"delivery evidence alone must not establish verified human acceptance")
}

func TestSpec_PublicAPI_IsHuman(t *testing.T) {
	for _, tt := range []struct {
		name       string
		actorType  string
		login      string
		publishers []string
		want       bool
	}{
		{"GitHub user", "User", "reviewer", nil, true},
		{"bot actor", "Bot", "automation", nil, false},
		{"publisher excluded", "User", "publisher", []string{"publisher"}, false},
		{"other publisher", "User", "reviewer", []string{"publisher"}, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, lifecycle.IsHuman(tt.actorType, tt.login, tt.publishers),
				"IsHuman must recognize non-bot users outside the publisher list")
		})
	}
}

func TestSpec_DesignDecision_RuleForClonesSignals(t *testing.T) {
	original, ok := lifecycle.RuleFor("create_issue")
	require.True(t, ok, "the output type in the usage example must have a rule")
	original.HumanDecision = slices.Clone(original.HumanDecision)
	original.Rejection = slices.Clone(original.Rejection)
	original.Reversal = slices.Clone(original.Reversal)
	copyRule, ok := lifecycle.RuleFor("create_issue")
	require.True(t, ok, "the documented output rule must remain registered")

	for _, signals := range [][]string{copyRule.HumanDecision, copyRule.Rejection, copyRule.Reversal} {
		for i := range signals {
			signals[i] = "caller-owned-change"
		}
	}
	copyRule.EffectClass = "caller-owned-change"

	fresh, ok := lifecycle.RuleFor("create_issue")
	require.True(t, ok, "changing a returned rule must not remove its registration")
	assert.Equal(t, original, fresh, "RuleFor must isolate registered rules and all signal slices from callers")

	_, ok = lifecycle.RuleFor("unregistered-spec-test-output")
	assert.False(t, ok, "RuleFor must identify an unregistered output type")
}

func TestSpec_PublicAPI_AdoptionEqual(t *testing.T) {
	utc := time.Date(2026, 11, 18, 10, 0, 0, 0, time.UTC)
	offset := time.FixedZone("spec-offset", 2*60*60)
	original := lifecycle.Adoption{
		State: "spec-state", Signal: "spec-signal", At: utc, ObservedAt: utc,
		Source: "spec-source", RuleVersion: lifecycle.RuleVersion,
		AcceptanceKind: "spec-kind", Actor: lifecycle.Actor{Login: "reviewer", Type: "User"},
		HumanVerified: true, Coverage: "spec-coverage",
	}
	for _, tt := range []struct {
		name   string
		change func(*lifecycle.Adoption)
		equal  bool
	}{
		{"same record", func(*lifecycle.Adoption) {}, true},
		{"same instants in another zone", func(a *lifecycle.Adoption) {
			a.At = utc.In(offset)
			a.ObservedAt = utc.In(offset)
		}, true},
		{"different decision clock", func(a *lifecycle.Adoption) { a.At = utc.Add(time.Second) }, false},
		{"different observation clock", func(a *lifecycle.Adoption) { a.ObservedAt = utc.Add(time.Second) }, false},
		{"different state", func(a *lifecycle.Adoption) { a.State = "other-state" }, false},
		{"different signal", func(a *lifecycle.Adoption) { a.Signal = "other-signal" }, false},
		{"different evidence", func(a *lifecycle.Adoption) { a.Source = "other-source" }, false},
		{"different rule version", func(a *lifecycle.Adoption) { a.RuleVersion = "other-version" }, false},
		{"different acceptance kind", func(a *lifecycle.Adoption) { a.AcceptanceKind = "other-kind" }, false},
		{"different deciding login", func(a *lifecycle.Adoption) { a.Actor.Login = "other-reviewer" }, false},
		{"different deciding actor type", func(a *lifecycle.Adoption) { a.Actor.Type = "Bot" }, false},
		{"different verification", func(a *lifecycle.Adoption) { a.HumanVerified = false }, false},
		{"different coverage", func(a *lifecycle.Adoption) { a.Coverage = "other-coverage" }, false},
		{"different error", func(a *lifecycle.Adoption) { a.Error = "observation failed" }, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			other := original
			tt.change(&other)
			assert.Equal(t, tt.equal, original.Equal(other), "Adoption.Equal must compare records after UTC normalization")
			assert.Equal(t, tt.equal, other.Equal(original), "Adoption.Equal must be symmetric")
		})
	}
}

func TestSpec_PublicAPI_SurvivalEqual(t *testing.T) {
	utc := time.Date(2026, 11, 18, 10, 0, 0, 0, time.UTC)
	original := lifecycle.Survival{State: "spec-state", Signal: "spec-signal", ObservedAt: utc, Coverage: "spec-coverage"}
	for _, tt := range []struct {
		name   string
		change func(*lifecycle.Survival)
		equal  bool
	}{
		{"same record", func(*lifecycle.Survival) {}, true},
		{"same instant in another zone", func(s *lifecycle.Survival) {
			s.ObservedAt = utc.In(time.FixedZone("spec-offset", -5*60*60))
		}, true},
		{"different observation clock", func(s *lifecycle.Survival) { s.ObservedAt = utc.Add(time.Second) }, false},
		{"different state", func(s *lifecycle.Survival) { s.State = "other-state" }, false},
		{"different signal", func(s *lifecycle.Survival) { s.Signal = "other-signal" }, false},
		{"different coverage", func(s *lifecycle.Survival) { s.Coverage = "other-coverage" }, false},
		{"different error", func(s *lifecycle.Survival) { s.Error = "observation failed" }, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			other := original
			tt.change(&other)
			assert.Equal(t, tt.equal, original.Equal(other), "Survival.Equal must compare records after UTC normalization")
			assert.Equal(t, tt.equal, other.Equal(original), "Survival.Equal must be symmetric")
		})
	}
}

func TestSpec_PublicAPI_Merge(t *testing.T) {
	at := time.Date(2026, 11, 18, 10, 0, 0, 0, time.UTC)
	original, err := lifecycle.Delivered(specIdentity(), at, at, "safe-output-manifest")
	require.NoError(t, err, "the documented delivery must be valid")
	previous := original
	previous.Execution = &lifecycle.Execution{
		Number: 1, LabelsBefore: []string{"triage"}, LabelsAdded: []string{"reviewed"},
		Metadata:    map[string]any{"context": "spec-example"},
		BeforeState: map[string]any{"state": "open"}, AfterState: map[string]any{"state": "closed"},
	}
	merged, err := lifecycle.Merge(previous, original)
	require.NoError(t, err, "replaying a delivery for the same effect must merge")
	assert.Equal(t, previous.ID, merged.ID, "merging must retain the effect ID")
	assert.Equal(t, previous.Execution, merged.Execution, "replayed delivery must preserve retained execution evidence")

	otherIdentity := specIdentity()
	otherIdentity.EvidenceID = "result:2"
	other, err := lifecycle.Delivered(otherIdentity, at, at, "safe-output-manifest")
	require.NoError(t, err, "a distinct evidence item must form a valid delivery")
	_, err = lifecycle.Merge(previous, other)
	assert.Error(t, err, "Merge must reject records identifying different effects")
}

// SPEC_AMBIGUITY: README.md does not enumerate valid observation states and
// signals or their authority ordering, so Observe transitions and observation
// precedence in Merge cannot be specified here without adding assumptions.
