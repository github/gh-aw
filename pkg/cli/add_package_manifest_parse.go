// add_package_manifest_parse.go: parsing raw YAML into a repositoryPackageManifest and
// validating manifest-level invariants (unique destinations, filenames, workflow privacy).

package cli

import (
	"fmt"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"github.com/rivo/uniseg"

	"github.com/goccy/go-yaml"

	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/semverutil"
)

type repositoryPackageManifest struct {
	ManifestVersion string
	MinVersion      string
	Name            string
	Emoji           string
	Icon            string
	Description     string
	License         string
	Private         bool
	Experimental    bool
	Imports         []string
	Includes        []repositoryPackageInclude
	Files           []string
	Resources       []repositoryPackageResource
	Bootstrap       *repositoryPackageBootstrap
	Skills          []string // skill directory paths (e.g. "skills/my-skill")
	Agents          []string // agent .md file paths (e.g. "agents/my-agent.md")
}

func parseRepositoryPackageManifest(manifestPath string, content []byte) (*repositoryPackageManifest, []string, error) {
	addPackageManifestLog.Printf("Parsing package manifest %s (%d bytes)", manifestPath, len(content))

	root, name, err := parseRepositoryPackageManifestRoot(manifestPath, content)
	if err != nil {
		return nil, nil, err
	}

	manifest := &repositoryPackageManifest{
		Name: strings.TrimSpace(name),
	}
	warnings, err := populateRepositoryPackageManifest(manifest, root, manifestPath)
	if err != nil {
		return nil, nil, err
	}
	return manifest, warnings, nil
}

func parseRepositoryPackageManifestRoot(manifestPath string, content []byte) (map[string]any, string, error) {
	var raw any
	if err := yaml.Unmarshal(content, &raw); err != nil {
		return nil, "", fmt.Errorf("invalid Agentic Workflow manifest %q: %s. Ensure the manifest is valid YAML. Example:\nname: My Package", manifestPath, parser.FormatYAMLError(err, 1, string(content)))
	}

	root, ok := raw.(map[string]any)
	if !ok {
		return nil, "", fmt.Errorf("invalid Agentic Workflow manifest %q: top-level document must be a mapping, not a list or scalar. Example:\nname: My Package", manifestPath)
	}

	// Validate name before schema validation to provide a clear error message for
	// the most common manifest authoring error (missing or empty name).
	name, ok := stringValue(root["name"])
	if !ok || strings.TrimSpace(name) == "" {
		return nil, "", fmt.Errorf("invalid Agentic Workflow manifest %q: name must be a non-empty string. Example:\nname: My Package", manifestPath)
	}

	if err := parser.ValidateRepositoryPackageManifestWithSchemaAndLocation(root, manifestPath); err != nil {
		return nil, "", fmt.Errorf("invalid Agentic Workflow manifest %q: %w", manifestPath, err)
	}

	return root, name, nil
}

func populateRepositoryPackageManifest(manifest *repositoryPackageManifest, root map[string]any, manifestPath string) ([]string, error) {
	var warnings []string
	if err := populateRepositoryPackageManifestVersions(manifest, root, manifestPath); err != nil {
		return nil, err
	}
	metadataWarnings, err := populateRepositoryPackageManifestMetadata(manifest, root, manifestPath)
	if err != nil {
		return nil, err
	}
	warnings = append(warnings, metadataWarnings...)
	addPackageManifestLog.Printf("Parsed manifest metadata from %s: includes=%d files=%d resources=%d skills=%d agents=%d",
		manifestPath, len(manifest.Includes), len(manifest.Files), len(manifest.Resources), len(manifest.Skills), len(manifest.Agents))
	return warnings, nil
}

