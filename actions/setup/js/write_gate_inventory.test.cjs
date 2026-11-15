// @ts-check

import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { createHash } from "node:crypto";
import { beforeEach, afterEach, describe, it, expect, vi } from "vitest";
import { DECISIONS } from "./write_gate_decisions.cjs";
import { buildWriteDecisionInventory, writeDecisionInventory, readDecisionInventory } from "./write_gate_inventory.cjs";

describe("independent normalized write decision inventory", () => {
  const digest = "a".repeat(64);
  const items = DECISIONS.map((_, index) => ({ output_id: `so_${String(index).padStart(12, "0")}`, item_sha256: "b".repeat(64), input_sha256: "c".repeat(64), complete: true, depends_on: [] }));
  let directory;
  let filename;
  beforeEach(() => {
    directory = fs.mkdtempSync(path.join(os.tmpdir(), "write-decision-inventory-"));
    filename = path.join(directory, "write-decisions.normalized.json");
  });
  afterEach(() => fs.rmSync(directory, { recursive: true, force: true }));

  it("preserves different decisions for same-type items independently of handler outcomes", () => {
    const verdict = { decision: "partial", reason: "items_checked", items: items.map((item, index) => ({ output_id: item.output_id, decision: DECISIONS[index], reason: `gate_${DECISIONS[index]}` })) };
    const inventory = buildWriteDecisionInventory({ items }, digest, verdict, vi.fn());
    expect(inventory.items.map(item => item.gate_decision)).toEqual(DECISIONS);
    const sha256 = writeDecisionInventory(filename, inventory);
    expect(readDecisionInventory(filename, sha256, digest)).toEqual(inventory);
    expect(fs.statSync(filename).mode & 0o777).toBe(0o600);
    expect(() => writeDecisionInventory(filename, inventory)).toThrow();
  });

  it("drops missing, malformed, duplicate-restrictive and incomplete entries", () => {
    const verdict = {
      decision: "partial",
      reason: "items_checked",
      items: [
        { output_id: items[0].output_id, decision: "apply", reason: "clear" },
        { output_id: items[0].output_id, decision: "stage", reason: "staged" },
        { output_id: items[1].output_id, decision: "invalid", reason: "invalid" },
        { output_id: items[1].output_id, decision: "apply", reason: "clear" },
        { output_id: items[3].output_id, decision: "apply", reason: "clear" },
      ],
    };
    const pending = items.map((item, index) => ({ ...item, complete: index !== 3 }));
    const inventory = buildWriteDecisionInventory({ items: pending }, digest, verdict, vi.fn());
    expect(inventory.items.map(item => item.gate_decision)).toEqual(["stage", "drop", "drop", "drop", "drop"]);
    expect(inventory.items[2].decision_reason).toBe("gate_item_missing");
    expect(inventory.items[3].decision_reason).toBe("gate_item_incomplete");
    expect(buildWriteDecisionInventory({ items }, digest, { decision: "failed", reason: "failure" }, vi.fn()).items.every(item => item.gate_decision === "drop")).toBe(true);
  });

  it("rejects tampering, missing digest pins and wrong pending manifests before use", () => {
    const inventory = buildWriteDecisionInventory({ items }, digest, { decision: "allow", reason: "clear" }, vi.fn());
    const sha256 = writeDecisionInventory(filename, inventory);
    expect(() => readDecisionInventory(filename, "", digest)).toThrow("integrity pin");
    expect(() => readDecisionInventory(filename, sha256, "d".repeat(64))).toThrow("pending manifest mismatch");
    fs.appendFileSync(filename, "\n");
    expect(() => readDecisionInventory(filename, sha256, digest)).toThrow("digest mismatch");
    expect(() => readDecisionInventory(path.join(directory, "missing.json"), sha256, digest)).toThrow();
  });

  it("rejects malformed normalized records even with a matching file digest", () => {
    const inventory = buildWriteDecisionInventory({ items }, digest, { decision: "allow", reason: "clear" }, vi.fn());
    inventory.items[0].gate_decision = "invalid";
    const bytes = JSON.stringify(inventory);
    fs.writeFileSync(filename, bytes);
    expect(() => readDecisionInventory(filename, createHash("sha256").update(bytes).digest("hex"), digest)).toThrow("Malformed normalized write decision item");
  });
});
