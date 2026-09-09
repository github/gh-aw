package workflow

import (
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
)

var compilerYamlStepGenerationLog = logger.New("workflow:compiler_yaml_step_generation")

// generateCheckoutActionsFolder generates the checkout step for the actions folder
// when running in dev mode and not using the action-tag feature. This is used to
// checkout the local actions before running the setup action.
//
// Returns a slice of strings that can be appended to a steps array, where each
// string represents a line of YAML for the checkout step. Returns nil if:
// - Not in dev or script mode
// - action-tag feature is specified (uses remote actions instead)
func (c *Compiler) generateCheckoutActionsFolder(data *WorkflowData) []string {
	compilerYamlStepGenerationLog.Printf("Generating checkout actions folder step: actionMode=%s, version=%s", c.actionMode, c.version)
	// Check if action-tag is specified - if so, we're using remote actions
	if data != nil && data.Features != nil {
		if actionTagVal, exists := data.Features["action-tag"]; exists {
			if actionTagStr, ok := actionTagVal.(string); ok && actionTagStr != "" {
				// action-tag is set, use remote actions - no checkout needed
				compilerYamlStepGenerationLog.Printf("Skipping checkout actions folder: action-tag=%s requests remote actions", actionTagStr)
				return nil
			}
		}
	}

	// Derive a clean git ref from the compiler's version string.
	// Required so that cross-repo callers checkout github/gh-aw at the correct
	// commit rather than the default branch, which may be missing JS modules
	// that were added after the latest tag.
	ref := versionToGitRef(c.version)

	// Script mode: checkout .github folder from github/gh-aw to /tmp/gh-aw/actions-source/
	if c.actionMode.IsScript() {
		lines := []string{
			"      - name: Checkout actions folder\n",
			fmt.Sprintf("        uses: %s\n", getActionPin("actions/checkout")),
			"        with:\n",
			"          repository: github/gh-aw\n",
		}
		if ref != "" {
			lines = append(lines, fmt.Sprintf("          ref: %s\n", ref))
		}
		lines = append(lines,
			"          sparse-checkout: |\n",
			"            actions\n",
			"          path: /tmp/gh-aw/actions-source\n",
			"          fetch-depth: 1\n",
			"          clean: false\n",
			"          persist-credentials: false\n",
		)
		return lines
	}

	// Dev mode: checkout actions folder from github/gh-aw so that cross-repo
	// callers (e.g. event-driven relays) can find the actions/ directory.
	// Without repository: the runner defaults to the caller's repo, which has
	// no actions/ directory, causing Setup Scripts to fail immediately.
	if c.actionMode.IsDev() {
		lines := []string{
			"      - name: Checkout actions folder\n",
			fmt.Sprintf("        uses: %s\n", getActionPin("actions/checkout")),
			"        with:\n",
			"          repository: github/gh-aw\n",
			"          sparse-checkout: |\n",
			"            actions\n",
			"          clean: false\n",
			"          persist-credentials: false\n",
		}
		return lines
	}

	// Release mode or other modes: no checkout needed
	return nil
}

// generateRestoreActionsSetupStep generates a single "Restore actions folder" step that
// re-checks out only the actions/setup subfolder from github/gh-aw. This is used in dev mode
// after a job step has checked out a different repository (or a different git branch) and
// replaced the workspace content, removing the actions/setup directory. Without restoring it,
// the GitHub Actions runner's post-step for "Setup Scripts" would fail with
// "Can't find 'action.yml', 'action.yaml' or 'Dockerfile' under .../actions/setup".
//
// The step is guarded by `if: always()` so it runs even if prior steps fail, ensuring
// the post-step cleanup can always complete.
//
// Returns the YAML for the step as a single string (for inclusion in a []string steps slice).
func (c *Compiler) generateRestoreActionsSetupStep() string {
	compilerYamlStepGenerationLog.Print("Generating restore actions setup step")
	var step strings.Builder
	step.WriteString("      - name: Restore actions folder\n")
	step.WriteString("        if: always()\n")
	fmt.Fprintf(&step, "        uses: %s\n", getActionPin("actions/checkout"))
	step.WriteString("        with:\n")
	step.WriteString("          repository: github/gh-aw\n")
	step.WriteString("          sparse-checkout: |\n")
	step.WriteString("            actions/setup\n")
	step.WriteString("          sparse-checkout-cone-mode: true\n")
	step.WriteString("          clean: false\n")
	step.WriteString("          persist-credentials: false\n")
	return step.String()
}

