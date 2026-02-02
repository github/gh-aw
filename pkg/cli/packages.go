package cli

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
)

var packagesLog = logger.New("cli:packages")

// Pre-compiled regexes for package processing (performance optimization)
var (
	includePattern = regexp.MustCompile(`^@include(\?)?\s+(.+)$`)
)

// WorkflowInfo represents metadata about an available workflow
type WorkflowInfo struct {
	ID          string `console:"header:ID"`
	Name        string `console:"header:Name"`
	Description string `console:"header:Description,omitempty"`
	Path        string `console:"-"` // Internal use only, not displayed
}

// InstallPackage installs agentic workflows from a GitHub repository
func InstallPackage(repoSpec string, verbose bool) error {
	packagesLog.Printf("Installing package: %s", repoSpec)
	if verbose {
		fmt.Fprintf(os.Stderr, "Installing package: %s\n", repoSpec)
	}

	// Parse repository specification (org/repo[@version])
	spec, err := parseRepoSpec(repoSpec)
	if err != nil {
		packagesLog.Printf("Failed to parse repository specification: %v", err)
		return fmt.Errorf("invalid repository specification: %w", err)
	}

	packagesLog.Printf("Parsed repo spec: slug=%s, version=%s", spec.RepoSlug, spec.Version)

	if verbose {
		fmt.Fprintf(os.Stderr, "Repository: %s\n", spec.RepoSlug)
		if spec.Version != "" {
			fmt.Fprintf(os.Stderr, "Version: %s\n", spec.Version)
		} else {
			fmt.Fprintf(os.Stderr, "Version: main (default)\n")
		}
	}

	// Get global packages directory
	packagesDir, err := getPackagesDir()
	if err != nil {
		packagesLog.Printf("Failed to determine packages directory: %v", err)
		return fmt.Errorf("failed to determine packages directory: %w", err)
	}

	packagesLog.Printf("Using packages directory: %s", packagesDir)
	if verbose {
		fmt.Fprintf(os.Stderr, "Installing to global packages directory: %s\n", packagesDir)
	}

	// Create packages directory
	if err := os.MkdirAll(packagesDir, 0755); err != nil {
		return fmt.Errorf("failed to create packages directory: %w", err)
	}

	// Create target directory for this repository
	targetDir := filepath.Join(packagesDir, spec.RepoSlug)
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return fmt.Errorf("failed to create package directory: %w", err)
	}

	// Check if package already exists
	if _, err := os.Stat(targetDir); err == nil {
		entries, err := os.ReadDir(targetDir)
		if err == nil && len(entries) > 0 {
			packagesLog.Printf("Package %s already exists. Updating...\n", spec.RepoSlug)
			// Remove existing content
			if err := os.RemoveAll(targetDir); err != nil {
				return fmt.Errorf("failed to remove existing package: %w", err)
			}
			if err := os.MkdirAll(targetDir, 0755); err != nil {
				return fmt.Errorf("failed to recreate package directory: %w", err)
			}
		}
	}

	// Download workflows from the repository
	packagesLog.Printf("Downloading workflows to: %s", targetDir)
	if err := downloadWorkflows(spec.RepoSlug, spec.Version, targetDir, verbose); err != nil {
		packagesLog.Printf("Failed to download workflows: %v", err)
		return fmt.Errorf("failed to download workflows: %w", err)
	}

	packagesLog.Printf("Successfully installed package: %s", spec.RepoSlug)
	return nil
}

