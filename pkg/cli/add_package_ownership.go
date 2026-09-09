package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/fileutil"
	"github.com/github/gh-aw/pkg/gitutil"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/workflow"
)

var addPackageOwnershipLog = logger.New("cli:add_package_ownership")

const packageOwnershipSchemaVersion = 1

type packageOwnershipRecord struct {
	SchemaVersion  int                         `json:"schemaVersion"`
	Package        string                      `json:"package"`
	Source         string                      `json:"source"`
	ResolvedCommit string                      `json:"resolvedCommit,omitempty"`
	Installer      string                      `json:"installer"`
	Files          []packageOwnershipFileEntry `json:"files"`
}

func sha256Bytes(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

type packageOwnershipFileEntry struct {
	Source      string `json:"source"`
	Destination string `json:"destination"`
	SHA256      string `json:"sha256"`
}

func writePackageOwnershipRecords(workflows []*ResolvedWorkflow, tracker *FileTracker, opts AddOptions) error {
	groups := packageManagedWorkflowGroups(workflows)
	if len(groups) == 0 {
		return nil
	}
	addPackageOwnershipLog.Printf("Writing package ownership records for %d package group(s)", len(groups))
	gitRoot, err := gitutil.FindGitRoot()
	if err != nil {
		return fmt.Errorf("failed to find git root for package ownership records: %w", err)
	}
	for packageSource, group := range groups {
		record, err := buildPackageOwnershipRecord(gitRoot, packageSource, group, opts)
		if err != nil {
			return err
		}
		recordPath := packageOwnershipRecordPath(gitRoot, packageSource)
		if err := os.MkdirAll(filepath.Dir(recordPath), constants.DirPermPublic); err != nil {
			return fmt.Errorf("failed to create package ownership directory: %w", err)
		}
		existed := fileutil.FileExists(recordPath)
		if tracker != nil {
			if existed {
				tracker.TrackModified(recordPath)
			} else {
				tracker.TrackCreated(recordPath)
			}
		}
		data, err := json.MarshalIndent(record, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to encode package ownership record: %w", err)
		}
		data = append(data, '\n')
		if err := os.WriteFile(recordPath, data, constants.FilePermPublic); err != nil {
			return fmt.Errorf("failed to write package ownership record %s: %w", recordPath, err)
		}
		addPackageOwnershipLog.Printf("Wrote package ownership record %s (%d file entries)", recordPath, len(record.Files))
	}
	return nil
}

func packageManagedWorkflowGroups(workflows []*ResolvedWorkflow) map[string][]*ResolvedWorkflow {
	groups := make(map[string][]*ResolvedWorkflow)
	for _, resolved := range workflows {
		if resolved == nil || resolved.Spec == nil || !resolved.Spec.FromRepositoryManifest || resolved.IsPackageProjectFile {
			continue
		}
		source := packageSourceForSpec(resolved.Spec, resolved.SourceInfo)
		if source == "" {
			continue
		}
		groups[source] = append(groups[source], resolved)
	}
	return groups
}

func packageSourceForSpec(spec *WorkflowSpec, sourceInfo *FetchedWorkflow) string {
	if spec.RepoSlug == "" {
		return ""
	}
	ref := spec.Version
	if sourceInfo != nil && sourceInfo.CommitSHA != "" {
		ref = sourceInfo.CommitSHA
	}
	return manifestSourceWithRef(&RepoSpec{
		RepoSlug:    spec.RepoSlug,
		PackagePath: spec.PackagePath,
	}, ref)
}

func buildPackageOwnershipRecord(gitRoot, packageSource string, workflows []*ResolvedWorkflow, opts AddOptions) (*packageOwnershipRecord, error) {
	record := &packageOwnershipRecord{
		SchemaVersion:  packageOwnershipSchemaVersion,
		Package:        strings.Split(packageSource, "@")[0],
		Source:         packageSource,
		ResolvedCommit: packageSourceRef(packageSource),
		Installer:      "gh-aw " + GetVersion(),
	}
	for _, resolved := range workflows {
		destination, err := packageManagedDestination(resolved, opts)
		if err != nil {
			return nil, err
		}
		digest, err := fileSHA256(filepath.Join(gitRoot, filepath.FromSlash(destination)))
		if err != nil {
			return nil, err
		}
		record.Files = append(record.Files, packageOwnershipFileEntry{
			Source:      filepath.ToSlash(resolved.Spec.WorkflowPath),
			Destination: destination,
			SHA256:      digest,
		})
	}
	slices.SortFunc(record.Files, func(a, b packageOwnershipFileEntry) int {
		return strings.Compare(a.Destination, b.Destination)
	})
	return record, nil
}

func packageManagedDestination(resolved *ResolvedWorkflow, opts AddOptions) (string, error) {
	spec := resolved.Spec
	switch {
	case resolved.IsPackageResourceFile:
		return filepath.ToSlash(filepath.Clean(filepath.FromSlash(spec.DestinationPath))), nil
	case resolved.IsPackageSkillFile:
		relPath, err := resolveSkillRelativePath(resolved)
		if err != nil {
			return "", err
		}
		return filepath.ToSlash(filepath.Join(workflow.GetEngineSkillDir(opts.EngineOverride), resolved.SkillName, relPath)), nil
	case resolved.IsPackageAgentFile:
		return filepath.ToSlash(filepath.Join(workflow.GetEngineSubAgentDir(opts.EngineOverride), filepath.Base(spec.WorkflowPath))), nil
	case resolved.IsActionWorkflow:
		return filepath.ToSlash(filepath.Join(packageOwnershipWorkflowDir(opts), spec.WorkflowName+".yml")), nil
	default:
		return filepath.ToSlash(filepath.Join(packageOwnershipWorkflowDir(opts), spec.WorkflowName+".md")), nil
	}
}

func packageOwnershipWorkflowDir(opts AddOptions) string {
	if opts.WorkflowDir != "" {
		return filepath.Clean(opts.WorkflowDir)
	}
	return constants.GetWorkflowDir()
}

func packageSourceRef(source string) string {
	if _, ref, ok := strings.Cut(source, "@"); ok {
		return ref
	}
	return ""
}

func packageOwnershipRecordPath(gitRoot, packageSource string) string {
	return filepath.Join(gitRoot, ".github", "aw", "packages", stablePackageID(packageSource)+".json")
}

func stablePackageID(packageSource string) string {
	base := strings.Split(packageSource, "@")[0]
	replacer := strings.NewReplacer("/", "-", "\\", "-", ":", "-", "@", "-")
	slug := strings.Trim(replacer.Replace(strings.ToLower(base)), "-")
	sum := sha256.Sum256([]byte(base))
	return fmt.Sprintf("%s-%s", slug, hex.EncodeToString(sum[:])[:12])
}

func fileSHA256(path string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("failed to read %s for digest: %w", path, err)
	}
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:]), nil
}

