package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/ctxutil"
	"github.com/github/gh-aw/pkg/errorutil"
	"github.com/github/gh-aw/pkg/gitutil"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/repoutil"
	"github.com/github/gh-aw/pkg/workflow"
)

var setupRepositoryLog = logger.New("cli:setup_repository")

func configureDefaultGHHostFromOriginRemoteIfUnset() {
	if os.Getenv("GH_HOST") != "" { //nolint:osgetenvlibrary
		return
	}

	if detectedHost := getHostFromOriginRemote(); detectedHost != "" && detectedHost != "github.com" {
		setupRepositoryLog.Printf("Auto-detected GHES host from git remote: %s", detectedHost)
		workflow.SetDefaultGHHost(detectedHost)
	} else if detectedHost == "github.com" {
		workflow.SetDefaultGHHost("")
	}
}

type SetupAuthOptions struct {
	Ctx  context.Context
	JSON bool
}

type SetupRepositoryCheckOptions struct {
	Ctx              context.Context
	Repo             string
	Dir              string
	RequireOwnerType string
	Verbose          bool
	JSON             bool
}

type SetupAuthResult struct {
	Authenticated bool `json:"authenticated"`
}

type SetupRepositoryCheckResult struct {
	Repository        string `json:"repository"`
	Directory         string `json:"directory"`
	Authenticated     bool   `json:"authenticated"`
	RepositoryExists  bool   `json:"repository_exists"`
	OwnerType         string `json:"owner_type"`
	RequiredOwnerType string `json:"required_owner_type"`
	CheckoutAttached  bool   `json:"checkout_attached"`
	CloneNeeded       bool   `json:"clone_needed"`
	CleanWorktree     *bool  `json:"clean_worktree,omitempty"`
}

// setupRepositoryRuntime holds the reusable repository/auth setup primitives that
// higher-level setup flows can compose. The helpers are intentionally generic so
// future auth/setup commands can reuse them.
type setupRepositoryRuntime struct {
	checkAuth          func(context.Context) error
	repoExists         func(context.Context, string) (bool, error)
	ownerType          func(context.Context, string) (string, error)
	createRepo         func(context.Context, string, string) error
	cloneRepo          func(context.Context, string, string) error
	dirOriginRepo      func(string) (string, error)
	checkCleanWorktree func(bool) error
}

func defaultSetupRepositoryRuntime() setupRepositoryRuntime {
	return setupRepositoryRuntime{
		checkAuth: func(context.Context) error {
			return checkGHAuthStatusShared(false)
		},
		repoExists: checkSetupRepositoryExists,
		ownerType:  checkSetupRepositoryOwnerType,
		createRepo: createSetupRepository,
		cloneRepo:  cloneSetupRepository,
		dirOriginRepo: func(dir string) (string, error) {
			remoteURL, _, err := resolveRemoteURL(dir)
			if err != nil {
				return "", err
			}
			repo := parseGitHubRepoSlugFromURL(remoteURL)
			if repo == "" {
				return "", fmt.Errorf("remote URL for %s does not point to a GitHub repository", dir)
			}
			return repo, nil
		},
		checkCleanWorktree: checkCleanWorkingDirectory,
	}
}

func checkSetupRepositoryExists(ctx context.Context, repo string) (bool, error) {
	output, err := workflow.RunGHCombinedContext(ctx, "Checking repository...", "repo", "view", repo, "--json", "nameWithOwner", "--jq", ".nameWithOwner")
	if err == nil {
		return strings.TrimSpace(string(output)) != "", nil
	}

	outputStr := string(output)
	message := strings.ToLower(outputStr)
	if strings.Contains(message, "could not resolve to a repository") || errorutil.IsNotFoundOutput(outputStr) {
		return false, nil
	}
	return false, fmt.Errorf("failed to check repository %s: %w", repo, err)
}

func checkSetupRepositoryOwnerType(ctx context.Context, owner string) (string, error) {
	output, err := workflow.RunGHContext(ctx, "Checking owner type...", "api", "users/"+owner, "--jq", ".type")
	if err == nil {
		return strings.TrimSpace(string(output)), nil
	}

	output, err = workflow.RunGHContext(ctx, "Checking owner type...", "api", "orgs/"+owner, "--jq", ".type")
	if err != nil {
		return "", fmt.Errorf("failed to check owner type for %s: %w", owner, err)
	}
	return strings.TrimSpace(string(output)), nil
}

func createSetupRepository(ctx context.Context, repo string, visibility string) error {
	output, err := workflow.RunGHCombinedContext(ctx, "Creating repository...", "repo", "create", repo, "--"+visibility)
	if err != nil {
		trimmed := strings.TrimSpace(string(output))
		if trimmed == "" {
			return fmt.Errorf("failed to create repository %s: %w", repo, err)
		}
		return fmt.Errorf("failed to create repository %s: %w: %s", repo, err, trimmed)
	}
	return nil
}

