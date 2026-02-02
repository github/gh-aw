package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/github/gh-aw/pkg/workflow"
	"github.com/goccy/go-yaml"
)

var compileValidationLog = logger.New("cli:compile_validation")

// RunActionlintOnFiles runs actionlint on multiple lock files in a single batch
// This is more efficient than running actionlint once per file
func RunActionlintOnFiles(lockFiles []string, verbose bool, strict bool) error {
	if len(lockFiles) == 0 {
		return nil
	}
	return runActionlintOnFile(lockFiles, verbose, strict)
}

// RunZizmorOnFiles runs zizmor on multiple lock files in a single batch
// This is more efficient than running zizmor once per file
func RunZizmorOnFiles(lockFiles []string, verbose bool, strict bool) error {
	if len(lockFiles) == 0 {
		return nil
	}
	return runZizmorOnFiles(lockFiles, verbose, strict)
}

// RunPoutineOnDirectory runs poutine security scanner once on a directory
// Poutine scans all workflows in a directory, so it only needs to run once
func RunPoutineOnDirectory(workflowDir string, verbose bool, strict bool) error {
	return runPoutineOnDirectory(workflowDir, verbose, strict)
}

// CompileWorkflowWithValidation compiles a workflow with always-on YAML validation for CLI usage
func CompileWorkflowWithValidation(compiler *workflow.Compiler, filePath string, verbose bool, runZizmorPerFile bool, runPoutinePerFile bool, runActionlintPerFile bool, strict bool, validateActionSHAs bool) error {
	compileValidationLog.Printf("Compiling workflow with validation: file=%s, strict=%v, validateSHAs=%v", filePath, strict, validateActionSHAs)

	// Set workflow identifier for schedule scattering (use repository-relative path for stability)
	relPath, err := getRepositoryRelativePath(filePath)
	if err != nil {
		compileValidationLog.Printf("Warning: failed to get repository-relative path for %s: %v", filePath, err)
		// Fallback to basename if we can't get relative path
		relPath = filepath.Base(filePath)
	}
	compiler.SetWorkflowIdentifier(relPath)

	// Set repository slug for this specific file (may differ from CWD's repo)
	fileRepoSlug := getRepositorySlugFromRemoteForPath(filePath)
	if fileRepoSlug != "" {
		compiler.SetRepositorySlug(fileRepoSlug)
		compileValidationLog.Printf("Repository slug for file set: %s", fileRepoSlug)
	}

	// Compile the workflow first
	if err := compiler.CompileWorkflow(filePath); err != nil {
		compileValidationLog.Printf("Workflow compilation failed: %v", err)
		return err
	}

	// Always validate that the generated lock file is valid YAML (CLI requirement)
	lockFile := stringutil.MarkdownToLockFile(filePath)
	if _, err := os.Stat(lockFile); err != nil {
		compileValidationLog.Print("Lock file not found, skipping validation (likely no-emit mode)")
		// Lock file doesn't exist (likely due to no-emit), skip YAML validation
		return nil
	}

	compileValidationLog.Print("Validating generated lock file YAML syntax")

	lockContent, err := os.ReadFile(lockFile)
	if err != nil {
		return fmt.Errorf("failed to read generated lock file for validation: %w", err)
	}

	// Validate the lock file is valid YAML
	var yamlValidationTest any
	if err := yaml.Unmarshal(lockContent, &yamlValidationTest); err != nil {
		return fmt.Errorf("generated lock file is not valid YAML: %w", err)
	}

	// Validate action SHAs if requested
	if validateActionSHAs {
		compileValidationLog.Print("Validating action SHAs in lock file")
		// Use the compiler's shared action cache to benefit from cached resolutions
		actionCache := compiler.GetSharedActionCache()
		if err := workflow.ValidateActionSHAsInLockFile(lockFile, actionCache, verbose); err != nil {
			// Action SHA validation warnings are non-fatal
			compileValidationLog.Printf("Action SHA validation completed with warnings: %v", err)
		}
	}

	// Run zizmor on the generated lock file if requested
	if runZizmorPerFile {
		if err := runZizmorOnFile(lockFile, verbose, strict); err != nil {
			return fmt.Errorf("zizmor security scan failed: %w", err)
		}
	}

	// Run poutine on the generated lock file if requested
	if runPoutinePerFile {
		if err := runPoutineOnFile(lockFile, verbose, strict); err != nil {
			return fmt.Errorf("poutine security scan failed: %w", err)
		}
	}

	// Run actionlint on the generated lock file if requested
	// Note: For batch processing, use RunActionlintOnFiles instead
	if runActionlintPerFile {
		if err := runActionlintOnFile([]string{lockFile}, verbose, strict); err != nil {
			return fmt.Errorf("actionlint linter failed: %w", err)
		}
	}

	return nil
}

