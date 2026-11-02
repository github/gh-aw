# Experimental connector contract v1

This is a configuration-interface prototype, not a deployed integration.
`ConnectorSpec` in `spec.go` is the authoritative typed schema; `Schema(kind)`
exports structural JSON Schema for all three document kinds. `LoadSpec` parses
YAML 1.2 or JSON, rejects unknown fields, and validates security invariants.
The version string is `github.com/gh-aw/connectors/v1`.

Provider definitions belong in `.github/aw/connectors/<provider>.yaml`.
Provider test fixtures belong in
`pkg/connectors/testdata/providers/<provider>/spec.yaml`. Use the complete
four-capability `testdata/example.yaml` as the starting point. Do not fork
the schema, shared parser, or test harness.

Run `go test ./pkg/connectors -run 'TestConnectorSpec|TestProviderFixtures' -count=1`.
To select one provider, use `-run 'TestProviderFixtures/<provider>$'`.

Definition, connection, and workflow selection are separate documents:

```yaml
apiVersion: github.com/gh-aw/connectors/v1
kind: ConnectorConnection
connector: example.com/issues
credentials:
  reader: {secret: EXAMPLE_READ_TOKEN}
  writer: {secret: EXAMPLE_WRITE_TOKEN}
  context-reader: {secret: EXAMPLE_CONTEXT_TOKEN}
resources:
  issues: [ISSUE-1]
```

```yaml
apiVersion: github.com/gh-aw/connectors/v1
kind: ConnectorBinding
definition: .github/aw/connectors/example.yaml
connection: .github/aw/connections/example.yaml
triggers: [issue-created]
mcp: [get-issue]
safeOutputs: [add-comment]
context: [issue-details]
```

Definition/connection paths are relative to the repository root, not the binding
file. Connections contain secret **names**, never values. Endpoints are fixed
HTTPS origins in the definition and cannot be overridden by connections or
agent inputs. Input names accept identifiers including native MCP camelCase
and snake_case; string inputs require an
explicit `maxLength`. `resources` maps input names to connection resource
allowlists. Required resource inputs must match those allowlists exactly.

Requests have a fixed method, endpoint and path. `{name}` path segments are
bound through `pathInputs`; `query` and `body` values are `{input: name}` or
`{literal: <JSON value>}`. Structured `object: {field: <Value>}` and
`array: [<Value>]` compose typed bindings recursively, with a maximum depth of
16 and bounded resolved JSON size. Each Value has exactly one source.
REST query parameters remain primitive scalars; GraphQL variables, JSON
bodies and JSON Patch values can be structured. There is no shell, JavaScript, Actions expression,
remote import or expression language. `graphql` is a fixed document; `query`
then supplies GraphQL variables, not URL parameters. `patch` is an array of
`{op: add|replace|test, path: /fixed/pointer, value: {input: name}}` (or
`value: {literal: ...}`) on safe-output POST/PATCH requests, serialized as
`application/json-patch+json`. It cannot coexist with another body.
Multipart, pagination, arbitrary headers, OAuth acquisition, automatic version
preflights and dynamic endpoints are unsupported.

String input references may use `encoding: jira-adf` or
`encoding: confluence-storage`. These fixed trusted encoders produce a real ADF
document with text/hard-break nodes, or escaped storage markup with line breaks,
respectively. They do not evaluate markup, accept raw HTML, infer rich formatting,
or run provider-supplied code:

```yaml
body:
  fields:
    object:
      summary: {input: title}
      description: {input: text, encoding: jira-adf}
```

`Input.pattern` accepts bounded RE2 patterns. `Operation.responseChecks` maps
JSON Pointers to required literal values (e.g. `/ok: true`); `responseRequired`
lists required non-null/nonempty evidence. `responseResources` maps JSON
Pointers to connection resource allowlists. `project` maps output field names
to JSON Pointers and excludes all other response fields. `responseTypes` maps
pointers to primitive `string`, `integer`, `number`, or `boolean` types;
use it to reject objects accidentally projected as metadata. `responseInputs`
maps pointers to required input names and enforces exact returned-resource
equality with the request, not just membership in a broad connection allowlist.
GraphQL `errors` are
always failures, including HTTP 200 responses. The runtime never retries writes.

