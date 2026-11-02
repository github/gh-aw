---
title: Microsoft Teams connector
description: Authentication, notification validation, and deployment boundaries for a scoped Microsoft Teams connector.
---

The Microsoft Teams connector targets a conversation in an approved tenant,
team, and standard channel. Microsoft Graph supplies change notifications and
bounded thread reads. An installed Teams bot supplies unattended replies and
Adaptive Cards. These are separate authorization paths, not interchangeable
Graph application permissions.

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

The unattended write path is the Bot Connector reply operation:

```http
POST {trusted-service-url}/v3/conversations/{conversation-id}/activities/{root-activity-id}
```

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

## MCP availability

Microsoft documents a preview Teams MCP server, `mcp_TeamsServer`, but now marks
application-specific Microsoft 365 MCP servers as backward-compatibility
surfaces that should not be used for new production projects. Microsoft
recommends Work IQ MCP for new integrations.

No unattended, read-only, channel-bound MCP endpoint is asserted by this
connector. The optional MCP surface is omitted rather than inventing a server,
tool allowlist, or app-only authorization flow. A separately deployed Work IQ
integration needs its own verified authentication, tenant policy, read-tool
allowlist, and scope enforcement.

## Deployment boundary

A declarative manifest and offline fixtures do not provision a public webhook,
Teams app, bot registration, Graph consent, subscription-renewal worker,
durable queue, deduplication store, credential exchange, or conversation-reference
registry. Do not treat successful manifest validation as proof of live delivery
or write authorization. Provider-specific protocol requirements that the common
runtime cannot express remain deployment prerequisites, not simulated successes.

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
