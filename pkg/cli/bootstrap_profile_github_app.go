package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"charm.land/huh/v2"
	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/repoutil"
	"github.com/github/gh-aw/pkg/workflow"
)

var githubAppBootstrapLog = logger.New("cli:bootstrap_profile_github_app")

func runBootstrapGitHubAppAction(ctx context.Context, repo string, action repositoryPackageBootstrapAction, state *bootstrapProfileExistingState) (*bootstrapCreatedGitHubApp, error) {
	_, hasVar := state.variables[action.AppIDVariable]
	_, hasSecret := state.secrets[action.PrivateKeySecret]
	if hasVar && hasSecret {
		githubAppBootstrapLog.Printf("GitHub App already configured: appIDVar=%s privateKeySecret=%s, skipping", action.AppIDVariable, action.PrivateKeySecret)
		return nil, nil
	}
	githubAppBootstrapLog.Printf("Running GitHub App bootstrap action: repo=%s appIDVar=%s", repo, action.AppIDVariable)

	overrides, err := loadBootstrapGitHubAppOverrides()
	if err != nil {
		return nil, err
	}

	owner, repoName, err := repoutil.SplitRepoSlug(repo)
	if err != nil {
		return nil, err
	}
	ownerType, err := bootstrapCheckOwnerType(ctx, owner)
	if err != nil {
		return nil, err
	}

	clientID := strings.TrimSpace(lookupEnv(bootstrapGitHubAppClientIDEnv))
	privateKey := strings.TrimRight(lookupEnv(bootstrapGitHubAppPrivateKeyEnv), "\r\n")
	handled, err := handleBootstrapGitHubAppExistingFlow(ctx, repo, action, overrides, clientID, privateKey)
	if err != nil {
		return nil, err
	}
	if handled {
		return nil, nil
	}

	handled, err = handleBootstrapGitHubAppCreateOrExistingChoice(ctx, repo, action, overrides)
	if err != nil {
		return nil, err
	}
	if handled {
		return nil, nil
	}

	if !bootstrapIsInteractive() && overrides.Mode != "create" {
		return nil, fmt.Errorf("creating a new GitHub App requires an interactive browser flow; provide existing credentials via %s and %s, or set %s=create to force browser-based creation. Example: export %s=Iv23example and %s='-----BEGIN PRIVATE KEY-----...'", bootstrapGitHubAppClientIDEnv, bootstrapGitHubAppPrivateKeyEnv, bootstrapGitHubAppModeEnv, bootstrapGitHubAppClientIDEnv, bootstrapGitHubAppPrivateKeyEnv)
	}
	createdApp, err := bootstrapCreateGitHubApp(ctx, repo, owner, repoName, ownerType, action, overrides)
	if err != nil {
		return nil, err
	}
	if err := bootstrapUpsertVariable(ctx, repo, action.AppIDVariable, createdApp.ClientID); err != nil {
		return nil, err
	}
	fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Set repository variable "+action.AppIDVariable))
	if err := bootstrapSetSecret(ctx, repo, action.PrivateKeySecret, createdApp.PEM); err != nil {
		return nil, err
	}
	fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Set repository secret "+action.PrivateKeySecret))
	if createdApp.InstallURL != "" {
		if err := waitForBootstrapGitHubAppInstallation(ctx, repo, createdApp); err != nil {
			return nil, err
		}
	}
	return createdApp, nil
}

// handleBootstrapGitHubAppExistingFlow handles the path where existing app credentials
// are provided via environment variables or an explicit "existing" mode override.
// Returns (true, nil) when credentials were applied and the caller should return, or
// (false, nil) when no existing credentials were detected and the caller should proceed.
func handleBootstrapGitHubAppExistingFlow(ctx context.Context, repo string, action repositoryPackageBootstrapAction, overrides bootstrapGitHubAppOverrides, clientID, privateKey string) (bool, error) {
	if clientID == "" && privateKey == "" && action.Mode != "existing" && overrides.Mode != "existing" {
		return false, nil
	}
	githubAppBootstrapLog.Printf("Applying existing GitHub App credentials: repo=%s hasClientID=%v hasPrivateKey=%v", repo, clientID != "", privateKey != "")
	resolvedClientID, resolvedPrivateKey, err := completeExistingGitHubAppCredentials(clientID, privateKey, action, repo)
	if err != nil {
		return false, err
	}
	if err := bootstrapUpsertVariable(ctx, repo, action.AppIDVariable, resolvedClientID); err != nil {
		return false, err
	}
	fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Set repository variable "+action.AppIDVariable))
	if err := bootstrapSetSecret(ctx, repo, action.PrivateKeySecret, resolvedPrivateKey); err != nil {
		return false, err
	}
	fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Set repository secret "+action.PrivateKeySecret))
	return true, nil
}

