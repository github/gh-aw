package workflow

import (
	"fmt"
	"strconv"
	"strings"
)

// wikiRepository returns the effective repository string for a wiki checkout.
// GitHub wiki repositories are accessible as "{owner}/{repo}.wiki".
// When the repository is empty (default current repo), returns "${{ github.repository }}.wiki".
// If the repository already ends with ".wiki" it is returned unchanged to prevent double-suffixing.
func wikiRepository(repository string) string {
	if repository == "" {
		checkoutManagerLog.Print("Wiki checkout using default current repository")
		return "${{ github.repository }}.wiki"
	}
	if strings.HasSuffix(repository, ".wiki") {
		return repository
	}
	return repository + ".wiki"
}

// isDefault reports whether the checkout entry is the default workspace-root
// checkout of the workflow repository (no explicit repository and no explicit path).
func (entry *resolvedCheckout) isDefault() bool {
	return entry.key.path == "" && entry.key.repository == ""
}

// GenerateCheckoutAppTokenSteps generates GitHub App token minting steps for all
// checkout entries that use app authentication. Each app-authenticated checkout
// gets its own minting step with a unique step ID, so the minted token can be
// referenced in the corresponding checkout step.
//
// The step ID for each checkout is "checkout-app-token-{index}" where index is
// the position in the ordered checkout list. Each returned slice element is a
// complete YAML step string, matching injectStepCondition's whole-step contract.
func (cm *CheckoutManager) GenerateCheckoutAppTokenSteps(c *Compiler, permissions *Permissions) []string {
	checkoutManagerLog.Printf("Building app token minting steps for %d checkout entries", len(cm.ordered))
	var steps []string
	for checkoutIndex, entry := range cm.ordered {
		if entry.githubApp == nil {
			continue
		}
		if cm.skipDefaultCheckout && entry.isDefault() {
			checkoutManagerLog.Printf("Skipping app token minting step for suppressed default checkout index=%d", checkoutIndex)
			continue
		}
		checkoutManagerLog.Printf("Generating app token minting step for checkout index=%d repo=%q", checkoutIndex, entry.key.repository)
		// Pass empty fallback so the app token defaults to github.event.repository.name.
		// Checkout-specific cross-repo scoping is handled via the explicit repository field.
		steps = append(steps, collapseYAMLLinesIntoSteps(c.buildGitHubAppTokenMintStepWithMeta(
			entry.githubApp,
			permissions,
			"",
			entry.key.repository,
			fmt.Sprintf("Generate GitHub App token for checkout (%d)", checkoutIndex),
			fmt.Sprintf("checkout-app-token-%d", checkoutIndex),
		))...)
	}
	return steps
}

// GenerateSafeOutputCheckoutAppTokenSteps generates GitHub App token minting steps
// for checkout.safe-outputs-github-app entries. These steps are consumed only by the
// safe_outputs job when choosing the checkout/push token for PR operations.
func (cm *CheckoutManager) GenerateSafeOutputCheckoutAppTokenSteps(c *Compiler, permissions *Permissions) []string {
	checkoutManagerLog.Printf("Building safe_outputs app token minting steps for %d checkout entries", len(cm.ordered))
	var steps []string
	for checkoutIndex, entry := range cm.ordered {
		if entry.safeOutputApp == nil {
			continue
		}
		if cm.skipDefaultCheckout && entry.isDefault() {
			checkoutManagerLog.Printf("Skipping safe_outputs app token minting step for suppressed default checkout index=%d", checkoutIndex)
			continue
		}
		checkoutManagerLog.Printf("Generating safe_outputs app token minting step for checkout index=%d repo=%q", checkoutIndex, entry.key.repository)
		steps = append(steps, collapseYAMLLinesIntoSteps(c.buildGitHubAppTokenMintStepWithMeta(
			entry.safeOutputApp,
			permissions,
			"",
			entry.key.repository,
			fmt.Sprintf("Generate safe_outputs GitHub App token for checkout (%d)", checkoutIndex),
			fmt.Sprintf("checkout-safe-output-app-token-%d", checkoutIndex),
		))...)
	}
	return steps
}

