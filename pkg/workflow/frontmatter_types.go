package workflow

import (
	"github.com/github/gh-aw/pkg/logger"
)

var frontmatterTypesLog = logger.New("workflow:frontmatter_types")

type RunnerTopology string

// RunnerTopologyArcDind is the topology value for ARC runners with Docker-in-Docker sidecars.
const RunnerTopologyArcDind RunnerTopology = "arc-dind"

// RunnerConfig represents runner topology configuration from the workflow frontmatter.
// The topology field is the single stable contract between gh-aw and AWF for runner
// environment detection — AWF resolves all internal details (network isolation, sysroot
// image, path-prefix probes, tool cache validation) from this signal.
type RunnerConfig struct {
	// Topology identifies the runner execution topology.
	// Supported values: "arc-dind" (ARC with Docker-in-Docker sidecar).
	Topology RunnerTopology `json:"topology,omitempty" yaml:"topology,omitempty"`
}

// RuntimeConfig represents the configuration for a single runtime
type RuntimeConfig struct {
	Version           string `json:"version,omitempty"`             // Version of the runtime (e.g., "20" for Node, "3.11" for Python)
	If                string `json:"if,omitempty"`                  // Optional GitHub Actions if condition (e.g., "hashFiles('go.mod') != ''")
	ActionRepo        string `json:"action-repo,omitempty"`         // Override the GitHub Actions repository (e.g., "actions/setup-node")
	ActionVersion     string `json:"action-version,omitempty"`      // Override the action version (e.g., "v4")
	Cooldown          *bool  `json:"cooldown,omitempty"`            // If false, disable the default dependency cooldown for installs associated with this runtime
	RunInstallScripts *bool  `json:"run-install-scripts,omitempty"` // If true, allow pre/post install scripts for this runtime (supply chain risk; emits warning or error in strict mode)
}

// RuntimesConfig represents the configuration for all runtime environments
// This provides type-safe access to runtime version overrides
type RuntimesConfig struct {
	Node    *RuntimeConfig `json:"node,omitempty"`    // Node.js runtime
	Python  *RuntimeConfig `json:"python,omitempty"`  // Python runtime
	Go      *RuntimeConfig `json:"go,omitempty"`      // Go runtime
	UV      *RuntimeConfig `json:"uv,omitempty"`      // uv package installer
	Bun     *RuntimeConfig `json:"bun,omitempty"`     // Bun runtime
	Deno    *RuntimeConfig `json:"deno,omitempty"`    // Deno runtime
	Dotnet  *RuntimeConfig `json:"dotnet,omitempty"`  // .NET runtime
	Elixir  *RuntimeConfig `json:"elixir,omitempty"`  // Elixir runtime
	GhAw    *RuntimeConfig `json:"gh-aw,omitempty"`   // gh-aw CLI runtime
	Haskell *RuntimeConfig `json:"haskell,omitempty"` // Haskell runtime
	Java    *RuntimeConfig `json:"java,omitempty"`    // Java runtime
	Ruby    *RuntimeConfig `json:"ruby,omitempty"`    // Ruby runtime
}

// GitHubActionsPermissionsConfig holds permission scopes supported by the GitHub Actions GITHUB_TOKEN.
// These scopes can be declared in the workflow's top-level permissions block and are enforced
// natively by GitHub Actions.
type GitHubActionsPermissionsConfig struct {
	Actions             string `json:"actions,omitempty"`
	Checks              string `json:"checks,omitempty"`
	CodeQuality         string `json:"code-quality,omitempty"`
	Contents            string `json:"contents,omitempty"`
	Deployments         string `json:"deployments,omitempty"`
	IDToken             string `json:"id-token,omitempty"`
	Issues              string `json:"issues,omitempty"`
	Discussions         string `json:"discussions,omitempty"`
	Drives              string `json:"drives,omitempty"`
	Packages            string `json:"packages,omitempty"`
	Pages               string `json:"pages,omitempty"`
	PullRequests        string `json:"pull-requests,omitempty"`
	RepositoryProjects  string `json:"repository-projects,omitempty"`
	SecurityEvents      string `json:"security-events,omitempty"`
	Statuses            string `json:"statuses,omitempty"`
	VulnerabilityAlerts string `json:"vulnerability-alerts,omitempty"`
}

