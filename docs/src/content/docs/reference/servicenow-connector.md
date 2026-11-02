---
title: ServiceNow connector
description: ServiceNow incident and request integration boundaries, credentials, and deployment prerequisites.
---

The ServiceNow provider targets explicitly approved incident and request records.
Configuration validation is not deployment: accepting a manifest does not install
instance automation, activate an MCP server, or authorize record mutations.

## Prototype definition

The definition is `.github/aw/connectors/servicenow.yaml`; its executable fixtures
are in `pkg/connectors/testdata/providers/servicenow/`. It uses the shared
`github.com/gh-aw/connectors/v1` parser, not a ServiceNow-specific configuration
format. The fixed `https://example.service-now.com` origin is a specimen: a
reviewed definition must pin the actual approved instance before use.

The definition declares two REST-backed agent read tools, two metadata context
reads, and two work-note appends for `incident` and `sc_req_item`. It deliberately
does not enable `sc_request`, state transitions, journal-history reads, or native
remote MCP. Reads request only `sys_id,number,state,sys_mod_count`, capped at 8192
response bytes; append receipts request only `sys_id`, capped at 1024 bytes.
Notes have a 4000-character input limit.
The minimal `sys_id` receipt acknowledges an HTTP response for the record; it
does not independently prove a durable journal entry was created or that tenant
business rules accepted the change. Verify write ACLs and journal behavior on
the instance before operational use; the prototype does not audit journal
history or record an acknowledged response as a verified business outcome.

The connection fixture contains secret names, not values. Its `incidents` and
`requested-items` resources are exact `sys_id` allowlists. Record inputs require
32 lowercase hexadecimal characters as well as an exact allowlist match.
Read responses must include nonempty `sys_id`, `number`, `state`, and
`sys_mod_count` strings; response IDs must match the requested record and belong
to the corresponding table's allowlist. Nested values in metadata fields fail
type validation rather than being included in context.
Response projection discards all fields except the declared metadata or receipt.
This is field exclusion, not semantic redaction of arbitrary allowed values.
`x_gh_aw.records.read` and `x_gh_aw.records.work-notes` name administrator-defined
OAuth scopes, not built-in ServiceNow scopes. They must be provisioned and bound
to appropriate REST/table/field access before tokens are issued.

The event fixtures describe a custom administrator-owned envelope, not a native
ServiceNow webhook payload. The sender supplies `event`, `instance`, `table`,
`sys_id`, and `delivery_id`. `gh-aw.local/servicenow-admin-ingress` is an
unimplemented verification-profile requirement, not an installed verifier or
a signing algorithm. Trigger projection does not authenticate that envelope.
The prototype has no receiver, ingress credential field, source enrollment,
durable replay store, or arbitrary-content redaction pipeline.
Those are deployment blockers, not implicit fallbacks.

The shared runtime executes these declarations against a host-approved HTTPS
origin with phase-separated secret resolution, timeouts, response bounds, and
no redirects or automatic retries. Safe-output staging is credential-free;
execution requires the same frozen plan digest, argument validation, and explicit
host write authorization. A prepared work-note append is not a recorded mutation.
No receiver is installed by these declarations.

The fixture checks use only local TLS mock servers. They exercise incident and
requested-item hydration, metadata projection, work-note staging/execution,
credential boundaries, bounded inputs/responses, resource-scoped event
projection, ACL failures, and non-retryable failed writes. No live instance,
tenant OAuth exchange, business rules, journal persistence, or MCP Server Console
configuration is verified by those tests.

## Workflow selection

Select provider capabilities together under `connectors.servicenow`. Definition
and connection paths are repository-relative. External documents retain their
v1 names (`triggers`, `mcp`, `httpMcp`, `context`, `safeOutputs`); workflow
selection uses `on`, `tools`, `context`, and `safe-outputs`.

```aw title="ServiceNow manual review"
on: workflow_dispatch
permissions:
  contents: read
  copilot-requests: write
network:
  allowed:
    - example.service-now.com
connectors:
  servicenow:
    definition: .github/aw/connectors/servicenow.yaml
    connection: pkg/connectors/testdata/providers/servicenow/connection.yaml
    tools: [get-incident, get-requested-item]
    context:
      incident-metadata:
        record: "0123456789abcdef0123456789abcdef"
    safe-outputs: [append-incident-work-note]
```

Each capability is optional and disabled when omitted. `tools` selects exact
REST-backed tool definitions or configured `httpMcp` registrations; it does not
enable all provider tools. `context` maps a selected recipe to literal input
values, not event expressions or agent-supplied URLs. Omitting `on` disables
ServiceNow event activation without disabling manual execution, reads, context,
or safe outputs. Do not scatter selections under root `tools.connectors`,
`on.connectors`, or `safe-outputs.connectors`.

The provider fixture directory contains `workflow.md` for the combined manual
example and separate tools-only, context-only, and effects-only examples.
`binding.yaml` is a low-level shared-runtime test fixture, not the workflow
frontmatter format. Fixture paths outside `.github/workflows` are inert examples,
not registered workflows. The supplied instance and record IDs are specimens:
review the fixed origin, table ACLs, identities, scopes, and resource allowlists
before configuring an actual tenant.

Connector event selection is a separate, deliberately unavailable capability:

```yaml
connectors:
  servicenow:
    definition: .github/aw/connectors/servicenow.yaml
    connection: pkg/connectors/testdata/providers/servicenow/connection.yaml
    on: [incident-updated]
```

`workflow-ingress-unavailable.md` is a negative example: activation must fail
pending an approved receiver. Root `on: workflow_dispatch` is used in the other
examples; it does not activate the ServiceNow subscription.

