package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
)

var writeGateReasonPattern = regexp.MustCompile(`^[a-z0-9_.-]{1,64}$`)
var writeGateDigestPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

// WriteGateReport describes observed publication policy, not authorization.
type WriteGateReport struct {
	Verdict         string          `json:"verdict"`
	Reason          string          `json:"reason"`
	ManifestDigest  string          `json:"manifest_digest,omitempty"`
	Integrity       string          `json:"integrity"`
	ConclusionCheck string          `json:"conclusion_check"`
	FailureCategory string          `json:"failure_category,omitempty"`
	Counts          map[string]int  `json:"counts,omitempty"`
	Items           []WriteGateItem `json:"items,omitempty"`
	Error           string          `json:"error,omitempty"`
}

type WriteGateItem struct {
	OutputID       string `json:"output_id"`
	Decision       string `json:"gate_decision"`
	DecisionReason string `json:"decision_reason,omitempty"`
	ItemSHA256     string `json:"item_sha256,omitempty"`
}

func readWriteGate(runDir string) *WriteGateReport {
	session, _, err := readSessionModelRouting(runDir)
	if err != nil {
		auditReportLog.Printf("Failed to read write gate evidence: %v", err)
		return nil
	}
	if session == nil {
		return nil
	}
	return session.WriteGate
}

func parseWriteGate(event sessionModelRoutingEvent) (*WriteGateReport, error) {
	var data struct {
		Verdict         string          `json:"verdict"`
		Reason          string          `json:"reason"`
		ManifestDigest  string          `json:"manifestDigest"`
		Integrity       string          `json:"integrity"`
		ConclusionCheck string          `json:"conclusionCheck"`
		FailureCategory string          `json:"failureCategory"`
		Counts          map[string]int  `json:"counts"`
		Items           []WriteGateItem `json:"items"`
	}
	if err := json.Unmarshal(event.Data, &data); err != nil {
		return nil, err
	}
	validCheck := func(check string) bool { return check == "verified" || check == "failed" || check == "unknown" }
	if (data.Verdict != "allow" && data.Verdict != "deny" && data.Verdict != "partial") ||
		!writeGateReasonPattern.MatchString(data.Reason) || !validCheck(data.Integrity) || !validCheck(data.ConclusionCheck) ||
		(data.ManifestDigest != "" && !writeGateDigestPattern.MatchString(data.ManifestDigest)) {
		return nil, fmt.Errorf("invalid write gate conclusion evidence")
	}
	for decision, count := range data.Counts {
		if count < 0 || !validGateItemDecision(decision) {
			return nil, fmt.Errorf("invalid write gate decision count")
		}
	}
	for _, item := range data.Items {
		if !validGateItemDecision(item.Decision) || item.OutputID == "" ||
			(item.ItemSHA256 != "" && !writeGateDigestPattern.MatchString(item.ItemSHA256)) ||
			(item.DecisionReason != "" && !writeGateReasonPattern.MatchString(item.DecisionReason)) {
			return nil, fmt.Errorf("invalid write gate item evidence")
		}
	}
	return &WriteGateReport{
		Verdict: data.Verdict, Reason: data.Reason, ManifestDigest: data.ManifestDigest,
		Integrity: data.Integrity, ConclusionCheck: data.ConclusionCheck, FailureCategory: data.FailureCategory,
		Counts: data.Counts, Items: data.Items,
	}, nil
}

func validGateItemDecision(decision string) bool {
	switch decision {
	case "apply", "hold", "review", "stage", "drop":
		return true
	default:
		return false
	}
}

func renderConsoleWriteGate(gate *WriteGateReport) {
	if gate == nil {
		return
	}
	fmt.Fprintf(os.Stderr, "  Write gate: verdict=%s reason=%s manifest_digest=%s integrity=%s conclusion_check=%s\n",
		gate.Verdict, gate.Reason, gate.ManifestDigest, gate.Integrity, gate.ConclusionCheck)
	if gate.Error != "" {
		fmt.Fprintf(os.Stderr, "    error: %s\n", safeModelRoutingText(gate.Error))
	}
	for _, item := range gate.Items {
		fmt.Fprintf(os.Stderr, "    %s: %s reason=%s digest=%s\n", safeModelRoutingText(item.OutputID), item.Decision, item.DecisionReason, item.ItemSHA256)
	}
}

func gateBlockedOutcome(item CreatedItemReport) (OutcomeReport, bool) {
	signal := item.GateSignal
	if signal == "" {
		signal = item.DecisionReason
	}
	switch signal {
	case "gate_denied", "gate_held", "gate_staged", "gate_dropped":
	default:
		if item.Status != "skipped" && item.Status != "staged" {
			return OutcomeReport{}, false
		}
		switch item.GateDecision {
		case "hold":
			signal = "gate_held"
		case "stage":
			signal = "gate_staged"
		case "drop":
			signal = "gate_dropped"
		default:
			return OutcomeReport{}, false
		}
	}
	if item.Status == "success" || item.Status == "warning" {
		// A replay or review publication is evaluated by its actual resource.
		return OutcomeReport{}, false
	}
	return OutcomeReport{Type: item.Type, Detail: signal,
		OutcomeEvaluation: outcomeEvidence(OutcomeStatusSkipped, EvidenceStrong, signal)}, true
}
