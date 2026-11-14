---
title: Model Routing
description: Let each workflow run use a model and reasoning effort suited to its task, chosen from the models the workflow allows.
sidebar:
  order: 605
---

> [!WARNING]
> Model routing is experimental, and its configuration and behavior may change. Review the [known limitations](#known-limitations) before enabling it.

## Why use model routing

Model routing chooses a model and reasoning effort for each workflow run based on its task. It can use less expensive models for routine work and stronger models for harder tasks, without pinning a model that may be retired. A fixed model is useful when a task needs a specific capability or more consistent behavior.

## Get started

Set `model: router` to route the main agent:

```aw wrap
---
on:
  issues:
    types: [opened]
permissions:
  contents: read
  issues: read
  copilot-requests: write
engine: copilot
model: router
safe-outputs:
  add-comment:
---

Find the likely cause of issue #${{ github.event.issue.number }} and comment with a suggested fix.
```

The router classifies each task as economy, balanced, or robust, then chooses the cheapest model and effort likely to succeed at that level. If classification is unavailable or unknown, it uses balanced. The example uses GitHub Copilot inference and needs [Copilot authentication](/gh-aw/reference/auth/#copilot-requests-write-permission). Routing also needs the AWF firewall, enabled by default.

Routing runs on the default Docker sandbox runtime, or on `sandbox.agent.runtime: cloud-hypervisor` with AWF v0.29.12 or later, on a Linux AWF host. Compilation fails for explicit routing with another runtime such as `nvx`, `runner.topology: arc-dind`, a `sandbox.agent.mounts` entry that exposes the Docker socket to the agent, or a native Windows or macOS runner; `runs-on: windows-latest` runs AWF in WSL and is supported. Support for other runtimes is tracked in [gh-aw-firewall#10335](https://github.com/github/gh-aw-firewall/issues/10335).

> [!NOTE]
> `model: auto` uses the engine or provider's own model selection, not gh-aw's per-task router. Use `model: router` for gh-aw routing.

Router values can also be set in `engine.model` or an imported workflow. `engine.model` takes precedence over the top-level `model`, and a workflow's own model takes precedence over an imported one.

## Setting the level yourself

Use a router level when the workflow's tasks have a known risk or complexity:

| Value | Success target | Use it for |
|---|---|---|
| `router/economy` | Lowest | Routine, low-risk tasks such as triage, labeling, and summaries. |
| `router/balanced` | Medium | General-purpose work. |
| `router/robust` | Highest | Tasks where a failed run is costly, such as opening pull requests. |

These are estimated success rates from benchmarks, not guarantees. The router may choose below the target if no candidate meets it or if a choice just below it costs much less. `model: router` chooses a level for each run instead.

## Choosing the level at run time

The model can be an expression that resolves to a router value when the run starts, so a dispatch input, a pre-activation step, or an experiment can choose the level:

```aw wrap
---
on:
  workflow_dispatch:
    inputs:
      level:
        type: string
engine: copilot
model: ${{ inputs.level || 'router/balanced' }}
---
```

The activation job resolves the expression once, and later jobs use its result, so the expression can read pre-activation step outputs. A router value routes the run at that level; any other value, such as a model name, runs that model without routing, as before. An empty value uses the model variables and the engine's default without routing, so write a default into the expression if you want one. A value that looks like a router value but isn't one, such as `router/fast`, `Router/Economy`, or `router/economy?effort=high`, stops the run before the agent starts.

This applies when the whole model is one expression, such as `${{ inputs.level }}` but not `${{ inputs.model }}?effort=high`, that reads only `inputs`, `vars`, run-wide `github` properties such as `github.event` or `github.run_attempt`, experiments, or pre-activation step outputs, and the workflow would otherwise be eligible for [default routing](#default-routing-for-workflows-without-a-model). Other expressions, such as ones that read an agent step's outputs or `github.job`, or call `hashFiles()`, run without routing as before. Pi isn't supported yet. Threat detection never receives the router value: when the run is routed, it uses its own default model. Router values in the `GH_AW_MODEL_AGENT_<ENGINE>` and `GH_AW_DEFAULT_MODEL_<ENGINE>` variables are still not accepted.

## Default routing for workflows without a model

Eligible workflows without a model already behave like `model: router`. For Copilot, Claude, and Codex, a nonempty `GH_AW_MODEL_AGENT_<ENGINE>` or `GH_AW_DEFAULT_MODEL_<ENGINE>` repository or organization variable overrides this default at run time; the agent variable wins. Pi has no such variables. Set `engine.model-routing: false` to opt out without pinning a model, or set a specific model.

<details>
<summary>Default routing eligibility</summary>

- The engine is `copilot`, `claude`, `codex`, or `pi`, using GitHub Copilot inference. Claude and Codex require `model-provider: github`; `copilot-requests: write` alone doesn't switch providers.
- Neither the workflow nor its imports set a top-level `model`, `engine.model` (including aliases, `auto`, and expressions), explicit routing settings, `engine.fallback-models`, or an `engine.env` model variable such as `COPILOT_MODEL` or `GH_AW_DEFAULT_MODEL_COPILOT`.
- The workflow's sandbox runtime, topology, mounts, and runner support routing, and the workflow doesn't use the `docker-sudo-iptables` runtime.
- The workflow uses a routing-capable AWF version and images and doesn't otherwise customize model selection or inference, for example through a custom engine command, model or effort arguments in `engine.args`, a custom agent, sub-agent models, a custom API target, or custom sandbox images.

The default applies only to the main agent, not threat detection, evals, or sub-agents. If eligibility isn't met or is uncertain, compilation preserves the previous behavior. When it applies, compilation prints an informational notice. Explicit router values aren't bypassed by model variables.

</details>

## Limiting which models it can pick

By default, the router considers supported models available to your organization's Copilot plan or native provider account. A [model policy](/gh-aw/reference/glossary/#model-policy-modelsallowed-modelsblocked) limits every model request in the run, including sub-agents and built-in agents; the firewall rejects requests outside it. Use model names or `*` patterns in `models.allowed` and `models.blocked`.

For example, route among GPT models only:

```aw wrap
engine: copilot
model: router
models:
  allowed: ["gpt-*"]
```

Or exclude Opus models while targeting robust results:

```aw wrap
engine: copilot
model: router/robust
models:
  blocked: ["claude-opus-*"]
```

The router only considers models with benchmark data in the AWF release pinned by gh-aw. Run [`gh aw upgrade`](/gh-aw/setup/working-with-workflows/#upgrading-workflows) when gh-aw updates AWF to make newly supported models candidates. Retired models and models unavailable to your account are skipped. Patterns keep the policy open to new models; a [shortlist](#a-shortlist-of-models) instead restricts the main agent's router choices.

## Engines and providers

Claude and Codex route either on their native provider or on GitHub Copilot, but only within their own model family:

| Engine | Provider | Setting | Candidate models |
|---|---|---|---|
| `copilot` | GitHub Copilot | None | Any |
| `pi` | GitHub Copilot | None | Any |
| `claude` | Anthropic (`ANTHROPIC_API_KEY`) | None, or `model-provider: anthropic` | `claude-*` only |
| `claude` | GitHub Copilot | `model-provider: github` | `claude-*` only |
| `codex` | OpenAI (`CODEX_API_KEY` or `OPENAI_API_KEY`) | None, or `model-provider: openai` | `gpt-*` only |
| `codex` | GitHub Copilot | `model-provider: github` | `gpt-*` only |

For native Anthropic routing:

```aw wrap
engine: claude
model: router/economy
```

For native OpenAI routing:

```aw wrap
engine: codex
model: router
```

For Claude on GitHub Copilot:

```aw wrap
engine:
  id: claude
  model-provider: github
model: router/balanced
```

### Native Anthropic and OpenAI routing

Native inference uses the workflow's provider API key in the firewall's API proxy and is billed by that provider, not the Copilot plan. AWF still accounts for the run's [AI Credits](/gh-aw/reference/cost-management/). Native model names follow the provider's naming, such as Anthropic's `claude-sonnet-5-5` rather than Copilot's `claude-sonnet-5.5`.

Native routing requires:

- The provider's own HTTPS endpoint (`https://api.anthropic.com` or `https://api.openai.com`), without a custom `engine.api-target`, base URL, `sandbox.agent.targets` gateway, or `engine.auth` for another cloud such as Azure OpenAI.
- A provider model list that includes the candidate, firewall metadata for its API and supported reasoning efforts, and router benchmark data. The policy and any shortlist must also allow it. Models without the required metadata are skipped.
- The AWF firewall and a routing-capable version; if `sandbox.agent.version` is pinned, use v0.28.33 or later. Organizations with approved registries can replace the router image using `sandbox.agent.images.router`; see [custom infrastructure images](/gh-aw/reference/sandbox/#custom-infrastructure-images-sandboxagentimages).

Routing doesn't switch providers or credentials, and fails rather than falling back to another provider. Native routing must be explicitly configured; the no-model default applies only to GitHub Copilot inference.

## Sub-agents

Routing chooses the main agent's model only. Declared sub-agents use their declared models, and built-in agents such as `explore` use their defaults, subject to the model policy. The compiler warns if a declared sub-agent model is outside the policy. `gh aw audit` reports sub-agent requests as deviations from the selected route, which is expected.

| Engine | Sub-agent models |
|---|---|
| Copilot in CLI mode (default) | Same family as the main agent's selected model, such as `gpt-*` or `claude-*` |
| Copilot in SDK mode (`engine.copilot-sdk: true`) | Any family |
| pi | Any family |
| Claude, Codex | The engine's own family |

In Copilot CLI mode, a sub-agent from a different family fails ([copilot-cli#5103](https://github.com/github/copilot-cli/issues/5103)), and the run logs a startup warning. Restrict candidates to the sub-agents' family or use SDK mode. Here SDK mode allows a Claude sub-agent regardless of the main agent's selected family:

```aw wrap
---
on: workflow_dispatch
permissions:
  contents: read
  copilot-requests: write
engine:
  id: copilot
  copilot-sdk: true
model: router
---

Review the open issues and ask the `triager` agent to suggest a label for each one.

## agent: `triager`
---
description: Suggests one label for an issue
model: claude-haiku-4.5
---
Reply with the single best label for the issue you are given.
```

## Check which model was used

The agent step's log shows the selection, for example:

```text
inference routing: mode=awf-routed model=gpt-5.6-sol effort=high
```

`gh aw audit <run-id>` shows the requested value, such as `router/economy`, next to the model the router picked, and includes a Model Routing section with the task classification, the selected model and effort, the ranked alternatives, and what the classifier, the selected model, and any other models cost. `gh aw logs` aggregates routing across runs, showing which model each kind of task received and what it cost. See [Audit](/gh-aw/reference/audit/) and [Cost Management](/gh-aw/reference/cost-management/).

## Advanced: engine model routing

Use `engine.model-routing` for a candidate shortlist or a speed-oriented goal. The following router values are equivalent to blocks with `goal: cost`:

| Router value | Equivalent block |
|---|---|
| `router` | `model-routing: {goal: cost, mode: auto}` |
| `router/economy` | `model-routing: {goal: cost, mode: economy}` |
| `router/balanced` | `model-routing: {goal: cost, mode: balanced}` |
| `router/robust` | `model-routing: {goal: cost, mode: robust}` |

Both `goal` and `mode` are required. `goal: cost-speed` favors the fastest model and effort meeting the target, even when a slower choice costs less:

```aw wrap
engine:
  id: copilot
  model-routing:
    goal: cost-speed
    mode: auto
```

An explicit block overrides a fixed `engine.model` or effort setting and the compiler warns. Remove the fixed setting when enabling routing.

### A shortlist of models

`allowed-models` limits the router's main-agent candidates, not other requests. It accepts exact model names and [model aliases](/gh-aw/reference/model-tables/#model-aliases), but not patterns:

```aw wrap
engine:
  id: copilot
  model-routing:
    goal: cost
    mode: auto
    allowed-models: [gpt-5.4-mini, gpt-5.6-sol, small, sonnet]
```

The compiler expands aliases to patterns for the routing provider and narrows them to the engine's family. Exact names stay exact, including a bundled model ID that is also an alias; a matching provider prefix also makes a name exact. An incompatible provider, unknown entry, or alias with no models in the engine's family fails compilation. Exact names aren't checked against current availability. If no shortlisted model is available at run time, routing fails. See the [model alias specification](/gh-aw/specs/model-alias-specification/) for expansion and provider rules.

The model policy also narrows a shortlist; it cannot be overridden by one:

```aw wrap
models:
  allowed: ["gpt-*"]
engine:
  id: copilot
  model-routing:
    goal: cost
    mode: balanced
    allowed-models: [gpt-5.4-mini, gpt-5.6-sol, claude-sonnet-5]
```

The compiler warns that `claude-sonnet-5` is excluded; if every entry is excluded, compilation fails. Alias patterns wholly excluded by the policy are dropped or narrowed, while partial overlaps remain subject to the firewall's per-model policy. Rules match case-insensitively; GitHub Copilot provider prefixes are equivalent, while native routing uses bare rules or its own provider's prefix. Expressions such as `${{ vars.ROUTING_MODEL }}` are allowed in a shortlist only without a model policy; a literal prefix for native routing must name its native provider.

### Shared routing settings

A shared file can set a router value or a routing block. For example, `.github/workflows/shared/routing.md` can contain:

```aw wrap
---
engine:
  id: copilot
  model-routing:
    goal: cost
    mode: auto
    allowed-models: [gpt-5.4-mini, gpt-5.6-sol]
---
```

An importing workflow uses those settings. It must omit `engine` or use its object form, such as `engine: { id: copilot }`. If it also enables routing for the same engine, its goal and mode apply. If any routing configuration for that engine omits `allowed-models`, there is no shortlist; otherwise the lists are combined. For a shared router value instead, set `model: router/economy` in the shared file; importing workflows can override it with their own model.

### Router value restrictions

Router values are not model names. Unknown or differently cased values such as `router/fast` or `router/Economy`, and parameters such as `router/economy?effort=high`, are rejected. The router chooses effort; pin a model to set it. Router values can't appear in model policies, fallbacks, model aliases, sub-agents, threat detection, or evals, and can't be combined with a routing block or `engine.fallback-models`. Write the value in the workflow, or in a model expression as described in [Choosing the level at run time](#choosing-the-level-at-run-time). Otherwise, a variable or expression that resolves to a router value at run time stops the run before the agent starts.

## Known limitations

The classifier can select a different model or effort for the same task on another run. Routing doesn't consider the run's AI Credits budget (`max-ai-credits`, 1,000 by default); robust runs can consume more credits. See [Cap AI Credits per Run](/gh-aw/reference/cost-management/#cap-ai-credits-per-run).

Before the main agent starts, a small candidate model classifies the interpolated prompt as data, and the router ranks candidates and efforts using benchmark results. Classification typically costs a small fraction of an AI Credit. Routing stays within the candidates regardless of task text and fails if it can't choose a model; it doesn't fall back to a fixed model.

## Troubleshooting

| Symptom | Cause and fix |
|---|---|
| Compilation rejects the provider or native endpoint | Use GitHub Copilot inference, or the engine's own native provider and HTTPS endpoint; remove custom targets. See [Engines and providers](#engines-and-providers). |
| Compilation rejects a shortlist entry or excludes all candidates | Use exact names or aliases from the error, not patterns; check the engine family and policy. See [A shortlist of models](#a-shortlist-of-models). |
| Compilation reports multiple engine fields after an import | Omit `engine` in the importing workflow or use `engine: { id: copilot }`. |
| The agent step fails and `gh aw audit` reports a routing failure | No candidate may be available, the router may have failed (exit code `78`), or the engine may not support the selection. Check the audit's Model Routing details. |
| A sub-agent fails with HTTP 400 in Copilot CLI mode | Its family differs from the selected main model. See [Sub-agents](#sub-agents). |
| `gh aw audit` reports deviated requests | Sub-agents or built-in agents may use another model; this is expected and isn't a policy violation. |
| A workflow without a model is routed and you want a fixed model | Set a model, an applicable model variable, or `engine.model-routing: false`; see [default routing](#default-routing-for-workflows-without-a-model). |
| `gh aw audit` reports degraded classification | Classification failed; `model: router` uses balanced in this case. |
