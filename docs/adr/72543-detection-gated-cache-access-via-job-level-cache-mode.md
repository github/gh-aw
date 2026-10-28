# ADR-72543: Detection-gated cache access via job-level `cache-mode`

## Status

Proposed in [PR #72543](https://github.com/github/gh-aw/pull/72543).
Adoption is pending repository maintainer review; no decider identities or human approval are asserted by this record.

## Context

An agent executes before threat detection can judge its output. Selecting a restore-only cache action does not restrict the job's scoped cache token: agent-executed code can still call the cache service directly. Preventing publication before detection therefore requires a platform-enforced token boundary, not just a different cache action.

Cache-memory publication must remain possible after detection explicitly accepts the run. Detection in warning mode can succeed as a job while rejecting persistence, so job success alone is not an acceptance signal. Acceptance also does not establish artifact authenticity or turn low-trust inputs into trusted cache content.

GitHub.com supports workflow- and job-level `cache-mode` token policies. GHES compatibility mode cannot assume that support. Trusted workflows also use dependency caches, whose existing save behavior should not be lost merely because threat detection is enabled.

## Decision

Use job-level `cache-mode` on GitHub.com to limit agent cache access while detection is pending, except where trusted dependency-cache saves require the platform's trigger-scoped default token. Without dependency-cache saves, the detection-enabled agent uses `cache-mode: read`. With dependency caches, low-trust events use restore-only actions and inherit the platform's read-only default; trusted events retain combined restore/save actions and the platform's default write access. Runtime event conditions preserve that distinction in mixed-trigger workflows because `cache-mode` accepts literal values, not event-dependent expressions.

A separate cache-memory publisher uses `cache-mode: write-only` and requires both successful prerequisite jobs and explicit detection acceptance (`needs.detection.outputs.detection_success == 'true'`). Missing acceptance, rejected output, or runtime-skipped detection must not authorize publication. Both publisher and dependency-save actions use a fail-closed event allowlist: `push`, `workflow_dispatch`, `repository_dispatch`, `delete`, `registry_package`, `page_build`, `schedule`, and merge-ref-isolated `pull_request`. All other events are restore-only, including issue/discussion events and unknown future events. Reusable workflows use the original caller's runtime event; `workflow_call` by itself grants nothing. Detection acceptance, including the detector's no-output short-circuit, does not justify overriding the platform's low-trust defaults or writing cache lineages restored by higher-trust runs.

GHES compatibility mode omits compiler-generated `cache-mode` fields. The Actions AST preserves authored job-level policies, and the `cache-poisoning` linter warns about explicit `write` or `write-only` overrides on known events outside the write allowlist, including inherited workflow and reusable-caller policies. The actionlint compatibility filter ignores only unsupported-key diagnostics at the supported workflow and job locations, not misplaced keys or malformed values.

### Alternatives considered

- **Restore-only actions alone:** Compatible with GHES and simpler, but insufficient because they do not restrict direct cache writes using the job token.
- **Workflow-level read-only cap:** Restricts the agent, but also prevents the separate publisher from writing accepted memory and disables trusted dependency-cache saves. Job-level policies and trigger-scoped defaults preserve those responsibilities.
- **Publisher gated only on detection job success:** Insufficient in warning mode, where a successful job can still reject persistence. An explicit acceptance output is required.

## Consequences

The GitHub.com agent token cannot write caches while the detection gate is active unless allowed dependency-cache compatibility requires the platform default. Publication requires an explicit acceptance signal and an event from the write allowlist. On eligible runs the separate publisher can write without receiving cache-read access.

Trusted dependency-cache saves are intentionally not detection-gated. A trusted agent with those caches retains direct token write capability before detection runs; restore-only actions would not remove that capability. This is a compatibility tradeoff, not a claim that detection attests cached dependencies.

GHES does not receive scoped-token enforcement from these generated fields. Restore-only actions there are not an equivalent security boundary. Authored custom jobs, third-party cache writers, dependency-cache namespaces, and persistent runner caches require their own review; detection acceptance is not attestation.

Compiler job policies, detection conditions, cache actions, AST validation, lint compatibility, and generated locks must remain consistent. Trusted dependency-cache save behavior must be covered by regression tests. Workflows without detection retain their existing same-job cache-memory behavior. Dry-run compilation omits the publisher and uses restore-only compiler-managed actions, with scoped-token read restrictions only outside GHES compatibility mode.
