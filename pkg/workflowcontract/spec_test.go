//go:build !integration

package workflowcontract_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSpec_DesignDecision_DocumentedContracts derives from README.md's
// Contracts table and test-only Overview. It checks exported declarations only;
// existing test bodies are not consulted or replicated.
func TestSpec_DesignDecision_DocumentedContracts(t *testing.T) {
	type testSignature struct {
		function *ast.FuncType
		testing  string
	}
	signatures := make(map[string]testSignature)
	entries, err := os.ReadDir(".")
	require.NoError(t, err, "the documented test-only package must be readable")
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), entry.Name(), nil, parser.SkipObjectResolution)
		require.NoError(t, err, "Go test declarations must parse in %s", entry.Name())
		testingName := ""
		for _, imported := range file.Imports {
			path, err := strconv.Unquote(imported.Path.Value)
			require.NoError(t, err, "import paths must be valid Go strings in %s", entry.Name())
			if path == "testing" {
				testingName = "testing"
				if imported.Name != nil {
					testingName = imported.Name.Name
				}
			}
		}
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if ok && function.Recv == nil && ast.IsExported(function.Name.Name) {
				signatures[function.Name.Name] = testSignature{function.Type, testingName}
			}
		}
	}

	for _, name := range []string{
		"TestCommitChangesAnalyzerExperimentContract",
		"TestDailyCLIToolsTesterCoverageContract",
		"TestDailyPerformanceSummaryUsesStableWindowMetrics",
		"TestGitHubQueryScriptsUseFileBackedPagination",
		"TestRoutedSmokeDetectionChecksMatchCompiledRouting",
	} {
		t.Run(name, func(t *testing.T) {
			signature, found := signatures[name]
			require.True(t, found, "README.md documents %s as a runnable Go contract test", name)
			assert.Zero(t, signature.function.Results.NumFields(), "a documented Go test must not return values")
			assert.Zero(t, signature.function.TypeParams.NumFields(), "a documented Go test must not have type parameters")
			require.Equal(t, 1, signature.function.Params.NumFields(), "a documented Go test must take one parameter")
			parameter, ok := signature.function.Params.List[0].Type.(*ast.StarExpr)
			require.True(t, ok, "a documented Go test must accept a pointer to testing.T")
			if signature.testing == "." {
				typeName, ok := parameter.X.(*ast.Ident)
				require.True(t, ok, "a dot-imported Go test parameter must identify testing.T")
				assert.Equal(t, "T", typeName.Name, "a documented Go test must accept testing.T")
				return
			}
			typeName, ok := parameter.X.(*ast.SelectorExpr)
			require.True(t, ok, "a documented Go test parameter must identify testing.T")
			qualifier, ok := typeName.X.(*ast.Ident)
			require.True(t, ok, "a documented Go test parameter must use its testing import")
			require.NotEmpty(t, signature.testing, "a documented Go test must import testing")
			assert.Equal(t, signature.testing, qualifier.Name, "the parameter must use the testing package")
			assert.Equal(t, "T", typeName.Sel.Name, "a documented Go test must accept testing.T")
		})
	}
}

// SPEC_AMBIGUITY: README.md names workflow contracts but does not specify the
// workflow paths, required wording, configuration values, or model sets needed
// to derive independent behavioral assertions for those contracts.