func collapseYAMLLinesIntoSteps(lines []string) []string {
	if len(lines) == 0 {
		return nil
	}

	var steps []string
	var current strings.Builder
	for _, line := range lines {
		if strings.HasPrefix(line, "      - ") && current.Len() > 0 {
			steps = append(steps, current.String())
			current.Reset()
		}
		current.WriteString(line)
	}
	if current.Len() > 0 {
		steps = append(steps, current.String())
	}
	return steps
}

// GenerateAdditionalCheckoutSteps generates YAML step lines for all non-default
// (additional) checkouts — those that target a specific path other than the root.
// The caller is responsible for emitting the default workspace checkout separately.
func (cm *CheckoutManager) GenerateAdditionalCheckoutSteps(getActionPin func(string) string) []string {
	checkoutManagerLog.Printf("Generating additional checkout steps from %d configured entries", len(cm.ordered))
	var lines []string
	for checkoutIndex, entry := range cm.ordered {
		// Skip the default checkout (handled separately)
		if entry.isDefault() {
			continue
		}
		lines = append(lines, generateCheckoutStepLines(entry, checkoutIndex, cm.keepCredentialsForPush, cm.pushToken, getActionPin)...)
	}
	checkoutManagerLog.Printf("Generated %d additional checkout step(s)", len(lines))
	return lines
}

// GenerateCheckoutManifestStep emits a step that writes a JSON manifest describing
// each non-default cross-repository checkout, keyed by lowercase repo slug. The
// manifest records the on-disk path and the resolved default branch for each repo
// so the safe-outputs MCP server (which runs without credentials) can look up the
// base branch without making any network calls.
//
// The manifest file lives at $RUNNER_TEMP/gh-aw/safeoutputs/checkout-manifest.json
// (under safeoutputs/ so it is bind-mounted into the containerized safe-outputs MCP
// server). The default branch is resolved at runtime via:
//  1. `git symbolic-ref --short refs/remotes/origin/HEAD` on the local checkout
//     (works when actions/checkout left the remote HEAD set, typical for fetch-depth: 0)
//  2. `gh api repos/<owner>/<repo> --jq .default_branch` as a credentialed fallback
//
// Returns an empty slice when there are no non-default cross-repo checkouts to record.
func (cm *CheckoutManager) GenerateCheckoutManifestStep(getActionPin func(string) string) []string {
	type manifestEntry struct {
		repository string
		path       string
		token      string
	}
	var entries []manifestEntry
	for checkoutIndex, entry := range cm.ordered {
		if entry.key.wiki {
			continue
		}
		if entry.key.repository == "" {
			continue
		}
		entries = append(entries, manifestEntry{
			repository: entry.key.repository,
			path:       entry.key.path,
			token:      resolveCheckoutTokenExpression(entry, checkoutIndex, false),
		})
	}
	if len(entries) == 0 {
		return nil
	}

	checkoutManagerLog.Printf("Generating checkout manifest step for %d cross-repo entries", len(entries))

	var sb strings.Builder
	sb.WriteString("      - name: Build checkout manifest for safe-outputs handlers\n")
	fmt.Fprintf(&sb, "        uses: %s\n", getActionPin("actions/github-script"))
	sb.WriteString("        env:\n")
	sb.WriteString("          GH_TOKEN: ${{ secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}\n")
	writeYAMLEnv(&sb, "          ", "GH_AW_CHECKOUT_MANIFEST_COUNT", strconv.Itoa(len(entries)))
	for manifestIndex, e := range entries {
		repoKey := fmt.Sprintf("GH_AW_CHECKOUT_REPO_%d", manifestIndex)
		pathKey := fmt.Sprintf("GH_AW_CHECKOUT_PATH_%d", manifestIndex)
		if strings.Contains(e.repository, "${{") {
			fmt.Fprintf(&sb, "          %s: %s\n", repoKey, githubExpressionWhitespaceReplacer.Replace(e.repository))
		} else {
			writeYAMLEnv(&sb, "          ", repoKey, e.repository)
		}
		if strings.Contains(e.path, "${{") {
			fmt.Fprintf(&sb, "          %s: %s\n", pathKey, githubExpressionWhitespaceReplacer.Replace(e.path))
		} else {
			writeYAMLEnv(&sb, "          ", pathKey, e.path)
		}
		if e.token != "" {
			tokenKey := fmt.Sprintf("GH_AW_CHECKOUT_TOKEN_%d", manifestIndex)
			if strings.Contains(e.token, "${{") {
				fmt.Fprintf(&sb, "          %s: %s\n", tokenKey, githubExpressionWhitespaceReplacer.Replace(e.token))
			} else {
				writeYAMLEnv(&sb, "          ", tokenKey, e.token)
			}
		}
	}
	sb.WriteString("        with:\n")
	sb.WriteString("          script: |\n")
	sb.WriteString("            const { main } = require('${{ runner.temp }}/gh-aw/actions/build_checkout_manifest.cjs');\n")
	sb.WriteString("            await main();\n")
	return []string{sb.String()}
}

