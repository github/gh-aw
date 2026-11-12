---
description: Prompt caching, AI-credit budget guardrails, and bounded file reads for GitHub Agentic Workflows.
---

# Token Optimization — Caching, Budgets, and Bounded Reads

See [token-optimization.md](token-optimization.md) for the full technique index and quick-reference checklist.

## Technique 9 — Enable Prompt Caching

Prompt caching is automatic via the AWF gateway. Cached input tokens are weighted at `0.1` versus `1.0` for uncached input — repeated context (system prompt, shared preamble) costs ~10× less when cached.

To maximize cache hits:

- **Keep stable content at the top of the prompt** — instructions that don't change between runs (role, output format, schema) before dynamic content (issue body, event context).
- **Use `cache-memory`** for workflows that re-read the same large knowledge base across runs; avoids duplicate context every turn.
- **Minimize dynamic context** — inject only the fields the agent needs: `${{ github.event.issue.number }}` instead of the full event payload.

---

## Technique 10 — Cap Spend with AI-Credit Guardrails

Two top-level frontmatter fields enforce AI Credit budgets directly, independent of the techniques above. Both accept an integer or a `K`/`M` short-form string (e.g. `100M`, `500K`). Typical workflow range: `100` to `2500`.

Do not treat a workflow exhausting its per-run budget as a reason to increase `max-ai-credits` immediately. First apply and measure every applicable cost optimization in this guide. Increase the limit only as a last resort when the workflow still cannot complete with acceptable quality within the existing budget.

- **`max-ai-credits:`** — Per-run AI credit budget enforced by the AWF firewall/API proxy (default `1000`). AWF clamps any budget above its hard cap, 10,000 AI credits by default. The agent is steered to stay within budget; set a negative value to disable enforcement. With AWF releases after v0.29.11, budget notices continue unless `sandbox.agent.token-steering: false`.
- **`max-daily-ai-credits:`** — Per-user 24-hour guardrail. At activation, gh-aw sums the triggering user's AI credits across their runs of this workflow over the last 24 hours and blocks execution once the total exceeds the threshold. Enabled by default with a system default threshold; set `-1` to disable, or an explicit value to override the default.

```yaml
max-ai-credits: 2K          # per-run cap (short-form string)
max-daily-ai-credits: 500M  # per-user 24h cap; -1 disables
```

For custom or private models, the top-level **`models:`** frontmatter field supplies pricing in the same structure as `models.json` (keyed `providers.<provider>.models.<model>.cost` with `input`/`output`/`cache_read`/`cache_write` per-token costs). Entries are merged with the built-in `models.json` at runtime — they override matching models and fill gaps for unknown ones — so AI Credit accounting stays accurate for models gh-aw does not price by default.

For self-hosted or BYOK models absent from the built-in table (e.g. Ollama, vLLM), set **`models.default-ai-credits-pricing`** (`input`/`output` in $/1M tokens, both `0` for free/local models); without it the AWF proxy rejects unrecognized models with HTTP 400 `unknown_model_ai_credits`.

---

## Technique 11 — Cap Session Context Growth from Large File Reads

> **Files larger than 20 KB must not be read in full.** Use targeted reads instead.

Before calling `get_file_contents`, check size with `wc -c <path>`. If > 20 KB, use `grep`, `glob`, `bash head`, or `view` with `view_range` to read only the section you need. The same rule applies after `glob **/*.md` — read each matched file with `grep` or `view_range`, not full-file reads.

For GitHub-hosted files, prefer `mode: gh-proxy` and access via `gh`/`bash` so output can be piped through `jq`, `grep`, or `head` before it enters context — the agent never receives the full file:

```bash
# gh-proxy: fetch only the lines you need, no full-file injection
gh api repos/{owner}/{repo}/contents/.github/aw/syntax-agentic.md \
  --jq '.content' | base64 -d | grep -n "## Sub-agents"
```

```bash
# Without gh-proxy: targeted local read
bash: grep -n "## Sub-agents" .github/aw/syntax-agentic.md
# or
view: .github/aw/syntax-agentic.md view_range=[45, 90]
```
