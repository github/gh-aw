import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { createFirewallLogReader, classifyFirewallSteeringRecord } from "./harness_firewall_logs.cjs";

let directory;
let file;
let log;
const line = (status = 403, decision = "TCP_DENIED:HIER_NONE", host = "PRIVATE.example:443") =>
  JSON.stringify({
    _schema: "audit/v0.26.0",
    ts: 1790899201.25,
    event: "http_access",
    host,
    status,
    decision,
    url: "https://PRIVATE.example/?secret=PRIVATE",
    user_agent: "PRIVATE",
  }) + "\n";
beforeEach(() => {
  directory = fs.mkdtempSync(path.join(os.tmpdir(), "firewall-steering-"));
  file = path.join(directory, "sandbox/firewall/audit/log.jsonl");
  fs.mkdirSync(path.dirname(file), { recursive: true });
  fs.writeFileSync(file, "");
  log = vi.fn();
});
afterEach(() => {
  vi.restoreAllMocks();
  fs.rmSync(directory, { recursive: true, force: true });
});
const reader = () => createFirewallLogReader({ env: { GH_AW_TMP_DIR: directory }, log });

describe("bounded trusted firewall evidence", () => {
  it.each([
    [403, "TCP_DENIED:HIER_NONE", "denied"],
    [0, "DENIED", "denied"],
    [403, "denied", "denied"],
    [407, "NONE_NONE:HIER_NONE", "denied"],
    [403, "NONE_NONE:HIER_NONE", "denied"],
    [0, "NONE_NONE:HIER_NONE", "warning"],
    [403, "TCP_MISS:HIER_DIRECT", undefined],
    [200, "NONE_NONE:HIER_NONE", undefined],
    [200, "TCP_DENIED:HIER_NONE", undefined],
    [502, "unknown", undefined],
  ])("classifies structured audit decisions (%s, %s)", (status, decision, expected) => {
    expect(classifyFirewallSteeringRecord(JSON.parse(line(status, decision)))).toBe(expected);
  });

  it.each(["-", "awmg-mcpg:8080", "awmg-cli-proxy:3128"])("does not mistake internal or placeholder traffic for an external denial: %s", host => {
    expect(classifyFirewallSteeringRecord(JSON.parse(line(403, "TCP_DENIED:HIER_NONE", host)))).toBeUndefined();
  });

  it("ignores tool output, quoted records, malformed text, and unrelated JSON", () => {
    for (const raw of [
      "permission denied",
      { type: "tool_result", content: line() },
      `"${line()}"`,
      "WARNING PRIVATE",
      "# comment",
      null,
      [],
      { ...JSON.parse(line()), _schema: "tool/v1" },
      { ...JSON.parse(line()), event: "tool_result" },
      { ...JSON.parse(line()), status: "403" },
      { ...JSON.parse(line()), status: -1 },
    ]) {
      expect(classifyFirewallSteeringRecord(raw)).toBeUndefined();
    }
  });

  it("baselines old records and counts each appended complete record only once", () => {
    fs.writeFileSync(file, line());
    const read = reader();
    expect(read()).toEqual({ warning: 0, denied: 0 });
    fs.appendFileSync(file, line() + line(0, "NONE_NONE:HIER_NONE"));
    expect(read()).toEqual({ warning: 1, denied: 1 });
    expect(read()).toEqual({ warning: 0, denied: 0 });
    expect(JSON.stringify(log.mock.calls)).not.toContain("PRIVATE");
  });

  it("retains incomplete appended records but never completes a stale baseline record", () => {
    fs.writeFileSync(file, line().trimEnd());
    const read = reader();
    fs.appendFileSync(file, "\n" + line().trimEnd());
    expect(read()).toEqual({ warning: 0, denied: 0 });
    fs.appendFileSync(file, "\n");
    expect(read()).toEqual({ warning: 0, denied: 1 });
  });

  it("detects log creation, truncation and inode rotation without replaying prior bytes", () => {
    fs.unlinkSync(file);
    const read = reader();
    expect(read()).toEqual({ warning: 0, denied: 0 });
    fs.writeFileSync(file, line() + line());
    expect(read().denied).toBe(2);
    fs.writeFileSync(file, line());
    expect(read().denied).toBe(1);
    fs.renameSync(file, `${file}.old`);
    fs.writeFileSync(file, line());
    expect(read().denied).toBe(1);
    expect(read().denied).toBe(0);
  });

  it("bounds tail reads and record size, then resumes at the next complete line", () => {
    const read = reader();
    fs.appendFileSync(file, "PRIVATE".repeat(20000) + "\n" + line());
    expect(read().denied).toBe(1);
    fs.appendFileSync(file, "PRIVATE".repeat(2000));
    expect(read().denied).toBe(0);
    fs.appendFileSync(file, "\n" + line());
    expect(read().denied).toBe(1);
    expect(log).toHaveBeenCalledWith(expect.stringContaining("tail_truncated"));
    expect(log).toHaveBeenCalledWith(expect.stringContaining("oversized_record_ignored"));
    expect(JSON.stringify(log.mock.calls)).not.toContain("PRIVATE");
  });

  it.each(["audit/audit.jsonl", "logs/log.jsonl", "logs/audit.jsonl"])("supports structured %s without following log file symlinks", relative => {
    fs.unlinkSync(file);
    const legacy = path.join(directory, "sandbox/firewall", relative);
    fs.mkdirSync(path.dirname(legacy), { recursive: true });
    fs.writeFileSync(legacy, "");
    const read = reader();
    fs.appendFileSync(legacy, line());
    expect(read().denied).toBe(1);
    fs.symlinkSync(legacy, file);
    expect(read().denied).toBe(0);
    expect(log).toHaveBeenCalledExactlyOnceWith(expect.stringContaining("unavailable_or_unsafe"));
    expect(read().denied).toBe(0);
    expect(log).toHaveBeenCalledOnce();
  });

  it("never opens or falls back to Squid logs, even when structured logs are missing", () => {
    fs.unlinkSync(file);
    const raw = path.join(directory, "sandbox/firewall/logs/squid-logs/access.log");
    fs.mkdirSync(path.dirname(raw), { recursive: true });
    const squid = '1790899201.25 172.30.0.20:1234 PRIVATE.example:443 1.2.3.4:443 1.1 CONNECT 403 TCP_DENIED:HIER_NONE https://PRIVATE.example/ "-"\n';
    fs.writeFileSync(raw, squid + line());
    const open = vi.spyOn(fs, "openSync");
    const read = reader();
    expect(read()).toEqual({ warning: 0, denied: 0 });
    fs.writeFileSync(file, squid + line());
    expect(read()).toEqual({ warning: 0, denied: 1 });
    expect(open.mock.calls.every(([name]) => String(name).endsWith(".jsonl"))).toBe(true);
    expect(open.mock.calls.some(([name]) => String(name).includes("squid"))).toBe(false);
    expect(classifyFirewallSteeringRecord(squid)).toBeUndefined();
  });

  it("rejects redirected log directories and disables evidence after a read error", () => {
    fs.renameSync(path.dirname(file), path.join(directory, "redirected"));
    fs.symlinkSync(path.join(directory, "redirected"), path.dirname(file));
    const read = reader();
    expect(log).toHaveBeenCalledExactlyOnceWith(expect.stringContaining("unavailable_or_unsafe"));
    expect(read()).toEqual({ warning: 0, denied: 0 });
  });

  it("reports access errors once and does not retry the denied path", () => {
    const read = reader();
    const open = vi.spyOn(fs, "openSync").mockImplementation(() => {
      throw Object.assign(new Error("PRIVATE"), { code: "EACCES" });
    });
    expect(read()).toEqual({ warning: 0, denied: 0 });
    expect(read()).toEqual({ warning: 0, denied: 0 });
    expect(open).toHaveBeenCalledOnce();
    expect(log).toHaveBeenCalledOnce();
    expect(JSON.stringify(log.mock.calls)).not.toContain("PRIVATE");
  });
});
