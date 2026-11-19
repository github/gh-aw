package workflow

import (
	"slices"

	"github.com/github/gh-aw/pkg/stringutil"
)

// safeOutputEffectAdapters mirrors the runtime-owned registry in
// actions/setup/js/safe_output_effect_plan.cjs. Only these native handlers, on
// their native manager and per-Claim routes, request resolved_effect binding.
var safeOutputEffectAdapters = map[string]string{
	"merge_pull_request": "gh-aw.merge-pull-request@1",
	"remove_labels":      "gh-aw.remove-labels@1",
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
