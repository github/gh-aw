---
title: How to protect an authorization baseline
description: Verify safe-output authorization policy and generated workflow integrity using an independently protected required check.
---

Use `gh aw validate --authorization-policy` to check that an immutable repository snapshot retains the approved literal authority for every eligible safe output. This read-only profile uses the ordinary compiler's authorization resolution and executor coverage. It does not acquire OIDC tokens, contact an authority, claim work, or run candidate scripts, actions, hooks, graders, or workflows.

> [!IMPORTANT]
> A baseline path is an input, not a trust guarantee. An ordinary workflow added in the same repository does not establish a protected required-check boundary. Without independently enforced checker configuration, the result is advisory.

## Protect the checker inputs

Administer the checker separately from the candidate repository. Store the baseline in a protected control repository at an approved **immutable commit**, or use an equivalently protected artifact. Pin its expected SHA-256 digest in checker configuration that the candidate cannot modify. Obtain the verifier binary from an independently trusted release or build; pin and verify its digest before invocation. Never build or install the authoritative verifier from the candidate.

Protect the checker workflow, wrapper, approved handwritten Actions inventory, compiler configuration, baseline revision, and expected digests from candidate edits. A `policy_revision` label correlates reports; it does not authenticate policy. Tightening or retiring policy requires an independently reviewed control-repository change. Do not introduce repository-local exemptions, automatic break-glass downgrades, or secret-bearing approval tokens.

Create a baseline with this exact v1 shape:

```json title="authorization-baseline.json"
{
  "version": 1,
  "policy_revision": "baseline-001",
  "repository_id": "123456789",
  "authority": {
    "endpoint": "https://authority.example.org/api/safe-output-authorization",
    "audience": "https://authority.example.org/gh-aw-safe-outputs"
  },
  "required_workflows": [
    ".github/workflows/triage.md"
  ]
}
```

The schema rejects unknown or duplicate keys, unsupported versions, mutable authority metadata, duplicate paths, and noncanonical or escaping paths. The policy is limited to 1 MiB, a 1–128 character ASCII revision identifier, a 1–20 digit positive repository ID, and 1–4096 required source paths of at most 256 bytes. Required paths identify direct `.md` children of `.github/workflows`. Endpoint and audience follow the existing literal authorization validation, including their 2048-byte limits. V1 has no setting to disable authorization or widen builtin exemptions.

## Inspect the exact candidate revision

Have the trusted wrapper obtain the numeric repository ID and, when needed for schedule scattering, `owner/repository` from verified platform context. A Git remote URL or candidate-owned file is not independent proof of repository identity. Missing or mismatched numeric identity produces an incomplete result.

Provision a complete local object database containing the exact pull-request candidate or merge-group commit selected by the platform. No checkout of candidate code is needed. The verifier reads raw Git objects into private temporary storage; checkout filters, hooks, and archive export attributes are not applied. Uncommitted files are not verified. Missing objects are not fetched automatically.

Install the pinned `gh aw` extension and trusted executables outside the candidate tree. Use an ephemeral isolated runner/container, a read-only candidate object database, a private `TMPDIR`, no write credentials or secrets, and denied outbound network access. Strip candidate-controlled environment variables, compiler overrides, Git configuration, and `PATH` entries. Do not mount writable host services or a Docker socket.

Run from the candidate repository using values supplied by the protected wrapper:

```bash
gh aw validate \
  --authorization-policy /trusted/authorization-baseline.json \
  --authorization-policy-sha256 "$PROTECTED_POLICY_SHA256" \
  --authorization-repository-id "$VERIFIED_REPOSITORY_ID" \
  --authorization-repository "$VERIFIED_REPOSITORY_SLUG" \
  --authorization-commit "$CANDIDATE_COMMIT" \
  --json > authorization-result.json
```

`--authorization-commit` accepts a full lowercase commit object ID; it defaults to the resolved `HEAD` commit. The protected wrapper must bind this argument to the platform-selected candidate, never to agent-supplied arguments or candidate-owned configuration. Do not expose the checker as an agent tool for arbitrary revision inspection. Verify that the report's `candidate_commit` matches the platform-selected revision before publishing a passing check; `HEAD` may identify a different revision. The repository slug reproduces repository-scattered schedules, not repository identity. The report fingerprints the actual compiler executable, baseline bytes, complete candidate tree inventory, workflow sources, imports/includes, candidate locks, and recomputed locks.

The verifier discovers all gh-aw entrypoints in `.github/workflows`, distinguishes shared/import-only documents using existing parsing, and includes every baseline-required workflow. A new entrypoint joins automatically. Deleting, renaming, or converting a required entrypoint into a shared document fails until the protected baseline is independently updated. Positional workflow subsets, directory overrides (including `GH_AW_WORKFLOWS_DIR`), engine/strict overrides, and fail-fast validation are rejected.

## Supply reproducible dependencies

This initial profile resolves local snapshot imports and compiler-embedded builtin definitions and action pins only. It does not load candidate-owned action-resolution caches, download remote imports, resolve unknown action tags, run package validators, or invoke external workflow linters. Use local reviewed imports and literal action SHAs or compiler-supported explicit source pin mappings. Unresolved dependencies and unsupported compiler inputs fail verification; they are not silently omitted.

Pin remote skills to full commit SHAs. Remote safe-output actions must declare inline `inputs` (use `inputs: {}` for no inputs); even SHA-pinned actions cannot fetch metadata during verification. Local dependency digests use compiler-resolved filenames, not manifest display paths or section selectors.

Relative `stop-after` timestamps depend on historical compilation time and are incomplete in this profile. Use absolute timestamps or source expressions. Absolute timestamps are recomputed from source, never accepted from a candidate lock's metadata. Use the same pinned compiler version and generation inputs when producing reviewed locks.

