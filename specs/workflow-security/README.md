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
| Jobs and steps | `status`, `step`, `started`, `Dependencies` | Ordered host setup, agent execution, detection, validation, credential minting, effects, and cleanup. |
| Artifacts | `artifacts`, `Consume`, `GoodOrigin` | Existence, producing job, run and invocation identity, naming prefix, instruction trust, secret/private labels. Current-run agent artifacts are still untrusted payloads. |
| Outputs and logs | `transfers` | Artifact, output, and log channels carry independently tracked secret labels. This abstracts covered redaction, not arbitrary encoded-secret detection. |
| Permissions | `grants`, `TokenPermissions`, `ValidatedEffects` | Repository read, issue write, content write, and inference capabilities are distinct. Workspace edits are not repository-resource writes. |
| Apps and secrets | `live`, `revoked`, `appScope`, `appRepos`, `agentSecrets` | Read-only checkout credentials, authorized engine credentials, and privileged installation tokens have different consumers and lifetimes. |
| Networking | `ToolCall`, `egress`, `NetworkPolicy` | Authorized GitHub/inference services, blocked destinations, and service-bound engine authentication. |
| Git | `checkouts`, `cleanup`, `agentBegan`, `privilegedCheckout`, `operations`, `errors` | Per-repository auth, temporary setup credentials, verified cleanup/failure, available blobs/refs, sparse/shallow state, local operations, REST, and privileged push. |

The `issue` effect creates at most one validated issue in the main public
repository. The `pull-request` effect prepares a full checkout in the privileged
job and pushes a local change with the scoped installation token. Both reject
private-source content at the public sink under this strict profile.

Detection may run after **agent failure**, and valid already-emitted requests
may still be processed after successful detection. This follows the emitted
`needs.agent.result != 'skipped'` policy rather than assuming all upstream jobs
must succeed. Failure before request publication causes origin validation to
fail. Failures revoke modeled job-owned credentials; eventual platform cleanup
is an explicit environmental assumption.

`DetectionPolicy` independently declares required, disabled, or conditional
detection. `DetectionEnabled` selects a conditional run. Required detection
cannot be bypassed merely by omitting the job. Disabled/conditionally skipped
configurations retain authorization, provenance, credential, and validation
invariants but do not claim detector approval. The inactive detector is
represented by a skipped state-machine slot, not a real runtime job.

## Evidence and invariants

Reviewed baseline: `542e937dd8447172c8c484cda3a9a5716ae99245`.
The authorities are [Security Architecture v1.1.0](../security-architecture-spec.md),
[Compiler Threat Detection v1.0.42](../compiler-threat-detection-spec.md),
[Checkout Behavior](../../docs/src/content/docs/specs/checkout-behavior-specification.md),
and the [Safe Outputs specification](../../docs/src/content/docs/specs/safe-outputs-specification.md).
Line references describe that baseline; symbols are the more durable mapping.
The checkout specification's 1.3.1 amendment requires verified cleanup before
agent execution, including the failure paths found by the corpus trial.

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
| `TokenLifetime` | Revoked tokens cannot become live again; successful or failed jobs retain no modeled engine or app token. | Job-scoped credentials, AR1–4 | App mint steps do not disable the external create-github-app-token post-action revocation. Platform cleanup/TTL is assumed, not locally proved. |
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

## Run the machine

Use Java 21, Python 3 (standard library only), the repository's Go toolchain,
and official [TLA+ Tools v1.8.0](https://github.com/tlaplus/tlaplus/releases/tag/v1.8.0).
The runner verifies this jar SHA-256 before executing it (the v1.8.0 release
asset SHA-256 published by GitHub; the release notes publish SHA-1):

```text
411ab54221cf0c9fa7ae18f07a3e0ebbdf9e5ba6254b79017e7007f1feb44e89
```

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
cleanup failure.

Counterexamples include raw `tlc-trace.json`, normalized `trace.json`, and
`events.txt`. Negative controls also generate `source.md` and
`counterexample.json`. Only `agent-write` directly changes a supported
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
| Safe-output token precedence | Checkout specification K:175–190 differs from `resolvePRCheckoutToken`:131–191, which supports operation PAT, checkout safe-output app, shared app/PAT, and defaults. Preserve this as a conformance observation, not a vulnerability claim. |
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
