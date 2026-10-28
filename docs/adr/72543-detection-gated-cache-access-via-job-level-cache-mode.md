# ADR-72543: Detection-Gated Cache Access via Job-Level `cache-mode`

**Date**: 2026-05-14
**Status**: Draft
**Deciders**: pelikhan (PR author); [TODO: verify reviewers]

---

### Context

When threat detection is enabled, compiled workflows run the agent job before detection has judged its output, and a separate cache-memory publisher job later persists agent memory. Previously the agent job used restore-only cache *actions*, but selecting a restore-only action does not restrict the job's scoped cache token, so agent-executed code could still write to the Actions cache before detection ran. In addition, detection running in warning mode can leave the detection job successful while its verdict rejects persistence, so "detection job succeeded" was not a sufficient gate for the publisher. GitHub.com now supports a `cache-mode` field at workflow and job level that limits the cache token's capability; GHES deployments may not support it.

### Decision

We will use the platform `cache-mode` field as the cache-access boundary for detection-gated workflows. On GitHub.com, the compiler emits `cache-mode: read` on the agent job whenever detection is enabled (for every trigger type, including trusted, mixed, dispatch, and expression-controlled detection), and compiles frontmatter dependency caches as restore-only. The separate cache-memory publisher gets `cache-mode: write-only` and only runs when the agent and detection jobs succeed **and** `needs.detection.outputs.detection_success == 'true'`. GHES compatibility mode omits all compiler-generated `cache-mode` fields. The Actions AST preserves job-level `cache-mode`, and the `cache-poisoning` linter warns about explicit `write`/`write-only` overrides on `pull_request_target`, `issue_comment`, and `workflow_run`, including inherited workflow-level and reusable-caller modes.

### Alternatives Considered

#### Alternative 1: Rely on restore-only cache actions alone

Keep the existing approach of using restore-only cache actions in the agent job. This needs no new platform feature and works on GHES. It was rejected because the action choice doesn't constrain the job's cache token: code running in the agent job can still write cache entries directly, which leaves the cache-poisoning path open.

#### Alternative 2: Workflow-level read-only cap

Set `cache-mode: read` once at workflow level for detection-enabled workflows. This is simpler and covers every job. It was rejected because it would also block the intended cache-memory publisher, which must write after detection accepts the output. The spec explicitly says the compiler MUST NOT impose a workflow-wide read cap that prevents this publication path.

#### Alternative 3: Gate the publisher only on detection job success

Run the publisher whenever the detection job finishes successfully, without checking the `detection_success` output. This is simpler, but in warning mode the detection job can succeed even when its verdict rejects persistence, so rejected output could still be persisted.

### Consequences

#### Positive
- The agent job's cache token is limited to reads on GitHub.com, so agent-executed code can't poison caches before detection runs.
- Persistence requires an explicit detection acceptance (`detection_success == 'true'`), so warning-mode rejections, missing acceptance, and runtime-skipped detection all block publication.
- The linter flags explicit write overrides on low-trust triggers, including inherited modes, so authors notice when they widen cache access.

#### Negative
- The publisher's explicit `write-only` mode deliberately overrides the platform's low-trust read-only default. Its security depends on detection plus input validation, and detection acceptance is not artifact authenticity or attestation.
- GHES deployments get no scoped-token enforcement because `cache-mode` is omitted there, so behavior differs by platform.
- Regenerates around 307 lock files and adds compiler, AST, and linter surface that must stay in sync with platform `cache-mode` semantics.

#### Neutral
- Workflows without detection keep their existing same-job cache-memory behavior. Dry-run omits the publisher and stays read-only.
- Authored custom jobs, third-party cache writers, dependency-cache namespaces, and persistent runner caches stay outside this boundary and need their own review.
- The detector's internal no-output short-circuit still counts as acceptance.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
