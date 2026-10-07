package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"

	"charm.land/huh/v2"
	lipgloss "charm.land/lipgloss/v2"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/repoutil"
	"github.com/github/gh-aw/pkg/setutil"
	"github.com/github/gh-aw/pkg/sliceutil"
	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/github/gh-aw/pkg/workflow"
)

var engineSecretsLog = logger.New("cli:engine_secrets")

var errUnsupportedClaudeOAuthToken = errors.New("CLAUDE_CODE_OAUTH_TOKEN is not supported for Claude workflows; use ANTHROPIC_API_KEY or configure Anthropic Workload Identity Federation instead")

// Overridable for testing
var (
	engineSecretsPromptFn = func(req SecretRequirement, config EngineSecretConfig) error {
		return promptForSecret(req, config)
	}
	engineSecretsConfirmExistingFn = func(secretName string, config EngineSecretConfig) (bool, error) {
		useExisting := true
		form := console.NewConfirmForm(
			huh.NewConfirm().
				Title(fmt.Sprintf("Use the existing %s repository secret?", secretName)).
				Description("GitHub does not expose stored secret values. Choosing this asserts that the existing secret is a valid fine-grained PAT with Copilot Requests permission.").
				Affirmative("Use existing secret").
				Negative("Replace secret").
				Value(&useExisting),
		)
		if err := form.RunWithContext(config.ctx()); err != nil {
			if console.IsCancelled(err) {
				return false, promptCancelled()
			}
			return false, fmt.Errorf("failed to confirm existing %s secret: %w", secretName, err)
		}
		return useExisting, nil
	}
	engineSecretsUploadFn = func(ctx context.Context, secretName, secretValue, repoSlug string, verbose bool, overwriteExisting bool) error {
		return uploadSecretToRepo(ctx, secretName, secretValue, repoSlug, verbose, overwriteExisting)
	}
)

// promptCancelled handles graceful cancellation of an interactive prompt.
// It prints "Cancelled." to stderr and returns an ExitCodeError with code 130.
func promptCancelled() error {
	fmt.Fprintln(os.Stderr, "Cancelled.")
	return &ExitCodeError{Code: 130}
}

// SecretRequirement represents a unified secret requirement for agentic workflows.
// This type unifies the legacy tokenSpec and EngineOption secret information.
type SecretRequirement struct {
	Name               string   // The secret name (e.g., "COPILOT_GITHUB_TOKEN")
	WhenNeeded         string   // Human-readable description of when this secret is needed
	Description        string   // Detailed description of the secret's purpose and required permissions
	Optional           bool     // Whether this secret is optional
	AlternativeEnvVars []string // Alternative environment variable names to check
	KeyURL             string   // URL where users can obtain their API key
	IsEngineSecret     bool     // True if this is an engine-specific secret (vs system-level)
	EngineName         string   // The engine this secret is for (if IsEngineSecret is true)
}

// EngineSecretConfig contains configuration for engine secret collection operations
type EngineSecretConfig struct {
	// Ctx is the context for cancellation (optional, but recommended for proper Ctrl-C handling)
	Ctx context.Context
	// RepoSlug is the repository slug to check for existing secrets (optional)
	RepoSlug string
	// Engine is the engine type to collect secrets for (e.g., "copilot", "claude", "codex")
	Engine string
	// Verbose enables verbose output
	Verbose bool
	// ExistingSecrets is a map of secret names that already exist in the repository
	ExistingSecrets map[string]struct{}
	// OverwriteExistingSecret forces uploads to replace an existing repository secret value.
	OverwriteExistingSecret bool
	// IncludeSystemSecrets includes system-level secrets like GH_AW_GITHUB_TOKEN
	IncludeSystemSecrets bool
	// IncludeOptional includes optional secrets in the requirements list
	IncludeOptional bool
}