// downloadWorkflows downloads all .md files from the workflows directory of a GitHub repository
func downloadWorkflows(repo, version, targetDir string, verbose bool) error {
	packagesLog.Printf("Downloading workflows from %s (version: %s) to %s", repo, version, targetDir)
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Downloading workflows from %s/workflows...", repo)))
	}

	// Create a temporary directory for cloning
	tempDir, err := os.MkdirTemp("", "gh-aw-clone-*")
	if err != nil {
		packagesLog.Printf("Failed to create temp directory: %v", err)
		return fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tempDir)
	packagesLog.Printf("Created temporary directory: %s", tempDir)

	isSHA := IsCommitSHA(version)

	// Prepare fallback git clone arguments
	// Support enterprise GitHub domains
	githubHost := getGitHubHost()

	repoURL := fmt.Sprintf("%s/%s", githubHost, repo)
	var gitArgs []string
	if isSHA {
		gitArgs = []string{"clone", repoURL, tempDir}
	} else {
		gitArgs = []string{"clone", "--depth", "1", repoURL, tempDir}
		if version != "" && version != "main" {
			gitArgs = append(gitArgs, "--branch", version)
		}
	}

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Cloning repository..."))
	}

	// Use helper to execute gh CLI with git fallback
	_, stderr, err := ghExecOrFallback(
		"git",
		gitArgs,
		[]string{"GIT_TERMINAL_PROMPT=0"}, // Prevent credential prompts
	)
	if err != nil {
		return fmt.Errorf("failed to clone repository: %w (output: %s)", err, stderr)
	}

	// If a specific SHA was requested, checkout that commit
	if isSHA {
		stdout, stderr, err := ghExecOrFallback(
			"git",
			[]string{"-C", tempDir, "checkout", version},
			nil,
		)
		if err != nil {
			return fmt.Errorf("failed to checkout commit %s: %w (output: %s)", version, err, stderr+stdout)
		}
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Checked out commit: %s", version)))
		}
	}

	// Get the current commit SHA from the cloned repository
	stdout, stderr, err := ghExecOrFallback(
		"git",
		[]string{"-C", tempDir, "rev-parse", "HEAD"},
		nil,
	)
	if err != nil {
		return fmt.Errorf("failed to get commit SHA: %w (output: %s)", err, stderr+stdout)
	}
	commitSHA := strings.TrimSpace(stdout)

	// Validate that we're at the expected commit if a specific SHA was requested
	if isSHA && commitSHA != version {
		return fmt.Errorf("cloned repository is at commit %s, but expected %s", commitSHA, version)
	}

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Repository commit SHA: %s", commitSHA)))
	}

	// Copy all .md files from temp directory to target
	if err := copyMarkdownFiles(tempDir, targetDir, verbose); err != nil {
		return err
	}

	// Store the commit SHA in a metadata file for later retrieval
	metadataPath := filepath.Join(targetDir, ".commit-sha")
	if err := os.WriteFile(metadataPath, []byte(commitSHA), 0644); err != nil {
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to write commit SHA metadata: %v", err)))
		}
	}

	return nil
}

// copyMarkdownFiles recursively copies markdown files from source to target directory
func copyMarkdownFiles(sourceDir, targetDir string, verbose bool) error {
	return filepath.Walk(sourceDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip if not a markdown file
		if info.IsDir() || !strings.HasSuffix(info.Name(), ".md") {
			return nil
		}

		// Get relative path from source directory
		relPath, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return fmt.Errorf("failed to get relative path: %w", err)
		}

		// Create target file path
		targetFile := filepath.Join(targetDir, relPath)

		// Create target directory if needed
		targetFileDir := filepath.Dir(targetFile)
		if err := os.MkdirAll(targetFileDir, 0755); err != nil {
			return fmt.Errorf("failed to create target directory %s: %w", targetFileDir, err)
		}

		// Copy file
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Copying: %s -> %s", relPath, targetFile)))
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed to read source file %s: %w", path, err)
		}

		if err := os.WriteFile(targetFile, content, 0644); err != nil {
			return fmt.Errorf("failed to write target file %s: %w", targetFile, err)
		}

		return nil
	})
}

// Package represents an installed package
type Package struct {
	Name      string
	Path      string
	Workflows []string
	CommitSHA string
}

// WorkflowSourceInfo contains information about where a workflow was found
type WorkflowSourceInfo struct {
	PackagePath string
	SourcePath  string
	CommitSHA   string // The actual commit SHA used when the package was installed
}

