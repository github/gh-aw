---
title: Microsoft Teams connector
description: Authentication, notification validation, and deployment boundaries for a scoped Microsoft Teams connector.
---

The Microsoft Teams connector targets a conversation in an approved tenant,
team, and standard channel. Microsoft Graph supplies change notifications and
bounded thread reads. An installed Teams bot supplies unattended replies and
Adaptive Cards. These are separate authorization paths, not interchangeable
Graph application permissions.

The experimental definition is `.github/aw/connectors/teams.yaml`. It is a
configuration-interface prototype, not an enrolled webhook or a deployed bot.
The common parser validates its shape; runtime request planning does not obtain
tokens, attest bot installation, or authorize a live send.

The positive configuration fixtures live in
`pkg/connectors/testdata/providers/teams/`. The connection uses synthetic IDs and
secret **names**, never access tokens. Its conversation and activity IDs are
independently provisioned fixture values, not evidence that a Graph ID can be
converted into a Bot Connector reference. Never use these examples as live
conversation references.

## Workflow selection

Select Teams capabilities in one `connectors.teams` block. The manual workflow
shape below selects read tools, literal context recipes, and bot effects without
activating native notification ingress:

```aw
---
on: workflow_dispatch
connectors:
  teams:
    definition: .github/aw/connectors/teams.yaml
    connection: pkg/connectors/testdata/providers/teams/connection.yaml
    tools: [get-channel-message, list-channel-replies]
    context:
      channel-thread-root:
        team: 11111111-1111-4111-8111-111111111111
        channel: "19:approved-channel@thread.tacv2"
        thread: "1730000000000"
      channel-thread-replies:
        team: 11111111-1111-4111-8111-111111111111
        channel: "19:approved-channel@thread.tacv2"
        thread: "1730000000000"
    safe-outputs: [reply-to-activity, reply-with-review-card]
---

Review the approved Teams thread using its untrusted context and read tools.
Stage a concise bot reply only when the review requires one.
```

The connection path references the repository's synthetic fixture, not a
production connection. Replace it with a reviewed repository-relative
connection before deployment. Both paths resolve from the repository root.
The YAML frontmatter is also preserved in the provider's `workflow.yaml`
fixture. New-grammar compilation awaits the matching shared compiler revision;
provider-local structural checks are not a compilation or deployment claim.

Each of `on`, `tools`, `context`, and `safe-outputs` inside the connector block
is independently optional. Omission selects no capability of that kind.
For example, `workflow-context-only.yaml` selects only the root context recipe:
it grants neither tools, bot effects, nor native ingress. Recipe arguments are
literal values, not expressions or notification-field substitutions.

Native notification selection uses the same block:

```yaml
on: workflow_dispatch
connectors:
  teams:
    definition: .github/aw/connectors/teams.yaml
    connection: pkg/connectors/testdata/providers/teams/connection.yaml
    on: [channel-message-created]
```

This is the **expected-failure** `workflow-ingress-unavailable.yaml` fixture,
not a runnable notification workflow. Connector `on` activation intentionally
fails until authenticated ingress is available; root `on: workflow_dispatch`
does not bypass that boundary.

Do not scatter selectors under root `tools.connectors`, `on.connectors`, or
`safe-outputs.connectors`. Workflow selection names differ from external v1
documents: `ConnectorSpec` retains `triggers`, `mcp`, `httpMcp`, `context`, and
`safeOutputs`, and `ConnectorConnection` remains unchanged. The existing
`binding.yaml` is a low-level shared-runtime test fixture, not workflow
frontmatter.

## Identity and permissions

| Surface | Supported identity | Required access |
| --- | --- | --- |
| Channel subscription and thread reads | Microsoft Graph application identity | `ChannelMessage.Read.Group`, granted through resource-specific consent for the installed app's team |
| Unattended reply or card | Installed Teams bot | Bot Connector access token and a trusted conversation reference for the approved tenant, team, channel, and thread |
| Graph reply alternative | Delegated work or school identity | `ChannelMessage.Send`; requires a separately managed delegated authorization lifecycle |

