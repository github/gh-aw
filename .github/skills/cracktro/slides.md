# Cracktro slide list

The single ordered list of slides, in reading order. Slides are linked by their stable Id (`#<id>`), never by position: the deck is a grid of bands and columns, so numbers shift whenever a slide is added or moved. Never write slide numbers anywhere; refer to a slide by its Id or heading (and CSS prefix). Legacy `#N` links still open the Nth slide and are rewritten to its `#<id>`.

## Format

Each slide is one `###` section titled `<Heading> {#<id>}`, followed by `####` fields in this order; nothing else follows the first slide:

```markdown
### Safety and control {#safety-and-control}

#### Notes

illustrative

#### Summary

One public sentence.

#### Keywords

sandbox, firewall, read-only token

#### Captions

- First speaker's turn.
- Second speaker's turn.
```

- **Heading** (title before `{#`): the slide's first heading text, exactly as rendered.
- **Id** (`{#<id>}`): the slide's permanent link anchor (`#<id>`), lowercase kebab-case. The runtime slugs the heading (lines joined by spaces) unless the slide object sets `id`. Never change a published Id: when you reword a heading, set `id` on the slide object in `deck.js` to keep the old one. Pick the Id when adding a slide.
- **Notes** (optional): `illustrative` when the speaker notes must match `/illustrative|synthetic|simulated/i`; omit the field otherwise.
- **Summary**: one public sentence with the slide's executive takeaway. Start band headers with `Band header (<band>):`. Describe the durable point, not measured counts or dates that age. No URLs, handles or slide numbers.
- **Keywords**: comma-separated public search terms a viewer might type for this slide: synonyms, acronyms, feature and option names, pattern names. Prefer terms absent from the Heading and Summary; each must be accurate for the slide. No URLs, handles or slide numbers.
- **Captions**: the kiosk closed captions (C): a short two-speaker TV news conversation retelling the Summary, one `- ` list item per turn, at least two turns, alternating speakers (each turn a sentence or two). Public, plain text: no URLs, handles, slide numbers, real people or mascot names.

A field's lines join with spaces, so long text may wrap. `docs/src/lib/cracktro/slides.js` parses this file and fails, with the line number, on a malformed slide title, an invalid Id, an unknown, repeated, missing or out-of-order field, an empty Heading, Summary, keyword or caption turn, fewer than two caption turns, an unknown Notes value or a duplicate. The docs build injects every slide into the talk page's JSON-LD (`hasPart`: position, name, description, keywords, `#<id>` URL); the deck's fuzzy search (`/`) reads keywords from there, and `docs/tests/talk.spec.ts` checks the deck's ids, headings and that metadata against this list. The build and dev server also generate `cracktro/captions.js` (`window.CracktroCaptions`) from the Captions fields; it is never committed, so the plain `python3 -m http.server` preview has no captions and kiosk CC falls back to each slide's notes.

Editing: add, remove or move one slide section in the same change as `deck.js`, and rewrite the Summary, Keywords and Captions whenever the slide's message changes. Keep the Id; never renumber anything.

## Slides

### GitHubAgenticWorkflows {#github-agentic-workflows}

#### Summary

Title slide: GitHub Agentic Workflows are coding agents that run in GitHub Actions, written in Markdown, with guardrails.

#### Keywords

gh-aw, intro, overview, title, coding agents, GitHub Actions, markdown

#### Captions

- Good evening, tonight's top story: GitHub Agentic Workflows, and what they mean for your repositories.
- Put simply, these are coding agents that run inside GitHub Actions, written in plain Markdown.
- Coding agents loose in the pipeline? That sounds a little adventurous for a weeknight.
- That is the twist. They come with guardrails built in, so teams stay firmly in control.

### Agentic Workflows {#agentic-workflows}

#### Summary

Band header (Agentic Workflows): what agentic workflows are, how they are written and why they pay off.

#### Keywords

platform, basics, what is gh-aw, introduction, why

#### Captions

- Coming up next, our Agentic Workflows segment, starting with the basics.
- We will cover what agentic workflows are, how teams write them, and why they pay off.
- Stay with us, the newcomers' guide starts right after this.

### CI/CD meetsContinuous AI {#ci-cd-meets-continuous-ai}

#### Notes

illustrative

#### Summary

Agentic steps run inside the GitHub Actions pipelines teams already use, on the same runners, logs and checks.

#### Keywords

pipeline, CI, CD, runners, checks, logs, continuous integration

#### Captions

- From the pipeline desk tonight: AI is moving into the same pipelines teams already run.
- Right, the agentic steps run inside existing GitHub Actions pipelines, on the same runners as everything else.
- So nobody has to learn a whole new system to watch these agents work?
- Exactly. Same runners, same logs, same checks. The agent just becomes another step you can see.

### Continuous AI {#continuous-ai}

#### Notes

illustrative

#### Summary

Continuous AI, named after CI/CD, is any automated AI that supports software collaboration, such as documentation, triage, fault analysis and quality, kept auditable, event-triggered and under the team's control.

#### Keywords

definition, category, GitHub Next, triage, docs, fault analysis, auditable

#### Captions

- A new term is making the rounds tonight: Continuous AI. What exactly are we talking about?
- It is named after CI/CD. Any automated AI that supports software collaboration, like docs, triage and fault analysis.
- And who holds the remote control when that AI is running around the clock?
- The team does. It stays auditable, triggered by events, and under the team's control at every step.

### Anatomy of a workflow {#anatomy-of-a-workflow}

#### Notes

illustrative

#### Summary

A workflow is one Markdown file: front matter declares triggers, tools, permissions, steps and safe outputs, and the body is the agent prompt.

#### Keywords

frontmatter, front matter, triggers, tools, permissions, prompt, file structure, yaml

#### Captions

- Let's pop the hood tonight and look at what a single workflow actually contains.
- It is just one Markdown file. The front matter declares triggers, tools, permissions, steps and safe outputs.
- And the rest of the page, the part below the front matter, what does that do?
- The body is the agent prompt. Settings up top, plain language instructions underneath, all in one file.

### Reuse your CI {#reuse-your-ci}

#### Notes

illustrative

#### Summary

Agentic workflows compile to plain GitHub Actions, so teams keep their environments, runners, Entra ID and Vault OIDC, GitHub Apps, marketplace actions, custom jobs and reusable workflow calls: all Actions infrastructure is reused.

#### Keywords

runners, OIDC, Entra ID, Vault, GitHub Apps, marketplace, reusable workflows, custom jobs

#### Captions

- Good news for platform teams tonight: your existing CI investment is not going anywhere.
- Agentic workflows compile to plain GitHub Actions, so environments, runners, GitHub Apps and marketplace actions all carry over.
- What about the identity plumbing, things like Entra ID and Vault OIDC, or custom jobs?
- All reused, along with reusable workflow calls. Every piece of your Actions infrastructure keeps doing its job.

### Turn Intent to Workflow {#turn-intent-to-workflow}

#### Summary

Describe a job in plain language, an agent writes the Markdown workflow, and gh aw compile deterministically turns it into a hardened Actions lock file.

#### Keywords

compile, lock file, gh aw compile, natural language, authoring, create workflow

#### Captions

- Tonight's how-to story: turning a sentence of intent into a working workflow.
- You describe the job in plain language, and an agent writes the Markdown workflow for you.
- Then what? Does that Markdown just run as is, straight off the page?
- Not quite. gh aw compile deterministically turns it into a hardened Actions lock file, and that is what runs.

### Ask once. The agent picks the guide. {#agentic-workflows-router}

#### Notes

illustrative

#### Summary

The /agentic-workflows agent is a router: it reads a plain-language request, picks a route such as create, update, debug, optimize, upgrade, report or share, loads that focused guide from .github/aw/ and follows it.

#### Keywords

agentic-workflows agent, router, dispatcher, slash command, intent, create workflow, update workflow, debug workflow, token optimization, upgrade, report, shared workflow, guides, prompts

#### Captions

- Next up: one slash command, many jobs. Ask the agentic workflows agent once and it picks the guide.
- Right, it works like a router. It reads your plain request and chooses create, update, debug, optimize or upgrade.
- So I do not need to know which guide to open?
- No. It loads that focused guide and follows it, so the right instructions arrive for the right job.

### The compiler corrects the agent. {#authoring-loop}

#### Notes

illustrative

#### Summary

The agent writes workflow Markdown, gh aw compile rejects mistakes with precise fixes such as did-you-mean hints and strict-mode permission errors (real, abridged compiler messages), and the agent applies them until the workflow compiles to a lock file.

#### Keywords

authoring loop, agentic loop, gh aw compile, compiler errors, did you mean, strict mode, validation, self-correction, lock file, feedback loop

#### Captions

