package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"

	"github.com/github/gh-aw/pkg/connectors"
	"github.com/github/gh-aw/pkg/sliceutil"
)

var connectorNamePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)

type ConnectorRegistration struct {
	Definition  string                          `json:"definition,omitempty"`
	Spec        *connectors.ConnectorSpec       `json:"spec,omitempty"`
	Connection  string                          `json:"connection"`
	Credentials map[string]connectors.SecretRef `json:"credentials,omitempty"`
	Resources   map[string][]string             `json:"resources,omitempty"`
	GitHubApps  map[string]*ConnectorGitHubApp  `json:"github-apps,omitempty"`
	OIDCTokens  map[string]*ConnectorGitHubOIDC `json:"oidc-tokens,omitempty"`
	On          []string                        `json:"on,omitempty"`
	Tools       []string                        `json:"tools,omitempty"`
	Context     map[string]map[string]any       `json:"context,omitempty"`
	SafeOutputs []string                        `json:"safe-outputs,omitempty"`
}

// WorkflowConnector is the compiler-resolved service, not an all-or-nothing
// integration. Each runtime receives only its selected phase snapshot.
type WorkflowConnector struct {
	Plan        *connectors.Plan
	Agent       *connectors.Plan
	Context     *connectors.Plan
	SafeOutputs *connectors.Plan
	ContextArgs map[string]map[string]any
	Alias       string
	GitHubApps  map[string]*ConnectorGitHubApp
	OIDCTokens  map[string]*ConnectorGitHubOIDC
}

func validateConnectorNames(names []string) error {
	seen := map[string]struct{}{}
	for _, name := range names {
		_, duplicate := seen[name]
		if !connectorNamePattern.MatchString(name) || duplicate {
			return fmt.Errorf("connector selection requires unique capability names, got %q", name)
		}
		seen[name] = struct{}{}
	}
	return nil
}

func resolveConnectorSelection(registration ConnectorRegistration, service *connectors.Service) (connectors.Selection, error) {
	selection := connectors.Selection{
		Triggers: registration.On, SafeOutputs: registration.SafeOutputs,
		Context: sliceutil.SortedKeys(registration.Context),
	}
	for _, names := range [][]string{registration.On, registration.Tools, registration.SafeOutputs, selection.Context} {
		if err := validateConnectorNames(names); err != nil {
			return selection, err
		}
	}
	for _, name := range registration.Tools {
		capability, err := service.ToolCapability(name)
		if err != nil {
			return selection, err
		}
		if capability == "mcp" {
			selection.MCP = append(selection.MCP, name)
		} else {
			selection.HTTPMCP = append(selection.HTTPMCP, name)
		}
	}
	return selection, nil
}

func resolveWorkflowConnector(service *connectors.Service, selection connectors.Selection, args map[string]map[string]any) (*WorkflowConnector, error) {
	plan, err := service.Select(selection)
	if err != nil {
		return nil, err
	}
	for name, values := range args {
		if err := plan.ValidateContext(name, values); err != nil {
			return nil, fmt.Errorf("context %q: %w", name, err)
		}
	}
	if len(selection.Triggers) > 0 {
		return nil, fmt.Errorf("on: %w; a declared verification profile is not receiver enrollment", plan.ActivateTriggers())
	}
	config := &WorkflowConnector{Plan: plan, ContextArgs: args}
	phases := []struct {
		name   string
		count  int
		target **connectors.Plan
	}{
		{"agent", len(selection.MCP) + len(selection.HTTPMCP), &config.Agent},
		{"context", len(selection.Context), &config.Context},
		{"safe-output", len(selection.SafeOutputs), &config.SafeOutputs},
	}
	for _, phase := range phases {
		if phase.count == 0 {
			continue
		}
		restricted, err := plan.ForPhase(phase.name)
		if err != nil {
			return nil, err
		}
		if _, err := restricted.Encode(); err != nil {
			return nil, err
		}
		*phase.target = restricted
	}
	return config, nil
}

func (c *Compiler) resolveWorkflowConnectors(frontmatter map[string]any) (map[string]*WorkflowConnector, error) {
	registrations, hasRegistrations := frontmatter["connectors"].(map[string]any)
	if !hasRegistrations {
		return nil, nil
	}
	if c.gitRoot == "" {
		return nil, errors.New("connectors require an explicit repository root to confine external definitions")
	}
	resolved := map[string]*WorkflowConnector{}
	for _, alias := range sliceutil.SortedKeys(registrations) {
		if !connectorNamePattern.MatchString(alias) {
			return nil, fmt.Errorf("invalid connector registration name %q", alias)
		}
		data, err := json.Marshal(registrations[alias])
		if err != nil {
			return nil, err
		}
		var registration ConnectorRegistration
		if err := connectors.Decode(data, &registration); err != nil {
			return nil, fmt.Errorf("connectors.%s: %w", alias, err)
		}
		if len(registration.GitHubApps)+len(registration.OIDCTokens) != 0 {
			if registration.Spec == nil {
				return nil, errors.New("connector github-app requires an inline declaration")
			}
			if err := validateConnectorGitHubApps(alias, registration); err != nil {
				return nil, fmt.Errorf("connectors.%s: %w", alias, err)
			}
			if err := validateConnectorOIDC(alias, registration); err != nil {
				return nil, fmt.Errorf("connectors.%s: %w", alias, err)
			}
		}
		service, err := loadWorkflowConnectorService(c.gitRoot, registration)
		if err != nil {
			return nil, fmt.Errorf("connectors.%s: %w", alias, err)
		}
		selection, err := resolveConnectorSelection(registration, service)
		if err != nil {
			return nil, fmt.Errorf("connectors.%s: %w", alias, err)
		}
		if len(selection.Triggers)+len(selection.MCP)+len(selection.HTTPMCP)+len(selection.Context)+len(selection.SafeOutputs) == 0 {
			continue
		}
		config, err := resolveWorkflowConnector(service, selection, registration.Context)
		if err != nil {
			return nil, fmt.Errorf("connectors.%s: %w", alias, err)
		}
		resolved[alias] = config
		config.Alias, config.GitHubApps = alias, registration.GitHubApps
		config.OIDCTokens = registration.OIDCTokens
	}
	return resolved, nil
}

func loadWorkflowConnectorService(root string, registration ConnectorRegistration) (*connectors.Service, error) {
	if registration.Spec != nil {
		if registration.Definition != "" {
			return nil, errors.New("connector registration requires exactly one definition or spec")
		}
		if registration.Connection == "" {
			connection := connectors.Connection{
				APIVersion: connectors.APIVersion, Kind: "ConnectorConnection",
				Connector: registration.Spec.ID, Credentials: registration.Credentials, Resources: registration.Resources,
			}
			return connectors.NewService(*registration.Spec, connection)
		}
		return connectors.LoadServiceSpec(root, *registration.Spec, registration.Connection)
	}
	return connectors.LoadService(root, registration.Definition, registration.Connection)
}
