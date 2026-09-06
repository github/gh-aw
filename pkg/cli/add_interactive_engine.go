package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"charm.land/huh/v2"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/setutil"
	"github.com/github/gh-aw/pkg/sliceutil"
	"github.com/github/gh-aw/pkg/workflow"
)

// selectAIEngineAndKey prompts the user to select an AI engine and provide API key
func (c *AddInteractiveConfig) selectAIEngineAndKey() error {
	addInteractiveLog.Print("Starting coding agent selection")

	// First, check which secrets already exist in the repository
	if err := c.checkExistingSecrets(); err != nil {
		return err
	}

	workflowSpecifiedEngine := c.getWorkflowSpecifiedEngine()
	defaultEngine := c.determineDefaultEngine(workflowSpecifiedEngine)

	// If engine is already overridden, skip selection
	if c.EngineOverride != "" {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Using coding agent: "+c.EngineOverride))
		return c.selectEngineAuthMethod(c.EngineOverride)
	}

	// Inform user if workflow specifies an engine
	if workflowSpecifiedEngine != "" {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Workflow specifies engine: "+workflowSpecifiedEngine))
	}

	// Build engine options with notes about existing secrets and workflow specification.
	// The list of engines is derived from the catalog to ensure all registered engines appear.
	engineOptions := c.buildEngineOptions(workflowSpecifiedEngine)

	var selectedEngine string

	prioritizeEngineOption(engineOptions, defaultEngine)

	form := console.NewSelectForm(
		huh.NewSelect[string]().
			Title("Which coding agent would you like to use?").
			Description("This determines which coding agent processes your workflows").
			Options(engineOptions...).
			Value(&selectedEngine),
	)

	if err := form.RunWithContext(c.Ctx); err != nil {
		return fmt.Errorf("failed to select coding agent: %w", err)
	}

	c.EngineOverride = selectedEngine
	fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Selected engine: "+selectedEngine))

	return c.selectEngineAuthMethod(selectedEngine)
}

func (c *AddInteractiveConfig) getWorkflowSpecifiedEngine() string {
	if c.resolvedWorkflows == nil || len(c.resolvedWorkflows.Workflows) == 0 {
		return ""
	}

	for _, wf := range c.resolvedWorkflows.Workflows {
		if wf.Engine == "" {
			continue
		}
		addInteractiveLog.Printf("Workflow specifies engine in frontmatter: %s", wf.Engine)
		return wf.Engine
	}
	return ""
}

func (c *AddInteractiveConfig) determineDefaultEngine(workflowSpecifiedEngine string) string {
	defaultEngine := string(constants.DefaultEngine)
	if c.EngineOverride != "" {
		return c.EngineOverride
	}

	for _, opt := range constants.EngineOptions {
		if setutil.Contains(c.existingSecrets, opt.SecretName) {
			addInteractiveLog.Printf("Found existing secret %s, recommending engine: %s", opt.SecretName, opt.Value)
			if opt.Value != string(constants.DefaultEngine) {
				return opt.Value
			}
			// The secret maps to the default engine; fall through so that a
			// workflow-specified engine or env-var credential can still override it.
			break
		}
	}

	if workflowSpecifiedEngine != "" {
		return workflowSpecifiedEngine
	}

	for _, opt := range constants.EngineOptions {
		envVar := opt.SecretName
		if opt.EnvVarName != "" {
			envVar = opt.EnvVarName
		}
		if lookupEnv(envVar) != "" {
			addInteractiveLog.Printf("Found env var %s, recommending engine: %s", envVar, opt.Value)
			return opt.Value
		}
	}

	return defaultEngine
}

func (c *AddInteractiveConfig) buildEngineOptions(workflowSpecifiedEngine string) []huh.Option[string] {
	catalog := workflow.NewEngineCatalog(workflow.NewEngineRegistry())
	return sliceutil.Map(catalog.All(), func(def *workflow.EngineDefinition) huh.Option[string] {
		opt := constants.GetEngineOption(def.ID)
		label := fmt.Sprintf("%s - %s", def.DisplayName, def.Description)
		if opt != nil && setutil.Contains(c.existingSecrets, opt.SecretName) {
			label += " [secret exists]"
		} else {
			label += " [no secret]"
		}
		if def.ID == workflowSpecifiedEngine {
			label += " [specified in workflow]"
		}
		return huh.NewOption(label, def.ID)
	})
}

