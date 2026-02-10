package workflow

import (
	"os"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
)

var logTypes = logger.New("workflow:compiler_types")

// CompilerOption is a functional option for configuring a Compiler
type CompilerOption func(*Compiler)

// WithVerbose sets the verbose logging flag
func WithVerbose(verbose bool) CompilerOption {
	return func(c *Compiler) { c.verbose = verbose }
}

// WithEngineOverride sets the AI engine override
func WithEngineOverride(engine string) CompilerOption {
	return func(c *Compiler) { c.engineOverride = engine }
}

// WithCustomOutput sets a custom output path for the compiled workflow
func WithCustomOutput(path string) CompilerOption {
	return func(c *Compiler) { c.customOutput = path }
}

// WithVersion overrides the auto-detected version
func WithVersion(version string) CompilerOption {
	return func(c *Compiler) { c.version = version }
}

// WithActionMode overrides the auto-detected action mode
func WithActionMode(mode ActionMode) CompilerOption {
	return func(c *Compiler) { c.actionMode = mode }
}

// WithSkipValidation configures whether to skip schema validation
func WithSkipValidation(skip bool) CompilerOption {
	return func(c *Compiler) { c.skipValidation = skip }
}

// WithNoEmit configures whether to validate without generating lock files
func WithNoEmit(noEmit bool) CompilerOption {
	return func(c *Compiler) { c.noEmit = noEmit }
}

// WithStrictMode configures whether to enable strict validation mode
func WithStrictMode(strict bool) CompilerOption {
	return func(c *Compiler) { c.strictMode = strict }
}

// WithFailFast configures whether to stop at first validation error
func WithFailFast(failFast bool) CompilerOption {
	return func(c *Compiler) { c.failFast = failFast }
}

// WithForceRefreshActionPins configures whether to force refresh of action pins
func WithForceRefreshActionPins(force bool) CompilerOption {
	return func(c *Compiler) { c.forceRefreshActionPins = force }
}

// WithWorkflowIdentifier sets the identifier for the current workflow being compiled
func WithWorkflowIdentifier(identifier string) CompilerOption {
	return func(c *Compiler) { c.workflowIdentifier = identifier }
}

// WithRepositorySlug sets the repository slug for schedule scattering
func WithRepositorySlug(slug string) CompilerOption {
	return func(c *Compiler) { c.repositorySlug = slug }
}

// WithGitRoot sets the git repository root directory for action cache path
func WithGitRoot(gitRoot string) CompilerOption {
	return func(c *Compiler) { c.gitRoot = gitRoot }
}

// FileTracker interface for tracking files created during compilation
type FileTracker interface {
	TrackCreated(filePath string)
}

// defaultVersion holds the version string for compiler creation
// This is set by the CLI package during initialization
var defaultVersion = "dev"

// SetDefaultVersion sets the default version for compiler creation
// This should be called once during CLI initialization
func SetDefaultVersion(version string) {
	defaultVersion = version
}

// GetDefaultVersion returns the default version
func GetDefaultVersion() string {
	return defaultVersion
}

