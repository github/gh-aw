package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/fileutil"
	"github.com/github/gh-aw/pkg/gitutil"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/workflow"
)

var updateManifestLog = logger.New("cli:update_manifest")

type manifestManagedWorkflowUpdate struct {
	wf             *workflowWithSource
	repo           string
	currentPath    string
	latestPath     string
	currentRef     string
	latestRef      string
	manifestSource string
}

func fetchManifestManagedDependencies(ctx context.Context, content []byte, repo, workflowPath, ref, targetDir string, verbose bool) error {
	spec := &WorkflowSpec{
		RepoSpec: RepoSpec{
			RepoSlug: repo,
			Version:  ref,
		},
		WorkflowPath: workflowPath,
	}
	return fetchAllRemoteDependenciesStrict(ctx, string(content), spec, targetDir, verbose, true, nil)
}

func parseManifestSourceSpec(source string) (*RepoSpec, bool, error) {
	repoSpec, ok, err := parseRepositoryPackageSpec(strings.TrimSpace(source))
	if !ok {
		return nil, false, nil
	}
	if err != nil {
		return nil, true, fmt.Errorf("manifest source %q is not valid: %w; expected format like \"owner/repo\" or \"owner/repo/path\"", source, err)
	}
	if repoSpec == nil {
		return nil, false, nil
	}
	return repoSpec, true, nil
}

func manifestSourceWithRef(repoSpec *RepoSpec, ref string) string {
	base := repositoryPackageIdentifier(repoSpec.RepoSlug, repoSpec.PackagePath)
	if ref == "" {
		return base
	}
	return base + "@" + ref
}

// manifestWorkflowPathByName maps the installed workflow name (derived from the install
// destination) to the package source path used to re-fetch the workflow.
func manifestWorkflowPathByName(installables []resolvedPackageInstallable) map[string]string {
	byName := make(map[string]string, len(installables))
	for _, installable := range installables {
		if !strings.HasSuffix(strings.ToLower(installable.DestinationPath), ".md") {
			continue
		}
		workflowID := normalizeWorkflowID(filepath.Base(installable.DestinationPath))
		byName[workflowID] = installable.SourcePath
	}
	return byName
}