- Now a look inside the authoring loop, where the compiler keeps the agent honest.
- The agent writes the workflow Markdown, then gh aw compile checks it and rejects any mistakes.
- And the errors actually help? Not just a wall of red text?
- They are precise: did-you-mean hints and strict-mode permission fixes. The agent applies them until it compiles to a lock file.

### Personas test the guide. Agent fixes it. {#persona-optimization}

#### Notes

illustrative

#### Summary

Agent Persona Explorer role-plays many kinds of users against /agentic-workflows every day, scores each answer on five dimensions, probes what survives in the first 8,000 bytes and files gaps that an agent fixes in the agent guidance.

#### Keywords

Agent Persona Explorer, personas, evaluation, scoring, prefix probe, 8000 bytes, prompt truncation, guidance optimization, Copilot, measured data, daily report

#### Captions

- Our next story: personas put the guide to the test every single day.
- An explorer agent role-plays many kinds of users and scores each answer on five dimensions.
- What happens when an answer falls short?
- It checks what fits in the first eight thousand bytes, files the gaps, and an agent fixes the guidance.

### Turn Workflow to Value {#turn-workflow-to-value}

#### Notes

illustrative

#### Summary

Track outcomes such as backlog burndown, quality and delivery speed to show what agentic workflows are worth.

#### Keywords

ROI, metrics, outcomes, backlog burndown, delivery speed, value

#### Captions

- The money question tonight: are these agentic workflows actually worth it for a team?
- The answer comes from tracking outcomes, things like backlog burndown, quality and delivery speed.
- So the measure is results on the board, not just how busy the robots look?
- Exactly. Outcomes are what show the value of agentic workflows, and teams can track them over time.

### Workflows → Process → Factory {#workflows-process-factory}

#### Notes

illustrative

#### Summary

Every factory is made of processes: a workflow goes from an issue to an agent to a pull request, sequencing workflows makes a process, and processes that feed each other, on any harness with developers at the gates, make a factory.

#### Keywords

workflows, process, factory, sequencing, composition, chaining, safe outputs, zoom out, blueprint, harnesses, engines, human in the loop

#### Captions

- Zooming out tonight: how single workflows add up to something much bigger.
- Start small. One workflow takes an issue, hands it to an agent and comes back with a pull request.
- And when you line several of those up, one after another?
- Sequence them and you have a process. Let processes feed each other, on any harness, with developers at the gates, and you have a factory.

### Security {#security}

#### Summary

Band header (Security): design every workflow as if the agent is already compromised.

#### Keywords

guardrails, threat model, compromised agent, defense in depth

#### Captions

- Coming up next, our Security segment, and it starts with a sobering rule.
- The guiding rule here is simple: design every workflow as if the agent is already compromised.
- Assume the worst, then build defenses in layers. Stay with us for the details.

### Agentic attacks {#agentic-attacks}

#### Notes

illustrative

#### Summary

Common agent attacks, from prompt injection to secret exfiltration and destructive commands, are each stopped by one or more layered defenses.

#### Keywords

prompt injection, exfiltration, secrets, destructive commands, threats, attack

#### Captions

- From the security desk tonight: the threats facing AI agents, and how they are being stopped.
- The common attacks range from prompt injection to secret exfiltration and destructive commands.
- That is quite a lineup. Is there one single wall that blocks all of them?
- No single wall. Each attack is stopped by one or more layered defenses working together.

### Safety and control {#safety-and-control}

#### Summary

Inputs are filtered, the agent runs sandboxed with a read-only token behind a firewall and gateway, and writes happen only through scanned safe outputs.

#### Keywords

sandbox, firewall, read-only token, MCP gateway, defense layers, AWF

#### Captions

- Our security coverage continues with the full chain of safety controls around every agent.
- Inputs are filtered first. Then the agent runs sandboxed, with a read-only token, behind a firewall and gateway.
- And if the agent needs to change something, say open an issue, how does that happen?
- Writes happen only through safe outputs, and those are scanned before anything is applied.

### Integrity filtering {#integrity-filtering}

#### Notes

illustrative

#### Summary

Malicious comments on a public pull request never reach the agent: the MCP gateway rates each item by its author's trust and drops everything below the configured minimum integrity before the model sees it.

#### Keywords

trust, min-integrity, untrusted input, public repo, MCP gateway, author association

#### Captions

- A developing story on public repositories: what happens to malicious comments on a pull request?
- They never reach the agent. The MCP gateway rates each item by how trusted its author is.
- So a stranger's sneaky comment just quietly disappears before the model reads it?
- Right. Anything below the configured minimum integrity is dropped before the model ever sees it.

### Zero-secret agents {#zero-secret-agents}

#### Notes

illustrative

#### Summary

The agent runs in an isolated sandbox, here the experimental micro-VM runtime, holding no credentials; every key stays on the host behind the MCP gateway, API proxy and egress firewall.

#### Keywords

credentials, micro-VM, API proxy, egress, no secrets, isolation

#### Captions

- Tonight on the security beat: agents that carry no secrets at all.
- The agent runs in an isolated sandbox, here the experimental micro-VM runtime, and holds no credentials.
- Then where do the keys live, if the agent is not holding them?
- Every key stays on the host, behind the MCP gateway, the API proxy and the egress firewall.

### Supply chain hardening {#supply-chain-hardening}

#### Notes

illustrative

#### Summary

The compiler applies layered static checks, including trigger admission, expression safety, pinned actions, least privilege and secret scans, before any workflow runs.

#### Keywords

pinned actions, SHA pinning, static analysis, least privilege, secret scan, expressions

#### Captions

- From the supply chain desk: checks that happen before a workflow ever runs.
- The compiler applies layered static checks, including trigger admission, expression safety and pinned actions.
- Pinned actions, expression safety. Is there more on that checklist before launch?
- Yes, least privilege and secret scans too. All of it is checked before any workflow runs.

### Compiler normalization {#compiler-normalization}

#### Notes

illustrative

#### Summary

gh aw lint flags unsafe patterns that coding agents often write into a workflow, such as tag-pinned actions (action-pin), expressions in shell commands (template-injection) and custom jobs that skip threat detection (custom-job-detection); the compiler rewrites them into commit SHA pins, environment-variable data and detection-gated jobs, and rejects write permissions on the agent, so the lock file lints clean.

#### Keywords

normalization, rewrite, agent-generated code, gh aw lint, lint rules, action-pin, template-injection, custom-job-detection, actionlint, SHA pinning, env var, shell injection, prompt expressions, detection gate, always(), contents: write, safe-outputs, lock file, CTR-002, CTR-005, CTR-006

#### Captions

- Here is a story about cleaning up after the robots: the compiler fixing agent-written workflows.
- gh aw lint flags unsafe patterns agents often write, like tag-pinned actions and expressions inside shell commands.
- Flagging is helpful, but does anything actually get fixed, or just reported?
- The compiler rewrites them into commit SHA pins, environment variables and detection-gated jobs, so the lock file lints clean.

### Safe outputs {#safe-outputs}

#### Notes

illustrative

#### Summary

Front matter declares each allowed write under safe-outputs, with options such as max, labels, title-prefix and target; the agent only proposes, threat detection reviews each proposal, and a separate scoped job applies only those declared outputs.

#### Keywords

writes, threat detection, safe-outputs, scoped jobs, permissions, proposals, YAML, front matter, create-issue, add-comment, max, title-prefix, labels, target, triggering, close-older-issues, hide-older-comments, limits

#### Captions

- Tonight we explain safe outputs, the system that decides what an agent is allowed to change.
- It starts in the front matter: each allowed write is a typed entry, with limits like max, labels and target.
- Agents only propose writes. Threat detection then reviews each proposal before anything happens.
- So who actually presses the button once a proposal is cleared?
- A separate, scoped job applies only the declared outputs, adding the configured prefix and labels. The agent itself never writes.

### Extend safe outputs {#extend-safe-outputs}

#### Notes

illustrative

#### Summary

Every agent write is a typed tool: built-in handlers cover common GitHub writes such as issues, pull requests, comments and labels, safe scripts add inline JavaScript run once per call without secrets, and safe jobs add a custom Actions job with its own steps and secrets, all behind threat detection and outside the sandbox.

#### Keywords

extensibility, custom safe outputs, safe-outputs.scripts, safe-outputs.jobs, safe-outputs.actions, inline script handlers, custom jobs, MCP tools, GH_AW_AGENT_OUTPUT, create-issue, create-pull-request, add-comment, create-discussion, add-labels, update-issue, push-to-pull-request-branch, chat notification, webhooks, secrets, built-in handlers

#### Captions

- Safe outputs are not a closed list. Tonight: how teams extend what an agent may change.
- Built-in handlers cover the common GitHub writes, such as issues, pull requests, comments and labels.
- And when a team needs something gh-aw does not ship, like a chat notification?
- A safe script adds a little JavaScript that runs once per call, with no secrets.
- A safe job is a full Actions job with its own steps and secrets. All three stay typed tools, behind threat detection, outside the sandbox.