// Compiler handles converting markdown workflows to GitHub Actions YAML
type Compiler struct {
	verbose                 bool
	quiet                   bool // If true, suppress success messages (for interactive mode)
	engineOverride          string
	customOutput            string              // If set, output will be written to this path instead of default location
	version                 string              // Version of the extension
	skipValidation          bool                // If true, skip schema validation
	noEmit                  bool                // If true, validate without generating lock files
	strictMode              bool                // If true, enforce strict validation requirements
	trialMode               bool                // If true, suppress safe outputs for trial mode execution
	trialLogicalRepoSlug    string              // If set in trial mode, the logical repository to checkout
	refreshStopTime         bool                // If true, regenerate stop-after times instead of preserving existing ones
	forceRefreshActionPins  bool                // If true, clear action cache and resolve all actions from GitHub API
	failFast                bool                // If true, stop at first validation error instead of collecting all errors
	actionCacheCleared      bool                // Tracks if action cache has already been cleared (for forceRefreshActionPins)
	markdownPath            string              // Path to the markdown file being compiled (for context in dynamic tool generation)
	actionMode              ActionMode          // Mode for generating JavaScript steps (inline vs custom actions)
	actionTag               string              // Override action SHA or tag for actions/setup (when set, overrides actionMode to release)
	jobManager              *JobManager         // Manages jobs and dependencies
	engineRegistry          *EngineRegistry     // Registry of available agentic engines
	fileTracker             FileTracker         // Optional file tracker for tracking created files
	warningCount            int                 // Number of warnings encountered during compilation
	stepOrderTracker        *StepOrderTracker   // Tracks step ordering for validation
	actionCache             *ActionCache        // Shared cache for action pin resolutions across all workflows
	actionResolver          *ActionResolver     // Shared resolver for action pins across all workflows
	actionPinWarnings       map[string]bool     // Shared cache of already-warned action pin failures (key: "repo@version")
	importCache             *parser.ImportCache // Shared cache for imported workflow files
	workflowIdentifier      string              // Identifier for the current workflow being compiled (for schedule scattering)
	scheduleWarnings        []string            // Accumulated schedule warnings for this compiler instance
	repositorySlug          string              // Repository slug (owner/repo) used as seed for scattering
	artifactManager         *ArtifactManager    // Tracks artifact uploads/downloads for validation
	scheduleFriendlyFormats map[int]string      // Maps schedule item index to friendly format string for current workflow
	gitRoot                 string              // Git repository root directory (if set, used for action cache path)
}

// NewCompiler creates a new workflow compiler with functional options.
// By default, it auto-detects the version and action mode.
// Common options: WithVerbose, WithEngineOverride, WithCustomOutput, WithVersion, WithActionMode
func NewCompiler(opts ...CompilerOption) *Compiler {
	// Get default version
	version := defaultVersion

	// Auto-detect git repository root for action cache path resolution
	// This ensures actions-lock.json is created at repo root regardless of CWD
	gitRoot := findGitRoot()

	// Create compiler with defaults
	c := &Compiler{
		verbose:           false,
		engineOverride:    "",
		version:           version,
		skipValidation:    true,                      // Skip validation by default for now since existing workflows don't fully comply
		actionMode:        DetectActionMode(version), // Auto-detect action mode based on version
		jobManager:        NewJobManager(),
		engineRegistry:    GetGlobalEngineRegistry(),
		stepOrderTracker:  NewStepOrderTracker(),
		artifactManager:   NewArtifactManager(),
		actionPinWarnings: make(map[string]bool), // Initialize warning cache
		gitRoot:           gitRoot,               // Auto-detected git root
	}

	// Apply functional options
	for _, opt := range opts {
		opt(c)
	}
	// Auto-detect action mode based on version in case version has been update
	c.actionMode = DetectActionMode(c.version)

	return c
}

// NewCompilerWithVersion creates a new workflow compiler with the legacy signature.
// Deprecated: Use NewCompiler with functional options instead.
// This function is kept for backward compatibility during migration.
func NewCompilerWithVersion(version string) *Compiler {
	return NewCompiler(
		WithVersion(version),
	)
}

// SetSkipValidation configures whether to skip schema validation
func (c *Compiler) SetSkipValidation(skip bool) {
	c.skipValidation = skip
}

// SetQuiet configures whether to suppress success messages (for interactive mode)
func (c *Compiler) SetQuiet(quiet bool) {
	c.quiet = quiet
}

// SetNoEmit configures whether to validate without generating lock files
func (c *Compiler) SetNoEmit(noEmit bool) {
	c.noEmit = noEmit
}

// SetFileTracker sets the file tracker for tracking created files
func (c *Compiler) SetFileTracker(tracker FileTracker) {
	c.fileTracker = tracker
}

// SetTrialMode configures whether to run in trial mode (suppresses safe outputs)
func (c *Compiler) SetTrialMode(trialMode bool) {
	c.trialMode = trialMode
}

// SetTrialLogicalRepoSlug configures the target repository for trial mode
func (c *Compiler) SetTrialLogicalRepoSlug(repo string) {
	c.trialLogicalRepoSlug = repo
}

