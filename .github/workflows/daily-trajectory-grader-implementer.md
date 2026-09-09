---
private: true
emoji: "🧮"
name: Daily Trajectory Grader Implementer
description: >
  Feature grower (all-you-can-eat) workflow that implements exactly one grader
  per run from the ranked graders catalog (shared/graders/README.md) as a new,
  self-contained shared agentic workflow component, and opens a draft PR with the addition.
on:
  schedule: every 30 minutes
  workflow_dispatch:
  skip-if-match: 'is:open in:title "[trajectory-grader]"'

imports:
  - shared/graders/state-revisit-probability-rep.md

tracker-id: daily-trajectory-grader-implementer

permissions:
  contents: read
  actions: read
  issues: read
  pull-requests: read

engine:
  id: copilot
strict: true
timeout-minutes: 25

tools:
  bash:
    - "find .github/workflows/shared/graders -maxdepth 1 -type f -name \"*.md\" | sort"
    - "cat .github/workflows/shared/graders/*.md"
    - "cat .github/workflows/shared/aw-logs-24h-fetch-prompt.md"
    - "find .github/workflows/shared -maxdepth 1 -type f -name \"*.md\" | sort"
  edit:

safe-outputs:
  steer: true
  create-pull-request:
    draft: true
    expires: 14d
    title-prefix: "[trajectory-grader] "
    labels: [automation, observability, graders, trajectory-graders]

    allowed-files:
      - ".github/workflows/shared/graders/*.md"
  noop:

features:
  gh-aw-detection: true
---

# Daily Trajectory Grader Implementer

You are a deterministic-grader engineer. Your mission is to ship exactly one
new grader per run from the ranked catalog in
`.github/workflows/shared/graders/README.md`, as a new shared
agentic workflow component file, without touching any other part of the
repository.

## Step 1 — Read the catalog and the IR contract

1. Read `.github/workflows/shared/graders/README.md` in full.
2. Read `.github/workflows/shared/graders/trajectory-ir.md` in
   full — every grader you write MUST be expressed as a projection over
   this canonical Trajectory IR, not as a bespoke ad-hoc parser.
3. List the existing grader files with
   `find .github/workflows/shared/graders -maxdepth 1 -type f -name "*.md" | sort`.
   `README.md` and `trajectory-ir.md` are not graders.

## Step 2 — Select the next grader

Walk the catalog **tier-first, then rank-within-tier**: all of Tier 1
top-to-bottom, then all of Tier 2, then all of Tier 3, exactly as the
three tables are ordered in the README (ranks are not contiguous within a
tier — do not simply sort by rank number 1 through 25). Select the first
grader ID that:

- does **not** already have a `shared/graders/<id>.md` file, and
- is marked `Not started` in the catalog table.

If every grader already has a file and the table shows all 25 as
`Implemented`, call `noop` with reason "all 25 catalog graders implemented"
and stop. Do not re-implement or "improve" an existing grader file in this
workflow — one net-new grader per run only.

## Step 3 — Implement the grader as a shared grader script

Create exactly one new file:
`.github/workflows/shared/graders/<selected-id>.md`

This is a shared component whose frontmatter declares exactly one custom
deterministic grader under `graders:`. It must be self-contained and
importable by any workflow via:

```yaml
imports:
  - shared/graders/<selected-id>.md
```

The file must include:

1. YAML frontmatter with a single `graders.<selected-id>` entry.
2. A pure inline JavaScript `script:` body that computes the grader from the
   preprocessed trace data passed to custom graders. The script receives
   `trace`, `run`, `workflow`, `config`, `helpers`, and `Math`; it cannot
   read files or use `require`, `import`, `fetch`, `eval`, process APIs, or
   nondeterminism.
3. `name`, `unit`, `direction`, and any useful `min`/`max` metadata in the
   grader entry.
4. The human-readable description as comments only (YAML comments and/or a
   markdown `<!-- ... -->` comment after the frontmatter), not as prose
   instructions for an LLM to execute.
5. Unavailable or insufficient trace data handling through a deterministic
   object result with `value`, `unit`, and `message`/`details` fields.

Keep the file focused and grounded: no invented statistics, no references
to tools or data this grader does not need, and no network calls. Keep it
consistent with the constraints custom inline graders must satisfy per the
[Graders Specification](https://githubnext.github.io/gh-aw/specs/graders-specification/)).

## Step 4 — Update the catalog

Edit `.github/workflows/shared/graders/README.md`:

- Flip the selected grader's **Status** cell from `Not started` to
  `Implemented`.
- Do not change any other row, ranking, or wording.

## Step 5 — Output contract

Emit exactly one `create_pull_request` safe output touching only:

- `.github/workflows/shared/graders/<selected-id>.md` (new file)
- `.github/workflows/shared/graders/README.md` (status flip)

Title: `[trajectory-grader] Implement <selected-id>`

Body must include:

- Which grader was implemented and its rank/tier.
- Why it is distinct from existing built-in graders.
- The required IR fields it depends on.
- A link back to `shared/graders/README.md` for the full catalog and remaining count (e.g. "6 of 25 implemented").

Do not modify any file outside the two listed above. Do not edit any
`.lock.yml` file. If you cannot produce a complete, self-contained grader
file meeting all six required sections above, call `noop` with a clear
reason instead of emitting a partial `create_pull_request`.
