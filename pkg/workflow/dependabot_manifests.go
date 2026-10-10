//go:build !js && !wasm

package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/fileutil"
	"github.com/github/gh-aw/pkg/sliceutil"
)

// npmInstallTimeout bounds how long "npm install --package-lock-only" is allowed to
// run. Without a timeout, a slow or unreachable npm registry can hang the command
// indefinitely, which is especially problematic in sandboxed/offline test environments.
const npmInstallTimeout = 60 * time.Second

// generateNpmManifests generates package.json and package-lock.json for npm dependencies
// detected in the workflows. It returns true if npm dependencies were found.
func (c *Compiler) generateNpmManifests(ctx context.Context, workflowDataList []*WorkflowData, workflowDir string, forceOverwrite bool) (bool, error) {
	npmDeps := c.collectNpmDependencies(workflowDataList)
	if len(npmDeps) == 0 {
		return false, nil
	}

	dependabotLog.Printf("Found %d unique npm dependencies", len(npmDeps))
	if c.verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Found %d npm dependencies in workflows", len(npmDeps))))
	}

	packageJSONPath := filepath.Join(workflowDir, "package.json")
	if err := c.generatePackageJSON(packageJSONPath, npmDeps, forceOverwrite); err != nil {
		return true, c.handleManifestGenerationError("package.json", err)
	}

	if err := c.generatePackageLock(ctx, workflowDir); err != nil {
		return true, c.handleManifestGenerationError("package-lock.json", err)
	}

	return true, nil
}

// generatePipManifests generates requirements.txt for pip dependencies detected in the
// workflows. It returns true if pip dependencies were found.
func (c *Compiler) generatePipManifests(workflowDataList []*WorkflowData, workflowDir string, forceOverwrite bool) (bool, error) {
	pipDeps := c.collectPipDependencies(workflowDataList)
	if len(pipDeps) == 0 {
		return false, nil
	}

	dependabotLog.Printf("Found %d unique pip dependencies", len(pipDeps))
	if c.verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Found %d pip dependencies in workflows", len(pipDeps))))
	}

	requirementsPath := filepath.Join(workflowDir, "requirements.txt")
	if err := c.generateRequirementsTxt(requirementsPath, pipDeps, forceOverwrite); err != nil {
		return true, c.handleManifestGenerationError("requirements.txt", err)
	}

	return true, nil
}

// generateGoManifests generates go.mod for Go dependencies detected in the workflows.
// It returns true if Go dependencies were found.
func (c *Compiler) generateGoManifests(workflowDataList []*WorkflowData, workflowDir string, forceOverwrite bool) (bool, error) {
	goDeps := c.collectGoDependencies(workflowDataList)
	if len(goDeps) == 0 {
		return false, nil
	}

	dependabotLog.Printf("Found %d unique go dependencies", len(goDeps))
	if c.verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Found %d go dependencies in workflows", len(goDeps))))
	}

	goModPath := filepath.Join(workflowDir, "go.mod")
	if err := c.generateGoMod(goModPath, goDeps, forceOverwrite); err != nil {
		return true, c.handleManifestGenerationError("go.mod", err)
	}

	return true, nil
}

// collectNpmDependencies collects all npm dependencies from workflow data
func (c *Compiler) collectNpmDependencies(workflowDataList []*WorkflowData) []NpmDependency {
	dependabotLog.Print("Collecting npm dependencies from workflows")

	depMap := make(map[string]string) // package name -> version (last seen)

	for _, workflowData := range workflowDataList {
		packages := extractNpxPackages(workflowData)
		for _, pkg := range packages {
			dep := parseNpmPackage(pkg)
			depMap[dep.Name] = dep.Version
		}
	}

	// Convert map to sorted slice
	var deps []NpmDependency
	for name, version := range depMap {
		deps = append(deps, NpmDependency{
			Name:    name,
			Version: version,
		})
	}

	// Sort by name for deterministic output
	slices.SortFunc(deps, func(a, b NpmDependency) int {
		switch {
		case a.Name < b.Name:
			return -1
		case a.Name > b.Name:
			return 1
		default:
			return 0
		}
	})

	dependabotLog.Printf("Collected %d unique dependencies", len(deps))
	return deps
}

