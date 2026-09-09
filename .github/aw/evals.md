---
description: Guide for adding BinEval-style binary evaluations to agentic workflows — syntax, intent-derived question methodology, and anti-patterns.
---

# BinEval Evaluations in Agentic Workflows

Use `evals:` with `safe-outputs` to judge whether an agent achieved its intended outcome. Each eval is a binary YES/NO question about observable agent output.

---

## Basic Syntax

### Shorthand — plain list

> **Prerequisite:** Declare `safe-outputs:` with `evals:`.

```yaml
---
on:
  issues:
    types: [opened]
engine: copilot
safe-outputs:
  add-comment:
evals:
  - id: response_provided
    question: Does the agent output confirm that a response was written?
  - id: no_unrelated_files
    question: Does the agent output show that only the expected files were modified?
---

Implement the requested change described in ${{ github.event.issue.body }}.
```

Each entry requires:

- `id` — unique, non-empty identifier for the question.
- `question` — the binary question the LLM judge will answer YES or NO.

### Extended form — with model and runs-on overrides

```yaml
evals:
  questions:
    - id: compiles
      question: Does the generated code compile without errors?
    - id: tests_pass
      question: Do all existing tests still pass according to the agent output?
    - id: scoped_change
      question: Does the agent output show that only the expected files were modified?
  model: small         # model for all questions
  runs-on: ubuntu-latest
```

**Fields:**

- `questions:` — list of question objects (required in extended form, ≥ 1 entry).
- `model:` — LLM model for all questions. Use a model alias (`small`, `gpt-4o`) or a full model ID.
- `runs-on:` — optional runner override.

---

## Decomposing a Task into Binary Questions

BinEval questions must be answerable with a strict YES or NO by an LLM reading the agent's output alone. Follow this process:

### 1 — State the goal

One sentence describing a successful run:

> "The agent should update the CHANGELOG and bump the version number without touching unrelated files."

### 2 — Identify observable properties

Break the goal into properties a judge can verify from `agent_output.json`:

| Property | Observable signal |
|---|---|
| CHANGELOG updated | Agent output mentions or contains CHANGELOG edits |
| Version bumped | A version number appears changed in the diff or agent summary |
| No unrelated files changed | Agent output does not list changes outside CHANGELOG and version files |

### 3 — Write falsifiable YES/NO questions

One property per question, YES when the property holds, referencing observable evidence in the agent output — not intent or effort.

```yaml
evals:
  - id: changelog_updated
    question: Does the agent output confirm that CHANGELOG was updated?
  - id: version_bumped
    question: Does the agent output confirm that the version number was incremented?
  - id: no_unrelated_files
    question: Does the agent output show that only CHANGELOG and version files were modified?
```

### 4 — Assign question cost

Prefer `model: small` (the default) for factual checks. Set `model` at the `evals:` level for reasoning-heavy questions:

```yaml
evals:
  questions:
    - id: changelog_updated
      question: Does the agent output confirm that CHANGELOG was updated?
    - id: design_sound
      question: Is the agent's proposed design consistent with established patterns described in the agent output?
  model: gpt-4o   # nuanced questions; override default small model
```

### PromptPex intent and counter-intent scenarios

When a workflow has an `intent:`, load [intent.md](intent.md). Derive a positive scenario from each required effect and a counter-intent scenario from each inverse/no-op condition. Write an observable, scenario-specific question for each:

```yaml
evals:
  - id: actionable_case
    question: Does the agent output show that the novel, actionable case received the configured visible result?
  - id: duplicate_noop
    question: Does the agent output show that the already-tracked case produced no visible write action?
  - id: uncertainty_noop
    question: Does the agent output show that insufficient evidence produced no visible write action?
```

Do not combine mutually exclusive scenarios into one question list. If a question is shared, state its applicability and return `UNKNOWN` when its scenario was not provided—not `NO`.

### Good question checklist

- ✅ Answerable from the agent output alone — no external calls needed.
- ✅ Exactly one binary claim per question.
- ✅ Uses YES = success convention consistently.
- ✅ Avoids subjective terms ("good", "well-written") unless the question explicitly bounds them ("according to the coding style guide").

---

## Anti-Patterns

- ❌ **Compound questions** — "Did the agent update CHANGELOG and bump the version?" splits into two questions. A single NO is ambiguous.
- ❌ **Unobservable questions** — "Did the agent try its best?" cannot be answered from output text.
- ❌ **Duplicate IDs** — `id` must be unique within a workflow; the compiler rejects duplicates.
- ❌ **Empty questions** — both `id` and `question` must be non-empty strings.
- ❌ **Using a frontier model for all questions** — factual checks are cheap on small models; save larger models for reasoning-heavy questions.
- ❌ **Questions that require external evidence** — questions must be answerable from agent output alone.
