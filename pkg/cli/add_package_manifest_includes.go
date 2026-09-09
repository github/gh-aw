// add_package_manifest_includes.go: parsing, validating, and normalizing the
// includes/files manifest fields into typed repositoryPackageInclude/resolvedPackageInstallable values.

package cli

import (
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
)

// repositoryPackageInclude is a single entry of the manifest 'includes' array. Legacy
// string entries only set Source; object entries additionally set Destination (and
// optionally Kind).
type repositoryPackageInclude struct {
	// Source is the entry path. For string entries it follows the historical rules
	// (see isSupportedManifestIncludePath); for object entries it is always resolved
	// relative to the package root.
	Source string
	// Destination is the repository-root-relative install path. Empty for string entries.
	Destination string
	// Kind is the optional declared entry kind ("agentic-workflow" or "action-workflow").
	Kind string
}

// isMapping reports whether the entry uses the object form with an explicit destination.
func (include repositoryPackageInclude) isMapping() bool {
	return include.Destination != ""
}

const (
	manifestIncludeKindAgenticWorkflow = "agentic-workflow"
	manifestIncludeKindActionWorkflow  = "action-workflow"
)

// manifestIncludesFromPaths converts plain path strings into legacy string-form entries.
func manifestIncludesFromPaths(paths []string) []repositoryPackageInclude {
	includes := make([]repositoryPackageInclude, 0, len(paths))
	for _, p := range paths {
		includes = append(includes, repositoryPackageInclude{Source: p})
	}
	return includes
}

func extractManifestIncludes(value any, manifestPath string) ([]repositoryPackageInclude, []string, error) {
	var rawIncludes []repositoryPackageInclude
	var warnings []string
	appendRawEntry := func(item any) error {
		if include, ok := stringValue(item); ok {
			include = strings.TrimSpace(include)
			if path.Base(filepath.ToSlash(include)) == repositoryPackageManifestFileName {
				cleaned, err := cleanManifestImportPath(include)
				if err != nil {
					return fmt.Errorf("invalid Agentic Workflow manifest %q: include %q is invalid: %w", manifestPath, include, err)
				}
				include = cleaned
			}
			rawIncludes = append(rawIncludes, repositoryPackageInclude{Source: include})
			return nil
		}
		mapping, ok := item.(map[string]any)
		if !ok {
			warnings = append(warnings, fmt.Sprintf("Ignoring includes entry in %s because it is neither a string nor a source/destination mapping", manifestPath))
			return nil
		}
		include, err := parseManifestIncludeMapping(mapping, manifestPath)
		if err != nil {
			return err
		}
		rawIncludes = append(rawIncludes, include)
		return nil
	}

	switch includes := value.(type) {
	case []any:
		for _, item := range includes {
			if err := appendRawEntry(item); err != nil {
				return nil, nil, err
			}
		}
	case []string:
		rawIncludes = append(rawIncludes, manifestIncludesFromPaths(includes)...)
	default:
		return nil, []string{fmt.Sprintf("Ignoring includes entry in %s because it is not a list", manifestPath)}, nil
	}

	normalized := make([]repositoryPackageInclude, 0, len(rawIncludes))
	seen := make(map[repositoryPackageInclude]struct{})
	for _, include := range rawIncludes {
		if !include.isMapping() && !isSupportedManifestIncludePath(include.Source) && !isManifestImportPath(include.Source) {
			warnings = append(warnings, fmt.Sprintf("Ignoring includes entry %q in %s: use workflow files (workflows/, agentic-workflows/, .github/workflows/), skill directories (skills/, .github/skills/), agent markdown files (agents/, .github/agents/), or a source/destination mapping", include.Source, manifestPath))
			continue
		}
		if _, exists := seen[include]; exists {
			continue
		}
		seen[include] = struct{}{}
		normalized = append(normalized, include)
	}
	addPackageManifestLog.Printf("Extracted %d includes entries from %s (%d warnings)", len(normalized), manifestPath, len(warnings))
	return normalized, warnings, nil
}

