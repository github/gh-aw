package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"

	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/repoutil"
	"github.com/github/gh-aw/pkg/stringutil"
)

var bootstrapActionsRepoLog = logger.New("cli:bootstrap_profile_actions_repo")

func runBootstrapRepoVariableAction(ctx context.Context, repo string, action repositoryPackageBootstrapAction, state *bootstrapProfileExistingState) (bool, error) {
	bootstrapActionsRepoLog.Printf("Running repo variable action: repo=%s, name=%s", repo, action.Name)
	if _, exists := state.variables[action.Name]; exists {
		bootstrapActionsRepoLog.Printf("Skipping variable %s: already set on repo", action.Name)
		return false, nil
	}
	value, ok, err := resolveBootstrapTextValue(bootstrapRepositoryVariableEnvName(action.Name), action.Prompt, action.Description, action.Default, action.Enum, action.Optional)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, nil
	}
	if err := bootstrapUpsertVariable(ctx, repo, action.Name, value); err != nil {
		return false, err
	}
	fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Set repository variable "+action.Name))
	return true, nil
}

func runBootstrapRepoSecretAction(ctx context.Context, repo string, action repositoryPackageBootstrapAction, state *bootstrapProfileExistingState) (bool, error) {
	bootstrapActionsRepoLog.Printf("Running repo secret action: repo=%s, name=%s", repo, action.Name)
	if _, exists := state.secrets[action.Name]; exists {
		bootstrapActionsRepoLog.Printf("Skipping secret %s: already set on repo", action.Name)
		return false, nil
	}
	value, ok, err := resolveBootstrapSecretValue(bootstrapRepositorySecretEnvName(action.Name), action.Prompt, action.Description, action.Optional)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, nil
	}
	if err := bootstrapSetSecret(ctx, repo, action.Name, value); err != nil {
		return false, err
	}
	fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Set repository secret "+action.Name))
	return true, nil
}

func runBootstrapCopilotAuthAction(ctx context.Context, repo string, action repositoryPackageBootstrapAction, state *bootstrapProfileExistingState, usesActionsToken bool) (bool, error) {
	bootstrapActionsRepoLog.Printf("Running Copilot auth action: repo=%s, usesActionsToken=%v", repo, usesActionsToken)
	if usesActionsToken {
		bootstrapActionsRepoLog.Print("Skipping Copilot PAT setup: workflows already support Actions token auth")
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Skipping Copilot PAT setup because selected workflows already support GitHub Actions token auth."))
		return false, nil
	}
	if _, exists := state.secrets[action.Secret]; exists {
		return false, nil
	}
	value, ok, err := resolveBootstrapSecretValue(action.Secret, "Copilot fine-grained PAT", "Enter a fine-grained personal access token starting with github_pat_.", false)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, nil
	}
	if err := stringutil.ValidateCopilotPAT(value); err != nil {
		return false, err
	}
	if err := bootstrapSetSecret(ctx, repo, action.Secret, value); err != nil {
		return false, err
	}
	fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Set repository secret "+action.Secret))
	return true, nil
}

func listBootstrapRepoVariableNames(ctx context.Context, repo string) ([]string, error) {
	output, err := runBootstrapGHContext(ctx, "Checking repository variables...", "api", fmt.Sprintf("/repos/%s/actions/variables?per_page=100", repo), "--paginate", "--jq", ".variables[].name")
	if err != nil {
		return nil, fmt.Errorf("failed to list repository variables for %s: %w", repo, err)
	}
	return parseBootstrapNames(output), nil
}

func listBootstrapRepoSecretNames(ctx context.Context, repo string) ([]string, error) {
	output, err := runBootstrapGHContext(ctx, "Checking repository secrets...", "api", fmt.Sprintf("/repos/%s/actions/secrets?per_page=100", repo), "--paginate", "--jq", ".secrets[].name")
	if err != nil {
		return nil, fmt.Errorf("failed to list repository secrets for %s: %w", repo, err)
	}
	return parseBootstrapNames(output), nil
}

func upsertBootstrapRepoVariable(repo, name, value string) error {
	target := defaultsTarget{}
	owner, repoName, err := repoutil.SplitRepoSlug(repo)
	if err != nil {
		return err
	}
	target.scope = defaultsScopeRepo
	target.repoOwner = owner
	target.repoName = repoName
	return upsertDefaultsVariable(target, name, value)
}

func setBootstrapRepoSecret(repo, name, value string) error {
	owner, repoName, err := repoutil.SplitRepoSlug(repo)
	if err != nil {
		return err
	}
	client, err := api.NewRESTClient(secretSetClientOptions(""))
	if err != nil {
		return err
	}
	return setRepoSecret(client, owner, repoName, name, value)
}

type bootstrapRepositoryLabel struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Color       string `json:"color"`
}

