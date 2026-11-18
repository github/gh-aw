# ADR-79083: Add Versioned JSON Lines Reporting to Update and Upgrade

**Date**: 2026-11-18
**Status**: Draft
**Deciders**: pelikhan (PR author); [TODO: verify other reviewers]

---

### Context

`gh aw update` and `gh aw upgrade` are run by people and by automation, including organization-wide rollouts that create pull requests in many repositories. Today their results are human-oriented terminal text, so a consumer that needs to know which workflow or lockfile changed, which repository was affected, or which pull request was created must scrape prose that can change between releases. These commands also run several phases (extension relaunch, codemods, per-repository worker fan-out, pull request creation), so a single final summary cannot describe partial failures. The output must stay unchanged by default, and machine output must not leak workflow contents or diffs. [TODO: verify whether any existing consumer already parses this output.]

### Decision

We will add a `--jsonl` flag to `gh aw update` and `gh aw upgrade` that writes a versioned (`schema_version: 1`) JSON Lines event stream to stdout, while human diagnostics remain on stderr. File changes are reported as SHA-256 fingerprints or filesystem metadata, never as contents, which keeps the stream free of workflow bodies. The primary driver is reliable automation: consumers identify changes and created pull requests by event type rather than by prose. The flag is reporting-only; it does not imply `--yes`, create pull requests, or switch to a dry run, and it is rejected with `--interactive`, `upgrade --audit`, and `upgrade --json`.

### Alternatives Considered

#### Alternative 1: Parse the existing human-readable output

Keeping the current output required no new surface and no maintenance. It was rejected because the output is informational and its wording is not a stable identifier, so any scraper would break silently on a release. It also cannot express the per-repository and per-phase structure that organization rollouts need.

#### Alternative 2: Emit a single JSON document at command completion

A single final document is simpler to validate and version as one object, and it is close to what `upgrade --json` already does for dependency audits. This was a close call. It was rejected because nothing is written until the run ends, so long rollouts and relaunch flows give no progress signal, and an interrupted run yields no output. A streamed format accepts the cost of documenting incomplete streams (see Consequences) in exchange for partial results and per-event consumption.

#### Alternative 3: Extend the existing `upgrade --json` audit format

Reusing the dependency-audit format would avoid a second JSON surface. It was rejected because that format is scoped to dependency auditing and has a different shape. The specification keeps the two formats separate, so consumers do not have to infer which one they are reading.

### Consequences

#### Positive
- Consumers can select `event == "pull_request"` with `status == "created"` instead of matching prose, as the specification examples show.
- Unknown event types and additional fields are tolerated within `schema_version` 1, so the stream can grow without breaking existing consumers.
- File events carry hashes or metadata rather than contents or diffs, so machine output does not expose workflow bodies.
- Organization rollouts and extension relaunches keep one `command_started` and one `command_completed` lifecycle, with an aggregated `changed_files` count.

#### Negative
- The event stream is a public contract. Incompatible changes require a new schema version, and consumers must support both during any transition.
- Organization worker events are buffered in memory until each repository result is reported. Relaunched upgrade output is re-encoded through a forwarder that rejects pending data above 8 MiB.
- Consumers must check the process exit status as well as the messages. A broken pipe or termination can leave a truncated stream, and missing messages do not prove that no side effects occurred.
- `metadata_only` fingerprints for generated agent artifacts are not content-integrity hashes, so consumers cannot use them to verify file contents.
- The `update` and `upgrade` commands gain reporter and context plumbing and a hidden `--jsonl-continuation` flag, adding maintenance surface to two heavily used commands.

#### Neutral
- `command_completed.status` reflects exit behavior, not that every best-effort phase succeeded. Consumers that require every phase to succeed must also inspect `warning` and `repository_completed` messages.
- Event order across repositories is not a chronology of remote side effects.
- Output is unchanged when `--jsonl` is absent. The contract is specified in `docs/src/content/docs/specs/update-upgrade-jsonl-specification.md`.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
