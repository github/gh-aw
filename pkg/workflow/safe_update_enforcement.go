package workflow

import (
	"errors"
	"sort"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/setutil"
	"github.com/github/gh-aw/pkg/workflow/compilerenv"
	"github.com/goccy/go-yaml"
)

var safeUpdateLog = logger.New("workflow:safe_update")

// githubTokenSecret is the one secret that is always permitted in safe update mode.
// Stored without the "secrets." prefix to match manifest storage format.
const githubTokenSecret = "GITHUB_TOKEN"

// ghAwInternalSecrets lists secrets that are automatically injected by the gh-aw
// compiler as part of standard tool and engine configurations (e.g. GitHub MCP server,
// Copilot engine). These are infrastructure secrets managed by gh-aw itself, not
// user- or AI-authored content, so they are always permitted in safe update mode.
var ghAwInternalSecrets = map[string]bool{
	"GH_AW_GITHUB_TOKEN":            true,
	"GH_AW_GITHUB_MCP_SERVER_TOKEN": true,
	"GH_AW_AGENT_TOKEN":             true,
	"GH_AW_CI_TRIGGER_TOKEN":        true,
	"GH_AW_PROJECT_GITHUB_TOKEN":    true,
	"COPILOT_GITHUB_TOKEN":          true,
	// Enterprise-wide OTLP exporter credentials injected by injectOTLPConfig when
	// no observability.otlp endpoint is configured in frontmatter.
	compilerenv.DefaultOTLPEndpoint: true,
	compilerenv.DefaultOTLPHeaders:  true,
}

// PullRequestEventTransition captures the pull_request / pull_request_target trigger
// presence before and after a workflow update, used to detect privilege escalation
// where a workflow is converted from pull_request to pull_request_target.
type PullRequestEventTransition struct {
	OldHasPullRequest           bool
	OldHasPullRequestTarget     bool
	CurrentHasPullRequest       bool
	CurrentHasPullRequestTarget bool
}

// SafeUpdateOptions contains the inputs used to validate a safe update.
type SafeUpdateOptions struct {
	Manifest                *GHAWManifest
	SecretNames             []string
	ActionRefs              []string
	CurrentRedirect         string
	PullRequestTransition   PullRequestEventTransition
	MemoryValidationScripts []GHAWManifestMemoryValidationScript
}

