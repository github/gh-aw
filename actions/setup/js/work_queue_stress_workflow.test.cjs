import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import { minimatch } from "minimatch";
import { parseDocument } from "./work_queue_yaml.cjs";

const root = resolve(import.meta.dirname, "../../..");
const document = parseDocument(readFileSync(resolve(root, ".github/workflows/work-queue-stress.yml"), "utf8"), { version: "1.2", uniqueKeys: true });
if (document.errors.length) throw new AggregateError(document.errors, "Invalid work queue stress workflow");
const workflow = document.toJS();
const pins = JSON.parse(readFileSync(resolve(root, ".github/aw/actions-lock.json"), "utf8")).entries;

describe("local work queue stress CI", () => {
  it("runs only for work queue JavaScript pull requests and main pushes", () => {
    for (const event of ["pull_request", "push"]) {
      expect(workflow.on[event].paths).toContain("actions/setup/js/**/*work_queue*.cjs");
      expect(workflow.on[event].paths).toContain("actions/setup/js/**/*work_queue*.js");
      expect(workflow.on[event].paths).toContain(".github/scripts/work-queue-stress*.cjs");
      expect(workflow.on[event].paths).toContain(".github/workflows/work-queue-stress.yml");
      const paths = workflow.on[event].paths;
      for (const file of ["actions/setup/js/work_queue_store.cjs", "actions/setup/js/finish_work_queue_claim.cjs", "actions/setup/js/subdir/work_queue_helper.js", ".github/scripts/work-queue-stress-worker.cjs"]) {
        expect(paths.some(pattern => minimatch(file, pattern))).toBe(true);
      }
      for (const file of ["actions/setup/js/start_mcp_gateway.cjs", "actions/setup/js/unrelated.js", "docs/README.md"]) {
        expect(paths.some(pattern => minimatch(file, pattern))).toBe(false);
      }
    }
    expect(workflow.on.push.branches).toEqual(["main"]);
    expect(workflow.on.pull_request_target).toBeUndefined();
    expect(Object.keys(workflow.jobs)).toEqual(["stress"]);
    expect(workflow.jobs.stress.needs).toBeUndefined();
  });

  it("uses read-only checkout, pinned actions, bounded execution and failure diagnostics", () => {
    expect(workflow.permissions).toEqual({ contents: "read" });
    expect(workflow.defaults.run.shell).toBe("bash");
    expect(workflow.jobs.stress["timeout-minutes"]).toBe(20);
    const steps = workflow.jobs.stress.steps;
    const checkout = steps.find(step => step.uses?.startsWith("actions/checkout@"));
    expect(checkout.with["persist-credentials"]).toBe(false);
    for (const step of steps.filter(step => step.uses)) {
      const [repository, sha] = step.uses.split("@");
      expect(sha).toMatch(/^[a-f0-9]{40}$/);
      expect(Object.values(pins).some(pin => pin.repo === repository && pin.sha === sha)).toBe(true);
    }
    const upload = steps.find(step => step.uses?.startsWith("actions/upload-artifact@"));
    expect(upload.if).toBe("always()");
    expect(upload.with.path).toBe("stress-results/");
    expect(upload.with["if-no-files-found"]).toBe("error");
  });

  it("separates retained-history scale from live completions and admission limits", () => {
    expect(workflow.jobs.stress.env).toEqual({
      HISTORY_ITEMS: "${{ github.event_name == 'pull_request' && '256' || '100000' }}",
      LIFECYCLE_ITEMS: "${{ github.event_name == 'pull_request' && '16' || '1024' }}",
    });
    const commands = workflow.jobs.stress.steps.filter(step => step.run).map(step => step.run);
    expect(commands.some(command => command.includes('--mode history --items "$HISTORY_ITEMS" --workers 2') && command.includes("--timeout-seconds 300"))).toBe(true);
    expect(commands.some(command => command.includes('--items "$LIFECYCLE_ITEMS" --workers 4 --queue-items 64') && command.includes("--timeout-seconds 600"))).toBe(true);
    expect(commands.some(command => command.includes("--mode saturation --items 100000 --workers 4") && command.includes("--timeout-seconds 60"))).toBe(true);
    expect(commands.some(command => command.includes("node --test .github/scripts/work-queue-stress.test.cjs"))).toBe(true);
  });

  it("publishes a human-readable performance summary even when a profile fails", () => {
    const steps = workflow.jobs.stress.steps;
    const summary = steps.find(step => step.run?.includes("work-queue-stress-summary.cjs"));
    expect(summary.if).toBe("always()");
    for (const name of ["invariants", "history", "lifecycle", "saturation"]) {
      expect(steps.some(step => step.id === name)).toBe(true);
      expect(summary.env[`${name.toUpperCase()}_OUTCOME`]).toBe(`\${{ steps.${name}.outcome }}`);
    }
    expect(steps.indexOf(summary)).toBeLessThan(steps.findIndex(step => step.uses?.startsWith("actions/upload-artifact@")));
    expect(steps.find(step => step.id === "invariants").run).toContain(".github/scripts/work-queue-stress-summary.test.cjs");
    expect(workflow.on.pull_request.paths.some(pattern => minimatch(".github/scripts/work-queue-stress-summary.cjs", pattern))).toBe(true);
  });
});
