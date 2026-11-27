package workflow

import (
	"fmt"
	"strings"
)

func (memory RepoMemoryEntry) githubApp(level PermissionLevel) *GitHubAppConfig {
	if level == PermissionRead && memory.GitHubAppRead != nil {
		return memory.GitHubAppRead
	}
	if level == PermissionWrite && memory.GitHubAppWrite != nil {
		return memory.GitHubAppWrite
	}
	return memory.GitHubApp
}

func repoMemoryAppTokenStepID(memory RepoMemoryEntry, level PermissionLevel) string {
	if level == PermissionRead {
		return "repo-memory-app-read-token-" + memory.ID
	}
	return "repo-memory-app-token-" + memory.ID
}

func repoMemoryAppTargetStepID(memory RepoMemoryEntry, level PermissionLevel) string {
	if level == PermissionRead {
		return "repo-memory-app-read-target-" + memory.ID
	}
	return "repo-memory-app-target-" + memory.ID
}

func repoMemoryAppTargetCondition(memory RepoMemoryEntry, level PermissionLevel) string {
	if memory.githubApp(level) == nil || !strings.Contains(memory.TargetRepo, "${{") {
		return ""
	}
	return fmt.Sprintf("steps.%s.outcome == 'success'", repoMemoryAppTargetStepID(memory, level))
}

func repoMemoryTargetRepo(memory RepoMemoryEntry, level PermissionLevel) string {
	target := memory.TargetRepo
	if target == "" {
		target = "${{ github.repository }}"
	}
	if repoMemoryAppTargetCondition(memory, level) != "" {
		id := repoMemoryAppTargetStepID(memory, level)
		target = fmt.Sprintf("${{ steps.%s.outputs.owner }}/${{ steps.%s.outputs.repository }}", id, id)
	}
	if memory.Wiki {
		target += ".wiki"
	}
	return target
}

// repoMemoryAppToken requires memory.githubApp(level) to be non-nil.
func repoMemoryAppToken(memory RepoMemoryEntry, level PermissionLevel) string {
	if memory.githubApp(level).IgnoreIfMissing {
		return fmt.Sprintf("${{ steps.%s.outputs.token || github.token }}", repoMemoryAppTokenStepID(memory, level))
	}
	return fmt.Sprintf("${{ steps.%s.outputs.token }}", repoMemoryAppTokenStepID(memory, level))
}

func (c *Compiler) buildRepoMemoryAppTokenMintStep(memory RepoMemoryEntry, level PermissionLevel, jobName string, conditions ...string) []string {
	app := *memory.githubApp(level)
	// Repo-memory never inherits additional app permissions.
	app.Permissions = nil
	if strings.TrimSpace(app.AppID) == "" {
		app.AppID = "${{ '' }}"
	}
	if strings.TrimSpace(app.PrivateKey) == "" {
		app.PrivateKey = "${{ '' }}"
	}
	if level == PermissionWrite {
		conditions = append([]string{"always()"}, conditions...)
	}
	var steps []string
	repository := "${{ github.event.repository.name }}"
	if memory.TargetRepo != "" {
		_, repository, _ = strings.Cut(memory.TargetRepo, "/")
	}
	if strings.Contains(memory.TargetRepo, "${{") {
		targetStepID := repoMemoryAppTargetStepID(memory, level)
		steps = append(steps, buildRepoMemoryAppTargetStep(memory, level, conditions)...)
		repository = fmt.Sprintf("${{ steps.%s.outputs.repository }}", targetStepID)
		app.Owner = fmt.Sprintf("${{ steps.%s.outputs.owner }}", targetStepID)
		app.Repositories = nil
		conditions = append(conditions, repoMemoryAppTargetCondition(memory, level))
	}
	permissions := NewPermissionsFromMap(map[PermissionScope]PermissionLevel{PermissionContents: level})
	mint := c.buildGitHubAppTokenMintStepForJob(jobName, &app, permissions, repository, memory.TargetRepo,
		"Generate repo-memory GitHub App token ("+memory.ID+")", repoMemoryAppTokenStepID(memory, level), conditions...)
	return append(steps, mint...)
}

func buildRepoMemoryAppTargetStep(memory RepoMemoryEntry, level PermissionLevel, conditions []string) []string {
	allowedRepos := []string{"${{ github.repository }}"}
	app := memory.githubApp(level)
	owner := app.Owner
	if owner == "" {
		owner = "${{ github.repository_owner }}"
	}
	for _, repo := range app.Repositories {
		allowedRepos = append(allowedRepos, fmt.Sprintf("%s/%s", owner, repo))
	}
	steps := []string{
		"      - name: Resolve repo-memory GitHub App target\n",
		fmt.Sprintf("        id: %s\n", repoMemoryAppTargetStepID(memory, level)),
	}
	if len(conditions) > 0 {
		steps = append(steps, fmt.Sprintf("        if: %s\n", combineGitHubIfExpressions(conditions...)))
	}
	return append(steps,
		"        env:\n",
		fmt.Sprintf("          GH_AW_TARGET_REPOSITORY: %s\n", memory.TargetRepo),
		fmt.Sprintf("          GH_AW_ALLOWED_REPOSITORIES: %q\n", strings.Join(allowedRepos, " ")),
		"        shell: bash\n",
		"        run: |\n",
		"          set -euo pipefail\n",
		"          [[ \"$GH_AW_TARGET_REPOSITORY\" =~ ^[a-zA-Z0-9_.-]+/[a-zA-Z0-9_.-]+$ ]] || { echo 'Invalid repo-memory target-repo; expected owner/repo' >&2; exit 1; }\n",
		"          case \" ${GH_AW_ALLOWED_REPOSITORIES,,} \" in\n",
		"            *\" ${GH_AW_TARGET_REPOSITORY,,} \"*) ;;\n",
		"            *) echo 'Repo-memory target-repo is not authorized by github-app.repositories' >&2; exit 1 ;;\n",
		"          esac\n",
		"          printf 'owner=%s\\nrepository=%s\\n' \"${GH_AW_TARGET_REPOSITORY%%/*}\" \"${GH_AW_TARGET_REPOSITORY#*/}\" >> \"$GITHUB_OUTPUT\"\n",
	)
}
