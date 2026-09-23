---
description: OpenTelemetry export and harness-execution-experience learning loops for token optimization in GitHub Agentic Workflows.
---

# Token Optimization — Observability and Harness Learning

See [token-optimization.md](token-optimization.md) for the full technique index and quick-reference checklist.

## Technique 7 — Measure Continuously with OpenTelemetry and AgenticOps

Export telemetry automatically and add workflows that keep finding token waste over time.

### Enable OTLP export

Add workflow-level OpenTelemetry export so each run emits token and phase data to your observability backend:

```yaml
observability:
  otlp:
    endpoint: ${{ secrets.GH_AW_OTEL_ENDPOINT }}
    headers: ${{ secrets.GH_AW_OTEL_HEADERS }}
```

Setup, agent, and conclusion spans carry token usage attributes. See [Frontmatter syntax](syntax-agentic.md#agentic-workflow-specific-fields).

When a `github-script` step needs a custom span, load the helper from the portable runner temporary directory:

```javascript
const path = require('path');
const otlp = require(path.join(process.env.RUNNER_TEMP, 'gh-aw', 'actions', 'otlp.cjs'));
```

### Add AgenticOps token workflows

- `copilot-token-audit` — scheduled audit of token usage across workflows
- `copilot-token-optimizer` — scheduled follow-up that identifies one expensive workflow and proposes concrete savings

Loop: export OTEL → summarize usage → open optimization issues → re-measure. See `.github/workflows/` for examples.

---

## Technique 8 — Learn from Harness Execution Experience

Treat the agent harness as six separate control surfaces rather than one prompt:

| Dimension | gh-aw control surface |
|---|---|
| Context assembly | Prompt structure, imports, DataOps, and context compression |
| Tool interaction | Tool selection, `gh-proxy`, `cli-proxy`, permissions, and result filtering |
| Generation control | Engine and model selection, `max-turns`, and `timeout-minutes` |
| Orchestration | Deterministic steps, sub-agents, planning, execution, and refinement |
| Memory management | `cache-memory`, `repo-memory`, summaries, and stale-context removal |
| Output processing | Safe outputs, schema validation, fallbacks, and `noop` behavior |

Start with the smallest known-good harness. Per experiment, record a compact entry (task features, config change, outcome quality, AIC/token cost, diagnosed failure dimension), distill repeated diagnoses into reusable patterns, and retrieve only relevant cases later instead of re-searching broadly. Select changes **correctness first**: maximize the quality metric, then minimize AIC among equivalent-quality variants so a cheap but degraded result cannot win.

Prioritize this for long-horizon, tool-heavy workflows with measurable headroom; keep retrieved experience compact so prompt caching offsets its input-token overhead. Based on [MemoHarness](https://arxiv.org/pdf/2607.14159) — treat its gains as directional (small held-out set, unablated components, cache-dependent cost advantage).