// GitHubAppPermissionsConfig holds permission scopes that are exclusive to GitHub App
// installation access tokens (not supported by GITHUB_TOKEN). When any of these are
// specified, a GitHub App must be configured in the workflow.
type GitHubAppPermissionsConfig struct {
	// Organization-level permissions (the common use-case placed first)
	OrganizationProjects                string `json:"organization-projects,omitempty"`
	Members                             string `json:"members,omitempty"`
	OrganizationAdministration          string `json:"organization-administration,omitempty"`
	TeamDiscussions                     string `json:"team-discussions,omitempty"`
	OrganizationHooks                   string `json:"organization-hooks,omitempty"`
	OrganizationMembers                 string `json:"organization-members,omitempty"`
	OrganizationPackages                string `json:"organization-packages,omitempty"`
	OrganizationSelfHostedRunners       string `json:"organization-self-hosted-runners,omitempty"`
	OrganizationCustomOrgRoles          string `json:"organization-custom-org-roles,omitempty"`
	OrganizationCustomProperties        string `json:"organization-custom-properties,omitempty"`
	OrganizationCustomRepositoryRoles   string `json:"organization-custom-repository-roles,omitempty"`
	OrganizationAnnouncementBanners     string `json:"organization-announcement-banners,omitempty"`
	OrganizationEvents                  string `json:"organization-events,omitempty"`
	OrganizationPlan                    string `json:"organization-plan,omitempty"`
	OrganizationUserBlocking            string `json:"organization-user-blocking,omitempty"`
	OrganizationPersonalAccessTokenReqs string `json:"organization-personal-access-token-requests,omitempty"`
	OrganizationPersonalAccessTokens    string `json:"organization-personal-access-tokens,omitempty"`
	OrganizationCopilot                 string `json:"organization-copilot,omitempty"`
	OrganizationCodespaces              string `json:"organization-codespaces,omitempty"`
	// Repository-level permissions
	Administration             string `json:"administration,omitempty"`
	Environments               string `json:"environments,omitempty"`
	GitSigning                 string `json:"git-signing,omitempty"`
	Workflows                  string `json:"workflows,omitempty"`
	RepositoryHooks            string `json:"repository-hooks,omitempty"`
	SingleFile                 string `json:"single-file,omitempty"`
	Codespaces                 string `json:"codespaces,omitempty"`
	RepositoryCustomProperties string `json:"repository-custom-properties,omitempty"`
	// User-level permissions
	EmailAddresses           string `json:"email-addresses,omitempty"`
	CodespacesLifecycleAdmin string `json:"codespaces-lifecycle-admin,omitempty"`
	CodespacesMetadata       string `json:"codespaces-metadata,omitempty"`
}

// PermissionsConfig represents GitHub Actions permissions configuration.
// Supports both shorthand (read-all, write-all) and detailed scope-based permissions.
// Embeds GitHubActionsPermissionsConfig for standard GITHUB_TOKEN scopes and
// GitHubAppPermissionsConfig for GitHub App-only scopes.
type PermissionsConfig struct {
	// Shorthand permission (read-all, write-all, read, write, none)
	Shorthand string `json:"-"` // Not in JSON, set when parsing shorthand format

	// GitHub Actions GITHUB_TOKEN permission scopes
	GitHubActionsPermissionsConfig

	// GitHub App-only permission scopes (require a GitHub App to be configured)
	GitHubAppPermissionsConfig
}

// GuardrailMetric defines a metric threshold that must not degrade during an experiment.
// If the threshold is violated the experiment should be aborted regardless of primary metric outcome.
type GuardrailMetric struct {
	// Name is the metric to guard (e.g. "success_rate", "empty_output_rate").
	Name string `json:"name"`

	// Direction declares whether lower or higher values are preferred ("min" or "max").
	Direction string `json:"direction,omitempty"`

	// Threshold is a comparison expression (e.g. ">=0.95", "==0").
	Threshold string `json:"threshold"`
}

