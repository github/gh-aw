package connectors

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

var namePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)
var idPattern = regexp.MustCompile(`^[a-z][a-z0-9-]*(\.[a-z][a-z0-9-]*)+/[a-z][a-z0-9-]*$`)
var pointerPattern = regexp.MustCompile(`^/(?:[^~]|~[01])*$`)

func (s *ConnectorSpec) Validate() error {
	if s.APIVersion != APIVersion || s.Kind != "ConnectorSpec" {
		return errors.New("expected apiVersion github.com/gh-aw/connectors/v1 and kind ConnectorSpec")
	}
	if !idPattern.MatchString(s.ID) {
		return errors.New("connector id must be namespaced, for example example.com/issues")
	}
	if len(s.Endpoints) == 0 {
		return errors.New("connector endpoints must not be empty")
	}
	for name, endpoint := range s.Endpoints {
		u, err := url.Parse(endpoint)
		if !namePattern.MatchString(name) || err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || strings.ContainsAny(endpoint, "?#") || (u.Path != "" && u.Path != "/") {
			return fmt.Errorf("endpoint %q must be a named HTTPS origin without credentials, path, query or fragment", name)
		}
	}
	if err := s.validateCredentials(); err != nil {
		return err
	}
	if err := s.validateTriggers(); err != nil {
		return err
	}
	return s.validateOperations()
}

func (s *ConnectorSpec) validateCredentials() error {
	for name, credential := range s.Credentials {
		if !namePattern.MatchString(name) || (credential.Phase != "agent" && credential.Phase != "safe-output" && credential.Phase != "context") {
			return fmt.Errorf("credential %q requires phase agent, safe-output or context", name)
		}
		if credential.Header != "Authorization" && credential.Header != "X-API-Key" && credential.Header != "PRIVATE-TOKEN" {
			return fmt.Errorf("credential %q has unsupported authentication header", name)
		}
		if credential.Scheme != "bearer" && credential.Scheme != "token" && credential.Scheme != "basic" && credential.Scheme != "raw" {
			return fmt.Errorf("credential %q has unsupported authentication scheme", name)
		}
		if len(credential.Scopes) == 0 {
			return fmt.Errorf("credential %q must declare least-privilege scopes", name)
		}
	}
	return nil
}

func (s *ConnectorSpec) validateTriggers() error {
	for name, trigger := range s.Triggers {
		if !namePattern.MatchString(name) || !idPattern.MatchString(trigger.VerificationProfile) || len(trigger.NativeEvents) == 0 || len(trigger.Match) == 0 || len(trigger.Normalize) == 0 {
			return fmt.Errorf("trigger %q requires verificationProfile, nativeEvents, match and normalize", name)
		}
		for pointer := range trigger.Match {
			if !validPointer(pointer) {
				return fmt.Errorf("trigger %q has invalid match JSON Pointer", name)
			}
		}
		for field, pointer := range trigger.Normalize {
			if !namePattern.MatchString(field) || !validPointer(pointer) {
				return fmt.Errorf("trigger %q has invalid normalization field or JSON Pointer", name)
			}
			for pointer, resource := range trigger.Resources {
				if !validPointer(pointer) || !namePattern.MatchString(resource) {
					return fmt.Errorf("trigger %q resource constraints require JSON Pointers and resource names", name)
				}
				if err := validatePrimitiveTypes(trigger.Types); err != nil {
					return fmt.Errorf("trigger %q: %w", name, err)
				}
			}
		}
	}
	return nil
}

func (s *ConnectorSpec) validateOperations() error {
	for name, tool := range s.MCP {
		if !toolPattern.MatchString(tool.Tool) {
			return fmt.Errorf("MCP %q requires an exact tool name (no wildcards)", name)
		}
		if err := s.validateOperation(name, tool.Operation, "agent"); err != nil {
			return err
		}
	}
	for name, operation := range s.SafeOutputs {
		if err := s.validateOperation(name, operation, "safe-output"); err != nil {
			return err
		}
	}
	for name, operation := range s.Context {
		if err := s.validateOperation(name, operation, "context"); err != nil {
			return err
		}
	}
	if err := s.validateHTTPMCP(); err != nil {
		return err
	}
	if err := s.validateCreatedResources(); err != nil {
		return err
	}
	if len(s.Triggers)+len(s.MCP)+len(s.HTTPMCP)+len(s.SafeOutputs)+len(s.Context) == 0 {
		return errors.New("connector must declare at least one capability")
	}
	return nil
}

func validPointer(pointer string) bool {
	return pointerPattern.MatchString(pointer)
}