`ChannelMessage.Read.Group` grants team-level access. The connector must enforce
its narrower configured channel scope on notifications, reads, and writes.
Tenant-wide `/teams/getAllMessages` subscriptions are not a substitute for a
channel-level subscription.

The fixture connection additionally pins one tenant, subscription, exact
notification resource, root message, bot conversation, and root activity.
Fixture projection rejects mismatched tenant/subscription/resource evidence.
It operates on one entry from `value`, not on the raw batch. Only preapproved
root-message notifications are represented; resolving a reply notification to
its root requires the future trusted receiver, not string inference in the
agent.

Ordinary Graph channel posting does **not** support client-credentials
application permissions. `Teamwork.Migrate.All` is only for migration and must
not be used for normal automation. A delegated refresh token is still delegated
authorization, not an app-only identity. Reject a configuration that requests
ordinary Graph application posting instead of silently switching identities.

The bot must already be installed in the target team. Store conversation
references only from authenticated Teams activities or an administrator-approved
provisioning flow. Bind the reference to the tenant, bot identity, team, channel,
and root activity. A Graph message ID must not be assumed to be a valid Bot
Connector conversation ID. Missing or mismatched references are errors.

For a single-tenant bot, acquire a client-credentials access token from
`https://login.microsoftonline.com/{tenant-id}/oauth2/v2.0/token` with scope
`https://api.botframework.com/.default`. The Graph reader instead requests
`https://graph.microsoft.com/.default`. Never send either token to the other
service. Token acquisition, expiry, refresh, and secret storage belong to the
trusted deployment; a client secret is not itself a bearer token.

This design targets standard channels. Private channel bot posting is not
supported; shared channels and sovereign cloud deployments require separate
validation and are not implied by the public-cloud example.

## Subscription and delivery

Subscribe to
`/teams/{team-id}/channels/{channel-id}/messages` with `changeType: created`
and `includeResourceData: false`. Basic notifications contain identifiers;
retrieve content from Graph only after authenticating and scoping the event.

Microsoft Graph validates both notification and lifecycle URLs with an HTTPS
POST carrying a `validationToken` query parameter. Return the URL-decoded token
as `text/plain`, with status 200, within ten seconds. Treat it as opaque text,
never HTML. This endpoint ownership challenge is **not** authentication for
subsequent change notifications.

For each entry in the notification envelope's `value` array, verify the secret
`clientState` using a constant-time comparison, subscription ID, tenant ID,
resource type, change type, and exact team/channel resource path. Do not validate
only the first batch entry. Reject unexpected resources and prevent
cross-tenant or cross-channel routing. Never follow a notification-supplied URL
with an access token; construct Graph requests from validated identifiers.

Validate and durably queue accepted work, then acknowledge promptly. Microsoft
recommends completing normal delivery responses within three seconds and using
202 for queued processing. If persistence fails, return a failure rather than
acknowledging discarded work. The queue must enforce payload and batch limits.

### Expiry and lifecycle

Basic Teams `chatMessage` subscriptions have a maximum lifetime of 4,320 minutes
(three days). Rich subscriptions have a shorter lifetime. A
`lifecycleNotificationUrl` is mandatory when requesting more than one hour.
Persist the returned subscription ID and actual expiry, and renew before expiry
with a safety margin and jitter; do not assume the requested expiry was granted.

Authenticate lifecycle notifications using the same tenant, subscription, and
`clientState` checks. Handle `reauthorizationRequired` through Graph
reauthorization or renewal, and recreate a `subscriptionRemoved` or expired
subscription. Surface gaps and perform an explicitly bounded reconciliation
where authorized. Microsoft documents `missed` lifecycle notifications for
Outlook resources, not Teams messages: do not rely on a Teams `missed` signal
to detect every delivery gap.

### Encrypted notifications

