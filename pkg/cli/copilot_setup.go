package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/goccy/go-yaml"

	"github.com/github/gh-aw/pkg/actionpins"
	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/fileutil"
	"github.com/github/gh-aw/pkg/gitutil"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/workflow"
)

var copilotSetupLog = logger.New("cli:copilot_setup")

const installScriptTempVariable = "$install_script"

// copilotSetupStepsStaticSHA is the pinned commit SHA of install-gh-aw.sh used in the static
// YAML test template and as the fallback when ResolveGhAwRef is unavailable.
// Run scripts/update-install-script-hashes.sh to refresh both this value and copilotSetupStepsStaticSHA256.
const copilotSetupStepsStaticSHA = "4c53fac4c30c2d9f27ea6fab5e5ce0f15a21e78e"

// copilotSetupStepsStaticSHA256 is the SHA256 hex digest of install-gh-aw.sh at copilotSetupStepsStaticSHA.
// Run scripts/update-install-script-hashes.sh to refresh both this value and copilotSetupStepsStaticSHA.
const copilotSetupStepsStaticSHA256 = "248ccebcb998c6a506548156e1bf9f02429cbbaec407d5adbdfd316ab0f866a0"

// sha256HexRegex matches a valid lowercase SHA256 hex digest (exactly 64 hex chars).
var sha256HexRegex = regexp.MustCompile(`^[0-9a-f]{64}$`)

var (
	resolveGhAwDefaultBranchForCopilotSetup   = getRepoDefaultBranchCached
	resolveGhAwRefForCopilotSetup             = workflow.ResolveGhAwRef
	resolveInstallScriptSHA256ForCopilotSetup = resolveInstallScriptSHA256
)

func latestCheckoutActionRef() string {
	return actionpins.ResolveLatestActionPin("actions/checkout", nil)
}

// resolveInstallScriptSHA256 fetches install-gh-aw.sh at the given immutable commit SHA
// and returns its SHA256 hex digest for use in a sha256sum integrity check.
// Returns an empty string and logs a warning if the fetch or computation fails.
func resolveInstallScriptSHA256(ctx context.Context, commitSHA string) string {
	if !gitutil.IsValidFullSHA(commitSHA) {
		copilotSetupLog.Printf("resolveInstallScriptSHA256: commitSHA %q is not a valid full SHA, skipping", commitSHA)
		return ""
	}
	scriptURL := fmt.Sprintf("https://raw.githubusercontent.com/github/gh-aw/%s/install-gh-aw.sh", commitSHA)
	res, err := FetchImportURL(ctx, scriptURL, FetchOptions{})
	if err != nil {
		copilotSetupLog.Printf("Could not fetch install-gh-aw.sh from %s to compute SHA256: %v", scriptURL, err)
		return ""
	}
	h := sha256.Sum256(res.Body)
	return hex.EncodeToString(h[:])
}

// sha256CheckLine returns a YAML-indented shell command (with trailing newline) that verifies
// digest against path using sha256sum. Returns an empty string if either parameter is invalid.
// digest must be a 64-char lowercase hex string (sha256HexRegex); path must contain only
// safe filesystem characters or be the generated install_script shell variable.
func sha256CheckLine(digest, path string) string {
	if !sha256HexRegex.MatchString(digest) {
		return ""
	}
	if path == "" || (path != installScriptTempVariable && strings.ContainsAny(path, " \t\n\"'\\$`;&|<>(){}*?")) {
		copilotSetupLog.Printf("sha256CheckLine: unsafe path %q, skipping", path)
		return ""
	}
	return fmt.Sprintf(`          printf '%%s  %%s\n' %q %q | sha256sum -c -`+"\n", digest, path)
}