// getSecretRequirementsForEngine returns all secrets needed for a specific engine.
// This combines engine-specific secrets with optional system-level secrets.
func getSecretRequirementsForEngine(engine string, includeSystemSecrets bool, includeOptional bool) []SecretRequirement {
	engineSecretsLog.Printf("Getting required secrets for engine: %s (system=%v, optional=%v)", engine, includeSystemSecrets, includeOptional)

	var requirements []SecretRequirement

	// Add system-level secrets first if requested
	if includeSystemSecrets {
		for _, sys := range constants.SystemSecrets {
			if sys.Optional && !includeOptional {
				continue
			}
			requirements = append(requirements, SecretRequirement{
				Name:           sys.Name,
				WhenNeeded:     sys.WhenNeeded,
				Description:    sys.Description,
				Optional:       sys.Optional,
				IsEngineSecret: false,
			})
		}
	}

	// Add engine-specific secret
	opt := constants.GetEngineOption(engine)
	if opt != nil {
		requirements = append(requirements, SecretRequirement{
			Name:               opt.SecretName,
			WhenNeeded:         opt.WhenNeeded,
			Description:        getEngineSecretDescription(opt),
			Optional:           false,
			AlternativeEnvVars: opt.AlternativeSecrets,
			KeyURL:             opt.KeyURL,
			IsEngineSecret:     true,
			EngineName:         engine,
		})
	}

	engineSecretsLog.Printf("Returning %d secret requirements for engine %s", len(requirements), engine)
	return requirements
}

// getEngineSecretDescription returns a detailed description for an engine secret
func getEngineSecretDescription(opt *constants.EngineOption) string {
	switch opt.Value {
	case string(constants.CopilotEngine):
		return "Fine-grained PAT with Copilot Requests permission."
	case string(constants.ClaudeEngine):
		return "API key from Anthropic Console for Claude API access."
	case string(constants.CodexEngine):
		return "API key from OpenAI for Codex/GPT API access."
	default:
		return fmt.Sprintf("API key for %s engine.", opt.Label)
	}
}

// secretRequirementsFromAuthDefinition converts an AuthDefinition into SecretRequirement
// entries so inline auth secrets are treated as required secrets (same as built-in
// engine secrets). Returns nil when auth is nil.
func secretRequirementsFromAuthDefinition(auth *workflow.AuthDefinition, engineName string) []SecretRequirement {
	if auth == nil {
		return nil
	}

	var reqs []SecretRequirement

	switch auth.Strategy {
	case workflow.AuthStrategyOAuthClientCreds:
		// OAuth client-credentials flow: require client-id and client-secret secrets.
		if auth.ClientIDRef != "" {
			reqs = append(reqs, SecretRequirement{
				Name:           auth.ClientIDRef,
				WhenNeeded:     fmt.Sprintf("OAuth client ID for %s engine", engineName),
				Description:    "GitHub Actions secret holding the OAuth 2.0 client ID used to obtain access tokens.",
				IsEngineSecret: true,
				EngineName:     engineName,
			})
		}
		if auth.ClientSecretRef != "" {
			reqs = append(reqs, SecretRequirement{
				Name:           auth.ClientSecretRef,
				WhenNeeded:     fmt.Sprintf("OAuth client secret for %s engine", engineName),
				Description:    "GitHub Actions secret holding the OAuth 2.0 client secret used to obtain access tokens.",
				IsEngineSecret: true,
				EngineName:     engineName,
			})
		}
	default:
		// api-key, bearer, or unset strategy: require the direct secret.
		if auth.Secret != "" {
			reqs = append(reqs, SecretRequirement{
				Name:           auth.Secret,
				WhenNeeded:     fmt.Sprintf("API key or token for %s engine", engineName),
				Description:    "GitHub Actions secret holding the API key or bearer token for provider authentication.",
				IsEngineSecret: true,
				EngineName:     engineName,
			})
		}
	}

	return reqs
}

