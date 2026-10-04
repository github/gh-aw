package conformance

import (
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/workflow"
	"gopkg.in/yaml.v3"
)

func fixtureDocument(t *testing.T) document {
	t.Helper()
	var doc document
	if err := yaml.Unmarshal([]byte(minimalLock), &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

func TestCompilerDeclaredDetectionPolicy(t *testing.T) {
	tests := []struct {
		name, mode    string
		removeJob     bool
		wantViolation string
	}{
		{"enabled", "enabled", false, ""},
		{"deleted-detector", "enabled", true, "missing detection"},
		{"missing-policy", "", false, "missing compiler-declared"},
		{"unknown-policy", "bogus", false, "unsupported compiler-declared"},
		{"declared-disabled", "disabled", true, ""},
		{"contradiction", "disabled", false, "contradicts disabled"},
		{"missing-condition", "conditional", false, "lacks enablement"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			doc := fixtureDocument(t)
			if tc.mode != "" {
				doc.Policy = &workflow.GHAWManifestDetectionPolicy{Mode: tc.mode}
			}
			if tc.removeJob {
				delete(doc.Jobs, "detection")
				processor := doc.Jobs["safe_outputs"]
				processor.Needs = yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "agent"}
				processor.If = "needs.agent.result != 'skipped'"
				doc.Jobs["safe_outputs"] = processor
			}
			got := strings.Join(verify(doc, "compiled"), "\n")
			if tc.wantViolation == "" && got != "" ||
				tc.wantViolation != "" && !strings.Contains(got, tc.wantViolation) {
				t.Fatalf("expected %q, got %s", tc.wantViolation, got)
			}
			if tc.removeJob && len(verify(doc, "daily")) == 0 {
				t.Fatal("strict profile must still reject disabled or deleted detection")
			}
		})
	}
}

func TestConditionalDetectionPolicy(t *testing.T) {
	for _, condition := range []string{
		"always() && inputs.enable_detection",
		"always()",
		"always() && !inputs.enable_detection",
		"always() && (inputs.enable_detection || inputs.bypass)",
	} {
		doc := fixtureDocument(t)
		doc.Policy = &workflow.GHAWManifestDetectionPolicy{Mode: "conditional", Condition: "${{ inputs.enable_detection }}"}
		detector := doc.Jobs["detection"]
		detector.If = condition
		doc.Jobs["detection"] = detector
		processor := doc.Jobs["safe_outputs"]
		processor.If = "always() && (needs.detection.result == 'success' || needs.detection.result == 'skipped')"
		doc.Jobs["safe_outputs"] = processor
		got := strings.Join(verify(doc, "compiled"), "\n")
		if condition == "always() && inputs.enable_detection" && got != "" {
			t.Fatalf("valid conditional policy rejected: %s", got)
		}
		if condition != "always() && inputs.enable_detection" && !strings.Contains(got, "DetectionPolicy") {
			t.Fatalf("enablement bypass accepted: %s", condition)
		}
	}
}