// If a resolver is provided and mode is release or action, attempts to resolve the SHA for a SHA-pinned reference.
// Falls back to a version tag reference if SHA resolution fails or resolver is nil.
func getActionRef(ctx context.Context, actionMode workflow.ActionMode, version string, resolver workflow.SHAResolver) string {
	if actionMode.IsRelease() && version != "" && version != "dev" {
		if resolver != nil {
			sha, err := resolver.ResolveSHA(ctx, "github/gh-aw-actions/setup-cli", version)
			if err == nil && sha != "" {
				return fmt.Sprintf("@%s # %s", sha, version)
			}
			copilotSetupLog.Printf("Failed to resolve SHA for setup-cli@%s: %v, falling back to version tag", version, err)
		}
		return "@" + version
	}
	if actionMode.IsAction() && version != "" && version != "dev" {
		if resolver != nil {
			sha, err := resolver.ResolveSHA(ctx, "github/gh-aw-actions/setup-cli", version)
			if err == nil && sha != "" {
				return fmt.Sprintf("@%s # %s", sha, version)
			}
			copilotSetupLog.Printf("Failed to resolve SHA for gh-aw-actions/setup-cli@%s: %v, falling back to version tag", version, err)
		}
		return "@" + version
	}
	return "@main"
}

const copilotSetupActionTemplate = `name: "Copilot Setup Steps"

# This workflow configures the environment for GitHub Copilot Agent with gh-aw MCP server
on:
  workflow_dispatch:
  push:
    paths:
      - .github/workflows/copilot-setup-steps.yml

permissions:
  contents: read

concurrency:
  group: ${{ github.workflow }}-${{ github.ref }}
  cancel-in-progress: true

jobs:
  # The job MUST be called 'copilot-setup-steps' to be recognized by GitHub Copilot Agent
  copilot-setup-steps:
    name: Copilot Setup Steps
    runs-on: ubuntu-latest

    # Set minimal permissions for setup steps
    # Copilot Agent receives its own token with appropriate permissions
    permissions:
      contents: read

    steps:
      - name: Checkout repository
        uses: %s
        with:
          persist-credentials: false
      - name: Install gh-aw extension
        uses: %s%s
        with:
          version: %s
`

const copilotSetupScriptTemplate = `name: "Copilot Setup Steps"

# This workflow configures the environment for GitHub Copilot Agent with gh-aw MCP server
on:
  workflow_dispatch:
  push:
    paths:
      - .github/workflows/copilot-setup-steps.yml

permissions:
  contents: read

concurrency:
  group: ${{ github.workflow }}-${{ github.ref }}
  cancel-in-progress: true

jobs:
  # The job MUST be called 'copilot-setup-steps' to be recognized by GitHub Copilot Agent
  copilot-setup-steps:
    name: Copilot Setup Steps
    runs-on: ubuntu-latest

    # Set minimal permissions for setup steps
    # Copilot Agent receives its own token with appropriate permissions
    permissions:
      contents: read

    steps:
      - name: Install gh-aw extension
        run: |
         install_script="$(mktemp)"
         trap 'rm -f "$install_script"' EXIT
         curl -fsSL https://raw.githubusercontent.com/github/gh-aw/%s/install-gh-aw.sh -o "$install_script"
%s          bash "$install_script"
`