// generateSetupStep generates the setup step based on the action mode.
// In script mode, it runs the setup.sh script directly from the checked-out source.
// In other modes (dev/release), it uses the setup action.
//
// Parameters:
//   - setupActionRef: The action reference for setup action (e.g., "./actions/setup" or "github/gh-aw/actions/setup@sha")
//   - destination: The destination path where files should be copied (e.g., SetupActionDestination)
//   - enableArtifactClient: Whether to install @actions/artifact so upload_artifact.cjs can upload via REST API directly
//   - traceID: Optional OTLP trace ID expression for cross-job span correlation (e.g., "${{ needs.activation.outputs.setup-trace-id }}"). Empty string means a new trace ID is generated.
//   - parentSpanID: Optional OTLP parent span ID expression for setup-span nesting (e.g., setupParentSpanNeedsExpr(constants.ActivationJobName)). Empty string means setup span is emitted as root.
//
// Returns a slice of strings representing the YAML lines for the setup step.
func buildSetupWorkflowRefExpr(data *WorkflowData) string {
	if data == nil || data.WorkflowID == "" {
		return "${{ github.repository }}/.github/workflows/unknown.lock.yml@${{ github.ref }}"
	}
	return fmt.Sprintf("${{ github.repository }}/.github/workflows/%s.lock.yml@${{ github.ref }}", data.WorkflowID)
}

func setupParentSpanNeedsExpr(upstreamJob constants.JobName) string {
	return fmt.Sprintf("${{ needs.%s.outputs.setup-parent-span-id || needs.%s.outputs.setup-span-id }}", upstreamJob, upstreamJob)
}

const (
	// otlpOIDCMintStepID is the step ID of the GitHub OIDC token mint step used for OTLP export auth.
	otlpOIDCMintStepID = "mint-otlp-oidc-token"
	// otlpWIFExchangeStepID is the step ID of the Google workload identity token exchange step.
	otlpWIFExchangeStepID = "exchange-otlp-workload-identity-token"
)

func (c *Compiler) generateOTLPOIDCMintStep(data *WorkflowData) []string {
	if data == nil {
		return nil
	}

	if app := getOTLPGitHubAppTokenConfig(data.RawFrontmatter); app != nil {
		compilerYamlStepGenerationLog.Print("Generating OTLP GitHub App token mint step before setup")
		return c.buildGitHubAppTokenMintStepWithMeta(app, nil, "", "", "Mint OTLP GitHub App token", otlpOIDCMintStepID)
	}

	workloadIdentity := getOTLPWorkloadIdentity(data.ParsedFrontmatter, data.RawFrontmatter)
	githubApp := getOTLPGitHubApp(data.ParsedFrontmatter, data.RawFrontmatter)
	if workloadIdentity == nil && githubApp == nil {
		return nil
	}

	compilerYamlStepGenerationLog.Print("Generating OTLP OIDC token mint step before setup")
	var audience string
	var stsAudience string
	if workloadIdentity != nil {
		audience, stsAudience = googleWIFAudiences(workloadIdentity.Audience)
	} else {
		audience = strings.TrimSpace(githubApp.Audience)
	}
	lines := []string{
		"      - name: Mint OTLP OIDC token\n",
		fmt.Sprintf("        id: %s\n", otlpOIDCMintStepID),
		fmt.Sprintf("        uses: %s\n", getCachedActionPin("actions/github-script", data)),
		"        with:\n",
		"          script: |\n",
		"            const audience = (process.env.GH_AW_OTLP_OIDC_AUDIENCE || '').trim();\n",
		"            const token = audience ? await core.getIDToken(audience) : await core.getIDToken();\n",
		"            core.setSecret(token);\n",
		"            core.setOutput('token', token);\n",
	}

	if audience != "" {
		lines = append(lines, "        env:\n")
		lines = append(lines, formatYAMLEnv("          ", "GH_AW_OTLP_OIDC_AUDIENCE", audience))
	}

	if workloadIdentity != nil {
		compilerYamlStepGenerationLog.Print("Generating Google OTLP workload identity token exchange step before setup")
		lines = append(lines,
			"      - name: Exchange OTLP workload identity token\n",
			fmt.Sprintf("        id: %s\n", otlpWIFExchangeStepID),
			fmt.Sprintf("        uses: %s\n", getCachedActionPin("actions/github-script", data)),
			"        env:\n",
			fmt.Sprintf("          GH_AW_OTLP_OIDC_TOKEN: ${{ steps.%s.outputs.token }}\n", otlpOIDCMintStepID),
		)
		lines = append(lines, formatYAMLEnv("          ", "GH_AW_OTLP_WIF_AUDIENCE", stsAudience))
		if serviceAccount := strings.TrimSpace(workloadIdentity.ServiceAccount); serviceAccount != "" {
			lines = append(lines, formatYAMLEnv("          ", "GH_AW_OTLP_WIF_SERVICE_ACCOUNT", serviceAccount))
		}
		lines = append(lines,
			"        with:\n",
			"          script: |\n",
		)
		lines = append(lines, FormatJavaScriptForYAML(getExchangeOTLPWorkloadIdentityScript())...)
	}

	return lines
}

