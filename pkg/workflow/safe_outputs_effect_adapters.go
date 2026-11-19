package workflow

import (
	"fmt"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/stringutil"
)

// safeOutputEffectAdapters mirrors the runtime-owned registry in
// actions/setup/js/safe_output_effect_plan.cjs. Only these native handlers, on
// their native manager and per-Claim routes, request resolved_effect binding.
var safeOutputEffectAdapters = map[string]string{
	"add_labels":                  "gh-aw.add-labels@1",
	"create_issue":                "gh-aw.create-issue@1",
	"create_pull_request":         "gh-aw.create-pull-request@1",
	"merge_pull_request":          "gh-aw.merge-pull-request@1",
	"push_to_pull_request_branch": "gh-aw.push-to-pull-request-branch@1",
	"remove_labels":               "gh-aw.remove-labels@1",
	"replace_label":               "gh-aw.replace-label@1",
	"update_issue":                "gh-aw.update-issue@1",
}

// safeOutputEffectAdapter returns the trusted adapter contract for a route, or
// "" when the route stays invocation-bound. Custom scripts, actions and jobs
// with the same name, generated phases and prepared Claim adapters never inherit
// a native adapter.
func safeOutputEffectAdapter(data *WorkflowData, name, executor string) string {
	adapter := safeOutputEffectAdapters[name]
	if adapter == "" || executor != "manager" {
		return ""
	}
	if slices.ContainsFunc(workQueuePreparedAdapterNames(data), func(prepared string) bool {
		return stringutil.NormalizeSafeOutputIdentifier(prepared) == name
	}) {
		return ""
	}
	return adapter
}

func templatableEnabled(value *string) bool {
	return value != nil && *value != "" && *value != "false" && *value != "0"
}

// validateEffectAdapterConfigurations rejects trusted configuration whose
// effects an exact-effect adapter cannot enumerate before authorization. Such
// outputs fail compilation rather than silently falling back to invocation
// binding or dropping configured behavior.
func validateEffectAdapterConfigurations(config *SafeOutputsConfig) error {
	if config == nil {
		return nil
	}
	protected := func(name string) bool {
		return config.AuthorizationRequirements[name] != nil && config.AuthorizationCoverage[name] != "staged"
	}
	var unsupported []string
	if c := config.CreateIssues; c != nil && protected("create_issue") {
		if templatableEnabled(c.Enabled) {
			unsupported = append(unsupported, "create-issue.close-older-issues")
		}
		if templatableEnabled(c.Group) {
			unsupported = append(unsupported, "create-issue.group")
		}
		if templatableEnabled(c.GroupByDay) {
			unsupported = append(unsupported, "create-issue.group-by-day")
		}
	}
	if c := config.AddLabels; c != nil && protected("add_labels") && c.CreateIfMissing != nil && *c.CreateIfMissing {
		unsupported = append(unsupported, "add-labels.create-if-missing")
	}
	if c := config.CreatePullRequests; c != nil && protected("create_pull_request") {
		if isCloseOlderPullRequestsEnabled(c) {
			unsupported = append(unsupported, "create-pull-request.close-older-pull-requests")
		}
		if c.FallbackAsIssue != nil && *c.FallbackAsIssue {
			unsupported = append(unsupported, "create-pull-request.fallback-as-issue")
		}
		if c.ManifestFilesPolicy != nil && normaliseProtectedFilesPolicy(*c.ManifestFilesPolicy) == "fallback-to-issue" {
			unsupported = append(unsupported, "create-pull-request.protected-files: fallback-to-issue")
		}
	}
	if c := config.PushToPullRequestBranch; c != nil && protected("push_to_pull_request_branch") {
		if c.FallbackAsPullRequest != nil && *c.FallbackAsPullRequest {
			unsupported = append(unsupported, "push-to-pull-request-branch.fallback-as-pull-request")
		}
		if c.ManifestFilesPolicy != nil && normaliseProtectedFilesPolicy(*c.ManifestFilesPolicy) == "fallback-to-issue" {
			unsupported = append(unsupported, "push-to-pull-request-branch.protected-files: fallback-to-issue")
		}
	}
	if len(unsupported) > 0 {
		return fmt.Errorf("safe-output authorization binds exact effects, which cannot include %s; remove these settings or the output's authorization requirement", strings.Join(unsupported, ", "))
	}
	return nil
}
