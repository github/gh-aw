package workflow

import (
	_ "embed"
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"
)

//go:embed prompts/checkouts_no_credentials_warning.md
var checkoutsNoCredentialsWarning string

// ParseCheckoutConfigs converts a raw frontmatter value (single map or array of maps)
// into a slice of CheckoutConfig entries.
// Returns (nil, nil) if the value is nil; for non-nil values, invalid types or shapes
// result in a non-nil error.
func ParseCheckoutConfigs(raw any) ([]*CheckoutConfig, error) {
	if raw == nil {
		return nil, nil
	}
	checkoutManagerLog.Printf("Parsing checkout configuration: type=%T", raw)

	var configs []*CheckoutConfig

	// Try single object first
	if singleMap, ok := raw.(map[string]any); ok {
		cfg, err := checkoutConfigFromMap(singleMap)
		if err != nil {
			return nil, fmt.Errorf("invalid checkout configuration, expected an object with checkout fields such as repository, ref, or path: %w", err)
		}
		configs = []*CheckoutConfig{cfg}
	} else if arr, ok := raw.([]any); ok {
		// Try array of objects
		configs = make([]*CheckoutConfig, 0, len(arr))
		for i, item := range arr {
			itemMap, ok := item.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("checkout[%d]: expected object, got %T", i, item)
			}
			cfg, err := checkoutConfigFromMap(itemMap)
			if err != nil {
				return nil, fmt.Errorf("checkout[%d]: %w", i, err)
			}
			configs = append(configs, cfg)
		}
	} else {
		return nil, fmt.Errorf("checkout must be an object or an array of objects, got %T. Expected a single checkout object or a list of checkout objects. Example:\ncheckout:\n  repository: owner/repo", raw)
	}

	// Validate that at most one logical checkout target has current: true.
	// Multiple current checkouts are not allowed since only one repo/path pair can be
	// the primary target for the agent at a time. Multiple configs that merge into the
	// same (repository, path, wiki) tuple are treated as a single logical checkout.
	currentTargets := make(map[string]struct{})
	for _, cfg := range configs {
		if !cfg.Current {
			continue
		}

		repo := strings.TrimSpace(cfg.Repository)
		path := strings.TrimSpace(cfg.Path)
		wiki := "false"
		if cfg.Wiki {
			wiki = "true"
		}
		key := repo + "\x00" + path + "\x00" + wiki

		currentTargets[key] = struct{}{}
	}
	if len(currentTargets) > 1 {
		checkoutManagerLog.Printf("Rejecting checkout config: %d distinct current targets, only one allowed", len(currentTargets))
		return nil, fmt.Errorf("only one checkout target may have current: true, found %d", len(currentTargets))
	}

	checkoutManagerLog.Printf("Parsed %d checkout configuration(s), current-targets=%d", len(configs), len(currentTargets))
	return configs, nil
}

