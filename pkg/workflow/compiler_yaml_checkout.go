package workflow

import (
	"encoding/json"
	"fmt"
	"strings"
)

// generateInitialAndCheckoutSteps emits the OTLP mask step, pre-steps, all checkout steps
// (default workspace checkout, dev-mode CLI build, additional checkouts), repository import
// checkouts, legacy agent import checkout, and the merge-.github-folder step.
// It returns the CheckoutManager (needed later for token invalidation and dev-mode restore)
// and a flag indicating whether the default workspace checkout was emitted.
// generateOTLPTelemetrySteps emits the OTLP masking and validation steps that must run
// before any other work in the agent job.
//
// Masking happens first so authentication tokens cannot leak in runner debug logs: the
// workflow-level OTEL_EXPORTER_OTLP_HEADERS env var is available from the very first step.
// The credentials check then fails fast when the enterprise default OTLP endpoint is
// configured without the matching credentials secret; it is a no-op when the endpoint
// variable is unset.
func (c *Compiler) generateOTLPTelemetrySteps(yaml *strings.Builder, data *WorkflowData) {
	if isOTLPHeadersPresent(data) {
		yaml.WriteString(generateOTLPHeadersMaskStep())
	}
	if isOTLPDefaultCredentialsCheckNeeded(data) {
		yaml.WriteString(generateOTLPDefaultCredentialsCheckStep())
	}
	// Mask custom OTLP attribute values so user-supplied values cannot leak into runner logs.
	if isOTLPAttributesPresent(data) {
		yaml.WriteString(generateOTLPAttributesMaskStep())
	}
}

func (c *Compiler) generateInitialAndCheckoutSteps(yaml *strings.Builder, data *WorkflowData) (*CheckoutManager, bool, error) {
	c.generateOTLPTelemetrySteps(yaml, data)

	// Add pre-steps before checkout and the subsequent built-in steps in this agent job.
	// This allows users to mint short-lived tokens (via custom actions) in the same
	// job as checkout, so the tokens are never dropped by the GitHub Actions runner's
	// add-mask behaviour that silently suppresses masked values across job boundaries.
	// Step outputs are available as ${{ steps.<id>.outputs.<name> }} and can be
	// referenced directly in checkout.token. Some compiler-injected setup steps may
	// still be emitted earlier than these pre-steps.
	c.generatePreSteps(yaml, data)

	// Determine if we need to add a checkout step
	needsCheckout := c.shouldAddCheckoutStep(data)
	compilerYamlLog.Printf("Checkout step needed: %t", needsCheckout)

	// Build a CheckoutManager with any user-configured checkouts
	checkoutMgr := NewCheckoutManager(data.CheckoutConfigs)

	// Propagate the platform (host) repo resolved by the activation job so that
	// checkout steps in this job and in safe_outputs can use the correct repository
	// for .github/.agents sparse checkouts when called cross-repo.
	// The activation job exposes this as needs.activation.outputs.target_repo.
	if hasWorkflowCallTrigger(data.On) && !data.InlinedImports {
		checkoutMgr.SetCrossRepoTargetRepo("${{ needs.activation.outputs.target_repo }}")
	}

	c.generateAppTokenAndDefaultCheckoutSteps(yaml, data, checkoutMgr, needsCheckout)

	// Emit additional (non-default) user-configured checkouts
	additionalLines := checkoutMgr.GenerateAdditionalCheckoutSteps(c.getActionPin)
	for _, line := range additionalLines {
		yaml.WriteString(line)
	}

	// Emit a manifest step that records the path and resolved default branch for each
	// non-default cross-repo checkout. The safe-outputs MCP server reads this file to
	// resolve base branches without making any credentialed network calls.
	for _, line := range checkoutMgr.GenerateCheckoutManifestStep(c.getActionPin) {
		yaml.WriteString(line)
	}

	if err := c.generateImportCheckoutAndMergeSteps(yaml, data); err != nil {
		return nil, false, err
	}

	return checkoutMgr, needsCheckout, nil
}

