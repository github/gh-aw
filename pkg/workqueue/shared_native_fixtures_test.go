package workqueue

import (
	"encoding/json"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Shared Go/JS conformance fixtures; work_queue_conformance_checks.cjs asserts
// the same fields against the JS engine.

// ledgerFixture is one generated ledger with its expectations.
type ledgerFixture struct {
	Commits   []json.RawMessage `json:"commits"`
	Canonical string            `json:"canonical"`
	Prefixes  []struct {
		Tip        string                    `json:"tip"`
		Commits    int                       `json:"commits"`
		Stats      Stats                     `json:"stats"`
		Works      map[string]map[string]any `json:"works"`
		Dispatches map[string]string         `json:"dispatches"`
		Next       struct {
			Keys   []string `json:"keys"`
			Reason string   `json:"reason"`
		} `json:"next"`
	} `json:"prefixes"`
	Projection json.RawMessage `json:"projection"`
	Assignment json.RawMessage `json:"assignment"`
	Checkpoint struct {
		Commit     json.RawMessage `json:"commit"`
		Projection json.RawMessage `json:"projection"`
	} `json:"checkpoint"`
	Rejections []struct {
		Name string `json:"name"`
		// Base truncates Commits before Extra; absent means the whole ledger.
		Base   *int              `json:"base"`
		Extra  []json.RawMessage `json:"extra"`
		Commit json.RawMessage   `json:"commit"`
		Code   string            `json:"code"`
	} `json:"rejections"`
}

type prefixFixture struct {
	Version        int                `json:"version"`
	NextParameters DispatchParameters `json:"next_parameters"`
	NextRequestID  string             `json:"next_request_id"`
	ledgerFixture
	Migration struct {
		Commits     []json.RawMessage `json:"commits"`
		Canonical   string            `json:"canonical"`
		Projection  json.RawMessage   `json:"projection"`
		LateMigrate struct {
			Commit json.RawMessage `json:"commit"`
			Code   string          `json:"code"`
		} `json:"late_migrate"`
	} `json:"migration"`
	Continuation ledgerFixture `json:"continuation"`
}

func loadFixture(t *testing.T, name string, target any) {
	t.Helper()
	data, err := os.ReadFile("../../specs/work-queue/fixtures/" + name)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(data, target))
}

func canonicalJSON(t *testing.T, data []byte) string {
	t.Helper()
	canonical, err := Canonical(data)
	require.NoError(t, err)
	return string(canonical)
}

func serializedProjection(t *testing.T, state Projection) string {
	t.Helper()
	data, err := SerializeProjection(state)
	require.NoError(t, err)
	return string(data)
}

func serializeRaw(t *testing.T, records []json.RawMessage) string {
	t.Helper()
	state, err := ReplayRaw(records)
	require.NoError(t, err)
	data, err := serializeTransactions(state.Transactions)
	require.NoError(t, err)
	return string(data)
}

func nodeKeys(state Projection, decision Decision) []string {
	keys := []string{}
	for _, operation := range decision.Operations {
		keys = append(keys, state.Works[operation.WorkID].NodeKey)
	}
	return keys
}

func TestSharedNativeConformanceFixtures(t *testing.T) {
	var fixture prefixFixture
	loadFixture(t, "canonical-prefix.json", &fixture)
	require.Equal(t, Version, fixture.Version)
	require.NotEmpty(t, fixture.Continuation.Commits)

	// Each shared checkpoint folds exactly one Work no later commit reads.
	dropped := map[string]string{"lifecycle": "c", "continuation": "p"}
	for name, ledger := range map[string]ledgerFixture{"lifecycle": fixture.ledgerFixture, "continuation": fixture.Continuation} {
		t.Run(name+" canonical wire", func(t *testing.T) { checkCanonicalWire(t, ledger) })
		t.Run(name+" every prefix", func(t *testing.T) { checkLedgerPrefixes(t, ledger, fixture.NextParameters, fixture.NextRequestID) })
		t.Run(name+" checkpoint", func(t *testing.T) { checkLedgerCheckpoint(t, ledger, dropped[name]) })
		for _, rejection := range ledger.Rejections {
			t.Run(name+" rejection "+rejection.Name, func(t *testing.T) {
				base := len(ledger.Commits)
				if rejection.Base != nil {
					base = *rejection.Base
				}
				records := append(append(append([]json.RawMessage{}, ledger.Commits[:base]...), rejection.Extra...), rejection.Commit)
				_, err := ReplayRaw(records)
				requireCode(t, err, rejection.Code)
			})
		}
	}

	t.Run("migration", func(t *testing.T) {
		require.Equal(t, fixture.Migration.Canonical, serializeRaw(t, fixture.Migration.Commits))
		state, err := ReplayRaw(fixture.Migration.Commits)
		require.NoError(t, err)
		require.Equal(t, canonicalJSON(t, fixture.Migration.Projection), serializedProjection(t, state))
		_, err = ReplayRaw(append(append([]json.RawMessage{}, fixture.Migration.Commits...), fixture.Migration.LateMigrate.Commit))
		requireCode(t, err, fixture.Migration.LateMigrate.Code)
	})
}