// parseManifestIncludeMapping validates an object-form includes entry and returns the
// normalized source, destination, and kind.
func parseManifestIncludeMapping(mapping map[string]any, manifestPath string) (repositoryPackageInclude, error) {
	source, _ := stringValue(mapping["source"])
	destination, _ := stringValue(mapping["destination"])
	kind, _ := stringValue(mapping["kind"])
	source = strings.TrimSpace(source)
	destination = strings.TrimSpace(destination)
	kind = strings.TrimSpace(kind)

	if source == "" || destination == "" {
		return repositoryPackageInclude{}, fmt.Errorf("invalid Agentic Workflow manifest %q: includes mapping entries require non-empty 'source' and 'destination'. Example:\nincludes:\n  - source: payload/workflows/reviewer.md\n    destination: %sreviewer.md", manifestPath, constants.WorkflowsDirSlash)
	}

	cleanedSource, err := cleanManifestRelativePath(source)
	if err != nil {
		return repositoryPackageInclude{}, fmt.Errorf("invalid Agentic Workflow manifest %q: includes source %q is invalid: %w. Sources must be package-relative paths without '..' segments. Example:\nincludes:\n  - source: payload/workflows/reviewer.md\n    destination: %sreviewer.md", manifestPath, source, err, constants.WorkflowsDirSlash)
	}
	_, wildcard := manifestIncludeWildcardParent(cleanedSource)
	if strings.Contains(cleanedSource, "*") && !wildcard {
		return repositoryPackageInclude{}, fmt.Errorf("invalid Agentic Workflow manifest %q: includes source %q is invalid: wildcards must be a single trailing /*", manifestPath, source)
	}

	cleanedDestination, err := cleanManifestRelativePath(destination)
	if err != nil {
		return repositoryPackageInclude{}, fmt.Errorf("invalid Agentic Workflow manifest %q: includes destination %q is invalid: %w. Destinations must be repository-root-relative paths without '..' segments. Example:\nincludes:\n  - source: payload/workflows/reviewer.md\n    destination: %sreviewer.md", manifestPath, destination, err, constants.WorkflowsDirSlash)
	}

	if wildcard {
		if cleanedDestination != constants.WorkflowsDir {
			return repositoryPackageInclude{}, fmt.Errorf("invalid Agentic Workflow manifest %q: includes destination %q is invalid: wildcard destinations must be the %s folder", manifestPath, destination, constants.WorkflowsDir)
		}
		if kind != "" && kind != manifestIncludeKindAgenticWorkflow && kind != manifestIncludeKindActionWorkflow {
			return repositoryPackageInclude{}, fmt.Errorf("invalid Agentic Workflow manifest %q: includes entry declares unsupported kind %q. Use kind: %s or kind: %s", manifestPath, kind, manifestIncludeKindAgenticWorkflow, manifestIncludeKindActionWorkflow)
		}
		return repositoryPackageInclude{
			Source:      cleanedSource,
			Destination: cleanedDestination,
			Kind:        kind,
		}, nil
	}

	sourceKind, err := manifestIncludeKindForPath(cleanedSource)
	if err != nil {
		return repositoryPackageInclude{}, fmt.Errorf("invalid Agentic Workflow manifest %q: includes source %q is invalid: %w", manifestPath, source, err)
	}

	if err := validateManifestIncludeDestination(cleanedDestination, sourceKind); err != nil {
		return repositoryPackageInclude{}, fmt.Errorf("invalid Agentic Workflow manifest %q: includes destination %q is invalid: %w", manifestPath, destination, err)
	}

	if kind != "" && kind != sourceKind {
		return repositoryPackageInclude{}, fmt.Errorf("invalid Agentic Workflow manifest %q: includes entry declares kind %q but source %q is a %s. Use kind: %s or change the source file extension", manifestPath, kind, source, sourceKind, sourceKind)
	}

	return repositoryPackageInclude{
		Source:      cleanedSource,
		Destination: cleanedDestination,
		Kind:        sourceKind,
	}, nil
}

