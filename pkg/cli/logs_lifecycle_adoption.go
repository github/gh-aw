package cli

import (
	"context"
	"fmt"
	"net/url"
	"path"
	"time"

	"github.com/github/gh-aw/pkg/lifecycle"
)

func observeLifecycle(ctx context.Context, run WorkflowRun, item CreatedItemReport, record lifecycle.Record) (OutcomeReport, error) {
	report := OutcomeReport{
		ItemID: record.ID, Type: item.Type, ObjectURL: record.TargetURL,
		ObjectNumber: resolveItemNumber(item), Repo: resolveItemRepo(item, logsOutcomeRepository(run)),
		CreatedAt: item.Timestamp, CheckedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	if item.Type == "create_issue" || item.Type == "create_pull_request" {
		observation := observeLifecycleProposal(ctx, run, item, record)
		var err error
		record, err = lifecycle.Observe(record, observation)
		if err != nil {
			return report, err
		}
	} else if rule, ok := lifecycle.RuleFor(item.Type); ok && rule.FallbackToDelivery {
		record.Survival = lifecycle.Survival{State: "unknown", Coverage: "unsupported", ObservedAt: time.Now().UTC()}
		if evaluator, supported := outcomeEvaluators[item.Type]; supported {
			evidence := evaluator(ctx, item, resolveItemRepo(item, logsOutcomeRepository(run)))
			evaluation := normalizeOutcomeEvaluation(evidence)
			switch {
			case evidence.EvalError != "":
				record.Survival = lifecycle.Survival{State: "unknown", Coverage: "unavailable", Error: evidence.EvalError, ObservedAt: time.Now().UTC()}
			case evaluation.OutcomeStatus == OutcomeStatusRejected && lifecycleEffectReversed(evaluation.Signal):
				record.Adoption.State, record.Adoption.Signal = "rejected", evaluation.Signal
				record.Adoption.At = time.Time{}
				record.Adoption.Source = "github-api"
				record.Adoption.ObservedAt = time.Now().UTC()
				record.ReversalSignal = evaluation.Signal
				record.Survival = lifecycle.Survival{State: "reversed", Signal: evaluation.Signal, ObservedAt: time.Now().UTC(), Coverage: "snapshot"}
			case evaluation.OutcomeStatus == OutcomeStatusRejected:
				record.Survival = lifecycle.Survival{State: "unknown", Signal: evaluation.Signal, ObservedAt: time.Now().UTC(), Coverage: "unsupported"}
			case evaluation.OutcomeStatus == OutcomeStatusAccepted && evaluation.Signal != "target_exists_only":
				record.Survival = lifecycle.Survival{State: "observed", Signal: evaluation.Signal, ObservedAt: time.Now().UTC(), Coverage: "snapshot"}
			}
		}
	}
	applyLifecycleObservation(&report, record)
	if item.Type == "create_pull_request" && record.Adoption.Coverage == "available" {
		enrichPullRequestEffort(ctx, item, &report)
	}
	return report, ctx.Err()
}

func applyLifecycleObservation(report *OutcomeReport, record lifecycle.Record) {
	report.Lifecycle = &record
	report.Adoption, report.Survival = &record.Adoption, &record.Survival
	report.OutcomeEvaluation = outcomeEvidence(OutcomeStatus(record.Adoption.State), EvidenceMedium, record.Adoption.Signal)
	if record.Adoption.HumanVerified {
		report.EvidenceStrength = EvidenceStrong
	}
	if record.Adoption.Coverage == "unavailable" {
		report.OutcomeEvaluation = outcomeEvidence(OutcomeStatusUnknown, EvidenceNone, "provider_evidence_unavailable")
		report.EvalError = record.Adoption.Error
	}
	report.Detail = record.Adoption.Signal
	if record.Survival.Error != "" {
		report.Detail += "; survival unavailable: " + record.Survival.Error
	}
	report.ReversalSignal = record.ReversalSignal
	if !record.ReversedAt.IsZero() {
		report.ReversedAt = record.ReversedAt.Format(time.RFC3339Nano)
	}
	if !record.Adoption.At.IsZero() {
		report.TimeToOutcomeHours = record.Adoption.At.Sub(record.Delivery.At).Hours()
	}
}

func lifecycleEffectReversed(signal string) bool {
	switch signal {
	case "state_reverted", "state_replaced", "reopened", "milestone_removed",
		"review_dismissed", "review_request_removed", "workflow_failed", "closed_by_merge":
		return true
	default:
		return false
	}
}

func observeLifecycleProposal(ctx context.Context, run WorkflowRun, item CreatedItemReport, record lifecycle.Record) lifecycle.Observation { //nolint:largefunc // Keeps one snapshot's resource, actor, authorization, and event-clock checks together.
	observation := lifecycle.Observation{ObservedAt: time.Now().UTC()}
	repository := resolveItemRepo(item, logsOutcomeRepository(run))
	number := resolveItemNumber(item)
	if number <= 0 || record.TargetURL == "" {
		observation.Error = "adoption requires the exact delivered issue or pull-request URL"
		return observation
	}
	kind := "issues"
	if item.Type == "create_pull_request" {
		kind = "pulls"
	}
	data, err := outcomeEvidenceGHAPIGet(ctx, fmt.Sprintf("%s/%d", kind, number), repository)
	if err != nil {
		// A 404 can also mean lost visibility; it is not proof of deletion.
		observation.Error = err.Error()
		return observation
	}
	if outcomeString(data["html_url"]) != record.TargetURL {
		observation.Error = "provider resource identity does not match the delivered URL"
		return observation
	}
	state := outcomeString(data["state"])
	if state == "open" {
		observation.State, observation.Signal = "pending", "open"
		if record.Adoption.State == "accepted" {
			observation.Signal = "reopened"
		}
		return observation
	}
	if state != "closed" {
		observation.State, observation.Signal = "unknown", "missing_resolution_evidence"
		return observation
	}
	actor := outcomeValue[map[string]any](data["closed_by"])
	clock := outcomeString(data["closed_at"])
	if kind == "pulls" && outcomeValue[bool](data["merged"]) {
		actor = outcomeValue[map[string]any](data["merged_by"])
		clock = outcomeString(data["merged_at"])
	}
	if actor == nil && kind == "pulls" {
		issue, err := outcomeEvidenceGHAPIGet(ctx, fmt.Sprintf("issues/%d", number), repository)
		if err != nil {
			observation.Error = err.Error()
			return observation
		}
		actor = outcomeValue[map[string]any](issue["closed_by"])
	}
	at, err := time.Parse(time.RFC3339Nano, clock)
	if err != nil {
		observation.State, observation.Signal = "unknown", "missing_decision_clock"
		return observation
	}
	observation.At = at
	publisher := outcomeNestedString(data["user"], "login")
	login, actorType := outcomeString(actor["login"]), outcomeString(actor["type"])
	if login != "" {
		observation.Actor = lifecycle.Actor{Login: login, Type: actorType}
	}
	if login != "" && (actorType == "Bot" || isBotUser(login)) {
		// Automation resolves the resource without establishing human adoption.
		observation.AcceptanceKind = "automation"
		observation.State, observation.Signal = lifecycleProposalResolution(kind, data)
		return observation
	}
	if !lifecycle.IsHuman(actorType, login, []string{publisher}) {
		observation.State, observation.Signal = "unknown", "unverified_decision_actor"
		return observation
	}
	permission, err := outcomeEvidenceGHAPIGet(ctx, path.Join("collaborators", url.PathEscape(login), "permission"), repository)
	if err != nil {
		observation.Error = "decision actor authorization unavailable: " + err.Error()
		return observation
	}
	switch outcomeString(permission["permission"]) {
	case "admin", "write", "maintain":
	default:
		observation.State, observation.Signal = "unknown", "unverified_decision_authorization"
		return observation
	}
	observation.HumanVerified = true
	observation.AcceptanceKind = "explicit_human"
	observation.State, observation.Signal = lifecycleProposalResolution(kind, data)
	return observation
}

// lifecycleProposalResolution maps a closed issue or pull request to its adoption outcome.
func lifecycleProposalResolution(kind string, data map[string]any) (state, signal string) {
	if kind == "pulls" {
		if outcomeValue[bool](data["merged"]) {
			return "accepted", "merged"
		}
		return "rejected", "closed_without_merge"
	}
	switch outcomeString(data["state_reason"]) {
	case "completed":
		return "accepted", "completed"
	case "not_planned":
		return "rejected", "closed_not_planned"
	default:
		return "unknown", "missing_resolution_reason"
	}
}