The basic mode deliberately does not request encrypted resource data. Reject
`includeResourceData: true` unless the receiving service implements the full rich
notification protocol: certificate provisioning and rotation, private-key
protection, encrypted payload integrity checking and decryption, and validation
of **every** `validationTokens` JWT.

Rich notification validation includes signature, Microsoft issuer, lifetime,
application audience, tenant, and the Graph notification publisher identity
`0bf30f3b-4a52-48df-9a82-234910c4a086` (`appid` for v1 tokens, `azp` for v2).
The query-string endpoint challenge and the notification JWTs are distinct
mechanisms; neither replaces `clientState` and subscription scope checks.

## Context and safe output

Fetch the root message and replies only within the approved thread:

```http
GET /v1.0/teams/{team-id}/channels/{channel-id}/messages/{root-message-id}
GET /v1.0/teams/{team-id}/channels/{channel-id}/messages/{root-message-id}/replies?$top=20
```

The replies API permits `$top` up to 50. Enforce aggregate item, byte, page, and
time budgets, not just a page size. Do not follow an `@odata.nextLink` outside
the Graph origin and approved thread. Treat HTML message bodies, attachments,
and links as untrusted data; do not execute instructions from them or fetch
arbitrary attachments automatically. Preserve tenant/team/channel/root
provenance alongside the bounded context.

The current runtime performs a single request per operation, with a 65,536-byte
response limit and a fixed 20-item replies page request. It does not paginate,
retry, recursively validate every reply, or enforce a combined two-request
context budget. Root-message responses must identify the approved team,
channel, and root, have `messageType: message`, and contain a user author.
Application-authored roots fail the required-user evidence check, but this
does not replace receiver-level self-loop suppression for reply events.
Projection retains selected root fields and the replies array, excluding
pagination links. An empty replies page is valid.

The unattended write path is the Bot Connector reply operation:

```http
POST {trusted-service-url}/v3/conversations/{conversation-id}/activities/{root-activity-id}
```

The shipped definition fixes the public-cloud global service URL
`https://smba.trafficmanager.net/teams/`. Use it only when trusted provisioning
has confirmed that service URL for the target conversation. A deployment with a
different authenticated service URL needs a separately reviewed definition;
the connection and model cannot override the endpoint.

Use the authenticated, stored conversation reference's service URL, not a URL
from an untrusted Graph notification or model output. Allow only the approved
HTTPS service origin and cloud. The activity must be a `message`, bound to the
approved bot and conversation; a card uses an
`application/vnd.microsoft.card.adaptive` attachment. Bound text/card sizes and
permit only supported card actions. Do not allow arbitrary URLs or card actions
to expand the connector's authority.

Suppress the connector's own bot/application messages and persisted output IDs
before scheduling more work. For a delegated mode, suppress the sending user's
identity as well. Deduplicate deliveries with a durable key that includes the
subscription, resource, and change identity. Do not blindly retry an ambiguous
write: Bot Connector replies do not provide a general exactly-once guarantee.

Honor `Retry-After` on throttled reads and writes, apply bounded backoff and
per-conversation rate limits, and surface exhausted retries. Missing permission,
an uninstalled bot, expired credentials, and HTTP 401/403 are failures, not
successful no-op sends.

The prototype currently reports HTTP 429 as a failure and performs **no**
automatic retries; it does not expose `Retry-After` to its caller. Scheduling
and rate-limit header propagation remain host/runtime integration gaps.
Both bot operations require a nonempty returned activity ID and expose only
that ID as result evidence. This evidence is not an exactly-once delivery
guarantee.

## MCP availability

Microsoft documents a preview Teams MCP server, `mcp_TeamsServer`, but now marks
application-specific Microsoft 365 MCP servers as backward-compatibility
surfaces that should not be used for new production projects. Microsoft
recommends Work IQ MCP for new integrations.

