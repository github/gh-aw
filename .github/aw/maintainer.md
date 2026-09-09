---
description: Guidance for designing bounded, stateful repository maintenance workflows that triage backlogs, improve code, and report to maintainers.
---

# Repository Maintenance Workflows

Use this guidance when building an agentic workflow that performs recurring repository maintenance. It distills the operating model of [Repo Assist](https://github.com/githubnext/agentics/blob/main/workflows/repo-assist.md) into reusable gh-aw design principles rather than prescribing one repository's labels, thresholds, or task weights.

## Operating principles

A maintenance workflow should make useful progress without consuming disproportionate maintainer attention.

- Prefer a small, valuable action over broad cleanup.
- Stay silent on an individual issue or pull request unless there is something accurate and actionable to add.
- Treat a whole-run `noop` as exceptional: inspect the bounded work queue and recorded follow-ups before concluding that no useful work exists.
- Never merge the workflow's own pull requests. Leave acceptance to maintainers.
- Read `AGENTS.md`, `CONTRIBUTING.md`, and repository-specific instructions before changing files.
- Preserve public APIs and avoid new dependencies unless a maintainer has approved the change in a tracked discussion.
- Create small draft pull requests with one concern, clear rationale, and validation results.
- Identify automated output consistently and interact with contributors politely.

These rules deliberately balance bias toward progress with quality over quantity. The workflow should search systematically for useful work, but it should not manufacture comments or changes to prove activity.

## Survey the repository before choosing a strategy

Do not begin maintenance workflow design from a generic task portfolio. First inspect the target repository so the initial strategy reflects its technology, activity, backlog, and maintainer practices. For broad automation requests, use this survey to mine candidate intents; for an explicit, narrow request, skip the survey unless repository evidence can materially disambiguate the outcome.

Build a bounded baseline from:

| Area | Signals to inspect |
|---|---|
| Project shape | Languages, manifests, generated files, monorepo boundaries, package layout, build systems, and deployment targets |
| Repository policy | `AGENTS.md`, `CONTRIBUTING.md`, `CODEOWNERS`, pull request templates, protected paths, release practices, and existing automation |
| Activity pattern | Commits, releases, issue and pull request arrival/closure rates, contributor activity, and bot-generated activity over a stated window |
| Issue state | Open count, age distribution, unlabelled and stale items, milestones, response status, common categories, and duplicate signals |
| Pull request state | Open count, age, reviews, failed checks, merge conflicts, abandoned work, and workflow-owned versus contributor-owned branches |
| Operational health | Format/lint/build/test commands, CI reliability, dependency update volume, flaky tests, release cadence, and recurring failures |

Use deterministic GitHub queries and repository inspection for this survey. Bound every query, state the observation window and limits, and mark unavailable data instead of guessing. Distinguish observations from recommendations in the design summary.

Derive evidence-backed candidate intents before the first portfolio. For each candidate, record the concise outcome, observed evidence, feasibility, expected value, risk, and uncertainties; do not persist this analysis in workflow frontmatter. Select and augment an intent using [intent.md](intent.md), then derive the first portfolio from it. For example:

- a large unlabelled issue backlog favors bounded classification before code changes
- many unanswered but active issues favors investigation and substantive responses
- unhealthy workflow-owned pull requests favors self-maintenance before creating more
- high contributor pull request volume favors review support and conservative stale-follow-up rules
- low activity or sparse tests favors low cadence, documentation, test discovery, and maintainer reports
- frequent releases or dependency churn favors release, dependency, and CI health tasks

Recommend two or three low-risk task families, a conservative cadence, per-run limits, state requirements, and pressure valves. Ask maintainers only for policy choices that repository data cannot establish, such as acceptable attention cost, protected areas, and whether contributor-facing comments are appropriate.

## Separate invocation modes

Support two distinct modes when both autonomous and requested maintenance are needed:

1. **Scheduled mode** selects bounded work from repository signals and follows the recurring maintenance loop.
2. **Command mode** follows the sanitized slash-command or `workflow_dispatch` instruction exclusively, while retaining the same safety, validation, and disclosure rules.

Do not run scheduled tasks or recurring reporting after completing command mode. Keep the branches explicit in the prompt so user instructions cannot accidentally expand an autonomous run.

## Use a deterministic control plane

Collect and reduce repository state in deterministic `steps:` before invoking the agent. Typical signals include:

- open and unlabelled issue counts
- open pull request counts, separated into workflow-owned and contributor pull requests
- stale items, failed checks, or merge conflicts
- age and last-human-activity timestamps
- previously recorded cursors and incomplete work

Write a compact JSON payload in a run-scoped artifact directory such as `/tmp/gh-aw/agent/`, containing the signals, candidate task weights, and selected task identifiers. Ask the agent to read that file rather than rediscovering the entire backlog.

Choose a small number of distinct tasks per run. Weighted selection can adapt the mix:

- increase labelling weight with the unlabelled backlog
- increase investigation and fixing weight with the issue backlog
- enable self-maintenance only when workflow-owned pull requests exist
- increase contributor follow-up weight with eligible stale pull requests
- retain a baseline chance for testing, performance, engineering, and roadmap work

Seed probabilistic selection with the workflow run ID so a run is reproducible for debugging. Record all inputs, computed weights, selected tasks, and substitutions in logs. Define a fallback for every task that may be inapplicable.

This hybrid model keeps collection, prioritization, limits, and bookkeeping deterministic while reserving agent judgment for classification, investigation, implementation, and communication.

## Define a maintenance portfolio

Tailor the portfolio to the repository. A broad assistant can draw from these task families:

| Task family | Expected behavior | Important bound |
|---|---|---|
| Label and triage | Apply only existing, allowlisted labels with high confidence; remove clearly incorrect labels | Cursor through untriaged items |
| Investigate and respond | Work oldest-first, prioritizing items without a useful prior response | Comment only with substantive findings |
| Fix actionable issues | Implement a minimal fix and add a regression test when practical | Skip duplicate or uncertain attempts |
| Engineering investment | Improve dependencies, CI, tooling, SDKs, or build configuration | Require clear benefit and validation |
| Code and documentation quality | Remove dead code, reduce duplication, clarify APIs, or close documentation gaps | Select only obvious, low-risk improvements |
| Maintain owned pull requests | Repair failures caused by the workflow and resolve merge conflicts | Push only to branches identified as workflow-owned |
| Follow up on stale pull requests | Offer help when a contributor is blocking progress | Never nudge when maintainers owe the response |
| Performance | Remove measurable waste or improve algorithms, caching, memory, or startup | Benchmark where practical |
| Testing | Add missing behavioral coverage or improve flaky, slow, or brittle tests | Do not optimize for coverage numbers alone |
| Move the repository forward | Continue a feature, difficult investigation, plan, or proposal | Resume recorded work before starting more |
| Maintainer report | Update a durable summary of actions and decisions needed | Report only current, actionable information |

Specify applicability tests and fallbacks in a table in the workflow prompt. Avoid making open-ended code improvement the universal fallback; investigation, triage, or `noop` is safer when no clearly beneficial change exists.

## Make recurring work systematic

Use persistent memory for continuity, not as a source of truth. Store only the minimum state needed to avoid duplicate work and resume fairly:

- issue and pull request cursors
- automated comments with timestamps and the latest observed human activity
- fix attempts, created outputs, and outcomes
- stale pull requests already nudged
- in-progress work, blockers, and next steps
- maintainer actions acknowledged or completed

Read memory at the start and update it at the end. Verify every remembered item against live repository state before acting because issues, comments, branches, and checks may have changed. Treat recorded follow-ups as queued work rather than passive notes.

Process bounded queues in a stable order, usually oldest-first, and advance a cursor across runs. Re-engage after new human activity, not after the workflow's own comment. Use explicit deduplication keys such as issue number plus last human comment timestamp.

## Bound activity and attention

Apply limits at several layers:

- Use a pre-activation check to skip scheduled runs when too many workflow-owned pull requests are already open.
- Use workflow concurrency to prevent overlapping maintenance runs.
- Cap selected tasks and candidate items per run.
- Set conservative `max` values on every safe output.
- Limit stale pull request nudges separately and remember each nudge.
- Restrict labels to a repository-specific allowlist.
- Use a stable title prefix or label to identify workflow-owned issues and pull requests.

Provide an explicit `noop` path when no candidate meets the quality bar. Before using it, require checks for unprocessed backlog items, recorded follow-ups, actionable bugs, and unhealthy workflow-owned pull requests.

## Keep writes safe

Keep the agent job read-only and route mutations through safe outputs. Enable only the GitHub toolsets needed for discovery.

Recommended safeguards include:

- create all pull requests as drafts
- constrain `create-pull-request.allowed-files` whenever the task scope is predictable
- use `protected-files: fallback-to-issue` for changes that require maintainer discussion
- constrain branch updates with a workflow-owned title prefix or equivalent identity check
- hide older automated comments where appropriate
- use exact targets and low output caps instead of broad `target: "*"` when possible
- use the highest input integrity compatible with the trigger

Treat public slash-command text, issue bodies, comments, and pull request content as untrusted. Sanitize command input, keep command mode bounded by the configured tools and safe outputs, and do not lower integrity or enable all GitHub toolsets without an explicit reason.

Restrict command mode by repository role when it can perform privileged maintenance. Do not automatically trigger CI for workflow-created pull requests in public repositories unless the abuse risk has been assessed and mitigated.

Infer package network access from repository manifests. Do not copy a broad multi-ecosystem network allowlist into every maintenance workflow.

## Validate every proposed change

Before creating a pull request:

1. inspect repository instructions and contribution requirements
2. implement the smallest focused change
3. format, lint, build, and test with existing project commands
4. add or update tests when behavior changes
5. scan the changed files for secrets and review the diff
6. create a draft pull request that explains the problem, rationale, trade-offs, related issue, and test status

Do not create a pull request when the change causes validation failures. If an unrelated infrastructure failure prevents validation, document the exact limitation rather than claiming success. For performance work, include measurements or clearly state why measurement was not practical.

## Maintain a human-facing summary

Persistent machine state is not a maintainer interface. Maintain a rolling issue or discussion that answers:

1. What needs maintainer attention now?
2. What did the workflow do recently?
3. What work is the workflow continuing next?

Put pending actions first, include direct links, and remove completed or obsolete entries instead of preserving an ever-growing checklist. Keep run history reverse chronological and link each entry to its workflow run. Read maintainer edits and comments before updating the summary so checked-off work and new direction are preserved.

When a run legitimately produces no action after checking all bounded queues and follow-ups, log a `noop` instead of adding a "nothing happened" summary entry.

## Rebuild methodology

Build the workflow in stages:

1. **Survey the repository and mine intents.** Record the project shape, contribution rules, validation commands, protected paths, activity window, issue and pull request health, labels, releases, CI reliability, and existing automation. Keep observed facts separate from evidence-backed candidate intents and strategy recommendations.
2. **Select and augment an intent, then choose the initial portfolio.** Start with two or three low-risk families such as labelling, investigation, and owned-PR maintenance. Add code-writing tasks only after observing output quality.
3. **Define live signals and applicability.** Document how each signal changes priority, when each task is eligible, and its fallback.
4. **Define state and deduplication.** Specify cursors, timestamps, ownership markers, and retention. Avoid storing repository data that can be fetched cheaply.
5. **Configure triggers and pressure valves.** Add a fuzzy schedule, optional manual or slash-command entrypoint, concurrency, and an open-PR guard.
6. **Configure least privilege and safe outputs.** Derive tools, permissions, network access, allowlists, protected files, and per-run caps from the chosen portfolio.
7. **Write the execution contract.** State the selected-task input, quality bar, stop conditions, contributor etiquette, validation policy, reporting format, and memory update requirement.
8. **Compile and inspect generated Actions.** Agents must run one-shot `gh aw compile` commands and must not use `--watch`, because watch mode does not terminate automatically.
9. **Roll out gradually.** Begin with low cadence and conservative caps. Review early runs before increasing scope or frequency.

Do not transplant Repo Assist's exact weights, labels, stale threshold, open-PR ceiling, or output maxima without measuring the target repository. These are policy choices, not universal defaults.

## Evaluate and tune

Review both productivity and attention cost:

- task selections, substitutions, and `noop` reasons
- duplicate-action rate and backlog coverage
- useful comments versus ignored or corrected comments
- pull request acceptance, closure, and rework rates
- validation failures and time to repair owned pull requests
- open workflow-owned pull request pressure
- maintainer actions requested and time outstanding
- token use, runtime, and output volume by task family

Tune deterministic weights, eligibility checks, cadence, and caps before expanding the prompt. Remove task families that consistently create low-value output. Add repository-specific tasks only when they have a measurable outcome and a clear human review path.
