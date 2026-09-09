package workflow

import (
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/setutil"
)

var checkoutManagerLog = logger.New("workflow:checkout_manager")

// CheckoutConfig represents a single checkout configuration from workflow frontmatter.
// It controls how actions/checkout is invoked in the agent job.
//
// Supports all relevant options from actions/checkout:
//
//	checkout:
//	  fetch-depth: 0
//	  github-token: ${{ secrets.MY_TOKEN }}
//
// Or multiple checkouts:
//
//	checkout:
//	  - fetch-depth: 0
//	  - repository: owner/other-repo
//	    path: ./libs/other
//	    ref: main
//	    github-token: ${{ secrets.CROSS_REPO_PAT }}
//
// GitHub App authentication is also supported:
//
//	checkout:
//	  - repository: owner/other-repo
//	    path: ./libs/other
//	    app:
//	      app-id: ${{ vars.APP_ID }}
//	      private-key: ${{ secrets.APP_PRIVATE_KEY }}
type CheckoutConfig struct {
	// Repository to checkout in owner/repo format. Defaults to the current repository.
	Repository string `json:"repository,omitempty"`

	// Ref (branch, tag, or SHA) to checkout. Defaults to the ref that triggered the workflow.
	Ref string `json:"ref,omitempty"`

	// Path within GITHUB_WORKSPACE to place the checkout. Defaults to the workspace root.
	Path string `json:"path,omitempty"`

	// PathExplicit tracks whether the workflow frontmatter explicitly provided a
	// path: field. This preserves the distinction between an omitted path and an
	// explicit root checkout (path: .), which is normalized to Path == "".
	PathExplicit bool `json:"-"`

	// GitHubToken overrides the default GITHUB_TOKEN for authentication.
	// Use ${{ secrets.MY_TOKEN }} to reference a repository secret.
	// Maps to the "token" input of actions/checkout.
	// Mutually exclusive with GitHubApp.
	GitHubToken string `json:"github-token,omitempty"`

	// GitHubApp configures GitHub App-based authentication for this checkout.
	// When set, a token minting step is generated before checkout using
	// actions/create-github-app-token, and the minted token is passed
	// to actions/checkout as the "token" input.
	// Mutually exclusive with GitHubToken.
	GitHubApp *GitHubAppConfig `json:"github-app,omitempty"`

	// SafeOutputGitHubApp configures GitHub App-based authentication used only by
	// safe_outputs git checkout/fetch/push operations for this checkout target.
	// This does not change activation/agent checkout authentication.
	SafeOutputGitHubApp *GitHubAppConfig `json:"safe-outputs-github-app,omitempty"`

	// FetchDepth controls the number of commits to fetch.
	// 0 fetches all history (full clone). 1 is a shallow clone (default).
	FetchDepth *int `json:"fetch-depth,omitempty"`

	// SparseCheckout enables sparse-checkout mode. Provide newline-separated patterns
	// (e.g., ".github/\nsrc/"). When multiple configs target the same path, patterns
	// are merged into a single checkout.
	SparseCheckout string `json:"sparse-checkout,omitempty"`

	// Submodules controls submodule checkout behavior: "recursive", "true", or "false".
	Submodules string `json:"submodules,omitempty"`

	// LFS enables checkout of Git LFS objects.
	LFS bool `json:"lfs,omitempty"`

	// Current marks this checkout as the logical "current" repository for the workflow.
	// When set, the AI agent will treat this repository as its primary working target.
	// Only one checkout may have Current set to true.
	// This is useful for workflows that run from a central repo targeting a different repo.
	Current bool `json:"current,omitempty"`

	// Fetch specifies additional Git refs to fetch after checkout.
	// A git fetch step is emitted after the actions/checkout step.
	//
	// Supported values:
	//   - "*"            – fetch all remote branches
	//   - "refs/pulls/open/*" – GH-AW shorthand for all open pull-request refs
	//   - branch name    – e.g. "main" or "feature/my-branch"
	//   - glob pattern   – e.g. "feature/*"
	//
	// Example:
	//   fetch: ["*"]
	//   fetch: ["refs/pulls/open/*"]
	//   fetch: ["main", "feature/my-branch"]
	Fetch []string `json:"fetch,omitempty"`

	// Wiki clones the repository's wiki git instead of the regular repository.
	// When true, the effective repository becomes "{repository}.wiki" (e.g. "owner/repo.wiki").
	// Defaults to false.
	Wiki bool `json:"wiki,omitempty"`

	// CleanGitCredentials keeps actions/checkout credential persistence enabled and
	// injects a follow-up cleanup step that removes credentials from git config files
	// (including submodule configs) without using git submodule foreach.
	CleanGitCredentials bool `json:"force-clean-git-credentials,omitempty"`
}