// isValidWorkflowFile checks if a markdown file is a valid workflow by attempting to parse its frontmatter.
// It validates that the file has proper YAML frontmatter delimited by "---" and contains the required "on" field.
//
// Parameters:
//   - filePath: Absolute or relative path to the markdown file to validate
//
// Returns:
//   - true if the file is a valid workflow (has parseable frontmatter with an "on" field)
//   - false if the file cannot be read, has invalid YAML, or lacks the required "on" field
func isValidWorkflowFile(filePath string) bool {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return false
	}

	// Try to extract frontmatter - a valid workflow should have parseable frontmatter
	result, err := parser.ExtractFrontmatterFromContent(string(content))
	if err != nil {
		return false
	}

	// A valid workflow must have frontmatter with at least an "on" field
	// Files without frontmatter or with empty frontmatter are not workflows
	if len(result.Frontmatter) == 0 {
		return false
	}

	// Check for the presence of the "on" field which is required for workflows
	if _, hasOn := result.Frontmatter["on"]; !hasOn {
		return false
	}

	return true
}

// listWorkflowsInPackage lists all available workflows in an installed package
func listWorkflowsInPackage(repoSlug string, verbose bool) ([]string, error) {
	workflows, err := listWorkflowsWithMetadata(repoSlug, verbose)
	if err != nil {
		return nil, err
	}

	// Convert WorkflowInfo to string paths for backwards compatibility
	paths := make([]string, len(workflows))
	for i, wf := range workflows {
		paths[i] = wf.Path
	}
	return paths, nil
}

// listWorkflowsWithMetadata lists all available workflows in an installed package with metadata
func listWorkflowsWithMetadata(repoSlug string, verbose bool) ([]WorkflowInfo, error) {
	packagesLog.Printf("Listing workflows in package: %s", repoSlug)

	packagesDir, err := getPackagesDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get packages directory: %w", err)
	}

	packagePath := filepath.Join(packagesDir, repoSlug)

	// Check if package exists
	if _, err := os.Stat(packagePath); os.IsNotExist(err) {
		return nil, fmt.Errorf("package not found: %s", repoSlug)
	}

	var workflows []WorkflowInfo

	// Walk through the package directory to find all .md files
	err = filepath.Walk(packagePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip directories and non-markdown files
		if info.IsDir() || !strings.HasSuffix(info.Name(), ".md") {
			return nil
		}

		// Skip metadata files
		if info.Name() == ".commit-sha" {
			return nil
		}

		// Check if this is a valid workflow file
		if !isValidWorkflowFile(path) {
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Skipping non-workflow file: %s", path)))
			}
			return nil
		}

		// Get relative path from package directory
		relPath, err := filepath.Rel(packagePath, path)
		if err != nil {
			return err
		}

		// Extract workflow ID (filename without extension)
		workflowID := normalizeWorkflowID(path)

		// For workflows in workflows/ directory, use simplified ID
		if strings.HasPrefix(relPath, "workflows/") {
			workflowID = normalizeWorkflowID(strings.TrimPrefix(relPath, "workflows/"))
		}

		// Extract name and description from frontmatter
		name, description := extractWorkflowMetadata(path)

		// Add to list with metadata
		workflows = append(workflows, WorkflowInfo{
			ID:          workflowID,
			Name:        name,
			Description: description,
			Path:        relPath,
		})

		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Found workflow: %s (ID: %s, Name: %s)", relPath, workflowID, name)))
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to scan package directory: %w", err)
	}

	packagesLog.Printf("Found %d workflows in package %s", len(workflows), repoSlug)
	return workflows, nil
}

// extractWorkflowMetadata extracts name and description from a workflow file's frontmatter
func extractWorkflowMetadata(filePath string) (name string, description string) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return "", ""
	}

	result, err := parser.ExtractFrontmatterFromContent(string(content))
	if err != nil {
		return "", ""
	}

	// Try to get name from frontmatter
	if nameVal, ok := result.Frontmatter["name"]; ok {
		if nameStr, ok := nameVal.(string); ok {
			name = nameStr
		}
	}

	// If no name in frontmatter, try to extract from first H1 heading
	if name == "" {
		lines := strings.Split(result.Markdown, "\n")
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "# ") {
				name = strings.TrimSpace(trimmed[2:])
				break
			}
		}
	}

	// If still no name, use filename as fallback (handled by caller)
	if name == "" {
		name = normalizeWorkflowID(filePath)
	}

	// Try to get description from frontmatter
	if descVal, ok := result.Frontmatter["description"]; ok {
		if descStr, ok := descVal.(string); ok {
			description = descStr
		}
	}

	return name, description
}

