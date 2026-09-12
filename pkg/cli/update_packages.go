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
	"github.com/github/gh-aw/pkg/workflow"
)

type installedPackageUpdate struct {
	record         packageOwnershipRecord
	workflows      []*workflowWithSource
	workflowsDir   string
	engineOverride string
}

func resolveInstalledPackageUpdates(targets []string) ([]string, []installedPackageUpdate, error) {
	if len(targets) == 0 {
		return nil, nil, nil
	}

	var workflowTargets []string
	var packageTargets []string
	for _, target := range targets {
		if isPackageUpdateTarget(target) {
			packageTargets = append(packageTargets, target)
		} else {
			workflowTargets = append(workflowTargets, target)
		}
	}
	if len(packageTargets) == 0 {
		return workflowTargets, nil, nil
	}

	gitRoot, err := gitutil.FindGitRoot()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to find git root for package updates: %w", err)
	}
	records, err := readPackageOwnershipRecords(gitRoot)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read installed package records: %w", err)
	}

	var packages []installedPackageUpdate
	selectedPackages := make(map[string]struct{})
	for _, target := range packageTargets {
		record, _, err := findInstalledPackageRecord(records, target)
		if err != nil {
			return nil, nil, err
		}
		if record == nil {
			return nil, nil, fmt.Errorf("package %q is not installed in this repository", target)
		}
		if _, selected := selectedPackages[strings.ToLower(record.Package)]; selected {
			continue
		}
		workflows, err := packageWorkflowsFromOwnershipRecord(gitRoot, *record)
		if err != nil {
			return nil, nil, err
		}
		workflowsDir, engineOverride := packageInstallContext(gitRoot, *record)
		packages = append(packages, installedPackageUpdate{
			record:         *record,
			workflows:      workflows,
			workflowsDir:   workflowsDir,
			engineOverride: engineOverride,
		})
		selectedPackages[strings.ToLower(record.Package)] = struct{}{}
	}
	return workflowTargets, packages, nil
}

func isPackageUpdateTarget(target string) bool {
	return isPackageURLTarget(target)
}

func findInstalledPackageRecord(records []packageOwnershipRecord, target string) (*packageOwnershipRecord, bool, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return nil, false, nil
	}

	if isPackageURLTarget(target) {
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

	return nil, false, nil
}

func isPackageURLTarget(target string) bool {
	target = strings.ToLower(strings.TrimSpace(target))
	return strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://")
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

	packagePath := record.Package[len(repoID):]
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

func packageInstallContext(gitRoot string, record packageOwnershipRecord) (workflowsDir string, engineOverride string) {
	for _, entry := range record.Files {
		destination := filepath.ToSlash(filepath.Clean(entry.Destination))
		if workflowsDir == "" && (strings.HasSuffix(strings.ToLower(destination), ".md") || isActionWorkflowPath(destination)) &&
			isSupportedPackageInstallablePath(entry.Source) {
			workflowsDir = filepath.Join(gitRoot, filepath.Dir(filepath.FromSlash(destination)))
		}
		if engineOverride != "" {
			continue
		}
		for _, engine := range ValidEngineNames() {
			skillPrefix := strings.TrimSuffix(workflow.GetEngineSkillDir(engine), "/") + "/"
			agentPrefix := strings.TrimSuffix(workflow.GetEngineSubAgentDir(engine), "/") + "/"
			if strings.HasPrefix(destination, skillPrefix) || strings.HasPrefix(destination, agentPrefix) {
				engineOverride = engine
				break
			}
		}
	}
	return workflowsDir, engineOverride
}
