package workflow

import (
	"fmt"
	"strings"
)

type credentialSubrepository struct {
	repository     string
	path           string
	envVarName     string
	pathEnvVarName string
}

func (cm *CheckoutManager) effectiveRootCheckoutRepository() string {
	root := "${{ github.repository }}"
	if override := cm.GetDefaultCheckoutOverride(); override != nil && override.key.wiki {
		root = wikiRepository(override.key.repository)
	}
	for _, entry := range cm.ordered {
		if entry.key.path == "" && entry.key.repository != "" {
			if entry.key.wiki {
				root = wikiRepository(entry.key.repository)
			} else {
				root = entry.key.repository
			}
		}
	}
	return root
}

func (cm *CheckoutManager) credentialSubrepositories() []credentialSubrepository {
	var repositories []credentialSubrepository
	for _, entry := range cm.ordered {
		if entry.key.repository == "" || entry.key.path == "" || entry.key.wiki || entry.key.multi {
			continue
		}
		index := len(repositories)
		repo := credentialSubrepository{
			repository: entry.key.repository,
			path:       entry.key.path,
			envVarName: fmt.Sprintf("GH_AW_SUBREPO_%d", index),
		}
		if strings.Contains(repo.path, "${{") {
			repo.pathEnvVarName = fmt.Sprintf("GH_AW_SUBREPO_PATH_%d", index)
		}
		repositories = append(repositories, repo)
	}
	return repositories
}

func (cm *CheckoutManager) runtimeRootCandidates() []credentialSubrepository {
	hasRuntimePath := false
	for _, entry := range cm.ordered {
		hasRuntimePath = hasRuntimePath || (!entry.key.multi && strings.Contains(entry.key.path, "${{"))
	}
	if !hasRuntimePath {
		return nil
	}
	var candidates []credentialSubrepository
	// Include literal paths too: a later literal root can supersede a runtime root.
	for _, entry := range cm.ordered {
		if entry.isDefault() || entry.key.multi {
			continue
		}
		repository := entry.key.repository
		if entry.key.wiki {
			repository = wikiRepository(repository)
		} else if repository == "" {
			repository = "${{ github.repository }}"
		}
		index := len(candidates)
		candidates = append(candidates, credentialSubrepository{
			repository: repository, path: entry.key.path,
			envVarName:     fmt.Sprintf("GH_AW_ROOT_REPO_%d", index),
			pathEnvVarName: fmt.Sprintf("GH_AW_ROOT_PATH_%d", index),
		})
	}
	return candidates
}

func runtimeRootEnvironmentLines(candidates []credentialSubrepository) []string {
	if len(candidates) == 0 {
		return nil
	}
	var env strings.Builder
	for _, candidate := range candidates {
		writeYAMLEnv(&env, "          ", candidate.envVarName, candidate.repository)
		writeYAMLEnv(&env, "          ", candidate.pathEnvVarName, candidate.path)
	}
	return []string{env.String()}
}

func runtimeRootIdentityLines(candidates []credentialSubrepository) []string {
	if len(candidates) == 0 {
		return nil
	}
	lines := []string{"          GH_AW_ROOT_DIRECTORY=$(pwd -P)\n"}
	for _, candidate := range candidates {
		lines = append(lines,
			fmt.Sprintf("          GH_AW_CHECKOUT_PATH=\"${%s:-.}\"\n", candidate.pathEnvVarName),
			"          if [ \"$GH_AW_CHECKOUT_PATH\" = \"-\" ]; then GH_AW_CHECKOUT_PATH=\"./-\"; fi\n",
			"          if ! GH_AW_CHECKOUT_DIRECTORY=$(cd -- \"$GH_AW_CHECKOUT_PATH\" 2>/dev/null && pwd -P); then\n",
			"            echo \"Unable to resolve checkout directory for Git identity verification\" >&2\n",
			"            exit 1\n",
			"          fi\n",
			fmt.Sprintf("          if [ \"$GH_AW_CHECKOUT_DIRECTORY\" = \"$GH_AW_ROOT_DIRECTORY\" ]; then GH_AW_GIT_REPOSITORY=\"${%s}\"; fi\n", candidate.envVarName),
		)
	}
	return append(lines, "          export GH_AW_GIT_REPOSITORY\n")
}

