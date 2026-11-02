package connectors

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// CallMCP forwards only selected, exact allowlisted remote tools after enforcing
// argument and resource constraints locally. Remote readOnly annotations are
// advisory; operators must approve tools and provision least-privilege tokens.
func (p *Plan) CallMCP(ctx context.Context, runtime Runtime, registration, tool string, args map[string]any) (*mcp.CallToolResult, error) {
	server, err := p.authorizeMCP(runtime, registration, tool, args)
	if err != nil {
		return nil, err
	}
	client := boundedClient(runtime.Client)
	transport := client.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	client.Transport = &mcpTransport{base: transport, plan: p, runtime: runtime, server: server}
	ctx, cancel := context.WithTimeout(ctx, client.Timeout)
	defer cancel()
	mcpClient := mcp.NewClient(&mcp.Implementation{Name: "gh-aw-connector", Version: "v1"}, nil)
	session, err := mcpClient.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint: server.URL, HTTPClient: client, MaxRetries: -1,
		DisableStandaloneSSE: true, MaxEventSize: server.MaxResponseBytes,
	}, nil)
	if err != nil {
		return nil, errors.New("connector HTTP MCP connection failed; no credential fallback performed")
	}
	defer func() {
		if err := session.Close(); err != nil {
			log.Printf("close connector MCP session: %v", err)
		}
	}()
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		return nil, errors.New("connector HTTP MCP tool request failed")
	}
	if result == nil || result.IsError || result.NeedsInput() {
		return nil, errors.New("connector HTTP MCP tool returned an error or requires unsupported interactive input")
	}
	encoded, err := json.Marshal(result)
	if err != nil || len(encoded) > server.MaxResponseBytes {
		return nil, errors.New("connector HTTP MCP result exceeds its byte bound")
	}
	return result, nil
}

func (p *Plan) authorizeMCP(runtime Runtime, registration, tool string, args map[string]any) (HTTPMCP, error) {
	if runtime.Phase != "agent" || !slices.Contains(p.binding.HTTPMCP, registration) {
		return HTTPMCP{}, errors.New("HTTP MCP registration is not selected or runtime phase is not agent")
	}
	server, ok := p.spec.HTTPMCP[registration]
	if !ok {
		return HTTPMCP{}, errors.New("unknown HTTP MCP registration")
	}
	constraints, ok := server.Tools[tool]
	if !ok {
		return HTTPMCP{}, errors.New("HTTP MCP tool is not in the exact allowlist")
	}
	if err := runtime.checkOrigin(server.URL); err != nil {
		return HTTPMCP{}, err
	}
	if err := validateArguments(constraints.Inputs, constraints.Resources, p.connection.Resources, args); err != nil {
		return HTTPMCP{}, err
	}
	return server, nil
}

type mcpTransport struct {
	base    http.RoundTripper
	plan    *Plan
	runtime Runtime
	server  HTTPMCP
}

func (transport *mcpTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	// No server-selected redirect, session URL or origin may receive credentials.
	if request.URL.String() != transport.server.URL {
		return nil, errors.New("HTTP MCP transport cannot change its fixed endpoint")
	}
	request = request.Clone(request.Context())
	request.Header = request.Header.Clone()
	for name, value := range transport.server.Headers {
		request.Header.Set(name, value)
	}
	if err := transport.plan.authenticate(request.Context(), transport.runtime, transport.server.Credential, request); err != nil {
		return nil, err
	}
	response, err := transport.base.RoundTrip(request)
	if err != nil {
		return nil, errors.New("HTTP MCP transport failed")
	}
	response.Body = http.MaxBytesReader(nil, response.Body, int64(transport.server.MaxResponseBytes))
	return response, nil
}

