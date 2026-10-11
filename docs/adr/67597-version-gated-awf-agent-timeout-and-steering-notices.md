# ADR-67597: Version-Gated AWF `agentTimeout` and First-Class Steering Notices in the Unified Session

**Date**: 2026-10-11
**Status**: Draft
**Deciders**: SivaKesava1 (PR author); gh-aw maintainers (review pending)

---

### Context

The agentic firewall (AWF) can steer a running agent by warning it at 80/90/95/99% of its AI-credit budget,
token budget, or runtime deadline. In gh-aw, the runtime (deadline) warnings never fire on the default Docker
runtime, because `container.agentTimeout` is only emitted for the Cloud Hypervisor and NVX runtimes
(`pkg/workflow/awf_config_build.go`). Separately, the per-request `steering` record that AWF writes into its
token-usage stream is dropped by the unified-session projection (`actions/setup/js/unified_session_payload.cjs`),
so `gh aw audit` and `gh aw logs` cannot show which warnings were actually delivered to the agent. Two
constraints shape the fix: AWF's deadline is counted from agent start while GitHub's `timeout-minutes` is
counted from step start, so a naive copy of the step timeout would let AWF's `exit 124` truncate the agent's
final output; and the behaviour depends on an AWF release (v0.28.51) that is not published yet, so it must not
change any currently compiled workflow. Fixes #67596.

### Decision

We will (a) emit `container.agentTimeout` on all runtimes behind an AWF version gate, and (b) promote AWF
steering notices to a first-class, schema-described field of the unified session that flows through to audit
and logs output.

For (a), a new `AWFAgentTimeoutSteeringMinVersion` constant plus an `awfSupportsAgentTimeoutSteering` check
follow the existing AWF feature-flag pattern (`pkg/workflow/awf_feature_flags.go`). `agentTimeout` is derived
from the resolved `timeout-minutes` only when that value is a **literal**; expressions (including the
`GH_AW_DEFAULT_TIMEOUT_MINUTES` default) are skipped with a debug log, and the value is raised to the step
timeout so the GitHub step timeout always fires first. Threat detection gets its own detection-job timeout.

For (b), `firewall.token_usage` (and its `usage.report` alias) retains `steering`, projected to
`{type, threshold}` with unknown or malformed values dropped; `ai_credit_steering` event-log lines map to a
dedicated `firewall.steering` event instead of the generic `firewall.event`. The unified-session spec is bumped
to 1.8.0 with requirement **T-UAS-071** (a missing field means "no notice recorded", not "no notice delivered"),
and a new `steering_notices` list is surfaced in `gh aw audit` and `gh aw logs --json`, with the audit and logs
JSON schemas updated to match.

### Alternatives Considered

#### Alternative 1: Always send `agentTimeout`, including for expression-valued timeouts

We could evaluate or approximate non-literal `timeout-minutes` (for example, defaulting to 20 minutes as the
issue suggested) so every workflow gets runtime steering. This was rejected because the compiler cannot know
the value of `${{ vars.GH_AW_DEFAULT_TIMEOUT_MINUTES }}` at compile time; guessing would either under-shoot the
real step timeout — letting AWF kill the agent before GitHub does, truncating final output — or over-shoot and
make the warnings meaningless. Requiring a literal `timeout-minutes` keeps the deadline provably consistent and
is documented in `sandbox.md`.

#### Alternative 2: Read steering records directly from `token-usage.jsonl` in the CLI

The audit/logs commands could parse AWF's raw `token-usage.jsonl` artifact when rendering, leaving the unified
session untouched. This was rejected because it reintroduces a second, undocumented ingestion path for firewall
data, bypasses the unified-session schema and its versioned contract, and would not be available to any other
consumer of the session artifact. Keeping the projection in `unified_session_payload.cjs` means one producer,
one schema, and one validated shape.

#### Alternative 3: Keep `agentTimeout` restricted to Cloud Hypervisor/NVX and document the gap

The smallest change is to leave the Docker runtime without runtime steering and simply document that timeout
warnings require a microVM runtime. This was rejected because Docker is the default runtime, so the feature
would remain effectively unreachable for most users, and the issue explicitly asks for parity.

### Consequences

#### Positive
- Runtime budget warnings now reach agents on the default Docker runtime, so agents can wrap up work before the
  deadline instead of being cut off.
- Delivered steering notices become visible and queryable in `gh aw audit` and `gh aw logs --json`, closing the
  observability gap between "AWF warned the agent" and "we can prove it".
- The version gate and the unchanged default AWF version (v0.28.50) mean recompiling this repository's workflows
  produces no lock-file changes, so the rollout is inert until AWF v0.28.51 ships.
- The steering shape is schema-described (`unified-session.schema.json`, `audit.schema.json`, `logs.schema.json`)
  and spec-versioned (1.8.0, T-UAS-071), so downstream consumers have a stable contract.

#### Negative
- The feature silently does nothing for workflows whose `timeout-minutes` is an expression, which is the compiled
  default; authors must opt in by writing a literal timeout, a subtlety that is easy to miss despite the docs.
- The behaviour depends on an unreleased AWF version, so the code path is untestable end-to-end until v0.28.51
  is published, and the min-version constant may need correcting if the release slips or renumbers.
- Another AWF feature flag plus a spec version bump adds to the growing version-compatibility matrix that every
  future AWF integration must reason about.
- Notices attached to requests that never produced a usage record (for example a failed upstream call) are not
  reported, so `steering_notices` under-counts in failure scenarios.

#### Neutral
- `pkg/cli/token_usage_subagent_session.go` was refactored to split helpers out of the session reader; this is
  structural only and does not change the reading semantics.
- A new synthetic golden case (`claude-awf-steering-notices`) was added; existing `expected*.json` files are
  unchanged and `logs_expected.json` changes only because it enumerates the new case.
- Runs without a `steering` field serialize exactly as before, so existing artifacts and dashboards are
  unaffected until AWF starts emitting the field.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
