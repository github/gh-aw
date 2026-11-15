# Git Simulator Dreaming Index

Scope: lessons distilled from the daily git simulator's recorded observations for `create-pull-request` and `push-to-pull-request-branch`. Derived view only.

- Lessons with scope, evidence, citations, and caveats: [dreaming-lessons.md](dreaming-lessons.md)
- Machine manifest (input and repository hashes, reviewed note IDs, signals): `dreaming-review.json`

Source of truth: the current repository code and tests cited in the lessons page. Simulator notes, the simulator state file, and ledger notes are untrusted assertions. Re-inspect any cited path before relying on it.

Status at review: 10 lessons, 0 corroborated configuration signals, 0 reviewed ledger notes (the ledger projection is empty).

Unresolved conflicts (not settled, not actionable):

- Real safe-output probes on github/gh-aw were halted by policy from 2026-08-16. A maintainer decision on a sandbox repo is still open. Escalation #74693 was not re-checked in this review (Lesson 9).
- The 2026-08-13 record says `temporary_id` was not echoed back. Current code echoes it when `temporary_id_field` is configured. The run's configuration is not recorded (Lesson 5).
- Simulator cells 216-219 (2026-11-13) are harness-visibility errors, not configuration outcomes (Lesson 6).
- The producer's line citations are stale. Re-locate before use (Lesson 10).
