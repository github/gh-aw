// This file handles detection and cleanup of credentials left by known GitHub Actions.
//
// # Known Action Credentials Cleanup
//
// Certain well-known GitHub Actions authenticate to cloud providers or container
// registries and leave credentials on disk. If these actions are detected in a
// workflow, a cleanup step is injected before the agentic engine executes to
// remove those credentials.
//
// # Known Actions
//
// The following actions are recognized and their credential locations cleared:
//   - google-github-actions/auth  → ./gha-creds-*.json (GCP service account keys)
//   - aws-actions/configure-aws-credentials → ~/.aws/credentials (AWS access keys)
//   - azure/login                 → ~/.azure/ (Azure service principal credentials)
//   - docker/login-action         → ~/.docker/config.json (Docker registry auth tokens)
//   - actions/checkout (with deploy key) → ~/.ssh/ (SSH private keys from deploy key)
//     Only triggered when the step has a non-empty 'with.ssh-key' input.
//
// # Integration
//
// Detection scans all merged workflow steps (custom steps, pre-steps, and
// pre-agent-steps) and stores the set of required cleanups in WorkflowData.
// The compiler then injects the cleanup step immediately before the agent
// execution step, at the same point as the git credentials cleaner.

package workflow

import (
	"maps"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/typeutil"
	"github.com/goccy/go-yaml"
)

var knownActionCredentialsLog = logger.New("workflow:known_action_credentials")

// knownCredentialLeakingAction describes a GitHub Action that leaves credentials
// on disk that must be cleaned before the agentic engine executes.
type knownCredentialLeakingAction struct {
	// actionPrefix is the action reference without the @version suffix
	// (e.g., "aws-actions/configure-aws-credentials")
	actionPrefix string
	// envVar is the environment variable set to "true" when this action is detected,
	// controlling which cleanup is performed by the shell script
	envVar string
	// credentialPaths describes the credentials the action creates (used in log messages)
	credentialPaths string
	// extraCheck is an optional additional predicate evaluated against the full step
	// map when the action prefix matches. When non-nil, both the prefix match and this
	// predicate must return true for the action to be considered detected.
	// When nil, the prefix match alone is sufficient.
	extraCheck func(step map[string]any) bool
}

// checkoutHasSSHKey returns true when an actions/checkout step has a non-empty
// 'with.ssh-key' input, which indicates a deploy key is being used and SSH
// credentials will be left on disk.
func checkoutHasSSHKey(step map[string]any) bool {
	withVal, ok := step["with"]
	if !ok {
		return false
	}
	withMap, ok := withVal.(map[string]any)
	if !ok {
		return false
	}
	sshKey, ok := withMap["ssh-key"]
	if !ok {
		return false
	}
	switch v := sshKey.(type) {
	case string:
		return strings.TrimSpace(v) != ""
	default:
		// Non-string value (e.g. an expression object) is treated as present
		return true
	}
}

// knownCredentialLeakingActions is the ordered list of GitHub Actions known to leave
// credentials on disk. Order determines the env-var order in the generated YAML step.
var knownCredentialLeakingActions = []knownCredentialLeakingAction{
	{
		actionPrefix:    "google-github-actions/auth",
		envVar:          "GH_AW_CLEAN_GCP",
		credentialPaths: "./gha-creds-*.json (GCP service account keys)",
	},
	{
		actionPrefix:    "aws-actions/configure-aws-credentials",
		envVar:          "GH_AW_CLEAN_AWS",
		credentialPaths: "~/.aws/credentials (AWS access keys)",
	},
	{
		actionPrefix:    "azure/login",
		envVar:          "GH_AW_CLEAN_AZURE",
		credentialPaths: "~/.azure/ (Azure service principal credentials)",
	},
	{
		actionPrefix:    "docker/login-action",
		envVar:          "GH_AW_CLEAN_DOCKER",
		credentialPaths: "~/.docker/config.json (Docker registry auth tokens)",
	},
	{
		actionPrefix:    "actions/checkout",
		envVar:          "GH_AW_CLEAN_SSH",
		credentialPaths: "~/.ssh/ (SSH private keys from deploy key)",
		// Only clean SSH keys when a deploy key is explicitly configured via ssh-key input.
		// Standard token-based checkouts do not leave SSH credentials on disk.
		extraCheck: checkoutHasSSHKey,
	},
}