// parseNpmPackage parses a package string like "@playwright/mcp@latest" into name and version
func parseNpmPackage(pkg string) NpmDependency {
	// Handle scoped packages (@org/package@version)
	if strings.HasPrefix(pkg, "@") {
		// Find the second @ for version separator
		parts := strings.Split(pkg, "@")
		if len(parts) >= 3 {
			// @org/package@version
			return NpmDependency{
				Name:    "@" + parts[1],
				Version: parts[2],
			}
		} else if len(parts) == 2 {
			// @org/package (no version)
			return NpmDependency{
				Name:    pkg,
				Version: "latest",
			}
		}
	}

	// Handle non-scoped packages (package@version)
	parts := strings.SplitN(pkg, "@", 2)
	if len(parts) == 2 {
		return NpmDependency{
			Name:    parts[0],
			Version: parts[1],
		}
	}

	// No version specified
	return NpmDependency{
		Name:    pkg,
		Version: "latest",
	}
}

// loadOrInitPackageJSON reads and parses an existing package.json at path, or returns a
// freshly initialized PackageJSON if the file does not exist.
func (c *Compiler) loadOrInitPackageJSON(path string) (PackageJSON, error) {
	var pkgJSON PackageJSON

	// Check if package.json already exists
	if _, err := os.Stat(path); err == nil {
		// File exists - merge dependencies
		dependabotLog.Print("Existing package.json found, merging dependencies")

		existingData, err := os.ReadFile(path)
		if err != nil {
			return pkgJSON, fmt.Errorf("failed to read existing package.json: %w", err)
		}

		if err := json.Unmarshal(existingData, &pkgJSON); err != nil {
			return pkgJSON, fmt.Errorf("failed to parse existing package.json: %w", err)
		}

		if c.verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Merging with existing package.json"))
		}
	} else {
		// New package.json
		dependabotLog.Print("Creating new package.json")
		pkgJSON = PackageJSON{
			Name:    "gh-aw-workflows-deps",
			Private: true,
			License: "MIT",
		}
	}

	return pkgJSON, nil
}

// generatePackageJSON creates or updates package.json with dependencies
func (c *Compiler) generatePackageJSON(path string, deps []NpmDependency, forceOverwrite bool) error {
	dependabotLog.Printf("Generating package.json at %s", path)

	pkgJSON, err := c.loadOrInitPackageJSON(path)
	if err != nil {
		return err
	}

	// Initialize dependencies map if nil
	if pkgJSON.Dependencies == nil {
		pkgJSON.Dependencies = make(map[string]string)
	}

	// Add/update dependencies
	for _, dep := range deps {
		pkgJSON.Dependencies[dep.Name] = dep.Version
	}

	// Write package.json with nice formatting
	jsonData, err := json.MarshalIndent(pkgJSON, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal package.json: %w", err)
	}

	// Add newline at end for POSIX compliance
	jsonData = append(jsonData, '\n')

	if err := os.WriteFile(path, jsonData, constants.FilePermPublic); err != nil {
		return fmt.Errorf("failed to write package.json: %w", err)
	}

	dependabotLog.Printf("Successfully wrote package.json with %d dependencies", len(pkgJSON.Dependencies))
	if c.verbose {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("Generated package.json with %d dependencies", len(pkgJSON.Dependencies))))
	}

	// Track the created file
	if c.fileTracker != nil {
		c.fileTracker.TrackCreated(path)
	}

	return nil
}

