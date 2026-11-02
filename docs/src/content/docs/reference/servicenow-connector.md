---
title: ServiceNow connector
description: ServiceNow incident and request integration boundaries, credentials, and deployment prerequisites.
---

The ServiceNow provider targets explicitly approved incident and request records.
Configuration validation is not deployment: accepting a manifest does not install
instance automation, activate an MCP server, or authorize record mutations.

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
