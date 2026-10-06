package cli

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/repoutil"
	"github.com/github/gh-aw/pkg/workflow"
	"github.com/github/gh-aw/pkg/workflow/compilerenv"
	"github.com/goccy/go-yaml"
	"github.com/spf13/cobra"
)

var envCmdLog = logger.New("cli:env_command")

const (
	defaultsScopeRepo      = "repo"
	defaultsScopeOrg       = "org"
	defaultsScopeEnt       = "ent"
	defaultsVisibilityAll  = "all"
	defaultsVisibilityPriv = "private"
	defaultsVisibilitySel  = "selected"
)

type defaultsFile struct {
	DefaultArtifactRetentionDays      *string `yaml:"default_artifact_retention_days"`
	DefaultMaxAICredits               *string `yaml:"default_max_ai_credits"`
	DefaultMaxTurnCacheMisses         *string `yaml:"default_max_turn_cache_misses"`
	DefaultDetectionMaxAICredits      *string `yaml:"default_detection_max_ai_credits"`
	DefaultMaxDailyAICredits          *string `yaml:"default_max_daily_ai_credits"`
	DefaultMaxTurns                   *string `yaml:"default_max_turns"`
	DefaultTimeoutMinutes             *string `yaml:"default_timeout_minutes"`
	DefaultAgentJobTimeoutMinutes     *string `yaml:"default_agent_job_timeout_minutes"`
	DefaultDetectionJobTimeoutMinutes *string `yaml:"default_detection_job_timeout_minutes"`
	DefaultDetectionModel             *string `yaml:"default_detection_model"`
	DefaultOTLPEndpoint               *string `yaml:"default_otlp_endpoint"`
	DefaultUTC                        *string `yaml:"default_utc"`
	DefaultModelCopilot               *string `yaml:"default_model_copilot"`
	DefaultModelClaude                *string `yaml:"default_model_claude"`
	DefaultModelCodex                 *string `yaml:"default_model_codex"`
}

type defaultsBinding struct {
	envName   string
	fieldName string
	get       func(*defaultsFile) **string
}

type defaultsTarget struct {
	scope      string
	repoOwner  string
	repoName   string
	org        string
	enterprise string
	visibility string
}

type defaultsUpdateChange struct {
	envName string
	field   string
	value   string
	delete  bool
}

type defaultsUpdatePreview struct {
	Scope  string `console:"header:Scope"`
	Target string `console:"header:Target"`
	File   string `console:"header:File"`
	Fields int    `console:"header:Fields"`
}

type defaultsUpdateRow struct {
	Field  string `console:"header:Field"`
	Action string `console:"header:Action"`
	Value  string `console:"header:Value,omitempty"`
}

type defaultsGHError struct {
	command  string
	exitCode int
	output   string
	cause    error
}

func (e *defaultsGHError) Error() string {
	if strings.TrimSpace(e.output) == "" {
		return fmt.Sprintf("%s failed (exit %d): %v", e.command, e.exitCode, e.cause)
	}
	return fmt.Sprintf("%s failed (exit %d): %s", e.command, e.exitCode, strings.TrimSpace(e.output))
}

func (e *defaultsGHError) Unwrap() error {
	return e.cause
}

