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

| Slide | Refresh skill |
|---|---|
| 16 Human Agent Processes | `refresh/human-agent-processes.md` |
| 18 Pull Request Review² | `refresh/pr-review-squared.md` |
| 21 Daily Unsloppers | `refresh/daily-unsloppers.md` |
| 22 Merge fast, unslop daily | `refresh/merge-fast-unslop-daily.md` |
| 23 Research · Assign · Observe | `refresh/research-assign-observe.md` |
| 24 Hunt · Falsify · Fix | `refresh/hunt-falsify-fix.md` |
| 27 Explore · Reuse · Lint | `refresh/explore-reuse-lint.md` |
| 29 Spec · Model · Test | `refresh/spec-model-test.md` |
| 31 Decide · Draft · Record | `refresh/decide-draft-record.md` |
| 32 Spec-driven testing | `refresh/spec-driven-testing.md` |
| 33 Docs that heal | `refresh/docs-that-heal.md` |
| 34 Multi-device tester | `refresh/multi-device-tester.md` |
| 35 Findings that land | `refresh/findings-that-land.md` |
| 36 Personas · Probe · Patch | `refresh/personas-probe-patch.md` |
| 38 Scan · Fix · Learn | `refresh/scan-fix-learn.md` |
| 59 Skills in review | `refresh/skills-in-review.md` |

