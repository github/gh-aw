---
title: Compiled workflow security model
description: Executable, bounded TLA+ specification of compiled workflow trust boundaries and git authorization.
---

# Compiled workflow security model

[`CompiledWorkflow.tla`](CompiledWorkflow.tla) models a compiled agentic workflow
as interacting principals, ordered steps, classified data, artifact handoffs,
scoped credentials, and authorized effects. TLC explores hostile inputs,
detector verdicts, missing git data, and failure schedules. Negative controls
remove individual protections to check that the corresponding invariant can
actually detect a violation.

This is a **bounded, policy-parameterized reference model**, not a proof that every
compiled workflow refines it. The compiled-profile verifier checks a narrow
structural connection to real YAML. It does not verify arbitrary scripts,
external services, or all compiler modes. Its Boolean guard analysis is
conservative, not a complete GitHub Actions expression evaluator.
The initial architecture review found no confirmed exploit; the subsequent
corpus trial exposed fail-open credential cleanup, now corrected with blocking
cleanup and independent postcondition verification.
Synthetic counterexamples must not be filed as product vulnerabilities.

## State and execution

The workflow has activation, agent, detection, and safe-output jobs. Each has a
status, dependencies, effective grants, and a step cursor. `Start`, `Finish`,
`Fail`, and `Skip` represent job lifecycle. Host checkout/setup steps are not the
sandboxed agent principal, even when GitHub Actions runs both in the same job.

| Surface | State / transitions | Meaning |
|---|---|---|
| Configuration | `context`, `Activate`, `TrustedConfiguration` | Trusted activation instructions; PR-base provenance is an implementation obligation. |
| Untrusted data | `AgentRequest`, `requestValid`, `target`, `request.private` | Declarative operation, schema validity, repository selection, and private-source classification remain separate from artifact origin. |
| Jobs and steps | `status`, `step`, `started`, `safeOutputResult`, `appTokenPost`, `Dependencies` | Ordered host setup, agent execution, detection, validation, credential minting, effects, and the app-token action post step. |
| Status-only custom job | [`StatusOnlyJob.tla`](StatusOnlyJob.tla) | A separate bounded model checks that an opted-in leaf status job waits for detection to finish, can proceed after failure or warn-mode findings, and satisfies the no-agent-data contract. The abstract capability flags do not verify the Actions expression parser or validator implementation. |
| Artifacts | `artifacts`, `Consume`, `GoodOrigin` | Existence, producing job, run and invocation identity, naming prefix, instruction trust, secret/private labels. Current-run agent artifacts are still untrusted payloads. |
| Outputs and logs | `transfers` | Artifact, output, and log channels carry independently tracked secret labels. This abstracts covered redaction, not arbitrary encoded-secret detection. |
| Permissions | `grants`, `TokenPermissions`, `ValidatedEffects` | Repository read, issue write, content write, and inference capabilities are distinct. Workspace edits are not repository-resource writes. |
| Apps and secrets | `live`, `revoked`, `appScope`, `appRepos`, `agentSecrets` | Read-only checkout credentials, authorized engine credentials, and privileged installation tokens have different consumers and lifetimes. |
| Networking | `ToolCall`, `egress`, `NetworkPolicy` | Authorized GitHub/inference services, blocked destinations, and service-bound engine authentication. |
| Git | `checkouts`, `cleanup`, `agentBegan`, `privilegedCheckout`, `operations`, `errors` | Per-repository auth, temporary setup credentials, verified cleanup/failure, available blobs/refs, sparse/shallow state, local operations, REST, and privileged push. |
| Runtime repository-list checkout | [`RuntimeRepositoryCheckout.tla`](RuntimeRepositoryCheckout.tla) | Trusted policy, bounded whole-batch validation, visibility, entry authentication, agent/privileged finalization, job-local manifests, staged isolation, and failure reporting. |
| Pull-request fork admission | [`PullRequestForkAdmission.tla`](PullRequestForkAdmission.tla) | Same-repository default, repository-pattern or explicit author-association admission, non-PR trigger preservation, and permission independence. |
| `pull_request_target` checkout policy | [`PullRequestTargetCheckout.tla`](PullRequestTargetCheckout.tla) | Strict warning acknowledgment, explicit checkout disablement, trusted base checkout, exact allowlisted external checkout, and fail-closed `fetch` handling. |
| PR checkout authorization | [`CommentTriggeredPRCheckout.tla`](CommentTriggeredPRCheckout.tla) | Same-repository Bot branch-event authorization for supported PR actions; `labeled` and other unlisted actions retain the collaborator permission floor, as does the stricter allow-listed comment/review exception. |
| Patch/bundle application | [`SafeOutputTransport.tla`](SafeOutputTransport.tla) | Captured bytes, trusted transformations, private snapshots, publication retries, sticky failures, bounded local checking cost, and finally cleanup. |
| Bundle Git operations | [`SafeOutputGit.tla`](SafeOutputGit.tla) | Explicit prerequisite recovery, offline optional discovery, bounded comparison/history work, credential roles, and separate size/protection provenance. |
| Host-side MCP compilation | [`MCPCompile.tla`](MCPCompile.tla) | Startup write capability, actor authorization, early option rejection, filesystem isolation, cached-only imports, repository-root cache selection, validation, and write-enabled emission. |
| Cooperative activation admission | [`ActivationAdmission.tla`](ActivationAdmission.tla) | Optional endpoint, isolated admission/conclusion OIDC capabilities, verified identity, retry/outage classification, independently resolved runtime allowances, privacy, and caller-owned trace correlation. |
| Admission conclusion delivery | [`AdmissionConclusion.tla`](AdmissionConclusion.tla) | Artifact-first compact delivery, hard payload bounds, upload-bound artifact references, full-artifact preservation, fresh OIDC, stable retries, outcome-preserving warnings, and durable deduplicated acceptance. |

The `issue` effect creates at most one validated issue in the main public
repository. The `pull-request` effect prepares a full checkout in the privileged
job and pushes a local change with the scoped installation token. Both reject
private-source content at the public sink under this strict profile.

Detection may run after **agent failure**, and valid already-emitted requests
may still be processed after successful detection. This follows the emitted
`needs.agent.result != 'skipped'` policy rather than assuming all upstream jobs
must succeed. Failure before request publication causes origin validation to
fail. Agent failures revoke the modeled engine credential. A minted installation
token remains live until the `actions/create-github-app-token` post step, which
revokes it after either safe-output success or failure.

## Pull-request fork admission

[`PullRequestForkAdmission.tla`](PullRequestForkAdmission.tla) models the
compiler-generated `pull_request` admission condition. Same-repository PRs are
always admitted; fork PRs are admitted only by a matching configured repository
pattern, wildcard, or explicit author-association. Omitted configuration and an
empty association list deny forks, while non-`pull_request` triggers are
unaffected. The author-association filter changes job admission only: the model
does not grant permissions, expose secrets, or establish that an author is
otherwise trustworthy.

