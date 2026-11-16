package cli

import (
	"context"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/errorutil"
)

var adoptionEvidenceGHAPIGet = ghAPIGet
var adoptionEvidenceGHAPIGetArray = ghAPIGetArray

func adoptionValue[T any](raw any) T {
	if value, ok := raw.(T); ok {
		return value
	}
	var zero T
	return zero
}

func adoptionEvidence(status AdoptionStatus, strength EvidenceStrength, signal string) AdoptionEvaluation {
	return AdoptionEvaluation{AdoptionStatus: status, EvidenceStrength: strength, Signal: signal}
}

func validAdoptionSHA(sha string) bool {
	if len(sha) < 7 || len(sha) > 40 {
		return false
	}
	for _, c := range sha {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}
	return true
}

func unsupportedAdoption(item CreatedItemReport, repo string) AdoptionReport {
	return AdoptionReport{
		Type: item.Type, ObjectURL: item.URL, ObjectNumber: resolveItemNumber(item), Repo: resolveItemRepo(item, repo),
		AdoptionEvaluation: adoptionEvidence(AdoptionStatusUnknown, EvidenceNone, "unsupported_evaluator"),
		Detail:             "no action-specific evaluator",
	}
}

func adoptionAPIError(report *AdoptionReport, err error, persistent bool) {
	if persistent && errorutil.IsNotFoundError(err) {
		report.AdoptionEvaluation = adoptionEvidence(AdoptionStatusRejected, EvidenceStrong, "deleted")
		report.Detail = "deleted or inaccessible"
		return
	}
	report.AdoptionEvaluation = adoptionEvidence(AdoptionStatusUnknown, EvidenceWeak, "evaluation_error")
	report.EvalError = err.Error()
}

func isNonBotActor(raw any) bool {
	user, ok := raw.(map[string]any)
	if !ok {
		return false
	}
	login := adoptionString(user["login"])
	return login != "" && !isBotUser(login) && !strings.EqualFold(adoptionString(user["type"]), "Bot")
}

func nonBotCommentsAfter(comments []map[string]any, timestamp string) int {
	start, err := time.Parse(time.RFC3339, timestamp)
	if err != nil {
		return 0
	}

	count := 0
	for _, comment := range comments {
		at, err := time.Parse(time.RFC3339, adoptionString(comment["created_at"]))
		if err == nil && at.After(start) && isNonBotActor(comment["user"]) {
			count++
		}
	}
	return count
}

func adoptionAfter(candidate, threshold string) bool {
	at, err := time.Parse(time.RFC3339, candidate)
	if err != nil {
		return false
	}
	start, err := time.Parse(time.RFC3339, threshold)
	return err == nil && at.After(start)
}

func adoptionCloseActor(ctx context.Context, number int, repo string) (bool, error) {
	return isLatestCloseByBot(ctx, number, repo, adoptionEvidenceGHAPIGetArray)
}
