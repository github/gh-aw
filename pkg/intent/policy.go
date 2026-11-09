package intent

import (
	"slices"

	"github.com/github/gh-aw/pkg/logger"
)

var policyLog = logger.New("intent:policy")

// autonomyRank maps autonomy levels to a restriction rank (higher = more restrictive).
// propose_only is the most restrictive (agents may only propose changes, not execute);
// supervised allows execution with required approval; bounded allows the most autonomy.
// Values outside this map are treated as rank 0 (unknown/least restrictive), so any
// known value wins over an unknown one.
var autonomyRank = map[string]int{
	"bounded":      1,
	"supervised":   2,
	"propose_only": 3,
}

// writeScopeRank maps write-scope values to a restriction rank (higher = more restrictive).
// none is the most restrictive (no writes permitted); feature_branch permits writes only
// to feature branches; any_branch permits the broadest write access.
// Values outside this map are treated as rank 0 (unknown/least restrictive).
var writeScopeRank = map[string]int{
	"any_branch":     1,
	"feature_branch": 2,
	"none":           3,
}

// ExecutionPolicy governs what an agent may do for a given intent.
//
// WARNING: PolicyCompiler is advisory only. All fields except Autonomy are
// compiled and recorded for audit but are NOT yet wired into runtime enforcement.
// Do not rely on this policy to gate actual tool calls or merge operations until
// Authorizer.AuthorizeTool is implemented and integrated into the execution path.
type ExecutionPolicy struct {
	Autonomy string `json:"autonomy"`

	// AllowedTools controls which tools the agent may call.
	// nil means unrestricted; []string{} (non-nil empty) means deny-all; non-empty
	// means restricted to the listed tools. JSON omitempty cannot preserve the
	// nil-vs-empty distinction; callers must check AllowedTools != nil at runtime.
	AllowedTools []string `json:"allowed_tools,omitempty"`
	DeniedTools  []string `json:"denied_tools,omitempty"`

	WriteScope string `json:"write_scope"`

	RequiredChecks []string `json:"required_checks,omitempty"`

	HumanApprovalRequired bool `json:"human_approval_required"`

	// AutoMergeAllowed uses a pointer so that an unset rule fragment (nil) is
	// distinguishable from an explicit denial (false). The merge logic only applies
	// the AND (more-restrictive) step when at least one side has an explicit value.
	// nil means the rule did not express a preference; false is an explicit denial;
	// true is an explicit grant.
	AutoMergeAllowed *bool `json:"auto_merge_allowed,omitempty"`

	MaxAttempts int `json:"max_attempts"`

	RuleIDs []string `json:"rule_ids,omitempty"`
}

// RepositoryContext carries repository-level context used when matching policy rules.
type RepositoryContext struct {
	Owner      string `json:"owner,omitempty"`
	Name       string `json:"name,omitempty"`
	Visibility string `json:"visibility,omitempty"` // "public" or "private"
	Org        string `json:"org,omitempty"`
}

// PolicyRule pairs a match condition with a policy fragment to apply.
type PolicyRule struct {
	ID    string          `json:"id"`
	Scope string          `json:"scope,omitempty"` // "organization", "repository", "intent", or "workflow"
	When  PolicyCondition `json:"when"`
	Set   ExecutionPolicy `json:"set"`
}

// PolicyCondition describes when a rule applies.
type PolicyCondition struct {
	Domain   string `json:"domain,omitempty"`
	Priority string `json:"priority,omitempty"`
	Risk     string `json:"risk,omitempty"`
	Org      string `json:"org,omitempty"`
}

// PolicyCompiler holds policy rules for callers that still exchange policy compiler
// configuration data.
//
// WARNING: the compiled policy is advisory only. Runtime enforcement is not yet
// wired to the orchestrator — see the intent-attribution-agent-governance spec for
// the required follow-up before treating compiled policies as a security gate.
type PolicyCompiler struct {
	Rules []PolicyRule
}

