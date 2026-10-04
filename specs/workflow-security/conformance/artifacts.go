package conformance

import (
	"fmt"
	"strings"
)

const invocationPrefix = "${{ needs.activation.outputs.artifact_prefix }}"

func trustedInvocationPrefix(doc document) bool {
	activation := doc.Jobs["activation"]
	if activation.Outputs["artifact_prefix"] != "${{ steps.artifact-prefix.outputs.prefix }}" {
		return false
	}
	count := 0
	for _, s := range activation.Steps {
		if s.ID == "artifact-prefix" {
			count++
			if s.If != "" || s.ContinueOnError != nil && s.ContinueOnError != false ||
				strings.TrimSpace(s.Run) != `bash "${RUNNER_TEMP}/gh-aw/actions/compute_artifact_prefix.sh"` {
				return false
			}
		}
	}
	return count == 1
}

func checkArtifacts(doc document) []string {
	uploaded := map[string]struct{}{}
	for _, s := range doc.Jobs["agent"].Steps {
		if strings.HasPrefix(s.Uses, "actions/upload-artifact@") {
			uploaded[fmt.Sprint(s.With["name"])] = struct{}{}
		}
	}
	var violations []string
	found := false
	for _, s := range doc.Jobs["safe_outputs"].Steps {
		if !strings.HasPrefix(s.Uses, "actions/download-artifact@") {
			continue
		}
		if _, ok := s.With["run-id"]; ok {
			violations = append(violations, "ArtifactProvenance: cross-run download")
		}
		if _, ok := s.With["repository"]; ok {
			violations = append(violations, "ArtifactProvenance: cross-repository download")
		}
		for _, prefix := range []string{"", invocationPrefix} {
			name := prefix + "agent"
			pattern := "{" + name + "," + prefix + "agent-output-fallback}"
			matched := fmt.Sprint(s.With["name"]) == name || fmt.Sprint(s.With["pattern"]) == pattern
			if matched {
				_, present := uploaded[name]
				found = found || present
				if prefix != "" && !trustedInvocationPrefix(doc) {
					violations = append(violations, "ArtifactProvenance: invocation prefix lacks trusted activation producer")
				}
			}
		}
	}
	if !found {
		violations = append(violations, "ArtifactProvenance: no matching agent artifact handoff")
	}
	return violations
}