// GenerateGitHubFolderCheckoutStep generates YAML step lines for a sparse checkout of
// the .github and .agents folders. This is used in the activation job to access workflow
// configuration and runtime imports.
//
// Parameters:
//   - repository: the repository to checkout. May be a literal "owner/repo" value or a
//     GitHub Actions expression such as "${{ steps.resolve-host-repo.outputs.target_repo }}".
//     Pass an empty string to omit the repository field and check out the current repository.
//   - ref: the branch, tag, or SHA to checkout. May be a literal value or a GitHub Actions
//     expression such as "${{ steps.resolve-host-repo.outputs.target_ref }}".
//     Pass an empty string to omit the ref field and use the repository's default branch.
//   - token: the GitHub token to use for authentication. Pass an empty string or
//     "${{ secrets.GITHUB_TOKEN }}" to use the default token (no token: field emitted).
//     For cross-org scenarios, pass a PAT or GitHub App token expression.
//   - getActionPin: resolves an action reference to a pinned SHA form.
//   - extraPaths: additional paths to include in the sparse-checkout beyond .github and .agents.
//
// Returns a slice of YAML lines (each ending with \n).
func (cm *CheckoutManager) GenerateGitHubFolderCheckoutStep(repository, ref, token string, getActionPin func(string) string, extraPaths ...string) []string {
	checkoutManagerLog.Printf("Generating .github/.agents folder checkout: repository=%q ref=%q", repository, ref)
	var sb strings.Builder

	sb.WriteString("      - name: Checkout .github and .agents folders\n")
	fmt.Fprintf(&sb, "        uses: %s\n", getActionPin("actions/checkout"))
	sb.WriteString("        with:\n")
	sb.WriteString("          persist-credentials: false\n")
	if repository != "" {
		fmt.Fprintf(&sb, "          repository: %s\n", repository)
	}
	if ref != "" {
		fmt.Fprintf(&sb, "          ref: %s\n", ref)
	}
	if token != "" && token != "${{ secrets.GITHUB_TOKEN }}" {
		fmt.Fprintf(&sb, "          token: %s\n", token)
	}
	sb.WriteString("          sparse-checkout: |\n")
	sb.WriteString("            .github\n")
	sb.WriteString("            .agents\n")
	for _, p := range extraPaths {
		fmt.Fprintf(&sb, "            %s\n", p)
	}
	sb.WriteString("          sparse-checkout-cone-mode: true\n")
	sb.WriteString("          fetch-depth: 1\n")

	return []string{sb.String()}
}

