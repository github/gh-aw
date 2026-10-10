//go:build !integration

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/github/gh-aw/pkg/github"
	"github.com/github/gh-aw/pkg/repoutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestComputeOutcomeSummary(t *testing.T) {
	reports := []OutcomeReport{
		{Type: "create_pull_request", OutcomeEvaluation: OutcomeEvaluation{OutcomeStatus: OutcomeStatusAccepted}, ZeroTouch: true, TimeToOutcomeHours: 2.0},
		{Type: "create_pull_request", OutcomeEvaluation: OutcomeEvaluation{OutcomeStatus: OutcomeStatusAccepted}, ZeroTouch: false, TimeToOutcomeHours: 8.0},
		{Type: "create_issue", OutcomeEvaluation: OutcomeEvaluation{OutcomeStatus: OutcomeStatusRejected}, TimeToOutcomeHours: 24.0},
		{Type: "add_comment", OutcomeEvaluation: OutcomeEvaluation{OutcomeStatus: OutcomeStatusIgnored}},
		{Type: "assign_to_agent", OutcomeEvaluation: OutcomeEvaluation{OutcomeStatus: OutcomeStatusPending}},
		{Type: "close_issue", OutcomeEvaluation: OutcomeEvaluation{OutcomeStatus: OutcomeStatusLifecycle}},
	}

	s := ComputeOutcomeSummary(reports, github.DefaultObjectiveMapping())

	assert.Equal(t, 6, s.Total, "total should count all reports")
	assert.Equal(t, 2, s.Accepted, "accepted count")
	assert.Equal(t, 1, s.Rejected, "rejected count")
	assert.Equal(t, 1, s.Ignored, "ignored count")
	assert.Equal(t, 1, s.Pending, "pending count")
	assert.Equal(t, 1, s.Lifecycle, "lifecycle count")
	assert.Equal(t, 1, s.ZeroTouch, "zero-touch count")
	assert.Equal(t, 0, s.AcceptedStrong, "accepted strong count")
	assert.Equal(t, 2, s.AcceptedMedium, "accepted medium count")
	assert.Equal(t, 0, s.AcceptedWeak, "accepted weak count")

	// AcceptanceRate = accepted / (accepted + rejected) = 2/3
	assert.InDelta(t, 0.6667, s.AcceptanceRate, 0.01, "acceptance rate")

	// WasteRate = rejected / total = 1/6
	assert.InDelta(t, 0.1667, s.WasteRate, 0.01, "waste rate")

	// ZeroTouchRate = zero_touch / accepted = 1/2
	assert.InDelta(t, 0.5, s.ZeroTouchRate, 0.01, "zero-touch rate")

	// MedianTimeToOutcome of [2.0, 8.0, 24.0] = 8.0
	assert.InDelta(t, 8.0, s.MedianTimeToOutcome, 0.01, "median time to outcome")
}

func TestComputeOutcomeSummaryEmpty(t *testing.T) {
	s := ComputeOutcomeSummary(nil, github.DefaultObjectiveMapping())

	assert.Equal(t, 0, s.Total, "empty total")
	assert.InDelta(t, 0.0, s.AcceptanceRate, 1e-12, "empty acceptance rate")
	assert.InDelta(t, 0.0, s.WasteRate, 1e-12, "empty waste rate")
	assert.InDelta(t, 0.0, s.ZeroTouchRate, 1e-12, "empty zero-touch rate")
}

func TestComputeOutcomeSummaryReconcilesUnknownAndErrors(t *testing.T) {
	reports := []OutcomeReport{
		{OutcomeEvaluation: outcomeEvidence(OutcomeStatusUnknown, EvidenceNone, "missing_reference")},
		{OutcomeEvaluation: outcomeEvidence(OutcomeStatusError, EvidenceWeak, "evaluation_error")},
		{OutcomeEvaluation: outcomeEvidence(OutcomeStatusLifecycleClose, EvidenceMedium, "lifecycle_close")},
	}
	s := ComputeOutcomeSummary(reports, github.DefaultObjectiveMapping())
	assert.Equal(t, 1, s.Unknown)
	assert.Equal(t, 1, s.Errors)
	assert.Equal(t, 1, s.Lifecycle)
	assert.Equal(t, s.Total, s.Accepted+s.Rejected+s.Ignored+s.Pending+s.Unknown+s.Errors+s.Lifecycle)
}

func TestParseNumberFromURL(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		expected int
	}{
		{"PR URL", "https://github.com/owner/repo/pull/42", 42},
		{"issue URL", "https://github.com/owner/repo/issues/108", 108},
		{"comment URL", "https://github.com/owner/repo/issues/123#issuecomment-456", 123},
		{"review comment URL", "https://github.com/owner/repo/pull/123/comments/456", 123},
		{"non-object numeric URL", "https://github.com/owner/repo/actions/runs/123", 0},
		{"empty", "", 0},
		{"no number", "https://github.com/owner/repo", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, parseNumberFromURL(tt.url), "parsed number from URL")
		})
	}
}

func TestParseRepoFromURL(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		expected string
	}{
		{"full URL", "https://github.com/owner/repo/pull/42", "owner/repo"},
		{"issues URL", "https://github.com/github/gh-aw/issues/123", "github/gh-aw"},
		{"no github", "https://example.com/foo", ""},
		{"empty", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, parseRepoFromURL(tt.url), "parsed repo from URL")
		})
	}
}

