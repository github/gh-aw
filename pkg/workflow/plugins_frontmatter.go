package workflow

import (
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/gitutil"
	"github.com/github/gh-aw/pkg/semverutil"
)

// hasPathTraversalSegment reports whether repoPath contains a "." or ".."
// path segment, which would let pluginCheckoutSubpath escape the plugin's
// dedicated checkout folder (.gh-aw-plugins/plugin-N) and resolve to an
// unpinned path elsewhere in the workspace.
func hasPathTraversalSegment(repoPath string) bool {
	for segment := range strings.SplitSeq(repoPath, "/") {
		if segment == "." || segment == ".." {
			return true
		}
	}
	return false
}

// PluginReference describes a single plugins[] entry in workflow frontmatter.
// It supports both legacy string-only entries and object entries with per-plugin auth,
// mirroring SkillReference.
type PluginReference struct {
	Plugin      string           `json:"plugin,omitempty"`
	GitHubToken string           `json:"github-token,omitempty"`
	GitHubApp   *GitHubAppConfig `json:"github-app,omitempty"`
}

// pluginReferencesOrFallback returns workflowData.PluginReferences when populated, or
// builds an equivalent slice from workflowData.Plugins (no per-plugin auth) otherwise.
// This keeps callers that construct WorkflowData with only Plugins (e.g. tests, or
// frontmatter shapes that never populated PluginReferences) working unchanged.
func pluginReferencesOrFallback(workflowData *WorkflowData) []PluginReference {
	if len(workflowData.PluginReferences) > 0 {
		return append([]PluginReference(nil), workflowData.PluginReferences...)
	}
	refs := make([]PluginReference, 0, len(workflowData.Plugins))
	for _, plugin := range workflowData.Plugins {
		refs = append(refs, PluginReference{Plugin: plugin})
	}
	return refs
}

func (c *Compiler) validatePlugins(workflowData *WorkflowData) error {
	if workflowData == nil {
		return nil
	}

	refs := pluginReferencesOrFallback(workflowData)

	for i, ref := range refs {
		plugin := ref.Plugin
		parsed := parseSkillRefSpec(plugin)
		if !parsed.isRemote || parsed.ref == "" {
			return fmt.Errorf("plugins[%d]: invalid plugin reference %q; expected owner/repository[/path]@ref, for example github/awesome-copilot/plugins/example@v1", i, plugin)
		}
		if hasPathTraversalSegment(parsed.repoPath) {
			return fmt.Errorf("plugins[%d]: repository path %q must not contain '.' or '..' segments; expected a plain repository-relative path", i, parsed.repoPath)
		}
		if !parsed.isFullSHA && len(parsed.ref) == 40 && gitutil.IsValidFullSHACaseInsensitive(parsed.ref) {
			return fmt.Errorf("plugins[%d]: ref %q looks like a commit SHA but must be lowercase hexadecimal; expected all-lowercase hex characters, for example a1b2c3", i, parsed.ref)
		}
		if looksLikeAmbiguousSHA(parsed.ref) {
			return fmt.Errorf("plugins[%d]: ref %q looks like a truncated or malformed commit SHA; use the full 40-character lowercase SHA or a branch/tag name", i, parsed.ref)
		}
		if !parsed.isFullSHA && (!skillRefCharsRegexp.MatchString(parsed.ref) || strings.Contains(parsed.ref, "..")) {
			return fmt.Errorf("plugins[%d]: ref %q contains unsupported characters; expected letters, digits, '.', '_', '-', and '/', starting with a letter or digit and not containing '..', for example v1.2.3", i, parsed.ref)
		}
	}

	mergedRefs, err := mergeValidatedPluginRefs(refs)
	if err != nil {
		return err
	}
	workflowData.PluginReferences = mergedRefs
	workflowData.Plugins = pluginRefsToStrings(mergedRefs)
	return nil
}

// pluginRefsToStrings returns the plain plugin spec strings from refs, in order.
func pluginRefsToStrings(refs []PluginReference) []string {
	plugins := make([]string, 0, len(refs))
	for _, ref := range refs {
		plugins = append(plugins, ref.Plugin)
	}
	return plugins
}