// findWorkflowInPackageForRepo searches for a workflow in installed packages
func findWorkflowInPackageForRepo(workflow *WorkflowSpec, verbose bool) ([]byte, *WorkflowSourceInfo, error) {

	packagesDir, err := getPackagesDir()
	if err != nil {
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to get packages directory: %v", err)))
		}
		return nil, nil, fmt.Errorf("failed to get packages directory: %w", err)
	}

	if _, err := os.Stat(packagesDir); os.IsNotExist(err) {
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("No packages directory found at %s", packagesDir)))
		}
		return nil, nil, fmt.Errorf("no packages directory found")
	}

	// Handle local workflows (starting with "./")
	if strings.HasPrefix(workflow.WorkflowPath, "./") {
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Searching local filesystem for workflow: %s", workflow.WorkflowPath)))
		}

		// For local workflows, use current directory as packagePath
		packagePath := "."
		workflowFile := workflow.WorkflowPath

		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Looking for local workflow: %s", workflowFile)))
		}

		content, err := os.ReadFile(workflowFile)
		if err != nil {
			return nil, nil, fmt.Errorf("local workflow '%s' not found: %w", workflow.WorkflowPath, err)
		}

		sourceInfo := &WorkflowSourceInfo{
			PackagePath: packagePath,
			SourcePath:  workflowFile,
			CommitSHA:   "", // Local workflows don't have commit SHA
		}

		return content, sourceInfo, nil
	}

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Searching packages in %s for workflow: %s", packagesDir, workflow.WorkflowPath)))
	}

	// Check if workflow name contains org/repo prefix
	// Fully qualified name: org/repo/workflow_name
	packagePath := filepath.Join(packagesDir, workflow.RepoSlug)
	workflowFile := filepath.Join(packagePath, workflow.WorkflowPath)

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Looking for qualified workflow: %s", workflowFile)))
	}

	content, err := os.ReadFile(workflowFile)
	if err != nil {
		// If the initial path failed and it starts with "workflows/",
		// try with ".github/workflows/" prefix as a fallback
		if strings.HasPrefix(workflow.WorkflowPath, "workflows/") {
			// Extract just the filename part after "workflows/"
			filenamePart := strings.TrimPrefix(workflow.WorkflowPath, "workflows/")
			fallbackPath := filepath.Join(".github", "workflows", filenamePart)
			fallbackWorkflowFile := filepath.Join(packagePath, fallbackPath)

			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Initial path not found, trying fallback: %s", fallbackWorkflowFile)))
			}

			var fallbackErr error
			content, fallbackErr = os.ReadFile(fallbackWorkflowFile)
			if fallbackErr == nil {
				// Success with fallback path, update workflowFile to the fallback path
				workflowFile = fallbackWorkflowFile
				if verbose {
					fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Found workflow at fallback path: %s", fallbackWorkflowFile)))
				}
			} else {
				// Both attempts failed, return the original error
				return nil, nil, fmt.Errorf("workflow '%s' not found in repo '%s'", workflow.WorkflowPath, workflow.RepoSlug)
			}
		} else {
			// Not a workflows/ path, return the original error
			return nil, nil, fmt.Errorf("workflow '%s' not found in repo '%s'", workflow.WorkflowPath, workflow.RepoSlug)
		}
	}

	// Try to read the commit SHA from metadata file
	var commitSHA string
	metadataPath := filepath.Join(packagePath, ".commit-sha")
	if shaBytes, err := os.ReadFile(metadataPath); err == nil {
		commitSHA = strings.TrimSpace(string(shaBytes))
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Found commit SHA from metadata: %s", commitSHA)))
		}
	} else if verbose {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Could not read commit SHA metadata: %v", err)))
	}

	sourceInfo := &WorkflowSourceInfo{
		PackagePath: packagePath,
		SourcePath:  workflowFile,
		CommitSHA:   commitSHA,
	}

	return content, sourceInfo, nil

}

