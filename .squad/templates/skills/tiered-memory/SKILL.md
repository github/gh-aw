---
name: tiered-memory
description: Three-tier agent memory model (hot/cold/wiki) for context reduction per spawn
domain: memory-management, performance
confidence: design (runtime not yet implemented)
source: design proposal
---

# Skill: Tiered Agent Memory

> **Status (v0.10.0):** This skill describes a **design proposal**, not a shipped runtime. Skill files install via `squad init`/`upgrade`, but the underlying tier scaffolding (`.squad/memory/hot/`, `cold/`, `wiki/`), Scribe promotion logic, and spawn-template tier-aware reads are tracked in [github/squad#1264](https://github.com/github/squad/issues/1264). Until those land, agents continue to load full `history.md` + `decisions.md` on every spawn.

## Overview

Squad agents today load their full context history on every spawn, which grows unboundedly across sessions. The Tiered Agent Memory model proposes a three-tier separation so agents only load the bytes that are actually relevant to the current task, with older context kept available on demand.

---

## Memory Tiers

### 🔥 Hot Tier — Current Session Context
- **Size target:** keep small (~2–4KB typical)
- **Load policy:** Always loaded. Every spawn includes hot memory by default.
- **Contents:** Current task description, active decisions made this session, immediate blockers, last 3–5 actions taken, who you are talking to right now.
- **Lifetime:** Current session only. Discarded after session ends (Scribe promotes relevant parts to Cold).
- **Purpose:** Provide immediate task context without any latency or load decision.

### ❄️ Cold Tier — Summarized Cross-Session History
- **Size target:** larger summary, not full transcript (~8–12KB typical)
- **Load policy:** Load on demand. Include only when the task explicitly needs history.
- **Contents:** Summarized past sessions (compressed by Scribe), cross-session decisions, recurring patterns, unresolved issues from prior work.
- **Lifetime:** Rolling window (default proposal: 30 days). Eligible entries are then promoted to Wiki.
- **Purpose:** Answer "what have we tried before?" and "what was decided?" without replaying full transcripts.
- **How to include:** Pass `--include-cold` in spawn template or add `## Cold Memory` section.

### 📚 Wiki Tier — Durable Structured Knowledge
- **Size target:** variable, structured reference docs
- **Load policy:** Async write, selective read. Load only when task requires domain knowledge.
- **Contents:** Architecture decisions (ADRs), agent charters, routing rules, stable conventions, external API contracts, known platform constraints.
- **Lifetime:** Permanent until explicitly deprecated.
- **Purpose:** Authoritative reference. Not history — structured facts.
- **How to include:** Pass `--include-wiki` or reference specific wiki doc paths in spawn template.

---

## When to Load Each Tier

| Situation | Hot | Cold | Wiki |
|-----------|-----|------|------|
| New task, no prior context needed | ✅ | ❌ | ❌ |
| Resuming interrupted work | ✅ | ✅ | ❌ |
| Debugging a recurring issue | ✅ | ✅ | ❌ |
| Implementing against a spec/ADR | ✅ | ❌ | ✅ |
| Onboarding to unfamiliar subsystem | ✅ | ❌ | ✅ |
| Post-incident review | ✅ | ✅ | ✅ |

---

## Spawn Template Pattern

The default spawn prompt should include **Hot tier only**:

```
## Memory Context

### Hot (current session)
{hot_context}
```

Add `--include-cold` when the task needs history:
```
## Memory Context

### Hot (current session)
{hot_context}

### Cold (summarized history — load on demand)
See: .squad/memory/cold/{agent-name}.md
```

Add `--include-wiki` when the task needs domain knowledge:
```
## Memory Context

### Hot (current session)
{hot_context}

### Wiki (durable reference)
See: .squad/memory/wiki/{topic}.md
```

---

## Integration with Scribe Agent (design — not yet implemented)

Scribe is the proposed memory coordinator for this system. Once the runtime lands, Scribe will:

1. **End of session:** Compress Hot → Cold summary (target: ~10% of session verbosity)
2. **Aged cold entries:** Promote Cold → Wiki for decisions/facts that aged into stable knowledge
3. **On-demand wiki writes:** Any agent can request Scribe to write a wiki entry mid-session

Until then, see the Scribe charter for current behavior: `.squad/agents/scribe/charter.md`

---

## Implementation Checklist (tracked in #1264)

- [ ] Scribe writes Hot context file at session start (`.squad/memory/hot/{agent}.md`)
- [ ] Scribe compresses and writes Cold summary at session end
- [ ] Spawn templates default to Hot-only
- [ ] Coordinators add `--include-cold` / `--include-wiki` flags as needed
- [ ] Wiki entries stored in `.squad/memory/wiki/`
- [ ] Cold entries stored in `.squad/memory/cold/` with rolling TTL

---

## References

- Tracking issue: [github/squad#1264](https://github.com/github/squad/issues/1264) — installation gap + runtime status
- Original design spike: [github/squad#686](https://github.com/github/squad/issues/686) — tiered memory implementation plan
- Related: [github/squad#600](https://github.com/github/squad/issues/600) — context payload growth

---

## Spawn Template

# Spawn Template: Agent with Tiered Memory

Use this template when spawning any Squad agent. By default it loads **Hot tier only**. Add optional sections as needed.

---

## Task

{task_description}

## WHY

{why_this_matters}

## Success Criteria

- [ ] {criterion_1}
- [ ] {criterion_2}

---

## Memory Context

### 🔥 Hot (always included)

> Paste current session context here (~2–4KB target):

```
Current task: {task_description}
Active decisions: {decisions_this_session}
Last actions: {last_3_to_5_actions}
Blockers: {current_blockers_or_none}
Talking to: {current_interlocutor}
```

---

### ❄️ Cold (include when task needs history — add `--include-cold`)

> Load on demand. Do not inline unless specifically needed.

Summarized cross-session history is at:
`.squad/memory/cold/{agent-name}.md`

Include when:
- Resuming interrupted work
- Debugging a recurring issue
- "What have we tried before?"

**To load cold memory, add this section and fetch the file before spawning:**

```
## Cold Memory Summary
{contents_of_.squad/memory/cold/{agent-name}.md}
```

---

### 📚 Wiki (include when task needs domain knowledge — add `--include-wiki`)

> Load on demand. Reference specific wiki docs by path.

Wiki entries are at: `.squad/memory/wiki/`

Include when:
- Implementing against an ADR or spec
- Onboarding to unfamiliar subsystem
- Need stable conventions or API contracts

**To load wiki, add this section and reference the specific doc:**

```
## Wiki Reference
{contents_of_.squad/memory/wiki/{topic}.md}
```

---

## Escalation

If blocked or uncertain:
- Architecture questions → @picard
- Security concerns → @worf
- Infrastructure/deployment → @belanna
- Memory/history questions → @scribe

---

## Notes

- Hot tier is always included; keep it focused
- Cold adds a summary; only include when history is relevant
- Wiki adds variable size; only include specific relevant docs
- Runtime backing is tracked in [github/squad#1264](https://github.com/github/squad/issues/1264) — until those changes land, this skill is design-only and agents continue to load full history.md + decisions.md on every spawn
