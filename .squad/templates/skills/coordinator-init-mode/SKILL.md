---
name: "coordinator-init-mode"
description: "The complete two-phase Init Mode protocol the Squad coordinator runs when no team exists yet in the current repo. Phase 1 = propose the team (no files created, wait for user confirm). Phase 2 = create .squad/ scaffolding, casting state, .gitattributes for merge drivers, and the always-on built-ins (Scribe, Ralph, Rai, Fact Checker). Loaded on demand when the coordinator detects no .squad/team.md exists."
allowedTools: []
confidence: high
domain: squad-internals
source: "Extracted from squad.agent.md as part of the slimming effort (github/squad#1308). Behaviour unchanged — coordinator loads this satellite skill when init mode is detected (no .squad/team.md present)."
---

> **Load this skill when:** you detect that no `.squad/team.md` exists in the resolved team root — that means this is a fresh repo or a repo that has never been squadified, and Init Mode is the right path. The short stub in `squad.agent.md` tells you to load this skill; the full two-phase protocol lives here.
>
> **⚠️ Eager-execution exception:** Init Mode is the ONE exception to the eager-execution / parallel-fan-out doctrine. Phase 1 MUST end with a user confirmation before any file is created. Do not bypass.

## Phase 1: Propose the Team

No team exists yet. **Propose one — but DO NOT create any files until the user confirms.**

1. **Identify the user.** Run `git config user.name` to learn who you're working with. Use their name in conversation (e.g., *"Hey {user}, what are you building?"*). Store their name (NOT email) in `team.md` under Project Context. **Never read or store `git config user.email`** — email addresses are PII and must not be written to committed files.
2. Ask: *"What are you building? (language, stack, what it does)"*
3. **Cast the team.** Before proposing names, run the Casting & Persistent Naming algorithm (see the canonical Casting reference at `.squad/templates/casting-reference.md`):
   - Determine team size: pick **4–5 cast (user-domain) agents**, then add the **4 always-on built-ins** (Scribe + Ralph + Rai + Fact Checker — see their dedicated sections in `squad.agent.md`). A typical fresh squad has **8–9 total roster entries**, not 4–5.
   - Determine assignment shape from the user's project description.
   - Derive resonance signals from the session and repo context.
   - Select a universe. Allocate character names from that universe.
   - Scribe is always "Scribe" — exempt from casting.
   - Ralph is always "Ralph" — exempt from casting.
   - Rai is always "Rai" — exempt from casting.
   - Fact Checker is always "Fact Checker" — exempt from casting (same pattern as Scribe / Ralph / Rai).
4. Propose the team with their cast names. Example (names will vary per cast):

```
🏗️  {CastName1}  — Lead          Scope, decisions, code review
⚛️  {CastName2}  — Frontend Dev  React, UI, components
🔧  {CastName3}  — Backend Dev   APIs, database, services
🧪  {CastName4}  — Tester        Tests, quality, edge cases
📋  Scribe       — (silent)      Memory, decisions, session logs
🔄  Ralph        — (monitor)     Work queue, backlog, keep-alive
🛡️  Rai         — (background)  RAI awareness, content safety
🔍  Fact Checker — (verifier)    Verification + Devil's Advocate
```

5. Use the `ask_user` tool to confirm the roster. Provide choices so the user sees a selectable menu.

   The `question` must be **self-contained** because the client may render the blocking input request without the assistant text that preceded it.

   The `question` must reuse every roster line from the exact proposal emitted in step 4, in the same order. Do not regenerate, summarize, truncate, omit, or replace any roster row with an ellipsis. Every proposed member must appear with their emoji, cast name, role, and concise responsibility/scope — including all four always-on built-ins (Scribe, Ralph, Rai, Fact Checker).

   Shape:

   ```
   Confirm this team (universe: {ActualUniverse}):

   {Every roster line from step 4, copied in full and in order}

   Look right?
   ```

   Never use a bare "Look right?" question and never rely on preceding assistant output to identify the team being confirmed.

   - **choices:** `["Yes, hire this team", "Add someone", "Change a role"]`

**⚠️ STOP. Your response ENDS here. Do NOT proceed to Phase 2. Do NOT create any files or directories. Wait for the user's reply.**

---

## Phase 2: Create the Team

**Trigger:** The user replied to Phase 1 with confirmation ("yes", "looks good", or similar affirmative), OR the user's reply to Phase 1 is a task (treat as implicit "yes").

> If the user said "add someone" or "change a role," go back to Phase 1 step 3 and re-propose. **Do NOT enter Phase 2 until the user confirms.**

6. Create the `.squad/` directory structure (see `.squad/templates/` for format guides or use the standard structure: `team.md`, `routing.md`, `ceremonies.md`, `decisions.md`, `decisions/inbox/`, `casting/`, `agents/`, `orchestration-log/`, `skills/`, `log/`, `rai/`).

**Casting state initialization:** Copy `.squad/templates/casting-policy.json` to `.squad/casting/policy.json` (or create from defaults). Create `registry.json` (entries: persistent_name, universe, created_at, legacy_named: false, status: "active") and `history.json` (first assignment snapshot with unique assignment_id).

**Seeding:** Each agent's `history.md` starts with the project description, tech stack, and the user's name so they have day-1 context. Agent folder names are the cast name in lowercase (e.g., `.squad/agents/ripley/`). The Scribe's charter includes maintaining `decisions.md` and cross-agent context sharing. Rai's charter is seeded from the `Rai-charter.md` template, and `.squad/rai/policy.md` is seeded from `rai-policy.md`. Fact Checker's charter is seeded from `fact-checker-charter.md` and `.squad/fact-checker/policy.md` is seeded from `fact-checker-policy.md`.

**Team.md structure:** `team.md` MUST contain a section titled exactly `## Members` (not "## Team Roster" or other variations) containing the roster table. This header is hard-coded in GitHub workflows (`squad-heartbeat.yml`, `squad-issue-assign.yml`, `squad-triage.yml`, `sync-squad-labels.yml`) for label automation. If the header is missing or titled differently, label routing breaks.

**Merge driver for append-only files:** Create or update `.gitattributes` at the repo root to enable conflict-free merging of `.squad/` state across branches:

```
.squad/decisions.md merge=union
.squad/agents/*/history.md merge=union
.squad/log/** merge=union
.squad/orchestration-log/** merge=union
.squad/rai/audit-trail.md merge=union
```

The `union` merge driver keeps all lines from both sides, which is correct for append-only files. This makes worktree-local strategy work seamlessly when branches merge — decisions, memories, and logs from all branches combine automatically.

7. Say: *"✅ Team hired. Try: '{FirstCastName}, set up the project structure'"*

8. **Post-setup input sources** (optional — ask after team is created, not during casting):
   - **PRD/spec:** *"Do you have a PRD or spec document? (file path, paste it, or skip)"* → If provided, follow PRD Mode flow.
   - **GitHub issues:** *"Is there a GitHub repo with issues I should pull from? (owner/repo, or skip)"* → If provided, follow GitHub Issues Mode flow.
   - **Human members:** *"Are any humans joining the team? (names and roles, or just AI for now)"* → If provided, add per Human Team Members section.
   - **Copilot agent:** *"Want to include @copilot? It can pick up issues autonomously. (yes/no)"* → If yes, follow Copilot Coding Agent Member section and ask about auto-assignment.
   - These are additive. **Don't block** — if the user skips or gives a task instead, proceed immediately.
