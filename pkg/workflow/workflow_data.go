package workflow

import (
	"context"
	"time"

	actionpins "github.com/github/gh-aw/pkg/actionpins"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
)

var workflowDataLog = logger.New("workflow:workflow_data")

// SkipIfMatchConfig holds the configuration for skip-if-match conditions
type SkipIfMatchConfig struct {
	Query string // GitHub search query to check before running workflow
	Max   int    // Maximum number of matches before skipping (defaults to 1)
	Scope string // Scope for the query: "none" disables auto repo:owner/repo scoping
	// Auth (github-token / github-app) is taken from on.github-token / on.github-app at the top level.
}

// SkipIfNoMatchConfig holds the configuration for skip-if-no-match conditions
type SkipIfNoMatchConfig struct {
	Query string // GitHub search query to check before running workflow
	Min   int    // Minimum number of matches required to proceed (defaults to 1)
	Scope string // Scope for the query: "none" disables auto repo:owner/repo scoping
	// Auth (github-token / github-app) is taken from on.github-token / on.github-app at the top level.
}

// SkipIfCheckFailingConfig holds the configuration for skip-if-check-failing conditions
type SkipIfCheckFailingConfig struct {
	Include      []string // check names to include (empty = all checks)
	Exclude      []string // check names to exclude
	Branch       string   // optional branch name to check (defaults to triggering ref or PR base branch)
	AllowPending bool     // if true, pending/in-progress checks are not treated as failing (default: treat pending as failing)
}
type WorkflowData struct {
	Name                           string
	WorkflowID                     string           // workflow identifier derived from markdown filename (basename without extension)
	CompiledVersion                string           // gh-aw compiler version emitted to generated install steps as GH_AW_COMPILED_VERSION (release tag for releases, "dev" for non-release builds) so the install script can resolve a compat.json window at runtime without churn
	TrialMode                      bool             // whether the workflow is running in trial mode
	TrialLogicalRepo               string           // target repository slug for trial mode (owner/repo)
	UseSamples                     bool             // whether the agentic step should be replaced by a deterministic samples replay driver (hidden feature)
	FrontmatterName                string           // name field from frontmatter (for code scanning alert driver default)
	FrontmatterEmoji               string           // emoji field from frontmatter (for display in footers and UI)
	FrontmatterYAML                string           // raw frontmatter YAML content (rendered as comment in lock file for reference)
	FrontmatterHash                string           // SHA-256 hash of frontmatter (computed before job building, used to derive stable heredoc delimiters)
	BodyHash                       string           // SHA-256 hash of the markdown body (computed before job building)
	FrontmatterFieldLines          map[string]int   // absolute 1-based line numbers of top-level frontmatter keys in the source file (populated by parser)
	RawMarkdown                    string           // raw markdown body before include expansion, used for frontmatter hash computation without re-reading the file
	Description                    string           // optional description rendered as comment in lock file
	Intent                         string           // optional intent (durable outcome the workflow exists to achieve) rendered as comment in lock file
	Docs                           string           // optional human-facing documentation URL preserved in lock metadata
	Source                         string           // optional source field (owner/repo@ref/path) rendered as comment in lock file
	Redirect                       string           // optional redirect field describing a moved workflow location
	TrackerID                      string           // optional tracker identifier for created assets (min 8 chars, alphanumeric + hyphens/underscores)
	MaxDailyAICredits              *string          // optional 24-hour per-workflow ET threshold (numeric string or GitHub Actions expression)
	MaxDailyAICreditsGitHubApp     *GitHubAppConfig // optional GitHub App for minting the token used by the daily AIC guardrail
	ImportedFiles                  []string         // list of files imported via imports field (rendered as comment in lock file)
	Skills                         []string         // skill specs from frontmatter (owner/repo@sha or owner/repo/skill/path@sha)
	SkillReferences                []SkillReference
	Plugins                        []string
	PluginReferences               []PluginReference
	ImportedMarkdown               string   // Only imports WITH inputs (for compile-time substitution)
	ImportPaths                    []string // Import file paths for runtime-import macro generation (imports without inputs)
	PromptImports                  []parser.PromptImportEntry
	MainWorkflowMarkdown           string         // main workflow markdown without imports (for runtime-import)
	IncludedFiles                  []string       // list of files included via @include directives (rendered as comment in lock file)
	ImportInputs                   map[string]any // input values from imports with inputs (for github.aw.inputs.* substitution)
	On                             string
	Permissions                    string
	Network                        string // top-level network permissions configuration
	Concurrency                    string // workflow-level concurrency configuration
	RunName                        string
	Env                            string
	EnvSources                     map[string]string // env var name → source ("(main workflow)" or import file path) for lock file header
	If                             string
	TimeoutMinutes                 string
	CustomSteps                    string
	PreSteps                       string // steps to run at the very start of the agent job, before checkout
	PreAgentSteps                  string // steps to run immediately before the agent execution step
	PostSteps                      string // steps to run after AI execution
	RunsOn                         string
	RunsOnSlim                     string // rendered runs-on snippet for framework/generated jobs (activation, safe-outputs, unlock, etc.)
	Environment                    string // environment setting for the main job
	Container                      string // container setting for the main job
	Services                       string // services setting for the main job
	Tools                          map[string]any
	LSP                            map[string]LSPServerConfig // top-level LSP server configuration for Copilot CLI
	ParsedTools                    *Tools                     // Structured tools configuration (NEW: parsed from Tools map)
	ExplicitlyDisabledTools        map[string]struct{}        // tool names explicitly set to false before default resolution mutates/removes their map entries
	BashDisabled                   bool                       // true when tools.bash was fully and explicitly refused (bash: false, or bash: []) after default-tool resolution; used by engines that can fully disable shell execution (e.g. Codex's features.shell_tool=false), see EngineCapabilities.BashDisable
	MarkdownContent                string
	AI                             string        // "claude" or "codex" (for backwards compatibility)
	Model                          string        // Top-level LLM model override (from frontmatter model: field or imports)
	EngineConfig                   *EngineConfig // Extended engine configuration
	AgentFile                      string        // Path to custom agent file (from imports)
	AgentImportSpec                string        // Original import specification for agent file (e.g., "owner/repo/path@ref")
	RepositoryImports              []string      // Repository-only imports (format: "owner/repo@ref") for .github folder merging
	StopTime                       string
	Cooldown                       time.Duration                   // minimum time between completed runs that executed the agent job
	SkipIfMatch                    *SkipIfMatchConfig              // skip-if-match configuration with query and max threshold
	SkipIfNoMatch                  *SkipIfNoMatchConfig            // skip-if-no-match configuration with query and min threshold
	SkipIfCheckFailing             *SkipIfCheckFailingConfig       // skip-if-check-failing configuration
	SkipRoles                      []string                        // roles to skip workflow for (e.g., [admin, maintainer, write])
	SkipBots                       []string                        // users to skip workflow for (e.g., [user1, user2])
	SkipAuthorAssociations         map[string][]string             // author associations to skip by event name (on.skip-author-associations)
	AllowBotAuthoredTriggerComment bool                            // allow bot-posted-menu / user-checks-box pattern (on.allow-bot-authored-trigger-comment)
	OnSteps                        []map[string]any                // steps to inject into the pre-activation job from on.steps
	OnRestoreMemory                bool                            // enable memory restore in pre-activation for on.steps via on.restore-memory (default false)
	OnPermissions                  *Permissions                    // additional permissions for the pre-activation job from on.permissions
	OnNeeds                        []string                        // custom workflow jobs that pre_activation/activation should depend on from on.needs
	AmbientFolders                 []string                        // workspace-relative folders to bundle in the activation artifact and restore before the agent runs
	ManualApproval                 string                          // environment name for manual approval from on: section
	Command                        []string                        // for /command trigger support - multiple command names
	CommandEvents                  []string                        // events where command should be active (nil = all events)
	CommandCentralized             bool                            // when true, slash_command uses centralized dispatch routing via workflow_dispatch
	CommandPlaceholder             string                          // optional footer hint text from slash_command.placeholder
	CommandOtherEvents             map[string]any                  // for merging command with other events
	LabelCommand                   []string                        // for label-command trigger support - label names that act as commands
	LabelCommandEvents             []string                        // events where label-command should be active (nil = all: issues, pull_request, discussion)
	LabelCommandDecentralized      bool                            // when true, label_command uses decentralized dispatch routing via agentic_commands.yml
	LabelCommandOtherEvents        map[string]any                  // for merging label-command with other events
	LabelCommandRemoveLabel        bool                            // whether to automatically remove the triggering label (default: true)
	AIReaction                     ReactionType                    // AI reaction type like "eyes", "heart", etc.
	ReactionIssues                 *bool                           // whether reactions are allowed on issues/issue_comment triggers (default: true)
	ReactionPullRequests           *bool                           // whether reactions are allowed on pull_request/pull_request_review_comment triggers (default: true)
	ReactionDiscussions            *bool                           // whether reactions are allowed on discussion/discussion_comment triggers (default: true)
	StatusComment                  *bool                           // whether to post status comments (default: true when ai-reaction is set, false otherwise)
	StatusCommentIssues            *bool                           // whether status comments are allowed on issues/issue_comment triggers (default: true)
	StatusCommentPullRequests      *bool                           // whether status comments are allowed on pull_request/pull_request_review_comment triggers (default: true)
	StatusCommentDiscussions       *bool                           // whether status comments are allowed on discussion/discussion_comment triggers (default: true)
	ActivationGitHubToken          string                          // custom github token from on.github-token for reactions/comments
	ActivationGitHubApp            *GitHubAppConfig                // github app config from on.github-app for minting activation tokens
	TopLevelGitHubApp              *GitHubAppConfig                // top-level github-app fallback for all nested github-app token minting operations
	LockForAgent                   bool                            // whether to lock the issue during agent workflow execution
	Jobs                           map[string]any                  // custom job configurations with dependencies
	Cache                          string                          // cache configuration
	NeedsTextOutput                bool                            // whether the workflow uses ${{ needs.task.outputs.text }}
	NetworkPermissions             *NetworkPermissions             // parsed network permissions
	SandboxConfig                  *SandboxConfig                  // parsed sandbox configuration (AWF or SRT)
	RunnerConfig                   *RunnerConfig                   // parsed runner topology configuration (e.g., arc-dind)
	SafeOutputs                    *SafeOutputsConfig              // output configuration for automatic output routes
	SafeOutputsInputEnvVars        map[string]string               // GH_AW_INPUT_* env vars referenced by safe-outputs config; populated during MCP setup generation so renderers can forward them to the nested container
	MCPScripts                     *MCPScriptsConfig               // mcp-scripts configuration for custom MCP tools
	Enclaves                       EnclavesConfig                  // AWF-owned private repository enclave executors
	LabelNames                     []string                        // label names that must match for pull_request_target labeled events (on.labels)
	Roles                          []string                        // permission levels required to trigger workflow
	Bots                           []string                        // allow list of bot identifiers that can trigger workflow
	RateLimit                      *RateLimitConfig                // rate limiting configuration for workflow triggers
	CacheMemoryConfig              *CacheMemoryConfig              // parsed cache-memory configuration
	DriveMemoryConfig              *DriveMemoryConfig              // parsed drive-memory configuration
	CommentMemoryConfig            *CommentMemoryConfig            // parsed tools.comment-memory configuration
	RepoMemoryConfig               *RepoMemoryConfig               // parsed repo-memory configuration
	Runtimes                       map[string]any                  // runtime version overrides from frontmatter
	ToolsTimeout                   string                          // timeout for tool/MCP operations: numeric string (seconds) or GitHub Actions expression (empty = use engine default)
	ToolsStartupTimeout            string                          // timeout for MCP server startup: numeric string (seconds) or GitHub Actions expression (empty = use engine default)
	Features                       map[string]any                  // feature flags and configuration options from frontmatter (supports bool and string values)
	Ctx                            context.Context                 // context propagated from the caller for network operations (e.g. SHA resolution)
	ActionCache                    *ActionCache                    // cache for action pin resolutions
	ActionResolver                 *ActionResolver                 // resolver for action pins
	DockerImages                   []string                        // container images collected at compile time (pinned refs when pins are cached)
	DockerImagePins                []GHAWManifestContainer         // full container pin info (image, digest, pinned_image) for manifest
	ActionResolutionFailures       []GHAWManifestResolutionFailure // unresolved action-ref pinning failures for lock manifest auditing
	StrictMode                     bool                            // strict mode for action pinning
	AllowActionRefs                bool                            // if true, unresolved action refs are warnings instead of errors
	ValidateAWFConfig              bool                            // if true, validate generated AWF config JSON against schema (set by --validate)
	SecretMasking                  *SecretMaskingConfig            // secret masking configuration
	ParsedFrontmatter              *FrontmatterConfig              // cached parsed frontmatter configuration (for performance optimization)
	RawFrontmatter                 map[string]any                  // raw parsed frontmatter map (for passing to hash functions without re-parsing)
	OTLPEndpoint                   string                          // resolved OTLP endpoint (from observability.otlp.endpoint, including imports; set by injectOTLPConfig)
	OTLPHeaders                    string                          // normalized OTLP headers in key=value,key=value format (from observability.otlp.headers, including imports; set by injectOTLPConfig)
	OTLPEndpoints                  string                          // JSON-encoded array of all OTLP endpoints (from observability.otlp.endpoints; set by injectOTLPConfig as GH_AW_OTLP_ENDPOINTS)
	OTLPUsesEnterpriseDefaults     bool                            // true when the OTLP endpoint/headers come from the enterprise default vars/secrets rather than frontmatter (set by injectOTLPConfig)
	ResolvedMCPServers             map[string]any                  // fully merged mcp-servers from main workflow and all imports (for mcp inspect)
	ActionPinWarnings              map[string]bool                 // cache of already-warned action pin failures (key: "repo@version")
	ActionMode                     ActionMode                      // action mode for workflow compilation (dev, release, script)
	HasExplicitGitHubTool          bool                            // true if tools.github was explicitly configured in frontmatter
	InlinedImports                 bool                            // if true, inline all imports at compile time (from inlined-imports frontmatter field)
	CheckoutConfigs                []*CheckoutConfig               // user-configured checkout settings from frontmatter
	CheckoutDisabled               bool                            // true when checkout: false is set in frontmatter, or auto-disabled for pull_request_target
	CheckoutExplicitlyDisabled     bool                            // true only when checkout: false is explicitly set in frontmatter (not auto-disabled)
	CheckoutSkipDefault            bool                            // true when permissions.contents: none skips only the default workflow-repository checkout
	IsPullRequestTarget            bool                            // true when the workflow's on: triggers contain pull_request_target (but NOT pull_request)
	HasDispatchItemNumber          bool                            // true when workflow_dispatch has item_number input (generated by label trigger shorthand)
	ConcurrencyJobDiscriminator    string                          // optional discriminator expression appended to job-level concurrency groups (from concurrency.job-discriminator)
	IsDetectionRun                 bool                            // true when this WorkflowData is used for inline threat detection (not the main agent run)
	IsEvalsRun                     bool                            // true when this WorkflowData is used for eval execution (separate from agent and detection runs)
	UpdateCheckDisabled            bool                            // true when check-for-updates: false is set in frontmatter (disables version check step in activation job)
	StaleCheckDisabled             bool                            // true when on.stale-check: false is set in frontmatter (disables frontmatter hash check step in activation job)
	StaleCheckFull                 bool                            // true when on.stale-check: full is set in frontmatter (enables body hash check alongside frontmatter hash check)
	EngineConfigSteps              []map[string]any                // steps returned by engine.RenderConfig — prepended before execution steps
	ServicePortExpressions         string                          // comma-separated ${{ job.services['<id>'].ports['<port>'] }} expressions for AWF --allow-host-service-ports
	RunInstallScripts              bool                            // true when runtimes.node.run-install-scripts: true is set (main workflow and/or imports); disables --ignore-scripts on generated npm install steps
	CachedPermissions              *Permissions                    // cached parsed Permissions object (for performance optimization); populated by applyDefaults after all permission mutations
	CachedPermissionScopeNamesErr  error                           // cached result of ValidatePermissionScopeNames(Permissions); nil = valid; populated by applyDefaults
	CachedPermissionScopeNamesSet  bool                            // true once CachedPermissionScopeNamesErr has been populated; distinguishes "valid (nil)" from "not yet computed"
	ConcurrencyGroupExpr           string                          // cached concurrency group expression extracted from Concurrency YAML (for performance optimization); populated by applyDefaults
	CachedConcurrencyGroupExprErr  error                           // cached result of validateConcurrencyGroupExpression(ConcurrencyGroupExpr); nil = valid; populated by applyDefaults
	Experiments                    map[string][]string             // A/B testing experiments: maps experiment name to variant list (from frontmatter)
	ExperimentConfigs              map[string]*ExperimentConfig    // Full A/B experiment metadata (populated alongside Experiments)
	ExperimentsStorage             ExperimentStorageMode           // "cache" or "repo" (default "repo"); controls how experiment state is persisted across runs
	CachedConcurrencyGroupExprSet  bool                            // true once CachedConcurrencyGroupExprErr has been populated; distinguishes "valid (nil)" from "not yet computed"
	CachedParsedToolsets           []string                        // cached result of ParseGitHubToolsets for the GitHub tool (for performance optimization); populated by applyDefaults
	CachedAllowedDomainsStr        string                          // cached allowed-domains string for sanitization (for performance optimization); computed once and reused across multiple compilation steps
	CachedAllowedDomainsComputed   bool                            // true once CachedAllowedDomainsStr has been set; distinguishes "computed empty" from "not yet computed"
	CachedRuntimeRequirements      []RuntimeRequirement            // cached runtime requirements derived from DetectRuntimeRequirements; reused by validation and YAML generation within one compilation
	CachedRuntimeRequirementsSet   bool                            // true once CachedRuntimeRequirements is populated; distinguishes "computed empty" from "not yet computed"
	KnownActionCredentialEnvVars   map[string]struct{}             // env vars for clean_known_action_credentials.sh; keyed by GH_AW_CLEAN_* names; nil when no known credential-leaking actions are detected
	ModelMappings                  map[string][]string             // merged model alias map (builtins + imported workflow aliases + main frontmatter overrides, in priority order); emitted to AWF config JSON as apiProxy.models for both agent and detection jobs
	ModelCosts                     map[string]any                  // model pricing data from frontmatter `models` field (providers structure); merged with built-in models.json at runtime by generate_aw_info.cjs
	ModelPolicyAllowed             []string                        // merged models.allowed policy list (union across imports + main frontmatter)
	ModelPolicyBlocked             []string                        // merged models.blocked policy list (union across imports + main frontmatter)
	DefaultAiCreditsPricing        *AiCreditsPricingConfig         // fallback per-token pricing from frontmatter models.default-ai-credits-pricing; used by AWF API proxy for unrecognized models
	ActionPinMappings              map[string]string               // action-pin redirect table from aw.json action_pins: maps "owner/repo@version" → "owner/repo@version"
	ContainerPinMappings           map[string]string               // container-pin redirect table from aw.json container_pins: maps source image → replacement image
	GHES                           bool                            // select action versions compatible with GitHub Enterprise Server
	Evals                          *EvalsConfig                    // BinEval evaluation configuration parsed from frontmatter evals field
	Graders                        *GradersConfig                  // Deterministic graders configuration parsed from frontmatter graders field
	ExcludedEnv                    []string                        // additional env var names to exclude from agent container via AWF --exclude-env (from frontmatter excluded-env field)
}