func checkCanonicalWire(t *testing.T, ledger ledgerFixture) {
	t.Helper()
	require.Equal(t, ledger.Canonical, serializeRaw(t, ledger.Commits))
	commits, err := Parse([]byte(ledger.Canonical))
	require.NoError(t, err)
	require.Len(t, commits, len(ledger.Commits))
	for index, commit := range commits {
		data, err := CanonicalValue(commit)
		require.NoError(t, err)
		require.Equal(t, canonicalJSON(t, ledger.Commits[index]), string(data))
	}
}

func checkLedgerPrefixes(t *testing.T, ledger ledgerFixture, next DispatchParameters, nextRequestID string) {
	t.Helper()
	require.Len(t, ledger.Prefixes, len(ledger.Commits))
	for index, expected := range ledger.Prefixes {
		state, err := ReplayRaw(ledger.Commits[:index+1])
		require.NoError(t, err, "prefix %d", index)
		require.Equal(t, expected.Tip, state.Tip)
		require.Equal(t, expected.Commits, state.Commits)
		require.Equal(t, expected.Stats, state.Stats, "prefix %d", index)
		for _, work := range state.Works {
			require.Equal(t, map[string]any{"state": work.State, "attempts": float64(work.Attempts)}, expected.Works[work.NodeKey], "prefix %d work %s", index, work.NodeKey)
		}
		for id, dispatch := range state.Dispatches {
			require.Equal(t, expected.Dispatches[id], dispatch.State)
		}
		decision, err := PlanDispatch(state, next, nextRequestID)
		require.NoError(t, err)
		require.Equal(t, expected.Next.Keys, nodeKeys(state, decision), "prefix %d", index)
		require.Equal(t, expected.Next.Reason, decision.Reason, "prefix %d", index)
	}
	state, err := ReplayRaw(ledger.Commits)
	require.NoError(t, err)
	require.Equal(t, canonicalJSON(t, ledger.Projection), serializedProjection(t, state))
	var assignment Assignment
	require.NoError(t, json.Unmarshal(ledger.Assignment, &assignment))
	actual, err := AssignmentForDispatch(state, assignment.DispatchID)
	require.NoError(t, err)
	data, err := CanonicalValue(actual)
	require.NoError(t, err)
	require.Equal(t, canonicalJSON(t, ledger.Assignment), string(data))
}

func checkLedgerCheckpoint(t *testing.T, ledger ledgerFixture, dropped string) {
	t.Helper()
	restored, err := ReplayRaw([]json.RawMessage{ledger.Checkpoint.Commit})
	require.NoError(t, err)
	require.Equal(t, canonicalJSON(t, ledger.Checkpoint.Projection), serializedProjection(t, restored))
	folded, err := ReplayRaw(ledger.Commits)
	require.NoError(t, err)
	// Every folded Work but the dropped one is restored unchanged, with its Claims and Dispatches.
	for id, work := range folded.Works {
		kept, retained := restored.Works[id]
		require.Equal(t, work.NodeKey != dropped, retained, work.NodeKey)
		if retained {
			require.Equal(t, *work, *kept, work.NodeKey)
		}
	}
	require.Len(t, restored.Works, len(folded.Works)-1)
	for id, claim := range folded.Claims {
		_, retained := restored.Claims[id]
		_, dispatched := restored.Dispatches[claim.DispatchID]
		require.Equal(t, folded.Works[claim.WorkID].NodeKey != dropped, retained && dispatched, id)
	}
	require.Len(t, restored.WorkCreators, len(restored.Works))
	// The Go checkpoint builder reproduces the shared checkpoint projection.
	var commit QueueCommit
	require.NoError(t, json.Unmarshal(ledger.Checkpoint.Commit, &commit))
	built, err := CheckpointCommit(folded, commit.Actor, commit.At)
	require.NoError(t, err)
	data, err := CanonicalValue(built)
	require.NoError(t, err)
	require.Equal(t, canonicalJSON(t, ledger.Checkpoint.Commit), string(data))
}

