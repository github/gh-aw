# Experimental connector contract v1

This is a configuration-interface prototype, not a deployed integration.
`ConnectorSpec` in `spec.go` is the authoritative typed schema; `LoadSpec` parses
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
agent inputs. Input names use lowercase kebab-case; string inputs require an
explicit `maxLength`. `resources` maps input names to connection resource
allowlists. Required resource inputs must match those allowlists exactly.

Requests have a fixed method, endpoint and path. `{name}` path segments are
bound through `pathInputs`; `query` and `body` values are `{input: name}` or
`{literal: <JSON value>}`. There is no shell, JavaScript, Actions expression,
remote import or expression language. `graphql` is a fixed document; `query`
then supplies GraphQL variables, not URL parameters. JSON Patch, multipart,
pagination, arbitrary headers, OAuth acquisition and dynamic endpoints are
unsupported in v1.

Each credential has exactly one phase: `agent`, `context`, or `safe-output`.
MCP entries are exact allowlisted tool names backed by bounded read requests,
not arbitrary remote MCP server access. GraphQL read documents must be queries,
never mutations. Context results must be labeled untrusted. Safe outputs are
typed requests to be validated again outside the agent credential boundary;
declaring an operation never authorizes or records a mutation.

Triggers declare native event names, exact string comparisons at JSON Pointers,
an approved-verifier profile identifier, and evidence-based JSON Pointer
normalization. A profile string does **not** approve a verifier. No receiver
ships here, and the existing gateway does not accept custom webhook sources.
Source enrollment, provenance, admission and replay protection must not be
bypassed. Validation and fixture projection are not operational activation.