// GenerateConfigureGitCredentialsSteps emits the "Configure Git credentials" step that
// installs a push-capable token. The root workspace checkout is always the workflow
// repository, so its remote is configured for ${{ github.repository }}. Any cross-repo
// checkout placed into a subdirectory is re-authenticated in place so a later push can
// reach it. The provided gitRemoteToken is used for all remotes.
//
// The agent job never pushes, so it has no equivalent of this step; it is used by the
// safe_outputs job, which supplies the push token (resolvePRCheckoutToken) and a gating
// condition.
func (cm *CheckoutManager) GenerateConfigureGitCredentialsSteps(gitRemoteToken string, condition ConditionNode) []string {
	conditionStr := RenderCondition(condition)

	// Collect subdirectory cross-repo checkouts that need per-repo re-authentication.
	type subRepo struct {
		repository string
		path       string
	}
	var subRepos []subRepo
	for _, entry := range cm.ordered {
		if entry.key.repository != "" && entry.key.path != "" && !entry.key.wiki {
			subRepos = append(subRepos, subRepo{repository: entry.key.repository, path: entry.key.path})
		}
	}

	if len(subRepos) == 0 {
		// Simple case: single root repo, call the script directly.
		rootRepo := "${{ github.repository }}"
		for _, entry := range cm.ordered {
			// If a non-default checkout targets the workspace root (no path:), it will clobber
			// the root checkout; configure git for the effective repo at the root.
			if entry.key.wiki || entry.key.path != "" || entry.key.repository == "" {
				continue
			}
			rootRepo = entry.key.repository
		}
		return []string{
			"      - name: Configure Git credentials\n",
			fmt.Sprintf("        if: %s\n", conditionStr),
			"        env:\n",
			fmt.Sprintf("          GITHUB_REPOSITORY: %s\n", rootRepo),
			"          GITHUB_SERVER_URL: ${{ github.server_url }}\n",
			fmt.Sprintf("          GIT_TOKEN: %s\n", gitRemoteToken),
			"        run: bash \"${RUNNER_TEMP}/gh-aw/actions/configure_git_credentials.sh\"\n",
		}
	}

	// Multi-repo case: configure the root repo, then re-authenticate each subdirectory checkout.
	rootRepo := "${{ github.repository }}"
	for _, entry := range cm.ordered {
		if entry.key.wiki || entry.key.path != "" || entry.key.repository == "" {
			continue
		}
		rootRepo = entry.key.repository
	}

	// Assign each sub-repo a dedicated env var so that GitHub Actions expressions
	// (e.g. "${{ github.event.inputs.target_repo }}") are never inlined directly
	// into the shell command, preventing template-injection scanner failures.
	// pathEnvVarName is set only when path is expression-based; otherwise the
	// literal path string is used directly in the shell command.
	type subRepoEnvVar struct {
		repository     string
		path           string
		envVarName     string
		pathEnvVarName string // non-empty only when path is a GitHub Actions expression
	}
	subRepoEnvVars := make([]subRepoEnvVar, len(subRepos))
	for i, repo := range subRepos {
		ev := subRepoEnvVar{
			repository: repo.repository,
			path:       repo.path,
			envVarName: fmt.Sprintf("GH_AW_SUBREPO_%d", i),
		}
		if strings.Contains(repo.path, "${{") {
			ev.pathEnvVarName = fmt.Sprintf("GH_AW_SUBREPO_PATH_%d", i)
		}
		subRepoEnvVars[i] = ev
	}

	// Build the env block, including a dedicated var for every sub-repo.
	envLines := []string{
		"        env:\n",
		fmt.Sprintf("          GITHUB_REPOSITORY: %s\n", rootRepo),
		"          GITHUB_SERVER_URL: ${{ github.server_url }}\n",
		fmt.Sprintf("          GIT_TOKEN: %s\n", gitRemoteToken),
	}
	for _, repo := range subRepoEnvVars {
		if strings.Contains(repo.repository, "${{") {
			// GitHub Actions expression — write unquoted so the runner expands it.
			// githubExpressionWhitespaceReplacer normalises any embedded newlines/tabs
			// in the expression string to spaces (defined in safe_outputs_app_config.go).
			envLines = append(envLines, fmt.Sprintf("          %s: %s\n", repo.envVarName, githubExpressionWhitespaceReplacer.Replace(repo.repository)))
		} else {
			// Plain string — quote for safe YAML scalar encoding.
			envLines = append(envLines, formatYAMLEnv("          ", repo.envVarName, repo.repository))
		}
		if repo.pathEnvVarName != "" {
			envLines = append(envLines, fmt.Sprintf("          %s: %s\n", repo.pathEnvVarName, githubExpressionWhitespaceReplacer.Replace(repo.path)))
		}
	}

	steps := []string{
		"      - name: Configure Git credentials\n",
		fmt.Sprintf("        if: %s\n", conditionStr),
	}
	steps = append(steps, envLines...)
	steps = append(steps,
		"        run: |\n",
		"          bash \"${RUNNER_TEMP}/gh-aw/actions/configure_git_credentials.sh\"\n",
		"          GIT_SERVER_URL_STRIPPED=\"${GITHUB_SERVER_URL#https://}\"\n",
	)
	for _, repo := range subRepoEnvVars {
		// Use the path env var reference when path is expression-based to avoid
		// inlining ${{ }} into the run: block (template-injection scanner risk).
		gitDir := fmt.Sprintf("%q", repo.path)
		if repo.pathEnvVarName != "" {
			gitDir = fmt.Sprintf("\"${%s}\"", repo.pathEnvVarName)
		}
		// Comment uses the path literal (or env var reference) — never the raw
		// repository expression — so ${{ }} never appears in the run: block.
		commentRef := repo.path
		if repo.pathEnvVarName != "" {
			commentRef = "${" + repo.pathEnvVarName + "}"
		}
		steps = append(steps,
			fmt.Sprintf("          # Re-authenticate git for %s\n", commentRef),
			fmt.Sprintf("          git -C %s remote set-url origin \"https://x-access-token:${GIT_TOKEN}@${GIT_SERVER_URL_STRIPPED}/${%s}.git\"\n", gitDir, repo.envVarName),
		)
	}
	steps = append(steps,
		"          echo \"Git configured with standard GitHub Actions identity\"\n",
	)
	return steps
}

