package connectors

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strings"
	"time"
)

// SecretResolver is host-owned and should expose secrets only for its phase.
// Manifests never contain values, environment expressions, or token acquisition.
type SecretResolver func(ctx context.Context, phase, secretName string) (string, error)

// Runtime is an explicitly trusted host execution boundary. AllowedOrigins
// comes from the operator, not from an agent or connector connection.
type Runtime struct {
	Phase          string
	AllowedOrigins []string
	ResolveSecret  SecretResolver
	Client         *http.Client
	AllowWrites    bool
}

type Result struct {
	Connector  string          `json:"connector"`
	Capability string          `json:"capability"`
	Untrusted  bool            `json:"untrusted"`
	StatusCode int             `json:"statusCode"`
	Data       json.RawMessage `json:"data"`
}

// Execute revalidates the typed request at the execution boundary. No operation
// retries, redirects, pagination, or credential fallback are performed.
func (p *Plan) Execute(ctx context.Context, runtime Runtime, capability, name string, args map[string]any) (*Result, error) {
	op, err := p.authorizeOperation(runtime, capability, name, args)
	if err != nil {
		return nil, err
	}
	request, err := p.buildRequest(ctx, op, args)
	if err != nil {
		return nil, err
	}
	if err := p.authenticate(ctx, runtime, op.Credential, request); err != nil {
		return nil, err
	}
	client := boundedClient(runtime.Client)
	response, err := client.Do(request)
	if err != nil {
		return nil, errors.New("connector HTTP request failed; outcome may be unknown, no retry performed")
	}
	defer func() {
		if err := response.Body.Close(); err != nil {
			log.Printf("close connector response: %v", err)
		}
	}()
	data, err := io.ReadAll(io.LimitReader(response.Body, int64(op.MaxResponseBytes)+1))
	if err != nil || len(data) > op.MaxResponseBytes {
		return nil, errors.New("connector response could not be read within its byte bound")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("connector HTTP status %d; no retry performed", response.StatusCode)
	}
	data, err = p.validateResponse(op, data)
	if err != nil {
		return nil, err
	}
	return &Result{Connector: p.spec.ID, Capability: path.Join(capability, name), Untrusted: true, StatusCode: response.StatusCode, Data: data}, nil
}

func (p *Plan) authorizeOperation(runtime Runtime, capability, name string, args map[string]any) (Operation, error) {
	var selected []string
	phase := capability
	switch capability {
	case "mcp":
		selected, phase = p.binding.MCP, "agent"
	case "safe-output":
		selected = p.binding.SafeOutputs
	case "context":
		selected = p.binding.Context
	default:
		return Operation{}, errors.New("capability must be mcp, context or safe-output")
	}
	if !slices.Contains(selected, name) || runtime.Phase != phase {
		return Operation{}, errors.New("connector capability is not selected or runtime phase does not match")
	}
	if phase == "safe-output" && !runtime.AllowWrites {
		return Operation{}, errors.New("connector safe-output execution requires explicit host write authorization")
	}
	op, ok := selectedOperation(p.spec, capability, name)
	if !ok {
		return Operation{}, errors.New("unknown connector operation")
	}
	if err := runtime.checkOrigin(p.spec.Endpoints[op.Request.Endpoint]); err != nil {
		return Operation{}, err
	}
	if err := validateArguments(op.Inputs, op.Resources, p.connection.Resources, args); err != nil {
		return Operation{}, err
	}
	return op, nil
}

func (runtime Runtime) checkOrigin(endpoint string) error {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "https" || u.User != nil || !slices.Contains(runtime.AllowedOrigins, u.Scheme+"://"+u.Host) {
		return errors.New("connector endpoint origin is not approved by the host")
	}
	return nil
}

func boundedClient(base *http.Client) *http.Client {
	client := http.Client{Timeout: 20 * time.Second}
	if base != nil {
		client = *base
	}
	if client.Timeout <= 0 || client.Timeout > 20*time.Second {
		client.Timeout = 20 * time.Second
	}
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return errors.New("connector redirects are prohibited")
	}
	client.Jar = nil
	return &client
}

