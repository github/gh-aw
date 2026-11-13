---
private: true
emoji: "🎭"
description: Explores agentic-workflows custom agent behavior by generating software personas and analyzing responses to common automation tasks
on: daily
max-daily-ai-credits: 10000
engine:
  id: claude
  model-provider: github
  model-routing:
    goal: cost
    mode: economy
permissions:
  contents: read
  actions: read
  copilot-requests: write
  issues: read
  pull-requests: read
experiments:
  # New cohort: whole-run AIC and duration now include one fixed prefix probe.
  sub_agent_strategy_prefix_v1:
    variants: [per_scenario, batch]
    description: "Test whether batch scenario testing reduces token costs vs. per-scenario sub-agent calls"
    hypothesis: "H0: no change in aic or duration. H1: batch reduces tokens by ≥20% and duration by ≥15% without quality loss"
    metric: aic
    secondary_metrics: [run_duration_minutes, scenarios_tested, output_quality_score]
    guardrail_metrics:
      - name: issue_created
        threshold: "==1"
      - name: scenarios_analyzed
        threshold: ">=3"
    min_samples: 14
    weight: [50, 50]
    start_date: "2026-11-13"
    analysis_type: t_test
    tags: [cost_optimization, token_efficiency, sub_agents]
# Token Budget Guardrails:
# - timeout: Reduced from 600 to 180 minutes for faster feedback
# - Prompt optimization: Reduced scenario testing scope (6-8 instead of 15-20)
# - Output limits: Concise documentation (<1000 words with progressive disclosure)
# - Target: 30-50% token reduction while maintaining quality
# Note: max-turns not available for default Copilot engine (Claude only)
tools:
  cli-proxy: true
  github:
    mode: local
  cache-memory: true
safe-outputs:
  create-issue:
    title-prefix: "Agent Persona Exploration - "
    labels: ["agent-research", "cookie"]
    max: 1
    close-older-issues: true
    expires: false
  threat-detection:
    engine: copilot
timeout-minutes: 180
imports:
  - shared/reporting.md


  - shared/otlp.md
  - shared/graders.md
features:
  gh-aw-detection: true
evals:
  - id: personas_generated
    question: Did the agent generate software personas for exploring custom agent behavior?
  - id: analysis_produced
    question: Was an analysis produced comparing agent responses across different automation tasks?
  - id: prefix_comprehension
    question: Did the agent compare one identical persona scenario under full and first-8000-UTF-8-byte guidance, record missing constraints/routes and quality deltas separately from invocation failures, and recommend density improvements supported by evidence?

---

# Agent Persona Explorer

Explore `agentic-workflows` behavior across worker personas and automation tasks.

## Execution contract

- Research only; never create/edit workflows. Keep the existing baseline and
  persona rotation; test 3-4 scenarios (all four initial-baseline scenarios).