### Model-checked {#model-checked}

#### Notes

illustrative

#### Summary

A bounded TLA+ model of the compiled workflow lets TLC check every job schedule against security invariants; seeded faults must break exactly one invariant, and witnesses prove validated writes still happen, shown as a replayed counterexample and witness trace across the job lanes.

#### Keywords

TLA+, TLC, formal methods, invariants, model checking, verification, counterexample

#### Captions

- From the formal methods desk: the workflow security story is now being mathematically checked.
- A bounded TLA+ model of the compiled workflow lets TLC check every job schedule against security invariants.
- How do you know the checker itself is catching real problems and not just nodding along?
- Seeded faults must break exactly one invariant, and witness traces prove that validated writes still happen.

### Need-to-know enclaves {#need-to-know-enclaves}

#### Notes

illustrative

#### Summary

Experimental: single-use, isolated enclave agents read private repositories and return only schema-constrained values, so private data cannot leak into public replies.

#### Keywords

private repos, enclave, data leak, schema, isolation, experimental

#### Captions

- An experimental story tonight about keeping private data private: need-to-know enclaves.
- Single-use, isolated enclave agents read private repositories and return only schema-constrained values.
- So they peek at the private material, but only hand back a tightly shaped answer?
- Exactly, and that means private data cannot leak into public replies. Still experimental, but promising.

### Continuous Red Teaming {#continuous-red-teaming}

#### Notes

illustrative

#### Summary

A daily red-team agent keeps trying new ways to escape the agent firewall, and each escape it finds is turned into a firewall fix.

#### Keywords

red team, firewall escape, pentest, adversarial, security testing

#### Captions

- Our security desk has an unusual hire tonight: an agent paid to break out.
- A daily red-team agent keeps trying new ways to escape the agent firewall.
- And when it actually succeeds? That sounds like bad news for someone.
- It is good news, really. Each escape it finds gets turned into a fix for the firewall.

### Stop manually translating intent into code. {#stop-manually-translating-intent-into-code}

#### Summary

The factory mindset: engineer processes that create trusted features instead of hand-translating intent into code.

#### Keywords

factory, mindset, processes, intent

#### Captions

- A bold message from the factory floor tonight: stop manually translating intent into code.
- It is a mindset shift. Engineer the processes that create trusted features, instead of hand-writing every change.
- So the craft moves from writing the code to designing the factory that builds it?
- That is the factory mindset in a nutshell. Build trusted processes, and let them turn intent into features.

### Human agent processes {#human-agent-processes}

#### Summary

Band header (Human agent processes): engineer processes, not code, with patterns that pair people and agents, some with a column of worked examples below.

#### Keywords

patterns, ops, human in the loop, collaboration, processes

#### Captions

- Coming up next, our Human agent processes segment.
- The theme is engineering processes, not code, using patterns that pair people with agents.
- We will walk through those patterns, with worked examples along the way. Stay with us.

### Event-driven Hand-offs {#event-driven-hand-offs}

#### Notes

illustrative

#### Summary

Event-driven hand-off patterns such as IssueOps, DailyOps, ReviewOps and LabelOps let agents do the work while people make the decisions.

#### Keywords

IssueOps, DailyOps, ReviewOps, LabelOps, events, handoff

#### Captions

- Tonight we look at hand-offs, the moments where work passes between people and agents.
- There are event-driven patterns for it, such as IssueOps, DailyOps, ReviewOps and LabelOps.
- And in all of those patterns, who gets the final say on what happens?
- Agents do the work, people make the decisions. That split is the heart of every pattern.

### Repo Commands {#repo-commands}

#### Notes

illustrative

#### Summary

Slash commands and label commands are synthetic triggers: one front matter line compiles into the real issue, comment, pull request and discussion events with a first-word guard, role check, reaction and status comment, so people call an agent exactly when they want it and can run it again.

#### Keywords

slash command, ChatOps, label command, triggers, reaction, comment command

#### Captions

- On the chat front tonight: calling an agent with a simple slash command or label.
- These are synthetic triggers. One front matter line compiles into the real issue, comment and pull request events.
- What keeps random folks from setting the agent off by accident in a busy thread?
- A first-word guard and a role check, plus a reaction and status comment. People call it exactly when they want it.

### LabelOps state machines {#labelops-state-machines}

#### Notes

illustrative

#### Summary

Labels become the states of a process: replace-label swaps one state for the next, allowed-transitions limits agents to declared edges, and label commands let people with the triage role drive the human steps without push or merge access.

#### Keywords

labels, states, transitions, replace-label, triage, workflow states

#### Captions

- A story about labels tonight, and how they are quietly becoming state machines.
- Labels become the states of a process. Replace-label swaps one state for the next.
- Can an agent just jump straight to whatever label it likes along the way?
- No. Allowed-transitions limits agents to declared edges, and people with triage role drive the human steps without push access.

### Pull Request Review² {#pull-request-review-squared}

#### Notes

illustrative

#### Summary

Marking a pull request ready starts agentic reviewers, and Copilot is nudged to fix their findings so the human reviewer sees a clean pull request.

#### Keywords

ReviewOps, code review, PR review, reviewers, ready for review

#### Captions

- Code review, squared: tonight's story on reviewers that review before the humans do.
- Marking a pull request ready starts agentic reviewers, and Copilot is nudged to fix what they find.
- So by the time a human opens it, most of the nitpicks are already handled?
- That is the goal. The human reviewer sees a clean pull request and can focus on what matters.

### Ponytail Reviewer {#ponytail-reviewer}

#### Notes

illustrative

#### Summary

A read-only agentic reviewer reads each ready pull request only for unnecessary complexity, posts one-line findings with the lines each saves, and says ship once nothing is left to cut; measured on gh-aw, most ready pull requests are already lean and most findings get acted on.

#### Keywords

simplicity, complexity, lean, review bot, ship, code review

#### Captions

- Meet a reviewer with a single obsession tonight: unnecessary complexity in your pull requests.
- It is read-only. It posts one-line findings showing the lines each one saves, and says ship when nothing is left to cut.
- Does it actually find much, or does it just nag everybody about tidiness?
- Measured on gh-aw, most ready pull requests are already lean, and most of its findings get acted on.

### Issue Monster {#issue-monster}

#### Notes

illustrative

#### Summary

Issue Monster loves cookie issues: agents file their follow-ups into a cookie-labelled issue queue; a scheduler runs cheap deterministic gates and filters every few minutes and only wakes an agent to assign the best few issues to Copilot.

#### Keywords

cookie, queue, scheduler, assign, Copilot, backlog

#### Captions

- A sweet story tonight: meet Issue Monster, a scheduler that loves cookie issues.
- Agents file their follow-ups into a cookie-labelled issue queue, and a scheduler keeps watch over it.
- Does the monster wake up an expensive agent every single time it checks that queue?
- No. It runs cheap deterministic gates first, and only wakes an agent to assign the best few issues to Copilot.

### Decide · Draft · Record {#decide-draft-record}

#### Notes

illustrative

#### Summary

When a ready pull request carries an architecture decision with no record, an agent drafts an ADR that people finalize and merge with the code, and later agents read it to learn why the feature works that way.

#### Keywords

ADR, architecture decision record, design, docs, decisions

#### Captions

- From the architecture desk: making sure big design decisions do not go unrecorded.
- When a ready pull request carries an architecture decision with no record, an agent drafts an ADR.
- Does the agent just publish that record on its own, or do people step in?
- People finalize and merge it with the code, and later agents read it to learn why things work that way.

### PR Sous Chef {#pr-sous-chef}

#### Notes

illustrative

#### Summary

PR Sous Chef regularly finds pull requests that sit idle with work waiting, such as unresolved review comments or failing CI, and nudges Copilot to pick them back up, while humans still review and merge.

#### Keywords

stale PRs, nudge, CI failures, review comments, idle pull requests

#### Captions

- In the kitchen tonight: PR Sous Chef, keeping idle pull requests from going cold.
- It regularly finds pull requests with work waiting, like unresolved review comments or failing CI.
- And then what, does it take over the stove and cook the fix itself?
- It nudges Copilot to pick the work back up, while humans still review and merge.

### Issues as intent {#issues-as-intent}

#### Notes

illustrative

#### Summary

Contributors run a debug agent on their own failures and file anonymized issues that state intent, which maintainers plan and assign to Copilot.

#### Keywords

IssueOps, debug, bug reports, anonymized, contributors, planning

#### Captions

- A community story tonight: how contributor bug reports are turning into clear statements of intent.
- Contributors run a debug agent on their own failures, then file anonymized issues that state intent.
- What happens to those issues once they land with the maintainers?
- Maintainers plan them and assign them to Copilot, so a contributor's failure becomes a tracked fix.

### Daily Improvers {#daily-improvers}