func TestNormalizeRepoForAPI(t *testing.T) {
	tests := []struct {
		name          string
		repo          string
		wantOwnerRepo string
		wantHost      string
	}{
		{"plain owner/repo", "owner/repo", "owner/repo", ""},
		{"GHES HOST/owner/repo", "myhost.com/owner/repo", "owner/repo", "myhost.com"},
		{"github.com/owner/repo treated as host prefix", "github.com/owner/repo", "owner/repo", "github.com"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ownerRepo, host := repoutil.NormalizeRepoForAPI(tt.repo)
			assert.Equal(t, tt.wantOwnerRepo, ownerRepo, "owner/repo portion")
			assert.Equal(t, tt.wantHost, host, "host portion")
		})
	}
}

func TestEscapeOwnerRepo(t *testing.T) {
	tests := []struct {
		name      string
		ownerRepo string
		want      string
	}{
		{name: "normal owner/repo", ownerRepo: "github/gh-aw", want: "github/gh-aw"},
		{name: "traversal in repo segment", ownerRepo: "owner/../etc/passwd", want: "owner/..%2Fetc%2Fpasswd"},
		{name: "percent encoded slash is neutralized", ownerRepo: "owner/repo%2Ftraversal", want: "owner/repo%252Ftraversal"},
		{name: "no slash fallback", ownerRepo: "noSlash", want: "noSlash"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, escapeOwnerRepo(tt.ownerRepo))
		})
	}
}

func TestValidateAPIEndpoint(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
		wantErr  string
	}{
		{name: "relative endpoint allowed", endpoint: "issues/comments/123"},
		{name: "leading slash rejected", endpoint: "/issues/comments/123", wantErr: "must not start"},
		{name: "dotdot segment rejected", endpoint: "issues/../comments/123", wantErr: "must not contain"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateAPIEndpoint(tt.endpoint)
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestIsBotUser(t *testing.T) {
	assert.True(t, isBotUser("github-actions[bot]"), "github-actions[bot] is a bot")
	assert.True(t, isBotUser("github-actions"), "github-actions is a bot")
	assert.True(t, isBotUser("copilot-swe-agent"), "copilot-swe-agent is a bot")
	assert.False(t, isBotUser("mnkiefer"), "human user is not a bot")
}

func TestCountHumanComments(t *testing.T) {
	comments := []map[string]any{
		{"user": map[string]any{"login": "octocat"}},
		{"user": map[string]any{"login": "github-actions[bot]"}},
		{"user": map[string]any{"login": "copilot-swe-agent"}},
		{"user": map[string]any{"login": "hubot"}},
	}

	assert.Equal(t, 2, countHumanComments(comments), "should count only non-bot comments")
	assert.Equal(t, 0, countHumanComments(nil), "empty comment list")
	assert.Equal(t, 0, countHumanComments([]map[string]any{{}}), "missing actor cannot establish non-bot activity")
}

func TestCountHumanCommentsAfter(t *testing.T) {
	comments := []map[string]any{
		{"created_at": "2026-05-12T00:00:00Z", "user": map[string]any{"login": "octocat"}},
		{"created_at": "2026-05-12T00:01:00Z", "user": map[string]any{"login": "github-actions[bot]"}},
		{"created_at": "2026-05-12T00:02:00Z", "user": map[string]any{"login": "monalisa"}},
	}

	assert.Equal(t, 1, countHumanCommentsAfter(comments, "2026-05-12T00:00:00Z"), "should count only later human replies")
}

func TestIsLatestCloseByBot(t *testing.T) {
	cases := []struct {
		name      string
		events    []map[string]any
		wantIsBot bool
	}{
		{
			name: "latest close by bot",
			events: []map[string]any{
				{"event": "closed", "actor": map[string]any{"login": "octocat"}},
				{"event": "reopened", "actor": map[string]any{"login": "octocat"}},
				{"event": "closed", "actor": map[string]any{"login": "github-actions[bot]"}},
			},
			wantIsBot: true,
		},
		{
			name: "latest close by human",
			events: []map[string]any{
				{"event": "closed", "actor": map[string]any{"login": "github-actions[bot]"}},
				{"event": "reopened", "actor": map[string]any{"login": "octocat"}},
				{"event": "closed", "actor": map[string]any{"login": "octocat"}},
			},
			wantIsBot: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			getEvents := func(_ context.Context, endpoint, repo string) ([]map[string]any, error) {
				require.Equal(t, "issues/42/events", endpoint)
				require.Equal(t, "owner/repo", repo)
				return tc.events, nil
			}

			closedByBot, err := isLatestCloseByBot(context.Background(), 42, "owner/repo", getEvents)
			require.NoError(t, err)
			assert.Equal(t, tc.wantIsBot, closedByBot, "should use the most recent close event")
		})
	}
}

func TestIsLatestCloseByBotRequiresCloseEvent(t *testing.T) {
	getEvents := func(_ context.Context, endpoint, repo string) ([]map[string]any, error) {
		return []map[string]any{{"event": "reopened"}}, nil
	}

	closedByBot, err := isLatestCloseByBot(context.Background(), 42, "owner/repo", getEvents)
	require.Error(t, err)
	assert.False(t, closedByBot)
}

func TestExtractCommentID(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		expected string
	}{
		{"issuecomment", "https://github.com/owner/repo/issues/123#issuecomment-456789", "456789"},
		{"comments path", "https://github.com/owner/repo/issues/comments/789012", "789012"},
		{"no comment", "https://github.com/owner/repo/issues/123", ""},
		{"empty", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, extractCommentID(tt.url), "extracted comment ID")
		})
	}
}

func TestResolveItemRepo(t *testing.T) {
	item := CreatedItemReport{Repo: "explicit/repo"}
	assert.Equal(t, "explicit/repo", resolveItemRepo(item, "fallback/repo"), "prefers item repo")

	item2 := CreatedItemReport{URL: "https://github.com/url/repo/pull/1"}
	assert.Equal(t, "url/repo", resolveItemRepo(item2, "fallback/repo"), "falls back to URL repo")

	item3 := CreatedItemReport{}
	assert.Equal(t, "fallback/repo", resolveItemRepo(item3, "fallback/repo"), "falls back to override")
}

