// Package workflow provides the repo-level configuration loader for aw.json.
//
// This file loads and validates .github/workflows/aw.json, which provides
// repository-level settings for agentic workflows such as customising the
// agentics-maintenance runner.
//
// Configuration reference:
//
//		{
//		  "ghes": true,               // enables GHES-compatible v3 artifact pins
//		  "help_command": false,      // disables builtin centralized /help comment handler
//		  "utc": "-08:00", // project home UTC offset for rendered local times
//		  "auto_upgrade": true, // set to true to generate agentic-auto-upgrade.yml with weekly schedule
//		  "auto_upgrade": { "cron": "0 9 * * 1", "options": ["--pre-releases"] }, // or object form: configure schedule and upgrade options
//		  "action_pins": {            // redirect action references to internal mirrors
//		    "actions/checkout@v4": "acme-corp/checkout@v4"
//		  },
//		  "container_pins": {         // redirect container images to internal mirrors
//	   "ghcr.io/owner/image:tag": {
//	     "image": "registry.acme.com/image:tag",
//	     "digest": "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
//	   }
//	 },
//		  "maintenance": {              // enables generation of agentics-maintenance.yml
//		    "runs_on": "custom runner", // string or string[] – runner label(s) for all
//		    "action_failure_issue_expires": 72, // expiration (hours) for conclusion failure issues
//		    "label_triggers": true, // set to true to enable all label-triggered jobs (opt-in)
//		    "disabled_jobs": ["close-expired-entities"], // optional maintenance jobs to omit
//		    "compile": {
//		      "create_pull_request_github_token": "MY_REPO_TOKEN" // create/update a deduplicated PR instead of an issue
//		    }
//		  }                            // maintenance jobs (default: ubuntu-slim)
//		}
//
//		{
//		  "maintenance": false          // disables agentic maintenance entirely
//		}
package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
)

var repoConfigLog = logger.New("workflow:repo_config")
var repoConfigSecretNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// RepoConfigFileName is the path of the repository-level configuration file
// relative to the git root.
const RepoConfigFileName = ".github/workflows/aw.json"

// DefaultActionFailureIssueExpiresHours is the default expiration (in hours)
// for action failure issues created by the conclusion job.
const DefaultActionFailureIssueExpiresHours = 24 * 7

// MaintenanceConfig holds maintenance-workflow-specific settings from aw.json.
type MaintenanceCompileConfig struct {
	// CreatePullRequestGitHubToken is the secret name used by the compile-workflows
	// maintenance job for GitHub API calls and branch pushes. When configured,
	// out-of-sync compiled workflows are reported via a deduplicated pull request
	// instead of an issue.
	CreatePullRequestGitHubToken string `json:"create_pull_request_github_token,omitempty"`
}

type MaintenanceConfig struct {
	// RunsOn is the runner label or labels used for all jobs in agentics-maintenance.yml.
	RunsOn RunsOnValue `json:"runs_on,omitempty"`

	// ActionFailureIssueExpires configures expiration (in hours) for action
	// failure issues opened by the conclusion job. Defaults to 168 (7 days).
	ActionFailureIssueExpires int `json:"action_failure_issue_expires,omitempty"`

	// ActionFailureIssueExpiresExplicit records whether action_failure_issue_expires
	// was explicitly present in aw.json, as opposed to falling back to the
	// implicit 168-hour default. This distinction matters because the implicit
	// default must not, by itself, force generation of agentics-maintenance.yml
	// (see scanWorkflowsForExpires); only an explicit opt-in does. Populated by
	// the JSON loader below, not by json.Unmarshal (the field is unexported from
	// the schema on purpose).
	ActionFailureIssueExpiresExplicit bool `json:"-"`

	// LabelTriggers controls all label-triggered jobs (disable_agentic_workflow,
	// label_apply_safe_outputs, etc.).
	// The value is treated as an opt-in flag: only true enables the jobs.
	// nil (omitted) or false both disable label-triggered jobs.
	// To opt in, set label_triggers: true in aw.json.
	LabelTriggers *bool `json:"label_triggers,omitempty"`

	// DisabledJobs lists maintenance job IDs that should be omitted from generated
	// agentics-maintenance workflows.
	DisabledJobs []string `json:"disabled_jobs,omitempty"`

	// Compile controls compile-workflows maintenance job behavior.
	Compile *MaintenanceCompileConfig `json:"compile,omitempty"`
}

