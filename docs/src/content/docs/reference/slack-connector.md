---
title: Slack connector
description: Slack API constraints and capability boundaries for external connector configuration.
---

The Slack connector targets an approved workspace and channel: receive an
`app_mention`, hydrate a bounded message thread, and propose a reply to that same
channel and parent thread. Connector configuration does not install a Slack app,
deploy an Events API receiver, or authorize access.

> [!CAUTION]
> This is a declaration-only configuration prototype. It does not provide a
> deployed webhook receiver or an unattended integration with Slack's hosted MCP
> server.

## Configuration interface

The definition is `.github/aw/connectors/slack.yaml`. Its reproducible connection,
binding, and event fixtures are under `pkg/connectors/testdata/providers/slack/`.
The shared parser validates the same definition used by the provider fixture;
there is no Slack-specific parser or execution engine.

Workflow configuration selects each capability in one service block:

```aw wrap
---
on: workflow_dispatch
permissions:
  contents: read
  copilot-requests: write
connectors:
  slack:
    definition: .github/aw/connectors/slack.yaml
    connection: pkg/connectors/testdata/providers/slack/connection.yaml
    tools: [read-thread]
    context:
      thread-context:
        channel: C123ABC456
        thread: "1515449522.000016"
    safe-outputs: [reply-in-thread]
---

Review the supplied untrusted thread and propose a reply only if useful.
```

`tools`, `context`, `safe-outputs`, and service `on` are independently optional;
omitting a part grants none of that capability. The runnable example uses root
`on: workflow_dispatch` and deliberately omits service `on`. Adding
`connectors.slack.on: [threaded-app-mention]` intentionally fails compilation
pending authenticated ingress. Do not scatter selections under root `tools`,
`on`, or `safe-outputs`.

The full positive example is `workflow.md` in the fixture directory;
`workflow-trigger-rejected.md` is the negative ingress example. External
`ConnectorSpec` and `ConnectorConnection` v1 fields are unchanged. `binding.yaml`
is a low-level shared runtime test fixture, not workflow frontmatter syntax.

Offline compilation checks the manual example with `--staged --no-emit`; it
does not contact Slack or send a reply. The negative ingress example must fail
with `connectors.slack: on: custom connector ingress is not operational`.
Development-mode compilation requires a compiler version stamped with a
published git reference for the trusted runtime checkout. Successful compilation
does not approve new secret references or make the prototype release-ready.

The definition exercises trigger, MCP read-tool, safe-output, and context
declarations. `read-thread` is a connector-owned REST-backed tool name in this
experimental interface, **not** a tool claimed to exist on Slack's hosted MCP
server. Both read declarations call the real `conversations.replies` endpoint,
request at most 15 messages in one page, and cap responses at 65,536 bytes.
Both require `ok: true`, `has_more: false`, and an approved parent message
timestamp, then project only messages and the completeness flag. A longer
thread fails explicitly rather than presenting a partial page as complete.
No pagination or automatic mutation retries are declared.

The IDs `T123ABC456`, `A123ABC456`, and `C123ABC456` are example approval
boundaries, not discoverable defaults. An operator must review the definition
and connection for the actual installed app. Connection credentials contain
secret names, never tokens. The example pins one approved parent timestamp as a
string in `resources.threads`; both reads and writes require an allowed channel
and parent thread.

The trigger targets mentions already in a thread. Top-level mentions require
the receiver to select `thread_ts` or fall back to `ts`; the v1 JSON Pointer
projection cannot express that fallback. Ordinary `message` events are not
enabled.

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
allowlist. The local REST-backed MCP adapter does not prove compatibility with
Slack's hosted MCP server. Do not invent npm package names,
MCP tool names, or endpoints to fill that gap.

## Prototype limitations

The shared contract validates declarations and resource-constrained requests.
It does not implement the required `slack.com/events-v0` verifier profile:
the identifier names a required future primitive, not an installed verifier.
Raw-body signature verification, clock/replay checks, signed challenge replies,
durable delivery deduplication, asynchronous acknowledgement, and bot/self-event
exclusion remain receiver requirements.

Connection resource allowlists constrain individual inputs, but do not prove
that a write belongs to the event that caused an agent run. Event-bound channel
and parent authority, Slack Connect policy, credential-to-workspace verification,
token acquisition/rotation, and ambiguous-outcome reconciliation remain host
responsibilities. Do not activate automatic replies without those controls.
Trigger evidence requires string message/thread IDs. Runtime request inputs
also enforce string timestamps and the declared timestamp pattern.

Replies require `ok: true`, returned channel and message IDs, and a returned
parent exactly equal to the requested parent, in addition to approval allowlists.
Returned IDs must be strings. Only those IDs are projected to the
result. HTTP 429 fails without retry; the generic error currently does not expose
`Retry-After` to a scheduler. There is no automatic rate-limit recovery.

The configuration therefore covers all four **declaration** sections, not a
production trigger-to-agent-to-reply pipeline. Hosted Slack MCP and live event
delivery are not demonstrated by the offline fixtures.