// GenerateDefaultCheckoutStep emits the default workspace checkout, applying any
// user-supplied overrides (token, fetch-depth, ref, etc.) on top of the required
// security defaults (persist-credentials: false).
//
// Parameters:
//   - trialMode: if true, optionally sets repository and token for trial execution
//   - trialLogicalRepoSlug: the repository to checkout in trial mode
//   - getActionPin: resolves an action reference to a pinned SHA form
//
// Returns a slice of YAML lines (each ending with \n).
func (cm *CheckoutManager) GenerateDefaultCheckoutStep(
	trialMode bool,
	trialLogicalRepoSlug string,
	getActionPin func(string) string,
) []string {
	override := cm.GetDefaultCheckoutOverride()
	checkoutManagerLog.Printf("Generating default checkout step: trialMode=%t, hasOverride=%t", trialMode, override != nil)

	var sb strings.Builder
	sb.WriteString("      - name: Checkout repository\n")
	fmt.Fprintf(&sb, "        uses: %s\n", getActionPin("actions/checkout"))
	sb.WriteString("        with:\n")

	cleanCreds := override != nil && override.cleanCreds
	if cm.keepCredentialsForPush {
		// safe_outputs job: retain credentials so later git fetch/push can authenticate
		// using the push-capable token installed at checkout time.
		sb.WriteString("          persist-credentials: true\n")
	} else if cleanCreds {
		sb.WriteString("          persist-credentials: true\n")
	} else {
		// Security: default behavior disables credential persistence so the agent cannot
		// exfiltrate credentials from disk.
		sb.WriteString("          persist-credentials: false\n")
	}

	// Track whether a token has been written to the checkout step so the safe_outputs
	// push-token fallback below does not double-emit.
	tokenEmitted := false

	// Apply trial mode overrides
	if trialMode {
		if trialLogicalRepoSlug != "" {
			fmt.Fprintf(&sb, "          repository: %s\n", trialLogicalRepoSlug)
		}
		effectiveToken := getEffectiveGitHubToken("")
		fmt.Fprintf(&sb, "          token: %s\n", effectiveToken)
		tokenEmitted = true
	}

	// Apply user overrides (only when NOT in trial mode to avoid conflicts)
	if !trialMode && override != nil {
		if override.key.wiki {
			// Wiki checkout: use "{repository}.wiki" as the effective repository.
			fmt.Fprintf(&sb, "          repository: %s\n", wikiRepository(override.key.repository))
		} else if override.key.repository != "" {
			fmt.Fprintf(&sb, "          repository: %s\n", override.key.repository)
		}
		if override.ref != "" && cm.defaultRefOverride == "" {
			fmt.Fprintf(&sb, "          ref: %s\n", override.ref)
		}
		// Prevent actions/checkout from adding --filter=blob:none when sparse-checkout
		// is specified. Blobless clones require credentials for lazy blob fetches, but
		// agent jobs intentionally do not retain git credentials after checkout, making
		// offline git operations fail. Using blob:limit=1073741824 (1 GiB) effectively
		// fetches all blobs up front on GitHub-hosted repos (GitHub rejects blobs > 100 MiB),
		// while keeping the filter non-empty so actions/checkout won't substitute blob:none.
		// The subsequent repair step then clears partial-clone markers entirely.
		if len(override.sparsePatterns) > 0 {
			sb.WriteString("          filter: 'blob:limit=1073741824'\n")
		}

		// Determine effective token: github-app-minted token takes precedence
		effectiveOverrideToken := override.token
		if override.githubApp != nil {
			// Determine the actual index of the default checkout to reference the correct
			// app-token step ID. Do not assume it is always at index 0.
			defaultIdx := 0
			if idx, ok := cm.index[override.key]; ok {
				defaultIdx = idx
			}
			//nolint:gosec // G101: False positive - this is a GitHub Actions expression template placeholder, not a hardcoded credential
			effectiveOverrideToken = fmt.Sprintf("${{ steps.checkout-app-token-%d.outputs.token }}", defaultIdx)
			if override.githubApp.shouldIgnoreMissingKey() {
				effectiveOverrideToken = combineTokenExpressions(effectiveOverrideToken, getEffectiveGitHubToken(override.token))
			}
		}
		if effectiveOverrideToken != "" {
			fmt.Fprintf(&sb, "          token: %s\n", effectiveOverrideToken)
			tokenEmitted = true
		}
		if override.fetchDepth != nil {
			fmt.Fprintf(&sb, "          fetch-depth: %d\n", *override.fetchDepth)
		}
		if len(override.sparsePatterns) > 0 {
			sb.WriteString("          sparse-checkout: |\n")
			for _, pattern := range override.sparsePatterns {
				fmt.Fprintf(&sb, "            %s\n", strings.TrimSpace(pattern))
			}
		}
		if override.submodules != "" {
			fmt.Fprintf(&sb, "          submodules: %s\n", override.submodules)
		}
		if override.lfs {
			sb.WriteString("          lfs: true\n")
		}
	}
	if cm.defaultRefOverride != "" {
		fmt.Fprintf(&sb, "          ref: %s\n", cm.defaultRefOverride)
	}

	// safe_outputs job: when no explicit token was written above, persist the resolved
	// push token so the credential retained in .git/config matches the token the
	// safe-output handlers use to fetch/push (avoiding both a wrong-token push and the
	// duplicate Authorization header that a separate per-command extraheader would add).
	if !trialMode && !tokenEmitted && cm.keepCredentialsForPush && cm.pushToken != "" {
		fmt.Fprintf(&sb, "          token: %s\n", cm.pushToken)
	}

	steps := []string{sb.String()}
	if override != nil && len(override.sparsePatterns) > 0 {
		steps = append(steps, generateSparseCheckoutPartialCloneResetStep(""))
	}
	if cleanCreds && !cm.keepCredentialsForPush {
		steps = append(steps, generateCheckoutCredentialsCleanupStep())
	}

	// Emit a git fetch step if the user requested additional refs.
	// In trial mode the fetch step is still emitted so the behaviour
	// mirrors production as closely as possible.
	if override != nil && len(override.fetchRefs) > 0 {
		defaultIdx := 0
		if idx, ok := cm.index[override.key]; ok {
			defaultIdx = idx
		}
		if fetchStep := generateFetchStepLines(override, defaultIdx); fetchStep != "" {
			steps = append(steps, fetchStep)
		}
	}

	return steps
}