// generateAppTokenAndDefaultCheckoutSteps mints checkout app tokens and emits the
// default workspace checkout (plus dev-mode CLI build steps) for the agent job.
//
// Tokens are minted directly in the agent job because they cannot be passed via job
// outputs from the activation job: actions/create-github-app-token calls ::add-mask::
// on the token, and the GitHub Actions runner silently drops masked values when used
// as job outputs (runner v2.308+). Minting here makes the token available as
// steps.checkout-app-token-{index}.outputs.token within the same job, just like the
// github-mcp-app-token pattern.
func (c *Compiler) generateAppTokenAndDefaultCheckoutSteps(yaml *strings.Builder, data *WorkflowData, checkoutMgr *CheckoutManager, needsCheckout bool) {
	if checkoutMgr.HasAppAuth() {
		compilerYamlLog.Print("Generating checkout app token minting steps in agent job")
		for _, step := range checkoutMgr.GenerateCheckoutAppTokenSteps(c, resolveCheckoutPermissions(data)) {
			yaml.WriteString(step)
		}
	}

	if !needsCheckout {
		return
	}

	// Emit the default workspace checkout, applying any user-supplied overrides
	defaultLines := checkoutMgr.GenerateDefaultCheckoutStep(
		c.trialMode,
		c.trialLogicalRepoSlug,
		c.getActionPin,
	)
	for _, line := range defaultLines {
		yaml.WriteString(line)
	}

	// Add CLI build steps in dev mode (after automatic checkout, before other steps)
	// This builds the gh-aw CLI and Docker image for use by the agentic-workflows MCP server
	// Only generate build steps if agentic-workflows tool is enabled
	if c.actionMode.IsDev() {
		if _, hasAgenticWorkflows := data.Tools["agentic-workflows"]; hasAgenticWorkflows {
			compilerYamlLog.Printf("Generating CLI build steps for dev mode (agentic-workflows tool enabled)")
			c.generateDevModeCLIBuildSteps(yaml)
		} else {
			compilerYamlLog.Printf("Skipping CLI build steps in dev mode (agentic-workflows tool not enabled)")
		}
	}
}

// generateImportCheckoutAndMergeSteps emits the checkout steps for repository imports
// and the legacy agent import, followed by the step that merges the remote .github
// folder into the workspace. Each repository import is checked out into a temporary
// folder so the merge script can copy files from it.
func (c *Compiler) generateImportCheckoutAndMergeSteps(yaml *strings.Builder, data *WorkflowData) error {
	if len(data.RepositoryImports) > 0 {
		compilerYamlLog.Printf("Adding checkout steps for %d repository imports", len(data.RepositoryImports))
		c.generateRepositoryImportCheckouts(yaml, data.RepositoryImports)
	}

	// Add checkout step for legacy agent import (if present)
	// This handles the older import format where a specific agent file is imported
	hasLegacyAgentImport := data.AgentFile != "" && data.AgentImportSpec != ""
	if hasLegacyAgentImport {
		compilerYamlLog.Printf("Adding checkout step for legacy agent import: %s", data.AgentImportSpec)
		c.generateLegacyAgentImportCheckout(yaml, data.AgentImportSpec)
	}

	// Add merge remote .github folder step for repository imports or agent imports
	if len(data.RepositoryImports) == 0 && !hasLegacyAgentImport {
		return nil
	}

	compilerYamlLog.Printf("Adding merge remote .github folder step")
	yaml.WriteString("      - name: Merge remote .github folder\n")
	fmt.Fprintf(yaml, "        uses: %s\n", getCachedActionPin("actions/github-script", data))
	yaml.WriteString("        env:\n")

	// Set repository imports if present
	if len(data.RepositoryImports) > 0 {
		// Convert to JSON array for the script
		repoImportsJSON, err := json.Marshal(data.RepositoryImports)
		if err != nil {
			return fmt.Errorf("failed to marshal repository imports for merge step: %w", err)
		}
		writeYAMLEnv(yaml, "          ", "GH_AW_REPOSITORY_IMPORTS", string(repoImportsJSON))
	}

	// Set agent import spec if present (legacy path)
	if hasLegacyAgentImport {
		writeYAMLEnv(yaml, "          ", "GH_AW_AGENT_FILE", data.AgentFile)
		writeYAMLEnv(yaml, "          ", "GH_AW_AGENT_IMPORT_SPEC", data.AgentImportSpec)
	}

	yaml.WriteString("        with:\n")
	yaml.WriteString("          script: |\n")
	yaml.WriteString("            const { setupGlobals } = require('${{ runner.temp }}/gh-aw/actions/setup_globals.cjs');\n")
	yaml.WriteString("            setupGlobals(core, github, context, exec, io, getOctokit);\n")
	yaml.WriteString("            const { main } = require('${{ runner.temp }}/gh-aw/actions/merge_remote_agent_github_folder.cjs');\n")
	yaml.WriteString("            await main();\n")
	return nil
}

