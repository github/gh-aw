package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/fileutil"
	"github.com/github/gh-aw/pkg/setutil"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/parser"
)

// includeDirectivePattern matches @include or @include? directives with their path argument
var includeDirectivePattern = regexp.MustCompile(`^@include(\?)?\s+(.+)$`)
var downloadRemoteImportFile = parser.DownloadFileFromGitHub
var downloadRemoteRuntimeImportFile = parser.DownloadFileFromGitHubForHost

// includesFetcher is the function type used by fetchAndSaveRemoteIncludes to retrieve
// a single include file. Passing a non-nil value overrides the default FetchIncludeFromSource
// implementation; this is used in tests to avoid real network calls.
type includesFetcher func(ctx context.Context, includePath string, baseSpec *WorkflowSpec, verbose bool) ([]byte, string, error)

type runtimeImportFetcher func(ctx context.Context, owner, repo, path, ref, host string) ([]byte, error)

type runtimeImportOpts struct {
	owner      string
	repo       string
	ref        string
	host       string
	repoRoot   string
	verbose    bool
	force      bool
	tracker    *FileTracker
	seen       map[string]struct{}
	downloadFn runtimeImportFetcher
}

func fetchAndSaveRemoteRuntimeImports(ctx context.Context, content string, spec *WorkflowSpec, targetDir string, verbose bool, force bool, tracker *FileTracker) error {
	if spec.RepoSlug == "" {
		return nil
	}

	parts := strings.SplitN(spec.RepoSlug, "/", 2)
	if len(parts) != 2 {
		return nil
	}

	ref := spec.Version
	if ref == "" {
		defaultBranch, err := getRepoDefaultBranch(ctx, spec.RepoSlug)
		if err != nil {
			remoteWorkflowLog.Printf("Failed to resolve default branch for %s, falling back to 'main': %v", spec.RepoSlug, err)
			ref = "main"
		} else {
			ref = defaultBranch
		}
		spec.Version = ref
	}

	opts := runtimeImportOpts{
		owner:      parts[0],
		repo:       parts[1],
		ref:        ref,
		host:       spec.Host,
		repoRoot:   filepath.Dir(filepath.Dir(targetDir)),
		verbose:    verbose,
		force:      force,
		tracker:    tracker,
		seen:       make(map[string]struct{}),
		downloadFn: downloadRemoteRuntimeImportFile,
	}

	return fetchRemoteRuntimeImportsRecursive(ctx, content, getParentDir(spec.WorkflowPath), opts)
}

func fetchRemoteRuntimeImportsRecursive(ctx context.Context, content, currentBaseDir string, opts runtimeImportOpts) error {
	body := content
	if parsed, err := parser.ExtractFrontmatterFromContent(content); err == nil {
		body = parsed.Markdown
	}

	for _, imp := range parser.ExtractBodyLevelImportPaths(body, currentBaseDir) {
		remoteFilePath := strings.TrimSpace(imp.Path)
		if remoteFilePath == "" {
			continue
		}
		if rest, ok := strings.CutPrefix(remoteFilePath, "/"); ok {
			remoteFilePath = rest
		}
		remoteFilePath = path.Clean(remoteFilePath)
		if remoteFilePath == "." || remoteFilePath == ".." || strings.HasPrefix(remoteFilePath, "../") {
			return fmt.Errorf("runtime-import path %q escapes repository root", imp.Path)
		}
		if setutil.Contains(opts.seen, remoteFilePath) {
			continue
		}
		opts.seen[remoteFilePath] = struct{}{}

		targetPath := filepath.Join(opts.repoRoot, filepath.FromSlash(remoteFilePath))
		if err := fileutil.ValidatePathWithinBase(opts.repoRoot, targetPath); err != nil {
			return fmt.Errorf("refusing to write runtime import outside repository root: %w", err)
		}

		fileExists := false
		if fileutil.FileExists(targetPath) {
			fileExists = true
			if !opts.force {
				continue
			}
		}

		importContent, err := opts.downloadFn(ctx, opts.owner, opts.repo, remoteFilePath, opts.ref, opts.host)
		if err != nil {
			return fmt.Errorf("failed to fetch runtime import %s: %w", remoteFilePath, err)
		}
		if err := os.MkdirAll(filepath.Dir(targetPath), constants.DirPermPublic); err != nil {
			return fmt.Errorf("failed to create directory for runtime import %s: %w", remoteFilePath, err)
		}
		if err := os.WriteFile(targetPath, importContent, constants.FilePermSensitive); err != nil {
			return fmt.Errorf("failed to write runtime import %s: %w", remoteFilePath, err)
		}
		if opts.tracker != nil {
			if fileExists {
				opts.tracker.TrackModified(targetPath)
			} else {
				opts.tracker.TrackCreated(targetPath)
			}
		}

		if err := fetchRemoteRuntimeImportsRecursive(ctx, string(importContent), path.Dir(remoteFilePath), opts); err != nil {
			return err
		}
	}

	return nil
}