// generateCheckoutStepLines generates YAML step lines for a single non-default checkout.
// The index parameter identifies the checkout's position in the ordered list, used to
// reference the correct app token minting step when app authentication is configured.
// When keepCredentialsForPush is true (safe_outputs job), credentials are retained
// (persist-credentials: true) and the post-checkout cleanup step is suppressed so a later
// git fetch/push can authenticate.
func generateCheckoutStepLines(entry *resolvedCheckout, index int, keepCredentialsForPush bool, pushToken string, getActionPin func(string) string) []string {
	checkoutManagerLog.Printf("Generating checkout step lines: index=%d, repo=%q, path=%q, ref=%q, appAuth=%v",
		index, entry.key.repository, entry.key.path, entry.ref, entry.githubApp != nil)
	name := "Checkout " + checkoutStepName(entry.key)
	var sb strings.Builder
	fmt.Fprintf(&sb, "      - name: %s\n", name)
	fmt.Fprintf(&sb, "        uses: %s\n", getActionPin("actions/checkout"))
	sb.WriteString("        with:\n")

	if keepCredentialsForPush {
		// safe_outputs job: retain credentials so later git fetch/push can authenticate.
		sb.WriteString("          persist-credentials: true\n")
	} else if entry.cleanCreds {
		sb.WriteString("          persist-credentials: true\n")
	} else {
		// Security: default behavior disables credential persistence
		sb.WriteString("          persist-credentials: false\n")
	}

	if entry.key.wiki {
		// Wiki checkout: use "{repository}.wiki" as the effective repository.
		fmt.Fprintf(&sb, "          repository: %s\n", wikiRepository(entry.key.repository))
	} else if entry.key.repository != "" {
		fmt.Fprintf(&sb, "          repository: %s\n", entry.key.repository)
	}
	if entry.ref != "" {
		fmt.Fprintf(&sb, "          ref: %s\n", entry.ref)
	}
	if entry.key.path != "" {
		fmt.Fprintf(&sb, "          path: %s\n", entry.key.path)
	}
	// Determine effective token: github-app-minted token takes precedence
	effectiveToken := resolveCheckoutTokenExpression(entry, index, false)
	// safe_outputs job: when this checkout declares no token/app of its own, persist the
	// resolved push token so the retained .git/config credential matches the token the
	// safe-output handlers use to fetch/push.
	if effectiveToken == "" && keepCredentialsForPush && pushToken != "" {
		effectiveToken = pushToken
	}
	if effectiveToken != "" {
		fmt.Fprintf(&sb, "          token: %s\n", effectiveToken)
	}
	if entry.fetchDepth != nil {
		fmt.Fprintf(&sb, "          fetch-depth: %d\n", *entry.fetchDepth)
	}
	if len(entry.sparsePatterns) > 0 {
		sb.WriteString("          sparse-checkout: |\n")
		for _, pattern := range entry.sparsePatterns {
			fmt.Fprintf(&sb, "            %s\n", strings.TrimSpace(pattern))
		}
		// Prevent actions/checkout from adding --filter=blob:none when sparse-checkout
		// is specified. Blobless clones require credentials for lazy blob fetches, but
		// agent jobs intentionally do not retain git credentials after checkout, making
		// offline git operations fail. Using blob:limit=1073741824 (1 GiB) effectively
		// fetches all blobs up front on GitHub-hosted repos (GitHub rejects blobs > 100 MiB),
		// while keeping the filter non-empty so actions/checkout won't substitute blob:none. The subsequent repair step then
		// clears partial-clone markers entirely.
		sb.WriteString("          filter: 'blob:limit=1073741824'\n")
	}
	if entry.submodules != "" {
		fmt.Fprintf(&sb, "          submodules: %s\n", entry.submodules)
	}
	if entry.lfs {
		sb.WriteString("          lfs: true\n")
	}

	steps := []string{sb.String()}
	if len(entry.sparsePatterns) > 0 {
		steps = append(steps, generateSparseCheckoutPartialCloneResetStep(entry.key.path))
	}
	if entry.cleanCreds && !keepCredentialsForPush {
		steps = append(steps, generateCheckoutCredentialsCleanupStep())
	}
	if fetchStep := generateFetchStepLines(entry, index); fetchStep != "" {
		steps = append(steps, fetchStep)
	}
	return steps
}