// PinContext returns an actionpins.PinContext backed by this WorkflowData.
// It is used to pass the resolver and warnings state to pkg/actionpins functions
// without introducing an import cycle.
//
// A new PinContext struct is allocated on each call, but the Warnings field always
// points to the same underlying map (data.ActionPinWarnings). This shared map is
// what makes deduplication of console messages work correctly across multiple calls
// on the same WorkflowData. Callers must not replace ctx.Warnings with a different
// map after construction — doing so would break deduplication.
func (d *WorkflowData) PinContext() *actionpins.PinContext {
	if d == nil {
		return nil
	}
	if d.ActionPinWarnings == nil {
		d.ActionPinWarnings = make(map[string]bool)
	}
	pinCtx := &actionpins.PinContext{
		Ctx:               d.Ctx,
		StrictMode:        d.StrictMode,
		EnforcePinned:     true,
		AllowActionRefs:   d.AllowActionRefs,
		GHES:              d.GHES,
		Warnings:          d.ActionPinWarnings,
		Mappings:          d.ActionPinMappings,
		ContainerMappings: d.ContainerPinMappings,
		RecordResolutionFailure: func(f actionpins.ResolutionFailure) {
			d.ActionResolutionFailures = append(d.ActionResolutionFailures, GHAWManifestResolutionFailure{
				Repo:      f.Repo,
				Ref:       f.Ref,
				ErrorType: string(f.ErrorType),
			})
		},
	}
	// Only set Resolver if non-nil to avoid passing a typed nil interface value
	// (which would be non-nil in actionpins but crash on method call).
	if d.ActionResolver != nil {
		pinCtx.Resolver = d.ActionResolver
	}
	// When GH_HOST is set to a non-github.com host (GHES/GHEC), the action
	// resolver targets that host and fails to resolve actions/* repos which live
	// on github.com.  Silently falling back to bundled hardcoded pins in that
	// case produces unverified SHA pins, so disable the fallback.
	// When GH_HOST is unset, fall back to the programmatic default host (set
	// for example from auto-detected git remotes).  Mirror setupGHCommand's
	// (github_cli.go) precedence: GH_HOST wins when present; default host is
	// only consulted when GH_HOST is absent.
	if ghHost := lookupProcessEnv("GH_HOST"); ghHost != "" {
		if ghHost != "github.com" {
			workflowDataLog.Print("Non-github.com GH_HOST detected; disabling hardcoded action pin fallback")
			pinCtx.SkipHardcodedFallback = true
		}
	} else if defaultHost := getDefaultGHHost(); defaultHost != "" && defaultHost != "github.com" {
		workflowDataLog.Print("Non-github.com default host detected; disabling hardcoded action pin fallback")
		pinCtx.SkipHardcodedFallback = true
	}
	workflowDataLog.Printf("Built pin context: strictMode=%t, skipHardcodedFallback=%t", pinCtx.StrictMode, pinCtx.SkipHardcodedFallback)
	return pinCtx
}

// getContainerPinMappings returns ContainerPinMappings when d is non-nil, otherwise nil.
// Used for nil-safe access when constructing MCPConfigRenderer.
func (d *WorkflowData) getContainerPinMappings() map[string]string {
	if d == nil {
		return nil
	}
	return d.ContainerPinMappings
}
