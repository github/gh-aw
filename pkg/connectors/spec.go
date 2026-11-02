// Package connectors implements the experimental, declaration-only connector
// contract. Validation does not enroll a webhook source or authorize execution.
package connectors

const APIVersion = "github.com/gh-aw/connectors/v1"

// ConnectorSpec contains immutable provider behavior, never connection secrets.
type ConnectorSpec struct {
	APIVersion  string                `json:"apiVersion"`
	Kind        string                `json:"kind"`
	ID          string                `json:"id"`
	Endpoints   map[string]string     `json:"endpoints"`
	Credentials map[string]Credential `json:"credentials"`
	Triggers    map[string]Trigger    `json:"triggers,omitempty"`
	MCP         map[string]ReadTool   `json:"mcp,omitempty"`
	HTTPMCP     map[string]HTTPMCP    `json:"httpMcp,omitempty"`
	SafeOutputs map[string]Operation  `json:"safeOutputs,omitempty"`
	Context     map[string]Operation  `json:"context,omitempty"`
}

type Credential struct {
	Phase  string   `json:"phase"`
	Header string   `json:"header"`
	Scheme string   `json:"scheme"`
	Scopes []string `json:"scopes"`
}

// Input supports bounded primitive values; objects, expressions, and defaults
// are deliberately excluded from this first contract.
type Input struct {
	Type      string   `json:"type"`
	Required  bool     `json:"required,omitempty"`
	Enum      []string `json:"enum,omitempty"`
	MaxLength int      `json:"maxLength,omitempty"`
	Minimum   *float64 `json:"minimum,omitempty"`
	Maximum   *float64 `json:"maximum,omitempty"`
	Pattern   string   `json:"pattern,omitempty"`
}

// Value is exactly one literal, input reference, structured object or array.
// Input references are data, not textual template expansion.
type Value struct {
	Input    string           `json:"input,omitempty"`
	Literal  any              `json:"literal,omitempty"`
	Object   map[string]Value `json:"object,omitzero"`
	Array    []Value          `json:"array,omitzero"`
	Encoding string           `json:"encoding,omitempty"`
}

type Request struct {
	Endpoint   string            `json:"endpoint"`
	Method     string            `json:"method"`
	Path       string            `json:"path"`
	PathInputs map[string]string `json:"pathInputs,omitempty"`
	Query      map[string]Value  `json:"query,omitempty"`
	Body       map[string]Value  `json:"body,omitempty"`
	GraphQL    string            `json:"graphql,omitempty"`
	Patch      []Patch           `json:"patch,omitempty"`
}

type Patch struct {
	Op    string `json:"op"`
	Path  string `json:"path"`
	Value Value  `json:"value"`
}

type Operation struct {
	Credential        string                  `json:"credential"`
	Inputs            map[string]Input        `json:"inputs,omitempty"`
	Resources         map[string]string       `json:"resources,omitempty"`
	Request           Request                 `json:"request"`
	MaxResponseBytes  int                     `json:"maxResponseBytes"`
	ResponseChecks    map[string]any          `json:"responseChecks,omitempty"`
	ResponseResources map[string]string       `json:"responseResources,omitempty"`
	ResponseRequired  []string                `json:"responseRequired,omitempty"`
	ResponseTypes     map[string]string       `json:"responseTypes,omitempty"`
	ResponseInputs    map[string]string       `json:"responseInputs,omitempty"`
	Project           map[string]string       `json:"project,omitempty"`
	Creates           *CreatedResource        `json:"creates,omitempty"`
	CreatedResources  map[string]string       `json:"createdResources,omitempty"`
	TrustedInputs     map[string]TrustedInput `json:"trustedInputs,omitempty"`
	Preflight         *TrustedPreflight       `json:"preflight,omitempty"`
	ResponseContains  map[string]string       `json:"responseContains,omitempty"`
}

type TrustedInput struct {
	Secret string `json:"secret,omitempty"`
	Value  string `json:"value,omitempty"`
}

// TrustedPreflight is a fixed read in the safe-output credential phase. Its
// arguments come exclusively from authored trusted inputs, never agent input.
type TrustedPreflight struct {
	Inputs           map[string]Input  `json:"inputs"`
	Request          Request           `json:"request"`
	MaxResponseBytes int               `json:"maxResponseBytes"`
	ResponseRequired []string          `json:"responseRequired,omitempty"`
	ResponseTypes    map[string]string `json:"responseTypes,omitempty"`
	ResponseInputs   map[string]string `json:"responseInputs,omitempty"`
	ResponseContains map[string]string `json:"responseContains,omitempty"`
	ResponseChecks   map[string]any    `json:"responseChecks,omitempty"`
	Project          map[string]string `json:"project"`
}

// CreatedResource explicitly authorizes binding a validated create response to
// a temporary reference within one trusted safe-output execution.
type CreatedResource struct {
	Resource    string `json:"resource"`
	Value       string `json:"value"`
	Scope       string `json:"scope"`
	TemporaryID string `json:"temporaryId"`
	ScopePrefix string `json:"scopePrefix,omitempty"`
}

type ReadTool struct {
	Operation
	Tool string `json:"tool"`
}

// HTTPMCP registers a real streamable-HTTP MCP endpoint, not a REST adapter.
// The host must enforce Inputs/Resources before forwarding any tools/call.
type HTTPMCP struct {
	URL              string             `json:"url"`
	Credential       string             `json:"credential"`
	Headers          map[string]string  `json:"headers,omitempty"`
	ReadOnly         bool               `json:"readOnly"`
	Tools            map[string]MCPTool `json:"tools"`
	MaxResponseBytes int                `json:"maxResponseBytes"`
}

type MCPTool struct {
	Inputs    map[string]Input  `json:"inputs"`
	Resources map[string]string `json:"resources,omitempty"`
}

// Trigger describes native equality matching and JSON Pointer projection.
// Verification profiles are identifiers for a future approved receiver, not
// executable verification code and not evidence of operational enrollment.
type Trigger struct {
	VerificationProfile string            `json:"verificationProfile"`
	NativeEvents        []string          `json:"nativeEvents"`
	Match               map[string]string `json:"match"`
	Normalize           map[string]string `json:"normalize"`
	Resources           map[string]string `json:"resources,omitempty"`
	Types               map[string]string `json:"types,omitempty"`
}

type SecretRef struct {
	Secret string `json:"secret"`
}

type Connection struct {
	APIVersion  string               `json:"apiVersion"`
	Kind        string               `json:"kind"`
	Connector   string               `json:"connector"`
	Credentials map[string]SecretRef `json:"credentials"`
	Resources   map[string][]string  `json:"resources"`
}

// Binding refers only to repository-local files, frozen during lowering.
type Binding struct {
	APIVersion  string   `json:"apiVersion"`
	Kind        string   `json:"kind"`
	Definition  string   `json:"definition"`
	Connection  string   `json:"connection"`
	Triggers    []string `json:"triggers,omitempty"`
	MCP         []string `json:"mcp,omitempty"`
	HTTPMCP     []string `json:"httpMcp,omitempty"`
	SafeOutputs []string `json:"safeOutputs,omitempty"`
	Context     []string `json:"context,omitempty"`
}