- Test information availability as well as response quality. Copilot CLI
  prompt-file queries may expose only the first **8,000 UTF-8 bytes**.
  Run the [paired prefix probe](#paired-prefix-probe) on one selected scenario,
  holding task, persona, model and scoring rubric fixed.
- Inspect whether objective, constraints, task routes, verification and stop
  conditions fit within **7,500 bytes including frontmatter**. Prefer short
  grammatical imperatives and precise terms; never reward lost safety,
  ambiguous shorthand or lower bytes at the expense of correct behavior.
- Keep prefix-probe scores separate from primary scenario scores. Whole-run AIC
  and duration include probe overhead; do not compare this new experiment cohort
  with pre-probe runs. Failed invocations are unavailable evidence, never scores.
- Publish one concise findings issue even for partial results, or `noop` if no
  data could be gathered. Preserve history only after publication as specified
  below. Recommendations target `.github/aw/*.md`, not `AGENTS.md`.

## Your Mission

Systematically test the "agentic-workflows" custom agent to understand its capabilities, identify common patterns, and discover potential improvements in how it responds to various workflow creation requests. Each run should explore a **different slice** of the full persona space using `cache-memory` to remember what has already been covered.

## Full Persona Pool

The following 9 personas cover both technical and non-technical information workers:

1. **Backend Engineer** - Works with APIs, databases, deployment automation
2. **Frontend Developer** - Focuses on UI testing, build processes, deployment previews
3. **DevOps Engineer** - Manages CI/CD pipelines, infrastructure, monitoring
4. **QA Tester** - Automates testing, bug reporting, test coverage analysis
5. **Product Manager** - Tracks product features, reviews metrics, coordinates releases
6. **Program Manager** - Coordinates cross-team milestones, schedules, and dependency tracking
7. **Designer** - Manages design systems, accessibility checks, visual asset reviews
8. **Legal / Compliance** - Tracks license compliance, policy files, and security disclosures
9. **Information Worker** - Manages documentation, knowledge bases, meeting notes, and internal wikis

## Phase 1: Select Personas for This Run (3 minutes)

Use `cache-memory` to load the exploration history and select personas.

1. **Load history**: Read `/tmp/gh-aw/cache-memory/agent-persona-explorer/explored-personas.json` and check whether `/tmp/gh-aw/cache-memory/agent-persona-explorer/baseline-completed.json` exists.
   - If the file does not exist, treat the explored list as empty.
   - The stored value is a JSON object: `{ "explored": ["Backend Engineer", "DevOps Engineer", ...] }`

2. **Select personas**:
   - If `baseline-completed.json` does not exist, select Program Manager, Designer, and Legal / Compliance for the one-time baseline, regardless of the explored list.
   - Otherwise, pick up to 3 personas from the Full Persona Pool that are **not** in the explored list. If 1 or 2 remain, select those before resetting the rotation; reset the explored list only when no personas remain.
   - In normal rotation, prioritize non-technical personas (Program Manager, Designer, Legal / Compliance, Information Worker) when multiple options are available.

3. **Store selected personas in working memory** for use in Phase 2 and beyond.

For each selected persona, note:
- Role name
- Primary responsibilities
- Common pain points that could be automated

## Phase 2: Generate Automation Scenarios (5 minutes)

If `baseline-completed.json` does not exist, use these **same four scenarios from the previous run** without changing their IDs, tasks, contexts, or expected workflow types:

| ID | Persona | Task | Context | Expected Workflow Type |
|---|---|---|---|---|
| PM-1 | Program Manager | Publish a weekly cross-team milestone digest with blockers. | Project issues carry milestone and team labels. Surface owners and overdue dependencies. | Scheduled |
| DS-1 | Designer | Review PRs changing shared design tokens for a linked design reference. | Token files are maintained in the repository. Explain missing links and accessibility implications. | PR automation |
| LC-1 | Legal / Compliance | Review new PR dependencies for disallowed SPDX licenses. | Dependency manifests and an approved-license policy are in the repository. Flag uncertain licenses for human review. | PR automation |
| LC-2 | Legal / Compliance | Audit policy and disclosure documents for stale review dates. | Policy files carry owners and review dates. Open a review issue for overdue documents. | Scheduled |

Mark these as the initial baseline and do not generate replacement scenarios for this run.

Otherwise, for each selected persona, generate **2 representative automation tasks** that would be appropriate for agentic workflows:

**Format for each scenario (keep concise):**
```
Persona: [Role Name]
Task: [Brief task description - max 1 sentence]
Context: [1-2 sentences max]
Expected Workflow Type: [Issue automation / PR automation / Scheduled / On-demand]
```

**Example scenarios by persona:**
- Backend Engineer: "Automatically review PR database schema changes for migration safety"
- Frontend Developer: "Generate visual regression test reports when new components are added"
- DevOps Engineer: "Monitor failed deployment logs and create incidents with root cause analysis"
- QA Tester: "Analyze test coverage changes in PRs and comment with recommendations"
- Product Manager: "Weekly digest of completed features grouped by customer impact"
- Program Manager: "Weekly cross-team milestone status digest with blocked items highlighted"
- Designer: "Flag PRs that modify shared design tokens or component CSS without a matching Figma link"
- Legal / Compliance: "Scan new dependencies added in PRs for non-permissive SPDX licenses"
- Information Worker: "Weekly summary of stale documentation files not updated in the last 90 days"

Store all scenarios in cache memory.

## Phase 3: Test Agent Responses (15 minutes)

**Token Budget Optimization**: Test all 4 scenarios in the initial baseline. On later runs, test a **representative subset of 3-4 scenarios** from those generated (not all) to reduce token consumption and ensure budget remains for Phase 5 publishing.

**For each scenario analyzed, capture a structured record:**
```json
{
  "scenario_id": "persona-task-number",
  "invocation": {
    "method": "claude-code-inline-sub-agent",
    "status": "succeeded",
    "error": null
  },
  "recommendation": {
    "trigger": "suggested trigger",
    "tools": ["suggested tools"],
    "permissions": {"scope": "read"},
    "network_access": ["suggested domains or ecosystems"],
    "safe_outputs": ["suggested safe outputs"],
    "prompt": "concise summary of the suggested workflow prompt"
  },
  "scores": {
    "trigger_appropriateness": null,
    "tool_selection_accuracy": null,
    "security_practices": null,
    "prompt_clarity": null,
    "completeness": null
  },
  "notes": "notable patterns or issues"
}
```

Summarize the recommendation; do not include full YAML. For successful invocations, replace each score placeholder with an integer from 1 to 5. Set `invocation.method` to `claude-code-inline-sub-agent`. If invocation fails, set `invocation.status` to `failed`, record the error in `invocation.error`, and set both `recommendation` and `scores` to `null`. Never encode an invocation failure as a score.

**Assessment questions:** Does the suggestion include appropriate triggers (`on:`)? Correct tools (github, web-fetch, playwright, etc.)? Proper safe-outputs? Security best practices (minimal permissions, network restrictions)? A clear, actionable prompt?

{{#if experiments.sub_agent_strategy_prefix_v1 == 'batch' }}
Invoke the inline `persona-evaluator` Claude Code sub-agent once with all 3-4 selected scenarios. Require one recommendation per scenario, and then record and score each response using the structured template above.
{{else}}
Invoke the inline `persona-evaluator` Claude Code sub-agent once per selected scenario, presenting it as a request from that persona. Record and score each response using the structured template above.
{{/if}}

The `persona-evaluator` is dynamically materialized as a Claude Code sub-agent from this workflow. It follows the repository's `agentic-workflows` custom-agent guidance; do not try to invoke the `agentic-workflows` CLI or MCP server with a freeform scenario because that interface only manages workflows and does not accept prompts.

**Important**: 
- You are ONLY testing the agent's responses, NOT creating actual workflows
- **Keep responses focused and concise** - summarize findings instead of verbose descriptions
- Aim for quality over quantity - fewer well-analyzed scenarios are better than many shallow ones
- **If any tool call fails, record the error briefly, mark scoring as unavailable for that scenario, and move on to the next scenario** - do NOT retry or get stuck

### Paired prefix probe

Use one successfully evaluated scenario as the full-guidance baseline; request
one additional, fresh isolated `persona-evaluator` invocation with
`guidance_mode: prefix_8000` and the identical scenario. Pass no full-response
rationale or shared conversation history. Do not add a scenario or alter the four initial
baseline tasks. If none succeeded, report the probe unavailable without scores.

For each guidance file used, measure UTF-8 bytes from the file's start, including
metadata. Use the repository checker for structural measurements:
`python3 scripts/check-instruction-prefixes.py --json <guidance-paths>`.
A checker exit of 1 reports violations, not a scenario invocation failure;
record unreadable files or tool errors separately.

The prefix evaluator must receive only each file's raw `read_bytes()[:8000]`
view, never a summary derived from its unseen tail. If truncation splits a UTF-8
character, record that boundary and omit only the incomplete trailing character
for display. Load additional guidance only through routes visible in the prefix;
apply the same byte limit to each linked file. Do not inspect hidden tails to
repair the response. Avoid recursively loading every link.
Do not use loaders that inject whole files in prefix mode. If ambient or tool
context exposes hidden guidance, mark the probe contaminated/unavailable rather
than claiming an isolated prefix comparison.

Store a separate `prefix_probe` object with scenario ID, guidance paths and
revision, full byte counts, visible/omitted bytes, contract-end byte when present,
both invocation statuses, both five-dimension score sets, candidate missing
rules/routes, and full-versus-prefix score deltas. Use `null` for unavailable
scores/deltas; exclude this extra invocation from primary scenario counts and
quality averages, not whole-run AIC or duration.
Compare required trigger/scope, tools, read permissions, safe outputs, `noop`,
verification and safety/stop behavior. Distinguish observed response regressions
from static risks; passing the byte checker does not prove comprehension.

Recommend moving lost rules/routes ahead of examples, replacing duplicate
prose with conditional links, and compressing wording without changing meaning.
Report which required decisions the prefix can and cannot support.

## Phase 4: Analyze Results (4 minutes)

Review all captured responses and identify:

### Common Patterns (be concise - bullet points preferred)
- What triggers does the agent most frequently suggest?
- Which tools are commonly recommended?
- Are there consistent security practices being applied?

### Quality Insights (summarize briefly)
- Which scenarios received the best responses (average score > 4)?
- Which scenarios received weak responses (average score < 3)?
- If scenario invocation failed, note that scoring is unavailable for affected scenarios and exclude them from the numeric average.
- Only describe authoring guidance as proven when supported by successful scenario responses. If no responses were evaluated, state that no guidance was proven.

### Potential Issues (only list critical issues)
- Does the agent ever suggest insecure configurations?
- Are there cases where it misunderstands the task?

### Improvement Opportunities (top 3 only)
- What additional guidance could help the agent?
- Should certain patterns be more strongly recommended?
- **Important**: Any documentation recommendations must target `.github/aw/*.md` files (e.g., `github-agentic-workflows.md`, `create-agentic-workflow.md`). Do **not** reference or suggest changes to `AGENTS.md` — that file is Go developer documentation for the `gh-aw` codebase and is unrelated to agentic workflow instructions.

## Phase 5: Document and Publish Findings (1 minute)

**MANDATORY OUTPUT**: Regardless of how many phases completed successfully, you MUST call either the `create issue` or the `noop` safe-output tool before finishing. Failing to call a safe-output tool is the most common cause of workflow failures.

Create a GitHub issue with a **concise** summary report. Use the `create issue` safe-output to publish your findings. Even if only 1-2 scenarios were tested, create the issue with partial results. Treat invocation failures as a standard partial-results outcome and explicitly mark scoring as unavailable where applicable.

**Issue title**: "Agent Persona Exploration - [DATE]" (e.g., "Agent Persona Exploration - 2024-01-16")

**Issue content structure**:

Follow these formatting guidelines when creating your persona analysis report:

### 1. Header Levels
**Use h3 (###) or lower for all headers in persona analysis reports to maintain proper document hierarchy.**

### 2. Progressive Disclosure
**Wrap detailed examples and data tables in `<details><summary>Section Name</summary>` tags to improve readability.**

Example:
```markdown
<details>
<summary>View Communication Examples</summary>

[Detailed examples of agent outputs, writing style samples, tone analysis]

</details>
```

### 3. Report Structure Pattern

```markdown
### Persona Overview
- **Agent**: [name]
- **Personas This Run**: [3 persona names]
- **Scenarios Tested**: [count - 4 for the initial baseline; otherwise 3-4 selected from those generated in Phase 2]
- **Average Quality Score**: [X.X/5.0 or N/A when invocation/scoring is unavailable]

### Key Findings (3-5 bullet points max)
[High-level insights - keep concise]

### 8,000-Byte Prefix Probe
[Scenario ID; guidance byte counts and contract boundary; full/prefix scores or
N/A; observed lost rules/routes; one evidence-backed packing recommendation.
Distinguish static risks from observed regressions.]

### Top Patterns (3-5 items max)
1. [Most common trigger types]
2. [Most recommended tools]
3. [Security practices observed]

<details>
<summary>View High Quality Responses (Top 2-3)</summary>

- [Scenario that worked well and why - keep brief]

</details>

<details>
<summary>View Areas for Improvement (Top 2-3)</summary>

- [Specific issues found - be direct]
- [Suggestions for enhancement - actionable]

</details>

### Recommendations (Top 3 only)
1. [Most important actionable recommendation — if documentation-related, reference `.github/aw/*.md` files, NOT `AGENTS.md`]
2. [Second priority suggestion]
3. [Third priority idea]
```

Store the structured records in `/tmp/gh-aw/cache-memory/agent-persona-explorer/results-${{ github.run_id }}.json` for historical comparison across runs.

**Update the exploration history**: After publishing the issue, update `/tmp/gh-aw/cache-memory/agent-persona-explorer/explored-personas.json`:
- Append the personas tested in this run to the existing explored list
- If the updated list now contains all 9 personas, reset to an empty list (start a new rotation cycle)
- Store as: `{ "explored": ["Persona A", "Persona B", ...] }`
- If this was the initial baseline and all four scenario invocations succeeded, write `{ "completed": true, "scenario_ids": ["PM-1", "DS-1", "LC-1", "LC-2"] }` to `baseline-completed.json`. If any invocation failed, leave the marker absent so the exact baseline is retried next run.

**Output Efficiency Guidelines:**
- Keep the main report under 1000 words
- Use details/summary tags extensively to hide verbose content
- Focus on actionable insights, not exhaustive documentation
- Prioritize quality over comprehensiveness

## Important Guidelines

**Research Ethics:**
- This is exploratory research - you're analyzing agent behavior, not creating production workflows
- Be objective in your assessment - both positive and negative findings are valuable
- Look for patterns across multiple scenarios, not just individual responses

**Memory Management:**
- Use cache memory to preserve context between runs
- Store structured data that can be compared over time
- Keep summaries concise but informative

**Quality Assessment:**
- Rate each dimension (1-5) based on:
  - 5 = Excellent, production-ready suggestion
  - 4 = Good, minor improvements needed
  - 3 = Adequate, several improvements needed
  - 2 = Poor, significant issues present
  - 1 = Unusable, fundamental misunderstanding

**Continuous Learning:**
- Compare results across runs to track improvements
- Note if the agent's responses change over time
- Identify if certain types of requests consistently produce better results

## agent: `persona-evaluator`
---
name: persona-evaluator
description: Evaluates workflow scenarios using the repository's agentic-workflows authoring guidance
model: inherit
---
Act as the `agentic-workflows` custom agent. By default use full guidance:
read `.github/agents/agentic-workflows.md` and follow ad hoc evaluation in
`.github/aw/create-agentic-workflow.md`. If `guidance_mode: prefix_8000`, use
only the first 8,000 UTF-8 bytes of each guidance file, including metadata;
follow only routes visible there and apply the same bound to linked guidance.
Never read hidden tails or reconstruct them from other full-guidance results.
Return guidance paths, byte counts and unavailable routes with the recommendation.
Do not create/edit workflows or score your own response.

Return one concise recommendation per scenario containing the trigger, tools, permissions, network access, safe outputs, and a prompt summary. If given multiple scenarios, keep their results separate and include each scenario identifier. Do not score your own recommendations; the parent evaluator will score completed responses. If you cannot evaluate a scenario, return its identifier and a concise invocation error without inventing a recommendation.

## end agent: `persona-evaluator`

## Success Criteria

Your effectiveness is measured by:
- **Safe output**: ALWAYS call either `create issue` or `noop` — this is the most critical requirement
- **Efficiency**: Complete analysis within token budget (timeout: 180 minutes, concise outputs)
- **Quality over quantity**: Test 3-4 representative scenarios thoroughly rather than many scenarios superficially
- **Actionable insights**: Provide 3-5 concrete, implementable recommendations
- **Concise documentation**: Report under 1000 words with progressive disclosure
- **Consistency**: Maintain objective, research-focused methodology

Execute all phases systematically and maintain an objective, research-focused approach to understanding the agentic-workflows custom agent's capabilities and limitations.

**CRITICAL**: You MUST call a safe-output tool before finishing. Choose one:
1. Call `create issue` to publish findings (preferred — even partial results are valuable)
2. Call `noop` if you were completely unable to gather any data

```json
{"noop": {"message": "No action needed: [brief explanation of what was analyzed and why]"}}
```
