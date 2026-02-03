package workflow

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/goccy/go-yaml"
)

var dependabotLog = logger.New("workflow:dependabot")

// PackageJSON represents the structure of a package.json file
type PackageJSON struct {
	Name            string            `json:"name"`
	Private         bool              `json:"private"`
	License         string            `json:"license,omitempty"`
	Dependencies    map[string]string `json:"dependencies,omitempty"`
	DevDependencies map[string]string `json:"devDependencies,omitempty"`
}

// DependabotConfig represents the structure of .github/dependabot.yml
type DependabotConfig struct {
	Version int                     `yaml:"version"`
	Updates []DependabotUpdateEntry `yaml:"updates"`
}

// DependabotUpdateEntry represents a single update configuration in dependabot.yml
type DependabotUpdateEntry struct {
	PackageEcosystem string `yaml:"package-ecosystem"`
	Directory        string `yaml:"directory"`
	Schedule         struct {
		Interval string `yaml:"interval"`
	} `yaml:"schedule"`
}

// NpmDependency represents a parsed npm package with version
type NpmDependency struct {
	Name    string
	Version string // semver range or specific version
}

// PipDependency represents a parsed pip package with version
type PipDependency struct {
	Name    string
	Version string // version specifier (e.g., ==1.0.0, >=2.0.0)
}

// GoDependency represents a parsed Go package
type GoDependency struct {
	Path    string // import path (e.g., github.com/user/repo)
	Version string // version or pseudo-version
}