func getOTLPAuthTokenStepID(data *WorkflowData) string {
	if data == nil {
		return otlpOIDCMintStepID
	}
	if getOTLPWorkloadIdentity(data.ParsedFrontmatter, data.RawFrontmatter) != nil {
		return otlpWIFExchangeStepID
	}
	return otlpOIDCMintStepID
}

func (c *Compiler) generateSetupStep(data *WorkflowData, setupActionRef string, destination string, enableArtifactClient bool, traceID string, parentSpanID string) []string {
	return c.generateSetupStepWithArtifactClientCondition(data, setupActionRef, destination, enableArtifactClient, traceID, parentSpanID, "")
}

func (c *Compiler) generateSetupStepWithArtifactClientCondition(data *WorkflowData, setupActionRef string, destination string, enableArtifactClient bool, traceID string, parentSpanID string, artifactClientCondition string) []string {
	lines := c.generateOTLPOIDCMintStep(data)
	hasOTLPOIDC := len(lines) > 0
	otlpAuthTokenStepID := getOTLPAuthTokenStepID(data)
	artifactClientCondition = strings.TrimSpace(artifactClientCondition)

	setupEngineID := ""
	if data != nil {
		if data.EngineConfig != nil && data.EngineConfig.ID != "" {
			setupEngineID = data.EngineConfig.ID
		} else if data.AI != "" {
			setupEngineID = data.AI
		}
	}

	// Script mode: run the setup.sh script directly
	if c.actionMode.IsScript() {
		setupLines := []string{
			"      - name: Setup Scripts\n",
			"        id: setup\n",
			"        run: |\n",
			"          bash /tmp/gh-aw/actions-source/actions/setup/setup.sh\n",
			"        env:\n",
			fmt.Sprintf("          INPUT_DESTINATION: %s\n", destination),
			"          INPUT_JOB_NAME: ${{ github.job }}\n",
		}
		if data != nil {
			setupLines = append(setupLines,
				fmt.Sprintf("          GH_AW_SETUP_WORKFLOW_NAME: %q\n", data.Name),
				fmt.Sprintf("          GH_AW_CURRENT_WORKFLOW_REF: %s\n", buildSetupWorkflowRefExpr(data)),
			)
			if v := getVersionForSetup(data, c.engineRegistry); v != "" {
				setupLines = append(setupLines, fmt.Sprintf("          GH_AW_INFO_VERSION: %q\n", v))
			}
			if v := getAWFVersionForSetup(data); v != "" {
				setupLines = append(setupLines, fmt.Sprintf("          GH_AW_INFO_AWF_VERSION: %q\n", v))
			}
			if data.Source != "" {
				setupLines = append(setupLines, "          GH_AW_INFO_BODY_MODIFIED: \"false\"\n")
			}
			if setupEngineID != "" {
				setupLines = append(setupLines, fmt.Sprintf("          GH_AW_INFO_ENGINE_ID: %q\n", setupEngineID))
			}
		}
		if traceID != "" {
			setupLines = append(setupLines, fmt.Sprintf("          INPUT_TRACE_ID: %s\n", traceID))
		}
		if parentSpanID != "" {
			setupLines = append(setupLines, fmt.Sprintf("          INPUT_PARENT_SPAN_ID: %s\n", parentSpanID))
		}
		if hasOTLPOIDC {
			setupLines = append(setupLines, fmt.Sprintf("          INPUT_OTLP_OIDC_TOKEN: ${{ steps.%s.outputs.token }}\n", otlpAuthTokenStepID))
		}
		if enableArtifactClient {
			if artifactClientCondition != "" {
				setupLines = append(setupLines, fmt.Sprintf("          INPUT_SAFE_OUTPUT_ARTIFACT_CLIENT: %s\n", artifactClientCondition))
			} else {
				setupLines = append(setupLines, "          INPUT_SAFE_OUTPUT_ARTIFACT_CLIENT: 'true'\n")
			}
		}
		lines = append(lines, setupLines...)
		return lines
	}

	// Dev/Release mode: use the setup action
	compilerYamlStepGenerationLog.Printf("Generating setup step: ref=%s, destination=%s, artifactClient=%t, traceID=%q, parentSpanID=%q", setupActionRef, destination, enableArtifactClient, traceID, parentSpanID)
	setupLines := []string{
		"      - name: Setup Scripts\n",
		"        id: setup\n",
		fmt.Sprintf("        uses: %s\n", setupActionRef),
		"        with:\n",
		fmt.Sprintf("          destination: %s\n", destination),
		"          job-name: ${{ github.job }}\n",
	}
	if traceID != "" {
		setupLines = append(setupLines, fmt.Sprintf("          trace-id: %s\n", traceID))
	}
	if parentSpanID != "" {
		setupLines = append(setupLines, fmt.Sprintf("          parent-span-id: %s\n", parentSpanID))
	}
	if hasOTLPOIDC {
		setupLines = append(setupLines, fmt.Sprintf("          otlp-oidc-token: ${{ steps.%s.outputs.token }}\n", otlpAuthTokenStepID))
	}
	if enableArtifactClient {
		if artifactClientCondition != "" {
			setupLines = append(setupLines, fmt.Sprintf("          safe-output-artifact-client: %s\n", artifactClientCondition))
		} else {
			setupLines = append(setupLines, "          safe-output-artifact-client: 'true'\n")
		}
	}
	setupLines = append(setupLines,
		"        env:\n",
		fmt.Sprintf("          GH_AW_SETUP_WORKFLOW_NAME: %q\n", data.Name),
		fmt.Sprintf("          GH_AW_CURRENT_WORKFLOW_REF: %s\n", buildSetupWorkflowRefExpr(data)),
	)
	if v := getVersionForSetup(data, c.engineRegistry); v != "" {
		setupLines = append(setupLines, fmt.Sprintf("          GH_AW_INFO_VERSION: %q\n", v))
	}
	if v := getAWFVersionForSetup(data); v != "" {
		setupLines = append(setupLines, fmt.Sprintf("          GH_AW_INFO_AWF_VERSION: %q\n", v))
	}
	if data.Source != "" {
		setupLines = append(setupLines, "          GH_AW_INFO_BODY_MODIFIED: \"false\"\n")
	}
	if setupEngineID != "" {
		setupLines = append(setupLines, fmt.Sprintf("          GH_AW_INFO_ENGINE_ID: %q\n", setupEngineID))
	}
	if hasWorkflowCallTrigger(data.On) {
		setupLines = append(setupLines, "          GH_AW_SETUP_AW_CONTEXT: ${{ inputs.aw_context }}\n")
	}
	lines = append(lines, setupLines...)
	return lines
}

