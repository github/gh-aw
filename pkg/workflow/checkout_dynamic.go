package workflow

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/github/gh-aw/pkg/workflow/actionslanguageservice"
)

const defaultCheckoutMaxRepos = 10

// Only direct configuration-variable references establish trusted policy provenance.
var checkoutAllowedReposVariablePattern = actionslanguageservice.ReferencePattern(true, false, func(path []string) []string {
	if len(path) == 2 && strings.EqualFold(path[0], "vars") && credentialIdentifier(path[1], false) {
		return []string{}
	}
	return nil
})

func validateCheckoutAllowedRepos(values []string) error {
	for _, value := range values {
		if hasExpressionMarker(value) {
			if !checkoutAllowedReposVariablePattern.MatchString(strings.TrimSpace(value)) {
				return errors.New("checkout.allowed-repos expressions must be a direct repository or organization variable reference such as ${{ vars.ALLOWED_REPOS }}; use literal scoped patterns or a trusted configuration variable instead of caller-controlled or composite expressions")
			}
			continue
		}
		for _, pattern := range strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == '\n' || r == '\r' }) {
			pattern = strings.TrimSpace(pattern)
			if pattern != "" && strings.Trim(pattern, "*/") == "" {
				return errors.New("checkout.allowed-repos must not allow every repository; use scoped patterns such as owner/* or */repo instead of *")
			}
		}
	}
	return nil
}

func generateSingleCheckoutValidationStep(entry *resolvedCheckout, index int, getActionPin func(string) string) string {
	var env strings.Builder
	writeYAMLEnv(&env, "          ", "GH_AW_CHECKOUT_REPOSITORY", entry.key.repository)
	return fmt.Sprintf("      - name: Validate single checkout repository (%d)\n        uses: %s\n        env:\n%s        with:\n          script: |\n            const { validateSingleRepository } = require('${{ runner.temp }}/gh-aw/actions/checkout_dynamic.cjs');\n            validateSingleRepository(process.env.GH_AW_CHECKOUT_REPOSITORY);\n",
		index, getActionPin("actions/github-script"), env.String())
}

func generateDynamicCheckoutSteps(entry *resolvedCheckout, index int, keepCredentials bool, pushToken string, getActionPin func(string) string) []string {
	env := buildDynamicCheckoutEnvironment(entry, index, keepCredentials, pushToken, getActionPin)
	prepare := buildDynamicCheckoutScriptStep("Validate runtime checkout repositories", "prepare", "await prepare({ core, github, getOctokit });", index, env, getActionPin)
	checkout := fmt.Sprintf("      - name: Checkout runtime repositories (%d)\n        uses: ./.gh-aw/checkouts/%d\n        env:\n%s", index, index, env)
	finish := buildDynamicCheckoutScriptStep("Finalize runtime checkouts", "finish",
		fmt.Sprintf("await finish({ cleanCredentials: %t });", !keepCredentials), index, env, getActionPin)
	steps := []string{prepare, checkout}
	if entry.cleanCreds && !keepCredentials {
		steps = append(steps, generateCheckoutCredentialsCleanupStep())
	}
	return append(steps, finish)
}

func dynamicCheckoutFailureOutput(configs []*CheckoutConfig) string {
	var outputs []string
	for index, entry := range NewCheckoutManager(configs).ordered {
		if entry.key.multi {
			outputs = append(outputs, fmt.Sprintf("steps.checkout-validate-%d.outputs.checkout_error", index))
		}
	}
	if len(outputs) == 0 {
		return ""
	}
	return "${{ " + strings.Join(outputs, " || ") + " }}"
}

func buildDynamicCheckoutFailureEnv(data *WorkflowData, jobName string) []string {
	if dynamicCheckoutFailureOutput(data.CheckoutConfigs) == "" {
		return nil
	}
	checkoutError := "needs." + jobName + ".outputs.checkout_error"
	if usesPatchesAndCheckouts(data.SafeOutputs) {
		checkoutError += " || needs.safe_outputs.outputs.checkout_error"
	}
	return []string{"          GH_AW_CHECKOUT_ERROR: ${{ " + checkoutError + " }}\n"}
}