func populateRepositoryPackageManifestVersions(manifest *repositoryPackageManifest, root map[string]any, manifestPath string) error {
	if manifestVersion, ok := stringValue(root["manifest-version"]); ok {
		manifest.ManifestVersion = strings.TrimSpace(manifestVersion)
	} else {
		manifest.ManifestVersion = repositoryPackageManifestVersion
	}

	if minVersion, ok := stringValue(root["min-version"]); ok {
		manifest.MinVersion = strings.TrimSpace(minVersion)
		if !isSupportedManifestMinVersion(manifest.MinVersion) {
			return fmt.Errorf("invalid Agentic Workflow manifest %q: min-version must use vMAJOR.minor.patch, got %q. Example:\nmin-version: v1.2.3", manifestPath, minVersion)
		}
		currentVersion := GetVersion()
		if !semverutil.IsValid(currentVersion) {
			return fmt.Errorf("invalid Agentic Workflow manifest %q: min-version validation requires a semantic-versioned compiler, but the current compiler version %q is not a valid semantic version. This indicates a build issue; rebuild gh-aw with a proper version tag. Example: v1.2.3", manifestPath, currentVersion)
		}
		currentVersion = semverutil.NormalizeGitDescribeSemver(currentVersion)
		if semverutil.Compare(currentVersion, manifest.MinVersion) < 0 {
			addPackageManifestLog.Printf("Manifest min-version %s exceeds current gh-aw version %s", manifest.MinVersion, currentVersion)
			return fmt.Errorf("invalid Agentic Workflow manifest %q: min-version %q requires gh-aw %s or newer (current: %s). Upgrade gh-aw, or lower min-version in aw.yml to a version at or below the current one. Example:\nmin-version: %s", manifestPath, manifest.MinVersion, manifest.MinVersion, currentVersion, currentVersion)
		}
	}
	return nil
}

func populateRepositoryPackageManifestMetadata(manifest *repositoryPackageManifest, root map[string]any, manifestPath string) ([]string, error) {
	warnings := populateRepositoryPackageManifestDescription(manifest, root, manifestPath)
	populateRepositoryPackageManifestBasicMetadata(manifest, root)
	if includesValue, ok := root["includes"]; ok {
		includes, includeWarnings, err := extractManifestIncludes(includesValue, manifestPath)
		if err != nil {
			return nil, err
		}
		for _, include := range includes {
			if isManifestImportPath(include.Source) {
				manifest.Imports = append(manifest.Imports, include.Source)
				continue
			}
			manifest.Includes = append(manifest.Includes, include)
		}
		warnings = append(warnings, includeWarnings...)
	}
	if filesValue, ok := root["files"]; ok {
		files, fileWarnings := extractManifestFiles(filesValue, manifestPath)
		manifest.Files = files
		warnings = append(warnings, fileWarnings...)
		if len(files) > 0 {
			warnings = append(warnings, fmt.Sprintf("Field 'files' in %s is deprecated; use 'includes' instead.", manifestPath))
			warnings = append(warnings, "Codemod suggestion:\n"+formatIncludesCodemodSuggestion(codemodManifestFilesToIncludes(files)))
		}
	}
	if resourcesValue, ok := root["resources"]; ok {
		resources, err := extractManifestResources(resourcesValue, manifestPath)
		if err != nil {
			return nil, err
		}
		manifest.Resources = resources
	}
	if err := extractRepositoryPackageManifestIcon(manifest, root, manifestPath); err != nil {
		return nil, err
	}
	return populateRepositoryPackageManifestExtensions(manifest, root, manifestPath, warnings)
}

func populateRepositoryPackageManifestBasicMetadata(manifest *repositoryPackageManifest, root map[string]any) {
	if emoji, ok := stringValue(root["emoji"]); ok {
		manifest.Emoji = emoji
	}
	if license, ok := stringValue(root["license"]); ok {
		manifest.License = license
	}
	if private, ok := root["private"].(bool); ok {
		manifest.Private = private
	}
	if experimental, ok := root["experimental"].(bool); ok {
		manifest.Experimental = experimental
	}
}

func populateRepositoryPackageManifestExtensions(manifest *repositoryPackageManifest, root map[string]any, manifestPath string, warnings []string) ([]string, error) {
	if skillsValue, ok := root["skills"]; ok {
		skills, skillWarnings := extractManifestSkillDirs(skillsValue, manifestPath)
		manifest.Skills = skills
		warnings = append(warnings, skillWarnings...)
	}
	if agentsValue, ok := root["agents"]; ok {
		agents, agentWarnings := extractManifestAgentFiles(agentsValue, manifestPath)
		manifest.Agents = agents
		warnings = append(warnings, agentWarnings...)
	}
	if configValue, ok := root["config"]; ok {
		warnings = append(warnings, "Using experimental feature: config")
		bootstrap, err := extractManifestConfig(configValue, manifestPath)
		if err != nil {
			return nil, err
		}
		manifest.Bootstrap = bootstrap
	}
	return warnings, nil
}

func extractRepositoryPackageManifestIcon(manifest *repositoryPackageManifest, root map[string]any, manifestPath string) error {
	if iconVal, ok := root["icon"]; ok {
		icon, isStr := stringValue(iconVal)
		if !isStr {
			return fmt.Errorf("invalid Agentic Workflow manifest %q: icon must be a string", manifestPath)
		}
		icon = strings.TrimSpace(icon)
		manifest.Icon = icon
		if err := validateRepositoryPackageManifestIcon(icon, manifest.Resources, manifestPath); err != nil {
			return err
		}
	}
	return nil
}

