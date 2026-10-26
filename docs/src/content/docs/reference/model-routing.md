---
title: Model Routing
description: Let each workflow run use a model and reasoning effort suited to its task, chosen from the models the workflow allows.
sidebar:
  order: 605
---

> [!WARNING]
> Model routing is experimental, and its configuration and behavior may change. Review the [known limitations](#known-limitations) before enabling it.

Model routing chooses the model and reasoning effort for each run of a workflow, based on the task that run receives. Instead of naming one model in `engine.model` and using it for everything, you state what to optimize for and which models are acceptable, and a router makes the choice for each run.

## Why use model routing

**Right-size the model for each task.** Most workflows get tasks of varying difficulty. An issue-triage workflow might get a typo report one day and a bug that spans several services the next. A single fixed model is a compromise: a large model spends AI Credits on easy tasks, and a small model struggles with hard ones. With routing, routine tasks run on small, inexpensive models, and harder tasks get stronger models or more reasoning effort. The workflow can cost less than it would on one large model, and produce better results on hard tasks than it would on one small model.

**Stop hard-coding model names.** Models are added and retired regularly. A workflow that pins a model in `engine.model` fails when that model is retired, and it keeps using that model after better or cheaper ones appear, until someone edits it. A routed workflow describes which models are acceptable, for example every GPT model, or every model except the most expensive ones. The router skips models that are no longer available, and new models become candidates when you upgrade gh-aw. See [Keeping up with new models](#keeping-up-with-new-models).

A fixed model is still the better fit when you need the same behavior on every run, because routing can choose differently for similar tasks, or when the task depends on one model's specific capabilities.

## Get started

To route a workflow, add `model-routing` under `engine` with a `goal` and a `mode`:

```aw wrap
---
on:
  issues:
    types: [opened]
permissions:
  contents: read
  issues: read
  copilot-requests: write
engine:
  id: copilot
  model-routing:
    goal: cost
    mode: auto
safe-outputs:
  add-comment:
---

Find the likely cause of issue #${{ github.event.issue.number }} and comment with a suggested fix.
```

This configuration is a good starting point for most workflows. `goal: cost` prefers the cheapest choice that is likely to succeed, and `mode: auto` lets the router judge how demanding each task is. Because the workflow doesn't restrict models, the router can choose any model it supports that your organization's Copilot plan offers. [Choose which models the router can use](#choose-which-models-the-router-can-use) shows how to narrow that.

Routing currently works only with models served through GitHub Copilot, so the workflow needs Copilot authentication, here through the [`copilot-requests: write`](/gh-aw/reference/auth/#copilot-requests-write-permission) permission. Workflows that call model providers such as Anthropic or OpenAI directly, with their own API keys, can't use routing yet; see [GitHub Copilot models only](#github-copilot-models-only). Routing also needs the AWF firewall, which is enabled by default. The Copilot and pi engines need nothing else. Claude and Codex need one more setting; see [Use routing with Claude, Codex, or pi](#use-routing-with-claude-codex-or-pi).

Compile and run the workflow as usual. To see which model a run used, see [Check which model was used](#check-which-model-was-used).

If the workflow already sets `engine.model`, or another fixed model or reasoning effort setting, routing overrides it and the compiler warns. Remove the fixed setting once routing is enabled.

## Choose a goal and mode

`goal` and `mode` are both required. Together they tell the router how to trade cost against the chance that the task succeeds.

The **mode** sets how likely the chosen model and effort should be to complete the task. Higher targets lead to stronger models and more reasoning effort, and so to higher cost:

| `mode` | Target | Use it for |
|---|---|---|
| `economy` | Lowest | Routine, low-risk tasks such as triage, labeling, and summaries. |
| `balanced` | Medium | General-purpose work. |
| `robust` | Highest | Tasks where a failed run is costly, such as workflows that open pull requests. |
| `auto` | Chosen per run | Workflows whose tasks vary. The router picks `economy`, `balanced`, or `robust` for each task. |

The **goal** decides which of the choices that meet the target the router prefers:

| `goal` | Prefers |
|---|---|
| `cost` | The cheapest model and effort that meet the target. |
| `cost-speed` | The fastest model and effort that meet the target, even when a slower choice would cost less. |

If you're unsure, start with `goal: cost` and `mode: auto`. Set a fixed mode when you know what kind of tasks the workflow gets, for example `economy` for a labeling workflow. Choose `cost-speed` when someone is waiting on the result, such as a workflow that replies to a comment.

Targets are success rates estimated from benchmark results on similar tasks, not guarantees for a particular repository. The router can choose below the target when no candidate meets it or, with `goal: cost`, when a choice just below the target costs much less.

## Choose which models the router can use

By default, the router chooses from every model it supports that your organization's Copilot plan offers. Two settings narrow that, and they do different jobs:

| Setting | What it does | Applies to | Entries |
|---|---|---|---|
| `models.allowed` and `models.blocked` | Sets which models the workflow may use at all. The firewall rejects requests for other models. | Every model request in the run: the main agent, sub-agents, and built-in agents | Model names or patterns with `*` |
| `engine.model-routing.allowed-models` | Gives the router a shortlist to choose from. | Only the router's choice for the main agent | Exact model names or [model aliases](#a-shortlist-of-model-aliases) |

`models.allowed` and `models.blocked` are the workflow's [model policy](/gh-aw/reference/glossary/#model-policy-modelsallowed-modelsblocked), and they work the same way with or without routing. Use them for rules such as "only GPT models" or "never Opus models". Use `allowed-models` when you want the router to choose among a few specific models. Most workflows need only one of the two. The examples below show common setups.

### Only one model family

To let the router choose among all GPT models, put a pattern in the model policy and omit `allowed-models`:

```aw wrap
models:
  allowed: ["gpt-*"]
engine:
  id: copilot
  model-routing:
    goal: cost
    mode: auto
```

New GPT models become candidates without editing the workflow. Because the policy applies to the whole run, sub-agents are also limited to GPT models.

> [!NOTE]
> `allowed-models` accepts exact model names and gh-aw model aliases, not patterns. A pattern such as `allowed-models: ["gpt-*"]` fails to compile. Put patterns in `models.allowed` or `models.blocked` instead.

### Every model except the most expensive

To keep the router away from the most expensive models, block them in the model policy:

```aw wrap
models:
  blocked: ["claude-opus-*"]
engine:
  id: copilot
  model-routing:
    goal: cost
    mode: robust
```

This is useful with `mode: robust`, which chooses stronger models more often.

### A fixed shortlist

To let the router choose only among models you've tested the workflow with, list them in `allowed-models`:

```aw wrap
engine:
  id: copilot
  model-routing:
    goal: cost
    mode: auto
    allowed-models: [gpt-5.4-mini, gpt-5.6-luna, gpt-5.6-sol]
```

The router never considers other models, including new ones, so update the list as models change. If a listed model is retired, the router chooses among the others, and if none is available, the run fails. The shortlist doesn't affect sub-agents.

### A shortlist of model aliases

To keep a shortlist working when models are retired, list [model aliases](/gh-aw/reference/model-tables/#model-aliases) such as `small` or `sonnet` instead of exact names:

```aw wrap
engine:
  id: copilot
  model-routing:
    goal: cost
    mode: auto
    allowed-models: [small, sonnet]
```

The compiler expands each alias into the GitHub Copilot model patterns it stands for, here `github-copilot/*haiku*`, `github-copilot/gpt-5*mini*`, `github-copilot/gpt-5*nano*`, `github-copilot/gemini-*flash*lite*`, and `github-copilot/*sonnet*`, and the router chooses among the available models that match. Routing always uses GitHub Copilot inference, so only an alias's `copilot/` patterns are used, whichever engine runs the workflow. Exact names stay exact, and you can mix them with aliases, as in `[gpt-5.6-luna, small]`.

Some names are both an alias and a GitHub Copilot model id, such as `gpt-5.4` and `gpt-5-mini`. When a name is a GitHub Copilot model id in gh-aw's bundled model catalogue, it stays exact, just as `engine.model` uses a model id over an alias with the same name. A `github-copilot/` or `copilot/` prefix, as in `github-copilot/gemini-3.8-flash`, always makes an entry exact. To route over an alias's models instead, list another alias that covers them, or define an alias with a different name under `models`.

An entry that is neither an alias nor an exact model name, such as the typo `smal`, fails to compile, and the error lists the valid aliases. Exact names aren't checked against the models currently available.

### A policy and a shortlist together

When a workflow sets both, the router chooses from the `allowed-models` entries that the policy allows. `allowed-models` can't permit a model that the policy excludes:

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

Here the router chooses `gpt-5.4-mini` or `gpt-5.6-sol`, and the compiler warns that `claude-sonnet-5` won't be selected. If the policy excludes every entry, compilation fails and the error names the excluded entries.

An alias is narrowed by the policy in the same way. A pattern that a `models.blocked` rule fully covers is dropped, and a pattern outside `models.allowed` is narrowed to the `models.allowed` entries it includes: with `models.allowed: [claude-haiku-4.5, gpt-5.4-mini]`, `allowed-models: [small]` becomes `claude-haiku-4.5` and `gpt-5.4-mini`. When a block or allow pattern only partly overlaps an alias pattern, such as `models.blocked: [claude-sonnet-4.5]` with `sonnet`, the alias pattern stays in the shortlist and the firewall applies the policy to each available model, so the router never selects a blocked model. The compiler warns about aliases that the policy excludes entirely. As in the firewall, policy rules match model names case-insensitively, and the `copilot/`, `github-copilot/`, and `github/` providers all match GitHub Copilot models.

`allowed-models` entries can be GitHub Actions expressions, such as `${{ vars.ROUTING_MODEL }}`, but only when the workflow sets neither `models.allowed` nor `models.blocked`, because the compiler can't check an expression against the policy.

### Shared routing settings

To use the same routing settings in several workflows, put them in a shared file and import it. For example, `.github/workflows/shared/routing.md`:

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

A workflow that imports it is routed with those settings:

```aw wrap
---
on: workflow_dispatch
permissions:
  contents: read
  copilot-requests: write
imports:
  - shared/routing.md
---

Summarize the open pull requests.
```

The importing workflow must either omit `engine` or use its object form, such as `engine: { id: copilot }`. If it also enables routing for the same engine, its `goal` and `mode` apply. Among workflows that enable routing for the same engine, if any omits `allowed-models`, the router has no shortlist; otherwise, their lists are combined.

### Keeping up with new models

The router chooses only among models it has benchmark data for. That data is part of the AWF release that gh-aw pins when it compiles the workflow. When gh-aw updates AWF with data for new models, run [`gh aw upgrade`](/gh-aw/setup/working-with-workflows/#upgrading-workflows) to recompile your workflows, and the new models become candidates if your settings allow them. Models that have been retired, or that your organization's Copilot plan doesn't offer, are skipped at run time.

To get the most from this, describe models with patterns in the model policy, or don't restrict them, rather than listing exact names in `allowed-models`.

## Sub-agents

Routing chooses the model for the main agent only. Declared sub-agents use the model they declare, and built-in agents such as `explore` use their default models, as long as the model policy allows them. The compiler warns when a sub-agent's model is outside the policy, because the firewall will reject its requests. `gh aw audit` reports sub-agent requests as deviations from the selected route, which is expected.

A sub-agent's model must also work with the engine and with the main agent's selected model:

| Engine | Sub-agent models |
|---|---|
| Copilot in CLI mode (default) | Same family as the main agent's selected model, such as `gpt-*` or `claude-*` |
| Copilot in SDK mode (`engine.copilot-sdk: true`) | Any family |
| pi | Any family |
| Claude, Codex | The engine's own family |

In Copilot CLI mode, a sub-agent from a different family than the selected main model fails ([copilot-cli#5103](https://github.com/github/copilot-cli/issues/5103)), and the run logs a warning at startup. If a CLI-mode workflow declares sub-agents, either keep every candidate in the sub-agents' family or use SDK mode. In this example, the router can choose a GPT or Claude model for the main agent, and the Claude sub-agent works with either:

```aw wrap
---
on: workflow_dispatch
permissions:
  contents: read
  copilot-requests: write
engine:
  id: copilot
  copilot-sdk: true
  model-routing:
    goal: cost
    mode: auto
    allowed-models: [gpt-5.4-mini, gpt-5.6-sol, claude-sonnet-5]
---

Review the open issues and ask the `triager` agent to suggest a label for each one.

## agent: `triager`
---
description: Suggests one label for an issue
model: claude-haiku-4.5
---
Reply with the single best label for the issue you are given.
```

## Use routing with Claude, Codex, or pi

Routing works with the `copilot`, `claude`, `codex`, and `pi` engines, as long as they use models served through GitHub Copilot. Claude and Codex call the Anthropic and OpenAI APIs by default, so they need `model-provider: github` to use Copilot instead, and they can only use models from their own family:

| Engine | Required setting | Candidate models |
|---|---|---|
| `copilot` | None | Any |
| `pi` | None | Any |
| `claude` | `model-provider: github` | `claude-*` only |
| `codex` | `model-provider: github` | `gpt-*` only |

For example, to route a Claude workflow between two Claude models:

```aw wrap
engine:
  id: claude
  model-provider: github
  model-routing:
    goal: cost
    mode: balanced
    allowed-models: [claude-sonnet-5, claude-opus-5]
```

Without `allowed-models`, Claude and Codex choose from every model in their family that the policy allows. The compiler rejects other engines and providers, and `allowed-models` entries from another family.

Aliases are limited to the engine's family as well. With Claude, `allowed-models: [small]` keeps only the alias's Claude models (`claude-*haiku*`), and with Codex, only its GPT models (`gpt-5*mini*` and `gpt-5*nano*`). An alias pattern that doesn't start with the family name is narrowed to the family only when the alias also lists the engine's own provider, as `sonnet` lists `anthropic/*sonnet*`, and the narrowed pattern matches a GitHub Copilot model in gh-aw's bundled catalogue. An alias with no models in the engine's family, such as `sonnet` or `grok` with Codex, fails to compile.

Routing also has these requirements:

- It needs the AWF firewall, which is the default sandbox.
- It can't be combined with `engine.fallback-models`.
- gh-aw's default AWF version supports routing. If a workflow pins AWF with `sandbox.agent.version`, use v0.28.33 or later.
- Organizations that require images from approved registries can replace the router image with `sandbox.agent.images.router`; see [Custom infrastructure images](/gh-aw/reference/sandbox/#custom-infrastructure-images-sandboxagentimages).

## How routing works

Routing happens once per run, before the agent starts:

1. A small model classifies the task by type (such as fix, refactor, or explain), scope, and complexity. It reads the workflow's prompt, including interpolated content such as the triggering issue's text, and treats that text as data, not instructions.
2. The [router](https://github.com/github/gh-aw-firewall/tree/main/containers/router) ranks the candidate models and efforts for that kind of task, using benchmark results, and chooses one according to the workflow's `goal` and `mode`.
3. The main agent runs the whole task with the chosen model and effort.

The router chooses only from the workflow's candidates, whatever the task text says. Classification runs on one of the candidates, preferring a small one, and typically costs a small fraction of an AI Credit per run. If routing can't choose a model, the run fails rather than falling back to a fixed model. Threat detection isn't routed and uses its configured detection model.

## Check which model was used

The agent step's log shows the selection, for example:

```text
inference routing: mode=awf-routed model=gpt-5.6-sol effort=high
```

`gh aw audit <run-id>` includes a Model Routing section with the task classification, the selected model and effort, the ranked alternatives, and what the classifier, the selected model, and any other models cost. `gh aw logs` aggregates routing across runs, showing which model each kind of task received and what it cost. See [Audit](/gh-aw/reference/audit/) and [Cost Management](/gh-aw/reference/cost-management/).

## Known limitations

### GitHub Copilot models only

The router currently chooses only among models served through GitHub Copilot, because its benchmark data and model availability checks cover Copilot's models. The compiler rejects routing in workflows that use another provider, such as Claude or Codex with their own Anthropic or OpenAI API keys, and in workflows on engines other than `copilot`, `claude`, `codex`, and `pi`. This limitation is temporary, and support for more providers and engines is planned.

### Selection varies between runs

The classifier is a language model, so the same task can get a different model or effort on another run, especially with `mode: auto`.

### Reasoning effort with Claude

The router doesn't yet account for which reasoning efforts each engine supports. If it chooses an effort Claude can't use (`none` or `minimal`), the run fails. Codex accepts every effort the router can choose, from `none` to `max`, and runs with the selected effort unchanged.

### AI Credits budget

Routing doesn't consider the run's AI Credits budget (`max-ai-credits`, 1,000 by default). Runs routed to larger models, which `robust` chooses more often, use more of it. See [Cap AI Credits per Run](/gh-aw/reference/cost-management/#cap-ai-credits-per-run).

### Sub-agents in Copilot CLI mode

Sub-agents from a different model family than the selected main model fail. See [Sub-agents](#sub-agents).

## Troubleshooting

| Symptom | Cause and fix |
|---|---|
| Compilation fails because routing requires GitHub Copilot inference | A Claude or Codex workflow uses its default provider. Set `engine.model-provider: github`. See [GitHub Copilot models only](#github-copilot-models-only). |
| Compilation fails when `allowed-models` contains a pattern such as `gpt-*` | `allowed-models` takes exact names and aliases. Put the pattern in `models.allowed` instead. See [Only one model family](#only-one-model-family). |
| Compilation fails because an `allowed-models` entry is neither a gh-aw model alias nor an exact model id | The entry is misspelt, such as `smal`. Use an alias from the list in the error or an exact model name. See [A shortlist of model aliases](#a-shortlist-of-model-aliases). |
| Compilation fails because all `allowed-models` are excluded by `models.allowed` or `models.blocked` | The policy excludes every candidate or, for Claude or Codex without `allowed-models`, the engine's whole family. Change either setting. |
| Compilation fails with an incompatible model-routing candidate | An `allowed-models` entry, or every model of an alias, isn't in the engine's model family. Remove it. |
| Compilation fails with "multiple engine fields found" after importing routing settings | The importing workflow uses `engine: copilot`. Remove `engine`, or use `engine: { id: copilot }`. |
| The agent step fails and `gh aw audit` reports a routing failure | Routing couldn't produce a usable selection: no candidate was available to the organization, the router failed (exit code `78`), or the engine can't use the selected model or effort. The audit's Model Routing section shows the failure code and details. |
| A sub-agent's requests fail with HTTP 400 in Copilot CLI mode | The sub-agent's model family differs from the selected main model. See [Sub-agents](#sub-agents). |
| `gh aw audit` reports deviated requests | Requests used a model other than the selected one, usually from sub-agents or built-in agents. This is expected and isn't a policy violation. |
| `gh aw audit` reports degraded classification | Classification failed, so routing continued without it; with `mode: auto`, the router used `balanced`. |
