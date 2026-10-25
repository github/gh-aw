package cli

import (
	"encoding/json"
	"fmt"
	"maps"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/typeutil"
	"github.com/github/gh-aw/pkg/workflow"
)

var bashAllowlistUnsupportedEngineCodemodLog = logger.New("cli:codemod_bash_allowlist_unsupported_engine")

// getBashAllowlistUnsupportedEngineCodemod creates a codemod that emits a guided error when
// a workflow restricts bash commands (tools.bash with a specific command list, an empty list,
// or bash: false) while using an engine that cannot enforce the restriction (for example codex).
//
// The restriction is silently ignored at runtime by such engines, so the compiler rejects it in
// strict mode. It is not auto-corrected because both remediations change semantics: rewriting the
// allow-list to bash: ["*"] widens the effective (declared) permissions, and switching engines
// changes which agent runs the workflow. The user must choose.
func getBashAllowlistUnsupportedEngineCodemod() Codemod {
	return Codemod{
		ID:           "bash-allowlist-unsupported-engine-guided-error",
		Name:         "Detect bash allow-list on an engine that ignores it (manual fix required)",
		Description:  "Detects a restricted 'tools.bash' configuration combined with an engine that does not enforce bash command allow-listing (such as codex), and emits a guided error because the fix (widening to bash: [\"*\"] or switching engines) changes workflow semantics.",
		IntroducedIn: "0.78.0",
		Guided:       true,
		Apply: func(content string, frontmatter map[string]any) (string, bool, error) {
			return applyBashAllowlistUnsupportedEngineCheck(content, frontmatter, "")
		},
		ApplyWithContext: func(content string, frontmatter map[string]any, filePath string) (string, bool, error) {
			return applyBashAllowlistUnsupportedEngineCheck(content, frontmatter, filePath)
		},
	}
}

// applyBashAllowlistUnsupportedEngineCheck is the shared implementation used by both Apply and
// ApplyWithContext. When filePath is non-empty, effective tools are resolved from imports and
// markdown includes so that restrictions introduced via shared imports are also caught.
func applyBashAllowlistUnsupportedEngineCheck(content string, frontmatter map[string]any, filePath string) (string, bool, error) {
	effectiveTools, err := resolveEffectiveBashTools(content, frontmatter, filePath)
	if err != nil {
		bashAllowlistUnsupportedEngineCodemodLog.Printf("Failed to resolve effective tools: %v", err)
		// Fall back to top-level tools only so we never swallow a real error silently.
		effectiveTools, _ = typeutil.LookupMap(frontmatter, "tools")
	}

	// Extract the bash value once so we avoid a double-read: HasBashExplicitRestriction
	// inspects it and describeBashRestriction renders it; both see the same value.
	bashVal := effectiveTools["bash"]
	if !workflow.HasBashExplicitRestriction(effectiveTools) {
		return content, false, nil
	}

	engineID := extractEngineIDFromFrontmatter(frontmatter)
	engine, err := workflow.GetGlobalEngineRegistry().GetEngine(engineID)
	if err != nil {
		bashAllowlistUnsupportedEngineCodemodLog.Printf("Unknown engine %q, skipping bash allow-list check", engineID)
		return content, false, nil
	}
	if engine.GetCapabilities().BashCommandAllowlist {
		return content, false, nil
	}

	bashAllowlistUnsupportedEngineCodemodLog.Printf("Engine %s ignores the restricted tools.bash configuration, emitting guided error", engineID)

	// Build the list of supported engines dynamically from the registry so the message stays
	// accurate as new engines gain BashCommandAllowlist support.
	supportedEngines := workflow.GetGlobalEngineRegistry().EnginesWithCapability(func(c workflow.EngineCapabilities) bool {
		return c.BashCommandAllowlist
	})

	return content, false, fmt.Errorf(
		"engine '%s' does not support bash command allow-listing: %s is silently ignored at runtime for this engine. "+
			"Manual fix required: switch to an engine that enforces the allow-list (%s), "+
			"or replace the configuration with 'bash: [\"*\"]' to make the unrestricted access explicit. "+
			"See: https://github.github.com/gh-aw/reference/tools/",
		engineID,
		describeBashRestriction(bashVal),
		strings.Join(supportedEngines, ", "),
	)
}

