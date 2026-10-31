// @ts-check
"use strict";

// Synthetic paired scenarios shared by mocked runtime tests and optional live evaluation.
const eventFixtures = [
  {
    name: "actionable issue",
    category: "actionability",
    context: {
      eventName: "issues",
      payload: {
        action: "opened",
        issue: { number: 12, title: "Crash when loading configuration", body: "The parser crashes on an empty config. Reproduce with gh aw compile and an empty workflow file.", labels: [{ name: "bug" }], state: "open" },
      },
    },
    question: "Run for actionable bug reports with a specific problem to investigate; skip informational announcements.",
    expected: "run",
  },
  {
    name: "informational issue",
    category: "actionability",
    context: {
      eventName: "issues",
      payload: { action: "opened", issue: { number: 13, title: "Release announcement", body: "Version 2 is available. This is informational only; no bug or requested change.", labels: [{ name: "announcement" }], state: "open" } },
    },
    question: "Run for actionable bug reports with a specific problem to investigate; skip informational announcements.",
    expected: "skip",
  },
  {
    name: "relevant issue",
    category: "relevance",
    context: {
      eventName: "issues",
      payload: { action: "opened", issue: { number: 14, title: "gh-aw compiler rejects a valid trigger", body: "gh aw compile rejects a valid on.decision question in this repository.", labels: [{ name: "bug" }] } },
    },
    question: "Run for issues about gh-aw workflow compilation; skip reports exclusively about unrelated software.",
    expected: "run",
  },
  {
    name: "irrelevant issue",
    category: "relevance",
    context: {
      eventName: "issues",
      payload: {
        action: "opened",
        issue: { number: 15, title: "Spreadsheet export in an unrelated application", body: "My standalone spreadsheet application cannot export CSV. This does not involve gh-aw, GitHub Actions, or this repository." },
      },
    },
    question: "Run for issues about gh-aw workflow compilation; skip reports exclusively about unrelated software.",
    expected: "skip",
  },
  {
    name: "actionable PR comment",
    category: "pr-comments",
    context: {
      eventName: "pull_request_review_comment",
      payload: {
        action: "created",
        pull_request: { number: 16, title: "Add decision question validation", body: "Validate resolved workflow questions.", draft: false },
        comment: { body: "Please handle empty resolved questions as fail-open and add a regression test for that case." },
      },
    },
    question: "Run for PR comments requesting a concrete code or test change; skip conversational acknowledgements.",
    expected: "run",
  },
  {
    name: "conversational PR comment",
    category: "pr-comments",
    context: {
      eventName: "pull_request_review_comment",
      payload: { action: "created", pull_request: { number: 17, title: "Add decision question validation", body: "Validate resolved workflow questions.", draft: false }, comment: { body: "Thanks for the update! This looks great to me." } },
    },
    question: "Run for PR comments requesting a concrete code or test change; skip conversational acknowledgements.",
    expected: "skip",
  },
  {
    name: "documentation request",
    category: "documentation",
    context: {
      eventName: "issues",
      payload: {
        action: "opened",
        issue: { number: 18, title: "Document the existing question limit", body: "Please add the existing eight-question limit to the trigger reference. No implementation changes are requested.", labels: [{ name: "documentation" }] },
      },
    },
    question: "Run only for documentation-only requests; skip requests that require implementing product behavior.",
    expected: "run",
  },
  {
    name: "implementation request",
    category: "documentation",
    context: {
      eventName: "issues",
      payload: { action: "opened", issue: { number: 19, title: "Implement a new scheduling feature", body: "Please implement a new runtime scheduler with per-repository queues. This requires product code changes, not documentation." } },
    },
    question: "Run only for documentation-only requests; skip requests that require implementing product behavior.",
    expected: "skip",
  },
  {
    name: "community discussion needs intervention",
    category: "community-intervention",
    context: {
      eventName: "discussion_comment",
      payload: {
        action: "created",
        discussion: { number: 20, title: "Design discussion", body: "Discussing proposed changes.", category: { name: "Ideas" } },
        comment: { body: "Moderator assistance is needed: repeated personal attacks are escalating and participants have asked for intervention." },
      },
    },
    question: "Run when a community discussion explicitly needs moderator intervention; skip respectful technical discussion.",
    expected: "run",
  },
  {
    name: "community discussion needs no intervention",
    category: "community-intervention",
    context: {
      eventName: "discussion_comment",
      payload: {
        action: "created",
        discussion: { number: 21, title: "Design discussion", body: "Discussing proposed changes.", category: { name: "Ideas" } },
        comment: { body: "Thanks for explaining the tradeoff. I respectfully prefer the smaller interface; could we compare the two designs?" },
      },
    },
    question: "Run when a community discussion explicitly needs moderator intervention; skip respectful technical discussion.",
    expected: "skip",
  },
  {
    name: "suitable automated work",
    category: "automation-suitability",
    context: {
      eventName: "issues",
      payload: {
        action: "opened",
        issue: { number: 22, title: "Fix a broken internal documentation link", body: "Update docs/reference.md so its link points to the existing docs/triggers.md page. This is a scoped repository edit needing no external accounts." },
      },
    },
    question: "Run for scoped repository work suitable for an automated agent; skip work requiring private human accounts, purchases, or in-person actions.",
    expected: "run",
  },
  {
    name: "unsuitable automated work",
    category: "automation-suitability",
    context: {
      eventName: "issues",
      payload: {
        action: "opened",
        issue: {
          number: 23,
          title: "Purchase a license using a personal account",
          body: "A human must use their private billing account to purchase a vendor license and complete in-person identity verification. No repository changes are requested.",
        },
      },
    },
    question: "Run for scoped repository work suitable for an automated agent; skip work requiring private human accounts, purchases, or in-person actions.",
    expected: "skip",
  },
];

module.exports = { eventFixtures };