// EnforceSafeUpdate validates that no new restricted secrets or unapproved action
// changes have been introduced compared to those recorded in the existing manifest.
//
// Manifest is the gh-aw-manifest extracted from the current lock file before
// recompilation.
//
//   - nil means a lock file was found but it predates the safe-updates feature
//     (no gh-aw-manifest section). Enforcement is skipped so legacy lock files
//     are not flagged on upgrade.
//   - non-nil (including an empty &GHAWManifest{}) means the caller has a
//     baseline to compare against. Pass &GHAWManifest{} when no lock file
//     exists yet (first compilation); all new secrets/actions will be flagged.
//
// SecretNames contains the raw names produced by CollectSecretReferences (i.e.
// they may or may not carry the "secrets." prefix; both forms are normalized
// via normalizeSecretName before comparison).
//
// ActionRefs contains the raw action reference strings produced by CollectActionReferences,
// e.g. "actions/checkout@abc1234 # v4".
//
// Returns a structured, actionable error when violations are found.
func EnforceSafeUpdate(options SafeUpdateOptions) error {
	if options.Manifest == nil {
		// Lock file exists but predates the safe-updates feature (no gh-aw-manifest
		// section). Skip enforcement so legacy lock files are not flagged on upgrade.
		safeUpdateLog.Print("Lock file has no gh-aw-manifest; skipping safe update enforcement (legacy lock file)")
		return nil
	}

	secretViolations := collectSecretViolations(options.Manifest, options.SecretNames)
	addedActions, removedActions := collectActionViolations(options.Manifest, options.ActionRefs)
	addedRedirect, removedRedirect := collectRedirectViolations(options.Manifest, options.CurrentRedirect)
	memoryValidationScriptChanges := collectMemoryValidationScriptChanges(options.Manifest, options.MemoryValidationScripts)
	pullRequestTargetEscalation := hasPullRequestTargetEscalation(options.PullRequestTransition.OldHasPullRequest, options.PullRequestTransition.OldHasPullRequestTarget, options.PullRequestTransition.CurrentHasPullRequest, options.PullRequestTransition.CurrentHasPullRequestTarget)

	if len(secretViolations) == 0 && len(addedActions) == 0 && len(removedActions) == 0 && addedRedirect == "" && removedRedirect == "" && len(memoryValidationScriptChanges) == 0 && !pullRequestTargetEscalation {
		safeUpdateLog.Printf("Safe update check passed (%d secret(s), %d action(s) verified)",
			len(options.SecretNames), len(options.ActionRefs))
		return nil
	}

	if len(secretViolations) > 0 {
		safeUpdateLog.Printf("Safe update violation: %d new secret(s) detected: %s",
			len(secretViolations), strings.Join(secretViolations, ", "))
	}
	if len(addedActions) > 0 {
		safeUpdateLog.Printf("Safe update violation: %d new action(s) added: %s",
			len(addedActions), strings.Join(addedActions, ", "))
	}
	if len(removedActions) > 0 {
		safeUpdateLog.Printf("Safe update violation: %d action(s) removed: %s",
			len(removedActions), strings.Join(removedActions, ", "))
	}
	if addedRedirect != "" {
		safeUpdateLog.Printf("Safe update violation: redirect added: %s", addedRedirect)
	}
	if removedRedirect != "" {
		safeUpdateLog.Printf("Safe update violation: redirect removed: %s", removedRedirect)
	}
	if len(memoryValidationScriptChanges) > 0 {
		safeUpdateLog.Printf("Safe update violation: %d memory validation script change(s) detected: %s",
			len(memoryValidationScriptChanges), strings.Join(memoryValidationScriptChanges, ", "))
	}
	if pullRequestTargetEscalation {
		safeUpdateLog.Print("Safe update violation: pull_request event converted to pull_request_target")
	}

	return buildSafeUpdateError(secretViolations, addedActions, removedActions, addedRedirect, removedRedirect, pullRequestTargetEscalation, memoryValidationScriptChanges)
}

func hasPullRequestTargetEscalation(oldHasPullRequest bool, oldHasPullRequestTarget bool, currentHasPullRequest bool, currentHasPullRequestTarget bool) bool {
	return oldHasPullRequest && !oldHasPullRequestTarget && !currentHasPullRequest && currentHasPullRequestTarget
}

func extractPullRequestEventPresenceFromOnField(onField any) (hasPR bool, hasPRTarget bool) {
	switch v := onField.(type) {
	case string:
		return v == "pull_request", v == "pull_request_target"
	case []any:
		for _, item := range v {
			event, ok := item.(string)
			if !ok {
				continue
			}
			if event == "pull_request" {
				hasPR = true
			}
			if event == "pull_request_target" {
				hasPRTarget = true
			}
		}
	case map[string]any:
		_, hasPR = v["pull_request"]
		_, hasPRTarget = v["pull_request_target"]
	}
	return hasPR, hasPRTarget
}

func extractPullRequestEventPresenceFromCompiledWorkflow(content string) (hasPR bool, hasPRTarget bool) {
	var parsed map[string]any
	if err := yaml.Unmarshal([]byte(content), &parsed); err != nil {
		return false, false
	}
	onField, ok := parsed["on"]
	if !ok {
		return false, false
	}
	return extractPullRequestEventPresenceFromOnField(onField)
}

// collectSecretViolations returns the normalized secret names that are new (not in the
// previous manifest) and are not among the always-allowed secrets (GITHUB_TOKEN and
// gh-aw-internal secrets automatically injected by the compiler).
func collectSecretViolations(manifest *GHAWManifest, secretNames []string) []string {
	known := make(map[string]struct {
	}, len(manifest.Secrets))
	for _, s := range manifest.Secrets {
		known[s] = struct {
		}{}
	}

	var violations []string
	for _, name := range secretNames {
		full := normalizeSecretName(name)
		if full == githubTokenSecret {
			continue
		}
		if ghAwInternalSecrets[full] {
			continue
		}
		if setutil.Contains(known, full) {
			continue
		}
		violations = append(violations, full)
	}
	sort.Strings(violations)
	return violations
}

