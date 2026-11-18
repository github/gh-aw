---
name: cracktro-speaker-notes
description: Write concise, research-informed cracktro speaker notes for technical audiences new to GitHub Agentic Workflows.
---

# Cracktro speaker notes

Use when adding or revising speaker notes. First load
`../cracktro/SKILL.md` and its `content-rules.md`; those public-content,
provenance and experimental-feature rules still apply.

## Audience and tone

- Address experienced software practitioners who are beginners in GitHub Agentic
  Workflows (AW), not members of its team. Assume no knowledge of internal names.
- Write calm, conversational speaking cues, not a script, sales pitch or changelog.
  Prefer concrete verbs, short sentences and “you” to impersonal exposition.
- Explain an unfamiliar acronym or mechanism locally. Distinguish `gh aw`
  (workflow authoring/compiler) from Copilot (one possible agent runtime).
- Aim for 55–95 words per slide. This is an editorial target, not a researched
  threshold. Allow extra words when evidence or safety caveats would otherwise
  be lost; never remove a limitation merely to hit the target.

## Required structure

Keep the slide's existing `notes` string. Use three newline-separated cues with
these literal labels, in order:

1. **What:** Orient the listener to the visible diagram, code or chart. Name the
   important actors and direction of flow, not every label.
2. **Why:** State the problem this feature or pattern solves and the relevant
   boundary or trade-off. Do not promise safety, savings or causal impact that
   the slide cannot support.
3. **When:** Give one concrete situation in which the audience should use it,
   or a decision it informs. For section maps, explain how to choose a topic;
   for measured examples, describe the reusable pattern, not a universal result.

Complement the visual rather than reading it aloud. Preserve synthetic or
illustrative labels, measurement windows, attribution caveats, prototype status,
experimental status and security boundaries. Distinguish measured results from
animated examples; observed association is not sole causation.

## Evidence behind the style

These are applications of multimedia-learning research to spoken delivery,
not evidence that this exact notes format improves learning:

- **Coherence:** exclude irrelevant detail. **Signaling:** cue the structure
  and important visual relationships. **Redundancy:** explain the graphic
  instead of reciting all its displayed text. See Mayer, “Principles for Reducing
  Extraneous Processing,” *The Cambridge Handbook of Multimedia Learning*
  (2nd ed., 2014),
  [chapter 12](https://www.cambridge.org/core/books/cambridge-handbook-of-multimedia-learning/principles-for-reducing-extraneous-processing-in-multimedia-learning-coherence-signaling-redundancy-spatial-contiguity-and-temporal-contiguity-principles/CD5B7AE1279A9AB81F8EEBB53DBEC86E).
- **Personalization:** prefer conversational to formal wording. Effects have
  boundary conditions, including learner expertise and lesson length; do not
  claim a universal benefit. See Mayer, “Principles Based on Social Cues,”
  same handbook (2014),
  [chapter 14](https://www.cambridge.org/core/books/cambridge-handbook-of-multimedia-learning/principles-based-on-social-cues-in-multimedia-learning-personalization-voice-image-and-embodiment-principles/3841340D8AD820C26DBCD39AE664BCEC).

## Authoring and verification

1. Find the stable slide Id and read its summary in `../cracktro/slides.md`.
   Inspect only that slide's block in `docs/public/cracktro/deck.js`.
2. Read the current notes and matching content-rule exception before drafting.
   Keep the slide's message and factual provenance; do not invent evidence.
3. For a requested council, have two models draft and challenge the wording.
   Honor the requested vendors/models; reconcile disagreements against the
   slide, implementation and public-content rules, not majority vote.
4. Read the cues aloud: can a newcomer explain what they see, why it matters
   and when to use it without team context? Check each cue against the visual.
5. Follow the cracktro skill's syntax, browser and public-content checks.
   Verify toolbar/N-key toggling, current-slide updates and mobile scrolling.
   Keep research URLs in this skill, never in the public notes.
