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
	MinLength int      `json:"minLength,omitempty"`
	Sanitize  string   `json:"sanitize,omitempty"`
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
	Prefix   string           `json:"prefix,omitempty"`
}

type Request struct {
	Endpoint        string            `json:"endpoint"`
	Method          string            `json:"method"`
	Path            string            `json:"path"`
	PathInputs      map[string]string `json:"pathInputs,omitempty"`
	Query           map[string]Value  `json:"query,omitempty"`
	Body            map[string]Value  `json:"body,omitempty"`
	GraphQL         string            `json:"graphql,omitempty"`
	Patch           []Patch           `json:"patch,omitempty"`
	AdditionalPatch []Patch           `json:"additionalPatch,omitempty"`
	Binary          *BinaryRequest    `json:"binary,omitempty"`
	Headers         map[string]string `json:"headers,omitempty"`
}

type BinaryRequest struct {
	Input          string   `json:"input"`
	MaxBytes       int      `json:"maxBytes"`
	Extensions     []string `json:"extensions,omitempty"`
	RejectContains []string `json:"rejectContains,omitempty"`
}

type Patch struct {
	Op    string `json:"op"`
	Path  string `json:"path"`
	Value Value  `json:"value"`
}

type Operation struct {
	Credential             string                  `json:"credential"`
	Inputs                 map[string]Input        `json:"inputs,omitempty"`
	Resources              map[string]string       `json:"resources,omitempty"`
	Request                Request                 `json:"request"`
	MaxResponseBytes       int                     `json:"maxResponseBytes"`
	ResponseChecks         map[string]any          `json:"responseChecks,omitempty"`
	ResponseResources      map[string]string       `json:"responseResources,omitempty"`
	ResponseRequired       []string                `json:"responseRequired,omitempty"`
	ResponseTypes          map[string]string       `json:"responseTypes,omitempty"`
	ResponseInputs         map[string]string       `json:"responseInputs,omitempty"`
	ResponseResourceInputs map[string]string       `json:"responseResourceInputs,omitempty"`
	ResponseOptionalEmpty  []string                `json:"responseOptionalEmpty,omitempty"`
	Project                map[string]string       `json:"project,omitempty"`
	Creates                *CreatedResource        `json:"creates,omitempty"`
	CreatedResources       map[string]string       `json:"createdResources,omitempty"`
	TrustedInputs          map[string]TrustedInput `json:"trustedInputs,omitempty"`
	Preflight              *TrustedPreflight       `json:"preflight,omitempty"`
	ResponseContains       map[string]string       `json:"responseContains,omitempty"`
	ResponseLimits         map[string]int          `json:"responseLimits,omitempty"`
	ResponseAnyInputs      map[string][]string     `json:"responseAnyInputs,omitempty"`
	Lookups                []InputLookup           `json:"lookups,omitempty"`
	Retry                  *RetryPolicy            `json:"retry,omitempty"`
	Checks                 []TrustedPreflight      `json:"checks,omitempty"`
	ResponseWhen           *EventPredicate         `json:"responseWhen,omitempty"`
}

// RetryPolicy bounds explicitly declared transient HTTP-status retries.
type RetryPolicy struct {
	MaxAttempts int   `json:"maxAttempts"`
	BackoffMS   int   `json:"backoffMs"`
	RetryOn     []int `json:"retryOn"`
	Idempotent  bool  `json:"idempotent,omitempty"`
}

type TrustedInput struct {
	Secret string `json:"secret,omitempty"`
	Value  string `json:"value,omitempty"`
}

// TrustedPreflight is a fixed read in the safe-output credential phase. Its
// arguments come exclusively from authored trusted inputs, never agent input.
// InputLookup reuses its bounded read shape with separate exact alias grants.
type TrustedPreflight struct {
	Inputs                 map[string]Input    `json:"inputs"`
	Resources              map[string]string   `json:"resources,omitempty"`
	Request                Request             `json:"request"`
	MaxResponseBytes       int                 `json:"maxResponseBytes"`
	ResponseRequired       []string            `json:"responseRequired,omitempty"`
	ResponseTypes          map[string]string   `json:"responseTypes,omitempty"`
	ResponseInputs         map[string]string   `json:"responseInputs,omitempty"`
	ResponseResourceInputs map[string]string   `json:"responseResourceInputs,omitempty"`
	ResponseWhen           *EventPredicate     `json:"responseWhen,omitempty"`
	ResponseOptionalEmpty  []string            `json:"responseOptionalEmpty,omitempty"`
	ResponseContains       map[string]string   `json:"responseContains,omitempty"`
	ResponseChecks         map[string]any      `json:"responseChecks,omitempty"`
	ResponseResources      map[string]string   `json:"responseResources,omitempty"`
	ResponseLimits         map[string]int      `json:"responseLimits,omitempty"`
	ResponseAnyInputs      map[string][]string `json:"responseAnyInputs,omitempty"`
	Project                map[string]string   `json:"project,omitempty"`
}

// InputLookup replaces one approved alias with an independently granted
// canonical identity. An empty Input checks relationships after resolution.
type InputLookup struct {
	Input string `json:"input,omitempty"`
	TrustedPreflight
}

// CreatedResource explicitly authorizes binding a validated create response to
// a temporary reference within one trusted safe-output execution.
type CreatedResource struct {
	Resource    string `json:"resource"`
	Value       string `json:"value"`
	Scope       string `json:"scope"`
	TemporaryID string `json:"temporaryId"`
	ScopePrefix string `json:"scopePrefix,omitempty"`
	ValueType   string `json:"valueType,omitempty"`
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
	ResourceOrigins     []string               `json:"resourceOrigins,omitempty"`
	VerificationProfile string                 `json:"verificationProfile"`
	NativeEvents        []string               `json:"nativeEvents"`
	Match               map[string]string      `json:"match"`
	Normalize           map[string]string      `json:"normalize"`
	Resources           map[string]string      `json:"resources,omitempty"`
	Types               map[string]string      `json:"types,omitempty"`
	When                *EventPredicate        `json:"when,omitempty"`
	Filters             map[string]EventFilter `json:"filters,omitempty"`
	Variants            []EventVariant         `json:"variants,omitempty"`
	URLIdentity         string                 `json:"urlIdentity,omitempty"`
	URLPathPrefix       string                 `json:"urlPathPrefix,omitempty"`
}

// EventPredicate is a bounded, data-only rule evaluated on authenticated source
// evidence. Other always references the root; array items use relative Path.
type EventPredicate struct {
	All       []EventPredicate `json:"all,omitempty"`
	Any       []EventPredicate `json:"any,omitempty"`
	Path      string           `json:"path,omitempty"`
	Op        string           `json:"op,omitempty"`
	Value     any              `json:"value,omitempty"`
	Other     string           `json:"other,omitempty"`
	Fallback  string           `json:"fallback,omitempty"`
	Values    []string         `json:"values,omitempty"`
	Separator string           `json:"separator,omitempty"`
	Root      bool             `json:"root,omitempty"`
	Items     *EventPredicate  `json:"items,omitempty"`
}

type EventFilter struct {
	Path   string `json:"path"`
	Type   string `json:"type"`
	Values []any  `json:"values,omitempty"`
}

// Variants replace evidence pointers only after an explicit discriminator
// matches. They never add resource authority.
type EventVariant struct {
	When      EventPredicate    `json:"when"`
	Normalize map[string]string `json:"normalize"`
	Resources map[string]string `json:"resources"`
	Types     map[string]string `json:"types,omitempty"`
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