// generatePackageLock runs npm install --package-lock-only to create package-lock.json
func (c *Compiler) generatePackageLock(ctx context.Context, workflowDir string) error {
	dependabotLog.Printf("Generating package-lock.json in %s", workflowDir)

	if strings.TrimSpace(workflowDir) == "" {
		return fmt.Errorf("invalid workflow directory %q: must not be empty or whitespace", workflowDir)
	}
	absWorkflowDir, err := filepath.Abs(workflowDir)
	if err != nil {
		return fmt.Errorf("failed to resolve workflow directory %q: %w", workflowDir, err)
	}
	absWorkflowDir, err = fileutil.ValidateAbsolutePath(absWorkflowDir)
	if err != nil {
		return fmt.Errorf("invalid workflow directory %q: %w", workflowDir, err)
	}
	info, err := os.Stat(absWorkflowDir)
	if err != nil {
		return fmt.Errorf("failed to stat workflow directory %q: %w", absWorkflowDir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("workflow directory %q is not a directory", absWorkflowDir)
	}

	// Check if npm is available
	npmPath, err := fileutil.ResolveExecutablePath("npm")
	if err != nil {
		return errors.New("npm command not found - cannot generate package-lock.json. Install Node.js/npm to enable this feature")
	}

	if c.verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Running npm install --package-lock-only..."))
	}

	if err := runNpmInstallPackageLockOnly(ctx, npmPath, absWorkflowDir); err != nil {
		return err
	}

	lockfilePath := filepath.Join(absWorkflowDir, "package-lock.json")
	if _, err := os.Stat(lockfilePath); err != nil {
		return errors.New("package-lock.json was not created")
	}

	dependabotLog.Print("Successfully generated package-lock.json")
	if c.verbose {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Generated package-lock.json"))
	}

	// Track the created file
	if c.fileTracker != nil {
		c.fileTracker.TrackCreated(lockfilePath)
	}

	return nil
}

// runNpmInstallPackageLockOnly runs "npm install --package-lock-only --ignore-scripts"
// in workflowDir. The provided ctx is derived from the upstream caller (so cancellation,
// e.g. from a CLI interrupt, propagates) and is further bounded by npmInstallTimeout so
// a slow or unreachable npm registry cannot hang the compiler (or tests) indefinitely.
func runNpmInstallPackageLockOnly(ctx context.Context, npmPath, workflowDir string) error {
	// Run npm install --package-lock-only without lifecycle scripts.
	// The generated package.json can be influenced by workflow content, so explicitly
	// disable script execution to avoid running untrusted hooks while generating lockfiles.
	timeoutCtx, cancel := context.WithTimeout(ctx, npmInstallTimeout)
	defer cancel()
	// #nosec G204 -- npmPath is resolved by exec.LookPath and validated as an absolute path above;
	// the fixed arguments contain no user-controlled data.
	cmd := exec.CommandContext(timeoutCtx, npmPath, "install", "--package-lock-only", "--ignore-scripts")
	cmd.Dir = workflowDir
	cmd.Env = append(os.Environ(), "NPM_CONFIG_IGNORE_SCRIPTS=true")

	// Capture output for error reporting
	output, err := cmd.CombinedOutput()
	if err != nil {
		if ctx.Err() != nil {
			// The parent context was cancelled or reached its own deadline; report that
			// rather than misattributing it to the npm-specific timeout below.
			return fmt.Errorf("npm install --package-lock-only cancelled: %w\nOutput: %s", ctx.Err(), string(output))
		}
		if timeoutCtx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("npm install --package-lock-only timed out after %s: %w\nOutput: %s", npmInstallTimeout, err, string(output))
		}
		return fmt.Errorf("npm install --package-lock-only failed: %w\nOutput: %s", err, string(output))
	}
	return nil
}

// collectPipDependencies collects all pip dependencies from workflow data
func (c *Compiler) collectPipDependencies(workflowDataList []*WorkflowData) []PipDependency {
	dependabotLog.Print("Collecting pip dependencies from workflows")

	depMap := make(map[string]string) // package name -> version (last seen)

	for _, workflowData := range workflowDataList {
		packages := extractPipPackages(workflowData)
		for _, pkg := range packages {
			dep := parsePipPackage(pkg)
			depMap[dep.Name] = dep.Version
		}
	}

	// Convert map to sorted slice
	var deps []PipDependency
	for name, version := range depMap {
		deps = append(deps, PipDependency{
			Name:    name,
			Version: version,
		})
	}

	// Sort by name for deterministic output
	slices.SortFunc(deps, func(a, b PipDependency) int {
		switch {
		case a.Name < b.Name:
			return -1
		case a.Name > b.Name:
			return 1
		default:
			return 0
		}
	})

	dependabotLog.Printf("Collected %d unique pip dependencies", len(deps))
	return deps
}