// SetStrictMode configures whether to enable strict validation mode
func (c *Compiler) SetStrictMode(strict bool) {
	c.strictMode = strict
}

// SetRefreshStopTime configures whether to force regeneration of stop-after times
func (c *Compiler) SetRefreshStopTime(refresh bool) {
	c.refreshStopTime = refresh
}

// SetForceRefreshActionPins configures whether to force refresh of action pins
func (c *Compiler) SetForceRefreshActionPins(force bool) {
	c.forceRefreshActionPins = force
}

// SetActionMode configures the action mode for JavaScript step generation
func (c *Compiler) SetActionMode(mode ActionMode) {
	c.actionMode = mode
}

// GetActionMode returns the current action mode
func (c *Compiler) GetActionMode() ActionMode {
	return c.actionMode
}

// SetActionTag sets the action tag override for actions/setup
func (c *Compiler) SetActionTag(tag string) {
	c.actionTag = tag
}

// GetActionTag returns the action tag override (empty if not set)
func (c *Compiler) GetActionTag() string {
	return c.actionTag
}

// GetVersion returns the version string used by the compiler
func (c *Compiler) GetVersion() string {
	return c.version
}

// IncrementWarningCount increments the warning counter
func (c *Compiler) IncrementWarningCount() {
	c.warningCount++
}

// GetWarningCount returns the current warning count
func (c *Compiler) GetWarningCount() int {
	return c.warningCount
}

// ResetWarningCount resets the warning counter to zero
func (c *Compiler) ResetWarningCount() {
	c.warningCount = 0
}

// SetWorkflowIdentifier sets the identifier for the current workflow being compiled
// This is used for deterministic schedule scattering
func (c *Compiler) SetWorkflowIdentifier(identifier string) {
	c.workflowIdentifier = identifier
}

// GetWorkflowIdentifier returns the current workflow identifier
func (c *Compiler) GetWorkflowIdentifier() string {
	return c.workflowIdentifier
}

// SetRepositorySlug sets the repository slug for schedule scattering
func (c *Compiler) SetRepositorySlug(slug string) {
	c.repositorySlug = slug
}

// GetRepositorySlug returns the repository slug
func (c *Compiler) GetRepositorySlug() string {
	return c.repositorySlug
}

// GetScheduleWarnings returns all accumulated schedule warnings for this compiler instance
func (c *Compiler) GetScheduleWarnings() []string {
	return c.scheduleWarnings
}

// getSharedActionResolver returns the shared action resolver, initializing it on first use
// This ensures all workflows compiled by this compiler instance share the same in-memory cache
func (c *Compiler) getSharedActionResolver() (*ActionCache, *ActionResolver) {
	if c.actionCache == nil {
		// Initialize cache and resolver on first use
		// Use git root if provided, otherwise fall back to current working directory
		baseDir := c.gitRoot
		if baseDir == "" {
			cwd, err := os.Getwd()
			if err != nil {
				cwd = "."
			}
			baseDir = cwd
		}
		c.actionCache = NewActionCache(baseDir)

		// Load existing cache unless force refresh is enabled
		if !c.forceRefreshActionPins {
			_ = c.actionCache.Load() // Ignore errors if cache doesn't exist
		} else {
			logTypes.Print("Force refresh action pins enabled: skipping cache load and will resolve all actions dynamically")
			// Mark as cleared since we skipped loading
			c.actionCacheCleared = true
		}

		c.actionResolver = NewActionResolver(c.actionCache)
		logTypes.Print("Initialized shared action cache and resolver for compiler")
	} else if c.forceRefreshActionPins && !c.actionCacheCleared {
		// If cache already exists but force refresh is set and we haven't cleared it yet, clear it once
		logTypes.Print("Force refresh action pins: clearing existing cache once for this run")
		c.actionCache.Entries = make(map[string]ActionCacheEntry)
		c.actionCacheCleared = true
	}
	return c.actionCache, c.actionResolver
}