// collectPackageIncludeDependencies collects dependencies for package-based workflows
func collectPackageIncludeDependencies(content, packagePath string, verbose bool) ([]IncludeDependency, error) {
	var dependencies []IncludeDependency
	seen := make(map[string]bool)

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Collecting package dependencies from: %s", packagePath)))
	}

	err := collectPackageIncludesRecursive(content, packagePath, &dependencies, seen, verbose)
	return dependencies, err
}

// collectPackageIncludesRecursive recursively processes @include directives in package content
func collectPackageIncludesRecursive(content, baseDir string, dependencies *[]IncludeDependency, seen map[string]bool, verbose bool) error {
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := scanner.Text()
		if matches := includePattern.FindStringSubmatch(line); matches != nil {
			isOptional := matches[1] == "?"
			includePath := strings.TrimSpace(matches[2])

			// Handle section references (file.md#Section)
			var filePath string
			if strings.Contains(includePath, "#") {
				parts := strings.SplitN(includePath, "#", 2)
				filePath = parts[0]
			} else {
				filePath = includePath
			}

			// Resolve the full source path relative to base directory
			fullSourcePath := filepath.Join(baseDir, filePath)

			// Skip if we've already processed this file
			if seen[fullSourcePath] {
				continue
			}
			seen[fullSourcePath] = true

			// Add dependency
			dep := IncludeDependency{
				SourcePath: fullSourcePath,
				TargetPath: filePath, // Keep relative path for target
				IsOptional: isOptional,
			}
			*dependencies = append(*dependencies, dep)

			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Found include dependency: %s -> %s", fullSourcePath, filePath)))
			}

			// Read the included file and process its includes recursively
			includedContent, err := os.ReadFile(fullSourcePath)
			if err != nil {
				if verbose {
					fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Could not read include file %s: %v", fullSourcePath, err)))
				}
				continue
			}

			// Extract markdown content from the included file
			markdownContent, err := parser.ExtractMarkdownContent(string(includedContent))
			if err != nil {
				if verbose {
					fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Could not extract markdown from %s: %v", fullSourcePath, err)))
				}
				continue
			}

			// Recursively process includes in the included file
			includedDir := filepath.Dir(fullSourcePath)
			if err := collectPackageIncludesRecursive(markdownContent, includedDir, dependencies, seen, verbose); err != nil {
				if verbose {
					fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Error processing includes in %s: %v", fullSourcePath, err)))
				}
			}
		}
	}

	return scanner.Err()
}

// copyIncludeDependenciesFromPackageWithForce copies include dependencies from package filesystem with force option
func copyIncludeDependenciesFromPackageWithForce(dependencies []IncludeDependency, githubWorkflowsDir string, verbose bool, force bool, tracker *FileTracker) error {
	for _, dep := range dependencies {
		// Create the target path in .github/workflows
		targetPath := filepath.Join(githubWorkflowsDir, dep.TargetPath)

		// Create target directory if it doesn't exist
		targetDir := filepath.Dir(targetPath)
		if err := os.MkdirAll(targetDir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", targetDir, err)
		}

		// Read source content from package
		sourceContent, err := os.ReadFile(dep.SourcePath)
		if err != nil {
			if dep.IsOptional {
				// For optional includes, just show an informational message and skip
				if verbose {
					fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Optional include file not found: %s (you can create this file to configure the workflow)", dep.TargetPath)))
				}
				continue
			}
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to read include file %s: %v", dep.SourcePath, err)))
			continue
		}

		// Check if target file already exists
		fileExists := false
		if existingContent, err := os.ReadFile(targetPath); err == nil {
			fileExists = true
			// File exists, compare contents
			if string(existingContent) == string(sourceContent) {
				// Contents are the same, skip
				if verbose {
					fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Include file %s already exists with same content, skipping", dep.TargetPath)))
				}
				continue
			}

			// Contents are different
			if !force {
				fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Include file %s already exists with different content, skipping (use --force to overwrite)", dep.TargetPath)))
				continue
			}

			// Force is enabled, overwrite
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Overwriting existing include file: %s", dep.TargetPath)))
		}

		// Track the file based on whether it existed before (if tracker is available)
		if tracker != nil {
			if fileExists {
				tracker.TrackModified(targetPath)
			} else {
				tracker.TrackCreated(targetPath)
			}
		}

		// Write to target
		if err := os.WriteFile(targetPath, sourceContent, 0644); err != nil {
			return fmt.Errorf("failed to write include file %s: %w", targetPath, err)
		}

		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Copied include file: %s -> %s", dep.SourcePath, targetPath)))
		}
	}

	return nil
}

