package workflow

import (
	"context"
	"maps"
	"os"

	"github.com/github/gh-aw/pkg/parser"
)

// SetSkipValidation configures whether to skip schema validation
func (c *Compiler) SetSkipValidation(skip bool) {
	c.skipValidation = skip
}

// SetContext sets the context used for network operations such as SHA resolution.
func (c *Compiler) SetContext(ctx context.Context) {
	c.ctx = ctx
}

// SetRequireDocker configures whether Docker must be available for container image validation.
// When true, validation fails with an error if Docker is not installed or the daemon is not running.
// When false (default), validation is silently skipped when Docker is unavailable.
func (c *Compiler) SetRequireDocker(require bool) {
	c.requireDocker = require
}

// SetQuiet configures whether to suppress success messages (for interactive mode)
func (c *Compiler) SetQuiet(quiet bool) {
	c.quiet = quiet
}

// SetBatchMode configures whether repetitive notices should be aggregated.
func (c *Compiler) SetBatchMode(batchMode bool) {
	c.batchMode = batchMode
}

// GetExperimentalFeatureUsage returns experimental feature usage counts collected in batch mode.
func (c *Compiler) GetExperimentalFeatureUsage() map[string]int {
	usage := make(map[string]int, len(c.featureUsage))
	maps.Copy(usage, c.featureUsage)
	return usage
}

// CopilotRequestsTipNeeded reports whether batch output should show the token-based inference tip.
func (c *Compiler) CopilotRequestsTipNeeded() bool {
	return c.copilotTipNeeded
}

// SetExperimentalFeatureUsage replaces the experimental feature usage map.
// Intended for use in tests that need to exercise aggregation output.
func (c *Compiler) SetExperimentalFeatureUsage(usage map[string]int) {
	c.featureUsage = usage
}

// SetCopilotTipNeeded sets whether the Copilot billing tip should be shown.
// Intended for use in tests that need to exercise aggregation output.
func (c *Compiler) SetCopilotTipNeeded(needed bool) {
	c.copilotTipNeeded = needed
}

// SetNoEmit configures whether to validate without generating lock files
func (c *Compiler) SetNoEmit(noEmit bool) {
	c.noEmit = noEmit
}

// SetApprove configures whether to skip safe update enforcement via the CLI --approve flag.
// When true, safe update enforcement is disabled regardless of strict mode setting,
// approving all changes.
func (c *Compiler) SetApprove(approve bool) {
	c.approve = approve
}

// SetForceStaged configures whether safe-outputs should always compile in staged mode.
func (c *Compiler) SetForceStaged(force bool) {
	c.forceStaged = force
}

