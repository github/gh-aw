---
title: Update and Upgrade JSONL Specification
description: Machine-readable messages for workflow updates, repository upgrades, and pull request delivery
sidebar:
  order: 1370
---

# Update and Upgrade JSONL Specification

**Version**: 1.0.0<br>
**Status**: Working Draft<br>
**Publication Date**: 2026-11-18<br>
**Editor**: GitHub Agentic Workflows Team<br>
**This Version**: [update-upgrade-jsonl-specification](/gh-aw/specs/update-upgrade-jsonl-specification/)<br>
**Latest Published Version**: This document

---

## Scope

`gh aw update --jsonl` and `gh aw upgrade --jsonl` provide structured messages for automation. Consumers can identify changes and created pull requests without parsing terminal formatting or human-readable descriptions.

The flag changes reporting, not authorization or execution: it does not imply `--yes`, create a pull request, or turn an operation into a dry run. Existing update, upgrade, and rollout policies continue to apply. Without `--jsonl`, output remains unchanged.

## Transport

Standard output contains UTF-8 JSON Lines: one compact JSON object followed by a newline per message. Embedded newlines in string values are JSON-escaped. Human-readable diagnostics remain on standard error; consumers MUST NOT merge standard error into the JSONL stream.

Consumers MUST tolerate unknown message types and additional object fields within a supported schema version. Human-readable text is informational, not a stable identifier.

## Message envelope

Every message MUST contain these fields:

| Field | Type | Meaning |
| --- | --- | --- |
| `schema_version` | integer | `1` for this specification. Incompatible changes require a new version. |
| `command` | string | `update` or `upgrade`. |
| `event` | string | Message discriminator from the table below. |

Other fields are omitted when not applicable:

| Field | Type | Meaning |
| --- | --- | --- |
| `repository` | string | Target `owner/repo` when known, including for organization rollouts. |
| `path` | string | Affected file path, relative to the repository checkout (slash-normalized); an external `--dir` may produce a path containing `..`. |
| `kind` | string | Change category or warning phase. |
| `operation` | string | `added`, `modified`, or `removed` for observed changes. |
| `before`, `after` | string | Previous and resulting values, interpreted according to `kind`. A missing value represents an absent side. |
| `url` | string | Pull request or issue URL. |
| `status` | string | Outcome associated with the event. |
| `error` | string | Human-readable diagnostic; not a stable error code. |
| `codemods` | string array | Applied codemod names. |
| `changed_files` | integer | Count of unique file paths with `change` messages whose `kind` is `workflow`, `lockfile`, or `artifact`, including files marked `metadata_only`. Source, source SHA, compiler, pin, and extension transitions do not increment this count. |
| `metadata_only` | boolean | `true` when the file fingerprint uses filesystem metadata rather than file contents. |

## Events

| `event` | Meaning |
| --- | --- |
| `command_started` | The JSONL command invocation has started. |
| `change` | An observed change, with `kind`, `path`, and `operation` where applicable. |
| `warning` | A phase or per-item diagnostic, with category in `kind` and diagnostic in `error`. The command may continue; its terminal outcome is reported separately. |
| `codemod` | Codemods applied to `path`, listed in `codemods`. |
| `pull_request` | A pull request was created: `status` is `created` and `url` identifies it. |
| `issue` | An issue was created: `status` is `created` and `url` identifies it when the API response provides a decodable URL. If the response cannot be decoded or has no URL, `error` explains that limitation and the command reports an error; the issue may nevertheless exist. |
| `repository_preview` | Planned work or a preview outcome; MUST NOT be interpreted as an applied change. |
| `repository_completed` | One rollout repository's outcome, with `repository`, `status`, and optional `url` or `error`. |
| `command_completed` | Terminal command outcome: `status` is `success` or `error`; failures include `error`. |

Repository outcomes include `created`, `unchanged`, `planned`, `failed`, `success`, `skipped: existing PR`, `skipped: compiler version matches`, and `skipped: no workflow Markdown files`. A skipped repository's `url`, when present, identifies an existing PR rather than a newly created one. Consumers MUST tolerate additional statuses.

File changes are determined by comparing snapshots immediately before and after the repository operation, including when it fails. Pre-existing local modifications are not attributed to the command unless the command changes those files again. The snapshot covers workflow and generated artifact paths used by update and upgrade, including resolved package destinations and external workflow directories. In local directories without a Git checkout, it uses a filesystem snapshot rooted at the current directory. Snapshot failures produce a warning and do not prevent the update or upgrade from running. The snapshot is not an audit of every file on disk.

### Change kinds

| `kind` | `before` and `after` |
| --- | --- |
| `workflow` | SHA-256 fingerprints of workflow Markdown. |
| `lockfile` | SHA-256 fingerprints of generated workflow YAML. |
| `artifact` | SHA-256 fingerprints of other observed files. |
| `source` | Workflow source specifications. |
| `source_sha` | Installed source commit SHAs, including updates that retain the same branch reference. |
| `compiler` | Compiler versions extracted from lock-file metadata. |
| `action`, `container` | Structured pin reference serialized as `repository@version@sha`. Empty version or SHA components are retained. |
| `extension` | Previous and installed CLI extension versions; this event has no file path and does not increment `changed_files`. |

File fingerprints represent content unless `metadata_only` is `true`. Metadata-only fingerprints support reporting generated agent artifacts without reading their contents; they are not content-integrity hashes.

Changes describe the net result of the operation rather than every intermediate write. A changed file path is emitted at most once as a `workflow`, `lockfile`, or `artifact` change, though it can also produce separate source, compiler, or dependency-pin messages. Consumers MUST NOT count each metadata transition as another changed file.

Codemod and repository-preview paths are relative to the corresponding repository checkout and slash-normalized. During organization dry runs, preview paths refer to the target repository checkout, not the agent's temporary directory.

## Flag compatibility

`--jsonl` cannot be combined with `--interactive`, `upgrade --audit`, or `upgrade --json`. The existing dependency-audit JSON format remains separate. JSONL output does not suppress confirmations; unattended organization or enterprise creation still requires `--yes`.

## Delivery and failure semantics

A reported update describes an observed operation, not a guarantee that every later operation succeeds. A created pull request can remain open even if a subsequent operation fails. Likewise, successful changes in one repository are not rolled back when another repository fails.

The public invocation emits one `command_started` and one `command_completed` message when reporting completes normally, including across an extension-upgrade relaunch. Organization workers report their events through the parent invocation, with the target `repository` attached. Cross-repository event order is not a chronology of remote side effects.

`command_completed.status` reflects the command's exit behavior, not an assurance that every best-effort phase succeeded. Consumers that require every phase to succeed MUST also inspect `warning` messages and individual repository outcomes.

Consumers MUST check the process exit status as well as messages. A broken pipe, process termination, or startup failure can leave an incomplete stream. Missing messages MUST NOT be interpreted as proof that no side effects occurred.

## Examples

```bash
gh aw update --jsonl > update.jsonl
gh aw upgrade --jsonl --create-pull-request > upgrade.jsonl
gh aw update --jsonl --org my-org --dry-run > preview.jsonl
```

A pull request creation message has this shape:

```json
{"schema_version":1,"command":"upgrade","event":"pull_request","repository":"owner/repo","url":"https://github.com/owner/repo/pull/42","status":"created"}
```

Consumers can extract created pull request URLs by selecting the event discriminator rather than matching prose:

```bash
jq -r 'select(.schema_version == 1 and .event == "pull_request" and .status == "created") | .url' upgrade.jsonl
```

See the [CLI reference](/gh-aw/setup/cli/) for the commands' other options.