#### Notes

illustrative

#### Summary

Scheduled research agents find problems such as duplicated code, assign them to Copilot, and developers refine the resulting pull requests.

#### Keywords

DailyOps, research, duplicate code, scheduled, refactor, improvements

#### Captions

- Rise and shine, it is time for the Daily Improvers, agents that tidy up on a schedule.
- Scheduled research agents find problems such as duplicated code, and assign them to Copilot.
- So the agents find it and fix it, and the developers just get to relax?
- Not quite. Developers refine the resulting pull requests, so people still shape the final change.

### Repo Assist {#repo-assist}

#### Notes

illustrative

#### Summary

A scheduled assistant picks maintenance tasks such as triage and stale pull requests, a maintainer reviews every result, and backlogs shrink.

#### Keywords

maintenance, triage, stale, backlog, assistant, maintainer

#### Captions

- For overworked maintainers tonight, some relief: a repository assistant on a schedule.
- Repo Assist picks maintenance tasks like triage and stale pull requests, all on its own schedule.
- Does anyone check its homework, or does it just ship whatever it likes?
- A maintainer reviews every result, and the backlogs shrink. Help, with a human still signing off.

### Research · Assign · Observe {#research-assign-observe}

#### Notes

illustrative

#### Summary

A daily benchmark agent files performance regressions, Copilot fixes them, and the next run observes the result to close the loop.

#### Keywords

performance, benchmarks, regressions, perf, ResearchPlanAssignOps

#### Captions

- On the performance beat tonight: research, assign, observe, a loop that keeps speed in check.
- A daily benchmark agent files performance regressions, and Copilot works on fixing them.
- How does anyone know a fix actually made things faster again?
- The next run observes the result, which closes the loop. Measure, fix, and measure again.

### Hunt · Falsify · Fix {#hunt-falsify-fix}

#### Notes

illustrative

#### Summary

A daily agent hunts for vulnerabilities in deterministically scoped, rotating files, discards every lead it can falsify, and files only the survivors as issues that Copilot fixes.

#### Keywords

vulnerabilities, security bugs, falsification, bug hunt

#### Captions

- From the security desk: a bug hunter that tries hard to prove itself wrong.
- A daily agent hunts for vulnerabilities in deterministically scoped, rotating files across the codebase.
- And every lead it turns up gets filed as an issue right away?
- No. It discards every lead it can falsify, files only the survivors, and Copilot fixes those.

### Query · Ask · Grow {#query-ask-grow}

#### Notes

illustrative

#### Summary

An expert DevOps agent read live telemetry through observability MCP servers and filed the most important instrumentation gap each day, growing OpenTelemetry support.

#### Keywords

observability, telemetry, OpenTelemetry, OTel, DevOps, instrumentation

#### Captions

- Tonight's observability story: an agent that reads live telemetry and reports what is missing.
- An expert DevOps agent read telemetry through observability MCP servers and found instrumentation gaps.
- Did it dump a huge list of problems on the team all at once?
- It filed the most important gap each day, steadily growing OpenTelemetry support.

### Instrument · Debug · Repeat {#instrument-debug-repeat}

#### Notes

illustrative

#### Summary

Namespaced debug logging is an agent's debugger: a DEBUG-scoped rerun turns a silent failure into stderr evidence of branch and state, and a daily Go Logger workflow keeps adding side-effect-free log calls through small reviewed pull requests.

#### Keywords

debug logging, DEBUG, DEBUG_COLORS, stderr, namespaces, logger, Go Logger, troubleshooting, silent failure

#### Captions

- Tonight we ask: what does an agent use as its debugger? The answer may surprise you.
- Namespaced debug logging. A DEBUG-scoped rerun turns a silent failure into stderr evidence of branch and state.
- And who keeps adding those log calls as the code keeps changing?
- A daily Go Logger workflow adds side-effect-free log calls through small, reviewed pull requests.

### Semantic clustering {#semantic-clustering}

#### Notes

illustrative

#### Summary

Grouping functions by intent rather than by text exposes misplaced code that duplicate detection misses, and Copilot moves it.

#### Keywords

refactoring, misplaced code, intent, functions, code organization

#### Captions

- Closing out tonight's segment: semantic clustering, sorting code by what it means, not how it looks.
- Grouping functions by intent rather than by text exposes misplaced code that duplicate detection misses.
- So it finds the function that wandered into the wrong neighborhood?
- Exactly, and then Copilot moves it to where it belongs. Tidier code, organized by purpose.

### Go Fan {#go-fan}

#### Notes

illustrative

#### Summary

A daily agent compares each Go dependency's releases with how the code uses it and files review issues that Copilot turns into fixes.

#### Keywords

Go, golang, dependencies, modules, releases, upgrades

#### Captions

- From the dependency desk tonight: an agent that is a genuine fan of your Go modules.
- Every day it compares each Go dependency's new releases with how your code actually uses it.
- So it reads release notes so the team does not have to?
- Exactly, and it files review issues that Copilot then turns into fixes. Upgrades stop piling up.

### Merge fast, unslop daily {#merge-fast-unslop-daily}

#### Notes

illustrative

#### Summary

Merge many small pull requests quickly and let agentic reviewers and daily improvers clean up, instead of polishing each one through endless nit rounds.

#### Keywords

slop, nits, small PRs, velocity, cleanup, iterate

#### Captions

- A new philosophy is sweeping engineering teams: merge fast, and clean up daily.
- Instead of polishing every pull request through endless rounds of nits, you merge many small ones quickly.
- And who tidies up the slop left behind?
- Agentic reviewers and daily improvers handle the cleanup, so velocity stays high and the code keeps improving.

### Dreaming {#dreaming}

#### Notes

illustrative

#### Summary

Dreaming agents mine the activity between people and agents, such as review threads, sessions and failed runs, to design and optimize agentic workflows, prompts, skills and linters.

#### Keywords

mining, optimization, prompts, skills, linters, self-improvement, meta

#### Captions

- Our science correspondent reports agents are now dreaming. Yes, you heard that right.
- Dreaming agents mine the activity between people and agents, like review threads, sessions and failed runs.
- And what do they do with all those memories?
- They use them to design and optimize agentic workflows, prompts, skills and linters. The system improves itself.

### Mine · Refine · Apply {#mine-refine-apply}

#### Notes

illustrative

#### Summary

Recurring review corrections are mined into Go linters, refined through review, and applied across the repository.

#### Keywords

linters, review corrections, static analysis, go vet, analyzers

#### Captions

- Tired of leaving the same review comment again and again? This next story is for you.
- Recurring review corrections get mined and turned into Go linters, the kind of static analyzers go vet runs.
- So the reviewer's pet peeve becomes a rule?
- Right. The linter is refined through review, then applied across the whole repository automatically.

### Explore · Reuse · Lint {#explore-reuse-lint}

#### Notes

illustrative

#### Summary

A daily code-analysis agent keeps a strategy cache to balance reusing proven strategies with exploring new ones, and every pattern it finds becomes a linter.

#### Keywords

strategy cache, code analysis, exploration, exploitation, linters

#### Captions

- Tonight, a code-analysis agent that keeps notes on what works.
- It runs daily with a strategy cache, balancing proven strategies against exploring brand new ones.
- Reuse what works, but keep looking. Sounds like a good life motto.
- And it pays off, because every pattern it finds becomes a linter that guards the code from then on.

### Scan · Fix · Learn {#scan-fix-learn}

#### Notes

illustrative

#### Summary

The .NET runtime team's CI health loop: a scanner files known build errors, a fixer attempts fixes, and a feedback agent tunes both.

#### Keywords

.NET, dotnet, runtime, CI health, build errors, flaky

#### Captions

- Over to the .NET runtime team, where CI health now runs on a loop.
- A scanner files known build errors, and a fixer agent attempts fixes for them.
- And if the scanner or fixer gets it wrong?
- A feedback agent tunes both of them. Scan, fix, learn, and the build keeps getting healthier.

### Spec-driven testing {#spec-driven-testing}

#### Notes

illustrative

#### Summary

Agents keep specifications current from human intent and code changes, and an enforcer turns the specification into tests that guard the next change.

#### Keywords

SpecOps, specifications, tests, requirements, enforcer

#### Captions

- In testing news, specifications are finally keeping up with the code.
- Agents keep specs current from human intent and from code changes, so the document never goes stale.
- A living spec. But does anyone check the code against it?
- An enforcer agent does, turning the specification into tests that guard the next change.

### Spec · Model · Test {#spec-model-test}

#### Notes

illustrative

#### Summary

Specification requirements become TLA+ invariants; model checking explores every state, and its traces become conformance tests.

#### Keywords

TLA+, invariants, conformance tests, model checking, traces

#### Captions