// checkoutKey uniquely identifies a checkout target used for grouping/deduplication.
// Repository, path, and wiki are used as key fields — ref and token are settings
// that can be merged across configs targeting the same (repository, path, wiki) tuple.
// Wiki is included because a wiki checkout and a regular checkout of the same repository
// at the same path must remain as separate steps.
type checkoutKey struct {
	repository string
	path       string
	wiki       bool
}

// resolvedCheckout is an internal merged checkout entry used by CheckoutManager.
type resolvedCheckout struct {
	key            checkoutKey
	ref            string           // last non-empty ref wins
	token          string           // last non-empty github-token wins
	githubApp      *GitHubAppConfig // GitHub App config (first non-nil wins)
	safeOutputApp  *GitHubAppConfig // safe_outputs-only GitHub App config (first non-nil wins)
	fetchDepth     *int             // nil means use default (1)
	sparsePatterns []string         // merged sparse-checkout patterns
	submodules     string
	lfs            bool
	current        bool     // true if this checkout is the logical current repository
	fetchRefs      []string // merged fetch ref patterns (see CheckoutConfig.Fetch)
	cleanCreds     bool     // true enables persist-credentials + injected cleanup step
	// wiki is intentionally not stored here; use entry.key.wiki instead.
}

// CheckoutManager collects checkout requests and merges them to minimize
// the number of actions/checkout steps emitted.
//
// Merging rules:
//   - Checkouts with the same (repository, ref, path, token) are merged into one.
//   - The deepest fetch-depth wins: 0 (full history) overrides any shallower value.
//   - Sparse-checkout patterns are unioned across merged configs.
//   - LFS and submodules are OR-ed (if any request enables them, the result enables them).
type CheckoutManager struct {
	// ordered preserves insertion order for deterministic output
	ordered []*resolvedCheckout
	// index maps checkoutKey to the position in ordered
	index map[checkoutKey]int
	// crossRepoTargetRepo holds the platform (host) repository to use when performing
	// .github/.agents sparse checkout steps for cross-repo workflow_call invocations.
	//
	// In the activation job this is set to "${{ steps.resolve-host-repo.outputs.target_repo }}".
	// In the agent and safe_outputs jobs it is set to "${{ needs.activation.outputs.target_repo }}".
	// An empty string means the checkout targets the current repository (github.repository).
	crossRepoTargetRepo string
	// crossRepoTargetRef holds the platform (host) ref (branch/tag/SHA) to use when
	// performing .github/.agents sparse checkout steps for cross-repo workflow_call
	// invocations pinned to a non-default branch.
	//
	// Currently only set in the activation job to
	// "${{ steps.resolve-host-repo.outputs.target_checkout_ref }}" (the immutable SHA).
	// Downstream jobs (agent, safe_outputs) do not currently set this field — they rely
	// on the default-branch checkout behaviour.
	// An empty string means the checkout uses the repository's default branch.
	crossRepoTargetRef string
	// keepCredentialsForPush, when true, makes every generated checkout step retain its
	// credentials (persist-credentials: true) and suppresses the post-checkout credential
	// cleanup step. This is enabled for the safe_outputs job, which legitimately performs
	// git fetch/push against the checked-out repositories (e.g. push_to_pull_request_branch,
	// create_pull_request) and therefore needs the push-capable token left on disk.
	//
	// The agent job leaves this false: the untrusted agent must not be able to read
	// credentials from disk, so its checkouts use persist-credentials: false.
	keepCredentialsForPush bool
	// pushToken is the token expression persisted into .git/config by every generated
	// checkout step when keepCredentialsForPush is enabled and the checkout entry does
	// not carry its own explicit token/app auth. Setting this ensures the credential
	// retained on disk matches the token the safe_outputs handlers use to push, so a
	// single (correct) Authorization header is sent and no separate per-command
	// http.extraheader injection is required. Empty means "fall back to the
	// actions/checkout default token".
	pushToken string
	// defaultRefOverride forces the workspace-root checkout to a compiler-generated
	// ref, such as a branch allocated before agent execution.
	defaultRefOverride string
}

