package workflow

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
)

// generateRuntimeAndWorkspaceSetupSteps emits runtime setup steps, the gh-aw temp directory
// creation step, GitHub Enterprise CLI configuration, DIFC proxy start, activation artifact
// download, comment-memory file preparation, cache-memory steps, repo-memory steps, the user's
// custom steps, and cache steps.
// Memory restore steps (comment-memory, cache-memory, repo-memory) intentionally run before
// custom steps so that deterministic steps: code can read prior state without requiring an LLM
// turn.
// It mutates data.CustomSteps (via deduplication) and returns whether the custom steps
// themselves contain a checkout action (used by the caller to compute needsGitConfig).
func (c *Compiler) generateRuntimeAndWorkspaceSetupSteps(yaml *strings.Builder, data *WorkflowData, needsCheckout bool) bool {
	runtimeSetupSteps, customStepsContainCheckout := c.prepareRuntimeSetupAndCheckoutInfo(data)
	compilerYamlLog.Printf("Custom steps contain checkout: %t (len(customSteps)=%d)", customStepsContainCheckout, len(data.CustomSteps))

	c.emitRuntimeSetupPrelude(yaml, data, needsCheckout, customStepsContainCheckout, runtimeSetupSteps)

	// Create /tmp/gh-aw/ base directory for all temporary files
	// This must be created before custom steps so they can use the temp directory
	yaml.WriteString("      - name: Create gh-aw temp directory\n")
	yaml.WriteString("        run: bash \"${RUNNER_TEMP}/gh-aw/actions/create_gh_aw_tmp_dir.sh\"\n")

	// Configure gh CLI for GitHub Enterprise hosts (*.ghe.com / GHES).
	// This step runs configure_gh_for_ghe.sh which:
	//   1. Detects the GitHub host from GITHUB_SERVER_URL
	//   2. For github.com: exits immediately (no-op)
	//   3. For GHE/GHES: authenticates gh CLI with the enterprise host and sets
	//      GH_HOST=<host> in GITHUB_ENV so every subsequent step in this job
	//      picks up the correct host without manual per-step configuration.
	// Must run after the setup action (so the script is available at ${RUNNER_TEMP}/gh-aw/actions/)
	// and before any custom steps that invoke gh CLI commands.
	yaml.WriteString("      - name: Configure gh CLI for GitHub Enterprise\n")
	yaml.WriteString("        run: bash \"${RUNNER_TEMP}/gh-aw/actions/configure_gh_for_ghe.sh\"\n")
	yaml.WriteString("        env:\n")
	yaml.WriteString("          GH_TOKEN: ${{ github.token }}\n")

	// Start DIFC proxy for pre-agent gh CLI calls (only when guard policies are configured
	// and pre-agent steps with GH_TOKEN are present). The proxy routes gh CLI calls through
	// integrity filtering before the agent runs. Must start before custom steps.
	c.generateStartDIFCProxyStep(yaml, data)

	// Download the activation artifact and prepare comment-memory files BEFORE user steps
	// so that deterministic steps: blocks can read prior comment-memory state without an LLM
	// turn. Unlike cache-memory/repo-memory (pure restores), comment-memory fetches comment
	// content via the GitHub API, which is available at this point in the job.
	c.generateActivationArtifactAndCommentMemorySteps(yaml, data)

	// Add cache-memory steps before custom steps so that user steps: code can read
	// /tmp/gh-aw/cache-memory/<key>/ without an LLM turn.
	compilerYamlLog.Printf("Generating cache-memory steps for workflow")
	generateCacheMemorySteps(yaml, data)

	// Mount experimental drive-memory stores before custom steps.
	compilerYamlLog.Printf("Generating drive-memory steps for workflow")
	generateDriveMemorySteps(yaml, data, c.getActionPin)

	// Add repo-memory clone steps before custom steps so that user steps: code can read
	// /tmp/gh-aw/repo-memory/<name>/ without an LLM turn.
	compilerYamlLog.Printf("Generating repo-memory steps for workflow")
	generateRepoMemorySteps(yaml, data)

	if !customStepsContainCheckout {
		generateSharedLogsCacheRestoreSteps(yaml, data)
	}

	c.emitCustomSteps(yaml, data, customStepsContainCheckout, runtimeSetupSteps)

	// Add cache steps if cache configuration is present. Keep workspace caches after user
	// steps so a user-provided checkout step cannot wipe restored repository paths.
	compilerYamlLog.Printf("Generating cache steps for workflow")
	generateCacheSteps(yaml, data, c.verbose)

	return customStepsContainCheckout
}