// handleBootstrapGitHubAppCreateOrExistingChoice handles the "create-or-existing" action
// mode, prompting interactively when needed. Returns (true, nil) when the user chose an
// existing app and credentials were applied, or (false, nil) to proceed with creation.
func handleBootstrapGitHubAppCreateOrExistingChoice(ctx context.Context, repo string, action repositoryPackageBootstrapAction, overrides bootstrapGitHubAppOverrides) (bool, error) {
	if action.Mode != "create-or-existing" && overrides.Mode != "create" {
		return false, nil
	}
	choice := overrides.Mode
	var err error
	if choice == "" {
		choice, err = chooseBootstrapGitHubAppMode()
		if err != nil {
			return false, err
		}
	}
	if choice != "existing" {
		return false, nil
	}
	resolvedClientID, resolvedPrivateKey, err := completeExistingGitHubAppCredentials("", "", action, repo)
	if err != nil {
		return false, err
	}
	if err := bootstrapUpsertVariable(ctx, repo, action.AppIDVariable, resolvedClientID); err != nil {
		return false, err
	}
	if err := bootstrapSetSecret(ctx, repo, action.PrivateKeySecret, resolvedPrivateKey); err != nil {
		return false, err
	}
	fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Configured existing GitHub App credentials"))
	return true, nil
}

func chooseBootstrapGitHubAppMode() (string, error) {
	if !bootstrapIsInteractive() {
		return "", fmt.Errorf("choose an existing GitHub App or set %s=create to allow browser-based creation in non-interactive environments. Example: export %s=existing", bootstrapGitHubAppModeEnv, bootstrapGitHubAppModeEnv)
	}
	var choice string
	form := console.NewSelectForm(huh.NewSelect[string]().
		Title("How should gh aw configure the GitHub App?").
		Description("Create a new GitHub App in the browser or provide credentials for an existing app.").
		Options(
			huh.NewOption("Create a new GitHub App", "create"),
			huh.NewOption("Use existing GitHub App credentials", "existing"),
		).
		Value(&choice))
	if err := form.Run(); err != nil {
		return "", err
	}
	if choice == "" {
		choice = "create"
	}
	return choice, nil
}

func completeExistingGitHubAppCredentials(existingClientID string, existingPrivateKey string, action repositoryPackageBootstrapAction, repo string) (string, string, error) {
	clientID := strings.TrimSpace(existingClientID)
	privateKey := strings.TrimRight(existingPrivateKey, "\r\n")
	var err error
	if clientID == "" {
		clientID, _, err = resolveBootstrapTextValue(bootstrapGitHubAppClientIDEnv, "GitHub App client ID", "Enter the GitHub App client ID to store in "+action.AppIDVariable+".", "", nil, false)
		if err != nil {
			return "", "", err
		}
	}
	if privateKey == "" {
		privateKey, _, err = resolveBootstrapSecretValue(bootstrapGitHubAppPrivateKeyEnv, "GitHub App private key", "Paste the PEM private key for the GitHub App used by "+repo+".", false)
		if err != nil {
			return "", "", err
		}
	}
	return clientID, privateKey, nil
}