func generateCheckoutCredentialsCleanupStep() string {
	return `      - name: Clean git credentials after checkout
        continue-on-error: true
        run: bash "${RUNNER_TEMP}/gh-aw/actions/clean_git_credentials_checkout.sh"
`
}

func generateSparseCheckoutPartialCloneResetStep(path string) string {
	gitPrefix := "git"
	if path != "" {
		gitPrefix = fmt.Sprintf(`git -C "${{ github.workspace }}/%s"`, path)
	}
	return fmt.Sprintf(`      - name: Clear partial clone markers after sparse checkout
        continue-on-error: true
        run: |
          %s config --local --unset-all remote.origin.promisor || true
          %s config --local --unset-all remote.origin.partialclonefilter || true
`, gitPrefix, gitPrefix)
}

// checkoutStepName returns a human-readable description for a checkout step.
func checkoutStepName(key checkoutKey) string {
	if key.repository != "" && key.path != "" {
		return fmt.Sprintf("%s into %s", key.repository, key.path)
	}
	if key.repository != "" {
		return key.repository
	}
	if key.path != "" {
		return key.path
	}
	return "repository"
}

// fetchRefToRefspec converts a user-facing fetch pattern to a git refspec.
//
// Special values:
//   - "*"            → "+refs/heads/*:refs/remotes/origin/*"
//   - "refs/pulls/open/*" → "+refs/pull/*/head:refs/remotes/origin/pull/*/head"
//
// All other values are treated as branch names or glob patterns and mapped to
// the canonical remote-tracking refspec form.
func fetchRefToRefspec(pattern string) string {
	switch pattern {
	case "*":
		checkoutManagerLog.Print("Fetch refspec: wildcard expanded to all branches")
		return "+refs/heads/*:refs/remotes/origin/*"
	case "refs/pulls/open/*":
		checkoutManagerLog.Print("Fetch refspec: open PRs pattern expanded")
		return "+refs/pull/*/head:refs/remotes/origin/pull/*/head"
	default:
		// Treat as branch name or glob: map to remote tracking ref
		return fmt.Sprintf("+refs/heads/%s:refs/remotes/origin/%s", pattern, pattern)
	}
}

