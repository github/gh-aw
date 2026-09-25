import { describe, expect, it } from "vitest";
import { existsSync, readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { spawnSync } from "node:child_process";

const __dirname = dirname(fileURLToPath(import.meta.url));
const proofPath = resolve(__dirname, "../../../specs/rs05a-checkout-validator-safety.smt2");

function expectedResults(proof) {
  return [...proof.matchAll(/^; EXPECT: ([A-Za-z0-9_]+) (sat|unsat)$/gm)].map(match => ({
    name: match[1],
    expected: match[2],
  }));
}

function z3Available() {
  const result = spawnSync("z3", ["--version"], { encoding: "utf8" });
  return result.status === 0;
}

describe("RS-05a checkout validator Z3 proof", () => {
  it("keeps a checked Z3 proof artifact for the checkout validator safety invariants", () => {
    expect(existsSync(proofPath)).toBe(true);

    const proof = readFileSync(proofPath, "utf8");
    const expected = expectedResults(proof);

    expect(expected.map(result => result.name)).toEqual([
      "workflow_dispatch_requires_verified_non_fork",
      "checkout_requires_write_or_higher_permission",
      "centralized_dispatch_requires_platform_bot_identity",
      "centralized_dispatch_requires_command_or_label_marker",
      "centralized_dispatch_requires_originating_actor",
      "centralized_dispatch_rejects_router_self_propagation",
      "workflow_dispatch_rejects_cross_repository_aw_context",
      "workflow_dispatch_rejects_noncanonical_pr_number",
      "workflow_dispatch_uses_refs_pull_checkout",
      "non_dispatch_pr_trigger_allows_forked_runtime_after_trust",
    ]);
    expect(expected.filter(result => result.expected === "unsat")).toHaveLength(9);
    expect(expected.filter(result => result.expected === "sat")).toHaveLength(1);
  });

  it.runIf(z3Available())("proves every modeled unsafe checkout state is unreachable", () => {
    const proof = readFileSync(proofPath, "utf8");
    const expected = expectedResults(proof);
    const result = spawnSync("z3", [proofPath], { encoding: "utf8" });

    expect(result.stderr).toBe("");
    expect(result.status).toBe(0);

    const lines = result.stdout
      .trim()
      .split(/\r?\n/)
      .map(line => line.replace(/^"|"$/g, ""));

    const actual = new Map();
    for (let index = 0; index < lines.length; index += 2) {
      actual.set(lines[index], lines[index + 1]);
    }

    expect(Object.fromEntries(actual)).toEqual(Object.fromEntries(expected.map(entry => [entry.name, entry.expected])));
  });
});