// NewCheckoutManager creates a new CheckoutManager pre-loaded with user-supplied
// CheckoutConfig entries from the frontmatter.
func NewCheckoutManager(userCheckouts []*CheckoutConfig) *CheckoutManager {
	checkoutManagerLog.Printf("Creating checkout manager with %d user checkout config(s)", len(userCheckouts))
	cm := &CheckoutManager{
		index: make(map[checkoutKey]int),
	}
	for _, cfg := range userCheckouts {
		cm.add(cfg)
	}
	return cm
}

// SetCrossRepoTargetRepo stores the platform (host) repository expression used for
// .github/.agents sparse checkout steps. Call this when the workflow has a workflow_call
// trigger and the checkout should target the platform repo rather than github.repository.
//
// In the activation job pass "${{ steps.resolve-host-repo.outputs.target_repo }}".
// In downstream jobs (agent, safe_outputs) pass "${{ needs.activation.outputs.target_repo }}".
func (cm *CheckoutManager) SetCrossRepoTargetRepo(repo string) {
	checkoutManagerLog.Printf("Setting cross-repo target repo: %q", repo)
	cm.crossRepoTargetRepo = repo
}

// GetCrossRepoTargetRepo returns the platform repo expression previously set by
// SetCrossRepoTargetRepo, or an empty string if no cross-repo target was set
// (same-repo invocation or inlined imports).
func (cm *CheckoutManager) GetCrossRepoTargetRepo() string {
	return cm.crossRepoTargetRepo
}

// SetCrossRepoTargetRef stores the platform (host) ref expression used for
// .github/.agents sparse checkout steps. Call this when the workflow has a workflow_call
// trigger and the checkout should target a specific branch rather than the default branch.
//
// In the activation job pass "${{ steps.resolve-host-repo.outputs.target_checkout_ref }}"
// (the immutable SHA for exact-revision pinning).
func (cm *CheckoutManager) SetCrossRepoTargetRef(ref string) {
	checkoutManagerLog.Printf("Setting cross-repo target ref: %q", ref)
	cm.crossRepoTargetRef = ref
}

// GetCrossRepoTargetRef returns the platform ref expression previously set by
// SetCrossRepoTargetRef, or an empty string if no cross-repo ref was set.
func (cm *CheckoutManager) GetCrossRepoTargetRef() string {
	return cm.crossRepoTargetRef
}

// SetKeepCredentialsForPush enables credential retention on all generated checkout steps.
// Call this for the safe_outputs job so the push-capable token installed at checkout time
// remains in .git/config for subsequent git fetch/push operations. The agent job must not
// call this; its checkouts intentionally strip credentials (persist-credentials: false).
func (cm *CheckoutManager) SetKeepCredentialsForPush(keep bool) {
	checkoutManagerLog.Printf("Setting keepCredentialsForPush: %t", keep)
	cm.keepCredentialsForPush = keep
}