Run the bounded model with the pinned TLA+ Tools installation:

```bash
java -cp /path/to/tla2tools.jar tlc2.TLC \
  -config specs/workflow-security/PullRequestForkAdmission.cfg \
  specs/workflow-security/PullRequestForkAdmission.tla
```

The `Fault` constant provides negative controls for default fork admission,
association matching, same-repository admission, non-PR filtering, and
permission escalation.

`DetectionPolicy` independently declares required, disabled, or conditional
detection. `DetectionEnabled` selects a conditional run. Required detection
cannot be bypassed merely by omitting the job. Disabled/conditionally skipped
configurations retain authorization, provenance, credential, and validation
invariants but do not claim detector approval. The inactive detector is
represented by a skipped state-machine slot, not a real runtime job.

## Cooperative activation admission

[`ActivationAdmission.tla`](ActivationAdmission.tla) formalizes the
[admission service contract](../../docs/src/content/docs/specs/admission-service-specification.md),
including the implemented client's daily-validation precedence, optional
component allowances, and admission/conclusion OIDC capability isolated from activation. It is a bounded reference model, not a proof of the runtime
or deployment-owned authority. Admission runs in an endpoint-gated job.
Activation requires successful or intentionally skipped admission; a successful
activation is required before agent execution. Independent OIDC features are
outside this admission-only permission model.

The finite model covers 2,304 combinations of effective endpoint
(absent/valid/invalid), verified invocation identity (valid/invalid), triggering
actor use (unused/independently verified/unverified), local cap
(disabled/stricter/equal/looser), existing or generated caller trace context,
independently optional threat-detection and evals allowances, resolved daily
fallback (allow/block), and hidden spend band.
Four checker profiles select outage `ignore`/`block` and client OTel export enabled/disabled.
Each admission has up to three attempts, two symbolic authority replicas,
symbolic caller trace IDs, and policy allowances of two credit units. Optional
allowances use zero only as an internal absence marker, never a valid wire value.
Validated primary and supplied component allowances replace their runtime limits.
Missing optional allowances and skipped or ignored admissions retain local limits.
Request identity and credentials are not handed off; component accounting is unchanged.

| Contract | Checked invariants |
|---|---|
| Optional configuration, isolated OIDC capability and activation dependency | `NoEndpointNoRequest`, `InvalidConfigurationBlocks`, `AdmissionJobIsolation`, `AdmissionDependency`, `ActivationGate` |
| OIDC-only authentication, verified claim/workflow mapping, independently checked rerun initiator, original actor privileges | `OIDCOnly`, `IdentityVerified`, `TriggeringActorVerified`, `OriginalActorPrivileges`, `InvocationBound` |
| Lost responses, replica changes, token-renewal abstraction, bounded retries, denial/protocol errors, explicit outage policy | `StableRetryDecision`, `RetryBound`, `DenialBlocks`, `MalformedBlocks`, `FatalErrorsBlock`, `OutagePolicyHonored` |
| Terminal deadline expiry, approved admission bypassing daily validation, daily fallback required for skip/ignored outage | `DeadlineHonored`, `DailyAdmissionPrecedence`, `DailyFallbackEnforced` |
| Validated primary/optional allowances, runtime budget selection, fallback on omitted optional allowances or skip/outage, and no identity/credential handoff | `CeilingValidated`, `RuntimeBudgetsResolved`, `NoFabricatedAdmission` |
| Empty durable-denial observation, spend-independent policy allowance, financially side-effect-free admission | `PrivateDenial`, `SpendPrivateCeiling`, `NoFinancialEffects` |
| Required caller `traceparent`, preserved existing/generated context, correlated admission export without forcing export | `TraceparentRequired`, `CallerTracePreserved`, `CorrelationPreserved`, `OptionalExport` |

`Termination` checks eventual completion under weak fairness of protocol
progress. HTTP/OIDC deadlines, backoff, cancellation delivery, and agent completion
are abstract atomic steps, not a verification of the
60-second end-to-end deadline or minimum
decision-retention duration. A transient result can occur on any attempt; the
environment need not eventually return an approval. Deadline expiry may terminate the sequence before all three
attempts and excludes late responses and further retries. Policy revocation can
return the same uniform denial after a cached approval.

The checker includes 29 synthetic single-guard faults and 18 reachability
witnesses. Witnesses demonstrate admitted, skipped, and fail-open agent starts;
lost-response recovery through another replica; a verified rerun initiator;
admission with request trace context but no client export; existing and generated
caller traces; all four optional-allowance combinations; terminal denial;
admission despite a blocking daily result; daily blocks after skipped
admission and ignored outages; early timeout fallback; and an admission job
failing after approval still preventing activation.
In particular, skipped and fail-open starts deliberately demonstrate
why this is **cooperative admission, not unbypassable organization policy or a
hard spending cap**. The model consumes already-resolved runtime settings;
repository-variable shadowing and an editor removing the job remain outside
the participating-workflow machine.

Use the same checksum-pinned TLA+ Tools v1.8.0 described below:

```bash
TLA2TOOLS_JAR=/path/to/tla2tools.jar JAVA_BIN=/path/to/java \
  python3 specs/workflow-security/check.py --model admission \
  --results /tmp/activation-admission-results
```

The default `check.py` run also includes admission and its companion conclusion model. Results retain model/config
hashes, effective profile settings, state counts, logs, and normalized
counterexample/witness traces. Secure profiles must exhaust without error;
each negative control or witness must violate exactly its named invariant
with TLC exit code 12. Tooling and parser failures never satisfy a control.

This model does not prove JWT cryptography, issuer/JWKS trust, HTTP/TLS or JSON
parsers (including integer wire spelling and Unicode scalar validity),
URL/redirect safety, GitHub App provisioning or installation-token
revocation, distributed cache correctness/expiry or bounded replay-state cleanup,
randomness, logging redaction, timing noninterference, or actual engine billing
enforcement. `identity = "valid"` assumes all required
signed claims, executing workflow-reference bindings, unambiguous invocation
identity, and any logical-policy mapping have been independently verified;
`triggeringActor = "verified"` assumes the specified authoritative run-attempt
lookup. Trace IDs are symbolic, not entropy proofs. Two hidden spend classes
check explicit response-field constraints, not full observational
noninterference: eligibility itself is an intentional disclosure. Allowances
are activation metadata, not a reservation protocol or a guarantee that concurrent
invocations cannot overshoot an organizational budget.
The daily fallback result is explicit; other activation checks are assumed satisfied.
Telemetry samples admission trace export without introducing a relay or downstream
metadata handoff. Equality of actual generated agent jobs and strict HTTP response
shapes require implementation-level tests.
The request's compiled frontmatter/body workflow hash is a cache hint, not a new
authorization input in this model. Hash derivation, trusted content binding,
and isolation of authority-side content caches require implementation tests;
matching hashes never replace the modeled invocation/identity checks.
Retry/token renewal and replicas reuse one abstract verified invocation and
shared decision store; actual concurrency, foreign invocation partitioning,
retention, and revocation require implementation-level tests. Audience claim
profiles, canonical run/attempt parsing, and completed-decision policy/usage
revalidation are also implementation obligations, not separate model dimensions.
Neither the compiler nor an authority implementation has been shown to refine
this model.

