package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
)

var privateCloneSHAPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

func privateCloneGit(ctx context.Context, dir string, args ...string) ([]byte, error) {
	base := []string{
		"-c", "core.hooksPath=" + os.DevNull,
		"-c", "core.fsmonitor=false",
		"-c", "credential.helper=",
		"-c", "credential.helper=!gh auth git-credential",
		"-c", "protocol.file.allow=never",
	}
	cmd := exec.CommandContext(ctx, "git", append(base, args...)...)
	cmd.Dir = dir
	cmd.Env = privateCloneGitEnvironment(dir)
	data, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("private-clone git operation failed: %w", err)
	}
	return data, nil
}

// privateCloneGitEnvironment drops inherited GIT_* overrides (GIT_DIR, GIT_WORK_TREE,
// GIT_INDEX_FILE, GIT_CONFIG_*) so every operation targets the disposable clone.
func privateCloneGitEnvironment(dir string) []string {
	env := make([]string, 0, len(os.Environ())+6)
	for _, value := range os.Environ() {
		key, _, _ := strings.Cut(value, "=")
		if !strings.HasPrefix(strings.ToUpper(key), "GIT_") && !strings.EqualFold(key, "SSH_ASKPASS") {
			env = append(env, value)
		}
	}
	return append(env, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_CEILING_DIRECTORIES="+filepath.Dir(dir), "GIT_TERMINAL_PROMPT=0",
		"GIT_NO_REPLACE_OBJECTS=1", "GIT_LFS_SKIP_SMUDGE=1")
}

var privateCloneCommitIdentity = []string{
	"-c", "user.name=GitHub Agentic Workflows", "-c", "user.email=actions@users.noreply.github.com", "-c", "commit.gpgsign=false",
}

// preparePrivateCloneSource checks out the selected source tree as an
// independent root commit, so publication transfers no upstream history.
func preparePrivateCloneSource(ctx context.Context, dir, source, sha string) error {
	for _, args := range [][]string{
		{"init", "--initial-branch=main", "--template="},
		{"fetch", "--no-tags", "--depth=1", trialRepositoryGitURL(source), sha},
	} {
		if _, err := privateCloneGit(ctx, dir, args...); err != nil {
			return fmt.Errorf("prepare upstream snapshot: %w", err)
		}
	}
	return snapshotPrivateCloneSource(ctx, dir, sha)
}

func snapshotPrivateCloneSource(ctx context.Context, dir, sha string) error {
	if err := validatePrivateCloneSourceTree(ctx, dir, sha); err != nil {
		return err
	}
	for _, args := range [][]string{
		{"checkout", "--detach", sha},
		{"checkout", "--orphan", "main"},
		append(slices.Clone(privateCloneCommitIdentity), "commit", "--allow-empty", "-m", "Source snapshot "+sha),
	} {
		if _, err := privateCloneGit(ctx, dir, args...); err != nil {
			return fmt.Errorf("prepare upstream snapshot: %w", err)
		}
	}
	return nil
}

func commitPrivateCloneBootstrap(ctx context.Context, dir string) ([]byte, error) {
	for _, args := range [][]string{
		{"add", "--all"},
		{"add", "--force", "--", constants.WorkflowsDir, privateCloneContextDir},
		append(slices.Clone(privateCloneCommitIdentity), "commit", "--allow-empty", "-m", "Configure private remediation workspace"),
		{"tag", "private-clone/bootstrap"},
	} {
		if _, err := privateCloneGit(ctx, dir, args...); err != nil {
			return nil, err
		}
	}
	return privateCloneGit(ctx, dir, "rev-parse", "HEAD")
}

