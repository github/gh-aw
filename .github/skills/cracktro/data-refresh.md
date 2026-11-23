# Cracktro data-slide refresh convention

Load when adding, changing or refreshing a slide that shows measured data.

Every slide that shows measured data has a refresh mini skill. Load only the one for the slide you are refreshing.

## Convention (required for any new or changed data slide)

- **Files:**
  - Plan: `refresh/<slug>.md`, where the slug is the kebab-case slide heading with no slide number (for example `scan-fix-learn`).
  - Optional helper: `refresh/<slug>.sh` or `.mjs`. It must be read-only (gh, git and jq only), use no secrets, and print JSON. Run it once and confirm it reproduces the baseline.
- **Front matter:**
  - `name: cracktro-refresh-<slug>`
  - `description: Recompute the measured data on the cracktro "<Title>" slide and update deck.js.`
- **Sections, in this order:**
  - `## Workflows mined`: a table with columns `Workflow file | Repo | Role in the slide's flow | Signal mined`. Add one row per agentic workflow whose output was counted, including indirect ones (assigners, reviewers identified by footer). Use the real file name and check that it exists. Add a "Copilot coding agent (not a workflow)" row when its work is counted.
  - `## 1. Slide`: the `// <Title>` marker, the CSS prefix, and the exact constants and strings that hold data.
  - `## 2. Window & snapshot`: the window dates, the snapshot date, and how to roll them forward.
  - `## 3. Sources`
  - `## 4. Queries`: exact copy-paste commands with their filters, the output shape, and the current values as a baseline.
  - `## 5. Update steps`: numbers, the window label on the slide, the speaker notes (they must still match `/illustrative|synthetic|simulated/i`), and the slide's Exception bullet in `content-rules.md`.
  - `## 6. Validation`: `node --check`, a preview screenshot, and the content rules.
  - `## 7. Pitfalls`: result caps (for example gh search stops at 1,000), fuzzy title search (filter with `startswith`), and any number that does not reproduce, with the reason.
- **Length and style:** about 150 lines at most; imperative and token-efficient.
- **The slide** must state its data window and snapshot date in a caption or panel label.
- **When adding or changing a data slide:** write or update its refresh skill and its Exception bullet in `content-rules.md` in the same change, and add a row to the table below. When a refresh shows that a published number is wrong (for example because of a result cap or a parsing bug), fix the slide, its notes, its `content-rules.md` Exception bullet and the plan's baseline together.
- **Repo widgets** (`repoWidget(id, name, stars, cls)` in `deck.js`, used on Ponytail Reviewer, Index & search and Skills): the slide shows the hard-coded `stars` default and fetches the live count on https pages only. Refresh the default with `gh api repositories/<id> --jq .stargazers_count` and update the snapshot date in the slide's Exception bullet.

| Slide | Refresh skill |
|---|---|
| Human Agent Processes | `refresh/human-agent-processes.md` |
| Pull Request Review² | `refresh/pr-review-squared.md` |
| Daily Improvers | `refresh/daily-improvers.md` |
| Merge fast, unslop daily | `refresh/merge-fast-unslop-daily.md` |
| Issue Monster | `refresh/issue-monster.md` |
| Research · Assign · Observe | `refresh/research-assign-observe.md` |
| Hunt · Falsify · Fix | `refresh/hunt-falsify-fix.md` |
| Explore · Reuse · Lint | `refresh/explore-reuse-lint.md` |
| Spec · Model · Test | `refresh/spec-model-test.md` |
| Decide · Draft · Record | `refresh/decide-draft-record.md` |
| PR Sous Chef | `refresh/pr-sous-chef.md` |
| Spec-driven testing | `refresh/spec-driven-testing.md` |
| Docs that heal | `refresh/docs-that-heal.md` |
| Multi-device tester | `refresh/multi-device-tester.md` |
| Findings that land | `refresh/findings-that-land.md` |
| Personas · Probe · Patch | `refresh/personas-probe-patch.md` |
| Personas test the guide. Agent fixes it. | `refresh/personas-probe-patch.md` (step 5.6) |
| Scan · Fix · Learn | `refresh/scan-fix-learn.md` |
| CLI vs MCP | `refresh/cli-vs-mcp.md` |
| Skills in review | `refresh/skills-in-review.md` |
| Ponytail Reviewer | `refresh/ponytail-reviewer.md` |
| Inlined Skills & Agents | `refresh/inlined-skills-agents.md` |
| It’s ok to say no(op) | `refresh/its-ok-to-say-noop.md` |
| Fuzzy schedules | `refresh/fuzzy-schedules.md` |
| Week of daily runs | `refresh/week-of-daily-runs.md` |
| From Delivery to Value | `refresh/from-delivery-to-value.md` |
| Deliver · Accept · Attribute | `refresh/deliver-accept-attribute.md` |
| Instrument · Debug · Repeat | embedded below: "Refresh: Instrument · Debug · Repeat" |

