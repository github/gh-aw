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

Organizations requiring approved registries can override the router image through `sandbox.agent.images.router`. Use a compatible, digest-pinned image; see [Sandbox image overrides](/gh-aw/reference/sandbox/).

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

The adjacent `token-usage.jsonl` contains credits per request. Classifier usage is marked with `purpose: "routing_classification"` and joined to routing requests by `request_id`.

`gh aw audit <run-id>` reports whether routing selected a route or failed, the objective and classification, selected model/effort/endpoint, top ranked choices, router version and latency, request outcomes and deviations, and a cost split for classifier, selected-model, and deviated traffic. The comparison notes model, effort, mode, or router-version changes even when total cost is similar. With `gh aw logs --format pretty|markdown` (or `--json`), the Model Routing section aggregates label-to-selection routes across runs, including run counts, total and average AIC, classifier cost, and deviation share. For example:

```text
Model Routing
  classifier_aic=0.059 deviated_requests=0/4 (0.0%)
  explain/local/trivial mode=economy selected=gpt-5.6-luna:high router=0.1.3 runs=1 aic_total=0.760 aic_avg=0.760
```

The per-run audit JSON includes `model_routing` with the same decision, request, failure, and cost details; logs JSON includes the cross-run `model_routing.routes` aggregate. See the [AWF routing audit-log schema](https://github.com/github/gh-aw-firewall/blob/main/docs/api-proxy-sidecar.md#model-routing-audit-log) for the complete record definition.

## Troubleshooting

| Signal | Meaning and checks |
|---|---|
| Exit code `78` | Routing failed, rather than silently falling back to a fixed model. The audit routing section shows failure `code` and `detail`; check for `no_route`, unavailable classifier/router, or configuration errors. |
| `degraded_reason` | The audit routing section reports why classification degraded and whether the router continued with fallback classification or failed. |
| `routed: "deviated"` | Audit shows request counts and the requested/selected models and efforts for deviations. An allowed sub-agent using its declared model can be a normal deviation; deviation is not itself a policy violation. For records from AWF before v0.28.39, an endpoint-only deviation is counted as selected-model traffic and the report notes this normalization. |
| Upstream HTTP or stream error | Inspect `sandbox/firewall/logs/api-proxy-logs/upstream-errors.jsonl` in the `agent` artifact alongside the routing log. In particular, a sub-agent HTTP 400 can indicate the API-family mismatch rather than a model-policy rejection. |

Use the [API proxy sidecar reference](https://github.com/github/gh-aw-firewall/blob/main/docs/api-proxy-sidecar.md) and [AWF configuration specification](https://github.com/github/gh-aw-firewall/blob/main/docs/awf-config-spec.md) for the upstream contracts and failure details.