func packageOwnershipAllowsOverwrite(gitRoot, destination, packageSource string) (owned bool, drifted bool) {
	records, err := readPackageOwnershipRecords(gitRoot)
	if err != nil {
		return false, false
	}
	normalized := filepath.ToSlash(filepath.Clean(destination))
	packageID := strings.Split(packageSource, "@")[0]
	for _, record := range records {
		if record.Package != packageID {
			continue
		}
		for _, file := range record.Files {
			if !strings.EqualFold(filepath.ToSlash(filepath.Clean(file.Destination)), normalized) {
				continue
			}
			current, err := fileSHA256(filepath.Join(gitRoot, filepath.FromSlash(file.Destination)))
			if err != nil {
				addPackageOwnershipLog.Printf("Ownership check for %s: package=%s owned=true drifted=true (digest read failed: %v)", destination, packageSource, err)
				return true, true
			}
			drifted := current != file.SHA256
			addPackageOwnershipLog.Printf("Ownership check for %s: package=%s owned=true drifted=%t", destination, packageSource, drifted)
			return true, drifted
		}
	}
	return false, false
}

func readPackageOwnershipRecords(gitRoot string) ([]packageOwnershipRecord, error) {
	dir := filepath.Join(gitRoot, ".github", "aw", "packages")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var records []packageOwnershipRecord
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		var record packageOwnershipRecord
		if err := json.Unmarshal(data, &record); err != nil {
			continue
		}
		records = append(records, record)
	}
	return records, nil
}