func updateManifestWorkflowGroup(ctx context.Context, source string, grouped []*workflowWithSource, opts UpdateWorkflowsOptions) ([]string, []updateFailure) { //nolint:largefunc
	updateManifestLog.Printf("updateManifestWorkflowGroup: source=%s, workflows=%d, force=%v, no_merge=%v", source, len(grouped), opts.Force, opts.NoMerge)
	var successes []string
	var failures []updateFailure
	var groupedSuccesses []string

	repoSpec, _, err := parseManifestSourceSpec(source)
	if err != nil {
		for _, wf := range grouped {
			failures = append(failures, updateFailure{Name: wf.Name, Error: err.Error()})
		}
		return successes, failures
	}
	if repoSpec == nil {
		return successes, failures
	}

	currentRef := repoSpec.Version
	if currentRef == "" {
		currentRef = "main"
	}
	latestRefResult, err := resolveLatestRefFn(ctx, repoSpec.RepoSlug, currentRef, opts.AllowMajor, opts.Verbose, opts.CoolDown)
	if err != nil {
		updateManifestLog.Printf("Failed to resolve latest manifest ref for %s: %v", repoSpec.RepoSlug, err)
		for _, wf := range grouped {
			failures = append(failures, updateFailure{Name: wf.Name, Error: fmt.Sprintf("failed to resolve latest manifest ref: %v", err)})
		}
		return successes, failures
	}
	if latestRefResult.CoolDownBlocked {
		updateManifestLog.Printf("Skipping manifest update for %s due to commit cooldown", repoSpec.RepoSlug)
		return successes, failures
	}
	latestRef := latestRefResult.Ref
	updateManifestLog.Printf("Resolved manifest refs: current=%s, latest=%s", currentRef, latestRef)
	sourceFieldRef := latestRef
	// Preserve branch-tracking behavior: when source points to a branch, keep the
	// branch name in source so future updates continue following that branch.
	// For tags/SHAs, pin to the resolved latest ref.
	if isBranchRef(currentRef) {
		sourceFieldRef = currentRef
	}

	currentPkg, err := resolveRepositoryPackage(ctx, &RepoSpec{
		RepoSlug:    repoSpec.RepoSlug,
		PackagePath: repoSpec.PackagePath,
		Version:     currentRef,
	}, "")
	if err != nil {
		for _, wf := range grouped {
			failures = append(failures, updateFailure{Name: wf.Name, Error: fmt.Sprintf("failed to resolve current manifest package: %v", err)})
		}
		return successes, failures
	}
	latestPkg, err := resolveRepositoryPackage(ctx, &RepoSpec{
		RepoSlug:    repoSpec.RepoSlug,
		PackagePath: repoSpec.PackagePath,
		Version:     latestRef,
	}, "")
	if err != nil {
		for _, wf := range grouped {
			failures = append(failures, updateFailure{Name: wf.Name, Error: fmt.Sprintf("failed to resolve latest manifest package: %v", err)})
		}
		return successes, failures
	}

	currentByName := manifestWorkflowPathByName(currentPkg.InstallationSource)
	latestByName := manifestWorkflowPathByName(latestPkg.InstallationSource)
	existingByName := make(map[string]*workflowWithSource, len(grouped))
	for _, wf := range grouped {
		existingByName[wf.Name] = wf
	}

	manifestSource := manifestSourceWithRef(repoSpec, sourceFieldRef)
	for name, wf := range existingByName {
		latestPath, exists := latestByName[name]
		if !exists {
			if err := removeManifestManagedWorkflow(wf.Path); err != nil {
				failures = append(failures, updateFailure{Name: wf.Name, Error: err.Error()})
				continue
			}
			groupedSuccesses = append(groupedSuccesses, wf.Name)
			continue
		}

		oldPath := currentByName[name]
		if oldPath == "" {
			oldPath = latestPath
		}
		update := manifestManagedWorkflowUpdate{
			wf:             wf,
			repo:           repoSpec.RepoSlug,
			currentPath:    oldPath,
			latestPath:     latestPath,
			currentRef:     currentRef,
			latestRef:      latestRef,
			manifestSource: manifestSource,
		}
		if err := updateManifestManagedWorkflow(ctx, update, opts); err != nil {
			failures = append(failures, updateFailure{Name: wf.Name, Error: err.Error()})
			continue
		}
		groupedSuccesses = append(groupedSuccesses, wf.Name)
	}

	targetDir := opts.WorkflowsDir
	if targetDir == "" {
		targetDir = getWorkflowsDir()
	}
	if len(grouped) > 0 {
		targetDir = filepath.Dir(grouped[0].Path)
	}
	for name, latestPath := range latestByName {
		if _, exists := existingByName[name]; exists {
			continue
		}
		if err := addManifestManagedWorkflow(ctx, targetDir, name, repoSpec.RepoSlug, latestPath, latestRef, manifestSource, opts); err != nil {
			failures = append(failures, updateFailure{Name: name, Error: err.Error()})
			continue
		}
		groupedSuccesses = append(groupedSuccesses, name)
	}

	if err := syncManifestManagedResources(ctx, repoSpec, latestPkg, latestRef, opts); err != nil {
		if len(groupedSuccesses) == 0 {
			failures = append(failures, updateFailure{Name: source, Error: err.Error()})
			return successes, failures
		}
		for _, name := range groupedSuccesses {
			failures = append(failures, updateFailure{Name: name, Error: err.Error()})
		}
		return successes, failures
	}
	assetEngine := resolveManifestAssetEngine(grouped, opts)
	if err := reconcileManifestManagedAssets(ctx, repoSpec, currentPkg, latestPkg, assetEngine, opts); err != nil {
		failures = append(failures, updateFailure{Name: source, Error: err.Error()})
	}
	if len(groupedSuccesses) == 0 && len(failures) == 0 {
		groupedSuccesses = append(groupedSuccesses, repositoryPackageIdentifier(repoSpec.RepoSlug, repoSpec.PackagePath))
	}
	successes = append(successes, groupedSuccesses...)

	return successes, failures
}