// getMissingRequiredSecrets filters requirements to return only missing required secrets.
// It skips optional secrets and checks both primary and alternative secret names.
func getMissingRequiredSecrets(requirements []SecretRequirement, existingSecrets map[string]struct {
}) []SecretRequirement {
	var missing []SecretRequirement
	for _, req := range requirements {
		// Skip optional secrets - we only care about required ones
		if req.Optional {
			continue
		}

		exists := setutil.Contains(existingSecrets, req.Name) || sliceutil.Any(req.AlternativeEnvVars, func(alt string) bool {
			return setutil.Contains(existingSecrets, alt)
		})
		if !exists {
			missing = append(missing, req)
		}
	}
	return missing
}

// ctx returns the context from the config, defaulting to background if nil
func (c EngineSecretConfig) ctx() context.Context {
	if c.Ctx != nil {
		return c.Ctx
	}
	return context.Background()
}

// checkAndEnsureEngineSecretsForEngine is the unified entry point for checking and collecting engine secrets.
// It checks existing secrets in the repository and environment, and prompts for missing ones.
func checkAndEnsureEngineSecretsForEngine(config EngineSecretConfig) error {
	engineSecretsLog.Printf("Checking and collecting secrets for engine: %s in repo: %s", config.Engine, config.RepoSlug)

	// Get required secrets for the engine
	requirements := getSecretRequirementsForEngine(config.Engine, config.IncludeSystemSecrets, config.IncludeOptional)

	// Check each requirement
	for _, req := range requirements {
		if req.Optional {
			// For optional secrets, just check and report
			if err := checkOptionalSecret(req, config); err != nil && config.Verbose {
				console.PrintWarningMessage(fmt.Sprintf("Optional secret %s: %v", req.Name, err))
			}
			continue
		}

		// For required secrets, ensure they're available
		if err := ensureSecretAvailable(req, config); err != nil {
			return fmt.Errorf("failed to ensure secret %s: %w", req.Name, err)
		}
	}

	return nil
}

// ensureSecretAvailable ensures that a required secret is available.
// It checks the repository, environment, and prompts the user if needed.
func ensureSecretAvailable(req SecretRequirement, config EngineSecretConfig) error {
	engineSecretsLog.Printf("Ensuring secret available: %s", req.Name)

	// Check if secret already exists in the repository
	if setutil.Contains(config.ExistingSecrets, req.Name) {
		if mustValidateExistingSecretValue(req) {
			useExisting, err := engineSecretsConfirmExistingFn(req.Name, config)
			if err != nil {
				return err
			}
			if useExisting {
				console.PrintSuccessMessage(fmt.Sprintf("Using existing %s secret in repository", req.Name))
				return nil
			}
			console.PrintInfoMessage("Paste the current or replacement fine-grained PAT so gh aw can validate it and update the repository secret.")
			revalidateConfig := config
			revalidateConfig.OverwriteExistingSecret = true
			return engineSecretsPromptFn(req, revalidateConfig)
		}
		console.PrintSuccessMessage(fmt.Sprintf("Using existing %s secret in repository", req.Name))
		return nil
	}

	// Check alternative secret names in repository
	for _, alt := range req.AlternativeEnvVars {
		if setutil.Contains(config.ExistingSecrets, alt) {
			if mustValidateExistingSecretValue(req) {
				useExisting, err := engineSecretsConfirmExistingFn(alt, config)
				if err != nil {
					return err
				}
				if useExisting {
					console.PrintSuccessMessage(fmt.Sprintf("Using existing %s secret in repository (alternative for %s)", alt, req.Name))
					return nil
				}
				console.PrintInfoMessage(fmt.Sprintf("Paste the current or replacement fine-grained PAT so gh aw can validate it and store it as %s.", req.Name))
				revalidateConfig := config
				revalidateConfig.OverwriteExistingSecret = true
				return engineSecretsPromptFn(req, revalidateConfig)
			}
			console.PrintSuccessMessage(fmt.Sprintf("Using existing %s secret in repository (alternative for %s)", alt, req.Name))
			return nil
		}
	}

	// Check environment variable
	envValue := os.Getenv(req.Name) //nolint:osgetenvlibrary
	if envValue == "" {
		// Check alternative environment variables
		for _, alt := range req.AlternativeEnvVars {
			envValue = os.Getenv(alt) //nolint:osgetenvlibrary
			if envValue != "" {
				engineSecretsLog.Printf("Found secret in alternative env var: %s", alt)
				break
			}
		}
	}

	if envValue != "" {
		// Validate if it's a Copilot token
		if req.IsEngineSecret && req.EngineName == string(constants.CopilotEngine) {
			if err := stringutil.ValidateCopilotPAT(envValue); err != nil {
				console.PrintWarningMessage(fmt.Sprintf("%s in environment is not a valid fine-grained PAT: %s", req.Name, stringutil.GetPATTypeDescription(envValue)))
				console.PrintErrorMessage(err.Error())
				// Continue to prompt for a new token
			} else {
				console.PrintSuccessMessage(fmt.Sprintf("Found valid %s in environment", req.Name))
				// Upload to repository if we have a repo slug
				if config.RepoSlug != "" {
					return engineSecretsUploadFn(config.ctx(), req.Name, envValue, config.RepoSlug, config.Verbose, config.OverwriteExistingSecret)
				}
				return nil
			}
		} else {
			console.PrintSuccessMessage(fmt.Sprintf("Found %s in environment", req.Name))
			// Upload to repository if we have a repo slug
			if config.RepoSlug != "" {
				return engineSecretsUploadFn(config.ctx(), req.Name, envValue, config.RepoSlug, config.Verbose, config.OverwriteExistingSecret)
			}
			return nil
		}
	}

	// Secret not found, prompt user for it
	if req.IsEngineSecret && req.EngineName == string(constants.ClaudeEngine) && os.Getenv("CLAUDE_CODE_OAUTH_TOKEN") != "" { //nolint:osgetenvlibrary
		return errUnsupportedClaudeOAuthToken
	}
	return engineSecretsPromptFn(req, config)
}

