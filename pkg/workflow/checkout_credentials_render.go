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
