package lifecycle

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLifecycleRetainsExecutionSelectorsAndRejectsConflictingReplay(t *testing.T) {
	record := testDelivery(t, "add_labels")
	record.Execution = &Execution{Number: 7, LabelsBefore: []string{}, LabelsAdded: []string{"bug"}}
	content, err := json.Marshal(record)
	require.NoError(t, err)
	var restored Record
	require.NoError(t, json.Unmarshal(content, &restored))
	require.NotNil(t, restored.Execution.LabelsBefore)
	merged, err := Merge(record, restored)
	require.NoError(t, err)
	assert.Equal(t, record.Execution, merged.Execution)
	replay := testDelivery(t, "add_labels")
	merged, err = Merge(record, replay)
	require.NoError(t, err)
	assert.Equal(t, record.Execution, merged.Execution)
	replay.Execution = &Execution{Number: 8, LabelsBefore: []string{}, LabelsAdded: []string{"bug"}}
	_, err = Merge(record, replay)
	assert.ErrorContains(t, err, "conflicting retained execution")
}

func testDelivery(t *testing.T, outputType string) Record {
	t.Helper()
	at := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	record, err := Delivered(Identity{
		Host: "github.com", Repository: "org/source", Workflow: "worker",
		RunID: 42, RunAttempt: 1, InvocationID: "worker:1", EvidenceID: "result:1",
		Type: outputType, TargetURL: "https://github.com/org/target/issues/7", TargetRepository: "org/target",
	}, at, at.Add(time.Minute), "safe-output-manifest")
	require.NoError(t, err)
	return record
}

func TestDeliveryAdoptionRules(t *testing.T) {
	for _, test := range []struct{ outputType, state, kind string }{
		{"create_issue", "pending", ""},
		{"create_pull_request", "pending", ""},
		{"create_discussion", "pending", ""},
		{"add_labels", "accepted", "fallback_delivery"},
		{"add_comment", "accepted", "fallback_delivery"},
		{"dispatch_workflow", "accepted", "fallback_delivery"},
		{"noop", "not_applicable", ""},
		{"connector_custom", "unknown", ""},
	} {
		t.Run(test.outputType, func(t *testing.T) {
			record := testDelivery(t, test.outputType)
			assert.Equal(t, "delivered", record.Delivery.State)
			assert.Equal(t, test.state, record.Adoption.State)
			assert.Equal(t, test.kind, record.Adoption.AcceptanceKind)
			assert.Equal(t, "unknown", record.Survival.State)
		})
	}
}

func TestIsHuman(t *testing.T) {
	publishers := []string{"gh-aw-bot"}
	tests := []struct {
		name, actorType, login string
		want                   bool
	}{
		{"user", "User", "alice", true},
		{"dependabot bot", "User", "dependabot[bot]", false},
		{"renovate bot upper case", "User", "Renovate[BOT]", false},
		{"bot actor type", "Bot", "alice", false},
		{"empty login", "User", "", false},
		{"publisher case-insensitive", "User", "GH-AW-BOT", false},
		{"organization", "Organization", "acme", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, IsHuman(tt.actorType, tt.login, publishers))
		})
	}
}

func TestDeliveredRejectsInvalidIdentityAndTargetURL(t *testing.T) {
	record := testDelivery(t, "create_issue")
	tests := []struct {
		name   string
		change func(*Identity)
	}{
		{"non-HTTPS scheme", func(i *Identity) { i.TargetURL = "http://github.com/org/target/issues/7" }},
		{"foreign host", func(i *Identity) { i.TargetURL = "https://example.com/org/target/issues/7" }},
		{"userinfo", func(i *Identity) { i.TargetURL = "https://user@github.com/org/target/issues/7" }},
		{"query string", func(i *Identity) { i.TargetURL = "https://github.com/org/target/issues/7?label=bug" }},
		{"zero run ID", func(i *Identity) { i.RunID = 0 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			identity := record.Identity
			tt.change(&identity)
			_, err := Delivered(identity, record.Delivery.At, record.Delivery.ObservedAt, record.Delivery.Source)
			assert.Error(t, err)
		})
	}
}