func (c *Compiler) prepareRuntimeSetupAndCheckoutInfo(data *WorkflowData) ([]GitHubActionStep, bool) {
	// Add automatic runtime setup steps if needed
	// This detects runtimes from custom steps and MCP configs
	runtimeRequirements := detectRuntimeRequirementsCached(data)

	// Deduplicate runtime setup steps from custom steps
	// This removes any runtime setup action steps (like actions/setup-go) from custom steps
	// since we're adding them. It also preserves user-customized setup actions and
	// filters those runtimes from requirements so we don't generate duplicates.
	if len(runtimeRequirements) > 0 && data.CustomSteps != "" {
		deduplicatedCustomSteps, filteredRequirements, err := DeduplicateRuntimeSetupStepsFromCustomSteps(data.CustomSteps, runtimeRequirements)
		if err != nil {
			compilerYamlLog.Printf("Warning: failed to deduplicate runtime setup steps: %v", err)
		} else {
			data.CustomSteps = deduplicatedCustomSteps
			runtimeRequirements = filteredRequirements
		}
	}

	// Generate runtime setup steps (after filtering out user-customized ones)
	runtimeSetupSteps := GenerateRuntimeSetupSteps(runtimeRequirements, data)
	compilerYamlLog.Printf("Detected runtime requirements: %d runtimes, %d setup steps", len(runtimeRequirements), len(runtimeSetupSteps))

	// Determine whether the (post-deduplication) custom steps contain a checkout
	// step. This check runs before sanitizeAndWarnCustomSteps is called inside
	// addCustomStepsWithRuntimeInsertion, but that is safe: sanitization only
	// rewrites ${{ }} expressions inside `run:` fields and never touches `uses:`
	// values, so the checkout-detection result is identical before and after
	// sanitization.
	customStepsContainCheckout := data.CustomSteps != "" && ContainsCheckout(data.CustomSteps)

	return runtimeSetupSteps, customStepsContainCheckout
}

func (c *Compiler) emitRuntimeSetupPrelude(yaml *strings.Builder, data *WorkflowData, needsCheckout bool, customStepsContainCheckout bool, runtimeSetupSteps []GitHubActionStep) {
	// Redirect tool cache for ARC/DinD runners BEFORE runtime setup steps.
	// On ARC, the standard RUNNER_TOOL_CACHE=/opt/hostedtoolcache is invisible to the DinD
	// daemon's filesystem. Redirecting to ${RUNNER_TEMP}/gh-aw/tool-cache ensures the cache
	// lives on the daemon-visible shared workspace volume.
	// ensures setup-* actions install to a path visible to both runner and DinD containers.
	// This step must run before any runtime setup steps (setup-go, setup-node, etc.) so that
	// those actions pick up the redirected path when they write into RUNNER_TOOL_CACHE.
	if isArcDindTopology(data) {
		c.generateArcDindToolCacheRedirectStep(yaml)
	}

	runtimeStepsEmittedEarly := needsCheckout || !customStepsContainCheckout
	if runtimeStepsEmittedEarly {
		// Case 1 or 3: Add runtime steps before custom steps
		// This ensures checkout -> runtime -> custom steps order
		compilerYamlLog.Printf("Adding %d runtime steps before custom steps (needsCheckout=%t, !customStepsContainCheckout=%t)", len(runtimeSetupSteps), needsCheckout, !customStepsContainCheckout)
		c.emitRuntimeSetupSteps(yaml, runtimeSetupSteps, isArcDindTopology(data))
	}
}

func (c *Compiler) generateArcDindToolCacheRedirectStep(yaml *strings.Builder) {
	yaml.WriteString("      - name: Redirect tool cache and install paths for ARC/DinD\n")
	yaml.WriteString("        run: |\n")
	yaml.WriteString("          mkdir -p \"${RUNNER_TEMP}/gh-aw/tool-cache\"\n")
	yaml.WriteString("          echo \"RUNNER_TOOL_CACHE=${RUNNER_TEMP}/gh-aw/tool-cache\" >> \"$GITHUB_ENV\"\n")
	yaml.WriteString("          echo \"DOTNET_INSTALL_DIR=${RUNNER_TEMP}/gh-aw/tool-cache/dotnet\" >> \"$GITHUB_ENV\"\n")
	yaml.WriteString("          echo \"GOPATH=${RUNNER_TEMP}/gh-aw/tool-cache/go\" >> \"$GITHUB_ENV\"\n")
}