func createBootstrapGitHubApp(ctx context.Context, repo, owner, repoName, ownerType string, action repositoryPackageBootstrapAction, overrides bootstrapGitHubAppOverrides) (*bootstrapCreatedGitHubApp, error) {
	state, err := bootstrapRandomHex(16)
	if err != nil {
		return nil, err
	}

	listener, err := netListener()
	if err != nil {
		return nil, err
	}
	defer listener.Close()

	appOwner, appOwnerType, appName, homepageURL, description, err := setupBootstrapGitHubAppDetails(ctx, repo, owner, ownerType, action, overrides)
	if err != nil {
		return nil, err
	}

	redirectURL := fmt.Sprintf("http://%s/callback", listener.Addr().String())
	manifest := buildBootstrapGitHubAppManifest(action, appName, homepageURL, redirectURL, description)
	bootstrapLog.Printf("Creating GitHub App via browser manifest flow: appOwner=%s, appName=%s, redirectURL=%s", appOwner, appName, redirectURL)
	registrationURL := buildBootstrapGitHubAppRegistrationURL(appOwner, appOwnerType, state)
	registrationPage, err := renderBootstrapGitHubAppRegistrationPage(registrationURL, manifest)
	if err != nil {
		return nil, fmt.Errorf("failed to encode GitHub App registration manifest for browser handoff; report this issue if it persists: %w", err)
	}

	resultCh := make(chan *bootstrapCreatedGitHubApp, 1)
	errCh := make(chan error, 1)
	flowCh := bootstrapGitHubAppFlowChannels{resultCh: resultCh, errCh: errCh}
	server := &http.Server{
		Handler: buildBootstrapGitHubAppMux(ctx, state, appOwner, appOwnerType, appName, description, registrationPage, flowCh),
	}
	go func() {
		defer func() {
			if r := recover(); r != nil {
				githubAppBootstrapLog.Printf("Panic in GitHub App registration server (recovered): %v", r)
			}
		}()
		_ = server.Serve(listener)
	}()
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	printBootstrapGitHubAppManifestReview(appOwner, manifest)
	openURL := fmt.Sprintf("http://%s/register", listener.Addr().String())
	opened := overrides.OpenBrowser && openBootstrapBrowser(openURL)
	if !opened {
		fmt.Fprintln(os.Stderr, console.FormatCommandMessage(openURL))
	}

	timeout := time.NewTimer(bootstrapProfileManifestTimeout)
	defer timeout.Stop()

	select {
	case createdApp := <-resultCh:
		return createdApp, nil
	case err := <-errCh:
		return nil, err
	case <-timeout.C:
		return nil, errors.New("timed out waiting for GitHub App creation to complete in the browser")
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// setupBootstrapGitHubAppDetails resolves the effective app owner (applying any override)
// and derives the app name, homepage URL, and description from the action and overrides.
func setupBootstrapGitHubAppDetails(ctx context.Context, repo, owner, ownerType string, action repositoryPackageBootstrapAction, overrides bootstrapGitHubAppOverrides) (string, string, string, string, string, error) {
	appOwner := owner
	appOwnerType := ownerType
	if overrides.Owner != "" {
		appOwner = overrides.Owner
		var err error
		appOwnerType, err = bootstrapCheckOwnerType(ctx, appOwner)
		if err != nil {
			return "", "", "", "", "", err
		}
	}
	appName := deriveBootstrapAppName(repo, firstNonEmpty(overrides.Name, action.AppName))
	homepageURL := firstNonEmpty(overrides.HomepageURL, action.HomepageURL)
	if homepageURL == "" {
		homepageURL = "https://github.com/" + repo
	}
	description := firstNonEmpty(overrides.Description, action.Description)
	if description == "" {
		description = "Bootstrap app for " + repo
	}
	return appOwner, appOwnerType, appName, homepageURL, description, nil
}

// buildBootstrapGitHubAppMux constructs the HTTP mux that handles the GitHub App manifest
// registration flow, including the /register page and the /callback that exchanges the code.
func buildBootstrapGitHubAppMux(ctx context.Context, csrfState, owner, ownerType, appName, description, registrationPage string, flowCh bootstrapGitHubAppFlowChannels) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/register", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, registrationPage)
	})
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		returnedState := r.URL.Query().Get("state")
		code := r.URL.Query().Get("code")
		if code == "" {
			http.Error(w, "Missing GitHub App manifest code.", http.StatusBadRequest)
			select {
			case flowCh.errCh <- errors.New("GitHub did not return an app manifest code"):
			default:
			}
			return
		}
		if !isBootstrapGitHubAppManifestCode(code) {
			http.Error(w, "Invalid GitHub App manifest code.", http.StatusBadRequest)
			select {
			case flowCh.errCh <- errors.New("invalid GitHub App manifest code format"):
			default:
			}
			return
		}
		if returnedState != csrfState {
			http.Error(w, "State mismatch while creating the GitHub App.", http.StatusBadRequest)
			select {
			case flowCh.errCh <- errors.New("state mismatch while creating the GitHub App"):
			default:
			}
			return
		}
		createdApp, exchangeErr := bootstrapExchangeGitHubAppCode(ctx, code, owner, ownerType, appName, description)
		if exchangeErr != nil {
			http.Error(w, "GitHub App creation completed, but gh aw could not exchange the manifest code.", http.StatusInternalServerError)
			select {
			case flowCh.errCh <- exchangeErr:
			default:
			}
			return
		}
		if createdApp.InstallURL != "" {
			http.Redirect(w, r, createdApp.InstallURL, http.StatusFound)
		} else {
			w.WriteHeader(http.StatusNoContent)
		}
		select {
		case flowCh.resultCh <- createdApp:
		default:
		}
	})
	return mux
}