func validateRepositoryPackageVisibility(manifest *repositoryPackageManifest, packageID string) ([]string, error) {
	if manifest.Private {
		return nil, fmt.Errorf("package %q is private and cannot be added", packageID)
	}
	if manifest.Experimental {
		return []string{fmt.Sprintf("Package %q is experimental and may change without notice.", packageID)}, nil
	}
	return nil, nil
}

func populateRepositoryPackageManifestDescription(manifest *repositoryPackageManifest, root map[string]any, manifestPath string) []string {
	if description, ok := stringValue(root["description"]); ok {
		manifest.Description = description
		if len(description) > 255 {
			return []string{fmt.Sprintf("Manifest %s description exceeds the 255-character marketplace display limit", manifestPath)}
		}
	}
	return nil
}

// validateUniqueManifestInstallDestinations rejects manifests where two entries would be
// installed to the same repository path, before any file is written.
func validateUniqueManifestInstallDestinations(installables []resolvedPackageInstallable, manifestPath string) error {
	seen := make(map[string]string, len(installables))
	for _, installable := range installables {
		key := strings.ToLower(installable.DestinationPath)
		if previous, exists := seen[key]; exists {
			addPackageManifestLog.Printf("Rejecting manifest %s: duplicate install destination %q for entries %q and %q", manifestPath, installable.DestinationPath, previous, installable.SourcePath)
			return fmt.Errorf("invalid Agentic Workflow manifest %q: includes entries %q and %q both install to %q. Each entry must have a unique destination; rename one of the destinations", manifestPath, previous, installable.SourcePath, installable.DestinationPath)
		}
		seen[key] = installable.SourcePath
	}
	return nil
}

func validateManifestInstallableWorkflowPrivacy(manifestPath string, installationSources []resolvedPackageInstallable, readWorkflow func(string) ([]byte, error)) error {
	for _, installable := range installationSources {
		installationSource := installable.SourcePath
		if isActionWorkflowPath(installationSource) {
			continue
		}

		content, err := readWorkflow(installationSource)
		if err != nil {
			return fmt.Errorf("invalid Agentic Workflow manifest %q: %w", manifestPath, err)
		}

		privateValue, hasPrivate := ExtractWorkflowPrivateSetting(string(content))
		if hasPrivate && privateValue {
			addPackageManifestLog.Printf("Rejecting manifest %s: installable workflow %s sets private: true", manifestPath, installationSource)
			return fmt.Errorf("invalid Agentic Workflow manifest %q: workflow %q sets private: true and cannot be included because private workflows cannot be added. Remove 'private: true' from the workflow frontmatter or exclude it from the manifest. Example:\n---\nprivate: false\n---", manifestPath, installationSource)
		}
	}

	return nil
}

func isSupportedManifestMinVersion(version string) bool {
	const expectedManifestMinVersionDotCount = 2
	return semverutil.IsActionVersionTag(version) && strings.Count(strings.TrimPrefix(version, "v"), ".") == expectedManifestMinVersionDotCount
}

func validateUniqueManifestWorkflowFilenames(installables []resolvedPackageInstallable, manifestPath string) error {
	seen := make(map[string]string, len(installables))
	for _, installable := range installables {
		installPath := installable.DestinationPath
		if !strings.HasSuffix(strings.ToLower(installPath), ".md") {
			continue
		}
		filenameWithoutExt := strings.TrimSuffix(filepath.Base(installPath), filepath.Ext(installPath))
		key := strings.ToLower(strings.TrimSpace(filenameWithoutExt))
		if key == "" { //nolint:tolowerequalfold
			continue
		}
		if previous, exists := seen[key]; exists {
			addPackageManifestLog.Printf("Rejecting manifest %s: duplicate workflow filename %q for entries %q and %q", manifestPath, filenameWithoutExt, previous, installPath)
			return fmt.Errorf("invalid Agentic Workflow manifest %q: duplicate workflow filename %q in files entries %q and %q. Filenames must be unique across a package; rename one of the workflow files. Example:\nfiles:\n  - workflows/%s.md\n  - workflows/%s-2.md", manifestPath, filenameWithoutExt, previous, installPath, filenameWithoutExt, filenameWithoutExt)
		}
		seen[key] = installPath
	}
	return nil
}

var octiconNameRegexp = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