## Admission conclusion delivery

[`AdmissionConclusion.tla`](AdmissionConclusion.tla) and
[`AdmissionConclusion.cfg`](AdmissionConclusion.cfg) model completion independently
of start-time admission, avoiding a product of unrelated protocol states.
The eligible conclusion job consumes a symbolic completed agent outcome and a
valid or invalid redacted unified-session file; an isolated callback job handles
authority delivery after it finishes. A skipped conclusion job makes no upload
or authority request.

For an eligible job, generation precedes the first artifact upload. A failed
upload receives one retry, and both attempts must settle before callback
preparation, credential acquisition or POST. Compact metadata delivery remains
reachable even when both artifact attempts fail. The model includes absent,
valid and invalid endpoints; valid/invalid independently verified identity;
admitted and ignored admission results; and success, failure and cancellation.
Both admission outage policies are checked without allowing completion failures
to change the captured outcome.

| Contract | Checked invariants |
|---|---|
| Artifact-first order, existing job eligibility, absent endpoint and invalid session/configuration | `ArtifactBeforeConclusion`, `JobEligibility`, `NoEndpointNoRequest`, `InvalidInputNoRequest` |
| Fresh isolated callback-job OIDC, independently verified identity, compact metadata, hard request bounds and unchanged retries | `ConclusionOIDCOnly`, `InvocationVerified`, `CompactProjection`, `BoundedPayload`, `StablePayload` |
| Upload-bound artifact locators and unchanged full session evidence | `ArtifactReferences`, `FullArtifactPreserved` |
| Three-attempt maximum, terminal deadline, preserved workflow outcome and caller trace | `RetryBound`, `DeadlineHonored`, `BestEffort`, `TraceCorrelation` |
| Durable acceptance before acknowledgement, deduplicated processing, immutable accepted payload and no billing mutation | `DurableAcceptance`, `SingleProcessing`, `AcceptedSessionImmutable`, `NoFinancialEffects` |

The checker runs two secure profiles, nineteen single-guard negative controls
and eight reachability witnesses. Witnesses cover acceptance, lost-acknowledgement
recovery through another replica, successful delivery after failed artifact
publication, delivery after agent failure or ignored admission, rejected
conflicting replay, and a warning after deadline expiry.
`Termination` requires eventual protocol completion under weak fairness;
token/HTTP deadlines and upload completion are abstract atomic transitions.

`--model admission` includes both admission models; use `--model conclusion`
to run only this companion:

```bash
TLA2TOOLS_JAR=/path/to/tla2tools.jar JAVA_BIN=/path/to/java \
  python3 specs/workflow-security/check.py --model conclusion \
  --results /tmp/admission-conclusion-results
```

The compact snapshot is a symbolic metadata-only projection with a bounded byte
count (one unit represents one KiB); size 65 is an explicit over-limit fault.
Successful uploads supply symbolic artifact IDs; a failed step may also retain
an observed ID if it failed after publication. The callback must snapshot exactly
the available IDs, never foreign or omitted IDs. The
full artifact remains `full-redacted` while only `compact-redacted` metadata may
enter the callback. A witness verifies successful delivery with artifact locators;
delivery without any observed upload IDs instead carries an empty locator set.
These abstractions specify the boundary, not a proof of UTF-8 byte arithmetic.

Session bytes, schema validation, issuer/audience verification, redirect handling,
redaction and tenant partitioning are implementation obligations, not proven by
symbolic payloads. The store is one atomic record for one verified invocation;
the model specifies durable idempotency, but does not prove a deployment's
distributed transaction, storage retention or replay cleanup. It does not prove
that GitHub will schedule a conclusion job after abrupt cancellation, nor that
artifact publication succeeds. Compiler tests separately check generated
generation/upload/retry/callback ordering for ordinary and work-queue workflows.

## Evidence and invariants

Reviewed baseline: `542e937dd8447172c8c484cda3a9a5716ae99245`.
The authorities are [Security Architecture v1.1.0](../security-architecture-spec.md),
[Compiler Threat Detection v1.0.54](../compiler-threat-detection-spec.md),
[Checkout Behavior](../../docs/src/content/docs/specs/checkout-behavior-specification.md),
and the [Safe Outputs specification](../../docs/src/content/docs/specs/safe-outputs-specification.md).
Line references describe that baseline; symbols are the more durable mapping.
The checkout specification's 1.3.1 amendment requires verified cleanup before
agent execution, including the failure paths found by the corpus trial.

### `pull_request_target` checkout policy

[`PullRequestTargetCheckout.tla`](PullRequestTargetCheckout.tla) is a separate,
bounded model of compiler rule CTR-008. It exhaustively checks the finite
combinations of trigger, strict mode, warning acknowledgment, checkout class,
and fetch class. The successful external allowlist class represents an exact
literal repository/ref pair; expressions, wildcard/PR refs, wiki checkouts,
omitted external refs, and unmatched pairs have distinct untrusted classes.
Any configured `fetch` makes a configured checkout untrusted. A `Fault`
constant provides negative controls for acknowledgment or fetch bypassing
checkout authorization.

Run the baseline model with the same pinned TLA+ Tools installation:

```bash
java -cp /path/to/tla2tools.jar tlc2.TLC \
  -config specs/workflow-security/PullRequestTargetCheckout.cfg \
  specs/workflow-security/PullRequestTargetCheckout.tla
```

The baseline exhausts 400 input combinations. To verify either negative
control, copy the `.cfg` to a temporary file, set `Fault` to
`"acknowledgment-bypasses-checkout"` or `"fetch-bypasses-checkout"`, and
confirm TLC reports `NoUntrustedCheckoutAcceptance`. This model specifies the
compiler decision boundary; it does not model Git's parser or prove safety of
the contents at an otherwise trusted mutable ref.

## Safe-output transport lifecycle