// checkoutConfigFromMap converts a raw map to a CheckoutConfig.
func checkoutConfigFromMap(m map[string]any) (*CheckoutConfig, error) { //nolint:largefunc // Existing checkout parsing remains centralized.
	cfg := &CheckoutConfig{}

	if v, ok := m["repository"]; ok {
		s, ok := v.(string)
		if !ok {
			return nil, errors.New("checkout.repository must be a string. Example:\ncheckout:\n  repository: owner/repo")
		}
		cfg.Repository = s
	}

	if v, ok := m["ref"]; ok {
		s, ok := v.(string)
		if !ok {
			return nil, errors.New("checkout.ref must be a string. Example:\ncheckout:\n  ref: main")
		}
		cfg.Ref = s
	}

	if v, ok := m["path"]; ok {
		s, ok := v.(string)
		if !ok {
			return nil, errors.New("checkout.path must be a string. Example:\ncheckout:\n  path: external/repo")
		}
		cfg.PathExplicit = true
		// Normalize "." to empty string: both mean the workspace root and
		// are treated identically by the checkout step generator.
		if s == "." {
			s = ""
		}
		if err := validateCheckoutPath(s); err != nil {
			return nil, err
		}
		cfg.Path = s
	}

	// Support both "github-token" (preferred) and "token" (backward compat)
	if v, ok := m["github-token"]; ok {
		s, ok := v.(string)
		if !ok {
			return nil, errors.New("checkout.github-token must be a string. Example:\ncheckout:\n  github-token: ${{ secrets.MY_TOKEN }}")
		}
		cfg.GitHubToken = s
	} else if v, ok := m["token"]; ok {
		// Backward compatibility: "token" is accepted but "github-token" is preferred
		s, ok := v.(string)
		if !ok {
			return nil, errors.New("checkout.token must be a string. Example:\ncheckout:\n  github-token: ${{ secrets.MY_TOKEN }}")
		}
		cfg.GitHubToken = s
	}

	// Parse app configuration for GitHub App-based authentication
	if v, ok := m["github-app"]; ok {
		appMap, ok := v.(map[string]any)
		if !ok {
			return nil, errors.New("checkout.github-app must be an object. Example:\ncheckout:\n  github-app:\n    app-id: ${{ vars.APP_ID }}\n    private-key: ${{ secrets.APP_PRIVATE_KEY }}")
		}
		cfg.GitHubApp = parseAppConfig(appMap)
		if cfg.GitHubApp.AppID == "" || cfg.GitHubApp.PrivateKey == "" {
			return nil, errors.New("checkout.github-app requires both client-id (or app-id) and private-key")
		}
	}

	// Parse app configuration for safe_outputs-only GitHub App authentication.
	parseSafeOutputAppConfig := func(fieldName string, value any) (*GitHubAppConfig, error) {
		appMap, ok := value.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("checkout.%s must be an object. Example:\ncheckout:\n  %s:\n    app-id: ${{ vars.APP_ID }}\n    private-key: ${{ secrets.APP_PRIVATE_KEY }}", fieldName, fieldName)
		}
		appConfig := parseAppConfig(appMap)
		if appConfig.AppID == "" || appConfig.PrivateKey == "" {
			return nil, fmt.Errorf("checkout.%s requires both client-id (or app-id) and private-key", fieldName)
		}
		return appConfig, nil
	}
	if v, ok := m["safe-outputs-github-app"]; ok {
		appConfig, err := parseSafeOutputAppConfig("safe-outputs-github-app", v)
		if err != nil {
			return nil, err
		}
		cfg.SafeOutputGitHubApp = appConfig
	}
	if _, ok := m["safe-output-github-app"]; ok {
		return nil, errors.New("checkout.safe-output-github-app is not supported; use checkout.safe-outputs-github-app")
	}

	// Validate mutual exclusivity of github-token and github-app
	if cfg.GitHubToken != "" && cfg.GitHubApp != nil {
		checkoutManagerLog.Print("Rejecting checkout config: github-token and github-app are mutually exclusive")
		return nil, errors.New("checkout: github-token and github-app are mutually exclusive; use one or the other")
	}

	checkoutManagerLog.Printf("Parsed checkout config: repo=%q, ref=%q, path=%q, current=%v, hasToken=%v, hasApp=%v",
		cfg.Repository, cfg.Ref, cfg.Path, cfg.Current, cfg.GitHubToken != "", cfg.GitHubApp != nil)

	if v, ok := m["fetch-depth"]; ok {
		switch n := v.(type) {
		case int:
			depth := n
			cfg.FetchDepth = &depth
		case int64:
			depth := int(n)
			cfg.FetchDepth = &depth
		case uint64:
			depth := int(n)
			cfg.FetchDepth = &depth
		case float64:
			if n != float64(int64(n)) {
				return nil, errors.New("checkout.fetch-depth must be an integer. Example:\ncheckout:\n  fetch-depth: 0")
			}
			depth := int(n)
			cfg.FetchDepth = &depth
		default:
			return nil, errors.New("checkout.fetch-depth must be an integer. Example:\ncheckout:\n  fetch-depth: 0")
		}
		if cfg.FetchDepth != nil && *cfg.FetchDepth < 0 {
			return nil, errors.New("checkout.fetch-depth must be >= 0, where 0 fetches the full history. Example:\ncheckout:\n  fetch-depth: 1")
		}
	}

	if v, ok := m["sparse-checkout"]; ok {
		s, ok := v.(string)
		if !ok {
			return nil, errors.New("checkout.sparse-checkout must be a string listing the paths to check out. Example:\ncheckout:\n  sparse-checkout: docs")
		}
		cfg.SparseCheckout = s
	}

	if v, ok := m["submodules"]; ok {
		switch sv := v.(type) {
		case string:
			cfg.Submodules = sv
		case bool:
			if sv {
				cfg.Submodules = "true"
			} else {
				cfg.Submodules = "false"
			}
		default:
			return nil, errors.New("checkout.submodules must be a string or boolean. Example:\ncheckout:\n  submodules: recursive")
		}
	}

	if v, ok := m["lfs"]; ok {
		b, ok := v.(bool)
		if !ok {
			return nil, errors.New("checkout.lfs must be a boolean. Example:\ncheckout:\n  lfs: true")
		}
		cfg.LFS = b
	}

	if v, ok := m["current"]; ok {
		b, ok := v.(bool)
		if !ok {
			return nil, errors.New("checkout.current must be a boolean. Example:\ncheckout:\n  current: true")
		}
		cfg.Current = b
	}

	if v, ok := m["fetch"]; ok {
		switch fv := v.(type) {
		case string:
			// Single string shorthand: treat as a one-element list
			if strings.TrimSpace(fv) == "" {
				return nil, errors.New("checkout.fetch string value must not be empty, it should name a git ref to fetch. Example:\ncheckout:\n  fetch: main")
			}
			cfg.Fetch = []string{fv}
		case []any:
			refs := make([]string, 0, len(fv))
			for i, item := range fv {
				s, ok := item.(string)
				if !ok {
					return nil, fmt.Errorf("checkout.fetch[%d] must be a string, got %T. Example:\ncheckout:\n  fetch: [\"main\", \"refs/pull/*/head\"]", i, item)
				}
				if strings.TrimSpace(s) == "" {
					return nil, fmt.Errorf("checkout.fetch[%d] must not be empty, every entry should name a git ref. Example:\ncheckout:\n  fetch: [\"main\"]", i)
				}
				refs = append(refs, s)
			}
			cfg.Fetch = refs
		default:
			return nil, errors.New("checkout.fetch must be a string or an array of strings. Example:\ncheckout:\n  fetch: [\"main\"]")
		}
	}

	if v, ok := m["wiki"]; ok {
		b, ok := v.(bool)
		if !ok {
			return nil, errors.New("checkout.wiki must be a boolean. Example:\ncheckout:\n  wiki: true")
		}
		cfg.Wiki = b
	}

	if v, ok := m["force-clean-git-credentials"]; ok {
		b, ok := v.(bool)
		if !ok {
			return nil, errors.New("checkout.force-clean-git-credentials must be a boolean. Example:\ncheckout:\n  force-clean-git-credentials: true")
		}
		cfg.CleanGitCredentials = b
	}

	return cfg, nil
}