- Now for something a little more mathematical, folks. Formal methods enter the chat.
- Specification requirements become TLA+ invariants, and model checking explores every possible state of the system.
- Every state? That sounds thorough. What comes out the other end?
- Its traces become conformance tests, so the real code is checked against the proven model.

### Work queue, modeled {#work-queue-modeled}

#### Notes

illustrative

#### Summary

The work queue is specified as TLA+ modules covering lifecycle, compare-and-swap publication on a git log, DAG and external gates; mutants must fail and witnesses must be reached.

#### Keywords

TLA+, compare-and-swap, git log, DAG, mutants, witnesses, formal

#### Captions

- Our formal methods beat continues with a work queue, fully modeled.
- The queue is specified as TLA+ modules covering lifecycle, compare-and-swap publication on a git log, DAG and external gates.
- How do they know the model is actually catching problems?
- Deliberate mutants must fail, and expected witnesses must be reached. The model has to prove it has teeth.

### Docs that heal {#docs-that-heal}

#### Notes

illustrative

#### Summary

Small agents update, simplify and test documentation the way a newcomer reads it, then fix the gaps and teach the updater new rules.

#### Keywords

documentation, docs, newcomer, unbloat, self-healing, tutorials

#### Captions

- Good news for anyone who has ever been lost in a tutorial: docs that heal themselves.
- Small agents update, simplify and test the documentation the way a newcomer would actually read it.
- And what happens at the spots where a newcomer would get stuck?
- They fix the gaps, then teach the updater new rules so the same gap does not come back.

### Multi-device tester {#multi-device-tester}

#### Notes

illustrative

#### Summary

A daily agent browses the documentation on phone, tablet and desktop viewports and files what breaks; Copilot fixes it, a developer reviews the pull request and the next run re-tests.

#### Keywords

mobile, responsive, viewports, playwright, browser testing, tablet

#### Captions

- Checking your phone right now? So is this agent, on behalf of the docs.
- Every day it browses the documentation on phone, tablet and desktop viewports and files whatever breaks.
- Then what happens next, does someone have to fix it by hand?
- Copilot fixes it, a developer reviews the pull request, and the next run re-tests. A tidy little loop.

### Findings that land {#findings-that-land}

#### Notes

illustrative

#### Summary

Docs auditor agents file what they find as issues; the findings handed to Copilot as one checkable defect get fixed by merged pull requests, while unassigned rolling reports mostly expire.

#### Keywords

audits, issues, docs auditor, defects, reports, follow-through

#### Captions

- An investigation into audit reports, and which findings actually get fixed.
- Docs auditor agents file their findings as issues. The ones handed to Copilot as one checkable defect get fixed.
- And the big rolling reports nobody is assigned to?
- They mostly expire. Lesson learned: make each finding a single checkable defect with a clear owner.

### Personas · Probe · Patch {#personas-probe-patch}

#### Notes

illustrative

#### Summary

A daily agent role-plays software personas asking the workflow-authoring agent for automations, scores its answers, and re-runs one on a truncated guidance prefix to find rules the cut hides; Copilot moves them up and the next run re-scores.

#### Keywords

personas, role-play, evals, prompt truncation, scoring, guidance

#### Captions

- Role-play hits the newsroom. An agent is pretending to be your software users.
- Daily, it plays personas asking the workflow-authoring agent for automations, then scores the answers it gets.
- Scoring answers is one thing. Is there a twist to this story?
- It re-runs one on a truncated guidance prefix to find rules the cut hides. Copilot moves them up, and the next run re-scores.

### Speech To Agent {#speech-to-agent}

#### Notes

illustrative

#### Summary

A dictation glossary maps misheard speech-to-text phrases to exact identifiers, so spoken prompts produce terms agents can grep.

#### Keywords

dictation, voice, speech-to-text, glossary, transcription

#### Captions

- For the folks who talk to their computers, this one is for you.
- A dictation glossary maps misheard speech-to-text phrases to the exact identifiers they were meant to be.
- So mumbling at the agent actually works now?
- Pretty much. Spoken prompts come out with precise terms that agents can grep for in the code.

### For PMs {#for-pms}

#### Notes

illustrative

#### Summary

Agentic workflows also serve product managers: an existing team habit triggers an agent, which leaves an artifact for a human to decide on.

#### Keywords

product managers, product, habits, artifacts, non-engineers

#### Captions

- Not an engineer? Stay with us, because this story is for product managers.
- Agentic workflows serve PMs too. An existing team habit triggers an agent, no new process required.
- And what does the PM get out of it?
- The agent leaves an artifact, and a human makes the decision. Agents prepare, people decide.

### If every PR needs deep human review, you don’t trust your process. {#if-every-pr-needs-deep-human-review-you-dont-trust-your-process}

#### Notes

illustrative

#### Summary

Trust the process: if every pull request needs deep human review, improve checks, agent reviewers and tests until a skim is enough.

#### Keywords

trust, review, process, confidence, skim

#### Captions

- A provocative headline tonight: if every pull request needs deep human review, you do not trust your process.
- The fix is not more reviewing. It is better checks, agent reviewers and tests.
- So we keep improving the process until what, exactly?
- Until a quick skim is enough. Build confidence into the process, and trust it.

### Cost Optimization {#cost-optimization}

#### Summary

Band header (Cost Optimization): debug, spend less and route smarter.

#### Keywords

cost, spend, tokens, budget, savings

#### Captions

- Coming up next, our Cost Optimization segment. Stay with us.
- We will look at how to debug agents, spend less on them, and route smarter.
- Keep your wallets handy, folks. Stay with us.

### Cost management {#cost-management}

#### Notes

illustrative

#### Summary

Cut agent spend by scheduling less often, fetching data in deterministic steps, capping credits, using sub-agents and routing models with experimental model routing.

#### Keywords

spend, credits, scheduling, sub-agents, model routing, tokens

#### Captions

- Let us talk money. How do teams keep agent spend under control?
- Schedule less often, fetch data in deterministic steps, cap credits, and use sub-agents for focused work.
- Any help picking which model to pay for?
- Yes, experimental model routing picks models for you. Small habits add up to real savings.

### Cost guardrails {#cost-guardrails}

#### Notes

illustrative

#### Summary

Layered guardrails stop runaway agents: execution controls, including the experimental user rate limit, throttle events, the daily budget blocks runs before activation, per-run limits hard-stop a run, and optional central admission approves or denies each run before it starts.

#### Keywords

rate limit, budget, limits, admission, runaway, throttle, max-ai-credits

#### Captions

- From the finance desk: how to stop a runaway agent before it runs up the bill.
- Guardrails are layered. Execution controls, including an experimental user rate limit, throttle events first.
- And if throttling alone is not enough to stop it?
- A daily budget blocks runs before activation, per-run limits hard-stop a run, and optional central admission approves or denies each run.

### Budget steering {#budget-steering}

#### Notes

illustrative

#### Summary

As a run nears its AI-credit or time budget, the firewall's API proxy appends a notice to the end of the agent's next model request saying what is left and what to do, once per conversation, so the agent submits what it has before the hard stop and audit records every delivery.

#### Keywords

budget notice, API proxy, time limit, credits, context, hard stop

#### Captions

- What happens when an agent is about to run out of budget? We have the answer.
- As a run nears its credit or time budget, the firewall's API proxy appends a notice to the agent's next model request.
- A heads-up for the agent. What does the notice actually say?
- What is left and what to do, once per conversation, so the agent submits its work before the hard stop. Audit records every delivery.

### Model routing {#model-routing}

#### Notes

illustrative

#### Summary

Experimental: with model: router, each run gets the cheapest model and reasoning effort that is good enough for its task and stakes, and audit shows why.

#### Keywords

router, cheapest model, reasoning effort, model selection, experimental

#### Captions

- Experimental news now: a router that shops for models on your behalf.
- With model set to router, each run gets the cheapest model and reasoning effort good enough for its task and stakes.
- Cheap is great, but can we see why it chose what it chose?
- Yes. Audit shows the reasoning behind each choice, so savings never become a mystery.

### DeterministicOps {#deterministic-ops}

#### Notes

illustrative

#### Summary

A dial runs from 100% agentic to fully deterministic: run history shows the same prompt lines becoming the same data-gathering calls every run, audit flags them, and translating them into steps: commands that write a file the agent reads turns the dial toward deterministic and cuts token use.

#### Keywords

deterministic, steps, tokens, data gathering, scripts, pre-fetch

#### Captions

- Picture a dial, folks. One end fully agentic, the other fully deterministic.
- Run history shows the same prompt lines turning into the same data-gathering calls every single run, and audit flags them.
- So what do you do with those repeats?
- Translate them into steps that write a file the agent reads. The dial turns deterministic, and token use drops.

### CLI vs MCP {#cli-vs-mcp}

#### Notes

illustrative

#### Summary