// cleanManifestRelativePath normalizes a manifest path and rejects absolute paths and
// paths that escape their root.
func cleanManifestRelativePath(p string) (string, error) {
	slashed := filepath.ToSlash(p)
	if slashed != "" && ((slashed[0] == '/' || slashed[0] == '\\') || filepath.IsAbs(p) || isWindowsDriveRelativePath(slashed)) {
		return "", errors.New("absolute paths are not allowed")
	}
	cleaned := path.Clean(slashed)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", errors.New("path traversal outside the root is not allowed")
	}
	return cleaned, nil
}

// isWindowsDriveRelativePath reports whether p starts with a Windows drive letter prefix
// (e.g. "C:/payload"). filepath.IsAbs does not detect these on non-Windows hosts.
func isWindowsDriveRelativePath(p string) bool {
	if len(p) < 2 || p[1] != ':' {
		return false
	}
	c := p[0]
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// manifestIncludeKindForPath returns the entry kind implied by the file extension.
func manifestIncludeKindForPath(p string) (string, error) {
	lower := strings.ToLower(p)
	switch {
	case strings.HasSuffix(lower, ".lock.yml"):
		return "", errors.New("compiled lock files (.lock.yml) cannot be installed")
	case strings.HasSuffix(lower, ".md"):
		return manifestIncludeKindAgenticWorkflow, nil
	case strings.HasSuffix(lower, ".yml"):
		return manifestIncludeKindActionWorkflow, nil
	default:
		return "", errors.New("only agentic workflows (.md) and action workflows (.yml) are supported")
	}
}

// validateManifestIncludeDestination validates that a mapping destination installs into an
// approved repository namespace with an extension matching its source.
func validateManifestIncludeDestination(destination, sourceKind string) error {
	if !strings.HasPrefix(destination, constants.WorkflowsDirSlash) {
		return fmt.Errorf("destinations must be under %s", constants.WorkflowsDirSlash)
	}
	remaining := strings.TrimPrefix(destination, constants.WorkflowsDirSlash)
	if remaining == "" || strings.Contains(remaining, "/") {
		return fmt.Errorf("destinations must be a direct child of %s", constants.WorkflowsDirSlash)
	}
	destinationKind, err := manifestIncludeKindForPath(destination)
	if err != nil {
		return err
	}
	if destinationKind != sourceKind {
		return fmt.Errorf("destination file extension must match the source (%s)", sourceKind)
	}
	return nil
}

func extractManifestFiles(value any, manifestPath string) ([]string, []string) {
	var rawFiles []string
	switch files := value.(type) {
	case []any:
		for _, item := range files {
			if file, ok := stringValue(item); ok {
				rawFiles = append(rawFiles, file)
			}
		}
	case []string:
		rawFiles = append(rawFiles, files...)
	default:
		return nil, []string{fmt.Sprintf("Ignoring files entry in %s because it is not a list of strings", manifestPath)}
	}

	var warnings []string
	normalized := make([]string, 0, len(rawFiles))
	seen := make(map[string]struct{})
	for _, file := range rawFiles {
		if !isSupportedPackageInstallablePath(file) {
			warnings = append(warnings, fmt.Sprintf("Ignoring files entry %q in %s: supported files are markdown (.md) files under workflows/, agentic-workflows/, or .github/workflows/, or action workflow (.yml) files under .github/workflows/", file, manifestPath))
			continue
		}
		if _, exists := seen[file]; exists {
			continue
		}
		seen[file] = struct{}{}
		normalized = append(normalized, file)
	}

	addPackageManifestLog.Printf("Extracted %d files entries from %s (%d warnings)", len(normalized), manifestPath, len(warnings))
	return normalized, warnings
}

func codemodManifestFilesToIncludes(files []string) []string {
	converted := make([]string, 0, len(files))
	for _, file := range files {
		converted = append(converted, path.Clean(filepath.ToSlash(file)))
	}
	return converted
}

func formatIncludesCodemodSuggestion(paths []string) string {
	if len(paths) == 0 {
		return "includes: []"
	}
	lines := []string{"includes:"}
	for _, p := range paths {
		lines = append(lines, "  - "+p)
	}
	return strings.Join(lines, "\n")
}

func splitManifestIncludePaths(includes []repositoryPackageInclude) (installable []repositoryPackageInclude, skillDirs, agentFiles []string) {
	for _, include := range includes {
		if include.isMapping() {
			installable = append(installable, include)
			continue
		}
		switch {
		case isSupportedSkillDirPath(include.Source):
			skillDirs = append(skillDirs, include.Source)
		case isSupportedAgentFilePath(include.Source):
			agentFiles = append(agentFiles, include.Source)
		case isSupportedPackageInstallablePath(include.Source):
			installable = append(installable, include)
		}
	}
	addPackageManifestLog.Printf("Split %d includes entries into %d installable, %d skill dir(s), %d agent file(s)", len(includes), len(installable), len(skillDirs), len(agentFiles))
	return installable, skillDirs, agentFiles
}

// extractManifestSkillDirs parses the skills array from an aw.yml manifest, validating
// and normalizing each entry. Each entry must be a path under skills/ that represents
// the directory for a skill (e.g. "skills/my-skill").
func extractManifestSkillDirs(value any, manifestPath string) ([]string, []string) {
	var rawDirs []string
	switch dirs := value.(type) {
	case []any:
		for _, item := range dirs {
			if dir, ok := stringValue(item); ok {
				rawDirs = append(rawDirs, dir)
			}
		}
	case []string:
		rawDirs = append(rawDirs, dirs...)
	default:
		return nil, []string{fmt.Sprintf("Ignoring skills entry in %s because it is not a list of strings", manifestPath)}
	}

	var warnings []string
	normalized := make([]string, 0, len(rawDirs))
	seen := make(map[string]struct{})
	for _, dir := range rawDirs {
		if !isSupportedSkillDirPath(dir) {
			warnings = append(warnings, fmt.Sprintf("Ignoring skills entry %q in %s: skill entries must be directory paths under skills/ (e.g. \"skills/my-skill\")", dir, manifestPath))
			continue
		}
		if _, exists := seen[dir]; exists {
			continue
		}
		seen[dir] = struct{}{}
		normalized = append(normalized, dir)
	}
	addPackageManifestLog.Printf("Extracted %d skill directories from %s (%d warnings)", len(normalized), manifestPath, len(warnings))
	return normalized, warnings
}

// extractManifestAgentFiles parses the agents array from an aw.yml manifest, validating
// and normalizing each entry. Each entry must be a .md file path under agents/.
func extractManifestAgentFiles(value any, manifestPath string) ([]string, []string) {
	var rawFiles []string
	switch files := value.(type) {
	case []any:
		for _, item := range files {
			if file, ok := stringValue(item); ok {
				rawFiles = append(rawFiles, file)
			}
		}
	case []string:
		rawFiles = append(rawFiles, files...)
	default:
		return nil, []string{fmt.Sprintf("Ignoring agents entry in %s because it is not a list of strings", manifestPath)}
	}

	var warnings []string
	normalized := make([]string, 0, len(rawFiles))
	seen := make(map[string]struct{})
	for _, file := range rawFiles {
		if !isSupportedAgentFilePath(file) {
			warnings = append(warnings, fmt.Sprintf("Ignoring agents entry %q in %s: agent entries must be .md file paths under agents/ (e.g. \"agents/my-agent.md\")", file, manifestPath))
			continue
		}
		if _, exists := seen[file]; exists {
			continue
		}
		seen[file] = struct{}{}
		normalized = append(normalized, file)
	}
	addPackageManifestLog.Printf("Extracted %d agent files from %s (%d warnings)", len(normalized), manifestPath, len(warnings))
	return normalized, warnings
}

// isSupportedSkillDirPath returns true when p is a valid skill directory path.
// Valid skill directory paths must be directly under skills/ (e.g. "skills/my-skill")
// with no further nesting.
func isSupportedSkillDirPath(p string) bool {
	cleaned := path.Clean(filepath.ToSlash(p))
	if !isSupportedSkillDirectoryPrefix(cleaned) {
		return false
	}
	root := skillDirectoryRoot(cleaned)
	remaining := strings.TrimPrefix(cleaned, root+"/")
	// Must have exactly one path component (direct child of skills/)
	return remaining != "" && !strings.Contains(remaining, "/")
}

// isSupportedAgentFilePath returns true when p is a valid agent file path.
// Valid agent paths must be .md files directly under agents/ (e.g. "agents/my-agent.md").
func isSupportedAgentFilePath(p string) bool {
	cleaned := path.Clean(filepath.ToSlash(p))
	if !isSupportedAgentDirectoryPrefix(cleaned) {
		return false
	}
	if !strings.HasSuffix(strings.ToLower(cleaned), ".md") {
		return false
	}
	root := agentDirectoryRoot(cleaned)
	remaining := strings.TrimPrefix(cleaned, root+"/")
	// Must be a direct child of agents/ (no subdirectories)
	return remaining != "" && !strings.Contains(remaining, "/")
}

func isSupportedManifestIncludePath(p string) bool {
	if strings.Contains(filepath.ToSlash(p), "*") {
		parent, ok := manifestIncludeWildcardParent(p)
		return ok && isSupportedManifestWildcardParent(parent)
	}
	return isSupportedPackageInstallablePath(p) || isSupportedSkillDirPath(p) || isSupportedAgentFilePath(p)
}

func manifestIncludeWildcardParent(p string) (string, bool) {
	slashed := filepath.ToSlash(p)
	if !strings.HasSuffix(slashed, "/*") || strings.Count(slashed, "*") != 1 {
		return "", false
	}
	parent := strings.TrimSuffix(slashed, "/*")
	if parent == "" {
		return "", false
	}
	cleaned, err := cleanManifestRelativePath(parent)
	if err != nil || cleaned != parent {
		return "", false
	}
	return parent, true
}

func isSupportedManifestWildcardParent(parent string) bool {
	switch parent {
	case "workflows", "agentic-workflows", constants.WorkflowsDir,
		"skills", "agents", constants.GithubDir + packageSkillsDirectory,
		constants.GithubDir + packageAgentsDirectory:
		return true
	default:
		return false
	}
}

func expandManifestWildcardMatches(include repositoryPackageInclude, parent string, candidates []string, isSupported func(string) bool) ([]repositoryPackageInclude, error) {
	sort.Strings(candidates)
	matches := make([]repositoryPackageInclude, 0, len(candidates))
	for _, candidate := range candidates {
		candidate = filepath.ToSlash(candidate)
		relative := strings.TrimPrefix(candidate, parent+"/")
		if relative == candidate || relative == "" || strings.Contains(relative, "/") {
			continue
		}
		source := path.Join(parent, relative)
		if isSupported(source) {
			match := repositoryPackageInclude{Source: source}
			if include.isMapping() {
				sourceKind, err := manifestIncludeKindForPath(source)
				if err != nil {
					continue
				}
				if include.Kind != "" && include.Kind != sourceKind {
					return nil, fmt.Errorf("includes wildcard %q declares kind %q but matched source %q is a %s", include.Source, include.Kind, source, sourceKind)
				}
				match.Destination = path.Join(include.Destination, path.Base(source))
				match.Kind = sourceKind
			}
			matches = append(matches, match)
		}
	}
	return matches, nil
}

func expandManifestWildcardCandidates(include repositoryPackageInclude, parent string, fileCandidates, dirCandidates []string) ([]repositoryPackageInclude, error) {
	isSupportedFile := func(source string) bool {
		if include.isMapping() {
			_, err := manifestIncludeKindForPath(source)
			return err == nil
		}
		return isSupportedPackageInstallablePath(source) || isSupportedAgentFilePath(source)
	}
	expanded, err := expandManifestWildcardMatches(include, parent, fileCandidates, isSupportedFile)
	if err != nil {
		return nil, err
	}
	if include.isMapping() {
		return expanded, nil
	}
	dirMatches, err := expandManifestWildcardMatches(include, parent, dirCandidates, isSupportedSkillDirPath)
	if err != nil {
		return nil, err
	}
	return append(expanded, dirMatches...), nil
}

func deduplicateManifestIncludes(includes []repositoryPackageInclude) []repositoryPackageInclude {
	deduplicated := make([]repositoryPackageInclude, 0, len(includes))
	seen := make(map[repositoryPackageInclude]struct{}, len(includes))
	for _, include := range includes {
		if _, exists := seen[include]; exists {
			continue
		}
		seen[include] = struct{}{}
		deduplicated = append(deduplicated, include)
	}
	return deduplicated
}

func isSupportedSkillDirectoryPrefix(cleaned string) bool {
	return strings.HasPrefix(cleaned, packageSkillsDirectory+"/") ||
		strings.HasPrefix(cleaned, constants.GithubDir+packageSkillsDirectory+"/")
}

func skillDirectoryRoot(cleaned string) string {
	switch {
	case strings.HasPrefix(cleaned, constants.GithubDir+packageSkillsDirectory+"/"):
		return constants.GithubDir + packageSkillsDirectory
	default:
		return packageSkillsDirectory
	}
}

func isSupportedAgentDirectoryPrefix(cleaned string) bool {
	return strings.HasPrefix(cleaned, packageAgentsDirectory+"/") ||
		strings.HasPrefix(cleaned, constants.GithubDir+packageAgentsDirectory+"/")
}

func agentDirectoryRoot(cleaned string) string {
	switch {
	case strings.HasPrefix(cleaned, constants.GithubDir+packageAgentsDirectory+"/"):
		return constants.GithubDir + packageAgentsDirectory
	default:
		return packageAgentsDirectory
	}
}

func normalizePackageInstallablePaths(includes []repositoryPackageInclude, packagePath string) []resolvedPackageInstallable {
	normalized := make([]resolvedPackageInstallable, 0, len(includes))
	seen := make(map[string]struct{})
	for _, include := range includes {
		sourcePath := include.Source
		if include.isMapping() {
			// Mapping sources are always package-relative, including for nested packages.
			sourcePath = joinRepositoryPackagePath(packagePath, sourcePath)
		} else {
			if !isSupportedPackageInstallablePath(sourcePath) {
				continue
			}
			// Paths under .github/ are treated as repo-root-relative even in nested
			// bundles (e.g. a bundle at "dependabot/" with ".github/workflows/foo.md"
			// refers to the repository-root ".github/workflows/foo.md", not to
			// "dependabot/.github/workflows/foo.md"). All other paths (e.g. workflows/,
			// agentic-workflows/) remain relative to the package root.
			if packagePath != "" && strings.HasPrefix(sourcePath, constants.GithubDir) {
				sourcePath = filepath.ToSlash(sourcePath)
			} else {
				sourcePath = joinRepositoryPackagePath(packagePath, sourcePath)
			}
		}
		if _, exists := seen[sourcePath]; exists {
			continue
		}
		seen[sourcePath] = struct{}{}
		destination := include.Destination
		if destination == "" {
			destination = defaultPackageInstallDestination(sourcePath)
		}
		normalized = append(normalized, resolvedPackageInstallable{
			SourcePath:      sourcePath,
			DestinationPath: destination,
		})
	}
	addPackageManifestLog.Printf("Normalized %d package installable paths (package path=%q)", len(normalized), packagePath)
	return normalized
}

func isSupportedPackageInstallablePath(p string) bool {
	// Normalize separators to forward slashes (consistent with joinRepositoryPackagePath) then
	// clean to reject path traversal (e.g. "workflows/../README.md" → "README.md").
	cleaned := path.Clean(filepath.ToSlash(p))
	lowerCleaned := strings.ToLower(cleaned)
	if strings.HasSuffix(lowerCleaned, ".md") {
		return strings.HasPrefix(cleaned, "workflows/") ||
			strings.HasPrefix(cleaned, "agentic-workflows/") ||
			strings.HasPrefix(cleaned, constants.WorkflowsDirSlash)
	}
	if isActionWorkflowPath(cleaned) {
		if !strings.HasPrefix(cleaned, constants.WorkflowsDirSlash) {
			return false
		}
		// Reject nested subdirectories: only direct children of .github/workflows/ are allowed.
		remaining := strings.TrimPrefix(cleaned, constants.WorkflowsDirSlash)
		return !strings.Contains(remaining, "/")
	}
	return false
}

func isActionWorkflowPath(p string) bool {
	lowerPath := strings.ToLower(p)
	return strings.HasSuffix(lowerPath, ".yml") && !strings.HasSuffix(lowerPath, ".lock.yml")
}