func credentialEnvironmentLines(root, token string, repositories []credentialSubrepository) []string {
	lines := []string{
		"        env:\n",
		fmt.Sprintf("          GH_AW_GIT_REPOSITORY: %s\n", root),
		"          GITHUB_SERVER_URL: ${{ github.server_url }}\n",
		fmt.Sprintf("          GIT_TOKEN: %s\n", token),
	}
	for _, repo := range repositories {
		if strings.Contains(repo.repository, "${{") {
			lines = append(lines, fmt.Sprintf("          %s: %s\n", repo.envVarName, githubExpressionWhitespaceReplacer.Replace(repo.repository)))
		} else {
			lines = append(lines, formatYAMLEnv("          ", repo.envVarName, repo.repository))
		}
		if repo.pathEnvVarName != "" {
			lines = append(lines, fmt.Sprintf("          %s: %s\n", repo.pathEnvVarName, githubExpressionWhitespaceReplacer.Replace(repo.path)))
		}
	}
	return lines
}

func credentialCommandLines(repositories []credentialSubrepository) []string {
	lines := []string{
		"        run: |\n",
		"          bash \"${RUNNER_TEMP}/gh-aw/actions/configure_git_credentials.sh\"\n",
	}
	for _, repo := range repositories {
		gitDir := fmt.Sprintf("%q", repo.path)
		commentRef := repo.path
		if repo.pathEnvVarName != "" {
			// A runtime "-" must address the literal checkout, not OLDPWD.
			lines = append(lines, fmt.Sprintf("          if [ \"${%s}\" = \"-\" ]; then %s=\"./-\"; fi\n", repo.pathEnvVarName, repo.pathEnvVarName))
			gitDir = fmt.Sprintf("\"${%s}\"", repo.pathEnvVarName)
			commentRef = "${" + repo.pathEnvVarName + "}"
		} else if repo.path == "-" {
			gitDir = `"./-"`
		}
		lines = append(lines,
			fmt.Sprintf("          # Re-authenticate git for %s\n", commentRef),
			fmt.Sprintf("          (cd -- %s && GH_AW_GIT_REPOSITORY=\"${%s}\" bash \"${RUNNER_TEMP}/gh-aw/actions/configure_git_credentials.sh\")\n", gitDir, repo.envVarName),
		)
	}
	return append(lines, "          echo \"Git configured with standard GitHub Actions identity\"\n")
}

func runtimeRootCredentialCommandLines(repositories, candidates []credentialSubrepository) []string {
	if len(candidates) == 0 {
		return credentialCommandLines(repositories)
	}
	lines := []string{"        run: |\n"}
	lines = append(lines, runtimeRootIdentityLines(candidates)...)
	lines = append(lines, "          bash \"${RUNNER_TEMP}/gh-aw/actions/configure_git_credentials.sh\"\n")
	for _, repo := range repositories {
		gitDir := fmt.Sprintf("%q", repo.path)
		if repo.pathEnvVarName != "" {
			lines = append(lines, fmt.Sprintf("          if [ \"${%s}\" = \"-\" ]; then %s=\"./-\"; fi\n", repo.pathEnvVarName, repo.pathEnvVarName))
			gitDir = fmt.Sprintf("\"${%s:-.}\"", repo.pathEnvVarName)
		} else if repo.path == "-" {
			gitDir = `"./-"`
		}
		// Root checkouts have already been authenticated using the last root identity.
		lines = append(lines, fmt.Sprintf("          (cd -- %s && if [ \"$(pwd -P)\" != \"$GH_AW_ROOT_DIRECTORY\" ]; then GH_AW_GIT_REPOSITORY=\"${%s}\" bash \"${RUNNER_TEMP}/gh-aw/actions/configure_git_credentials.sh\"; fi)\n", gitDir, repo.envVarName))
	}
	return lines
}
