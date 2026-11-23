import { afterEach, describe, expect, it, vi } from "vitest";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { collectLifecycle, LifecycleAPIError } from "./collect_lifecycle.cjs";

const temporary = [];
const now = new Date("2026-11-17T05:30:00Z");

function directories() {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "lifecycle-collector-test-"));
  temporary.push(root);
  const memoryDir = path.join(root, "memory");
  const metricsDir = path.join(root, "metrics");
  fs.mkdirSync(memoryDir);
  fs.mkdirSync(metricsDir);
  return { memoryDir, metricsDir, repository: "acme/repo", now };
}

function record(id, overrides = {}) {
  return {
    id,
    host: "github.com",
    repository: "acme/repo",
    workflow: ".github/workflows/test.lock.yml",
    run_id: 42,
    run_attempt: 1,
    type: "create_issue",
    delivery: { at: "2026-11-01T00:00:00Z" },
    adoption: { state: "pending", coverage: "available", human_verified: false },
    survival: { state: "unknown", coverage: "unobserved" },
    ...overrides,
  };
}

function writeRows(filename, records, version = 7) {
  fs.writeFileSync(filename, records.map(lifecycle => JSON.stringify({ schema_version: version, kind: "output_lifecycle", lifecycle }) + "\n").join(""));
}

function rows(filename) {
  return fs
    .readFileSync(filename, "utf8")
    .trim()
    .split("\n")
    .filter(Boolean)
    .map(line => JSON.parse(line).lifecycle);
}

function value(args, name) {
  return args[args.indexOf(name) + 1];
}

function refresh(args, observe = item => item) {
  const ledger = path.join(value(args, "--output"), "lifecycle.jsonl");
  const records = rows(ledger).map(observe);
  writeRows(ledger, records);
  return { retained_adoption: { records, refreshed: records.length, deferred: 0, coverage: records.some(row => row.adoption.coverage !== "available") ? "partial" : "complete" } };
}

function appendRecords(args, records) {
  const filename = path.join(value(args, "--output"), "lifecycle.jsonl");
  const current = new Map(rows(filename).map(item => [item.id, item]));
  for (const item of records) if (!current.has(item.id)) current.set(item.id, item);
  writeRows(filename, [...current.values()]);
}

afterEach(() => {
  for (const root of temporary.splice(0)) fs.rmSync(root, { recursive: true, force: true });
  vi.restoreAllMocks();
});

