package connectors

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

var toolPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.-]{0,127}$`)
var argumentPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]{0,127}$`)
var writeGraphQLPattern = regexp.MustCompile(`\b(mutation|subscription)\b`)

func validatePayload(op Operation, phase string) error {
	request := op.Request
	if request.Method == http.MethodGet && (len(request.Body) != 0 || request.GraphQL != "" || len(request.Patch) != 0) {
		return errors.New("GET requests cannot contain a body, GraphQL document or JSON Patch")
	}
	if request.GraphQL != "" && phase != "safe-output" && writeGraphQLPattern.MatchString(request.GraphQL) {
		return errors.New("read GraphQL documents cannot contain mutation or subscription tokens")
	}
	if len(request.Patch) != 0 {
		if phase != "safe-output" || request.GraphQL != "" || len(request.Body) != 0 || (request.Method != http.MethodPatch && request.Method != http.MethodPost) {
			return errors.New("JSON Patch requires safe-output POST/PATCH without another body")
		}
		for _, patch := range request.Patch {
			if patch.Op != "add" && patch.Op != "replace" && patch.Op != "test" {
				return errors.New("JSON Patch supports only fixed-path add, replace and test operations")
			}
			if !validPointer(patch.Path) || strings.ContainsAny(patch.Path, "{}$") {
				return errors.New("JSON Patch paths must be fixed JSON Pointers")
			}
		}
	}
	for pointer := range op.ResponseChecks {
		if !validPointer(pointer) {
			return errors.New("responseChecks keys must be JSON Pointers")
		}
	}
	for pointer, resource := range op.ResponseResources {
		if !validPointer(pointer) || !namePattern.MatchString(resource) {
			return errors.New("responseResources requires JSON Pointer keys and resource names")
		}
	}
	for _, pointer := range op.ResponseRequired {
		if !validPointer(pointer) {
			return errors.New("responseRequired entries must be JSON Pointers")
		}
	}
	for name, pointer := range op.Project {
		if !namePattern.MatchString(name) || !validPointer(pointer) {
			return errors.New("project requires named fields with JSON Pointer values")
		}
	}
	return nil
}

func validateValue(value Value, inputs map[string]Input) error {
	if (value.Input == "") == (value.Literal == nil) {
		return errors.New("value requires exactly one input or non-null literal")
	}
	if value.Input != "" {
		if _, ok := inputs[value.Input]; !ok {
			return fmt.Errorf("value references unknown input %q", value.Input)
		}
	}
	return nil
}

func (s *ConnectorSpec) validateHTTPMCP() error {
	for name, server := range s.HTTPMCP {
		u, err := url.Parse(server.URL)
		if !namePattern.MatchString(name) || err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || strings.ContainsAny(server.URL, "{}$\\") {
			return fmt.Errorf("HTTP MCP %q requires a fixed HTTPS URL", name)
		}
		credential, ok := s.Credentials[server.Credential]
		if !ok || credential.Phase != "agent" || !server.ReadOnly || len(server.Tools) == 0 {
			return fmt.Errorf("HTTP MCP %q requires an agent credential, readOnly: true and exact tools", name)
		}
		if server.MaxResponseBytes < 1 || server.MaxResponseBytes > 1024*1024 {
			return fmt.Errorf("HTTP MCP %q requires maxResponseBytes between 1 and 1048576", name)
		}
		if err := validateMCPHeaders(server.Headers); err != nil {
			return err
		}
		for tool, constraints := range server.Tools {
			if !toolPattern.MatchString(tool) {
				return fmt.Errorf("HTTP MCP %q has invalid exact tool name %q", name, tool)
			}
			if err := validateInputs(constraints.Inputs); err != nil {
				return err
			}
			for input, resource := range constraints.Resources {
				field, ok := constraints.Inputs[input]
				if !ok || field.Type != "string" || !field.Required || !namePattern.MatchString(resource) {
					return fmt.Errorf("HTTP MCP %q resource constraints must reference required string inputs", name)
				}
			}
		}
	}
	return nil
}

func validateMCPHeaders(headers map[string]string) error {
	for name, value := range headers {
		if !strings.HasPrefix(name, "X-MCP-") || strings.ContainsAny(name+value, "\r\n$") {
			return errors.New("HTTP MCP fixed headers must use X-MCP-* names without expressions or line breaks")
		}
	}
	return nil
}
