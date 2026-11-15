//go:build !integration

package workflow

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os/exec"
	"testing"
)

// These axes mirror the JS enumerator independently. The original plan's axes
// were absent; including separate later/custom jobs, review scope and dependency
// eligibility yields 564,480 cases rather than padding to 120,960.
type itemDecisionPropertyInput struct {
	gate, entry, outcome, detection, replay, family                                  string
	manifestMatches, staged, approverRecorded, reviewRepoUsable, dependenciesApplied bool
}

var itemDecisionPropertyValues = []string{"apply", "hold", "review", "stage", "drop"}

func itemDecisionPropertyRank(value string) byte {
	for rank, decision := range itemDecisionPropertyValues {
		if value == decision {
			return byte(rank)
		}
	}
	return 4
}

// itemDecisionPropertyOracle is intentionally independent of the JS production
// evaluator: authorization predicates and lattice joins are expressed directly.
func itemDecisionPropertyOracle(input itemDecisionPropertyInput) byte {
	if (input.gate != "allow" && input.gate != "partial") || !input.manifestMatches ||
		input.detection == "strict-failed" || !input.dependenciesApplied {
		return 4
	}
	gate := byte(0)
	if input.gate == "partial" {
		gate = itemDecisionPropertyRank(input.entry)
	}
	if input.replay != "run" {
		if input.replay != "approved" || !input.approverRecorded || gate != 1 {
			return 4
		}
		gate = 0
	}
	result := max(gate, itemDecisionPropertyRank(input.outcome))
	if input.staged {
		result = max(result, 3)
	}
	if input.detection == "warn-veto" {
		result = 4
	}
	if result == 2 && !input.reviewRepoUsable {
		result = 3
	}
	return result
}

func TestWriteGateDecisionsExhaustiveMirror(t *testing.T) {
	var ranks []byte
	histogram := map[string]int{}
	for _, gate := range []string{"allow", "partial", "deny", "failed", "missing", "malformed"} {
		for _, entry := range []string{"apply", "hold", "review", "stage", "drop", "missing", "invalid"} {
			for _, outcome := range itemDecisionPropertyValues {
				for _, detection := range []string{"pass", "warn-reviewable", "warn-veto", "strict-failed"} {
					for _, manifest := range []bool{false, true} {
						for _, replay := range []string{"run", "unapproved", "approved"} {
							for _, staged := range []bool{false, true} {
								for _, family := range []string{"code", "issues", "comments", "labels", "dispatch", "later", "custom"} {
									for _, approver := range []bool{false, true} {
										for _, reviewRepo := range []bool{false, true} {
											for _, dependencies := range []bool{false, true} {
												input := itemDecisionPropertyInput{gate, entry, outcome, detection, replay, family, manifest, staged, approver, reviewRepo, dependencies}
												rank := itemDecisionPropertyOracle(input)
												ranks = append(ranks, rank)
												histogram[itemDecisionPropertyValues[rank]]++
											}
										}
									}
								}
							}
						}
					}
				}
			}
		}
	}
	if len(ranks) != 564480 {
		t.Fatalf("enumerated %d cases, expected 564480", len(ranks))
	}
	digest := sha256.Sum256(ranks)
	command := exec.Command("node", "../../actions/setup/js/write_gate_decisions.test_helpers.cjs")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("running production JS exhaustive model: %v\n%s", err, output)
	}
	var actual struct {
		Count     int                 `json:"count"`
		SHA256    string              `json:"sha256"`
		Histogram map[string]int      `json:"histogram"`
		Failures  map[string]int      `json:"failures"`
		Defects   map[string][]string `json:"defects"`
	}
	if err := json.Unmarshal(output, &actual); err != nil {
		t.Fatalf("decoding JS enumeration: %v\n%s", err, output)
	}
	if actual.Count != len(ranks) || actual.SHA256 != hex.EncodeToString(digest[:]) {
		t.Fatalf("JS/Go exhaustive decisions diverged: count=%d sha256=%s, Go count=%d sha256=%x", actual.Count, actual.SHA256, len(ranks), digest)
	}
	for _, decision := range itemDecisionPropertyValues {
		if actual.Histogram[decision] != histogram[decision] {
			t.Errorf("%s histogram: JS=%d Go=%d", decision, actual.Histogram[decision], histogram[decision])
		}
	}
	if len(actual.Failures) != 0 {
		t.Fatalf("production model violates properties: %v", actual.Failures)
	}
	seeds := map[string][]string{
		"gate_failure_allow":           {"WG-1", "WG-4", "WG-5", "WG-9"},
		"malformed_file_allow":         {"WG-1", "WG-4", "WG-5", "WG-9"},
		"later_job_ignores_verdict":    {"WG-1", "WG-4", "WG-5", "WG-6"},
		"integrity_skipped":            {"WG-3", "WG-9"},
		"missing_entry_apply":          {"WG-4", "WG-5", "WG-6", "WG-9"},
		"staged_overridden":            {"WG-4", "WG-5"},
		"most_permissive":              {"WG-4", "WG-5", "WG-6", "WG-9"},
		"unusable_review_repo_apply":   {"WG-4", "WG-5", "WG-8", "WG-9"},
		"custom_job_ignores_decisions": {"WG-4", "WG-5", "WG-6"},
		"warn_veto_ignored":            {"WG-4", "WG-5"},
		"replay_bypass":                {"WG-1", "WG-3", "WG-4", "WG-6", "WG-9"},
		"hold_without_approval":        {"WG-4", "WG-9"},
	}
	for seed, required := range seeds {
		t.Run(seed, func(t *testing.T) {
			found := map[string]bool{}
			for _, property := range actual.Defects[seed] {
				found[property] = true
			}
			for _, property := range required {
				if !found[property] {
					t.Errorf("seeded defect did not violate %s; observed %v", property, actual.Defects[seed])
				}
			}
		})
	}
}