func loadBootstrapGitHubAppOverrides() (bootstrapGitHubAppOverrides, error) {
	overrides := bootstrapGitHubAppOverrides{
		Mode:        "",
		Owner:       strings.TrimSpace(lookupEnv(bootstrapGitHubAppOwnerEnv)),
		Name:        strings.TrimSpace(lookupEnv(bootstrapGitHubAppNameEnv)),
		HomepageURL: strings.TrimSpace(lookupEnv(bootstrapGitHubAppURLEnv)),
		Description: strings.TrimSpace(lookupEnv(bootstrapGitHubAppDescriptionEnv)),
		OpenBrowser: true,
	}

	switch mode := strings.ToLower(strings.TrimSpace(lookupEnv(bootstrapGitHubAppModeEnv))); mode {
	case "", "auto":
	case "create", "existing":
		overrides.Mode = mode
	default:
		return bootstrapGitHubAppOverrides{}, fmt.Errorf("%s must be one of: auto, create, existing. Example: export %s=create", bootstrapGitHubAppModeEnv, bootstrapGitHubAppModeEnv)
	}

	if raw := strings.TrimSpace(lookupEnv(bootstrapNoOpenBrowserEnv)); raw != "" {
		disabled, err := parseBootstrapBool(raw)
		if err != nil {
			return bootstrapGitHubAppOverrides{}, fmt.Errorf("%s: %w", bootstrapNoOpenBrowserEnv, err)
		}
		overrides.OpenBrowser = !disabled
	}

	return overrides, nil
}

func isBootstrapGitHubAppManifestCode(code string) bool {
	if code == "" {
		return false
	}
	for _, ch := range code {
		if ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '-' || ch == '_' {
			continue
		}
		return false
	}
	return true
}

func bootstrapExchangeGitHubAppCodeImpl(ctx context.Context, code, owner, ownerType, appName, description string) (*bootstrapCreatedGitHubApp, error) {
	output, err := workflow.RunGHContext(ctx, "Exchanging GitHub App manifest code...", "api", "-X", "POST", "-H", "Accept: application/vnd.github+json", "/app-manifests/"+code+"/conversions")
	if err != nil {
		return nil, err
	}
	var payload bootstrapGitHubAppExchangeResponse
	if err := json.Unmarshal(output, &payload); err != nil {
		return nil, fmt.Errorf("failed to parse GitHub App manifest exchange response: %w", err)
	}
	return &bootstrapCreatedGitHubApp{
		Owner:       owner,
		OwnerType:   ownerType,
		Name:        firstNonEmpty(payload.Name, appName),
		SettingsURL: payload.HTMLURL,
		InstallURL:  buildBootstrapGitHubAppInstallURL(payload.Slug),
		ClientID:    payload.ClientID,
		AppID:       strconv.FormatInt(payload.ID, 10),
		PEM:         payload.PEM,
		Slug:        payload.Slug,
	}, nil
}