func TestResolveItemNumber(t *testing.T) {
	item := CreatedItemReport{Number: 42}
	assert.Equal(t, 42, resolveItemNumber(item), "prefers item number")

	item2 := CreatedItemReport{URL: "https://github.com/owner/repo/pull/99"}
	assert.Equal(t, 99, resolveItemNumber(item2), "falls back to URL number")

	item3 := CreatedItemReport{}
	assert.Equal(t, 0, resolveItemNumber(item3), "returns 0 when no number")
}

func TestMedianFloat(t *testing.T) {
	assert.InDelta(t, 0.0, medianFloat(nil), 1e-12, "empty slice")
	assert.InDelta(t, 5.0, medianFloat([]float64{5.0}), 1e-12, "single element")
	assert.InDelta(t, 3.0, medianFloat([]float64{1.0, 3.0, 5.0}), 1e-12, "odd count")
	assert.InDelta(t, 2.5, medianFloat([]float64{1.0, 2.0, 3.0, 4.0}), 1e-12, "even count")
	assert.InDelta(t, 3.0, medianFloat([]float64{5.0, 1.0, 3.0}), 1e-12, "unsorted")
}

func TestLabelsToStringsUseSharedConversion(t *testing.T) {
	assert.Equal(t, []string{"bug", "feature"}, labelsToStringsFromNodes([]any{
		map[string]any{"name": "bug"},
		map[string]any{"name": "feature"},
		map[string]any{"description": "missing name"},
	}))
	assert.Equal(t, []string{"bug", "feature"}, labelsToStringsFromMaps([]map[string]any{
		{"name": "bug"},
		{"name": "feature"},
		{"description": "missing name"},
	}))
}

func TestTimeBetween(t *testing.T) {
	hours := timeBetween("2026-05-12T00:00:00Z", "2026-05-12T02:30:00Z")
	assert.InDelta(t, 2.5, hours, 0.01, "2.5 hours between timestamps")

	assert.InDelta(t, 0.0, timeBetween("bad", "2026-05-12T00:00:00Z"), 1e-12, "bad from timestamp")
	assert.InDelta(t, 0.0, timeBetween("2026-05-12T00:00:00Z", "bad"), 1e-12, "bad to timestamp")
}

func TestEvaluateOutcomesSkipsNoopAndMetadata(t *testing.T) {
	items := []CreatedItemReport{
		{Type: "noop", Timestamp: "2026-05-12T00:00:00Z"},
		{Type: "missing_tool", Timestamp: "2026-05-12T00:00:00Z"},
		{Type: "missing_data", Timestamp: "2026-05-12T00:00:00Z"},
		{Type: "report_incomplete", Timestamp: "2026-05-12T00:00:00Z"},
	}

	reports := EvaluateOutcomes(context.Background(), items, "owner/repo", github.DefaultObjectiveMapping())
	assert.Empty(t, reports, "noop and metadata types should be skipped")
}

func TestEvaluateOutcomesErrorOnMissingData(t *testing.T) {
	items := []CreatedItemReport{
		{Type: "create_pull_request", Timestamp: "2026-05-12T00:00:00Z"},
	}

	reports := EvaluateOutcomes(context.Background(), items, "", github.DefaultObjectiveMapping())
	assert.Len(t, reports, 1, "should produce one report")
	assert.Equal(t, OutcomeStatusUnknown, reports[0].OutcomeStatus, "missing reference is unknown, not an API failure")
}

func TestEnrichOutcomeWithObjectiveValue_TracesPullRequestToRootIssue(t *testing.T) {
	oldGraphQL := objectiveMappingGHAPIGraphQL
	oldGetArray := objectiveMappingGHAPIGetArray
	t.Cleanup(func() {
		objectiveMappingGHAPIGraphQL = oldGraphQL
		objectiveMappingGHAPIGetArray = oldGetArray
	})

	var capturedQuery string
	var capturedVariables map[string]any
	objectiveMappingGHAPIGraphQL = func(_ context.Context, query string, variables map[string]any, repo string) (map[string]any, error) {
		capturedQuery = query
		capturedVariables = variables
		return map[string]any{
			"data": map[string]any{
				"repository": map[string]any{
					"pullRequest": map[string]any{
						"id": "PR_kwDOAAABCD4",
						"closingIssuesReferences": map[string]any{
							"nodes": []any{
								map[string]any{
									"id":     "I_kwDOAAABCQ4",
									"number": float64(1234),
									"url":    "https://github.com/owner/repo/issues/1234",
									"labels": map[string]any{"nodes": []any{
										map[string]any{"name": "agentic-campaign"},
										map[string]any{"name": "security"},
									}},
								},
							},
						},
					},
				},
			},
		}, nil
	}
	objectiveMappingGHAPIGetArray = func(_ context.Context, endpoint string, repo string) ([]map[string]any, error) {
		return nil, fmt.Errorf("unexpected fallback label fetch: %s", endpoint)
	}

	report := OutcomeReport{Type: "create_pull_request", ObjectURL: "https://github.com/owner/repo/pull/77", ObjectNumber: 77}
	mapping := &github.ObjectiveMapping{
		LabelToValue:    map[string]int{"agentic-campaign": 90, "security": 85},
		MultiLabelLogic: "max",
		PriorityLabels:  []string{"agentic-campaign", "security"},
	}

	enrichOutcomeWithObjectiveValue(context.Background(), &report, "owner/repo", mapping)

	assert.Equal(t, 90, report.ObjectiveValue)
	assert.Equal(t, []string{"agentic-campaign", "security"}, report.ObjectiveLabels)
	assert.Equal(t, "https://github.com/owner/repo/issues/1234", report.TracedRootURL)
	assert.Equal(t, "mapped", report.AttributionStatus)
	assert.Equal(t, "closing_issue", report.AttributionSource)

	assert.NotContains(t, capturedQuery, "owner/repo", "query should not interpolate values into the GraphQL document")
	assert.NotContains(t, capturedQuery, `"owner"`, "query should not contain the owner value quoted as a literal")
	assert.NotContains(t, capturedQuery, `"repo"`, "query should not contain the repo value quoted as a literal")
	assert.NotContains(t, capturedQuery, "77", "query should not contain the object number interpolated as a literal")
	assert.Contains(t, capturedQuery, "query($owner: String!, $name: String!, $number: Int!)", "query should declare GraphQL variables")
	assert.Equal(t, map[string]any{"owner": "owner", "name": "repo", "number": 77}, capturedVariables, "values should be passed as GraphQL variables")
}

