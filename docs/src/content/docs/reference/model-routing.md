---
title: Model Routing
description: Let each workflow run choose a model, pick a routing level, and check which model did the work.
sidebar:
  order: 605
---

An issue triage workflow might get a one-line question today and a hard-to-reproduce bug tomorrow. With `model: router`, the main agent can use a less expensive model for the question and a more capable one for the bug. Routing chooses a model and reasoning effort for each run, not for each message.

Use routing when the work varies. Pin a model instead if the workflow needs a particular capability or you want the same model every time. Routing is experimental; it does not guarantee success or cap spending.

## Get started

Set `model: router` in your workflow's frontmatter. This workflow reads each new issue and posts one suggested next step:

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

Read the issue. Suggest one next step that would help the author, and explain why.
```

If your workflow already pins a model under `engine`, remove that pin so the top-level choice takes effect. Run `gh aw compile` and commit the updated workflow and lock file together. This example uses [GitHub Copilot authentication](/gh-aw/reference/auth/#copilot-requests-write-permission) and the default firewall.

<span id="setting-the-level-yourself"></span>

## Choose a routing level

| Set `model` to | When to use it |
|---|---|
| `router` | Let each task determine the level. Start here for a workflow that handles a mix of requests. |
| `router/economy` | Favor lower cost for routine work, such as labeling issues or summarizing activity. |
| `router/balanced` | Use a consistent middle ground for general work, such as reviewing issues for missing information. |
| `router/robust` | Aim for a higher chance of success when a missed finding or a second attempt would be costly. |

These are levels, not model names. Two runs at the same level need not use the same model. `model: auto` is different: it leaves model selection to the engine or provider rather than the gh-aw router.

The deprecated nested `engine.model-routing.goal: cost-speed` requires an AWF pin older than v0.29.17. With the default firewall, choose a cost-routing level explicitly; `cost-speed` is not an alias for `cost`.

## A shortlist of models

To let the main agent choose between specific models, give it a shortlist alongside a literal router value:

```aw wrap
model: router
model-routing:
  allowed-models: [gpt-5.6-luna, gpt-6-sol]
```

Use exact model names or [model aliases](/gh-aw/reference/model-tables/), not wildcard patterns. Declare the list once, either in the workflow or in a [shared import](/gh-aw/reference/imports/).

The shortlist does **not** restrict sub-agents. For example, a Claude sub-agent can still use its own model while the main agent chooses between the two GPT models above, if the engine supports that combination. Copilot SDK mode supports mixed families; Copilot CLI mode needs the sub-agent and the selected main model to be in the same family. To restrict *every* model request in the main agent's job, including sub-agent requests, use a [model policy](/gh-aw/reference/glossary/#model-policy-modelsallowed-modelsblocked) instead. A shortlist cannot override that policy.

## Default routing for workflows without a model

Eligible workflows using GitHub Copilot inference already route when no model is set. To keep your usual model overrides and engine default **without** routing, omit `model` and set:

```aw wrap
model-routing: false
```

Don't combine the opt-out with a router value or shortlist. A configured runtime model override can take precedence over implicit routing, but does not bypass an explicit `model: router`.

Routing picks models from the live model catalogue. With [`engine.model-catalog: false`](/gh-aw/reference/engines/#model-aliases) (for example, for Azure AI Gateway), implicit routing is not applied and `model: router` fails compilation.

<span id="engines-and-providers"></span>

## Which engines can route?

Copilot and Pi can route on GitHub Copilot. Claude and Codex can also route on GitHub Copilot with `model-provider: github`; Claude chooses among Claude models and Codex among GPT models. The Gemini engine does not support routing.

### Native Anthropic and OpenAI routing

For [Claude on Anthropic](/gh-aw/reference/engines/claude/) or [Codex on OpenAI](/gh-aw/reference/engines/codex/), set a router value explicitly. Routing uses your configured provider and its billing; it does not switch accounts or providers to find a model.

## Which models does the router support?

The [routing tables in Agent Workflow Firewall (AWF) v0.29.17](https://github.com/github/gh-aw-firewall/tree/v0.29.17/containers/router/routing), the version gh-aw uses by default, include these GitHub Copilot model IDs:

<!-- When DefaultFirewallVersion changes, check this list against the new AWF routing tables. -->

| Family | GitHub Copilot model IDs |
|---|---|
| Claude | `claude-haiku-5.5`, `claude-sonnet-5`, `claude-sonnet-5.5`, `claude-opus-5.5` |
| GPT 5.6 | `gpt-5.6-luna`, `gpt-5.6-sol`, `gpt-5.6-terra` |
| GPT 6 | `gpt-6-astra`, `gpt-6-luna`, `gpt-6-sol` |
| GPT 6.1 | `gpt-6.1-sol`, `gpt-6.1-terra` |
| Gemini | `gemini-3.8-flash`, `gemini-4-argon` |
| MAI | `mai-code-1.1-flash` |

For native OpenAI, the tables use the same GPT IDs. For native Anthropic, they use `claude-haiku-5-5`, `claude-sonnet-5`, `claude-sonnet-5-5`, and `claude-opus-5-5`.

At least one supported model must be enabled for the workflow's provider account and eligible for its engine, model policy, and shortlist. A model also needs a compatible API endpoint and reasoning effort. **If no model qualifies, the run fails before the main agent starts.** If the workflow pins a different AWF version, check that version's routing tables.

## Check which model was used

Run `gh aw audit <run-url>` and look at **Model Routing**. Compare the level you requested, the model and effort selected, and the cost with the work the agent actually produced. Use `gh aw logs` to compare several runs before deciding whether to change the level.

If the router found no eligible model, use `gh aw audit <run-url> --models` to see which models were available for that run, then check whether your engine, shortlist, or policy ruled them out. Widen the shortlist if appropriate; the router won't pick a model outside it.

<span id="migrate-nested-routing"></span>

Updating an existing workflow after a compiler warning? See [Migrate routing settings](/gh-aw/setup/working-with-workflows/#migrate-routing-settings).
