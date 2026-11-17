---
name: cracktro-refresh-inlined-skills-agents
description: Recompute the measured data on the cracktro "Inlined Skills & Agents" slide and update deck.js.
---

# Refresh: Inlined Skills & Agents

Run the read-only helper, update only this slide's measured strip and guidance,
then verify the diagram and evidence remain distinct. Stop on query errors;
never substitute zero for missing data.

## Workflows mined

| Workflow file | Repo | Role in the slide's flow | Signal mined |
|---|---|---|---|
| `.github/workflows/code-simplifier.md` | gh-aw | Deferred language rules and cheap file scouts | Direct skill/agent definitions, model declarations, Actions run conclusions |
| `.github/workflows/pr-code-quality-reviewer.md` | gh-aw | Small first-pass reviewer and deferred publication rules | Direct skill/agent definitions, model declarations, Actions run conclusions |
| `.github/workflows/github-mcp-tools-report.md` | gh-aw | Small diff/documentation workers and deferred report rules | Direct skill/agent definitions, model declarations, Actions run conclusions |

Inventory all top-level non-`smoke-` workflow Markdown files at the pinned source
revision. Count only direct inline blocks recognized by the runtime extractors;
do not expand shared imports or infer invocations.

## 1. Slide

- Marker: `// Inlined Skills & Agents`; group `cost`, prefix `isa-`.
- `EXAMPLES`: `[workflow name, skill count, agent count, agent model, successful runs, total runs]`.
- `.isa-inventory`: workflow denominator, inline-using workflows, block counts and workflows using both.
- `.isa-caption`: run window, snapshot and interpretation caveat.
- `notes:`: exact inventory, definitions, examples and limitations.
- Keep synthetic `BLK`, `TURNS` and bar heights independent of measured values.

## 2. Window & snapshot

- Run window: 18 Oct–16 Nov 2026 inclusive, 30 complete UTC days.
- Source/run snapshot: 17 Nov 2026; source revision `bef5588c471` on main.
- For a refresh, fetch the intended main ref normally, omit helper arguments
  for the last 30 complete UTC days, and record its resolved source revision.

## 3. Sources

- Public gh-aw git tree, top-level `.github/workflows/*.md`, excluding `smoke-*`.
- Runtime parsers `extract_inline_skills.cjs` and `extract_inline_sub_agents.cjs`.
- REST Actions runs for each selected workflow's `.lock.yml`, filtered by
  inclusive UTC `created` dates; `status=success` supplies the numerator.

## 4. Queries

```bash
node .github/skills/cracktro/refresh/inlined-skills-agents.mjs 2026-10-18 2026-11-16 bef5588c471
```

Output: `{snapshot, sourceCommit, window: {start,end,days}, inventory, examples}`.
Each example has `{file, skills: [name], agents: [{name,model}], runs, successfulRuns}`.
The helper sums nonoverlapping seven-day API count queries. It stops if a chunk
reaches 1,000 or a count is missing, avoiding search-result caps.

Baseline:

| Inventory | Count |
|---|---|
| Non-smoke top-level workflows | 291 |
| With skills / agents / either / both | 13 / 33 / 41 / 5 |
| Skill blocks / agent blocks | 21 / 62 |

| Workflow | Skills | Agents | Agent model | Successful / total runs |
|---|---|---|---|---|
| code-simplifier | 4 | 2 | claude-haiku-4.5 | 28 / 30 |
| pr-code-quality-reviewer | 1 | 1 | small | 948 / 1,093 |
| github-mcp-tools-report | 1 | 2 | small | 6 / 6 |

## 5. Update steps

1. Replace `EXAMPLES` and inventory text from helper output.
2. Update the caption, speaker notes and this baseline with the window,
   snapshot and source revision. Keep notes explicitly illustrative.
3. Update this slide's Exception bullet in `content-rules.md`.
4. Update its `slides.md` summary only if the message changes.
5. Keep `data-refresh.md` linked to this mini skill.

## 6. Validation

```bash
node --check docs/public/cracktro/deck.js
node --check .github/skills/cracktro/refresh/inlined-skills-agents.mjs
python3 scripts/check-instruction-prefixes.py .github/skills/cracktro/refresh/inlined-skills-agents.md
```

Preview the slide, advance twice and screenshot `.stage` in 16:9. Check the
measured strip clears the minimap and dock, labels fit and backward navigation
restores each step. Run `docs/tests/talk.spec.ts` when docs dependencies exist;
otherwise report it not run. Follow public-content and colour-accessibility rules.

## 7. Pitfalls

- Current declarations do not establish which skills or agents ran, or which
  definitions existed for every historical run. Never label this as invocation data.
- Whole-workflow success includes deterministic/noop paths, and reruns may change
  conclusions after the snapshot. Do not equate success with savings or agent quality.
- Model aliases are declarations, not observed model resolutions.
- Imported inline blocks, installed skills, standalone custom agents and smoke
  workflows are outside the inventory; do not describe it as all skill usage.
- Git refs can move. The helper resolves one commit before reading all sources.
- The illustrative graph defers loading, not automatic unloading: skill bodies
  remain in the main conversation once loaded; sub-agent contexts are separate.
