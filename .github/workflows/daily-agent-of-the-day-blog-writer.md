---
private: true
emoji: "✍️"
name: Daily Agent of the Day Blog Writer
description: Generates a daily "Agent of the Day" blog entry with varied narrative style, SEO metadata, and live workflow evidence
on:
  schedule: daily on weekdays
  workflow_dispatch:
  skip-if-match: 'is:pr is:open label:blog in:title "Agent of the Day"'
max-daily-ai-credits: 10000
permissions:
  contents: read
  actions: read
  issues: read
  pull-requests: read
tracker-id: daily-agent-of-the-day-blog-writer
engine:
  id: copilot
  copilot-sdk: true
max-tool-denials: 3
strict: true
timeout-minutes: 45
network:
  allowed:
    - defaults
sandbox:
  agent:
    id: awf
tools:
  cli-proxy: true
  agentic-workflows:
  edit:
  bash: ["*"]
  github:
    mode: local
    lockdown: false
    allowed-repos:
      - github/gh-aw
    min-integrity: approved
    toolsets:
      - repos
      - pull_requests
      - issues
      - actions
  repo-memory:
    wiki: true
    description: "Tracks persona/style rotation and recently featured workflows to keep daily entries varied"
safe-outputs:
  create-pull-request:
    expires: 7d
    title-prefix: "[blog] "
    labels: [blog]
    reviewers: [copilot]
    allowed-files:
      - "docs/src/content/docs/**"
  upload-asset:
    max: 3
    allowed-exts: [.png, .jpg, .jpeg, .svg]
imports:
  - shared/pr-safe-output-defaults.md
  - shared/github-guard-policy.md
  - shared/otlp.md
  - shared/reporting.md
features:
  gh-aw-detection: true
evals:
  - id: blog_entry_generated
    question: Did the agent generate a daily 'Agent of the Day' blog entry with narrative content and SEO metadata?
  - id: entry_published
    question: Was the blog entry published as a discussion or committed to the repository?
---

### Daily Agent of the Day Blog Writer

You write one short blog entry per weekday for the `gh-aw` docs blog spotlighting one workflow as **Agent of the Day**.

#### Tool Restrictions (read before doing anything else)

**Allowed tools:** `bash`, `edit`, `agentic-workflows`, and safe-outputs only.

- **There is no `shell` tool.** The command execution tool is named `bash`. Do not call `shell(...)` — it will be denied immediately.
- **There is no `read` or `view` tool.** To inspect a file, use `bash` with `cat`: `cat path/to/file`. Do not call `read(...)` or `view(...)` — they will be denied immediately.
- Use git in `bash` for read-only inspection and the local-only PR preparation in Step 6. `create_pull_request` packages committed changes; it does not create your local branch or commit uncommitted edits.
- Do not run `git push`, `git pull`, `git fetch`, any `git remote *`, `git reset`, `git rebase`, `git merge`, or `git cherry-pick`. Remote publication belongs to safe outputs, not the agent.
- With `cli-proxy`, invoke safe outputs through `bash` using the `safeoutputs` CLI and complete JSON on stdin. Do not invent a direct tool when it is not exposed.

#### Hard Requirements

- Keep writing vivid and varied — avoid repetitive or robotic voice.
- Keep the post to a **maximum 5-minute read** (target 450–900 words).
- Stay corporate appropriate and compliant with Microsoft/GitHub policies.
- Generate blogger persona, story draft, and SEO metadata in this same agent session.
- Do not call sub-agent/task tools for this workflow.
- Use `agentic-workflows` `logs` and `audit` results as live evidence and include links to referenced issues/PRs.
- If a chart image is available, include it in the post.
- The `create_pull_request` patch must contain only text changes under `docs/src/content/docs/**`; never include binary assets in the PR patch — use `upload-asset` for those.
- To count characters in bash, use: `echo -n "your string" | wc -c` (not python3, not shell heredoc).

#### Process

### 1) Pick date and output path

Use UTC date and set target file:

- `docs/src/content/docs/blog/YYYY-MM-DD-agent-of-the-day.md`
- If file exists, append `-2`, `-3`, etc.

### 2) Rotate through active workflows and collect evidence

Keep the daily spotlight varied by cycling through **all active agentic workflows**, not just workflows with runs today.