// GetSharedActionResolverForTest exposes the shared action resolver for testing purposes
// This should only be used in tests
func (c *Compiler) GetSharedActionResolverForTest() (*ActionCache, *ActionResolver) {
	return c.getSharedActionResolver()
}

// getSharedImportCache returns the shared import cache, initializing it on first use
// This ensures all workflows compiled by this compiler instance share the same import cache
func (c *Compiler) getSharedImportCache() *parser.ImportCache {
	if c.importCache == nil {
		// Initialize cache on first use
		cwd, err := os.Getwd()
		if err != nil {
			cwd = "."
		}
		c.importCache = parser.NewImportCache(cwd)
		logTypes.Print("Initialized shared import cache for compiler")
	}
	return c.importCache
}

// GetSharedActionCache returns the shared action cache used by this compiler instance.
// The cache is lazily initialized on first access and shared across all workflows.
// This allows action SHA validation and other operations to reuse cached resolutions.
func (c *Compiler) GetSharedActionCache() *ActionCache {
	cache, _ := c.getSharedActionResolver()
	return cache
}

// GetArtifactManager returns the artifact manager for tracking uploads/downloads
func (c *Compiler) GetArtifactManager() *ArtifactManager {
	if c.artifactManager == nil {
		c.artifactManager = NewArtifactManager()
	}
	return c.artifactManager
}

// SkipIfMatchConfig holds the configuration for skip-if-match conditions
type SkipIfMatchConfig struct {
	Query string // GitHub search query to check before running workflow
	Max   int    // Maximum number of matches before skipping (defaults to 1)
}

// SkipIfNoMatchConfig holds the configuration for skip-if-no-match conditions
type SkipIfNoMatchConfig struct {
	Query string // GitHub search query to check before running workflow
	Min   int    // Minimum number of matches required to proceed (defaults to 1)
}

