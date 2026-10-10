package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/github/gh-aw/pkg/setutil"
	"github.com/github/gh-aw/pkg/stringutil"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/fileutil"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/sliceutil"
	"github.com/github/gh-aw/pkg/workflow"
)

var runPushLog = logger.New("cli:run_push")

// collectWorkflowFiles collects the workflow .md file, its corresponding .lock.yml file,
// and the transitive closure of all imported files.
// Note: This function always recompiles the workflow to ensure the lock file is up-to-date,
// regardless of the frontmatter hash status.
func collectWorkflowFiles(ctx context.Context, workflowPath string, verbose bool, approve bool) ([]string, error) {
	runPushLog.Printf("Collecting files for workflow: %s", workflowPath)

	files := make(map[string]struct { // Use map to avoid duplicates
	})
	visited := make(map[string]struct {
	})

	// Get absolute path for the workflow
	absWorkflowPath, err := filepath.Abs(workflowPath)
	if err != nil {
		runPushLog.Printf("Failed to get absolute path for %s: %v", workflowPath, err)
		return nil, fmt.Errorf("failed to get absolute path for workflow: %w", err)
	}
	runPushLog.Printf("Resolved absolute workflow path: %s", absWorkflowPath)

	// Validate the absolute path
	absWorkflowPath, err = fileutil.ValidateAbsolutePath(absWorkflowPath)
	if err != nil {
		runPushLog.Printf("Invalid workflow path: %v", err)
		return nil, fmt.Errorf("invalid workflow path: %w", err)
	}

	// Add the workflow .md file
	files[absWorkflowPath] = struct {
	}{}
	runPushLog.Printf("Added workflow file: %s", absWorkflowPath)

	// Check lock file and log hash status for observability
	lockFilePath := stringutil.MarkdownToLockFile(absWorkflowPath)
	runPushLog.Printf("Checking lock file: %s", lockFilePath)

	// Always recompile, but check and log hash status for observability
	if fileutil.FileExists(lockFilePath) {
		runPushLog.Printf("Lock file exists: %s", lockFilePath)
		// Check frontmatter hash for observability
		runPushLog.Print("Checking frontmatter hash for observability")
		if hashMismatch, err := checkFrontmatterHashMismatch(absWorkflowPath, lockFilePath); err != nil {
			runPushLog.Printf("Error checking frontmatter hash: %v", err)
			// Don't fail, just log the error
		} else if hashMismatch {
			runPushLog.Print("Lock file frontmatter hash changed (will recompile)")
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Frontmatter hash changed, recompiling workflow..."))
			}
		} else {
			runPushLog.Print("Lock file frontmatter hash unchanged (will still recompile)")
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Recompiling workflow..."))
			}
		}
	} else if os.IsNotExist(err) {
		// Lock file doesn't exist
		runPushLog.Printf("Lock file not found: %s", lockFilePath)
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Lock file not found, compiling workflow..."))
		}
	} else {
		runPushLog.Printf("Error checking lock file: %v", err)
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Compiling workflow..."))
		}
	}

	// Always recompile (hash check is for observability only)
	runPushLog.Printf("Always recompiling workflow: %s", absWorkflowPath)
	if err := recompileWorkflow(ctx, absWorkflowPath, verbose, approve); err != nil {
		runPushLog.Printf("Failed to recompile workflow: %v", err)
		return nil, fmt.Errorf("failed to recompile workflow: %w", err)
	}
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Workflow compiled successfully"))
	}
	runPushLog.Printf("Recompilation completed successfully")

	// Add the corresponding .lock.yml file
	if fileutil.FileExists(lockFilePath) {
		files[lockFilePath] = struct {
		}{}
		runPushLog.Printf("Added lock file: %s", lockFilePath)
	} else if verbose {
		runPushLog.Printf("Lock file not found after compilation: %s", lockFilePath)
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage("Lock file not found after compilation: "+lockFilePath))
	}

	// Collect transitive closure of imported files
	runPushLog.Printf("Starting import collection for %s", absWorkflowPath)
	if err := collectImports(absWorkflowPath, files, visited, verbose); err != nil {
		runPushLog.Printf("Failed to collect imports: %v", err)
		return nil, fmt.Errorf("failed to collect imports: %w", err)
	}
	runPushLog.Printf("Import collection completed")

	// Convert map to slice
	result := sliceutil.MapKeys(files)

	// Sort files for stable output
	sort.Strings(result)
	runPushLog.Printf("Sorted %d files for stable output", len(result))

	runPushLog.Printf("Collected %d files total", len(result))
	return result, nil
}