// SetFileTracker sets the file tracker for tracking created files
func (c *Compiler) SetFileTracker(tracker FileCreationTracker) {
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

// SetUseSamples configures whether to replace the agentic step with a
// deterministic replay driver that feeds `samples` entries to the safe-outputs
// MCP server via real `tools/call` JSON-RPC. Hidden feature used by
// `gh aw compile --use-samples`.
func (c *Compiler) SetUseSamples(use bool) {
	c.useSamples = use
}

// SetStrictMode configures whether to enable strict validation mode
func (c *Compiler) SetStrictMode(strict bool) {
	c.strictMode = strict
}

// SetAllowActionRefs configures whether unresolved action refs are warnings.
// When false (default), unresolved action refs are compiler errors.
func (c *Compiler) SetAllowActionRefs(allow bool) {
	c.allowActionRefs = allow
}

// SetGHESCompat enables GHES compatibility mode via the --ghes CLI flag.
// It overrides the aw.json ghes field for the current compilation run.
// Artifact actions use versions supported by GHES.
func (c *Compiler) SetGHESCompat(enabled bool) {
	c.ghesCompatFromCLI = enabled
	c.ghesCompatConfigured = false
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

// SetActionsRepo sets the external actions repository override.
// When set, this overrides the default "github/gh-aw-actions" repository used in action mode.
func (c *Compiler) SetActionsRepo(repo string) {
	c.actionsRepo = repo
}

// effectiveActionsRepo returns the actions repository to use for action mode references.
// Returns the override if set, otherwise returns the default GitHubActionsOrgRepo constant.
func (c *Compiler) effectiveActionsRepo() string {
	if c.actionsRepo != "" {
		return c.actionsRepo
	}
	return GitHubActionsOrgRepo
}

// EffectiveActionsRepo returns the actions repository used for action mode references.
// Returns the override if set, otherwise returns the default GitHubActionsOrgRepo.
func (c *Compiler) EffectiveActionsRepo() string {
	return c.effectiveActionsRepo()
}

// GetVersion returns the version string used by the compiler
func (c *Compiler) GetVersion() string {
	return c.version
}

// IncrementWarningCount increments the warning counter
func (c *Compiler) IncrementWarningCount() {
	c.warningCount++
}

// SetConfiguredModelValidator configures optional validation against an external active model inventory.
func (c *Compiler) SetConfiguredModelValidator(validator func(data *WorkflowData) []string) {
	c.configuredModelValidator = validator
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

// SetRepositorySlug sets the repository slug for schedule scattering
func (c *Compiler) SetRepositorySlug(slug string) {
	c.repositorySlug = slug
}

// LockRepositorySlug marks the repository slug as explicitly set (e.g. via --schedule-seed)
// so that per-file git-remote detection cannot override it.
func (c *Compiler) LockRepositorySlug() {
	c.repositorySlugLocked = true
}

// IsRepositorySlugLocked reports whether the repository slug has been locked
// via LockRepositorySlug and must not be overridden by per-file detection.
func (c *Compiler) IsRepositorySlugLocked() bool {
	return c.repositorySlugLocked
}

// SetRepositorySlugIfUnlocked sets the repository slug only when it has not been
// locked via LockRepositorySlug.  This is the method per-file git-remote detection
// should call so that an explicit --schedule-seed flag is never overridden.
func (c *Compiler) SetRepositorySlugIfUnlocked(slug string) {
	if !c.repositorySlugLocked {
		c.SetRepositorySlug(slug)
	}
}

// GetRepositorySlug returns the repository slug (owner/repo) set on this compiler instance.
func (c *Compiler) GetRepositorySlug() string {
	return c.repositorySlug
}

// GetScheduleWarnings returns all accumulated schedule warnings for this compiler instance
func (c *Compiler) GetScheduleWarnings() []string {
	return c.scheduleWarnings
}

// AddSafeUpdateWarning appends a safe update warning to the compiler's accumulated list.
// Callers should invoke this when a safe update violation is detected instead of
// returning a compilation error, so that compilation still succeeds and the agent
// receives actionable guidance.
func (c *Compiler) AddSafeUpdateWarning(warning string) {
	if c.safeUpdateWarnings == nil {
		c.safeUpdateWarnings = []string{}
	}
	c.safeUpdateWarnings = append(c.safeUpdateWarnings, warning)
}

// GetSafeUpdateWarnings returns all accumulated safe update warnings for this compiler instance.
func (c *Compiler) GetSafeUpdateWarnings() []string {
	return c.safeUpdateWarnings
}

// SetPriorManifests replaces the entire pre-cached manifest map.
func (c *Compiler) SetPriorManifests(manifests map[string]*GHAWManifest) {
	if manifests == nil {
		manifests = make(map[string]*GHAWManifest)
	}
	c.priorManifests = manifests
}

// ensureSharedActionCacheAndResolver lazily initializes (on first call) and returns the
// compiler's shared ActionCache and ActionResolver pair. The resolver always wraps the
// returned cache, so both values are initialized and returned together to keep that
// pairing explicit; all workflows compiled by this compiler instance share the same
// in-memory cache.
func (c *Compiler) ensureSharedActionCacheAndResolver() (*ActionCache, *ActionResolver) {
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
	cache, _ := c.ensureSharedActionCacheAndResolver()
	return cache
}

// GetSharedActionResolver returns the shared action resolver used by this compiler instance.
// The resolver is lazily initialized on first access and shared across all workflows.
// It tracks which cache keys were used during compilation, enabling orphaned-entry pruning.
func (c *Compiler) GetSharedActionResolver() *ActionResolver {
	_, resolver := c.ensureSharedActionCacheAndResolver()
	return resolver
}
