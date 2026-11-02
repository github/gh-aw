package connectors

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strconv"
	"strings"
)

// MaxEncodedPlanBytes keeps a compiled plan below the per-environment-variable
// limit on Linux runners. Base64 also prevents Actions expression evaluation.
const MaxEncodedPlanBytes = 65536

type compiledPlan struct {
	Digest     string        `json:"digest"`
	Spec       ConnectorSpec `json:"spec"`
	Connection Connection    `json:"connection"`
	Binding    Binding       `json:"binding"`
}

// Encode freezes content into the lock file rather than loading mutable
// repository definitions on the runner. It contains secret names, never values.
func (p *Plan) Encode() (string, error) {
	data, err := json.Marshal(compiledPlan{p.Digest, p.spec, p.connection, p.binding})
	if err != nil {
		return "", fmt.Errorf("encode connector plan: %w", err)
	}
	encoded := base64.StdEncoding.EncodeToString(data)
	if len(encoded) > MaxEncodedPlanBytes {
		return "", errors.New("compiled connector plan exceeds 65536 encoded bytes; select fewer capabilities")
	}
	return encoded, nil
}

// DecodePlan verifies a compiler-owned snapshot. A digest detects corruption;
// it is not a signature and does not authorize an untrusted plan.
func DecodePlan(encoded string) (*Plan, error) {
	if len(encoded) > MaxEncodedPlanBytes {
		return nil, errors.New("compiled connector plan exceeds its byte bound")
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, errors.New("compiled connector plan must be base64 JSON")
	}
	var document compiledPlan
	if err := decodeStrictJSON(data, &document); err != nil {
		return nil, err
	}
	if err := document.Spec.Validate(); err != nil {
		return nil, err
	}
	if err := validateConnection(document.Spec, document.Connection); err != nil {
		return nil, err
	}
	if document.Binding.APIVersion != APIVersion || document.Binding.Kind != "ConnectorBinding" {
		return nil, errors.New("compiled connector plan requires a versioned binding")
	}
	if err := validateSelection(document.Spec, document.Binding, document.Connection); err != nil {
		return nil, err
	}
	plan, err := freezePlan(document.Spec, document.Connection, document.Binding)
	if err != nil {
		return nil, err
	}
	if plan.Digest != document.Digest {
		return nil, errors.New("compiled connector plan digest mismatch")
	}
	return plan, nil
}

// ForPhase strips all other capabilities, endpoints and credential references.
// The compiler distributes separate snapshots to the three execution hosts.
func (p *Plan) ForPhase(phase string) (*Plan, error) { //nolint:largefunc // Keep capability, origin, grant and credential pruning together at this authority boundary.
	spec := ConnectorSpec{
		APIVersion: APIVersion, Kind: "ConnectorSpec", ID: p.spec.ID,
		Endpoints: map[string]string{}, Credentials: map[string]Credential{},
		MCP: map[string]ReadTool{}, HTTPMCP: map[string]HTTPMCP{},
		Context: map[string]Operation{}, SafeOutputs: map[string]Operation{},
	}
	binding := Binding{APIVersion: APIVersion, Kind: "ConnectorBinding", Definition: p.binding.Definition, Connection: p.binding.Connection}
	connection := Connection{
		APIVersion: APIVersion, Kind: "ConnectorConnection", Connector: p.spec.ID,
		Credentials: map[string]SecretRef{}, Resources: map[string][]string{},
	}
	addCredential := func(name string) {
		spec.Credentials[name] = p.spec.Credentials[name]
		connection.Credentials[name] = p.connection.Credentials[name]
	}
	addResources := func(resources map[string]string) {
		for _, name := range resources {
			if len(p.connection.Resources[name]) > 0 {
				connection.Resources[name] = p.connection.Resources[name]
			}
		}
	}
	addOperation := func(op Operation) {
		addCredential(op.Credential)
		spec.Endpoints[op.Request.Endpoint] = p.spec.Endpoints[op.Request.Endpoint]
		addResources(op.Resources)
		addResources(op.ResponseResources)
	}
	switch phase {
	case "agent":
		binding.MCP, binding.HTTPMCP = p.binding.MCP, p.binding.HTTPMCP
		for _, name := range binding.MCP {
			tool := p.spec.MCP[name]
			spec.MCP[name] = tool
			addOperation(tool.Operation)
		}
		for _, name := range binding.HTTPMCP {
			server := p.spec.HTTPMCP[name]
			spec.HTTPMCP[name] = server
			addCredential(server.Credential)
			for _, tool := range server.Tools {
				addResources(tool.Resources)
			}
		}
		// ConnectorSpec requires at least one endpoint even for HTTP-MCP-only
		// services. Keep only the selected remote origins, not unused APIs.
		index := 0
		for _, name := range binding.HTTPMCP {
			server := spec.HTTPMCP[name]
			u, err := url.Parse(server.URL)
			if err != nil {
				return nil, err
			}
			for {
				key := "remote-mcp-" + strconv.Itoa(index)
				index++
				if _, exists := spec.Endpoints[key]; !exists {
					spec.Endpoints[key] = u.Scheme + "://" + u.Host
					break
				}
			}
		}
	case "context":
		binding.Context = p.binding.Context
		for _, name := range binding.Context {
			op := p.spec.Context[name]
			spec.Context[name] = op
			addOperation(op)
		}
	case "safe-output":
		binding.SafeOutputs = p.binding.SafeOutputs
		for _, name := range binding.SafeOutputs {
			op := p.spec.SafeOutputs[name]
			op.CreatedResources = maps.Clone(op.CreatedResources)
			for input, creator := range op.CreatedResources {
				if !slices.Contains(binding.SafeOutputs, creator) {
					delete(op.CreatedResources, input)
				}
			}
			spec.SafeOutputs[name] = op
			addOperation(op)
		}
	default:
		return nil, errors.New("connector phase must be agent, context or safe-output")
	}
	if err := spec.Validate(); err != nil {
		return nil, err
	}
	if err := validateSelection(spec, binding, connection); err != nil {
		return nil, err
	}
	return freezePlan(spec, connection, binding)
}