func syncManifestManagedResources(ctx context.Context, repoSpec *RepoSpec, pkg *resolvedRepositoryPackage, ref string, opts UpdateWorkflowsOptions) error { //nolint:largefunc
	if pkg == nil || repoSpec == nil {
		return nil
	}
	gitRoot, err := gitutil.FindGitRoot()
	if err != nil {
		return fmt.Errorf("failed to find git root for package resources: %w", err)
	}
	owner, repo, err := splitRepositoryPackageSlug(repoSpec.RepoSlug)
	if err != nil {
		return err
	}
	packageBase := repositoryPackageIdentifier(repoSpec.RepoSlug, repoSpec.PackagePath)
	addPackageOwnershipLog.Printf("Syncing manifest-managed resources for package=%s, resource_files=%d", packageBase, len(pkg.ResourceFiles))
	recordPath := packageOwnershipRecordPath(gitRoot, packageBase)
	record := packageOwnershipRecord{
		SchemaVersion:  packageOwnershipSchemaVersion,
		Package:        packageBase,
		Source:         manifestSourceWithRef(repoSpec, ref),
		ResolvedCommit: ref,
		Installer:      "gh-aw " + GetVersion(),
	}
	if existing, err := readPackageOwnershipRecord(recordPath); err == nil && existing != nil {
		record.Files = existing.Files
	}
	existingFiles := append([]packageOwnershipFileEntry(nil), record.Files...)

	desired := make(map[string]resolvedPackageResource, len(pkg.ResourceFiles))
	for _, resource := range pkg.ResourceFiles {
		desired[filepath.ToSlash(filepath.Clean(resource.DestinationPath))] = resource
	}

	type downloadedPackageResource struct {
		resource    resolvedPackageResource
		destination string
		destPath    string
		content     []byte
		sha256      string
	}
	var downloads []downloadedPackageResource

	for _, resource := range pkg.ResourceFiles {
		destination := filepath.ToSlash(filepath.Clean(resource.DestinationPath))
		destPath := filepath.Join(gitRoot, filepath.FromSlash(destination))
		if err := fileutil.ValidatePathWithinBase(gitRoot, destPath); err != nil {
			return fmt.Errorf("resource %q escapes repository root: %w", destination, err)
		}
		if fileutil.FileExists(destPath) && !opts.Force {
			if owned, drifted := packageOwnershipAllowsOverwrite(gitRoot, destination, packageBase); !owned || drifted {
				if owned {
					return fmt.Errorf("resource %q has local modifications; use --force to overwrite", destination)
				}
				return fmt.Errorf("resource %q already exists; use --force to overwrite", destination)
			}
		}
		content, err := downloadPackageFileFromGitHubForHost(ctx, owner, repo, resource.SourcePath, ref, "")
		if err != nil {
			return fmt.Errorf("failed to download package resource %s: %w", resource.SourcePath, err)
		}
		downloads = append(downloads, downloadedPackageResource{
			resource:    resource,
			destination: destination,
			destPath:    destPath,
			content:     content,
			sha256:      sha256Bytes(content),
		})
	}

	type fileRollback struct {
		path    string
		existed bool
		content []byte
		mode    os.FileMode
	}
	var rollbacks []fileRollback
	rollbackChanges := func() {
		addPackageOwnershipLog.Printf("Rolling back %d file change(s) for package=%s", len(rollbacks), packageBase)
		for _, rollback := range slices.Backward(rollbacks) {
			if rollback.existed {
				_ = os.MkdirAll(filepath.Dir(rollback.path), constants.DirPermPublic)
				_ = os.WriteFile(rollback.path, rollback.content, rollback.mode)
				continue
			}
			_ = os.Remove(rollback.path)
		}
	}

	for _, download := range downloads {
		rollback := fileRollback{path: download.destPath}
		if stat, err := os.Stat(download.destPath); err == nil {
			rollback.existed = true
			rollback.mode = stat.Mode().Perm()
			content, readErr := os.ReadFile(download.destPath)
			if readErr != nil {
				rollbackChanges()
				return fmt.Errorf("failed to read existing package resource %s: %w", download.destination, readErr)
			}
			rollback.content = content
		}
		if err := os.MkdirAll(filepath.Dir(download.destPath), constants.DirPermPublic); err != nil {
			rollbackChanges()
			return fmt.Errorf("failed to create package resource directory: %w", err)
		}
		if err := os.WriteFile(download.destPath, download.content, constants.FilePermPublic); err != nil {
			rollbackChanges()
			return fmt.Errorf("failed to write package resource %s: %w", download.destination, err)
		}
		rollbacks = append(rollbacks, rollback)
	}

	var retained []packageOwnershipFileEntry
	var staleRemoved []string
	for _, entry := range existingFiles {
		destination := filepath.ToSlash(filepath.Clean(entry.Destination))
		if !isPackageResourceDestination(destination) {
			retained = append(retained, entry)
			continue
		}
		if _, stillDesired := desired[destination]; stillDesired {
			continue
		}

		path := filepath.Join(gitRoot, filepath.FromSlash(destination))
		current, digestErr := fileSHA256(path)
		if digestErr != nil || current != entry.SHA256 {
			retained = append(retained, entry)
			continue
		}

		rollback := fileRollback{path: path}
		if stat, err := os.Stat(path); err == nil {
			rollback.existed = true
			rollback.mode = stat.Mode().Perm()
			content, readErr := os.ReadFile(path)
			if readErr != nil {
				rollbackChanges()
				return fmt.Errorf("failed to read stale package resource %s before removal: %w", destination, readErr)
			}
			rollback.content = content
		}
		rollbacks = append(rollbacks, rollback)
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			rollbackChanges()
			return fmt.Errorf("failed to remove stale package resource %s: %w", destination, err)
		}
		staleRemoved = append(staleRemoved, destination)
	}

	record.Files = retained
	for _, download := range downloads {
		record.Files = upsertPackageOwnershipFile(record.Files, packageOwnershipFileEntry{
			Source:      download.resource.SourcePath,
			Destination: download.destination,
			SHA256:      download.sha256,
		})
	}
	for _, destination := range staleRemoved {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Removed stale package resource: "+destination))
	}
	if len(record.Files) == 0 {
		return nil
	}
	slices.SortFunc(record.Files, func(a, b packageOwnershipFileEntry) int {
		return strings.Compare(a.Destination, b.Destination)
	})
	if err := os.MkdirAll(filepath.Dir(recordPath), constants.DirPermPublic); err != nil {
		return fmt.Errorf("failed to create package ownership directory: %w", err)
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode package ownership record: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(recordPath, data, constants.FilePermPublic); err != nil {
		return fmt.Errorf("failed to write package ownership record: %w", err)
	}
	return nil
}