// DetectKnownCredentialLeakingActions scans a list of workflow steps and returns a
// map of environment variable names to true for each known credential-leaking action
// found. The returned map is used to generate the cleanup step env block.
// Returns nil when no known actions are detected.
func DetectKnownCredentialLeakingActions(steps []any) map[string]struct {
} {
	detected := map[string]struct {
	}{}

	for _, step := range steps {
		stepMap, ok := step.(map[string]any)
		if !ok {
			continue
		}
		uses, ok := stepMap["uses"].(string)
		if !ok || uses == "" {
			continue
		}

		// Strip inline comment annotations (e.g., "actions/checkout@v4 # pinned")
		uses, _, _ = strings.Cut(uses, " #")
		uses = strings.TrimSpace(uses)
		// Strip the @version suffix
		actionRef, _, _ := strings.Cut(uses, "@")

		for _, known := range knownCredentialLeakingActions {
			if actionRef != known.actionPrefix {
				continue
			}
			// When an extra predicate is configured, both the prefix match and the
			// predicate must pass before treating this action as detected.
			if known.extraCheck != nil && !known.extraCheck(stepMap) {
				continue
			}
			detected[known.envVar] = struct {
			}{}
			knownActionCredentialsLog.Printf(
				"Detected known credential-leaking action: %s → will clean %s",
				actionRef, known.credentialPaths,
			)
		}
	}

	if len(detected) == 0 {
		return nil
	}
	return detected
}

// detectKnownCredentialLeakingActionsFromYAML parses a YAML steps string and delegates
// to DetectKnownCredentialLeakingActions. The YAML string may use a "steps:" wrapper
// (as produced by processAndMergeSteps) or be a bare sequence. Returns nil if no
// known actions are detected or if the YAML cannot be parsed.
func detectKnownCredentialLeakingActionsFromYAML(stepsYAML string) map[string]struct {
} {
	if stepsYAML == "" {
		return nil
	}

	// Try wrapped form first: "steps:\n  - ...\n"
	var wrapped map[string]any
	if err := yaml.Unmarshal([]byte(stepsYAML), &wrapped); err == nil {
		if stepsVal, ok := wrapped["steps"]; ok {
			if steps, ok := stepsVal.([]any); ok {
				return DetectKnownCredentialLeakingActions(steps)
			}
		}
	}

	// Fall back to bare sequence form
	var steps []any
	if err := yaml.Unmarshal([]byte(stepsYAML), &steps); err == nil {
		return DetectKnownCredentialLeakingActions(steps)
	}

	return nil
}

// mergeKnownActionEnvVars merges two env-var maps into a single map.
// Either argument may be nil.
func mergeKnownActionEnvVars(a, b map[string]struct {
}) map[string]struct {
} {
	if len(a) == 0 && len(b) == 0 {
		return nil
	}
	merged := make(map[string]struct {
	}, typeutil.SafeAllocationCapacity(len(a), len(b)))
	maps.Copy(merged, a)
	maps.Copy(merged, b)
	return merged
}

// DetectKnownCredentialLeakingActionsFromWorkflowData scans all step collections in
// workflowData (custom steps, pre-steps, pre-agent-steps) and returns the merged set
// of environment variables required for the known-action credentials cleanup step.
// Returns nil when no known credential-leaking actions are found.
func DetectKnownCredentialLeakingActionsFromWorkflowData(workflowData *WorkflowData) map[string]struct {
} {
	result := mergeKnownActionEnvVars(
		detectKnownCredentialLeakingActionsFromYAML(workflowData.CustomSteps),
		mergeKnownActionEnvVars(
			detectKnownCredentialLeakingActionsFromYAML(workflowData.PreSteps),
			detectKnownCredentialLeakingActionsFromYAML(workflowData.PreAgentSteps),
		),
	)
	if len(result) > 0 {
		knownActionCredentialsLog.Printf("Known credential-leaking actions detected, env vars: %v", result)
	}
	return result
}
