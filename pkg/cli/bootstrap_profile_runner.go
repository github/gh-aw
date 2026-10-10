package cli

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/tty"
	"github.com/github/gh-aw/pkg/workflow"
)

const (
	bootstrapProfileManifestTimeout  = 10 * time.Minute
	bootstrapProfileInstallPollDelay = 5 * time.Second
	bootstrapGitHubAppModeEnv        = "GH_AW_BOOTSTRAP_GITHUB_APP_MODE"
	bootstrapGitHubAppOwnerEnv       = "GH_AW_BOOTSTRAP_GITHUB_APP_OWNER"
	bootstrapGitHubAppNameEnv        = "GH_AW_BOOTSTRAP_GITHUB_APP_NAME"
	bootstrapGitHubAppURLEnv         = "GH_AW_BOOTSTRAP_GITHUB_APP_URL"
	bootstrapGitHubAppDescriptionEnv = "GH_AW_BOOTSTRAP_GITHUB_APP_DESCRIPTION"
	bootstrapGitHubAppClientIDEnv    = "GH_AW_BOOTSTRAP_GITHUB_APP_CLIENT_ID"
	bootstrapGitHubAppPrivateKeyEnv  = "GH_AW_BOOTSTRAP_GITHUB_APP_PRIVATE_KEY"
	bootstrapNoOpenBrowserEnv        = "GH_AW_BOOTSTRAP_NO_OPEN_BROWSER"
)

var (
	runBootstrapGHContext   = workflow.RunGHContext
	bootstrapIsInteractive  = tty.IsStderrTerminal
	bootstrapUpsertVariable = func(_ context.Context, repo, name, value string) error {
		return upsertBootstrapRepoVariable(repo, name, value)
	}
	bootstrapSetSecret = func(_ context.Context, repo, name, value string) error {
		return setBootstrapRepoSecret(repo, name, value)
	}
	bootstrapUpsertLabel            = upsertBootstrapRepoLabel
	bootstrapLabelNeedsMutation     = repoLabelNeedsMutation
	bootstrapCreateGitHubApp        = createBootstrapGitHubApp
	bootstrapCheckOwnerType         = checkSetupRepositoryOwnerType
	bootstrapExchangeGitHubAppCode  = bootstrapExchangeGitHubAppCodeImpl
	bootstrapInferGitHubAppRequires = inferBootstrapGitHubAppRequirements
)

type bootstrapProfileRunConfig struct {
	Repo     string
	RepoDir  string
	Sources  []string
	Profile  *resolvedBootstrapProfile
	Yes      bool
	PlanOnly bool
	Verbose  bool
	Force    bool
	// UseCopilotRequests indicates the user chose org-billing (copilot-requests) auth
	// instead of a PAT. When true, copilot-auth config actions are skipped because
	// the workflow already has permissions.copilot-requests: write injected.
	UseCopilotRequests bool
	// DisableGitHubAppPermissionInference disables inferring GitHub App
	// permissions/events from the package's resolved workflows. When true, only
	// permissions/events explicitly declared in aw.yml are applied to the App.
	DisableGitHubAppPermissionInference bool
}

type bootstrapProfileExistingState struct {
	variables map[string]struct{}
	secrets   map[string]struct{}
}

type bootstrapGitHubAppOverrides struct {
	Mode        string
	Owner       string
	Name        string
	HomepageURL string
	Description string
	OpenBrowser bool
}

type bootstrapCreatedGitHubApp struct {
	Owner       string
	OwnerType   string
	Name        string
	SettingsURL string
	InstallURL  string
	ClientID    string
	AppID       string
	PEM         string
	Slug        string
}

type bootstrapGitHubAppExchangeResponse struct {
	HTMLURL  string `json:"html_url"`
	ClientID string `json:"client_id"`
	ID       int64  `json:"id"`
	PEM      string `json:"pem"`
	Slug     string `json:"slug"`
	Name     string `json:"name"`
}

type bootstrapGitHubAppUserInstallation struct {
	ID                  int64
	ClientID            string
	AppID               string
	AppSlug             string
	RepositorySelection string
}

// bootstrapGitHubAppFlowChannels holds the send-only channels used to deliver results
// from the HTTP callback handler back to the manifest-flow coordinator.
type bootstrapGitHubAppFlowChannels struct {
	resultCh chan<- *bootstrapCreatedGitHubApp
	errCh    chan<- error
}

