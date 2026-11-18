---
title: "Before the Agent Starts: The Activation Gate"
description: "Use centralized admission to decide whether an agentic workflow should start and how many AI Credits it may use, with an integration path for CAO."
authors:
  - copilot
date: 2026-10-27
metadata:
  seoDescription: "How gh-aw activation admission works: OIDC authentication, centralized AI Credit allowances, outage behavior, completion callbacks, and CAO integration."
---

A scheduled workflow is ready to run. Its trigger matches, its authorization checks pass, and its prompt is compiled. But should it start another agent session right now?

That question can depend on more than one repository: an organization-wide budget, a workflow's priority, or a centrally managed allowance. The activation gate adds a place to make that decision before the agent starts. In gh-aw configuration, the feature is called **admission**, omitted by default and enabled repository-wide with `"admission": true` in `aw.json`.

gh-aw implements the workflow-side client and compiler wiring. A separately deployed **admission authority** owns policy, identity verification, and trusted usage accounting.

## A decision before inference

For a workflow with a configured endpoint, a compiler-owned step in `activation` runs after trusted setup and before author pre-steps and daily validation. It requests a GitHub Actions OIDC identity token for the authority's audience, then posts the invocation identity and compiled workflow hash to `/api/admission`.

The authority verifies the identity and evaluates its policy. An approval supplies the primary agent's maximum AI Credits, with optional independent allowances for threat detection and evals:

```json
{
  "max_ai_credits": 1000,
  "max_ai_credits_threat_detection": 100,
  "max_ai_credits_evals": 50
}
```

These are illustrative allowances, not built-in tiers. Each supplied allowance replaces that component's runtime limit, even if it is higher than the workflow's configured limit. Omitted optional allowances leave the existing component limits unchanged; the three values are not a shared pool.

A valid approval supersedes the local daily-credit check for that activation. Any required shared-budget decision therefore belongs in the authority. An explicit denial stops activation before the agent runs.

## Enable once, configure centrally

Set `"admission": true` in the existing `.github/workflows/aw.json` object to enable admission repository-wide, preserving its other settings. Without a repository override, a single workflow can opt in with:

```aw wrap
on:
  workflow_dispatch:
  admission: true
```

An explicit repository setting takes precedence over workflow frontmatter. This controls admission code generation without changing activation or conclusion permissions.

Recompile affected workflows with `gh aw compile` after changing those settings and commit the source configuration and generated `.lock.yml` files.

The runtime configuration comes from organization or repository Actions variables:

| Variable | Purpose |
|---|---|
| `GH_AW_DEFAULT_ADMISSION_URL` | Full HTTPS endpoint ending in `/api/admission` |
| `GH_AW_DEFAULT_ADMISSION_AUDIENCE` | OIDC audience; defaults to the endpoint URL |
| `GH_AW_DEFAULT_ADMISSION_ON_OUTAGE` | `ignore` by default, or `block` |

Once the steps are compiled in, changing these variables does not require recompilation. An empty endpoint skips admission and retains existing local checks, without skipping activation's runner or bootstrap. Repository variables take precedence over organization variables.

Authentication uses GitHub Actions OIDC, not a PAT or a provider credential. The feature grants job-wide `id-token: write` to activation and conclusion, not to the agent merely because admission is enabled. Activation hooks and conclusion actions share that capability even when an empty endpoint skips the authority-request steps. Hand-authored reusable-workflow callers must also permit that scope.

For managed-defaults commands and rollout details, see [centralized cost controls](/gh-aw/reference/cost-management/#configure-advanced-cost-controls-with-admission).

## Where CAO fits

For a **CAO integration**, this admission contract is the boundary between the executing workflow and centralized policy. A CAO deployment acting as the authority would verify workflow identity, apply its protected policy, and return allowances through the same protocol. The workflow does not need a CAO-specific credential or custom admission script.

That is an integration contract, not a claim that installing gh-aw deploys CAO or a hosted admission server. The authority must be provisioned and implement the [admission service specification](/gh-aw/specs/admission-service-specification/).

This cost-admission check is also distinct from the experimental [CAO gateway trigger path](/gh-aw/reference/webhook-triggers/). Gateway provenance and hash-bound admission validate incoming dispatches; activation admission decides whether a participating execution may start with a particular credit allowance.

## Close the loop after execution

The same compile-time setting also controls a callback step inside conclusion. After unified-session artifact upload and any retry finish, this step reads the local session and upload step outputs, then sends bounded compact metadata and artifact IDs to `/api/conclusion` on the authority's origin, using a fresh OIDC token.

The authority can use the callback to process completion metadata and retrieve the full session through authenticated GitHub artifact access. It is end-of-session delivery, not live streaming or billing settlement. Delivery is best-effort: a callback failure warns without changing completed agent or safe-output results.

## A cooperative gate, not a hard cap

Outage behavior is an explicit availability choice. With the default `ignore`, an exhausted transient outage falls back to existing local controls. With `block`, it stops activation. Explicit denials and invalid approvals block under either policy.

Admission remains cooperative. Workflow editors can remove participation, repository-variable administrators can override organization defaults, and concurrent runs can overshoot a shared budget. Hard quotas, reservations, and mid-run cancellation require independently enforced backend controls.

The activation gate provides a focused extension point: make the start-time decision centrally, return explicit runtime allowances, and receive completion metadata afterward, while keeping policy-server responsibilities separate from workflow execution.
