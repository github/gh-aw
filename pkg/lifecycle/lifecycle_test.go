package lifecycle

import (
	"encoding/json"
	"testing"
	"time"

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
	require.Equal(t, record.Execution, merged.Execution)
	replay := testDelivery(t, "add_labels")
	merged, err = Merge(record, replay)
	require.NoError(t, err)
	require.Equal(t, record.Execution, merged.Execution)
	replay.Execution = &Execution{Number: 8, LabelsBefore: []string{}, LabelsAdded: []string{"bug"}}
	_, err = Merge(record, replay)
	require.ErrorContains(t, err, "conflicting retained execution")
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
			require.Equal(t, "delivered", record.Delivery.State)
			require.Equal(t, test.state, record.Adoption.State)
			require.Equal(t, test.kind, record.Adoption.AcceptanceKind)
			require.Equal(t, "unknown", record.Survival.State)
		})
	}
}

func TestLifecycleIdentitySeparatesEffectsAndAttempts(t *testing.T) {
	first := testDelivery(t, "add_labels")
	for _, change := range []func(*Identity){
		func(i *Identity) { i.EvidenceID = "result:2" },
		func(i *Identity) { i.InvocationID = "worker:2" },
		func(i *Identity) { i.RunAttempt = 2 },
		func(i *Identity) { i.Repository = "org/other" },
	} {
		identity := first.Identity
		change(&identity)
		next, err := Delivered(identity, first.Delivery.At, first.Delivery.ObservedAt, first.Delivery.Source)
		require.NoError(t, err)
		require.NotEqual(t, first.ID, next.ID)
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
	require.Equal(t, "observed", accepted.Survival.State)
	require.Equal(t, at, accepted.Adoption.At)
	reversed, err := Observe(accepted, Observation{
		State: "pending", Signal: "reopened", At: at.Add(time.Hour), ObservedAt: at.Add(2 * time.Hour),
	})
	require.NoError(t, err)
	require.Equal(t, "reversed", reversed.Survival.State)
	require.Equal(t, at.Add(time.Hour), reversed.ReversedAt)
	merged, err := Merge(reversed, delivered)
	require.NoError(t, err)
	require.Equal(t, reversed.Adoption, merged.Adoption)
	require.Equal(t, reversed.Survival, merged.Survival)
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
	require.Equal(t, first.Adoption.State, failed.Adoption.State)
	require.Equal(t, "unavailable", failed.Adoption.Coverage)
	require.Empty(t, failed.ReversalSignal)
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
	_, err = Merge(Record{}, conflict)
	require.ErrorContains(t, err, "verified decision")

	fallback := testDelivery(t, "add_comment")
	for _, human := range []bool{false, true} {
		_, err = Observe(fallback, Observation{
			State: "accepted", Signal: "delivered", At: fallback.Delivery.At.Add(time.Second),
			ObservedAt: at, HumanVerified: human, AcceptanceKind: "fallback_delivery",
		})
		require.ErrorContains(t, err, "fallback is not permitted")
	}
	for _, outputType := range []string{"connector_custom", "noop"} {
		record := testDelivery(t, outputType)
		record.Adoption = accepted.Adoption
		_, err = Merge(Record{}, record)
		require.ErrorContains(t, err, "registered value-eligible output type")
	}
}