func (c *Compiler) generateArcDindNodePathStep(yaml *strings.Builder, ifCondition string) {
	yaml.WriteString("      - name: Ensure Node.js is at daemon-visible path\n")
	if ifCondition != "" {
		fmt.Fprintf(yaml, "        if: %s\n", ifCondition)
	}
	yaml.WriteString("        run: |\n")
	yaml.WriteString("          NODE_BIN=\"$(command -v node)\"\n")
	yaml.WriteString("          NODE_PREFIX=\"$(dirname \"$(dirname \"$NODE_BIN\")\")\"\n")
	yaml.WriteString("          TOOL_DEST=\"${RUNNER_TEMP}/gh-aw/tool-cache/node\"\n")
	yaml.WriteString("          if [[ \"$NODE_PREFIX\" != \"${RUNNER_TEMP}\"/* ]]; then\n")
	yaml.WriteString("            echo \"Node at $NODE_PREFIX is not under RUNNER_TEMP, copying to $TOOL_DEST\"\n")
	yaml.WriteString("            mkdir -p \"$TOOL_DEST\"\n")
	yaml.WriteString("            cp -a \"$NODE_PREFIX\"/. \"$TOOL_DEST\"/\n")
	yaml.WriteString("            echo \"${TOOL_DEST}/bin\" >> \"$GITHUB_PATH\"\n")
	yaml.WriteString("            echo \"GH_AW_NODE_BIN=${TOOL_DEST}/bin/node\" >> \"$GITHUB_ENV\"\n")
	yaml.WriteString("          fi\n")
}

func (c *Compiler) emitRuntimeSetupSteps(yaml *strings.Builder, runtimeSetupSteps []GitHubActionStep, ensureArcDindNodePath bool) {
	nodePathStepEmitted := false
	for _, step := range runtimeSetupSteps {
		for _, line := range step {
			yaml.WriteString(line)
			yaml.WriteByte('\n')
		}
		if ensureArcDindNodePath && !nodePathStepEmitted && extractStepName(strings.Join(step, "\n")) == "Setup Node.js" {
			c.generateArcDindNodePathStep(yaml, extractStepIfCondition(step))
			nodePathStepEmitted = true
		}
	}
}

// extractStepIfCondition returns the unwrapped if: expression from a generated step.
// It returns empty string when no if: line is present and also when an if: line has no value.
func extractStepIfCondition(step GitHubActionStep) string {
	for _, line := range step {
		if after, ok := strings.CutPrefix(strings.TrimSpace(line), "if:"); ok {
			return strings.TrimSpace(after)
		}
	}
	return ""
}

func (c *Compiler) emitCustomSteps(yaml *strings.Builder, data *WorkflowData, customStepsContainCheckout bool, runtimeSetupSteps []GitHubActionStep) {
	// Add custom steps if present
	if data.CustomSteps == "" {
		return
	}

	// When the DIFC proxy is active, inject proxy routing env vars as step-level env
	// on each custom step. Step-level env takes precedence over $GITHUB_ENV without
	// mutating it, so GHE host values are preserved for non-proxied steps.
	customStepsToEmit := data.CustomSteps
	if hasDIFCProxyNeeded(data) {
		customStepsToEmit = injectProxyEnvIntoCustomSteps(customStepsToEmit)
	}
	postLastCheckoutSteps := sharedLogsCacheRestoreSteps(data)
	if ambientRestoreStep := restoreAmbientFoldersSteps(data); len(ambientRestoreStep) > 0 {
		postLastCheckoutSteps = append(postLastCheckoutSteps, ambientRestoreStep)
	}
	if customStepsContainCheckout && (len(runtimeSetupSteps) > 0 || len(postLastCheckoutSteps) > 0) {
		// Custom steps contain checkout: insert runtime steps after the first checkout and
		// workspace restore steps after the last checkout. Inserting restore steps after the
		// last checkout ensures that a multi-checkout custom steps block (where a later root
		// checkout would wipe .github/aw/logs or ambient folders) leaves restored content in
		// place for later custom steps and the agent.
		compilerYamlLog.Printf("Calling addCustomStepsWithRuntimeInsertion: %d runtime steps after first checkout, %d post-checkout steps after last checkout", len(runtimeSetupSteps), len(postLastCheckoutSteps))
		c.addCustomStepsWithRuntimeInsertion(yaml, customStepsToEmit, runtimeSetupSteps, postLastCheckoutSteps, data.ParsedTools, isArcDindTopology(data))
	} else {
		// No checkout in custom steps or no steps to insert, just add custom steps as-is
		compilerYamlLog.Printf("Calling addCustomStepsAsIs (customStepsContainCheckout=%t, runtimeStepsCount=%d)", customStepsContainCheckout, len(runtimeSetupSteps))
		c.addCustomStepsAsIs(yaml, customStepsToEmit)
	}
}