// generateCopilotSetupStepsYAML generates the copilot-setup-steps.yml content based on action mode
func generateCopilotSetupStepsYAML(ctx context.Context, actionMode workflow.ActionMode, version string, resolver workflow.SHAResolver) string {
	// Determine the action reference - use SHA-pinned or version tag in release/action mode, @main in dev mode
	actionRef := getActionRef(ctx, actionMode, version, resolver)
	checkoutRef := latestCheckoutActionRef()

	if actionMode.IsRelease() || actionMode.IsAction() {
		// Determine the action repo based on mode
		actionRepo := "github/gh-aw-actions/setup-cli"
		return fmt.Sprintf(copilotSetupActionTemplate, checkoutRef, actionRepo, actionRef, version)
	}

	// Default (dev/script mode): resolve the repository default branch via the
	// GitHub API, then pin it to a SHA so the downloaded script is immutable.
	// Fall back to the known immutable SHA and checksum if resolution fails.
	defaultBranch := "main"
	if branch, err := resolveGhAwDefaultBranchForCopilotSetup(ctx, "github/gh-aw"); err != nil {
		copilotSetupLog.Printf("Could not resolve github/gh-aw default branch for dev-mode template, falling back to %q: %v", defaultBranch, err)
	} else if branch = strings.TrimSpace(branch); branch != "" {
		defaultBranch = branch
	} else {
		copilotSetupLog.Printf("Could not resolve github/gh-aw default branch for dev-mode template: empty branch returned, falling back to %q", defaultBranch)
	}
	installRef := copilotSetupStepsStaticSHA
	installSHA256 := copilotSetupStepsStaticSHA256
	if sha, err := resolveGhAwRefForCopilotSetup(ctx, defaultBranch); err == nil && gitutil.IsValidFullSHA(sha) {
		// Fetch the script to compute an explicit SHA256 integrity check line.
		if digest := resolveInstallScriptSHA256ForCopilotSetup(ctx, sha); sha256HexRegex.MatchString(digest) {
			installRef = sha
			installSHA256 = digest
		} else {
			copilotSetupLog.Printf("Could not resolve install-gh-aw.sh SHA256 at %s; using the static pinned installer", sha)
		}
	} else {
		copilotSetupLog.Printf("Could not resolve github/gh-aw %s SHA for dev-mode template, using the static pinned installer: %v", defaultBranch, err)
	}
	sha256Cmd := sha256CheckLine(installSHA256, installScriptTempVariable)
	return fmt.Sprintf(copilotSetupScriptTemplate, installRef, sha256Cmd)
}

// copilotSetupStepsYAML is a static dev-mode template used only for YAML validity tests.
// It is built from copilotSetupStepsStaticSHA and copilotSetupStepsStaticSHA256 so that
// scripts/update-install-script-hashes.sh can refresh both values in a single place.
// The runtime function generateCopilotSetupStepsYAML resolves the ref dynamically
// from the repository default branch via the GitHub API, falling back to the static pin.
var copilotSetupStepsYAML = fmt.Sprintf(
	copilotSetupScriptTemplate,
	copilotSetupStepsStaticSHA,
	sha256CheckLine(copilotSetupStepsStaticSHA256, installScriptTempVariable),
)

// copilotSetupStepsJobName is the job name GitHub Copilot coding agent looks for in
// .github/workflows/copilot-setup-steps.yml. When it is missing (or the workflow cannot
// be parsed) Copilot reports "no supported setup-steps job found in your workflow file".
const copilotSetupStepsJobName = "copilot-setup-steps"

// copilotSetupStepsSupportedTriggers are the workflow triggers Copilot supports for the
// setup steps workflow. A workflow that only declares other triggers (for example
// workflow_call) is not usable as copilot setup steps.
var copilotSetupStepsSupportedTriggers = []string{"workflow_dispatch", "push", "pull_request"}

// validateCopilotSetupStepsContent checks that content is a syntactically valid GitHub
// Actions workflow that GitHub Copilot coding agent can use as setup steps: it must parse
// as YAML, declare a supported trigger, and define a 'copilot-setup-steps' job with inline
// steps (reusable workflow calls are not supported).
func validateCopilotSetupStepsContent(content []byte) error {
	var doc map[string]any
	if err := yaml.Unmarshal(content, &doc); err != nil {
		return fmt.Errorf("invalid YAML: %w", err)
	}
	if len(doc) == 0 {
		return errors.New("workflow is empty")
	}

	if err := validateCopilotSetupStepsTriggers(doc); err != nil {
		return err
	}

	jobsValue, ok := doc["jobs"]
	if !ok {
		return errors.New("missing 'jobs' section")
	}
	jobs, ok := jobsValue.(map[string]any)
	if !ok {
		return errors.New("'jobs' section is not a map")
	}
	jobValue, ok := jobs[copilotSetupStepsJobName]
	if !ok {
		return fmt.Errorf("missing '%s' job", copilotSetupStepsJobName)
	}
	job, ok := jobValue.(map[string]any)
	if !ok {
		return fmt.Errorf("'%s' job is not a map", copilotSetupStepsJobName)
	}
	if _, ok := job["uses"]; ok {
		return fmt.Errorf("'%s' job must define inline steps, reusable workflow calls ('uses') are not supported", copilotSetupStepsJobName)
	}
	if err := validateCopilotSetupStepsRunsOn(job); err != nil {
		return err
	}
	steps, ok := job["steps"].([]any)
	if !ok || len(steps) == 0 {
		return fmt.Errorf("'%s' job has no steps", copilotSetupStepsJobName)
	}
	for i, stepValue := range steps {
		step, ok := stepValue.(map[string]any)
		if !ok {
			return fmt.Errorf("'%s' job step %d is not a map", copilotSetupStepsJobName, i+1)
		}
		if !hasNonEmptyStringField(step, "run") && !hasNonEmptyStringField(step, "uses") {
			return fmt.Errorf("'%s' job step %d must define 'run' or 'uses'", copilotSetupStepsJobName, i+1)
		}
	}
	return nil
}