func (p *Plan) authenticate(ctx context.Context, runtime Runtime, name string, request *http.Request) error {
	credential, ok := p.spec.Credentials[name]
	if !ok || credential.Phase != runtime.Phase || runtime.ResolveSecret == nil {
		return errors.New("connector credential phase mismatch or missing host secret resolver")
	}
	ref, ok := p.connection.Credentials[name]
	if !ok {
		return errors.New("connector connection credential reference is missing")
	}
	secret, err := runtime.ResolveSecret(ctx, runtime.Phase, ref.Secret)
	if err != nil || secret == "" || strings.ContainsAny(secret, "\r\n") {
		return errors.New("connector credential resolution failed")
	}
	prefix := map[string]string{"bearer": "Bearer ", "token": "token ", "basic": "Basic ", "raw": ""}[credential.Scheme]
	request.Header.Set(credential.Header, prefix+secret)
	return nil
}

func (p *Plan) buildRequest(ctx context.Context, op Operation, args map[string]any) (*http.Request, error) {
	path := op.Request.Path
	for placeholder, input := range op.Request.PathInputs {
		value, ok := args[input].(string)
		if !ok || value == "" || value == "." || strings.ContainsAny(value, "/\\%?#\r\n") || strings.Contains(value, "..") {
			return nil, fmt.Errorf("path argument %q must be one nonempty safe segment", input)
		}
		path = strings.ReplaceAll(path, "{"+placeholder+"}", url.PathEscape(value))
	}
	u, err := url.Parse(strings.TrimSuffix(p.spec.Endpoints[op.Request.Endpoint], "/") + path)
	if err != nil {
		return nil, errors.New("connector request URL could not be constructed")
	}
	body, contentType, err := requestBody(op.Request, args)
	if err != nil {
		return nil, err
	}
	if op.Request.GraphQL == "" {
		query := u.Query()
		for key, value := range resolveValues(op.Request.Query, args) {
			query.Set(key, fmt.Sprint(value))
		}
		u.RawQuery = query.Encode()
	}
	request, err := http.NewRequestWithContext(ctx, op.Request.Method, u.String(), bytes.NewReader(body))
	if err != nil {
		return nil, errors.New("connector request could not be constructed")
	}
	request.Header.Set("Accept", "application/json")
	if len(body) != 0 {
		request.Header.Set("Content-Type", contentType)
	}
	return request, nil
}

func resolveValues(values map[string]Value, args map[string]any) map[string]any {
	result := map[string]any{}
	for key, value := range values {
		if value.Input == "" {
			result[key] = value.Literal
		} else if input, ok := args[value.Input]; ok {
			result[key] = input
		}
	}
	return result
}

func requestBody(request Request, args map[string]any) ([]byte, string, error) {
	var body any
	contentType := "application/json"
	switch {
	case request.GraphQL != "":
		body = map[string]any{"query": request.GraphQL, "variables": resolveValues(request.Query, args)}
	case len(request.Patch) != 0:
		patches := make([]map[string]any, 0, len(request.Patch))
		for _, patch := range request.Patch {
			value, ok := resolveValues(map[string]Value{"value": patch.Value}, args)["value"]
			if !ok {
				return nil, "", errors.New("JSON Patch value is missing")
			}
			patches = append(patches, map[string]any{"op": patch.Op, "path": patch.Path, "value": value})
		}
		body, contentType = patches, "application/json-patch+json"
	case len(request.Body) != 0:
		body = resolveValues(request.Body, args)
	default:
		return nil, contentType, nil
	}
	data, err := json.Marshal(body)
	if err != nil {
		return nil, "", fmt.Errorf("encode typed connector request: %w", err)
	}
	if len(data) > MaxDocumentBytes {
		return nil, "", errors.New("connector request body exceeds byte bound")
	}
	return data, contentType, nil
}
