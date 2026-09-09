package workflow

import (
	"fmt"
	"math"
	"path"
	"regexp"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var pluginInstallationLog = logger.New("workflow:plugin_installation")

// pluginInstallSpec describes how an engine consumes checked-out Agent Plugins.
//
// Every engine with Agent Plugins support checks each plugin out at its pinned SHA.
// Engines whose CLI exposes a plugin installation command additionally run
// "<Command> <InstallArgs...> <plugin path>" for each plugin. Engines that discover
// plugins from a well-known workspace folder instead set Directory, and each plugin
// is staged into "<Directory>/<plugin name>". Engines whose installation flow does not
// fit either shape (for example one that requires resolving the plugin's own manifest
// name) instead set CustomInstall.
type pluginInstallSpec struct {
	// Command is the engine CLI executable used for CLI-based installation.
	Command string
	// InstallArgs are the CLI arguments placed before the local plugin path.
	InstallArgs []string
	// Directory is the folder the engine scans for plugins. It is either
	// workspace-relative (for example ".kiro/powers") or home-relative
	// (for example "~/.cursor/plugins/local").
	Directory string
	// CustomInstall, when set, replaces the Directory/Command handling above and is
	// invoked once per checked-out plugin to produce any additional installation steps.
	CustomInstall func(parsed parsedSkillRefSpec, checkoutPath, installPath string, index int) []GitHubActionStep
}

// pluginDirectoryRegexp restricts plugin staging directories to a safe character set so
// the generated shell commands cannot be manipulated through an engine definition.
var pluginDirectoryRegexp = regexp.MustCompile(`^(?:~/)?[A-Za-z0-9._-][A-Za-z0-9_./-]*$`)

// resolvePluginDirectory validates the declared plugin directory and expands a leading
// "~/" to "$HOME/" so the generated shell commands resolve it at runtime.
func resolvePluginDirectory(directory string) (string, bool) {
	if !pluginDirectoryRegexp.MatchString(directory) || strings.Contains(directory, "..") {
		return "", false
	}
	if rest, found := strings.CutPrefix(directory, "~/"); found {
		return "$HOME/" + rest, true
	}
	return directory, true
}

// pluginLocalPaths returns the workspace-relative directory of each checked-out plugin,
// in the same order as workflowData.Plugins. Invalid entries are skipped because they are
// rejected earlier by validatePlugins.
func pluginLocalPaths(workflowData *WorkflowData) []string {
	if workflowData == nil {
		return nil
	}
	paths := make([]string, 0, len(workflowData.Plugins))
	for i, plugin := range workflowData.Plugins {
		parsed := parseSkillRefSpec(plugin)
		if !parsed.isRemote || parsed.ref == "" {
			continue
		}
		paths = append(paths, pluginCheckoutSubpath(parsed, i))
	}
	return paths
}

// pluginCheckoutPath returns the workspace-relative checkout folder of the index-th plugin.
func pluginCheckoutPath(index int) string {
	return fmt.Sprintf(".gh-aw-plugins/plugin-%d", index)
}

// pluginCheckoutSubpath returns the workspace-relative path of the plugin directory
// inside its checkout folder.
func pluginCheckoutSubpath(parsed parsedSkillRefSpec, index int) string {
	repoParts := strings.Split(parsed.repoPath, "/")
	if len(repoParts) < 2 {
		// Guarded by validatePlugins, which requires an owner/repo path; this
		// is a defensive fallback in case that invariant ever changes.
		return pluginCheckoutPath(index)
	}
	return path.Join(pluginCheckoutPath(index), strings.Join(repoParts[2:], "/"))
}

// pluginRepoSubpath returns the path of the plugin directory relative to its checked-out
// repository root, in the "./..." form expected by tooling that references a plugin by a
// path relative to another root (for example a generated Codex marketplace entry). It
// returns "." when the plugin is the repository root itself.
func pluginRepoSubpath(parsed parsedSkillRefSpec) string {
	repoParts := strings.Split(parsed.repoPath, "/")
	if len(repoParts) <= 2 {
		return "."
	}
	return "./" + strings.Join(repoParts[2:], "/")
}

// pluginStagingName returns a collision-safe destination name for staging a
// plugin into a directory-based engine's plugin folder. It uses the full
// sub-path beneath "owner/repo" (joined with "__") so that two plugins with
// the same basename but different sub-paths (for example "org/a/plugins/foo"
// and "org/b/plugins/foo") stage to distinct destinations instead of one
// silently overwriting the other.
func pluginStagingName(parsed parsedSkillRefSpec, index int) string {
	repoParts := strings.Split(parsed.repoPath, "/")
	if len(repoParts) <= 2 {
		return fmt.Sprintf("plugin-%d-%s", index, strings.Join(repoParts, "__"))
	}
	return fmt.Sprintf("plugin-%d-%s", index, strings.Join(repoParts[2:], "__"))
}

// pluginAppTokenStepID returns the step ID used to mint (and later reference) the
// GitHub App installation token for the index-th plugin's github-app credential.
func pluginAppTokenStepID(index int) string {
	return fmt.Sprintf("plugin-app-token-%d", index)
}

// pluginTokenExpression resolves the checkout token expression for the index-th plugin,
// based on its optional per-plugin github-token/github-app credential. Returns an empty
// string when no credential is configured, in which case the checkout step omits the
// "token" input entirely and actions/checkout falls back to the workflow's default token.
func pluginTokenExpression(workflowData *WorkflowData, index int) string {
	if workflowData == nil || index < 0 || index >= len(workflowData.PluginReferences) {
		return ""
	}
	ref := workflowData.PluginReferences[index]
	if ref.GitHubApp != nil {
		token := fmt.Sprintf("${{ steps.%s.outputs.token }}", pluginAppTokenStepID(index))
		if ref.GitHubApp.shouldIgnoreMissingKey() {
			token = combineTokenExpressions(token, getEffectiveGitHubToken(""))
		}
		return token
	}
	return ref.GitHubToken
}

// generatePluginAuthTokenSteps mints GitHub App installation tokens for every plugin
// whose github-app credential is configured. Each minting step is emitted before the
// engine's plugin installation steps (which include the checkout step consuming the
// minted token), regardless of which agentic engine is selected: the checkout of a
// plugin's pinned commit is engine-agnostic and always happens before any
// engine-specific installation command runs.
func (c *Compiler) generatePluginAuthTokenSteps(workflowData *WorkflowData) []GitHubActionStep {
	if workflowData == nil || len(workflowData.PluginReferences) == 0 {
		return nil
	}

	var steps []GitHubActionStep
	for i, ref := range workflowData.PluginReferences {
		if ref.GitHubApp == nil {
			continue
		}
		repoParts := strings.Split(parseSkillRefSpec(ref.Plugin).repoPath, "/")
		if len(repoParts) < 2 {
			continue
		}
		lines := c.buildGitHubAppTokenMintStepWithMeta(
			ref.GitHubApp,
			nil,
			repoParts[1],
			strings.Join(repoParts[:2], "/"),
			fmt.Sprintf("Generate GitHub App token for agent plugin %d", i+1),
			pluginAppTokenStepID(i),
		)
		steps = append(steps, GitHubActionStep(strings.Split(strings.TrimSuffix(strings.Join(lines, ""), "\n"), "\n")))
	}
	return steps
}

func generatePluginInstallationSteps(workflowData *WorkflowData, spec pluginInstallSpec) []GitHubActionStep {
	if workflowData == nil || len(workflowData.Plugins) == 0 {
		return nil
	}

	steps := make([]GitHubActionStep, 0, pluginInstallationStepCapacity(len(workflowData.Plugins)))
	checkoutAction := getActionPinForData("actions/checkout", workflowData)
	for i, plugin := range workflowData.Plugins {
		parsed := parseSkillRefSpec(plugin)
		if !parsed.isRemote || parsed.ref == "" {
			pluginInstallationLog.Printf("Skipping invalid plugin reference after validation: %q", plugin)
			continue
		}

		repoParts := strings.Split(parsed.repoPath, "/")
		if len(repoParts) < 2 {
			pluginInstallationLog.Printf("Skipping malformed plugin path %q", parsed.repoPath)
			continue
		}
		repository := strings.Join(repoParts[:2], "/")
		checkoutPath := pluginCheckoutPath(i)
		installPath := pluginCheckoutSubpath(parsed, i)

		steps = append(steps, newPluginCheckoutStep(workflowData, checkoutAction, parsed, repository, checkoutPath, i))

		if spec.CustomInstall != nil {
			steps = append(steps, spec.CustomInstall(parsed, checkoutPath, installPath, i)...)
			continue
		}

		if spec.Directory != "" {
			stageDirectory, ok := resolvePluginDirectory(spec.Directory)
			if !ok {
				pluginInstallationLog.Printf("Skipping unsupported plugin directory: %q", spec.Directory)
			} else {
				targetPath := path.Join(stageDirectory, pluginStagingName(parsed, i))
				stageCommand := strings.Join([]string{
					fmt.Sprintf("mkdir -p %q", stageDirectory),
					fmt.Sprintf("rm -rf %q", targetPath),
					fmt.Sprintf("cp -R %q %q", "./"+installPath, targetPath),
				}, "\n")
				stageStep := []string{"      - name: Stage agent plugin " + parsed.repoPath}
				steps = append(steps, FormatStepWithCommandAndEnv(stageStep, stageCommand, nil))
			}
		}

		if spec.Command != "" && len(spec.InstallArgs) > 0 {
			installArgs := make([]string, 0, len(spec.InstallArgs)+2)
			installArgs = append(installArgs, spec.Command)
			installArgs = append(installArgs, spec.InstallArgs...)
			installArgs = append(installArgs, "./"+installPath)
			installCommand := shellJoinArgs(installArgs)
			installStep := []string{"      - name: Install agent plugin " + parsed.repoPath}
			steps = append(steps, FormatStepWithCommandAndEnv(installStep, installCommand, nil))
		}
	}

	return steps
}

func pluginInstallationStepCapacity(pluginCount int) int {
	if pluginCount <= math.MaxInt/2 {
		return pluginCount * 2
	}
	return pluginCount
}

func newPluginCheckoutStep(workflowData *WorkflowData, checkoutAction string, parsed parsedSkillRefSpec, repository, checkoutPath string, index int) GitHubActionStep {
	step := GitHubActionStep{
		"      - name: Checkout agent plugin " + parsed.repoPath,
		"        uses: " + checkoutAction,
		"        with:",
		"          repository: " + repository,
		"          ref: " + parsed.ref,
		"          path: " + checkoutPath,
		"          persist-credentials: false",
	}
	if tokenExpr := pluginTokenExpression(workflowData, index); tokenExpr != "" {
		step = append(step, "          token: "+tokenExpr)
	}
	return step
}