## Refresh: Instrument · Debug · Repeat

Embedded in this file instead of `refresh/instrument-debug-repeat.md`. Keep the same sections.

### Workflows mined

| Workflow file | Repo | Role in the slide's flow | Signal mined |
|---|---|---|---|
| `.github/workflows/go-logger.md` | gh-aw | daily instrumentation (manifest → ≤ 5 Go files → build/fmt → PR) | `[log] ` PRs with footer `gh-aw-workflow-id: go-logger`, merged flag |

### 1. Slide

- Marker `// Instrument · Debug · Repeat`, CSS prefix `idr-`.
- `MEAS`: `13` opened, `13` merged, `52` file touches (summed `changed_files`), `+280` lines added.
- Lane label `MEASURED · GH-AW GO LOGGER · 20 OCT – 18 NOV 2026`. Captions: latest sample `1 Go file · +7 lines · 4 log calls`, `snapshot 19 Nov 2026 · file touches, not unique files`, `shows delivery, not a debugging speedup`.
- Notes sentence starting "Measured:". Everything above the strip (failure, DEBUG rerun, log lines, agent) is illustrative.

### 2. Window & snapshot

- Created 20 Oct to 18 Nov 2026 (30 days); snapshot 19 Nov 2026.
- Roll forward by shifting both `created:` dates and the snapshot. The schedule is daily, but not every day yields a PR; never claim one per day.

### 3. Sources

- Cohort (13, all merged): #71327, #71541, #71993, #72299, #73689, #74375, #74649, #74728, #75592, #75813, #76117, #77033, #78903.
- Changed files per PR: 3,5,5,5,5,2,2,4,5,5,5,5,1 (sum 52; touches, not unique files). Additions: 25,16,7,17,34,14,7,13,34,28,43,35,7 (sum 280).
- Latest sample #78903 (merged 18 Nov 2026): 1 file `compiler_yaml_header.go`, +7 lines, 4 log calls.
- Earlier reference: #2085 (merged 21 Oct 2025, 5 files, +42 lines). Creation PR #2084 (Copilot, merged 21 Oct 2025) is prompt provenance only; see `workflow-prompts.md`.

### 4. Queries

```bash
set -euo pipefail
# raw search (47 hits at snapshot) → literal prefix (13) → per-PR verification; no files written
Q='repo:github/gh-aw "[log]" in:title created:2026-10-20..2026-11-18 is:pr'
gh api -X GET search/issues --paginate -f q="$Q" \
  --jq '.items[] | select(.title|startswith("[log] ")) | .number' |
while read -r n; do
  gh api repos/github/gh-aw/pulls/$n \
    --jq '{n:.number, merged, files:.changed_files, add:.additions, gl:(.body // "" | test("gh-aw-workflow-id: go-logger"))}'
done | jq -s '
  (map(select(.gl|not)) | map(.n)) as $foreign
  | if ($foreign|length) > 0 then error("not go-logger: \($foreign)") else . end
  | {opened:length, merged:map(select(.merged))|length,
     touches:(map(.files)|add), additions:(map(.add)|add)}'
gh pr diff 78903 -R github/gh-aw | grep -c '^+.*\.Printf('   # latest sample log calls
```

Baseline: `{opened:13, merged:13, touches:52, additions:280}`.

### 5. Update steps

Edit `MEAS`, the lane label window, the captions, the notes "Measured:" sentence (keep "illustrative") and the `idr-` Exception bullet in `content-rules.md`.

### 6. Validation

`node --check docs/public/cracktro/deck.js`, a `.stage` preview screenshot at 1920x1080 after the loop lights up, and the content rules (no PR numbers, people or URLs on the slide).

### 7. Pitfalls

- `in:title "[log]"` is fuzzy (47 raw hits): keep only `startswith("[log] ")`. The jq step fails if any remaining PR lacks the go-logger footer; investigate and exclude it explicitly rather than counting it.
- Search stops at 1,000 results; keep windows small.
- `changed_files` summed counts touches; the same file can appear in several PRs.
- Actions run success is not delivery; count merged PRs.
- One PR (#75592) carried a threat-detection tooling warning, which is not a security finding; do not describe the cohort as "clean".