// reconcileManifestManagedAssets installs package-owned action workflows, skills, and
// agents that were added to the latest manifest. These assets do not carry source
// frontmatter, so their package ownership is derived from ownership records tracked
// under .github/aw/packages. Existing destinations are only overwritten when they are
// tracked as owned by this package (and unmodified locally, or opts.Force is set);
// otherwise the reconciliation fails rather than clobbering an unrelated file.
func reconcileManifestManagedAssets(ctx context.Context, repoSpec *RepoSpec, currentPkg *resolvedRepositoryPackage, latestPkg *resolvedRepositoryPackage, engineOverride string, opts UpdateWorkflowsOptions) error { //nolint:largefunc
	gitRoot, err := gitutil.FindGitRoot()
	if err != nil {
		return fmt.Errorf("unable to find repository root for package assets: %w", err)
	}
	owner, repository, err := splitRepositoryPackageSlug(repoSpec.RepoSlug)
	if err != nil {
		return err
	}
	packageBase := repositoryPackageIdentifier(repoSpec.RepoSlug, repoSpec.PackagePath)

	warnUpstreamRemovedSkillsAndAgents(currentPkg, latestPkg)

	for _, installable := range latestPkg.InstallationSource {
		if !isActionWorkflowPath(installable.SourcePath) {
			continue
		}
		destination := filepath.ToSlash(filepath.Clean(installable.DestinationPath))
		destPath := filepath.Join(gitRoot, filepath.FromSlash(destination))
		fileExists := false
		if _, err := os.Stat(destPath); err == nil {
			fileExists = true
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("unable to inspect new package action workflow destination %s: %w", destPath, err)
		}
		if fileExists {
			if err := ensurePackageAssetOverwriteAllowed(gitRoot, destination, packageBase, opts.Force); err != nil {
				return err
			}
		}
		content, err := downloadPackageFileFromGitHubForHost(ctx, owner, repository, installable.SourcePath, latestPkg.ResolvedRef, "")
		if err != nil {
			return fmt.Errorf("unable to download new package action workflow %s: %w", installable.SourcePath, err)
		}
		if err := os.MkdirAll(filepath.Dir(destPath), constants.DirPermPublic); err != nil {
			return fmt.Errorf("unable to create package action workflow directory: %w", err)
		}
		if err := os.WriteFile(destPath, content, constants.FilePermPublic); err != nil {
			return fmt.Errorf("unable to install new package action workflow %s: %w", installable.DestinationPath, err)
		}
		if fileExists {
			fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Updated package action workflow: "+filepath.Base(destPath)))
		} else {
			fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Added package action workflow: "+filepath.Base(destPath)))
		}
	}

	for _, skill := range latestPkg.SkillFiles {
		destPath, err := packageSkillDestinationPath(gitRoot, skill, engineOverride)
		if err != nil {
			return err
		}
		if fileutil.FileExists(destPath) {
			destination, err := filepath.Rel(gitRoot, destPath)
			if err != nil {
				return fmt.Errorf("unable to resolve relative destination for package skill %s: %w", skill.SourcePath, err)
			}
			if err := ensurePackageAssetOverwriteAllowed(gitRoot, filepath.ToSlash(destination), packageBase, opts.Force); err != nil {
				return err
			}
		}
		content, err := downloadPackageFileFromGitHubForHost(ctx, owner, repository, skill.SourcePath, latestPkg.ResolvedRef, "")
		if err != nil {
			return fmt.Errorf("unable to download new package skill %s: %w", skill.SourcePath, err)
		}
		resolved := &ResolvedWorkflow{
			Content:            content,
			Spec:               &WorkflowSpec{WorkflowPath: skill.SourcePath},
			IsPackageSkillFile: true,
			SkillName:          skill.SkillName,
		}
		if err := addSkillFileWithTracking(resolved, nil, AddOptions{
			EngineOverride: engineOverride,
			Quiet:          false,
			Force:          true,
		}, gitRoot); err != nil {
			return fmt.Errorf("unable to install new package skill %s: %w", skill.SourcePath, err)
		}
	}

	for _, agent := range latestPkg.AgentFiles {
		agentsDir := filepath.Join(gitRoot, workflow.GetEngineSubAgentDir(engineOverride))
		destPath := filepath.Join(agentsDir, filepath.Base(agent))
		if fileutil.FileExists(destPath) {
			destination, err := filepath.Rel(gitRoot, destPath)
			if err != nil {
				return fmt.Errorf("unable to resolve relative destination for package agent %s: %w", agent, err)
			}
			if err := ensurePackageAssetOverwriteAllowed(gitRoot, filepath.ToSlash(destination), packageBase, opts.Force); err != nil {
				return err
			}
		}
		content, err := downloadPackageFileFromGitHubForHost(ctx, owner, repository, agent, latestPkg.ResolvedRef, "")
		if err != nil {
			return fmt.Errorf("unable to download new package agent %s: %w", agent, err)
		}
		resolved := &ResolvedWorkflow{Content: content, Spec: &WorkflowSpec{WorkflowPath: agent}, IsPackageAgentFile: true}
		if err := addAgentFileWithTracking(resolved, nil, AddOptions{EngineOverride: engineOverride, Force: true}, gitRoot); err != nil {
			return fmt.Errorf("unable to install new package agent %s: %w", agent, err)
		}
	}
	return nil
}

