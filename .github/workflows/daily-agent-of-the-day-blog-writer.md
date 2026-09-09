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
    runtime: cloud-hypervisor
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
  steer: true
  create-pull-request:
    expires: 7d
    title-prefix: "[blog] "
    labels: [blog]
    reviewers: [copilot]
    draft: false
    allowed-files:
      - "docs/src/content/docs/**"
  upload-asset:
    max: 3
    allowed-exts: [.png, .jpg, .jpeg, .svg]
imports:
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
- Use git in `bash` for read-only inspection only:
  - `git status`
  - `git diff -- docs/src/content/docs/**`
- Do not run git write or branch-management commands yourself (`git checkout`, `git switch`, `git branch`, `git add`, `git commit`, `git reset`, `git rebase`, `git merge`, `git cherry-pick`, `git push`, `git pull`, or any `git remote *`).
- After editing the blog file, call `create_pull_request` directly. It will package the edited docs files into the PR for you.

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

### 2) Collect live workflow evidence

Use `agentic-workflows` MCP tools:

1. `list` to identify active workflows.
2. `logs` for recent runs (last 3 days, up to 5 runs) of top candidates.
3. `audit` for structured evidence when available.

From evidence, extract:

- Workflow behavior observed in real runs.
- Real links to created/updated issues and PRs.
- Any chart/image links or artifact links.

If no useful data appears for the selected workflow, pick another active workflow.

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
- After verifying only the intended docs files changed, call `create_pull_request` directly. Do not stage, commit, or switch branches yourself.

### 7) No-action rule

If no trustworthy live evidence can be gathered after checking multiple workflows, call `noop` with a short explanation.

### 8) Mandatory safe-output completion

You **MUST** finish by calling exactly one safe-output tool:

- `create_pull_request` when you created the blog post and are ready to open the PR.
- `noop` when no action is needed after valid analysis.
- `report_incomplete` when blocked by infrastructure/tooling failures (for example repeated `Permission denied`, unavailable MCP tools, or inaccessible repository state).

Never end with plain text only and no safe-output call.

#### Quality Bar

- No fabricated details.
- No policy-unsafe or non-corporate language.
- Keep it concise, energetic, and developer-friendly.
- Vary rhythm and phrasing between runs.