func (s *ConnectorSpec) validateOperation(name string, op Operation, phase string) error {
	if !namePattern.MatchString(name) {
		return fmt.Errorf("invalid %s operation name %q", phase, name)
	}
	credential, ok := s.Credentials[op.Credential]
	if !ok || credential.Phase != phase {
		return fmt.Errorf("%s %q requires a credential declared for that phase", phase, name)
	}
	if _, ok := s.Endpoints[op.Request.Endpoint]; !ok {
		return fmt.Errorf("operation %q references unknown endpoint", name)
	}
	if op.MaxResponseBytes < 1 || op.MaxResponseBytes > 1024*1024 {
		return fmt.Errorf("operation %q maxResponseBytes must be between 1 and 1048576", name)
	}
	method := op.Request.Method
	if method != "GET" && method != "POST" && method != "PUT" && method != "PATCH" && method != "DELETE" {
		return fmt.Errorf("operation %q has unsupported method", name)
	}
	if phase != "safe-output" && method != "GET" && (method != "POST" || !strings.HasPrefix(strings.TrimSpace(op.Request.GraphQL), "query ")) {
		return fmt.Errorf("operation %q must be a GET or fixed GraphQL query", name)
	}
	if op.Request.GraphQL != "" && (method != "POST" || len(op.Request.Body) != 0) {
		return fmt.Errorf("operation %q GraphQL requires POST and no body (query contains variable declarations; body inputs use query)", name)
	}
	if err := validatePayload(op, phase); err != nil {
		return fmt.Errorf("operation %q: %w", name, err)
	}
	path := op.Request.Path
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") || strings.ContainsAny(path, "?#\\%") || strings.Contains(path, "..") || strings.Contains(path, "${") {
		return fmt.Errorf("operation %q requires a fixed absolute path without query, escaping or expressions", name)
	}
	if err := validateInputs(op.Inputs); err != nil {
		return fmt.Errorf("operation %q: %w", name, err)
	}
	if err := s.validateTrustedInputs(name, op, phase); err != nil {
		return fmt.Errorf("operation %q: %w", name, err)
	}
	for pointer, input := range op.ResponseContains {
		field, exists := op.Inputs[input]
		if strings.Count(pointer, "/*/") != 1 || !validPointer(pointer) || !exists || !field.Required || field.Type != "string" {
			return errors.New("responseContains requires an array-element pointer and a required string input")
		}
	}
	return validateRequestBindings(name, op, path)
}

func validateInputs(inputs map[string]Input) error {
	for key, input := range inputs {
		if !argumentPattern.MatchString(key) {
			return fmt.Errorf("invalid input name %q", key)
		}
		if input.Type != "string" && input.Type != "integer" && input.Type != "number" && input.Type != "boolean" {
			return fmt.Errorf("input %q has unsupported type", key)
		}
		if input.Type == "string" && (input.MaxLength < 1 || input.MaxLength > 65536) {
			return fmt.Errorf("string input %q requires maxLength between 1 and 65536", key)
		}
		if input.Type != "string" && (input.MaxLength != 0 || len(input.Enum) != 0) {
			return fmt.Errorf("input %q string constraints require type string", key)
		}
		if input.Type != "number" && input.Type != "integer" && (input.Minimum != nil || input.Maximum != nil) {
			return fmt.Errorf("input %q numeric bounds require number or integer type", key)
		}
		if input.Pattern != "" {
			if input.Type != "string" || len(input.Pattern) > 1024 {
				return fmt.Errorf("input %q pattern requires a string and at most 1024 bytes", key)
			}
			//nolint:regexpdynamicpattern // RE2 pattern is bounded to 1024 bytes and compilation errors are returned.
			if _, err := regexp.Compile(input.Pattern); err != nil {
				return fmt.Errorf("input %q has invalid pattern: %w", key, err)
			}
		}
		if input.Minimum != nil && input.Maximum != nil && *input.Minimum > *input.Maximum {
			return fmt.Errorf("input %q minimum exceeds maximum", key)
		}
	}
	return nil
}

func validateRequestBindings(name string, op Operation, path string) error {
	for placeholder, input := range op.Request.PathInputs {
		if !namePattern.MatchString(placeholder) || !strings.Contains(path, "{"+placeholder+"}") {
			return fmt.Errorf("operation %q has unused or invalid path input", name)
		}
		declared, ok := op.Inputs[input]
		if !ok || !declared.Required || declared.Type != "string" {
			return fmt.Errorf("operation %q path inputs must reference required strings", name)
		}
		path = strings.ReplaceAll(path, "{"+placeholder+"}", "value")
	}
	if strings.ContainsAny(path, "{}") {
		return fmt.Errorf("operation %q has unresolved path placeholders", name)
	}
	for input, resource := range op.Resources {
		declared, ok := op.Inputs[input]
		if !ok || !declared.Required || declared.Type != "string" || !namePattern.MatchString(resource) {
			return fmt.Errorf("operation %q resource constraints must reference required string inputs and named connection resources", name)
		}
	}
	for _, values := range []map[string]Value{op.Request.Query, op.Request.Body} {
		for key, value := range values {
			if key == "" {
				return fmt.Errorf("operation %q request keys must not be empty", name)
			}
			if err := validateValue(value, op.Inputs); err != nil {
				return fmt.Errorf("operation %q: %w", name, err)
			}
		}
	}
	for _, patch := range op.Request.Patch {
		if err := validateValue(patch.Value, op.Inputs); err != nil {
			return fmt.Errorf("operation %q patch: %w", name, err)
		}
	}
	if op.Request.GraphQL == "" {
		for _, value := range op.Request.Query {
			if value.Object != nil || value.Array != nil || value.Encoding != "" || !primitiveLiteral(value.Literal) {
				return fmt.Errorf("operation %q REST query values must be primitive scalars", name)
			}
		}
	}
	return nil
}
