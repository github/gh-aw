package conformance

import (
	"slices"

	"github.com/github/gh-aw/pkg/workflow"
)

func effectivePolicy(doc document, profile string) (*workflow.GHAWManifestDetectionPolicy, []string) {
	required := &workflow.GHAWManifestDetectionPolicy{Mode: "enabled"}
	if profile != "compiled" {
		return required, nil
	}
	if doc.Policy == nil {
		return required, []string{"DetectionPolicy: missing compiler-declared policy; recompile the workflow"}
	}
	if !slices.Contains([]string{"enabled", "disabled", "conditional"}, doc.Policy.Mode) {
		return required, []string{"DetectionPolicy: unsupported compiler-declared mode"}
	}
	if doc.Policy.Mode == "conditional" && doc.Policy.Condition == "" {
		return required, []string{"DetectionPolicy: conditional mode lacks enablement expression"}
	}
	if doc.Policy.Mode != "conditional" && doc.Policy.Condition != "" {
		return required, []string{"DetectionPolicy: only conditional mode can declare enablement"}
	}
	return doc.Policy, nil
}

func checkGraph(doc document, policy *workflow.GHAWManifestDetectionPolicy) []string {
	var violations []string
	required := []string{"activation", "agent", "safe_outputs"}
	prerequisites := map[string][]string{"agent": {"activation"}, "safe_outputs": {"agent"}}
	if policy.Mode != "disabled" {
		required = append(required, "detection")
		prerequisites["detection"] = []string{"agent"}
		prerequisites["safe_outputs"] = append(prerequisites["safe_outputs"], "detection")
	} else if _, found := doc.Jobs["detection"]; found {
		violations = append(violations, "DetectionPolicy: detection job contradicts disabled policy")
	}
	for _, name := range required {
		if _, ok := doc.Jobs[name]; !ok {
			violations = append(violations, "JobIsolation: missing "+name+" job")
		}
	}
	for name, expected := range prerequisites {
		needs, err := dependencies(doc.Jobs[name].Needs)
		if err != nil {
			violations = append(violations, "JobIsolation: "+name+": "+err.Error())
			continue
		}
		for _, dependency := range expected {
			if !slices.Contains(needs, dependency) {
				violations = append(violations, "JobIsolation: "+name+" lacks "+dependency)
			}
		}
	}
	violations = append(violations, checkDetectionGate(doc, policy)...)
	return violations
}

func checkDetectionGate(doc document, policy *workflow.GHAWManifestDetectionPolicy) []string {
	if policy.Mode == "disabled" {
		return nil
	}
	results := []string{"success"}
	var violations []string
	if policy.Mode == "conditional" {
		results = append(results, "skipped")
		if err := requirePredicate(doc.Jobs["detection"].If, policy.Condition); err != nil {
			violations = append(violations, "DetectionPolicy: "+err.Error())
		}
	}
	if err := requireJobResults(doc.Jobs["safe_outputs"].If, "detection", results, false); err != nil {
		violations = append(violations, "DetectionGate: "+err.Error())
	}
	return violations
}