// recompileWorkflow compiles a workflow using CompileWorkflows
func recompileWorkflow(ctx context.Context, workflowPath string, verbose bool, approve bool) error {
	runPushLog.Printf("Recompiling workflow: %s", workflowPath)

	config := CompileConfig{
		MarkdownFiles:        []string{workflowPath},
		Verbose:              verbose,
		EngineOverride:       "",
		Validate:             true,
		Watch:                false,
		WorkflowDir:          "",
		NoEmit:               false,
		Purge:                false,
		TrialMode:            false,
		TrialLogicalRepoSlug: "",
		Strict:               false,
		Approve:              approve,
	}

	runPushLog.Printf("Compilation config: Validate=%v, NoEmit=%v", config.Validate, config.NoEmit)

	runPushLog.Printf("Starting compilation with CompileWorkflows")
	if _, err := CompileWorkflows(ctx, config); err != nil {
		runPushLog.Printf("Compilation failed: %v", err)
		return fmt.Errorf("compilation failed: %w", err)
	}

	runPushLog.Printf("Successfully recompiled workflow: %s", workflowPath)
	return nil
}

// checkLockFileStatus checks if a lock file is missing or outdated and returns status info.
// Note: This is used for display/warning purposes only. The actual compilation decision
// is made in collectWorkflowFiles, which always recompiles regardless of hash status.
type LockFileStatus struct {
	Missing  bool
	Outdated bool
	LockPath string
}

// checkLockFileStatus checks the status of a workflow's lock file.
// This function is used to determine whether to show warnings to the user about
// outdated lock files. It does NOT control whether recompilation happens -
// collectWorkflowFiles always recompiles regardless of the hash status.
func checkLockFileStatus(workflowPath string) (*LockFileStatus, error) {
	runPushLog.Printf("Checking lock file status for: %s", workflowPath)

	// Get absolute path for the workflow
	absWorkflowPath, err := filepath.Abs(workflowPath)
	if err != nil {
		runPushLog.Printf("Failed to get absolute path for %s: %v", workflowPath, err)
		return nil, fmt.Errorf("failed to get absolute path for workflow: %w", err)
	}
	runPushLog.Printf("Resolved absolute path: %s", absWorkflowPath)

	// Validate the absolute path
	absWorkflowPath, err = fileutil.ValidateAbsolutePath(absWorkflowPath)
	if err != nil {
		runPushLog.Printf("Invalid workflow path: %v", err)
		return nil, fmt.Errorf("invalid workflow path: %w", err)
	}

	lockFilePath := stringutil.MarkdownToLockFile(absWorkflowPath)
	runPushLog.Printf("Expected lock file path: %s", lockFilePath)
	status := &LockFileStatus{
		LockPath: lockFilePath,
	}

	// Check if lock file exists
	if _, err := os.Stat(lockFilePath); err != nil {
		if os.IsNotExist(err) {
			status.Missing = true
			runPushLog.Printf("Lock file missing: %s", lockFilePath)
			return status, nil
		}
		runPushLog.Printf("Error stating lock file: %v", err)
		return nil, fmt.Errorf("failed to stat lock file: %w", err)
	}
	runPushLog.Printf("Lock file exists: %s", lockFilePath)

	// Lock file exists - check frontmatter hash
	hashMismatch, err := checkFrontmatterHashMismatch(absWorkflowPath, lockFilePath)
	if err != nil {
		runPushLog.Printf("Error checking frontmatter hash: %v", err)
		// Treat hash check error as outdated to be safe
		status.Outdated = true
		runPushLog.Printf("Lock file considered outdated due to hash check error")
	} else if hashMismatch {
		status.Outdated = true
		runPushLog.Printf("Lock file outdated (frontmatter hash mismatch)")
	} else {
		runPushLog.Printf("Lock file is up-to-date (frontmatter hash matches)")
	}

	return status, nil
}