// warnUpstreamRemovedSkillsAndAgents reports skill and agent files that were present in
// the currently-installed package manifest but are no longer listed in the latest
// manifest. These assets are intentionally left untouched (not deleted) since the
// removal may be transient or unintended upstream; the local copy is kept and a warning
// is printed so the user can decide whether to remove it manually.
func warnUpstreamRemovedSkillsAndAgents(currentPkg, latestPkg *resolvedRepositoryPackage) {
	if currentPkg == nil || latestPkg == nil {
		return
	}

	latestSkillSources := make(map[string]struct{}, len(latestPkg.SkillFiles))
	for _, skill := range latestPkg.SkillFiles {
		latestSkillSources[skill.SourcePath] = struct{}{}
	}
	for _, skill := range currentPkg.SkillFiles {
		if _, exists := latestSkillSources[skill.SourcePath]; exists {
			continue
		}
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf(
			"Skill %q was removed from the upstream package; skipping update and keeping the local copy. Remove it manually if it is no longer needed.",
			skill.SourcePath)))
	}

	latestAgentSources := make(map[string]struct{}, len(latestPkg.AgentFiles))
	for _, agent := range latestPkg.AgentFiles {
		latestAgentSources[agent] = struct{}{}
	}
	for _, agent := range currentPkg.AgentFiles {
		if _, exists := latestAgentSources[agent]; exists {
			continue
		}
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf(
			"Agent %q was removed from the upstream package; skipping update and keeping the local copy. Remove it manually if it is no longer needed.",
			agent)))
	}
}

// ensurePackageAssetOverwriteAllowed returns an error unless the given destination is
// safe to overwrite: either the caller passed opts.Force, or the destination is tracked
// as owned by packageBase and has not been modified locally since it was installed.
func ensurePackageAssetOverwriteAllowed(gitRoot, destination, packageBase string, force bool) error {
	if force {
		return nil
	}
	owned, drifted := packageOwnershipAllowsOverwrite(gitRoot, destination, packageBase)
	if !owned {
		return fmt.Errorf("package asset %q already exists and is not tracked as owned by %s; use --force to overwrite", destination, packageBase)
	}
	if drifted {
		return fmt.Errorf("package asset %q has local modifications; use --force to overwrite", destination)
	}
	return nil
}

func resolveManifestAssetEngine(grouped []*workflowWithSource, opts UpdateWorkflowsOptions) string {
	if opts.EngineOverride != "" {
		return opts.EngineOverride
	}
	for _, wf := range grouped {
		content, err := os.ReadFile(wf.Path)
		if err != nil {
			continue
		}
		if engine := strings.TrimSpace(ExtractWorkflowEngine(string(content))); engine != "" {
			updateManifestLog.Printf("Using engine %q from installed manifest-managed workflow %s for package asset reconciliation", engine, wf.Name)
			return engine
		}
	}
	return ""
}

