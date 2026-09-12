package cli

import (
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/fileutil"
	"github.com/github/gh-aw/pkg/gitutil"
)

type installedPackageUpdate struct {
	record    packageOwnershipRecord
	workflows []*workflowWithSource
}

func resolveInstalledPackageUpdates(targets []string) ([]string, []installedPackageUpdate, error) {
	if len(targets) == 0 {
		return nil, nil, nil
	}

	gitRoot, err := gitutil.FindGitRoot()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to find git root for package updates: %w", err)
	}
	records, err := readPackageOwnershipRecords(gitRoot)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read installed package records: %w", err)
	}

	var workflowTargets []string
	var packages []installedPackageUpdate
	selectedPackages := make(map[string]struct{})
	for _, target := range targets {
		record, packageLike, err := findInstalledPackageRecord(records, target)
		if err != nil {
			return nil, nil, err
		}
		if record == nil {
			if packageLike {
				return nil, nil, fmt.Errorf("package %q is not installed in this repository", target)
			}
			workflowTargets = append(workflowTargets, target)
			continue
		}
		if _, selected := selectedPackages[strings.ToLower(record.Package)]; selected {
			continue
		}
		workflows, err := packageWorkflowsFromOwnershipRecord(gitRoot, *record)
		if err != nil {
			return nil, nil, err
		}
		packages = append(packages, installedPackageUpdate{record: *record, workflows: workflows})
		selectedPackages[strings.ToLower(record.Package)] = struct{}{}
	}
	return workflowTargets, packages, nil
}

func findInstalledPackageRecord(records []packageOwnershipRecord, target string) (*packageOwnershipRecord, bool, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return nil, false, nil
	}

	if strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") {
		parsed, err := url.Parse(target)
		if err != nil {
			return nil, true, fmt.Errorf("invalid package URL %q: %w", target, err)
		}
		if !isGitHubHost(parsed.Hostname()) {
			return nil, true, fmt.Errorf("package URL host %q is not supported; expected github.com or a GitHub Enterprise host", parsed.Hostname())
		}
		var bestMatch *packageOwnershipRecord
		for i := range records {
			if packageURLMatchesRecord(parsed, records[i]) &&
				(bestMatch == nil || len(records[i].Package) > len(bestMatch.Package)) {
				bestMatch = &records[i]
			}
		}
		return bestMatch, true, nil
	}

	repoSpec, ok, err := parseRepositoryPackageSpec(target)
	if err != nil {
		return nil, ok, err
	}
	if !ok || repoSpec == nil {
		return nil, false, nil
	}
	packageID := repositoryPackageIdentifier(repoSpec.RepoSlug, repoSpec.PackagePath)
	for i := range records {
		if strings.EqualFold(records[i].Package, packageID) {
			return &records[i], true, nil
		}
	}
	return nil, true, nil
}

func packageURLMatchesRecord(packageURL *url.URL, record packageOwnershipRecord) bool {
	parts := splitURLPath(packageURL.Path)
	if len(parts) < 2 {
		return false
	}
	parts[1] = strings.TrimSuffix(parts[1], ".git")
	repoID := strings.Join(parts[:2], "/")
	if !strings.EqualFold(record.Package, repoID) &&
		!strings.HasPrefix(strings.ToLower(record.Package), strings.ToLower(repoID)+"/") {
		return false
	}

	packagePath := strings.TrimPrefix(record.Package, repoID)
	packagePath = strings.Trim(packagePath, "/")
	if len(parts) == 2 {
		return packagePath == ""
	}

	remainder := parts[2:]
	if remainder[0] != "tree" && remainder[0] != "blob" {
		return strings.EqualFold(strings.Join(remainder, "/"), packagePath) ||
			strings.EqualFold(strings.TrimSuffix(strings.Join(remainder, "/"), "/aw.yml"), packagePath)
	}
	if packagePath != "" {
		joined := strings.Join(remainder[1:], "/")
		packageSuffix := path.Join("/", strings.ToLower(packagePath))
		return strings.HasSuffix(strings.ToLower(joined), packageSuffix) ||
			strings.HasSuffix(strings.ToLower(joined), path.Join(packageSuffix, "aw.yml"))
	}
	return len(remainder) == 2 || (remainder[0] == "blob" && len(remainder) == 3 && strings.EqualFold(remainder[2], "aw.yml"))
}

func splitURLPath(rawPath string) []string {
	var parts []string
	for part := range strings.SplitSeq(strings.Trim(rawPath, "/"), "/") {
		if part != "" {
			parts = append(parts, part)
		}
	}
	return parts
}

func packageWorkflowsFromOwnershipRecord(gitRoot string, record packageOwnershipRecord) ([]*workflowWithSource, error) {
	var workflows []*workflowWithSource
	for _, entry := range record.Files {
		if !strings.HasSuffix(strings.ToLower(entry.Destination), ".md") {
			continue
		}
		workflowPath := filepath.Join(gitRoot, filepath.FromSlash(entry.Destination))
		if err := fileutil.ValidatePathWithinBase(gitRoot, workflowPath); err != nil {
			return nil, fmt.Errorf("installed package %q contains an invalid workflow destination %q: %w", record.Package, entry.Destination, err)
		}
		if _, err := os.Stat(workflowPath); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("failed to inspect installed package workflow %q: %w", entry.Destination, err)
		}
		source := readFullSourceFromFile(workflowPath)
		repoSpec, ok, err := parseManifestSourceSpec(source)
		if err != nil || !ok || repoSpec == nil {
			continue
		}
		if !strings.EqualFold(repositoryPackageIdentifier(repoSpec.RepoSlug, repoSpec.PackagePath), record.Package) {
			continue
		}
		workflows = append(workflows, &workflowWithSource{
			Name:       normalizeWorkflowID(filepath.Base(workflowPath)),
			Path:       workflowPath,
			SourceSpec: source,
		})
	}
	return workflows, nil
}
