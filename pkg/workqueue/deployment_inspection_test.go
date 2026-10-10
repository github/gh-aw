package workqueue

import (
	"strings"
	"testing"
)

func TestDeploymentInspectionPreservesContractsPinsAndFrozenDispatchAuthority(t *testing.T) {
	commits := testGenesis(t, deploymentPolicy)
	work := testNode(t, commits, "inspection-pin")
	work.ExecutionRef = pinnedRef(commits, t)
	commits = testSubmit(t, commits, "inspection-pin", work)
	commits, decision := testGrant(t, commits, "inspection-grant", 1, 1)
	dispatchID := decision.Assignments[0].DispatchID
	state, err := Replay(commits)
	if err != nil {
		t.Fatal(err)
	}
	originalProfile := state.Dispatches[dispatchID].Profile
	commits = deployTestWorker(t, commits, "inspection-deploy", "default", strings.Repeat("b", 40), strings.Repeat("b", 64), false)
	checkpoint, err := CompactCheckpoint(commits, strings.Repeat("c", 40), testActor("administrator"), 4000)
	if err != nil {
		t.Fatal(err)
	}
	for _, history := range [][]QueueCommit{commits, checkpoint} {
		page, err := TraceQueue(history, TraceOptions{RequestID: "inspection-deploy", Limit: 256}, 5000)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, event := range page.Events {
			if event.Kind != "Deployment" {
				continue
			}
			found = true
			if event.Pool != "default" || event.WorkerProfile != "default" || event.ExpectedRef != originalProfile.Ref ||
				event.ExpectedContract != originalProfile.LogicalContract || event.Profile == nil ||
				event.Profile.Ref != strings.Repeat("b", 40) || event.Profile.LogicalContract != strings.Repeat("b", 64) ||
				event.Available == nil || *event.Available {
				t.Fatalf("deployment trace lost exact CAS/profile/availability: %+v", event)
			}
		}
		if !found {
			t.Fatal("deployment request lost its retained receipt event")
		}
		page, err = TraceQueue(history, TraceOptions{RequestID: "inspection-grant", Limit: 256}, 5000)
		if err != nil {
			t.Fatal(err)
		}
		foundWork, foundDispatch := false, false
		for _, event := range page.Events {
			if event.Kind == "Work" {
				foundWork = true
				if event.ExecutionRef != work.ExecutionRef || event.AdmissionContract != originalProfile.LogicalContract {
					t.Fatal("inspection dropped the original Work pin or sealed admission")
				}
			}
			if event.Kind == "Claim" && event.DispatchID == dispatchID {
				foundDispatch = true
				if event.Profile == nil || *event.Profile != originalProfile {
					t.Fatal("inspection substituted current deployment for the frozen Dispatch profile")
				}
			}
		}
		if !foundWork || !foundDispatch {
			t.Fatalf("inspection lost Work or Dispatch provenance across checkpoint: work=%v dispatch=%v", foundWork, foundDispatch)
		}
	}
}