func prioritizeEngineOption(engineOptions []huh.Option[string], defaultEngine string) {
	for i, opt := range engineOptions {
		if opt.Value != defaultEngine {
			continue
		}
		if i > 0 {
			engineOptions[0], engineOptions[i] = engineOptions[i], engineOptions[0]
		}
		return
	}
}

// selectEngineAuthMethod prompts for engine-specific authentication method choices
// (for example, Copilot org billing vs. a personal access token) that only affect
// generated workflow content and have no remote repository side effects. This runs
// during engine selection, before the user has chosen between the PR and local-write
// paths. Collecting and uploading the actual secret value is deferred to
// configureEngineAPISecret, which must only run after the user commits to the PR
// path and the working directory has been confirmed clean.
func (c *AddInteractiveConfig) selectEngineAuthMethod(engine string) error {
	// If --no-secret flag is set, skip auth-method selection entirely.
	if c.SkipSecret {
		return nil
	}

	// For Copilot, ask the user whether to use copilot-requests (org billing) or a PAT.
	// Only prompt when an interactive context is available (wizard path); default to PAT otherwise.
	if engine == string(constants.CopilotEngine) && c.Ctx != nil {
		return c.selectCopilotAuthMethod()
	}

	return nil
}

// configureEngineAPISecret collects the API key for the selected engine using the unified engine secrets functions
// and uploads it to the repository. This has remote side effects and must only be called after the user has
// chosen to create a PR and the working directory has been confirmed clean.
func (c *AddInteractiveConfig) configureEngineAPISecret(engine string) error {
	addInteractiveLog.Printf("Collecting API key for engine: %s", engine)

	// If --no-secret flag is set, skip secrets configuration entirely.
	// Note: for Copilot workflows, --no-secret implies the PAT path; users who want
	// copilot-requests (org billing) should not pass --no-secret.
	if c.SkipSecret {
		opt := constants.GetEngineOption(engine)
		if opt != nil {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Skipping %s secret setup (--no-secret flag set).", opt.SecretName)))
		} else {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Skipping secret setup (--no-secret flag set)."))
		}
		return nil
	}

	// The Copilot auth-method choice (org billing vs. PAT) was already made in
	// selectEngineAuthMethod during engine selection. If the user chose org billing,
	// no secret needs to be collected or uploaded.
	if engine == string(constants.CopilotEngine) && c.UseCopilotRequests {
		return nil
	}

	// If user doesn't have write access, skip secrets configuration.
	// Users without write access cannot configure repository secrets.
	if !c.hasWriteAccess {
		opt := constants.GetEngineOption(engine)
		if opt != nil {
			fmt.Fprintln(os.Stderr, "")
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Skipping %s secret setup — write access is required to configure repository secrets.", opt.SecretName)))
			fmt.Fprintln(os.Stderr, "")
			fmt.Fprintln(os.Stderr, "Once you have write access or an admin configures the repository, set the secret with:")
			fmt.Fprintln(os.Stderr, console.FormatCommandMessage(fmt.Sprintf("  gh aw secrets set %s --repo %s", opt.SecretName, c.RepoOverride)))
		}
		return nil
	}

	// Use the unified checkAndEnsureEngineSecrets function
	config := EngineSecretConfig{
		Ctx:                  c.Ctx,
		RepoSlug:             c.RepoOverride,
		Engine:               engine,
		Verbose:              c.Verbose,
		ExistingSecrets:      c.existingSecrets,
		IncludeSystemSecrets: false, // Don't include system secrets in add-wizard
		IncludeOptional:      false,
	}

	if err := checkAndEnsureEngineSecretsForEngine(config); err != nil {
		return err
	}

	// Update existingSecrets to reflect that the secret was uploaded
	// This prevents duplicate secret uploads in createWorkflowChangesAndConfigureSecret later
	opt := constants.GetEngineOption(engine)
	if opt != nil {
		c.existingSecrets[opt.SecretName] = struct{}{}
		addInteractiveLog.Printf("Updated existingSecrets to include %s after upload", opt.SecretName)
	}

	return nil
}

// authMethodCopilotRequests is the wizard option value for Copilot org-billing authentication
// (permissions.copilot-requests: write). Extracted as a package-level constant so both the
// form definition and applyCopilotAuthMethodChoice reference the same sentinel.
const authMethodCopilotRequests = "copilot-requests"
const authMethodPAT = "pat"