// generateActivationArtifactAndCommentMemorySteps emits the activation artifact download and,
// when comment-memory is configured, the minimal config write and comment-memory file preparation
// steps. These steps are placed BEFORE the user's custom steps: block so that deterministic
// steps can read prior comment-memory state without an LLM turn.
//
// The activation artifact (aw-prompts/prompt.txt, base/ snapshot, etc.) is downloaded here so
// the comment-memory setup can inject prompt guidance into prompt.txt. The rest of the agent
// setup (git config, engine install, MCP) still runs in generateEngineInstallAndPreAgentSteps.
func (c *Compiler) generateActivationArtifactAndCommentMemorySteps(yaml *strings.Builder, data *WorkflowData) {
	// Download activation artifact from activation job (contains aw_info.json, prompt.txt,
	// base/ snapshot and engine-specific sub-agent/skill dirs).
	// In workflow_call context, apply the per-invocation prefix to avoid name clashes.
	// Must happen before comment-memory preparation (needs prompt.txt for injection) and
	// before the base-branch restore in generateEngineInstallAndPreAgentSteps.
	compilerYamlLog.Print("Adding activation artifact download step")
	activationArtifactName := artifactPrefixExprForDownstreamJob(data) + constants.ActivationArtifactName.String()
	yaml.WriteString("      - name: Download activation artifact\n")
	fmt.Fprintf(yaml, "        uses: %s\n", c.getActionPin("actions/download-artifact"))
	yaml.WriteString("        with:\n")
	fmt.Fprintf(yaml, "          name: %s\n", activationArtifactName)
	yaml.WriteString("          path: /tmp/gh-aw\n")
	generateRestoreAmbientFoldersStep(yaml, data)

	// Materialize tools.comment-memory state as editable markdown files BEFORE user steps.
	// This prepares /tmp/gh-aw/comment-memory/*.md from prior comment history and injects
	// prompt guidance so the agent can update files directly and persist them via the
	// comment_memory safe output.
	if data.CommentMemoryConfig == nil {
		return
	}

	// Write a minimal comment-memory config so setup_comment_memory_files.cjs can locate the
	// comment_memory section. The full safeoutputs config (generated in MCP setup) is written
	// later; this early write provides only what the read step needs.
	if !c.generateCommentMemoryEarlyConfigStep(yaml, data) {
		return
	}

	yaml.WriteString("      - name: Prepare comment memory files\n")
	fmt.Fprintf(yaml, "        uses: %s\n", getCachedActionPin("actions/github-script", data))
	yaml.WriteString("        with:\n")
	fmt.Fprintf(yaml, "          github-token: %s\n", getEffectiveSafeOutputGitHubToken(data.CommentMemoryConfig.GitHubToken))
	yaml.WriteString("          script: |\n")
	yaml.WriteString("            const { setupGlobals } = require('${{ runner.temp }}/gh-aw/actions/setup_globals.cjs');\n")
	yaml.WriteString("            setupGlobals(core, github, context, exec, io, getOctokit);\n")
	yaml.WriteString("            const { main } = require('${{ runner.temp }}/gh-aw/actions/setup_comment_memory_files.cjs');\n")
	yaml.WriteString("            await main();\n")
}

// generateCommentMemoryEarlyConfigStep emits a step that writes a minimal comment-memory
// configuration to ${RUNNER_TEMP}/gh-aw/safeoutputs/config.json so that the
// "Prepare comment memory files" step can read the comment_memory handler config before the
// full "Generate Safe Outputs Config" step runs later in MCP setup.
// The full safeoutputs config written by MCP setup will overwrite this file.
// Returns true if the step was emitted, false if the step was skipped (e.g. handler missing).
func (c *Compiler) generateCommentMemoryEarlyConfigStep(yaml *strings.Builder, data *WorkflowData) bool {
	lines, ok := c.generateCommentMemoryEarlyConfigLines(data)
	if !ok {
		return false
	}
	for _, line := range lines {
		yaml.WriteString(line)
	}
	return true
}