// FetchIncludeFromSource fetches an include file from GitHub directly using a workflowspec format path.
// The includePath should be in the format: owner/repo/path/to/file.md[@ref]
// If the includePath is a relative path, it's resolved relative to the baseSpec.
// Returns: (content, section, error) where section is the #fragment from the path (e.g., "#section-name").
func FetchIncludeFromSource(ctx context.Context, includePath string, baseSpec *WorkflowSpec, verbose bool) ([]byte, string, error) {
	baseSpecStr := "<nil>"
	if baseSpec != nil {
		baseSpecStr = baseSpec.String()
	}
	remoteWorkflowLog.Printf("Fetching include from source: path=%s, base=%s", includePath, baseSpecStr)

	// Extract section reference (e.g., "#section-name") from the path upfront
	// This ensures consistent behavior regardless of which code path is taken
	cleanPath := includePath
	var section string
	if idx := strings.Index(includePath, "#"); idx != -1 {
		cleanPath = includePath[:idx]
		section = includePath[idx:]
	}

	// Check if this is a workflowspec format (owner/repo/path[@ref])
	if isWorkflowSpecFormat(cleanPath) {
		return fetchWorkflowSpecInclude(ctx, cleanPath, includePath, section)
	}

	return fetchRelativeSourceInclude(ctx, cleanPath, includePath, section, baseSpec)
}

func fetchWorkflowSpecInclude(ctx context.Context, cleanPath, includePath, section string) ([]byte, string, error) {
	// Split on @ to get path and ref
	parts := strings.SplitN(cleanPath, "@", 2)
	pathPart := parts[0]
	var ref string
	if len(parts) == 2 {
		ref = parts[1]
	} else {
		ref = "main"
	}

	// Parse path: owner/repo/path/to/file.md
	slashParts := strings.Split(pathPart, "/")
	if len(slashParts) < 3 {
		return nil, section, errors.New("invalid workflowspec: must be owner/repo/path[@ref]")
	}

	owner := slashParts[0]
	repo := slashParts[1]
	filePath := strings.Join(slashParts[2:], "/")

	// Download the file
	content, err := parser.DownloadFileFromGitHub(ctx, owner, repo, filePath, ref)
	if err != nil {
		return nil, section, fmt.Errorf("failed to fetch include from %s: %w", includePath, err)
	}

	return content, section, nil
}

func fetchRelativeSourceInclude(ctx context.Context, cleanPath, includePath, section string, baseSpec *WorkflowSpec) ([]byte, string, error) {
	// For relative paths, resolve against the base spec
	if baseSpec != nil && baseSpec.RepoSlug != "" {
		parts := strings.SplitN(baseSpec.RepoSlug, "/", 2)
		if len(parts) == 2 {
			owner := parts[0]
			repo := parts[1]
			ref := baseSpec.Version
			if ref == "" {
				ref = "main"
			}

			// Remove @ ref suffix if present in the clean path (for relative paths with explicit refs)
			filePath := cleanPath
			if idx := strings.Index(filePath, "@"); idx != -1 {
				filePath = filePath[:idx]
			}

			// If it's a relative path starting with shared/, it's relative to .github/
			var fullPath string
			if strings.HasPrefix(filePath, "shared/") {
				fullPath = constants.GithubDir + filePath
			} else {
				// Otherwise, resolve relative to the workflow path directory
				baseDir := getParentDir(baseSpec.WorkflowPath)
				if baseDir != "" {
					fullPath = baseDir + "/" + filePath
				} else {
					fullPath = filePath
				}
			}

			content, err := parser.DownloadFileFromGitHub(ctx, owner, repo, fullPath, ref)
			if err != nil {
				return nil, section, fmt.Errorf("failed to fetch include %s from %s/%s: %w", filePath, owner, repo, err)
			}

			return content, section, nil
		}
	}

	return nil, section, fmt.Errorf("cannot resolve include path: %s (no base spec provided)", includePath)
}