// selectCopilotAuthMethod prompts the user to choose between copilot-requests (org billing)
// and a Personal Access Token for Copilot authentication.
// Sets c.UseCopilotRequests when org billing is chosen.
func (c *AddInteractiveConfig) selectCopilotAuthMethod() error {
	addInteractiveLog.Print("Prompting user for Copilot authentication method")
	// Detect org Copilot CLI billing status before building the form.
	// c.RepoOverride is in "owner/repo" format; we need just the org login.
	// When no org login is available the result is inconclusive (same as a
	// non-200 response) so the user still sees the info note.
	copilotRequestsLabel := "Use copilot-requests (org's Copilot billing, no PAT)"

	var probe orgCopilotBillingProbeResult
	if orgLogin, _, found := strings.Cut(c.RepoOverride, "/"); found && orgLogin != "" {
		probe = probeCopilotBillingForOrg(c.Ctx, orgLogin)
	} else {
		probe = orgCopilotBillingProbeResult{
			InfoNote: copilotBillingInconclusiveNote,
		}
	}
	c.copilotCLIBillingStatus = probe.BillingStatus
	copilotRequestsLabel += probe.LabelSuffix

	// Build select options.
	// When billing is confirmed enabled, copilot-requests is listed first (pre-selected).
	// When billing is disabled or inconclusive, PAT is listed first (default selection).
	// The copilot-requests option is always shown; when disabled a validation guard
	// prevents it from being submitted.
	patOpt := huh.NewOption("Use a Personal Access Token (PAT) as COPILOT_GITHUB_TOKEN", authMethodPAT)
	copilotRequestsOpt := huh.NewOption(copilotRequestsLabel, authMethodCopilotRequests)

	var options []huh.Option[string]
	switch probe.BillingStatus {
	case "enabled":
		// copilot-requests pre-selected
		options = []huh.Option[string]{copilotRequestsOpt.Selected(true), patOpt}
	default:
		// PAT is default (first) for disabled or inconclusive
		options = []huh.Option[string]{patOpt.Selected(true), copilotRequestsOpt}
	}

	var authMethod string
	selectField := huh.NewSelect[string]().
		Title("How would you like Copilot workflows to authenticate?").
		Description(copilotAuthMethodDescription(probe, c.secretSources[constants.CopilotGitHubToken])).
		Options(options...).
		Value(&authMethod)

	if probe.Disabled {
		selectField = selectField.Validate(func(v string) error {
			if v == authMethodCopilotRequests {
				return errors.New("org Copilot CLI billing is disabled — please choose PAT")
			}
			return nil
		})
	}

	form := console.NewSelectForm(selectField)

	if err := form.RunWithContext(c.Ctx); err != nil {
		return fmt.Errorf("failed to select Copilot authentication method: %w", err)
	}

	c.applyCopilotAuthMethodChoice(authMethod)
	return nil
}

func copilotAuthMethodDescription(probe orgCopilotBillingProbeResult, source secretSource) string {
	copilotRequestsDescription := "• copilot-requests: Use the org's Copilot billing seat; no PAT required."
	if probe.InfoNote != "" {
		copilotRequestsDescription += "\n  (NOTE: " + probe.InfoNote + "\n   Check with your org admin if you want to use this option.)"
	}
	patDescription := "• PAT: Create or use a COPILOT_GITHUB_TOKEN repository secret."
	if source != "" {
		patDescription = "• PAT: Reuse the existing COPILOT_GITHUB_TOKEN " + string(source) + " secret."
	}
	return patDescription + "\n" + copilotRequestsDescription
}

// applyCopilotAuthMethodChoice records the user's Copilot auth method selection and prints
// the corresponding status message. It is pure (no I/O beyond stderr) and intentionally
// separated from the huh form so the assignment logic is unit-testable without mocking the TUI.
func (c *AddInteractiveConfig) applyCopilotAuthMethodChoice(authMethod string) {
	if authMethod == authMethodCopilotRequests {
		c.UseCopilotRequests = true
		c.UseCopilotPAT = false
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Selected copilot-requests: permissions.copilot-requests: write will be added to your workflow"))
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("No COPILOT_GITHUB_TOKEN secret is required — Copilot usage is billed to your org's Copilot seat."))
	} else {
		c.UseCopilotRequests = false
		c.UseCopilotPAT = authMethod == authMethodPAT
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Selected authentication: COPILOT_GITHUB_TOKEN"))
	}
}