func dynamicCheckoutToken(entry *resolvedCheckout, index int, keepCredentials bool, pushToken string) string {
	token := resolveCheckoutTokenExpression(entry, index, false)
	if keepCredentials && entry.safeOutputApp != nil {
		token = fmt.Sprintf("${{ steps.checkout-safe-output-app-token-%d.outputs.token }}", index)
		if entry.safeOutputApp.shouldIgnoreMissingKey() {
			fallback := resolveCheckoutTokenExpression(entry, index, true)
			if entry.githubApp == nil && entry.token == "" && pushToken != "" {
				fallback = pushToken
			}
			token = combineTokenExpressions(token, fallback)
		}
	}
	if token == "" {
		token = pushToken
	}
	if token == "" {
		token = resolveGitHubToken("")
	}
	return token
}

func buildDynamicCheckoutEnvironment(entry *resolvedCheckout, index int, keepCredentials bool, pushToken string, getActionPin func(string) string) string {
	depth := 1
	if entry.fetchDepth != nil {
		depth = *entry.fetchDepth
	}
	var env strings.Builder
	writeYAMLEnv(&env, "          ", "GH_AW_DYNAMIC_CHECKOUT_INDEX", strconv.Itoa(index))
	writeYAMLEnv(&env, "          ", "GH_AW_DYNAMIC_REPOSITORIES", entry.key.repository)
	// Expressions remain data in environment variables, never executable script text.
	writeYAMLEnv(&env, "          ", "GH_AW_DYNAMIC_ALLOWED_REPOS", strings.Join(entry.allowedRepos, "\n"))
	writeYAMLEnv(&env, "          ", "GH_AW_DYNAMIC_MAX_REPOS", strconv.Itoa(entry.maxRepos))
	writeYAMLEnv(&env, "          ", "GH_AW_DYNAMIC_ALLOW_PRIVATE_REPOS", strconv.FormatBool(entry.allowPrivateRepos))
	writeYAMLEnv(&env, "          ", "GH_AW_DYNAMIC_PREFIX", entry.key.path)
	writeYAMLEnv(&env, "          ", "GH_AW_DYNAMIC_REF", entry.ref)
	writeYAMLEnv(&env, "          ", "GH_AW_DYNAMIC_FETCH_DEPTH", strconv.Itoa(depth))
	writeYAMLEnv(&env, "          ", "GH_AW_DYNAMIC_SPARSE", strings.Join(entry.sparsePatterns, "\n"))
	writeYAMLEnv(&env, "          ", "GH_AW_DYNAMIC_SUBMODULES", entry.submodules)
	writeYAMLEnv(&env, "          ", "GH_AW_DYNAMIC_LFS", strconv.FormatBool(entry.lfs))
	writeYAMLEnv(&env, "          ", "GH_AW_DYNAMIC_FETCH_COUNT", strconv.Itoa(len(entry.fetchRefs)))
	for i, ref := range entry.fetchRefs {
		writeYAMLEnv(&env, "          ", fmt.Sprintf("GH_AW_DYNAMIC_FETCH_%d", i), ref)
	}
	writeYAMLEnv(&env, "          ", "GH_AW_DYNAMIC_PERSIST", strconv.FormatBool(keepCredentials || entry.cleanCreds))
	checkoutAction, _, _ := strings.Cut(getActionPin("actions/checkout"), " #")
	writeYAMLEnv(&env, "          ", "GH_AW_DYNAMIC_CHECKOUT_ACTION", checkoutAction)
	env.WriteString("          GH_AW_DYNAMIC_TOKEN: " + dynamicCheckoutToken(entry, index, keepCredentials, pushToken) + "\n")
	return env.String()
}

func buildDynamicCheckoutScriptStep(name, function, invocation string, index int, env string, getActionPin func(string) string) string {
	stepID := ""
	if function == "prepare" {
		stepID = fmt.Sprintf("        id: checkout-validate-%d\n", index)
	}
	return fmt.Sprintf("      - name: %s (%d)\n%s        uses: %s\n        env:\n%s        with:\n          script: |\n            const { %s } = require('${{ runner.temp }}/gh-aw/actions/checkout_dynamic.cjs');\n            %s\n",
		name, index, stepID, getActionPin("actions/github-script"), env, function, invocation)
}