// validateCopilotSetupStepsRunsOn verifies the job declares a runner. A null or empty
// 'runs-on' parses as YAML but does not define an executable Actions job.
func validateCopilotSetupStepsRunsOn(job map[string]any) error {
	runsOnValue, ok := job["runs-on"]
	if !ok {
		return fmt.Errorf("'%s' job is missing 'runs-on'", copilotSetupStepsJobName)
	}
	switch typed := runsOnValue.(type) {
	case string:
		if strings.TrimSpace(typed) != "" {
			return nil
		}
	case []any:
		for _, item := range typed {
			if label, ok := item.(string); ok && strings.TrimSpace(label) != "" {
				return nil
			}
		}
	case map[string]any:
		// runs-on group/labels object syntax
		if hasNonEmptyStringField(typed, "group") {
			return nil
		}
		if labels, ok := typed["labels"]; ok {
			if label, ok := labels.(string); ok && strings.TrimSpace(label) != "" {
				return nil
			}
			if list, ok := labels.([]any); ok {
				for _, item := range list {
					if label, ok := item.(string); ok && strings.TrimSpace(label) != "" {
						return nil
					}
				}
			}
		}
	}
	return fmt.Errorf("'%s' job has an empty 'runs-on'", copilotSetupStepsJobName)
}

// hasNonEmptyStringField reports whether the map contains key with a non-blank string value.
func hasNonEmptyStringField(m map[string]any, key string) bool {
	value, ok := m[key].(string)
	return ok && strings.TrimSpace(value) != ""
}

// validateCopilotSetupStepsTriggers verifies the workflow declares at least one trigger
// supported by Copilot.
func validateCopilotSetupStepsTriggers(doc map[string]any) error {
	onValue, ok := doc["on"]
	if !ok {
		return errors.New("missing 'on' section")
	}

	var triggers []string
	switch typed := onValue.(type) {
	case string:
		triggers = []string{typed}
	case []any:
		for _, item := range typed {
			if name, ok := item.(string); ok {
				triggers = append(triggers, name)
			}
		}
	case map[string]any:
		for name := range typed {
			triggers = append(triggers, name)
		}
	default:
		return errors.New("'on' section has an unexpected format")
	}

	for _, trigger := range triggers {
		if slices.Contains(copilotSetupStepsSupportedTriggers, trigger) {
			return nil
		}
	}
	return fmt.Errorf("'on' section must include one of %s", strings.Join(copilotSetupStepsSupportedTriggers, ", "))
}

// ensureCopilotSetupSteps creates or updates .github/workflows/copilot-setup-steps.yml
func ensureCopilotSetupSteps(ctx context.Context, verbose bool, actionMode workflow.ActionMode, version string) error {
	return ensureCopilotSetupStepsWithUpgrade(ctx, verbose, actionMode, version, false)
}