func executeBootstrapProfile(ctx context.Context, config bootstrapProfileRunConfig) error {
	if config.Profile == nil || config.Profile.Profile == nil {
		return nil
	}

	bootstrapLog.Printf("Executing bootstrap profile: repo=%s, actions=%d, useCopilotRequests=%t", config.Repo, len(config.Profile.Profile.Config), config.UseCopilotRequests)

	state, err := bootstrapProfileState(ctx, config.Repo)
	if err != nil {
		return err
	}
	usesActionsToken, err := profileSourcesUseActionsTokenCopilotAuth(ctx, config.Sources)
	if err != nil {
		return err
	}

	var inferredPermissions map[string]string
	var inferredEvents []string
	if !config.DisableGitHubAppPermissionInference && hasBootstrapGitHubAppAction(config.Profile.Profile.Config) {
		profileSources := config.Sources
		if config.Profile.Source != "" {
			profileSources = []string{config.Profile.Source}
		}
		inferredPermissions, inferredEvents, err = bootstrapInferGitHubAppRequires(ctx, profileSources)
		if err != nil {
			return err
		}
	}

	for _, action := range config.Profile.Profile.Config {
		if action.Type == "github-app" {
			action.Permissions, action.Events = mergeBootstrapGitHubAppRequirements(action.Permissions, action.Events, inferredPermissions, inferredEvents)
		}
		pending, err := bootstrapActionNeedsMutation(ctx, config.Repo, action, state, usesActionsToken)
		if err != nil {
			return err
		}
		if !pending && action.Type != "handoff" {
			bootstrapLog.Printf("Skipping bootstrap action (no mutation needed): type=%s", action.Type)
			continue
		}

		bootstrapLog.Printf("Applying bootstrap action: type=%s", action.Type)
		if err := applyBootstrapAction(ctx, config, action, state, usesActionsToken); err != nil {
			return err
		}
	}

	return nil
}

func applyBootstrapAction(ctx context.Context, config bootstrapProfileRunConfig, action repositoryPackageBootstrapAction, state *bootstrapProfileExistingState, usesActionsToken bool) error {
	switch action.Type {
	case "require-owner-type":
		if err := runBootstrapRequireOwnerType(ctx, config.Repo, action); err != nil {
			return err
		}
	case "repo-variable":
		applied, err := runBootstrapRepoVariableAction(ctx, config.Repo, action, state)
		if err != nil {
			return err
		}
		if applied {
			state.variables[action.Name] = struct{}{}
		}
	case "repo-secret":
		applied, err := runBootstrapRepoSecretAction(ctx, config.Repo, action, state)
		if err != nil {
			return err
		}
		if applied {
			state.secrets[action.Name] = struct{}{}
		}
	case "repo-label":
		if err := bootstrapUpsertLabel(ctx, config.Repo, action.Name, action.Description, action.Color); err != nil {
			return err
		}
	case "github-app":
		_, err := runBootstrapGitHubAppAction(ctx, config.Repo, action, state)
		if err != nil {
			return err
		}
		state.variables[action.AppIDVariable] = struct{}{}
		state.secrets[action.PrivateKeySecret] = struct{}{}
	case "copilot-auth":
		if config.UseCopilotRequests {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Skipping Copilot PAT setup because org Copilot billing is enabled."))
			return nil
		}
		applied, err := runBootstrapCopilotAuthAction(ctx, config.Repo, action, state, usesActionsToken)
		if err != nil {
			return err
		}
		if applied {
			state.secrets[action.Secret] = struct{}{}
		}
	case "commit-and-push":
		if err := runBootstrapCommitAndPushAction(ctx, config.RepoDir, action); err != nil {
			return err
		}
	case "handoff":
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(action.Message))
	default:
		return fmt.Errorf("unsupported bootstrap action type %q. Example: use one of %s", action.Type, bootstrapActionTypeExample)
	}
	return nil
}

func hasBootstrapGitHubAppAction(actions []repositoryPackageBootstrapAction) bool {
	for _, action := range actions {
		if action.Type == "github-app" {
			return true
		}
	}
	return false
}

func bootstrapProfileState(ctx context.Context, repo string) (*bootstrapProfileExistingState, error) {
	variableNames, err := listBootstrapRepoVariableNames(ctx, repo)
	if err != nil {
		return nil, err
	}
	secretNames, err := listBootstrapRepoSecretNames(ctx, repo)
	if err != nil {
		return nil, err
	}

	state := &bootstrapProfileExistingState{
		variables: make(map[string]struct{}, len(variableNames)),
		secrets:   make(map[string]struct{}, len(secretNames)),
	}
	for _, name := range variableNames {
		state.variables[name] = struct{}{}
	}
	for _, name := range secretNames {
		state.secrets[name] = struct{}{}
	}
	return state, nil
}

func bootstrapActionNeedsMutation(ctx context.Context, repo string, action repositoryPackageBootstrapAction, state *bootstrapProfileExistingState, usesActionsToken bool) (bool, error) {
	switch action.Type {
	case "require-owner-type":
		return false, runBootstrapRequireOwnerType(ctx, repo, action)
	case "repo-variable":
		_, exists := state.variables[action.Name]
		return !exists, nil
	case "repo-secret":
		_, exists := state.secrets[action.Name]
		return !exists, nil
	case "repo-label":
		return bootstrapLabelNeedsMutation(ctx, repo, action.Name, action.Description, action.Color)
	case "github-app":
		_, hasVar := state.variables[action.AppIDVariable]
		_, hasSecret := state.secrets[action.PrivateKeySecret]
		return !hasVar || !hasSecret, nil
	case "copilot-auth":
		_, hasSecret := state.secrets[action.Secret]
		return !hasSecret && !usesActionsToken, nil
	case "commit-and-push":
		return true, nil
	case "handoff":
		return false, nil
	default:
		return false, fmt.Errorf("unsupported bootstrap action type %q. Example: use one of %s", action.Type, bootstrapActionTypeExample)
	}
}
