package workflow

import (
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/stringutil"
)

// AuthorizationRoute describes a compiler-resolved output or generated executor.
// Authority values are deliberately excluded from JSON; callers publish fingerprints.
type AuthorizationRoute struct {
	Name     string `json:"name"`
	Executor string `json:"executor"`
	Coverage string `json:"coverage"`
	Exempt   bool   `json:"exempt"`
	Staged   bool   `json:"staged"`
	// EffectAdapter names the trusted resolved-effect adapter contract
	// supported by this route; empty routes remain invocation-bound.
	EffectAdapter string `json:"effect_adapter,omitempty"`
	// ScopeBinding is the binding a live protected invocation requests:
	// resolved_effect or invocation. Empty when no authority is requested.
	ScopeBinding  string                   `json:"scope_binding,omitempty"`
	Authorization *SafeOutputAuthorization `json:"-"`
}

// InspectEffectiveAuthorization reads the existing resolution and coverage model.
// It never supplies defaults or changes the candidate's authorization requirements.
func InspectEffectiveAuthorization(data *WorkflowData) []AuthorizationRoute {
	routes := []AuthorizationRoute{}
	if data.SafeOutputs == nil {
		return routes
	}
	config := data.SafeOutputs
	for name, coverage := range config.AuthorizationCoverage {
		base, _, _ := strings.Cut(name, ":")
		requirement := config.AuthorizationRequirements[base]
		if requirement != nil {
			copy := *requirement
			requirement = &copy
		}
		builtin := false
		executor, staged := inspectAuthorizationCustomExecutor(config, base)
		staged = staged || templatableBoolIsTrue(config.Staged) || coverage == "staged"
		for _, handler := range safeOutputHandlers {
			if handler.ToolName == base {
				builtin = handler.Builtin
				if field, ok := safeOutputPointerFieldValue(config, handler.StructField); ok && !field.IsNil() {
					staged = staged || templatableBoolIsTrue(safeOutputHandlerStaged(field))
				}
				break
			}
		}
		if config.DispatchRepository != nil {
			for name, tool := range config.DispatchRepository.Tools {
				if stringutil.NormalizeSafeOutputIdentifier(name) == base {
					staged = staged || templatableBoolIsTrue(tool.Staged)
				}
			}
		}
		if base == commentMemoryHandlerKey && data.CommentMemoryConfig != nil {
			staged = staged || templatableBoolIsTrue(data.CommentMemoryConfig.Staged)
		}
		adapter := ""
		if base == name {
			adapter = safeOutputEffectAdapter(data, name, executor)
		}
		binding := ""
		if requirement != nil && !staged {
			binding = "invocation"
			if adapter != "" {
				binding = "resolved_effect"
			}
		}
		routes = append(routes, AuthorizationRoute{
			Name: name, Executor: executor, Coverage: coverage, Authorization: requirement,
			Exempt: builtin && requirement == nil, Staged: staged, EffectAdapter: adapter, ScopeBinding: binding,
		})
	}
	slices.SortFunc(routes, func(a, b AuthorizationRoute) int { return strings.Compare(a.Name, b.Name) })
	return routes
}

func inspectAuthorizationCustomExecutor(config *SafeOutputsConfig, base string) (executor string, staged bool) {
	executor = safeOutputAuthorizationExecutor(base)
	for name, script := range config.Scripts {
		if stringutil.NormalizeSafeOutputIdentifier(name) == base {
			executor = "script"
			staged = staged || templatableBoolIsTrue(safeScriptAuthorizationStaged(script))
		}
	}
	for name, action := range config.Actions {
		if stringutil.NormalizeSafeOutputIdentifier(name) == base {
			executor = "action"
			staged = staged || templatableBoolIsTrue(action.Staged)
		}
	}
	for name, job := range config.Jobs {
		if stringutil.NormalizeSafeOutputIdentifier(name) == base {
			executor = "job"
			staged = staged || templatableBoolIsTrue(job.Staged)
		}
	}
	return executor, staged
}