// generateRepositoryImportCheckouts generates checkout steps for repository imports
// Each repository is checked out into a temporary folder at .github/aw/imports/<owner>-<repo>-<sanitized-ref>
// relative to GITHUB_WORKSPACE. This allows the merge script to copy files from pre-checked-out folders instead of doing git operations
func (c *Compiler) generateRepositoryImportCheckouts(yaml *strings.Builder, repositoryImports []string) {
	for _, repoImport := range repositoryImports {
		compilerYamlLog.Printf("Generating checkout step for repository import: %s", repoImport)

		// Parse the import spec to extract owner, repo, and ref
		// Format: owner/repo@ref or owner/repo
		owner, repo, ref := parseRepositoryImportSpec(repoImport)
		if owner == "" || repo == "" {
			compilerYamlLog.Printf("Warning: failed to parse repository import: %s", repoImport)
			continue
		}

		// Generate a sanitized directory name for the checkout
		// Use a consistent format: owner-repo-ref
		// NOTE: Path must be relative to GITHUB_WORKSPACE for actions/checkout@v7
		sanitizedRef := sanitizeRefForPath(ref)
		checkoutPath := fmt.Sprintf(".github/aw/imports/%s-%s-%s", owner, repo, sanitizedRef)

		// Generate the checkout step
		fmt.Fprintf(yaml, "      - name: Checkout repository import %s/%s@%s\n", owner, repo, ref)
		fmt.Fprintf(yaml, "        uses: %s\n", getActionPin("actions/checkout"))
		yaml.WriteString("        with:\n")
		fmt.Fprintf(yaml, "          repository: %s/%s\n", owner, repo)
		fmt.Fprintf(yaml, "          ref: %s\n", ref)
		fmt.Fprintf(yaml, "          path: %s\n", checkoutPath)
		yaml.WriteString("          sparse-checkout: |\n")
		yaml.WriteString("            .github/\n")
		yaml.WriteString("          persist-credentials: false\n")

		compilerYamlLog.Printf("Added checkout step: %s/%s@%s -> %s", owner, repo, ref, checkoutPath)
	}
}

// parseRepositoryImportSpec parses a repository import specification
// Format: owner/repo@ref or owner/repo (defaults to "main" if no ref)
// Returns: owner, repo, ref
func parseRepositoryImportSpec(importSpec string) (owner, repo, ref string) {
	// Remove section reference if present (file.md#Section)
	cleanSpec := importSpec
	if before, _, ok := strings.Cut(importSpec, "#"); ok {
		cleanSpec = before
	}

	// Split on @ to get path and ref
	parts := strings.Split(cleanSpec, "@")
	pathPart := parts[0]
	ref = "main" // default ref
	if len(parts) > 1 {
		ref = parts[1]
	}

	// Parse path: owner/repo
	slashParts := strings.Split(pathPart, "/")
	if len(slashParts) != 2 {
		return "", "", ""
	}

	owner = slashParts[0]
	repo = slashParts[1]

	return owner, repo, ref
}

// generateLegacyAgentImportCheckout generates a checkout step for legacy agent imports
// Accepted format: owner/repo@ref or owner/repo (defaults to ref "main")
// Specs with extra path segments are rejected by parseRepositoryImportSpec.
// Only the .github/ folder is checked out via sparse-checkout.
func (c *Compiler) generateLegacyAgentImportCheckout(yaml *strings.Builder, agentImportSpec string) {
	compilerYamlLog.Printf("Generating checkout step for legacy agent import: %s", agentImportSpec)

	// Parse the import spec to extract owner, repo, and ref
	owner, repo, ref := parseRepositoryImportSpec(agentImportSpec)
	if owner == "" || repo == "" {
		compilerYamlLog.Printf("Warning: failed to parse legacy agent import spec: %s", agentImportSpec)
		return
	}

	// Generate a sanitized directory name for the checkout
	sanitizedRef := sanitizeRefForPath(ref)
	checkoutPath := fmt.Sprintf("/tmp/gh-aw/repo-imports/%s-%s-%s", owner, repo, sanitizedRef)

	// Generate the checkout step
	fmt.Fprintf(yaml, "      - name: Checkout agent import %s/%s@%s\n", owner, repo, ref)
	fmt.Fprintf(yaml, "        uses: %s\n", getActionPin("actions/checkout"))
	yaml.WriteString("        with:\n")
	fmt.Fprintf(yaml, "          repository: %s/%s\n", owner, repo)
	fmt.Fprintf(yaml, "          ref: %s\n", ref)
	fmt.Fprintf(yaml, "          path: %s\n", checkoutPath)
	yaml.WriteString("          sparse-checkout: |\n")
	yaml.WriteString("            .github/\n")
	yaml.WriteString("          persist-credentials: false\n")

	compilerYamlLog.Printf("Added legacy agent checkout step: %s/%s@%s -> %s", owner, repo, ref, checkoutPath)
}

