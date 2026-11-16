package cli

import (
	"context"
	"fmt"
	"net/url"
	"path"
	"strings"

	"github.com/github/gh-aw/pkg/repoutil"
)

// evalAssignToAgent requires a post-assignment link to a visible agent-authored PR.
func evalAssignToAgent(ctx context.Context, item CreatedItemReport, repoOverride string) AdoptionReport {
	repo := resolveItemRepo(item, repoOverride)
	num := resolveItemNumber(item)
	report := AdoptionReport{
		Type: item.Type, ObjectURL: item.URL, ObjectNumber: num, Repo: repo,
		AdoptionEvaluation: adoptionEvidence(AdoptionStatusUnknown, EvidenceNone, "missing_reference"),
	}
	if num == 0 || repo == "" {
		return report
	}
	_, err := adoptionEvidenceGHAPIGet(ctx, fmt.Sprintf("issues/%d", num), repo)
	if err != nil {
		adoptionAPIError(&report, err, false)
		return report
	}
	events, err := adoptionEvidenceGHAPIGetArray(ctx, fmt.Sprintf("issues/%d/timeline", num), repo)
	if err != nil {
		adoptionAPIError(&report, err, false)
		return report
	}
	for _, event := range events {
		if adoptionString(event["event"]) != "cross-referenced" {
			continue
		}
		at := adoptionString(event["created_at"])
		if !adoptionAfter(at, item.Timestamp) {
			continue
		}
		source := adoptionValue[map[string]any](event["source"])
		issue := adoptionValue[map[string]any](source["issue"])
		if issue["pull_request"] == nil {
			continue
		}
		login := adoptionNestedString(issue["user"], "login")
		if !strings.EqualFold(login, "copilot-swe-agent") && !strings.EqualFold(login, "github-actions[bot]") {
			continue
		}
		prNumber := metadataInt(issue, "number")
		prRepo := linkedAdoptionIssueRepo(issue, repo)
		if prNumber <= 0 || prRepo == "" {
			continue
		}
		pr, err := adoptionEvidenceGHAPIGet(ctx, fmt.Sprintf("pulls/%d", prNumber), prRepo)
		if err != nil {
			adoptionAPIError(&report, err, false)
			return report
		}

		if adoptionValue[bool](pr["merged"]) {
			report.AdoptionEvaluation = adoptionEvidence(AdoptionStatusAccepted, EvidenceStrong, "merged")
		} else if adoptionString(pr["state"]) == "closed" {
			report.AdoptionEvaluation = adoptionEvidence(AdoptionStatusRejected, EvidenceStrong, "closed_without_merge")
		} else {
			report.AdoptionEvaluation = adoptionEvidence(AdoptionStatusPending, EvidenceMedium, "open")
		}
		report.Detail = report.Signal
		return report
	}
	report.AdoptionEvaluation = adoptionEvidence(AdoptionStatusUnknown, EvidenceNone, "missing_execution_state")
	report.Detail = "no attributable agent PR"
	return report
}

func linkedAdoptionIssueRepo(issue map[string]any, sourceRepo string) string {
	u, err := url.Parse(adoptionString(issue["repository_url"]))
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		return ""
	}
	_, repo, found := strings.Cut(u.Path, "/repos/")
	owner, name, hasName := strings.Cut(repo, "/")
	if !found || !hasName || owner == "" || name == "" || strings.Contains(name, "/") || owner == "." || owner == ".." || name == "." || name == ".." {
		return ""
	}
	_, host := repoutil.NormalizeRepoForAPI(sourceRepo)
	if host != "" {
		return path.Join(host, repo)
	}
	return repo
}