// Secrets and Origins describe only the already-selected, phase-restricted
// snapshot. Callers must never derive runtime authority from agent arguments.
func (p *Plan) Secrets() []string {
	names := []string{}
	for _, ref := range p.connection.Credentials {
		names = append(names, ref.Secret)
	}
	for _, name := range p.binding.SafeOutputs {
		for _, source := range p.spec.SafeOutputs[name].TrustedInputs {
			if source.Secret != "" {
				names = append(names, source.Secret)
			}
		}
	}
	slices.Sort(names)
	return slices.Compact(names)
}

func (p *Plan) Origins() []string {
	origins := []string{}
	for _, endpoint := range p.spec.Endpoints {
		origins = append(origins, strings.TrimSuffix(endpoint, "/"))
	}
	slices.Sort(origins)
	return slices.Compact(origins)
}

func (p *Plan) ToolNames() []string {
	names := []string{}
	for _, name := range p.binding.MCP {
		names = append(names, p.spec.MCP[name].Tool)
	}
	for _, name := range p.binding.HTTPMCP {
		for tool := range p.spec.HTTPMCP[name].Tools {
			names = append(names, name+"__"+tool)
		}
	}
	slices.Sort(names)
	return names
}

func (p *Plan) SafeOutputNames() []string {
	names := slices.Clone(p.binding.SafeOutputs)
	slices.Sort(names)
	return names
}

func (p *Plan) Inputs(capability, name string) (map[string]Input, error) {
	op, ok := selectedOperation(p.spec, capability, name)
	if !ok {
		return nil, fmt.Errorf("unknown connector %s capability %q", capability, name)
	}
	data, err := json.Marshal(op.Inputs)
	if err != nil {
		return nil, err
	}
	var inputs map[string]Input
	if err := decodeJSON(data, &inputs); err != nil {
		return nil, err
	}
	for name := range op.TrustedInputs {
		delete(inputs, name)
	}
	return inputs, nil
}

func (p *Plan) InputSchema(capability, name string) (map[string]any, error) {
	inputs, err := p.Inputs(capability, name)
	if err != nil {
		return nil, err
	}
	if capability == "safe-output" {
		for input := range p.spec.SafeOutputs[name].CreatedResources {
			field := inputs[input]
			if field.Pattern != "" {
				field.Pattern = "(?:" + field.Pattern + ")|(?:" + temporaryResourcePattern.String() + ")"
			}
			inputs[input] = field
		}
	}
	return inputSchema(inputs), nil
}

func (p *Plan) ValidateContext(name string, args map[string]any) error {
	if !slices.Contains(p.binding.Context, name) {
		return fmt.Errorf("context capability %q is not selected", name)
	}
	op := p.spec.Context[name]
	return validateArguments(op.Inputs, op.Resources, p.connection.Resources, args)
}