// fetchAndSaveRemoteFrontmatterImports fetches and saves files referenced in the frontmatter
// 'imports:' field of a remote workflow. These relative-path imports are resolved against
// the workflow's location in the source repository and saved locally so compilation can find them.
// This is analogous to fetchAndSaveRemoteIncludes, which handles @include directives in the
// markdown body; this function handles the YAML frontmatter 'imports:' field.
// Import failures are non-fatal (best-effort); the compiler will report any still-missing files.
func fetchAndSaveRemoteFrontmatterImports(ctx context.Context, content string, spec *WorkflowSpec, targetDir string, verbose bool, force bool, tracker *FileTracker) error {
	return fetchAndSaveRemoteFrontmatterImportsWithOptions(ctx, content, spec, targetDir, verbose, force, tracker, false)
}

func fetchAndSaveRemoteFrontmatterImportsWithOptions(ctx context.Context, content string, spec *WorkflowSpec, targetDir string, verbose bool, force bool, tracker *FileTracker, strict bool) error {
	if spec.RepoSlug == "" {
		return nil
	}

	remoteWorkflowLog.Printf("Fetching frontmatter imports for workflow: repo=%s, path=%s", spec.RepoSlug, spec.WorkflowPath)

	parts := strings.SplitN(spec.RepoSlug, "/", 2)
	if len(parts) != 2 {
		return nil
	}
	owner, repo := parts[0], parts[1]
	ref := spec.Version
	if ref == "" {
		// Resolve the actual default branch of the source repo rather than assuming "main"
		defaultBranch, err := getRepoDefaultBranch(ctx, spec.RepoSlug)
		if err != nil {
			remoteWorkflowLog.Printf("Failed to resolve default branch for %s, falling back to 'main': %v", spec.RepoSlug, err)
			ref = "main"
		} else {
			ref = defaultBranch
		}
		// Persist the resolved default ref so other callers do not need to re-resolve it
		spec.Version = ref
	}

	// workflowBaseDir is the directory of the top-level workflow in the source repo
	// (e.g. ".github/workflows"). It serves as both the starting point for resolving
	// relative imports and as the prefix to strip when computing local target paths.
	workflowBaseDir := getParentDir(spec.WorkflowPath)

	// seen is keyed by fully-resolved remote file path. It is shared across all recursion
	// levels so that every import (at any depth) is downloaded at most once and import
	// cycles (A imports B, B imports A) are broken without infinite recursion.
	seen := make(map[string]struct {
	})
	return fetchFrontmatterImportsRecursive(ctx, content, workflowBaseDir, frontmatterImportsOpts{
		owner:           owner,
		repo:            repo,
		ref:             ref,
		originalBaseDir: workflowBaseDir,
		targetDir:       targetDir,
		verbose:         verbose,
		force:           force,
		tracker:         tracker,
		strict:          strict,
		seen:            seen,
	})
}

// frontmatterImportsOpts holds the constant parameters for fetchFrontmatterImportsRecursive.
// Only `content` and `currentBaseDir` change per recursion level; everything else is constant.
type frontmatterImportsOpts struct {
	owner           string
	repo            string
	ref             string
	originalBaseDir string
	targetDir       string
	verbose         bool
	force           bool
	tracker         *FileTracker
	strict          bool
	seen            map[string]struct{}
	// downloadFn is the function used to fetch file content from the source repository.
	// When nil, parser.DownloadFileFromGitHub is used. Tests may inject a stub to avoid
	// network calls and observe which paths were requested.
	downloadFn func(ctx context.Context, owner, repo, path, ref string) ([]byte, error)
}