func upsertBootstrapRepoLabel(_ context.Context, repo, name, description, color string) error {
	owner, repoName, err := repoutil.SplitRepoSlug(repo)
	if err != nil {
		return err
	}
	client, err := api.NewRESTClient(secretSetClientOptions(""))
	if err != nil {
		return fmt.Errorf("failed to create GitHub API client for repository label %q: %w", name, err)
	}
	return upsertBootstrapRepoLabelWithClient(client, owner, repoName, name, description, color)
}

func repoLabelNeedsMutation(_ context.Context, repo, name, description, color string) (bool, error) {
	owner, repoName, err := repoutil.SplitRepoSlug(repo)
	if err != nil {
		return false, err
	}
	client, err := api.NewRESTClient(secretSetClientOptions(""))
	if err != nil {
		return false, fmt.Errorf("failed to create GitHub API client for repository label %q: %w", name, err)
	}
	return repoLabelNeedsMutationWithClient(client, owner, repoName, name, description, color)
}

type bootstrapLabelRESTClient interface {
	Get(path string, response any) error
	Post(path string, body io.Reader, response any) error
	Patch(path string, body io.Reader, response any) error
}

func repoLabelNeedsMutationWithClient(client bootstrapLabelRESTClient, owner, repoName, name, description, color string) (bool, error) {
	repo := path.Join(owner, repoName)
	existing, found, err := getBootstrapRepoLabelWithClient(client, owner, repoName, name)
	if err != nil {
		return false, fmt.Errorf("failed to inspect repository label %q in %s: %w", name, repo, err)
	}
	return !found || !bootstrapRepoLabelMatches(existing, name, description, color), nil
}

func upsertBootstrapRepoLabelWithClient(client bootstrapLabelRESTClient, owner, repoName, name, description, color string) error {
	repo := path.Join(owner, repoName)
	labelPath := bootstrapRepoLabelPath(owner, repoName, name)
	existing, found, err := getBootstrapRepoLabelWithClient(client, owner, repoName, name)
	if err != nil {
		return fmt.Errorf("failed to inspect repository label %q in %s: %w", name, repo, err)
	}
	if found {
		if bootstrapRepoLabelMatches(existing, name, description, color) {
			return nil
		}
		body, marshalErr := json.Marshal(map[string]string{
			"new_name":    name,
			"description": description,
			"color":       color,
		})
		if marshalErr != nil {
			return fmt.Errorf("failed to encode repository label %q: %w", name, marshalErr)
		}
		if err := client.Patch(labelPath, bytes.NewReader(body), nil); err != nil {
			return fmt.Errorf("failed to update repository label %q in %s: %w", name, repo, err)
		}
		return nil
	}

	body, err := json.Marshal(bootstrapRepositoryLabel{
		Name:        name,
		Description: description,
		Color:       color,
	})
	if err != nil {
		return fmt.Errorf("failed to encode repository label %q: %w", name, err)
	}
	if err := client.Post(fmt.Sprintf("repos/%s/labels", repo), bytes.NewReader(body), nil); err != nil {
		return fmt.Errorf("failed to create repository label %q in %s: %w", name, repo, err)
	}
	return nil
}

func getBootstrapRepoLabelWithClient(client bootstrapLabelRESTClient, owner, repoName, name string) (bootstrapRepositoryLabel, bool, error) {
	var existing bootstrapRepositoryLabel
	err := client.Get(bootstrapRepoLabelPath(owner, repoName, name), &existing)
	if err == nil {
		return existing, true, nil
	}
	if bootstrapHTTPStatusMatches(err, http.StatusNotFound) {
		return bootstrapRepositoryLabel{}, false, nil
	}
	return bootstrapRepositoryLabel{}, false, err
}

func bootstrapRepoLabelPath(owner, repoName, name string) string {
	return fmt.Sprintf("repos/%s/labels/%s", path.Join(owner, repoName), url.PathEscape(name))
}

func bootstrapRepoLabelMatches(existing bootstrapRepositoryLabel, name, description, color string) bool {
	return strings.EqualFold(existing.Name, name) && existing.Description == description && strings.EqualFold(existing.Color, color)
}

type bootstrapHTTPStatusCoder interface {
	HTTPStatusCode() int
}

type bootstrapStatusCoder interface {
	StatusCode() int
}

func bootstrapHTTPStatusMatches(err error, status int) bool {
	var httpErr *api.HTTPError
	if errors.As(err, &httpErr) {
		return httpErr.StatusCode == status
	}

	var httpStatusErr bootstrapHTTPStatusCoder
	if errors.As(err, &httpStatusErr) {
		return httpStatusErr.HTTPStatusCode() == status
	}

	var statusErr bootstrapStatusCoder
	if errors.As(err, &statusErr) {
		return statusErr.StatusCode() == status
	}

	return false
}