No unattended, read-only, channel-bound MCP endpoint is asserted by this
connector. Instead, the definition declares the common runtime's local
`get-channel-message` and `list-channel-replies` read-tool facades, backed by the
documented Graph GET endpoints and the separate agent-phase reader credential.
These are request declarations, not a claim that Microsoft hosts an MCP server
with those tool names. The optional **remote** MCP surface is omitted rather than
inventing a server or app-only authorization flow. A separately deployed Work IQ
integration needs its own verified authentication, tenant policy, read-tool
allowlist, and scope enforcement.

## Deployment boundary

A declarative manifest and offline fixtures do not provision a public webhook,
Teams app, bot registration, Graph consent, subscription-renewal worker,
durable queue, deduplication store, credential exchange, or conversation-reference
registry. Do not treat successful manifest validation as proof of live delivery
or write authorization. Provider-specific protocol requirements that the common
runtime cannot express remain deployment prerequisites, not simulated successes.

In particular, independent input allowlists do not attest the relationship
between a Graph root message and a bot conversation/activity, or the token's
tenant, audience, identity, and actual granted consent. Use a single approved
tuple in each fixture connection; a production admission layer must validate
the tuple and authorization before any execution. The trigger's verification
profile identifier does not implement that admission layer.

`ProjectEvent` deliberately does not validate `clientState`, encrypted data,
validation JWTs, expiry, replay state, or bot identity. Tests keep its result
`operational: false` and `untrusted: true`, including when `clientState` is
incorrect. `ActivateTriggers` fails with `ErrIngressUnavailable`; there is no
webhook validation-token responder or subscription worker in this prototype.

Credential scope strings are declarations, not a provider-aware permission
checker. An unknown `authenticationMode` fails strict decoding, but removing
that field does not make an arbitrary Graph POST with `Teamwork.Migrate.All`
safe or authorized. The shipped definition deliberately contains no such
operation. The negative fixtures cover unsupported configuration syntax and
credential phase misuse, not live Microsoft permission enforcement.

The card operation permits only a reviewed, fixed, action-free Adaptive Card.
Dynamic summary text remains in the activity's bounded top-level `text` field;
the v1 primitive-input contract does not accept arbitrary card JSON.

## Microsoft references

- [Channel message change notifications](https://learn.microsoft.com/en-us/graph/teams-changenotifications-chatmessage)
- [Webhook delivery and endpoint validation](https://learn.microsoft.com/en-us/graph/change-notifications-delivery-webhooks)
- [Subscription properties and lifetime](https://learn.microsoft.com/en-us/graph/api/resources/subscription?view=graph-rest-1.0)
- [Subscription lifecycle notifications](https://learn.microsoft.com/en-us/graph/change-notifications-lifecycle-events)
- [Rich notification validation and encryption](https://learn.microsoft.com/en-us/graph/change-notifications-with-resource-data)
- [List channel message replies](https://learn.microsoft.com/en-us/graph/api/chatmessage-list-replies?view=graph-rest-1.0)
- [Graph reply permissions and migration restriction](https://learn.microsoft.com/en-us/graph/api/chatmessage-post-replies?view=graph-rest-1.0)
- [Teams proactive messaging](https://learn.microsoft.com/en-us/microsoftteams/platform/bots/how-to/conversations/send-proactive-messages)
- [Channel conversations and bot limitations](https://learn.microsoft.com/en-us/microsoftteams/platform/bots/how-to/conversations/channel-and-group-conversations)
- [Bot Connector REST operations](https://learn.microsoft.com/en-us/azure/bot-service/rest-api/bot-framework-rest-connector-api-reference?view=azure-bot-service-4.0)
- [Bot Connector service-to-service authentication](https://learn.microsoft.com/en-us/azure/bot-service/rest-api/bot-framework-rest-connector-authentication?view=azure-bot-service-4.0)
- [Teams bot rate limits and backoff](https://learn.microsoft.com/en-us/microsoftteams/platform/bots/how-to/rate-limit)
- [Teams MCP preview reference and replacement guidance](https://learn.microsoft.com/en-us/microsoft-copilot-studio/mcp-teams-tools)