// CompileWorkflowDataWithValidation compiles from already-parsed WorkflowData with validation
// This avoids re-parsing when the workflow data has already been parsed
func CompileWorkflowDataWithValidation(compiler *workflow.Compiler, workflowData *workflow.WorkflowData, filePath string, verbose bool, runZizmorPerFile bool, runPoutinePerFile bool, runActionlintPerFile bool, strict bool, validateActionSHAs bool) error {
	compileValidationLog.Printf("Compiling from parsed WorkflowData: file=%s", filePath)

	// Compile the workflow using already-parsed data
	if err := compiler.CompileWorkflowData(workflowData, filePath); err != nil {
		compileValidationLog.Printf("WorkflowData compilation failed: %v", err)
		return err
	}

	// Always validate that the generated lock file is valid YAML (CLI requirement)
	lockFile := stringutil.MarkdownToLockFile(filePath)
	if _, err := os.Stat(lockFile); err != nil {
		compileValidationLog.Print("Lock file not found, skipping validation (likely no-emit mode)")
		// Lock file doesn't exist (likely due to no-emit), skip YAML validation
		return nil
	}

	compileValidationLog.Print("Validating generated lock file YAML syntax")

	lockContent, err := os.ReadFile(lockFile)
	if err != nil {
		return fmt.Errorf("failed to read generated lock file for validation: %w", err)
	}

	// Validate the lock file is valid YAML
	var yamlValidationTest any
	if err := yaml.Unmarshal(lockContent, &yamlValidationTest); err != nil {
		return fmt.Errorf("generated lock file is not valid YAML: %w", err)
	}

	// Validate action SHAs if requested
	if validateActionSHAs {
		compileValidationLog.Print("Validating action SHAs in lock file")
		// Use the compiler's shared action cache to benefit from cached resolutions
		actionCache := compiler.GetSharedActionCache()
		if err := workflow.ValidateActionSHAsInLockFile(lockFile, actionCache, verbose); err != nil {
			// Action SHA validation warnings are non-fatal
			compileValidationLog.Printf("Action SHA validation completed with warnings: %v", err)
		}
	}

	// Run zizmor on the generated lock file if requested
	if runZizmorPerFile {
		if err := runZizmorOnFile(lockFile, verbose, strict); err != nil {
			return fmt.Errorf("zizmor security scan failed: %w", err)
		}
	}

	// Run poutine on the generated lock file if requested
	if runPoutinePerFile {
		if err := runPoutineOnFile(lockFile, verbose, strict); err != nil {
			return fmt.Errorf("poutine security scan failed: %w", err)
		}
	}

	// Run actionlint on the generated lock file if requested
	// Note: For batch processing, use RunActionlintOnFiles instead
	if runActionlintPerFile {
		if err := runActionlintOnFile([]string{lockFile}, verbose, strict); err != nil {
			return fmt.Errorf("actionlint linter failed: %w", err)
		}
	}

	return nil
}

// validateCompileConfig validates the configuration flags before compilation
// This is extracted for faster testing without full compilation
func validateCompileConfig(config CompileConfig) error {
	compileValidationLog.Printf("Validating compile config: files=%d, dependabot=%v, purge=%v, workflowDir=%s", len(config.MarkdownFiles), config.Dependabot, config.Purge, config.WorkflowDir)

	// Validate dependabot flag usage
	if config.Dependabot {
		if len(config.MarkdownFiles) > 0 {
			compileValidationLog.Print("Config validation failed: dependabot flag with specific files")
			return fmt.Errorf("--dependabot flag cannot be used with specific workflow files")
		}
		if config.WorkflowDir != "" && config.WorkflowDir != ".github/workflows" {
			compileValidationLog.Printf("Config validation failed: dependabot with custom dir: %s", config.WorkflowDir)
			return fmt.Errorf("--dependabot flag cannot be used with custom --dir")
		}
	}

	// Validate purge flag usage
	if config.Purge && len(config.MarkdownFiles) > 0 {
		compileValidationLog.Print("Config validation failed: purge flag with specific files")
		return fmt.Errorf("--purge flag can only be used when compiling all markdown files (no specific files specified)")
	}

	// Validate workflow directory path
	if config.WorkflowDir != "" && filepath.IsAbs(config.WorkflowDir) {
		compileValidationLog.Printf("Config validation failed: absolute path in workflowDir: %s", config.WorkflowDir)
		return fmt.Errorf("--dir must be a relative path, got: %s", config.WorkflowDir)
	}

	compileValidationLog.Print("Config validation successful")
	return nil
}