// generateCommentMemoryEarlyConfigLines returns the formatted YAML lines for the
// early comment-memory config write step. This is shared by the agent job, custom
// jobs, and pre-activation memory restore so all deterministic paths can prepare
// comment-memory files before the full safe-outputs config exists.
func (c *Compiler) generateCommentMemoryEarlyConfigLines(data *WorkflowData) ([]string, bool) {
	var globalFooter *bool
	var globalBodyFooter string
	if data.SafeOutputs != nil {
		globalFooter = data.SafeOutputs.Footer
		globalBodyFooter = data.SafeOutputs.BodyFooter
	}
	cfg := buildCommentMemoryHandlerConfig(data.CommentMemoryConfig, globalFooter, globalBodyFooter)
	if cfg == nil {
		return nil, false
	}
	// INTENTIONALLY MINIMAL: this config contains only the comment_memory section and
	// deliberately omits workspace-path injections and checkout mappings, which are not
	// needed by setup_comment_memory_files.cjs. The full safeoutputs config (generated by
	// generateSafeOutputsConfig later in MCP setup) will overwrite this file. Do not add
	// handler-registry-wide iterations here.
	// github-token is stripped from the early config: the github-script step supplies the
	// token directly via its `github-token:` input, so embedding it here would be redundant
	// and could embed a secret or context expression into the YAML shell script body.
	delete(cfg, "github-token")
	configMap := map[string]any{commentMemoryHandlerKey: cfg}
	jsonBytes, err := json.Marshal(configMap)
	if err != nil {
		compilerYamlLog.Printf("Warning: failed to marshal comment-memory config: %v", err)
		return nil, false
	}
	configJSON := string(jsonBytes)
	delimiter := GenerateHeredocDelimiterFromContent("COMMENT_MEMORY_CONFIG", configJSON)
	if err := ValidateHeredocContent(configJSON, delimiter); err != nil {
		compilerYamlLog.Printf("Warning: comment-memory config contains heredoc delimiter; skipping early config write: %v", err)
		return nil, false
	}
	var lines []string
	lines = append(lines, "      - name: Write comment-memory configuration\n")
	lines = append(lines, "        run: |\n")
	lines = append(lines, "          mkdir -p \"${RUNNER_TEMP}/gh-aw/safeoutputs\"\n")
	lines = append(lines, fmt.Sprintf("          cat > \"${RUNNER_TEMP}/gh-aw/safeoutputs/config.json\" << '%s'\n", delimiter)) //nolint:generatedyamlheredoc // Early config rendering runs before the reusable JavaScript step is available.
	// The 10-space YAML block-scalar indentation is stripped by the YAML parser before the
	// shell script is executed, so the JSON content lands at column 0 inside the heredoc.
	// This matches the pattern used by mcp_setup_generator.go for the full config write.
	lines = append(lines, fmt.Sprintf("          %s\n", configJSON))
	lines = append(lines, fmt.Sprintf("          %s\n", delimiter))
	return lines, true
}

// addCustomStepsAsIs adds custom steps after sanitizing any GitHub Actions expressions
// found directly in run: fields.  Any ${{ ... }} expression in a run: script is
// extracted into an env: variable to prevent shell injection attacks; a compiler
// warning is emitted for every such extraction.
func (c *Compiler) addCustomStepsAsIs(yaml *strings.Builder, customSteps string) {
	customSteps = c.sanitizeAndWarnCustomSteps(customSteps)
	// Remove "steps:" line and adjust indentation
	lines := strings.Split(customSteps, "\n")
	if len(lines) > 1 {
		var blockScalarState yamlBlockScalarState
		for _, line := range lines[1:] {
			isBS := blockScalarState.update(line)
			appendYAMLLine(yaml, "      ", line, isBS)
		}
	}
}