func cloneSetupRepository(ctx context.Context, repo string, dir string) error {
	cmd := workflow.ExecGHContext(ctx, "repo", "clone", repo, dir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		trimmed := strings.TrimSpace(string(output))
		if trimmed == "" {
			return fmt.Errorf("failed to clone repository %s into %s: %w", repo, dir, err)
		}
		return fmt.Errorf("failed to clone repository %s into %s: %w: %s", repo, dir, err, trimmed)
	}
	return nil
}

type setupCheckoutInspection struct {
	attached    bool
	cloneNeeded bool
}

func inspectSetupCheckout(dir string, repo string, originRepoLookup func(string) (string, error)) (*setupCheckoutInspection, error) {
	setupRepositoryLog.Printf("Inspecting checkout: dir=%s, repo=%s", dir, repo)
	info, err := os.Stat(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if nestedErr := rejectNestedNonExistentCheckoutPath(dir); nestedErr != nil {
				return nil, nestedErr
			}
			setupRepositoryLog.Printf("Checkout directory %s does not exist; clone needed", dir)
			return &setupCheckoutInspection{cloneNeeded: true}, nil
		}
		return nil, fmt.Errorf("failed to inspect %s: %w", dir, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("target path %s exists but is not a directory", dir)
	}

	gitRoot, err := gitutil.FindGitRootFrom(dir)
	if err != nil {
		empty, emptyErr := isDirectoryEmpty(dir)
		if emptyErr != nil {
			return nil, emptyErr
		}
		if empty {
			return &setupCheckoutInspection{cloneNeeded: true}, nil
		}
		return nil, fmt.Errorf("target directory %s exists but is not a git checkout for %s", dir, repo)
	}

	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve %s: %w", dir, err)
	}
	if gitRoot != absDir {
		return nil, fmt.Errorf("target directory %s is inside a different git checkout rooted at %s", dir, gitRoot)
	}

	originRepo, err := originRepoLookup(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve git remote for %s: %w", dir, err)
	}
	if !strings.EqualFold(strings.TrimSpace(originRepo), strings.TrimSpace(repo)) {
		return nil, fmt.Errorf("target directory %s points to %s, not %s", dir, originRepo, repo)
	}

	setupRepositoryLog.Printf("Checkout at %s is attached to %s", dir, repo)
	return &setupCheckoutInspection{attached: true}, nil
}

func rejectNestedNonExistentCheckoutPath(dir string) error {
	existingPath, err := firstExistingParent(dir)
	if err != nil {
		return err
	}
	if existingPath == "" {
		return nil
	}

	gitRoot, err := gitutil.FindGitRootFrom(existingPath)
	if err != nil {
		if errors.Is(err, gitutil.ErrNotGitRepository) {
			return nil
		}
		return fmt.Errorf("failed to inspect git checkout for %s: %w", existingPath, err)
	}

	insideGitRoot, err := isNestedPathUnder(dir, gitRoot)
	if err != nil {
		return err
	}
	if insideGitRoot {
		return fmt.Errorf("target directory %s is inside a different git checkout rooted at %s", dir, gitRoot)
	}
	return nil
}

func isNestedPathUnder(path string, root string) (bool, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return false, fmt.Errorf("failed to resolve %s: %w", path, err)
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return false, fmt.Errorf("failed to resolve %s: %w", root, err)
	}

	rel, err := filepath.Rel(absRoot, absPath)
	if err != nil {
		return false, fmt.Errorf("failed to compare %s with %s: %w", absPath, absRoot, err)
	}
	if rel == "." {
		return false, nil
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false, nil
	}
	return true, nil
}

func firstExistingParent(path string) (string, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("failed to resolve %s: %w", path, err)
	}

	current := absPath
	for {
		_, statErr := os.Stat(current)
		if statErr == nil {
			return current, nil
		}
		if !errors.Is(statErr, os.ErrNotExist) {
			return "", fmt.Errorf("failed to inspect %s: %w", current, statErr)
		}

		parent := filepath.Dir(current)
		if parent == current {
			return "", nil
		}
		current = parent
	}
}

func resolveSetupCheckoutDir(repo string, dir string) string {
	if strings.TrimSpace(dir) != "" {
		return dir
	}
	return filepath.Base(repo)
}

func normalizeSetupOwnerType(ownerType string) string {
	switch strings.ToLower(strings.TrimSpace(ownerType)) {
	case "organization":
		return "org"
	case "user":
		return "user"
	default:
		return strings.ToLower(strings.TrimSpace(ownerType))
	}
}

func isDirectoryEmpty(dir string) (bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, fmt.Errorf("failed to read %s: %w", dir, err)
	}
	return len(entries) == 0, nil
}

