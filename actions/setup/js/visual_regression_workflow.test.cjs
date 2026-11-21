import { describe, expect, it } from "vitest";
import fs from "node:fs";
import path from "node:path";

const { buildCopilotSDKPermissionHandler } = require("./copilot_sdk_permissions.cjs");

const workflow = fs.readFileSync(path.resolve(__dirname, "../../../.github/workflows/visual-regression-checker.md"), "utf8");
const compiledWorkflow = fs.readFileSync(path.resolve(__dirname, "../../../.github/workflows/visual-regression-checker.lock.yml"), "utf8");

describe("visual regression workflow contract", () => {
  it("serves provisioned build artifacts on sandbox loopback instead of the runner host", () => {
    expect(workflow).toContain("run: npm ci");
    expect(workflow).toContain("run: npm run build");
    expect(workflow).toContain("`docs/node_modules`");
    expect(workflow).toContain("`docs/dist`");
    expect(workflow).toContain("npm --prefix docs run preview -- --host 127.0.0.1 --port 4321");
    expect(workflow).toContain("background/async mode");
    expect(workflow).toContain("--retry-max-time 25 --connect-timeout 2 --max-time 5` as one standalone call, bounded to 30 seconds");
    expect(workflow).not.toContain("host.docker.internal");
    expect(workflow).not.toContain("nohup");
    expect(workflow).toContain("verify it stopped through `list_bash`");
  });

  it("grants the exact readiness URL with options following the URL", () => {
    expect(workflow).toContain('- "curl http://localhost:4321/gh-aw/:*"');
    expect(compiledWorkflow).toContain('"shell(curl http://localhost:4321/gh-aw/:*)"');
    for (const source of [workflow, compiledWorkflow]) {
      expect(source).not.toContain("curl http://localhost:*");
    }
  });

  it("allows the readiness command through the SDK matcher only with the corrected grant", () => {
    const probe = workflow.match(/Poll `([^`]+)`/)[1];
    const config = JSON.parse(compiledWorkflow.match(/GH_AW_COPILOT_SDK_TOOL_CONFIG: '([^'\n]+)'/)[1]);
    const approveAll = () => ({ kind: "approve-once" });
    const correctedHandler = buildCopilotSDKPermissionHandler(config.permissions, approveAll);
    const oldHandler = buildCopilotSDKPermissionHandler({ allowedTools: ["shell(curl http://localhost:*)"] }, approveAll);

    for (const identifiers of [[], ["curl"], [probe]]) {
      const request = {
        kind: "shell",
        fullCommandText: probe,
        commands: identifiers.map(identifier => ({ identifier })),
      };
      expect(correctedHandler(request)).toEqual({ kind: "approve-once" });
      expect(oldHandler(request)).toEqual({
        kind: "reject",
        feedback: "Tool invocation is not allowed by workflow tool permissions.",
      });
    }
  });

  it("uses the provisioned CLI commands and all three viewports", () => {
    for (const command of ["open http://localhost:4321/gh-aw/", "resize 375 812", "screenshot --filename=", "snapshot", "close"]) {
      expect(workflow).toContain(`playwright-cli ${command}`);
    }
    expect(workflow).toContain("--full-page");
    expect(workflow).toContain("768 × 1024");
    expect(workflow).toContain("1440 × 900");
    expect(workflow).not.toMatch(/playwright-cli browser_/);
  });

  it("inspects a large accessibility tree with a saved snapshot and ranged reads, not a shell pipeline", () => {
    const capture = workflow.match(/`(playwright-cli snapshot --filename=(\/tmp\/gh-aw\/agent\/[\w.-]+))`/);
    expect(capture).not.toBeNull();
    const [, command, snapshotPath] = capture;
    const config = JSON.parse(compiledWorkflow.match(/GH_AW_COPILOT_SDK_TOOL_CONFIG: '([^'\n]+)'/)[1]);
    const handler = buildCopilotSDKPermissionHandler(config.permissions, () => ({ kind: "approve-once" }));
    expect(handler({ kind: "shell", fullCommandText: command }).kind).toBe("approve-once");
    expect(handler({ kind: "read", path: snapshotPath }).kind).toBe("approve-once");
    expect(workflow).toMatch(/native `view` tool.*`view_range: \[1, 60\]`.*to EOF/);
    expect(handler({ kind: "shell", fullCommandText: `cd /tmp/gh-aw/agent && grep -n heading ${snapshotPath} | head -80` }).kind).toBe("reject");
  });

  it("permits standalone readiness and browser calls while rejecting observed helper chains", () => {
    const config = JSON.parse(compiledWorkflow.match(/GH_AW_COPILOT_SDK_TOOL_CONFIG: '([^'\n]+)'/)[1]);
    const handler = buildCopilotSDKPermissionHandler(config.permissions, () => ({ kind: "approve-once" }));
    const readiness = workflow.match(/Poll `([^`]+)`/)[1];
    for (const command of [readiness, "playwright-cli open http://localhost:4321/gh-aw/"]) {
      expect(handler({ kind: "shell", fullCommandText: command }).kind).toBe("approve-once");
    }
    for (const command of ["for i in $(seq 1 15); do curl http://localhost:4321/gh-aw/; sleep 2; done", "mkdir -p /tmp/gh-aw/agent && playwright-cli open http://localhost:4321/gh-aw/", "ps -o pid,ppid,cmd -p 1943"]) {
      expect(handler({ kind: "shell", fullCommandText: command }).kind).toBe("reject");
    }
    expect(workflow).toContain("Use one standalone command per Bash call");
    expect(workflow).toContain("is pre-created; do not run `mkdir`");
    expect(workflow).toContain("On the first tool denial");
  });

  it("reports incomplete only when a denied capability blocks a required step", () => {
    const prompt = workflow.replace(/\s+/g, " ");
    expect(prompt).toContain(
      "On the first tool denial, stop using that capability; do not retry variants or suggest credential changes. If the denial blocks a required step, call `report_incomplete` with the exact command and missing evidence. If it blocks only optional verification, skip it, note the limitation in the summary, and do not call `report_incomplete`; continue the remaining required work."
    );
  });

  it("does not mistake missing baselines for failed capture or clean comparisons", () => {
    expect(workflow).toContain("Its absence must not block screenshots or accessibility inspection");
    expect(workflow).toContain("Record visual comparison as **unavailable**, never **unchanged**");
    expect(workflow).toContain("an unavailable baseline alone is not a capture failure");
    expect(workflow).toContain("Call `noop` only when actual baseline comparison established no differences");
    expect(workflow).toContain("Do not fetch a baseline from an unapproved location");
  });

  it("preserves credential isolation, output limits, and existing proxy policy", () => {
    expect(workflow.match(/persist-credentials: false/g)).toHaveLength(2);
    expect(workflow).toContain("add-comment:\n    max: 1");
    expect(workflow).toContain("Do not start a runner-host server, bind to `0.0.0.0`, install packages, or change proxy configuration");
    expect(workflow).not.toMatch(/sandbox:\s*\n\s*agent:\s*false/);
  });
});