describe("deterministic retained lifecycle collection", () => {
  it("refreshes an older delivery after restoring only its ledger on a cold runner", () => {
    const options = directories();
    const original = record("old-delivery");
    writeRows(path.join(options.memoryDir, "lifecycle.jsonl"), [original], 8);
    const collect = vi.fn(args => {
      if (!args.includes("--retained")) return { runs: [] };
      return refresh(args, item => ({
        ...item,
        adoption: { state: "accepted", coverage: "available", human_verified: true, acceptance_kind: "explicit_human", at: "2026-11-16T00:00:00Z" },
        survival: { state: "observed", coverage: "snapshot" },
      }));
    });
    const result = collectLifecycle({ ...options, collect });
    expect(result.totals).toMatchObject({ delivered: 1, accepted_human: 1, accepted_delivery_fallback: 0, survival_observed: 1 });
    expect(result.coverage).toBe("complete");
    const retained = rows(path.join(options.memoryDir, "lifecycle.jsonl"))[0];
    expect(retained.id).toBe(original.id);
    expect(retained.delivery).toEqual(original.delivery);
    expect(fs.readdirSync(options.memoryDir).sort()).toEqual(["collection.json", "lifecycle.jsonl"]);
    expect(JSON.parse(fs.readFileSync(path.join(options.metricsDir, "lifecycle.json"), "utf8"))).toEqual(result);
    expect(collect.mock.calls.at(-1)[0]).toContain("--retained");
  });

  it("counts automation decisions separately from human and delivery-fallback adoption", () => {
    const options = directories();
    const actor = { login: "github-actions[bot]", type: "Bot" };
    const decisions = {
      "bot-merged": { state: "accepted", signal: "merged", coverage: "available", human_verified: false, acceptance_kind: "automation", actor, at: "2026-11-16T00:00:00Z" },
      "bot-closed": { state: "rejected", signal: "closed_without_merge", coverage: "available", human_verified: false, acceptance_kind: "automation", actor, at: "2026-11-16T00:00:00Z" },
    };
    writeRows(
      path.join(options.memoryDir, "lifecycle.jsonl"),
      Object.keys(decisions).map(id => record(id, { type: "create_pull_request" }))
    );
    const result = collectLifecycle({
      ...options,
      collect: args => (args.includes("--retained") ? refresh(args, item => ({ ...item, adoption: decisions[item.id] })) : { runs: [] }),
    });
    expect(result.totals).toMatchObject({ delivered: 2, accepted_human: 0, accepted_automation: 1, accepted_delivery_fallback: 0, rejected: 1, lifecycle: 0 });

    const verified = directories();
    writeRows(path.join(verified.memoryDir, "lifecycle.jsonl"), [record("bot-merged", { type: "create_pull_request" })]);
    expect(() =>
      collectLifecycle({
        ...verified,
        collect: args => (args.includes("--retained") ? refresh(args, item => ({ ...item, adoption: { ...decisions[item.id], human_verified: true } })) : { runs: [] }),
      })
    ).toThrow("Invalid retained adoption acceptance");
  });

  it("resumes a saved window and preserves its high-water mark across backward pagination", () => {
    const options = directories();
    writeRows(path.join(options.memoryDir, "lifecycle.jsonl"), []);
    const stateFile = path.join(options.memoryDir, "collection.json");
    fs.writeFileSync(
      stateFile,
      JSON.stringify({
        schema_version: 1,
        repository: options.repository,
        completed_until: "2026-11-10T00:00:00Z",
        window: { start_date: "2026-11-09T23:00:00Z", end_date: "2026-11-15T00:00:00Z", before_run_id: 100, before_date: "2026-11-13T00:00:00Z" },
        unavailable_runs: [],
      })
    );
    let batches = 0;
    const collect = vi.fn(args => {
      if (args.includes("--retained")) return refresh(args);
      batches++;
      if (batches === 1) return { runs: [], continuation: { before_run_id: 90, end_date: "2026-11-12T00:00:00Z" } };
      return { runs: [] };
    });
    collectLifecycle({ ...options, collect });
    expect(value(collect.mock.calls[0][0], "--start-date")).toBe("2026-11-09T23:00:00Z");
    expect(value(collect.mock.calls[0][0], "--before-run-id")).toBe("100");
    expect(value(collect.mock.calls[1][0], "--end-date")).toBe("2026-11-12T00:00:00Z");
    expect(JSON.parse(fs.readFileSync(stateFile, "utf8"))).toMatchObject({ completed_until: "2026-11-15T00:00:00Z", window: null });
    collect.mockClear();
    collectLifecycle({ ...options, collect });
    expect(value(collect.mock.calls[0][0], "--start-date")).toBe("2026-11-14T23:00:00.000Z");
  });

  it("keeps a failed discovery window retryable while still refreshing retained targets", () => {
    const options = directories();
    vi.spyOn(console, "error").mockImplementation(() => {});
    writeRows(path.join(options.memoryDir, "lifecycle.jsonl"), [record("old-delivery")]);
    const collect = vi.fn(args => {
      if (args.includes("--retained")) return refresh(args);
      throw new LifecycleAPIError("provider unavailable");
    });
    const result = collectLifecycle({ ...options, collect });
    expect(result.coverage).toBe("partial");
    expect(result.discovery_complete).toBe(false);
    expect(result.errors).toEqual(["provider unavailable"]);
    const state = JSON.parse(fs.readFileSync(path.join(options.memoryDir, "collection.json"), "utf8"));
    expect(state.completed_until).toBeNull();
    expect(state.window.start_date).toBe("2026-11-16T05:30:00.000Z");
    expect(result.totals.delivered).toBe(1);
  });

  it("retains a bounded prefix without advancing discovery, then finishes on replay", () => {
    const options = directories();
    const candidates = [record("first", { execution: { padding: "x".repeat(450_000) } }), record("second", { execution: { padding: "y".repeat(450_000) } })];
    const collect = vi.fn(args => {
      if (args.includes("--retained")) return refresh(args);
      appendRecords(args, candidates);
      return { runs: [{ run_id: 42 }], delivery: [{ run_id: 42, coverage: "complete" }] };
    });
    const first = collectLifecycle({ ...options, collect });
    expect(first.discovery_complete).toBe(false);
    expect(first.totals.delivered).toBe(1);
    const second = collectLifecycle({ ...options, collect });
    expect(second.discovery_complete).toBe(true);
    expect(second.totals.delivered).toBe(2);
    expect(new Set(rows(path.join(options.memoryDir, "lifecycle.jsonl")).map(item => item.id)).size).toBe(2);
  });

  it("retries unavailable artifacts without losing their run IDs or starving discovery", () => {
    const options = directories();
    let incomplete = true;
    const collect = vi.fn(args => {
      if (args.includes("--retained")) return refresh(args);
      if (args.includes("--stdin")) {
        expect(args).not.toContain("--max-github-api-rate-limit");
        return { runs: [{ run_id: 42 }], delivery: [{ run_id: 42, coverage: "complete" }] };
      }
      if (incomplete) {
        incomplete = false;
        return { runs: [{ run_id: 42 }], delivery: [{ run_id: 42, coverage: "unavailable" }] };
      }
      return { runs: [] };
    });
    const first = collectLifecycle({ ...options, collect });
    expect(first.unavailable_runs).toBe(1);
    expect(first.coverage).toBe("partial");
    const second = collectLifecycle({ ...options, collect });
    expect(second.unavailable_runs).toBe(0);
    expect(second.coverage).toBe("complete");
    expect(collect.mock.calls.find(([args]) => args.includes("--stdin"))[1]).toBe("42\n");
  });

  it("keeps a retry-only run queued until all of its deliveries fit durably", () => {
    const options = directories();
    writeRows(path.join(options.memoryDir, "lifecycle.jsonl"), []);
    const stateFile = path.join(options.memoryDir, "collection.json");
    fs.writeFileSync(
      stateFile,
      JSON.stringify({
        schema_version: 1,
        repository: options.repository,
        completed_until: "2026-11-15T00:00:00Z",
        window: null,
        unavailable_runs: [42],
      })
    );
    const candidates = [record("first", { execution: { padding: "x".repeat(450_000) } }), record("second", { execution: { padding: "y".repeat(450_000) } })];
    const collect = vi.fn(args => {
      if (args.includes("--retained")) return refresh(args);
      if (!args.includes("--stdin")) return { runs: [] };
      appendRecords(args, candidates);
      return { runs: [{ run_id: 42 }], delivery: [{ run_id: 42, coverage: "complete" }] };
    });
    const first = collectLifecycle({ ...options, collect });
    expect(first.totals.delivered).toBe(1);
    expect(first.unavailable_runs).toBe(1);
    expect(JSON.parse(fs.readFileSync(stateFile, "utf8")).unavailable_runs).toEqual([42]);
    const second = collectLifecycle({ ...options, collect });
    expect(second.totals.delivered).toBe(2);
    expect(second.unavailable_runs).toBe(0);
    expect(second.discovery_complete).toBe(true);
  });

  it("fails explicitly for a delivery that can never fit instead of replaying forever", () => {
    const options = directories();
    const original = record("retained");
    const ledgerFile = path.join(options.memoryDir, "lifecycle.jsonl");
    writeRows(ledgerFile, [original]);
    const collect = vi.fn(args => {
      appendRecords(args, [record("oversized", { execution: { padding: "x".repeat(800_000) } })]);
      return { runs: [{ run_id: 42 }], delivery: [{ run_id: 42, coverage: "complete" }] };
    });
    expect(() => collectLifecycle({ ...options, collect })).toThrow("One delivery exceeds the per-collection persistence budget");
    expect(rows(ledgerFile)).toEqual([original]);
    expect(fs.existsSync(path.join(options.memoryDir, "collection.json"))).toBe(false);
    expect(collect).toHaveBeenCalledTimes(1);
  });

  it("does not turn an unavailable recheck into current acceptance or rejection", () => {
    const options = directories();
    writeRows(path.join(options.memoryDir, "lifecycle.jsonl"), [
      record("accepted-before", {
        adoption: { state: "accepted", coverage: "unavailable", human_verified: true, acceptance_kind: "explicit_human", error: "provider unavailable" },
      }),
    ]);
    const result = collectLifecycle({
      ...options,
      collect: args => (args.includes("--retained") ? refresh(args) : { runs: [] }),
    });
    expect(result.totals).toMatchObject({ delivered: 1, accepted_human: 0, rejected: 0, unknown: 1 });
    expect(result.coverage).toBe("partial");
    expect(rows(path.join(options.memoryDir, "lifecycle.jsonl"))[0].adoption.state).toBe("accepted");
  });

  it("rejects a reported decision that was not persisted in the ledger", () => {
    const options = directories();
    const original = record("old-delivery");
    const ledgerFile = path.join(options.memoryDir, "lifecycle.jsonl");
    writeRows(ledgerFile, [original]);
    expect(() =>
      collectLifecycle({
        ...options,
        collect: args => {
          if (!args.includes("--retained")) return { runs: [] };
          const report = refresh(args);
          report.retained_adoption.records[0].adoption = { state: "accepted", coverage: "available", acceptance_kind: "explicit_human", human_verified: true };
          return report;
        },
      })
    ).toThrow("Lifecycle report does not match the retained ledger");
    expect(rows(ledgerFile)).toEqual([original]);
    expect(fs.existsSync(path.join(options.metricsDir, "lifecycle.json"))).toBe(false);
  });

  it("can finish discovery for the observed 618-run daily fleet within its bounded invocation", () => {
    const options = directories();
    const fleet = Array.from({ length: 618 }, (_, index) => record(`delivery-${index}`, { run_id: 10000 - index }));
    let discovered = 0;
    const result = collectLifecycle({
      ...options,
      collect: args => {
        if (args.includes("--retained")) return refresh(args);
        expect(value(args, "--artifacts")).toBe("delivery");
        const batch = fleet.slice(discovered, discovered + Number(value(args, "--count")));
        discovered += batch.length;
        appendRecords(args, batch);
        return {
          runs: batch.map(item => ({ run_id: item.run_id })),
          delivery: batch.map(item => ({ run_id: item.run_id, coverage: "complete" })),
          ...(discovered < fleet.length ? { continuation: { before_run_id: batch.at(-1).run_id, end_date: now.toISOString() } } : {}),
        };
      },
    });
    expect(result.discovery_complete).toBe(true);
    expect(result.totals.delivered).toBe(fleet.length);
    expect(result.coverage).toBe("complete");
  });

  it("does not interpret a missing restored ledger as verified zero deliveries", () => {
    const options = directories();
    fs.writeFileSync(
      path.join(options.memoryDir, "collection.json"),
      JSON.stringify({
        schema_version: 1,
        repository: options.repository,
        completed_until: "2026-11-15T00:00:00Z",
        window: null,
        unavailable_runs: [],
      })
    );
    const collect = vi.fn();
    expect(() => collectLifecycle({ ...options, collect })).toThrow("Lifecycle checkpoint has no retained ledger");
    expect(collect).not.toHaveBeenCalled();
    expect(fs.existsSync(path.join(options.metricsDir, "lifecycle.json"))).toBe(false);
  });

  it("does not advance discovery when a processed run has no delivery report", () => {
    const options = directories();
    expect(() =>
      collectLifecycle({
        ...options,
        collect: () => ({ runs: [{ run_id: 42 }], delivery: [] }),
      })
    ).toThrow("Missing delivery collection evidence");
    expect(fs.existsSync(path.join(options.memoryDir, "collection.json"))).toBe(false);
  });
});