func RunSetupAuth(opts SetupAuthOptions) error {
	return runSetupAuthWithRuntime(opts, defaultSetupRepositoryRuntime())
}

func runSetupAuthWithRuntime(opts SetupAuthOptions, runtime setupRepositoryRuntime) error {
	ctx := ctxutil.OrBackground(opts.Ctx)

	configureDefaultGHHostFromOriginRemoteIfUnset()

	if err := runtime.checkAuth(ctx); err != nil {
		return fmt.Errorf("failed to verify GitHub CLI authentication: %w", err)
	}

	if opts.JSON {
		return renderSetupJSON(SetupAuthResult{Authenticated: true})
	}

	fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("GitHub CLI authentication verified"))
	return nil
}

func RunSetupRepositoryCheck(opts SetupRepositoryCheckOptions) error {
	return runSetupRepositoryCheckWithRuntime(normalizeSetupRepositoryCheckOptions(opts), defaultSetupRepositoryRuntime())
}

func normalizeSetupRepositoryCheckOptions(opts SetupRepositoryCheckOptions) SetupRepositoryCheckOptions {
	if opts.RequireOwnerType == "" {
		opts.RequireOwnerType = "any"
	}
	return opts
}

func validateSetupRepositoryCheckOptions(opts SetupRepositoryCheckOptions) error {
	if !isValidOwnerRepoSlug(opts.Repo) {
		return errors.New("--repo must use the OWNER/REPO format")
	}

	switch opts.RequireOwnerType {
	case "any", "org", "user":
	default:
		return errors.New("--require-owner-type must be one of: any, org, user")
	}

	return nil
}

func isValidOwnerRepoSlug(repo string) bool {
	owner, name, err := repoutil.SplitRepoSlug(repo)
	return err == nil && strings.TrimSpace(owner) != "" && strings.TrimSpace(name) != ""
}

func runSetupRepositoryCheckWithRuntime(opts SetupRepositoryCheckOptions, runtime setupRepositoryRuntime) error {
	if err := validateSetupRepositoryCheckOptions(opts); err != nil {
		return err
	}

	ctx := ctxutil.OrBackground(opts.Ctx)

	configureDefaultGHHostFromOriginRemoteIfUnset()

	if err := runtime.checkAuth(ctx); err != nil {
		return fmt.Errorf("failed to verify GitHub CLI authentication: %w", err)
	}

	setupRepositoryLog.Printf("Running repository check: repo=%s, requireOwnerType=%s", opts.Repo, opts.RequireOwnerType)

	owner := strings.Split(opts.Repo, "/")[0]
	ownerType, err := runtime.ownerType(ctx, owner)
	if err != nil {
		return err
	}
	ownerType = normalizeSetupOwnerType(ownerType)
	setupRepositoryLog.Printf("Resolved owner type for %s: %s", owner, ownerType)
	if opts.RequireOwnerType != "any" && ownerType != opts.RequireOwnerType {
		return fmt.Errorf("owner %s is %s, but --require-owner-type=%s was requested", owner, ownerType, opts.RequireOwnerType)
	}

	repoExists, err := runtime.repoExists(ctx, opts.Repo)
	if err != nil {
		return err
	}
	if !repoExists {
		return fmt.Errorf("repository %s does not exist", opts.Repo)
	}

	dir := resolveSetupCheckoutDir(opts.Repo, opts.Dir)
	inspection, err := inspectSetupCheckout(dir, opts.Repo, runtime.dirOriginRepo)
	if err != nil {
		return err
	}

	if inspection.attached {
		if err := withWorkingDir(dir, func() error {
			return runtime.checkCleanWorktree(opts.Verbose)
		}); err != nil {
			return err
		}
	}

	result := SetupRepositoryCheckResult{
		Repository:        opts.Repo,
		Directory:         dir,
		Authenticated:     true,
		RepositoryExists:  true,
		OwnerType:         ownerType,
		RequiredOwnerType: opts.RequireOwnerType,
		CheckoutAttached:  inspection.attached,
		CloneNeeded:       inspection.cloneNeeded,
	}
	if inspection.attached {
		clean := true
		result.CleanWorktree = &clean
	}

	if opts.JSON {
		return renderSetupJSON(result)
	}

	fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Setup repository check for "+opts.Repo))
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage("- GitHub CLI authenticated"))
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage("- repository exists"))
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage("- owner type: "+ownerType))
	if inspection.attached {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("- attached checkout at "+dir))
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("- working tree is clean"))
	} else if inspection.cloneNeeded {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("- no checkout at %s; directory is ready for clone", dir)))
	}
	fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Setup repository checks passed"))
	return nil
}

func renderSetupJSON(output any) error {
	b, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal setup JSON: %w", err)
	}
	fmt.Fprintln(os.Stdout, string(b))
	return nil
}
