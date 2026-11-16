import { describe, expect, it } from "vitest";
import fs from "node:fs";
import { ADOPTION_REGISTRY_VERSION, evaluateSafeOutputDelivery, projectSafeOutputLifecycle, safeOutputAdoptionRules } from "./safe_output_adoption.cjs";

describe("safe-output adoption", () => {
  it("covers every built-in and compiler-managed output type", () => {
    const tools = JSON.parse(fs.readFileSync(new URL("./safe_outputs_tools.json", import.meta.url), "utf8")).map(tool => tool.name);
    const expected = [...new Set([...tools, "call_workflow", "comment_memory", "dispatch_repository", "dispatch_workflow"])].sort();
    expect(safeOutputAdoptionRules().map(rule => rule.type)).toEqual(expected);
    expect(expected).toHaveLength(55);
    expect(ADOPTION_REGISTRY_VERSION).toBe("gh-aw-v1.2.3-adoption-v1");
  });

  it.each([
    ["create_issue", "pending", "human_decision_pending", "adoption-registry"],
    ["add_comment", "accepted", "delivered", "delivery-rule"],
    ["dispatch_workflow", "accepted", "delivered", "delivery-rule"],
    ["noop", "not_applicable", "no_repository_value_effect", "adoption-registry"],
    ["custom_output", "unknown", "unsupported_safe_output", "adoption-registry"],
  ])("evaluates %s delivery as %s", (type, state, signal, source) => {
    expect(evaluateSafeOutputDelivery(type, "2026-11-16T12:00:00Z")).toMatchObject({ state, signal, source });
  });

  it.each([
    ["safe_output.result", "create_issue", undefined, undefined, "delivered", "pending"],
    ["safe_output.delivery", "add_comment", "success", undefined, "delivered", "accepted"],
    ["safe_output.delivery", "create_issue", "failed", undefined, "failed", "unknown"],
    ["safe_output.delivery", "noop", "staged", undefined, "staged", "not_applicable"],
    ["safe_output.delivery", "dispatch_workflow", "delegated", undefined, "deferred", "unknown"],
    ["safe_output.delivery", "merge_pull_request", "failed", "indeterminate", "unknown", "unknown"],
  ])("projects %s %s/%s as delivery=%s adoption=%s", (eventType, type, status, operation_outcome, deliveryState, adoptionState) => {
    expect(projectSafeOutputLifecycle({ type, status, operation_outcome }, eventType, "2026-11-16T12:00:00Z")).toMatchObject({
      deliveryState,
      adoptionState,
      adoptionRuleVersion: ADOPTION_REGISTRY_VERSION,
    });
  });
});