var validDisabledMaintenanceJobs = map[string]string{
	normalizeMaintenanceJobName("close-expired-entities"):         "close-expired-entities",
	normalizeMaintenanceJobName("apply_safe_outputs"):             "apply_safe_outputs",
	normalizeMaintenanceJobName("label_disable_agentic_workflow"): "label_disable_agentic_workflow",
	normalizeMaintenanceJobName("label_apply_safe_outputs"):       "label_apply_safe_outputs",
}

// IsLabelTriggerEnabled returns true only when label_triggers is explicitly set to true.
// The default (nil / omitted) is treated as disabled (false) — opt-in semantics.
func (m *MaintenanceConfig) IsLabelTriggerEnabled() bool {
	if m == nil || m.LabelTriggers == nil {
		return false
	}
	return *m.LabelTriggers
}

// normalizeMaintenanceJobName normalizes a maintenance job name for
// case/whitespace-insensitive comparison, converting underscores to hyphens.
func normalizeMaintenanceJobName(name string) string {
	normalized := strings.ToLower(strings.TrimSpace(name))
	return strings.ReplaceAll(normalized, "_", "-")
}

// IsJobDisabled reports whether the provided maintenance job ID is explicitly
// disabled in aw.json.
func (m *MaintenanceConfig) IsJobDisabled(jobName string) bool {
	if m == nil || len(m.DisabledJobs) == 0 {
		return false
	}
	normalizedJobName := normalizeMaintenanceJobName(jobName)
	for _, disabledJob := range m.DisabledJobs {
		if normalizeMaintenanceJobName(disabledJob) == normalizedJobName {
			return true
		}
	}
	return false
}

// RepoConfig is the parsed representation of aw.json.
type RepoConfig struct {
	// Strict enforces strict mode for every workflow compiled in the repository.
	// The schema only permits true when this field is present.
	Strict bool

	// GHES enables GitHub Enterprise Server compatibility mode.
	// When true, the compiler uses artifact action versions supported by GHES.
	GHES bool

	// UTC is the project's home UTC offset used for rendering local times in CLI output.
	// The value must be a numeric UTC offset such as "+00:00" or "-08:00".
	UTC string

	// HelpCommand controls builtin centralized /help command behavior.
	// When nil or true, the builtin help command is enabled.
	// Set to false in aw.json to disable it.
	HelpCommand *bool

	// AutoUpgrade enables generation of agentic-auto-upgrade.yml when true.
	// The workflow runs on a fuzzy weekly schedule and runs the upgrade operation
	// to check for and report available workflow upgrades.
	// Opt-in: nil (omitted) or false both disable generation.
	AutoUpgrade *bool

	// AutoUpgradeCron is an optional custom cron expression for the
	// agentic-auto-upgrade workflow schedule. When non-empty, it overrides
	// the default fuzzy weekly schedule. Requires AutoUpgrade to be true.
	AutoUpgradeCron string

	// AutoUpgradeOptions contains supported command-line options passed to
	// gh aw upgrade by the agentic-auto-upgrade workflow.
	AutoUpgradeOptions []string

	// MaintenanceDisabled is true when maintenance has been explicitly set to false
	// in aw.json, disabling agentic-maintenance generation and any features that
	// depend on it (such as expires).
	MaintenanceDisabled bool

	// Maintenance holds maintenance-specific settings when maintenance is enabled
	// and an object was provided (nil when maintenance is not configured or is
	// disabled).
	Maintenance *MaintenanceConfig

	// ActionPins maps action repository@version references to replacement
	// repository@version references. Enterprises running in a private cloud
	// can use this to redirect actions to internal mirrors. Keys and values
	// must use the format "owner/repo@ref".
	ActionPins map[string]string

	// ContainerPins maps container image references to replacement image
	// targets. Enterprises running in a private cloud can use this to
	// redirect container images to internal mirrors. Keys are source image
	// references (e.g. "ghcr.io/owner/image:tag") and values are objects
	// with separate image ref and SHA-256 digest fields so that each
	// component can be validated independently.
	ContainerPins map[string]ContainerPinTarget
}

// ContainerPinTarget holds the replacement image reference for a container_pins
// entry. The image ref and digest are stored separately for independent
// validation. The combined pinned reference is "Image@Digest".
type ContainerPinTarget struct {
	// Image is the replacement container image reference without a digest
	// component (e.g. "registry.acme.com/image:tag").
	Image string `json:"image"`
	// Digest is the SHA-256 digest of the replacement image
	// (e.g. "sha256:<64 lowercase hex characters>").
	Digest string `json:"digest"`
}

