package workflow

import "github.com/github/gh-aw/pkg/logger"

var compilerYAMLPolicyLog = logger.New("workflow:compiler_yaml_policy")

// effectiveStrictMode computes the effective strict mode for a workflow.
// Priority: CLI flag or repository config > frontmatter strict field > default (true).
// This should be used when emitting metadata/env vars to correctly reflect the
// workflow's strictness as inferred from the source (frontmatter).
func (c *Compiler) effectiveStrictMode(frontmatter map[string]any) bool {
	if c.strictMode {
		// CLI flag takes precedence
		compilerYAMLPolicyLog.Print("Strict mode enabled by CLI flag")
		return true
	}
	if repoConfig, err := c.loadRepoConfig(); err == nil && repoConfig.Strict {
		compilerYAMLPolicyLog.Print("Strict mode enforced by repository config")
		return true
	}
	if strictVal, exists := frontmatter["strict"]; exists {
		if strictBool, ok := strictVal.(bool); ok {
			compilerYAMLPolicyLog.Printf("Strict mode resolved from frontmatter strict field: %v", strictBool)
			return strictBool
		}
	}
	// Default: strict mode is on when no explicit setting
	compilerYAMLPolicyLog.Print("Strict mode defaulting to true (no explicit setting)")
	return true
}

// effectiveSafeUpdate returns true when safe update mode should be enforced for
// the given workflow. Safe update mode is equivalent to strict mode: it is
// enabled whenever strict mode is active (CLI --strict flag, frontmatter
// strict: true, or the default). It can be disabled via the CLI --approve flag
// to approve all changes.
func (c *Compiler) effectiveSafeUpdate(data *WorkflowData) bool {
	if c.approve {
		compilerYAMLPolicyLog.Print("Safe update disabled: --approve flag set")
		return false
	}
	return c.effectiveStrictMode(data.RawFrontmatter)
}
