package workflow

import (
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var githubAppOwnerDerivationLog = logger.New("workflow:github_app_owner_derivation")

// inferSingleCheckoutRepositoryForGitHubAppOwner returns the single explicit checkout.repository
// value when the workflow targets exactly one distinct repository. It ignores the default checkout
// of the current repository and returns an empty string when multiple distinct repositories are
// configured.
func inferSingleCheckoutRepositoryForGitHubAppOwner(data *WorkflowData) string {
	if data == nil {
		return ""
	}

	checkoutMgr := NewCheckoutManager(data.CheckoutConfigs)
	var repository string
	for _, entry := range checkoutMgr.ordered {
		if entry.key.repository == "" {
			continue
		}
		if repository == "" {
			githubAppOwnerDerivationLog.Printf("Using checkout.repository %q as GitHub App owner source candidate", entry.key.repository)
			repository = entry.key.repository
			continue
		}
		if entry.key.repository != repository {
			githubAppOwnerDerivationLog.Printf("Cannot infer a single GitHub App owner source: found multiple checkout repositories (%q, %q)", repository, entry.key.repository)
			return ""
		}
	}

	if repository == "" && hasWorkflowCallTrigger(data.On) {
		githubAppOwnerDerivationLog.Print("No explicit checkout.repository found for workflow_call; using needs.activation.outputs.target_repo as owner source")
		return targetRepoExprForDownstreamJob()
	}

	if repository == "" {
		githubAppOwnerDerivationLog.Print("No explicit checkout.repository found; GitHub App owner will fall back to github.repository_owner")
	}

	return repository
}

func buildGitHubAppOwnerResolutionSteps(repositoryExpr, stepName, stepID string) (string, []string) {
	if owner, ok := deriveLiteralGitHubAppOwner(repositoryExpr); ok {
		githubAppOwnerDerivationLog.Printf("Resolved GitHub App owner %q from literal repository %q", owner, repositoryExpr)
		return owner, nil
	}
	if strings.TrimSpace(repositoryExpr) == "" {
		githubAppOwnerDerivationLog.Print("No repository expression provided for GitHub App owner derivation; using github.repository_owner")
		return "${{ github.repository_owner }}", nil
	}

	ownerStepID := stepID + "-owner"
	ownerStepName := strings.Replace(stepName, "Generate GitHub App token", "Derive GitHub App owner", 1)
	if ownerStepName == stepName {
		ownerStepName = "Derive GitHub App owner"
	}

	var steps []string
	steps = append(steps, fmt.Sprintf("      - name: %s\n", ownerStepName))
	steps = append(steps, fmt.Sprintf("        id: %s\n", ownerStepID))
	steps = append(steps, "        env:\n")
	steps = append(steps, fmt.Sprintf("          GH_AW_TARGET_REPOSITORY: %s\n", githubExpressionWhitespaceReplacer.Replace(repositoryExpr)))
	steps = append(steps, "        shell: bash\n")
	steps = append(steps, "        run: |\n")
	steps = append(steps, "          set -euo pipefail\n")
	steps = append(steps, "          echo \"[gh-aw] Deriving GitHub App owner from GH_AW_TARGET_REPOSITORY\"\n")
	steps = append(steps, "          repo=\"${GH_AW_TARGET_REPOSITORY%.wiki}\"\n")
	steps = append(steps, "          echo \"[gh-aw] Normalized repository candidate: $repo\"\n")
	steps = append(steps, "          owner=\"${repo%%/*}\"\n")
	steps = append(steps, "          if [[ -z \"$repo\" || -z \"$owner\" || \"$owner\" == \"$repo\" ]]; then\n")
	steps = append(steps, "            echo \"[gh-aw] Unable to derive GitHub App owner from repository: $GH_AW_TARGET_REPOSITORY\" >&2\n")
	steps = append(steps, "            exit 1\n")
	steps = append(steps, "          fi\n")
	steps = append(steps, "          echo \"[gh-aw] Derived GitHub App owner: $owner\"\n")
	steps = append(steps, "          echo \"owner=$owner\" >> \"$GITHUB_OUTPUT\"\n")

	return "${{ steps." + ownerStepID + ".outputs.owner }}", steps
}

func resolveGitHubAppOwner(app *GitHubAppConfig, ownerSourceRepository, stepName, stepID string) (string, []string) {
	if app != nil && strings.TrimSpace(app.Owner) != "" {
		return app.Owner, nil
	}
	return buildGitHubAppOwnerResolutionSteps(ownerSourceRepository, stepName, stepID)
}

func deriveLiteralGitHubAppOwner(repository string) (string, bool) {
	repository = strings.TrimSpace(repository)
	if repository == "" {
		return "", false
	}

	owner, _, hasRepo := strings.Cut(repository, "/")
	if !hasRepo {
		return "", false
	}

	owner = strings.TrimSpace(owner)
	if owner == "" || strings.Contains(owner, "${{") {
		return "", false
	}
	return owner, true
}
