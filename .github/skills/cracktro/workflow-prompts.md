# Workflow prompt lines (`/agentic-workflows …`)

Slides built on a gh-aw agentic workflow show, at `top: 30px`, the first user message of the Copilot session that created that workflow, prefixed `/agentic-workflows`. Clicking the prefix opens the full prompt in a phone-style (GitHub app) dialog.

## Code (deck.js)

- `---------- workflow prompts ----------`: `PROMPTS = { "<h2 text>": [workflow, abridged line, full prompt] }`. The key is the slide h2's joined `.line` text, whitespace-collapsed.
- `.awp` = top line (`button` = clickable prefix, `nowrap` + ellipsis); `.awd*` = dialog (`openPrompt`, `closePrompt`). While open, a capturing `keydown` listener swallows deck keys (Escape closes) and the overlay stops click/touch propagation.
- Abridged line: ≤ ~137 characters of prompt text (1700 px width at 17 px mono, prefix included). Check `scrollWidth === clientWidth` for every `.awp`.
- Full prompt: verbatim, but strip blockquote `> `, drop URL lines, collapse blank runs. Must still pass `content-rules.md` (no `https://`, no `github/<repo>`, no 40-hex, no people).

## Mining algorithm

1. **Find the creating PR.** Run `git log --diff-filter=A --format='%h %s' -- .github/workflows/<name>.md`. Take the oldest commit. If the subject has no `(#N)`, run `gh api repos/github/gh-aw/commits/<sha>/pulls --jq '.[].number'`.
2. **Check the PR body.** Run `gh pr view N --json author,body`. If the author is human, skip it. Older Copilot PRs (before Feb 2026) contain `<details><summary>Original prompt</summary>…</details>`, and that block is the first user message.
   - If the block contains `This section details on the original issue you should resolve` / `<issue_title>`, the prompt was an issue, so skip it.
3. **Otherwise use the agents tasks API.** Tasks exist from 2026-02-02 onward. `gh agent-task view` needs OAuth, so use `gh api`.
   - Get the PR database id: `gh api repos/github/gh-aw/pulls/N --jq .id`.
   - List tasks (the list caps at **500** across any pagination). Scan in windows:
     `gh api "/agents/repos/github/gh-aw/tasks?sort=updated_at&direction=asc&since=<ISO>&per_page=100&page=P"`.
     After 500 results, set `since` to the last `updated_at` and continue.
   - Match a task whose `artifacts[]` has `type=="pull"` and `data.id==<db id>`.
4. **Read the prompt.** Run `gh api /agents/repos/github/gh-aw/tasks/<task_id> --jq '.sessions | sort_by(.created_at) | .[0].prompt'`.
   - Skip the slide if the prompt is empty or is a follow-up (`@copilot review…`) rather than an AW-creation request.
5. **Redact and abridge.** Strip URLs and abridge with `…` to fit. Keep the author's wording, including typos.

## Exact references (14 Nov 2026)

| Slide | Workflow | PR | Source |
|---|---|---|---|
| Pull Request Review² | pr-sous-chef | #31911 | task `a70501b3-515a-4256-b691-e77b75eb8394`, session `4e5f906c-6ef8-407f-9c87-90e1bf7a7bd4` |
| Daily Improvers | duplicate-code-detector | #1293 | PR body "Original prompt" (Serena DOCKER.md URL stripped) |
| Merge fast, unslop daily | jsweep | #6854 | PR body |
| Factory Factory | repo-factory (recipe, not a workflow) | #80178 | Authored for the slide (user-directed), not mined from a session |
| Issue Monster | issue-monster | #4684 | PR body "Original prompt" |
| Query · Ask · Grow | daily-otel-instrumentation-advisor | #24655 | task `4f75f9b3-462c-4143-ae1d-0dae11684d5a`, session `2b0febeb-53b8-4f19-83d4-471aed9ce7d9` |
| Instrument · Debug · Repeat | go-logger | #2084 | PR body "Original prompt" (Copilot PR, merged 2025-10-21) |
| Mine · Refine · Apply | linter-miner | #32263 | task `414cf3c4-ff06-4015-92ec-2c9eec136b3e`, session `3e5e3881-fdf1-4f74-8c10-674d786224d3` |
| Explore · Reuse · Lint | sergo | #10063 | PR body |
| Semantic clustering | semantic-function-refactor | #2109 | PR body (commit subject lacks PR number) |
| Go Fan | go-fan | #5092 | PR body |
| Spec-driven testing | spec-extractor / enforcer / librarian | #26083 | task `fca20c82-56ce-4da2-bfc3-629bc47c120f`, session `1b7312fb-2292-4227-84ea-2d170e3906be` |
| Docs that heal | daily-doc-updater | #1861 | PR body |
| Hunt · Falsify · Fix | daily-vulnhunter-scan | #46742 | task `47b76064-1f05-4c71-b671-c2939ad34ef1`, session `5287d715-09be-492d-ba81-44e6b8175b31` (VulnHunter URL replaced by the `capitalone/VulnHunter` slug) |
| Speech To Agent | dictation-prompt | #2147 | PR body |

Alternates that were not used:
- lint-monster: #32687, task `906f95f4-b0fb-4743-a58d-44bcc5693072`, session `57e5e168-24eb-4ba4-8dcc-15bf71e21dd5`.
- code-simplifier: #9636.
- unbloat-docs: #1464.
- docs-noob-tester: #4088.
- daily-function-namer: #20732.

Skipped slides:
- firewall-escape (#4769): human PR.
- Issue-derived prompts: auto-triage-issues (#10529), daily-cli-performance (#8330), daily-doc-healer (#18375) and workflow-health-manager (#7021).
- daily-work-queue-formal-verification (#66024, task `34b856b1-2dc8-4aa7-a954-eef150ca7d9f`): the prompt was a review follow-up.
- agent-job-health (#52108, task `c61d0dc7-94f5-4510-9e52-2158490e6c0f`): empty prompt.
- Grafana advisor (#30190): human PR.
- Scan·Fix·Learn: not a gh-aw workflow.
- Multi-device tester (daily-multi-device-docs-tester, #3603): human PR, generated in a local `create-agentic-workflow` interactive session; later PRs only fix or retune it.
