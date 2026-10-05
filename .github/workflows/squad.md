---
name: Squad
run-name: "Squad — ${{ github.event.inputs.command || github.event.comment.body || github.event.issue.title || 'run' }}"
description: Cast, connect, or adopt a Squad AI team for your repository
emoji: "🤖"
private: false
on:
  bots: ["github-actions[bot]"]
  slash_command:
    name: squad
    events:
      - issues
      - issue_comment
      - pull_request_review_comment
  workflow_dispatch:
    inputs:
      command:
        description: 'Squad command (e.g., cast, connect org/repo, adopt org/repo, status)'
        required: false
        default: 'cast'
      issue_number:
        description: 'Issue number to implement when run manually'
        required: false
        type: string
permissions:
  contents: read
  copilot-requests: write
  issues: read
  pull-requests: read
network:
  allowed:
    - defaults
imports:
  - shared/squad.md
  - shared/planning-ontology.md
  - shared/planning-policy.md
tools:
  bash: true
  github:
    mode: gh-proxy
    toolsets: [default]
safe-outputs:
  data:
    type: object
    properties:
      squad_artifact:
        type: string
        enum:
          - research
          - plan
          - plan-accepted
          - phases-accepted
          - triage
          - lifecycle-state
          - program
          - implementation
          - validation
          - scope-accepted
          - impl-accepted
          - impl-phases-accepted
          - phases-activated
          - activated
      schema_version:
        type: string
        enum: ["1"]
      origin_issue:
        type: integer
        minimum: 1
      phases:
        type: array
        items:
          type: integer
          minimum: 1
    required:
      - squad_artifact
      - schema_version
      - origin_issue
      - phases
    additionalProperties: false
  create-pull-request:
    title-prefix: "[squad] "
    labels: [squad]
    max: 3
    auto-close-issue: false
    allowed-base-branches:
      - "squad/*"
    allowed-files:
      - ".squad/**"
      - ".github/agents/squad.agent.md"
      - "meet-the-squad.md"
    protected-files: allowed
    max-patch-files: 500
    expires: 14d
  create-issue:
    labels: [squad]
    max: 75
  add-comment:
    max: 20
    target: "*"
  dispatch-workflow:
    workflows: [squad-implement-worker]
    max: 3
source: bradygaster/squad/workflows/squad.md@dev
---

## Planning Artifact Data Contract (all modes)

gh-aw removes HTML comments from prompts and sanitized output bodies. Never use HTML comments as Squad state markers.

Every machine-readable planning comment MUST include safe-output `data` with:

```json
{
  "squad_artifact": "{artifact_kind}",
  "schema_version": "1",
  "origin_issue": 123,
  "phases": []
}
```

Use the triggering issue number for `origin_issue`. Because gh-aw requires every declared schema property, emit `phases: []` for non-phase artifacts and the accumulated phase numbers for phase-state artifacts. Validation results remain in the human-readable body. gh-aw appends the validated envelope as a `Structured data:` fenced JSON block in the GitHub body.

When locating artifacts:
1. **Paginate fully** — fetch ALL comments (paginate if >30).
2. **Parse structured data** — match exact `squad_artifact`, `schema_version: "1"`, and the current `origin_issue`.
3. **Latest = newest** — if multiple comments match, use the most recent.

# Squad — `/squad` Slash Command

## Trigger Context

Read slash command from: `github.event.comment.body` (issue/PR comment), `github.event.issue.body` (issue body), or `github.event.inputs.command` (workflow_dispatch, default: `cast`).

- **Issue body:** `github.event.issue.body` — the full issue description
- **Issue comment:** `github.event.comment.body` — the full comment text
- **PR review comment:** `github.event.comment.body` — the full comment text
- **Workflow dispatch:** `github.event.inputs.command` — manual input (default: `cast`)
- **Manual issue target:** `github.event.inputs.issue_number` — issue number for
  `/squad implement` runs started from the Actions tab

The activation job already ran `squad init --preset default`, which produced a
generic 5-agent team (lead, reviewer, devrel, security, docs) in `.squad/`. Cast
mode REPLACES this scaffolding with a team tailored to the repository.

This workflow does not create or modify files under `.github/workflows/`.
Repository owners must configure Copilot setup steps separately when needed.

## Modes

| Command | Mode |
|---------|------|
| `/squad cast` | Cast |
| `/squad connect <source>` | Connect |
| `/squad adopt <url>` | Adopt |
| `/squad cast-member <spec>` | Cast Member |
| `/squad retire <name>` | Retire |
| `/squad status` | Status |
| `/squad research` | Research |
| `/squad plan` | Plan |
| `/squad plan revise <feedback>` | Plan Revise |
| `/squad triage` | Triage |
| `/squad triage revise <feedback>` | Triage Revise |
| `/squad plan program` | Plan Program |
| `/squad plan program revise <feedback>` | Plan Program Revise |
| `/squad plan implementation` | Plan Implementation |
| `/squad plan validate` | Plan Validate |
| `/squad plan accept` | Plan Accept (fast-path) |
| `/squad plan accept phase {N}` | Plan Accept |
| `/squad plan accept scope` | Plan Accept Scope |
| `/squad plan accept implementation` | Plan Accept Implementation |
| `/squad plan accept implementation phase {N}` | Plan Accept Implementation |
| `/squad plan activate` | Plan Activate |
| `/squad plan activate phase {N}` | Plan Activate |
| `/squad implement` | Implement |
| `/squad` (no args) | Cast |

## Parse Command