// collectImports recursively collects all imported files (transitive closure)
func collectImports(workflowPath string, files map[string]struct {
}, visited map[string]struct {
}, verbose bool) error {
	// Avoid processing the same file multiple times
	if setutil.Contains(visited, workflowPath) {
		runPushLog.Printf("Skipping already visited file: %s", workflowPath)
		return nil
	}
	visited[workflowPath] = struct {
	}{}

	runPushLog.Printf("Processing imports for: %s", workflowPath)

	// Read the workflow file
	content, err := os.ReadFile(workflowPath)
	if err != nil {
		runPushLog.Printf("Failed to read workflow file %s: %v", workflowPath, err)
		return fmt.Errorf("failed to read workflow file %s: %w", workflowPath, err)
	}
	runPushLog.Printf("Read %d bytes from %s", len(content), workflowPath)

	// Extract frontmatter to get imports field
	result, err := parser.ExtractFrontmatterFromContent(string(content))
	if err != nil {
		// No frontmatter is okay - might be a simple file
		runPushLog.Printf("No frontmatter in %s, skipping imports extraction: %v", workflowPath, err)
		return nil
	}
	runPushLog.Printf("Extracted frontmatter from %s", workflowPath)

	// Get imports from frontmatter
	importsField, exists := result.Frontmatter["imports"]
	if !exists {
		runPushLog.Printf("No imports field in %s", workflowPath)
		return nil
	}
	runPushLog.Printf("Found imports field in %s", workflowPath)

	// Parse imports field - can be array of strings or objects with path,
	// or an object with 'aw' subfield for agentic workflow paths.
	workflowDir := filepath.Dir(workflowPath)
	runPushLog.Printf("Workflow directory: %s", workflowDir)
	var imports []string

	// extractPathsFromArray extracts file paths from a []any import list.
	extractPathsFromArray := func(items []any) []string {
		var paths []string
		for i, item := range items {
			switch importItem := item.(type) {
			case string:
				runPushLog.Printf("Import %d: string format: %s", i, importItem)
				paths = append(paths, importItem)
			case map[string]any:
				if pathValue, hasPath := importItem["path"]; hasPath {
					if pathStr, ok := pathValue.(string); ok {
						runPushLog.Printf("Import %d: object format with path: %s", i, pathStr)
						paths = append(paths, pathStr)
					} else {
						runPushLog.Printf("Import %d: object has path but not string type", i)
					}
				} else {
					runPushLog.Printf("Import %d: object missing path field", i)
				}
			default:
				runPushLog.Printf("Import %d: unknown type: %T", i, importItem)
			}
		}
		return paths
	}

	switch v := importsField.(type) {
	case []any:
		runPushLog.Printf("Parsing imports as []any with %d items", len(v))
		imports = extractPathsFromArray(v)
	case []string:
		runPushLog.Printf("Parsing imports as []string with %d items", len(v))
		imports = v
	case map[string]any:
		// Object form: extract paths from the 'aw' subfield.
		// The 'apm-packages' subfield contains package names, not file paths.
		if awAny, hasAW := v["aw"]; hasAW {
			switch aw := awAny.(type) {
			case []any:
				runPushLog.Printf("Parsing imports.aw as []any with %d items", len(aw))
				imports = extractPathsFromArray(aw)
			case []string:
				runPushLog.Printf("Parsing imports.aw as []string with %d items", len(aw))
				imports = aw
			}
		}
	default:
		runPushLog.Printf("Imports field has unexpected type: %T", v)
	}

	runPushLog.Printf("Found %d imports in %s", len(imports), workflowPath)

	// Process each import
	for i, importPath := range imports {
		runPushLog.Printf("Processing import %d/%d: %s", i+1, len(imports), importPath)

		// Resolve the import path
		resolvedPath := resolveImportPath(importPath, workflowDir, importPathRunPushOpts)
		if resolvedPath == "" {
			runPushLog.Printf("Could not resolve import path: %s", importPath)
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage("Could not resolve import: "+importPath))
			}
			continue
		}
		runPushLog.Printf("Resolved import path: %s -> %s", importPath, resolvedPath)

		// Get absolute path
		var absImportPath string
		if filepath.IsAbs(resolvedPath) {
			absImportPath = resolvedPath
			runPushLog.Printf("Import path is absolute: %s", absImportPath)
		} else {
			absImportPath = filepath.Join(workflowDir, resolvedPath)
			runPushLog.Printf("Joined relative path: %s + %s = %s", workflowDir, resolvedPath, absImportPath)
		}

		// Check if file exists
		if _, err := os.Stat(absImportPath); err != nil {
			runPushLog.Printf("Import file not found: %s (error: %v)", absImportPath, err)
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage("Import file not found: "+absImportPath))
			}
			continue
		}
		runPushLog.Printf("Import file exists: %s", absImportPath)

		// Add the import file
		files[absImportPath] = struct {
		}{}
		runPushLog.Printf("Added import file: %s", absImportPath)

		// Recursively collect imports from this file
		runPushLog.Printf("Recursively collecting imports from: %s", absImportPath)
		if err := collectImports(absImportPath, files, visited, verbose); err != nil {
			runPushLog.Printf("Failed to recursively collect imports from %s: %v", absImportPath, err)
			return err
		}
	}

	runPushLog.Printf("Finished processing imports for: %s", workflowPath)
	return nil
}