func TestLifecycleIdentitySeparatesEffectsAndAttempts(t *testing.T) {
	first := testDelivery(t, "add_labels")
	identity := first.Identity
	identity.TargetURL = ""
	first, err := Delivered(identity, first.Delivery.At, first.Delivery.ObservedAt, first.Delivery.Source)
	require.NoError(t, err)
	tests := []struct {
		name   string
		change func(*Identity)
	}{
		{"evidence ID", func(i *Identity) { i.EvidenceID = "result:2" }},
		{"invocation ID", func(i *Identity) { i.InvocationID = "worker:2" }},
		{"run attempt", func(i *Identity) { i.RunAttempt = 2 }},
		{"repository", func(i *Identity) { i.Repository = "org/other" }},
		{"host", func(i *Identity) { i.Host = "github.example" }},
		{"workflow", func(i *Identity) { i.Workflow = "other-worker" }},
		{"type", func(i *Identity) { i.Type = "add_comment" }},
		{"target URL", func(i *Identity) { i.TargetURL = "https://github.com/org/target/issues/8" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			identity := first.Identity
			tt.change(&identity)
			next, err := Delivered(identity, first.Delivery.At, first.Delivery.ObservedAt, first.Delivery.Source)
			require.NoError(t, err)
			assert.NotEqual(t, first.ID, next.ID)
		})
	}
}

func TestLifecycleDecisionAndReversal(t *testing.T) {
	delivered := testDelivery(t, "create_issue")
	at := delivered.Delivery.At.Add(time.Hour)
	accepted, err := Observe(delivered, Observation{
		State: "accepted", Signal: "completed", At: at, ObservedAt: at.Add(time.Minute),
		HumanVerified: true, AcceptanceKind: "explicit_human",
	})
	require.NoError(t, err)
	assert.Equal(t, "observed", accepted.Survival.State)
	assert.Equal(t, at, accepted.Adoption.At)
	reversed, err := Observe(accepted, Observation{
		State: "pending", Signal: "reopened", At: at.Add(time.Hour), ObservedAt: at.Add(2 * time.Hour),
	})
	require.NoError(t, err)
	assert.Equal(t, "reversed", reversed.Survival.State)
	assert.Equal(t, at.Add(time.Hour), reversed.ReversedAt)
	merged, err := Merge(reversed, delivered)
	require.NoError(t, err)
	assert.Equal(t, reversed.Adoption, merged.Adoption)
	assert.Equal(t, reversed.Survival, merged.Survival)
}

func TestLifecycleRejectsConflictingAndUnverifiedEvidence(t *testing.T) {
	record := testDelivery(t, "create_issue")
	at := record.Delivery.At.Add(time.Hour)
	_, err := Observe(record, Observation{State: "accepted", Signal: "completed", At: at, ObservedAt: at})
	require.ErrorContains(t, err, "verified decision")
	observation := Observation{State: "pending", Signal: "open", ObservedAt: at}
	first, err := Observe(record, observation)
	require.NoError(t, err)
	observation.State = "unknown"
	second, err := Observe(record, observation)
	require.NoError(t, err)
	_, err = Merge(first, second)
	require.ErrorContains(t, err, "equal-clock")
	failed, err := Observe(first, Observation{ObservedAt: at.Add(time.Minute), Error: "not found or inaccessible"})
	require.NoError(t, err)
	assert.Equal(t, first.Adoption.State, failed.Adoption.State)
	assert.Equal(t, "unavailable", failed.Adoption.Coverage)
	assert.Empty(t, failed.ReversalSignal)
}