// parsePipPackage parses a pip package string like "requests==2.28.0" into name and version
func parsePipPackage(pkg string) PipDependency {
	// Handle version specifiers (==, >=, <=, >, <, !=, ~=)
	for _, sep := range []string{"==", ">=", "<=", "!=", "~=", ">", "<"} {
		if idx := strings.Index(pkg, sep); idx > 0 {
			return PipDependency{
				Name:    pkg[:idx],
				Version: pkg[idx:], // Include the separator
			}
		}
	}

	// No version specified
	return PipDependency{
		Name:    pkg,
		Version: "",
	}
}

// mergeExistingRequirements reads an existing requirements.txt at path (if any) and adds any
// packages not already present in reqMap. It returns whether an existing file was found.
func (c *Compiler) mergeExistingRequirements(path string, reqMap map[string]string) (bool, error) {
	if _, err := os.Stat(path); err != nil {
		return false, nil
	}

	// File exists - merge dependencies
	dependabotLog.Print("Existing requirements.txt found, merging dependencies")

	existingData, err := os.ReadFile(path)
	if err != nil {
		return true, fmt.Errorf("failed to read existing requirements.txt: %w", err)
	}

	// Parse existing requirements
	lines := strings.SplitSeq(string(existingData), "\n")
	for line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		dep := parsePipPackage(line)
		// Only add if not already in our new deps
		if _, exists := reqMap[dep.Name]; !exists {
			reqMap[dep.Name] = dep.Version
		}
	}

	if c.verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Merging with existing requirements.txt"))
	}

	return true, nil
}

// generateRequirementsTxt creates or updates requirements.txt with dependencies
func (c *Compiler) generateRequirementsTxt(path string, deps []PipDependency, forceOverwrite bool) error {
	dependabotLog.Printf("Generating requirements.txt at %s", path)

	// Build requirements map for merging
	reqMap := make(map[string]string)
	for _, dep := range deps {
		reqMap[dep.Name] = dep.Version
	}

	if existed, err := c.mergeExistingRequirements(path, reqMap); err != nil {
		return err
	} else if !existed {
		dependabotLog.Print("Creating new requirements.txt")
	}

	// Sort dependencies by name
	sortedNames := sliceutil.SortedKeys(reqMap)

	// Build requirements.txt content
	var lines []string
	for _, name := range sortedNames {
		version := reqMap[name]
		if version != "" {
			lines = append(lines, name+version)
		} else {
			lines = append(lines, name)
		}
	}

	content := strings.Join(lines, "\n") + "\n"

	if err := os.WriteFile(path, []byte(content), constants.FilePermPublic); err != nil {
		return fmt.Errorf("failed to write requirements.txt: %w", err)
	}

	dependabotLog.Printf("Successfully wrote requirements.txt with %d dependencies", len(reqMap))
	if c.verbose {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("Generated requirements.txt with %d dependencies", len(reqMap))))
	}

	// Track the created file
	if c.fileTracker != nil {
		c.fileTracker.TrackCreated(path)
	}

	return nil
}

// collectGoDependencies collects all Go dependencies from workflow data
func (c *Compiler) collectGoDependencies(workflowDataList []*WorkflowData) []GoDependency {
	dependabotLog.Print("Collecting Go dependencies from workflows")

	depMap := make(map[string]string) // package path -> version (last seen)

	for _, workflowData := range workflowDataList {
		packages := extractGoPackages(workflowData)
		for _, pkg := range packages {
			dep := parseGoPackage(pkg)
			depMap[dep.Path] = dep.Version
		}
	}

	// Convert map to sorted slice
	var deps []GoDependency
	for path, version := range depMap {
		deps = append(deps, GoDependency{
			Path:    path,
			Version: version,
		})
	}

	// Sort by path for deterministic output
	slices.SortFunc(deps, func(a, b GoDependency) int {
		switch {
		case a.Path < b.Path:
			return -1
		case a.Path > b.Path:
			return 1
		default:
			return 0
		}
	})

	dependabotLog.Printf("Collected %d unique Go dependencies", len(deps))
	return deps
}