func validateRepositoryPackageManifestIcon(icon string, resources []repositoryPackageResource, manifestPath string) error {
	iconStr := strings.TrimSpace(icon)
	if iconStr == "" {
		return fmt.Errorf("invalid Agentic Workflow manifest %q: icon must be a non-empty string", manifestPath)
	}

	// 1. GitHub primer octicon (:name: syntax)
	if strings.HasPrefix(iconStr, ":") && strings.HasSuffix(iconStr, ":") {
		inner := strings.TrimPrefix(strings.TrimSuffix(iconStr, ":"), ":")
		if !octiconNameRegexp.MatchString(inner) {
			return fmt.Errorf("invalid Agentic Workflow manifest %q: icon octicon name must use :name: syntax with lowercase letters, numbers, and hyphens, got %q", manifestPath, iconStr)
		}
		return nil
	}

	// 2. Emoji
	if isEmojiString(iconStr) {
		return nil
	}

	// 3. Package resource location (SVG only)
	if isResourcePathMatch(iconStr, resources) {
		if !strings.HasSuffix(strings.ToLower(iconStr), ".svg") {
			return fmt.Errorf("invalid Agentic Workflow manifest %q: icon file %q in package resources must be an SVG file (.svg)", manifestPath, iconStr)
		}
		return nil
	}

	// Look like a path or SVG file but not in resources?
	if strings.HasSuffix(strings.ToLower(iconStr), ".svg") || strings.Contains(iconStr, "/") {
		return fmt.Errorf("invalid Agentic Workflow manifest %q: icon file %q must be declared in package resources", manifestPath, iconStr)
	}

	return fmt.Errorf("invalid Agentic Workflow manifest %q: icon %q is invalid: must be an emoji, a GitHub primer octicon name (e.g. :check-circle:), or an SVG file declared in package resources", manifestPath, iconStr)
}

func isResourcePathMatch(iconPath string, resources []repositoryPackageResource) bool {
	cleanedIcon, err := cleanManifestRelativePath(iconPath)
	if err != nil {
		cleanedIcon = path.Clean(filepath.ToSlash(iconPath))
	}
	lowerIcon := strings.ToLower(cleanedIcon)
	for _, res := range resources {
		cleanSource, err1 := cleanManifestRelativePath(res.Source)
		if err1 != nil {
			cleanSource = path.Clean(filepath.ToSlash(res.Source))
		}
		cleanDest, err2 := cleanManifestRelativePath(res.Destination)
		if err2 != nil {
			cleanDest = path.Clean(filepath.ToSlash(res.Destination))
		}
		if lowerIcon == strings.ToLower(cleanSource) || lowerIcon == strings.ToLower(cleanDest) {
			return true
		}
	}
	return false
}

func isEmojiString(s string) bool {
	if s == "" {
		return false
	}
	graphemes := uniseg.NewGraphemes(s)
	count := 0
	for graphemes.Next() {
		if !isEmojiGrapheme(graphemes.Runes()) {
			return false
		}
		count++
	}
	return count > 0
}

func isEmojiGrapheme(runes []rune) bool {
	if len(runes) == 0 {
		return false
	}
	if len(runes) == 2 && isRegionalIndicator(runes[0]) && isRegionalIndicator(runes[1]) {
		return true
	}
	if (runes[0] == '#' || runes[0] == '*' || unicode.IsDigit(runes[0])) &&
		len(runes) >= 2 && runes[len(runes)-1] == '\u20e3' {
		return len(runes) == 2 || (len(runes) == 3 && runes[1] == '\ufe0f')
	}

	expectBase := true
	hasBase := false
	for _, r := range runes {
		switch {
		case expectBase && isEmojiBase(r):
			expectBase = false
			hasBase = true
		case !expectBase && (r == '\ufe0f' || isEmojiModifier(r) || unicode.Is(unicode.M, r)):
		case !expectBase && r == '\u200d':
			expectBase = true
		default:
			return false
		}
	}
	return hasBase && !expectBase
}

func isEmojiBase(r rune) bool {
	return (r >= 0x1f000 && r <= 0x1faff) ||
		(r >= 0x2300 && r <= 0x23ff) ||
		(r >= 0x2600 && r <= 0x27bf) ||
		r == 0x00a9 || r == 0x00ae || r == 0x203c || r == 0x2049
}

func isEmojiModifier(r rune) bool {
	return r >= 0x1f3fb && r <= 0x1f3ff
}

func isRegionalIndicator(r rune) bool {
	return r >= 0x1f1e6 && r <= 0x1f1ff
}
