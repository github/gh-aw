package cli

// CompileConfig holds configuration options for compiling workflows
type CompileConfig struct {
	MarkdownFiles             []string // Files to compile (empty for all files)
	Verbose                   bool     // Enable verbose output
	EngineOverride            string   // Override AI engine setting
	Validate                  bool     // Enable schema validation
	Watch                     bool     // Enable watch mode
	WorkflowDir               string   // Custom workflow directory
	NoEmit                    bool     // Validate without generating lock files
	Purge                     bool     // Remove orphaned lock files
	TrialMode                 bool     // Enable trial mode (suppress safe outputs)
	TrialLogicalRepoSlug      string   // Target repository for trial mode
	UseSamples                bool     // Hidden: replace agentic step with a deterministic samples replay driver
	Strict                    bool     // Enable strict mode validation
	Dependabot                bool     // Generate Dependabot manifests for npm dependencies
	ForceOverwrite            bool     // Force overwrite of existing files (dependabot.yml)
	RefreshStopTime           bool     // Force regeneration of stop-after times instead of preserving existing ones
	ForceRefreshActionPins    bool     // Force refresh of action pins by clearing cache and resolving from GitHub API
	ForceRefreshContainerPins bool     // Force refresh of container image digest pins before compiling
	AllowActionRefs           bool     // Allow unresolved action refs as warnings instead of errors
	Staged                    bool     // Force all safe-outputs into staged mode
	Zizmor                    bool     // Run zizmor security scanner on generated .lock.yml files
	Poutine                   bool     // Run poutine security scanner on generated .lock.yml files
	Actionlint                bool     // Run actionlint linter on generated .lock.yml files
	RunnerGuard               bool     // Run runner-guard taint analysis scanner on generated .lock.yml files
	Syft                      bool     // Run syft SBOM scanner on container images referenced in compiled .lock.yml files
	Grype                     bool     // Run grype vulnerability scanner on container images referenced in compiled .lock.yml files
	Grant                     bool     // Run grant license scanner on container images referenced in compiled .lock.yml files
	Yamllint                  bool     // Run yamllint YAML linter on generated .lock.yml files
	Shellcheck                bool     // Run shellcheck linting of run step scripts (disabled by default; enabled by --shellcheck)
	JSONOutput                bool     // Output validation results as JSON
	ShowAllErrors             bool     // Display all prioritized errors instead of the default top five
	ActionMode                string   // How action scripts are referenced: dev, release, or action. Auto-detected if empty.
	ActionTag                 string   // Pin action refs to this SHA or version tag (e.g. v1, <full-sha>). Sets release mode unless ActionMode is already "action". Mutually exclusive with GHAwRef at the CLI layer.
	ActionsRepo               string   // Override the external actions repository (default: github/gh-aw-actions)
	Stats                     bool     // Display statistics table sorted by file size
	Models                    bool     // Warn about configured models absent from the observed active model inventory
	FailFast                  bool     // Stop at first error instead of collecting all errors
	ScheduleSeed              string   // Override repository slug used for fuzzy schedule scattering (e.g. owner/repo)
	Approve                   bool     // Approve all safe update changes, skipping safe update enforcement regardless of strict mode setting.
	ValidateImages            bool     // Require Docker to be available for container image validation (fail instead of skipping when Docker is unavailable)
	PriorManifestFile         string   // Path to a JSON file containing pre-cached manifests (map[lockFile]*GHAWManifest) collected at MCP server startup; takes precedence over git HEAD / filesystem reads for safe update enforcement
	GHESCompat                bool     // Enable GHES-compatible v3 artifact actions (overrides aw.json ghes field)
	activeModels              *activeModelInventory
}

func (c CompileConfig) shellcheckEnabled() bool {
	return c.Shellcheck
}

// ValidationResult represents the validation result for a single workflow
type ValidationResult struct {
	Workflow     string            `json:"workflow"`
	Valid        bool              `json:"valid"`
	Errors       []ValidationIssue `json:"errors"`
	Warnings     []ValidationIssue `json:"warnings"`
	CompiledFile string            `json:"compiled_file,omitempty"`
	Labels       []string          `json:"labels,omitempty"` // Labels referenced in safe-outputs configurations
}