// parseGoPackage parses a Go package string like "github.com/user/repo@v1.2.3" into path and version
func parseGoPackage(pkg string) GoDependency {
	// Handle version separator @
	if idx := strings.Index(pkg, "@"); idx > 0 {
		return GoDependency{
			Path:    pkg[:idx],
			Version: pkg[idx+1:],
		}
	}

	// No version specified - will use latest
	return GoDependency{
		Path:    pkg,
		Version: "latest",
	}
}

// extractGoPackages extracts Go package paths from workflow data
func extractGoPackages(workflowData *WorkflowData) []string {
	return collectPackagesFromWorkflow(workflowData, extractGoFromCommands, "")
}

// extractGoFromCommands extracts Go package paths from command strings
func extractGoFromCommands(commands string) []string {
	extractor := PackageExtractor{
		CommandNames:        []string{"go"},
		RequiredSubcommands: []string{"install", "get"},
		TrimSuffixes:        "&|;",
	}
	return extractor.ExtractPackages(commands)
}

// loadOrInitGoModLines returns the initial lines for go.mod: either the preserved module/go
// version declarations from an existing file, or a freshly initialized module declaration.
func (c *Compiler) loadOrInitGoModLines(path string) ([]string, error) {
	var lines []string

	// Check if go.mod already exists
	if _, err := os.Stat(path); err == nil {
		// File exists - read and preserve module declaration
		dependabotLog.Print("Existing go.mod found, merging dependencies")

		existingData, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("failed to read existing go.mod: %w", err)
		}

		existingLines := strings.SplitSeq(string(existingData), "\n")
		// Keep module declaration and go version
		for line := range existingLines {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "module ") || strings.HasPrefix(trimmed, "go ") {
				lines = append(lines, line)
			}
		}

		if c.verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Merging with existing go.mod"))
		}
	} else {
		// New go.mod
		dependabotLog.Print("Creating new go.mod")
		lines = append(lines, "module github.com/github/gh-aw-workflows-deps")
		lines = append(lines, "")
		lines = append(lines, "go 1.21")
	}

	return lines, nil
}

// appendGoModRequireSection appends a require block for the given dependencies to lines,
// skipping dependencies without an explicit version.
func appendGoModRequireSection(lines []string, deps []GoDependency) []string {
	var entries []string
	for _, dep := range deps {
		version := dep.Version
		if version == "latest" || version == "" {
			// Skip dependencies without explicit versions - they should be added manually
			// or resolved using 'go get' or 'go mod tidy'. Using v0.0.0 as a placeholder
			// can cause issues with Go module resolution.
			dependabotLog.Printf("Skipping %s: no version specified (use 'go get %s@latest' to resolve)", dep.Path, dep.Path)
			continue
		}
		entries = append(entries, fmt.Sprintf("\t%s %s", dep.Path, version))
	}
	if len(entries) == 0 {
		return lines
	}

	lines = append(lines, "", "require (")
	lines = append(lines, entries...)
	lines = append(lines, ")")

	return lines
}

// generateGoMod creates or updates go.mod with dependencies
func (c *Compiler) generateGoMod(path string, deps []GoDependency, forceOverwrite bool) error {
	dependabotLog.Printf("Generating go.mod at %s", path)

	lines, err := c.loadOrInitGoModLines(path)
	if err != nil {
		return err
	}

	// Add require section if we have dependencies
	lines = appendGoModRequireSection(lines, deps)

	content := strings.Join(lines, "\n") + "\n"

	if err := os.WriteFile(path, []byte(content), constants.FilePermPublic); err != nil {
		return fmt.Errorf("failed to write go.mod: %w", err)
	}

	dependabotLog.Printf("Successfully wrote go.mod with %d dependencies", len(deps))
	if c.verbose {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("Generated go.mod with %d dependencies", len(deps))))
	}

	// Track the created file
	if c.fileTracker != nil {
		c.fileTracker.TrackCreated(path)
	}

	return nil
}
