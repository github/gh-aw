package cli

import (
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var sandboxRuntimeProfileCodemodLog = logger.New("cli:codemod_sandbox_runtime_profile")

const (
	sandboxRuntimeDocker             = "docker"
	sandboxRuntimeDockerSudoIptables = "docker-sudo-iptables"
	sandboxRuntimeCloudHypervisor    = "cloud-hypervisor"
)

// getSandboxRuntimeProfileCodemod creates a codemod that migrates the removed
// sandbox.agent.sudo and sandbox.agent.legacy-security fields to the equivalent
// sandbox.agent.runtime profile.
//
//	sudo: false (or omitted)          -> runtime omitted (equivalent to runtime: docker)
//	legacy-security: enable           -> runtime: docker-sudo-iptables
//	sudo: true (no other runtime)     -> runtime: docker-sudo-iptables
//
// Other mixed profiles that cannot be migrated unambiguously return an actionable error
// so the author can choose between strict isolation and the privileged iptables profile.
func getSandboxRuntimeProfileCodemod() Codemod {
	return Codemod{
		ID:           "sandbox-runtime-profiles",
		Name:         "Migrate sandbox.agent.sudo and legacy-security to runtime profiles",
		Description:  "Replaces the removed 'sandbox.agent.sudo' and 'sandbox.agent.legacy-security' fields with the equivalent 'sandbox.agent.runtime' profile",
		IntroducedIn: "0.42.0",
		Apply: func(content string, frontmatter map[string]any) (string, bool, error) {
			agent, ok := getSandboxAgentMap(frontmatter)
			if !ok {
				return content, false, nil
			}

			sudoVal, hasSudo := agent["sudo"]
			legacyVal, hasLegacy := agent["legacy-security"]
			if !hasSudo && !hasLegacy {
				return content, false, nil
			}

			sudoEnabled, _ := sudoVal.(bool)
			legacyEnabled := false
			if legacyStr, isStr := legacyVal.(string); isStr && legacyStr == "enable" {
				legacyEnabled = true
			}

			runtime, _ := agent["runtime"].(string)
			targetRuntime, err := resolveMigratedSandboxRuntime(runtime, sudoEnabled, legacyEnabled)
			if err != nil {
				return content, false, err
			}

			newContent, applied, err := applyFrontmatterLineTransform(content, func(lines []string) ([]string, bool) {
				result, modified := migrateSandboxAgentSecurityLines(lines, runtime, targetRuntime)
				if modified {
					// Dropping the only key under sandbox.agent leaves a dangling
					// "agent:" (and possibly "sandbox:") key that YAML parses as null.
					result = removeParentBlockIfTrulyEmpty(result, "agent")
					result = removeParentBlockIfTrulyEmpty(result, "sandbox")
				}
				return result, modified
			})
			if applied {
				sandboxRuntimeProfileCodemodLog.Printf("Migrated sandbox.agent security fields to runtime profile %q", targetRuntime)
			}
			return newContent, applied, err
		},
	}
}

// resolveMigratedSandboxRuntime returns the runtime value the workflow should use
// after migration, or an empty string when no runtime key is required.
func resolveMigratedSandboxRuntime(runtime string, sudoEnabled, legacyEnabled bool) (string, error) {
	privileged := sudoEnabled || legacyEnabled

	// sudo: false / legacy-security absent keeps the secure default: the fields are
	// simply dropped and the runtime (if any) is preserved.
	if !privileged {
		return "", nil
	}

	switch runtime {
	case "", sandboxRuntimeDocker, sandboxRuntimeDockerSudoIptables:
		return sandboxRuntimeDockerSudoIptables, nil
	case sandboxRuntimeCloudHypervisor:
		// This runtime only used sudo as an installation marker; the compiler now
		// derives the required privileges, so the runtime is kept unchanged.
		if legacyEnabled {
			return "", mixedSandboxProfileError(runtime)
		}
		return "", nil
	default:
		return "", mixedSandboxProfileError(runtime)
	}
}

func mixedSandboxProfileError(runtime string) error {
	return fmt.Errorf(
		"sandbox.agent.runtime: %s combined with privileged security options (sudo/legacy-security) is no longer supported. "+
			"Choose one runtime profile: keep 'runtime: %s' for strict network isolation and remove 'sudo'/'legacy-security', "+
			"or use 'runtime: %s' for the privileged iptables profile with host access",
		runtime, runtime, sandboxRuntimeDockerSudoIptables,
	)
}

// migrateSandboxAgentSecurityLines removes the sudo and legacy-security keys from the
// sandbox.agent block. When targetRuntime is non-empty and the block has no runtime
// key yet, the first removed key is replaced by the runtime key so the profile is
// preserved in place. When the block already has a runtime key but its value differs
// from targetRuntime (for example an unsupported runtime migrating to the privileged iptables profile),
// the existing runtime line's value is rewritten in place.
func migrateSandboxAgentSecurityLines(lines []string, oldRuntime, targetRuntime string) ([]string, bool) {
	start, end, indent, found := findSandboxAgentBlock(lines)
	if !found {
		return lines, false
	}

	hasRuntime := oldRuntime != ""
	needsRuntime := targetRuntime != "" && !hasRuntime
	needsRuntimeUpdate := targetRuntime != "" && hasRuntime && targetRuntime != oldRuntime
	result := make([]string, 0, len(lines))
	modified := false

	for i, line := range lines {
		if i < start || i >= end {
			result = append(result, line)
			continue
		}
		trimmed := strings.TrimSpace(line)
		if needsRuntimeUpdate && getIndentation(line) == indent && strings.HasPrefix(trimmed, "runtime:") {
			result = append(result, indent+"runtime: "+targetRuntime+trailingCommentSuffix(strings.TrimPrefix(trimmed, "runtime:")))
			modified = true
			continue
		}
		if getIndentation(line) != indent ||
			(!strings.HasPrefix(trimmed, "sudo:") && !strings.HasPrefix(trimmed, "legacy-security:")) {
			result = append(result, line)
			continue
		}
		modified = true
		if needsRuntime {
			result = append(result, indent+"runtime: "+targetRuntime)
			needsRuntime = false
		}
	}

	return result, modified
}

// trailingCommentSuffix returns the trailing YAML comment of a value part, prefixed by a
// space so it can be appended to a rewritten key line, or an empty string when the value
// has no trailing comment.
func trailingCommentSuffix(valuePart string) string {
	idx := findTrailingCommentIndex(valuePart)
	if idx < 0 {
		return ""
	}
	return " " + strings.TrimRight(valuePart[idx:], " \t")
}

// findSandboxAgentBlock locates the child lines of the sandbox.agent mapping.
// It returns the half-open line range of the block's children and the indentation
// shared by those children.
func findSandboxAgentBlock(lines []string) (start, end int, indent string, found bool) {
	agentLine := findSandboxAgentLine(lines)
	if agentLine < 0 {
		return 0, 0, "", false
	}

	agentIndent := getIndentation(lines[agentLine])
	start = agentLine + 1
	end = len(lines)
	for j := start; j < len(lines); j++ {
		trimmed := strings.TrimSpace(lines[j])
		if trimmed == "" {
			continue
		}
		lineIndent := getIndentation(lines[j])
		if len(lineIndent) <= len(agentIndent) {
			end = j
			break
		}
		if indent == "" {
			indent = lineIndent
		}
	}

	if indent == "" {
		return 0, 0, "", false
	}
	return start, end, indent, true
}

// findSandboxAgentLine returns the index of the "agent:" key nested under the
// top-level "sandbox:" key, or -1 when it is absent.
func findSandboxAgentLine(lines []string) int {
	sandboxIndent := ""
	inSandbox := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		lineIndent := getIndentation(line)

		if !inSandbox {
			if isBareMappingKeyLine(trimmed, "sandbox") {
				inSandbox = true
				sandboxIndent = lineIndent
			}
			continue
		}

		// Left the sandbox block without finding an agent mapping.
		if len(lineIndent) <= len(sandboxIndent) {
			return -1
		}
		if isBareMappingKeyLine(trimmed, "agent") {
			return i
		}
	}
	return -1
}

// isBareMappingKeyLine reports whether a trimmed line declares the given mapping key
// with no inline value. A trailing comment is allowed (e.g. "agent:  # comment").
func isBareMappingKeyLine(trimmed, key string) bool {
	rest, ok := strings.CutPrefix(trimmed, key+":")
	if !ok {
		return false
	}
	rest = strings.TrimSpace(rest)
	return rest == "" || strings.HasPrefix(rest, "#")
}

// getSandboxAgentMap returns the parsed sandbox.agent mapping from frontmatter.
func getSandboxAgentMap(frontmatter map[string]any) (map[string]any, bool) {
	sandboxVal, ok := frontmatter["sandbox"]
	if !ok {
		return nil, false
	}
	sandboxMap, ok := sandboxVal.(map[string]any)
	if !ok {
		return nil, false
	}
	agentMap, ok := sandboxMap["agent"].(map[string]any)
	return agentMap, ok
}