func TestLifecycleValidatesRetainedDecisionsAndAcceptanceKinds(t *testing.T) {
	record := testDelivery(t, "create_issue")
	at := record.Delivery.At.Add(time.Hour)
	accepted, err := Observe(record, Observation{
		State: "accepted", Signal: "completed", At: at, ObservedAt: at,
		HumanVerified: true, AcceptanceKind: "explicit_human",
	})
	require.NoError(t, err)
	conflict := accepted
	conflict.Adoption.Coverage = "unavailable"
	_, err = Merge(accepted, conflict)
	require.ErrorContains(t, err, "equal-clock")
	conflict = accepted
	conflict.Adoption.HumanVerified = false
	t.Run("rejects unverified retained decision", func(t *testing.T) {
		_, err := Merge(Record{}, conflict)
		require.ErrorContains(t, err, "verified decision")
	})

	fallback := testDelivery(t, "add_comment")
	_, err = Observe(fallback, Observation{
		State: "accepted", Signal: "delivered", At: fallback.Delivery.At,
		ObservedAt: at, AcceptanceKind: "fallback_delivery",
	})
	require.NoError(t, err)
	t.Run("rejects human-verified fallback", func(t *testing.T) {
		_, err := Observe(fallback, Observation{
			State: "accepted", Signal: "delivered", At: fallback.Delivery.At,
			ObservedAt: at, HumanVerified: true, AcceptanceKind: "fallback_delivery",
		})
		require.ErrorContains(t, err, "fallback is not permitted")
	})
	t.Run("rejects fallback after delivery time", func(t *testing.T) {
		_, err := Observe(fallback, Observation{
			State: "accepted", Signal: "delivered", At: fallback.Delivery.At.Add(time.Second),
			ObservedAt: at, AcceptanceKind: "fallback_delivery",
		})
		require.ErrorContains(t, err, "fallback is not permitted")
	})
	for _, outputType := range []string{"connector_custom", "noop"} {
		t.Run(outputType, func(t *testing.T) {
			unsupported := testDelivery(t, outputType)
			unsupported.Adoption = accepted.Adoption
			_, err = Merge(Record{}, unsupported)
			assert.ErrorContains(t, err, "registered value-eligible output type")
		})
	}
}

func TestLifecycleRejectsInvalidObservations(t *testing.T) {
	record := testDelivery(t, "create_issue")
	tests := []struct {
		name        string
		observation Observation
		wantError   string
	}{
		{"invalid state", Observation{State: "invalid", ObservedAt: record.Delivery.At}, "invalid adoption state"},
		{"zero observation time", Observation{State: "pending"}, "adoption observation requires an observation clock"},
		{"decision before delivery", Observation{
			State: "pending", At: record.Delivery.At.Add(-time.Second), ObservedAt: record.Delivery.At,
		}, "adoption decision precedes delivery"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Observe(record, tt.observation)
			assert.ErrorContains(t, err, tt.wantError)
		})
	}
}

func TestLifecycleMergeRejectsConflictingDeliveryEvidence(t *testing.T) {
	record := testDelivery(t, "create_issue")
	incoming := record
	incoming.Delivery.At = incoming.Delivery.At.Add(time.Second)
	_, err := Merge(record, incoming)
	assert.ErrorContains(t, err, "conflicting lifecycle identity or delivery evidence")
}

func TestLifecycleMergeRejectsInvalidIncomingRecordWithoutPrevious(t *testing.T) {
	incoming := testDelivery(t, "create_issue")
	incoming.ID = "invalid"
	_, err := Merge(Record{}, incoming)
	assert.ErrorContains(t, err, "invalid lifecycle identity")
}

func TestAdoptionEqualNormalizesTimeZones(t *testing.T) {
	utc := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	local := utc.In(time.FixedZone("UTC-7", -7*60*60))
	adoption := Adoption{At: utc, ObservedAt: utc}
	sameInstant := Adoption{At: local, ObservedAt: local}
	assert.True(t, adoption.At.Equal(sameInstant.At))
	assert.NotEqual(t, adoption, sameInstant)
	assert.True(t, adoption.Equal(sameInstant))

	record := testDelivery(t, "create_issue")
	accepted, err := Observe(record, Observation{
		State: "accepted", Signal: "completed", At: utc, ObservedAt: utc,
		HumanVerified: true, AcceptanceKind: "explicit_human",
	})
	require.NoError(t, err)
	incoming := accepted
	incoming.Adoption.At = local
	incoming.Adoption.ObservedAt = local
	_, err = Merge(accepted, incoming)
	assert.NoError(t, err)
}

func TestLifecycleMergeAcceptsUTCEquivalentDeliveryTime(t *testing.T) {
	record := testDelivery(t, "create_issue")
	incoming := record
	incoming.Delivery.At = record.Delivery.At.In(time.FixedZone("UTC-7", -7*60*60))
	_, err := Merge(record, incoming)
	assert.NoError(t, err)
}
