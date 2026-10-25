package cli

import (
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/fileutil"
	"github.com/github/gh-aw/pkg/gitutil"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
)

var importPathLog = logger.New("cli:importpath")

// importPathResolverOpts configures the canonical import-path resolver.
//
// The three call sites in pkg/cli differ on three axes, each controlled by a
// dedicated option field:
//
//   - Section-strip: whether "#anchor" suffixes are removed before resolution.
//   - Workflowspec:  what to do when the path is in "owner/repo/path[@ref]" format.
//   - Absolute base: how "/" prefix paths are turned into a usable result.
//
// Preset option groups used by the three call sites:
//
//	imports.go        – importPathImportsOpts
//	dependency_graph  – importPathDepGraphOpts (ParserGitRoot must be set per call)
//	run_push          – importPathRunPushOpts
type importPathResolverOpts struct {
	// StripSectionRef removes the "#anchor" fragment from the path before resolution.
	StripSectionRef bool
	// WorkflowSpecPassthrough returns workflowspec-format paths ("owner/repo/path[@ref]") unchanged.
	// Mutually exclusive with WorkflowSpecSkip.
	WorkflowSpecPassthrough bool
	// WorkflowSpecSkip returns "" for workflowspec-format paths.
	// Mutually exclusive with WorkflowSpecPassthrough.
	WorkflowSpecSkip bool
	// RepoRelativeAbsolute handles "/" prefix paths by stripping the leading "/" and returning
	// the remainder as a plain forward-slash string (no disk lookup, no git-root required).
	// When false, "/" prefix paths are resolved against GitRoot (or via gitutil.FindGitRoot).
	RepoRelativeAbsolute bool
	// GitRoot is the git root directory used for resolving "/" prefix paths when
	// RepoRelativeAbsolute is false and UseParserFallback is false.
	// When empty, gitutil.FindGitRoot() is called at resolution time.
	GitRoot string
	// UseParserFallback enables stat+parser-fallback resolution.
	// For relative paths: os.Stat is tried first; if not found, parser.ResolveIncludePath is used.
	// For "/" prefix paths: parser.ResolveIncludePath is used directly (no stat check).
	// Requires ParserGitRoot to be set.
	UseParserFallback bool
	// ParserGitRoot is the git root passed to parser.NewImportCache.
	// Required when UseParserFallback is true.
	ParserGitRoot string
	// NormalizeSlash applies filepath.Clean and filepath.ToSlash to the resolved path.
	NormalizeSlash bool
}

// importPathImportsOpts is the option preset for imports.go call sites.
// Workflowspec paths are returned unchanged, "/" prefix paths become repo-relative
// strings (leading "/" stripped), and relative paths are cleaned and normalised to
// forward slashes.
var importPathImportsOpts = importPathResolverOpts{
	WorkflowSpecPassthrough: true,
	RepoRelativeAbsolute:    true,
	NormalizeSlash:          true,
}

// importPathRunPushOpts is the option preset for run_push.go call sites.
// Section refs are stripped, workflowspec paths return "", "/" prefix paths are
// resolved against the git root, and relative paths are joined with baseDir.
var importPathRunPushOpts = importPathResolverOpts{
	StripSectionRef:  true,
	WorkflowSpecSkip: true,
}

// resolveImportPath is the canonical import-path resolver for pkg/cli.
//
// importPath is the raw path from the workflow's imports/includes field.
// baseDir is the directory against which relative paths are resolved; callers
// that have a workflow file path should pass filepath.Dir(workflowPath).
func resolveImportPath(importPath, baseDir string, opts importPathResolverOpts) string {
	importPathLog.Printf("resolveImportPath: importPath=%q baseDir=%q", importPath, baseDir)

	// 1. Strip section references (e.g. "shared.md#Section" → "shared.md").
	if opts.StripSectionRef {
		if i := strings.Index(importPath, "#"); i >= 0 {
			importPath = importPath[:i]
		}
	}

	// 2. Workflowspec-format handling ("owner/repo/path[@sha]").
	if isWorkflowSpecFormat(importPath) {
		if opts.WorkflowSpecPassthrough {
			importPathLog.Printf("resolveImportPath: workflowspec passthrough for %q", importPath)
			return importPath
		}
		if opts.WorkflowSpecSkip {
			importPathLog.Printf("resolveImportPath: skipping workflowspec %q", importPath)
			return ""
		}
	}

	// 3. Absolute paths (starting with "/").
	if withoutLeadingSlash, hasLeadingSlash := strings.CutPrefix(importPath, "/"); hasLeadingSlash {
		if opts.RepoRelativeAbsolute {
			// Strip "/" and return as a repo-relative string (no disk lookup).
			return withoutLeadingSlash
		}
		if !opts.UseParserFallback {
			// Resolve against the git root on disk.
			gitRoot := opts.GitRoot
			if gitRoot == "" {
				var err error
				gitRoot, err = gitutil.FindGitRoot()
				if err != nil {
					return ""
				}
			}
			return filepath.Join(gitRoot, withoutLeadingSlash)
		}
		// UseParserFallback=true: fall through to parser resolution below.
	}

	// 4. Relative paths (and "/" prefix paths in parser-fallback mode).
	if opts.UseParserFallback {
		return resolveImportWithParserFallback(importPath, baseDir, opts)
	}

	// Direct resolution with optional path normalisation.
	result := filepath.Join(baseDir, importPath)
	if opts.NormalizeSlash {
		result = filepath.ToSlash(filepath.Clean(result))
	}
	return result
}

func resolveImportWithParserFallback(importPath, baseDir string, opts importPathResolverOpts) string {
	// Only attempt stat for non-absolute paths (absolute paths go directly to parser,
	// matching the original dependency_graph.go behaviour).
	if !strings.HasPrefix(importPath, "/") {
		absPath := filepath.Join(baseDir, importPath)
		if fileutil.FileExists(absPath) {
			importPathLog.Printf("resolveImportPath: resolved %q via stat to %q", importPath, absPath)
			return absPath
		}
	}
	importCache := parser.NewImportCache(opts.ParserGitRoot)
	fullPath, err := parser.ResolveIncludePath(importPath, baseDir, importCache)
	if err != nil {
		importPathLog.Printf("resolveImportPath: parser fallback failed for %q: %v", importPath, err)
		return ""
	}
	importPathLog.Printf("resolveImportPath: parser fallback resolved %q to %q", importPath, fullPath)
	return fullPath
}