// fetchFrontmatterImportsRecursive is the internal worker for fetchAndSaveRemoteFrontmatterImports.
//
// Parameters that change per recursion level:
//   - content: the text of the file whose imports are being processed
//   - currentBaseDir: directory of that file inside the source repo (used to resolve relative paths)
//
// Parameters that remain constant across all recursion levels (in opts):
//   - owner, repo, ref: source repository coordinates
//   - originalBaseDir: directory of the top-level workflow (used to map remote paths → local paths)
//   - targetDir: the `.github/workflows` directory in the user's repo
//   - seen: shared visited set (keyed by fully-resolved remote path) — prevents cycles & duplicates
func fetchFrontmatterImportsRecursive(ctx context.Context, content, currentBaseDir string, opts frontmatterImportsOpts) error {
	result, err := parser.ExtractFrontmatterFromContent(content)
	if err != nil || result.Frontmatter == nil {
		return nil
	}

	importsField, exists := result.Frontmatter["imports"]
	if !exists {
		return nil
	}

	importPaths := frontmatterImportPaths(importsField)
	if len(importPaths) == 0 {
		return nil
	}
	remoteWorkflowLog.Printf("Processing %d frontmatter imports recursively: owner=%s, repo=%s, ref=%s", len(importPaths), opts.owner, opts.repo, opts.ref)
	absTargetDir, err := filepath.Abs(opts.targetDir)
	if err != nil {
		if opts.strict {
			return fmt.Errorf("failed to resolve import target directory: %w", err)
		}
		return nil
	}
	for _, importPath := range importPaths {
		if err := fetchFrontmatterImport(ctx, importPath, currentBaseDir, absTargetDir, opts); err != nil {
			return err
		}
	}
	return nil
}

func frontmatterImportPaths(importsField any) []string {
	var importPaths []string
	switch v := importsField.(type) {
	case []any:
		for _, item := range v {
			switch importItem := item.(type) {
			case string:
				importPaths = append(importPaths, importItem)
			case map[string]any:
				// Handle uses: and path: forms (mirrors GitHub Actions reusable workflow syntax)
				if usesVal, ok := importItem["uses"]; ok {
					if p, ok := usesVal.(string); ok {
						importPaths = append(importPaths, p)
					}
				} else if pathVal, ok := importItem["path"]; ok {
					if p, ok := pathVal.(string); ok {
						importPaths = append(importPaths, p)
					}
				}
			}
		}
	case []string:
		importPaths = v
	}
	return importPaths
}

func fetchFrontmatterImport(ctx context.Context, importPath, currentBaseDir, absTargetDir string, opts frontmatterImportsOpts) error {
	// Skip workflowspec-format imports (already pinned to a remote ref)
	if isWorkflowSpecFormat(importPath) {
		return nil
	}

	// Strip any section reference (file.md#Section → file.md)
	filePath := importPath
	if before, _, hasSec := strings.Cut(importPath, "#"); hasSec {
		filePath = before
	}
	if filePath == "" {
		return nil
	}

	remoteFilePath := resolveFrontmatterRemotePath(filePath, currentBaseDir, opts.originalBaseDir)
	if remoteFilePath == ".." || strings.HasPrefix(remoteFilePath, "../") {
		if opts.strict {
			return fmt.Errorf("import path %q escapes repository root", importPath)
		}
		if opts.verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Skipping import with unsafe path: %q", importPath)))
		}
		return nil
	}
	if setutil.Contains(opts.seen, remoteFilePath) {
		remoteWorkflowLog.Printf("Skipping already-seen import: %s", remoteFilePath)
		return nil
	}
	opts.seen[remoteFilePath] = struct{}{}
	targetPath, err := resolveFrontmatterImportTarget(importPath, remoteFilePath, absTargetDir, opts)
	if err != nil || targetPath == "" {
		return err
	}
	return fetchAndSaveFrontmatterImport(ctx, remoteFilePath, targetPath, opts)
}