1. Read trigger body from event payload.
2. Strip `/squad` prefix, trim whitespace.
3. Match **longest-prefix-first**:
   - `plan accept implementation` (3), `plan accept scope` (3), `plan program revise` (3)
   - `plan implementation` (2), `plan program` (2), `plan activate` (2), `plan validate` (2), `plan accept` (2), `plan revise` (2), `triage revise` (2)
   - `cast-member` (1), `plan` (1), `cast`, `connect`, `adopt`, `retire`, `status`, `research`, `triage`, `implement`
4. Default to `cast` if empty.
5. **Phase selector:** If remaining args contain `phase {N}`, extract N.

## Execute Mode

**MODE ISOLATION:** Execute ONLY the active mode's section. Other mode instructions do not apply.

**BREADCRUMB ≠ DELIVERABLE:** Every mode posts an acknowledgment first. This is never the deliverable — always complete ALL subsequent steps.

---

## Team Guard

**Applies to:** Research, Triage, Plan, Plan Program, Plan Implementation, Plan Validate, Plan Revise, Triage Revise, Plan Accept, Plan Accept Scope, Plan Accept Implementation, Plan Activate.
**Exempt:** Cast, Connect, Adopt, Cast Member, Retire, Status, Implement (these run their own pre-checks).

### Step TG-1: Check Team Presence

```bash
git show HEAD:.squad/team.md 2>/dev/null | awk '{sub(/\r$/,"")} /^## Members/{f=1;next} f&&/^#/{f=0} f&&/^\|/&&!/^\|[-: |]*\|$/&&!/\| *Name *\|/' | grep -q . && echo TEAM_PRESENT || echo TEAM_ABSENT
```

`TEAM_PRESENT` requires at least one Markdown table data row inside the `## Members` section of the **git-committed HEAD revision** of `.squad/team.md`. Neither the header row (`| Name | Role | … |`) nor the separator row (`|---|---|`) qualifies. A path absent from HEAD, an empty committed file, a header-only scaffold, or zero member rows all yield `TEAM_ABSENT`.

**Why committed HEAD, not local files:** The activation pre-step (e.g., `squad init --preset default`) may restore a local `.squad/` scaffold before the agent job runs. Reading the local filesystem would return TEAM_PRESENT for that scaffold even though no team has been cast and committed. `git show HEAD:.squad/team.md` reads only what is in the repository's committed state — activation-restored local files are intentionally invisible to this guard. Local activation state is preserved for Cast generation (TG-3 onward); only the guard decision reads committed HEAD.

The leading `sub(/\r$/,"")` normalizes CRLF line endings so Windows-formatted team.md files are classified correctly. If the repo has no commits yet, `git show HEAD:...` exits non-zero and the pipe produces no output → TEAM_ABSENT.

- `TEAM_PRESENT` → proceed to the original mode's section.
- `TEAM_ABSENT` → execute **Auto-Cast Pivot** below; do not proceed with the original mode this run.

### Auto-Cast Pivot

**Canonical command variables** (derived from Parse Command output — never from raw input):
- `{canonical_mode}` — the parsed mode enum (e.g., `research`, `plan`, `plan-activate`); if the mode cannot be determined, substitute the literal string `squad` in prose
- `{canonical_command}` — reconstructed user-safe command: `/squad {canonical_mode}` with optional `phase {N}` suffix; if `{canonical_mode}` cannot be determined, use `/squad` as the safe fallback
- `{phase_n}` — numeric phase if present; omit the field otherwise

**Universal response invariant:** current state · result · one primary next action · recovery on ⚠️/🔴.

#### TG-3: Dedup Open Cast PR

```bash
gh pr list --state open --json number,url,headRefName --jq '[.[] | select(.headRefName | (startswith("squad/cast-") and (startswith("squad/cast-member-") | not)))] | first'
```

**If an open Cast PR is found (rerun before merge):**
- `add-comment`:
  ```
  🤖 Squad has already opened a Cast PR for this issue.

  **Current state:** Cast PR open — your team is ready for review.
  **Result:** No duplicate PR opened.
  **Next action:** Merge the Cast PR, then return to this issue and rerun: `{canonical_command}`

  **Cast PR:** {pr_url}
  ```
- Stop. Do not run Cast mode.

**If no open Cast PR found (first run, failed run, or a prior Cast PR was closed):**
- Execute Cast Mode Steps 0–6 using this issue as the casting brief.
- The Cast PR body may reference the originating issue and canonical command in plain language, but MUST NOT contain `Fixes`, `Closes`, or `Resolves` closing keywords for the originating work issue.
- Then `add-comment`:
  ```
  🤖 Your `{canonical_mode}` command found no team yet — Squad has automatically opened a Cast PR to assemble one.

  **Current state:** No team detected — Squad auto-pivoted to Cast.
  **Result:** A Cast PR has been opened. Your `{canonical_mode}` command is paused this run — resume it after merging.
  **Next action:** Merge the Cast PR, then return to this issue and rerun: `{canonical_command}`

  ⚠️ A direct Cast PR link is not available in this comment. Find it in the **Pull Requests** tab.
  ```
- A closed or failed Cast PR is not durable team state. A later rerun with no committed roster and no open Cast PR MUST attempt Cast again.
- Stop. Do not run the original command this run.

**Recovery (Cast step failure):** Report the exact error in plain language. Tell the user to rerun `{canonical_command}` on this issue to retry. Never instruct the user to run `/squad cast` separately.

---

#### Cast Mode

Analyze repo, compose team, assign character names from a fictional universe, generate `.squad/` scaffolding, open PR.

**Acknowledge:** `🤖 Squad is analyzing your repo and assembling a team…`

##### Step 0: Brief Resolution

Evaluate issue (title + body) and repo content to determine primary casting input:

| Repo | Issue | Result |
|------|-------|--------|
| Empty | Empty | **Noop** — post "Nothing to cast from" message, stop |
| Empty | Has content | **Issue wins** |
| Has content | Has content | **Merge** — repo base, issue augments/overrides |
| Has content | Empty/minimal | **Repo wins** |
| Any | Explicit team spec | **Issue is source of truth** |