// promptForSecret prompts the user to provide a secret value
func promptForSecret(req SecretRequirement, config EngineSecretConfig) error {
	engineSecretsLog.Printf("Prompting for secret: %s", req.Name)

	// Copilot requires special handling with PAT creation instructions
	if req.IsEngineSecret && req.EngineName == string(constants.CopilotEngine) {
		return promptForCopilotPATUnified(req, config)
	}

	// System secrets (GH_AW_*) require PAT-specific prompting, not API key wording
	if !req.IsEngineSecret {
		return promptForSystemTokenUnified(req, config)
	}

	return promptForGenericAPIKeyUnified(req, config)
}

func mustValidateExistingSecretValue(req SecretRequirement) bool {
	return req.IsEngineSecret && req.EngineName == string(constants.CopilotEngine)
}

// promptForCopilotPATUnified prompts the user for a Copilot PAT with detailed instructions
func promptForCopilotPATUnified(req SecretRequirement, config EngineSecretConfig) error {
	preconfiguredPATURL := buildCopilotPATCreationURL()

	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Create a fine-grained Personal Access Token (PAT) from the preconfigured page below, then paste it back here.")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Preconfigured token creation page:")
	console.PrintCommandMessage("  " + preconfiguredPATURL)

	openBrowser := true
	confirmForm := console.NewConfirmForm(
		huh.NewConfirm().
			Title("Open the preconfigured token creation page in your browser now?").
			Affirmative("Yes, open browser").
			Negative("No, I'll use the URL above").
			Value(&openBrowser),
	)
	if err := confirmForm.RunWithContext(config.ctx()); err != nil {
		if console.IsCancelled(err) {
			return promptCancelled()
		}
		// Non-interactive: skip the consent gate and fall through to token input
	} else if openBrowser {
		if openBootstrapBrowser(preconfiguredPATURL) {
			console.PrintSuccessMessage("Opened the preconfigured Copilot PAT page in your browser.")
		} else {
			console.PrintWarningMessage("Couldn't open your browser automatically (no supported opener found) — open the URL above manually.")
		}
	}

	var token string
	form := console.NewInputForm(
		huh.NewInput().
			Title("Paste an existing or newly created fine-grained Copilot PAT:").
			Description("The page only prefills the token form. You still need to complete token creation in GitHub. Resource owner and repository access require manual selection in the browser. A reusable token must be a fine-grained PAT for your personal account with repository access set to Public repositories and Copilot Requests permission available. Do not rely on the PAT display name alone in GitHub's token list. Copy the token you want to use from GitHub, paste it into this hidden field, then press Enter. Must start with 'github_pat_'. Classic PATs (ghp_...) are not supported. Help: https://github.github.com/gh-aw/reference/auth/#copilot_github_token.").
			EchoMode(huh.EchoModePassword).
			Value(&token).
			Validate(func(s string) error {
				s = strings.TrimSpace(s)
				if s == "" {
					return errors.New("token is required")
				}
				if len(s) < 10 {
					return errors.New("token appears to be too short")
				}
				return stringutil.ValidateCopilotPAT(s)
			}),
	)

	if err := form.RunWithContext(config.ctx()); err != nil {
		if console.IsCancelled(err) {
			return promptCancelled()
		}
		return fmt.Errorf("failed to get Copilot token: %w", err)
	}

	token = strings.TrimSpace(token)

	console.PrintSuccessMessage("Valid fine-grained Copilot token received")

	// Upload to repository if we have a repo slug
	if config.RepoSlug != "" {
		return uploadSecretToRepo(config.ctx(), req.Name, token, config.RepoSlug, config.Verbose, config.OverwriteExistingSecret)
	}

	return nil
}