func waitForBootstrapGitHubAppInstallation(ctx context.Context, repo string, createdApp *bootstrapCreatedGitHubApp) error {
	if createdApp == nil || createdApp.InstallURL == "" || createdApp.Slug == "" {
		return nil
	}
	githubAppBootstrapLog.Printf("Waiting for GitHub App installation: repo=%s slug=%s installURL=%s", repo, createdApp.Slug, createdApp.InstallURL)
	bootstrapLog.Printf("Polling for GitHub App installation: repo=%s, slug=%s", repo, createdApp.Slug)
	deadlineTimer := time.NewTimer(bootstrapProfileManifestTimeout)
	defer deadlineTimer.Stop()
	pollTicker := time.NewTicker(bootstrapProfileInstallPollDelay)
	defer pollTicker.Stop()
	var lastErr error
	for {
		installed, err := bootstrapGitHubAppInstalled(ctx, repo, createdApp)
		if err == nil && installed {
			fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("GitHub App installation detected for "+repo))
			return nil
		}
		if err != nil {
			if !isRetryableBootstrapGitHubAppInstallationError(err) {
				return fmt.Errorf("failed to check GitHub App installation for %s: %w", repo, err)
			}
			lastErr = err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadlineTimer.C:
			if lastErr != nil {
				return fmt.Errorf("timed out waiting for the GitHub App installation to complete for %s: %w", repo, lastErr)
			}
			return fmt.Errorf("timed out waiting for the GitHub App installation to complete for %s", repo)
		case <-pollTicker.C:
		}
	}
}

func bootstrapGitHubAppInstalled(ctx context.Context, repo string, createdApp *bootstrapCreatedGitHubApp) (bool, error) {
	installations, err := listBootstrapUserInstallations(ctx)
	if err != nil {
		return false, err
	}
	for _, installation := range installations {
		if !bootstrapGitHubAppInstallationMatches(installation, createdApp) {
			continue
		}
		if installation.RepositorySelection != "selected" || repo == "" {
			return installation.ID > 0, nil
		}
		repositories, err := listBootstrapUserInstallationRepositories(ctx, installation.ID)
		if err != nil {
			return false, err
		}
		for _, repository := range repositories {
			if strings.EqualFold(repository, repo) {
				return true, nil
			}
		}
		return false, nil
	}
	return false, nil
}

func listBootstrapUserInstallations(ctx context.Context) ([]bootstrapGitHubAppUserInstallation, error) {
	output, err := runBootstrapGHContext(
		ctx,
		"Checking GitHub App installation...",
		"api",
		"/user/installations?per_page=100",
		"--paginate",
		"--jq",
		`.installations[] | [(.id|tostring), (.client_id // ""), (.app_id|tostring), .app_slug, .repository_selection] | @tsv`,
	)
	if err != nil {
		return nil, err
	}
	trimmed := strings.TrimSpace(string(output))
	if trimmed == "" {
		return nil, nil
	}
	lines := strings.Split(trimmed, "\n")
	installations := make([]bootstrapGitHubAppUserInstallation, 0, len(lines))
	for _, line := range lines {
		fields := strings.Split(line, "\t")
		if len(fields) != 5 {
			return nil, fmt.Errorf("failed to parse user installation response line %q", line)
		}
		installationID, err := strconv.ParseInt(fields[0], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("failed to parse user installation id %q: %w", fields[0], err)
		}
		installations = append(installations, bootstrapGitHubAppUserInstallation{
			ID:                  installationID,
			ClientID:            fields[1],
			AppID:               fields[2],
			AppSlug:             fields[3],
			RepositorySelection: fields[4],
		})
	}
	return installations, nil
}

func listBootstrapUserInstallationRepositories(ctx context.Context, installationID int64) ([]string, error) {
	output, err := runBootstrapGHContext(
		ctx,
		"Checking GitHub App installation repositories...",
		"api",
		fmt.Sprintf("/user/installations/%d/repositories?per_page=100", installationID),
		"--paginate",
		"--jq",
		".repositories[].full_name",
	)
	if err != nil {
		return nil, err
	}
	return parseBootstrapNames(output), nil
}

func bootstrapGitHubAppInstallationMatches(installation bootstrapGitHubAppUserInstallation, createdApp *bootstrapCreatedGitHubApp) bool {
	if createdApp == nil {
		return false
	}
	if installation.ClientID != "" && createdApp.ClientID != "" && installation.ClientID == createdApp.ClientID {
		return true
	}
	if installation.AppSlug != "" && createdApp.Slug != "" && installation.AppSlug == createdApp.Slug {
		return true
	}
	return installation.AppID != "" && createdApp.AppID != "" && installation.AppID == createdApp.AppID
}
