---
title: Slack connector
description: Slack API constraints and capability boundaries for external connector configuration.
---

The Slack connector targets an approved workspace and channel: receive an
`app_mention`, hydrate a bounded message thread, and propose a reply to that same
channel and parent thread. Connector configuration does not install a Slack app,
deploy an Events API receiver, or authorize access.

> [!CAUTION]
> This reference records provider requirements. It does not claim that a deployed
> webhook receiver or unattended Slack MCP integration is available.

## Credentials and scope

Use an installed Slack app subscribed to
[`app_mention`](https://docs.slack.dev/reference/events/app_mention/), with
`app_mentions:read`. Restrict accepted events to explicitly approved `team_id`,
`api_app_id`, and channel IDs. Channel membership and workspace authorization
remain prerequisites; a signed event is not permission to access another channel.

Keep the signing secret, read credential, and write credential separate. Thread
hydration uses
[`conversations.replies`](https://docs.slack.dev/reference/methods/conversations.replies/)
with `channels:history` for public channels or `groups:history` for private
channels, using a token type supported by the installed app and method. The write
credential needs only `chat:write` and membership in the approved channel.
Do not grant `chat:write.public`, `chat:write.customize`, or workspace-wide search
merely to answer a mention. Never expose the write token to the agent.

## Events and delivery

Verify
[Slack request signatures](https://docs.slack.dev/authentication/verifying-requests-from-slack/)
before parsing or using the payload. Compute HMAC-SHA256 with the signing secret
over `v0:<X-Slack-Request-Timestamp>:<exact raw body>`, prefix the hex digest with
`v0=`, and compare in constant time. Header names are case-insensitive. Reject
timestamps more than 300 seconds in either direction from the receiver's clock;
do not reconstruct signed bytes from parsed JSON.

For a verified `url_verification` request, return HTTP 200 with the challenge
value, without dispatching an agent. The
[URL verification handshake](https://docs.slack.dev/apis/events-api/using-http-request-urls/)
is distinct from event processing.

For accepted `event_callback` deliveries, persist and deduplicate by `event_id`
before dispatch. Acknowledge within three seconds and process asynchronously.
[Slack retries](https://docs.slack.dev/apis/events-api/#retries) failed delivery
immediately, after one minute, and after five minutes. Optional delayed events
can retry hourly for 24 hours. Retry headers describe delivery attempts, not
permission to dispatch a second agent.

Ignore bot-generated and self-authored messages, including messages containing
`bot_id`, and reject unsupported event types and message subtypes. If ordinary
`message` events are enabled later, edits, deletions, and bot subtypes must not
silently become new human requests. Shared-channel content remains untrusted
even when its envelope is signed.

## Thread context and replies

Slack message timestamps are string identifiers, not floating-point numbers.
Use `event.thread_ts` when present, otherwise `event.ts`, as the parent thread
identifier. Bind hydration and output to this verified workspace, channel, and
parent; agent-provided channel or thread values must not redirect either action.

Call `conversations.replies` with a finite `limit` and bounded pagination,
response bytes, and total messages. Surface incomplete context explicitly.
Treat message text as untrusted data rather than instructions or executable
templates. Never follow URLs from message text as authenticated API endpoints.

Safe output uses
[`chat.postMessage`](https://docs.slack.dev/reference/methods/chat.postMessage/)
with fixed `channel` and `thread_ts`, `reply_broadcast: false`,
`unfurl_links: false`, and `unfurl_media: false`. Bound text length; do not allow
arbitrary blocks, attachments, impersonation fields, or metadata through an
unvalidated output object.

## Errors and rate limits

Slack Web API responses require both a successful HTTP status and JSON
`ok: true`. HTTP 200 with `ok: false` is a failure, not a successful write.
Missing or malformed success fields must also fail explicitly.

Honor HTTP 429 and `Retry-After`, with bounded retries for reads. Posting
generally allows one message per second per channel, with additional workspace
limits. A write timeout, lost response, HTTP 5xx, or Slack `internal_error` /
`fatal_error` can leave an ambiguous outcome: do not automatically resend and
claim exactly-once delivery. Preserve that uncertainty for reconciliation.
See [Slack rate limits](https://docs.slack.dev/apis/web-api/rate-limits/).

## MCP availability

Slack documents an official
[MCP server](https://docs.slack.dev/ai/slack-mcp-server/) at
`https://mcp.slack.com/mcp`, using Streamable HTTP and confidential user OAuth.
Only internal or Marketplace-published apps are eligible. Slack does not support
SSE or dynamic client registration for this server.

This is not evidence that an unattended bot token works with that endpoint.
An unattended connector must explicitly mark MCP unsupported unless its runtime
supports Slack's managed user OAuth lifecycle and a verified read-only tool
allowlist. REST hydration is not MCP coverage. Do not invent npm package names,
MCP tool names, or endpoints to fill that gap.