func buildCopilotPATCreationURL() string {
	values := url.Values{}
	values.Set("name", constants.CopilotGitHubToken)
	values.Set("user_copilot_requests", "read")
	return buildPATCreationURL(values)
}

func buildGenericPATCreationURL() string {
	return buildPATCreationURL(nil)
}

func buildPATCreationURL(values url.Values) string {
	hostURL := getGitHubHost()
	// Only consult the git remote when the caller has not made an explicit host
	// choice via an environment variable.  Falling back when an env var selects
	// public GitHub would silently override that explicit choice.
	if !isAnyGitHubHostEnvVarSet() {
		if detectedHost := getHostFromOriginRemote(); detectedHost != "" && detectedHost != "github.com" {
			hostURL = stringutil.NormalizeGitHubHostURL(detectedHost)
		}
	}

	baseURL := strings.TrimRight(hostURL, "/") + "/settings/personal-access-tokens/new"
	if len(values) == 0 {
		return baseURL
	}

	return baseURL + "?" + values.Encode()
}

// promptForSystemTokenUnified prompts the user for a system-level GitHub token (PAT)
// This uses PAT-specific wording instead of "API key" since system secrets are GitHub tokens
func promptForSystemTokenUnified(req SecretRequirement, config EngineSecretConfig) error {
	engineSecretsLog.Printf("Prompting for system token: %s", req.Name)
	patURL := buildGenericPATCreationURL()

	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintf(os.Stderr, "%s requires a GitHub Personal Access Token (PAT).\n", req.Name)
	fmt.Fprintln(os.Stderr, "")
	console.PrintInfoMessage("When needed: " + req.WhenNeeded)
	console.PrintInfoMessage("Recommended scopes: " + req.Description)
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Create a token at:")
	console.PrintCommandMessage("  " + patURL)
	fmt.Fprintln(os.Stderr, "")

	var token string
	form := console.NewInputForm(
		huh.NewInput().
			Title(fmt.Sprintf("Paste your %s token:", req.Name)).
			Description("The token will be stored securely as a repository secret").
			EchoMode(huh.EchoModePassword).
			Value(&token).
			Validate(func(s string) error {
				if len(s) < 10 {
					return errors.New("token appears to be too short")
				}
				return nil
			}),
	)

	if err := form.RunWithContext(config.ctx()); err != nil {
		if console.IsCancelled(err) {
			return promptCancelled()
		}
		return fmt.Errorf("failed to get %s token: %w", req.Name, err)
	}

	console.PrintSuccessMessage(req.Name + " token received")

	// Upload to repository if we have a repo slug
	if config.RepoSlug != "" {
		return uploadSecretToRepo(config.ctx(), req.Name, token, config.RepoSlug, config.Verbose, config.OverwriteExistingSecret)
	}

	return nil
}