// generateDevModeCLIBuildSteps generates the steps needed to build the gh-aw CLI and Docker image in dev mode
// These steps are injected after checkout in dev mode to create a locally built Docker image that includes
// the gh-aw binary and all dependencies. The agentic-workflows MCP server uses this image instead of alpine:latest.
//
// The build process:
// 1. Setup Go using go.mod version
// 2. Build the gh-aw CLI binary for linux/amd64 (since it runs in a Linux container)
// 3. Setup Docker Buildx for advanced build features
// 4. Build Docker image and tag it as localhost/gh-aw:dev
//
// The built image is used by the agentic-workflows MCP server configuration (see mcp_config_builtin.go)
func (c *Compiler) generateDevModeCLIBuildSteps(yaml *strings.Builder) {
	compilerYamlLog.Print("Generating dev mode CLI build steps")

	// Step 1: Setup Go for building the CLI
	yaml.WriteString("      - name: Setup Go for CLI build\n")
	fmt.Fprintf(yaml, "        uses: %s\n", getActionPin("actions/setup-go"))
	yaml.WriteString("        with:\n")
	yaml.WriteString("          go-version-file: go.mod\n")
	yaml.WriteString("          cache: true\n")

	// Step 2: Build CLI binary for linux/amd64
	// Use the standard build command from CI/Makefile (not release build)
	// CGO_ENABLED=0 for static linking (required for Alpine containers)
	yaml.WriteString("      - name: Build gh-aw CLI\n")
	yaml.WriteString("        run: |\n")
	yaml.WriteString("          echo \"Building gh-aw CLI for linux/amd64...\"\n")
	yaml.WriteString("          mkdir -p dist\n")
	yaml.WriteString("          VERSION=$(git describe --tags --always --dirty)\n")
	yaml.WriteString("          CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \\\n")
	yaml.WriteString("            -ldflags \"-s -w -X main.version=${VERSION}\" \\\n")
	yaml.WriteString("            -o dist/gh-aw-linux-amd64 \\\n")
	yaml.WriteString("            ./cmd/gh-aw\n")
	yaml.WriteString("          # Copy binary to root for direct execution in user-defined steps\n")
	yaml.WriteString("          cp dist/gh-aw-linux-amd64 ./gh-aw\n")
	yaml.WriteString("          chmod +x ./gh-aw\n")
	yaml.WriteString("          echo \"✓ Built gh-aw CLI successfully\"\n")

	// Step 3: Setup Docker Buildx
	yaml.WriteString("      - name: Setup Docker Buildx\n")
	fmt.Fprintf(yaml, "        uses: %s\n", getActionPin("docker/setup-buildx-action"))

	// Step 4: Build Docker image
	// Use the Dockerfile at the repository root which expects BINARY build arg
	yaml.WriteString("      - name: Build gh-aw Docker image\n")
	fmt.Fprintf(yaml, "        uses: %s\n", getActionPin("docker/build-push-action"))
	yaml.WriteString("        with:\n")
	yaml.WriteString("          context: .\n")
	yaml.WriteString("          platforms: linux/amd64\n")
	yaml.WriteString("          push: false\n")
	yaml.WriteString("          load: true\n")
	yaml.WriteString("          tags: localhost/gh-aw:dev\n")
	yaml.WriteString("          build-args: |\n")
	yaml.WriteString("            BINARY=dist/gh-aw-linux-amd64\n")
}