// Compile applies the compiler's rules to rec and repo and returns the resulting
// ExecutionPolicy. Indeterminate records always receive the safest policy
// regardless of configured rules (fail-closed). For other statuses the first
// matching rule seeds the accumulator directly; subsequent matching rules are
// merged with stricter-wins semantics. Autonomy and WriteScope values that are
// not recognized by the rank tables are replaced with the safest default for
// that field when seeding. If no rules match, the safest default policy is returned.
func (c PolicyCompiler) Compile(rec IntentRecord, repo RepositoryContext) ExecutionPolicy {
	policyLog.Printf("Compiling policy: status=%s rules=%d", rec.Status, len(c.Rules))
	// Only authoritative mapped attribution may select a rule. Unknown statuses,
	// including empty values, must not receive a relaxed policy from a wildcard rule.
	if rec.Status != AttributionMapped {
		policyLog.Printf("Fail-closed for non-mapped status=%s, returning safest default policy", rec.Status)
		return safestDefaultPolicy()
	}

	var accumulated ExecutionPolicy
	matched := false
	for _, rule := range c.Rules {
		if !rule.matches(rec, repo) {
			continue
		}
		if !matched {
			// Seed the accumulator with a deep copy of the first matching
			// rule's policy so that permissive values (e.g. auto_merge: true,
			// max_attempts: 5) are not silently discarded by the safest-default
			// base, and so that pointer/slice fields cannot alias rule.Set.
			accumulated = sanitizeSeedPolicy(deepCopyPolicy(rule.Set), rule.ID)
			accumulated.RuleIDs = []string{rule.ID}
			matched = true
			policyLog.Printf("First matching rule: id=%s autonomy=%s write_scope=%s", rule.ID, accumulated.Autonomy, accumulated.WriteScope)
		} else {
			accumulated = mergePolicy(accumulated, rule.Set)
			accumulated.RuleIDs = append(accumulated.RuleIDs, rule.ID)
			policyLog.Printf("Merging additional rule: id=%s", rule.ID)
		}
	}
	if !matched {
		policyLog.Print("No rules matched, returning safest default policy")
		return safestDefaultPolicy()
	}
	policyLog.Printf("Compiled policy: autonomy=%s write_scope=%s human_approval=%v matched_rules=%d", accumulated.Autonomy, accumulated.WriteScope, accumulated.HumanApprovalRequired, len(accumulated.RuleIDs))
	return accumulated
}

// sanitizeSeedPolicy validates the Autonomy and WriteScope values of the policy
// that seeds the accumulator in Compile. Because the seed is copied verbatim (it
// is not merged through the rank tables), an unrecognized value would otherwise
// be carried into the compiled policy untouched and rank as 0 (least restrictive)
// in every later comparison — a fail-open outcome. Any value that is not present
// in the rank tables is replaced with the safest default for that field. An empty
// value means "unspecified" and is left as-is, matching mergePolicy's semantics.
func sanitizeSeedPolicy(p ExecutionPolicy, ruleID string) ExecutionPolicy {
	safest := safestDefaultPolicy()
	if _, ok := autonomyRank[p.Autonomy]; !ok && p.Autonomy != "" {
		policyLog.Printf("Rule %s has unrecognized autonomy %q, falling back to %s", ruleID, p.Autonomy, safest.Autonomy)
		p.Autonomy = safest.Autonomy
	}
	if _, ok := writeScopeRank[p.WriteScope]; !ok && p.WriteScope != "" {
		policyLog.Printf("Rule %s has unrecognized write scope %q, falling back to %s", ruleID, p.WriteScope, safest.WriteScope)
		p.WriteScope = safest.WriteScope
	}
	return p
}

// safestDefaultPolicy returns the most restrictive execution policy: propose-only,
// no write scope, human approval required, auto-merge denied, and a single attempt.
func safestDefaultPolicy() ExecutionPolicy {
	f := false
	return ExecutionPolicy{
		Autonomy:              "propose_only",
		WriteScope:            "none",
		HumanApprovalRequired: true,
		AutoMergeAllowed:      &f,
		MaxAttempts:           1,
	}
}