[`SafeOutputTransport.tla`](SafeOutputTransport.tla) supplements the job-level
model with the runtime contract in Safe Outputs
[PTI-001–007](../../docs/src/content/docs/specs/safe-outputs-specification.md#patch-and-bundle-transport-integrity-requirements).
It separates original bytes/identity, captured baselines, trusted transformed
bytes, private snapshots, applied bytes, and publication-time verification.
Resolution requires producer/consumer agreement on the qualified repository
and a single qualified or legacy namespace for the patch/bundle pair; ambiguity
fails before capture. Namespaces abstract exact shared filename derivation;
real-Git producer/consumer round trips cover bare, padded, implicit, overridden,
and fork-backed target repository cases.
One adversarial mutation may change bytes, identity, regular-file status, or
an existing snapshot between boundaries; FIFO replacement also occurs before
capture. Rate-limited
publication can retry once; each attempt rechecks integrity.

The patch profile registers an abort after conflicting application; the bundle
profile registers a temporary ref. Both complete through finally cleanup.
Cleanup may itself fail, but snapshots must still be disposed and the failure
reported. An already completed remote effect is not rolled back if a later
completion check fails. Diagnostic safety and zero full-artifact hashes around
an unrelated local command are abstract obligations, not measurements of Node.

Run both transport baselines and the Git-operation profiles below, including
all negative controls and reachability witnesses, with
the pinned TLA+ runtime described below. The runner rejects incorrect tooling
status or invariant names and automatically removes temporary configurations,
TLC states, and traces:

```bash
TLA2TOOLS_JAR=/path/to/tla2tools.jar JAVA_BIN=/path/to/java \
  python3 specs/workflow-security/check_transport.py
```

`SafeOutputTransport.cfg` defaults to `Transport = "patch"`. The runner also
checks `Transport = "bundle"`. For each synthetic negative control, it changes
only `Fault` and requires TLC exit code 12 with the corresponding invariant:

| Fault | Expected invariant |
|---|---|
| `unqualified-producer` | `QualifiedRepository` |
| `ambiguous-selection` | `UnambiguousResolution` |
| `blocking-open` | `NonblockingOpen` |
| `reopen-original` | `StableConsumption` |
| `rebaseline` | `OriginalBaseline` |
| `skip-publication-check` | `PublicationIntegrity` |
| `retry-integrity` | `PermanentFailure` |
| `cleanup-blocked` | `CleanupDisposition` |
| `cleanup-error-hidden` | `CleanupDisposition` |
| `unsafe-diagnostics` | `SafeDiagnostics` |
| `redundant-hashing` | `BoundedLocalCost` |

With `Fault = "none"`, append each of `NoSuccessfulWrite`,
`NoRejectedMutation`, `NoRateLimitRetry`, `NoCleanupFailure`, and
`NoConflictCleanup` as an invariant to obtain positive reachability witnesses
for publication, rejection, retry, surfaced cleanup failure, and conflict
cleanup, respectively. These deliberately false predicates and synthetic
faults are not product vulnerabilities.

The bounded model assumes snapshot privacy, collision-free byte classes,
trusted transformations and exact registered-cleanup dispatch. It does not
model arbitrary same-user filesystem races, Git parsing, App API semantics,
unbounded retries, credential selection, or the full structured logging
payload. Publication is atomic in this abstraction; post-await checks before
auto-merge, older-PR closure, or coding-agent assignment and exact-HEAD rewrite
rollback remain implementation obligations covered by
`transport_integrity.test.cjs`, `transport_integrity_handlers.test.cjs`,
`transport_integrity_git.test.cjs`, `error_recovery.test.cjs`, and
`create_or_update_pull_request.test.cjs` under `actions/setup/js/`.
The existing `check.py` and its checked-in examples cover the job and runtime
repository-list checkout models; `check_transport.py` checks transport and Git
operations separately. Use `--model transport` or `--model git` to select one.

## Safe-output bundle Git operations

[`SafeOutputGit.tla`](SafeOutputGit.tla) models the unsigned push-to-PR bundle
path in Safe Outputs
[BGO-001–008](../../docs/src/content/docs/specs/safe-outputs-specification.md#bundle-git-operations-and-validation-baselines).
Prerequisite recovery and optional baseline discovery are distinct stages.
Missing prerequisites fail by default; only trusted literal opt-in permits
recovery with the validated head-repository credential. Metadata discovery uses
the target checkout's credential instead, and never imports remote objects.
The recovery flag never authorizes downloads during optional discovery.

An exact pinned tip merged locally bypasses REST. Otherwise, bounded parent
discovery compares against that pinned tip. Only `behind`/`identical` establishes
an ancestor size baseline; `ahead` and candidate-specific 404 can continue
within the request budget. Authorization, rate-limit, service, transport, timeout,
and unknown failures stop comparison discovery. An inconclusive exact-tip
ancestry probe can still enter bounded parent discovery without downloading
the tip. Missing required local data, unsupported operations, time/output
limits, history overflow, and budget exhaustion fall back to PR-head validation
when the remaining checks cannot establish a baseline.
A successful validation tree is required before an exemption is used. A
non-tip ancestor never exempts protected history, including A-add/B-remove or
reverted changes, even when it reduces size accounting. Exact-tip exemptions
still do not excuse agent-authored protected changes.

The runner exhausts all combinations of `AllowFullHistoryFetch` and `Shallow`.
It requires exhaustive success for these four secure profiles, then the exact
named counterexample for each synthetic fault:

| Fault | Expected invariant |
|---|---|
| `implicit-recovery` | `RecoveryOptIn` |
| `optional-fetch` / `lazy-fetch` | `NoOptionalMaterialization` |
| `wrong-recovery-token` | `RecoveryAuthentication` |
| `wrong-metadata-token` | `MetadataAuthentication` |
| `unbounded-local` | `BoundedLocalInspection` |
| `unbounded-history` | `BoundedHistory` |
| `unbounded-comparisons` | `BoundedComparisons` |
| `retry-global-error` | `NoGlobalFailureRetry` |
| `comparison-retry` | `NoComparisonRetries` |
| `unbounded-request` | `BoundedRequests` |
| `ancestor-exemption` | `ProtectionProvenance` |
| `unsafe-diagnostics` | `SafeDiagnostics` |

The wrong-recovery-token control runs with opt-in enabled; the others use the
default-deny shallow profile. Secure profiles additionally check shallow
preservation, exact-tip zero-comparison behavior, conservative fallback, and
protected-change rejection before publication. With `Fault = "none"`, the runner
appends each of `NoAncestorBaseline`, `NoExactTipBaseline`, `NoRecovery`,
`NoConservativeFallback`, `NoSafePublication`, `NoProtectedRejection`, and
`NoMissingParentContinuation`. These false invariants witness effective ancestor
and exact-tip baselines, successful opted-in recovery, fallback, valid
publication, protected-history rejection, and a request following a 404,
respectively. They prevent success from consisting only of rejected operations.

```bash
TLA2TOOLS_JAR=/path/to/tla2tools.jar JAVA_BIN=/path/to/java \
  python3 specs/workflow-security/check_transport.py --model git
```

`SafeOutputGit.cfg` matches the implementation's four-comparison budget and
100-record history limit. Finite boundary classes exercise both the 100th and
101st records, and the over-budget fault attempts a fifth comparison.
The implementation also uses a 5-second deadline per optional Git
command/request and 1 MiB per process output stream.
The model abstracts elapsed time/output into finite success, missing-data,
timeout, and overflow outcomes. It checks decision boundaries, not real time,
process termination, byte counts, Git parsing/DAG correctness, merge correctness,
actual credential grants or restoration, signed delivery, ordinary PR-head
fetch/push operations, or implementation refinement. Pinned refs, local ancestry
answers, REST reachability responses, and protected-history classifications are
assumed accurate inputs to the abstraction. The job-level model covers
credential lifetime; runtime conformance for exact thresholds, subprocess
termination, abort signals, and shallow/partial-clone preservation remains in
`push_to_pull_request_branch_comparisons.test.cjs`,
`push_to_pull_request_branch.integration.test.cjs`, and
`bundle_transport.integration.test.cjs` under `actions/setup/js/`.

## Runtime repository-list checkout

[`RuntimeRepositoryCheckout.tla`](RuntimeRepositoryCheckout.tla) refines the
checkout and safe-output boundary in RCR8-RCR15 of the
[Safe Outputs specification](../../docs/src/content/docs/specs/safe-outputs-specification.md).
It follows one entry through compilation, preflight, metadata validation,
checkout, finalization, manifest publication, consumer execution, or trusted
failure reporting. Each run chooses the agent or privileged job; both derive
paths from the same canonical repository/prefix function rather than sharing
an agent-writable manifest. Only literal `multi: true` enters the list pipeline.
Omitted/false mode permits single-repository expressions without list policy
and validates exactly one slug before reaching the static-checkout boundary;
static checkout execution remains covered by the original model.

Finite input classes include trusted literal/configuration-variable policies,
caller/composite policies, missing policies, scoped matching sets and unrestricted
policies, explicit multi opt-in, single expressions with list-only features
unrestricted, explicit/wildcard selectors, empty and case-duplicate lists, canonical
redirects, unverifiable metadata, public/private/internal hosts and targets with
and without explicit private-repository opt-in,
invalid prefixes, unsafe target paths, LFS/submodule settings, and staged mode.
The bounded lists use three repositories and limits 1, 2, and the production
default 10. Token classes distinguish entry PAT/read-App/write-App overrides,
representative optional-key fallbacks, effective operation/shared credentials,
and an unrelated entry's scoped App token. Cleanup can succeed or fail.
Safe-output write authorization is independent of checkout authorization.
Token names track credential provenance, not permission grants; `job-default`
is the current job's GitHub token, whose grants differ between job kinds.

| Predicates | Contract / implementation connection |
|---|---|
| `ExplicitMultiMode`, `SingleRepositoryOnly`, `MandatoryAllowlist`, `PolicyProvenance`, `ExplicitSelector`, `ScopedPolicy` | RCR8 / CTR-033; explicit `Multi` classification, [`validateSingleRepository`](../../actions/setup/js/checkout_dynamic.cjs), [`validateCheckoutAllowedRepos`](../../pkg/workflow/checkout_dynamic.go), [`parseAllowedRepos`](../../actions/setup/js/checkout_dynamic.cjs), and compiler policy regressions. |
| `SelectedRepositoriesAllowed`, `BoundedSelection`, `BatchValidatedBeforeCheckout`, `ValidatedPathsSafe`, `CanonicalRepositoriesAllowed`, `VisibilityIsolation`, `PrivateRepositoryOptIn` | RCR9; [`prepareCheckouts`](../../actions/setup/js/checkout_dynamic.cjs) validates the complete batch, requires explicit opt-in for non-public targets, and never permits them on public hosts. |
| `EntryAuthentication` | RCR11; [`dynamicCheckoutToken`](../../pkg/workflow/checkout_dynamic.go) and [`prepareSharedPRCheckout`](../../pkg/workflow/compiler_safe_outputs_steps.go) preserve explicit entry credentials and resolve the shared fallback independently of neighboring Apps. |
| `NoSecondaryReads` | RCR12; generated actions disable LFS/submodules and [`finish`](../../actions/setup/js/checkout_dynamic.cjs) explicitly disables recursive submodule fetching. |
| `AgentCredentialsCleared`, `PrivilegedCredentialsRetained` | RCR13; agent finalization cleans/verifies actual Git configurations before engine execution; privileged finalization retains write authentication. |
| `ManifestPrecedesConsumers`, `SafeOutputAuthorization` | RCR6/RCR10; job-local manifest generation follows finalization, and checkout authorization never expands a handler's write allowlist. |
| `StagedIsolation`, `CheckoutFailureReported` | RCR14/RCR15; preview mode omits checkout dependencies, and known checkout failures remain reportable even without recorded agent jobs. |

The existing checker runs this model, its 23 deliberate guard mutations, and
eight reachability witnesses alongside `CompiledWorkflow`. Witnesses establish
that runtime checkout, deep-prefix handler lookup, known failure reporting,
single expressions without an allowlist, single-checkout LFS/submodule/private
access without list restrictions, the hosting-repository case, and opted-in private/internal-host checkouts are
reachable; the safety properties therefore do not pass by rejecting everything.
The mutations are hypothetical broken guards, not confirmed product bugs.

This is a bounded contract model, not compiler/runtime refinement or a liveness
proof. Policy matching is represented by finite sets, API visibility and
canonicalization by response classes, and configuration cleanup by a verified
success/failure result. It does not prove expression syntax, wildcard semantics,
the 9007199254740991 parser bound, filesystem/symlink enforcement, Git behavior,
installation-token grants, arbitrary independent tooling, or multi-entry merge
semantics. Existing compiler and runtime tests supply those narrower conformance
checks; the real-Git `.gitmodules` regression is not replaced by this abstraction.

## Pull request checkout authorization

[`CommentTriggeredPRCheckout.tla`](CommentTriggeredPRCheckout.tla) exhaustively
models `pull_request` and `pull_request_target` actions `opened`, `synchronize`,
`ready_for_review`, `reopened`, `labeled`, and other unlisted actions, as well as `issue_comment`,
`pull_request_review_comment`, and `pull_request_review`. The branch-event
exception requires a Bot sender whose login exactly matches the runtime actor
and matching positive runtime, PR-head, and PR-base repository identities. The
comment/review exception separately requires an allow-listed Bot sender, exact
equality between the webhook sender and matching comment or review author,
canonical equality between the runtime actor and sender, and matching
repository identities. Every other case retains the write-or-higher
collaborator permission check. The finite model includes App slug and `[bot]`
spellings, non-Bot actors, mismatched identities, missing or mismatched
repository identities, unsupported actions and triggers, and a `Fault` control
for bypassing each authorization guard. Runtime conformance vectors live in
[`checkout_pr_branch.test.cjs`](../../actions/setup/js/checkout_pr_branch.test.cjs).

Run the model with the same pinned TLA+ Tools installation:

```bash
java -cp /path/to/tla2tools.jar tlc2.TLC \
  -config specs/workflow-security/CommentTriggeredPRCheckout.cfg \
  specs/workflow-security/CommentTriggeredPRCheckout.tla
```

For each negative control, copy the `.cfg` to a temporary file, set `Fault` to
one of `"event-bypass"`, `"sender-type-bypass"`, `"author-bypass"` (which covers
comment and review author mismatches), `"actor-bypass"`, `"allowlist-bypass"`,
`"repository-bypass"`, `"branch-event-bypass"`, `"branch-action-bypass"`,
`"branch-sender-type-bypass"`, `"branch-sender-bypass"`, or
`"branch-repository-bypass"`, and confirm TLC reports
`NoUnauthorizedBotBypass`.

| Predicate | Required safety condition | Architecture / threat rule | Compiler/runtime evidence |
|---|---|---|---|
| `TypeOK` | Every modeled job, token, checkout, channel, and request has a valid shape. | Compiler typed configuration and runtime request contracts | [`WorkflowData`](../../pkg/workflow/workflow_data.go), [`Job`](../../pkg/workflow/jobs.go), [`safe_output_validator`](../../actions/setup/js/safe_output_validator.cjs). |
| `JobIsolation` | Agent has no repository-write grants; generated prerequisites cannot be bypassed. | A:389–398 OI-01/02; CTR-001/005 | [`validateDangerousPermissions`](../../pkg/workflow/dangerous_permissions_validation.go):23–77; [`guardIfAgainstStatusFuncBypass`](../../pkg/workflow/compiler_builtin_job_augmentation.go):391–423. |
| `NoCredentialPersistence` | No retained checkout credential is accessible once the untrusted agent begins. Trusted force-clean setup may temporarily retain credentials. | K:192–205, T-CHK-018; AR1 | [`generateCheckoutCredentialsCleanupStep`](../../pkg/workflow/checkout_step_generator.go); [`verify_git_credentials.sh`](../../actions/setup/sh/verify_git_credentials.sh). |
| `ArtifactProvenance` | Accepted requests come from this run and this invocation's agent artifact. | AR2; OI-01/02 | [`generateUnifiedArtifactUpload`](../../pkg/workflow/compiler_yaml_artifacts.go):31–61; [`buildSafeOutputsDownloadSteps`](../../pkg/workflow/compiler_safe_outputs_job.go):246 onward. Payload validation remains necessary. |
| `DetectionGate` | When declared policy requires a detector for this run, modeled approval precedes every effect. | A:755–759,793–797; WTD1–3 | [`buildSafeOutputsJobCondition`](../../pkg/workflow/compiler_safe_outputs_job.go):859–881; [`processMessages`](../../actions/setup/js/safe_output_handler_manager.cjs):877–963. Structural checks prove job-result gating, not threat-classification correctness. |
| `ValidatedEffects` | Only configured, revalidated, authorized safe-output effects can mutate the target. | OI-06/07/11; CTR-005/012/015 | [`resolveAndValidateRepo`](../../actions/setup/js/repo_helpers.cjs):162–204; [`processSafeOutput`](../../actions/setup/js/safe_output_processor.cjs). |
| `AppLeastPrivilege` | Installation repositories and permissions are explicit and within this profile's grant. | A:428–440,666–674; K:157–190 | [`buildGitHubAppTokenMintStepWithMeta`](../../pkg/workflow/safe_outputs_app_config.go):427–461; [`validateAppTokenPermissions`](../../pkg/workflow/app_token_permissions_validation.go):41–100. |
| `PrivilegedCheckoutIsolation` | Persisted push credentials exist only in a live privileged processor context. | K:175–205; AR1–4 | [`buildSharedPRCheckoutSteps`](../../pkg/workflow/compiler_safe_outputs_steps.go):32–100; [`GenerateConfigureGitCredentialsSteps`](../../pkg/workflow/checkout_step_generator.go):259–382. |
| `SecretConfinement` | Write credentials cannot reach the agent or any modeled publication channel; authorized inference credentials remain permitted. | CTR-017; AR4; MCP scripts SN-SCOPE | [`ComputeAWFExcludeEnvVarNames`](../../pkg/workflow/awf_env.go):84–182; [`classifyStepSecrets`](../../pkg/workflow/strict_mode_steps_validation.go):27–190; [`StepOrderTracker`](../../pkg/workflow/step_order_validation.go):94–189. |
| `TrustedExecution` | Request bytes remain data, not privileged executable code. | SG-01; CTR-006/009/010 | [`template_injection_validation`](../../pkg/workflow/template_injection_validation.go); [`safe_output_handler_manager`](../../actions/setup/js/safe_output_handler_manager.cjs). |
| `GitAuthorization` | Authenticated remote operations use credentials scoped to that repository and operation. | K:122–130,157–205 | [`resolveCheckoutTokenExpression`](../../pkg/workflow/checkout_step_generator.go):733–751; [`resolvePRCheckoutToken`](../../pkg/workflow/github_token.go):131–191. |
| `NoImplicitFetch` | Credential-free reasoning never silently fetches or pushes to compensate for missing local objects. | K:212–229; checkout credential policy | [`generateFetchStepLines`](../../pkg/workflow/checkout_step_generator.go):680–730; [`safe_outputs_push_to_pr_branch.md`](../../actions/setup/md/safe_outputs_push_to_pr_branch.md). |
| `TokenLifetime` | Revoked tokens cannot become live again; completed successful or failed jobs retain no modeled engine or app token. | Job-scoped credentials, AR1–4 | Models the `actions/create-github-app-token` post step revoking a minted token after success or failure; `skip-token-revoke` disables this cleanup. |
| `NetworkPolicy` | Egress follows the effective allowlist and engine auth goes only to inference. | NI-01–14; CTR-011 | [`GetAllowedDomains` / `GetBlockedDomains`](../../pkg/workflow/domains.go); [`appendEnvAndMountArgs`](../../pkg/workflow/awf_command_builder.go):482–499. External firewall enforcement is assumed. |
| `TrustedConfiguration` | Activation instruction authority is not inherited from attacker-controlled configuration. | CTR-028/030 | [`activationCheckoutRef`](../../pkg/workflow/compiler_activation_job.go):494–501; [`restore_base_github_folders.sh`](../../actions/setup/sh/restore_base_github_folders.sh):40–83. |
| `OutputLimit` | The declared maximum of one effect is not exceeded. | OI validation / configured operation bounds | [`safe_output_validator`](../../actions/setup/js/safe_output_validator.cjs), operation-specific safe-output handlers. |
| `PrivateSinkPolicy` | Private-source content is not published to this public target without an explicit policy grant. | CTR-031; PPF1–4 | [`validatePrivateToPublicFlowsPolicy`](../../pkg/workflow/strict_mode_private_to_public_flows_validation.go):15–38; external GitHub gateway/proxy source/sink policies. |

`SecretConfinement`, `TrustedExecution`, and `NetworkPolicy` express desired
boundary obligations, not a proof of semantic prompt-injection resistance,
complete secret redaction, or noninterference. Sanitization preserves a data
channel; it does not transform data into trusted instructions.

## Git semantics

`Checkout(r)` uses that entry's token during trusted setup. `CheckoutMode`
selects transient checkout or explicit force-clean persistence. `CleanCheckout`
either removes and verifies credentials or fails the agent job before reasoning;
`CheckoutComplete` cannot start the engine while credentials remain.
The `cleanup-fail-open` mutation reproduces the historical ignored-error path.
`main` is sparse and shallow in the sparse profile;
`private_dependency` is full. Sparse patterns, available blobs, available refs,
and depth are independent fields, even though the supplied small profiles
choose them together. Configured `fetch:` is part of authenticated setup.

`diff` uses local state. `show-base` requires a local base ref; `read-blob`
requires a local object. A missing object/ref, credential-free `fetch`/`push`,
or unauthorized cross-repository `gh-read` records an explicit error. It never
widens/deepens the repository, consults a credential helper, or invents success.
The `gh-read` transition is a separately authorized REST tool with command-env
auth; git configuration does not authorize it.

Safe-output MCP patch/bundle preparation belongs to the credential-free
principal. A privileged `pull-request` effect uses **another checkout** and may
legitimately retain a push token until cleanup. Therefore the invariant is not
“every checkout always has `persist-credentials: false`.”

The model abstracts refs to `HEAD` and `base`, paths to two locally available
objects, and repositories to two identities. It does not prove Git ref/path
parsing, symlink confinement, `.git` indirection, submodules, object graph
ancestry, merge correctness, signed pushes, or concurrent checkout discovery.
These require separate refinement of [`gitutil`](../../pkg/gitutil/gitutil.go)
and [`findRepoCheckout`](../../actions/setup/js/find_repo_checkout.cjs).
The separate [bundle Git-operation model](#safe-output-bundle-git-operations)
specifies privileged recovery and optional validation-baseline decisions without
adding remote access to the credential-free agent's `GitOperation` transition.

## Host-side MCP compilation

[`MCPCompile.tla`](MCPCompile.tla) models the compiler threat specification's
[MCP-COMPILE-001–006](../compiler-threat-detection-spec.md#521-host-side-mcp-compilation-boundary)
as a separate host-side boundary, not a generated workflow job. Each bounded
request selects an MCP or trusted CLI no-emit entry point, tool, immutable
server-startup write capability, configured actor validation and permission,
option class, workflow validity, import availability and expansion path
(frontmatter, body, or nested include), local image availability/requirement,
stale lock validity, and repository-root/subdirectory/non-Git launch context.
`Authorize`, `Prepare`, `ResolveImports`, `ResolveImages`, `Validate`, and `Finalize` enforce the
ordering between admission, subprocess/scanner preparation, cache access,
local-image inspection, validation, and output effects. Missing local images
produce a warning unless image validation is required, when they produce an error.

| Invariants | Boundary |
|---|---|
| `StartupCapability`, `ActorAuthorization`, `WritingOptionsGated`, `DeniedBeforeExecution` | Callers cannot opt into write tools; read-only compilation bypasses only the write-actor gate, while write-enabled operations retain it. Rejected requests start no compilation or scanner. |
| `ReadOnlyMode`, `NoExternalScanners`, `NoRemoteDownloads`, `NoImagePulls`, `ReadOnlyIsolation` | Read-only requests force dedicated read-only mode, no-emit, and no update check, execute no external scanners, download no missing imports/includes, pull no images, and record no mutations on success or failure. |
| `CacheRootConforms`, `ReadOnlyAvailability`, `ValidationPreserved` | Git checkouts use the repository-root cache, non-Git contexts use the working directory, cached valid imports remain usable from subdirectories, malformed stale output cannot invalidate generated YAML, and invalid inputs remain errors. |
| `WriteEnabledEmission`, `TrustedNoEmitAvailability` | Authorized write-enabled compilation emits locks; trusted CLI no-emit compilation can populate imports without emitting a lock file. |

`check.py` includes the secure baseline, nineteen synthetic negative controls,
and seven reachability witnesses. Controls independently break startup authority,
actor checks, option rejection, dedicated read-only/no-emit/update-check flags, scanner ordering,
missing-import download prevention, diagnostic/post-processing/cache-hit write
suppression, cache-root selection, read-only admission, validation, or
write-enabled emission. Additional controls reproduce Docker pulling, uncached
body/nested include fallback, stale-lock validation, and applying the stronger
read-only policy to trusted no-emit CLI callers. Witnesses reach read-only success, cached subdirectory
success, an uncached-import error without writes, write-enabled emission, and
write-actor denial, plus cached nested-include success and trusted CLI no-emit
import refresh, so blanket rejection cannot satisfy the model vacuously.

Mutations abstract creation, modification, or deletion of source, lock,
diagnostic, action-pin, attributes, import-cache, update-cache, and temporary
include files; Docker pulls have a separate tracked effect. This
does not model bytes, path resolution, symlinks, concurrent callers, exact Git
discovery, scanner internals, all CLI flag combinations, or server-startup
temporary manifest baselines. The model is not a filesystem sandbox or a proof
that the implementation refines it. Its concrete evidence is
[`mcp_server_compile_test.go`](../../pkg/cli/mcp_server_compile_test.go)'s
real-binary snapshots of file inventory, bytes, modes, and modification times,
isolated temporary-directory include tests, fake-Docker regressions, and the
implementation and unit-test mapping in compiler threat §7.3.

## Run the machine

Use Java 21, Python 3 (standard library only), the repository's Go toolchain,
and official [TLA+ Tools v1.8.0](https://github.com/tlaplus/tlaplus/releases/tag/v1.8.0).
The runner verifies this jar SHA-256 before executing it (the v1.8.0 release
asset SHA-256 published by GitHub for asset `670958559`, updated on
2026-10-24; the release notes publish SHA-1):

```text
d15655eeaff4b3f7b3d65c46b838bc0f35d131b2ef4b1e473689b62c98d4726f
```

Upstream rebuilds this release asset and moves the `v1.8.0` tag; its versioned
URL is not immutable. This pin identifies the 4,502,259-byte asset uploaded by
[upstream build 40049599214](https://github.com/tlaplus/tlaplus/actions/runs/40049599214)
at source commit `2ef2af8c767b76e5db5cba2878f027ac07aad36c`.
The downloaded SHA-256 matches both GitHub's asset metadata and that build's
upload log. This correlation is not an independent build attestation.

When upstream replaces the asset again, verification must fail closed. Do not
derive the expected checksum dynamically or bypass either runtime check.
When intentionally updating the release pin, verify the downloaded bytes
against the release asset metadata and the upstream build's upload log, then
update the workflow and generated lock, runner, regression test, and this digest
together. An immutable, vetted distribution would be needed to prevent future
release-URL drift.
Existing example reports retain the jar digest used to generate their evidence.

```bash
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s specs/workflow-security -p '*_test.py'
TLA2TOOLS_JAR=/path/to/tla2tools.jar JAVA_BIN=/path/to/java \
  python3 specs/workflow-security/check.py --results /tmp/workflow-security-new
go test ./specs/workflow-security/conformance
```

After `make build`, add `--compiler ./gh-aw` to the checker command to require
both acceptance of the source seed and rejection of its concrete write-grant
mutation. This does not dispatch either workflow.

The results directory must not already exist. Every case includes a self-contained
model/config, full TLC log, and saved state data. Secure configurations exhaust
the reachable graph, not a depth-constrained prefix. Ten positive configurations
cover sparse/full checkout, issue/pull-request effects, force-clean lifecycle,
reusable invocation naming, and declared detection modes. Twenty-one deliberately
broken protections (including failure-path token retention) and eight reachability
witnesses cover authenticated push, temporary host credentials, and a blocking
cleanup failure. The additional runtime-checkout model exhausts both job kinds
and verifies the controls and witnesses described above. The MCP compilation
model adds its secure baseline, nineteen negative controls, and seven witnesses.

Counterexamples include raw `tlc-trace.json`, normalized `trace.json`, and
`events.txt`. `CompiledWorkflow` negative controls also generate `source.md` and
`counterexample.json`; runtime-checkout controls generate classification reports
without inventing source-level exploits. Only `agent-write` directly changes a supported
frontmatter grant; the strict compiler must reject it. Other source seeds
explicitly describe the hypothetical compiler/runtime mutation needed to
realize their trace. They are not source-level exploits. Witnesses deliberately
violate `NoSuccessfulWrite`, `NoDeniedRequest`, `NoMissingGitData`,
`NoCrossRepoCheckout`, `NoFailedJob`, `NoCleanupFailure`, or
`NoTemporaryCredentials` while retaining all security invariants.

The runner requires TLC exit 0 plus exhaustive-success output for secure runs,
or exit 12 plus the **exact expected invariant** for a negative control/witness.
A different safety failure, parse error, deadlock, timeout, missing trace,
or wrong jar is a failure. Deadlock checking is disabled because intentional
failed/stopped workflows are terminal; no liveness or fairness theorem is made.

### Connect source to compiled jobs

The compile-only seed uses fictitious credentials and a fake dependency.
Never dispatch it. Compile a copy into a new temporary directory:

```bash
make build
mkdir /tmp/workflow-security-source
cp specs/workflow-security/fixtures/cross-repo.md /tmp/workflow-security-source/
./gh-aw compile /tmp/workflow-security-source/cross-repo.md --approve --json
go run ./cmd/gh-aw-security-model --profile seed \
  /tmp/workflow-security-source/cross-repo.lock.yml
```

The verifier decodes actual YAML and rejects missing job dependencies,
repository-write agent permissions, checkout credentials without immediately
following fail-closed cleanup and verification,
cross-run artifact downloads, missing strict detector gates, unpinned external
actions, wrong dependency credentials, excessive app scopes, or disabled token
revocation. Its `daily` profile omits seed-specific token/app assertions while
still requiring detection. The `compiled` profile reads an independent
`gh-aw-manifest.threat_detection` declaration emitted by the compiler. Missing
or unknown policy is an error; absent jobs cannot establish an opt-out. Reports
include the effective policy so a disabled detector is never presented as
equivalent assurance to required detection.

Guard checks use actionlint's AST and conservative Boolean implication. An
`always()` condition is accepted only when compiler-owned success is enforced;
OR branches, negation, and status-function calls cannot simply bypass the check.
Symbolic artifact names must match across producer/consumer and refer to the
trusted activation prefix helper, not arbitrary caller-selected prefixes.
The prefix helper hashes inputs and run attempt; identical inputs in the same
attempt intentionally share a prefix. Caller-provided invocation uniqueness
and hash collision resistance remain assumptions, not guarantees proved here.
Manifest provenance, payload
validation, redaction coverage, external network enforcement, and token expiry
remain obligations, not facts extracted by this structural check.

Validate the entire existing compiled corpus together with TLC:

```bash
go build -o /tmp/gh-aw-security-model ./cmd/gh-aw-security-model
TLA2TOOLS_JAR=/path/to/tla2tools.jar JAVA_BIN=/path/to/java \
  python3 specs/workflow-security/check.py --results /tmp/workflow-security-corpus \
  --verifier /tmp/gh-aw-security-model --compiled-workflows .github/workflows
```

`compiled-workflows.json` records every lock's hash, policy, outcome, and
violations. Any structural or tooling failure fails the runner. Legacy locks
without the explicit policy must be recompiled; they are not silently accepted.

[`examples.json`](examples.json) records representative action traces generated
by the pinned checker. Regenerate it only from a successful full run:

```bash
python3 specs/workflow-security/check.py --results /tmp/workflow-security-new \
  --write-examples
```

## Refinement backlog and architecture exceptions

The architecture review identified **contract distinctions, not confirmed
exploitable bugs**. Preserve them rather than strengthening the model's
assumptions until real behavior disappears:

| Boundary not fully modeled | Evidence / required refinement |
|---|---|
| Pre-activation actor authorization, bots, replay | A:608–660,966–1012; CTR-027/029; model trusted event identity and separate membership/checkout gates. |
| Compiler expressions and freshness | CTR-010/016/018; model expression classification, manifest approvals, frontmatter/body hashes, legacy fallbacks, and status-function augmentation. |
| Detection warning handling | WTD1–3; disabled/conditional modes are modeled, but warning-mode annotated publication, push-to-PR conversion, and abort-only operations still need operation-specific refinement. |
| Authorized custom steps/tools and augmented app grants | CTR-017; agent-job host secrets and explicit tool grants are legitimate. Do not assume all job secrets or permission augmentation are forbidden. |
| Redaction coverage and exceptions | Ordering checks do not guarantee successful redaction of every artifact. Binary/unscanned exceptions and fallback `always()` uploads need explicit coverage/lifecycle states. |
| Cache integrity and publication | CTR-019; model policy/integrity namespaces, restoration, detection-gated publication, and detection-disabled post-action saving. |
| Checkout merging, primary target, and directory identity | Entry identity is `(repository,path,wiki)`; `current` does not change cwd. Model token/app alternatives, unioned fetch/sparse patterns, and deepest history selection. |
| Safe-output token precedence | Runtime entry precedence, representative missing-key fallbacks, and independence from neighboring scoped Apps are modeled. Static/shared checkout target selection, installation grants, and handler/head-token variants still require separate refinement. |
| Filesystem and platform enforcement | Artifact service origin, immutable event metadata, AWF/gateway isolation, labels, action pin integrity, post-job cleanup, and revocation are environmental assumptions at this layer. |

## Daily investigation

[`daily-workflow-security-model.md`](../../.github/workflows/daily-workflow-security-model.md)
runs daily with read-only repository permissions and mediated safe outputs.
It prepares checksum-pinned Java/TLC and a native compiled-profile verifier,
checks the machine, rotates one trust boundary, and proposes validated model
refinements through a restricted draft PR.

New credible defects or architectural gaps are deduplicated and filed with the
exact label **security critical**. That label is a triage marker, not proof of
severity. Intentional mutations, reachability witnesses, rejected sources, and
tooling failures are not reported as confirmed vulnerabilities. New findings
must include a minimized trace, benign source, compiled-step mapping, source
evidence, assumptions, and reproducible commands; no live attack or workflow
dispatch is permitted.