// WorkflowData holds all the data needed to generate a GitHub Actions workflow
type WorkflowData struct {
	Name                 string
	WorkflowID           string         // workflow identifier derived from markdown filename (basename without extension)
	TrialMode            bool           // whether the workflow is running in trial mode
	TrialLogicalRepo     string         // target repository slug for trial mode (owner/repo)
	FrontmatterName      string         // name field from frontmatter (for code scanning alert driver default)
	FrontmatterYAML      string         // raw frontmatter YAML content (rendered as comment in lock file for reference)
	Description          string         // optional description rendered as comment in lock file
	Source               string         // optional source field (owner/repo@ref/path) rendered as comment in lock file
	TrackerID            string         // optional tracker identifier for created assets (min 8 chars, alphanumeric + hyphens/underscores)
	ImportedFiles        []string       // list of files imported via imports field (rendered as comment in lock file)
	ImportedMarkdown     string         // Only imports WITH inputs (for compile-time substitution)
	ImportPaths          []string       // Import file paths for runtime-import macro generation (imports without inputs)
	MainWorkflowMarkdown string         // main workflow markdown without imports (for runtime-import)
	IncludedFiles        []string       // list of files included via @include directives (rendered as comment in lock file)
	ImportInputs         map[string]any // input values from imports with inputs (for github.aw.inputs.* substitution)
	On                   string
	Permissions          string
	Network              string // top-level network permissions configuration
	Concurrency          string // workflow-level concurrency configuration
	RunName              string
	Env                  string
	If                   string
	TimeoutMinutes       string
	CustomSteps          string
	PostSteps            string // steps to run after AI execution
	RunsOn               string
	Environment          string // environment setting for the main job
	Container            string // container setting for the main job
	Services             string // services setting for the main job
	Tools                map[string]any
	ParsedTools          *Tools // Structured tools configuration (NEW: parsed from Tools map)
	MarkdownContent      string
	AI                   string        // "claude" or "codex" (for backwards compatibility)
	EngineConfig         *EngineConfig // Extended engine configuration
	AgentFile            string        // Path to custom agent file (from imports)
	AgentImportSpec      string        // Original import specification for agent file (e.g., "owner/repo/path@ref")
	RepositoryImports    []string      // Repository-only imports (format: "owner/repo@ref") for .github folder merging
	StopTime             string
	SkipIfMatch          *SkipIfMatchConfig   // skip-if-match configuration with query and max threshold
	SkipIfNoMatch        *SkipIfNoMatchConfig // skip-if-no-match configuration with query and min threshold
	ManualApproval       string               // environment name for manual approval from on: section
	Command              []string             // for /command trigger support - multiple command names
	CommandEvents        []string             // events where command should be active (nil = all events)
	CommandOtherEvents   map[string]any       // for merging command with other events
	AIReaction           string               // AI reaction type like "eyes", "heart", etc.
	LockForAgent         bool                 // whether to lock the issue during agent workflow execution
	Jobs                 map[string]any       // custom job configurations with dependencies
	Cache                string               // cache configuration
	NeedsTextOutput      bool                 // whether the workflow uses ${{ needs.task.outputs.text }}
	NetworkPermissions   *NetworkPermissions  // parsed network permissions
	SandboxConfig        *SandboxConfig       // parsed sandbox configuration (AWF or SRT)
	SafeOutputs          *SafeOutputsConfig   // output configuration for automatic output routes
	SafeInputs           *SafeInputsConfig    // safe-inputs configuration for custom MCP tools
	Roles                []string             // permission levels required to trigger workflow
	Bots                 []string             // allow list of bot identifiers that can trigger workflow
	CacheMemoryConfig    *CacheMemoryConfig   // parsed cache-memory configuration
	RepoMemoryConfig     *RepoMemoryConfig    // parsed repo-memory configuration
	Runtimes             map[string]any       // runtime version overrides from frontmatter
	PluginInfo           *PluginInfo          // Consolidated plugin information (plugins, custom token, MCP configs)
	ToolsTimeout         int                  // timeout in seconds for tool/MCP operations (0 = use engine default)
	GitHubToken          string               // top-level github-token expression from frontmatter
	ToolsStartupTimeout  int                  // timeout in seconds for MCP server startup (0 = use engine default)
	Features             map[string]any       // feature flags and configuration options from frontmatter (supports bool and string values)
	ActionCache          *ActionCache         // cache for action pin resolutions
	ActionResolver       *ActionResolver      // resolver for action pins
	StrictMode           bool                 // strict mode for action pinning
	SecretMasking        *SecretMaskingConfig // secret masking configuration
	ParsedFrontmatter    *FrontmatterConfig   // cached parsed frontmatter configuration (for performance optimization)
	ActionPinWarnings    map[string]bool      // cache of already-warned action pin failures (key: "repo@version")
	ActionMode           ActionMode           // action mode for workflow compilation (dev, release, script)
}

// BaseSafeOutputConfig holds common configuration fields for all safe output types
type BaseSafeOutputConfig struct {
	Max         int    `yaml:"max,omitempty"`          // Maximum number of items to create
	GitHubToken string `yaml:"github-token,omitempty"` // GitHub token for this specific output type
}