// upgradeCopilotSetupSteps upgrades the version in existing copilot-setup-steps.yml
func upgradeCopilotSetupSteps(ctx context.Context, verbose bool, actionMode workflow.ActionMode, version string) error {
	return ensureCopilotSetupStepsWithUpgrade(ctx, verbose, actionMode, version, true)
}

// ensureCopilotSetupStepsWithUpgrade creates .github/workflows/copilot-setup-steps.yml
// If the file already exists, it renders console instructions instead of editing
// When upgradeVersion is true and called from upgrade command, this is a special case
func ensureCopilotSetupStepsWithUpgrade(ctx context.Context, verbose bool, actionMode workflow.ActionMode, version string, upgradeVersion bool) error {
	copilotSetupLog.Printf("Creating copilot-setup-steps.yml with action mode: %s, version: %s, upgradeVersion: %v", actionMode, version, upgradeVersion)

	// Create a SHA resolver for release/action mode to enable SHA-pinned action references
	var resolver workflow.SHAResolver
	if actionMode.IsRelease() || actionMode.IsAction() {
		cache := workflow.NewActionCache(".")
		_ = cache.Load() // Ignore errors if cache doesn't exist yet
		resolver = workflow.NewActionResolver(cache)
	}

	workflowsDir := constants.GetWorkflowDir()
	setupStepsPath := filepath.Join(workflowsDir, "copilot-setup-steps.yml")
	if err := fileutil.EnsureParentDir(setupStepsPath, constants.DirPermPublic); err != nil {
		return fmt.Errorf("failed to create workflows directory: %w", err)
	}
	copilotSetupLog.Printf("Ensured directory exists: %s", workflowsDir)

	// Check if file already exists
	if _, err := os.Stat(setupStepsPath); err == nil {
		return handleExistingCopilotSetupSteps(ctx, verbose, actionMode, version, resolver, setupStepsPath, upgradeVersion)
	}

	// File doesn't exist - create it
	generated := generateCopilotSetupStepsYAML(ctx, actionMode, version, resolver)
	if err := validateCopilotSetupStepsContent([]byte(generated)); err != nil {
		return fmt.Errorf("generated copilot-setup-steps.yml is not valid: %w", err)
	}
	if err := os.WriteFile(setupStepsPath, []byte(generated), constants.FilePermSensitive); err != nil {
		return fmt.Errorf("failed to write copilot-setup-steps.yml: %w", err)
	}
	copilotSetupLog.Printf("Created file: %s", setupStepsPath)

	return nil
}

func handleExistingCopilotSetupSteps(ctx context.Context, verbose bool, actionMode workflow.ActionMode, version string, resolver workflow.SHAResolver, setupStepsPath string, upgradeVersion bool) error {
	copilotSetupLog.Printf("File already exists: %s", setupStepsPath)

	content, err := os.ReadFile(setupStepsPath)
	if err != nil {
		return fmt.Errorf("failed to read existing copilot-setup-steps.yml: %w", err)
	}

	warnIfCopilotSetupStepsInvalid(setupStepsPath, content)
	contentStr := string(content)
	hasLegacyInstall := strings.Contains(contentStr, "install-gh-aw.sh") ||
		(strings.Contains(contentStr, "Install gh-aw extension") && strings.Contains(contentStr, "curl -fsSL"))
	hasActionInstall := strings.Contains(contentStr, "actions/setup-cli")

	if (hasLegacyInstall || hasActionInstall) && upgradeVersion {
		return upgradeExistingCopilotSetupSteps(ctx, verbose, actionMode, version, resolver, setupStepsPath, content)
	}

	if hasLegacyInstall || hasActionInstall {
		copilotSetupLog.Print("Extension install step already exists, file is up to date")
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessageStderr(fmt.Sprintf("Skipping %s (already has gh-aw extension install step)", setupStepsPath)))
		}
		return nil
	}

	copilotSetupLog.Print("File exists without install step, rendering update instructions instead of editing")
	renderCopilotSetupUpdateInstructions(ctx, setupStepsPath, actionMode, version, resolver)
	return nil
}

