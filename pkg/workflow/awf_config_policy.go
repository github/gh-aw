// This file resolves model policy rules and domain lists used by the AWF
// configuration file. See awf_config_build.go for how these values are applied.

package workflow

import (
	"strings"

	"github.com/github/gh-aw/pkg/setutil"
	"github.com/github/gh-aw/pkg/workflow/compilerenv"
)

// splitDomainList splits a comma-separated domain string into a deduplicated
// slice. Empty entries are ignored. The order of the original list is preserved for
// non-duplicate entries; this keeps the allow-list deterministic.
func splitDomainList(domains string) []string {
	var result []string
	seen := make(map[string]struct {
	})
	for d := range strings.SplitSeq(domains, ",") {
		d = strings.TrimSpace(d)
		if d != "" && !setutil.Contains(seen, d) {
			seen[d] = struct {
			}{}
			result = append(result, d)
		}
	}
	return result
}

// resolveModelPolicyForAWFConfig applies policy precedence independently per list:
// allowed rules are narrowed using intersection with env policy, while blocked
// rules are widened using union with env policy.
func resolveModelPolicyForAWFConfig(workflowData *WorkflowData) ([]string, []string) {
	envAllowed, hasAllowedOverride := compilerenv.ResolvePolicyModelsAllowed()
	envBlocked, hasBlockedOverride := compilerenv.ResolvePolicyModelsBlocked()
	var allowed []string
	var blocked []string
	if workflowData != nil {
		allowed = workflowData.ModelPolicyAllowed
		blocked = workflowData.ModelPolicyBlocked
	}
	if hasAllowedOverride {
		allowed = intersectModelPolicyRules(allowed, envAllowed)
	}
	if hasBlockedOverride {
		blocked = unionModelPolicyRules(blocked, envBlocked)
	}
	blockedSet := make(map[string]struct{}, len(blocked))
	for _, model := range blocked {
		blockedSet[model] = struct{}{}
	}
	allowed = filterAllowedModelConflictsWithSet(allowed, blockedSet)
	return allowed, blocked
}

func intersectModelPolicyRules(local, override []string) []string {
	if len(override) == 0 {
		return append([]string(nil), local...)
	}
	// No local allow-list means no workflow restriction; keep the env allow-list.
	if len(local) == 0 {
		return append([]string(nil), override...)
	}
	localSet := make(map[string]struct{}, len(local))
	for _, model := range local {
		localSet[model] = struct{}{}
	}
	result := make([]string, 0, len(override))
	for _, model := range override {
		if _, ok := localSet[model]; ok {
			result = append(result, model)
		}
	}
	return result
}

func unionModelPolicyRules(local, override []string) []string {
	result := make([]string, 0)
	seen := make(map[string]struct{})
	for _, model := range local {
		if _, ok := seen[model]; ok {
			continue
		}
		seen[model] = struct{}{}
		result = append(result, model)
	}
	for _, model := range override {
		if _, ok := seen[model]; ok {
			continue
		}
		seen[model] = struct{}{}
		result = append(result, model)
	}
	return result
}
