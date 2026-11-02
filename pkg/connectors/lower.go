package connectors

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

var secretPattern = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)

// Plan is a frozen definition, connection and explicit capability selection.
// It contains secret references only. Digest binds all three documents.
type Plan struct {
	Digest     string `json:"digest"`
	spec       ConnectorSpec
	connection Connection
	binding    Binding
}

// Lower loads repository-confined local files and freezes their validated
// content. It does not enroll sources, resolve secrets, or contact services.
func Lower(root string, binding Binding) (*Plan, error) {
	if binding.APIVersion != APIVersion || binding.Kind != "ConnectorBinding" {
		return nil, errors.New("expected versioned ConnectorBinding")
	}
	dir, err := os.OpenRoot(root)
	if err != nil {
		return nil, fmt.Errorf("open connector repository root: %w", err)
	}
	defer func() {
		if err := dir.Close(); err != nil {
			log.Printf("close connector repository root: %v", err)
		}
	}()
	var spec ConnectorSpec
	if err := readLocal(dir, binding.Definition, &spec); err != nil {
		return nil, err
	}
	var connection Connection
	if err := readLocal(dir, binding.Connection, &connection); err != nil {
		return nil, err
	}
	if err := spec.Validate(); err != nil {
		return nil, err
	}
	if err := validateConnection(spec, connection); err != nil {
		return nil, err
	}
	if err := validateSelection(spec, binding, connection); err != nil {
		return nil, err
	}
	return freezePlan(spec, connection, binding)
}

func readLocal(root *os.Root, path string, target any) error {
	if !filepath.IsLocal(path) || strings.ContainsAny(path, "\\:$") {
		return fmt.Errorf("connector path %q must be repository-relative without expressions", path)
	}
	file, err := root.Open(path)
	if err != nil {
		return fmt.Errorf("open confined connector file %q: %w", path, err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			log.Printf("close connector file: %v", err)
		}
	}()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("connector path %q must be a readable regular file", path)
	}
	data, err := io.ReadAll(io.LimitReader(file, MaxDocumentBytes+1))
	if err != nil {
		return fmt.Errorf("read connector file %q: %w", path, err)
	}
	return Decode(data, target)
}

func freezePlan(spec ConnectorSpec, connection Connection, binding Binding) (*Plan, error) {
	data, err := json.Marshal(struct {
		Spec       ConnectorSpec
		Connection Connection
		Binding    Binding
	}{spec, connection, binding})
	if err != nil {
		return nil, fmt.Errorf("freeze connector plan: %w", err)
	}
	// Copy the selection too: callers must not be able to widen a frozen plan
	// by mutating their original binding slices after lowering.
	var frozen struct {
		Spec       ConnectorSpec
		Connection Connection
		Binding    Binding
	}
	if err := decodeJSON(data, &frozen); err != nil {
		return nil, fmt.Errorf("copy connector plan: %w", err)
	}
	hash := sha256.Sum256(data)
	return &Plan{Digest: hex.EncodeToString(hash[:]), spec: frozen.Spec, connection: frozen.Connection, binding: frozen.Binding}, nil
}

func validateConnection(spec ConnectorSpec, connection Connection) error {
	if connection.APIVersion != APIVersion || connection.Kind != "ConnectorConnection" || connection.Connector != spec.ID {
		return errors.New("connection must be versioned ConnectorConnection matching the definition id")
	}
	for name, ref := range connection.Credentials {
		if _, ok := spec.Credentials[name]; !ok || !secretPattern.MatchString(ref.Secret) || strings.HasPrefix(ref.Secret, "GITHUB_") {
			return fmt.Errorf("connection credential %q requires a declared credential and a non-reserved secret name", name)
		}
	}
	for resource, values := range connection.Resources {
		if !namePattern.MatchString(resource) || len(values) == 0 || slices.Contains(values, "") || slices.Contains(values, "*") {
			return fmt.Errorf("connection resource %q requires a nonempty exact allowlist", resource)
		}
	}
	return nil
}

func validateSelection(spec ConnectorSpec, binding Binding, connection Connection) error {
	groups := map[string][]string{"trigger": binding.Triggers, "mcp": binding.MCP, "httpMcp": binding.HTTPMCP, "safe-output": binding.SafeOutputs, "context": binding.Context}
	total := 0
	for phase, names := range groups {
		seen := map[string]struct{}{}
		for _, name := range names {
			if _, exists := seen[name]; exists {
				return fmt.Errorf("duplicate %s selection %q", phase, name)
			}
			seen[name] = struct{}{}
			op, ok := selectedOperation(spec, phase, name)
			if !ok {
				return fmt.Errorf("unknown %s capability %q", phase, name)
			}
			if phase == "trigger" {
				if err := requireResources(spec.Triggers[name].Resources, connection.Resources); err != nil {
					return err
				}
				continue
			}
			if _, ok := connection.Credentials[op.Credential]; !ok {
				return fmt.Errorf("selected capability %q has no connection credential reference", name)
			}
			for _, resource := range op.Resources {
				if len(connection.Resources[resource]) == 0 {
					return fmt.Errorf("selected capability %q has no connection resource allowlist %q", name, resource)
				}
			}
			if err := requireResources(op.ResponseResources, connection.Resources); err != nil {
				return err
			}
		}
		total += len(names)
	}
	if total == 0 {
		return errors.New("connector binding must explicitly select at least one capability")
	}
	if err := validateRemoteResources(spec, binding, connection); err != nil {
		return err
	}
	return validateToolNames(spec, binding)
}

func requireResources(constraints map[string]string, resources map[string][]string) error {
	for _, resource := range constraints {
		if len(resources[resource]) == 0 {
			return fmt.Errorf("connection resource allowlist %q is required", resource)
		}
	}
	return nil
}

func selectedOperation(spec ConnectorSpec, phase, name string) (Operation, bool) {
	switch phase {
	case "trigger":
		_, ok := spec.Triggers[name]
		return Operation{}, ok
	case "mcp":
		tool, ok := spec.MCP[name]
		return tool.Operation, ok
	case "httpMcp":
		server, ok := spec.HTTPMCP[name]
		return Operation{Credential: server.Credential}, ok
	case "safe-output":
		op, ok := spec.SafeOutputs[name]
		return op, ok
	case "context":
		op, ok := spec.Context[name]
		return op, ok
	}
	return Operation{}, false
}

func validateRemoteResources(spec ConnectorSpec, binding Binding, connection Connection) error {
	for _, name := range binding.HTTPMCP {
		for _, tool := range spec.HTTPMCP[name].Tools {
			for _, resource := range tool.Resources {
				if len(connection.Resources[resource]) == 0 {
					return fmt.Errorf("HTTP MCP %q requires connection resource %q", name, resource)
				}
			}
		}
	}
	return nil
}