// resolveEffectiveBashTools returns the effective tools map for the workflow, merging in tools
// from imports and markdown includes when a file path is available. When filePath is empty (for
// example in unit tests), only the raw top-level tools from frontmatter are returned.
//
// The returned map is a best-effort result: if import resolution fails the error is returned so
// the caller can fall back gracefully.
func resolveEffectiveBashTools(content string, frontmatter map[string]any, filePath string) (map[string]any, error) {
	topTools, _ := typeutil.LookupMap(frontmatter, "tools")

	// Fast path: if the top-level tools already declares a bash key, the top-level value is
	// authoritative (it wins in the MergeTools merge), so we never need to resolve imports.
	if _, hasBash := topTools["bash"]; hasBash {
		return topTools, nil
	}

	// If no file path is available (e.g. unit tests), return top-level tools as-is.
	if filePath == "" {
		return topTools, nil
	}

	return resolveEffectiveTools(content, frontmatter, filePath)
}

// resolveEffectiveTools returns the effective tools map for the workflow, merging in tools from
// imports and markdown includes when a file path is available.
func resolveEffectiveTools(content string, frontmatter map[string]any, filePath string) (map[string]any, error) {
	topTools, _ := typeutil.LookupMap(frontmatter, "tools")

	if filePath == "" {
		return topTools, nil
	}

	baseDir := filepath.Dir(filePath)
	importCache := parser.NewImportCache("")

	// Resolve tools from frontmatter imports (imports: [...] section).
	importsResult, err := parser.ProcessImportsFromFrontmatterWithSource(frontmatter, baseDir, importCache, filePath, content)
	if err != nil {
		return nil, fmt.Errorf("resolving imports: %w", err)
	}

	// Resolve tools from markdown <!-- include: ... --> directives.
	includedTools, _, err := parser.ExpandIncludesWithManifest(content, baseDir, true)
	if err != nil {
		return nil, fmt.Errorf("expanding includes: %w", err)
	}

	// Combine all imported and included tools lines (same format as the compiler).
	allExternalTools := strings.Join(nonEmptyStrs(importsResult.MergedTools, includedTools), "\n")
	if allExternalTools == "" {
		return topTools, nil
	}

	// Merge external tools into the top-level tools map, line by line (each line is a JSON object).
	effective := make(map[string]any)
	maps.Copy(effective, topTools)
	for line := range strings.SplitSeq(allExternalTools, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line == "{}" {
			continue
		}
		var imported map[string]any
		if err := json.Unmarshal([]byte(line), &imported); err != nil {
			continue
		}
		merged, err := parser.MergeTools(effective, imported)
		if err != nil {
			return nil, fmt.Errorf("merging tools from import: %w", err)
		}
		effective = merged
	}

	return effective, nil
}

// nonEmptyStrs returns a slice containing only the non-empty strings from the arguments.
func nonEmptyStrs(strs ...string) []string {
	out := make([]string, 0, len(strs))
	for _, s := range strs {
		if strings.TrimSpace(s) != "" {
			out = append(out, s)
		}
	}
	return out
}

// describeBashRestriction renders a short human-readable description of the offending
// tools.bash configuration for use in the guided error message.
func describeBashRestriction(bashConfig any) string {
	switch value := bashConfig.(type) {
	case bool:
		return fmt.Sprintf("'bash: %t'", value)
	case []any:
		if len(value) == 0 {
			return "'bash: []'"
		}
		commands := make([]string, 0, len(value))
		for _, cmd := range value {
			commands = append(commands, fmt.Sprintf("%q", fmt.Sprintf("%v", cmd)))
		}
		return fmt.Sprintf("'bash: [%s]'", strings.Join(commands, ", "))
	}
	return "the tools.bash configuration"
}