Each credential has exactly one phase: `agent`, `context`, or `safe-output`.
`mcp` entries are exact allowlisted REST-backed read tools. `httpMcp` entries
register real streamable-HTTP MCP servers:

```yaml
httpMcp:
  provider:
    url: https://mcp.example.com/mcp
    credential: reader
    readOnly: true
    headers: {X-MCP-Readonly: "true"}
    maxResponseBytes: 65536
    tools:
      get_issue:
        inputs:
          issueId: {type: string, required: true, maxLength: 80}
        resources: {issueId: issues}
```

Select them with `ConnectorBinding.httpMcp: [provider]`. The adapter exposes
`provider__get_issue` and validates exact tools, primitive argument schemas and
resource constraints before forwarding. Only fixed `X-MCP-*` headers are
supported; credentials use the phase-specific reference. `readOnly` is an
operator declaration, not proof: approve tools and provision read-scoped
credentials. Never pass registrations directly to engines: that bypasses the
adapter's argument checks. GraphQL read documents must be queries,
never mutations. Context results must be labeled untrusted. Safe outputs are
typed requests to be validated again outside the agent credential boundary;
declaring an operation never authorizes or records a mutation.

Triggers declare native event names, exact string comparisons at JSON Pointers,
an approved-verifier profile identifier, and evidence-based JSON Pointer
normalization. A profile string does **not** approve a verifier. No receiver
ships here, and the existing gateway does not accept custom webhook sources.
Source enrollment, provenance, admission and replay protection must not be
bypassed. Validation and fixture projection are not operational activation.
`Trigger.resources` maps evidence JSON Pointers to connection allowlists.
Native numeric IDs are compared losslessly to their canonical JSON numeric
spelling in string allowlists. `Trigger.types` uses the same primitive type
guards as `responseTypes`; require `string` for string-only native identifiers
such as Slack timestamps. Neither type guards nor projection replace verifier,
replay, tenant enrollment, bot/self filtering or event-bound write authority.

## Host integration

`Lower(repositoryRoot, binding)` loads confined repository-local files, freezes
all three documents and returns a content-addressed `Plan`. `Plan.Digest` changes
when definitions, secret references, resources or selections change. No remote
definition fetches or credential resolution occur during lowering.

`Plan.ProjectEvent(name, nativeEvent, payload)` exercises matching, resource
constraints and projection, explicitly returning `operational: false` and
`untrusted: true`. `Plan.ActivateTriggers()` fails with `ErrIngressUnavailable`.
It cannot be used to bypass existing gateway admission/provenance checks.

`Plan.Execute(ctx, runtime, capability, name, args)` accepts `mcp`, `context` or
`safe-output`. `Runtime` requires a matching phase (`agent` for MCP), an
operator-owned `AllowedOrigins`, and `ResolveSecret(ctx, phase, secretName)`.
Safe-output writes additionally require `AllowWrites: true`. All HTTP responses
are bounded, all redirects refused, and results labeled untrusted.

`Plan.PrepareSafeOutput(name, args)` returns a credential-free
`SafeOutputRequest`, not an effect. Host safe-output handling passes it to
`Plan.ExecuteSafeOutput` in the separate safe-output phase, which checks the
plan digest and validates arguments again. It does not record staged requests
as successful mutations.

`Plan.CallMCP` calls a real remote SDK endpoint under local tool/argument gates.
`Plan.MCPServer(runtime)` registers selected REST-backed and remote MCP tools
using the standard MCP SDK, for a trusted host adapter behind the existing
gateway. This package does not add workflow frontmatter syntax, spawn a server,
or install a receiver. Existing imports, built-in tools, safe outputs and gateway
security behavior are unchanged. Compiler job scheduling and operational
enrollment remain explicit integration work, not implied by a valid declaration.
