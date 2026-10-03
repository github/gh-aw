// Package nolint provides shared helpers for nolint-directive detection
// used by linters within pkg/linters.
package nolint

import (
	"fmt"
	"go/token"
	"go/types"
	"reflect"
	"strings"

	"golang.org/x/tools/go/analysis"

	"github.com/github/gh-aw/pkg/logger"
)

var pkgLog = logger.New("linters:nolint")

// DirectiveIndex records nolint directives by filename, line, and linter name.
type DirectiveIndex map[string]map[int]map[string]struct{}

// Analyzer builds a shared nolint directive index once per package so analyzers
// can reuse it via pass.ResultOf.
var Analyzer = &analysis.Analyzer{
	Name:             "nolintindex",
	Doc:              "indexes nolint directives for gh-aw custom linters",
	ResultType:       reflect.TypeFor[DirectiveIndex](),
	RunDespiteErrors: true,
	Run: func(pass *analysis.Pass) (any, error) {
		return BuildDirectiveIndex(pass), nil
	},
}

// Index returns the shared nolint directive index for pass.
func Index(pass *analysis.Pass) (DirectiveIndex, error) {
	idx, ok := pass.ResultOf[Analyzer].(DirectiveIndex)
	if !ok {
		return nil, fmt.Errorf("nolint analyzer result has unexpected type %T", pass.ResultOf[Analyzer])
	}
	return idx, nil
}

// BuildDirectiveIndex scans all comments in the analysis pass and returns a map
// from filename → line → set of linter names that carry a nolint directive.
func BuildDirectiveIndex(pass *analysis.Pass) DirectiveIndex {
	pkgPath := "<unknown>"
	if pass.Pkg != nil {
		pkgPath = pass.Pkg.Path()
	}
	pkgLog.Printf("building nolint directive index for %s (%d files)", pkgPath, len(pass.Files))
	noLintLinesByFile := make(DirectiveIndex, len(pass.Files))
	for _, file := range pass.Files {
		filename := pass.Fset.PositionFor(file.Pos(), false).Filename
		if filename == "" {
			continue
		}
		for _, group := range file.Comments {
			for _, comment := range group.List {
				text := strings.TrimSpace(strings.TrimPrefix(comment.Text, "//"))
				if !strings.HasPrefix(text, "nolint:") {
					continue
				}
				payload := strings.TrimPrefix(text, "nolint:")
				if i := strings.Index(payload, "//"); i >= 0 {
					payload = payload[:i]
				}
				if i := strings.IndexAny(payload, " \t"); i >= 0 {
					payload = payload[:i]
				}
				line := pass.Fset.PositionFor(comment.Slash, false).Line
				if noLintLinesByFile[filename] == nil {
					noLintLinesByFile[filename] = make(map[int]map[string]struct{})
				}
				if noLintLinesByFile[filename][line] == nil {
					noLintLinesByFile[filename][line] = make(map[string]struct{})
				}
				for token := range strings.SplitSeq(payload, ",") {
					name := strings.TrimSpace(token)
					if name == "" {
						continue
					}
					noLintLinesByFile[filename][line][name] = struct{}{}
				}
			}
		}
	}
	pkgLog.Printf("indexed nolint directives in %d files", len(noLintLinesByFile))
	return noLintLinesByFile
}

// HasDirectiveForLinter reports whether the given source position is covered by
// a suppression directive for linterName (or "nolint:all"). Both same-line and
// previous-line directives are recognised, matching golangci-lint behaviour.
func HasDirectiveForLinter(position token.Position, idx DirectiveIndex, linterName string) bool {
	if position.Filename == "" {
		return false
	}
	return hasDirectiveForLine(position.Line, idx[position.Filename], linterName) ||
		hasDirectiveForLine(position.Line-1, idx[position.Filename], linterName)
}

func hasDirectiveForLine(line int, lines map[int]map[string]struct{}, linterName string) bool {
	if lines == nil {
		return false
	}
	return hasDirectiveName(lines[line], linterName)
}

func hasDirectiveName(names map[string]struct{}, linterName string) bool {
	if names == nil {
		return false
	}
	if _, ok := names[linterName]; ok {
		return true
	}
	_, ok := names["all"]
	return ok
}

// ImplementsError reports whether t implements the built-in error interface.
func ImplementsError(t types.Type) bool {
	obj := types.Universe.Lookup("error")
	if obj == nil {
		return false
	}
	errIface, ok := obj.Type().Underlying().(*types.Interface)
	if !ok {
		return false
	}

	if types.Implements(t, errIface) {
		return true
	}
	if p, ok := t.(*types.Pointer); ok {
		return types.Implements(p, errIface)
	}
	return types.Implements(types.NewPointer(t), errIface)
}