// validateCheckoutPath rejects checkout paths that escape the workspace root.
// actions/checkout requires the path to be located under $GITHUB_WORKSPACE, and
// paths containing ".." segments would also render misleadingly in the agent prompt
// because they collapse into a plausible-looking but incorrect location.
func validateCheckoutPath(p string) error {
	if p == "" {
		return nil
	}
	normalized := strings.ReplaceAll(p, "\\", "/")
	if strings.HasPrefix(normalized, "/") {
		return fmt.Errorf("checkout.path must be a relative path under the workspace, got %q. Example:\ncheckout:\n  path: external/repo", p)
	}
	if slices.Contains(strings.Split(normalized, "/"), "..") {
		return fmt.Errorf("checkout.path must not contain \"..\" segments, got %q. Use a path under the workspace. Example:\ncheckout:\n  path: external/repo", p)
	}
	return nil
}

// checkouts for inclusion in the GitHub context prompt.
// Returns an empty string when no checkouts are configured.
//
// Each checkout is shown with its full absolute path using the runtime github.workspace value.
// The root checkout (path == "") is annotated as "(cwd)" since that is the working
// directory of the agent process. The generated content may include
// "${{ github.repository }}" for any checkout that does not have an explicit repository
// configured; callers must ensure these expressions are processed by an ExpressionExtractor
// so the placeholder substitution step can resolve them at runtime.
func buildCheckoutsPromptContent(checkouts []*CheckoutConfig) string { //nolint:largefunc // Checkout annotations are emitted together for consistent prompt guidance.
	if len(checkouts) == 0 {
		checkoutManagerLog.Print("buildCheckoutsPromptContent: no checkouts configured, returning empty content")
		return ""
	}
	checkoutManagerLog.Printf("Building checkouts prompt content for %d checkout(s)", len(checkouts))

	var sb strings.Builder
	sb.WriteString("- **checkouts**: The following repositories have been checked out and are available in the workspace:\n")

	for _, cfg := range checkouts {
		if cfg == nil {
			continue
		}

		// Build the full absolute path using the runtime workspace as root.
		// Normalize the path: strip "./" prefix; bare "." and "" both mean root.
		relPath := strings.TrimPrefix(cfg.Path, "./")
		if relPath == "." {
			relPath = ""
		}
		isRoot := relPath == ""
		absPath := "${{ github.workspace }}"
		if !isRoot {
			absPath = path.Join(absPath, relPath)
		}

		// Determine repo: use configured value or fall back to the triggering repository expression.
		// For wiki checkouts, append the ".wiki" suffix so the prompt accurately reflects what was checked out.
		repo := cfg.Repository
		if repo == "" {
			repo = "${{ github.repository }}"
		}
		if cfg.Wiki {
			if !strings.HasSuffix(repo, ".wiki") {
				repo += ".wiki"
			}
		}

		line := fmt.Sprintf("  - repo `%s` → `%s`", repo, absPath)
		if isRoot {
			line += " (cwd)"
		}
		if cfg.Wiki {
			line += " (wiki)"
		}
		if cfg.Current {
			line += " (**current** - this is the repository you are working on; use this as the target for all GitHub operations unless otherwise specified)"
		}

		// Annotate fetch-depth so the agent knows how much history is available
		if cfg.FetchDepth != nil && *cfg.FetchDepth == 0 {
			line += " [full history, all branches available as remote-tracking refs]"
		} else if cfg.FetchDepth != nil {
			line += fmt.Sprintf(" [shallow clone, fetch-depth=%d]", *cfg.FetchDepth)
		} else {
			line += " [shallow clone, fetch-depth=1 (default)]"
		}

		// Annotate additionally fetched refs
		if len(cfg.Fetch) > 0 {
			line += fmt.Sprintf(" [additional refs fetched: %s]", strings.Join(cfg.Fetch, ", "))
		}
		if strings.TrimSpace(cfg.SparseCheckout) != "" {
			line += " [sparse checkout enabled]"
		}

		sb.WriteString(line + "\n")
	}

	// General guidance about the workspace root and unavailable branches
	sb.WriteString("  - **Note**: The workspace path reported above may contain a separate shallow, credential-free checkout of the host repository. " +
		"Use the exact checkout path shown for the repository you need. Before concluding that a branch is unavailable, confirm your working directory matches that path and inspect refs there. " +
		"If the branch is not present in that checkout and is not listed as an additional fetched ref, it has NOT been checked out. For private repositories you cannot fetch it. " +
		"If the branch is required and not available, exit with an error and ask the user to add it to the " +
		"`fetch:` option of the `checkout:` configuration (e.g., `fetch: [\"refs/pulls/open/*\"]` for all open PR refs, " +
		"or `fetch: [\"main\", \"feature/my-branch\"]` for specific branches).\n")

	// Credential warning — always present for any checkout-enabled workflow.
	sb.WriteString(checkoutsNoCredentialsWarning)

	return sb.String()
}