A fat MCP call such as listing code scanning alerts pours every field of every alert into the context; with gh-proxy and cli-proxy the agent works in bash, discovers each CLI through --help, writes the full response to a file and pipes out only the answer with jq, sort and head, so data grows on disk instead of in context.

#### Keywords

gh-proxy, cli-proxy, jq, bash, context window, MCP tools, tokens

#### Captions

- A head-to-head matchup tonight: CLI versus MCP, and your context window is the prize.
- A fat MCP call, like listing code scanning alerts, pours every field of every alert into context.
- That sounds expensive. So what is the alternative approach?
- With gh-proxy and cli-proxy, the agent saves the full response to a file and pipes out only the answer with jq.

### Inlined Skills & Agents {#inlined-skills-and-agents}

#### Notes

illustrative

#### Summary

Defer detailed instructions into inline skills and delegate focused work to smaller-model agents, with real workflow examples showing both patterns in use.

#### Keywords

skills, sub-agents, smaller models, delegation, prompt size

#### Captions

- Prompts getting too long? Here is a tip from the engineering desk.
- Defer detailed instructions into inline skills, and delegate focused work to agents running on smaller models.
- Does anyone actually work this way in practice?
- Real workflow examples show both patterns in use. Smaller prompts, smaller models, same results.

### Audit & Optimize {#audit-and-optimize}

#### Notes

illustrative

#### Summary

A health agent spots a failing or costly workflow, audits its unified session through the agentic-workflows skill, and tunes it so the next run is cheaper.

#### Keywords

audit, optimization, health, cost, tuning, failing workflow

#### Captions

- Tonight, a health agent that checks up on other workflows.
- It spots a failing or costly workflow, then audits its unified session through the agentic-workflows skill.
- A doctor for workflows, then. And what is the treatment?
- It tunes the workflow, so the next run is cheaper. Diagnosis and cure in one loop.

### Agentic Engineering {#agentic-engineering}

#### Summary

Band header (Agentic Engineering): skip needless runs and give agents memory, hand-offs, expiry, search and measurement.

#### Keywords

engineering, memory, handoffs, search, measurement

#### Captions

- Coming up next, our Agentic Engineering segment. Stay with us.
- We cover skipping needless runs, and giving agents memory, hand-offs, expiry, search and measurement.
- The nuts and bolts of agents that work well together. Stay tuned.

### Agents that debug agents {#agents-that-debug-agents}

#### Notes

illustrative

#### Summary

The agentic-workflows MCP server exposes logs, audit and audit-diff so any agent can debug, audit and optimize other workflows from unified sessions.

#### Keywords

debugging, logs, audit, audit-diff, MCP server, troubleshooting

#### Captions

- It sounds like science fiction, but it is here: agents that debug agents.
- The agentic-workflows MCP server exposes logs, audit and audit-diff tools to any agent.
- So any agent can investigate another workflow's problems?
- Exactly. It can debug, audit and optimize other workflows straight from their unified sessions.

### Any agent, any provider {#any-agent-any-provider}

#### Notes

illustrative

#### Summary

One Markdown workflow switches its coding agent and model provider with two front matter lines while keeping the same guardrails.

#### Keywords

engines, Copilot, Claude, Codex, Gemini, models, providers

#### Captions

- Switching agents used to mean a rewrite. Not anymore, our analyst reports.
- One Markdown workflow switches its coding agent and model provider with just two front matter lines.
- And the safety guardrails? Do they survive the switch?
- They stay exactly the same. Any agent, any provider, one set of guardrails.

### Don’t Run the Agent {#dont-run-the-agent}

#### Notes

illustrative

#### Summary

The cheapest agent call is the one never made: cheap deterministic gates from triggers to noop short-circuits run first, and an experimental model-based skip decision only when necessary.

#### Keywords

skip, gates, noop, skip-if, pre-activation, cheap checks

#### Captions

- Our top money-saving tip tonight is a simple one: do not run the agent.
- The cheapest agent call is the one never made. Cheap deterministic gates, from triggers to noop short-circuits, run first.
- And what if those cheap gates cannot decide on their own?
- Only then, if necessary, an experimental model-based skip decision steps in. Cheap checks first, agents last.

### Fuzzy schedules {#fuzzy-schedules}

#### Notes

illustrative

#### Summary

Fuzzy schedules such as daily hash each workflow to its own stable, off-peak time so runs never spike together; hourly, every N hours or minutes and weekly forms scatter too, while every N days and raw cron stay fixed; and the All You Can Eat pattern wakes often but skips while its last output is open, serving one item at a time as people consume them.

#### Keywords

cron, schedule, daily, hourly, off-peak, scatter, all you can eat

#### Captions

- Traffic report for your workflows: fuzzy schedules are keeping the roads clear.
- A daily schedule hashes each workflow to its own stable, off-peak time, so runs never spike together.
- Clever. Does every kind of schedule get scattered like that?
- Most do, though raw cron stays fixed. The All You Can Eat pattern wakes often but skips while its last output is open.

### Week of daily runs {#week-of-daily-runs}

#### Summary

A measured week of gh-aw's daily workflows by weekday and UTC hour shows the fuzzy scatter at fleet scale: nothing runs at 00 or 01 UTC, the quiet 02 to 05 UTC hours carry the most, weekends lighten as weekday-only reports drop out, and no hour holds more than a small share of the fleet.

#### Keywords

schedule, heatmap, UTC, weekday, fleet, runs, load

#### Captions

- We have the numbers on a measured week of gh-aw's own daily workflows.
- Plotted by weekday and UTC hour, the fuzzy scatter shows up at fleet scale. The earliest hours after midnight stay empty.
- So where does the load actually land during the week?
- The quiet early-morning UTC hours carry the most, weekends lighten, and no hour holds more than a small share.

### Agent memory {#agent-memory}

#### Notes

illustrative

#### Summary

Configured memories are restored read-only into each run, and trusted jobs persist cache, repository, comment and ledger memory afterward.

#### Keywords

memory, cache-memory, repo-memory, persistence, state

#### Captions

- Can agents remember things between runs? Our analyst has the details.
- Configured memories are restored read-only into each run, so the agent starts with what it knew before.
- Read-only, you say? Then who saves the new memories afterward?
- Trusted jobs persist cache, repository, comment and ledger memory after the run. The agent never writes directly.

### Memory with a History {#memory-with-a-history}

#### Notes

illustrative

#### Summary

Ledgers keep append-only records on their own branch, agents read a disposable SQLite projection and request changes, and trusted jobs validate and append them.

#### Keywords

ledger, SQLite, append-only, branch, history, state

#### Captions

- Next up on the engineering desk, agent memory that keeps its receipts.
- Ledgers keep append-only records on their own branch. Agents read a disposable SQLite projection of them.
- So can an agent quietly rewrite that history, then?
- No. Agents only request changes, and trusted jobs validate and append them. History stays intact.

### Durable execution {#durable-execution}

#### Notes

illustrative

#### Summary

Experimental: durable execution checkpoints the agent's native session, even after a failed step, so a rerun resumes the session instead of starting over.

#### Keywords

resume, checkpoint, rerun, session, retry, experimental

#### Captions

- Experimental, and very welcome: agents that pick up where they left off.
- Durable execution checkpoints the agent's native session, even after a failed step.
- So a rerun does not start from scratch?
- Right, a rerun resumes the session instead of starting over. Less wasted work, less waiting.

### Agents Trigger Agents {#agents-trigger-agents}

#### Notes

illustrative

#### Summary

Agents chain through safe-output dispatches with typed inputs and completion signals, each running inside its own workflow run.

#### Keywords

dispatch, workflow_dispatch, chaining, orchestration, call workflow

#### Captions

- Chain reaction tonight: agents are now triggering other agents.
- They chain through safe-output dispatches, with typed inputs and completion signals passed along.
- Does that mean one giant run doing everything?
- No, each agent runs inside its own workflow run. Orchestration without one big tangle.

### Ephemerals {#ephemerals}

#### Notes

illustrative

#### Summary

Safe outputs carry their own end, so old reports, discussions, comments and drafts close or collapse automatically and human attention is protected.

#### Keywords

expires, expiry, close, cleanup, hide, noise

#### Captions

- Good news for your notifications: some safe outputs now come with an expiry date.
- Old reports, discussions, comments and drafts close or collapse automatically when their time is up.
- So that means less clutter in everyone's inbox, then?
- Exactly. Safe outputs carry their own end, and human attention is protected from the noise.

### Index & search {#index-and-search}

#### Notes

illustrative

#### Summary

Experimental: a non-agentic job builds a search index and the agent gets read-only query access, cutting the tokens spent reading whole files.

#### Keywords

search index, retrieval, tokens, query, experimental

#### Captions

- One more experimental feature before we go: search for your agents.
- A non-agentic job builds a search index, and the agent gets read-only query access to it.
- Why not just let the agent read the files?
- Reading whole files burns tokens. Querying the index cuts that cost while keeping the agent read-only.