// pluginRefAuthEqual reports whether a and b declare the same per-plugin credential.
// Used to detect genuine conflicts when the same plugin is declared more than once
// (for example once in the main workflow and once through an import) with different
// github-token/github-app values.
func pluginRefAuthEqual(a, b PluginReference) bool {
	if a.GitHubToken != b.GitHubToken {
		return false
	}
	return reflect.DeepEqual(a.GitHubApp, b.GitHubApp)
}

// pluginRefHasAuth reports whether ref declares a per-plugin github-token or github-app
// credential.
func pluginRefHasAuth(ref PluginReference) bool {
	return ref.GitHubToken != "" || ref.GitHubApp != nil
}

// mergePluginRefAuth folds incoming's credential into target when the two declarations
// of the same plugin path need to be combined into a single merged entry. If target has
// no credential, it adopts incoming's. If both declare a credential, they must match,
// otherwise the generated checkout step could silently use the wrong (or no) token for
// this plugin depending on which duplicate declaration happened to be processed first.
func mergePluginRefAuth(target *PluginReference, incoming PluginReference) error {
	if !pluginRefHasAuth(incoming) {
		return nil
	}
	if !pluginRefHasAuth(*target) {
		target.GitHubToken = incoming.GitHubToken
		target.GitHubApp = incoming.GitHubApp
		return nil
	}
	if !pluginRefAuthEqual(*target, incoming) {
		return fmt.Errorf("plugin %q is declared with conflicting github-token/github-app credentials; use the same credentials for every declaration", parseSkillRefSpec(target.Plugin).repoPath)
	}
	return nil
}

func mergeValidatedPluginRefs(refs []PluginReference) ([]PluginReference, error) {
	merged := make([]PluginReference, 0, len(refs))
	indexByPath := make(map[string]int, len(refs))

	for _, ref := range refs {
		parsed := parseSkillRefSpec(ref.Plugin)
		index, exists := indexByPath[parsed.repoPath]
		if !exists {
			indexByPath[parsed.repoPath] = len(merged)
			merged = append(merged, ref)
			continue
		}

		existing := parseSkillRefSpec(merged[index].Plugin)
		if existing.ref == parsed.ref {
			if err := mergePluginRefAuth(&merged[index], ref); err != nil {
				return nil, err
			}
			continue
		}
		if !semverutil.IsValid(existing.ref) || !semverutil.IsValid(parsed.ref) {
			return nil, fmt.Errorf("plugin %q is declared with conflicting refs %q and %q; use the same ref for every declaration", parsed.repoPath, existing.ref, parsed.ref)
		}
		if !semverutil.IsCompatible(existing.ref, parsed.ref) {
			return nil, fmt.Errorf("plugin %q is declared with incompatible semantic versions %q and %q", parsed.repoPath, existing.ref, parsed.ref)
		}
		if err := mergePluginRefAuth(&merged[index], ref); err != nil {
			return nil, err
		}
		if semverutil.Compare(parsed.ref, existing.ref) > 0 {
			merged[index].Plugin = ref.Plugin
		}
	}

	return merged, nil
}