Snapshot materialization rejects symlinks, submodules, unsupported paths, files larger than 16 MiB, inventories over 50,000 files, and total content over 256 MiB. These cases are incomplete, not compliant. Arrange approved dependencies in the inspected snapshot rather than weakening containment or allowing live dependency resolution.

## Review executable workflow inventory

Every discovered entrypoint's complete lock is regenerated in memory and compared **byte for byte**, including executable YAML. No normalization discards conditions, steps, scripts, authority bindings, or jobs. Missing gates, altered conditions, extra write-capable steps, removed launch/standalone jobs, and a forged compliant manifest cannot pass this comparison. Even comment-only differences require deterministic recompilation. No lock is emitted or repaired.

Orphaned `.lock.yml` workflows fail. Unexpected `.yml` or `.yaml` workflows produce `inventory_unresolved`; they are not counted as authorized gh-aw outputs. Maintain a separate, independently reviewed inventory of legitimate handwritten Actions workflows in the trusted wrapper. Pass each approved path explicitly:

```bash
gh aw validate \
  --authorization-policy /trusted/authorization-baseline.json \
  --authorization-repository-id "$VERIFIED_REPOSITORY_ID" \
  --authorization-policy-sha256 "$PROTECTED_POLICY_SHA256" \
  --authorization-commit "$CANDIDATE_COMMIT" \
  --authorization-actions-workflow .github/workflows/build.yml \
  --json
```

Approved handwritten paths appear in `ungoverned_workflows`, not in authorization coverage. Generated `.lock.yml` paths cannot be exempted this way. Approval of a handwritten path is **not** approval of its contents or credentials; the deployment must govern those independently.

## Enforce and publish the result

Configure an organization/repository ruleset requiring the independently administered workflow where the host, plan, and organization policy support it. Alternatively, require a check published by a separately administered GitHub App and constrain its expected provider/app identity where supported. Prevent candidate changes from replacing or disabling the checker; restrict ruleset changes and bypass permissions separately.

Run the check for every relevant candidate and merge-group revision, including changes to sources, imports, locks, and `.github/workflows/aw.json`. Prefer running on every candidate: do not rely on candidate-controlled path filters, workflow definitions, or job conditions. A checker that never runs must not satisfy the requirement. If the platform cannot enforce this protected identity and configuration, describe the result as advisory rather than claiming mandatory governance.

Publish `authorization-result.json` as a check artifact even when the verifier exits nonzero. Use trusted publication steps and credentials isolated from candidate inspection. Treat process crashes, absent/malformed reports, schema-version mismatches, and artifact/publication failures as failing checks. Never use `continue-on-error` to promote an incomplete result.

The versioned JSON object contains:

| Field | Meaning |
|---|---|
| `version`, `status` | Result schema version `1`; `compliant`, `violations`, or `incomplete` |
| `candidate_commit`, `snapshot_digest` | Inspected commit and SHA-256 of its full raw tree inventory |
| `repository_id`, `repository` | Checker-supplied platform identity and optional compilation context |
| `policy_revision`, `policy_digest`, `policy_fingerprint` | Correlation label, baseline SHA-256, and expected authority fingerprint |
| `compiler_version`, `compiler_digest` | Pinned compiler version and executable SHA-256 |
| `eligible`, `exempt`, `staged` | Counts of normalized routes, including generated executors; staged counts statically true staging only |
| `workflows` | Source, lock, generated-content and dependency digests, plus resolved route bindings and coverage. Routes also carry optional `effect_adapter` (trusted adapter contract, such as `gh-aw.merge-pull-request@1`) and `scope_binding` (`resolved_effect` or `invocation` for a live protected route); these additive v1 fields do not change `coverage` |
| `findings` | Stable code, workflow/route when applicable, observed binding, expected fingerprint and remediation message |
| `ungoverned_workflows` | Independently approved handwritten workflow paths outside gh-aw authorization scope |

Authority fingerprints are SHA-256 of UTF-8 `endpoint`, a NUL separator, and `audience`. Reports do not print endpoint/audience values. Missing and mutable bindings are represented as `unconfigured` and `environment`. Findings use `authorization_required`, `authority_mismatch`, `mutable_authority_binding`, `required_workflow_missing`, `executor_uncovered`, `generated_workflow_drift`, `inventory_unresolved`, `policy_invalid`, and `verification_incomplete`. Incomplete verification takes status precedence when both incomplete findings and violations exist. Only `compliant` exits zero.

The four compiler-classified diagnostics (`noop`, `report-missing-tool`, `report-missing-data`, and `report-incomplete`) retain their inherited exemption. Explicit diagnostic authorization must still match. Custom names or diagnostic-looking bodies cannot gain exemption. Compiler-enabled internal memory/ledger outputs, native/custom executors, standalone outputs, workflow launches and prepared Claim adapters remain in inventory even when authorization is missing.

Global or local staging does not forgive missing configuration. Expressions are not evaluated to select a convenient staged path. Runtime staged previews remain authorization-free. A diagnostics-only workflow with zero eligible routes is reported as such; it is not evidence that a live invocation was authorized. The compiler's auto-injected `create-issue` output, when enabled, is eligible too.

## Understand the boundary

A passing protected check proves declared authorization-baseline compliance and source-to-lock integrity for the inspected snapshot. It is not a runtime authorization decision and does not prove authority health, independent credential custody, remediation outcomes, exact-effect binding, or that arbitrary custom code and unrelated workflows cannot use other write paths. Runtime authorization and target-side credential enforcement remain necessary.