// matches reports whether the rule's condition is satisfied by rec and repo.
// Empty condition fields act as wildcards. Domain, Priority, and Risk are
// matched against the record's labels. Org is matched against both the
// repository org and the repository owner.
func (r PolicyRule) matches(rec IntentRecord, repo RepositoryContext) bool {
	if r.When.Domain != "" && !slices.Contains(rec.Labels, r.When.Domain) {
		return false
	}
	if r.When.Priority != "" && !slices.Contains(rec.Labels, r.When.Priority) {
		return false
	}
	if r.When.Risk != "" && !slices.Contains(rec.Labels, r.When.Risk) {
		return false
	}
	if r.When.Org != "" && r.When.Org != repo.Org && r.When.Org != repo.Owner {
		return false
	}
	return true
}

// deepCopyPolicy returns an independent copy of p with pointer and slice fields
// freshly allocated, so that mutations to the copy cannot affect the original.
// AllowedTools uses slices.Clone (not cloneStrings) to preserve the nil-vs-empty
// distinction: nil = unrestricted, []string{} = deny-all.
func deepCopyPolicy(p ExecutionPolicy) ExecutionPolicy {
	result := p
	if p.AutoMergeAllowed != nil {
		v := *p.AutoMergeAllowed
		result.AutoMergeAllowed = &v
	}
	result.AllowedTools = slices.Clone(p.AllowedTools) // preserves nil vs []string{}
	result.DeniedTools = cloneStrings(p.DeniedTools)
	result.RequiredChecks = cloneStrings(p.RequiredChecks)
	result.RuleIDs = cloneStrings(p.RuleIDs)
	return result
}

// mergePolicy overlays fragment onto base, preserving the stricter value for each
// field. String fields (Autonomy, WriteScope) are replaced only when the fragment's
// value is more restrictive per the defined rank tables. Boolean gates are ORed
// (human approval) or ANDed (auto-merge). Numeric limits take the minimum.
// AllowedTools is intersected (stricter-wins); DeniedTools and RequiredChecks are
// unioned.
func mergePolicy(base, fragment ExecutionPolicy) ExecutionPolicy {
	result := base
	if fragment.Autonomy != "" && autonomyRank[fragment.Autonomy] > autonomyRank[result.Autonomy] {
		result.Autonomy = fragment.Autonomy
	}
	if fragment.WriteScope != "" && writeScopeRank[fragment.WriteScope] > writeScopeRank[result.WriteScope] {
		result.WriteScope = fragment.WriteScope
	}
	if fragment.HumanApprovalRequired {
		result.HumanApprovalRequired = true
	}
	if fragment.AutoMergeAllowed != nil {
		if result.AutoMergeAllowed == nil || (!*fragment.AutoMergeAllowed && *result.AutoMergeAllowed) {
			v := *fragment.AutoMergeAllowed
			result.AutoMergeAllowed = &v
		}
	}
	if fragment.MaxAttempts > 0 && fragment.MaxAttempts < result.MaxAttempts {
		result.MaxAttempts = fragment.MaxAttempts
	}
	result.AllowedTools = intersectAllowedTools(base.AllowedTools, fragment.AllowedTools)
	result.DeniedTools = append(cloneStrings(base.DeniedTools), fragment.DeniedTools...)
	result.RequiredChecks = append(cloneStrings(base.RequiredChecks), fragment.RequiredChecks...)
	return result
}

// intersectAllowedTools merges two AllowedTools slices with stricter-wins semantics.
// nil means unrestricted (matches any tool); []string{} means deny-all.
// The intersection of two non-nil lists returns only tools present in both.
func intersectAllowedTools(base, fragment []string) []string {
	if base == nil {
		return slices.Clone(fragment) // unrestricted base defers to fragment's restriction
	}
	if fragment == nil {
		return slices.Clone(base) // unrestricted fragment defers to base's restriction
	}
	// Both non-nil: intersect so only tools allowed by both sides are permitted.
	// result is initialized to []string{} (deny-all) rather than nil (unrestricted)
	// so that the intersection of two empty non-nil lists produces deny-all, not
	// unrestricted. This preserves stricter-wins semantics for explicit deny-all rules.
	result := []string{}
	for _, tool := range base {
		if slices.Contains(fragment, tool) {
			result = append(result, tool)
		}
	}
	return result
}