func resolveFrontmatterRemotePath(filePath, currentBaseDir, originalBaseDir string) string {
	// Resolve the remote file path to an absolute repo path.
	// Use path (not filepath) because this is always a forward-slash URL/API path.
	var remoteFilePath string
	if rest, ok := strings.CutPrefix(filePath, "/"); ok {
		// Absolute path from repo root (e.g. "/scripts/helper.md")
		remoteFilePath = rest
	} else if strings.HasPrefix(filePath, "./") || strings.HasPrefix(filePath, "../") {
		// Explicitly-relative path (e.g. "./serena.md"): resolve relative to the
		// current importing file's directory so that sibling-file references work
		// correctly regardless of nesting depth.
		if currentBaseDir != "" {
			remoteFilePath = path.Join(currentBaseDir, filePath)
		} else {
			remoteFilePath = filePath
		}
	} else {
		// Non-explicit relative path: resolution strategy mirrors the compiler's
		// determineNestedBaseDir logic — it depends on whether the path contains a
		// "/" separator.
		//
		// Paths WITHOUT "/" (e.g. "helper.md"): the importing file treats them as
		// siblings in its own directory, so resolve relative to currentBaseDir.
		// Example: "control-precompute.md" from ".github/workflows/shared/control.md"
		//          → ".github/workflows/shared/control-precompute.md"
		//
		// Paths WITH "/" (e.g. "shared/foo.md"): workflows in this repository write
		// these paths relative to the workflow root (.github/workflows), not relative to
		// the importing file's directory, so resolve against originalBaseDir.
		// Example: "shared/reporting.md" from ".github/workflows/shared/daily-audit-base.md"
		//          → ".github/workflows/shared/reporting.md"
		var baseDir string
		if !strings.Contains(filePath, "/") {
			baseDir = currentBaseDir
		} else {
			baseDir = originalBaseDir
		}
		if baseDir != "" {
			remoteFilePath = path.Join(baseDir, filePath)
		} else {
			remoteFilePath = filePath
		}
	}
	remoteFilePath = path.Clean(remoteFilePath)
	return remoteFilePath
}

func resolveFrontmatterImportTarget(importPath, remoteFilePath, absTargetDir string, opts frontmatterImportsOpts) (string, error) {
	// Derive the local path relative to targetDir by stripping the original base-dir
	// prefix from the remote path. This ensures that imports in nested files resolve
	// to the correct location regardless of how many levels deep the recursion goes.
	//
	// Example: originalBaseDir=".github/workflows"
	//   remoteFilePath=".github/workflows/shared/analysis.md" → localRelPath="shared/analysis.md"
	//   (nested) remoteFilePath=".github/workflows/other.md"  → localRelPath="other.md"
	var localRelPath string
	if opts.originalBaseDir != "" && strings.HasPrefix(remoteFilePath, opts.originalBaseDir+"/") {
		localRelPath = remoteFilePath[len(opts.originalBaseDir)+1:]
	} else {
		// Workflow at repo root, or import outside the original base dir:
		// use the full remote path relative to targetDir.
		localRelPath = remoteFilePath
	}
	localRelPath = filepath.Clean(filepath.FromSlash(localRelPath))
	// Strip any leading separator produced by Clean on root-relative paths.
	localRelPath = strings.TrimLeft(localRelPath, string(filepath.Separator))
	// Reject empty or "." paths (would point to targetDir itself) as a safety guard.
	// ".." cannot appear here because remoteFilePath was already rejected above if it
	// started with "..", and path.Clean cannot introduce new ".." components.
	if localRelPath == "" || localRelPath == "." {
		if opts.strict {
			return "", fmt.Errorf("invalid import path %q", importPath)
		}
		return "", nil
	}
	targetPath := filepath.Join(opts.targetDir, localRelPath)

	// Belt-and-suspenders: verify the resolved path is inside targetDir
	absTargetPath, absErr := filepath.Abs(targetPath)
	if absErr != nil {
		if opts.strict {
			return "", fmt.Errorf("failed to resolve import target path %q: %w", importPath, absErr)
		}
		return "", nil
	}
	if rel, relErr := filepath.Rel(absTargetDir, absTargetPath); relErr != nil || strings.HasPrefix(rel, "..") {
		if opts.strict {
			return "", fmt.Errorf("refusing to write import outside target directory: %q", importPath)
		}
		if opts.verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Refusing to write import outside target directory: %q", importPath)))
		}
		return "", nil
	}
	return targetPath, nil
}