// githubActionsOrg is the owner whose actions are always trusted and never flagged
// as unapproved additions, regardless of what was recorded in the manifest.
const githubActionsOrg = "actions"

// ghAwActionPrefixes lists the repo prefixes for gh-aw's own infrastructure actions.
// These are always trusted and never flagged as unapproved additions, since they are
// managed by the gh-aw project itself and upgraded automatically by `gh aw upgrade`.
var ghAwActionPrefixes = []string{
	"github/gh-aw/actions/",
	"github/gh-aw-actions/",
}

// isTrustedActionRepo reports whether a repo string belongs to a trusted org or project.
// Trusted repos include the "actions/" GitHub org, gh-aw's own infrastructure actions,
// and actions used by the runtime manager (e.g. ruby/setup-ruby, oven-sh/setup-bun).
func isTrustedActionRepo(repo string) bool {
	if strings.HasPrefix(repo, githubActionsOrg+"/") {
		return true
	}
	for _, prefix := range ghAwActionPrefixes {
		if strings.HasPrefix(repo, prefix) {
			return true
		}
	}
	_, ok := actionRepoToRuntime[repo]
	return ok
}

// collectActionViolations compares the new action refs against the previous manifest
// and returns two sorted slices: repos that were added and repos that were removed.
// The comparison uses the action repo as the key, so SHA/version changes to an
// already-approved repo are not flagged.
// Actions belonging to the "actions/" GitHub org, gh-aw infrastructure repos, and
// runtime manager repos are always trusted and never flagged.
func collectActionViolations(manifest *GHAWManifest, actionRefs []string) (added []string, removed []string) {
	// Build known repo set from previous manifest.
	knownRepos := make(map[string]struct {
	}, len(manifest.Actions))
	for _, a := range manifest.Actions {
		knownRepos[a.Repo] = struct {
		}{}
	}

	// Build new repo set from the freshly compiled action refs.
	newActions := parseActionRefs(actionRefs)
	newRepos := make(map[string]struct {
	}, len(newActions))
	for _, a := range newActions {
		newRepos[a.Repo] = struct {
		}{}
	}

	// Find additions: repos present in the new compilation but absent from the manifest.
	// Trusted actions (actions/ org, gh-aw infrastructure, runtime manager) are always allowed and never flagged.
	for repo := range newRepos {
		if isTrustedActionRepo(repo) {
			continue
		}
		if !setutil.Contains(knownRepos, repo) {
			added = append(added, repo)
		}
	}

	// Find removals: repos present in the previous manifest but absent from the new compilation.
	// Trusted actions (actions/ org, gh-aw infrastructure, runtime manager) are always allowed, so their removal is not flagged.
	for repo := range knownRepos {
		if isTrustedActionRepo(repo) {
			continue
		}
		if !setutil.Contains(newRepos, repo) {
			removed = append(removed, repo)
		}
	}

	sort.Strings(added)
	sort.Strings(removed)
	return added, removed
}

// collectRedirectViolations compares the redirect recorded in the previous manifest
// with the redirect currently configured in frontmatter.
// It returns:
//   - added: a redirect newly configured in current frontmatter
//   - removed: a previously-approved redirect that is now absent
func collectRedirectViolations(manifest *GHAWManifest, currentRedirect string) (added string, removed string) {
	knownRedirect := strings.TrimSpace(manifest.Redirect)
	current := strings.TrimSpace(currentRedirect)

	if knownRedirect == current {
		return "", ""
	}
	if knownRedirect == "" && current != "" {
		return current, ""
	}
	if knownRedirect != "" && current == "" {
		return "", knownRedirect
	}
	// At this point both values are non-empty and differ after TrimSpace normalization,
	// so treat the change as one removed redirect plus one added redirect.
	return current, knownRedirect
}