// GenerateDependabotManifests generates manifest files and dependabot.yml for detected dependencies
func (c *Compiler) GenerateDependabotManifests(workflowDataList []*WorkflowData, workflowDir string, forceOverwrite bool) error {
	dependabotLog.Print("Starting Dependabot manifest generation")

	// Track which ecosystems have dependencies
	ecosystems := make(map[string]bool)

	// Collect npm dependencies
	npmDeps := c.collectNpmDependencies(workflowDataList)
	if len(npmDeps) > 0 {
		ecosystems["npm"] = true
		dependabotLog.Printf("Found %d unique npm dependencies", len(npmDeps))
		if c.verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Found %d npm dependencies in workflows", len(npmDeps))))
		}

		// Generate package.json
		packageJSONPath := filepath.Join(workflowDir, "package.json")
		if err := c.generatePackageJSON(packageJSONPath, npmDeps, forceOverwrite); err != nil {
			if c.strictMode {
				return fmt.Errorf("failed to generate package.json: %w", err)
			}
			c.IncrementWarningCount()
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to generate package.json: %v", err)))
		} else {
			// Generate package-lock.json
			if err := c.generatePackageLock(workflowDir); err != nil {
				if c.strictMode {
					return fmt.Errorf("failed to generate package-lock.json: %w", err)
				}
				c.IncrementWarningCount()
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to generate package-lock.json: %v", err)))
			}
		}
	}

	// Collect pip dependencies
	pipDeps := c.collectPipDependencies(workflowDataList)
	if len(pipDeps) > 0 {
		ecosystems["pip"] = true
		dependabotLog.Printf("Found %d unique pip dependencies", len(pipDeps))
		if c.verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Found %d pip dependencies in workflows", len(pipDeps))))
		}

		// Generate requirements.txt
		requirementsPath := filepath.Join(workflowDir, "requirements.txt")
		if err := c.generateRequirementsTxt(requirementsPath, pipDeps, forceOverwrite); err != nil {
			if c.strictMode {
				return fmt.Errorf("failed to generate requirements.txt: %w", err)
			}
			c.IncrementWarningCount()
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to generate requirements.txt: %v", err)))
		}
	}

	// Collect go dependencies
	goDeps := c.collectGoDependencies(workflowDataList)
	if len(goDeps) > 0 {
		ecosystems["gomod"] = true
		dependabotLog.Printf("Found %d unique go dependencies", len(goDeps))
		if c.verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Found %d go dependencies in workflows", len(goDeps))))
		}

		// Generate go.mod
		goModPath := filepath.Join(workflowDir, "go.mod")
		if err := c.generateGoMod(goModPath, goDeps, forceOverwrite); err != nil {
			if c.strictMode {
				return fmt.Errorf("failed to generate go.mod: %w", err)
			}
			c.IncrementWarningCount()
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to generate go.mod: %v", err)))
		}
	}

	// If no dependencies found at all, skip
	if len(ecosystems) == 0 {
		dependabotLog.Print("No dependencies found, skipping manifest generation")
		if c.verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage("No dependencies detected in workflows, skipping Dependabot manifest generation"))
		}
		return nil
	}

	// Generate dependabot.yml with all detected ecosystems
	dependabotPath := filepath.Join(filepath.Dir(workflowDir), "dependabot.yml")
	if err := c.generateDependabotConfig(dependabotPath, ecosystems, forceOverwrite); err != nil {
		if c.strictMode {
			return fmt.Errorf("failed to generate dependabot.yml: %w", err)
		}
		c.IncrementWarningCount()
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to generate dependabot.yml: %v", err)))
	}

	if c.verbose {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Successfully generated Dependabot manifests"))
	}

	return nil
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
	sort.Slice(deps, func(i, j int) bool {
		return deps[i].Name < deps[j].Name
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

// generatePackageJSON creates or updates package.json with dependencies
func (c *Compiler) generatePackageJSON(path string, deps []NpmDependency, forceOverwrite bool) error {
	dependabotLog.Printf("Generating package.json at %s", path)

	var pkgJSON PackageJSON

	// Check if package.json already exists
	if _, err := os.Stat(path); err == nil {
		// File exists - merge dependencies
		dependabotLog.Print("Existing package.json found, merging dependencies")

		existingData, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed to read existing package.json: %w", err)
		}

		if err := json.Unmarshal(existingData, &pkgJSON); err != nil {
			return fmt.Errorf("failed to parse existing package.json: %w", err)
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

	if err := os.WriteFile(path, jsonData, 0644); err != nil {
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
func (c *Compiler) generatePackageLock(workflowDir string) error {
	dependabotLog.Printf("Generating package-lock.json in %s", workflowDir)

	// Check if npm is available
	npmPath, err := exec.LookPath("npm")
	if err != nil {
		return fmt.Errorf("npm command not found - cannot generate package-lock.json. Install Node.js/npm to enable this feature")
	}

	if c.verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Running npm install --package-lock-only..."))
	}

	// Run npm install --package-lock-only
	cmd := exec.Command(npmPath, "install", "--package-lock-only")
	cmd.Dir = workflowDir

	// Capture output for error reporting
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("npm install --package-lock-only failed: %w\nOutput: %s", err, string(output))
	}

	lockfilePath := filepath.Join(workflowDir, "package-lock.json")
	if _, err := os.Stat(lockfilePath); err != nil {
		return fmt.Errorf("package-lock.json was not created")
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

// generateDependabotConfig creates or updates .github/dependabot.yml
func (c *Compiler) generateDependabotConfig(path string, ecosystems map[string]bool, forceOverwrite bool) error {
	dependabotLog.Printf("Generating dependabot.yml at %s", path)

	var config DependabotConfig

	// Check if dependabot.yml already exists
	if _, err := os.Stat(path); err == nil {
		// File exists - read and merge configuration
		dependabotLog.Print("Existing dependabot.yml found, merging configuration")
		existingData, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed to read existing dependabot.yml: %w", err)
		}

		if err := yaml.Unmarshal(existingData, &config); err != nil {
			// If we can't parse it, start fresh
			dependabotLog.Print("Could not parse existing dependabot.yml, creating new one")
			config = DependabotConfig{Version: 2}
		}
	} else {
		// New dependabot.yml
		dependabotLog.Print("Creating new dependabot.yml")
		config = DependabotConfig{Version: 2}
	}

	// Add ecosystems that don't already exist for .github/workflows
	for ecosystem := range ecosystems {
		exists := false
		for _, update := range config.Updates {
			if update.PackageEcosystem == ecosystem && update.Directory == "/.github/workflows" {
				exists = true
				break
			}
		}

		if !exists {
			entry := DependabotUpdateEntry{
				PackageEcosystem: ecosystem,
				Directory:        "/.github/workflows",
			}
			entry.Schedule.Interval = "weekly"
			config.Updates = append(config.Updates, entry)
		}
	}

	// Write dependabot.yml
	yamlData, err := yaml.Marshal(&config)
	if err != nil {
		return fmt.Errorf("failed to marshal dependabot.yml: %w", err)
	}

	if err := os.WriteFile(path, yamlData, 0644); err != nil {
		return fmt.Errorf("failed to write dependabot.yml: %w", err)
	}

	dependabotLog.Print("Successfully wrote dependabot.yml")
	if c.verbose {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Updated .github/dependabot.yml"))
	}

	// Track the created file
	if c.fileTracker != nil {
		c.fileTracker.TrackCreated(path)
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
	sort.Slice(deps, func(i, j int) bool {
		return deps[i].Name < deps[j].Name
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

// generateRequirementsTxt creates or updates requirements.txt with dependencies
func (c *Compiler) generateRequirementsTxt(path string, deps []PipDependency, forceOverwrite bool) error {
	dependabotLog.Printf("Generating requirements.txt at %s", path)

	// Build requirements map for merging
	reqMap := make(map[string]string)
	for _, dep := range deps {
		if dep.Version != "" {
			reqMap[dep.Name] = dep.Version
		} else {
			reqMap[dep.Name] = ""
		}
	}

	// Check if requirements.txt already exists
	if _, err := os.Stat(path); err == nil {
		// File exists - merge dependencies
		dependabotLog.Print("Existing requirements.txt found, merging dependencies")

		existingData, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed to read existing requirements.txt: %w", err)
		}

		// Parse existing requirements
		lines := strings.Split(string(existingData), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			dep := parsePipPackage(line)
			// Only add if not already in our new deps
			if _, exists := reqMap[dep.Name]; !exists {
				if dep.Version != "" {
					reqMap[dep.Name] = dep.Version
				} else {
					reqMap[dep.Name] = ""
				}
			}
		}

		if c.verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Merging with existing requirements.txt"))
		}
	} else {
		dependabotLog.Print("Creating new requirements.txt")
	}

	// Sort dependencies by name
	var sortedNames []string
	for name := range reqMap {
		sortedNames = append(sortedNames, name)
	}
	sort.Strings(sortedNames)

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

	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
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
	sort.Slice(deps, func(i, j int) bool {
		return deps[i].Path < deps[j].Path
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

// generateGoMod creates or updates go.mod with dependencies
func (c *Compiler) generateGoMod(path string, deps []GoDependency, forceOverwrite bool) error {
	dependabotLog.Printf("Generating go.mod at %s", path)

	// Build module content
	var lines []string

	// Check if go.mod already exists
	if _, err := os.Stat(path); err == nil {
		// File exists - read and preserve module declaration
		dependabotLog.Print("Existing go.mod found, merging dependencies")

		existingData, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed to read existing go.mod: %w", err)
		}

		existingLines := strings.Split(string(existingData), "\n")
		// Keep module declaration and go version
		for _, line := range existingLines {
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

	// Add require section if we have dependencies
	if len(deps) > 0 {
		lines = append(lines, "")
		lines = append(lines, "require (")
		for _, dep := range deps {
			version := dep.Version
			if version == "latest" || version == "" {
				// Skip dependencies without explicit versions - they should be added manually
				// or resolved using 'go get' or 'go mod tidy'. Using v0.0.0 as a placeholder
				// can cause issues with Go module resolution.
				dependabotLog.Printf("Skipping %s: no version specified (use 'go get %s@latest' to resolve)", dep.Path, dep.Path)
				continue
			}
			lines = append(lines, fmt.Sprintf("\t%s %s", dep.Path, version))
		}
		lines = append(lines, ")")
	}

	content := strings.Join(lines, "\n") + "\n"

	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
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