func fetchAndSaveFrontmatterImport(ctx context.Context, remoteFilePath, targetPath string, opts frontmatterImportsOpts) error {
	// Check existence before downloading: if the file already exists and force=false,
	// skip the download entirely (no unnecessary network round-trip). However, still
	// recurse into the existing file's imports so that any transitive dependencies
	// it references are fetched even when the parent file was already present.
	fileExists := false
	if fileutil.FileExists(targetPath) {
		fileExists = true
		if !opts.force {
			if opts.verbose {
				fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Import file already exists, skipping: "+targetPath))
			}
			return fetchExistingFrontmatterImportDependencies(ctx, remoteFilePath, targetPath, opts)
		}
	}

	importContent, saved, err := downloadAndWriteFrontmatterImport(ctx, remoteFilePath, targetPath, opts)
	if err != nil || !saved {
		return err
	}
	if opts.verbose {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Fetched import: "+targetPath))
	}
	if opts.tracker != nil {
		if fileExists {
			opts.tracker.TrackModified(targetPath)
		} else {
			opts.tracker.TrackCreated(targetPath)
		}
	}
	importedBaseDir := path.Dir(remoteFilePath)
	if err := fetchFrontmatterImportsRecursive(ctx, string(importContent), importedBaseDir, opts); err != nil && opts.strict {
		return err
	}
	return nil
}

func fetchExistingFrontmatterImportDependencies(ctx context.Context, remoteFilePath, targetPath string, opts frontmatterImportsOpts) error {
	if existingContent, readErr := os.ReadFile(targetPath); readErr == nil {
		importedBaseDir := path.Dir(remoteFilePath)
		if err := fetchFrontmatterImportsRecursive(ctx, string(existingContent), importedBaseDir, opts); err != nil && opts.strict {
			return err
		}
	} else {
		if opts.strict {
			return fmt.Errorf("failed to read existing import %s: %w", targetPath, readErr)
		}
		remoteWorkflowLog.Printf("Failed to read existing import %s for recursion: %v", targetPath, readErr)
	}
	return nil
}

func downloadAndWriteFrontmatterImport(ctx context.Context, remoteFilePath, targetPath string, opts frontmatterImportsOpts) ([]byte, bool, error) {
	// Download from the source repository
	downloadFn := opts.downloadFn
	if downloadFn == nil {
		downloadFn = downloadRemoteImportFile
	}
	importContent, err := downloadFn(ctx, opts.owner, opts.repo, remoteFilePath, opts.ref)
	if err != nil {
		if opts.strict {
			return nil, false, fmt.Errorf("failed to fetch import %s: %w", remoteFilePath, err)
		}
		remoteWorkflowLog.Printf("Failed to download import %s from %s/%s@%s: %v", remoteFilePath, opts.owner, opts.repo, opts.ref, err)
		if opts.verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to fetch import %s: %v", remoteFilePath, err)))
		}
		return nil, false, nil
	}

	// Create the parent directory if needed
	if err := os.MkdirAll(filepath.Dir(targetPath), constants.DirPermPublic); err != nil {
		if opts.strict {
			return nil, false, fmt.Errorf("failed to create directory for import %s: %w", remoteFilePath, err)
		}
		if opts.verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to create directory for import %s: %v", remoteFilePath, err)))
		}
		return nil, false, nil
	}

	// Write the file
	if err := os.WriteFile(targetPath, importContent, constants.FilePermSensitive); err != nil {
		if opts.strict {
			return nil, false, fmt.Errorf("failed to write import %s: %w", remoteFilePath, err)
		}
		if opts.verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to write import %s: %v", remoteFilePath, err)))
		}
		return nil, false, nil
	}
	return importContent, true, nil
}

// includesFetchOptions holds the constant parameters for fetchAndSaveRemoteIncludesWithOptions.
// Only `content` changes across the recursive calls made while processing nested includes;
// everything else stays the same for the duration of a single top-level fetch operation.
type includesFetchOptions struct {
	spec      *WorkflowSpec
	targetDir string
	verbose   bool
	force     bool
	tracker   *FileTracker
	fetchFn   includesFetcher
	strict    bool
}

func fetchAndSaveRemoteIncludesWithOptions(ctx context.Context, content string, opts includesFetchOptions) error {
	remoteWorkflowLog.Printf("Fetching remote includes for workflow: %s", opts.spec.String())
	if opts.fetchFn == nil {
		opts.fetchFn = FetchIncludeFromSource
	}

	// Parse the workflow content to find @include directives
	scanner := bufio.NewScanner(strings.NewReader(content))
	seen := make(map[string]struct {
	})

	for scanner.Scan() {
		line := scanner.Text()
		matches := includeDirectivePattern.FindStringSubmatch(line)
		if matches == nil {
			continue
		}

		isOptional := matches[1] == "?"
		includePath := strings.TrimSpace(matches[2])

		// Remove section reference for file fetching
		filePath := includePath
		if before, _, ok := strings.Cut(includePath, "#"); ok {
			filePath = before
		}

		// Reject paths with traversal components before any further processing.
		if strings.Contains(filePath, "..") {
			return fmt.Errorf("include path %q contains illegal path traversal components", filePath)
		}

		// Skip if already processed
		if setutil.Contains(seen, filePath) {
			continue
		}
		seen[filePath] = struct {
		}{}

		if err := fetchAndSaveRemoteInclude(ctx, includePath, filePath, isOptional, opts); err != nil {
			return err
		}
	}
	return nil
}

