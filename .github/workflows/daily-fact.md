---
description: Posts a daily poetic verse about the gh-aw project to a discussion thread
on:
  schedule:
    - cron: "0 11 * * 1-5"  # 11 AM UTC, weekdays only
  workflow_dispatch:
permissions:
  contents: read
  issues: read
  pull-requests: read
  actions: read
  discussions: read
tracker-id: daily-fact-thread
engine:
  id: codex
  model: gpt-5.1-codex-mini
strict: true
timeout-minutes: 15
features:
  action-tag: "c4e091835c7a94dc7d3acb8ed3ae145afb4995f3"

network:
  allowed:
    - defaults

tools:
  github:
    toolsets:
      - default
      - discussions
safe-outputs:
  add-comment:
    target: "4750"
  messages:
    footer: "> 🪶 *Penned with care by [{workflow_name}]({run_url})*"
    run-started: "📜 Hark! The muse awakens — [{workflow_name}]({run_url}) begins its verse upon this {event_type}..."
    run-success: "✨ Lo! [{workflow_name}]({run_url}) hath woven its tale to completion, like a sonnet finding its final rhyme. 🌟"
    run-failure: "🌧️ Alas! [{workflow_name}]({run_url}) {status}, its quill fallen mid-verse. The poem remains unfinished..."
imports:
  - shared/mood.md
---

{{#runtime-import? .github/shared-instructions.md}}

# Daily Fact About gh-aw

Your task is to post a poetic, whimsical fact about the ${{ github.repository }} project to discussion #4750.

## Data Sources

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

## Guidelines

- **Favor recent updates** but include variety - pick something interesting, not just the most recent
- **Be specific**: Include PR numbers, issue references, or release tags when relevant
- **Keep it short**: One or two poetic sentences for the main fact, optionally with a brief context
- **Be poetic**: Use lyrical, whimsical language that celebrates the beauty of code and collaboration
- **Add variety**: Don't repeat the same type of fact every day (e.g., alternate between PRs, issues, releases, contributors, code patterns)

## Output Format

Create a single comment with this structure:

```
🌅 **A Verse from the gh-aw Chronicles**

[Your poetic fact here, referencing specific PRs, issues, or releases with links]

---
*Whispered to you by the Poet of Workflows 🪶*
```

## Examples

Good facts (poetic tone):
- "In the garden of code, PR #1234 bloomed — the `playwright` tool now dances upon the stage, orchestrating browsers in graceful automation! 🎭"
- "Like five stars falling into place, issues of MCP woes were caught and mended this week — the path to custom tools grows ever clearer."
- "From the forge of v0.45.0 emerges `cache-memory`, a keeper of thoughts that transcends the fleeting runs of workflows! 💾"
- "A tireless artisan toiled this week, mending three fractures in the YAML tapestry. Gratitude flows to @contributor! 🙌"

Bad facts:
- "The repository was updated today." (too vague, lacks poetry)
- "There were some changes." (not specific, uninspired)
- Long paragraphs (keep it brief and lyrical)

Now, analyze the recent activity and compose one poetic fact to share in discussion #4750.