// pushWorkflowFiles commits and pushes the workflow files to the repository
func pushWorkflowFiles(ctx context.Context, workflowName string, files []string, refOverride string, verbose bool) error {
	if ctx == nil {
		return errors.New("context is required")
	}
	runPushLog.Printf("Pushing %d files for workflow: %s", len(files), workflowName)
	runPushLog.Printf("Files to push: %v", files)

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Staging %d files for commit", len(files))))
		for _, file := range files {
			fmt.Fprintf(os.Stderr, "  - %s\n", file)
		}
	}

	// Stage all files
	gitArgs := append([]string{"add"}, files...)
	runPushLog.Printf("Executing git command: git %v", gitArgs)
	cmd := exec.CommandContext(ctx, "git", gitArgs...)
	if output, err := cmd.CombinedOutput(); err != nil {
		runPushLog.Printf("Failed to stage files: %v, output: %s", err, string(output))
		return fmt.Errorf("failed to stage files: %w\nOutput: %s", err, string(output))
	}
	runPushLog.Printf("Successfully staged %d files", len(files))

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Files staged successfully"))
	}

	// Check if there are any staged files in git (after we've staged our files)
	runPushLog.Printf("Checking staged files with git diff --cached --name-only")
	statusCmd := exec.CommandContext(ctx, "git", "diff", "--cached", "--name-only")
	statusOutput, err := statusCmd.CombinedOutput()
	if err != nil {
		runPushLog.Printf("Failed to check git status: %v, output: %s", err, string(statusOutput))
		return fmt.Errorf("failed to check git status: %w\nOutput: %s", err, string(statusOutput))
	}
	runPushLog.Printf("Git status output: %s", string(statusOutput))

	// Check if there are no staged changes (nothing to commit)
	if strings.TrimSpace(string(statusOutput)) == "" {
		runPushLog.Printf("No staged changes detected")
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage("No changes to commit"))
		}
		runPushLog.Print("No changes to commit")
		return nil
	}

	// Now that we know there are changes to commit, check that current branch matches --ref value if specified
	// This happens after we've determined there are actual changes, so we don't fail unnecessarily
	if refOverride != "" {
		runPushLog.Printf("Checking if current branch matches --ref value: %s", refOverride)
		currentBranch, err := getCurrentBranch()
		if err != nil {
			runPushLog.Printf("Failed to determine current branch: %v", err)
			return fmt.Errorf("failed to determine current branch: %w", err)
		}
		runPushLog.Printf("Current branch: %s", currentBranch)

		if currentBranch != refOverride {
			runPushLog.Printf("Current branch (%s) does not match --ref value (%s)", currentBranch, refOverride)
			return fmt.Errorf("--push requires the current branch (%s) to match the --ref value (%s). Switching branches is not supported. Please checkout the target branch first", currentBranch, refOverride)
		}

		runPushLog.Printf("Current branch matches --ref value: %s", currentBranch)
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Verified current branch matches --ref: "+currentBranch))
		}
	}

	// Get the list of staged files
	stagedFiles := strings.Split(strings.TrimSpace(string(statusOutput)), "\n")
	runPushLog.Printf("Found %d staged files: %v", len(stagedFiles), stagedFiles)

	// Check if there are staged files beyond what we just staged
	// Convert our files list to a map for quick lookup
	runPushLog.Printf("Building map of our files for comparison")
	ourFiles := make(map[string]struct {
	})
	for _, file := range files {
		// Normalize the path
		absPath, err := filepath.Abs(file)
		if err == nil {
			// Validate the absolute path
			validPath, validErr := fileutil.ValidateAbsolutePath(absPath)
			if validErr == nil {
				ourFiles[validPath] = struct {
				}{}
				runPushLog.Printf("Added to our files map: %s (absolute: %s)", file, validPath)
			} else {
				runPushLog.Printf("Failed to validate path for %s: %v", absPath, validErr)
			}
		} else {
			runPushLog.Printf("Failed to get absolute path for %s: %v", file, err)
		}
		ourFiles[file] = struct {
		}{}
		runPushLog.Printf("Added to our files map: %s", file)
	}

	// Check if there are any staged files that aren't in our list
	runPushLog.Printf("Checking for extra staged files not in our list")
	var extraStagedFiles []string
	for _, stagedFile := range stagedFiles {
		runPushLog.Printf("Checking staged file: %s", stagedFile)
		// Try both absolute and relative paths
		absStagedPath, err := filepath.Abs(stagedFile)
		if err == nil {
			// Validate the staged path
			validPath, validErr := fileutil.ValidateAbsolutePath(absStagedPath)
			if validErr == nil && setutil.Contains(ourFiles, validPath) {
				runPushLog.Printf("Staged file %s matches our file %s (absolute)", stagedFile, validPath)
				continue
			}
		}
		if setutil.Contains(ourFiles, stagedFile) {
			runPushLog.Printf("Staged file %s matches our file (relative)", stagedFile)
			continue
		}
		runPushLog.Printf("Extra staged file detected: %s", stagedFile)
		extraStagedFiles = append(extraStagedFiles, stagedFile)
	}

	// If there are extra staged files that we didn't stage, give up
	if len(extraStagedFiles) > 0 {
		runPushLog.Printf("Found %d extra staged files not in our list, refusing to proceed: %v", len(extraStagedFiles), extraStagedFiles)

		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, console.FormatErrorMessage("Cannot proceed: there are already staged files in git that are not part of this workflow"))
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Extra staged files:"))
		for _, file := range extraStagedFiles {
			fmt.Fprintf(os.Stderr, "  - %s\n", file)
		}
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Please commit or unstage these files before using --push"))
		fmt.Fprintln(os.Stderr, "")

		return errors.New("git has staged files not part of workflow - commit or unstage them before using --push")
	}
	runPushLog.Printf("No extra staged files detected - all staged files are part of our workflow")

	// Create commit message
	commitMessage := "Updated agentic workflow " + workflowName
	runPushLog.Printf("Creating commit with message: %s", commitMessage)

	// Show what will be committed and ask for confirmation using console helper
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Ready to commit and push the following files:"))
	for _, file := range files {
		fmt.Fprintf(os.Stderr, "  - %s\n", file)
	}
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintf(os.Stderr, console.FormatInfoMessage("Commit message: %s\n"), commitMessage)
	fmt.Fprintln(os.Stderr, "")

	// Ask for confirmation using console helper
	runPushLog.Printf("Requesting user confirmation for commit and push")
	confirmed, err := console.ConfirmAction(
		"Do you want to commit and push these changes?",
		"Yes, commit and push",
		"No, cancel",
	)
	if err != nil {
		runPushLog.Printf("Confirmation failed: %v", err)
		return fmt.Errorf("confirmation failed: %w", err)
	}

	if !confirmed {
		runPushLog.Print("Push cancelled by user")
		return errors.New("push cancelled by user")
	}
	runPushLog.Printf("User confirmed - proceeding with commit and push")

	// Commit the changes
	runPushLog.Printf("Executing git commit with message: %s", commitMessage)
	cmd = exec.CommandContext(ctx, "git", "commit", "-m", commitMessage)
	if output, err := cmd.CombinedOutput(); err != nil {
		runPushLog.Printf("Failed to commit: %v, output: %s", err, string(output))
		return fmt.Errorf("failed to commit changes: %w\nOutput: %s", err, string(output))
	}
	runPushLog.Printf("Commit successful")

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Changes committed successfully"))
	}

	// Push the changes
	runPushLog.Print("Pushing changes to remote")
	runPushLog.Printf("Executing git push")
	cmd = exec.CommandContext(ctx, "git", "push")
	if output, err := cmd.CombinedOutput(); err != nil {
		runPushLog.Printf("Failed to push: %v, output: %s", err, string(output))
		return fmt.Errorf("failed to push changes: %w\nOutput: %s", err, string(output))
	}
	runPushLog.Printf("Push to remote successful")

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Changes pushed to remote"))
	}

	runPushLog.Print("Push completed successfully")
	return nil
}

