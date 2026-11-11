package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/github/gh-aw/pkg/connectors"
)

// DevelopmentConnector is an import-schema-resolved declaration. Loading it
// grants no capabilities, reads no credentials, and activates no ingress.
type DevelopmentConnector struct {
	Registration ConnectorRegistration
	Service      *connectors.Service
	Source       string
	Markdown     string
}

// LoadDevelopmentConnector uses the compiler's normal import and declaration
// normalization, including typed with inputs and their defaults. Sources are
// repository-owned files; no built-in provider catalog is consulted.
func LoadDevelopmentConnector(root, id, source string, inputs map[string]any, grants map[string][]string) (*DevelopmentConnector, error) {
	if !connectorNamePattern.MatchString(id) {
		return nil, errors.New("connector id must be a lowercase connector registration name")
	}
	c := NewCompiler()
	if root == "" {
		root = c.gitRoot
	}
	if root == "" {
		root = "."
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	c.gitRoot = root
	markdown, source, err := developmentConnectorSource(root, id, source)
	if err != nil {
		return nil, err
	}
	importSpec := map[string]any{"uses": source}
	if len(inputs) > 0 {
		importSpec["with"] = inputs
	}
	frontmatter := map[string]any{"imports": []any{importSpec}}
	virtualPath := filepath.Join(root, "connector-development.md")
	if err := c.normalizeImportedConnectors(frontmatter, virtualPath, ""); err != nil {
		return nil, fmt.Errorf("resolve connector import: %w", err)
	}
	registration, err := developmentConnectorRegistration(id, frontmatter, grants)
	if err != nil {
		return nil, err
	}
	service, err := loadWorkflowConnectorService(root, registration)
	if err != nil {
		return nil, err
	}
	return &DevelopmentConnector{Registration: registration, Service: service, Source: source, Markdown: string(markdown)}, nil
}

func developmentConnectorRegistration(id string, frontmatter map[string]any, grants map[string][]string) (ConnectorRegistration, error) {
	var registration ConnectorRegistration
	registrations, ok := frontmatter["connectors"].(map[string]any)
	if !ok || len(registrations) != 1 || registrations[id] == nil {
		return registration, fmt.Errorf("source must register exactly connector %q", id)
	}
	data, err := json.Marshal(registrations[id])
	if err != nil {
		return registration, err
	}
	if err := connectors.Decode(data, &registration); err != nil {
		return registration, err
	}
	if registration.Spec == nil || registration.Connection != "" || registration.Definition != "" {
		return registration, errors.New("connector development requires a self-contained AW Markdown declaration")
	}
	if err := validateConnectorGitHubApps(id, registration); err != nil {
		return registration, err
	}
	if err := validateConnectorOIDC(id, registration); err != nil {
		return registration, err
	}
	for resource, values := range grants {
		if !developmentConnectorResource(registration.Spec, resource) {
			return registration, fmt.Errorf("grant %q is not a declared resource constraint", resource)
		}
		if registration.Resources == nil {
			registration.Resources = map[string][]string{}
		}
		registration.Resources[resource] = values
	}
	return registration, nil
}

func developmentConnectorSource(root, id, source string) (content []byte, resolved string, resultErr error) {
	dir, err := os.OpenRoot(root)
	if err != nil {
		return nil, "", fmt.Errorf("open connector repository root: %w", err)
	}
	defer func() {
		if err := dir.Close(); err != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("close connector repository root: %w", err))
		}
	}()
	if source == "" {
		source = filepath.Join(".github", "workflows", "connectors", id+".md")
		_, err := dir.Lstat(source)
		if errors.Is(err, os.ErrNotExist) {
			return nil, "", fmt.Errorf("connector %q not found locally; create %s, use --file with a repository-owned AW Markdown declaration, or create a local wrapper using the normal imports URL syntax for a trusted connector source", id, source)
		}
		if err != nil {
			return nil, "", fmt.Errorf("inspect local connector declaration: %w", err)
		}
	}
	if filepath.IsAbs(source) {
		source, err = filepath.Rel(root, source)
		if err != nil {
			return nil, "", err
		}
	}
	if !filepath.IsLocal(source) {
		return nil, "", errors.New("--file must refer to a declaration within the repository root")
	}
	file, err := dir.Open(source)
	if err != nil {
		return nil, "", fmt.Errorf("open local connector declaration (no source fallback): %w", err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("close connector declaration: %w", err))
		}
	}()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, "", errors.New("connector declaration must be a regular file")
	}
	data, err := io.ReadAll(io.LimitReader(file, connectors.MaxDocumentBytes+1))
	if err != nil {
		return nil, "", err
	}
	if len(data) > connectors.MaxDocumentBytes {
		return nil, "", errors.New("connector declaration exceeds 256 KiB")
	}
	return data, filepath.ToSlash(source), nil
}

func developmentConnectorResource(spec *connectors.ConnectorSpec, resource string) bool {
	has := func(resources map[string]string) bool {
		for _, name := range resources {
			if name == resource {
				return true
			}
		}
		return false
	}
	operationHas := func(op connectors.Operation) bool {
		if has(op.Resources) || has(op.ResponseResources) {
			return true
		}
		if op.Preflight != nil && (has(op.Preflight.Resources) || has(op.Preflight.ResponseResources)) {
			return true
		}
		for _, lookup := range op.Lookups {
			if has(lookup.Resources) || has(lookup.ResponseResources) {
				return true
			}
		}
		return false
	}
	for _, op := range spec.MCP {
		if operationHas(op.Operation) {
			return true
		}
	}
	for _, ops := range []map[string]connectors.Operation{spec.Context, spec.SafeOutputs} {
		for _, op := range ops {
			if operationHas(op) {
				return true
			}
		}
	}
	for _, server := range spec.HTTPMCP {
		for _, tool := range server.Tools {
			if has(tool.Resources) {
				return true
			}
		}
	}
	for _, trigger := range spec.Triggers {
		if has(trigger.Resources) {
			return true
		}
	}
	return false
}