// SetPushToken sets the token expression persisted into .git/config by the generated
// checkout steps when keepCredentialsForPush is enabled. Call this for the safe_outputs
// job with the resolved PR push token so the retained credential matches the token the
// handlers use to fetch/push. Has no effect on entries that declare their own token/app.
func (cm *CheckoutManager) SetPushToken(token string) {
	checkoutManagerLog.Printf("Setting pushToken: present=%t", token != "")
	cm.pushToken = token
}

// add processes a single CheckoutConfig and either creates a new entry or merges
// it into an existing entry with the same key.
func (cm *CheckoutManager) add(cfg *CheckoutConfig) {
	if cfg == nil {
		return
	}

	// Normalize path: "." and "" both refer to the workspace root.
	normalizedPath := cfg.Path
	if normalizedPath == "." {
		normalizedPath = ""
	}
	// Normalize repository for wiki checkouts: strip a trailing ".wiki" suffix so that
	// "owner/repo" and "owner/repo.wiki" with Wiki:true resolve to the same deduplication key.
	normalizedRepo := cfg.Repository
	if cfg.Wiki && strings.HasSuffix(normalizedRepo, ".wiki") {
		normalizedRepo = strings.TrimSuffix(normalizedRepo, ".wiki")
	}
	key := checkoutKey{
		repository: normalizedRepo,
		path:       normalizedPath,
		wiki:       cfg.Wiki,
	}

	if idx, exists := cm.index[key]; exists {
		// Merge into existing entry; first-seen wins for ref and token/app (auth is mutually exclusive:
		// once either github-token or github-app is set for an entry, the other method is not added
		// even if a later config provides it — this preserves the main workflow's auth choice).
		entry := cm.ordered[idx]
		entry.fetchDepth = deeperFetchDepth(entry.fetchDepth, cfg.FetchDepth)
		if cfg.Ref != "" && entry.ref == "" {
			entry.ref = cfg.Ref // first-seen ref wins
		}
		if cfg.GitHubToken != "" && entry.token == "" && entry.githubApp == nil {
			entry.token = cfg.GitHubToken // first-seen auth wins (mutually exclusive with github-app)
		}
		if cfg.GitHubApp != nil && entry.githubApp == nil && entry.token == "" {
			entry.githubApp = cfg.GitHubApp // first-seen auth wins (mutually exclusive with github-token)
		}
		if cfg.SafeOutputGitHubApp != nil && entry.safeOutputApp == nil {
			entry.safeOutputApp = cfg.SafeOutputGitHubApp // first-seen safe_outputs auth wins
		}
		if cfg.SparseCheckout != "" {
			entry.sparsePatterns = mergeSparsePatterns(entry.sparsePatterns, cfg.SparseCheckout)
		}
		if cfg.LFS {
			entry.lfs = true
		}
		if cfg.Current {
			entry.current = true
		}
		if cfg.Submodules != "" && entry.submodules == "" {
			entry.submodules = cfg.Submodules
		}
		if len(cfg.Fetch) > 0 {
			entry.fetchRefs = mergeFetchRefs(entry.fetchRefs, cfg.Fetch)
		}
		if cfg.CleanGitCredentials {
			entry.cleanCreds = true
		}
		checkoutManagerLog.Printf("Merged checkout for path=%q repository=%q", key.path, key.repository)
	} else {
		entry := &resolvedCheckout{
			key:           key,
			ref:           cfg.Ref,
			token:         cfg.GitHubToken,
			githubApp:     cfg.GitHubApp,
			safeOutputApp: cfg.SafeOutputGitHubApp,
			fetchDepth:    cfg.FetchDepth,
			submodules:    cfg.Submodules,
			lfs:           cfg.LFS,
			current:       cfg.Current,
			cleanCreds:    cfg.CleanGitCredentials,
		}
		if cfg.SparseCheckout != "" {
			entry.sparsePatterns = mergeSparsePatterns(nil, cfg.SparseCheckout)
		}
		if len(cfg.Fetch) > 0 {
			entry.fetchRefs = mergeFetchRefs(nil, cfg.Fetch)
		}
		cm.index[key] = len(cm.ordered)
		cm.ordered = append(cm.ordered, entry)
		checkoutManagerLog.Printf("Added checkout for path=%q repository=%q", key.path, key.repository)
	}
}