// validateFrontmatterPlugins validates the raw "plugins" frontmatter entries, allowing
// both legacy string entries and object entries with optional per-plugin github-token
// or github-app credentials (mutually exclusive). Basic spec-string shape (owner/repo@ref
// syntax, ref character set, path traversal, etc.) is validated later by validatePlugins
// once the workflow data has been assembled.
func validateFrontmatterPlugins(frontmatter map[string]any) error {
	rawPlugins, hasPlugins := frontmatter["plugins"]
	if !hasPlugins {
		return nil
	}

	plugins, ok := rawPlugins.([]any)
	if !ok {
		return errors.New("plugins must be an array of plugin references. Example: plugins: [\"owner/repo@sha\"]")
	}

	pluginInstallationLog.Printf("validateFrontmatterPlugins: validating %d plugin entr(ies)", len(plugins))

	for i, rawPlugin := range plugins {
		switch typed := rawPlugin.(type) {
		case string:
			// Full spec-string validation happens later in validatePlugins.
		case map[string]any:
			if len(typed) == 0 {
				return fmt.Errorf("plugins[%d] must include a non-empty plugin field. Example: plugins[%d]: {plugin: \"owner/repo@sha\"}", i, i)
			}
			pluginValue, hasPlugin := typed["plugin"]
			if !hasPlugin {
				return fmt.Errorf("plugins[%d].plugin is required. Example: plugins[%d].plugin: \"owner/repo@sha\"", i, i)
			}
			pluginSpec, ok := pluginValue.(string)
			if !ok {
				return fmt.Errorf("plugins[%d].plugin must be a string. Example: plugins[%d].plugin: \"owner/repo@sha\"", i, i)
			}
			if strings.TrimSpace(pluginSpec) == "" {
				return fmt.Errorf("plugins[%d].plugin must be a non-empty string. Example: plugins[%d].plugin: \"owner/repo@sha\"", i, i)
			}
			for key := range typed {
				switch key {
				case "plugin", "github-token", "github-app":
					// allowed
				default:
					return fmt.Errorf("plugins[%d].%s is not supported; allowed fields are plugin, github-token, github-app", i, key)
				}
			}
			_, hasToken := typed["github-token"]
			_, hasApp := typed["github-app"]
			if hasToken && hasApp {
				return fmt.Errorf("plugins[%d]: github-token and github-app are mutually exclusive; use one or the other", i)
			}
			if tokenValue, hasToken := typed["github-token"]; hasToken {
				token, ok := tokenValue.(string)
				if !ok {
					return fmt.Errorf("plugins[%d].github-token must be a string. Example: plugins[%d].github-token: \"${{ secrets.MY_TOKEN }}\"", i, i)
				}
				if !skillsGitHubTokenExpressionRegexp.MatchString(token) {
					return fmt.Errorf(
						"plugins[%d].github-token must be a valid GitHub token expression. Example: plugins[%d].github-token: \"${{ secrets.NAME }}\" or \"${{ needs.auth.outputs.token }}\"",
						i,
						i,
					)
				}
			}
			if app, hasApp := typed["github-app"]; hasApp {
				appMap, ok := app.(map[string]any)
				if !ok {
					return fmt.Errorf("plugins[%d].github-app must be an object. Example: plugins[%d].github-app: {client-id: \"Iv1.abc\", private-key: \"...\"}", i, i)
				}
				parsed := parseAppConfig(appMap)
				if !parsed.hasRequiredCredentials() {
					return fmt.Errorf("plugins[%d].github-app must include non-empty client-id/app-id and private-key. Example: plugins[%d].github-app: {client-id: \"Iv1.abc\", private-key: \"...\"}", i, i)
				}
			}
		default:
			return fmt.Errorf("plugins[%d] must be a string or object. Example: plugins[%d]: \"owner/repo@sha\" or {plugin: \"owner/repo@sha\"}", i, i)
		}
	}

	return nil
}

// parseRawPluginReferences converts raw "plugins" frontmatter entries (already validated
// by validateFrontmatterPlugins) into structured PluginReference values.
func parseRawPluginReferences(rawPlugins []any) []PluginReference {
	pluginInstallationLog.Printf("parseRawPluginReferences: parsing %d raw plugin entr(ies)", len(rawPlugins))
	refs := make([]PluginReference, 0, len(rawPlugins))
	for _, rawPlugin := range rawPlugins {
		switch typed := rawPlugin.(type) {
		case string:
			pluginSpec := strings.TrimSpace(typed)
			if pluginSpec == "" {
				continue
			}
			refs = append(refs, PluginReference{Plugin: pluginSpec})
		case map[string]any:
			pluginSpec, _ := typed["plugin"].(string)
			if strings.TrimSpace(pluginSpec) == "" {
				continue
			}
			ref := PluginReference{Plugin: strings.TrimSpace(pluginSpec)}
			if token, ok := typed["github-token"].(string); ok {
				ref.GitHubToken = token
			}
			if appMap, ok := typed["github-app"].(map[string]any); ok {
				ref.GitHubApp = parseAppConfig(appMap)
			}
			refs = append(refs, ref)
		}
	}
	if len(refs) == 0 {
		return nil
	}
	pluginInstallationLog.Printf("parseRawPluginReferences: parsed %d plugin reference(s)", len(refs))
	return refs
}

func (c *Compiler) validatePluginSupport(workflowData *WorkflowData) error {
	if workflowData == nil || len(workflowData.Plugins) == 0 {
		return nil
	}

	engine, err := c.getAgenticEngine(ResolveEngineID(workflowData))
	if err != nil {
		return err
	}
	if !engine.GetCapabilities().Plugins {
		return fmt.Errorf("plugins are not supported by engine %q; remove the plugins field or use an engine with Agent Plugins support. See: %s", engine.GetID(), constants.DocsEnginesURL)
	}
	return nil
}