The four manual examples are covered by compiler regression tests using the
consolidated interface. These verify independent selection, phase-specific
credential references in generated jobs, and rejection of invalid tools and
context targets. The ingress fixture must fail with
`ErrIngressUnavailable`; accepting its external definition is not activation.
CLI no-emit compilation also reports the new specimen secret references as
unapproved safe-update changes. That warning does not provision or authorize
the tokens; tenant configuration still requires review.

## Instance and record scope

Approve an exact HTTPS instance origin before supplying credentials. Do not derive
the instance origin, target table, API URL, or credentials from an event payload.
Do not follow redirects to another origin or traverse reference links returned by
the API.

Incident records use `incident`; requests and requested items use different tables,
`sc_request` and `sc_req_item`. Enable only tables whose ACLs and business rules
have been reviewed for the integration. A display number such as `INC0009009` is
not the record's `sys_id`. Record paths require the canonical 32-hexadecimal
`sys_id`; display-number lookup needs a separately bounded, authorized resolution
step and must reject missing or ambiguous matches.

## Credentials and hydration

Use distinct ingress, read, and write credentials. Store secrets outside the
manifest. ServiceNow supports OAuth for REST integrations; provision dedicated
integration identities with table, record, field, application-scope, and domain
permissions appropriate to the tenant. A successful authentication does not imply
access to every requested field.

Hydrate an admitted event with an authenticated single-record Table API request:

```text
GET /api/now/table/incident/{sys_id}?sysparm_fields=sys_id,number,state,sys_mod_count&sysparm_display_value=false&sysparm_exclude_reference_link=true
```

Use a fixed field projection and explicit response-size and timeout bounds.
ServiceNow can omit ACL-inaccessible fields: required identity and policy fields
must be present and correctly typed before the record is used. Do not replace
missing protected fields with defaults. Record content is untrusted data, not
instructions, and must not select privileged operations.

Descriptions, caller details, work notes, and attachments can contain sensitive
information. Do not include them in logs or mutation receipts. Enable content
hydration only with an approved field projection, bounded content, and redaction;
identifiers and minimal status metadata are the default context.

## Event delivery

An ingress subscription describes which events the receiver will accept. It does
not register a universal ServiceNow webhook. An instance administrator must
configure a record-triggered Flow with an outbound REST action, or approved
Business Rule/outbound REST integration, to deliver events to the receiver.
IntegrationHub REST steps can require an additional subscription and plugin.

Configure authentication explicitly for that outbound integration. ServiceNow
does not supply one universal built-in signed-webhook format for arbitrary table
changes. A custom signature scheme is valid only when both the instance sender
and ingress verifier implement the same documented protocol. Authentication,
event IDs, timestamps, replay rejection, tenant/table scope, and delivery limits
must be enforced before hydration. Never accept record content as proof of
origin or authorization.

## Read MCP

ServiceNow has native MCP Server Console capabilities, but availability depends
on the instance release, installed applications, and activated server/tools. The
instance administrator supplies the published server URL and OAuth configuration.
There is no universal hosted ServiceNow incident MCP endpoint or guaranteed tool
name.

Expose only audited read tools backed by ACL-enforcing implementations and a
read-only identity. A tool allowlist is not a substitute for server-side
authorization. Do not expose a general record-update MCP tool to the agent.
Token acquisition and refresh must be configured explicitly; a client secret is
not an access token. Omit MCP when no compatible authenticated read endpoint has
been configured rather than silently substituting an unofficial server.

## Mutations

An internal work-note append sends only a new `work_notes` value to the approved
record using Table API `PATCH`. It does not replace journal history and is not a
customer-visible `comments` update. Do not copy previous journal entries into the
new value. Duplicate requests can append duplicate notes; do not retry an
ambiguous write automatically or claim exactly-once behavior without verified
server-side deduplication.

Status changes require a tenant-approved mapping of semantic operations to raw
state values for each table, including permitted source states and mandatory
fields. Incident state values are not universal request-state semantics.
Resolution or closure may require fields such as resolution codes and notes;
reject operations that cannot satisfy those rules rather than expanding the
field allowlist.

No universal Table API `ETag`/`If-Match` guarantee has been established for this
provider. Reading `sys_mod_count` and then issuing a PATCH is not atomic
compare-and-swap. A transition that requires optimistic concurrency must remain
disabled until an instance endpoint actually enforces the precondition
atomically. Do not represent a client-side preflight read as a concurrency guard.

Allow only explicitly configured tables, original hydrated targets, operations,
and writable fields. Reject arbitrary fields, record URLs, cross-instance
targets, and agent-supplied credentials. Keep write credentials outside the
agent's read environment. Reconcile ambiguous outcomes using an authorized
operator process, without publishing sensitive journal text.

## Official references

- [Table API reference](https://developer.servicenow.com/dev.do#!/reference/api/zurich/rest/c_TableAPI)
- [Update an incident using REST API Explorer](https://www.servicenow.com/docs/r/api-reference/rest-api-explorer/get-started-update-incident.html)
- [Table API ACL and field-projection FAQ](https://support.servicenow.com/kb?id=kb_article_view&sysparm_article=KB0534905)
- [Outbound REST authentication](https://www.servicenow.com/docs/r/api-reference/web-services/c_OutboundRESTAuth.html)
- [IntegrationHub REST step](https://www.servicenow.com/docs/r/integrate-applications/integration-hub/rest-request-action-designer.html)
- [MCP Server Console](https://www.servicenow.com/docs/r/intelligent-experiences/mcp-platform-manager-landing.html)
- [ServiceNow developer advocate: native MCP setup, tools, and OAuth](https://www.servicenow.com/community/developer-advocate-blog/building-an-mcp-server-on-servicenow-and-connecting-claude-to-it/ba-p/3588171)