// GetDefaultCheckoutOverride returns the resolved checkout for the default workspace
// (empty path, empty repository). Returns nil if the user did not configure one.
// Checks both wiki=false and wiki=true variants so that a wiki default checkout
// is also returned when the user sets wiki: true at the root.
func (cm *CheckoutManager) GetDefaultCheckoutOverride() *resolvedCheckout {
	// Non-wiki default checkout takes precedence.
	key := checkoutKey{}
	if idx, ok := cm.index[key]; ok {
		return cm.ordered[idx]
	}
	// Fall back to wiki=true default checkout (empty path and empty repository).
	wikiKey := checkoutKey{wiki: true}
	if idx, ok := cm.index[wikiKey]; ok {
		return cm.ordered[idx]
	}
	return nil
}

// HasAppAuth returns true if any checkout entry uses GitHub App authentication.
func (cm *CheckoutManager) HasAppAuth() bool {
	for _, entry := range cm.ordered {
		if entry.githubApp != nil {
			return true
		}
	}
	return false
}

// HasSafeOutputAppAuth returns true if any checkout entry uses safe_outputs-only
// GitHub App authentication.
func (cm *CheckoutManager) HasSafeOutputAppAuth() bool {
	for _, entry := range cm.ordered {
		if entry.safeOutputApp != nil {
			return true
		}
	}
	return false
}

// ResolveSafeOutputCheckoutTokenExpression returns a safe_outputs checkout token
// expression derived from checkout.safe-outputs-github-app for the target repo.
// The selected checkout precedence is:
//  1. explicit matching checkout.repository == targetRepo (when targetRepo is non-empty)
//  2. checkout marked current: true
//  3. default checkout override (workspace root)
func (cm *CheckoutManager) ResolveSafeOutputCheckoutTokenExpression(targetRepo string) (string, bool) {
	findSafeOutputAppCheckoutIndex := func() int {
		targetRepo = strings.TrimSpace(targetRepo)
		if targetRepo != "" && targetRepo != "*" {
			for idx, entry := range cm.ordered {
				if entry.key.wiki || entry.safeOutputApp == nil {
					continue
				}
				if entry.key.repository == targetRepo {
					return idx
				}
			}
		}

		for idx, entry := range cm.ordered {
			if entry.key.wiki || entry.safeOutputApp == nil {
				continue
			}
			if entry.current {
				return idx
			}
		}

		if override := cm.GetDefaultCheckoutOverride(); override != nil && !override.key.wiki && override.safeOutputApp != nil {
			if idx, ok := cm.index[override.key]; ok {
				return idx
			}
		}
		return -1
	}

	idx := findSafeOutputAppCheckoutIndex()
	if idx < 0 {
		return "", false
	}

	//nolint:gosec // G101: False positive - this is a GitHub Actions expression template placeholder, not a hardcoded credential
	token := fmt.Sprintf("${{ steps.checkout-safe-output-app-token-%d.outputs.token }}", idx)
	app := cm.ordered[idx].safeOutputApp
	if app != nil && app.shouldIgnoreMissingKey() {
		token = combineTokenExpressions(token, getEffectiveSafeOutputGitHubToken(""))
	}
	return token, true
}