// promptForGenericAPIKeyUnified prompts the user for a generic API key
func promptForGenericAPIKeyUnified(req SecretRequirement, config EngineSecretConfig) error {
	engineSecretsLog.Printf("Prompting for API key: %s", req.Name)

	// Get engine option for label
	opt := constants.GetEngineOption(req.EngineName)
	label := req.Name
	if opt != nil {
		label = opt.Label
	}

	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintf(os.Stderr, "%s requires an API key.\n", label)
	fmt.Fprintln(os.Stderr, "")
	if req.KeyURL != "" {
		fmt.Fprintln(os.Stderr, "Get your API key from:")
		console.PrintCommandMessage("  " + req.KeyURL)
		fmt.Fprintln(os.Stderr, "")
	}

	var apiKey string
	form := console.NewInputForm(
		huh.NewInput().
			Title(fmt.Sprintf("Paste your %s API key:", label)).
			Description("The key will be stored securely as a repository secret").
			EchoMode(huh.EchoModePassword).
			Value(&apiKey).
			Validate(func(s string) error {
				if len(s) < 10 {
					return errors.New("API key appears to be too short")
				}
				return nil
			}),
	)

	if err := form.RunWithContext(config.ctx()); err != nil {
		if console.IsCancelled(err) {
			return promptCancelled()
		}
		return fmt.Errorf("failed to get %s API key: %w", label, err)
	}

	console.PrintSuccessMessage(label + " API key received")

	// Upload to repository if we have a repo slug
	if config.RepoSlug != "" {
		return uploadSecretToRepo(config.ctx(), req.Name, apiKey, config.RepoSlug, config.Verbose, config.OverwriteExistingSecret)
	}

	return nil
}

// checkOptionalSecret checks if an optional secret is available (without prompting)
func checkOptionalSecret(req SecretRequirement, config EngineSecretConfig) error {
	// Check repository
	if setutil.Contains(config.ExistingSecrets, req.Name) {
		if config.Verbose {
			console.PrintInfoMessage(fmt.Sprintf("Optional secret %s exists in repository", req.Name))
		}
		return nil
	}

	// Check environment
	if os.Getenv(req.Name) != "" { //nolint:osgetenvlibrary
		if config.Verbose {
			console.PrintInfoMessage(fmt.Sprintf("Optional secret %s found in environment", req.Name))
		}
		return nil
	}

	return errors.New("not configured")
}

// uploadSecretToRepo uploads a secret to the repository and can optionally replace an existing value.
func uploadSecretToRepo(ctx context.Context, secretName, secretValue, repoSlug string, verbose bool, overwriteExisting bool) error {
	engineSecretsLog.Printf("Uploading secret %s to %s", secretName, repoSlug)

	// Check if secret already exists
	output, err := workflow.RunGHCombined("Checking secrets...", "secret", "list", "--repo", repoSlug)
	if err == nil && stringContainsSecretName(string(output), secretName) {
		if !overwriteExisting {
			if verbose {
				console.PrintInfoMessage(fmt.Sprintf("Secret %s already exists, skipping upload", secretName))
			}
			return nil
		}
		if verbose {
			console.PrintInfoMessage(fmt.Sprintf("Secret %s already exists, replacing it with the validated value", secretName))
		}
	}

	// Upload the secret
	if verbose {
		console.PrintInfoMessage(fmt.Sprintf("Uploading %s secret to repository", secretName))
	}

	output, err = workflow.RunGHInputContext(
		ctx,
		"Setting secret...",
		bytes.NewBufferString(secretValue),
		"secret", "set", secretName, "--repo", repoSlug,
	)
	if err != nil {
		return fmt.Errorf("failed to set %s secret: %w (output: %s)", secretName, err, string(output))
	}

	console.PrintSuccessMessage(fmt.Sprintf("Uploaded %s secret to repository", secretName))
	return nil
}