"Explicit source-of-truth signal" = issue reads like a team spec (role lists, team-size declarations, operating-model descriptions).

##### Step 1: Repo Analysis

Analyze: languages/frameworks, project structure, CI/CD, testing, docs, tooling, README/purpose. Produce mental summary: project type, technologies, team size (4–7), needed specialist roles.

##### Step 2: Team Composition

Every team gets a **Lead**. Then allocate specialists based on signals:

| Signal | Role |
|--------|------|
| Frontend framework | Frontend Engineer |
| Backend/API | Backend Engineer |
| DB schemas/migrations | Data Engineer |
| Test suites | Test Engineer |
| CI/CD, Docker | DevOps/Platform |
| Auth, crypto | Security Engineer |
| Docs, tutorials | DevRel/Docs |
| Multiple packages | Integration Engineer |
| ML/data pipelines | ML Engineer |
| Mobile | Mobile Engineer |

Guidelines: 4–7 agents. Min: Lead + 2 specialists + 1 quality role. Scribe, Ralph, Rai are built-in (don't count).

##### Step 3: Universe & Name Allocation

1. Count agents from Step 2.
2. Select universe (pick one whose capacity fits with minimal waste):

| Universe | Cap | Shape |
|----------|-----|-------|
| The Usual Suspects | 6 | small, noir |
| Reservoir Dogs | 8 | small, noir |
| Alien | 8 | small, sci-fi |
| The Goonies | 8 | small, adventure |
| The Matrix | 10 | medium, sci-fi |
| Firefly | 10 | medium, sci-fi |
| Star Wars | 12 | medium, sci-fi |
| Breaking Bad | 12 | medium, drama |
| Futurama | 12 | medium, sci-fi |
| Ocean's Eleven | 14 | medium, heist |
| Arrested Development | 15 | medium, comedy |
| Lost | 18 | large, mystery |
| DC Universe | 18 | large, action |
| The Simpsons | 20 | large, comedy |
| Marvel Cinematic Universe | 25 | large, action |

3. Name rules: one universe only, pressure/function over authority, no spoilers, early-introduction names, Scribe/Ralph/Rai keep built-in names.
4. Record in `.squad/casting/registry.json`: `{ "agents": { "{id}": { "created_at": "ISO", "persistent_name": "Name", "universe": "Universe", "legacy_named": false, "status": "active" } } }`
5. Initialize `.squad/casting/history.json`: `{ "universe_usage_history": [{ "universe": "Name", "assigned_at": "ISO", "agent_count": N }], "assignment_cast_snapshots": {} }`

##### Step 4: Generate Scaffolding

Create/replace:

1. **`.squad/team.md`** — Roster table: Coordinator (Squad), Members (Name|Role|Charter path|Status), always-on (Scribe, Ralph, Rai), Coding Agent (@copilot with `copilot-auto-assign: false`).
2. **`.squad/agents/{id}/charter.md`** — Per agent: `# Name — Role`, Identity block (name, role, expertise, style), "What I Own", Boundaries (handle/don't), Model: auto.
3. **`.squad/routing.md`** — Domain→agent routing table.
4. **`.squad/casting/registry.json`** — From Step 3.
5. **`.squad/casting/history.json`** — From Step 3.
6. **`.squad/casting/policy.json`** — Standard policy with all 15 universes.
7. **`.squad/decisions/`** — Empty directory.
8. **`.github/agents/squad.agent.md`** — Verify exists (from `squad init`), include in PR.

##### Step 5: Generate meet-the-squad.md

Create `meet-the-squad.md` at repo root with: title, universe name, team table (Name|Role|Specialty|How to talk), Always-On Support table, How to Work With Your Squad (label-based assignment with `9B8FCC` color, iteration commands, routing reference), "What Happened Here" block with analysis rationale (languages, structure, CI/CD, rationale), footer with cast date.

##### Step 6: Open PR

`create-pull-request`: branch `squad/cast-{repo}`, title `[squad] Cast your Squad — {description}`, body with team summary. Append to the PR body: "After merging, return to the originating issue and rerun `{canonical_command}` to resume your work." Files: `.squad/`, `.github/agents/squad.agent.md`, `meet-the-squad.md`. Stage only these.

##### Step 7: Post Completion

`add-comment`: `🧑‍🤝‍🧑 Your Squad is ready for review.\n\n**PR:** #{pr_number}\n\nMerge the PR to activate your team. Run /squad status afterward to verify.`

---

#### Connect Mode

Link repo to an external Squad source. Commits only a config pointer.

**Acknowledge:** `🤖 Squad is setting up the remote connection…`

1. **Parse source:** Extract `owner/repo` from args. Accept full URLs or shorthand. If missing, post usage help, stop.
2. **Validate:** Run `gh api repos/{owner}/{repo}/contents/.squad/team.md --jq .name`. On 404/error, post error comment, stop.
3. **Write config:** Create `.squad/config.json`: `{ "squadSource": "{owner}/{repo}", "mode": "connect", "connectedAt": "ISO" }`. Only `.squad/` file committed.
4. **Generate meet-the-squad.md** with Connect rationale: "externally managed — connected from `{source}`."
5. **Open PR:** `create-pull-request`: branch `squad/connect-{repo}`, files: `.squad/config.json`, `meet-the-squad.md` only.
6. **Post:** `🔗 Squad connection configured.\n\n**PR:** #{pr_number}`

---

#### Adopt Mode

Fetch complete squad from remote, commit locally. No ongoing sync.

**Acknowledge:** `🤖 Squad is importing the team definition…`

1. **Parse source:** Same as Connect. If missing, post usage help, stop.
2. **Validate & fetch:** `gh api repos/{owner}/{repo}/contents/.squad --jq '.[].name'`. On error, post, stop. Fetch `.squad/` recursively + `.github/agents/squad.agent.md` if exists.
3. **Install:** Copy `.squad/` (replacing init scaffolding) + agent file. Write `.squad/config.json`: `{ "squadSource": "{owner}/{repo}", "mode": "adopt", "adoptedAt": "ISO" }`.
4. **Adapt:** Update `.squad/routing.md` paths and charter references to match target repo structure.
5. **Generate meet-the-squad.md** with Adopt rationale: "adopted from `{source}` and now locally owned."
6. **Open PR:** `create-pull-request`: branch `squad/adopt-{repo}`, files: `.squad/`, `.github/agents/squad.agent.md`, `meet-the-squad.md`.
7. **Post:** `📥 Squad adopted from remote source.\n\n**PR:** #{pr_number}`

---

#### Cast Member Mode

Add/modify/rename a single team member within an existing squad.

Subcommands: `/squad cast-member <description>` (add), `/squad cast-member rename|modify <name> to <change>`.

1. **Parse:** Determine operation (add vs modify/rename).
2. **Validate squad:** Confirm `.squad/team.md` and registry exist. If not, suggest `/squad cast`, stop.
3. **Check duplicates** (new only): If similar role exists, ask user to confirm.
4. **Allocate identity** (new only): Same universe, unused name, same naming rules. If universe full, suggest retire or re-cast.
5. **Generate/regenerate charter:** New: create from template. Modify: update expertise/ownership/boundaries, preserve name and `created_at`.
6. **Update files:** `.squad/team.md`, `.squad/routing.md`, `.squad/casting/registry.json`, `meet-the-squad.md`.
7. **Open PR:** On Squad PR: follow-up PR targeting existing branch. On issue: `create-pull-request` branch `squad/cast-member-{id}`, title `[squad] Add/Modify {Name}`.
8. **Post:** `👤 {Name} ({Role}) has been added to the team.\n\n**PR:** #{pr_number}`

---

#### Retire Mode

Remove a member from active roster, archive charter.

1. **Identify:** Match arg against name/role/id (case-insensitive). If no/ambiguous match, list active members, ask to clarify.
2. **Archive:** Move `.squad/agents/{id}/` to `.squad/agents/_alumni/{id}/`. Add retirement header to charter.
3. **Update files:** Registry: set `status: "retired"`, add `retired_at`. team.md: remove row. routing.md: remove/reassign rules. meet-the-squad.md: remove from table.
4. **Open PR:** Same context-aware behavior as Cast Member. Branch: `squad/retire-{id}`.
5. **Post:** `👋 {Name} has been retired from the team.\n\n**PR:** #{pr_number}`

---

#### Status Mode

Read-only team composition report.

**Acknowledge:** `🤖 Squad is checking team status…`

1. If `.squad/team.md` missing, reply "no squad cast yet, suggest `/squad cast`".
2. Read team.md + registry.json.
3. Post comment: team name, universe, member count, active members table, link to team.md.

---

#### Implement Mode

Implement mode dispatches an isolated implementation worker for a regular issue.
When invoked on an epic, it dispatches workers for up to three currently
unblocked children. The worker relays merged implementation pull requests back
to this mode so it can automatically refill the parent epic's available slots.

**Acknowledge:** Post `🤖 Squad is preparing implementation…` using the
`add-comment` safe-output.

##### Step 1: Validate and Gather Context

1. Resolve the target issue number from `github.event.inputs.issue_number` for a
   workflow dispatch, otherwise from the triggering issue. If invoked from a
   pull request review comment, explain that `/squad implement` must be run from
   the target issue.
2. Read the target issue title, body, labels, state, and relevant comments.
3. Find open child issues using native GitHub sub-issue relationships. Also
   include open issues whose body contains a
   `Parent: #{target-issue-number}` line for compatibility with older plans.
4. If child issues exist, treat the target as an epic and follow the Epic
   Dispatch procedure below. Do not implement the epic body directly.
5. If no child issues exist, call the workflow-specific
   `squad_implement_worker` safe-output tool with `issue_number` set to the
   target issue number.
6. Post a comment linking the dispatched worker run. The worker performs
   dependency, duplicate pull request, routing, implementation, and validation
   checks.

##### Epic Dispatch

For each open child issue:

1. Parse its `Depends on:` line and check the state of every referenced issue.
2. Exclude children with any open dependency.
3. Find children that already have an open pull request whose branch starts
   with `squad/implement-{child-number}-` or whose body closes that child.
   These are active implementation children.
4. Calculate `available-slots = max(0, 3 - active-implementation-count)`.
5. Exclude active implementation children from the ready set.
6. Sort ready children by issue number and select at most `available-slots`.

For each selected child, call the workflow-specific `squad_implement_worker`
safe-output tool with this input:

```json
{
  "issue_number": "{child-issue-number}"
}
```

Never call the generic `dispatch_workflow` tool. Never emit a dispatch without a
non-empty numeric `issue_number`. Emit exactly one workflow-specific dispatch
per selected child, and only report a child as dispatched after the tool returns
success.

Post a comment on the epic listing the dispatched children, blocked children,
children with existing implementation pull requests, and any ready children
deferred because all three slots are occupied. If no child is ready or no slot
is available, post the status summary and do not dispatch a workflow.

After each implementation pull request merges, this workflow runs again and
fills newly available slots. Continue until the epic has no open children.
`/squad implement` remains available as a manual recovery command.

---

#### Research Mode

Deep analysis → structured findings comment. Read-only + comment. Works on open/closed issues.

**Acknowledge:** `🤖 Squad is researching this…`

**TASK:** Steps 1–4. The deliverable is Step 3's findings comment. Reserve ≥40% budget for Step 3.

##### Step 1: Determine Scope

- Issue-driven: issue has substantial content → research codebase in that context.
- Repo-driven: issue minimal → general architecture/health assessment.
- Combined: issue is lens on repo.
- Text after `/squad research` = research focus.

##### Step 2: Deep Repo Analysis

Budget-aware breadth-first investigation: architecture mapping, technology audit, code health, gap analysis, risk identification, prior art. If `.squad/team.md` exists, frame findings by team ownership.

##### Step 3: Post Findings

`add-comment` with `data: {"squad_artifact":"research","schema_version":"1","origin_issue":{issue_number},"phases":[]}`.

Structure: `## 🔬 Squad Research — {Title}` → Summary (2-3 sentences) → Current State → Gap Analysis → Risk & Complexity table (Area|Risk 🟢/🟡/🔴|Complexity S/M/L/XL|Notes) → Key Findings (with evidence) → Recommendations → Next Step (`/squad triage` or `/squad plan`).

Must be ≥200 chars of substantive findings. Tailor sections to scope.

##### Step 4: Verify Completion [MANDATORY]

Confirm: structured artifact data posted, heading present, ≥200 chars substantive content, ≥1 recommendation. If ANY fails, go back and post now.

---

#### Plan Mode

Decompose issue into sub-issues as a comment. Does NOT create issues. Works on open/closed issues.

**Acknowledge:** `🤖 Squad is creating a plan…`

**TASK:** Steps 1–3. Deliverable = Step 3.

##### Step 1: Gather Context

1. Read issue body (the epic/brief).
2. Find latest `research` artifact comment for this issue. If found, use as primary context. If not, do lightweight repo analysis.
3. Read `.squad/team.md` if exists for agent assignments.
4. Text after `/squad plan` = planning guidance.

##### Step 2: Decompose

Break into discrete work items. **Minimum 3 items** unless genuinely atomic (explain why if fewer). Each item: independently deliverable, single-owner, testable, right-sized. Consider: dependency order, parallel tracks, risk ordering, vertical slices.

##### Step 3: Post Plan

`add-comment` with `data: {"squad_artifact":"plan","schema_version":"1","origin_issue":{issue_number},"phases":[]}`.

Structure: `## 📋 Squad Plan — {Title}` → reference line → Phase tables (# | Title | Owner | Size | Depends On) → Details per item (Scope, Acceptance criteria, Notes) → Dependency Graph → Execution Notes → Next Steps (`/squad plan accept`, `/squad plan accept phase 1`, `/squad plan revise`, `/squad plan`).

Do NOT create issues.

---

#### Plan Accept Mode (Fast Path)

`/squad plan accept` [phase {N}] — combines scope+impl+activation for simple workflows.

**Behavior:** If `program` or `implementation` artifacts exist, run Accept Scope → Accept Impl → Activate in sequence. If only a `plan` artifact exists, use legacy behavior below.

**Acknowledge:** `🤖 Squad is creating the planned issues…`

##### Step 1: Find Plan

Find latest `plan` artifact comment for this issue. If none: reply "No plan found. Run `/squad plan` first."

##### Step 1a: Phase Resolution

1. Extract `requested_phase` from args (or null).
2. Find the latest `phases-accepted` artifact and read its `phases` array → `accepted_phases` (or []).
3. Validate: already-accepted → stop with next-available hint. Out-of-order → stop with sequential hint.
4. Filter items: by phase if set, by unaccepted if prior phases exist, all if fresh.
5. If no items remain after filter: stop.

##### Step 2: Create Sub-Issues — Hierarchical

If plan has phases: Root → Phase issues → Task issues. Flat plan: tasks directly under root.

For each work item, `create-issue`:
- Title: work item title
- Labels: `squad` (color `9B8FCC`), `squad:{owner}` (color `9B8FCC`)
- Body: scope, acceptance criteria, context (parent, phase, size, depends on, owner), notes, footer
- Parent: phase issue (hierarchical) or root (flat)
- Size: set Project field if available, else body `**Size:**` line

Cross-phase deps: look up real issue numbers from prior acceptance comments.

Create in dependency order. Labels must have descriptions and colors.

##### Step 3: Native Dependency Edges

Add `blockedBy` relationships via GitHub API. Graceful fallback to body-text references if unavailable.

##### Step 4: Post Summary

Artifact data varies:
- Phase-specific: `data: {"squad_artifact":"phases-accepted","schema_version":"1","origin_issue":{issue_number},"phases":[{accumulated}]}` → Phase accepted table + remaining phases table
- Full (no phases): `data: {"squad_artifact":"plan-accepted","schema_version":"1","origin_issue":{issue_number},"phases":[]}` → All issues table

---

#### Plan Revise Mode

**Acknowledge:** `🤖 Squad is revising the plan…`

1. Find the latest `plan` artifact. If none: reply "No plan found."
2. Read feedback after "revise".
3. Apply feedback to plan.
4. **EDIT the existing artifact comment** (never post a duplicate).
5. Prepend revision note.

---

#### Triage Mode

Classify research findings as work/decision/excluded. Bridge between research and planning. Works on open/closed issues.

**Acknowledge:** `🤖 Squad is triaging research findings…`

**TASK:** Steps 1–4. Deliverable = Step 3.

The planning ontology is imported — follow its schemas directly.

##### Step 1: Validate

1. Find the latest `research` artifact for this issue. If none: reply "Run `/squad research` first." Stop.
2. Read root issue body (the Intent). If empty: reply "Issue body empty — add description." Stop.

##### Step 2: Classify

For each finding, assign disposition:
- **`work`**: needs building/changing. Include scope sketch, effort (S/M/L/XL), rationale.
- **`decision`**: requires human judgment. Flag question, impact, what it blocks.
- **`excluded`**: not relevant to intent. Reference intent in justification.

Default to `decision` when uncertain.

##### Step 3: Post Triage

`add-comment` with `data: {"squad_artifact":"triage","schema_version":"1","origin_issue":{issue_number},"phases":[]}`.

Structure: `## 🔍 Squad Triage — Dispositions` → Intent + reference lines → Work Items table (Finding|Scope Sketch|Effort|Rationale) → Decisions Needed table (Finding|Question|Impact|Blocks) → Excluded table (Finding|Reason) → Summary counts → Next step: `/squad plan program` or `/squad triage revise`.

##### Step 4: Update Lifecycle

Find/create the `lifecycle-state` artifact comment. Include `data: {"squad_artifact":"lifecycle-state","schema_version":"1","origin_issue":{issue_number},"phases":[]}`. Set Triage = `✅ Done`, state = Triaged, next = `/squad plan program`.

---

#### Triage Revise Mode

**Acknowledge:** `🤖 Squad is revising triage dispositions…`

1. Find the latest `triage` artifact. If none: reply "Run `/squad triage` first."
2. Read feedback after "revise".
3. Apply: reclassify, split, merge, adjust.
4. **EDIT the existing artifact comment** (one current artifact per issue). Prepend revision note.
5. Update lifecycle.

---

#### Planning Policy Resolution

All planning modes resolve policy before executing.

The planning policy schema is imported — follow it directly.

Steps:
1. Check issue body HTML-comment content for a line beginning `squad-policy:` or `squad-setting:`. Match the comment content, not the HTML delimiters.
2. Check repo for `.squad/planning-policy.md` with YAML frontmatter.
3. Match profile (`default`, `lean`, `enterprise`, `spike`, or custom).
4. Fall back to defaults for unset values.

Apply: artifact limits, sizing constraints, hierarchy rules, GitHub representation, validation strictness. Report active policy in every plan output: `Policy: {profile} ({overrides or "no overrides"})`.

---

#### Plan Program Mode

High-level program plan (the WHAT). Transforms triage work items into initiatives/epics/stories/milestones/dependencies. Works on open/closed issues.

**Acknowledge:** `🤖 Squad is building the program plan…`

The planning ontology is imported — follow its schemas directly.

##### Step 1: Validate

Find the latest `triage` artifact. If none: reply "Run `/squad triage` first." Stop. Read root issue body.

##### Step 2: Parse Triage

Extract work items, decisions, excluded from triage comment.

##### Step 3: Construct Hierarchy

Build: Initiatives (outcome-bearing top-level) → Epics (capability groupings) → Stories (user-observable increments) → Milestones (demonstrable outcomes) → Dependencies (DAG).

Rules: every triage work item → ≥1 story. Story → 1 epic. Epic → 1 initiative. Epic → 1 milestone. No cycles. Vertical slices preferred.

##### Step 4: GitHub Mapping

| Concept | GitHub Rep | Notes |
|---------|-----------|-------|
| Initiatives | Root issues | Labeled `initiative` |
| Epics | Parent issues | Labeled `epic` |
| Stories | Sub-issues | Standard |
| Milestones | GitHub milestones | Named after outcome |
| Dependencies | Issue bodies | Native `blocked-by` when available |

Not created yet — describes what activation will produce.

##### Step 5: Post Program Plan

`add-comment` with `data: {"squad_artifact":"program","schema_version":"1","origin_issue":{issue_number},"phases":[]}`.

Structure: `## 📋 Squad Program Plan` → Intent + triage ref → Milestones table (Milestone|Outcome|Contains) → Initiatives & Epics (per initiative: outcome, epic table with Description|Stories|Milestone|Depends On, details per epic with Outcome/Stories/Acceptance criteria) → Unresolved Decisions table → Program Metadata → Dependency Graph → Next: `/squad plan accept scope` or `/squad plan program revise`.

##### Step 6: Update Lifecycle

Set Program Plan = `✅ Done`, state = Program Planned, next = `/squad plan accept scope`.

---

#### Plan Program Revise Mode

**Acknowledge:** `🤖 Squad is revising the program plan…`

Works on open/closed issues.

1. Find the latest `program` artifact. If none: reply "Run `/squad plan program` first."
2. Check for a `scope-accepted` artifact. If scope accepted: require override flag or stop.
3. Read feedback after "revise".
4. Apply revisions maintaining structural integrity (all Step 3 rules still apply, DAG preserved).
5. **EDIT existing comment**. Prepend revision note.
6. Update lifecycle. If scope was invalidated (override), set Scope back to `⬚ Pending`.

---

#### Plan Implementation Mode

Decompose program plan into PR-sized tasks with deps, sizing, agent assignments. Works on open/closed issues.

**Acknowledge:** `🤖 Squad is building the implementation plan…`

The planning ontology is imported — follow its schemas directly.

**TASK:** Steps 1–5. Deliverable = Step 4.

##### Step 1: Validate

Search in order: `scope-accepted` artifact (use as authoritative) → `program` artifact (draft) → `plan` artifact (fast-path). If none: reply "Run `/squad plan program` or `/squad plan` first." Stop.

##### Step 2: Decompose Into Tasks

Per task specify: Title, Scope (files/modules/APIs), Acceptance criteria, Size (XS <1h, S 1-3h, M 3-8h, L 1-2d; max per policy default L), Dependencies (task numbers), Agent, Rollout notes.

Rules: no task > max_task_size. DAG only. Every task traces to program item. Every epic has ≥1 task. Vertical slices. Group into phases by dependency order (Phase 1 = no deps).

##### Step 3: Validate Structure

Check: sizes ≤ L, no cycles, traceability, coverage, agent validity. Fix before posting.

##### Step 4: Post Implementation Plan

`add-comment` with `data: {"squad_artifact":"implementation","schema_version":"1","origin_issue":{issue_number},"phases":[]}`.

Structure: `## 🔧 Squad Implementation Plan` → Program ref → Phase tables (Title|Size|Depends On|Agent|Epic) → Details per task (Scope, Acceptance criteria, Dependencies, Rollout, Traces to) → Dependency Graph → Sizing Summary table → Validation Pre-check → Next: `/squad plan validate` or `/squad plan accept implementation`.

##### Step 5: Update Lifecycle

Set Implementation Plan = `✅ Done`, state = Implementation planned, next = `/squad plan validate`.

---

#### Plan Validate Mode

Readiness gate checking plan artifacts for structural issues.

**Acknowledge:** `🤖 Squad is validating the plan…`

The planning ontology is imported — follow its schemas directly.

**TASK:** Steps 1–5. Deliverable = Step 3.

##### Step 1: Locate Artifacts

Find the latest `program`, `implementation`, and `triage` artifacts. At minimum one of program/implementation must exist or stop.

##### Step 2: Run Checks

| # | Check | Applies To | Fails When |
|---|-------|-----------|-----------|
| 1 | Unresolved IDs | Program, Impl | TBD/TODO/??? in value fields |
| 2 | Missing traceability | Impl→Program | Task doesn't trace to program item |
| 3 | Invalid hierarchy | Program | Epic with no stories |
| 4 | Dependency cycles | Impl | Circular chains |
| 5 | Oversized work | Impl | Task > L |
| 6 | Missing decisions | Program | Unresolved decisions blocking epics |
| 7 | Incomplete metadata | Both | Missing sizes/agents/criteria |
| 8 | Orphaned items | Both | Triage items not in program |
| 9 | Milestone gaps | Program | Epics not in any milestone |

Severity: ❌ Critical (blocks acceptance): 1–6, 8. ⚠️ Warning: 7, 9, borderline 5.

##### Step 3: Post Result

`add-comment` with `data: {"squad_artifact":"validation","schema_version":"1","origin_issue":{issue_number},"phases":[]}`. Keep `RESULT: PASS` or `RESULT: FAIL` in the human-readable body.

Structure: `## ✅/❌ Squad Plan Validation — PASSED/FAILED` → Validated artifacts + timestamp → Results table (Check|Status|Details) → Issues Found (Critical ❌ then Warnings ⚠️ with fix instructions) → Summary (counts + verdict) → Next action.

Rules: any ❌ = FAILED heading+verdict. Warnings alone ≠ failure.

##### Step 4: Update Lifecycle

Set Validation = `✅ Done` or `❌ Failed`. Next on pass: `/squad plan accept implementation`. On fail: fix + re-run.

##### Step 5: Surface Next Action

Pass: suggest accept. Fail: suggest fix + re-validate.

---

#### Plan Accept Scope Mode

`/squad plan accept scope` — locks the program plan (the WHAT).

**Acknowledge:** `🤖 Squad is accepting scope and creating the program backlog…`

##### Step 1: Validate

1. Find the latest `program` artifact. If none: reply "Run `/squad plan program` first." Stop.
2. Check whether a `scope-accepted` artifact already exists → reply already accepted, stop.

##### Step 2: Readiness

1. Check triage for unresolved decisions blocking epics → list them, stop.
2. Check program plan for placeholders → list them, stop.

##### Step 3: Record

`add-comment` with `data: {"squad_artifact":"scope-accepted","schema_version":"1","origin_issue":{issue_number},"phases":[]}`.

Content: `## ✅ Scope Accepted` → program plan version link, accepted by, date, what was approved (initiative/epic counts, scope boundary), lock note.

##### Step 4: Update Lifecycle

Set Scope = `✅ Done`, next = `/squad plan implementation`.

---

#### Plan Accept Implementation Mode

`/squad plan accept implementation` [phase {N}] — locks the implementation plan (the HOW). Supports incremental phase acceptance.

**Acknowledge:** `🤖 Squad is creating implementation tasks…`

##### Step 1: Validate

**Precondition:** a `validation` artifact whose human-readable body contains `RESULT: PASS` must exist.

1. Find a `scope-accepted` artifact. If none: reply "Accept scope first." Stop.
2. Find the latest `implementation` artifact. If none: reply "Run implementation first." Stop.
3. Find validation PASS. If none/FAIL: reply "Run validate first." Stop.
4. Check for an `impl-accepted` artifact. If it exists and no phase arg: reply already accepted, stop.

##### Step 1a: Phase Resolution

Same pattern as Plan Accept: extract `requested_phase`, find the latest `impl-phases-accepted` artifact and read its `phases` array → `accepted_impl_phases`, validate order/duplication, scope acceptance to phase or remaining.

##### Step 2: Validate Integrity

Run: size ≤ L, no cycles, traceability, coverage, agent validity (scoped to target items). On failure: list issues, stop.

**Sizing source:** Use the latest passed `validation` artifact's Sizing Summary table as authoritative. Copy verbatim. Do NOT re-derive from plan text.

##### Step 3: Record

Artifact data varies:
- Phase: `data: {"squad_artifact":"impl-phases-accepted","schema_version":"1","origin_issue":{issue_number},"phases":[{accumulated}]}` → `## ✅ Implementation Phase {N} Accepted` with phase sizing, remaining phases table
- Full: `data: {"squad_artifact":"impl-accepted","schema_version":"1","origin_issue":{issue_number},"phases":[]}` → `## ✅ Implementation Accepted` with total sizing (from validation), lock note

Both include: impl plan link, scope acceptance link, accepted by, date, counts.

##### Step 4: Update Lifecycle

Phase: `🔄 Phase {N} of {total}`. Full: `✅ Done`, next = `/squad plan activate`.

##### Step 5: Auto-Activate (Phase-Specific Only)

After phase acceptance, check if ready for automatic activation:
1. All prior phases must be accepted AND activated (check the latest `phases-activated` artifact's `phases` array).
2. If Phase 1 or all prior activated: auto-activate using Plan Activate logic for this phase.
3. If prior phases not activated: tell user to activate them first.
4. Update lifecycle to reflect both acceptance and activation.

---

#### Plan Activate Mode

`/squad plan activate` [phase {N}] — creates real GitHub issues/milestones. Irreversible.

**Acknowledge:** Phase: `🤖 Squad is activating Phase {N}…` Full: `🤖 Squad is activating the team…`

##### Step 1: Validate

**Phase-specific:**
1. Check the latest `impl-phases-accepted` artifact's `phases` array contains requested phase. If not: stop.
2. Check ordering: prior phase must be in the latest `phases-activated` artifact's `phases` array. If not: stop.
3. Check not already activated.

**No phase:**
1. Find an `impl-accepted` artifact or fully accepted `impl-phases-accepted` state. If none: stop.
2. Check for an `activated` artifact: if it exists, only create missing issues (idempotent).

##### Hallucination Guard

After EVERY `create-issue` call: verify returned issue number, stop on failure, NEVER predict issue numbers.

##### Output Budget Awareness

Count expected issues before starting. If total > 50: recommend phased activation (`/squad plan activate phase {N}`) and proceed with the current phase only. If total > 30: use compact issue bodies (scope + acceptance criteria only; omit elaboration).

##### Label Pre-flight

Before the first `create-issue`, verify labels `squad` and any `squad:{agent}` exist. If missing, record them in the activation summary as a prerequisite gap (label creation requires `issues: write` + `create-label` safe-output — not configured in this workflow). Continue activation — `create-issue` will apply any existing labels normally; unavailable labels are omitted and reported, not silently applied.

##### Transient Failure Handling

On `5xx` response from `create-issue`: wait briefly and retry once. On second failure or `4xx`: record the issue title as skipped in the activation summary, continue with remaining issues. Never abort the full run for a single transient failure.

##### Sub-issue Fallback

When setting a `parent` sub-issue relationship returns `404` or `422` (feature disabled or repo plan): degrade gracefully — record the intended parent as a body reference (`Parent: #{issue_number}`), then continue. Never fail activation over sub-issue API unavailability.

##### Step 2: Create Issues — Full Hierarchy

Root → Epics → Tasks. Phase-specific: filter to matching phase heading.

**2a. Create Milestones:** Check for existing, create missing. Record IDs. On failure: document in root issue body instead.

**2b. Create Epic Issues:** `create-issue` per epic (dedup by title `[Epic] {name}` if already exists from prior phase).
- Title: `[Epic] {name}`
- Labels: `squad` (0075ca), `squad:{agent}` (e4e669)
- Body: outcome, stories, epic-level acceptance criteria, context (parent, initiative, milestone, deps)
- Parent: sub-issue of root intent issue
- Milestone: assigned

**⚠️ DO NOT STOP after epics. Tasks MUST follow immediately.**

**2c. Create Task Issues:** `create-issue` per task in dependency order.

> **⚠️ ATOMIC CONTRACT — strictly one task at a time:**
> For each task: compose ONLY that task's body → call `create-issue` immediately → verify the returned issue number → then move to the next task.
> **DO NOT** compose or buffer multiple task bodies before making calls. One compose → one call → one verify, repeated per task.

- Title: task title
- Labels: `squad` (0075ca), `squad:{agent}` (e4e669). No `size:*` labels unless policy says so.
- Body: one sentence describing scope; 1-2 acceptance criteria; one compact context line (parent epic, size, deps)
- Parent: sub-issue of EPIC (not root)
- Milestone: same as parent epic
- Size: Project field if available, else body line

**2d. Self-Validation:** Compare created/recognized task count vs expected (use the plan's declared total — not the safe-output cap). If created count is below expected: call `report_incomplete` immediately with `created={N}`, `expected={M}`, and the last verified issue number — never noop. Post: `N of M issues created so far — rerun the identical activation command to continue.` Re-runs are idempotent via title match. Never surface the `create-issue` or `add-comment` safe-output caps as the reason for a partial run.

Labels must have descriptions and intentional colors.

##### Step 3: Native Dependency Edges

Add `blockedBy` via API for tasks and epics. Graceful fallback. Never fail activation over edge creation.

##### Step 4: Post Activation Record

**LAST action** — only after Steps 2+3 complete.

Phase artifact: `data: {"squad_artifact":"phases-activated","schema_version":"1","origin_issue":{issue_number},"phases":[{accumulated}]}` → `## ✅ Phase {N} Activated — {count} issues` + issue table + remaining phases table.

Full artifact: `data: {"squad_artifact":"activated","schema_version":"1","origin_issue":{issue_number},"phases":[]}` → `## ✅ Plan Activated — {epic_count} epics, {task_count} tasks` + hierarchy summary, created epics table, created tasks table, dependency order.

Terminal (last phase): emit `data: {"squad_artifact":"activated","schema_version":"1","origin_issue":{issue_number},"phases":[{all_phases}]}` with an "All Phases Activated" heading.

##### Step 5: Update Lifecycle

Phase: `🔄 Phase {N} of {total} activated`. Next: accept/activate next phase.
Full/last: `✅ Done`, state = Activated. Terminal — no next action needed.