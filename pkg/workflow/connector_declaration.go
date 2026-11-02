package workflow

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/github/gh-aw/pkg/connectors"
	"github.com/github/gh-aw/pkg/parser"
)

// ConnectorDeclaration extends AW frontmatter; versioned runtime documents are
// compiler internals, not additional files an author needs to maintain.
type ConnectorDeclaration struct {
	Experimental bool                            `json:"experimental,omitempty"`
	Docs         []string                        `json:"docs,omitempty"`
	Endpoints    map[string]string               `json:"endpoints"`
	Credentials  map[string]ConnectorCredential  `json:"credentials"`
	Resources    map[string][]string             `json:"resources,omitempty"`
	Triggers     map[string]connectors.Trigger   `json:"triggers,omitempty"`
	Tools        map[string]connectors.ReadTool  `json:"tools,omitempty"`
	MCPServers   map[string]connectors.HTTPMCP   `json:"mcp-servers,omitempty"`
	SafeOutputs  map[string]connectors.Operation `json:"safe-outputs,omitempty"`
}

type ConnectorCredential struct {
	connectors.Credential
	Secret     string               `json:"secret,omitempty"`
	GitHubApp  *ConnectorGitHubApp  `json:"github-app,omitempty"`
	GitHubOIDC *ConnectorGitHubOIDC `json:"github-oidc,omitempty"`
	Token      *ConnectorToken      `json:"token,omitempty"`
}

func normalizeConnectorDeclaration(alias string, value any) (map[string]any, bool, error) {
	config, ok := value.(map[string]any)
	if !ok {
		return nil, false, fmt.Errorf("connector %q must be an object", alias)
	}
	// Retain the prototype's direct registration syntax for existing callers.
	if _, legacy := config["definition"]; legacy {
		return config, false, nil
	}
	if _, legacy := config["spec"]; legacy {
		return config, false, nil
	}
	data, err := json.Marshal(config)
	if err != nil {
		return nil, false, err
	}
	var declaration ConnectorDeclaration
	if err := connectors.Decode(data, &declaration); err != nil {
		return nil, false, fmt.Errorf("connectors.%s: %w", alias, err)
	}
	frontmatter := map[string]any{"connectors": map[string]any{alias: config}}
	if err := parser.ValidateIncludedFileFrontmatterWithSchemaAndLocation(frontmatter, ""); err != nil {
		return nil, false, err
	}
	registration, err := declaration.registration(alias)
	return registration, true, err
}

func (declaration ConnectorDeclaration) registration(alias string) (map[string]any, error) {
	if err := validateConnectorDocumentationURLs(alias, declaration.Docs); err != nil {
		return nil, err
	}
	spec := connectors.ConnectorSpec{
		APIVersion: connectors.APIVersion, Kind: "ConnectorSpec", ID: "aw.github.com/" + alias,
		Endpoints: declaration.Endpoints, Credentials: map[string]connectors.Credential{},
		Triggers: declaration.Triggers, MCP: declaration.Tools, HTTPMCP: declaration.MCPServers,
		SafeOutputs: declaration.SafeOutputs,
	}
	credentials := map[string]connectors.SecretRef{}
	apps := map[string]*ConnectorGitHubApp{}
	oidc := map[string]*ConnectorGitHubOIDC{}
	for name, credential := range declaration.Credentials {
		spec.Credentials[name] = credential.Credential
		if strings.HasPrefix(credential.Secret, connectorAppSecretPrefix) {
			return nil, fmt.Errorf("connector secret names beginning with %s are reserved", connectorAppSecretPrefix)
		}
		sources := 0
		for _, configured := range []bool{credential.Secret != "", credential.Token != nil, credential.GitHubApp != nil, credential.GitHubOIDC != nil} {
			if configured {
				sources++
			}
		}
		if sources > 1 {
			return nil, fmt.Errorf("connector credential %q requires exactly one token, github-app, github-oidc or legacy secret route", name)
		}
		if credential.Token != nil {
			credential.Secret = credential.Token.Secret
			if credential.Secret == "" || strings.HasPrefix(credential.Secret, connectorAppSecretPrefix) {
				return nil, fmt.Errorf("connector token route %q requires a non-reserved secret name", name)
			}
		}
		if credential.GitHubApp != nil {
			apps[name] = credential.GitHubApp
			credential.Secret = connectorAppSecret(alias, name)
		}
		if credential.GitHubOIDC != nil {
			oidc[name] = credential.GitHubOIDC
			credential.Secret = connectorAppSecret(alias, name)
		}
		if credential.Secret != "" {
			credentials[name] = connectors.SecretRef{Secret: credential.Secret}
		}
	}
	resources := nonemptyConnectorResources(declaration.Resources)
	fields := map[string]any{"experimental": declaration.Experimental, "spec": spec, "credentials": credentials, "resources": resources, "github-apps": apps, "oidc-tokens": oidc}
	if declaration.Docs != nil {
		fields["docs"] = declaration.Docs
	}
	data, err := json.Marshal(fields)
	if err != nil {
		return nil, err
	}
	var registration map[string]any
	if err := connectors.Decode(data, &registration); err != nil {
		return nil, err
	}
	return registration, nil
}

func validateConnectorDocumentationURLs(alias string, references []string) error {
	for _, reference := range references {
		docURL, err := url.ParseRequestURI(reference)
		if err != nil || docURL.Scheme != "https" || docURL.Hostname() == "" || docURL.User != nil || strings.ContainsAny(reference, " \t\r\n") || strings.Contains(reference, "${") {
			return fmt.Errorf("connectors.%s.docs: each reference must be a literal HTTPS documentation URL without credentials", alias)
		}
	}
	return nil
}

func nonemptyConnectorResources(resources map[string][]string) map[string][]string {
	result := map[string][]string{}
	for name, values := range resources {
		if len(values) > 0 {
			result[name] = values
		}
	}
	return result
}
