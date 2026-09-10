package cli

import (
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
)

type repositoryPackageResource struct {
	Source      string
	Destination string
}

func extractManifestResources(value any, manifestPath string) ([]repositoryPackageResource, error) {
	items, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("invalid Agentic Workflow manifest %q: resources must be a list of source/destination mappings", manifestPath)
	}
	resources := make([]repositoryPackageResource, 0, len(items))
	seenDestinations := make(map[string]string, len(items))
	for _, item := range items {
		mapping, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("invalid Agentic Workflow manifest %q: resources entries must be source/destination mappings", manifestPath)
		}
		resource, err := parseManifestResourceMapping(mapping, manifestPath)
		if err != nil {
			return nil, err
		}
		key := strings.ToLower(resource.Destination)
		if previous, exists := seenDestinations[key]; exists {
			return nil, fmt.Errorf("invalid Agentic Workflow manifest %q: resources entries %q and %q both install to %q. Each resource must have a unique destination", manifestPath, previous, resource.Source, resource.Destination)
		}
		seenDestinations[key] = resource.Source
		resources = append(resources, resource)
	}
	addPackageManifestLog.Printf("Extracted %d resources entries from %s", len(resources), manifestPath)
	return resources, nil
}

func parseManifestResourceMapping(mapping map[string]any, manifestPath string) (repositoryPackageResource, error) {
	source, _ := stringValue(mapping["source"])
	destination, _ := stringValue(mapping["destination"])
	source = strings.TrimSpace(source)
	destination = strings.TrimSpace(destination)
	if source == "" || destination == "" {
		return repositoryPackageResource{}, fmt.Errorf("invalid Agentic Workflow manifest %q: resources entries require non-empty 'source' and 'destination'. Example:\nresources:\n  - source: templates/bug.yml\n    destination: .github/ISSUE_TEMPLATE/bug.yml", manifestPath)
	}

	cleanedSource, err := cleanManifestRelativePath(source)
	if err != nil {
		return repositoryPackageResource{}, fmt.Errorf("invalid Agentic Workflow manifest %q: resources source %q is invalid: %w", manifestPath, source, err)
	}
	cleanedDestination, err := cleanManifestRelativePath(destination)
	if err != nil {
		return repositoryPackageResource{}, fmt.Errorf("invalid Agentic Workflow manifest %q: resources destination %q is invalid: %w", manifestPath, destination, err)
	}
	if err := validateManifestResourceDestination(cleanedDestination); err != nil {
		return repositoryPackageResource{}, fmt.Errorf("invalid Agentic Workflow manifest %q: resources destination %q is invalid: %w", manifestPath, destination, err)
	}
	addPackageManifestLog.Printf("Parsed resource mapping: source=%s destination=%s", cleanedSource, cleanedDestination)
	return repositoryPackageResource{Source: cleanedSource, Destination: cleanedDestination}, nil
}

func validateManifestResourceDestination(destination string) error {
	switch {
	case strings.HasPrefix(destination, constants.GithubDir+"ISSUE_TEMPLATE/"):
		remaining := strings.TrimPrefix(destination, constants.GithubDir+"ISSUE_TEMPLATE/")
		if remaining == "" || strings.Contains(remaining, "/") {
			return fmt.Errorf("issue template resources must be direct children of %sISSUE_TEMPLATE", constants.GithubDir)
		}
		lower := strings.ToLower(remaining)
		if !strings.HasSuffix(lower, ".yml") && !strings.HasSuffix(lower, ".yaml") {
			return errorsForResourceDestination()
		}
		return nil
	case destination == constants.GithubDir+"CODEOWNERS":
		return nil
	case strings.HasPrefix(destination, constants.GithubDir+"aw/"):
		remaining := strings.TrimPrefix(destination, constants.GithubDir+"aw/")
		if remaining == "" || strings.HasPrefix(remaining, "../") {
			return errorsForResourceDestination()
		}
		return nil
	default:
		return errorsForResourceDestination()
	}
}

func errorsForResourceDestination() error {
	return errors.New("destinations must be .github/CODEOWNERS, .github/ISSUE_TEMPLATE/*.yml, .github/ISSUE_TEMPLATE/*.yaml, or under .github/aw/")
}

func normalizePackageResourcePaths(resources []repositoryPackageResource, packagePath string) []resolvedPackageResource {
	normalized := make([]resolvedPackageResource, 0, len(resources))
	for _, resource := range resources {
		normalized = append(normalized, resolvedPackageResource{
			SourcePath:      joinRepositoryPackagePath(packagePath, resource.Source),
			DestinationPath: resource.Destination,
		})
	}

	addPackageManifestLog.Printf("Normalized %d resource paths (package path=%q)", len(normalized), packagePath)
	return normalized
}

func packageResourceDestinationKey(destination string) string {
	return strings.ToLower(filepath.ToSlash(filepath.Clean(destination)))
}

func normalizeLocalPackageResourcePaths(resources []repositoryPackageResource, packageDir string) ([]resolvedPackageResource, error) {
	normalized := make([]resolvedPackageResource, 0, len(resources))
	for _, resource := range resources {
		absolutePath := filepath.Clean(filepath.Join(packageDir, filepath.FromSlash(resource.Source)))
		if err := validateLocalPackageMappingSource(absolutePath, packageDir, resource.Source); err != nil {
			addPackageManifestLog.Printf("Rejecting local resource source %q outside package dir %q: %v", resource.Source, packageDir, err)
			return nil, err
		}
		normalized = append(normalized, resolvedPackageResource{
			SourcePath:      absolutePath,
			DestinationPath: resource.Destination,
		})
	}
	return normalized, nil
}

func packageResourceName(resource resolvedPackageResource) string {
	base := path.Base(filepath.ToSlash(resource.DestinationPath))
	return strings.TrimSuffix(base, path.Ext(base))
}
