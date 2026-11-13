---
name: prompt-token-efficiency
description: Rewrite prompts for minimal tokens, maximal clarity, and low ambiguity for LLM consumption.
---

# Prompt Token Efficiency

Use when writing or revising prompts, instructions, skills, or their generators.
Optimize agent comprehension and useful information per byte without changing
intent, safety, output contracts, or failure behavior.

## Prefix contract

Copilot CLI prompt-file queries may return only the first **8,000 UTF-8 bytes**.
Count bytes from the start of the file, including frontmatter, whitespace, links,
and multibyte characters; line and character counts are not substitutes.

1. Fit a complete execution contract within **7,500 bytes**, leaving 500 bytes of headroom.
2. Order it: objective/activation, mandatory constraints, task-to-reference routes,
   procedure, verification/output contract, failure/stop conditions.
3. Keep safety gates and required completion steps before examples. Put only
   optional explanations, extended examples, and specialized detail afterward.
4. Link required detail before the cutoff with an explicit condition for loading
   it. Never rely on a later table of contents or on instructions to read a tail
   that the agent cannot discover.
5. Prefer a file under 7,500 bytes. If longer, mark the end of its self-contained
   contract with `<!-- instruction-prefix-end -->` on a separate line by byte
   7,500. Close frontmatter, fences, lists and tables before that marker.
6. Run `python3 scripts/check-instruction-prefixes.py PATH...` in this repository.
   The checker measures structure/bytes, not semantic completeness. Read the
   exact first 8,000 bytes separately and verify every required decision is
   actionable, or routes to a named reference. Report unavailable checks.

For comparison, evaluate the same task against full guidance and its exact
8,000-byte prefix. Record missing rules, routes, outputs and stop conditions;
never accept lower cost or fewer bytes as proof of equivalent behavior.

## Goals

1. Minimize token count
2. Maximize clarity
3. Minimize ambiguity
4. Optimize for LLM execution, not human prose style

## Core Rules

- Keep task-critical information; move optional detail behind explicit routes.
- Remove pleasantries, repetition, and narrative framing.
- Prefer short, concrete instructions over descriptive paragraphs.
- Use explicit constraints and output format requirements.
- Use stable terminology (one term per concept).
- Use short grammatical imperatives, not cryptic fragments or undefined shorthand.
- State actor, condition and action; keep negation and exceptions next to the rule.
- Preserve exact field names, commands, thresholds, compatibility and auth boundaries.
- Replace vague words (`appropriate`, `some`, `better`) with measurable criteria.
- Put required context before optional context.
- Avoid conflicting instructions.

## Prose Compression Pattern

Rewrite prose to be direct and compact:

1. Start with the objective in one short sentence.
2. Keep only facts needed to complete the task.
3. Replace long qualifiers with concrete limits.
4. Remove filler words that do not change behavior.
5. End with explicit success criteria.

## LLM-Optimized Writing Style

- Use imperative statements.
- Prefer bullets over long prose.
- Keep each instruction atomic.
- Avoid examples unless needed to prevent failure.
- If examples are required, include one minimal example.

## Ambiguity Checks

Before finalizing a prompt, verify:

- Any undefined noun is resolved.
- Any pronoun has a clear antecedent.
- Scope limits are explicit (time range, file range, quantity limits).
- Success criteria are testable.
- Output format is unambiguous.

## Prose Rewrite Checks

When rewriting, ensure the final prompt:

- Uses fewer words than the original.
- Packs more actionable guidance into the first 8,000 bytes; record before/after
  total bytes, contract-end byte, and any required detail moved behind a link.
- Preserves all required constraints.
- Uses concrete nouns instead of pronouns where possible.
- Avoids optional wording unless options are actually allowed.
- States required output and any length bound in plain language.