func upgradeExistingCopilotSetupSteps(ctx context.Context, verbose bool, actionMode workflow.ActionMode, version string, resolver workflow.SHAResolver, setupStepsPath string, content []byte) error {
	copilotSetupLog.Print("Extension install step exists, attempting version upgrade (upgrade command)")

	upgraded, updatedContent, err := upgradeSetupCliVersionInContent(ctx, content, actionMode, version, resolver)
	if err != nil {
		return fmt.Errorf("failed to upgrade setup-cli version: %w", err)
	}
	if !upgraded {
		copilotSetupLog.Print("No version upgrade needed")
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessageStderr("No version upgrade needed for "+setupStepsPath))
		}
		return nil
	}
	if err := validateCopilotSetupStepsContent(updatedContent); err != nil {
		return fmt.Errorf("upgraded copilot-setup-steps.yml is not valid: %w", err)
	}
	if err := os.WriteFile(setupStepsPath, updatedContent, constants.FilePermSensitive); err != nil {
		return fmt.Errorf("failed to update copilot-setup-steps.yml: %w", err)
	}
	copilotSetupLog.Printf("Upgraded version in file: %s", setupStepsPath)
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessageStderr(fmt.Sprintf("Updated %s with new version %s", setupStepsPath, version)))
	}
	return nil
}

// warnIfCopilotSetupStepsInvalid prints a warning when an existing copilot-setup-steps.yml
// cannot be used by GitHub Copilot coding agent, which otherwise fails at runtime with
// "no supported setup-steps job found in your workflow file".
func warnIfCopilotSetupStepsInvalid(filePath string, content []byte) {
	err := validateCopilotSetupStepsContent(content)
	if err == nil {
		return
	}
	copilotSetupLog.Printf("Existing %s is not a valid Copilot setup steps workflow: %v", filePath, err)
	fmt.Fprintln(os.Stderr, console.FormatWarningMessageStderr(fmt.Sprintf(
		"%s is not usable by GitHub Copilot coding agent: %v", filePath, err)))
}

// renderCopilotSetupUpdateInstructions renders console instructions for updating copilot-setup-steps.yml
func renderCopilotSetupUpdateInstructions(ctx context.Context, filePath string, actionMode workflow.ActionMode, version string, resolver workflow.SHAResolver) {
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, console.FormatInfoMessageStderr("Existing file detected: "+filePath))
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "To enable GitHub Copilot Agent integration, please add the following steps")
	fmt.Fprintln(os.Stderr, "to the 'copilot-setup-steps' job in your .github/workflows/copilot-setup-steps.yml file:")
	fmt.Fprintln(os.Stderr)

	// Determine the action reference
	actionRef := getActionRef(ctx, actionMode, version, resolver)

	if actionMode.IsRelease() || actionMode.IsAction() {
		actionRepo := "github/gh-aw-actions/setup-cli"
		fmt.Fprintln(os.Stderr, "      - name: Checkout repository")
		fmt.Fprintln(os.Stderr, "        uses: "+latestCheckoutActionRef())
		fmt.Fprintln(os.Stderr, "        with:")
		fmt.Fprintln(os.Stderr, "          persist-credentials: false")
		fmt.Fprintln(os.Stderr, "      - name: Install gh-aw extension")
		fmt.Fprintln(os.Stderr, "        uses: "+actionRepo+actionRef)
		fmt.Fprintln(os.Stderr, "        with:")
		fmt.Fprintln(os.Stderr, "          version: "+version)
	} else {
		// Dev/script mode: try to resolve main to a pinned SHA and checksum; fall back to
		// the known immutable SHA and checksum if resolution is unavailable.
		installRef := copilotSetupStepsStaticSHA
		installSHA256 := copilotSetupStepsStaticSHA256
		if sha, err := workflow.ResolveGhAwRef(ctx, "main"); err == nil && gitutil.IsValidFullSHA(sha) {
			if digest := resolveInstallScriptSHA256(ctx, sha); sha256HexRegex.MatchString(digest) {
				installRef = sha
				installSHA256 = digest
			} else {
				copilotSetupLog.Printf("Could not resolve install-gh-aw.sh SHA256 at %s; using the static pinned installer", sha)
			}
		} else {
			copilotSetupLog.Printf("Could not resolve github/gh-aw main SHA for install instructions, using the static pinned installer: %v", err)
		}
		fmt.Fprintln(os.Stderr, "      - name: Install gh-aw extension")
		fmt.Fprintln(os.Stderr, "        run: |")
		fmt.Fprintln(os.Stderr, "          install_script=\"$(mktemp)\"")
		fmt.Fprintln(os.Stderr, `          trap 'rm -f "$install_script"' EXIT`)
		fmt.Fprintln(os.Stderr, `          curl -fsSL https://raw.githubusercontent.com/github/gh-aw/`+installRef+`/install-gh-aw.sh -o "$install_script"`)
		if line := sha256CheckLine(installSHA256, installScriptTempVariable); line != "" {
			fmt.Fprint(os.Stderr, line) // sha256CheckLine includes trailing newline
		}
		fmt.Fprintln(os.Stderr, `          bash "$install_script"`)
	}
	fmt.Fprintln(os.Stderr)
}

