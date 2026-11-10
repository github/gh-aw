---
private: true
emoji: "🧪"
description: Smoke Copilot SDK inline sub-agents
on:
  schedule: every 2 days
  workflow_dispatch:
permissions:
  contents: read
  copilot-requests: write
name: Smoke Copilot Sub Agents
strict: true
model: gpt-5.3-codex
engine:
  id: copilot
  copilot-sdk: true
  bare: true
max-tool-denials: 3
experiments:
  sub_agent_strategy:
    variants: [inline_strict, delegated_sequential]
    description: "Measure whether inline sub-agent orchestration is the best reliability/cost tradeoff for a model-identity smoke test. Both variants run the mandatory sub-agent contract; the experiment outcome is scored separately and never relaxes the smoke post-step gate."
    hypothesis: "H0: no change in the sub_agent_strategy_contract_met YES rate between inline_strict and delegated_sequential. H1: delegated_sequential improves it by >= 0.15 absolute versus inline_strict."
    metric: eval:sub_agent_strategy_contract_met
    secondary_metrics: [run_duration_seconds, output_validity_rate]
    guardrail_metrics:
      - name: empty_output_rate
        direction: min
        threshold: 0.01
      - name: false_pass_rate
        direction: min
        threshold: 0.05
    min_samples: 30
    weight: [50, 50]
    start_date: "2026-07-23"
    end_date: "2027-03-01"
    issue: 47551
safe-outputs:
  create-issue:
    expires: 2h
    group: true
    close-older-issues: true
    close-older-key: "smoke-copilot-sub-agents"
    labels: [automation, testing]
timeout-minutes: 10
features:
  gh-aw-detection: false
imports:
  - shared/reporting.md
evals:
  - id: sub_agent_strategy_contract_met
    question: Does the agent output show that the orchestration objective for experiment sub_agent_strategy was met, with all three sub-agents invoked exactly once and their exact identities validated using the assigned orchestration style?

sandbox:
  agent:
    id: awf