var defaultsBindings = []defaultsBinding{
	{envName: compilerenv.DefaultArtifactRetentionDays, fieldName: "default_artifact_retention_days", get: func(f *defaultsFile) **string { return &f.DefaultArtifactRetentionDays }},
	{envName: compilerenv.DefaultMaxAICredits, fieldName: "default_max_ai_credits", get: func(f *defaultsFile) **string { return &f.DefaultMaxAICredits }},
	{envName: compilerenv.DefaultMaxTurnCacheMisses, fieldName: "default_max_turn_cache_misses", get: func(f *defaultsFile) **string { return &f.DefaultMaxTurnCacheMisses }},
	{envName: compilerenv.DefaultDetectionMaxAICredits, fieldName: "default_detection_max_ai_credits", get: func(f *defaultsFile) **string { return &f.DefaultDetectionMaxAICredits }},
	{envName: compilerenv.DefaultMaxDailyAICredits, fieldName: "default_max_daily_ai_credits", get: func(f *defaultsFile) **string { return &f.DefaultMaxDailyAICredits }},
	{envName: compilerenv.DefaultMaxTurns, fieldName: "default_max_turns", get: func(f *defaultsFile) **string { return &f.DefaultMaxTurns }},
	{envName: compilerenv.DefaultTimeoutMinutes, fieldName: "default_timeout_minutes", get: func(f *defaultsFile) **string { return &f.DefaultTimeoutMinutes }},
	{envName: compilerenv.DefaultAgentJobTimeoutMinutes, fieldName: "default_agent_job_timeout_minutes", get: func(f *defaultsFile) **string { return &f.DefaultAgentJobTimeoutMinutes }},
	{envName: compilerenv.DefaultDetectionJobTimeoutMinutes, fieldName: "default_detection_job_timeout_minutes", get: func(f *defaultsFile) **string { return &f.DefaultDetectionJobTimeoutMinutes }},
	{envName: compilerenv.DefaultDetectionModel, fieldName: "default_detection_model", get: func(f *defaultsFile) **string { return &f.DefaultDetectionModel }},
	{envName: compilerenv.DefaultOTLPEndpoint, fieldName: "default_otlp_endpoint", get: func(f *defaultsFile) **string { return &f.DefaultOTLPEndpoint }},
	{envName: compilerenv.DefaultUTC, fieldName: "default_utc", get: func(f *defaultsFile) **string { return &f.DefaultUTC }},
	{envName: compilerenv.DefaultModelCopilot, fieldName: "default_model_copilot", get: func(f *defaultsFile) **string { return &f.DefaultModelCopilot }},
	{envName: compilerenv.DefaultModelClaude, fieldName: "default_model_claude", get: func(f *defaultsFile) **string { return &f.DefaultModelClaude }},
	{envName: compilerenv.DefaultModelCodex, fieldName: "default_model_codex", get: func(f *defaultsFile) **string { return &f.DefaultModelCodex }},
}

var defaultsExecGH = workflow.ExecGH
var defaultsGetCurrentRepoSlug = GetCurrentRepoSlug

func NewEnvCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "env",
		Short: "Manage compiler defaults as GitHub Actions variables",
		Long: `Manage compiler default variables in bulk for a repository, organization, or enterprise scope.

The YAML file is flat and uses default_-prefixed lowercase keys (e.g., default_max_turns).
Set a field to null (or omit it) in update mode to delete the variable from the selected scope.
Any field with a non-null string value will be set or updated.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(newDefaultsGetCommand())
	cmd.AddCommand(newDefaultsUpdateCommand())
	cmd.AddCommand(newLegacyGHGuardSubcommand())
	return cmd
}

func newDefaultsGetCommand() *cobra.Command {
	var scope, repo, org, enterprise string

	cmd := &cobra.Command{
		Use:   "get [file]",
		Short: "Download default compiler variables into a YAML file",
		Long: `Download compiler defaults into a YAML file.

When [file] is omitted, the command writes to file.yml in the current directory.

Scope resolution:
- --scope defaults to repo.
- repo scope uses --repo owner/repo, or the current repository when --repo is omitted.
- org scope uses --org when provided; otherwise it infers the organization from --repo (or the current repository).
- ent scope requires --enterprise <slug>.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			outputFile := "file.yml"
			if len(args) == 1 {
				outputFile = args[0]
			}
			target, err := resolveDefaultsTarget(scope, repo, org, enterprise, "", false)
			if err != nil {
				return err
			}
			return defaultsGetToFile(target, outputFile)
		},
	}

	cmd.Flags().StringVar(&scope, "scope", "", "Variable scope (repo|org|ent). Defaults to repo")
	cmd.Flags().StringVarP(&repo, "repo", "r", "", "Target repository (owner/repo format only; GHES host prefixes are not supported). Defaults to current repository")
	cmd.Flags().StringVar(&org, "org", "", "Target organization (required for --scope org unless inferable from --repo/current repo)")
	cmd.Flags().StringVar(&enterprise, "enterprise", "", "Target enterprise slug (required for --scope ent)")
	return cmd
}