func fetchAndSaveRemoteInclude(ctx context.Context, includePath, filePath string, isOptional bool, opts includesFetchOptions) error {
	// Fetch the include file
	includeContent, _, err := opts.fetchFn(ctx, includePath, opts.spec, opts.verbose)
	if err != nil {
		if isOptional {
			if opts.verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage("Optional include not found: "+includePath))
			}
			return nil
		}
		return fmt.Errorf("failed to fetch include %s: %w", includePath, err)
	}

	// Determine target path for the include file
	targetPath, err := remoteIncludeTargetPath(filePath, opts)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(targetPath), constants.DirPermPublic); err != nil {
		return fmt.Errorf("failed to create directory for %s: %w", targetPath, err)
	}
	fileExists := false
	if fileutil.FileExists(targetPath) {
		fileExists = true
		if !opts.force {
			if opts.verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage("Include file already exists, skipping: "+targetPath))
			}
			return nil
		}
	}
	return writeRemoteIncludeAndFetchDependencies(ctx, includeContent, filePath, targetPath, fileExists, opts)
}

func remoteIncludeTargetPath(filePath string, opts includesFetchOptions) (string, error) {
	var targetPath string
	if strings.HasPrefix(filePath, "shared/") {
		// shared/ files go to .github/shared/
		targetPath = filepath.Join(filepath.Dir(opts.targetDir), filePath)
	} else if isWorkflowSpecFormat(filePath) {
		// Workflowspec includes: extract just the filename and put in shared/
		parts := strings.Split(filePath, "/")
		filename := parts[len(parts)-1]
		targetPath = filepath.Join(filepath.Dir(opts.targetDir), "shared", filename)
	} else {
		// Relative includes go alongside the workflow
		targetPath = filepath.Join(opts.targetDir, filePath)
	}
	writeBase := opts.targetDir
	if strings.HasPrefix(filePath, "shared/") || isWorkflowSpecFormat(filePath) {
		writeBase = filepath.Join(filepath.Dir(opts.targetDir), "shared")
	}
	if err := fileutil.ValidatePathWithinBase(writeBase, targetPath); err != nil {
		return "", fmt.Errorf("refusing to write include outside allowed directory %s: %w", writeBase, err)
	}
	return targetPath, nil
}

func writeRemoteIncludeAndFetchDependencies(ctx context.Context, includeContent []byte, filePath, targetPath string, fileExists bool, opts includesFetchOptions) error {
	// Write the include file
	if err := os.WriteFile(targetPath, includeContent, constants.FilePermSensitive); err != nil {
		return fmt.Errorf("failed to write include file %s: %w", targetPath, err)
	}

	if opts.verbose {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Fetched include: "+targetPath))
	}

	// Track the file
	if opts.tracker != nil {
		if fileExists {
			opts.tracker.TrackModified(targetPath)
		} else {
			opts.tracker.TrackCreated(targetPath)
		}
	}

	// Recursively fetch includes from the fetched file
	if err := fetchAndSaveRemoteIncludesWithOptions(ctx, string(includeContent), opts); err != nil {
		if opts.strict {
			return fmt.Errorf("failed to fetch nested includes from %s: %w", filePath, err)
		}
		if opts.verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to fetch nested includes from %s: %v", filePath, err)))
		}
	}
	return nil
}