post-steps:
  - name: Assert sub-agent model and wire API evidence
    if: always()
    uses: actions/github-script@v9.0.0
    env:
      SMOKE_EXECUTION: ${{ steps.agentic_execution.outcome }}
    with:
      script: |
        const assert = require("node:assert/strict");
        const fs = require("node:fs");
        const path = require("node:path");
        const root = "/tmp/gh-aw";
        const { writeUnifiedSession } = require(path.join(process.env.RUNNER_TEMP, "gh-aw/actions/unified_session.cjs"));
        writeUnifiedSession({ rootDir: root, engine: "copilot" });
        const readJsonl = file => fs.readFileSync(file, "utf8")
          .split("\n").filter(line => line.trim()).map(line => JSON.parse(line));
        const events = readJsonl(path.join(root, "usage/aw_session.jsonl"));
        const started = events.filter(event => event.type === "subagent.started");
        // [agent, configured model, expected model.resolution status, endpoint]
        const expected = [
          ["haiku-whoami", "claude-haiku-4.5", "exact", "/chat/completions"],
          ["gpt5-whoami", "gpt-5", "resolved", "/responses"],
          ["luna-whoami", "gpt-6-luna", "exact", "/responses"],
        ];
        const expectedNames = new Set(expected.map(([agentName]) => agentName));
        const servedModel = model => String(model || "")
          .replace(/^copilot(?:-(?:responses|completions))?(?:-\d+)?\//, "")
          .replace(/-[0-9]{4}(?:-[0-9]{2}-[0-9]{2}|[0-9]{4})$/, "")
          .replace(/^(claude-.+)-([0-9]+)-([0-9]+)$/, "$1-$2.$3");
        const resolutionRecords = events.filter(event => event.type === "model.resolution" && event.data?.engine === "copilot");
        const executionSucceeded = process.env.SMOKE_EXECUTION === "success";
        // Check model resolution for every sub-agent first so a startup failure reports its cause.
        const servedModels = new Map();
        for (const [agentName, configuredModel, status] of expected) {
          const resolutions = resolutionRecords.filter(event => event.data.role === agentName);
          if (resolutions.length === 0 && !executionSucceeded) {
            const causes = resolutionRecords.filter(event => event.data.cause).map(event => `${event.data.role}: ${event.data.cause}`);
            assert.fail(`Copilot SDK execution failed (outcome: ${process.env.SMOKE_EXECUTION || "unknown"}) before ${agentName} model.resolution was recorded; recorded resolution causes: ${causes.join(", ") || "none"}`);
          }
          assert.equal(resolutions.length, 1, `${agentName} must have exactly one model.resolution record`);
          const resolution = resolutions[0].data;
          assert.equal(resolution.status, status, `${agentName} model.resolution status is ${resolution.status}, expected ${status} (cause: ${resolution.cause || "none"})`);
          assert.equal(resolution.configured_model, configuredModel, `${agentName} model.resolution is for ${resolution.configured_model}, expected ${configuredModel}`);
          const model = servedModel(resolution.resolved_model);
          assert.ok(model, `${agentName} has no resolved model`);
          assert.ok(![...servedModels.values()].includes(model), `${agentName} resolved to ${model}, which another sub-agent also uses, so its proxy requests are indistinguishable`);
          servedModels.set(agentName, model);
          if (status === "exact") assert.equal(model, configuredModel, `${agentName} resolved to ${model}, expected ${configuredModel}`);
        }
        assert.equal(process.env.SMOKE_EXECUTION, "success", "Copilot SDK execution failed");
        const unexpected = [...new Set(started.map(event => event.data?.agentName).filter(agentName => !expectedNames.has(agentName)))];
        assert.equal(unexpected.length, 0, `Unexpected sub-agent invocation(s): ${unexpected.join(", ")}`);
        assert.equal(started.length, expected.length, `Expected exactly ${expected.length} sub-agent invocations, found ${started.length}`);
        assert.equal(started[0]?.data.agentName, expected[0][0], `${expected[0][0]} must be the first sub-agent invoked`);
        const proxyPaths = [
          "sandbox/firewall/logs/api-proxy-logs/token-usage.jsonl",
          "sandbox/firewall/audit/api-proxy-logs/token-usage.jsonl",
          "sandbox/firewall-audit-logs/api-proxy-logs/token-usage.jsonl",
        ];
        const proxyFile = proxyPaths.map(file => path.join(root, file)).find(file => fs.existsSync(file));
        assert.ok(proxyFile, "Missing api-proxy token usage evidence");
        const requests = readJsonl(proxyFile);
        for (const [agentName, , , endpoint] of expected) {
          const model = servedModels.get(agentName);
          const invocations = started.filter(event => event.data.agentName === agentName);
          assert.equal(invocations.length, 1, `${agentName} must actually run exactly once`);
          const invocation = invocations[0];
          const invocationId = invocation.data.invocationId || invocation.agentId;
          assert.ok(invocationId, `${agentName} is missing an invocation ID`);
          const activity = events.filter(event => event.data?.invocationId === invocationId || event.agentId === invocationId);
          assert.ok(activity.some(event => event.type === "subagent.completed"), `${agentName} did not complete`);
          assert.ok(!activity.some(event => event.type === "subagent.failed"), `${agentName} failed`);
          const models = activity.flatMap(event => [
            event.data.model, event.data.selectedModel, event.data.resolvedModel, event.data.firstDispatchedModel,
          ]).filter(Boolean);
          assert.ok(models.length > 0, `${agentName} has no recorded model`);
          assert.ok(models.every(value => servedModel(value) === model), `${agentName} changed or fell back from ${model}`);
          assert.ok(requests.some(request =>
            servedModel(request.model) === model &&
            (request.path || request.endpoint) === endpoint &&
            Number(request.status) === 200 &&
            request.purpose !== "routing_classification"),
            `${agentName} has no successful ${model} request on ${endpoint}`);
        }
---

# Smoke Test: Copilot SDK Inline Sub-Agents

**IMPORTANT: Keep all outputs extremely short.**

## Tasks

These steps are mandatory in every run. Reporting an expected identity without actually executing the sub-agent through a `task` tool call does not satisfy this test.

1. **Required first step:** your first action in this run must be a `task` tool call. Call `haiku-whoami` with exactly this prompt: `who am i?`. Do this before any other tool call, including skills and safe outputs.
2. Then make exactly one `task` tool call each to `gpt5-whoami` and `luna-whoami`, each with exactly this prompt: `who am i?`. Do not call `haiku-whoami` again.
3. Check the exact responses:
   - `haiku-whoami` → `claude-haiku-4.5`
   - `gpt5-whoami` → `gpt-5`
   - `luna-whoami` → `gpt-6-luna`
4. Do not use any other agent or any unnecessary tool calls.

{{#if experiments.sub_agent_strategy == 'delegated_sequential' }}
Orchestration: Execute and validate each sub-agent one at a time before moving to the next.
{{/if}}
{{#if experiments.sub_agent_strategy == 'inline_strict' }}
Orchestration: Validate all three results and keep the current inline orchestration behavior.
{{/if}}

## Output

Always create an issue titled **"Smoke Test: Copilot Sub Agents - ${{ github.run_id }}"** with:
- One line per sub-agent showing expected value, actual value, and ✅/❌
- Overall status: PASS only if all three sub-agents actually ran exactly once and all three exact matches succeed, otherwise FAIL
{{#if experiments.sub_agent_strategy == 'delegated_sequential' }}
- Experiment variant: `delegated_sequential` (scored separately; it does not change the overall status)
{{/if}}
{{#if experiments.sub_agent_strategy == 'inline_strict' }}
- Experiment variant: `inline_strict` (scored separately; it does not change the overall status)
{{/if}}
- Run URL: ${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ github.run_id }}

## agent: `haiku-whoami`
---
description: Returns the Haiku model identity for smoke testing
model: claude-haiku-4.5
---
When asked `who am i?`, reply with exactly:

`claude-haiku-4.5`

No extra words, punctuation, or formatting.

## agent: `gpt5-whoami`
---
description: Returns the gpt-5 alias identity for smoke testing
model: gpt-5
---
When asked `who am i?`, reply with exactly:

`gpt-5`

No extra words, punctuation, or formatting.

## agent: `luna-whoami`
---
description: Returns the GPT-6 Luna model identity for smoke testing
model: gpt-6-luna
---
When asked `who am i?`, reply with exactly:

`gpt-6-luna`

No extra words, punctuation, or formatting.