// resolveCheckoutPermissions determines the permissions used when minting checkout
// GitHub App tokens. Both the agent job and the safe_outputs job resolve them the same
// way: explicit cached permissions take precedence, then parsed frontmatter permissions,
// then the default permission set.
func resolveCheckoutPermissions(data *WorkflowData) *Permissions {
	switch {
	case data.CachedPermissions != nil:
		return data.CachedPermissions
	case data.Permissions != "":
		return NewPermissionsParser(data.Permissions).ToPermissions()
	default:
		return NewPermissions()
	}
}

// GetCurrentRepository returns the repository slug for the checkout marked
// current:true. Returns an empty string when no current checkout is configured
// or when the current checkout targets the workflow repository.
func (cm *CheckoutManager) GetCurrentRepository() string {
	for _, entry := range cm.ordered {
		if entry.current {
			return entry.key.repository
		}
	}
	return ""
}

// GetCurrentCheckoutPath returns the current checkout path after trimming
// leading "./" and surrounding whitespace. Returns an empty string when no
// current checkout is configured or when the current checkout is at workspace
// root.
func (cm *CheckoutManager) GetCurrentCheckoutPath() string {
	for _, entry := range cm.ordered {
		if !entry.current {
			continue
		}
		path := strings.TrimSpace(strings.TrimPrefix(entry.key.path, "./"))
		if path == "." || path == "" {
			return ""
		}
		return path
	}
	return ""
}

// HasExternalRootCheckout returns true if any checkout entry targets an external
// repository (non-empty repository field) and writes to the workspace root (empty path).
// When such a checkout exists, the workspace root is replaced with the external
// repository content, which removes any locally-checked-out actions/setup directory.
// In dev mode, a "Restore actions folder" step must be added after such checkouts so
// the runner's post-step for the Setup Scripts action can find action.yml.
//
// Note: the "." path is normalized to "" in add(), so both "" and "." are covered
// by the entry.key.path == "" check.
func (cm *CheckoutManager) HasExternalRootCheckout() bool {
	for _, entry := range cm.ordered {
		if entry.key.repository != "" && entry.key.path == "" {
			return true
		}
	}
	return false
}

// deeperFetchDepth returns the deeper of two fetch-depth values.
// 0 means full history and is always "deepest"; otherwise lower positive values
// are shallower. nil means "use default".
func deeperFetchDepth(a, b *int) *int {
	if a == nil && b == nil {
		return nil
	}
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	// 0 = full history = deepest
	if *a == 0 || *b == 0 {
		zero := 0
		return &zero
	}
	// For positive depths, larger value = more history = deeper
	if *a > *b {
		return a
	}
	return b
}

// mergeSparsePatterns parses and unions sparse-checkout patterns.
// Patterns can be newline-separated.
func mergeSparsePatterns(existing []string, newPatterns string) []string {
	seen := make(map[string]struct {
	}, len(existing))
	result := make([]string, 0, len(existing))

	for _, p := range existing {
		p = strings.TrimSpace(p)
		if p != "" && !setutil.Contains(seen, p) {
			seen[p] = struct {
			}{}
			result = append(result, p)
		}
	}

	for p := range strings.SplitSeq(newPatterns, "\n") {
		p = strings.TrimSpace(p)
		if p != "" && !setutil.Contains(seen, p) {
			seen[p] = struct {
			}{}
			result = append(result, p)
		}
	}

	return result
}

// mergeFetchRefs unions two sets of fetch ref patterns preserving insertion order.
func mergeFetchRefs(existing []string, newRefs []string) []string {
	seen := make(map[string]struct {
	}, len(existing))
	result := make([]string, 0)
	for _, r := range existing {
		r = strings.TrimSpace(r)
		if r != "" && !setutil.Contains(seen, r) {
			seen[r] = struct {
			}{}
			result = append(result, r)
		}
	}
	for _, r := range newRefs {
		r = strings.TrimSpace(r)
		if r != "" && !setutil.Contains(seen, r) {
			seen[r] = struct {
			}{}
			result = append(result, r)
		}
	}
	return result
}