// stringContainsSecretName checks if the gh secret list output contains a secret name
func stringContainsSecretName(output, secretName string) bool {
	lines := strings.SplitSeq(output, "\n")
	for line := range lines {
		if len(line) >= len(secretName) {
			if line[:len(secretName)] == secretName && (len(line) == len(secretName) || line[len(secretName)] == '\t' || line[len(secretName)] == ' ') {
				return true
			}
		}
	}
	return false
}

// getExistingSecretsInRepo checks which secrets exist in the repository
func getExistingSecretsInRepo(repoSlug string) (map[string]struct {
}, error) {
	engineSecretsLog.Printf("Checking existing secrets for repo: %s", repoSlug)

	existingSecrets := make(map[string]struct {
	})

	// List secrets from repository
	output, err := workflow.RunGHCombined("Checking secrets...", "secret", "list", "--repo", repoSlug)
	if err != nil {
		engineSecretsLog.Printf("Could not list secrets for %s: %v", repoSlug, err)
		return existingSecrets, err
	}

	// Check for all known engine secrets
	secretNames := constants.GetAllEngineSecretNames()

	outputStr := string(output)
	for _, name := range secretNames {
		if stringContainsSecretName(outputStr, name) {
			existingSecrets[name] = struct {
			}{}
		}
	}

	return existingSecrets, nil
}

// GetEngineSecretNameAndValue returns the secret name and value for an engine.
// It checks if the secret exists in the repository and retrieves the value from environment if needed.
// Returns: secretName, secretValue (empty if exists in repo or not in env), existsInRepo, error
func GetEngineSecretNameAndValue(engine string, existingSecrets map[string]struct{}) (string, string, bool, error) {
	engineSecretsLog.Printf("Getting secret name and value for engine: %s", engine)

	opt := constants.GetEngineOption(engine)
	if opt == nil {
		return "", "", false, fmt.Errorf("unknown engine: %s", engine)
	}

	secretName := opt.SecretName

	// Check if secret already exists in repository
	if setutil.Contains(existingSecrets, secretName) {
		engineSecretsLog.Printf("Secret %s already exists in repository", secretName)
		return secretName, "", true, nil
	}

	// Check alternative secret names in repository
	for _, alt := range opt.AlternativeSecrets {
		if setutil.Contains(existingSecrets, alt) {
			engineSecretsLog.Printf("Alternative secret %s exists in repository", alt)
			return secretName, "", true, nil
		}
	}

	// Get value from environment variable
	// Use EnvVarName if specified, otherwise use SecretName
	envVar := opt.SecretName
	if opt.EnvVarName != "" {
		envVar = opt.EnvVarName
	}

	value := os.Getenv(envVar) //nolint:osgetenvlibrary
	if value == "" {
		// Check alternative environment variables
		for _, alt := range opt.AlternativeSecrets {
			value = os.Getenv(alt) //nolint:osgetenvlibrary
			if value != "" {
				engineSecretsLog.Printf("Found secret in alternative env var: %s", alt)
				break
			}
		}
	}

	if engine == string(constants.ClaudeEngine) && value == "" && os.Getenv("CLAUDE_CODE_OAUTH_TOKEN") != "" { //nolint:osgetenvlibrary
		return "", "", false, errUnsupportedClaudeOAuthToken
	}

	return secretName, value, false, nil
}

