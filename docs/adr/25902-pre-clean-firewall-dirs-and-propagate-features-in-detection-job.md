# ADR-25902: Pre-Clean Stale Firewall Directories and Propagate Features in Detection Job

**Date**: 2026-04-12
**Status**: Draft
**Deciders**: Unknown (inferred from PR #25902 by Copilot)

---

## Part 1 — Narrative (Human-Friendly)

### Context

PR #25868 merged firewall audit/log files (squid.conf, cache.log, access.log, etc.) into the unified agent artifact. When the detection job downloads this artifact, it extracts into `/tmp/gh-aw/`, which pre-populates `sandbox/firewall/logs` and `sandbox/firewall/audit` with files from the completed agent run. The AWF (Agent Workflow Firewall) squid container then crashes on startup with exit code 1 because squid cannot initialize when these directories are already populated by a prior run. Separately, `buildPullAWFContainersStep` constructed a minimal `WorkflowData` without the `Features` field, so feature-dependent container images were silently omitted from the detection job's container pre-pull.

### Decision

We will add a `buildCleanFirewallDirsStep()` function that runs `rm -rf` on `AWFProxyLogsDir` and `AWFAuditDir` as the first step in the detection job, immediately after artifact download and before any container startup. We will also propagate `Features: data.Features` into the minimal `WorkflowData` constructed inside `buildPullAWFContainersStep()`, matching the propagation pattern already used in `buildDetectionEngineExecutionStep()`. These two fixes are grouped because they both correct silent failures in the detection job's initialization phase.

### Alternatives Considered

#### Alternative 1: Exclude Firewall Directories from the Agent Artifact

The agent job's artifact upload step could be modified to exclude `sandbox/firewall/logs` and `sandbox/firewall/audit` from the unified artifact. This would prevent the detection job from ever receiving stale files. It was not chosen because it requires changes to the agent job's artifact construction logic (a separate concern), risks accidentally excluding firewall metadata that future detection steps may need, and does not fix the root cause—the detection job's squid container must tolerate any pre-existing state at its working directories.

#### Alternative 2: Reconfigure Squid to Ignore Pre-Existing Directory Contents

The squid container could be started with configuration flags or a startup wrapper that clears or ignores existing cache and log directories. This would make the detection job resilient regardless of artifact contents. It was not chosen because it requires changes to the AWF container image or its startup scripts, which are maintained in a separate repository (`gh-aw-firewall`), making the fix more invasive and slower to deploy than a pre-clean step in the compiled workflow.

#### Alternative 3: Restructure the Artifact to Use a Different Extraction Path

The detection job could download the agent artifact to a temporary location and selectively copy only the files it needs (prompt.txt, agent_output.json, patches), never extracting into `/tmp/gh-aw/` at all. This would fully prevent any artifact contents from polluting the firewall directories. It was not chosen because it requires a significant refactor of `buildAgentOutputDownloadSteps` and the artifact path conventions relied upon by multiple downstream steps, making it a higher-risk change than a targeted pre-clean.

### Consequences

#### Positive
- The detection job's squid container starts reliably regardless of what the agent artifact deposited in the firewall directories.
- Feature-dependent container images are correctly included in the detection job's container pre-pull, preventing silent feature gaps between the agent job and the detection job.
- The fix is minimal and localized—one new function and one field addition—with low regression risk.

#### Negative
- The pre-clean step runs unconditionally on every detection job invocation, even when the firewall directories happen to be empty. This adds a trivially fast `rm -rf` to every detection job but is effectively a no-op in environments that do not include firewall files in the artifact.
- The clean step permanently deletes contents of `AWFProxyLogsDir` and `AWFAuditDir` before the squid container runs. If future logic needs to preserve artifact-sourced firewall state for detection purposes, this step would need to be revisited.

#### Neutral
- The `Features` propagation change brings `buildPullAWFContainersStep` into alignment with the existing pattern in `buildDetectionEngineExecutionStep`, reducing inconsistency in how the detection job constructs its minimal `WorkflowData`.
- Tests (`TestCleanFirewallDirsStepPresent`, `TestCleanFirewallDirsStepOrdering`, `TestBuildPullAWFContainersStepPropagatesFeatures`) encode the ordering constraint and feature propagation requirement as executable documentation.

---

## Part 2 — Normative Specification (RFC 2119)

> The key words **MUST**, **MUST NOT**, **REQUIRED**, **SHALL**, **SHALL NOT**, **SHOULD**, **SHOULD NOT**, **RECOMMENDED**, **MAY**, and **OPTIONAL** in this section are to be interpreted as described in [RFC 2119](https://www.rfc-editor.org/rfc/rfc2119).

### Detection Job Initialization

1. The detection job **MUST** execute a firewall directory clean step before any AWF container startup step.
2. The clean step **MUST** remove `AWFProxyLogsDir` and `AWFAuditDir` using `rm -rf`, tolerating the case where these directories do not exist.
3. The clean step **MUST NOT** be conditioned on the detection guard output — it **SHALL** run unconditionally so that containers never encounter stale state.
4. Implementations **SHOULD** use the `constants.AWFProxyLogsDir` and `constants.AWFAuditDir` symbolic constants rather than hardcoded paths, so that path changes are propagated automatically.

### WorkflowData Propagation in Container Pre-Pull

1. `buildPullAWFContainersStep` **MUST** propagate the `Features` field from the parent `WorkflowData` into the minimal `WorkflowData` it constructs for `collectDockerImages`.
2. Any field of the parent `WorkflowData` that affects which container images are collected (including `ActionCache` and `Features`) **MUST** be propagated into the minimal `WorkflowData` used for image collection in the detection job.
3. Implementations **MUST NOT** omit feature-flag fields when constructing minimal `WorkflowData` instances for detection-job sub-steps, as omission silently alters image selection behavior.

### Conformance

An implementation is considered conformant with this ADR if it satisfies all **MUST** and **MUST NOT** requirements above. Specifically: the detection job's step ordering places the firewall directory clean before any container pull step, the clean step runs unconditionally, and `buildPullAWFContainersStep` propagates `Features` (and `ActionCache`) from the parent `WorkflowData`. Failure to meet any **MUST** or **MUST NOT** requirement constitutes non-conformance.

---

*This is a DRAFT ADR generated by the [Design Decision Gate](https://github.com/github/gh-aw/actions/runs/24307339373) workflow. The PR author must review, complete, and finalize this document before the PR can merge.*