// addCustomStepsWithRuntimeInsertion adds custom steps and inserts runtime steps after the first
// checkout and postLastCheckoutSteps (e.g. cache restore) after the last checkout. In the common
// single-checkout case both insertions happen at the same point. For multi-checkout workflows
// (where a later root checkout would wipe a previously restored path such as .github/aw/logs)
// the post-last-checkout steps are deferred until after the final checkout so the path remains
// intact when the subsequent command runs.
// Like addCustomStepsAsIs it sanitizes any ${{ ... }} expressions found in run: fields before writing.
func (c *Compiler) addCustomStepsWithRuntimeInsertion(yaml *strings.Builder, customSteps string, runtimeSetupSteps []GitHubActionStep, postLastCheckoutSteps []GitHubActionStep, tools *ToolsConfig, ensureArcDindNodePath bool) {
	customSteps = c.sanitizeAndWarnCustomSteps(customSteps)
	firstCheckoutIndex, hasCheckoutStep := findFirstCheckoutStepIndex(customSteps)
	lastCheckoutIndex, _ := findLastCheckoutStepIndex(customSteps)
	hasPostLastSteps := len(postLastCheckoutSteps) > 0
	// Remove "steps:" line and adjust indentation
	lines := strings.Split(customSteps, "\n")
	if len(lines) <= 1 {
		return
	}

	insertedRuntime := false
	insertedPostLast := false
	i := 1 // Start from index 1 to skip "steps:" line
	currentStepIndex := -1
	stepIndent := -1
	var blockScalarState yamlBlockScalarState

	for i < len(lines) {
		line := lines[i]
		isBS := blockScalarState.update(line)

		// Skip empty lines
		if strings.TrimSpace(line) == "" {
			yaml.WriteString("\n")
			i++
			continue
		}

		// Add the line with proper indentation
		appendYAMLLine(yaml, "      ", line, isBS)

		// Check if this line starts a top-level step
		trimmed := strings.TrimSpace(line)
		indent := len(line) - len(strings.TrimLeft(line, " "))
		isStepStart := strings.HasPrefix(trimmed, "- ")
		if isStepStart {
			if stepIndent == -1 {
				stepIndent = indent
			}
			isStepStart = indent == stepIndent
		}

		if isStepStart {
			currentStepIndex++
			isFirstCheckout := hasCheckoutStep && currentStepIndex == firstCheckoutIndex && !insertedRuntime
			isLastCheckout := hasPostLastSteps && hasCheckoutStep && currentStepIndex == lastCheckoutIndex && !insertedPostLast

			if isFirstCheckout || isLastCheckout {
				// This is a checkout step (first, last, or both): copy all its lines until the next step
				i++
				for i < len(lines) {
					nextLine := lines[i]
					nextTrimmed := strings.TrimSpace(nextLine)
					nextIndent := len(nextLine) - len(strings.TrimLeft(nextLine, " "))

					// Stop if we hit the next step, but only when we are not inside a
					// block scalar payload (e.g. "sparse-checkout: |\n  - src" -- the
					// "- src" content line starts with "- " but is not a step boundary).
					if !blockScalarState.IsInPayload() && nextTrimmed != "" && strings.HasPrefix(nextTrimmed, "- ") && nextIndent == stepIndent {
						break
					}

					// Add the line (this also advances the block scalar state machine)
					nextIsBS := blockScalarState.update(nextLine)
					appendYAMLLine(yaml, "      ", nextLine, nextIsBS)
					i++
				}

				if isFirstCheckout {
					// Insert runtime steps after the first checkout step
					compilerYamlLog.Printf("Inserting %d runtime setup steps after first checkout in custom steps", len(runtimeSetupSteps))
					c.emitRuntimeSetupSteps(yaml, runtimeSetupSteps, ensureArcDindNodePath)
					insertedRuntime = true
				}
				if isLastCheckout || (isFirstCheckout && firstCheckoutIndex == lastCheckoutIndex) {
					// Insert post-last-checkout steps (e.g. cache restore) after the last checkout.
					// When first == last (single-checkout), this runs immediately after runtime insertion above.
					compilerYamlLog.Printf("Inserting %d post-last-checkout steps after last checkout in custom steps", len(postLastCheckoutSteps))
					for _, step := range postLastCheckoutSteps {
						for _, stepLine := range step {
							yaml.WriteString(stepLine)
							yaml.WriteByte('\n')
						}
					}
					insertedPostLast = true
				}

				continue // Continue with the next iteration (i is already advanced)
			}
		}

		i++
	}
}

// sanitizeAndWarnCustomSteps applies sanitizeCustomStepsYAML to the custom steps string,
// emits a compiler warning for every expression that was extracted, and returns the
// sanitized string.  If sanitization fails or produces no changes the original is returned.
func (c *Compiler) sanitizeAndWarnCustomSteps(customSteps string) string {
	sanitized, warnings, err := sanitizeCustomStepsYAML(customSteps)
	if err != nil {
		compilerYamlLog.Printf("Failed to sanitize custom steps YAML: %v", err)
		return customSteps
	}
	for _, w := range warnings {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(w))
		c.IncrementWarningCount()
	}
	return sanitized
}