type selectionFixture struct {
	Version int `json:"version"`
	Cases   []struct {
		Name  string `json:"name"`
		Nodes []struct {
			Key      string   `json:"key"`
			Priority int      `json:"priority"`
			Account  string   `json:"account"`
			Profile  string   `json:"profile"`
			After    []string `json:"after"`
		} `json:"nodes"`
		Open       []string `json:"open"`
		Parameters struct {
			MaxClaims       int      `json:"max_claims"`
			MaxDispatches   int      `json:"max_dispatches"`
			Concurrency     int      `json:"concurrency"`
			PerAccountLimit int      `json:"per_account_limit"`
			Profiles        []string `json:"profiles"`
		} `json:"parameters"`
		Expected []string `json:"expected"`
		Reason   string   `json:"reason"`
	} `json:"cases"`
}

func profilesFor(names []string) map[string]DispatchProfile {
	profiles := map[string]DispatchProfile{}
	for _, name := range names {
		profiles[name] = DispatchProfile{Workflow: ".github/workflows/" + name + ".lock.yml"}
	}
	return profiles
}

func TestSharedSelectionFixtures(t *testing.T) {
	var fixture selectionFixture
	loadFixture(t, "selection.json", &fixture)
	require.Equal(t, Version, fixture.Version)
	require.NotEmpty(t, fixture.Cases)
	for _, selection := range fixture.Cases {
		t.Run(selection.Name, func(t *testing.T) {
			byKey := map[string]WorkDefinition{}
			works := []WorkDefinition{}
			profileSet := map[string]struct{}{}
			for _, node := range selection.Nodes {
				work := testWork(t, node.Key, func(work *WorkDefinition) {
					work.Priority = node.Priority
					work.FairnessKey = node.Account
					work.WorkerProfile = node.Profile
					for _, key := range node.After {
						work.DependsOn = append(work.DependsOn, Dependency{Kind: "work", WorkID: byKey[key].WorkID})
					}
				})
				byKey[node.Key] = work
				works = append(works, work)
				profileSet[node.Profile] = struct{}{}
			}
			log := testSubmit(t, nil, works...)
			if len(selection.Open) > 0 {
				names := []string{}
				for name := range profileSet {
					names = append(names, name)
				}
				sort.Strings(names)
				parameters := DispatchParameters{Pool: "default", MaxClaims: 256, MaxDispatches: 256, Concurrency: 4096, Profiles: profilesFor(names)}
				request, err := NewRequest("dispatch_next", testDispatcher, parameters)
				require.NoError(t, err)
				claims := []any{}
				for _, key := range selection.Open {
					claimID, dispatchID, handle := ClaimIdentity(request.ID, byKey[key].WorkID)
					claims = append(claims, ClaimOperation{Kind: "Claim", WorkID: byKey[key].WorkID, ClaimID: claimID, DispatchID: dispatchID, Handle: handle})
				}
				state, err := Replay(log)
				require.NoError(t, err)
				commit := BuildCommit(state, testDispatcher, request, mustOps(t, claims...), 1)
				_, err = AppendCommit(state, commit)
				require.NoError(t, err)
				log = append(log, commit)
			}
			state, err := Replay(log)
			require.NoError(t, err)
			before := serializedProjection(t, state)
			decision, err := PlanDispatch(state, DispatchParameters{Pool: "default", MaxClaims: selection.Parameters.MaxClaims, MaxDispatches: selection.Parameters.MaxDispatches, Concurrency: selection.Parameters.Concurrency, PerAccountLimit: selection.Parameters.PerAccountLimit, Profiles: profilesFor(selection.Parameters.Profiles)}, "request:selection-fixture")
			require.NoError(t, err)
			require.Equal(t, before, serializedProjection(t, state), "planning never mutates the projection")
			require.Equal(t, selection.Expected, nodeKeys(state, decision))
			require.Equal(t, selection.Reason, decision.Reason)
		})
	}
}