func packageSkillDestinationPath(gitRoot string, skill resolvedPackageSkillFile, engineOverride string) (string, error) {
	resolved := &ResolvedWorkflow{
		Spec:      &WorkflowSpec{WorkflowPath: skill.SourcePath},
		SkillName: skill.SkillName,
	}
	relPath, err := resolveSkillRelativePath(resolved)
	if err != nil {
		return "", fmt.Errorf("unable to resolve destination for package skill %s: %w", skill.SourcePath, err)
	}
	return filepath.Join(gitRoot, workflow.GetEngineSkillDir(engineOverride), skill.SkillName, relPath), nil
}

func removeManifestManagedWorkflow(workflowPath string) error {
	updateManifestLog.Printf("Removing manifest-managed workflow no longer in manifest: %s", filepath.Base(workflowPath))
	if err := os.Remove(workflowPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("unable to remove workflow %s: %w", filepath.Base(workflowPath), err)
	}
	lockPath := strings.TrimSuffix(workflowPath, ".md") + ".lock.yml"
	if err := os.Remove(lockPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("unable to remove lock file %s: %w", filepath.Base(lockPath), err)
	}
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Removed workflow no longer listed in manifest: "+filepath.Base(workflowPath)))
	return nil
}

func updateManifestManagedWorkflow(ctx context.Context, update manifestManagedWorkflowUpdate, opts UpdateWorkflowsOptions) error { //nolint:largefunc
	updateManifestLog.Printf("Updating manifest-managed workflow %s: %s@%s -> %s@%s", update.wf.Name, update.currentPath, update.currentRef, update.latestPath, update.latestRef)
	sourceSpecCurrent := sourceSpecWithRef(&SourceSpec{Repo: update.repo, Path: update.currentPath}, update.currentRef)
	newContent, err := downloadWorkflowContentFn(ctx, update.repo, update.latestPath, update.latestRef, opts.Verbose)
	if err != nil {
		return fmt.Errorf("unable to download workflow %s/%s@%s: %w", update.repo, update.latestPath, update.latestRef, err)
	}

	if !opts.Force && update.currentRef == update.latestRef && update.currentPath == update.latestPath {
		sourceContent, err := downloadWorkflowContentFn(ctx, update.repo, update.currentPath, update.currentRef, opts.Verbose)
		if err == nil {
			currentContent, readErr := os.ReadFile(update.wf.Path)
			if readErr == nil && !hasLocalModifications(string(sourceContent), string(currentContent), sourceSpecCurrent, filepath.Dir(update.wf.Path), opts.Verbose) {
				fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Workflow %s is already up to date (%s)", update.wf.Name, shortRef(update.currentRef))))
				return nil
			}
		}
	}

	merge := !opts.NoMerge
	var finalContent string
	var hasConflicts bool
	if merge {
		baseContent, err := downloadWorkflowContentFn(ctx, update.repo, update.currentPath, update.currentRef, opts.Verbose)
		if err != nil {
			updateManifestLog.Printf("Cannot fetch base for 3-way merge of %s, falling back to overwrite: %v", update.wf.Name, err)
			merge = false
		} else {
			currentContent, err := os.ReadFile(update.wf.Path)
			if err != nil {
				return fmt.Errorf("unable to read current workflow: %w", err)
			}
			newSourceSpec := sourceSpecWithRef(&SourceSpec{Repo: update.repo, Path: update.latestPath}, update.latestRef)
			mergedContent, conflicts, mergeErr := MergeWorkflowContent(string(baseContent), string(currentContent), string(newContent), sourceSpecCurrent, newSourceSpec, update.wf.Path, opts.Verbose)
			if mergeErr != nil {
				return fmt.Errorf("unable to merge workflow content: %w", mergeErr)
			}
			finalContent = mergedContent
			hasConflicts = conflicts
		}
	}
	if !merge {
		finalContent = string(newContent)
		processedContent, err := processIncludesInContent(finalContent, &WorkflowSpec{
			RepoSpec: RepoSpec{
				RepoSlug: update.repo,
				Version:  update.latestRef,
			},
			WorkflowPath: update.latestPath,
		}, update.latestRef, filepath.Dir(update.wf.Path), opts.Verbose)
		if err == nil {
			finalContent = processedContent
		}
	}

	finalContent, err = UpdateFieldInFrontmatter(finalContent, "source", update.manifestSource)
	if err != nil {
		return fmt.Errorf("unable to update source frontmatter: %w", err)
	}

	if opts.NoStopAfter {
		cleanedContent, err := RemoveFieldFromOnTrigger(finalContent, "stop-after")
		if err == nil {
			finalContent = cleanedContent
		}
	} else if opts.StopAfter != "" {
		updatedContent, err := SetFieldInOnTrigger(finalContent, "stop-after", opts.StopAfter)
		if err == nil {
			finalContent = updatedContent
		}
	}

	if !opts.DisableSecurityScanner {
		if findings := workflow.ScanMarkdownSecurity(finalContent); len(findings) > 0 {
			return fmt.Errorf("workflow '%s' has %d security scan issue(s); review the findings and resolve them before updating, or pass --no-security-scanner to skip this check", update.wf.Name, len(findings))
		}
	}

	if err := fetchManifestManagedDependencies(ctx, newContent, update.repo, update.latestPath, update.latestRef, filepath.Dir(update.wf.Path), opts.Verbose); err != nil {
		return fmt.Errorf("unable to update workflow dependencies: %w", err)
	}
	if err := os.WriteFile(update.wf.Path, []byte(finalContent), constants.FilePermPublic); err != nil {
		return fmt.Errorf("unable to write updated workflow: %w", err)
	}
	if hasConflicts {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Updated %s from %s to %s with CONFLICTS - please review and resolve manually", update.wf.Name, shortRef(update.currentRef), shortRef(update.latestRef))))
		return nil
	}
	fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("Updated %s from %s to %s", update.wf.Name, shortRef(update.currentRef), shortRef(update.latestRef))))
	if !opts.NoCompile {
		if err := compileWorkflowsForUpdate(ctx, []string{update.wf.Path}, opts.WorkflowsDir, opts.EngineOverride, opts.Verbose, opts.Approve); err != nil {
			return fmt.Errorf("unable to compile updated workflow: %w", err)
		}
	}
	return nil
}