func newDefaultsUpdateCommand() *cobra.Command {
	var scope, repo, org, enterprise, visibility string
	var yes, dryRun bool

	cmd := &cobra.Command{
		Use:   "update [file]",
		Short: "Upload default compiler variables from a YAML file",
		Long: `Upload compiler defaults from a YAML file.

When [file] is omitted, the command reads from file.yml in the current directory.

Scope and flag behavior:
- --scope is required (repo|org|ent).
- repo scope uses --repo owner/repo, or the current repository when --repo is omitted.
- org scope uses --org when provided; otherwise it infers the organization from --repo (or the current repository).
- ent scope requires --enterprise <slug>.
- --visibility controls access when creating org or ent variables (all|private|selected). Existing variables keep their visibility.
- --dry-run previews planned changes and exits without applying updates.
- --yes skips the confirmation prompt for real updates; it has no effect with --dry-run.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			inputFile := "file.yml"
			if len(args) == 1 {
				inputFile = args[0]
			}
			if err := validateDefaultsVisibility(scope, visibility, cmd.Flags().Changed("visibility")); err != nil {
				return errors.New(console.FormatErrorMessage(err.Error()))
			}
			target, err := resolveDefaultsTarget(scope, repo, org, enterprise, visibility, true)
			if err != nil {
				return err
			}
			return defaultsUpdateFromFile(target, inputFile, yes, dryRun)
		},
	}

	cmd.Flags().StringVar(&scope, "scope", "", "Variable scope (repo|org|ent)")
	cmd.Flags().StringVarP(&repo, "repo", "r", "", "Target repository (owner/repo format only; GHES host prefixes are not supported). Defaults to current repository")
	cmd.Flags().StringVar(&org, "org", "", "Target organization (required for --scope org unless inferable from --repo/current repo)")
	cmd.Flags().StringVar(&enterprise, "enterprise", "", "Target enterprise slug (required for --scope ent)")
	cmd.Flags().StringVar(&visibility, "visibility", defaultsVisibilityAll, "Visibility for newly created org or ent variables (all|private|selected)")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompt")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview updates without applying any changes")
	_ = cmd.MarkFlagRequired("scope")
	return cmd
}

func defaultsGetToFile(target defaultsTarget, outputFile string) error {
	envCmdLog.Printf("Getting defaults to file: scope=%s, output=%s", target.scope, outputFile)
	var file defaultsFile

	for _, binding := range defaultsBindings {
		value, err := fetchDefaultsVariable(target, binding.envName)
		if err != nil {
			return err
		}
		if value != "" {
			v := value
			*binding.get(&file) = &v
		}
		// nil (variable not set) serializes as null in YAML
	}

	data, err := yaml.Marshal(&file)
	if err != nil {
		return fmt.Errorf("failed to serialize defaults YAML: %w", err)
	}
	if err := os.WriteFile(outputFile, data, constants.FilePermPublic); err != nil {
		return fmt.Errorf("failed to write defaults file %q: %w", outputFile, err)
	}
	fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Saved defaults to "+outputFile))
	return nil
}

func defaultsUpdateFromFile(target defaultsTarget, inputFile string, skipConfirmation, dryRun bool) error {
	envCmdLog.Printf("Updating defaults from file: scope=%s, input=%s, dryRun=%t", target.scope, inputFile, dryRun)
	data, err := os.ReadFile(inputFile)
	if err != nil {
		return fmt.Errorf("failed to read defaults file %q: %w", inputFile, err)
	}

	file, err := defaultsParseFile(inputFile, data)
	if err != nil {
		return err
	}
	if err := defaultsValidateFile(&file); err != nil {
		return err
	}

	changes := defaultsBuildUpdateChanges(&file)
	envCmdLog.Printf("Built %d default variable changes", len(changes))
	if dryRun {
		renderDefaultsUpdatePreview(target, inputFile, changes)
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Dry-run mode enabled; no variables were changed."))
		return nil
	}
	if err := confirmDefaultsUpdate(target, inputFile, changes, skipConfirmation, console.ConfirmAction); err != nil {
		return err
	}

	for _, change := range changes {
		if change.delete {
			if err := deleteDefaultsVariable(target, change.envName); err != nil {
				return err
			}
			continue
		}
		if err := upsertDefaultsVariable(target, change.envName, change.value); err != nil {
			return err
		}
	}

	fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Updated defaults from "+inputFile))
	return nil
}

func defaultsParseFile(inputFile string, data []byte) (defaultsFile, error) {
	var file defaultsFile
	if err := yaml.UnmarshalWithOptions(data, &file, yaml.DisallowUnknownField()); err != nil {
		return defaultsFile{}, fmt.Errorf("failed to parse defaults file %q: %w", inputFile, err)
	}
	return file, nil
}

func defaultsValidateFile(file *defaultsFile) error {
	var validationErrors []string

	if err := validateDefaultArtifactRetentionDays(file.DefaultArtifactRetentionDays); err != nil {
		validationErrors = append(validationErrors, err.Error())
	}
	validateNonZeroInt := func(field string, value *string) {
		if value == nil {
			return
		}
		trimmed := strings.TrimSpace(*value)
		parsed, err := strconv.ParseInt(trimmed, 10, 64)
		if err != nil || parsed == 0 {
			validationErrors = append(validationErrors, field+" must be a non-zero integer when set")
		}
	}
	validatePositiveInt := func(field string, value *string) {
		if value == nil {
			return
		}
		trimmed := strings.TrimSpace(*value)
		parsed, err := strconv.ParseInt(trimmed, 10, 64)
		if err != nil || parsed <= 0 {
			validationErrors = append(validationErrors, field+" must be a positive integer when set")
		}
	}
	validateNonEmpty := func(field string, value *string) {
		if value == nil {
			return
		}
		if strings.TrimSpace(*value) == "" {
			validationErrors = append(validationErrors, field+" cannot be empty when set")
		}
	}
	validateUTCOffset := func(field string, value *string) {
		if value == nil {
			return
		}
		if _, err := workflow.NormalizeUTCOffset(*value); err != nil {
			validationErrors = append(validationErrors, field+" "+err.Error())
		}
	}

	validateNonZeroInt("default_max_ai_credits", file.DefaultMaxAICredits)
	validatePositiveInt("default_max_turn_cache_misses", file.DefaultMaxTurnCacheMisses)
	validateNonZeroInt("default_detection_max_ai_credits", file.DefaultDetectionMaxAICredits)
	validateNonZeroInt("default_max_daily_ai_credits", file.DefaultMaxDailyAICredits)
	validatePositiveInt("default_max_turns", file.DefaultMaxTurns)
	validatePositiveInt("default_timeout_minutes", file.DefaultTimeoutMinutes)
	validatePositiveInt("default_agent_job_timeout_minutes", file.DefaultAgentJobTimeoutMinutes)
	validatePositiveInt("default_detection_job_timeout_minutes", file.DefaultDetectionJobTimeoutMinutes)
	validateNonEmpty("default_detection_model", file.DefaultDetectionModel)
	validateUTCOffset("default_utc", file.DefaultUTC)
	validateNonEmpty("default_model_copilot", file.DefaultModelCopilot)
	validateNonEmpty("default_model_claude", file.DefaultModelClaude)
	validateNonEmpty("default_model_codex", file.DefaultModelCodex)

	if len(validationErrors) > 0 {
		return fmt.Errorf("invalid defaults file: %s", strings.Join(validationErrors, "; "))
	}
	return nil
}

func validateDefaultArtifactRetentionDays(value *string) error {
	if value == nil {
		return nil
	}
	days, err := strconv.Atoi(strings.TrimSpace(*value))
	if err != nil || days < 1 || days > 400 {
		return errors.New("default_artifact_retention_days must be an integer between 1 and 400 when set, for example: \"7\"")
	}
	return nil
}

func defaultsBuildUpdateChanges(file *defaultsFile) []defaultsUpdateChange {
	changes := make([]defaultsUpdateChange, 0, len(defaultsBindings))
	for _, binding := range defaultsBindings {
		ptr := *binding.get(file)
		if ptr == nil {
			changes = append(changes, defaultsUpdateChange{
				envName: binding.envName,
				field:   binding.fieldName,
				delete:  true,
			})
		} else {
			changes = append(changes, defaultsUpdateChange{
				envName: binding.envName,
				field:   binding.fieldName,
				value:   *ptr,
				delete:  false,
			})
		}
	}
	return changes
}

func confirmDefaultsUpdate(
	target defaultsTarget,
	inputFile string,
	changes []defaultsUpdateChange,
	skipConfirmation bool,
	confirmAction func(title, affirmative, negative string) (bool, error),
) error {
	renderDefaultsUpdatePreview(target, inputFile, changes)

	if skipConfirmation {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Skipping confirmation because --yes was provided."))
		return nil
	}

	confirmed, err := confirmAction(
		"Do you want to update these defaults?",
		"Yes, update",
		"No, cancel",
	)
	if err != nil {
		return fmt.Errorf("failed to get confirmation: %w", err)
	}
	if !confirmed {
		return errors.New("defaults update cancelled")
	}
	return nil
}

func renderDefaultsUpdatePreview(target defaultsTarget, inputFile string, changes []defaultsUpdateChange) {
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Defaults update preview:"))
	fmt.Fprint(os.Stderr, console.RenderStruct(defaultsUpdatePreview{
		Scope:  target.scope,
		Target: target.displayName(),
		File:   inputFile,
		Fields: len(changes),
	}))
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr)
	fmt.Fprint(os.Stderr, console.RenderStruct(defaultsUpdateRows(changes)))
	fmt.Fprintln(os.Stderr)
}

func defaultsUpdateRows(changes []defaultsUpdateChange) []defaultsUpdateRow {
	rows := make([]defaultsUpdateRow, 0, len(changes))
	for _, change := range changes {
		action := "set"
		if change.delete {
			action = "delete"
		}
		rows = append(rows, defaultsUpdateRow{
			Field:  change.field,
			Action: action,
			Value:  change.value,
		})
	}
	return rows
}

func validateDefaultsVisibility(scope, visibility string, visibilitySet bool) error {
	switch visibility {
	case defaultsVisibilityAll, defaultsVisibilityPriv, defaultsVisibilitySel:
	default:
		return fmt.Errorf("invalid --visibility value %q. Expected one of all, private, selected. Example: gh aw env update defaults.yml --scope org --org my-org --visibility all", visibility)
	}
	if strings.TrimSpace(scope) == defaultsScopeRepo && visibilitySet {
		return errors.New("--visibility is not valid for repo scope. Expected --scope org or --scope ent. Example: gh aw env update defaults.yml --scope org --org my-org --visibility all")
	}
	return nil
}

func resolveDefaultsTarget(scope, repo, org, enterprise, visibility string, scopeRequired bool) (defaultsTarget, error) {
	normalizedScope := strings.TrimSpace(scope)
	if normalizedScope == "" {
		if scopeRequired {
			return defaultsTarget{}, errors.New("scope is required; use --scope repo|org|ent")
		}
		normalizedScope = defaultsScopeRepo
	}

	switch normalizedScope {
	case defaultsScopeRepo:
		repoSlug := strings.TrimSpace(repo)
		if repoSlug == "" {
			var err error
			repoSlug, err = defaultsGetCurrentRepoSlug()
			if err != nil {
				return defaultsTarget{}, fmt.Errorf("failed to detect current repository: %w", err)
			}
		}
		owner, name, err := repoutil.SplitRepoSlug(repoSlug)
		if err != nil {
			return defaultsTarget{}, fmt.Errorf("invalid repository slug %q: %w", repoSlug, err)
		}
		return defaultsTarget{scope: defaultsScopeRepo, repoOwner: owner, repoName: name}, nil
	case defaultsScopeOrg:
		targetOrg := strings.TrimSpace(org)
		if targetOrg == "" {
			repoSlug := strings.TrimSpace(repo)
			if repoSlug == "" {
				var err error
				repoSlug, err = defaultsGetCurrentRepoSlug()
				if err != nil {
					return defaultsTarget{}, fmt.Errorf("failed to detect current repository: %w", err)
				}
			}
			owner, _, err := repoutil.SplitRepoSlug(repoSlug)
			if err != nil {
				return defaultsTarget{}, fmt.Errorf("invalid repository slug %q: %w", repoSlug, err)
			}
			targetOrg = owner
		}
		return defaultsTarget{scope: defaultsScopeOrg, org: targetOrg, visibility: visibility}, nil
	case defaultsScopeEnt:
		targetEnt := strings.TrimSpace(enterprise)
		if targetEnt == "" {
			return defaultsTarget{}, errors.New("enterprise scope requires --enterprise <slug>")
		}
		return defaultsTarget{scope: defaultsScopeEnt, enterprise: targetEnt, visibility: visibility}, nil
	default:
		return defaultsTarget{}, fmt.Errorf("invalid scope %q; expected repo, org, or ent", scope)
	}
}

func (t defaultsTarget) variablesEndpoint() string {
	switch t.scope {
	case defaultsScopeRepo:
		return fmt.Sprintf("repos/%s/%s/actions/variables", t.repoOwner, t.repoName)
	case defaultsScopeOrg:
		return fmt.Sprintf("orgs/%s/actions/variables", t.org)
	default:
		return fmt.Sprintf("enterprises/%s/actions/variables", t.enterprise)
	}
}

func (t defaultsTarget) variableEndpoint(name string) string {
	return fmt.Sprintf("%s/%s", t.variablesEndpoint(), url.PathEscape(name))
}

func (t defaultsTarget) displayName() string {
	switch t.scope {
	case defaultsScopeRepo:
		return t.repoOwner + "/" + t.repoName //nolint:manualpathconcat // repository slug for display, not a filesystem path
	case defaultsScopeOrg:
		return t.org
	default:
		return t.enterprise
	}
}

func runDefaultsGH(args ...string) ([]byte, error) {
	envCmdLog.Printf("Running gh command: %v", args)
	cmd := defaultsExecGH(args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		command := "gh " + strings.Join(args, " ")
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return out, &defaultsGHError{
				command:  command,
				exitCode: exitErr.ExitCode(),
				output:   string(out),
				cause:    err,
			}
		}
		return out, fmt.Errorf("%s: %w", command, err)
	}
	return out, nil
}

func fetchDefaultsVariable(target defaultsTarget, name string) (string, error) {
	out, err := runDefaultsGH("api", target.variableEndpoint(name), "--jq", ".value")
	if err != nil {
		if isDefaultsNotFoundError(err, out) {
			return "", nil
		}
		return "", fmt.Errorf("failed to fetch %s: %w", name, err)
	}
	return strings.TrimRight(string(out), "\r\n"), nil
}

func upsertDefaultsVariable(target defaultsTarget, name, value string) error {
	patchOut, patchErr := runDefaultsGH("api", "-X", "PATCH", target.variableEndpoint(name), "-f", "name="+name, "-f", "value="+value)
	if patchErr == nil {
		return nil
	}
	if !isDefaultsNotFoundError(patchErr, patchOut) {
		return fmt.Errorf("failed to update %s: %w", name, errWithOutput(patchErr, patchOut))
	}

	envCmdLog.Printf("Variable %s not found via PATCH, creating via POST", name)
	args := []string{"api", "-X", "POST", target.variablesEndpoint(), "-f", "name=" + name, "-f", "value=" + value}
	if target.scope == defaultsScopeOrg || target.scope == defaultsScopeEnt {
		args = append(args, "-f", "visibility="+target.visibility)
	}
	out, err := runDefaultsGH(args...)
	if err != nil {
		if (target.scope == defaultsScopeOrg || target.scope == defaultsScopeEnt) && isDefaultsMissingVisibilityError(err, out) {
			scopeName := "organization"
			example := "gh aw env update defaults.yml --scope org --org my-org --visibility all"
			if target.scope == defaultsScopeEnt {
				scopeName = "enterprise"
				example = "gh aw env update defaults.yml --scope ent --enterprise my-ent --visibility all"
			}
			message := fmt.Sprintf("failed to create %s at %s scope: %s variables require a visibility. Expected one of all, private, selected. Example: %s", name, target.scope, scopeName, example)
			return fmt.Errorf("%s: %w", message, errWithOutput(err, out))
		}
		return fmt.Errorf("failed to create %s at %s scope: %w", name, target.scope, errWithOutput(err, out))
	}
	return nil
}

func deleteDefaultsVariable(target defaultsTarget, name string) error {
	out, err := runDefaultsGH("api", "-X", "DELETE", target.variableEndpoint(name))
	if err != nil && !isDefaultsNotFoundError(err, out) {
		return fmt.Errorf("failed to delete %s: %w", name, errWithOutput(err, out))
	}
	return nil
}

func isDefaultsNotFoundError(err error, out []byte) bool {
	if err == nil {
		return false
	}
	var ghErr *defaultsGHError
	if errors.As(err, &ghErr) {
		return strings.Contains(strings.ToLower(ghErr.output), "http 404")
	}
	return strings.Contains(strings.ToLower(string(out)), "http 404")
}

func isDefaultsMissingVisibilityError(err error, out []byte) bool {
	if err == nil {
		return false
	}
	response := strings.ToLower(string(out))
	return strings.Contains(response, "http 422") &&
		(strings.Contains(response, "missing visibility") ||
			strings.Contains(response, `"visibility" is missing`) ||
			(strings.Contains(response, "missing required") && strings.Contains(response, "visibility")))
}

func errWithOutput(err error, out []byte) error {
	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" {
		return err
	}
	return fmt.Errorf("%w: %s", err, trimmed)
}
