# Experimental AW connectors

Declare connectors directly under `connectors.<id>` in ordinary AW Markdown.
There is no required connection, definition, binding, or versioned authoring file.
Provider declarations belong in `.github/aw/connectors/<provider>.md`.
`ConnectorDeclaration` in `pkg/workflow/connector_declaration.go` extends the
existing frontmatter; it reuses this package's typed, bounded operation primitives.

Consumers reuse normal `imports` / `uses` / `with` syntax:

```yaml
imports:
  - uses: .github/aw/connectors/example.md
    with:
      issue: ISSUE-1
on: workflow_dispatch
tools:
  tracker: [get-issue]
safe-outputs:
  tracker-add-comment:
```

Declarations use AW capability sections and `triggers` for event definitions. Capability maps define APIs;
the root workflow selects them. A minimal direct or imported declaration is:

```yaml
connectors:
  tracker:
    endpoints:
      api: https://api.example.com
    credentials:
      reader:
        phase: agent
        header: Authorization
        scheme: bearer
        scopes: ["issues:read"]
        secret: EXAMPLE_READ_TOKEN
    resources:
      issues: [ISSUE-1]
    tools:
      get-issue:
        tool: get-issue
        credential: reader
        inputs:
          issue: {type: string, required: true, maxLength: 80}
        resources: {issue: issues}
        request:
          endpoint: api
          method: GET
          path: /issues/{issue}
          pathInputs: {issue: issue}
        maxResponseBytes: 65536
```

`triggers`, `tools`, `mcp-servers`, and `safe-outputs` inside the declaration
are maps of trigger, REST read-tool, HTTP MCP, and effect definitions.
Importing or declaring them grants nothing. Root workflow selections use
`tools.<alias>`
and qualified `on["<alias>-<trigger>"]` and
`safe-outputs["<alias>-<operation>"]` selections. Trigger and effect entries
accept null or `{}`. Ambiguous prefixes and built-in GitHub name collisions
are rejected; unqualified built-ins are never redirected to a connector.

Credentials contain secret **names**, never values. Declaration `resources`
contains exact resource allowlists. Both can use ordinary `import-schema`
parameters with normal defaults and `with` substitution; imported configuration
is trusted, reviewed repository code. Agent inputs cannot override endpoints,
secret references, or grants. Only selected phases receive credentials.
Duplicate registrations and imported capability selections are rejected.

There is no public connector `context` field, recipe, or context credential.
Gateway envelopes contain IDs/canonical URLs, event types, and matched filters,
not resource bodies. Selected read APIs retrieve content after origin, shape,
and scope validation; never attach credentials to event-supplied arbitrary URLs.
Manual examples name approved IDs, not an automatic hydration pipeline.

`ConnectorSpec`, `Connection`, and `Binding` remain internal frozen-plan types
at version `github.com/gh-aw/connectors/v1`. The compiler constructs them from
AW declarations; `NewService` copies and validates their content without file
I/O. Their existing YAML/JSON fixtures and external-file prototype APIs remain
for runtime compatibility testing, not as the recommended authoring interface.
Run `go test ./pkg/connectors -count=1` for the runtime suite.

Input names accept identifiers including native MCP camelCase and snake_case;
string inputs require an explicit `maxLength`. Operation `resources` maps input
names to the declaration's resource allowlists. Required resource inputs must
match those allowlists exactly.

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

AW credentials have exactly one phase: `agent` or `safe-output`. The internal
frozen wire format retains a legacy `context` phase for compatibility fixtures;
it is not part of the Markdown declaration interface.
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

`LoadService(repositoryRoot, definitionPath, connectionPath)` loads one consistent
service registration without selecting or executing anything. `Service.Select`
accepts independently optional capability lists. Only selected parts need
connection credential references and resource grants.

`Lower(repositoryRoot, binding)` remains the standalone binding API. It freezes
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

`Plan.ForPhase` prunes unselected capabilities, endpoints, credentials and grants
before distribution. `Plan.Encode` and `DecodePlan` carry content-addressed
snapshots as base64 JSON, limited to 65,536 encoded bytes per phase. Base64 keeps
definition data out of Actions expression evaluation. A digest is not a
signature: only the trusted compiler/lock file may supply runtime plans.

`Plan.CallMCP` calls a real remote SDK endpoint under local tool/argument gates.
`Plan.MCPServer(runtime)` registers selected REST-backed and remote MCP tools
using the standard MCP SDK, for a trusted host adapter behind the existing
gateway. The workflow compiler recognizes the centralized service grammar and
lowers supported selections to that adapter, pre-agent context reads and existing
staged safe-output handlers. The CLI's `connectors serve` and `connectors invoke`
commands host frozen, single-phase plans, never mutable source files.
Context inputs are literal typed arguments; context responses remain untrusted.
Safe-output credentials are never distributed to the agent host. Trial and
staged compilation use credential-free intent validation instead of writes.
Existing dry-run restrictions on custom MCP hosts and effect scripts still
apply; a rejected dry-run declaration does not execute.
Connector effects are not supported for work-queue participants because separate
effect verification is not implemented. Existing built-in tools, safe outputs
and gateway security behavior are unchanged. No receiver is installed and custom
trigger selection fails compilation rather than inventing dispatch provenance.
