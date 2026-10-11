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
    variants: [inline_strict, delegated_sequential, single_agent_control]
    description: "Measure whether inline sub-agent orchestration is the best reliability/cost tradeoff for a model-identity smoke test."
    hypothesis: "H0: no change in pass_rate between inline_strict and delegated_sequential. H1: delegated_sequential improves pass_rate by >= 0.15 absolute versus inline_strict. Note: single_agent_control is a synthetic negative baseline (always FAIL by design) excluded from H1 comparisons."
    metric: pass_rate
    secondary_metrics: [run_duration_seconds, output_validity_rate]
    guardrail_metrics:
      - name: empty_output_rate
        direction: min
        threshold: 0.01
      - name: false_pass_rate
        direction: min
        threshold: 0.05
    min_samples: 30
    weight: [34, 33, 33]
    start_date: "2026-07-23"
    end_date: "2026-10-23"
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
  - id: sub_agent_strategy_goal_met
    question: Does the agent output show that the objective for experiment sub_agent_strategy was successfully completed?

sandbox:
  agent:
    id: awf
post-steps:
  - name: Assert sub-agent model and wire API evidence
    if: always()
    uses: actions/github-script@v9.0.0
    env:
      SMOKE_VARIANT: ${{ needs.activation.outputs.sub_agent_strategy }}
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
        if (process.env.SMOKE_VARIANT === "single_agent_control") {
          assert.equal(started.length, 0, "Control variant must not invoke sub-agents");
          return;
        }
        assert.equal(process.env.SMOKE_EXECUTION, "success", "Copilot SDK execution failed");
        const proxyPaths = [
          "sandbox/firewall/logs/api-proxy-logs/token-usage.jsonl",
          "sandbox/firewall/audit/api-proxy-logs/token-usage.jsonl",
          "sandbox/firewall-audit-logs/api-proxy-logs/token-usage.jsonl",
        ];
        const proxyFile = proxyPaths.map(file => path.join(root, file)).find(file => fs.existsSync(file));
        assert.ok(proxyFile, "Missing api-proxy token usage evidence");
        const requests = readJsonl(proxyFile);
        const expected = [
          ["haiku-whoami", "claude-haiku-4.5", "/chat/completions"],
          ["mini-whoami", "gpt-5-mini", "/responses"],
          ["nano-whoami", "gpt-5-nano", "/responses"],
        ];
        const servedModel = model => String(model || "")
          .replace(/^copilot(?:-(?:responses|completions))?(?:-\d+)?\//, "")
          .replace(/-[0-9]{4}(?:-[0-9]{2}-[0-9]{2}|[0-9]{4})$/, "")
          .replace(/^(claude-.+)-([0-9]+)-([0-9]+)$/, "$1-$2.$3");
        for (const [agentName, model, endpoint] of expected) {
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

{{#if experiments.sub_agent_strategy == 'single_agent_control' }}
1. Do not call any sub-agent.
2. Produce the issue in the same format, but mark each agent as `not_invoked`.
3. Set overall status to FAIL.
4. Do not use unnecessary tool calls.
{{/if}}
{{#if experiments.sub_agent_strategy == 'delegated_sequential' }}
1. Call `haiku-whoami`, `mini-whoami`, and `nano-whoami` exactly once each.
2. Ask each sub-agent exactly this question: `who am i?`
3. Check the exact responses:
   - `haiku-whoami` → `claude-haiku-4.5`
   - `mini-whoami` → `gpt-5-mini`
   - `nano-whoami` → `gpt-5-nano`
4. Execute and validate each sub-agent one at a time before moving to the next.
5. Do not use any other agent or any unnecessary tool calls.
{{/if}}
{{#if experiments.sub_agent_strategy == 'inline_strict' }}
1. Call `haiku-whoami`, `mini-whoami`, and `nano-whoami` exactly once each.
2. Ask each sub-agent exactly this question: `who am i?`
3. Check the exact responses:
   - `haiku-whoami` → `claude-haiku-4.5`
   - `mini-whoami` → `gpt-5-mini`
   - `nano-whoami` → `gpt-5-nano`
4. Validate all three results and keep the current inline orchestration behavior.
5. Do not use any other agent or any unnecessary tool calls.
{{/if}}

## Output

Always create an issue titled **"Smoke Test: Copilot Sub Agents - ${{ github.run_id }}"** with:
- One line per sub-agent showing expected value, actual value, and ✅/❌
- Overall status: PASS only if all three exact matches succeed, otherwise FAIL
- Experiment variant: `{{ experiments.sub_agent_strategy }}`
- Run URL: ${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ github.run_id }}

## agent: `haiku-whoami`
---
description: Returns the Haiku model identity for smoke testing
model: claude-haiku-4.5
---
When asked `who am i?`, reply with exactly:

`claude-haiku-4.5`

No extra words, punctuation, or formatting.

## agent: `mini-whoami`
---
description: Returns the GPT-5 mini model identity for smoke testing
model: gpt-5-mini
---
When asked `who am i?`, reply with exactly:

`gpt-5-mini`

No extra words, punctuation, or formatting.

## agent: `nano-whoami`
---
description: Returns the GPT-5 nano model identity for smoke testing
model: gpt-5-nano
---
When asked `who am i?`, reply with exactly:

`gpt-5-nano`

No extra words, punctuation, or formatting.