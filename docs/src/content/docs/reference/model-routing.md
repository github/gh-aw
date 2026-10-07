---
title: Model Routing
description: Configure experimental per-run model and reasoning-effort selection for the Copilot engine, including requirements, limitations, logs, and troubleshooting.
sidebar:
  order: 605
---

> [!WARNING]
> Model routing is experimental. Review the [known limitations](#known-limitations), especially the threat-detection failure, before enabling it.

## How routing works

`engine.model-routing` chooses the Copilot model and reasoning effort (`none` through `max`, where supported) once per run, before the agent starts, instead of using a fixed model. When routing is enabled, `engine.model` and any configured effort setting are ignored.

AWF's API proxy makes a small LLM call to classify the workflow's task text from `user.txt`, not gh-aw's system instructions. This text includes anything interpolated into the workflow prompt, such as issue bodies. The classifier treats it as untrusted data, not instructions to follow.

The public [githubnext/gh-aw-router](https://github.com/githubnext/gh-aw-router) then ranks the allowed model-and-effort choices using precompiled routing tables fitted on benchmark results and model capability data. Models not covered by the tables are never selected, even if allowed. The [router README](https://github.com/githubnext/gh-aw-router/blob/main/README.md) describes the classification labels, profiles, `/classify` and `/route` APIs, and table format. The router does not itself call an LLM; AWF executes the classifier call.

The selection is advisory. Requests for other policy-allowed models are admitted and logged as deviations, rather than rejected merely for differing from the selection. `allowed-models` defines the request policy for the routed task; declared sub-agent models are also admitted as described under [sub-agents](#sub-agents).

## Configuration

All three fields are required:

| Field | Values | Meaning |
|---|---|---|
| `goal` | `cost`, `cost-speed` | `cost` prefers the cheapest model-and-effort choice that meets the quality bar. `cost-speed` also weighs execution time. |
| `mode` | `economy`, `balanced`, `robust`, `auto` | `economy`, `balanced`, and `robust` set increasing quality bars, generally with increasing cost. `auto` lets the classifier recommend one of these three profiles for the task. |
| `allowed-models` | Non-empty list of Copilot model IDs | Models routing may choose from and the routed task may call. The organization must have access to them, and the routing tables must cover them for selection. |

```aw wrap
engine:
  id: copilot
  model-routing:
    goal: cost
    mode: auto
    allowed-models:
      - gpt-5.4-mini
      - gpt-5.4
      - gpt-5.6-luna
```

Candidate lists from imported workflows using the same engine are merged. Routing adds one classifier call per run, typically costing a fraction of a credit.

## Requirements

Routing requires the Copilot engine and the AWF firewall enabled. The compiler rejects other engines or a disabled firewall.

| AWF version | Capability |
|---|---|
| v0.28.29 or later | Task-level routing |
| v0.28.33 or later | Separate router candidates from admitted sub-agent models ([gh-aw#66234](https://github.com/github/gh-aw/pull/66234)) |
| v0.28.35 or later | Model-routing audit log |

Since [gh-aw#66291](https://github.com/github/gh-aw/pull/66291), routed workflows default to AWF **v0.28.37** and router **0.1.3**, with version-matched, digest-pinned images. Most authors need no version or image overrides. An override of `sandbox.agent.version` must name a published AWF release, not just a Git tag.

Organizations requiring approved registries can override the router image through `sandbox.agent.images.router`. Use a compatible, digest-pinned image; see [Sandbox image overrides](../sandbox/).

`allowed-models` must satisfy the workflow's `models.allowed` and `models.blocked` policy. GitHub Actions expressions in `allowed-models` are rejected when either policy is set, because the compiler cannot validate their runtime values. Models must also be available to the organization's Copilot plan; unavailable models are skipped.

For AWF-side routing configuration, including `routing.candidateModels`, see the [AWF configuration specification](https://github.com/github/gh-aw-firewall/blob/main/docs/awf-config-spec.md#13a-task-level-model-routing). The [API proxy sidecar reference](https://github.com/github/gh-aw-firewall/blob/main/docs/api-proxy-sidecar.md) describes discovery, selection, request policy, and logging.

## Sub-agents

Since [gh-aw#66234](https://github.com/github/gh-aw/pull/66234), inline and imported sub-agents declaring a `model:` are admitted by the request policy, even when that model is not in `allowed-models`. This does not expand router candidates: routing still selects only from `allowed-models`. Workflow model policies still apply.

Copilot CLI uses one wire API per session. A sub-agent whose model uses the other API family from the main model (Claude versus GPT-5) fails with an upstream HTTP 400; see [gh-aw-firewall#9509](https://github.com/github/gh-aw-firewall/issues/9509). Under routing, each fixed sub-agent model must therefore be compatible with **every** allowed main model, not just one candidate.

## Known limitations

### Threat detection fails without failing the workflow

[gh-aw#66224](https://github.com/github/gh-aw/issues/66224) tracks threat detection failing in routed workflows. Detection defaults to `continue-on-error`, so the run still succeeds despite the detection failure.

Authors can ignore the detection job's failure for now and do not need to turn detection off. However, **safe outputs are then applied without a threat verdict**. Authors relying on detection should decide whether this behavior is acceptable before using routing.

### Selection can vary between runs

The classifier's recommendation is not deterministic, especially with `mode: auto`. Runs of the same task can select different models or efforts. An explicit profile fixes the quality bar, but does not make classification or selection deterministic.

The [sub-agent API-family constraint](#sub-agents) also applies until mixed-family CLI sessions are supported.

## Observability

With AWF v0.28.35 or later, the `agent` artifact contains:

```text
sandbox/firewall/logs/api-proxy-logs/model-routing.jsonl
```

It records classification labels and mode, ranked choices, the selection, router version, and one terminal record per inference request. Request records report `as_selected` or `deviated` and the outcome; requests whose model or effort cannot be observed can be marked `unobserved`.

The adjacent `token-usage.jsonl` contains credits per request. Classifier usage is marked with `purpose: "routing_classification"`. Correlate inference usage with routing records by `request_id` when usage is available.

`gh aw logs` and `gh aw audit` do not yet provide a dedicated routing summary. Inspect the artifact's JSONL files for routing decisions and classifier-specific costs; dedicated CLI presentation will be available when routing-log support is added. See the [AWF routing audit-log schema](https://github.com/github/gh-aw-firewall/blob/main/docs/api-proxy-sidecar.md#model-routing-audit-log) for field definitions rather than relying on a fixed schema copied here.

## Troubleshooting

| Signal | Meaning and checks |
|---|---|
| Exit code `78` | Routing failed, rather than silently falling back to a fixed model. Check for `no_route` (no eligible model-and-effort choice), unavailable classifier or router, or configuration/contract errors. An upstream failure using the selected provider and model can also cause this exit code. Check allowed models, plan availability, table coverage, and the pinned image versions. |
| `degraded_reason` | Classification did not complete normally, and the record explains why. A degraded classification is not necessarily a terminal routing failure: inspect the following selection or failure record to see whether routing continued with fallback classification. |
| `routed: "deviated"` | Compare requested model, effort, and provider with the selected values and read `deviations`, `outcome`, and HTTP `status`. An allowed sub-agent using its declared model can be a normal deviation. A deviation is not itself a policy violation; an excluded model is still rejected. Endpoint differences alone do not mark a request as deviated. |
| Upstream HTTP or stream error | Inspect `sandbox/firewall/logs/api-proxy-logs/upstream-errors.jsonl` in the `agent` artifact alongside the routing log. In particular, a sub-agent HTTP 400 can indicate the API-family mismatch rather than a model-policy rejection. |

Use the [API proxy sidecar reference](https://github.com/github/gh-aw-firewall/blob/main/docs/api-proxy-sidecar.md) and [AWF configuration specification](https://github.com/github/gh-aw-firewall/blob/main/docs/awf-config-spec.md) for the upstream contracts and failure details.
