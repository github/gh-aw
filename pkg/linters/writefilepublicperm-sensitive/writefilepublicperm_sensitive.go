// Package writefilepublicpermsensitive implements a Go analysis linter that
// flags os.WriteFile, os.OpenFile, and os.MkdirAll calls that use the public
// permission constant for paths or content that look sensitive.
package writefilepublicpermsensitive

import (
	"go/ast"
	"go/token"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/tools/go/analysis"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/astutil"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
)

// Analyzer is the writefilepublicpermsensitive analysis pass.
var Analyzer = analyzerutil.NewAtPath(
	"writefilepublicpermsensitive",
	"reports os.WriteFile, os.OpenFile, and os.MkdirAll calls that use FilePermPublic for sensitive-looking paths or content and recommends FilePermSensitive",
	"writefilepublicperm-sensitive",
	run,
)

var sensitiveWords = map[string]bool{
	"token": true, "tokens": true,
	"secret": true, "secrets": true,
	"key": true, "keys": true,
	"credential": true, "credentials": true,
	"password": true, "passwords": true,
	"auth": true,
	"env":  true,
	"lock": true,
}

func run(pass *analysis.Pass) (any, error) {
	noLintIndex, generatedFiles, err := analyzerutil.Indexes(pass)
	if err != nil {
		return nil, err
	}
	nodeFilter := []ast.Node{(*ast.CallExpr)(nil)}
	return analyzerutil.Preorder(pass, nodeFilter, func(n ast.Node) {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return
		}
		checkCall(pass, call, generatedFiles, noLintIndex)
	})
}

func checkCall(pass *analysis.Pass, call *ast.CallExpr, generatedFiles filecheck.GeneratedIndex, noLintIndex nolint.DirectiveIndex) {
	if _, ok := astutil.UnwrapParenExpr(call.Fun).(*ast.SelectorExpr); !ok {
		return
	}
	pkgPath, name, ok := astutil.PackageCall(pass, call)
	if !ok || pkgPath != "os" {
		return
	}

	var permIdx int
	var inspected []ast.Expr
	switch name {
	case "WriteFile":
		if len(call.Args) != 3 {
			return
		}
		permIdx, inspected = 2, call.Args[:2]
	case "OpenFile":
		if len(call.Args) != 3 {
			return
		}
		permIdx, inspected = 2, call.Args[:1]
	case "MkdirAll":
		if len(call.Args) != 2 {
			return
		}
		permIdx, inspected = 1, call.Args[:1]
	default:
		return
	}

	if !isFilePermPublic(call.Args[permIdx]) {
		return
	}
	pos := pass.Fset.PositionFor(call.Pos(), false)
	if filecheck.ShouldSkipFilename(pos.Filename, generatedFiles) {
		return
	}
	var match string
	for _, e := range inspected {
		if match = sensitiveWord(e); match != "" {
			break
		}
	}
	if match == "" {
		return
	}
	if nolint.HasDirectiveForLinter(pos, noLintIndex, "writefilepublicpermsensitive") {
		return
	}
	pass.ReportRangef(call.Args[permIdx],
		"os.%s uses FilePermPublic for sensitive-looking data (%q); use FilePermSensitive (0600) instead",
		name, match)
}

// isFilePermPublic reports whether expr is a selector ending in FilePermPublic.
func isFilePermPublic(expr ast.Expr) bool {
	sel, ok := astutil.UnwrapParenExpr(expr).(*ast.SelectorExpr)
	return ok && sel.Sel.Name == "FilePermPublic"
}

// sensitiveWord returns the first sensitive word found in identifiers,
// selector names, or string literals within expr.
func sensitiveWord(expr ast.Expr) string {
	var found string
	ast.Inspect(expr, func(n ast.Node) bool {
		if found != "" {
			return false
		}
		var text string
		switch n := n.(type) {
		case *ast.Ident:
			text = n.Name
		case *ast.BasicLit:
			if n.Kind != token.STRING {
				return true
			}
			s, err := strconv.Unquote(n.Value)
			if err != nil {
				return true
			}
			text = s
		default:
			return true
		}
		found = firstSensitiveWord(text)
		return true
	})
	return found
}

// firstSensitiveWord splits text on non-alphanumerics and camelCase boundaries
// and returns the first word that is a sensitive term.
func firstSensitiveWord(text string) string {
	runes := []rune(text)
	var word []rune
	flush := func() string {
		w := strings.ToLower(string(word))
		word = word[:0]
		if sensitiveWords[w] {
			return w
		}
		return ""
	}
	for i, r := range runes {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			if w := flush(); w != "" {
				return w
			}
			continue
		}
		if unicode.IsUpper(r) && len(word) > 0 {
			prev := runes[i-1]
			nextLower := i+1 < len(runes) && unicode.IsLower(runes[i+1])
			if unicode.IsLower(prev) || unicode.IsDigit(prev) || (unicode.IsUpper(prev) && nextLower) {
				if w := flush(); w != "" {
					return w
				}
			}
		}
		word = append(word, r)
	}
	return flush()
}
