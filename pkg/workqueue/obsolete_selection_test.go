package workqueue

import (
	"bytes"
	"fmt"
	"slices"
	"strings"
	"testing"
)

func TestObsoleteWorkSelectionSpecification(t *testing.T) {
	base, nodes := cancellationVersionFixture(t)
	base = deployTestWorker(t, base, "incompatible", "default", strings.Repeat("b", 40), strings.Repeat("b", 64), true)
	for _, name := range []string{
		"eligible", "claimed", "owner", "pin", "pending", "failed", "verified", "completed", "cancelled",
		"missing", "missing-revision", "unavailable", "compatible", "legacy", "expired", "before-deadline",
		"byte-order", "policy-limit", "caller-limit",
	} {
		state, err := Replay(base)
		if err != nil {
			t.Fatal(err)
		}
		work := state.Works[nodes[0].WorkID]
		deployment := state.Deployments["default"]["default"]
		revision := deployment.Revisions[deployment.CurrentRef]
		limit := 256
		switch name {
		case "claimed", "completed", "cancelled":
			work.State = name
		case "owner":
			work.ClaimID = "owned"
		case "pin":
			work.ExecutionRef = nodes[1].ExecutionRef
		case "pending", "failed", "verified":
			work.Barrier = name
		case "missing":
			delete(state.Deployments["default"], "default")
		case "missing-revision":
			delete(deployment.Revisions, deployment.CurrentRef)
		case "unavailable":
			revision.Available = false
			deployment.Revisions[deployment.CurrentRef] = revision
		case "compatible":
			revision.Profile.LogicalContract = work.AdmissionContract
			deployment.Revisions[deployment.CurrentRef] = revision
		case "legacy":
			state.Policy.Authorization = "legacy"
		case "expired", "before-deadline":
			deadline := int64(9000)
			if name == "before-deadline" {
				deadline++
			}
			work.ExpiresAt = &deadline
		case "byte-order", "policy-limit", "caller-limit":
			delete(state.Works, work.WorkID)
			for i := range 260 {
				copy := *work
				copy.WorkID = fmt.Sprintf("work-%03d", 259-i)
				state.Works[copy.WorkID] = &copy
			}
			switch name {
			case "policy-limit":
				state.Policy.Limits.Operations = 2
			case "caller-limit":
				limit = 1
			}
		}
		before, _ := canonicalValue(state)
		ids, err := ObsoleteWorkIDs(state, 9000, limit)
		if err != nil {
			t.Fatal(err)
		}
		after, _ := canonicalValue(state)
		if !bytes.Equal(before, after) {
			t.Fatal("selection mutated the authoritative projection")
		}
		expectedCount := 0
		switch name {
		case "eligible", "before-deadline", "caller-limit":
			expectedCount = 1
		case "policy-limit":
			expectedCount = 2
		case "byte-order":
			expectedCount = 256
		}
		if len(ids) != expectedCount || !slices.IsSorted(ids) {
			t.Fatalf("%s selection violates eligibility, ordering or limits: %v", name, ids)
		}
		if name == "eligible" || name == "before-deadline" {
			if !slices.Equal(ids, []string{nodes[0].WorkID}) {
				t.Fatalf("%s selected a compatible or pinned Work: %v", name, ids)
			}
		}
		if name == "byte-order" && (ids[0] != "work-000" || ids[255] != "work-255") {
			t.Fatalf("batch did not take the exact byte-ordered prefix: %v", ids)
		}
		if name == "policy-limit" && !slices.Equal(ids, []string{"work-000", "work-001"}) ||
			name == "caller-limit" && !slices.Equal(ids, []string{"work-000"}) {
			t.Fatalf("%s did not take the exact bounded prefix: %v", name, ids)
		}
	}
}

func TestObsoleteWorkSelectionRequiresValidBounds(t *testing.T) {
	state, err := Replay(testGenesis(t, deploymentPolicy))
	if err != nil {
		t.Fatal(err)
	}
	for _, limit := range []int{0, -1, 257} {
		if _, err := ObsoleteWorkIDs(state, 1000, limit); err == nil {
			t.Fatalf("invalid selection bound accepted: %d", limit)
		}
	}
	for _, at := range []int64{-1, MaxTimestamp + 1} {
		if _, err := ObsoleteWorkIDs(state, at, 256); err == nil {
			t.Fatalf("invalid selection time accepted: %d", at)
		}
	}
	if _, err := ObsoleteWorkIDs(Projection{}, 1000, 256); err == nil {
		t.Fatal("absent queue silently treated as an empty retirement batch")
	}
}