func collectMemoryValidationScriptChanges(manifest *GHAWManifest, current []GHAWManifestMemoryValidationScript) []string {
	previous := make(map[string]string, len(manifest.MemoryValidationScripts))
	for _, script := range manifest.MemoryValidationScripts {
		previous[script.Memory] = script.SHA256
	}
	currentByMemory := make(map[string]string, len(current))
	for _, script := range current {
		currentByMemory[script.Memory] = script.SHA256
	}
	var changes []string
	for memory, hash := range currentByMemory {
		switch previousHash, ok := previous[memory]; {
		case !ok:
			changes = append(changes, memory+" (added)")
		case previousHash != hash:
			changes = append(changes, memory+" (modified)")
		}
	}
	for memory := range previous {
		if _, ok := currentByMemory[memory]; !ok {
			changes = append(changes, memory+" (removed)")
		}
	}
	sort.Strings(changes)
	return changes
}

// buildSafeUpdateError creates a clear, structured error message that names the
// offending secrets, actions, and redirects and tells the user how to remediate.
func buildSafeUpdateError(secretViolations, addedActions, removedActions []string, addedRedirect, removedRedirect string, hasPullRequestTargetEscalation bool, memoryValidationScriptChanges []string) error {
	var sb strings.Builder
	sb.WriteString("safe update mode detected unapproved changes\n")

	if len(secretViolations) > 0 {
		sb.WriteString("\nNew restricted secret(s):\n  - ")
		sb.WriteString(strings.Join(secretViolations, "\n  - "))
	}
	if len(addedActions) > 0 {
		sb.WriteString("\nNew unapproved action(s):\n  - ")
		sb.WriteString(strings.Join(addedActions, "\n  - "))
	}
	if len(removedActions) > 0 {
		sb.WriteString("\nPreviously-approved action(s) removed:\n  - ")
		sb.WriteString(strings.Join(removedActions, "\n  - "))
	}
	if addedRedirect != "" {
		sb.WriteString("\nNew redirect configured:\n  - ")
		sb.WriteString(addedRedirect)
	}
	if removedRedirect != "" {
		sb.WriteString("\nPreviously-approved redirect removed:\n  - ")
		sb.WriteString(removedRedirect)
	}
	if len(memoryValidationScriptChanges) > 0 {
		sb.WriteString("\nMemory validation script changes:\n  - ")
		sb.WriteString(strings.Join(memoryValidationScriptChanges, "\n  - "))
	}
	if hasPullRequestTargetEscalation {
		sb.WriteString("\nEvent trigger security escalation:\n  - pull_request was converted to pull_request_target")
	}

	sb.WriteString("\n\nRemediation options:\n  1. Use the --approve flag to allow the changes.\n  2. Revert the unapproved changes.\n  3. Use an interactive coding agent to review and approve the changes.")
	return errors.New(sb.String())
}

// buildSafeUpdateWarningPrompt wraps the raw safe update violation message in a
// structured prompt that:
//   - Asks the calling agent to review each new secret / action for malicious use
//   - Asks the agent to include a security review note in the pull request description
//     so that human reviewers are aware of the changes
func buildSafeUpdateWarningPrompt(violationMsg string) string {
	var sb strings.Builder
	sb.WriteString("⚠️  SECURITY REVIEW REQUIRED — safe update mode detected unapproved changes:\n\n")
	sb.WriteString(violationMsg)
	sb.WriteString("\n\n")
	sb.WriteString("Action required for the calling agent:\n")
	sb.WriteString("  1. Review every new secret and action listed above for potential malicious use\n")
	sb.WriteString("     (e.g. credential exfiltration, supply-chain attacks, unexpected network access).\n")
	sb.WriteString("  2. Add a security review note to the pull request description that:\n")
	sb.WriteString("     - Lists any new or removed secrets, actions, and redirect changes from this compilation.\n")
	sb.WriteString("     - Confirms you have reviewed each one and explains why it is safe to add.\n")
	sb.WriteString("     - Flags any item you cannot fully verify so a human reviewer can inspect it.\n")
	return sb.String()
}
