//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/require"
	nodeyaml "go.yaml.in/yaml/v3"
)

func TestBundledConnectorImportMetadata(t *testing.T) {
	files, err := filepath.Glob("../../.github/workflows/connectors/*.md")
	require.NoError(t, err)
	require.NotEmpty(t, files)
	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			source, err := os.ReadFile(file)
			require.NoError(t, err)
			parts := strings.SplitN(string(source), "---", 3)
			require.Len(t, parts, 3)
			var document struct {
				Inputs map[string]struct {
					Type        string
					Description string
					Items       map[string]any
				} `yaml:"import-schema"`
			}
			require.NoError(t, yaml.Unmarshal([]byte(parts[1]), &document))
			require.NotEmpty(t, document.Inputs)
			for name, input := range document.Inputs {
				require.NotEmpty(t, strings.TrimSpace(input.Description), "binding %s needs concise setup metadata", name)
				if input.Type == "array" {
					require.NotEmpty(t, input.Items["type"], "array binding %s needs explicit item types", name)
				}
			}
		})
	}
}

func connectorAuthoringField(node *nodeyaml.Node, field string) *nodeyaml.Node {
	if node == nil {
		return nil
	}
	if node.Kind == nodeyaml.AliasNode {
		node = node.Alias
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == field {
			return node.Content[i+1]
		}
	}
	return nil
}

func checkConnectorMapOrder(t *testing.T, node *nodeyaml.Node, fields []string) {
	t.Helper()
	if node == nil {
		return
	}
	if node.Kind == nodeyaml.AliasNode {
		node = node.Alias
	}
	require.Equal(t, nodeyaml.MappingNode, node.Kind)
	previous := -1
	for i := 0; i+1 < len(node.Content); i += 2 {
		name := node.Content[i].Value
		if name == "<<" {
			require.Zero(t, i, "merge keys must precede explicit overrides")
			continue
		}
		rank := slices.Index(fields, name)
		require.NotEqual(t, -1, rank, "add field %s to the documented authoring order", name)
		require.GreaterOrEqual(t, rank, previous, "field %s is out of authoring order", name)
		previous = rank
	}
}

func checkConnectorOperationOrder(t *testing.T, operation *nodeyaml.Node) {
	t.Helper()
	checkConnectorMapOrder(t, operation, []string{
		"tool", "credential", "inputs", "resources", "trustedInputs", "lookups", "preflight", "checks",
		"createdResources", "creates", "request", "maxResponseBytes", "retry", "responseChecks", "responseRequired",
		"responseTypes", "responseInputs", "responseResourceInputs", "responseResources", "responseOptionalEmpty",
		"responseContains", "responseLimits", "responseAnyInputs", "responseWhen", "project",
	})
	inputs := connectorAuthoringField(operation, "inputs")
	if inputs == nil {
		return
	}
	if inputs.Kind == nodeyaml.AliasNode {
		inputs = inputs.Alias
	}
	for i := 1; i < len(inputs.Content); i += 2 {
		if inputs.Content[i-1].Value == "<<" {
			continue
		}
		checkConnectorMapOrder(t, inputs.Content[i], []string{
			"type", "required", "minLength", "maxLength", "minimum", "maximum", "enum", "pattern", "sanitize",
		})
	}
}

func TestBundledConnectorAuthoringOrder(t *testing.T) {
	files, err := filepath.Glob("../../.github/workflows/connectors/*.md")
	require.NoError(t, err)
	require.Len(t, files, 14)
	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			source, err := os.ReadFile(file)
			require.NoError(t, err)
			parts := strings.SplitN(string(source), "---", 3)
			require.Len(t, parts, 3)
			var document nodeyaml.Node
			require.NoError(t, nodeyaml.Unmarshal([]byte(parts[1]), &document))
			require.Len(t, document.Content, 1)
			connectors := connectorAuthoringField(document.Content[0], "connectors")
			require.NotNil(t, connectors)
			require.Len(t, connectors.Content, 2)
			declaration := connectors.Content[1]
			checkConnectorMapOrder(t, declaration, []string{
				"experimental", "docs", "endpoints", "credentials", "resources", "triggers", "tools", "mcp-servers", "safe-outputs",
			})
			for _, kind := range []string{"tools", "safe-outputs"} {
				operations := connectorAuthoringField(declaration, kind)
				if operations != nil {
					for i := 1; i < len(operations.Content); i += 2 {
						checkConnectorOperationOrder(t, operations.Content[i])
					}
				}
			}
		})
	}
}

func TestJiraResourceBindingsRequireStringItems(t *testing.T) {
	root, err := filepath.Abs("../..")
	require.NoError(t, err)
	for name, value := range map[string]string{
		"cloud-ids": "11111111-1111-4111-8111-111111111111",
		"issue-ids": "123", "issues": "APP-1", "projects": "APP", "queries": "issue = APP-1",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := LoadDevelopmentConnector(root, "jira", "", map[string]any{name: []any{value}}, nil)
			require.NoError(t, err)
			for _, item := range []any{123, map[string]any{"id": value}} {
				_, err := LoadDevelopmentConnector(root, "jira", "", map[string]any{name: []any{value, item}}, nil)
				require.ErrorContains(t, err, name)
				require.ErrorContains(t, err, "string")
			}
		})
	}
}