// SafeOutputsConfig holds configuration for automatic output routes
type SafeOutputsConfig struct {
	CreateIssues                    *CreateIssuesConfig                    `yaml:"create-issues,omitempty"`
	CreateDiscussions               *CreateDiscussionsConfig               `yaml:"create-discussions,omitempty"`
	UpdateDiscussions               *UpdateDiscussionsConfig               `yaml:"update-discussion,omitempty"`
	CloseDiscussions                *CloseDiscussionsConfig                `yaml:"close-discussions,omitempty"`
	CloseIssues                     *CloseIssuesConfig                     `yaml:"close-issue,omitempty"`
	ClosePullRequests               *ClosePullRequestsConfig               `yaml:"close-pull-request,omitempty"`
	MarkPullRequestAsReadyForReview *MarkPullRequestAsReadyForReviewConfig `yaml:"mark-pull-request-as-ready-for-review,omitempty"`
	AddComments                     *AddCommentsConfig                     `yaml:"add-comments,omitempty"`
	CreatePullRequests              *CreatePullRequestsConfig              `yaml:"create-pull-requests,omitempty"`
	CreatePullRequestReviewComments *CreatePullRequestReviewCommentsConfig `yaml:"create-pull-request-review-comments,omitempty"`
	CreateCodeScanningAlerts        *CreateCodeScanningAlertsConfig        `yaml:"create-code-scanning-alerts,omitempty"`
	AutofixCodeScanningAlert        *AutofixCodeScanningAlertConfig        `yaml:"autofix-code-scanning-alert,omitempty"`
	AddLabels                       *AddLabelsConfig                       `yaml:"add-labels,omitempty"`
	RemoveLabels                    *RemoveLabelsConfig                    `yaml:"remove-labels,omitempty"`
	AddReviewer                     *AddReviewerConfig                     `yaml:"add-reviewer,omitempty"`
	AssignMilestone                 *AssignMilestoneConfig                 `yaml:"assign-milestone,omitempty"`
	AssignToAgent                   *AssignToAgentConfig                   `yaml:"assign-to-agent,omitempty"`
	AssignToUser                    *AssignToUserConfig                    `yaml:"assign-to-user,omitempty"` // Assign users to issues
	UpdateIssues                    *UpdateIssuesConfig                    `yaml:"update-issues,omitempty"`
	UpdatePullRequests              *UpdatePullRequestsConfig              `yaml:"update-pull-request,omitempty"` // Update GitHub pull request title/body
	PushToPullRequestBranch         *PushToPullRequestBranchConfig         `yaml:"push-to-pull-request-branch,omitempty"`
	UploadAssets                    *UploadAssetsConfig                    `yaml:"upload-asset,omitempty"`
	UpdateRelease                   *UpdateReleaseConfig                   `yaml:"update-release,omitempty"`               // Update GitHub release descriptions
	CreateAgentSessions             *CreateAgentSessionConfig              `yaml:"create-agent-session,omitempty"`         // Create GitHub Copilot agent sessions
	UpdateProjects                  *UpdateProjectConfig                   `yaml:"update-project,omitempty"`               // Smart project board management (create/add/update)
	CreateProjects                  *CreateProjectsConfig                  `yaml:"create-project,omitempty"`               // Create GitHub Projects V2
	CreateProjectStatusUpdates      *CreateProjectStatusUpdateConfig       `yaml:"create-project-status-update,omitempty"` // Create GitHub project status updates
	LinkSubIssue                    *LinkSubIssueConfig                    `yaml:"link-sub-issue,omitempty"`               // Link issues as sub-issues
	HideComment                     *HideCommentConfig                     `yaml:"hide-comment,omitempty"`                 // Hide comments
	DispatchWorkflow                *DispatchWorkflowConfig                `yaml:"dispatch-workflow,omitempty"`            // Dispatch workflow_dispatch events to other workflows
	MissingTool                     *MissingToolConfig                     `yaml:"missing-tool,omitempty"`                 // Optional for reporting missing functionality
	MissingData                     *MissingDataConfig                     `yaml:"missing-data,omitempty"`                 // Optional for reporting missing data required to achieve goals
	NoOp                            *NoOpConfig                            `yaml:"noop,omitempty"`                         // No-op output for logging only (always available as fallback)
	ThreatDetection                 *ThreatDetectionConfig                 `yaml:"threat-detection,omitempty"`             // Threat detection configuration
	Jobs                            map[string]*SafeJobConfig              `yaml:"jobs,omitempty"`                         // Safe-jobs configuration (moved from top-level)
	App                             *GitHubAppConfig                       `yaml:"app,omitempty"`                          // GitHub App credentials for token minting
	AllowedDomains                  []string                               `yaml:"allowed-domains,omitempty"`
	AllowGitHubReferences           []string                               `yaml:"allowed-github-references,omitempty"` // Allowed repositories for GitHub references (e.g., ["repo", "org/repo2"])
	Staged                          bool                                   `yaml:"staged,omitempty"`                    // If true, emit step summary messages instead of making GitHub API calls
	Env                             map[string]string                      `yaml:"env,omitempty"`                       // Environment variables to pass to safe output jobs
	GitHubToken                     string                                 `yaml:"github-token,omitempty"`              // GitHub token for safe output jobs
	MaximumPatchSize                int                                    `yaml:"max-patch-size,omitempty"`            // Maximum allowed patch size in KB (defaults to 1024)
	RunsOn                          string                                 `yaml:"runs-on,omitempty"`                   // Runner configuration for safe-outputs jobs
	Messages                        *SafeOutputMessagesConfig              `yaml:"messages,omitempty"`                  // Custom message templates for footer and notifications
	Mentions                        *MentionsConfig                        `yaml:"mentions,omitempty"`                  // Configuration for @mention filtering in safe outputs
}

