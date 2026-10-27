package workqueue

import "testing"

func TestPermitted(t *testing.T) {
	tests := []struct {
		name string
		role string
		kind string
		want bool
	}{
		{name: "policy administrator", role: "administrator", kind: "Policy", want: true},
		{name: "policy producer", role: "producer", kind: "Policy", want: true},
		{name: "policy dispatcher", role: "dispatcher", kind: "Policy", want: true},
		{name: "policy worker rejected", role: "worker", kind: "Policy"},
		{name: "pool install administrator", role: "administrator", kind: "PoolInstall", want: true},
		{name: "pool install producer", role: "producer", kind: "PoolInstall", want: true},
		{name: "pool install dispatcher", role: "dispatcher", kind: "PoolInstall", want: true},
		{name: "pool install worker rejected", role: "worker", kind: "PoolInstall"},
		{name: "control administrator", role: "administrator", kind: "Control", want: true},
		{name: "checkpoint administrator", role: "administrator", kind: "Checkpoint", want: true},
		{name: "control producer rejected", role: "producer", kind: "Control"},
		{name: "deployment dispatcher", role: "dispatcher", kind: "Deployment", want: true},
		{name: "deployment worker rejected", role: "worker", kind: "Deployment"},
		{name: "work producer", role: "producer", kind: "Work", want: true},
		{name: "work dispatcher", role: "dispatcher", kind: "Work", want: true},
		{name: "work worker", role: "worker", kind: "Work", want: true},
		{name: "work administrator", role: "administrator", kind: "Work", want: true},
		{name: "work reconciler rejected", role: "reconciler", kind: "Work"},
		{name: "claim dispatcher", role: "dispatcher", kind: "Claim", want: true},
		{name: "claim administrator", role: "administrator", kind: "Claim", want: true},
		{name: "claim worker", role: "worker", kind: "Claim", want: true},
		{name: "claim producer rejected", role: "producer", kind: "Claim"},
		{name: "completion worker", role: "worker", kind: "Completion", want: true},
		{name: "completion administrator rejected", role: "administrator", kind: "Completion"},
		{name: "result reconciler", role: "reconciler", kind: "Result", want: true},
		{name: "delivery failure reconciler", role: "reconciler", kind: "DeliveryFailure", want: true},
		{name: "release reconciler", role: "reconciler", kind: "Release", want: true},
		{name: "result worker rejected", role: "worker", kind: "Result"},
		{name: "observation reconciler", role: "reconciler", kind: "Observation", want: true},
		{name: "observation dispatcher", role: "dispatcher", kind: "Observation", want: true},
		{name: "observation administrator", role: "administrator", kind: "Observation", want: true},
		{name: "observation worker", role: "worker", kind: "Observation", want: true},
		{name: "observation producer rejected", role: "producer", kind: "Observation"},
		{name: "dispatch dispatcher", role: "dispatcher", kind: "Dispatch", want: true},
		{name: "dispatch worker", role: "worker", kind: "Dispatch", want: true},
		{name: "dispatch reconciler", role: "reconciler", kind: "Dispatch", want: true},
		{name: "dispatch administrator rejected", role: "administrator", kind: "Dispatch"},
		{name: "claim cancellation worker", role: "worker", kind: "ClaimCancellation", want: true},
		{name: "claim cancellation reconciler", role: "reconciler", kind: "ClaimCancellation", want: true},
		{name: "claim cancellation dispatcher rejected", role: "dispatcher", kind: "ClaimCancellation"},
		{name: "work cancellation producer", role: "producer", kind: "WorkCancellation", want: true},
		{name: "work cancellation administrator", role: "administrator", kind: "WorkCancellation", want: true},
		{name: "work cancellation reconciler", role: "reconciler", kind: "WorkCancellation", want: true},
		{name: "work cancellation worker", role: "worker", kind: "WorkCancellation", want: true},
		{name: "work cancellation dispatcher rejected", role: "dispatcher", kind: "WorkCancellation"},
		{name: "work priority administrator", role: "administrator", kind: "WorkPriority", want: true},
		{name: "work priority worker rejected", role: "worker", kind: "WorkPriority"},
		{name: "issue link projector", role: "projector", kind: "IssueLink", want: true},
		{name: "issue comment projector", role: "projector", kind: "IssueComment", want: true},
		{name: "issue link administrator rejected", role: "administrator", kind: "IssueLink"},
		{name: "unknown kind rejected", role: "administrator", kind: "Unknown"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := permitted(test.role, test.kind); got != test.want {
				t.Fatalf("permitted(%q, %q) = %t, want %t", test.role, test.kind, got, test.want)
			}
		})
	}
}