// ContinualExperimentConfig opts an existing A/B experiment into automatic traffic ramping.
// The first declared variant remains control and the second is the candidate.
type ContinualExperimentConfig struct {
	Seed string `json:"seed"`
	Ramp []int  `json:"ramp"`
}

// ExperimentNotify specifies where to post significance alerts when an experiment reaches
// statistical significance.
type ExperimentNotify struct {
	// Discussion is a GitHub discussion number to post a significance comment to.
	Discussion int `json:"discussion,omitempty"`

	// Issue is a GitHub issue number to post a significance comment to.
	Issue int `json:"issue,omitempty"`
}

// ExperimentDecisionConfig configures deterministic interpretation of an experiment analysis.
type ExperimentDecisionConfig struct {
	// MinimumEffect is the minimum absolute improvement required for promotion.
	MinimumEffect float64 `json:"minimum_effect,omitempty"`

	// RegressionTolerance is the maximum absolute regression tolerated before rejection.
	// When omitted, MinimumEffect is used.
	RegressionTolerance *float64 `json:"regression_tolerance,omitempty"`

	// Confidence is the required evidence threshold in the open interval (0, 1).
	// Frequentist analyses require p <= 1-confidence; Bayesian analyses use probability of superiority.
	Confidence float64 `json:"confidence,omitempty"`
}

// ExperimentConfig represents the rich metadata for a single A/B experiment.
// The bare-array form (e.g. prompt_style: [concise, verbose]) is normalized to this
// struct with only the Variants field populated.
type ExperimentConfig struct {
	// Variants is the ordered list of variant strings for this experiment (required, ≥ 2).
	Variants []string `json:"variants"`

	// Description is a human-readable explanation of what the experiment tests.
	Description string `json:"description,omitempty"`

	// Hypothesis states the null and alternative hypotheses for the experiment.
	// e.g. "H0: no change in effective_tokens. H1: concise reduces tokens by >=15%"
	Hypothesis string `json:"hypothesis,omitempty"`

	// Metric names the primary metric that should be observed (e.g. "effective_tokens").
	Metric string `json:"metric,omitempty"`

	// SecondaryMetrics lists additional metrics to track alongside the primary metric.
	SecondaryMetrics []string `json:"secondary_metrics,omitempty"`

	// GuardrailMetrics defines thresholds that must not degrade during the experiment.
	// If any guardrail is violated the experiment should be aborted.
	GuardrailMetrics []GuardrailMetric `json:"guardrail_metrics,omitempty"`

	// MinSamples is the minimum number of runs required per variant before
	// statistical analysis is considered reliable.
	MinSamples int `json:"min_samples,omitempty"`

	// Weight holds an optional per-variant probability weight.  When provided its length
	// must equal the length of Variants.  Values are relative (they need not sum to 100).
	Weight []int `json:"weight,omitempty"`

	// Issue is an optional GitHub issue number that tracks this experiment.
	Issue int `json:"issue,omitempty"`

	// StartDate is an optional ISO-8601 date (YYYY-MM-DD) before which the experiment
	// is not active.  When today is before this date the control variant (first variant)
	// is used.
	StartDate string `json:"start_date,omitempty"`

	// EndDate is an optional ISO-8601 date (YYYY-MM-DD) after which the experiment is
	// no longer active.  When today is after this date the control variant is used.
	EndDate string `json:"end_date,omitempty"`

	// AnalysisType declares the statistical test used by automated reporting tooling.
	// Valid values: t_test, mann_whitney, proportion_test, bayesian_ab.
	AnalysisType string `json:"analysis_type,omitempty"`

	// Decision configures deterministic interpretation of the statistical analysis.
	Decision *ExperimentDecisionConfig `json:"decision,omitempty"`

	// Tags are free-form labels for filtering experiments in dashboards.
	Tags []string `json:"tags,omitempty"`

	// Notify specifies where to post significance alerts when the experiment concludes.
	Notify *ExperimentNotify `json:"notify,omitempty"`

	// Continual enables deterministic control/candidate assignment and automatic traffic ramping.
	Continual *ContinualExperimentConfig `json:"continual,omitempty"`
}

