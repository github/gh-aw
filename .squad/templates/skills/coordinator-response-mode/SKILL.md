---
name: "coordinator-response-mode"
description: "Selecting WHO handles work is the Routing table; selecting HOW they handle it (Direct, Lightweight, Standard, Full) is Response Mode. This skill contains the complete decision table, exemplar prompts for each mode, the Lightweight spawn template, and the upgrade rules. Squad coordinator loads this on demand once routing has identified the agent — to pick the right ceremony level for the task."
allowedTools: []
confidence: high
domain: squad-internals
source: "Extracted from squad.agent.md as part of the slimming effort (github/squad#1308). Behaviour unchanged — coordinator loads this satellite skill after Routing, before spawn."
---

> **Load this skill when:** you have routed work to an agent and need to pick the response mode (Direct / Lightweight / Standard / Full). The 1-line stub in `squad.agent.md` is for awareness; this skill is the full decision table + templates.

## Response Mode Selection

After routing determines WHO handles work, select the response MODE based on task complexity. **Bias toward upgrading** — when uncertain, go one tier higher rather than risk under-serving.

| Mode | When | How | Target |
|------|------|-----|--------|
| **Direct** | Status checks, factual questions the coordinator already knows, simple answers from context | Coordinator answers directly — NO agent spawn | ~2-3s |
| **Lightweight** | Single-file edits, small fixes, follow-ups, simple scoped read-only queries | Spawn ONE agent with minimal prompt (see Lightweight Spawn Template below). Use `agent_type: "explore"` for read-only queries | ~8-12s |
| **Standard** | Normal tasks, single-agent work requiring full context | Spawn one agent with full ceremony — charter inline, history read, decisions read. This is the current default | ~25-35s |
| **Full** | Multi-agent work, complex tasks touching 3+ concerns, "Team" requests | Parallel fan-out, full ceremony, Scribe included | ~40-60s |

## Direct Mode exemplars

Coordinator answers instantly, no spawn:

- *"Where are we?"* → Summarize current state from context: branch, recent work, what the team's been doing. A user favorite — make it instant.
- *"How many tests do we have?"* → Run a quick command, answer directly.
- *"What branch are we on?"* → `git branch --show-current`, answer directly.
- *"Who's on the team?"* → Answer from `team.md` already in context.
- *"What did we decide about X?"* → Answer from `decisions.md` already in context.

## Lightweight Mode exemplars

One agent, minimal prompt:

- *"Fix the typo in README"* → Spawn one agent, no charter, no history read.
- *"Add a comment to line 42"* → Small scoped edit, minimal context needed.
- *"What does this function do?"* → `agent_type: "explore"` (Haiku model, fast).
- Follow-up edits after a Standard/Full response — context is fresh, skip ceremony.

## Standard Mode exemplars

One agent, full ceremony:

- *"{AgentName}, add error handling to the export function"*
- *"{AgentName}, review the prompt structure"*
- Any task requiring architectural judgment or multi-file awareness.

## Full Mode exemplars

Multi-agent, parallel fan-out:

- *"Team, build the login page"*
- *"Add OAuth support"*
- Any request that touches 3+ agent domains.

## Mode upgrade rules

- If a Lightweight task turns out to need history or decisions context → treat as Standard.
- If uncertain between Direct and Lightweight → choose Lightweight.
- If uncertain between Lightweight and Standard → choose Standard.
- **Never downgrade mid-task.** If you started Standard, finish Standard.

## Lightweight Spawn Template

Skip charter, history, and decisions reads — just the task:

```
agent_type: "general-purpose"
model: "{resolved_model}"
mode: "background"
name: "{name}"
description: "{emoji} {Name}: {brief task summary}"
prompt: |
  You are {Name}, the {Role} on this project.
  TEAM ROOT: {team_root}
  CURRENT_DATETIME: <resolved CURRENT_DATETIME literal>
  WORKTREE_PATH: {worktree_path}
  WORKTREE_MODE: {true|false}
  **Requested by:** {current user name}
  
  {% if WORKTREE_MODE %}
  **WORKTREE:** Working in `{WORKTREE_PATH}`. All operations relative to this path. Do NOT switch branches.
  {% endif %}

  TASK: {specific task description}
  TARGET FILE(S): {exact file path(s)}

  Do the work. Keep it focused.
  If you made a meaningful decision, persist it with `memory.write` (class: `decision`) when available, or fall back to `squad_decide` / `squad_state_write` to `decisions/inbox/{name}-{brief-slug}.md`. Do not run git notes, switch branches, or write mutable `.squad/` state by hand.

  ⚠️ OUTPUT: Report outcomes in human terms. Never expose tool internals or SQL.
  ⚠️ RESPONSE ORDER: After ALL tool calls, write a plain text summary as FINAL output.
```

For **read-only queries**, use the explore agent: `agent_type: "explore"` with `"You are {Name}, the {Role}. CURRENT_DATETIME: <resolved CURRENT_DATETIME literal> — {question} TEAM ROOT: {team_root}"`
