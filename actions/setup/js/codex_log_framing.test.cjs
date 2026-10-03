// @ts-check
import { describe, expect, it } from "vitest";
import { createRequire } from "module";

const req = createRequire(import.meta.url);
const { collectCodexJSONRecords } = req("./codex_log_framing.cjs");

describe("Codex JSON record provenance", () => {
  const result = { type: "result", num_turns: 1, usage: { input_tokens: 10 } };

  it.each(["api.fetch({}) success in 2ms:", "[2026-10-02T00:00:00Z] api.fetch({}) succeeded in 0.2s:", "echo test failed in 1ms:"])("excludes framed tool payloads after %s", outcome => {
    const payload = JSON.stringify([{ type: "result", num_turns: 999, nested: { text: 'escaped " braces [ }' } }], null, 2);
    expect(collectCodexJSONRecords(`${outcome}\n${payload}\n${JSON.stringify(result)}`)).toEqual([result]);
  });

  it.each([JSON.stringify(result), JSON.stringify([result]), JSON.stringify([result], null, 2)])("preserves genuine compatibility results and document arrays: %s", content => {
    expect(collectCodexJSONRecords(content)).toEqual([result]);
  });

  it("retains native lifecycle records after a framed single-line result", () => {
    const native = { type: "thread.started", thread_id: "real" };
    expect(collectCodexJSONRecords(`api.fetch({}) success in 1ms:\n${JSON.stringify(result)}\n${JSON.stringify(native)}`)).toEqual([native]);
  });
});