// setupCliUsesPattern matches the uses: line for either github/gh-aw/actions/setup-cli
// or github/gh-aw-actions/setup-cli.
// It handles unquoted version-tag refs, unquoted SHA-pinned refs (with trailing comment),
// and quoted refs produced by some YAML marshalers (e.g. "...@sha # vX.Y.Z").
var setupCliUsesPattern = regexp.MustCompile(
	`(?m)^(\s+uses:[ \t]*)"?(github/gh-aw(?:-actions)?/(?:actions/)?setup-cli@[^"\n]*)"?([ \t]*)$`)

var checkoutUsesLinePattern = regexp.MustCompile(`^([ \t]*)uses:[ \t]*"?actions/checkout@[^"\n]*"?[ \t]*\r?$`)

// versionInWithPattern matches the version: parameter in the with: block that immediately
// follows any setup-cli uses: line (any ref format: version tag, SHA-pinned, or quoted).
// It is anchored to the same action repos as setupCliUsesPattern so that it only updates
// the version belonging to the setup-cli step, but is independent of the exact ref value.
// This allows it to correct pre-existing drift where the uses: comment and with: version:
// were already out of sync before the upgrade was run.
//
// Pattern breakdown:
//
//	[ \t]+uses:[ \t]*         — indented uses: key with optional surrounding spaces
//	"?github/gh-aw(?:-actions)?/(?:actions/)?setup-cli@[^"\n]*"?
//	                           — any setup-cli ref (version tag, SHA+comment, or quoted)
//	[^\n]*\n                   — rest of the uses: line (e.g. trailing spaces)
//	(?:[^\n]*\n)*?             — zero or more lines between uses: and with: (non-greedy)
//	[ \t]+with:[ \t]*\n        — indented with: key
//	(?:[^\n]*\n)*?             — zero or more lines between with: and version: (non-greedy)
//	[ \t]+version:[ \t]*       — indented version: key (final part of the prefix captured as group 1)
//	(\S+)                      — the version value (captured as group 2)
//	([ \t]*(?:\n|$))           — trailing whitespace and line terminator (captured as group 3)
//
// Note: In the full pattern, group 1 wraps the entire prefix from the setup-cli `uses:` line
// through the `version:` key (and following spaces), group 2 is just the version value, and
// group 3 is the trailing whitespace plus the line terminator.
var versionInWithPattern = regexp.MustCompile(
	`(?s)([ \t]+uses:[ \t]*"?github/gh-aw(?:-actions)?/(?:actions/)?setup-cli@[^"\n]*"?[^\n]*\n(?:[^\n]*\n)*?[ \t]+with:[ \t]*\n(?:[^\n]*\n)*?[ \t]+version:[ \t]*)(\S+)([ \t]*(?:\n|$))`)

