package workflow

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/github/gh-aw/pkg/connectors"
	"github.com/github/gh-aw/pkg/sliceutil"
)

func narrowConnectorMCPSelection(config map[string]any, selected map[string]any) ([]string, error) {
	if len(selected) == 0 || config["spec"] == nil {
		return nil, errors.New("targeted MCP selection requires an imported declaration and nonempty registration mapping; example: tools: {jira: {personal: [getJiraIssue]}}")
	}
	data, err := json.Marshal(config["spec"])
	if err != nil {
		return nil, err
	}
	var spec connectors.ConnectorSpec
	if err := connectors.Decode(data, &spec); err != nil {
		return nil, err
	}
	names := sliceutil.SortedKeys(selected)
	for _, name := range names {
		server, exists := spec.HTTPMCP[name]
		if !exists {
			return nil, fmt.Errorf("targeted selection %q requires a declared HTTP MCP registration; select fixed REST/GraphQL reads with a list", name)
		}
		tools, err := selectedConnectorMCPTools(name, selected[name], server.Tools)
		if err != nil {
			return nil, err
		}
		server.Tools = tools
		spec.HTTPMCP[name] = server
	}
	if err := spec.Validate(); err != nil {
		return nil, err
	}
	data, err = json.Marshal(spec)
	if err != nil {
		return nil, err
	}
	var normalized map[string]any
	if err := json.Unmarshal(data, &normalized); err != nil {
		return nil, err
	}
	config["spec"] = normalized
	return names, nil
}

func selectedConnectorMCPTools(registration string, raw any, declared map[string]connectors.MCPTool) (map[string]connectors.MCPTool, error) {
	var names []string
	switch values := raw.(type) {
	case []string:
		names = values
	case []any:
		for _, raw := range values {
			name, ok := raw.(string)
			if !ok {
				return nil, errors.New("targeted MCP tools require an array of exact native tool names; example: [getJiraIssue]")
			}
			names = append(names, name)
		}
	default:
		return nil, errors.New("targeted MCP tools require a nonempty array; example: [getJiraIssue]")
	}
	if len(names) == 0 || len(names) > len(declared) {
		return nil, errors.New("targeted MCP selection requires a nonempty subset of the declaration's exact tool allowlist")
	}
	selected := map[string]connectors.MCPTool{}
	for _, name := range names {
		tool, exists := declared[name]
		if !exists {
			return nil, fmt.Errorf("MCP registration %q does not declare exact tool %q; wildcards and unknown tools are unsupported", registration, name)
		}
		if _, duplicate := selected[name]; duplicate {
			return nil, fmt.Errorf("MCP tool %q is selected more than once", name)
		}
		selected[name] = tool
	}
	return selected, nil
}