// IsAutoUpgradeEnabled returns true only when auto_upgrade is explicitly set to true.
// The default (nil / omitted) is treated as disabled (false) — opt-in semantics.
func (r *RepoConfig) IsAutoUpgradeEnabled() bool {
	if r == nil || r.AutoUpgrade == nil {
		return false
	}
	return *r.AutoUpgrade
}

// UnmarshalJSON implements json.Unmarshaler to handle the polymorphic maintenance
// and auto_upgrade fields. maintenance can be either the boolean false (disable)
// or a configuration object; auto_upgrade can be a boolean or an object with an
// optional cron field.
func (r *RepoConfig) UnmarshalJSON(data []byte) error { //nolint:largefunc // Polymorphic repository fields are decoded together.
	// Use an intermediate struct with json.RawMessage to defer maintenance and
	// auto_upgrade parsing.
	var raw struct {
		Strict        bool                          `json:"strict,omitempty"`
		GHES          bool                          `json:"ghes,omitempty"`
		HelpCommand   *bool                         `json:"help_command,omitempty"` // nil = use default (enabled)
		UTC           string                        `json:"utc,omitempty"`
		AutoUpgrade   json.RawMessage               `json:"auto_upgrade,omitempty"`
		Maintenance   json.RawMessage               `json:"maintenance,omitempty"`
		ActionPins    map[string]string             `json:"action_pins,omitempty"`
		ContainerPins map[string]ContainerPinTarget `json:"container_pins,omitempty"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	r.Strict = raw.Strict
	r.GHES = raw.GHES
	r.HelpCommand = raw.HelpCommand
	r.UTC = strings.TrimSpace(raw.UTC)
	r.ActionPins = raw.ActionPins
	r.ContainerPins = raw.ContainerPins

	// Parse polymorphic auto_upgrade: boolean or { "cron": "..." } object.
	if len(raw.AutoUpgrade) > 0 && string(raw.AutoUpgrade) != "null" {
		var b bool
		if err := json.Unmarshal(raw.AutoUpgrade, &b); err == nil {
			r.AutoUpgrade = &b
		} else {
			// Object form: { "cron": "...", "options": [...] } — implies enabled.
			var autoUpgradeObj struct {
				Cron    string   `json:"cron,omitempty"`
				Options []string `json:"options,omitempty"`
			}
			if err := json.Unmarshal(raw.AutoUpgrade, &autoUpgradeObj); err != nil {
				return fmt.Errorf("auto_upgrade configuration is not recognized: %w. Expected a boolean or an object with 'cron' and 'options' fields, for example: {\"cron\": \"0 9 * * 1\", \"options\": [\"--pre-releases\"]}", err)
			}
			enabled := true
			r.AutoUpgrade = &enabled
			r.AutoUpgradeCron = strings.TrimSpace(autoUpgradeObj.Cron)
			r.AutoUpgradeOptions = autoUpgradeObj.Options
		}
	}

	if len(raw.Maintenance) == 0 || string(raw.Maintenance) == "null" {
		return nil
	}

	// Try boolean first: maintenance: false disables the feature.
	var b bool
	if err := json.Unmarshal(raw.Maintenance, &b); err == nil {
		repoConfigLog.Printf("Maintenance field parsed as boolean: disabled=%v", !b)
		r.MaintenanceDisabled = !b
		return nil
	}

	// Otherwise deserialise as an object with JSON annotations.
	var mc MaintenanceConfig
	if err := json.Unmarshal(raw.Maintenance, &mc); err != nil {
		return fmt.Errorf("maintenance configuration is not recognized: %w. Expected a boolean or a maintenance object, for example: {\"disabled_jobs\": []}", err)
	}
	// Detect whether action_failure_issue_expires was explicitly present in the
	// source JSON, distinct from falling back to the implicit 168-hour default.
	var mcPresence map[string]json.RawMessage
	if err := json.Unmarshal(raw.Maintenance, &mcPresence); err == nil {
		if _, ok := mcPresence["action_failure_issue_expires"]; ok {
			mc.ActionFailureIssueExpiresExplicit = true
		}
	}
	repoConfigLog.Printf("Maintenance field parsed as object: runsOn=%v, issueExpires=%d, issueExpiresExplicit=%v", mc.RunsOn, mc.ActionFailureIssueExpires, mc.ActionFailureIssueExpiresExplicit)
	r.Maintenance = &mc
	return nil
}

// IsHelpCommandEnabled returns true when the builtin centralized /help command
// handler should be enabled. The default is enabled.
func (r *RepoConfig) IsHelpCommandEnabled() bool {
	if r == nil || r.HelpCommand == nil {
		return true
	}
	return *r.HelpCommand
}

// LoadRepoConfig loads and validates .github/workflows/aw.json from the
// provided git root directory.  The function returns a non-nil *RepoConfig
// with default values when the file does not exist (the file is optional).
// An error is returned only when the file exists but cannot be read or fails
// schema validation.
func LoadRepoConfig(gitRoot string) (*RepoConfig, error) {
	configPath := filepath.Join(gitRoot, RepoConfigFileName)
	repoConfigLog.Printf("Loading repo config from %s", configPath)

	data, err := os.ReadFile(filepath.Clean(configPath))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			repoConfigLog.Print("Repo config file not found, using defaults")
			return &RepoConfig{}, nil
		}
		return nil, fmt.Errorf("could not read %s: %w. Check that the file exists and is readable", RepoConfigFileName, err)
	}

	// Validate against the embedded JSON schema before deserialising.
	if err := validateRepoConfigJSON(data, configPath); err != nil {
		return nil, err
	}

	// Deserialise into typed structs via JSON annotations.
	var cfg RepoConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("could not parse %s as JSON: %w. Check the file for syntax errors such as trailing commas or unquoted keys", RepoConfigFileName, err)
	}
	if err := validateRepoConfigValues(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// validateRepoConfigJSON validates raw JSON bytes against the repo config schema.
func validateRepoConfigJSON(data []byte, filePath string) error {
	repoConfigLog.Printf("Validating repo config JSON schema: %s (%d bytes)", filePath, len(data))
	schema, err := parser.GetCompiledRepoConfigSchema()
	if err != nil {
		return fmt.Errorf("could not compile the repo config schema: %w. This indicates a bug in gh-aw; please report it", err)
	}

	var doc any
	if err := json.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("could not parse %s as JSON: %w. Check the file for syntax errors such as trailing commas or unquoted keys", filePath, err)
	}

	if err := schema.Validate(doc); err != nil {
		repoConfigLog.Printf("Repo config schema validation failed: %v", err)
		return fmt.Errorf("%s does not match the expected schema: %w. See the repo config documentation for the required fields", RepoConfigFileName, err)
	}

	repoConfigLog.Print("Repo config JSON schema validation passed")
	return nil
}

func validateRepoConfigValues(cfg *RepoConfig) error {
	if cfg == nil {
		return nil
	}
	if cfg.UTC != "" {
		normalized, err := NormalizeUTCOffset(cfg.UTC)
		if err != nil {
			return fmt.Errorf("%s has an unsupported utc value: %w. Expected a UTC offset like \"+02:00\" or \"-05:00\"", RepoConfigFileName, err)
		}
		cfg.UTC = normalized
	}
	if cfg.AutoUpgradeCron != "" {
		if err := validateCronExpression(cfg.AutoUpgradeCron); err != nil {
			return fmt.Errorf("%s has an unsupported auto_upgrade.cron value: %w. Expected a 5-field cron expression, for example: \"0 9 * * 1\"", RepoConfigFileName, err)
		}
	}
	if cfg.Maintenance != nil {
		seenDisabledJobs := map[string]string{}
		for _, jobName := range cfg.Maintenance.DisabledJobs {
			normalizedJobName := normalizeMaintenanceJobName(jobName)
			if normalizedJobName == "" {
				return fmt.Errorf("%s has a blank entry in maintenance.disabled_jobs. Expected a non-empty job name, for example: \"stale-issue-cleanup\"", RepoConfigFileName)
			}
			if _, ok := validDisabledMaintenanceJobs[normalizedJobName]; !ok {
				return fmt.Errorf("%s references unrecognized maintenance.disabled_jobs entry %q. Valid values are: close-expired-entities, apply_safe_outputs, label_disable_agentic_workflow, label_apply_safe_outputs. Example:\nmaintenance:\n  disabled_jobs:\n    - close-expired-entities", RepoConfigFileName, jobName)
			}
			if previous, exists := seenDisabledJobs[normalizedJobName]; exists {
				return fmt.Errorf("%s has duplicate maintenance.disabled_jobs entries %q and %q after normalization. Expected each job to be listed once. Example:\nmaintenance:\n  disabled_jobs:\n    - close-expired-entities", RepoConfigFileName, previous, jobName)
			}
			seenDisabledJobs[normalizedJobName] = jobName
		}
	}

	if cfg.Maintenance == nil || cfg.Maintenance.Compile == nil {
		return nil
	}
	compileCfg := cfg.Maintenance.Compile
	secretName := compileCfg.CreatePullRequestGitHubToken
	if secretName != "" && !repoConfigSecretNamePattern.MatchString(secretName) {
		return fmt.Errorf("%s has an unsupported maintenance.compile.create_pull_request_github_token value. Expected a secret name matching %s, for example: \"MY_PAT_TOKEN\"", RepoConfigFileName, repoConfigSecretNamePattern.String())
	}
	return nil
}

// ActionFailureIssueExpiresHours returns the configured action failure issue
// expiration in hours, or the default value when unset.
func (r *RepoConfig) ActionFailureIssueExpiresHours() int {
	if r != nil && r.Maintenance != nil && r.Maintenance.ActionFailureIssueExpires > 0 {
		return r.Maintenance.ActionFailureIssueExpires
	}
	return DefaultActionFailureIssueExpiresHours
}

// IsActionFailureIssueExpiresExplicit returns true when aw.json explicitly sets
// maintenance.action_failure_issue_expires, as opposed to relying on the
// implicit 168-hour default. Only an explicit value is treated as an opt-in
// trigger for generating agentics-maintenance.yml.
func (r *RepoConfig) IsActionFailureIssueExpiresExplicit() bool {
	return r != nil && r.Maintenance != nil && r.Maintenance.ActionFailureIssueExpiresExplicit
}

// cronFieldRange describes the allowed numeric range for a cron field.
type cronFieldRange struct {
	name string
	min  int
	max  int
}

var cronFieldRanges = []cronFieldRange{
	{"minute", 0, 59},
	{"hour", 0, 23},
	{"day-of-month", 1, 31},
	{"month", 1, 12},
	{"day-of-week", 0, 7},
}

// validateCronExpression validates a 5-field POSIX cron expression.
// It checks that the expression has exactly 5 space-separated fields and that
// each field's numeric literals fall within the allowed range for that position.
func validateCronExpression(expr string) error {
	fields := strings.Split(expr, " ")
	if len(fields) != 5 {
		return fmt.Errorf("cron expression should have exactly 5 fields, got %d. Example: \"0 9 * * 1\"", len(fields))
	}
	for i, field := range fields {
		r := cronFieldRanges[i]
		if err := validateCronField(field, r.min, r.max); err != nil {
			return fmt.Errorf("field %d (%s): %w", i+1, r.name, err)
		}
	}
	return nil
}

// validateCronField validates a single cron field value against an allowed range.
// It supports the following forms: *, n, n-m, n/s, */s, n-m/s, and comma-separated combinations.
func validateCronField(field string, min, max int) error {
	for part := range strings.SplitSeq(field, ",") {
		if err := validateCronPart(part, min, max); err != nil {
			return err
		}
	}
	return nil
}

// validateCronPart validates a single part of a cron field (before splitting on comma).
func validateCronPart(part string, min, max int) error {
	// Strip optional step value (e.g. "*/5" or "1-5/2").
	base, step, hasStep := strings.Cut(part, "/")
	if hasStep {
		sv, err := strconv.Atoi(step)
		if err != nil || sv < 1 {
			return fmt.Errorf("cron step value %q should be a positive integer. Example: \"*/5\"", step)
		}
	}
	part = base

	if part == "*" {
		return nil
	}

	// Range (e.g. "1-5").
	if lo, hi, ok := strings.Cut(part, "-"); ok {
		loN, err1 := strconv.Atoi(lo)
		hiN, err2 := strconv.Atoi(hi)
		if err1 != nil || err2 != nil {
			return fmt.Errorf("cron range %q should be two integers separated by a hyphen. Example: \"1-5\"", part)
		}
		if loN < min || hiN > max || loN > hiN {
			return fmt.Errorf("range %d-%d out of bounds [%d-%d]", loN, hiN, min, max)
		}
		return nil
	}

	// Plain integer.
	n, err := strconv.Atoi(part)
	if err != nil {
		return fmt.Errorf("cron value %q should be an integer. Example: \"5\"", part)
	}
	if n < min || n > max {
		return fmt.Errorf("value %d out of bounds [%d-%d]", n, min, max)
	}
	return nil
}
