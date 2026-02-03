package cli

import (
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/stringutil"
)

var compileConfigLog = logger.New("cli:compile_config")

// CompileConfig holds configuration options for compiling workflows
type CompileConfig struct {
	MarkdownFiles          []string // Files to compile (empty for all files)
	Verbose                bool     // Enable verbose output
	EngineOverride         string   // Override AI engine setting
	Validate               bool     // Enable schema validation
	Watch                  bool     // Enable watch mode
	WorkflowDir            string   // Custom workflow directory
	SkipInstructions       bool     // Deprecated: Instructions are no longer written during compilation
	NoEmit                 bool     // Validate without generating lock files
	Purge                  bool     // Remove orphaned lock files
	TrialMode              bool     // Enable trial mode (suppress safe outputs)
	TrialLogicalRepoSlug   string   // Target repository for trial mode
	Strict                 bool     // Enable strict mode validation
	Dependabot             bool     // Generate Dependabot manifests for npm dependencies
	ForceOverwrite         bool     // Force overwrite of existing files (dependabot.yml)
	RefreshStopTime        bool     // Force regeneration of stop-after times instead of preserving existing ones
	ForceRefreshActionPins bool     // Force refresh of action pins by clearing cache and resolving from GitHub API
	Zizmor                 bool     // Run zizmor security scanner on generated .lock.yml files
	Poutine                bool     // Run poutine security scanner on generated .lock.yml files
	Actionlint             bool     // Run actionlint linter on generated .lock.yml files
	JSONOutput             bool     // Output validation results as JSON
	ActionMode             string   // Action script inlining mode: inline, dev, or release
	ActionTag              string   // Override action SHA or tag for actions/setup (overrides action-mode to release)
	Stats                  bool     // Display statistics table sorted by file size
	FailFast               bool     // Stop at first error instead of collecting all errors
}

// WorkflowFailure represents a failed workflow with its error count
type WorkflowFailure struct {
	Path       string // File path of the workflow
	ErrorCount int    // Number of errors in this workflow
}

// CompilationStats tracks the results of workflow compilation
type CompilationStats struct {
	Total           int
	Errors          int
	Warnings        int
	FailedWorkflows []string          // Names of workflows that failed compilation (deprecated, use FailedWorkflowDetails)
	FailureDetails  []WorkflowFailure // Detailed information about failed workflows
}

// CompileValidationError represents a single validation error or warning
type CompileValidationError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
	Line    int    `json:"line,omitempty"`
}

// ValidationResult represents the validation result for a single workflow
type ValidationResult struct {
	Workflow     string                   `json:"workflow"`
	Valid        bool                     `json:"valid"`
	Errors       []CompileValidationError `json:"errors"`
	Warnings     []CompileValidationError `json:"warnings"`
	CompiledFile string                   `json:"compiled_file,omitempty"`
}

// sanitizeValidationResults creates a sanitized copy of validation results with all
// error and warning messages sanitized to remove potential secret key names.
// This is applied at the JSON output boundary to ensure no sensitive information
// is leaked regardless of where error messages originated.
func sanitizeValidationResults(results []ValidationResult) []ValidationResult {
	if results == nil {
		return nil
	}

	compileConfigLog.Printf("Sanitizing validation results: workflow_count=%d", len(results))

	sanitized := make([]ValidationResult, len(results))
	for i, result := range results {
		sanitized[i] = ValidationResult{
			Workflow:     result.Workflow,
			Valid:        result.Valid,
			CompiledFile: result.CompiledFile,
			Errors:       make([]CompileValidationError, len(result.Errors)),
			Warnings:     make([]CompileValidationError, len(result.Warnings)),
		}

		// Sanitize all error messages
		for j, err := range result.Errors {
			sanitized[i].Errors[j] = CompileValidationError{
				Type:    err.Type,
				Message: stringutil.SanitizeErrorMessage(err.Message),
				Line:    err.Line,
			}
		}

		// Sanitize all warning messages
		for j, warn := range result.Warnings {
			sanitized[i].Warnings[j] = CompileValidationError{
				Type:    warn.Type,
				Message: stringutil.SanitizeErrorMessage(warn.Message),
				Line:    warn.Line,
			}
		}
	}

	return sanitized
}
