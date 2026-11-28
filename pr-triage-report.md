### Overview

- Repository: `github/gh-aw`
- Run ID: `46144024277`
- Scanned open PRs: 30
- Eligible open Copilot PRs: 0
- After author + cooldown filters: 0 eligible PRs
- Result: no triage actions required; all open PRs were excluded by author or recency filters.

### Executive totals

- Total eligible PRs: 0
- Categorized by type: docs=0, test=0, formatting=0, chore=0, refactor=0, bug=0, feature=0
- Categorized by risk: low=0, medium=0, high=0
- Categorized by priority: low=0, medium=0, high=0
- Recommended actions: auto_merge=0, fast_track=0, batch_review=0, defer=0, close=0

### Key findings

- No eligible `app/github-copilot` pull requests remained after applying the 1-hour cooldown and author gate.
- This run therefore produced no PR comments, no label changes, and no batch recommendations.
- The repository currently has a fresh PR backlog, but none were safe to triage under the active guardrails.

### Details

<details>
<summary>Scanned PR set</summary>

- 30 open PRs were scanned.
- 0 were authored by `app/github-copilot`.
- 30 were skipped because they were either authored by a different user or opened/updated within the last hour.
</details>

### Next actions

- Re-run this triage workflow once the cooldown window has passed for active Copilot PRs.
- Keep the process gated on author + freshness checks to avoid commenting on in-flight agent work.