// displayMissingSecrets shows information about missing secrets with setup instructions
func displayMissingSecrets(requirements []SecretRequirement, repoSlug string, existingSecrets map[string]struct {
}) {
	var requiredMissing, optionalMissing []SecretRequirement

	for _, req := range requirements {
		// Check if secret exists
		exists := setutil.Contains(existingSecrets, req.Name) || sliceutil.Any(req.AlternativeEnvVars, func(alt string) bool {
			return setutil.Contains(existingSecrets, alt)
		})

		if !exists {
			if req.Optional {
				optionalMissing = append(optionalMissing, req)
			} else {
				requiredMissing = append(requiredMissing, req)
			}
		}
	}

	// Extract owner and repo from slug for command examples
	parts := splitRepoSlug(repoSlug)
	cmdOwner := parts[0]
	cmdRepo := parts[1]

	if len(requiredMissing) > 0 {
		console.PrintErrorMessage("Required secrets are missing:")
		for _, req := range requiredMissing {
			fmt.Fprintln(os.Stderr, "")
			console.PrintInfoMessage("Secret: " + req.Name)
			console.PrintInfoMessage("When needed: " + req.WhenNeeded)
			console.PrintInfoMessage("Recommended scopes: " + req.Description)
			console.PrintCommandMessage(fmt.Sprintf("gh aw secrets set %s --owner %s --repo %s", req.Name, cmdOwner, cmdRepo))
		}
	}

	if len(optionalMissing) > 0 {
		fmt.Fprintln(os.Stderr, "")
		console.PrintWarningMessage("Optional secrets are missing:")
		for _, req := range optionalMissing {
			fmt.Fprintln(os.Stderr, "")
			console.PrintInfoMessage(fmt.Sprintf("Secret: %s (optional)", req.Name))
			console.PrintInfoMessage("When needed: " + req.WhenNeeded)
			console.PrintInfoMessage("Recommended scopes: " + req.Description)
			console.PrintCommandMessage(fmt.Sprintf("gh aw secrets set %s --owner %s --repo %s", req.Name, cmdOwner, cmdRepo))
		}
	}

	fmt.Fprintln(os.Stderr, "")
}

// displaySecretsSummaryTable displays a summary table of all required secrets with their status
func displaySecretsSummaryTable(requirements []SecretRequirement, existingSecrets map[string]struct {
}) {
	// Filter to only required secrets (not optional)
	var requiredOnly []SecretRequirement
	for _, req := range requirements {
		if !req.Optional {
			requiredOnly = append(requiredOnly, req)
		}
	}

	// If no required secrets, don't show the table
	if len(requiredOnly) == 0 {
		return
	}

	fmt.Fprintln(os.Stderr, "")
	console.PrintInfoMessage("Required secrets summary:")
	fmt.Fprintln(os.Stderr, "")

	// Calculate max width for alignment
	maxNameWidth := 0
	for _, req := range requiredOnly {
		if lipgloss.Width(req.Name) > maxNameWidth {
			maxNameWidth = lipgloss.Width(req.Name)
		}
	}

	// Display each required secret with status
	for _, req := range requiredOnly {
		// Check if secret exists
		exists := setutil.Contains(existingSecrets, req.Name)
		var altUsed string
		if !exists {
			// Check alternatives
			for _, alt := range req.AlternativeEnvVars {
				if setutil.Contains(existingSecrets, alt) {
					exists = true
					altUsed = alt
					break
				}
			}
		}

		// Format status indicator
		var statusLine string
		if exists {
			if altUsed != "" {
				statusLine = console.FormatSuccessMessage(fmt.Sprintf("(via %s)", altUsed))
			} else {
				statusLine = console.FormatSuccessMessage("")
			}
		} else {
			statusLine = console.FormatErrorMessage("")
		}

		// Format secret name with padding
		nameWithPadding := lipgloss.NewStyle().Width(maxNameWidth).Render(req.Name)

		// Display the line
		fmt.Fprintf(os.Stderr, "  %s %s - %s\n", statusLine, nameWithPadding, req.WhenNeeded)
	}

	fmt.Fprintln(os.Stderr, "")
}

// splitRepoSlug splits "owner/repo" into [owner, repo]
// Uses repoutil.SplitRepoSlug internally but provides backward-compatible array return
func splitRepoSlug(slug string) [2]string {
	owner, repo, err := repoutil.SplitRepoSlug(slug)
	if err != nil {
		// Fallback behavior for invalid format
		return [2]string{slug, ""}
	}
	return [2]string{owner, repo}
}