func TestSharedReasonFixtures(t *testing.T) {
	var fixture struct {
		Version int                 `json:"version"`
		Enums   map[string][]string `json:"enums"`
		Cases   []struct {
			Name   string `json:"name"`
			Kind   string `json:"kind"`
			Reason any    `json:"reason"`
			Valid  bool   `json:"valid"`
		} `json:"cases"`
	}
	loadFixture(t, "reason-validation.json", &fixture)
	require.Equal(t, Version, fixture.Version)
	require.Equal(t, ClaimCancellationReasons, fixture.Enums["ClaimCancellation"])
	require.Equal(t, WorkCancellationReasons, fixture.Enums["WorkCancellation"])
	require.Equal(t, ReleaseReasons, fixture.Enums["Release"])
	for _, reason := range fixture.Cases {
		t.Run(reason.Name, func(t *testing.T) {
			operation := map[string]any{"kind": reason.Kind, "reason": reason.Reason}
			switch reason.Kind {
			case "ClaimCancellation":
				operation["work_id"], operation["claim_id"] = "w", "c"
			case "WorkCancellation":
				operation["work_id"] = "w"
			default:
				operation["dispatch_id"] = "d"
			}
			data, err := json.Marshal(operation)
			require.NoError(t, err)
			value, err := decodeStrict(data)
			require.NoError(t, err)
			_, err = validateOperationValue(value)
			if reason.Valid {
				require.NoError(t, err)
			} else {
				requireCode(t, err, "reason_invalid")
			}
		})
	}
}

func TestSharedCanonicalAndIdentityFixtures(t *testing.T) {
	var canonical struct {
		Valid []struct {
			Name     string `json:"name"`
			Input    string `json:"input"`
			Expected string `json:"expected"`
		} `json:"valid"`
		Invalid []string `json:"invalid"`
	}
	loadFixture(t, "canonical.json", &canonical)
	for _, fixture := range canonical.Valid {
		actual, err := Canonical([]byte(fixture.Input))
		require.NoError(t, err, fixture.Name)
		require.Equal(t, fixture.Expected, string(actual), fixture.Name)
	}
	for _, input := range canonical.Invalid {
		_, err := Canonical([]byte(input))
		require.Error(t, err, input)
	}
	var identities struct {
		Identity []struct {
			Name     string `json:"name"`
			Text     string `json:"text"`
			Repeat   int    `json:"repeat"`
			Expected int    `json:"expected_utf8_bytes"`
			Valid    bool   `json:"valid"`
		} `json:"identity"`
		Decimal []struct {
			Name   string `json:"name"`
			Digits int    `json:"digits"`
			Valid  bool   `json:"valid"`
		} `json:"decimal"`
	}
	loadFixture(t, "identity-validation.json", &identities)
	for _, fixture := range identities.Identity {
		value := strings.Repeat(fixture.Text, fixture.Repeat)
		require.Len(t, value, fixture.Expected, fixture.Name)
		_, err := identity(value, "identity")
		if fixture.Valid {
			require.NoError(t, err, fixture.Name)
		} else {
			requireCode(t, err, "ledger_invalid")
		}
	}
	for _, fixture := range identities.Decimal {
		_, err := decimal(strings.Repeat("9", fixture.Digits), "native ID", "ledger_invalid")
		if fixture.Valid {
			require.NoError(t, err, fixture.Name)
		} else {
			requireCode(t, err, "ledger_invalid")
		}
	}
}

func TestJSCodecFixtures(t *testing.T) {
	data, err := os.ReadFile("../../actions/setup/js/work_queue_conformance_fixtures.json")
	require.NoError(t, err)
	var fixtures struct {
		Version int `json:"version"`
		Codec   []struct {
			Name      string `json:"name"`
			Input     string `json:"input"`
			Canonical string `json:"canonical"`
			Invalid   bool   `json:"invalid"`
		} `json:"codec"`
	}
	require.NoError(t, json.Unmarshal(data, &fixtures))
	require.Equal(t, Version, fixtures.Version)
	for _, fixture := range fixtures.Codec {
		actual, err := Canonical([]byte(fixture.Input))
		if fixture.Invalid {
			require.Error(t, err, fixture.Name)
			continue
		}
		require.NoError(t, err, fixture.Name)
		require.Equal(t, fixture.Canonical, string(actual), fixture.Name)
	}
}
