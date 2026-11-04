---
private: true
emoji: "🧪"
description: Smoke Claude inline sub-agent registration and pre-start model resolution
on:
  workflow_dispatch:
permissions:
  contents: read
name: Smoke Claude Sub Agents
strict: true
engine:
  id: claude
  bare: false
timeout-minutes: 10
features:
  gh-aw-detection: false
sandbox:
  agent:
    id: awf
safe-outputs:
  create-issue: false
  threat-detection: false
  report-failure-as-issue: false
  report-failed-jobs: false
  noop:
    report-as-issue: false
  missing-tool:
    create-issue: false
  missing-data:
    create-issue: false
  report-incomplete:
    create-issue: false
post-steps:
  - name: Assert declared sub-agent ran on its resolved model
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
        const worker = "marker-worker";
        const marker = "GH-AW-CLAUDE-SUBAGENT-MARKER-7F3A";
        const { writeUnifiedSession } = require(path.join(process.env.RUNNER_TEMP, "gh-aw/actions/unified_session.cjs"));
        writeUnifiedSession({ rootDir: root, engine: "claude" });
        const readJsonl = file => fs.readFileSync(file, "utf8").split("\n")
          .filter(line => line.trim().startsWith("{"))
          .flatMap(line => { try { return [JSON.parse(line)]; } catch { return []; } });
        assert.equal(process.env.SMOKE_EXECUTION, "success", "Claude execution failed");
        const events = readJsonl(path.join(root, "usage/aw_session.jsonl"));
        const stream = readJsonl(path.join(root, "agent-stdio.log"));

        // Pre-start resolution of the declared `model: small` worker.
        const resolution = events.find(event => event.type === "model.resolution" && event.data?.engine === "claude" && event.data?.role === worker);
        assert.ok(resolution, `${worker} has no model.resolution record`);
        assert.ok(["resolved", "exact"].includes(resolution.data.status), `${worker} model resolution ${resolution.data.status}`);
        assert.equal(resolution.data.configured_model, "small", `${worker} must declare model: small`);
        const model = resolution.data.resolved_model;
        assert.ok(model && model !== "small", `${worker} has no resolved model`);

        // The generated definition Claude Code loads carries the identity and the concrete id.
        const definition = fs.readFileSync(path.join(process.env.GITHUB_WORKSPACE, ".claude/agents", `${worker}.md`), "utf8");
        assert.match(definition, new RegExp(`^name:\\s*"?${worker}"?\\s*$`, "m"), `${worker} definition has no native name`);
        assert.ok(definition.includes(`model: ${JSON.stringify(model)}`), `${worker} definition does not carry ${model}`);

        // Native initialization registered the worker.
        const init = stream.find(record => record.type === "system" && record.subtype === "init");
        assert.ok(init?.agents?.includes(worker), `native initialization did not register ${worker}: ${JSON.stringify(init?.agents)}`);

        // The first agent action delegates to the worker, and that delegation succeeds.
        const toolUses = stream.filter(record => record.type === "assistant" && !record.parent_tool_use_id)
          .flatMap(record => record.message?.content || []).filter(item => item.type === "tool_use");
        const first = toolUses[0];
        assert.ok(first && ["Agent", "Task"].includes(first.name) && first.input?.subagent_type === worker, `first agent action must invoke ${worker}: ${JSON.stringify(first && { name: first.name, input: first.input })}`);
        const result = stream.filter(record => record.type === "user").flatMap(record => record.message?.content || [])
          .find(item => item.type === "tool_result" && item.tool_use_id === first.id);
        assert.ok(result && !result.is_error, `${worker} delegation failed: ${JSON.stringify(result?.content)}`);
        assert.ok(JSON.stringify(result.content).includes(marker), `${worker} did not return the marker`);

        // The started sub-agent is the worker on the resolved model, with no built-in fallback.
        const started = events.filter(event => event.type === "subagent.started");
        assert.ok(!started.some(event => event.data?.agentType === "general-purpose" || event.data?.agentName === "general-purpose"), "fell back to general-purpose");
        const invocation = started.find(event => event.data?.toolCallId === first.id);
        assert.ok(invocation && (invocation.data.agentName === worker || invocation.data.agentType === worker), `subagent.started does not name ${worker}`);
        const activity = events.filter(event => event.agentId === invocation.agentId);
        const configured = activity.filter(event => event.type === "subagent.configured");
        assert.ok(configured.length > 0 && configured.every(event => event.data?.model === model), `${worker} was not configured on ${model}: ${JSON.stringify(configured.map(event => event.data?.model))}`);
        assert.ok(activity.some(event => event.type === "subagent.completed"), `${worker} did not complete`);
        assert.ok(!activity.some(event => event.type === "subagent.failed"), `${worker} failed`);

        // Served-model evidence comes from the proxy. Claude Code's stream does not include the
        // worker's own assistant messages, only its prompt. Detection and routing are off in this
        // smoke, so a request on the resolved model can only come from the worker, as long as the
        // main agent never used that model.
        const mainModels = stream.filter(record => record.type === "assistant" && !record.parent_tool_use_id).map(record => record.message?.model);
        assert.ok(!mainModels.includes(model), `main agent also used ${model}; served-model evidence is ambiguous`);
        const proxyPaths = [
          "sandbox/firewall/logs/api-proxy-logs/token-usage.jsonl",
          "sandbox/firewall/audit/api-proxy-logs/token-usage.jsonl",
          "sandbox/firewall-audit-logs/api-proxy-logs/token-usage.jsonl",
        ];
        const proxyFile = proxyPaths.map(file => path.join(root, file)).find(file => fs.existsSync(file));
        assert.ok(proxyFile, "Missing api-proxy token usage evidence");
        const onModel = readJsonl(proxyFile).filter(request => request.model === model);
        assert.ok(onModel.length > 0, `${worker}: the proxy served no request on ${model}`);
        // token-usage.jsonl records only 2xx responses; AWF writes failed requests to the sibling
        // upstream-errors.jsonl, which exists only when a request failed.
        const errorFile = path.join(path.dirname(proxyFile), "upstream-errors.jsonl");
        const failed = fs.existsSync(errorFile) ? readJsonl(errorFile).filter(record => record.model === model) : [];
        assert.equal(failed.length, 0, `${worker}: a request on ${model} failed: ${JSON.stringify(failed.map(record => record.status))}`);
---

# Smoke Test: Claude Inline Sub-Agents

This is a read-only smoke test. Do not modify any files and do not use any other tools before the delegation below.

1. Your **first action** must be a single foreground call of the `Agent` tool with `subagent_type` set to `marker-worker` and the prompt `marker?`. Do not run it in the background.
2. Do not use `general-purpose` or any other agent, and do not answer on the worker's behalf. If `marker-worker` is unavailable, call the `noop` safe output with a message saying it is unavailable, then stop.
3. Finish by calling the `noop` safe output once, with the worker's exact response as the message. Do not publish anything else.

## agent: `marker-worker`
---
description: Returns a fixed marker for the Claude sub-agent smoke test
model: small
---
When asked `marker?`, reply with exactly:

`GH-AW-CLAUDE-SUBAGENT-MARKER-7F3A`

No extra words, punctuation, formatting, or tool calls.