func addManifestManagedWorkflow(ctx context.Context, targetDir, name, repo, latestPath, latestRef, manifestSource string, opts UpdateWorkflowsOptions) error {
	updateManifestLog.Printf("Adding new manifest-managed workflow %s from %s/%s@%s", name, repo, latestPath, latestRef)
	newContent, err := downloadWorkflowContentFn(ctx, repo, latestPath, latestRef, opts.Verbose)
	if err != nil {
		return fmt.Errorf("unable to download new manifest workflow %s/%s@%s: %w", repo, latestPath, latestRef, err)
	}

	content, err := UpdateFieldInFrontmatter(string(newContent), "source", manifestSource)
	if err != nil {
		return fmt.Errorf("unable to add source frontmatter for %s: %w", name, err)
	}
	if opts.NoStopAfter {
		cleanedContent, err := RemoveFieldFromOnTrigger(content, "stop-after")
		if err == nil {
			content = cleanedContent
		}
	} else if opts.StopAfter != "" {
		updatedContent, err := SetFieldInOnTrigger(content, "stop-after", opts.StopAfter)
		if err == nil {
			content = updatedContent
		}
	}
	if !opts.DisableSecurityScanner {
		if findings := workflow.ScanMarkdownSecurity(content); len(findings) > 0 {
			return fmt.Errorf("workflow '%s' has %d security scan issue(s); review the findings and resolve them before adding, or pass --no-security-scanner to skip this check", name, len(findings))
		}
	}

	destPath := filepath.Join(targetDir, name+".md")
	if err := fetchManifestManagedDependencies(ctx, []byte(content), repo, latestPath, latestRef, targetDir, opts.Verbose); err != nil {
		return fmt.Errorf("unable to install workflow dependencies: %w", err)
	}
	if err := os.WriteFile(destPath, []byte(content), constants.FilePermPublic); err != nil {
		return fmt.Errorf("unable to write new manifest workflow %s: %w", destPath, err)
	}
	fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Added new workflow from manifest: "+filepath.Base(destPath)))
	if !opts.NoCompile {
		if err := compileWorkflowsForUpdate(ctx, []string{destPath}, opts.WorkflowsDir, opts.EngineOverride, opts.Verbose, opts.Approve); err != nil {
			return fmt.Errorf("unable to compile new manifest workflow: %w", err)
		}
	}
	return nil
}