// checkFrontmatterHashMismatch checks if the frontmatter hash in the lock file
// matches the recomputed hash from the workflow file.
// Returns true if there's a mismatch (lock file is stale), false if they match.
// Note: This is used for logging/observability only. The compilation decision
// is made by collectWorkflowFiles, which always recompiles regardless of hash status.
func checkFrontmatterHashMismatch(workflowPath, lockFilePath string) (bool, error) {
	runPushLog.Printf("Checking frontmatter hash for %s", workflowPath)

	// Read lock file to extract existing hash
	lockContent, err := os.ReadFile(lockFilePath)
	if err != nil {
		return false, fmt.Errorf("failed to read lock file: %w", err)
	}

	// Extract hash from lock file using the workflow package function
	metadata, _, err := workflow.ExtractMetadataFromLockFile(string(lockContent))
	var existingHash string
	if err == nil && metadata != nil {
		existingHash = metadata.FrontmatterHash
	}
	if existingHash == "" {
		runPushLog.Print("No frontmatter-hash found in lock file")
		// No hash in lock file - consider it stale to regenerate with hash
		return true, nil
	}
	runPushLog.Printf("Existing hash from lock file: %s", existingHash)

	// Compute current hash from workflow file
	cache := parser.NewImportCache("")
	currentHash, err := parser.ComputeFrontmatterHashFromFile(workflowPath, cache)
	if err != nil {
		return false, fmt.Errorf("failed to compute frontmatter hash: %w", err)
	}
	runPushLog.Printf("Current hash from workflow: %s", currentHash)

	// Compare hashes
	mismatch := existingHash != currentHash
	if mismatch {
		runPushLog.Printf("Hash mismatch: existing=%s, current=%s", existingHash, currentHash)
	} else {
		runPushLog.Print("Hashes match")
	}

	return mismatch, nil
}