### Imports & reuse {#imports-and-reuse}

#### Notes

illustrative

#### Summary

Shared Markdown components are written and reviewed once, imported by path or from another repository at a ref, and merged and hashed by the compiler.

#### Keywords

imports, shared components, includes, modules, reuse

#### Captions

- From the reuse desk tonight: agentic workflows can share building blocks instead of copying them around.
- Right. Shared Markdown components are written and reviewed once, then imported by path or from another repository at a ref.
- And what does the compiler do with all those pieces?
- It merges and hashes them, so teams reuse trusted parts and know exactly what went into each workflow.

### Skills {#skills}

#### Notes

illustrative

#### Summary

Skills are declared in front matter, pinned to a commit and installed before the agent starts; a triage sub-agent picks the ones that fit each pull request and every review comment cites the skill it applied.

#### Keywords

skills, SKILL.md, pinned, sub-agent, triage, review comments

#### Captions

- Our next story is about skills, and how agents pick the right ones for the job.
- Skills are declared in front matter, pinned to a commit and installed before the agent even starts.
- So how does the agent know which skills fit a given pull request?
- A triage sub-agent picks the fitting ones, and every review comment cites the skill it applied.

### Skills in review {#skills-in-review}

#### Summary

Because every review comment cites its skill, a skills reviewer's history can be measured: comments by skill week by week, what authors and Copilot did next, and skill inventory and conflict events exported to OpenTelemetry.

#### Keywords

skills reviewer, metrics, OpenTelemetry, inventory, conflicts

#### Captions

- Following up on skills, analysts can now grade the reviewer itself.
- Because every review comment cites its skill, you can track comments by skill week by week.
- Can you also see what happened after a comment landed?
- Yes, what authors and Copilot did next, plus skill inventory and conflict events exported to OpenTelemetry.

### Experiments, graders & evals {#experiments-graders-and-evals}

#### Notes

illustrative

#### Summary

Experimental: A/B experiments assign a workflow variant to each run, graders measure every run, and evals judge which variant is better.

#### Keywords

A/B testing, experiments, graders, evals, variants, grading

#### Captions

- From the lab, an experimental feature: running A/B tests on agentic workflows.
- Experiments assign a workflow variant to each run, and graders measure every single run.
- So who gets to decide which version actually wins the matchup?
- Evals do. They judge which variant is better, so improvements are measured, not guessed.

### Turn Intent into Value {#turn-intent-into-value}

#### Notes

illustrative

#### Summary

Experimental: the same Markdown that becomes automation also defines value: a design agent extracts its goal, what counts, how to check and what it ships into one deterministic value function, frozen before any results are seen.

#### Keywords

value function, intent, goals, metrics, acceptance criteria, deterministic measurement, experimental

#### Captions

- Now an experimental twist: the same Markdown that drives automation can also define its value.
- A design agent extracts the goal, what counts, how to check it and what it ships.
- And all of that turns into one value function?
- One deterministic value function, frozen before any results are seen, so nobody moves the goalposts later.

### Deliver · Accept · Attribute {#deliver-accept-attribute}

#### Notes

illustrative

#### Summary

Experimental: one workflow pull request, followed on its GitHub timeline, earns attributed value only after it is delivered, a person accepts it, equal windows around the merge show the outcome change and its files survive the whole window: event-aligned, not sole causation.

#### Keywords

attribution, pull request, acceptance, merge window, outcome measurement, survival, experimental

#### Captions

- Still in the experimental corner, tonight we follow a single workflow pull request along its timeline.
- It earns attributed value only after it is delivered and a person actually accepts it.
- That sounds strict. What else has to happen?
- Equal windows around the merge must show the outcome change, and its files must survive the whole window.

### From Delivery to Value {#from-delivery-to-value}

#### Summary

Experimental: the full record of a workflow puts every opened and merged pull request, the all-author outcome line and its before and after windows on one timeline: a strong outcome, with value attributed only where every gate passes.

#### Keywords

historical replay, attribution window, pull requests, outcomes, survival, value measurement, experimental

#### Captions

- Zooming out now, from one pull request to a workflow's full record.
- Every opened and merged pull request lands on one timeline, with the all-author outcome line and its before and after windows.
- So a strong outcome automatically counts as the workflow's win?
- Not quite. Value is attributed only where every gate passes. Careful accounting, still experimental.

### Enterprise {#enterprise}

#### Summary

Band header (Enterprise): any agent, any provider, any scale.

#### Keywords

enterprise, scale, organization, governance, admins

#### Captions

- Coming up next, stay with us for our Enterprise segment.
- This one is about running agentic workflows with any agent, any provider, at any scale.
- Stay with us, admins, this segment is for you.

### Infrastructure as code {#infrastructure-as-code}

#### Notes

illustrative

#### Summary

Workflows compile to plain Actions YAML, so they run on any runner, reuse the Actions ecosystem and keep enterprise controls, with the agent behind the firewall.

#### Keywords

YAML, IaC, runners, actions, enterprise controls, compile

#### Captions

- Enterprise news: agentic workflows are infrastructure as code at heart.
- They compile to plain Actions YAML, so they run on any runner and reuse the Actions ecosystem.
- What about the controls enterprises already depend on?
- Those stay in place, and the agent keeps running behind the firewall.

### OpenTelemetry {#opentelemetry}

#### Notes

illustrative

#### Summary

Agentic workflows export OpenTelemetry traces to collectors you run, configured per workflow or as an organization default.

#### Keywords

OTel, OTLP, traces, telemetry, observability, collector

#### Captions

- On the observability beat, agentic workflows speak OpenTelemetry.
- They export traces to collectors you run yourself, not to someone else's backend.
- Is that something teams configure workflow by workflow, then?
- Either per workflow or as an organization default, whichever fits your team.

### Distributed tracing {#distributed-tracing}

#### Notes

illustrative

#### Summary

One shared trace follows a task from its workflow runs and jobs through agent sessions, tool calls, retries and dispatched runs in other repositories, with outcomes and cost on every span.

#### Keywords

traces, spans, OpenTelemetry, cross-repo, cost, observability

#### Captions

- Tracing a task from start to finish, our next story follows one shared trace.
- That trace covers workflow runs and jobs, agent sessions, tool calls, retries and dispatched runs in other repositories.
- And can you see what each step cost?
- Yes, outcomes and cost are recorded on every span, so nothing hides in the gaps.

### GitHub-native governance {#github-native-governance}

#### Notes

illustrative

#### Summary

Defaults and policy variables are set at enterprise, organization or repository scope and enforced with GitHub controls such as protected environments.

#### Keywords

policy, variables, organization, protected environments, defaults, admins

#### Captions

- Governance now, and the headline is that it stays GitHub-native.
- Defaults and policy variables can be set at enterprise, organization or repository scope.
- And who actually enforces those rules once they are set?
- GitHub controls do, such as protected environments, the same tools admins already trust.

### Operator-owned infrastructure {#operator-owned-infrastructure}

#### Notes

illustrative

#### Summary

Operators run agentic jobs on their own Linux runners, on Kubernetes, or in air-gapped networks with mirrored actions and container images.

#### Keywords

self-hosted runners, Kubernetes, air-gapped, mirror, on-prem

#### Captions

- For teams that want to own the hardware, here is your infrastructure update.
- Operators can run agentic jobs on their own Linux runners or on Kubernetes.
- Even in locked-down networks with no outside access at all?
- Yes, air-gapped networks work too, using mirrored actions and container images.

### GitHub App permissions {#github-app-permissions}

#### Notes

illustrative

#### Summary

A GitHub App installed on selected repositories mints short-lived scoped tokens, keeping the agent read-only and leaving writes to trusted safe-output jobs in each repository.

#### Keywords

GitHub App, tokens, installation token, scoped, permissions, least privilege

#### Captions

- Turning to the security desk: how agentic workflows handle permissions with a GitHub App.
- The app is installed on selected repositories and mints short-lived, scoped tokens.
- So does the agent get to write with those tokens?
- No. The agent stays read-only, and writes are left to trusted safe-output jobs in each repository.

### Multi-repo PRs {#multi-repo-prs}

#### Notes

illustrative

#### Summary

One run checks out several repositories with a read-only GitHub App, the agent edits them without any token, and the safe-outputs job uses a second, write-capable GitHub App to open one pull request in each allowlisted repository.

#### Keywords

multi-repo, cross-repo, pull requests, GitHub App, checkout, allowlist

#### Captions

- Big changes across many repositories, and one run that handles them all.
- The run checks out several repositories with a read-only GitHub App, and the agent edits them without any token.
- Then who actually opens the pull requests at the end?
- The safe-outputs job, using a second write-capable GitHub App, opens one per allowlisted repository.

### OIDC token broker {#oidc-token-broker}

#### Notes

illustrative

#### Summary