// SafeOutputMessagesConfig holds custom message templates for safe-output footer and notification messages
type SafeOutputMessagesConfig struct {
	Footer                         string `yaml:"footer,omitempty" json:"footer,omitempty"`                                                    // Custom footer message template
	FooterInstall                  string `yaml:"footer-install,omitempty" json:"footerInstall,omitempty"`                                     // Custom installation instructions template
	FooterWorkflowRecompile        string `yaml:"footer-workflow-recompile,omitempty" json:"footerWorkflowRecompile,omitempty"`                // Custom footer template for workflow recompile issues
	FooterWorkflowRecompileComment string `yaml:"footer-workflow-recompile-comment,omitempty" json:"footerWorkflowRecompileComment,omitempty"` // Custom footer template for comments on workflow recompile issues
	StagedTitle                    string `yaml:"staged-title,omitempty" json:"stagedTitle,omitempty"`                                         // Custom styled mode title template
	StagedDescription              string `yaml:"staged-description,omitempty" json:"stagedDescription,omitempty"`                             // Custom staged mode description template
	AppendOnlyComments             bool   `yaml:"append-only-comments,omitempty" json:"appendOnlyComments,omitempty"`                          // If true, post run status as new comments instead of updating the activation comment
	RunStarted                     string `yaml:"run-started,omitempty" json:"runStarted,omitempty"`                                           // Custom workflow activation message template
	RunSuccess                     string `yaml:"run-success,omitempty" json:"runSuccess,omitempty"`                                           // Custom workflow success message template
	RunFailure                     string `yaml:"run-failure,omitempty" json:"runFailure,omitempty"`                                           // Custom workflow failure message template
	DetectionFailure               string `yaml:"detection-failure,omitempty" json:"detectionFailure,omitempty"`                               // Custom detection job failure message template
	AgentFailureIssue              string `yaml:"agent-failure-issue,omitempty" json:"agentFailureIssue,omitempty"`                            // Custom footer template for agent failure tracking issues
	AgentFailureComment            string `yaml:"agent-failure-comment,omitempty" json:"agentFailureComment,omitempty"`                        // Custom footer template for comments on agent failure tracking issues
}

// MentionsConfig holds configuration for @mention filtering in safe outputs
type MentionsConfig struct {
	// Enabled can be:
	//   true: mentions always allowed (error in strict mode)
	//   false: mentions always escaped
	//   nil: use default behavior with team members and context
	Enabled *bool `yaml:"enabled,omitempty" json:"enabled,omitempty"`

	// AllowTeamMembers determines if team members can be mentioned (default: true)
	AllowTeamMembers *bool `yaml:"allow-team-members,omitempty" json:"allowTeamMembers,omitempty"`

	// AllowContext determines if mentions from event context are allowed (default: true)
	AllowContext *bool `yaml:"allow-context,omitempty" json:"allowContext,omitempty"`

	// Allowed is a list of user/bot names always allowed (bots not allowed by default)
	Allowed []string `yaml:"allowed,omitempty" json:"allowed,omitempty"`

	// Max is the maximum number of mentions per message (default: 50)
	Max *int `yaml:"max,omitempty" json:"max,omitempty"`
}

// SecretMaskingConfig holds configuration for secret redaction behavior
type SecretMaskingConfig struct {
	Steps []map[string]any `yaml:"steps,omitempty"` // Additional secret redaction steps to inject after built-in redaction
}
