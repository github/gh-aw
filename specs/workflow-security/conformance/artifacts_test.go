package conformance

import (
	"strings"
	"testing"
)

func TestInvocationArtifactProvenance(t *testing.T) {
	for _, mutation := range []string{"", "untrusted-prefix", "different-prefix", "cross-run", "skipped-prefix"} {
		t.Run(mutation, func(t *testing.T) {
			doc := fixtureDocument(t)
			activation := doc.Jobs["activation"]
			activation.Outputs = map[string]string{"artifact_prefix": "${{ steps.artifact-prefix.outputs.prefix }}"}
			activation.Steps = []step{{ID: "artifact-prefix", Run: `bash "${RUNNER_TEMP}/gh-aw/actions/compute_artifact_prefix.sh"`}}
			if mutation == "untrusted-prefix" {
				activation.Outputs["artifact_prefix"] = "${{ inputs.artifact_prefix }}"
			}
			if mutation == "skipped-prefix" {
				activation.Steps[0].If = "false"
			}
			doc.Jobs["activation"] = activation
			agent := doc.Jobs["agent"]
			agent.Steps[1].With["name"] = invocationPrefix + "agent"
			doc.Jobs["agent"] = agent
			processor := doc.Jobs["safe_outputs"]
			delete(processor.Steps[0].With, "name")
			processor.Steps[0].With["pattern"] = "{" + invocationPrefix + "agent," + invocationPrefix + "agent-output-fallback}"
			if mutation == "different-prefix" {
				processor.Steps[0].With["pattern"] = "{other-agent,other-agent-output-fallback}"
			}
			if mutation == "cross-run" {
				processor.Steps[0].With["run-id"] = "other-run"
			}
			doc.Jobs["safe_outputs"] = processor
			got := strings.Join(verify(doc, "daily"), "\n")
			if mutation == "" && got != "" {
				t.Fatalf("trusted symbolic handoff rejected: %s", got)
			}
			if mutation != "" && !strings.Contains(got, "ArtifactProvenance") {
				t.Fatalf("provenance mutation %q accepted", mutation)
			}
		})
	}
}