func readPackageOwnershipRecord(path string) (*packageOwnershipRecord, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var record packageOwnershipRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return nil, err
	}
	return &record, nil
}

func upsertPackageOwnershipFile(entries []packageOwnershipFileEntry, next packageOwnershipFileEntry) []packageOwnershipFileEntry {
	for i := range entries {
		if strings.EqualFold(filepath.ToSlash(filepath.Clean(entries[i].Destination)), filepath.ToSlash(filepath.Clean(next.Destination))) {
			entries[i] = next
			return entries
		}
	}
	return append(entries, next)
}

func isPackageResourceDestination(destination string) bool {
	return strings.EqualFold(destination, constants.GithubDir+"CODEOWNERS") ||
		strings.HasPrefix(destination, constants.GithubDir+"ISSUE_TEMPLATE/") ||
		strings.HasPrefix(destination, constants.GithubDir+"aw/") ||
		workflow.IsValidOperationalValueEvaluatorRunPath(destination)
}

func removePackageOwnedFilesIfUnused(packageBase string) error {
	gitRoot, err := gitutil.FindGitRoot()
	if err != nil {
		return err
	}
	if packageBase == "" || packageHasRemainingWorkflows(gitRoot, packageBase) {
		return nil
	}
	recordPath := packageOwnershipRecordPath(gitRoot, packageBase)
	record, err := readPackageOwnershipRecord(recordPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var kept []packageOwnershipFileEntry
	for _, entry := range record.Files {
		destination := filepath.ToSlash(filepath.Clean(entry.Destination))
		path := filepath.Join(gitRoot, filepath.FromSlash(destination))
		if _, statErr := os.Stat(path); os.IsNotExist(statErr) && isMarkdownOwnedWorkflowDestination(destination) {
			continue
		}
		current, digestErr := fileSHA256(path)
		if digestErr == nil && current == entry.SHA256 {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				kept = append(kept, entry)
				continue
			}
			fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Removed package-owned file: "+destination))
			continue
		}
		kept = append(kept, entry)
	}
	if len(kept) == 0 {
		if err := os.Remove(recordPath); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	record.Files = kept
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(recordPath, data, constants.FilePermPublic)
}

func isMarkdownOwnedWorkflowDestination(destination string) bool {
	if !strings.HasPrefix(destination, constants.WorkflowsDirSlash) {
		return false
	}
	lower := strings.ToLower(destination)
	return strings.HasSuffix(lower, ".md") || strings.HasSuffix(lower, ".lock.yml")
}

func packageHasRemainingWorkflows(gitRoot, packageBase string) bool {
	pattern := filepath.Join(gitRoot, constants.GetWorkflowDir(), "*.md")
	files, err := filepath.Glob(pattern)
	if err != nil {
		return false
	}
	for _, file := range files {
		source := readFullSourceFromFile(file)
		repoSpec, ok, err := parseManifestSourceSpec(source)
		if err != nil || !ok || repoSpec == nil {
			continue
		}
		if repositoryPackageIdentifier(repoSpec.RepoSlug, repoSpec.PackagePath) == packageBase {
			return true
		}
	}
	return false
}
