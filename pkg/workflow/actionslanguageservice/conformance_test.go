package actionslanguageservice

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

type conformanceCase struct {
	Name     string   `json:"name"`
	Kind     string   `json:"kind"`
	Source   string   `json:"source"`
	Contexts []string `json:"contexts"`
	Accepted bool     `json:"accepted"`
	Tree     any      `json:"tree"`
}

func conformanceCases(t *testing.T) []conformanceCase {
	t.Helper()
	content, err := os.ReadFile("testdata/conformance.json")
	require.NoError(t, err)
	var fixture struct {
		Revision string            `json:"revision"`
		Cases    []conformanceCase `json:"cases"`
	}
	require.NoError(t, json.Unmarshal(content, &fixture))
	require.Equal(t, "4043eda158e16579cc5fb1b0b07a4bce2a76f0b5", fixture.Revision)
	require.NotEmpty(t, fixture.Cases)
	return fixture.Cases
}

func TestConformanceSnapshot(t *testing.T) {
	for _, test := range conformanceCases(t) {
		t.Run(test.Name, func(t *testing.T) {
			var tree *Expression
			var err error
			switch test.Kind {
			case "expression":
				tree, err = ParseExpression(test.Source, test.Contexts)
			case "workflow":
				_, err = ParseWorkflow(test.Name, test.Source)
			default:
				t.Fatalf("unknown conformance kind %q", test.Kind)
			}
			require.Equal(t, test.Accepted, err == nil, "native parser error: %v", err)
			if test.Accepted && test.Kind == "expression" {
				require.Equal(t, test.Tree, normalizedTree(tree))
			}
		})
	}
}

func TestOfficialParsersConformance(t *testing.T) {
	if os.Getenv("GH_AW_ACTIONS_PARSER_CONFORMANCE") != "1" {
		t.Skip("run npm ci && npm test in conformance/ for the live official parser comparison")
	}
	tests := append(conformanceCases(t), boundaryCases()...)
	request, err := json.Marshal(tests)
	require.NoError(t, err)
	oracle, err := filepath.Abs("conformance/oracle.bundle.mjs")
	require.NoError(t, err)
	cmd := exec.Command("node", oracle)
	cmd.Stdin = bytes.NewReader(request)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	require.NoError(t, err, "official parser oracle failed: %s", stderr.String())
	var results []struct {
		Accepted bool `json:"accepted"`
		Tree     any  `json:"tree"`
	}
	require.NoError(t, json.Unmarshal(output, &results))
	require.Len(t, results, len(tests))
	for i, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			if test.Kind == "expression" {
				tree, err := ParseExpression(test.Source, test.Contexts)
				require.Equal(t, results[i].Accepted, err == nil, "native error: %v", err)
				if err == nil {
					require.Equal(t, results[i].Tree, normalizedTree(tree))
				}
			} else {
				_, err := ParseWorkflow(test.Name, test.Source)
				require.Equal(t, results[i].Accepted, err == nil, "native error: %v", err)
			}
		})
	}
}

func normalizedTree(expr *Expression) any {
	if expr == nil {
		return nil
	}
	result := []any{expr.Kind}
	switch expr.Kind {
	case "literal":
		switch expr.Token.Kind {
		case "null":
			return append(result, "null")
		case "string", "identifier":
			return append(result, "string", expr.Token.Value.(string))
		case "number":
			number := expr.Token.Value.(float64)
			var value any = number
			if math.IsInf(number, 1) {
				value = "Infinity"
			} else if math.IsInf(number, -1) {
				value = "-Infinity"
			} else if math.IsNaN(number) {
				value = "NaN"
			}
			return append(result, "number", value)
		case "true", "false":
			return append(result, "boolean", expr.Token.Kind == "true")
		}
	case "context", "call", "unary", "binary", "logical":
		result = append(result, expr.Token.Text)
	case "group", "index", "star":
	default:
		panic(fmt.Sprintf("unknown expression kind %q", expr.Kind))
	}
	for _, child := range expr.Children {
		result = append(result, normalizedTree(child))
	}
	return result
}