// RateLimitConfig represents rate limiting configuration for workflow triggers
// Limits how many times a user can trigger a workflow within a time window
type RateLimitConfig struct {
	Max          int      `json:"max-runs-per-window,omitempty"` // Maximum runs allowed per user per time window (default: 5)
	Window       int      `json:"window,omitempty"`              // Time window in minutes (default: 60)
	Events       []string `json:"events,omitempty"`              // Event types to apply rate limiting to (e.g., ["workflow_dispatch", "issue_comment"])
	IgnoredRoles []string `json:"ignored-roles,omitempty"`       // Roles that are exempt from rate limiting (e.g., ["admin", "maintainer"])
}

// OTLPEndpointConfig holds configuration for a single OTLP endpoint entry
// used when the `endpoint` field is an object or an element of an array.
type OTLPEndpointConfig struct {
	// URL is the OTLP collector endpoint URL (e.g. "https://traces.example.com:4317").
	// Supports GitHub Actions expressions such as ${{ secrets.OTLP_ENDPOINT }}.
	// When a static URL is provided, its hostname is automatically added to the
	// network firewall allowlist.
	URL string `json:"url,omitempty"`

	// Headers holds HTTP headers to include with every OTLP export request for this endpoint.
	// Same format as OTLPConfig.Headers: preferred map form or deprecated comma-separated string.
	Headers any `json:"headers,omitempty"`
}

// OTLPGitHubAppConfig holds optional runtime GitHub app auth configuration for OTLP export.
// GitHub Actions OIDC token minting is implied when this block is present.
type OTLPGitHubAppConfig struct {
	// Audience is an optional OIDC audience passed to core.getIDToken(audience).
	Audience string `json:"audience,omitempty"`
}

// OTLPWorkloadIdentityConfig configures cloud workload identity federation for OTLP export.
type OTLPWorkloadIdentityConfig struct {
	Provider       string `json:"provider,omitempty"`
	Audience       string `json:"audience,omitempty"`
	ServiceAccount string `json:"service-account,omitempty"`
}

// OTLPConfig holds configuration for OTLP (OpenTelemetry Protocol) trace export.
type OTLPConfig struct {
	// Endpoint accepts one of three forms:
	//   - string:        backward-compat URL  (e.g. "https://traces.example.com:4317")
	//   - object:        single endpoint with URL and optional headers
	//                    (e.g. {url: "https://...", headers: {Authorization: "Bearer ${{ secrets.TOKEN }}"}})
	//   - array:         multiple endpoints for concurrent fan-out
	//                    (e.g. [{url: "https://primary:4317", headers: {...}}, {url: "https://backup:4317"}])
	// Supports GitHub Actions expressions such as ${{ secrets.OTLP_ENDPOINT }}.
	// When a static URL is provided, its hostname is automatically added to the
	// network firewall allowlist.
	Endpoint any `json:"endpoint,omitempty"`

	// Headers holds HTTP headers for the backward-compat string endpoint form.
	// Only used when Endpoint is a plain string; object/array endpoint entries
	// carry their own per-endpoint headers.
	// Supported forms:
	//   - a map of header name to value (e.g. {"Authorization": "Bearer ${{ secrets.TOKEN }}"})
	//   - a comma-separated list of key=value pairs (e.g. "Authorization=Bearer <token>")
	// Both forms are injected as the standard OTEL_EXPORTER_OTLP_HEADERS environment variable.
	Headers any `json:"headers,omitempty"`

	// IfMissing controls runtime behavior when OTLP endpoint/header values are
	// missing (for example because a referenced secret is unset). Supported values:
	//   - "error" (default): fail workflow startup
	//   - "warn": log a warning and skip MCP gateway OTLP config
	//   - "ignore": skip MCP gateway OTLP config without warning
	// This setting affects MCP gateway setup only. Other OTLP-aware steps still
	// receive workflow-level OTEL_* environment variables.
	IfMissing string `json:"if-missing,omitempty"`

	// Attributes defines additional custom key-value string attributes to attach
	// to every OTLP span emitted by this workflow (setup, agent, and conclusion).
	// Values support template variables using {{ variable }} syntax, where the
	// variable name is any OTLP attribute key already computed for the span
	// (e.g. {{ gh-aw.episode.id }}, {{ github.actor }}).
	//
	// Example – emit Langfuse session/user attributes alongside the standard ones:
	//   attributes:
	//     langfuse.session.id: "{{ gh-aw.episode.id }}"
	//     session.id:          "{{ gh-aw.episode.id }}"
	//     langfuse.user.id:    "{{ github.actor }}"
	//     user.id:             "{{ github.actor }}"
	Attributes map[string]string `json:"attributes,omitempty"`

	// ResourceAttributes defines additional OTEL_RESOURCE_ATTRIBUTES entries to
	// append to the standard gh-aw/GitHub resource attributes.
	//
	// Values may be static strings or GitHub Actions expressions such as
	// ${{ github.repository }}. Do not use secrets.* or vars.* expressions here:
	// resource attributes are exported to external tracing backends and are not
	// treated as secret values.
	ResourceAttributes map[string]string `json:"resource-attributes,omitempty"`

	// GitHubApp configures runtime OTLP authentication via the `github-app` key.
	// Supported values:
	//   github-app:
	//     audience: "api://AzureADTokenExchange" # optional
	//
	// When configured, gh-aw mints an OIDC token before actions/setup and passes
	// it to setup so OTLP requests can include an Authorization bearer token.
	GitHubApp *OTLPGitHubAppConfig `json:"github-app,omitempty"`

	// WorkloadIdentity exchanges a GitHub Actions OIDC token for a cloud access
	// token before OTLP export. Google is currently the supported provider.
	WorkloadIdentity *OTLPWorkloadIdentityConfig `json:"workload-identity,omitempty"`
}