// IncludeDependency represents a file dependency from @include directives
type IncludeDependency struct {
	SourcePath string // Path in the source (local)
	TargetPath string // Relative path where it should be copied in .github/workflows
	IsOptional bool   // Whether this is an optional include (@include?)
}

// discoverWorkflowsInPackage discovers all workflow files in an installed package
// Returns a list of WorkflowSpec for each discovered workflow
func discoverWorkflowsInPackage(repoSlug, version string, verbose bool) ([]*WorkflowSpec, error) {
	packagesLog.Printf("Discovering workflows in package: %s (version: %s)", repoSlug, version)

	packagesDir, err := getPackagesDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get packages directory: %w", err)
	}

	packagePath := filepath.Join(packagesDir, repoSlug)
	if _, err := os.Stat(packagePath); os.IsNotExist(err) {
		return nil, fmt.Errorf("package not found: %s (try installing it first)", repoSlug)
	}

	var workflows []*WorkflowSpec

	// Walk through the package directory and find all .md files
	err = filepath.Walk(packagePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip if not a markdown file
		if info.IsDir() || !strings.HasSuffix(info.Name(), ".md") {
			return nil
		}

		// Check if this is a valid workflow file
		if !isValidWorkflowFile(path) {
			if verbose {
				fmt.Fprintf(os.Stderr, "Skipping non-workflow file: %s\n", path)
			}
			return nil
		}

		// Get relative path from package root
		relPath, err := filepath.Rel(packagePath, path)
		if err != nil {
			return fmt.Errorf("failed to get relative path: %w", err)
		}

		// Create workflow spec
		spec := &WorkflowSpec{
			RepoSpec: RepoSpec{
				RepoSlug: repoSlug,
				Version:  version,
			},
			WorkflowPath: relPath,
			WorkflowName: normalizeWorkflowID(relPath),
		}

		workflows = append(workflows, spec)

		if verbose {
			fmt.Fprintf(os.Stderr, "Discovered workflow: %s\n", spec.String())
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to walk package directory: %w", err)
	}

	packagesLog.Printf("Discovered %d workflows in package %s", len(workflows), repoSlug)
	return workflows, nil
}

// ExtractWorkflowDescription extracts the description field from workflow content string
func ExtractWorkflowDescription(content string) string {
	result, err := parser.ExtractFrontmatterFromContent(content)
	if err != nil {
		return ""
	}

	if desc, ok := result.Frontmatter["description"]; ok {
		if descStr, ok := desc.(string); ok {
			return descStr
		}
	}

	return ""
}

// ExtractWorkflowEngine extracts the engine field from workflow content string.
// Supports both string format (engine: copilot) and nested format (engine: { id: copilot }).
func ExtractWorkflowEngine(content string) string {
	result, err := parser.ExtractFrontmatterFromContent(content)
	if err != nil {
		return ""
	}

	if engine, ok := result.Frontmatter["engine"]; ok {
		// Handle string format: engine: copilot
		if engineStr, ok := engine.(string); ok {
			return engineStr
		}
		// Handle nested format: engine: { id: copilot }
		if engineMap, ok := engine.(map[string]any); ok {
			if id, ok := engineMap["id"]; ok {
				if idStr, ok := id.(string); ok {
					return idStr
				}
			}
		}
	}

	return ""
}

// ExtractWorkflowDescriptionFromFile extracts the description field from a workflow file
func ExtractWorkflowDescriptionFromFile(filePath string) string {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return ""
	}

	return ExtractWorkflowDescription(string(content))
}
