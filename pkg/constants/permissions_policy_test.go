//go:build !integration

package constants

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPermissionConstantsValues(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		got  fs.FileMode
		want fs.FileMode
	}{
		{name: "FilePermSensitive", got: FilePermSensitive, want: 0o600},
		{name: "FilePermPublic", got: FilePermPublic, want: 0o644},
		{name: "FilePermExecutable", got: FilePermExecutable, want: 0o755},
		{name: "DirPermSensitive", got: DirPermSensitive, want: 0o750},
		{name: "DirPermOwnerOnly", got: DirPermOwnerOnly, want: 0o700},
		{name: "DirPermPublic", got: DirPermPublic, want: 0o755},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tt.got)
		})
	}
}

func TestNoRawOctalPermissionLiteralsInOSCalls(t *testing.T) {
	t.Parallel()
	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok, "runtime.Caller failed")

	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", ".."))
	octalPattern := regexp.MustCompile(`^0o?[0-7]+$`)

	roots := []string{
		filepath.Join(repoRoot, "pkg"),
		filepath.Join(repoRoot, "cmd"),
	}

	fset := token.NewFileSet()
	for _, root := range roots {
		err := filepath.Walk(root, func(path string, info fs.FileInfo, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if info.IsDir() {
				return nil
			}
			if filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") || filepath.Base(path) == "permissions_policy_test.go" {
				return nil
			}

			file, parseErr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
			if parseErr != nil {
				t.Fatalf("failed to parse %s: %v", path, parseErr)
			}

			ast.Inspect(file, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}

				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				x, ok := sel.X.(*ast.Ident)
				if !ok || x.Name != "os" {
					return true
				}

				var argIndex int
				switch sel.Sel.Name {
				case "MkdirAll", "Mkdir", "Chmod":
					argIndex = 1
				case "WriteFile", "OpenFile":
					argIndex = 2
				default:
					return true
				}

				if argIndex >= len(call.Args) {
					return true
				}

				lit, ok := call.Args[argIndex].(*ast.BasicLit)
				if !ok || lit.Kind != token.INT {
					return true
				}

				if octalPattern.MatchString(lit.Value) {
					t.Errorf("raw octal permission literal %s in %s", lit.Value, path)
				}

				return true
			})

			return nil
		})
		if err != nil {
			t.Fatalf("failed to walk %s tree: %v", root, err)
		}
	}
}