// fetchAllRemoteDependencies fetches all remote dependencies for a workflow:
// includes (@include directives), frontmatter imports, dispatch workflows, and resources.
// This is the single entry point shared by both the add and trial commands.
//
// Error handling is intentionally asymmetric:
//   - @include and frontmatter import errors are best-effort: failures emit a warning when
//     verbose is true but do not stop the overall operation.
//   - Dispatch-workflow and resource errors are fatal and are returned to the caller.
func fetchAllRemoteDependencies(ctx context.Context, content string, spec *WorkflowSpec, targetDir string, verbose bool, force bool, tracker *FileTracker) error {
	return fetchAllRemoteDependenciesWithOptions(ctx, content, spec, targetDir, verbose, force, tracker, false)
}

func fetchAllRemoteDependenciesStrict(ctx context.Context, content string, spec *WorkflowSpec, targetDir string, verbose bool, force bool, tracker *FileTracker) error {
	return fetchAllRemoteDependenciesWithOptions(ctx, content, spec, targetDir, verbose, force, tracker, true)
}

func fetchAllRemoteDependenciesWithOptions(ctx context.Context, content string, spec *WorkflowSpec, targetDir string, verbose bool, force bool, tracker *FileTracker, strict bool) error {
	remoteWorkflowLog.Printf("Fetching all remote dependencies: spec=%s, targetDir=%s, force=%v", spec.String(), targetDir, force)
	// Fetch and save @include directive dependencies (best-effort: errors are not fatal).
	if err := fetchAndSaveRemoteIncludesWithOptions(ctx, content, includesFetchOptions{
		spec:      spec,
		targetDir: targetDir,
		verbose:   verbose,
		force:     force,
		tracker:   tracker,
		strict:    strict,
	}); err != nil {
		if strict {
			return fmt.Errorf("failed to fetch include dependencies: %w", err)
		}
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to fetch include dependencies: %v", err)))
		}
	}
	// Fetch and save frontmatter 'imports:' dependencies so they are available
	// locally during compilation. Keeping these as relative paths (not workflowspecs)
	// ensures the compiler resolves them from disk rather than downloading from GitHub.
	// Best-effort: errors are not fatal.
	if err := fetchAndSaveRemoteFrontmatterImportsWithOptions(ctx, content, spec, targetDir, verbose, force, tracker, strict); err != nil {
		if strict {
			return fmt.Errorf("failed to fetch frontmatter import dependencies: %w", err)
		}
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to fetch frontmatter import dependencies: %v", err)))
		}
	}
	// Fetch and save required runtime-import dependencies so installs include the
	// explicit runtime-import closure without copying unrelated .github contents.
	if err := fetchAndSaveRemoteRuntimeImports(ctx, content, spec, targetDir, verbose, force, tracker); err != nil {
		if strict {
			return fmt.Errorf("failed to fetch runtime-import dependencies: %w", err)
		}
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage("Failed to fetch runtime-import dependencies; activation may fail"))
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(err.Error()))
		}
	}
	return fetchRemoteWorkflowAndResourceDependencies(ctx, content, spec, targetDir, verbose, force, tracker)
}

func fetchRemoteWorkflowAndResourceDependencies(ctx context.Context, content string, spec *WorkflowSpec, targetDir string, verbose bool, force bool, tracker *FileTracker) error {
	// Fetch and save workflows referenced in safe-outputs.dispatch-workflow so they are
	// available locally. Workflow names using GitHub Actions expression syntax are skipped.
	if err := fetchAndSaveRemoteDispatchWorkflows(ctx, content, spec, targetDir, verbose, force, tracker); err != nil {
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to fetch dispatch workflow dependencies: %v", err)))
		}
		return fmt.Errorf("failed to fetch dispatch workflow dependencies: %w", err)
	}
	// Fetch and save worker workflows referenced in safe-outputs.call-workflow so they are
	// available locally. Workflow names using GitHub Actions expression syntax are skipped.
	if err := fetchAndSaveRemoteCallWorkflows(ctx, content, spec, targetDir, verbose, force, tracker); err != nil {
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to fetch call-workflow worker dependencies: %v", err)))
		}
		return fmt.Errorf("failed to fetch call-workflow worker dependencies: %w", err)
	}
	// Fetch files listed in the 'resources:' frontmatter field (additional workflow or
	// action files that should be present alongside this workflow).
	if err := fetchAndSaveRemoteResources(ctx, content, spec, targetDir, verbose, force, tracker); err != nil {
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to fetch resource dependencies: %v", err)))
		}
		return fmt.Errorf("failed to fetch resource dependencies: %w", err)
	}
	return nil
}