func publishPrivateClone(ctx context.Context, dir, destination string) error {
	if _, err := privateCloneGH(ctx, "repo", "create", destination, "--private", "--disable-wiki"); err != nil {
		return fmt.Errorf("create a new private repository (existing repositories are not reused): %w", err)
	}
	if _, err := privateCloneGH(ctx, "api", "--method", "PUT", path.Join("repos", destination, "actions/permissions"), "-F", "enabled=false"); err != nil {
		return fmt.Errorf("disable Actions in newly created %s; no source was pushed: %w", destination, err)
	}
	if _, err := privateCloneGH(ctx, "api", "--method", "PUT", path.Join("repos", destination, "actions/permissions/workflow"),
		"-f", "default_workflow_permissions=read", "-F", "can_approve_pull_request_reviews=true"); err != nil {
		return fmt.Errorf("configure clone-local PR creation in %s (Actions remains disabled): %w", destination, err)
	}
	if err := verifyPrivateCloneDestination(ctx, destination); err != nil {
		return err
	}
	for _, args := range [][]string{
		{"remote", "add", "origin", trialRepositoryGitURL(destination)},
		{"push", "origin", "HEAD:refs/heads/main", "refs/tags/private-clone/bootstrap"},
	} {
		if _, err := privateCloneGit(ctx, dir, args...); err != nil {
			return fmt.Errorf("publish %s (Actions remains disabled): %w", destination, err)
		}
	}
	return nil
}

func verifyPrivateCloneDestination(ctx context.Context, destination string) error {
	data, err := privateCloneGH(ctx, "api", "repos/"+destination)
	if err != nil {
		return err
	}
	var repo struct {
		Private    *bool  `json:"private"`
		Visibility string `json:"visibility"`
		Fork       *bool  `json:"fork"`
		FullName   string `json:"full_name"`
	}
	if err := json.Unmarshal(data, &repo); err != nil {
		return fmt.Errorf("verify private-clone destination: %w", err)
	}
	// private is also true for internal repositories, which enterprise members can read.
	if repo.Private == nil || !*repo.Private || repo.Visibility != "private" || repo.Fork == nil || *repo.Fork || !strings.EqualFold(repo.FullName, destination) {
		return errors.New("private-clone destination must be the requested standalone private repository; no source was pushed")
	}
	data, err = privateCloneGH(ctx, "api", path.Join("repos", destination, "actions/permissions"))
	if err != nil {
		return err
	}
	var permissions struct {
		Enabled *bool `json:"enabled"`
	}
	if err := json.Unmarshal(data, &permissions); err != nil {
		return fmt.Errorf("verify Actions is disabled: %w", err)
	}
	if permissions.Enabled == nil || *permissions.Enabled {
		return errors.New("private-clone requires Actions to be disabled before pushing; no source was pushed")
	}
	return nil
}

func startPrivateCloneInvestigation(ctx context.Context, destination, starterWorkflow string) error {
	if _, err := privateCloneGH(ctx, "api", "--method", "PUT", path.Join("repos", destination, "actions/permissions"), "-F", "enabled=true"); err != nil {
		return fmt.Errorf("source and secrets were pushed to %s, but Actions could not be enabled; no workflow was dispatched: %w", destination, err)
	}
	data, err := privateCloneGH(ctx, "api", path.Join("repos", destination, "actions/permissions"))
	if err != nil {
		return fmt.Errorf("verify Actions is enabled in %s: %w", destination, err)
	}
	var permissions struct {
		Enabled *bool `json:"enabled"`
	}
	if err := json.Unmarshal(data, &permissions); err != nil {
		return fmt.Errorf("verify Actions is enabled in %s: %w", destination, err)
	}
	if permissions.Enabled == nil || !*permissions.Enabled {
		return fmt.Errorf("actions is not enabled in %s; starter workflow was not dispatched", destination)
	}
	if _, err := privateCloneGH(ctx, "workflow", "run", starterWorkflow, "--repo", destination, "--ref", "main"); err != nil {
		return fmt.Errorf("actions is enabled in %s, but starter workflow %s could not be dispatched: %w", destination, starterWorkflow, err)
	}
	return nil
}