Experimental: a deployment-owned broker verifies a job's OIDC identity and returns a short-lived credential, so no long-lived secret is stored in the repository.

#### Keywords

OIDC, credentials, broker, short-lived, secrets, identity

#### Captions

- From the security desk, an experimental way to keep long-lived secrets out of repositories.
- A deployment-owned broker verifies a job's OIDC identity before handing out anything.
- And what does the job get back once it checks out?
- A short-lived credential, so no long-lived secret has to be stored in the repository.

### Push SDLC {#push-sdlc}

#### Notes

illustrative

#### Summary

A task arrives with its own squad of human-led AI agents that join the developer's repository, pair with them, then step back and leave their decisions behind.

#### Keywords

squad, team, pairing, SDLC, AI agents, onboarding

#### Captions

- A different kind of team story now: help that arrives with the task.
- Each task comes with its own squad of human-led AI agents that join the developer's repository.
- So do those agents simply take over the work entirely?
- They pair with the developer, then step back and leave their decisions behind.

### Connectors {#connectors}

#### Notes

illustrative

#### Summary

Experimental: a connector declares everything a third-party service offers; a workflow imports it and selects only the reads and writes it needs.

#### Keywords

third-party, integrations, Slack, Jira, connectors, services

#### Captions

- Integration news tonight: experimental connectors for your favorite third-party services.
- A connector declares everything a third-party service offers, all in one place.
- Does every workflow get all of that access?
- No, a workflow imports the connector and selects only the reads and writes it needs.

### Webhook gateway {#webhook-gateway}

#### Notes

illustrative

#### Summary

Experimental: a hosted receiver, a gh cao research prototype, authenticates external and GitHub events, matches declared subscriptions and dispatches verified batches to a workflow as untrusted input.

#### Keywords

webhooks, external events, receiver, dispatch, integrations, CAO, gh cao, research prototype

#### Captions

- Events from outside GitHub are knocking, and an experimental webhook gateway answers the door.
- It is a hosted receiver, a gh cao research prototype, that authenticates external and GitHub events.
- And what happens once an incoming event has been verified?
- It matches declared subscriptions and dispatches verified batches to a workflow, still treated as untrusted input.

### Central Agentic Ops {#central-agentic-ops}

#### Summary

Band header (Central Agentic Ops): gh cao, a research prototype for operating agentic workflows across many repositories.

#### Keywords

CAO, gh cao, central, fleet, operations, research prototype

#### Captions

- Coming up next, our Central Agentic Ops segment.
- It covers gh cao, a research prototype for operating agentic workflows across many repositories.
- Think fleet operations at a whole new level. Stay tuned.

### Hyperscale Agentics {#hyperscale-agentics}

#### Notes

illustrative

#### Summary

Research prototype: at enterprise scale every agent run reports into telemetry, an operations database and a session data lake, so fleets are managed from aggregates.

#### Keywords

scale, fleet, telemetry, operations database, data lake

#### Captions

- Research prototype news now, from the world of hyperscale agentics.
- At enterprise scale, every agent run reports into telemetry, an operations database and a session data lake.
- So how does anyone keep track of that many runs?
- You manage fleets from aggregates, not by staring at individual runs.

### Centralized campaigns {#centralized-campaigns}

#### Notes

illustrative

#### Summary

Research prototype: a control repository installs and tracks campaigns that roll agentic processes out across an enterprise's repositories.

#### Keywords

campaigns, rollout, control repo, fleet, organization

#### Captions

- Rolling out change across a whole enterprise is our next research prototype story.
- A control repository installs and tracks campaigns across the enterprise's repositories.
- And what exactly do those campaigns carry out to repositories?
- Agentic processes, rolled out consistently instead of set up one repository at a time.

### Dispatcher & worker {#dispatcher-and-worker}

#### Notes

illustrative

#### Summary

Research prototype: a campaign dispatcher and its workers import one shared control component, the dispatcher hands each worker a single target repository, and each prompt keeps only its mission.

#### Keywords

dispatcher, orchestrator, worker, shared control, imports, dispatch-workflow, campaign anatomy, EU CRA, Cyber Resilience Act

#### Captions

- Inside a campaign now, a look at the dispatcher and its workers.
- In this research prototype, both import one shared control component.
- So how does the work actually get split up between them?
- The dispatcher hands each worker a single target repository, and each prompt keeps only its mission.

### AW maintenance {#aw-maintenance}

#### Notes

illustrative

#### Summary

Research prototype: maintenance loops keep a fleet of agentic workflows healthy by updating versions, fixing drifted lock files, clustering failures and optimizing tokens.

#### Keywords

maintenance, upgrades, lock files, drift, failures, tokens

#### Captions

- Even agentic workflows need upkeep, and maintenance loops are on the job in this research prototype.
- They keep a fleet healthy by updating versions and fixing lock files that have drifted.
- Do they do anything beyond those routine version updates?
- They also cluster failures and optimize tokens, so the fleet stays healthy and lean.

### Session data lake {#session-data-lake}

#### Notes

illustrative

#### Summary

Research prototype: with opt-in, redacted run sessions land in an immutable, queryable data lake kept separate from the dashboard store.

#### Keywords

data lake, sessions, redaction, analytics, storage

#### Captions

- Where do run sessions go after the run? Our research prototype desk reports.
- With opt-in, redacted run sessions land in an immutable, queryable data lake.
- Is that the same place the dashboards use?
- No, it is kept separate from the dashboard store by design.

### Ops data for Agents {#ops-data-for-agents}

#### Notes

illustrative

#### Summary

Research prototype: agents query operations data through a constrained dashboard language and a read-only MCP server to find and optimize the costliest workflows.

#### Keywords

dashboard, query language, MCP, operations, costliest workflows

#### Captions

- Agents analyzing operations data is our next research prototype story.
- They query it through a constrained dashboard language and a read-only MCP server.
- And what are those agents looking for in all that data?
- The costliest workflows, so they can find them and optimize them.

### AW factory {#aw-factory}

#### Summary

Band header (AW factory): how gh-aw builds itself with agentic workflows, from its measured project history to the safe output that lands agent changes as pull requests.

#### Keywords

dogfooding, factory, history, how gh-aw is built

#### Captions

- Coming up next, stay tuned for our AW factory segment.
- It shows how gh-aw builds itself with agentic workflows, from measured project history to agent changes landing as pull requests.
- A behind-the-scenes look at the factory floor. Stay with us.

### First commit to GA {#first-commit-to-ga}

#### Summary

Measured history of gh-aw from its first commit to general availability, showing how much of the work agents and workflows started.

#### Keywords

history, timeline, GA, general availability, commits, stats

#### Captions

- A look back now at gh-aw's journey from first commit to general availability.
- This is measured history, showing how much of the work agents and workflows started.
- So the project really was built with its own tools?
- That is the story the history tells, agentic workflows helping build agentic workflows.

### How PRs land {#how-prs-land}

#### Summary

Measured time to merge and size of gh-aw pull requests, compared by whether people, Copilot or workflows started them.

#### Keywords

pull requests, merge time, PR size, stats, Copilot, metrics

#### Captions

- How do pull requests actually land in gh-aw? The numbers are in.
- We measured time to merge and size of pull requests, split by who started them.
- So who are the contenders in this particular race?
- People, Copilot and workflows, compared side by side on speed and size.

### How workflows evolve {#how-workflows-evolve}

#### Summary

Measured history of how gh-aw's own workflow Markdown changed and grew over time.

#### Keywords

history, markdown growth, evolution, stats, workflows

#### Captions

- Workflows change over time too, and gh-aw kept the receipts.
- This is measured history of how gh-aw's own workflow Markdown changed and grew.
- So the workflows evolve like any other code?
- Exactly, they are edited, extended and improved, and the history shows that growth.

### Patch to PR {#patch-to-pr}

#### Notes

illustrative

#### Summary

The create-pull-request safe output turns an agent's local commits into a pull request through scanning, file policy checks and signed pushes, falling back to issues.

#### Keywords

create-pull-request, patch, signed commits, git push, safe output

#### Captions

- Back on the security desk, how an agent's commits safely become a pull request.
- The create-pull-request safe output turns local commits into a pull request through scanning and file policy checks.
- And can reviewers trust the pushes that come out of it?
- They are signed, and if a pull request cannot be created, it falls back to opening issues.

### GıtHub Agentic Workflows {#finale}

#### Notes

illustrative

#### Summary

Finale: a control repository coordinates small, guarded agentic processes across many repositories; processes, not prompts.

#### Keywords

finale, ending, closing, processes not prompts, control repo, thanks

#### Captions

- And that brings us to tonight's final story: GitHub Agentic Workflows.
- A control repository coordinates small, guarded agentic processes across many repositories.
- If viewers remember one thing, what should it be?
- Processes, not prompts. Thanks for watching, and good night from all of us.
