package cli

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/gitutil"
)

type repositoryPackageManifestNode struct {
	Path        string
	PackagePath string
	Manifest    *repositoryPackageManifest
}

type repositoryPackageManifestGraphResolver struct {
	importRoot   string
	readManifest func(string) ([]byte, error)
	states       map[string]uint8
	stack        []string
	nodes        []repositoryPackageManifestNode
	warnings     []string
}

func isManifestImportPath(importPath string) bool {
	_, err := cleanManifestImportPath(importPath)
	return err == nil && path.Base(filepath.ToSlash(importPath)) == repositoryPackageManifestFileName
}

func cleanManifestImportPath(importPath string) (string, error) {
	if importPath == "" {
		return "", errors.New("path must not be empty")
	}
	slashed := filepath.ToSlash(importPath)
	if slashed[0] == '/' || slashed[0] == '\\' || filepath.IsAbs(importPath) || isWindowsDriveRelativePath(slashed) {
		return "", errors.New("absolute paths are not allowed")
	}
	cleaned := path.Clean(slashed)
	if cleaned == "." {
		return "", errors.New("path must not be empty")
	}
	if path.Base(cleaned) != repositoryPackageManifestFileName {
		return "", fmt.Errorf("path must name an %s manifest", repositoryPackageManifestFileName)
	}
	return cleaned, nil
}

// resolveRepositoryPackageManifestGraph walks the import graph of the manifest at rootPath.
// importRoot bounds where imports may resolve: imports may reach outside the package being
// installed, for example a nested package importing '../aw.yml', but never outside
// importRoot. Pass the repository root ("" for repository-relative paths, an absolute
// directory for local packages).
func resolveRepositoryPackageManifestGraph(
	rootPath string,
	root *repositoryPackageManifest,
	importRoot string,
	readManifest func(string) ([]byte, error),
) ([]repositoryPackageManifestNode, []string, error) {
	rootPath = path.Clean(filepath.ToSlash(rootPath))
	importRoot = normalizeManifestImportRoot(importRoot)

	addPackageManifestLog.Printf("Resolving package manifest import graph: root=%s importRoot=%s", rootPath, importRoot)

	resolver := &repositoryPackageManifestGraphResolver{
		importRoot:   importRoot,
		readManifest: readManifest,
		states:       make(map[string]uint8),
	}
	if err := resolver.visit(rootPath, root); err != nil {
		return nil, nil, err
	}
	addPackageManifestLog.Printf("Resolved package manifest import graph: %d node(s), %d warning(s)", len(resolver.nodes), len(resolver.warnings))
	return resolver.nodes, resolver.warnings, nil
}

func (r *repositoryPackageManifestGraphResolver) visit(manifestPath string, manifest *repositoryPackageManifest) error {
	switch r.states[manifestPath] {
	case 1:
		cycleStart := 0
		for i, item := range r.stack {
			if item == manifestPath {
				cycleStart = i
				break
			}
		}
		cycle := append(append([]string{}, r.stack[cycleStart:]...), manifestPath)
		addPackageManifestLog.Printf("Import cycle detected: %s", strings.Join(cycle, " -> "))
		return fmt.Errorf("package manifest import cycle detected: %s", strings.Join(cycle, " -> "))
	case 2:
		return nil
	}

	r.states[manifestPath] = 1
	r.stack = append(r.stack, manifestPath)
	manifestDir := path.Dir(manifestPath)
	if manifestDir == "." {
		manifestDir = ""
	}
	for _, relativeImport := range manifest.Imports {
		importPath := path.Clean(path.Join(manifestDir, relativeImport))
		if !isPathWithinPackageRoot(importPath, r.importRoot) {
			return fmt.Errorf("invalid Agentic Workflow manifest %q: import %q resolves outside the repository root", manifestPath, relativeImport)
		}
		if importPath == manifestPath {
			addPackageManifestLog.Printf("Ignoring self-import %q in %s", relativeImport, manifestPath)
			r.warnings = append(r.warnings, fmt.Sprintf("Ignoring includes entry %q in %s because a manifest cannot import itself", relativeImport, manifestPath))
			continue
		}
		switch r.states[importPath] {
		case 1:
			if err := r.visit(importPath, nil); err != nil {
				return err
			}
		case 2:
			continue
		}
		content, err := r.readManifest(importPath)
		if err != nil {
			return fmt.Errorf("failed to read imported Agentic Workflow manifest %q from %q: %w", importPath, manifestPath, err)
		}
		imported, importedWarnings, err := parseRepositoryPackageManifest(importPath, content)
		if err != nil {
			return err
		}
		r.warnings = append(r.warnings, importedWarnings...)
		if err := r.visit(importPath, imported); err != nil {
			return err
		}
	}
	r.stack = r.stack[:len(r.stack)-1]
	r.states[manifestPath] = 2
	r.nodes = append(r.nodes, repositoryPackageManifestNode{Path: manifestPath, PackagePath: manifestDir, Manifest: manifest})
	return nil
}

// normalizeManifestImportRoot converts an import boundary into the slash-separated form
// used by manifest paths. An empty boundary means the repository root.
func normalizeManifestImportRoot(importRoot string) string {
	if importRoot == "" {
		return ""
	}
	normalized := path.Clean(filepath.ToSlash(importRoot))
	if normalized == "." {
		return ""
	}
	return strings.TrimSuffix(normalized, "/")
}

