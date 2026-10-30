# ADR-73205: Export Unified Sessions as ATIF-v1.8 via a Reusable Converter and `--atif` Flag

**Date**: 2026-05-08
**Status**: Draft
**Deciders**: pelikhan, Unknown

---

### Context

gh-aw records agent runs as versioned unified session JSONL (`aw_session.jsonl`), which can be retrieved through `gh aw sessions download`, `gh aw logs`, and `gh aw audit`. Users want to bring these recorded sessions into [Harbor](https://docs.harborframework.com/agents/atif), whose tooling consumes trajectories in the Agent Trajectory Interchange Format (ATIF). The unified session format contains evidence that does not map one-to-one to ATIF dialogue: subagent activity, ambiguous or unmatched tool observations, and provenance-scoped tool calls. Any export must stay valid against Harbor's unmodified `TrajectoryValidator` without inventing dialogue, and it must not change how existing download commands behave by default.

### Decision

We will add a reusable `pkg/atif` package that converts one unified session into an ATIF-v1.8 `Trajectory`. Evidence that cannot safely become ATIF steps or tool calls goes into schema-approved `extra` fields rather than being dropped or fabricated. Instead of adding a separate ATIF command or download path, we will expose the conversion through an opt-in `--atif` flag on the existing `sessions download`, `logs`, and `audit` commands. `sessions download --atif` replaces the JSONL output with ATIF. `logs` and `audit` keep their JSONL and reports and write a sibling `.atif` file, using the same session acquisition and reconstruction path that already exists.

### Alternatives Considered

#### Alternative 1: Standalone `gh aw atif` command or separate download path

A dedicated command would keep ATIF concerns isolated and simplify flag interactions. It was rejected because it would duplicate run resolution, artifact download, caching, multi-run handling, and session reconstruction that already live in the session-download commands. The PR description explicitly avoids "a separate download path or standalone ATIF command."

#### Alternative 2: Lossy direct mapping (drop or synthesize unmappable events)

The converter could map only clean request/response/tool pairs and either discard subagent and ambiguous observations or synthesize dialogue or embedded trajectories for them. That would make the output smaller and more "native" ATIF. It was rejected because it loses audit evidence or invents content the agent never produced. Keeping the source records in `extra.unified_event` and `extra.line` keeps the export faithful and traceable to the source.

#### Alternative 3: Convert outside gh-aw (external script or Harbor-side adapter)

Users or Harbor could convert the unified JSONL themselves. This was not chosen because the unified format is versioned and owned by gh-aw. Keeping the converter in-tree lets it change with the format and be tested against fixtures.

### Consequences

#### Positive
- Recorded sessions can be exported for Harbor from the commands users already run. Run IDs, URLs, `--repo`, filtering, caching, and multi-run options work unchanged.
- `pkg/atif` is a reusable library boundary that other callers, such as graders or future exporters, can share.
- Source records and line ordering are kept in extras, so ATIF output can be traced back to the original unified session.
- The output is validated against Harbor's unmodified `TrajectoryValidator` using checked-in fixtures (`pkg/atif/testdata/*.atif`).

#### Negative
- gh-aw now has to track an external schema (ATIF-v1.8). Schema revisions upstream will need converter and fixture updates.
- The `--atif` flag means different things on different commands: on `sessions download` it replaces the output format (and conflicts with `--format markdown`), while on `logs` and `audit` it adds sibling files. This asymmetry has to be documented.
- ATIF output keeps private prompts and tool payloads. It is not a privacy-preserving artifact and must be handled like raw session JSONL.
- Subagent and ambiguous observations stay as extension evidence. Harbor consumers that ignore `extra` will see an incomplete picture of those interactions.

#### Neutral
- Conversion is bounded by a 64 MiB input limit (`MaxSessionBytes`). Larger sessions fail rather than being truncated.
- Reconstructing or normalizing historical sessions can still require Node.js through the existing session-download path.
- Structured non-text content is JSON-encoded into messages and not exported as ATIF media assets.
- New user-facing guidance is in `.github/aw/harbor.md`, and `docs/src/content/docs/setup/cli.md` is updated.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