// MCPServer exposes constrained REST read tools and real HTTP MCP proxies using
// the same MCP protocol consumed by the existing gateway. Run it behind the
// gateway using a trusted host adapter; never render remote registrations
// directly into engine config, which would bypass local argument checks.
func (p *Plan) MCPServer(runtime Runtime) (*mcp.Server, error) {
	if runtime.Phase != "agent" {
		return nil, errors.New("connector MCP server requires an agent-phase runtime")
	}
	server := mcp.NewServer(&mcp.Implementation{Name: "gh-aw-connectors", Version: "v1"}, nil)
	for _, name := range p.binding.MCP {
		tool := p.spec.MCP[name]
		server.AddTool(&mcp.Tool{Name: tool.Tool, InputSchema: inputSchema(tool.Inputs)}, p.readToolHandler(runtime, name))
	}
	for _, registration := range p.binding.HTTPMCP {
		for tool, constraints := range p.spec.HTTPMCP[registration].Tools {
			name := registration + "__" + tool
			server.AddTool(&mcp.Tool{Name: name, InputSchema: inputSchema(constraints.Inputs)}, p.remoteToolHandler(runtime, registration, tool))
		}
	}
	return server, nil
}

func (p *Plan) readToolHandler(runtime Runtime, name string) mcp.ToolHandler {
	return func(ctx context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, err := decodeToolArguments(request)
		if err != nil {
			return toolError(err), nil
		}
		result, err := p.Execute(ctx, runtime, "mcp", name, args)
		if err != nil {
			return toolError(err), nil
		}
		data, err := json.Marshal(result)
		if err != nil {
			return toolError(err), nil
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(data)}}}, nil
	}
}

func (p *Plan) remoteToolHandler(runtime Runtime, registration, tool string) mcp.ToolHandler {
	return func(ctx context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, err := decodeToolArguments(request)
		if err != nil {
			return toolError(err), nil
		}
		result, err := p.CallMCP(ctx, runtime, registration, tool, args)
		if err != nil {
			return toolError(err), nil
		}
		// Treat all provider content as untrusted, including remote annotations.
		data, err := json.Marshal(map[string]any{"untrusted": true, "connector": p.spec.ID, "result": result})
		if err != nil {
			return toolError(err), nil
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(data)}}}, nil
	}
}

func decodeToolArguments(request *mcp.CallToolRequest) (map[string]any, error) {
	if request.Params == nil || len(request.Params.Arguments) > MaxDocumentBytes {
		return nil, errors.New("connector MCP arguments are missing or exceed the byte bound")
	}
	var args map[string]any
	if err := decodeJSON(request.Params.Arguments, &args); err != nil || args == nil {
		return nil, errors.New("connector MCP arguments must be a JSON object")
	}
	return args, nil
}

func toolError(err error) *mcp.CallToolResult {
	return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}}}
}

func inputSchema(inputs map[string]Input) map[string]any {
	properties := map[string]any{}
	required := []string{}
	for name, input := range inputs {
		property := map[string]any{"type": input.Type}
		if input.Required {
			required = append(required, name)
		}
		if input.Type == "string" {
			property["maxLength"] = input.MaxLength
		}
		if len(input.Enum) != 0 {
			property["enum"] = input.Enum
		}
		if input.Pattern != "" {
			property["pattern"] = input.Pattern
		}
		if input.Minimum != nil {
			property["minimum"] = *input.Minimum
		}
		if input.Maximum != nil {
			property["maximum"] = *input.Maximum
		}
		properties[name] = property
	}
	slices.Sort(required)
	return map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}
}

func validateToolNames(spec ConnectorSpec, binding Binding) error {
	names := map[string]struct{}{}
	for _, name := range binding.MCP {
		tool := spec.MCP[name].Tool
		if _, exists := names[tool]; exists {
			return fmt.Errorf("connector MCP tool name collision %q", tool)
		}
		names[tool] = struct{}{}
	}
	for _, name := range binding.HTTPMCP {
		for tool := range spec.HTTPMCP[name].Tools {
			full := name + "__" + tool
			_, exists := names[full]
			if exists || len(full) > 128 || strings.ContainsAny(full, "*?") {
				return fmt.Errorf("connector MCP tool name collision or excessive length %q", full)
			}
			names[full] = struct{}{}
		}
	}
	return nil
}