func TestBuildGraphQLArgs(t *testing.T) {
	t.Run("strings use -f and are never rewritten into -F", func(t *testing.T) {
		args, err := buildGraphQLArgs("query($owner: String!) { x }", map[string]any{
			"owner": `@file/etc/passwd`,
		})
		require.NoError(t, err)
		assert.Equal(t, []string{
			"api", "graphql",
			"-f", "query=query($owner: String!) { x }",
			"-f", "owner=@file/etc/passwd",
		}, args)
	})

	t.Run("strings with placeholder syntax stay literal via -f", func(t *testing.T) {
		args, err := buildGraphQLArgs("query { x }", map[string]any{
			"name": "{repo}",
		})
		require.NoError(t, err)
		assert.Contains(t, args, "-f")
		nameIdx := slices.Index(args, "name={repo}")
		require.GreaterOrEqual(t, nameIdx, 0, "name value should be passed literally")
		assert.Equal(t, "-f", args[nameIdx-1], "string variables must use -f, not -F")
	})

	t.Run("ints use -F for correct GraphQL typing", func(t *testing.T) {
		args, err := buildGraphQLArgs("query { x }", map[string]any{
			"number": 42,
		})
		require.NoError(t, err)
		assert.Equal(t, []string{
			"api", "graphql",
			"-f", "query=query { x }",
			"-F", "number=42",
		}, args)
	})

	t.Run("multiple variables are emitted in sorted key order deterministically", func(t *testing.T) {
		variables := map[string]any{
			"zebra": "z",
			"apple": "a",
			"mango": 3,
		}
		for range 10 {
			args, err := buildGraphQLArgs("query { x }", variables)
			require.NoError(t, err)
			assert.Equal(t, []string{
				"api", "graphql",
				"-f", "query=query { x }",
				"-f", "apple=a",
				"-F", "mango=3",
				"-f", "zebra=z",
			}, args)
		}
	})

	t.Run("unsupported variable types are rejected explicitly", func(t *testing.T) {
		_, err := buildGraphQLArgs("query { x }", map[string]any{
			"bad": 3.14,
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "bad")
		assert.Contains(t, err.Error(), "float64")
	})

	t.Run("no variables produces only the query argument", func(t *testing.T) {
		args, err := buildGraphQLArgs("query { x }", nil)
		require.NoError(t, err)
		assert.Equal(t, []string{"api", "graphql", "-f", "query=query { x }"}, args)
	})
}

func TestEnrichOutcomeWithObjectiveValue_FallsBackToDirectLabels(t *testing.T) {
	oldGraphQL := objectiveMappingGHAPIGraphQL
	oldGetArray := objectiveMappingGHAPIGetArray
	t.Cleanup(func() {
		objectiveMappingGHAPIGraphQL = oldGraphQL
		objectiveMappingGHAPIGetArray = oldGetArray
	})

	objectiveMappingGHAPIGraphQL = func(_ context.Context, query string, _ map[string]any, repo string) (map[string]any, error) {
		return nil, errors.New("no linked issues")
	}
	objectiveMappingGHAPIGetArray = func(_ context.Context, endpoint string, repo string) ([]map[string]any, error) {
		return []map[string]any{{"name": "automation"}, {"name": "testing"}}, nil
	}

	report := OutcomeReport{Type: "create_issue", ObjectURL: "https://github.com/owner/repo/issues/42", ObjectNumber: 42}
	mapping := &github.ObjectiveMapping{LabelToValue: map[string]int{"automation": 70, "testing": 65}, MultiLabelLogic: "max"}

	enrichOutcomeWithObjectiveValue(context.Background(), &report, "owner/repo", mapping)

	assert.Equal(t, 70, report.ObjectiveValue)
	assert.Equal(t, []string{"automation", "testing"}, report.ObjectiveLabels)
	assert.Equal(t, "https://github.com/owner/repo/issues/42", report.TracedRootURL)
	assert.Equal(t, "mapped", report.AttributionStatus)
	assert.Equal(t, "issue_labels", report.AttributionSource)
}

func TestEnrichOutcomeWithObjectiveValue_MultipleClosingIssuesRemainAmbiguous(t *testing.T) {
	oldGraphQL := objectiveMappingGHAPIGraphQL
	oldGetArray := objectiveMappingGHAPIGetArray
	t.Cleanup(func() {
		objectiveMappingGHAPIGraphQL = oldGraphQL
		objectiveMappingGHAPIGetArray = oldGetArray
	})

	objectiveMappingGHAPIGraphQL = func(_ context.Context, query string, _ map[string]any, repo string) (map[string]any, error) {
		return map[string]any{
			"data": map[string]any{
				"repository": map[string]any{
					"pullRequest": map[string]any{
						"id": "PR_kwDOAAABCD4",
						"closingIssuesReferences": map[string]any{
							"nodes": []any{
								map[string]any{
									"id":  "I_kwDOAAABCQ4",
									"url": "https://github.com/owner/repo/issues/1234",
									"labels": map[string]any{"nodes": []any{
										map[string]any{"name": "agentic-campaign"},
									}},
								},
								map[string]any{
									"id":  "I_kwDOAAABCR4",
									"url": "https://github.com/owner/repo/issues/1235",
									"labels": map[string]any{"nodes": []any{
										map[string]any{"name": "security"},
									}},
								},
							},
						},
					},
				},
			},
		}, nil
	}
	objectiveMappingGHAPIGetArray = func(_ context.Context, endpoint string, repo string) ([]map[string]any, error) {
		return []map[string]any{{"name": "automation"}}, nil
	}

	report := OutcomeReport{Type: "create_pull_request", ObjectURL: "https://github.com/owner/repo/pull/77", ObjectNumber: 77}
	mapping := &github.ObjectiveMapping{
		LabelToValue:    map[string]int{"agentic-campaign": 90, "security": 85, "automation": 70},
		MultiLabelLogic: "max",
	}

	enrichOutcomeWithObjectiveValue(context.Background(), &report, "owner/repo", mapping)

	assert.Equal(t, "ambiguous", report.AttributionStatus)
	assert.Equal(t, "closing_issue", report.AttributionSource)
	assert.Empty(t, report.TracedRootURL)
	assert.Zero(t, report.ObjectiveValue)
	assert.Empty(t, report.ObjectiveLabels)
}

func TestNormalizeOutcomeEvaluationTargetExistsOnly(t *testing.T) {
	report := OutcomeReport{
		Type:              "add_labels",
		OutcomeEvaluation: OutcomeEvaluation{OutcomeStatus: OutcomeStatusUnknown},
		Detail:            "object still exists",
	}

	eval := normalizeOutcomeEvaluation(report)
	assert.Equal(t, OutcomeStatusUnknown, eval.OutcomeStatus)
	assert.Equal(t, EvidenceWeak, eval.EvidenceStrength)
	assert.Equal(t, "target_exists_only", eval.Signal)
}

func TestEvalGenericStickyTargetExistsOnlyFallback(t *testing.T) {
	old := genericOutcomeGHAPIGet
	t.Cleanup(func() {
		genericOutcomeGHAPIGet = old
	})
	genericOutcomeGHAPIGet = func(_ context.Context, endpoint string, repo string) (map[string]any, error) {
		return map[string]any{"state": "open"}, nil
	}

	report := evalGenericSticky(context.Background(),
		CreatedItemReport{Type: "add_labels", Number: 42, Repo: "owner/repo"},
		"owner/repo",
	)

	assert.Equal(t, OutcomeStatusUnknown, report.OutcomeStatus)
	assert.Equal(t, EvidenceWeak, report.EvidenceStrength)
	assert.Equal(t, "target_exists_only", report.Signal)
}

func TestOutcomeSummaryExcludesExistsOnlyFromAccepted(t *testing.T) {
	reports := []OutcomeReport{
		{
			Type: "add_labels",
			OutcomeEvaluation: OutcomeEvaluation{
				OutcomeStatus:    OutcomeStatusUnknown,
				EvidenceStrength: EvidenceWeak,
				Signal:           "target_exists_only",
			},
		},
		{
			Type: "create_pull_request",
			OutcomeEvaluation: OutcomeEvaluation{
				OutcomeStatus:    OutcomeStatusAccepted,
				EvidenceStrength: EvidenceStrong,
				Signal:           "merged",
			},
		},
	}

	s := ComputeOutcomeSummary(reports, github.DefaultObjectiveMapping())
	assert.Equal(t, 1, s.Accepted)
	assert.Equal(t, 1, s.AcceptedStrong)
	assert.Equal(t, 0, s.AcceptedWeak)
	assert.Equal(t, 1, s.FallbackExistsOnlyCount)
}

func TestOutcomeReportJSONCarriesSingleOutcomeStatus(t *testing.T) {
	report := OutcomeReport{
		Type: "dispatch_workflow",
		OutcomeEvaluation: OutcomeEvaluation{
			OutcomeStatus:    OutcomeStatusError,
			EvidenceStrength: EvidenceWeak,
			Signal:           "evaluation_error",
		},
	}

	data, err := json.Marshal(report)
	require.NoError(t, err)

	var payload map[string]any
	require.NoError(t, json.Unmarshal(data, &payload))
	assert.Equal(t, "error", payload["outcome_status"])
	assert.NotContains(t, payload, "result")
}

func TestOutcomeSummaryCountsEvalErrorsFromNormalizedStatus(t *testing.T) {
	reports := []OutcomeReport{
		{
			Type: "dispatch_workflow",
			OutcomeEvaluation: OutcomeEvaluation{
				OutcomeStatus: OutcomeStatusUnknown,
			},
			EvalError: "connection refused",
		},
	}

	s := ComputeOutcomeSummary(reports, github.DefaultObjectiveMapping())
	assert.Equal(t, 1, s.Errors)
}

func TestWriteOutcomeJSONLEmitsNormalizedFields(t *testing.T) {
	dir := t.TempDir()
	reports := []OutcomeReport{
		{
			Type: "add_labels",
			OutcomeEvaluation: OutcomeEvaluation{
				OutcomeStatus:    OutcomeStatusUnknown,
				EvidenceStrength: EvidenceWeak,
				Signal:           "target_exists_only",
			},
			CreatedAt: "2026-05-12T00:00:00Z",
			CheckedAt: "2026-05-12T01:00:00Z",
		},
	}

	writeOutcomeJSONL(dir, 123, reports)

	data, err := os.ReadFile(filepath.Join(dir, "outcomes-123.jsonl"))
	require.NoError(t, err)

	var entry map[string]any
	require.NoError(t, json.Unmarshal(bytes.TrimSpace(data), &entry))
	assert.Equal(t, "unknown", entry["outcome_status"])
	assert.Equal(t, "weak", entry["evidence_strength"])
	assert.Equal(t, "target_exists_only", entry["signal"])
	assert.NotContains(t, entry, "result")
}

func TestEvalAddReviewerAcceptedWithApproval(t *testing.T) {
	oldGet := outcomeReviewGHAPIGet
	oldGetArray := outcomeReviewGHAPIGetArray
	t.Cleanup(func() {
		outcomeReviewGHAPIGet = oldGet
		outcomeReviewGHAPIGetArray = oldGetArray
	})

	outcomeReviewGHAPIGet = func(_ context.Context, endpoint string, repo string) (map[string]any, error) {
		return map[string]any{"users": []any{}, "teams": []any{}}, nil
	}
	outcomeReviewGHAPIGetArray = func(_ context.Context, endpoint string, repo string) ([]map[string]any, error) {
		return []map[string]any{
			{
				"state":        "APPROVED",
				"submitted_at": "2026-05-12T01:00:00Z",
				"user":         map[string]any{"login": "reviewer1"},
			},
		}, nil
	}

	report := evalAddReviewer(context.Background(), CreatedItemReport{
		Type:      "add_reviewer",
		Number:    42,
		Repo:      "owner/repo",
		Timestamp: "2026-05-12T00:00:00Z",
		Metadata: map[string]any{
			"requested_reviewers": []any{"reviewer1"},
		},
	}, "owner/repo")

	assert.Equal(t, OutcomeStatusAccepted, report.OutcomeStatus)
	assert.Equal(t, EvidenceStrong, report.EvidenceStrength)
	assert.Equal(t, "review_approved", report.Signal)
}

func TestEvalAddReviewerRejectedWhenRequestRemoved(t *testing.T) {
	oldGet := outcomeReviewGHAPIGet
	oldGetArray := outcomeReviewGHAPIGetArray
	t.Cleanup(func() {
		outcomeReviewGHAPIGet = oldGet
		outcomeReviewGHAPIGetArray = oldGetArray
	})

	outcomeReviewGHAPIGet = func(_ context.Context, endpoint string, repo string) (map[string]any, error) {
		return map[string]any{"users": []any{}, "teams": []any{}}, nil
	}
	outcomeReviewGHAPIGetArray = func(_ context.Context, endpoint string, repo string) ([]map[string]any, error) {
		return []map[string]any{}, nil
	}

	report := evalAddReviewer(context.Background(), CreatedItemReport{
		Type:      "add_reviewer",
		Number:    42,
		Repo:      "owner/repo",
		Timestamp: "2026-05-12T00:00:00Z",
		Metadata: map[string]any{
			"requested_reviewers": []any{"reviewer1"},
		},
	}, "owner/repo")

	assert.Equal(t, OutcomeStatusRejected, report.OutcomeStatus)
	assert.Equal(t, EvidenceStrong, report.EvidenceStrength)
	assert.Equal(t, "review_request_removed", report.Signal)
}

func TestEvalSubmitPullRequestReviewDismissed(t *testing.T) {
	oldGet := outcomeReviewGHAPIGet
	oldGetArray := outcomeReviewGHAPIGetArray
	t.Cleanup(func() {
		outcomeReviewGHAPIGet = oldGet
		outcomeReviewGHAPIGetArray = oldGetArray
	})

	outcomeReviewGHAPIGet = func(_ context.Context, endpoint string, repo string) (map[string]any, error) {
		return map[string]any{"state": "open", "merged": false}, nil
	}
	outcomeReviewGHAPIGetArray = func(_ context.Context, endpoint string, repo string) ([]map[string]any, error) {
		return []map[string]any{
			{"id": float64(101), "state": "DISMISSED", "submitted_at": "2026-05-12T01:00:00Z"},
		}, nil
	}

	report := evalSubmitPullRequestReview(context.Background(), CreatedItemReport{
		Type:      "submit_pull_request_review",
		URL:       "https://github.com/owner/repo/pull/42#pullrequestreview-101",
		Number:    42,
		Repo:      "owner/repo",
		Timestamp: "2026-05-12T01:00:00Z",
		Metadata:  map[string]any{"review_id": float64(101)},
	}, "owner/repo")

	assert.Equal(t, OutcomeStatusRejected, report.OutcomeStatus)
	assert.Equal(t, EvidenceStrong, report.EvidenceStrength)
	assert.Equal(t, "review_dismissed", report.Signal)
}

func TestEvalSubmitPullRequestReviewChangesRequestedMergedAfterPush(t *testing.T) {
	oldGet := outcomeReviewGHAPIGet
	oldGetArray := outcomeReviewGHAPIGetArray
	t.Cleanup(func() {
		outcomeReviewGHAPIGet = oldGet
		outcomeReviewGHAPIGetArray = oldGetArray
	})

	outcomeReviewGHAPIGet = func(_ context.Context, endpoint string, repo string) (map[string]any, error) {
		return map[string]any{
			"state":     "closed",
			"merged":    true,
			"merged_at": "2026-05-12T05:00:00Z",
		}, nil
	}
	outcomeReviewGHAPIGetArray = func(_ context.Context, endpoint string, repo string) ([]map[string]any, error) {
		switch endpoint {
		case "pulls/42/reviews":
			return []map[string]any{
				{"id": float64(101), "state": "CHANGES_REQUESTED", "submitted_at": "2026-05-12T02:00:00Z"},
			}, nil
		case "pulls/42/commits":
			return []map[string]any{
				{"commit": map[string]any{"committer": map[string]any{"date": "2026-05-12T03:00:00Z"}}},
			}, nil
		default:
			return []map[string]any{}, nil
		}
	}

	report := evalSubmitPullRequestReview(context.Background(), CreatedItemReport{
		Type:      "submit_pull_request_review",
		URL:       "https://github.com/owner/repo/pull/42#pullrequestreview-101",
		Number:    42,
		Repo:      "owner/repo",
		Timestamp: "2026-05-12T02:00:00Z",
		Metadata:  map[string]any{"review_id": float64(101)},
	}, "owner/repo")

	assert.Equal(t, OutcomeStatusAccepted, report.OutcomeStatus)
	assert.Equal(t, EvidenceMedium, report.EvidenceStrength)
	assert.Equal(t, "changes_requested_addressed", report.Signal)
}

func TestEvalSubmitPullRequestReviewPendingWhenLatestOnOpenPR(t *testing.T) {
	oldGet := outcomeReviewGHAPIGet
	oldGetArray := outcomeReviewGHAPIGetArray
	t.Cleanup(func() {
		outcomeReviewGHAPIGet = oldGet
		outcomeReviewGHAPIGetArray = oldGetArray
	})

	outcomeReviewGHAPIGet = func(_ context.Context, endpoint string, repo string) (map[string]any, error) {
		return map[string]any{"state": "open", "merged": false}, nil
	}
	outcomeReviewGHAPIGetArray = func(_ context.Context, endpoint string, repo string) ([]map[string]any, error) {
		return []map[string]any{
			{"id": float64(100), "state": "COMMENTED", "submitted_at": "2026-05-12T00:30:00Z"},
			{"id": float64(101), "state": "COMMENTED", "submitted_at": "2026-05-12T01:00:00Z"},
		}, nil
	}

	report := evalSubmitPullRequestReview(context.Background(), CreatedItemReport{
		Type:      "submit_pull_request_review",
		URL:       "https://github.com/owner/repo/pull/42#pullrequestreview-101",
		Number:    42,
		Repo:      "owner/repo",
		Timestamp: "2026-05-12T01:00:00Z",
		Metadata:  map[string]any{"review_id": float64(101)},
	}, "owner/repo")

	assert.Equal(t, OutcomeStatusPending, report.OutcomeStatus)
	assert.Equal(t, EvidenceMedium, report.EvidenceStrength)
	assert.Equal(t, "latest_review_pending", report.Signal)
}

func TestEvalAddReviewerPendingWhenRequestStillOutstanding(t *testing.T) {
	oldGet := outcomeReviewGHAPIGet
	oldGetArray := outcomeReviewGHAPIGetArray
	t.Cleanup(func() {
		outcomeReviewGHAPIGet = oldGet
		outcomeReviewGHAPIGetArray = oldGetArray
	})

	outcomeReviewGHAPIGet = func(_ context.Context, endpoint string, repo string) (map[string]any, error) {
		return map[string]any{
			"users": []any{map[string]any{"login": "reviewer1"}},
			"teams": []any{},
		}, nil
	}
	outcomeReviewGHAPIGetArray = func(_ context.Context, endpoint string, repo string) ([]map[string]any, error) {
		return []map[string]any{}, nil
	}

	report := evalAddReviewer(context.Background(), CreatedItemReport{
		Type:      "add_reviewer",
		Number:    42,
		Repo:      "owner/repo",
		Timestamp: "2026-05-12T00:00:00Z",
		Metadata: map[string]any{
			"requested_reviewers": []any{"reviewer1"},
		},
	}, "owner/repo")

	assert.Equal(t, OutcomeStatusPending, report.OutcomeStatus)
	assert.Equal(t, EvidenceMedium, report.EvidenceStrength)
	assert.Equal(t, "awaiting_review", report.Signal)
}

func TestEvalAddReviewerUsesLatestReviewerState(t *testing.T) {
	oldGet := outcomeReviewGHAPIGet
	oldGetArray := outcomeReviewGHAPIGetArray
	t.Cleanup(func() {
		outcomeReviewGHAPIGet = oldGet
		outcomeReviewGHAPIGetArray = oldGetArray
	})

	outcomeReviewGHAPIGet = func(_ context.Context, endpoint string, repo string) (map[string]any, error) {
		return map[string]any{"users": []any{}, "teams": []any{}}, nil
	}
	outcomeReviewGHAPIGetArray = func(_ context.Context, endpoint string, repo string) ([]map[string]any, error) {
		return []map[string]any{
			{"state": "APPROVED", "submitted_at": "2026-05-12T01:00:00Z", "user": map[string]any{"login": "reviewer1"}},
			{"state": "CHANGES_REQUESTED", "submitted_at": "2026-05-12T02:00:00Z", "user": map[string]any{"login": "reviewer1"}},
		}, nil
	}

	report := evalAddReviewer(context.Background(), CreatedItemReport{
		Type:      "add_reviewer",
		Number:    42,
		Repo:      "owner/repo",
		Timestamp: "2026-05-12T00:00:00Z",
		Metadata: map[string]any{
			"requested_reviewers": []any{"reviewer1"},
		},
	}, "owner/repo")

	assert.Equal(t, OutcomeStatusAccepted, report.OutcomeStatus)
	assert.Equal(t, EvidenceMedium, report.EvidenceStrength)
	assert.Equal(t, "review_submitted", report.Signal)
}

func TestTimestampOnOrAfterMalformedReturnsFalse(t *testing.T) {
	assert.False(t, timestampOnOrAfter("invalid", "2026-05-12T00:00:00Z"))
	assert.False(t, timestampOnOrAfter("2026-05-12T00:00:00Z", "invalid"))
}

func TestTimestampOnOrAfterEmptyCandidateAndThresholdHandling(t *testing.T) {
	assert.False(t, timestampOnOrAfter("", "2026-05-12T00:00:00Z"))
	assert.True(t, timestampOnOrAfter("2026-05-12T00:00:00Z", ""))
}

func TestEvalSubmitPullRequestReviewChangesRequestedMissingCommitDatesStaysUnknown(t *testing.T) {
	oldGet := outcomeReviewGHAPIGet
	oldGetArray := outcomeReviewGHAPIGetArray
	t.Cleanup(func() {
		outcomeReviewGHAPIGet = oldGet
		outcomeReviewGHAPIGetArray = oldGetArray
	})

	outcomeReviewGHAPIGet = func(_ context.Context, endpoint string, repo string) (map[string]any, error) {
		return map[string]any{
			"state":     "closed",
			"merged":    true,
			"merged_at": "2026-05-12T05:00:00Z",
		}, nil
	}
	outcomeReviewGHAPIGetArray = func(_ context.Context, endpoint string, repo string) ([]map[string]any, error) {
		switch endpoint {
		case "pulls/42/reviews":
			return []map[string]any{
				{"id": float64(101), "state": "CHANGES_REQUESTED", "submitted_at": "2026-05-12T02:00:00Z"},
			}, nil
		case "pulls/42/commits":
			return []map[string]any{
				{"commit": map[string]any{"committer": map[string]any{"date": ""}, "author": map[string]any{"date": ""}}},
			}, nil
		default:
			return []map[string]any{}, nil
		}
	}

	report := evalSubmitPullRequestReview(context.Background(), CreatedItemReport{
		Type:      "submit_pull_request_review",
		URL:       "https://github.com/owner/repo/pull/42#pullrequestreview-101",
		Number:    42,
		Repo:      "owner/repo",
		Timestamp: "2026-05-12T02:00:00Z",
		Metadata:  map[string]any{"review_id": float64(101)},
	}, "owner/repo")

	assert.Equal(t, OutcomeStatusUnknown, report.OutcomeStatus)
	assert.Equal(t, EvidenceWeak, report.EvidenceStrength)
	assert.Equal(t, "unknown", report.Signal)
}

func TestEvalSubmitPullRequestReviewApprovedMergedUsesSharedSignal(t *testing.T) {
	oldGet := outcomeReviewGHAPIGet
	oldGetArray := outcomeReviewGHAPIGetArray
	t.Cleanup(func() {
		outcomeReviewGHAPIGet = oldGet
		outcomeReviewGHAPIGetArray = oldGetArray
	})

	outcomeReviewGHAPIGet = func(_ context.Context, endpoint string, repo string) (map[string]any, error) {
		return map[string]any{
			"state":     "closed",
			"merged":    true,
			"merged_at": "2026-05-12T05:00:00Z",
		}, nil
	}
	outcomeReviewGHAPIGetArray = func(_ context.Context, endpoint string, repo string) ([]map[string]any, error) {
		return []map[string]any{
			{"id": float64(101), "state": "APPROVED", "submitted_at": "2026-05-12T02:00:00Z"},
		}, nil
	}

	report := evalSubmitPullRequestReview(context.Background(), CreatedItemReport{
		Type:      "submit_pull_request_review",
		URL:       "https://github.com/owner/repo/pull/42#pullrequestreview-101",
		Number:    42,
		Repo:      "owner/repo",
		Timestamp: "2026-05-12T02:00:00Z",
		Metadata:  map[string]any{"review_id": float64(101)},
	}, "owner/repo")

	assert.Equal(t, OutcomeStatusAccepted, report.OutcomeStatus)
	assert.Equal(t, EvidenceStrong, report.EvidenceStrength)
	assert.Equal(t, "review_approved", report.Signal)
}

func TestEvalSubmitPullRequestReviewPendingIgnoresUnsubmittedDrafts(t *testing.T) {
	oldGet := outcomeReviewGHAPIGet
	oldGetArray := outcomeReviewGHAPIGetArray
	t.Cleanup(func() {
		outcomeReviewGHAPIGet = oldGet
		outcomeReviewGHAPIGetArray = oldGetArray
	})

	outcomeReviewGHAPIGet = func(_ context.Context, endpoint string, repo string) (map[string]any, error) {
		return map[string]any{"state": "open", "merged": false}, nil
	}
	outcomeReviewGHAPIGetArray = func(_ context.Context, endpoint string, repo string) ([]map[string]any, error) {
		return []map[string]any{
			{"id": float64(101), "state": "COMMENTED", "submitted_at": "2026-05-12T01:00:00Z"},
			{"id": float64(102), "state": "PENDING", "submitted_at": ""},
		}, nil
	}

	report := evalSubmitPullRequestReview(context.Background(), CreatedItemReport{
		Type:      "submit_pull_request_review",
		URL:       "https://github.com/owner/repo/pull/42#pullrequestreview-101",
		Number:    42,
		Repo:      "owner/repo",
		Timestamp: "2026-05-12T01:00:00Z",
		Metadata:  map[string]any{"review_id": float64(101)},
	}, "owner/repo")

	assert.Equal(t, OutcomeStatusPending, report.OutcomeStatus)
	assert.Equal(t, EvidenceMedium, report.EvidenceStrength)
	assert.Equal(t, "latest_review_pending", report.Signal)
}