// ObservabilityConfig represents workflow observability options.
type ObservabilityConfig struct {
	OTLP *OTLPConfig `json:"otlp,omitempty"`
}

// FrontmatterConfig represents the structured configuration from workflow frontmatter
// This provides compile-time type safety and clearer error messages compared to map[string]any
type FrontmatterConfig struct {
	// Core workflow fields
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	// Intent captures the durable outcome the workflow exists to achieve
	// (why it exists), as opposed to Description (what it does).
	Intent string `json:"intent,omitempty"`
	Emoji  string `json:"emoji,omitempty"` // Optional emoji to represent the workflow visually
	// Engine accepts both a plain string engine name (e.g. "copilot") and an object-style
	// configuration (e.g. {id: copilot, max-continuations: 2}).  Using any prevents
	// JSON unmarshal failures when the engine is an object, which would otherwise cause
	// ParseFrontmatterConfig to return nil and break features that depend on it (e.g. OTLP).
	Engine                      any                          `json:"engine,omitempty"`
	Source                      string                       `json:"source,omitempty"`
	Redirect                    string                       `json:"redirect,omitempty"`
	TrackerID                   string                       `json:"tracker-id,omitempty"`
	ThreatDetectionSuppressions []ThreatDetectionSuppression `json:"threat-detection-suppress,omitempty"`
	TimeoutMinutes              *TemplatableInt32            `json:"timeout-minutes,omitempty"`
	MaxTurns                    *TemplatableInt32            `json:"max-turns,omitempty"`
	MaxRuns                     *TemplatableInt32            `json:"max-runs,omitempty"` // Deprecated: top-level legacy alias for max-turns; migrate with 'gh aw fix'
	MaxAICredits                *TemplatableInt32            `json:"max-ai-credits,omitempty"`
	MaxTurnCacheMisses          *int32                       `json:"max-turn-cache-misses,omitempty"`
	MaxDailyAICredits           *TemplatableInt32            `json:"max-daily-ai-credits,omitempty"`
	MaxToolDenials              *TemplatableInt32            `json:"max-tool-denials,omitempty"`
	Strict                      *bool                        `json:"strict,omitempty"`  // Pointer to distinguish unset from false
	Private                     *bool                        `json:"private,omitempty"` // If true, workflow cannot be added to other repositories
	Labels                      []string                     `json:"labels,omitempty"`
	Skills                      []any                        `json:"skills,omitempty"`
	SkillReferences             []SkillReference             `json:"-"`
	Plugins                     []any                        `json:"plugins,omitempty"`
	PluginReferences            []PluginReference            `json:"-"`
	AmbientFolders              []string                     `json:"ambient-folders,omitempty"`
	GitHubApp                   *GitHubAppConfig             `json:"github-app,omitempty"`

	// Configuration sections - using strongly-typed structs
	Tools            *ToolsConfig               `json:"tools,omitempty"`
	LSP              map[string]LSPServerConfig `json:"lsp,omitempty"`
	MCPServers       map[string]any             `json:"mcp-servers,omitempty"` // Legacy field, use Tools instead
	RuntimesTyped    *RuntimesConfig            `json:"-"`                     // New typed field (not in JSON to avoid conflict)
	Runtimes         map[string]any             `json:"runtimes,omitempty"`    // Deprecated: use RuntimesTyped
	Jobs             map[string]any             `json:"jobs,omitempty"`        // Custom workflow jobs (too dynamic to type)
	SafeOutputs      *SafeOutputsConfig         `json:"safe-outputs,omitempty"`
	MCPScripts       *MCPScriptsConfig          `json:"mcp-scripts,omitempty"`
	Enclaves         EnclavesConfig             `json:"enclaves,omitempty"`
	PermissionsTyped *PermissionsConfig         `json:"-"` // New typed field (not in JSON to avoid conflict)

	// Event and trigger configuration
	On          map[string]any `json:"on,omitempty"`          // Complex trigger config with many variants (too dynamic to type)
	OnNeeds     []string       `json:"-"`                     // New typed field extracted from on.needs (not in JSON to avoid conflict)
	OnStopAfter string         `json:"-"`                     // Typed field extracted from on.stop-after (not in JSON to avoid conflict). Accepts a relative delta ("+25h"), an absolute timestamp, or a GitHub Actions expression (e.g. "${{ inputs.stop-after }}").
	Permissions map[string]any `json:"permissions,omitempty"` // Deprecated: use PermissionsTyped (can be string or map)
	Concurrency map[string]any `json:"concurrency,omitempty"`
	If          string         `json:"if,omitempty"`

	// Network and sandbox configuration
	Network *NetworkPermissions `json:"network,omitempty"`
	Sandbox *SandboxConfig      `json:"sandbox,omitempty"`
	Runner  *RunnerConfig       `json:"runner,omitempty"`

	// Feature flags and other settings
	Features map[string]any    `json:"features,omitempty"` // Dynamic feature flags
	Env      map[string]string `json:"env,omitempty"`
	Secrets  map[string]any    `json:"secrets,omitempty"`

	// Workflow execution settings
	RunsOn        any            `json:"runs-on,omitempty"`      // Supports string, array, or object GitHub Actions runner forms
	RunsOnSlim    any            `json:"runs-on-slim,omitempty"` // Runner for all framework/generated jobs; supports the same forms as runs-on
	RunName       string         `json:"run-name,omitempty"`
	PreSteps      []any          `json:"pre-steps,omitempty"`       // Pre-workflow steps (run before checkout)
	Steps         []any          `json:"steps,omitempty"`           // Custom workflow steps
	PreAgentSteps []any          `json:"pre-agent-steps,omitempty"` // Steps run immediately before agent execution
	PostSteps     []any          `json:"post-steps,omitempty"`      // Post-workflow steps
	Environment   map[string]any `json:"environment,omitempty"`     // GitHub environment
	Container     map[string]any `json:"container,omitempty"`
	Services      map[string]any `json:"services,omitempty"`
	Cache         map[string]any `json:"cache,omitempty"`

	// Import and inclusion
	Imports        any            `json:"imports,omitempty"`         // Can be string or array
	ImportSchema   map[string]any `json:"import-schema,omitempty"`   // Schema for validating 'with' values when this workflow is imported
	InlinedImports bool           `json:"inlined-imports,omitempty"` // If true, inline all imports at compile time instead of using runtime-import macros
	Resources      []string       `json:"resources,omitempty"`       // Additional workflow .md or action .yml files to fetch alongside this workflow

	// Metadata
	Metadata      map[string]string    `json:"metadata,omitempty"` // Custom metadata key-value pairs
	SecretMasking *SecretMaskingConfig `json:"secret-masking,omitempty"`
	Observability *ObservabilityConfig `json:"observability,omitempty"`

	// A/B testing experiments: maps experiment name to either a bare variant array or an
	// object-form ExperimentConfig.  Typed as map[string]any so JSON unmarshaling succeeds
	// for both the legacy bare-array form and the new object form; use ExperimentConfigs for
	// typed access.  See ExperimentConfig and extractExperimentConfigsFromFrontmatter.
	Experiments map[string]any `json:"experiments,omitempty"`

	// ExperimentConfigs holds the fully-typed experiment metadata, populated alongside
	// Experiments during frontmatter parsing.  Keys match those of Experiments.
	ExperimentConfigs map[string]*ExperimentConfig `json:"-"`

	// ModelCosts holds model pricing data in the same structure as models.json.
	// Declared in frontmatter as the `models` field (json:"models,omitempty") using a top-level
	// `providers` key. At runtime the activation job merges this with the built-in models.json
	// so that custom or adjusted cost values are reflected in effective-token accounting.
	// Structure: {"providers": {"<provider>": {"models": {"<model>": {"cost": {...}}}}}}
	ModelCosts map[string]any `json:"models,omitempty"`
	// ModelPolicyAllowed is the experimental frontmatter models.allowed (allowlist), merged as a union across imports.
	ModelPolicyAllowed []string `json:"-"`
	// ModelPolicyBlocked is the experimental frontmatter models.blocked (denylist), merged as a union across imports.
	ModelPolicyBlocked []string `json:"-"`

	// Rate limiting configuration
	RateLimit *RateLimitConfig `json:"user-rate-limit,omitempty"`

	// Update check configuration.
	// When set to false, the version update check step is skipped in the activation job.
	// This flag is not allowed in strict mode.
	UpdateCheck *bool `json:"check-for-updates,omitempty"`

	// Checkout configuration for the agent job.
	// Controls how actions/checkout is invoked.
	// Can be a single CheckoutConfig object or an array of CheckoutConfig objects.
	// Set to false to disable the default checkout step entirely.
	Checkout                   any               `json:"checkout,omitempty"` // Raw value (object, array, or false)
	CheckoutConfigs            []*CheckoutConfig `json:"-"`                  // Parsed checkout configs (not in JSON)
	CheckoutDisabled           bool              `json:"-"`                  // true when checkout: false is set in frontmatter
	CheckoutExplicitlyDisabled bool              `json:"-"`                  // true only when checkout: false is explicitly written by the user in frontmatter
	CheckoutSkipDefault        bool              `json:"-"`                  // true when permissions.contents: none skips only the default workflow-repository checkout

	// Model is the top-level LLM model default. An engine.model value overrides it
	// for that engine instance.
	// Example: model: gpt-5.4
	Model string `json:"model,omitempty"`

	// BinEval evaluations: list of binary questions evaluated after safe-outputs.
	// Can be a plain list (shorthand) or an object with a questions list and optional
	// engine-config / runs-on overrides.
	Evals any `json:"evals,omitempty"`

	// Graders configures deterministic graders that compute metrics from
	// post-agent trace files. Can be {} for zero-config (all built-ins) or a map
	// of grader IDs with optional enabled/script overrides.
	Graders any `json:"graders,omitempty"`

	// ExcludedEnv lists additional environment variable names that must be excluded from
	// the agent container via AWF's --exclude-env flag.  Use this when an env var is set
	// from a source that the compiler cannot automatically detect as credential-bearing
	// (e.g. a workflow_dispatch input that carries a token).
	ExcludedEnv []string `json:"excluded-env,omitempty"`
}
