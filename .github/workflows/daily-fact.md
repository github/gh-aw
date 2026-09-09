---
private: true
emoji: "📚"
description: Posts a daily poetic verse about the gh-aw project to a discussion thread
on:
  schedule:
    - cron: "daily around 14:00 on weekdays"  # ~2 PM UTC, weekdays only
  workflow_dispatch:
max-daily-ai-credits: 10000
permissions:
  contents: read
  issues: read
  pull-requests: read
  actions: read
  discussions: read
tracker-id: daily-fact-thread
model: openai/gpt-5.3-codex
engine:
  id: codex
  model-provider: openai
strict: true
experiments:
  reasoning_depth:
    variants: [single_pass, multi_candidate]
    description: "Tests whether deliberating over multiple candidate facts before writing improves verse novelty and engagement."
    hypothesis: "H0: no change in discussion engagement rate. H1: multi_candidate produces more novel verses with higher reaction counts (expected +20% reactions)."
    metric: discussion_reaction_count
    secondary_metrics: [output_length_chars, run_duration_ms]
    guardrail_metrics:
      - name: empty_output_rate
        threshold: "<0.05"
      - name: run_success_rate
        threshold: ">=0.95"
    min_samples: 30
    weight: [50, 50]
    start_date: "2026-05-11"
    issue: 31324
timeout-minutes: 15
runs-on: ubuntu-latest
runtimes:
  node:
    version: "22"
inlined-imports: true
network:
  allowed:
    - defaults

sandbox:
  agent:
    id: awf
    runtime: cloud-hypervisor
tools:
  cli-proxy: true
  github:
    mode: local
    toolsets:
      - default
      - discussions
safe-outputs:
  add-comment:
    target: "4750"
  messages:
    footer: "> 🪶 *Penned with care by [{workflow_name}]({run_url})*{ai_credits_suffix}{history_link}"
    run-started: "📜 Hark! The muse awakens — [{workflow_name}]({run_url}) begins its verse upon this {event_type}..."
    run-success: "✨ Lo! [{workflow_name}]({run_url}) hath woven its tale to completion, like a sonnet finding its final rhyme. 🌟"
    run-failure: "🌧️ Alas! [{workflow_name}]({run_url}) {status}, its quill fallen mid-verse. The poem remains unfinished..."
imports:
  - shared/otlp.md
  - shared/mcp/mempalace.md
  - shared/reporting.md
features:
  gh-aw-detection: true
evals:
  - id: verse_posted
    question: Did the agent post a daily poetic verse about the gh-aw project to a discussion thread?
---

{{#runtime-import? .github/shared-instructions.md}}

### Daily Fact About gh-aw

Your task is to post a poetic, whimsical fact about the ${{ github.repository }} project to discussion #4750.

#### Step 0: Load Memory

Before gathering repository activity, check what has already been celebrated in the palace to avoid repetition.

1. Call `mempalace_status` to confirm the palace is ready.
2. Call `mempalace_search` with `query: "gh-aw daily fact"` and `wing: "daily-facts"` to retrieve recently posted facts. On the very first run the palace will be empty — that is fine, proceed without results.
3. Note any PR numbers, issue numbers, release tags, or contributor handles that appear in the results — **do not repeat those topics today**.

#### Data Sources

Mine recent activity from the repository to find interesting facts. Focus on:

1. **Recent PRs** (merged in the last 1-2 weeks)
   - New features added
   - Bug fixes
   - Refactoring efforts
   - Performance improvements

2. **Recent Releases** (if any)
   - New version highlights
   - Breaking changes
   - Notable improvements

3. **Recent Closed Issues** (resolved in the last 1-2 weeks)
   - Bugs that were fixed
   - Feature requests implemented
   - Community contributions

#### Guidelines

- **Check memory first**: Skip any PR, issue, or release that already appears in the palace results from Step 0
{{#if experiments.reasoning_depth == 'multi_candidate'}}
- **Multi-candidate deliberation**: Before writing, identify exactly **3 distinct candidate facts** (one PR, one issue or release, one contributor or pattern). For each candidate write one sentence on why it is novel today. Then score each candidate 1–5 on: (a) novelty vs palace memory, (b) intrinsic poetic potential. Select the highest-scoring candidate and write the verse for that one only.
{{else}}
- **Favor recent updates** but include variety - pick something interesting, not just the most recent
{{/if}}
- **Be specific**: Include PR numbers, issue references, or release tags when relevant
- **Keep it short**: One or two poetic sentences for the main fact, optionally with a brief context
- **Be poetic**: Use lyrical, whimsical language that celebrates the beauty of code and collaboration
- **Add variety**: Don't repeat the same type of fact every day (e.g., alternate between PRs, issues, releases, contributors, code patterns)

#### Output Format

Create a single comment with this structure:

```
🌅 **A Verse from the gh-aw Chronicles**

[Your poetic fact here, referencing specific PRs, issues, or releases with links]

---
*Whispered to you by the Poet of Workflows 🪶*
```

#### Examples

Good facts (poetic tone):
- "In the garden of code, PR #1234 bloomed — the `playwright` tool now dances upon the stage, orchestrating browsers in graceful automation! 🎭"
- "Like five stars falling into place, issues of MCP woes were caught and mended this week — the path to custom tools grows ever clearer."
- "From the forge of v0.45.0 emerges `cache-memory`, a keeper of thoughts that transcends the fleeting runs of workflows! 💾"
- "A tireless artisan toiled this week, mending three fractures in the YAML tapestry. Gratitude flows to @contributor! 🙌"

Bad facts:
- "The repository was updated today." (too vague, lacks poetry)
- "There were some changes." (not specific, uninspired)
- Long paragraphs (keep it brief and lyrical)

#### Step 3: Save to Memory

After posting the comment, store the fact in the palace so it will be excluded from future runs:

Call `mempalace_add_drawer` with:
- `wing`: `"daily-facts"`
- `room`: the category of the fact — use one of these canonical values:
  - `"pr"` — a merged pull request
  - `"release"` — a release or version tag
  - `"issue"` — a closed issue
  - `"contributor"` — a community contributor highlight
  - `"pattern"` — a code pattern or architectural observation
- `content`: a short record containing the PR/issue/release identifier and a one-line summary of the fact posted today

This ensures tomorrow's verse celebrates something new.

Now, analyze the recent activity and compose one poetic fact to share in discussion #4750.