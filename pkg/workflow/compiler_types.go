package workflow

import (
	"context"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
)

var logTypes = logger.New("workflow:compiler_types")

// FileCreationTracker interface for tracking files created during compilation
type FileCreationTracker interface {
	TrackCreated(filePath string)
}

// Compiler handles converting markdown workflows to GitHub Actions YAML
type Compiler struct {
	ctx                     context.Context // Context for network operations (e.g. SHA resolution); defaults to context.Background()
	verbose                 bool
	quiet                   bool // If true, suppress success messages (for interactive mode)
	batchMode               bool // If true, aggregate repetitive notices across workflows
	engineOverride          string
	customOutput            string                   // If set, output will be written to this path instead of default location
	version                 string                   // Version of the extension
	skipValidation          bool                     // If true, skip schema validation
	noEmit                  bool                     // If true, validate without generating lock files
	strictMode              bool                     // If true, enforce strict validation requirements
	allowActionRefs         bool                     // If true, unresolved action refs are warnings instead of errors
	approve                 bool                     // If true, approve safe update changes (skip safe update enforcement)
	forceStaged             bool                     // If true, force all safe-outputs into staged mode
	trialMode               bool                     // If true, suppress safe outputs for trial mode execution
	trialLogicalRepoSlug    string                   // If set in trial mode, the logical repository to checkout
	useSamples              bool                     // If true, replace the agentic step with a deterministic samples replay driver (hidden feature)
	refreshStopTime         bool                     // If true, regenerate stop-after times instead of preserving existing ones
	forceRefreshActionPins  bool                     // If true, clear action cache and resolve all actions from GitHub API
	failFast                bool                     // If true, stop at first validation error instead of collecting all errors
	actionCacheCleared      bool                     // Tracks if action cache has already been cleared (for forceRefreshActionPins)
	markdownPath            string                   // Path to the markdown file being compiled (for context in dynamic tool generation)
	actionMode              ActionMode               // Mode for generating JavaScript steps (inline vs custom actions)
	actionTag               string                   // Override action SHA or tag for actions/setup (when set, overrides actionMode to release)
	actionsRepo             string                   // Override the external actions repository (default: github/gh-aw-actions)
	jobManager              *JobManager              // Manages jobs and dependencies
	engineRegistry          *EngineRegistry          // Registry of available agentic engines
	engineCatalog           *EngineCatalog           // Catalog of engine definitions backed by the registry
	fileTracker             FileCreationTracker      // Optional file tracker for tracking created files
	warningCount            int                      // Number of warnings encountered during compilation
	stepOrderTracker        *StepOrderTracker        // Tracks step ordering for validation
	actionCache             *ActionCache             // Shared cache for action pin resolutions across all workflows
	actionResolver          *ActionResolver          // Shared resolver for action pins across all workflows
	actionPinWarnings       map[string]bool          // Shared cache of already-warned action pin failures (key: "repo@version")
	importCache             *parser.ImportCache      // Shared cache for imported workflow files
	workflowIdentifier      string                   // Identifier for the current workflow being compiled (for schedule scattering)
	scheduleWarnings        []string                 // Accumulated schedule warnings for this compiler instance
	safeUpdateWarnings      []string                 // Accumulated safe update warnings (new secrets/actions requiring review)
	repositorySlug          string                   // Repository slug (owner/repo) used as seed for scattering
	repositorySlugLocked    bool                     // If true, repositorySlug was set via --schedule-seed and must not be overridden by per-file detection
	artifactManager         *ArtifactManager         // Tracks artifact uploads/downloads for validation
	scheduleFriendlyFormats map[int]string           // Maps schedule item index to friendly format string for current workflow
	gitRoot                 string                   // Git repository root directory (if set, used for action cache path)
	repoConfig              *RepoConfig              // Cached repository-level aw.json config
	repoConfigErr           error                    // Cached repo config load error
	repoConfigLoaded        bool                     // True once repo config has been loaded (success or failure)
	contentOverride         string                   // If set, use this content instead of reading from disk (for Wasm/in-memory compilation)
	skipHeader              bool                     // If true, skip ASCII art header in generated YAML (for Wasm/editor mode)
	inlinePrompt            bool                     // If true, inline markdown content in YAML instead of using runtime-import macros (for Wasm builds)
	priorManifests          map[string]*GHAWManifest // Pre-cached manifests keyed by lock file path; takes precedence over git HEAD / filesystem reads
	requireDocker           bool                     // If true, fail validation when Docker is not available instead of silently skipping
	ghesCompatFromCLI       bool                     // If true, GHES compat was requested via --ghes CLI flag (takes precedence over aw.json)
	ghesArtifactCompat      bool                     // If true, emit GHES-compatible v3 pins for artifact actions
	ghesCompatConfigured    bool                     // True once GHES compatibility has been resolved from CLI/config
	ownerTypeCache          map[string]string        // Cached GitHub owner type ("User"/"Organization"/"") keyed by owner login; not goroutine-safe (Compiler is used sequentially)
	copilotRequestsTipShown map[string]bool          // Tracks markdown paths that already emitted the copilot-requests enable tip in this compiler instance
	copilotTipNeeded        bool                     // Tracks whether batch output should include the copilot-requests enable tip
	featureUsage            map[string]int           // Counts experimental feature usage across workflows in batch mode
	permissionWarningShown  map[string]string        // Tracks markdown paths and last warning fingerprint (frontmatter hash when available, otherwise formatted warning text)
	allowedDomainsCache     map[string]allowedDomain // Cached allowed-domains per markdown path with the frontmatter hash that produced it
	// modelPricingResolver is an optional callback for resolving per-token pricing of models that
	// are absent from the embedded models.json catalog. When non-nil it is called during
	// buildInitialWorkflowData for the workflow's configured model; any returned pricing is merged
	// into WorkflowData.ModelCosts so it is embedded in GH_AW_INFO_MODEL_COSTS in the lock.yml.
	// Injected by the cli package (which has access to the embedded catalog and models.dev download).
	modelPricingResolver     func(ctx context.Context, provider, model string) (map[string]float64, bool)
	configuredModelValidator func(data *WorkflowData) []string
}

type allowedDomain struct {
	frontmatterHash string
	domains         string
}