// generateSetRuntimePathsStep generates a step that sets RUNNER_TEMP-based env vars
// via $GITHUB_OUTPUT. These cannot be set in job-level env: because the runner context
// is not available there (only in step-level env: and run: blocks).
// The step ID "set-runtime-paths" is referenced by downstream steps that consume these outputs.
func (c *Compiler) generateSetRuntimePathsStep() []string {
	compilerYamlStepGenerationLog.Print("Generating set-runtime-paths step")
	return []string{
		"      - name: Set runtime paths\n",
		"        id: set-runtime-paths\n",
		"        env:\n",
		"          GH_AW_RUNNER_TOOL_CACHE: ${{ runner.tool_cache }}\n",
		"        run: |\n",
		"          if [ -z \"${RUNNER_TOOL_CACHE:-}\" ]; then\n",
		"            echo \"RUNNER_TOOL_CACHE=${GH_AW_RUNNER_TOOL_CACHE}\" >> \"$GITHUB_ENV\"\n",
		"          fi\n",
		"          {\n",
		"            echo \"GH_AW_SAFE_OUTPUTS=${RUNNER_TEMP}/gh-aw/safeoutputs/outputs.jsonl\"\n",
		"            echo \"GH_AW_SAFE_OUTPUTS_CONFIG_PATH=${RUNNER_TEMP}/gh-aw/safeoutputs/config.json\"\n",
		"            echo \"GH_AW_SAFE_OUTPUTS_TOOLS_PATH=${RUNNER_TEMP}/gh-aw/safeoutputs/tools.json\"\n",
		"          } >> \"$GITHUB_OUTPUT\"\n",
	}
}

// generateScriptModeCleanupStep generates a cleanup step for script mode that sends an OTLP
// conclusion span and removes /tmp/gh-aw/. This mirrors the post.js post step that runs
// automatically when using a `uses:` action in dev/release/action mode.
//
// The step is guarded by `if: always()` so it runs even if prior steps fail, ensuring
// trace spans are exported and temporary files are cleaned up in all cases.
//
// Only call this in script mode (c.actionMode.IsScript()).
func (c *Compiler) generateScriptModeCleanupStep() string {
	compilerYamlStepGenerationLog.Print("Generating script-mode cleanup step")
	var step strings.Builder
	step.WriteString("      - name: Clean Scripts\n")
	step.WriteString("        if: always()\n")
	step.WriteString("        run: |\n")
	step.WriteString("          bash /tmp/gh-aw/actions-source/actions/setup/clean.sh\n")
	step.WriteString("        env:\n")
	fmt.Fprintf(&step, "          INPUT_DESTINATION: %s\n", SetupActionDestination)
	step.WriteString("          INPUT_JOB_NAME: ${{ github.job }}\n")
	return step.String()
}
