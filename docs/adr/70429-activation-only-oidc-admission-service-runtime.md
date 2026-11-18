# ADR-70429: OIDC Activation Admission and Runtime Credit Allowances

**Date**: 2026-10-20
**Status**: Draft
**Deciders**: Unknown (PR #70429, implementing the admission contract introduced in #70286)

---

### Context

The admission service specification (`docs/src/content/docs/specs/admission-service-specification.md`, introduced in #70286) defines a centralized authority consulted before agent execution. The existing daily AI-credit (AIC) guardrail requires preparatory token minting, observation restoration and ledger access. Organizations need centralized admission and runtime credit allowances without per-workflow source changes or forwarding repository API credentials. The runtime must tolerate authority outages, reuse existing agent/detection/evals jobs and proxy enforcement, and fit into compiled workflows and statically computed reusable caller permission envelopes.

### Decision

Run a compiler-owned `admission` step inside activation (`pkg/workflow/compiler_admission.go`), gated by the managed `GH_AW_DEFAULT_ADMISSION_URL` Actions variable. Activation gains `id-token: write` for the start-time admission check. The check follows existing trusted setup and precedes author pre-steps and daily-AIC preparation. Failure fails activation and prevents agent execution; a skipped check leaves fallback behavior intact. Existing prechecks remain prerequisites. This revises the original separate-job design to avoid an extra runner, bootstrap and skipped-ancestor condition handling. The accepted tradeoff is that activation hooks share job-wide OIDC capability even without a configured endpoint.

The completion counterpart runs as a compiler-owned step inside `conclusion`, granting that job `id-token: write` while retaining its reporting and artifact-publication work. After upload and any retry finish, the callback reads the local session through the shared compact projection and takes artifact IDs directly from step outputs. It posts these values to the same authority's `/api/conclusion` route with a fresh token for the same audience and independently reconstructed invocation identity. The projection retains allowlisted metadata, excluding agent messages and unbounded nested payloads: at most 128 records/16 KiB JSONL and 64 KiB for the complete request. The full redacted artifact remains unchanged and can be retrieved through independently verified artifact IDs. No token or request identity crosses jobs through outputs. Failed artifact publication does not suppress compact delivery, but full-session availability remains explicitly unknown. Delivery remains best-effort under either admission outage policy; failures warn without changing completed execution results. The callback has a two-minute step timeout and continues on error. The companion `AdmissionConclusion.tla` model checks ordering, bounded metadata, artifact references, full-artifact preservation, fresh credentials, stable retries and durable deduplicated acceptance.

The client (`actions/setup/js/check_admission.cjs` and `admission_protocol.cjs`) authenticates only with GitHub Actions OIDC, rejects redirects, strictly validates identity and JSON, and makes at most three attempts within one 60-second deadline using shared `withRetry` cancellation and timeout support. It sends caller-owned W3C `traceparent` and compiler-generated `workflow_hash` as an untrusted cache hint. Valid approval replaces daily-AIC validation and its preparatory steps; denial or malformed approval prevents activation. Unconfigured admission and ignored outages retain daily validation. Reusable callers must grant the callee's admission OIDC permission.

Validated admission step results and allowances propagate through activation outputs to the existing AWF budget path. A supplied allowance replaces that component's local limit, including an unlimited setting; missing optional detection/evals allowances preserve independent component policy. Skipped or ignored admission retains existing budgets. Missing required or invalid runtime allowances fail closed. Acquired credentials and request identity stay client-local; budget handoff variables are unset before the agent container starts. Lifecycle logs and collapsed step summaries report outcomes, allowance presence and sources, not credentials, identities, amounts or service diagnostics. The structured unified-session artifact records the validated numeric allowances with admission provenance.

### Alternatives Considered

#### Alternative 1: Run admission in the agent job (or in every downstream job)

Admission could be re-checked immediately before each component runs. This was rejected because it would distribute OIDC capability and change downstream permissions and eligibility. Keeping authentication and the decision in activation allows existing runtime budget inputs to consume validated component allowances without further service calls.

#### Alternative 2: Authenticate with a GitHub App token or forwarded repository credentials

The client could reuse the existing App-token mint pattern (as the daily-AIC guardrail does) or forward `GITHUB_TOKEN` so the authority can query GitHub itself. This was rejected because it hands a reusable credential to an external, separately deployed service. An audience-bound OIDC token proves workflow identity without granting API access and needs no secrets in the repository.

#### Alternative 3: Grant `id-token: write` only when admission is configured

The compiler cannot conditionally generate permissions from runtime Actions variables. Activation and conclusion jobs compiled with admission therefore retain OIDC capability when their endpoint-gated client steps are skipped. Isolating admission in other jobs would avoid this exposure, but adds runners and duplicate bootstrap.

#### Alternative 4: Isolate the completion callback in a separate job

The original design used a separate callback job to avoid granting OIDC to conclusion's reporting/actions. The revised design accepts job-wide OIDC in conclusion, which already rejects author step hooks, to remove another runner, duplicate bootstrap and compact-session job-output handoff. Callback execution still follows artifact publication attempts and remains best-effort.

#### Alternative 5: Run admission alongside the daily-AIC guardrail instead of replacing it

Both checks could always run, with either able to block. This was rejected because it duplicates accounting that the central authority is meant to own, and it keeps the cost of the App-token mint, cache restore, and ledger clone on every admitted run. Keeping daily-AIC as the fallback preserves protection when admission is disabled or ignored during an outage.

#### Alternative 6: Discover completion through polling or `workflow_run` webhooks

This was rejected for this delivery path because it requires separate completion discovery and artifact lookup. The callback delivers bounded metadata and artifact locators directly after publication, without waiting for a workflow-completion event.

#### Alternative 7: Send the full unified session inline

This was rejected because full sessions contain unbounded agent messages and tool payloads. A bounded metadata-only callback keeps request size and disclosure limited while preserving the full artifact for separately authorized retrieval.

### Consequences

#### Positive
- Organizations can configure centralized, cooperative admission across workflows compiled with admission through Actions variables, without recompilation for subsequent runtime configuration changes.
- No reusable credential leaves the runner: the authority receives only an audience-scoped OIDC token, and redirects are refused.
- Admitted runs skip the daily-AIC guardrail and its preparatory steps (App token mint, observation restore and publish, ledger clone), which shortens activation.
- Existing agent, detection and evals jobs and dependencies are reused; only budget inputs and selection change.
- Admission reuses activation's runner and setup, with no separate start-time job or skipped-ancestor conditions.
- The completion callback reuses conclusion's runner and setup and reads the local compact projection without job-output transport.
- The bounded retry/deadline support added to the shared `withRetry` helper can be reused by other callers.

#### Negative
- Activation hooks and actions compiled with admission share job-wide OIDC capability, even when the endpoint is empty. The admission step cannot provide a per-step credential boundary.
- Conclusion reporting and actions compiled with admission share job-wide OIDC capability, including when the endpoint is empty.
- Reusable `workflow_call` callers must grant `id-token: write`. Existing caller envelopes (and the hand-authored Windows probe caller) had to change, and third-party callers that do not grant it will fail.
- Admission remains cooperative, not an unbypassable organization spending cap. Supplied allowances configure existing runtime proxy limits, but deployments still need a separately deployed authority and protected backend policy/accounting.
- When the authority is unavailable, activation can wait up to the 60-second deadline before the outage policy applies.
- The shared `withRetry` helper is more complex (timeouts, absolute deadlines, abortable sleeps, injectable clocks), which affects every caller.

#### Neutral
- `workflow_hash` (frontmatter hash + body hash) is sent as an untrusted cache hint only. The authority must not treat it as authorization.
- Artifact IDs are untrusted locators, not authorization. Full-session retrieval requires independently authenticated GitHub access and verification that each artifact belongs to the authenticated repository, run and workflow invocation/attempt.
- The response contract has no trace or correlation ID. Tracing relies on the caller-provided W3C `traceparent`.
- Most of the original diff is regenerated lock files, and the TLA+ model (`specs/workflow-security/ActivationAdmission.tla`) and its checker were revised to match the admission contract.
- `gh aw env` defaults gain three new keys (`default_admission_url`, `default_admission_audience`, `default_admission_on_outage`), with validation.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
