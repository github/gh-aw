---
title: Model Routing
description: Reduce inference cost by matching each workflow task to a model. Choose a routing level, control candidate models, and understand availability.
sidebar:
  order: 605
---

## Why use model routing

Model routing can reduce inference cost by choosing a model and reasoning effort suited to each workflow task, rather than using the same powerful model for every run. Routine work can use less expensive models, while harder tasks can use more capable ones.

Use routing when a workflow handles tasks of varying complexity, or when you want to balance cost and expected success without maintaining a fixed model choice. The router can choose another supported model when one is retired or unavailable, as long as an eligible candidate remains. Pin a model instead when you need a specific capability, explicit reasoning effort, or more consistent model selection.

> [!WARNING]
> Model routing is experimental, and its configuration and behavior may change. Routing does not guarantee task success or a spending limit. See [known limitations](#known-limitations).

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

Recompile the workflow with `gh aw compile` after changing its configuration. This example uses GitHub Copilot inference and needs [Copilot authentication](/gh-aw/reference/auth/#copilot-requests-write-permission). Routing also needs the AWF firewall, enabled by default; see [runtime requirements](#runtime-requirements) for supported environments.

Eligible workflows on GitHub Copilot inference already use routing when no model is set. To opt out without pinning a model, set `engine.model-routing: false`. See [default routing](#default-routing-for-workflows-without-a-model).

> [!NOTE]
> `model: auto` uses the engine or provider's own model selection, not gh-aw's per-task router. Use `model: router` for gh-aw routing.

## Setting the level yourself

Start with `model: router` when the work varies from run to run. The router assesses each task and chooses an economy, balanced, or robust level, then selects a model and effort for that level. Choose a fixed level when the workflow has a known tolerance for cost and failed runs:

| Value | Success target | Use it for |
|---|---|---|
| `router` | Chosen per task | Workflows that receive a mix of routine and difficult tasks. |
| `router/economy` | Lowest | Routine, low-risk tasks such as triage, labeling, and summaries. |
| `router/balanced` | Medium | General-purpose work. |
| `router/robust` | Highest | Tasks where a failed run is costly, such as opening pull requests. |

With these settings, the router favors lower cost at the chosen success target. The targets are benchmark estimates, not guarantees: it may choose below the target if no candidate meets it or if a choice just below it costs much less. For a faster response instead of the lowest cost, use [`goal: cost-speed`](#advanced-engine-model-routing).

## Supported models

Routing supports a specific set of models, not every model an engine can run. The AWF release this gh-aw version uses by default (**v0.29.14**) includes routing benchmark data for these GitHub Copilot model IDs:

<!-- Keep the version and model IDs aligned with DefaultFirewallVersion and AWF's routing tables. -->

| Family | Model IDs |
|---|---|
| Claude | `claude-haiku-5.5`, `claude-sonnet-5`, `claude-sonnet-5.5`, `claude-opus-5.5` |
| GPT 5.6 | `gpt-5.6-luna`, `gpt-5.6-sol`, `gpt-5.6-terra` |
| GPT 6 | `gpt-6-astra`, `gpt-6-luna`, `gpt-6-sol` |
| GPT 6.1 | `gpt-6.1-sol`, `gpt-6.1-terra` |
| Gemini | `gemini-3.8-flash`, `gemini-4-argon` |
| MAI | `mai-code-1.1-flash` |

This is a supported-model list, not an availability guarantee. At run time, candidates must also be available to the workflow's Copilot account or native provider account, compatible with the [engine and provider](#engines-and-providers), and permitted by the workflow's [model policy](#limiting-which-models-it-can-pick) and any [shortlist](#a-shortlist-of-models). Retired models and unsupported model-effort combinations are skipped. A prompt that exceeds a model's context capacity can also exclude it.

For native Anthropic inference, the bundled routing metadata supports `claude-sonnet-5`, `claude-sonnet-5-5`, and `claude-opus-5-5`. Native OpenAI candidates use the GPT names above, subject to the [native routing requirements](#native-anthropic-and-openai-routing). Gemini and MAI models can be routed through GitHub Copilot inference with the `copilot` or `pi` engine. The `gemini` engine does not support routing.

The list comes from the [routing tables bundled with AWF v0.29.14](https://github.com/github/gh-aw-firewall/tree/v0.29.14/containers/router/routing). If you pin a different `sandbox.agent.version` or supply custom images, its catalogue may differ. Run [`gh aw upgrade`](/gh-aw/setup/working-with-workflows/#upgrading-workflows) when gh-aw updates AWF to make newly supported models candidates. Use `gh aw audit <run-url> --models` to inspect the model catalogue recorded for a particular workflow run.

### What happens if no model is available?

If no eligible candidate remains, routing fails before the main agent starts. It does not silently use a fixed model, switch providers, or choose outside your policy or shortlist.

Check the run's Model Routing details with `gh aw audit <run-id>`, and its available models with `gh aw audit <run-id> --models`. Check whether the account exposes a supported model and whether your policy or shortlist excludes it. Widen those restrictions if appropriate. To pick up newer routing support, run [`gh aw upgrade`](/gh-aw/setup/working-with-workflows/#upgrading-workflows) and check any `sandbox.agent.version` pin. Alternatively, configure a fixed model instead of routing.

An unavailable model is different from an unavailable task classification. If classification fails or returns an unknown level, `model: router` uses the balanced level, but it still needs an eligible model.

## Limiting which models it can pick

Use a [model policy](/gh-aw/reference/glossary/#model-policy-modelsallowed-modelsblocked) to keep the main agent job within approved model families or exclude models you do not want it to use. `models.allowed` and `models.blocked` accept model names or `*` patterns and apply to every model request in that job, including sub-agents and built-in agents. The firewall rejects requests outside the policy. The separate [threat-detection job](/gh-aw/reference/threat-detection/) does not inherit these rules and may use a model outside them.

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

Patterns keep a policy open to newly supported models. To restrict only the main agent's router choices while leaving other agents free to use different permitted models, use a [shortlist](#a-shortlist-of-models).

## Engines and providers

Routing supports the Copilot, pi, Claude, and Codex engines, with the provider and model-family limits below. The `gemini` engine does not support routing. Claude and Codex route either on their native provider or on GitHub Copilot, but only within their own model family:

| Engine | Provider | Setting | Candidate models |
|---|---|---|---|
| `copilot` | GitHub Copilot | None | Any [supported model](#supported-models) |
| `pi` | GitHub Copilot | None | Any [supported model](#supported-models) |
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

In AWF v0.29.14, the native GPT entries in the routing tables have no bundled reasoning-effort metadata. Native OpenAI routing needs the provider's live catalogue to supply the missing API and effort information. If it does not, no GPT candidate is eligible and routing fails before the main agent starts. Use GitHub Copilot inference or a fixed model instead.

Routing never switches providers or credentials; native routing fails rather than falling back to GitHub Copilot. Native routing must be explicitly configured; the no-model default applies only to GitHub Copilot inference.

## Check which model was used

The agent step's log shows the selection, for example:

```text
inference routing: mode=awf-routed model=gpt-5.6-sol effort=high
```

`gh aw audit <run-id>` shows the requested value, such as `router/economy`, next to the model the router picked. Its Model Routing section includes the task classification, selected model and effort, ranked alternatives, and the costs of classification, the selected model, and any other models used. Use this to check whether routing is making suitable choices for your workflow.

`gh aw logs` aggregates routing across runs, showing which model each kind of task received and what it cost. See [Audit](/gh-aw/reference/audit/) and [Cost Management](/gh-aw/reference/cost-management/).

## Default routing for workflows without a model

Eligible workflows without a model already behave like `model: router`. For Copilot, Claude, and Codex, a nonempty `GH_AW_MODEL_AGENT_<ENGINE>` or `GH_AW_DEFAULT_MODEL_<ENGINE>` repository or organization variable overrides this default at run time; the agent variable wins. Pi has no such variables. Set `engine.model-routing: false` to opt out without pinning a model, or set a specific model.

Default routing also [fails before the main agent starts](#what-happens-if-no-model-is-available) if no eligible model remains, even if the workflow did not explicitly request routing.

<details>
<summary>Default routing eligibility</summary>

- The engine is `copilot`, `claude`, `codex`, or `pi`, using GitHub Copilot inference. Claude and Codex require `model-provider: github`; `copilot-requests: write` alone doesn't switch providers.
- Neither the workflow nor its imports set a top-level `model`, `engine.model` (including aliases, `auto`, and expressions), explicit routing settings, `engine.fallback-models`, or an `engine.env` model variable such as `COPILOT_MODEL` or `GH_AW_DEFAULT_MODEL_COPILOT`.
- The workflow's sandbox runtime, topology, mounts, and runner support routing, and the workflow doesn't use the `docker-sudo-iptables` runtime.
- The workflow uses a routing-capable AWF version and images and doesn't otherwise customize model selection or inference, for example through a custom engine command, model or effort arguments in `engine.args`, a custom agent, sub-agent models, a custom API target, or custom sandbox images.

The default applies only to the main agent, not threat detection, evals, or sub-agents. If eligibility isn't met or is uncertain, compilation preserves the previous behavior. When it applies, compilation prints an informational notice. Explicit router values aren't bypassed by model variables.

</details>

## Advanced: engine model routing

The `model: router` values cover cost-oriented routing. Use `engine.model-routing` when you also need a candidate shortlist or a speed-oriented goal. Both `goal` and `mode` are required: `goal: cost` favors lower cost, while `goal: cost-speed` favors the fastest model and effort meeting the target, even when a slower choice costs less.

For example, choose the level per task while favoring speed:

```aw wrap
engine:
  id: copilot
  model-routing:
    goal: cost-speed
    mode: auto
```

With `goal: cost`, `mode: auto` is equivalent to `model: router`; `mode: economy`, `balanced`, and `robust` correspond to the three fixed router levels. An explicit routing block overrides a fixed `engine.model` or effort setting and the compiler warns. Remove the fixed setting when enabling routing, and do not combine the block with a `model: router` value.

### A shortlist of models

Use `allowed-models` to limit the router's main-agent candidates. Unlike `models.allowed`, it does not restrict other model requests. It accepts exact model names and [model aliases](/gh-aw/reference/model-tables/#model-aliases), such as `sonnet`, but not patterns:

```aw wrap
engine:
  id: copilot
  model-routing:
    goal: cost
    mode: auto
    allowed-models: [gpt-5.6-luna, gpt-6-sol]
```

The model policy also narrows a shortlist; a shortlist cannot override it. For example, with `models.allowed: ["gpt-*"]`, a shortlisted Claude model is excluded and the compiler warns. If the policy excludes every entry, compilation fails. Exact names are not checked against current availability, so a shortlist can compile but still [leave no eligible model at run time](#what-happens-if-no-model-is-available).

See [shortlist resolution details](#shortlist-resolution-details) for alias expansion, provider prefixes, and expressions.

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

## Configuration details

### Choosing the level at run time

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

### Shortlist resolution details

The compiler expands aliases to patterns for the routing provider and narrows them to the engine's family. Exact names stay exact, including a bundled model ID that is also an alias; a matching provider prefix also makes a name exact. An incompatible provider, unknown entry, or alias with no models in the engine's family fails compilation. See the [model alias specification](/gh-aw/specs/model-alias-specification/) for expansion and provider rules.

Alias patterns wholly excluded by the policy are dropped or narrowed, while partial overlaps remain subject to the firewall's per-model policy. Rules match case-insensitively; GitHub Copilot provider prefixes are equivalent, while native routing uses bare rules or its own provider's prefix. Expressions such as `${{ vars.ROUTING_MODEL }}` are allowed in a shortlist only without a model policy; a literal prefix for native routing must name its native provider.

### Shared routing settings

Router values can also be set in `engine.model` or an imported workflow. `engine.model` takes precedence over the top-level `model`, and a workflow's own model takes precedence over an imported one.

A shared file can set a router value or a routing block. For example, `.github/workflows/shared/routing.md` can contain:

```aw wrap
---
engine:
  id: copilot
  model-routing:
    goal: cost
    mode: auto
    allowed-models: [gpt-5.6-luna, gpt-6-sol]
---
```

An importing workflow uses those settings. It must omit `engine` or use its object form, such as `engine: { id: copilot }`. If it also enables routing for the same engine, its goal and mode apply. If any routing configuration for that engine omits `allowed-models`, there is no shortlist; otherwise the lists are combined. For a shared router value instead, set `model: router/economy` in the shared file; importing workflows can override it with their own model.

### Router value restrictions

Router values are not model names. Unknown or differently cased values such as `router/fast` or `router/Economy`, and parameters such as `router/economy?effort=high`, are rejected. The router chooses effort; pin a model to set it. Router values can't appear in model policies, fallbacks, model aliases, sub-agents, threat detection, or evals, and can't be combined with a routing block or `engine.fallback-models`. Write the value in the workflow, or in a model expression as described in [Choosing the level at run time](#choosing-the-level-at-run-time). Otherwise, a variable or expression that resolves to a router value at run time stops the run before the agent starts.

## Known limitations

The classifier can select a different model or effort for the same task on another run. Routing doesn't consider the run's AI Credits budget (`max-ai-credits`, 1,000 by default); robust runs can consume more credits. See [Cap AI Credits per Run](/gh-aw/reference/cost-management/#cap-ai-credits-per-run).

### Runtime requirements

Routing runs on the default Docker sandbox runtime, or on `sandbox.agent.runtime: cloud-hypervisor` with AWF v0.29.12 or later, on a Linux AWF host. Compilation fails for explicit routing with another runtime such as `nvx`, `runner.topology: arc-dind`, a `sandbox.agent.mounts` entry that exposes the Docker socket to the agent, or a native Windows or macOS runner; `runs-on: windows-latest` runs AWF in WSL and is supported. Support for other runtimes is tracked in [gh-aw-firewall#10335](https://github.com/github/gh-aw-firewall/issues/10335).

## How routing works

Before the main agent starts, a small candidate model classifies the interpolated prompt as data, and the router ranks candidates and efforts using benchmark results. Classification typically costs a small fraction of an AI Credit. Routing stays within the candidates regardless of task text. The selected model and effort apply to the main agent for that run; routing does not choose a new model for each message.

## Troubleshooting

| Symptom | Cause and fix |
|---|---|
| Compilation says routing is not supported for this engine or runtime | Use `copilot`, `pi`, `claude`, or `codex` on a supported runtime; see [runtime requirements](#runtime-requirements). |
| Compilation rejects the provider or native endpoint | Use GitHub Copilot inference, or the engine's own native provider and HTTPS endpoint; remove custom targets. See [Engines and providers](#engines-and-providers). |
| Compilation rejects a shortlist entry or excludes all candidates | Use exact names or aliases from the error, not patterns; check the engine family and policy. See [A shortlist of models](#a-shortlist-of-models). |
| Compilation reports multiple engine fields after an import | Omit `engine` in the importing workflow or use `engine: { id: copilot }`. |
| The agent step fails before the main agent starts, often with exit code `78` | Routing could not select a model. Check the audit's Model Routing details for the reason, such as [no eligible model](#what-happens-if-no-model-is-available), a router error, or an engine compatibility issue. |
| A sub-agent fails with HTTP 400 in Copilot CLI mode | Its family differs from the selected main model. See [Sub-agents](#sub-agents). |
| `gh aw audit` reports deviated requests | Sub-agents or built-in agents may use another model; this is expected and isn't a policy violation. |
| A workflow without a model is routed and you want a fixed model | Set a model, an applicable model variable, or `engine.model-routing: false`; see [default routing](#default-routing-for-workflows-without-a-model). |
| `gh aw audit` reports degraded classification | Classification failed; `model: router` uses balanced in this case. |
