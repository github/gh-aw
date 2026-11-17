# Cracktro slide list

The single ordered list of slides. A slide's number is its row position here (first row = slide 1, `#1`); never write slide numbers anywhere else. Refer to a slide by its heading (and CSS prefix); link previews as `#N` with N looked up here.

Columns (one table, one row per line; `|` is not allowed inside a cell):

- **Heading**: the slide's first heading text, exactly as rendered.
- **Notes**: empty, or `illustrative` when the speaker notes must match `/illustrative|synthetic|simulated/i`.
- **Summary**: one public sentence with the slide's executive takeaway. Start band headers with `Band header (<band>):`. Describe the durable point, not measured counts or dates that age. No URLs, handles or slide numbers.

`docs/src/lib/cracktro/slides.js` parses this file and fails on a malformed row, an empty Heading or Summary, an unknown Notes value or a duplicate. The docs build injects every row into the talk page's JSON-LD (`hasPart`: position, name, description, `#N` URL); `docs/tests/talk.spec.ts` checks the deck and that metadata against this list.

Editing: add, remove or move one row per slide in the same change as `deck.js`, and rewrite the Summary whenever the slide's message changes. Do not renumber anything.

| Heading | Notes | Summary |
|---|---|---|
| GitHubAgenticWorkflows |  | Title slide: GitHub Agentic Workflows are coding agents that run in GitHub Actions, written in Markdown, with guardrails. |
| Agentic Workflows |  | Band header (Agentic Workflows): what agentic workflows are, how they are written and why they pay off. |
| CI/CD meetsContinuous AI | illustrative | Agentic steps run inside the GitHub Actions pipelines teams already use, on the same runners, logs and checks. |
| Continuous AI | illustrative | Continuous AI, named after CI/CD, is any automated AI that supports software collaboration, such as documentation, triage, fault analysis and quality, kept auditable, event-triggered and under the team's control. |
| Anatomy of a workflow | illustrative | A workflow is one Markdown file: front matter declares triggers, tools, permissions, steps and safe outputs, and the body is the agent prompt. |
| Turn Intent to Automation |  | Describe a job in plain language, an agent writes the Markdown workflow, and gh aw compile deterministically turns it into a hardened Actions lock file. |
| Turn Automation into Value | illustrative | Evidence comes first: an evidence contract is frozen before results, then repository and lifecycle evidence are collected as inputs; Operational Value is the repository outcome measured in native units at a fixed cadence regardless of actor, while each safe output's delivery, adoption and survival evidence is evaluated only afterwards into attributed contribution, read beside the outcome trend as association, not causation, and never added into it. |
| Define once. | illustrative | Operational value for one workflow is designed once, not counted from runs: a design agent turns the adopted workflow's natural-language intent, precompute steps and safe-output guardrails into candidate repository-native measures, a human confirms the design in a single review, and the skill generates one verified, frozen deterministic value function with exactly one primary native measure plus optional diagnostics that are never added into it. |
| Measure repeatedly. | illustrative | The frozen value function then runs on cadence without an LLM: each pass fixes a window, collects evidence, evaluates the same code and publishes the repository outcome as CAO Value, with diagnostics beside it, a baseline only when evidenced, missing windows never counted as zero and contribution never added, so value means outcomes, not run success. |
| Security |  | Band header (Security): design every workflow as if the agent is already compromised. |
| Agentic attacks | illustrative | Common agent attacks, from prompt injection to secret exfiltration and destructive commands, are each stopped by one or more layered defenses. |
| Safety and control |  | Inputs are filtered, the agent runs sandboxed with a read-only token behind a firewall and gateway, and writes happen only through scanned safe outputs. |
| Integrity filtering | illustrative | Malicious comments on a public pull request never reach the agent: the MCP gateway rates each item by its author's trust and drops everything below the configured minimum integrity before the model sees it. |
| Zero-secret agents | illustrative | The agent runs in an isolated sandbox, here the experimental micro-VM runtime, holding no credentials; every key stays on the host behind the MCP gateway, API proxy and egress firewall. |
| Supply chain hardening | illustrative | The compiler applies layered static checks, including trigger admission, expression safety, pinned actions, least privilege and secret scans, before any workflow runs. |
| Safe outputs | illustrative | Agents only propose writes; threat detection reviews each proposal and a separate, scoped job applies only the declared outputs. |
| Need-to-know enclaves | illustrative | Experimental: single-use, isolated enclave agents read private repositories and return only schema-constrained values, so private data cannot leak into public replies. |
| Continuous Red Teaming | illustrative | A daily red-team agent keeps trying new ways to escape the agent firewall, and each escape it finds is turned into a firewall fix. |
| Stop manually translating intent into code. |  | The factory mindset: engineer processes that create trusted features instead of hand-translating intent into code. |
| Human agent processes |  | Band header (Human agent processes): engineer processes, not code, with patterns that pair people and agents, some with a column of worked examples below. |
| Event-driven Hand-offs | illustrative | Event-driven hand-off patterns such as IssueOps, DailyOps, ReviewOps and LabelOps let agents do the work while people make the decisions. |
| Pull Request Review² | illustrative | Marking a pull request ready starts agentic reviewers, and Copilot is nudged to fix their findings so the human reviewer sees a clean pull request. |
| Ponytail Reviewer | illustrative | A read-only agentic reviewer reads each ready pull request only for unnecessary complexity, posts one-line findings with the lines each saves, and says ship once nothing is left to cut; measured on gh-aw, most ready pull requests are already lean and most findings get acted on. |
| Issue Monster | illustrative | Issue Monster loves cookie issues: agents file their follow-ups into a cookie-labelled issue queue; a scheduler runs cheap deterministic gates and filters every few minutes and only wakes an agent to assign the best few issues to Copilot. |
| Decide · Draft · Record | illustrative | When a ready pull request carries an architecture decision with no record, an agent drafts an ADR that people finalize and merge with the code, and later agents read it to learn why the feature works that way. |
| PR Sous Chef | illustrative | PR Sous Chef regularly finds pull requests that sit idle with work waiting, such as unresolved review comments or failing CI, and nudges Copilot to pick them back up, while humans still review and merge. |
| Issues as intent | illustrative | Contributors run a debug agent on their own failures and file anonymized issues that state intent, which maintainers plan and assign to Copilot. |
| Daily Improvers | illustrative | Scheduled research agents find problems such as duplicated code, assign them to Copilot, and developers refine the resulting pull requests. |
| Repo Assist | illustrative | A scheduled assistant picks maintenance tasks such as triage and stale pull requests, a maintainer reviews every result, and backlogs shrink. |
| Research · Assign · Observe | illustrative | A daily benchmark agent files performance regressions, Copilot fixes them, and the next run observes the result to close the loop. |
| Hunt · Falsify · Fix | illustrative | A daily agent hunts for vulnerabilities in deterministically scoped, rotating files, discards every lead it can falsify, and files only the survivors as issues that Copilot fixes. |
| Query · Ask · Grow | illustrative | An expert DevOps agent read live telemetry through observability MCP servers and filed the most important instrumentation gap each day, growing OpenTelemetry support. |
| Semantic clustering | illustrative | Grouping functions by intent rather than by text exposes misplaced code that duplicate detection misses, and Copilot moves it. |
| Go Fan | illustrative | A daily agent compares each Go dependency's releases with how the code uses it and files review issues that Copilot turns into fixes. |
| Merge fast, unslop daily | illustrative | Merge many small pull requests quickly and let agentic reviewers and daily improvers clean up, instead of polishing each one through endless nit rounds. |
| Dreaming | illustrative | Dreaming agents mine the activity between people and agents, such as review threads, sessions and failed runs, to design and optimize agentic workflows, prompts, skills and linters. |
| Mine · Refine · Apply | illustrative | Recurring review corrections are mined into Go linters, refined through review, and applied across the repository. |
| Explore · Reuse · Lint | illustrative | A daily code-analysis agent keeps a strategy cache to balance reusing proven strategies with exploring new ones, and every pattern it finds becomes a linter. |
| Scan · Fix · Learn | illustrative | The .NET runtime team's CI health loop: a scanner files known build errors, a fixer attempts fixes, and a feedback agent tunes both. |
| Spec-driven testing | illustrative | Agents keep specifications current from human intent and code changes, and an enforcer turns the specification into tests that guard the next change. |
| Spec · Model · Test | illustrative | Specification requirements become TLA+ invariants; model checking explores every state, and its traces become conformance tests. |
| Docs that heal | illustrative | Small agents update, simplify and test documentation the way a newcomer reads it, then fix the gaps and teach the updater new rules. |
| Multi-device tester | illustrative | A daily agent browses the documentation on phone, tablet and desktop viewports and files what breaks; Copilot fixes it, a developer reviews the pull request and the next run re-tests. |
| Findings that land | illustrative | Docs auditor agents file what they find as issues; the findings handed to Copilot as one checkable defect get fixed by merged pull requests, while unassigned rolling reports mostly expire. |
| Personas · Probe · Patch | illustrative | A daily agent role-plays software personas asking the workflow-authoring agent for automations, scores its answers, and re-runs one on a truncated guidance prefix to find rules the cut hides; Copilot moves them up and the next run re-scores. |
| Speech To Agent | illustrative | A dictation glossary maps misheard speech-to-text phrases to exact identifiers, so spoken prompts produce terms agents can grep. |
| For PMs | illustrative | Agentic workflows also serve product managers: an existing team habit triggers an agent, which leaves an artifact for a human to decide on. |
| If every PR needs deep human review, you don’t trust your process. | illustrative | Trust the process: if every pull request needs deep human review, improve checks, agent reviewers and tests until a skim is enough. |
| Cost Optimization |  | Band header (Cost Optimization): debug, spend less and route smarter. |
| Cost management | illustrative | Cut agent spend by scheduling less often, fetching data in deterministic steps, capping credits, using sub-agents and routing models with experimental model routing. |
| Cost guardrails | illustrative | Layered guardrails stop runaway agents: execution controls, including the experimental user rate limit, throttle events, the daily budget blocks runs before activation, per-run limits hard-stop a run, and optional central admission approves or denies each run before it starts. |
| Budget steering | illustrative | As a run nears its AI-credit or time budget, the firewall's API proxy appends a notice to the end of the agent's next model request saying what is left and what to do, once per conversation, so the agent submits what it has before the hard stop and audit records every delivery. |
| Model routing | illustrative | Experimental: with model: router, each run gets the cheapest model and reasoning effort that is good enough for its task and stakes, and audit shows why. |
| DeterministicOps | illustrative | Run history reveals data-gathering calls repeated on every run; audit flags them, and moving them into deterministic steps cuts token use. |
| Audit & Optimize | illustrative | A health agent spots a failing or costly workflow, audits its unified session through the agentic-workflows skill, and tunes it so the next run is cheaper. |
| Agentic Engineering |  | Band header (Agentic Engineering): skip needless runs and give agents memory, hand-offs, expiry, search and measurement. |
| Agents that debug agents | illustrative | The agentic-workflows MCP server exposes logs, audit and audit-diff so any agent can debug, audit and optimize other workflows from unified sessions. |
| Any agent, any provider | illustrative | One Markdown workflow switches its coding agent and model provider with two front matter lines while keeping the same guardrails. |
| Don’t Run the Agent | illustrative | The cheapest agent call is the one never made: cheap deterministic gates from triggers to noop short-circuits run first, and an experimental model-based skip decision only when necessary. |
| Agent memory | illustrative | Configured memories are restored read-only into each run, and trusted jobs persist cache, repository, comment and ledger memory afterward. |
| Memory with a History | illustrative | Ledgers keep append-only records on their own branch, agents read a disposable SQLite projection and request changes, and trusted jobs validate and append them. |
| Durable execution | illustrative | Experimental: durable execution checkpoints the agent's native session, even after a failed step, so a rerun resumes the session instead of starting over. |
| Agents Trigger Agents | illustrative | Agents chain through safe-output dispatches with typed inputs and completion signals, each running inside its own workflow run. |
| Ephemerals | illustrative | Safe outputs carry their own end, so old reports, discussions, comments and drafts close or collapse automatically and human attention is protected. |
| Index & search | illustrative | Experimental: a non-agentic job builds a search index and the agent gets read-only query access, cutting the tokens spent reading whole files. |
| Imports & reuse | illustrative | Shared Markdown components are written and reviewed once, imported by path or from another repository at a ref, and merged and hashed by the compiler. |
| Skills | illustrative | Skills are declared in front matter, pinned to a commit and installed before the agent starts; a triage sub-agent picks the ones that fit each pull request and every review comment cites the skill it applied. |
| Skills in review |  | Because every review comment cites its skill, a skills reviewer's history can be measured: comments by skill week by week, what authors and Copilot did next, and skill inventory and conflict events exported to OpenTelemetry. |
| Experiments, graders & evals | illustrative | Experimental: A/B experiments assign a workflow variant to each run, graders measure every run, and evals judge which variant is better. |
| Enterprise |  | Band header (Enterprise): any agent, any provider, any scale. |
| Infrastructure as code | illustrative | Workflows compile to plain Actions YAML, so they run on any runner, reuse the Actions ecosystem and keep enterprise controls, with the agent behind the firewall. |
| OpenTelemetry | illustrative | Agentic workflows export OpenTelemetry traces to collectors you run, configured per workflow or as an organization default. |
| Distributed tracing | illustrative | One shared trace follows a task from its workflow runs and jobs through agent sessions, tool calls, retries and dispatched runs in other repositories, with outcomes and cost on every span. |
| GitHub-native governance | illustrative | Defaults and policy variables are set at enterprise, organization or repository scope and enforced with GitHub controls such as protected environments. |
| Operator-owned infrastructure | illustrative | Operators run agentic jobs on their own Linux runners, on Kubernetes, or in air-gapped networks with mirrored actions and container images. |
| GitHub App permissions | illustrative | A GitHub App installed on selected repositories mints short-lived scoped tokens, keeping the agent read-only and leaving writes to trusted safe-output jobs in each repository. |
| OIDC token broker | illustrative | Experimental: a deployment-owned broker verifies a job's OIDC identity and returns a short-lived credential, so no long-lived secret is stored in the repository. |
| Push SDLC | illustrative | A task arrives with its own squad of human-led AI agents that join the developer's repository, pair with them, then step back and leave their decisions behind. |
| Connectors | illustrative | Experimental: a connector declares everything a third-party service offers; a workflow imports it and selects only the reads and writes it needs. |
| Webhook gateway | illustrative | Experimental: a hosted receiver authenticates external and GitHub events, matches declared subscriptions and dispatches verified batches to a workflow as untrusted input. |
| Central Agentic Ops |  | Band header (Central Agentic Ops): gh cao, a research prototype for operating agentic workflows across many repositories. |
| Hyperscale Agentics | illustrative | Research prototype: at enterprise scale every agent run reports into telemetry, an operations database and a session data lake, so fleets are managed from aggregates. |
| Centralized campaigns | illustrative | Research prototype: a control repository installs and tracks campaigns that roll agentic processes out across an enterprise's repositories. |
| AW maintenance | illustrative | Research prototype: maintenance loops keep a fleet of agentic workflows healthy by updating versions, fixing drifted lock files, clustering failures and optimizing tokens. |
| Session data lake | illustrative | Research prototype: with opt-in, redacted run sessions land in an immutable, queryable data lake kept separate from the dashboard store. |
| Ops data for Agents | illustrative | Research prototype: agents query operations data through a constrained dashboard language and a read-only MCP server to find and optimize the costliest workflows. |
| AW factory |  | Band header (AW factory): how gh-aw builds itself with agentic workflows, from its measured project history to the safe output that lands agent changes as pull requests. |
| First commit to GA |  | Measured history of gh-aw from its first commit to general availability, showing how much of the work agents and workflows started. |
| How PRs land |  | Measured time to merge and size of gh-aw pull requests, compared by whether people, Copilot or workflows started them. |
| How workflows evolve |  | Measured history of how gh-aw's own workflow Markdown changed and grew over time. |
| Patch to PR | illustrative | The create-pull-request safe output turns an agent's local commits into a pull request through scanning, file policy checks and signed pushes, falling back to issues. |
| GıtHub Agentic Workflows | illustrative | Finale: a control repository coordinates small, guarded agentic processes across many repositories; processes, not prompts. |