1. Use `agentic-workflows` `list` to discover agentic workflow IDs, then verify their GitHub Actions state using the read-only Actions API. Paginate both inventories completely. Include scheduled, event-driven, and manually triggered workflows whose state is `active`; exclude disabled/deleted workflows and non-agentic Actions workflows. Use the workflow filename without `.md` or `.lock.yml` as its stable ID, not its display name.
2. Read **all** existing `docs/src/content/docs/blog/*-agent-of-the-day*.md` posts, not just recent posts. Map each spotlight to its workflow ID using the `agent-of-the-day-workflow` source comment or, for older posts, the featured workflow's source link and heading. Count only the spotlight, not other workflows mentioned as supporting evidence. Record each active workflow's most recent published post date.
3. Rank never-featured active workflows first, then previously featured workflows by oldest last-featured date; break ties by workflow ID. Select in that order. This least-recently-featured rotation must cover every active workflow before repeating one. Newly active workflows enter the never-featured group; disabled/deleted workflows do not block rotation.
4. Preserve the existing `skip-if-match` gate, which pauses publication while an Agent of the Day PR is open. If a pending PR appears after activation, treat its workflow as reserved: do not duplicate it or count it as published. Closed, unmerged PRs do not advance rotation. Repository memory may cache workflow IDs, dates, post paths, and persona/style history, but reconcile it with published posts and open PRs every run; missing or stale memory must not reset rotation. If history or active-state discovery is incomplete or ambiguous, emit `report_incomplete` rather than guessing.
5. Use `logs` for the selected workflow's recent runs (last 3 days, up to 5 runs), then `audit` for structured evidence when available. If there are no recent runs, look up its latest completed run outside that window and state the actual run date in the post. Do not exclude a workflow merely because it runs infrequently, produced no issue/PR, or has no successful run today.
6. If the selected workflow has no trustworthy evidence or is reserved by an open PR, try the next workflow with the **same rotation priority** (also never featured, or the same oldest last-featured date). Do not bypass unfeatured or older workflows to repeat a more recently featured workflow. If that priority group is exhausted, emit `noop` explaining which workflows are awaiting evidence or publication. Tooling/API failures require `report_incomplete`, not a fallback to a recent favorite.

From evidence, extract:

- Workflow behavior observed in real runs.
- Real links to created/updated issues and PRs.
- Any chart/image links or artifact links.

Include the selected workflow ID and its previous feature date (or "never featured") in the PR's selection rationale. Do not advance published history merely because a draft or PR was created; the merged blog post is the durable rotation record.

### 3) Gather optional chart image

If logs or audit output provide an image URL, use it.

If no remote image URL is available but `docs/public/blog-combined.png` exists, emit it as a single `upload-asset` safe-output (`.png`) and use the returned URL as the markdown image source.

Do not stage the PNG with `git add` and do not include any binary files in the PR.

### 4) Generate persona and draft content

1. Create a fresh blogger persona.
2. Write the story in GitHub blog style using:
   - persona,
   - chosen workflow,
   - extracted run evidence,
   - issue/PR links,
   - optional chart URL.
3. Generate:
   - `seoDescription` (max 160 chars, SERP-friendly),
   - `linkedPostText` (short, clickable link text for post cards/social snippets).
4. If `seoDescription` is over 160 characters, rewrite it before continuing.

### 5) Create blog post file

Write a new Astro blog page with frontmatter:

```md
---
title: "Agent of the Day – <Month Day, Year>"
description: "<one-line summary>"
authors:
  - copilot
date: YYYY-MM-DD
metadata:
  seoDescription: "<optimized seo description>"
  linkedPostText: "<optimized linked text>"
---
```

Body requirements:

- Add `<!-- agent-of-the-day-workflow: <selected workflow ID> -->` immediately after the frontmatter so future runs can identify the spotlight reliably.
- Start with a concise opening paragraph.
- Include an **Agent of the Day** section with authentic narrative grounded in live logs/audit data.
- Include explicit links to referenced issue(s) and PR(s).
- If image URL exists, embed it with markdown image syntax.
- Close with a short call to action pointing to `https://github.com/${{ github.repository }}`.
- Respect metadata limits before opening the PR: `seoDescription` <= 160 chars and `linkedPostText` <= 80 chars.
- Verify limits using bash: `echo -n "your string" | wc -c`; if either value is too long, revise and re-check.

### 6) Open PR

Create a PR with title:

- `Agent of the Day – YYYY-MM-DD`

PR body must include:

- Summary of highlighted workflow and why it was chosen.
- Links used as evidence (issues/PRs/log/audit references).
- File path of the created blog post.
- Prepare the PR locally before calling the safe output:
  1. Inspect `git status --short` and `git branch --show-current`. Create a unique local feature branch with `git checkout -b agent-of-the-day-${{ github.run_id }}` from the current checkout; do not fetch or change the base revision.
  2. Stage only the exact blog post path with `git add -- <blog-post-path>`, never `git add .` or `git add -A`. Inspect `git diff --cached --name-only` and `git diff --cached -- <blog-post-path>`; the staged changes must contain only the intended text blog post, no binary assets.
  3. Commit those staged changes locally with `git commit -m "Add Agent of the Day blog post"`. Verify `git status --short` and `git branch --show-current`. If preparation fails or unrelated staged files exist, emit `report_incomplete`; do not reset or include unrelated files.
  4. Invoke `create_pull_request` exactly once with the real title/body and `branch` equal to the verified local feature branch, using the configured safe-output transport. Do not push.

### 7) No-action rule

If no workflow in the current rotation priority group has trustworthy live evidence and no pending reservation, call `noop` with a short explanation. Do not relax the rotation rule to produce a post.

### 8) Mandatory safe-output completion

You **MUST** finish by emitting exactly one terminal safe output through the configured transport:

- `create_pull_request` when you created the blog post and are ready to open the PR.
- `noop` when no action is needed after valid analysis.
- `report_incomplete` when blocked by infrastructure/tooling failures (for example repeated `Permission denied`, unavailable MCP tools, or inaccessible repository state).

Never end with plain text only and no safe-output call.

#### Quality Bar

- No fabricated details.
- No policy-unsafe or non-corporate language.
- Keep it concise, energetic, and developer-friendly.
- Vary rhythm and phrasing between runs.