// generateFetchStepLines generates a "Fetch additional refs" YAML step for the given checkout
// entry when it has fetch refs configured. Returns an empty string when there are no fetch refs.
// The index parameter identifies the checkout's position in the ordered list, used to
// reference the correct app token minting step when app authentication is configured.
//
// Authentication: the token is passed as the GH_AW_FETCH_TOKEN environment variable and
// injected via git's http.extraheader config option at the command level (-c flag), which
// avoids writing credentials to disk and is consistent with the persist-credentials: false
// policy. Note that http.extraheader values are visible in the git process's environment
// (like all GitHub Actions environment variables containing secrets); GitHub Actions
// automatically masks secret values in logs.
func generateFetchStepLines(entry *resolvedCheckout, index int) string {
	if len(entry.fetchRefs) == 0 {
		return ""
	}

	checkoutManagerLog.Printf("Generating fetch step for index=%d, refs=%v", index, entry.fetchRefs)

	// Build step name
	name := "Fetch additional refs"
	if entry.key.repository != "" {
		name = "Fetch additional refs for " + entry.key.repository
	}

	// Determine authentication token
	token := resolveCheckoutTokenExpression(entry, index, true)

	// Build refspecs
	refspecs := make([]string, 0, len(entry.fetchRefs))
	for _, ref := range entry.fetchRefs {
		refspecs = append(refspecs, fmt.Sprintf("'%s'", fetchRefToRefspec(ref)))
	}

	// Build the git command, navigating to the checkout directory when needed
	gitPrefix := "git"
	if entry.key.path != "" {
		gitPrefix = fmt.Sprintf(`git -C "${{ github.workspace }}/%s"`, entry.key.path)
	}

	// Mirror the fetch-depth from the actions/checkout step so this fetch doesn't
	// expand a shallow clone into a full-history fetch.
	// - nil (unset) → actions/checkout defaults to depth=1; pass --depth=1
	// - 0           → full history explicitly requested; omit the flag
	// - N > 0       → pass --depth=N to match
	depthFlag := ""
	effectiveDepth := 1
	if entry.fetchDepth != nil {
		effectiveDepth = *entry.fetchDepth
	}
	if effectiveDepth > 0 {
		depthFlag = fmt.Sprintf(" --depth=%d", effectiveDepth)
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "      - name: %s\n", name)
	sb.WriteString("        env:\n")
	fmt.Fprintf(&sb, "          GH_AW_FETCH_TOKEN: %s\n", token)
	sb.WriteString("        run: |\n")
	sb.WriteString("          header=$(printf \"x-access-token:%s\" \"${GH_AW_FETCH_TOKEN}\" | base64 -w 0)\n")
	fmt.Fprintf(&sb, `          %s -c "http.extraheader=Authorization: Basic ${header}" fetch origin%s %s`+"\n",
		gitPrefix, depthFlag, strings.Join(refspecs, " "))
	return sb.String()
}

func resolveCheckoutTokenExpression(entry *resolvedCheckout, checkoutIndex int, defaultWhenEmpty bool) string {
	token := entry.token
	if entry.githubApp != nil {
		// The token is minted in the agent job itself (same-job step reference).
		//nolint:gosec // G101: False positive - this is a GitHub Actions expression template placeholder, not a hardcoded credential
		token = fmt.Sprintf("${{ steps.checkout-app-token-%d.outputs.token }}", checkoutIndex)
		if entry.githubApp.shouldIgnoreMissingKey() {
			token = combineTokenExpressions(token, getEffectiveGitHubToken(entry.token))
		}
	}
	if token == "" && defaultWhenEmpty {
		token = getEffectiveGitHubToken("")
	}
	return token
}