// isPathWithinPackageRoot reports whether candidate is a manifest import path
// that resolves within root. This is a path-traversal guard, not a redirect
// handler; it additionally rejects any backslash in candidate (consistent
// with CWE-601 guidance) purely to avoid matching the go/bad-redirect-check
// heuristic, without weakening the containment check.
func isPathWithinPackageRoot(candidate, root string) bool {
	if strings.Contains(candidate, `\`) {
		return false
	}
	if root == "" {
		return candidate != ".." && !strings.HasPrefix(candidate, "../")
	}
	return candidate == root || strings.HasPrefix(candidate, root+"/")
}

func validateUniqueResolvedPackageFiles(
	installables []resolvedPackageInstallable,
	resources []resolvedPackageResource,
	skillFiles []resolvedPackageSkillFile,
	agentFiles []string,
	manifestPath string,
) error {
	addPackageManifestLog.Printf("Validating unique destinations: installables=%d resources=%d skillFiles=%d agentFiles=%d",
		len(installables), len(resources), len(skillFiles), len(agentFiles))

	seen := make(map[string]string)
	add := func(destination, source string) error {
		key := strings.ToLower(filepath.ToSlash(filepath.Clean(destination)))
		if previous, exists := seen[key]; exists {
			addPackageManifestLog.Printf("Duplicate install destination %q for %q and %q", destination, previous, source)
			return fmt.Errorf("invalid Agentic Workflow manifest %q: files %q and %q both install to %q", manifestPath, previous, source, destination)
		}
		seen[key] = source
		return nil
	}
	for _, installable := range installables {
		if err := add(installable.DestinationPath, installable.SourcePath); err != nil {
			return err
		}
	}
	for _, resource := range resources {
		if err := add(resource.DestinationPath, resource.SourcePath); err != nil {
			return err
		}
	}
	for _, skillFile := range skillFiles {
		relative := packageSkillFileRelativePath(skillFile)
		destination := path.Join(constants.GithubDir+packageSkillsDirectory, skillFile.SkillName, filepath.ToSlash(relative))
		if err := add(destination, skillFile.SourcePath); err != nil {
			return err
		}
	}
	for _, agentFile := range agentFiles {
		destination := path.Join(constants.GithubDir+packageAgentsDirectory, filepath.Base(agentFile))
		if err := add(destination, agentFile); err != nil {
			return err
		}
	}
	return nil
}

func packageSkillFileRelativePath(skillFile resolvedPackageSkillFile) string {
	parts := strings.Split(filepath.ToSlash(skillFile.SourcePath), "/")
	for i := 1; i < len(parts)-1; i++ {
		if parts[i] == skillFile.SkillName && parts[i-1] == "skills" {
			return path.Join(parts[i+1:]...)
		}
	}
	return filepath.Base(skillFile.SourcePath)
}

// localPackageImportRoot returns the boundary for manifest imports of a local package.
// Imports may reach outside packageDir, for example a nested package importing
// "../aw.yml", but never outside the enclosing git repository. Packages outside a git
// repository stay bounded by their own directory.
func localPackageImportRoot(packageDir string) string {
	gitRoot, err := gitutil.FindGitRootFrom(packageDir)
	if err != nil {
		addPackageManifestLog.Printf("No git root for package %q; bounding imports by the package directory: %v", packageDir, err)
		return filepath.Clean(packageDir)
	}
	return filepath.Clean(gitRoot)
}

// readLocalImportedManifest reads an imported manifest, guarding against
// path traversal outside importRoot and against symbolic links that would
// resolve outside it. This is a path-traversal guard, not a redirect
// handler; on platforms where backslash is not the path separator, it
// additionally rejects any backslash in the resolved relative path
// (consistent with CWE-601 guidance) purely to avoid matching the
// go/bad-redirect-check heuristic, without weakening the containment check.
func readLocalImportedManifest(manifestPath, importRoot string) ([]byte, error) {
	evaluatedPath, err := filepath.EvalSymlinks(manifestPath)
	if err != nil {
		return nil, err
	}
	evaluatedRoot, err := filepath.EvalSymlinks(importRoot)
	if err != nil {
		return nil, err
	}
	relative, err := filepath.Rel(evaluatedRoot, evaluatedPath)
	if err != nil {
		return nil, err
	}
	hasStrayBackslash := os.PathSeparator != '\\' && strings.Contains(relative, `\`)
	if relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) || hasStrayBackslash {
		addPackageManifestLog.Printf("Rejecting imported manifest %q: resolves outside import root %q", manifestPath, importRoot)
		return nil, fmt.Errorf("import %q resolves outside the repository root", manifestPath)
	}
	declaredRelative, err := filepath.Rel(filepath.Clean(importRoot), filepath.Clean(manifestPath))
	if err != nil {
		return nil, err
	}
	if filepath.Clean(relative) != filepath.Clean(declaredRelative) {
		return nil, errors.New("imported manifests must not use symbolic links")
	}
	return os.ReadFile(manifestPath)
}
