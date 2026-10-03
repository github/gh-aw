package conformance

import "testing"

func TestJobGuardImplication(t *testing.T) {
	tests := []struct {
		condition string
		accepted  bool
	}{
		{"needs.activation.outputs.limit != 'true'", true},
		{"always() && needs.activation.result == 'success'", true},
		{"${{ always() && needs['activation'].result == 'success' }}", true},
		{"always() && !(needs.activation.result != 'success')", true},
		{"always() && ((needs.activation.result == 'success' && inputs.a) || (needs.activation.result == 'success' && inputs.b))", true},
		{"always() && success()", true},
		{"contains('always()', 'x')", true},
		{"always()", false},
		{"always() && needs.activation.result != 'failure'", false},
		{"needs.activation.result == 'success' || always()", false},
		{"always() && (needs.activation.result == 'success' || inputs.b)", false},
		{"always() && !needs.activation.result == 'success'", false},
		{"always() && (", false},
	}
	for _, tc := range tests {
		t.Run(tc.condition, func(t *testing.T) {
			err := requireJobResults(tc.condition, "activation", []string{"success"}, true)
			if (err == nil) != tc.accepted {
				t.Fatalf("accepted=%v, error=%v", tc.accepted, err)
			}
		})
	}
}

func TestDetectionGuardImplication(t *testing.T) {
	for _, condition := range []string{
		"always()", "needs.detection.result == 'success' || true",
		"always() && needs.detection.result != 'failure'",
		"always() && needs.detection.result == 'skipped'",
	} {
		if err := requireJobResults(condition, "detection", []string{"success"}, false); err == nil {
			t.Fatalf("unsafe detection gate accepted: %s", condition)
		}
	}
	if err := requireJobResults("always() && (needs.detection.result == 'success' || needs.detection.result == 'skipped')",
		"detection", []string{"success", "skipped"}, false); err != nil {
		t.Fatal(err)
	}
}
