---
name: cracktro-refresh-cli-vs-mcp
description: Recompute the measured data on the cracktro "CLI vs MCP" slide and update deck.js.
---

# Refresh "CLI vs MCP"

Load `.github/skills/cracktro/SKILL.md` first. Never read `docs/public/cracktro/deck.js` whole; `grep -n`, then `view_range`.

## Workflows mined

Same-workflow before/after natural experiments: commits that flip `tools.github.mode` between `local` (GitHub MCP server) and `gh-proxy` (`gh` CLI). Each run's `usage` artifact is mined.

| Workflow file | Repo | Role in the slide | Signal mined |
|---|---|---|---|
| `.github/workflows/ponytail-reviewer.md` | github/gh-aw | the two measured boxes and the caption | first-request prompt tokens, run totals; switched `local` → `gh-proxy` + `cli-proxy: true` in one commit, same model |
| 21 workflows in the bulk `gh-proxy` → `local` commit (`ai-moderator.md`, `tidy.md`, `go-fan.md`, ... listed in the miner's `cases[]`) | github/gh-aw | notes only (whole-run AI credits) | same metrics; the commit also changed the sandbox runtime |
| the 14 busiest workflows with no `.md` edit within 7 days of the bulk commit | github/gh-aw | notes only (drift control) | same metrics, before vs after the bulk commit |

## 1. Slide

- Marker: `// CLI vs MCP:` (`grep -n "// CLI vs MCP:" docs/public/cracktro/deck.js`). CSS prefix `cvm-`. Group `cost`. `#cli-vs-mcp` in `slides.md` (Notes `illustrative`: the flow is synthetic).
- The flow (fat `list_code_scanning_alerts` response vs `--help` → `> alerts.json` → `jq | sort | uniq -c | head`) and its `IN CONTEXT` boxes are synthetic and qualitative: no measured numbers there.
- Data lives inside that block only:
  1. The `.cvm-cap` caption: `MEASURED, schemas only`, workflow count, runs per side, window, first-request medians (16.7k → 12.8k), whole-run verdict, snapshot.
  2. `notes:` string: repeats every number, the window, the snapshot, the bulk result and its confound.
- Elsewhere: the `content-rules.md` Exception bullet "the CLI vs MCP slide (`cvm-` classes)".

## 2. Window & snapshot

- Each side is bounded by the workflow's previous/next `.md` edit and capped at `DAYS` (7) from the switch; the `CAP` (15) successful runs nearest the switch are kept. A side needs `MIN` 3 runs.
- Baseline (snapshot 2026-11-17): ponytail-reviewer MCP side 2026-08-23 → 08-27, gh-proxy side 08-27 → 09-03; bulk commit 2026-09-05, sides 08-29 → 09-12.

## 3. Sources

- `gh run download <id> -R github/gh-aw -n usage`: `agent/token_usage.jsonl`, one line per model request (`input_tokens`, `cache_read_tokens`, `cache_write_tokens`, `output_tokens`, `ai_credits_this_response`, `path`, `model`). Skip `detection/`.
- Surface of the MCP side: the compiled lock at the MCP-side commit. `GH_AW_MCP_CLI_SERVERS_LIST` naming `` `github` `` means cli-proxy mounted the GitHub MCP as a CLI (no schemas in context); otherwise its schemas were offered to the engine. Front matter alone misses `cli-proxy` set by imports.
- Tool count: `gh run download <id> -n agent`, `mcp-logs/tools.json` (`servers.github.length`) of an MCP-side run.

## 4. Queries

Run from the repo root (needs full git history and `gh auth login`); downloads are cached in `CACHE`:

```bash
RAW=$TMPDIR/cvm-raw.json node .github/skills/cracktro/refresh/cli-vs-mcp.mjs > $TMPDIR/cvm-out.json
node -e 'const o=require(process.env.TMPDIR+"/cvm-out.json");console.log(JSON.stringify(o.summary));const c=o.cases.find(c=>c.wf==="ponytail-reviewer");console.log(c.before,c.after,c.delta)'
```

- To add a switch, append its short SHA to `SWITCHES`; find candidates with `git log -G'mode: (gh-proxy|local)' --format='%h %cI %s' -- .github/workflows/*.md`.
- Medians are per workflow per side; `summary.*.<metric>.medianPct` is the median of per-workflow % changes (MCP side → gh-proxy side), `lower` the count that fell.

Baseline (snapshot 2026-11-17):

| Group | Workflows · runs | First request | Prompt tokens | AI credits |
|---|---|---|---|---|
| ponytail-reviewer, schemas → gh-proxy + cli-proxy | 1 · 30 | 16,690 → 12,766 (−23.5%) | 182,639 → 190,929 (+4.5%) | 4.15 → 4.27 (+2.9%) |
| bulk commit, all cases | 21 · 250 | ~+0.4k on MCP side (median) | — | median −19.2%, 16/21 lower, range −78% … +123% |
| bulk, GitHub MCP behind cli-proxy | 13 · 154 | −1.3% | −22.1% | −6.4% (10/13) |
| drift controls | 14 · 212 | 0% | −3% | −1.7% (9/14) |

## 5. Update steps

1. Update the `.cvm-cap` caption (medians, window, runs per side, snapshot); the `ctx(...)` boxes stay qualitative.
2. Update the notes: every number, window, snapshot, the bulk range and confound. Keep `illustrative and synthetic` for the flow.
3. Update the `content-rules.md` Exception bullet. The `slides.md` entry stays free of numbers and dates.
4. If a newer clean switch (one workflow, same model, no runtime change) exists, prefer it for the boxes and say which in the notes.

## 6. Validation

- `node --check docs/public/cracktro/deck.js` and the preview at `#cli-vs-mcp`: boxes fit their 536-px rects; caption stays inside the 1620-px viewBox.
- The ponytail case shows `mcpSurface: "schemas"` and equal `requests` medians on both sides; controls stay near 0%.
- No URLs, SHAs, run IDs or logins on the slide or in the notes.

## 7. Pitfalls

- Logs and artifacts expire after ~90 days: the baseline runs (Aug–Sep 2026) become unrecomputable around early December 2026. Keep the baseline table; refresh from newer switches instead.
- `/responses` (OpenAI, Codex, MAI) `input_tokens` already include cached tokens; Anthropic counts `cache_read`/`cache_write` separately. The miner's `inclusive()` handles both; never add cache to `/responses` input.
- Claude and Codex engines appear to defer MCP tool schemas: their local-mode first request was only ~0.4k larger than gh-proxy. Only engines that load schemas up front (Copilot CLI with tool search off) show the ~3.9k jump. Do not generalise the −23% beyond that.
- Whole-run tokens and credits are dominated by task variance (n = 3–15 per side). Do not put a whole-run percentage on the slide.
- The bulk commit also moved the sandbox to cloud-hypervisor and 18 files changed engine (excluded): it is confounded.
- Many successful runs have no `usage` artifact (agent skipped); `gh run list` caps at 1000, so the miner filters by `--created`.
- macOS `awk` has no `\s`; use `[ ]`.
