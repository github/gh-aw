---
name: issue-markdown
description: Build and refactor issue, comment, and review bodies in actions/setup/js using typed report trees and a single publication render.
---

# Issue and comment Markdown

Use this skill when generating or changing Markdown published to issues, comments,
or reviews, especially in `actions/setup/js/`. Build a `Report` tree first; render
only the complete document. Do not assemble Markdown using interpolation,
concatenation, `body +=`, or arrays of Markdown strings joined with newlines.
Parsing an assembled string afterward is not a tree-first refactor.

## Find publication paths

Search production `.cjs` files for `issues.create`, `issues.update`,
`createComment`, `updateComment`, `createReview`, `createReplyForReviewComment`,
and GraphQL body variables. Trace each body back through shared helpers, fallback
comments, retries, staged previews, and append/prepend/replace-island updates.
Exclude test fixtures, HTTP payload assembly, and step-summary-only output.
Jira ADF text is not Markdown: preserve its literal entities, Unicode, and
paragraphs instead of routing it through the Markdown parser or renderer.

## Use the existing tree

`actions/setup/js/report_ast.cjs` defines `Report`, `ReportBlock`, and `ReportInline`.
Use JSDoc annotations so `npm run typecheck` verifies node kinds:

```javascript
/** @type {import("./report_ast.cjs").Report} */
const report = {
  type: "report",
  title: "Workflow result",
  children: [
    {
      type: "paragraph",
      agentTainted: false,
      children: [
        { type: "text", value: "Created related issue: ", agentTainted: false },
        { type: "issue_reference", number: String(issue.number), agentTainted: false },
      ],
    },
    { type: "code_block", language: "text", value: diagnostic, agentTainted: true },
  ],
};
```

Use `heading`, `paragraph`, `list`, `table`, `details`, `quote`, `alert`,
`separator`, `code_block`, and typed inline nodes instead of Markdown punctuation.
Use `xml_marker`, `json_marker`, `expiration`, and `closing_reference` for machine
metadata and closing links. Preserve `xml_marker.compact` for identifiers whose
exact spelling is used by cleanup. Use `review_marker` for the durable,
URI-encoded review caller reference; keep the authoritative marker last.
Dynamic labels, paths, errors, and identifiers belong
in text/code nodes; link destinations belong in `link.url`.

Import existing agent/user Markdown with `parseReportMarkdown`, specifying
`agentTainted: true`, the correct `allowedMentions`, and the existing heading and
metadata policy. Parse configured templates before scalar substitution using
`context`; use typed inline/block slots for inserted nodes. Never interpolate
untrusted values into a template before parsing it. A literal code fence in
imported content must not swallow attribution or adjacent generated blocks.

For managed comment memory, set `code_block.language` to
`gh-aw-comment-memory:<id>` and `fenceLength: 6`; rendering lengthens the fence
past embedded backticks. Do not construct or slice fences by hand.

## Compose shared attribution

Use the typed variants instead of rendering a helper and reparsing its output:

| Purpose | Tree-first API |
| --- | --- |
| Generated attribution and guard notices | `buildGeneratedFooterBlocks` / `assembleReportBodyParts` in `markdown_body_helpers.cjs` |
| Deferred footer templates and diagnostic sections | `buildFooterBlocks(getFooterMessageParts(context))` in `messages_footer.cjs` |
| Configured policy footer | `getBodyFooterBlocks` or `appendConfiguredReportFooter` |
| Detection warning | `getDetectionCautionBlocks` |
| Failure-comment attribution | `getFooterAgentFailureCommentBlocks` |
| Authoritative execution metadata | `appendReportExecutionProvenance` |

Keep custom templates and their scalar context separate until composition.
Headers and footers may jointly open/close `<details>`; use
`buildIssueBodyReport` to parse these wrappers together around body slots.
Allocate unique slot names against both source text and previously allocated
slots, and reject wrappers that swallow slots inside code fences.
Internal framework notes carried between handlers use non-JSON-forgeable Symbols
such as `COMMENT_PREAMBLE` and `ISSUE_BODY_SUFFIX`, never agent-settable flags.

## Preserve publication contracts

Framework-authored nodes use `agentTainted: false`; agent-owned descendants retain
their taint even inside framework wrappers. Keep required footer diagnostics,
metadata, and policy text protected during size fitting. Do not mark an entire
agent body trusted to keep it from being shortened.

For attributed safe-output issues and comments, use `buildIssueBodyReport` in
`issue_body.cjs`; it accepts Markdown or typed body blocks and retains configured
wrappers, attribution, expiration, and execution provenance. Comment callers set
the appropriate `itemType` and `workflowIdMarker: false`. Pass the original
workflow `runUrl` when the target repository differs from the execution repository.
Fallback issues must import the original agent body and compose recovery nodes,
not reparse an already assembled or rendered PR/discussion description.

For other issue producers, pass a typed report to
`renderIssueParameters`/`createReportIssue` in `issue_report.cjs`, preserving the
existing authentication and retry boundary. `buildIssueReport` accepts typed
blocks as well as imported Markdown. For comments, render the complete tree with
`renderSizedReport` from `report_size.cjs` and pass its body to the existing API.
Do not render fragments and concatenate or reparse them. Keep one logical render
boundary, including metadata and trusted footers in its size budget.
`renderSizedReport` includes a terminal newline: callers that trim it can reserve
`maxLength + 1`, then check the final publication limit. Never truncate the
rendered Markdown or trusted policy text to fit.

## Validate the migration

Preserve footer on/off behavior, marker identity and order, mention/link limits,
expiration, cross-repository attribution, structured state recovery, managed
memory extraction, review fallbacks, and append/prepend/island semantics.
Assert tree shape as well as rendered output. Cover hostile backticks and HTML
delimiters, Unicode, escaped table cells, and the exact publication size limit.
Update formatting expectations only for intentional canonical rendering, not to
hide lost content or metadata.

Run the matching Vitest suites from `actions/setup/js`, `npm run typecheck`,
and the repository JavaScript formatting/lint gates. Follow the developer skill
for final repository validation and publication.
