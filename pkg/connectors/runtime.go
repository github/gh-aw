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
	Phase           string
	AllowedOrigins  []string
	ResolveSecret   SecretResolver
	Client          *http.Client
	AllowWrites     bool
	StagedFilesRoot string
	binaryBody      []byte
}

type Result struct {
	Connector       string          `json:"connector"`
	Capability      string          `json:"capability"`
	Untrusted       bool            `json:"untrusted"`
	StatusCode      int             `json:"statusCode"`
	Data            json.RawMessage `json:"data"`
	CreatedResource string          `json:"createdResource,omitempty"`
}

// Execute revalidates the typed request at the execution boundary. No
// redirects, pagination, or credential fallback are performed. Retries require
// an explicit bounded policy and never replay uncertain transport failures.
func (p *Plan) Execute(ctx context.Context, runtime Runtime, capability, name string, args map[string]any) (*Result, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	op, err := p.authorizeOperation(runtime, capability, name, args)
	if err != nil {
		return nil, err
	}
	args, err = p.resolveInputLookups(ctx, runtime, op, args)
	if err != nil {
		return nil, err
	}
	if op.Request.Binary != nil {
		runtime.binaryBody, err = readConnectorFile(runtime.StagedFilesRoot, op.Request.Binary, args)
		if err != nil {
			return nil, err
		}
	}
	if err := p.executeOperationChecks(ctx, runtime, op, args); err != nil {
		return nil, err
	}
	return p.executeOperation(ctx, runtime, capability, name, op, args)
}

func (p *Plan) executeOperation(ctx context.Context, runtime Runtime, capability, name string, op Operation, args map[string]any) (*Result, error) {
	if err := runtime.checkOrigin(p.spec.Endpoints[op.Request.Endpoint]); err != nil {
		return nil, err
	}
	request, err := p.buildRequest(ctx, op, args)
	if err != nil {
		return nil, err
	}
	if op.Request.Binary != nil {
		if runtime.Phase != "safe-output" || !runtime.AllowWrites {
			return nil, errors.New("binary connector requests require the authorized safe-output host")
		}
		content := runtime.binaryBody
		if content == nil {
			return nil, errors.New("binary connector content was not prepared by the authorized host")
		}
		request.Body = io.NopCloser(bytes.NewReader(content))
		request.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(content)), nil }
		request.ContentLength = int64(len(content))
		request.Header.Set("Content-Type", "application/octet-stream")
	}
	if err := p.authenticate(ctx, runtime, op.Credential, request); err != nil {
		return nil, err
	}
	client := boundedClient(runtime.Client)
	data, status, err := connectorHTTP(ctx, client, request, op.MaxResponseBytes, op.Retry)
	if err != nil {
		return nil, err
	}
	data, err = p.validateResponse(op, data, args)
	if err != nil {
		return nil, err
	}
	return &Result{Connector: p.spec.ID, Capability: path.Join(capability, name), Untrusted: true, StatusCode: status, Data: data}, nil
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
	if len(op.TrustedInputs) > 0 && p.trustedOperation != name {
		return Operation{}, errors.New("trusted connector inputs must be resolved by the safe-output host")
	}
	if err := runtime.checkOrigin(p.spec.Endpoints[op.Request.Endpoint]); err != nil {
		return Operation{}, err
	}
	if _, err := p.validateLookupArguments(op, args); err != nil {
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
		values, err := resolveValues(op.Request.Query, args)
		if err != nil {
			return nil, err
		}
		for key, value := range values {
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
	for name, value := range op.Request.Headers {
		request.Header.Set(name, value)
	}
	return request, nil
}

func resolveValues(values map[string]Value, args map[string]any) (map[string]any, error) {
	result := map[string]any{}
	size := 2
	for key, value := range values {
		resolved, present, err := resolveValue(value, args)
		if err != nil {
			return nil, err
		}
		if !present {
			continue
		}
		encoded, err := json.Marshal(resolved)
		if err != nil {
			return nil, fmt.Errorf("encode structured connector value: %w", err)
		}
		size += len(encoded) + len(key)*6 + 4
		if size > MaxDocumentBytes {
			return nil, errors.New("structured object exceeds the JSON byte bound")
		}
		result[key] = resolved
	}
	return result, nil
}

func requestBody(request Request, args map[string]any) ([]byte, string, error) {
	var body any
	contentType := "application/json"
	switch {
	case request.GraphQL != "":
		variables, err := resolveValues(request.Query, args)
		if err != nil {
			return nil, "", err
		}
		body = map[string]any{"query": request.GraphQL, "variables": variables}
	case len(request.Patch)+len(request.AdditionalPatch) != 0:
		all := append(slices.Clone(request.Patch), request.AdditionalPatch...)
		patches := make([]map[string]any, 0, len(all))
		for _, patch := range all {
			value, present, err := resolveValue(patch.Value, args)
			if err != nil {
				return nil, "", err
			}
			if !present {
				return nil, "", errors.New("JSON Patch value is missing")
			}
			patches = append(patches, map[string]any{"op": patch.Op, "path": patch.Path, "value": value})
		}
		body, contentType = patches, "application/json-patch+json"
	case len(request.Body) != 0:
		resolved, err := resolveValues(request.Body, args)
		if err != nil {
			return nil, "", err
		}
		body = resolved
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