// upgradeSetupCliVersionInContent replaces the setup-cli action reference, any
// actions/checkout reference, and the associated version: parameter in the raw
// YAML content using targeted regex substitutions, preserving all other
// formatting in the file.
//
// Returns (upgraded, updatedContent, error).  upgraded is false when no change
// was required (e.g. already at the target version, or file has no setup-cli step).
func upgradeSetupCliVersionInContent(ctx context.Context, content []byte, actionMode workflow.ActionMode, version string, resolver workflow.SHAResolver) (bool, []byte, error) {
	if !actionMode.IsRelease() && !actionMode.IsAction() {
		return false, content, nil
	}

	if !setupCliUsesPattern.Match(content) {
		return false, content, nil
	}

	actionRef := getActionRef(ctx, actionMode, version, resolver)
	actionRepo := "github/gh-aw-actions/setup-cli"
	newUses := actionRepo + actionRef

	// Replace the uses: line, stripping any surrounding quotes in the process.
	updated := setupCliUsesPattern.ReplaceAll(content, []byte("${1}"+newUses+"${3}"))
	updated, checkoutUpdated := pinCheckoutUsesInContent(updated)

	// Replace the version: value in the with: block immediately following the
	// setup-cli uses: line.  versionInWithPattern matches any valid setup-cli
	// reference so it succeeds even when there was pre-existing drift between
	// the uses: comment and the version: parameter before the upgrade was run.
	updated = versionInWithPattern.ReplaceAll(updated, []byte("${1}"+version+"${3}"))

	if !checkoutUpdated && bytes.Equal(content, updated) {
		return false, content, nil
	}
	return true, updated, nil
}

func pinCheckoutUsesInContent(content []byte) ([]byte, bool) {
	contentStr := string(content)
	hadTrailingNewline := strings.HasSuffix(contentStr, "\n")
	lines := strings.Split(strings.TrimSuffix(contentStr, "\n"), "\n")
	checkoutRef := latestCheckoutActionRef()
	changed := false

	for i := 0; i < len(lines); i++ {
		matches := checkoutUsesLinePattern.FindStringSubmatch(lines[i])
		if matches == nil {
			continue
		}

		indent := matches[1]
		lines[i] = indent + "uses: " + checkoutRef
		changed = true

		if i+1 < len(lines) && strings.HasPrefix(lines[i+1], indent+"with:") {
			blockEnd := checkoutWithBlockEnd(lines, i+2, indent)
			if !checkoutWithBlockHasPersistCredentials(lines, i+2, blockEnd) {
				insert := indent + "  persist-credentials: false"
				lines = slices.Insert(lines, i+2, insert)
				blockEnd++
			}
			i = blockEnd - 1
			continue
		}

		lines = slices.Insert(lines, i+1,
			indent+"with:",
			indent+"  persist-credentials: false",
		)
		i += 2
	}

	if !changed {
		return content, false
	}
	result := strings.Join(lines, "\n")
	if hadTrailingNewline {
		result += "\n"
	}
	return []byte(result), true
}

func checkoutWithBlockEnd(lines []string, start int, usesIndent string) int {
	for i := start; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "" {
			if checkoutNextNonBlankLineInBlock(lines, i+1, usesIndent) {
				continue
			}
			return i
		}
		if !strings.HasPrefix(lines[i], usesIndent+"  ") {
			return i
		}
	}
	return len(lines)
}

func checkoutNextNonBlankLineInBlock(lines []string, start int, usesIndent string) bool {
	for i := start; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "" {
			continue
		}
		return strings.HasPrefix(lines[i], usesIndent+"  ")
	}
	return false
}

func checkoutWithBlockHasPersistCredentials(lines []string, start int, end int) bool {
	for i := start; i < end; i++ {
		trimmed := strings.TrimSpace(lines[i])
		if strings.HasPrefix(trimmed, "persist-credentials:") {
			return true
		}
	}
	return false
}
