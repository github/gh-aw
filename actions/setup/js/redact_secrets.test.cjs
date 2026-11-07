import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import fs from "fs";
import path from "path";
import os from "os";
const mockCore = {
  debug: vi.fn(),
  info: vi.fn(),
  notice: vi.fn(),
  warning: vi.fn(),
  error: vi.fn(),
  setFailed: vi.fn(),
  setOutput: vi.fn(),
  exportVariable: vi.fn(),
  setSecret: vi.fn(),
  getInput: vi.fn(),
  getBooleanInput: vi.fn(),
  getMultilineInput: vi.fn(),
  getState: vi.fn(),
  saveState: vi.fn(),
  startGroup: vi.fn(),
  endGroup: vi.fn(),
  group: vi.fn(),
  addPath: vi.fn(),
  setCommandEcho: vi.fn(),
  isDebug: vi.fn().mockReturnValue(!1),
  getIDToken: vi.fn(),
  summary: { addRaw: vi.fn().mockReturnThis(), write: vi.fn().mockResolvedValue(void 0) },
};
global.core = mockCore;
const { persistDetectionMasks } = require("./detection_mask_state.cjs");
const redactScript = fs.readFileSync(path.join(__dirname, "redact_secrets.cjs"), "utf8");
describe("redact_secrets.cjs", () => {
  let tempDir, ambientPathsSpy;
  it("sanitizes detection results, copied firewall logs and late-created accounting files", async () => {
    const { main } = require("./redact_secrets.cjs");
    const secret = "synthetic-detection-private-value";
    vi.stubEnv("GH_AW_SECRET_NAMES", "DETECTION_TEST");
    vi.stubEnv("SECRET_DETECTION_TEST", secret);
    const root = path.join(process.env.RUNNER_TEMP, "gh-aw");
    const detection = path.join(root, "threat-detection");
    const source = path.join(root, "sandbox/firewall/logs/token-usage.jsonl");
    const copied = path.join(detection, "sandbox/firewall/logs/token-usage.jsonl");
    const audit = path.join(detection, "sandbox/firewall/audit/audit.log");
    const rotated = path.join(detection, "sandbox/firewall/logs/access.log.1");
    const extensionless = path.join(detection, "sandbox/firewall/audit/diagnostic");
    for (const file of [source, copied, audit]) fs.mkdirSync(path.dirname(file), { recursive: true });
    const result = path.join(detection, "detection_result.json");
    const log = path.join(detection, "detection.log");
    const summary = path.join(root, "step-summary.md");
    const verdict = { status: "success", reason: secret, verdict: { secrets: false, prompt_injection: false, malicious_patch: false } };
    fs.writeFileSync(result, JSON.stringify(verdict));
    fs.writeFileSync(log, `Safe detection diagnostic: ${secret}`);
    fs.writeFileSync(summary, `Detection summary: ${secret}`);
    fs.writeFileSync(source, JSON.stringify({ model: secret, input_tokens: 123, output_tokens: 45 }) + "\n");
    fs.copyFileSync(source, copied);
    fs.writeFileSync(audit, `Firewall diagnostic: ${secret}`);
    fs.writeFileSync(rotated, `Rotated firewall diagnostic: ${secret}`);
    fs.writeFileSync(extensionless, `Extensionless firewall diagnostic: ${secret}`);

    const options = { additionalDirectories: [path.join(detection, "sandbox/firewall")] };
    await main(options);

    for (const file of [result, log, summary, source, copied, audit, rotated, extensionless]) expect(fs.readFileSync(file, "utf8")).not.toContain(secret);
    expect(JSON.parse(fs.readFileSync(result, "utf8"))).toEqual({ ...verdict, reason: "***REDACTED***" });
    expect(JSON.parse(fs.readFileSync(copied, "utf8"))).toEqual({ model: "***REDACTED***", input_tokens: 123, output_tokens: 45 });
    expect(fs.readFileSync(log, "utf8")).toContain("Safe detection diagnostic");

    const usage = path.join(detection, "detection_usage.json");
    const usageJSONL = path.join(detection, "detection_usage.jsonl");
    fs.writeFileSync(usage, JSON.stringify({ primary_model: secret, ai_credits: 2 }));
    fs.writeFileSync(usageJSONL, JSON.stringify({ provider: secret, ai_credits: 2 }) + "\n");
    await main(options);

    expect(JSON.parse(fs.readFileSync(usage, "utf8"))).toEqual({ primary_model: "***REDACTED***", ai_credits: 2 });
    expect(JSON.parse(fs.readFileSync(usageJSONL, "utf8"))).toEqual({ provider: "***REDACTED***", ai_credits: 2 });
    expect(mockCore.setFailed).not.toHaveBeenCalled();
  });
  it("fails closed and removes detection output when file redaction fails", async () => {
    const { main } = require("./redact_secrets.cjs");
    const secret = "synthetic-detection-private-value";
    vi.stubEnv("GH_AW_SECRET_NAMES", "DETECTION_TEST");
    vi.stubEnv("SECRET_DETECTION_TEST", secret);
    const detection = path.join(process.env.RUNNER_TEMP, "gh-aw/threat-detection");
    fs.mkdirSync(detection, { recursive: true });
    const result = path.join(detection, "detection_result.json");
    fs.writeFileSync(result, JSON.stringify({ reason: secret }));
    const read = fs.readFileSync;
    const failure = vi.spyOn(fs, "readFileSync").mockImplementation((file, ...args) => {
      if (file === result) throw Object.assign(new Error(secret), { code: "EIO" });
      return read(file, ...args);
    });
    try {
      await main();
    } finally {
      failure.mockRestore();
    }
    expect(fs.existsSync(result)).toBe(false);
    expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("Removed artifact source after secret redaction failed"));
    for (const method of ["info", "warning", "error", "setFailed"]) expect(JSON.stringify(mockCore[method].mock.calls)).not.toContain(secret);
  });
  it("preserves detection-only runtime masks across separate passes without artifact state", async () => {
    const { main } = require("./redact_secrets.cjs");
    const secret = 'synthetic-unknown-"\\credential';
    const root = path.join(process.env.RUNNER_TEMP, "gh-aw");
    const detection = path.join(root, "threat-detection");
    const firewall = path.join(detection, "sandbox/firewall/logs");
    fs.mkdirSync(firewall, { recursive: true });
    const log = path.join(detection, "detection.log");
    const result = path.join(detection, "detection_result.json");
    const copied = path.join(firewall, "access.log.1");
    fs.writeFileSync(log, `::add-mask::${secret}\nSafe detection diagnostic: ${secret}\n`);
    fs.writeFileSync(result, JSON.stringify({ status: "success", reason: secret }));
    fs.writeFileSync(copied, `Firewall diagnostic: ${secret}`);
    mockCore.exportVariable.mockImplementation((name, value) => {
      expect(mockCore.setSecret).toHaveBeenCalledWith(secret);
      expect(mockCore.setSecret).toHaveBeenCalledWith(value);
      expect(mockCore.setSecret.mock.invocationCallOrder.at(-1)).toBeLessThan(mockCore.exportVariable.mock.invocationCallOrder.at(-1));
      vi.stubEnv(name, value);
    });
    const options = { additionalDirectories: [path.dirname(firewall)], persistDetectionMasks };

    await main(options);

    expect(process.env.GH_AW_SECRET_NAMES).toBeUndefined();
    expect(mockCore.exportVariable).toHaveBeenCalledWith("GH_AW_DETECTION_MASKED_VALUES", JSON.stringify([secret]));
    expect(fs.readFileSync(log, "utf8")).toBe("Safe detection diagnostic: ***\n");
    expect(JSON.parse(fs.readFileSync(result, "utf8"))).toEqual({ status: "success", reason: "***" });
    expect(fs.readFileSync(copied, "utf8")).toBe("Firewall diagnostic: ***");
    const usage = path.join(detection, "detection_usage.json");
    const usageJSONL = path.join(detection, "detection_usage.jsonl");
    fs.writeFileSync(usage, JSON.stringify({ primary_model: secret, ai_credits: 2 }));
    fs.writeFileSync(usageJSONL, JSON.stringify({ provider: secret, ai_credits: 2 }) + "\n");
    // Reload the script as a separate github-script invocation, not an in-memory cache.
    await eval(`(async () => { ${redactScript}; await main(options); })()`);

    expect(JSON.parse(fs.readFileSync(usage, "utf8"))).toEqual({ primary_model: "***", ai_credits: 2 });
    expect(JSON.parse(fs.readFileSync(usageJSONL, "utf8"))).toEqual({ provider: "***", ai_credits: 2 });
    expect(fs.readdirSync(detection).sort()).toEqual(["detection.log", "detection_result.json", "detection_usage.json", "detection_usage.jsonl", "sandbox"]);
    expect(fs.readdirSync(root)).toEqual(["threat-detection"]);
    expect(mockCore.setSecret.mock.calls).toEqual([[secret], [JSON.stringify([secret])], [secret], [JSON.stringify([secret])]]);
    expect(mockCore.setFailed).not.toHaveBeenCalled();
    for (const method of ["info", "warning", "error", "setFailed"]) expect(JSON.stringify(mockCore[method].mock.calls)).not.toContain(secret);
  });
  it.each(["read", "register", "register-state", "persist", "invalid-state", "missing-persistence"])("removes detection sources safely if runtime mask %s fails", async failureStage => {
    const { main } = require("./redact_secrets.cjs");
    const secret = "synthetic-unknown-runtime-credential";
    const detection = path.join(process.env.RUNNER_TEMP, "gh-aw/threat-detection");
    const firewall = path.join(detection, "sandbox/firewall");
    fs.mkdirSync(firewall, { recursive: true });
    const log = path.join(detection, "detection.log");
    const result = path.join(detection, "detection_result.json");
    const copied = path.join(firewall, "access.log.1");
    fs.writeFileSync(log, `::add-mask::${secret}\n${secret}`);
    fs.writeFileSync(result, JSON.stringify({ reason: secret }));
    fs.writeFileSync(copied, secret);
    const read = fs.readFileSync;
    const readSpy = vi.spyOn(fs, "readFileSync").mockImplementation((file, ...args) => {
      if (failureStage === "read" && file === log) throw Object.assign(new Error(secret), { code: "EIO" });
      return read(file, ...args);
    });
    if (failureStage === "register" || failureStage === "register-state")
      mockCore.setSecret.mockImplementation(value => {
        if (failureStage === "register" || value === JSON.stringify([secret])) throw new Error(secret);
      });
    if (failureStage === "persist")
      mockCore.exportVariable.mockImplementation(() => {
        throw new Error(secret);
      });
    if (failureStage === "invalid-state") vi.stubEnv("GH_AW_DETECTION_MASKED_VALUES", JSON.stringify({ secret }));
    try {
      await main({ additionalDirectories: [firewall], persistDetectionMasks: failureStage === "missing-persistence" ? undefined : persistDetectionMasks });
    } finally {
      readSpy.mockRestore();
    }
    for (const file of [log, result, copied]) expect(fs.existsSync(file)).toBe(false);
    if (failureStage !== "persist") expect(mockCore.exportVariable).not.toHaveBeenCalled();
    expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("Removed artifact sources after runtime mask collection failed"));
    for (const method of ["info", "warning", "error", "setFailed"]) expect(JSON.stringify(mockCore[method].mock.calls)).not.toContain(secret);
  });
  it.each(["compressed", "binary", "invalid-utf8", "oversized"])("excludes %s firewall byproducts without decoding or corrupting them", async kind => {
    const { main } = require("./redact_secrets.cjs");
    const secret = "synthetic-firewall-private-value";
    vi.stubEnv("GH_AW_SECRET_NAMES", "FIREWALL_TEST");
    vi.stubEnv("SECRET_FIREWALL_TEST", secret);
    const firewall = path.join(process.env.RUNNER_TEMP, "gh-aw/threat-detection/sandbox/firewall");
    fs.mkdirSync(firewall, { recursive: true });
    const source = path.join(firewall, `${secret}.log.${kind}`);
    const normal = path.join(firewall, "access.log.1");
    const diagnostic = path.join(firewall, "diagnostic");
    fs.writeFileSync(normal, `Safe firewall diagnostic: ${secret}`);
    fs.writeFileSync(diagnostic, `UTF-8 diagnostic: \u00e9 \ud83d\ude80 ${secret}`);
    const content = kind === "compressed" ? require("node:zlib").gzipSync(secret) : kind === "binary" ? Buffer.from(`\0${secret}`) : kind === "invalid-utf8" ? Buffer.concat([Buffer.from([0xff]), Buffer.from(secret)]) : Buffer.from(secret);
    fs.writeFileSync(source, content);
    if (kind === "oversized") fs.truncateSync(source, 16 * 1024 * 1024 + 1);
    const write = vi.spyOn(fs, "writeFileSync");
    const read = vi.spyOn(fs, "readSync");
    try {
      await main({ additionalDirectories: [firewall] });
      expect(write.mock.calls.some(([file]) => file === source)).toBe(false);
      if (kind === "oversized") expect(read.mock.calls.every(([, buffer]) => buffer.length < 16 * 1024 * 1024)).toBe(true);
    } finally {
      write.mockRestore();
      read.mockRestore();
    }
    expect(fs.existsSync(source)).toBe(false);
    expect(fs.readFileSync(normal, "utf8")).toBe("Safe firewall diagnostic: ***REDACTED***");
    expect(fs.readFileSync(diagnostic, "utf8")).toBe("UTF-8 diagnostic: \u00e9 \ud83d\ude80 ***REDACTED***");
    expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Removed unsupported artifact before upload"));
    expect(mockCore.setFailed).not.toHaveBeenCalled();
    for (const method of ["info", "warning", "error", "setFailed"]) expect(JSON.stringify(mockCore[method].mock.calls)).not.toContain(secret);
  });
  it("fails closed with safe diagnostics if an unsupported firewall source cannot be removed", async () => {
    const { main } = require("./redact_secrets.cjs");
    const secret = "synthetic-firewall-private-value";
    const firewall = path.join(process.env.RUNNER_TEMP, "gh-aw/threat-detection/sandbox/firewall");
    fs.mkdirSync(firewall, { recursive: true });
    const source = path.join(firewall, "access.log.gz");
    const content = require("node:zlib").gzipSync(secret);
    fs.writeFileSync(source, content);
    const unlink = fs.unlinkSync;
    const cleanup = vi.spyOn(fs, "unlinkSync").mockImplementation(file => {
      if (file === source) throw Object.assign(new Error(secret), { code: "EACCES" });
      return unlink(file);
    });
    try {
      await main({ additionalDirectories: [firewall] });
    } finally {
      cleanup.mockRestore();
    }
    expect(fs.readFileSync(source)).toEqual(content);
    expect(mockCore.setFailed).toHaveBeenCalled();
    expect(mockCore.warning).toHaveBeenCalledWith("Secret redaction failed (stage=unlink, code=EACCES)");
    for (const method of ["info", "warning", "error", "setFailed"]) expect(JSON.stringify(mockCore[method].mock.calls)).not.toContain(secret);
  });
  it.each(["OTEL_EXPORTER_OTLP_HEADERS", "GH_AW_OTLP_ALL_HEADERS", "GH_AW_OTLP_ENDPOINTS"])("redacts decoded OTLP passwords from artifacts and summaries using %s without setup log masks", async variable => {
    const { main, redactFilesInDir, redactStepSummaryContent } = require("./redact_secrets.cjs");
    const passwords = ['synthetic-otlp-"password\\one', "four", "12345678", "9007199254740993"];
    const encoded = passwords.map(password => Buffer.from(`123456:${password}`).toString("base64"));
    const headers = encoded.map(value => `Authorization=${encodeURIComponent(`Basic ${value}`)}`);
    vi.stubEnv(variable, variable === "GH_AW_OTLP_ENDPOINTS" ? JSON.stringify(headers.map(header => ({ url: "https://traces.example.com", headers: header }))) : headers.join(","));
    const dir = path.join(process.env.RUNNER_TEMP, "gh-aw");
    fs.mkdirSync(dir, { recursive: true });
    const file = path.join(dir, "evals.json");
    const log = path.join(dir, "evals.log");
    const scalarFile = path.join(dir, "numeric.json");
    const payload = JSON.stringify({ toolOutput: passwords.join("\n"), header: headers.join(","), credentials: encoded, safe: "123456" }).replace(/}$/, ',"nested":{"ids":[12345678,{"token":9007199254740993},42]}}');
    fs.writeFileSync(file, payload);
    fs.writeFileSync(log, [...passwords, ...encoded, ...headers].join("\n"));
    fs.writeFileSync(scalarFile, "12345678\n");

    await main();

    const assertRedacted = text => {
      const parsed = JSON.parse(text);
      expect(parsed.safe).toBe("123456");
      expect(parsed.nested).toEqual({ ids: ["***", { token: "***" }, 42] });
      for (const password of passwords) expect(parsed.toolOutput).not.toContain(password);
      for (const credential of encoded) expect(text).not.toContain(credential);
      for (const header of headers) expect(text).not.toContain(header);
    };
    assertRedacted(fs.readFileSync(file, "utf8"));
    const logContent = fs.readFileSync(log, "utf8");
    for (const value of [...passwords, ...encoded, ...headers]) expect(logContent).not.toContain(value);
    expect(logContent.split("\n")[2]).toBe("***");
    expect(JSON.parse(fs.readFileSync(scalarFile, "utf8"))).toBe("***");
    expect(mockCore.setFailed).not.toHaveBeenCalled();

    fs.writeFileSync(file, payload);
    fs.writeFileSync(scalarFile, "12345678\n");
    await redactFilesInDir(dir);
    assertRedacted(fs.readFileSync(file, "utf8"));
    expect(JSON.parse(fs.readFileSync(scalarFile, "utf8"))).toBe("***");
    assertRedacted(redactStepSummaryContent(payload));
    expect(JSON.parse(redactStepSummaryContent("12345678\n"))).toBe("***");
    expect(mockCore.info).not.toHaveBeenCalled();
  });
  it("redacts decoded Grafana Cloud tokens even without an exact-value secret", () => {
    const { redactBuiltInPatterns } = require("./redact_secrets.cjs");
    const token = "glc_" + Buffer.from(JSON.stringify({ example: "synthetic-test-only" })).toString("base64");
    const result = redactBuiltInPatterns(`curl --user 123456:${token}\n${token}`);

    expect(result.content).not.toContain(token);
    expect(result.redactionCount).toBe(2);
    expect(result.detectedPatterns).toContain("Grafana Cloud API Token");
    expect(redactBuiltInPatterns("glc_short placeholder")).toMatchObject({ content: "glc_short placeholder", redactionCount: 0 });
  });
  (beforeEach(() => {
    const exists = fs.existsSync;
    ambientPathsSpy = vi.spyOn(fs, "existsSync").mockImplementation(file => (String(file) === "/tmp/gh-aw" || String(file).startsWith("/tmp/gh-aw/") ? false : exists(file)));
    ((tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "redact-test-"))),
      vi.stubEnv("RUNNER_TEMP", path.join(tempDir, "runner-temp")),
      vi.stubEnv("HOME", path.join(tempDir, "home")),
      vi.stubEnv("GITHUB_WORKSPACE", path.join(tempDir, "workspace")),
      Object.values(mockCore).forEach(fn => {
        "function" == typeof fn && fn.mockClear();
      }),
      mockCore.summary.addRaw && mockCore.summary.addRaw.mockClear(),
      mockCore.summary.write && mockCore.summary.write.mockClear(),
      delete process.env.GH_AW_SECRET_NAMES);
    mockCore.exportVariable.mockReset();
    mockCore.setSecret.mockReset();
    vi.stubEnv("GH_AW_DETECTION_MASKED_VALUES", "");
    for (const variable of ["OTEL_EXPORTER_OTLP_HEADERS", "GH_AW_OTLP_ALL_HEADERS", "GH_AW_OTLP_ENDPOINTS"]) vi.stubEnv(variable, "");
  }),
    afterEach(() => {
      ambientPathsSpy.mockRestore();
      vi.unstubAllEnvs();
      tempDir && fs.existsSync(tempDir) && fs.rmSync(tempDir, { recursive: !0, force: !0 });
      for (const key of Object.keys(process.env)) key.startsWith("SECRET_") && delete process.env[key];
    }),
    it("scans large nested file lists without overflowing the call stack", () => {
      const { findFiles } = require("./redact_secrets.cjs");
      const child = path.join(tempDir, "aw-mcp");
      const entries = Array.from({ length: 150000 }, (_, i) => ({
        name: `log-${i}.jsonl`,
        isSymbolicLink: () => false,
        isDirectory: () => false,
        isFile: () => true,
      }));
      const readdir = vi.spyOn(fs, "readdirSync").mockImplementation(dir => {
        if (dir === child) return entries;
        if (dir === tempDir) return [{ name: "aw-mcp", isSymbolicLink: () => false, isDirectory: () => true }];
        throw new Error("Unexpected directory");
      });
      try {
        const files = findFiles(tempDir, [".jsonl"]);
        expect(files).toHaveLength(entries.length);
        expect(files[0]).toBe(path.join(child, "log-0.jsonl"));
        expect(files.at(-1)).toBe(path.join(child, "log-149999.jsonl"));
      } finally {
        readdir.mockRestore();
      }
    }),
    describe("file handling", () => {
      it.each(["ordinary.json", "safeoutputs.jsonl"])("silently redacts UTF-8 payloads in %s", filename => {
        const { processFile } = require("./redact_secrets.cjs");
        const secret = "synthetic-private-value";
        const file = path.join(tempDir, filename);
        const content = JSON.stringify({ type: "add_comment", body: "\u00e9 \ud83d\ude80 " + secret });
        fs.writeFileSync(file, content);

        expect(processFile(file, [secret])).toBeGreaterThan(0);

        const output = fs.readFileSync(file, "utf8");
        expect(Buffer.byteLength(content, "utf8")).toBeGreaterThan(content.length);
        expect(output).not.toContain(secret);
        expect(mockCore.info).not.toHaveBeenCalled();
        expect(mockCore.warning).not.toHaveBeenCalled();
        expect(mockCore.setFailed).not.toHaveBeenCalled();
      });

      it("does not log unchanged artifacts", () => {
        const { processFile } = require("./redact_secrets.cjs");
        const file = path.join(tempDir, "ordinary.log");
        fs.writeFileSync(file, "public");

        expect(processFile(file, [])).toBe(0);

        expect(mockCore.info).not.toHaveBeenCalled();
        expect(mockCore.warning).not.toHaveBeenCalled();
        expect(mockCore.setFailed).not.toHaveBeenCalled();
      });

      it.each(["utf8", "latin1"])("removes binary artifacts containing %s runtime credentials without rewriting them", encoding => {
        const { processFile } = require("./redact_secrets.cjs");
        const file = path.join(tempDir, "binary.json");
        const secret = "caf\u00e9-runtime-credential";
        fs.writeFileSync(file, Buffer.concat([Buffer.from([0xff, 0]), Buffer.from(secret, encoding), Buffer.from([0xfe])]));

        expect(processFile(file, [], [secret])).toBeGreaterThan(0);

        expect(fs.existsSync(file)).toBe(false);
        expect(mockCore.setFailed).not.toHaveBeenCalled();
      });

      it("reports input bytes only after a fail-closed redaction error without payload data", () => {
        const { processFile } = require("./redact_secrets.cjs");
        const file = path.join(tempDir, "safeoutputs.jsonl");
        const content = '{"body":"\u00e9 \ud83d\ude80 synthetic-private-value"';
        fs.writeFileSync(file, content);

        expect(processFile(file, ["synthetic-private-value"])).toBe(0);

        expect(fs.existsSync(file)).toBe(false);
        expect(mockCore.info).not.toHaveBeenCalled();
        expect(mockCore.warning).toHaveBeenCalledWith("Secret redaction failed (stage=redact, code=unknown)");
        expect(mockCore.setFailed).toHaveBeenCalledWith(`ERR_VALIDATION: Removed artifact source after secret redaction failed (input_bytes=${Buffer.byteLength(content, "utf8")})`);
        expect([...mockCore.warning.mock.calls, ...mockCore.setFailed.mock.calls].flat().join("\n")).not.toContain("synthetic-private-value");
      });

      it.each(["safeoutputs.jsonl", "agent_output.json"])("coalesces all credential sources in executable %s payloads", filename => {
        const { processFile } = require("./redact_secrets.cjs");
        const token = "ghs_" + "A".repeat(40);
        const cases = [
          { text: "SECRET_AB_LONGVALUE", secrets: ["AB_LONGVALUE"], masks: ["SECRET_AB"] },
          { text: "prefix-" + token + "-configured", secrets: ["prefix-" + token + "-configured"], masks: [] },
          { text: "before" + token, secrets: [], masks: ["beforeghs_"] },
        ];
        const file = path.join(tempDir, filename);
        for (const { text, secrets, masks } of cases) {
          const message = { type: "submit_pull_request_review", event: "COMMENT", pull_request_number: 12, body: `before [${text}] after` };
          const envelope = filename === "agent_output.json";
          fs.writeFileSync(file, JSON.stringify(envelope ? { items: [message], errors: [] } : message) + "\n");

          expect(processFile(file, secrets, [...masks, "body", "COMMENT", "type", "items"])).toBeGreaterThan(0);

          const output = JSON.parse(fs.readFileSync(file, "utf8"));
          const expected = { ...message, body: "before [***REDACTED***] after" };
          expect(output).toEqual(envelope ? { items: [expected], errors: [] } : expected);
          expect(mockCore.setFailed).not.toHaveBeenCalled();
        }
      });

      it.each(["safeoutputs.jsonl", "agent_output.json"])("keeps decoded free-form strings literal while redacting %s", filename => {
        const { processFile } = require("./redact_secrets.cjs");
        const file = path.join(tempDir, filename);
        const values = [
          ['{ "key" : "SECRET_AB_LONGVALUE" }', '{ "key" : "***REDACTED***" }'],
          ['"SECRET_AB_LONGVALUE"', '"***REDACTED***"'],
          ["12345678", "***REDACTED***"],
          [String.raw`literal \u0053ECRET_WORD \n C:\new\path`, String.raw`literal \u0053ECRET_WORD \n C:\new\path`],
          ["first\nSECRET_AB_LONGVALUE\nlast", "first\n***REDACTED***\nlast"],
          ['{ "key" : "public" }', '{ "key" : "public" }'],
          [String.raw`before synthetic\npassword after`, "before ***REDACTED*** after"],
          ["before synthetic\n::add-mask::middle\npassword after", "before ***REDACTED*** after"],
          ["before\n::add-mask::unrelated\nafter", "before\nafter"],
          ["before\n::add-mask::SECRET_AB_LONGVALUE\nafter", "before\nafter"],
          ["before\n::add-mask::whole-command\nafter", "before\nafter"],
          ["before runtime\n::add-mask::middle\npassword after", "before *** after"],
        ];
        const record = Object.fromEntries(values.map(([value], index) => [`field-${index}`, value]));
        record["SECRET_AB_LONGVALUE"] = "public";
        const message = { type: "ledger_append", record };
        const expected = { type: "ledger_append", record: { ...Object.fromEntries(values.map(([, value], index) => [`field-${index}`, value])), "***REDACTED***": "public" } };
        const envelope = filename === "agent_output.json";
        fs.writeFileSync(file, JSON.stringify(envelope ? { items: [message], errors: [] } : message));

        expect(
          processFile(file, ["AB_LONGVALUE", "12345678", "SECRET_WORD", String.raw`synthetic\npassword`, "synthetic\n::add-mask::middle\npassword"], ["SECRET_AB", "::add-mask::whole-command", "runtime\n::add-mask::middle\npassword"])
        ).toBeGreaterThan(0);

        expect(JSON.parse(fs.readFileSync(file, "utf8"))).toEqual(envelope ? { items: [expected], errors: [] } : expected);
        expect(mockCore.setFailed).not.toHaveBeenCalled();
      });

      it.each(['export SYNTHETIC_DIR="/example/synthetic"', 'mkdir -p "/example/synthetic"'])("keeps complete-line quoted shell masks as plain redaction markers (%s)", line => {
        const { processFile } = require("./redact_secrets.cjs");
        const file = path.join(tempDir, "shell-mask.log");
        for (const configured of [false, true]) {
          fs.writeFileSync(file, line + "\n");

          expect(processFile(file, configured ? [line] : [], configured ? [] : [line])).toBeGreaterThan(0);

          expect(fs.readFileSync(file, "utf8")).toBe((configured ? "***REDACTED***" : "***") + "\n");
          expect(mockCore.setFailed).not.toHaveBeenCalled();
        }
      });
      it.each(['synthetic"password', "synthetic\\password", "synthetic\\npassword", "synthetic\tpassword", "synthetic\npassword", 'synthetic"\\\tpassword'])(
        "redacts configured secrets before runtime masking can serialize malformed records (%j)",
        secret => {
          const { processFile } = require("./redact_secrets.cjs");
          const file = path.join(tempDir, "combined.log");
          fs.writeFileSync(file, '"bad\t" "\\u006fpaque" token=' + secret);

          expect(processFile(file, [secret], ["opaque", "synthetic"])).toBeGreaterThan(0);

          const output = fs.readFileSync(file, "utf8");
          const decoded = output
            .split("\n")
            .map(line => {
              try {
                return JSON.parse(line);
              } catch {
                return line;
              }
            })
            .join("\n");
          expect(decoded).not.toContain(secret);
          expect(decoded).not.toContain("password");
          expect(decoded).not.toContain("opaque");
          expect(mockCore.setFailed).not.toHaveBeenCalled();
          expect(mockCore.info.mock.calls.flat().join("\n")).not.toContain(secret);
        }
      );

      it("redacts invalid JSON as text while retaining encoded runtime-mask recovery", () => {
        const { processFile } = require("./redact_secrets.cjs");
        const file = path.join(tempDir, "invalid-json.log");
        const builtIn = "ghs_" + "A".repeat(40);
        const content = `{"configured":"configured-secret","runtime":"opaque","builtIn":"${builtIn}", invalid}`;
        fs.writeFileSync(file, content);

        expect(processFile(file, ["configured-secret"], ["opaque"])).toBeGreaterThan(0);

        expect(fs.readFileSync(file, "utf8")).toBe('{"configured":"***REDACTED***","runtime":"***","builtIn":"***REDACTED***", invalid}');
        expect(mockCore.setFailed).not.toHaveBeenCalled();

        fs.writeFileSync(file, '"bad\\q "\\u006fpaque"');
        expect(processFile(file, [], ["opaque"])).toBeGreaterThan(0);
        expect(fs.readFileSync(file, "utf8")).toBe('"bad\\q "***"');
        expect(mockCore.setFailed).not.toHaveBeenCalled();
      });

      it.each(["configured", "runtime"])("redacts raw %s secrets inside recoverable strings in malformed JSON", source => {
        const { processFile } = require("./redact_secrets.cjs");
        const secret = String.raw`synthetic\npassword`;
        const file = path.join(tempDir, "malformed.json");
        const content = `{"token":"${secret}", "other":"\\u0041", broken`;
        fs.writeFileSync(file, content, { mode: 0o600 });

        expect(processFile(file, source === "configured" ? [secret] : [], source === "runtime" ? [secret] : [])).toBeGreaterThan(0);

        const output = fs.readFileSync(file, "utf8");
        expect(output).not.toContain(secret);
        expect(output).not.toContain("password");
        expect(output).toContain('"other":"\\u0041"');
        expect(mockCore.setFailed).not.toHaveBeenCalled();
        expect(mockCore.info.mock.calls.flat().join("\n")).not.toContain(secret);
      });

      it("redacts encoded credentials across malformed multiline strings without fragmenting overlapping secrets", () => {
        const { processFile } = require("./redact_secrets.cjs");
        const secret = "synthetic\npassword";
        const runtime = "before" + secret;
        const file = path.join(tempDir, "malformed.json");
        fs.writeFileSync(file, '{"token":"before\\u0073ynthetic\npassword", broken');

        expect(processFile(file, [secret], [runtime])).toBeGreaterThan(0);

        const output = fs.readFileSync(file, "utf8");
        expect(output).not.toContain("password");
        expect(output).not.toContain("ynthetic");
        expect(mockCore.setFailed).not.toHaveBeenCalled();
      });

      it("matches multiline credentials before removing embedded add-mask command lines", () => {
        const { processFile } = require("./redact_secrets.cjs");
        const secret = "synthetic\n::add-mask::ignored\npassword";
        const file = path.join(tempDir, "malformed.json");
        fs.writeFileSync(file, '{"token":"' + secret + '", broken');

        expect(processFile(file, [secret])).toBeGreaterThan(0);

        const output = fs.readFileSync(file, "utf8");
        expect(output).not.toContain("synthetic");
        expect(output).not.toContain("password");
        expect(output).not.toContain("::add-mask::");
        expect(mockCore.setFailed).not.toHaveBeenCalled();
      });

      it.each(["configured", "runtime", "built-in"])("sanitizes %s credentials across malformed JSON mutations and encodings", source => {
        const { processFile } = require("./redact_secrets.cjs");
        const secret = source === "built-in" ? "ghs_" + "A".repeat(40) : 'synthetic"\\\t\npassword';
        const unicode = Array.from(secret, char => "\\u" + char.charCodeAt(0).toString(16).padStart(4, "0")).join("");
        const spellings = [secret, JSON.stringify(secret).slice(1, -1), unicode];
        const file = path.join(tempDir, "malformed.jsonl");
        const mutations = [
          token => `{"token":"${token}"`,
          token => `{"token":"${token}`,
          token => `{"token":"${token}",}`,
          token => `{"token":"${token}", "broken":"\\q"}`,
          token => `"bad\t" {"token":"${token}"}`,
          token => `{"safe":true}\n{"token":"${token}", broken`,
          token => `{"safe":true}\n"\\u0070ublic"\n{"token":"${token}", broken`,
        ];
        for (const spelling of spellings) {
          for (const mutate of mutations) {
            fs.writeFileSync(file, mutate(spelling), { mode: 0o600 });

            expect(processFile(file, source === "configured" ? [secret] : [], source === "runtime" ? [secret] : [])).toBeGreaterThan(0);

            const output = fs.readFileSync(file, "utf8");
            let record = output;
            try {
              record = JSON.parse(output);
            } catch {
              // Sanitized malformed records need not become valid JSON.
            }
            const decoded = record.replace(/\\(["\\/bfnrt]|u[0-9a-fA-F]{4})/g, escape => JSON.parse('"' + escape + '"'));
            expect(output).not.toContain(secret);
            expect(output).not.toContain(spelling);
            expect(decoded).not.toContain(secret);
            expect(decoded).not.toContain(source === "built-in" ? "A".repeat(40) : "password");
            expect(mockCore.setFailed).not.toHaveBeenCalled();
            expect(mockCore.warning).not.toHaveBeenCalled();
          }
        }
      });

      it.each(['synthetic"password', "synthetic\\password", "synthetic\tpassword", "synthetic\npassword"])("redacts decoded configured secrets alongside overlapping runtime masks in JSON and mixed records (%j)", secret => {
        const { processFile } = require("./redact_secrets.cjs");
        const record = JSON.stringify({ [secret]: secret, safe: "public" });
        const encoded = record.replace(/synthetic/g, "\\u0073ynthetic");
        for (const [name, input] of [
          ["valid.json", encoded],
          ["mixed.log", "output: " + encoded],
        ]) {
          const file = path.join(tempDir, name);
          fs.writeFileSync(file, input);

          expect(processFile(file, [secret], ["synthetic"])).toBeGreaterThan(0);

          const output = fs.readFileSync(file, "utf8");
          const decoded = JSON.parse(name === "valid.json" ? output : output.slice("output: ".length));
          expect(Object.keys(decoded)).toEqual(["***REDACTED***", "safe"]);
          expect(Object.values(decoded)).toEqual(["***REDACTED***", "public"]);
          expect(mockCore.setFailed).not.toHaveBeenCalled();
        }
      });

      it.each([false, true])("removes full built-in and configured credentials before overlapping runtime masks (encoded=%s)", encoded => {
        const { processFile } = require("./redact_secrets.cjs");
        const token = "ghs_" + "A".repeat(40);
        const secret = "prefix-" + token + "-configured";
        const file = path.join(tempDir, "overlapping.json");
        const record = JSON.stringify({ token, secret, safe: "public" });
        fs.writeFileSync(file, encoded ? record.replace(/ghs_/g, "\\u0067hs_") : record);

        expect(processFile(file, [secret], ["ghs_", "prefix"])).toBeGreaterThan(0);

        expect(JSON.parse(fs.readFileSync(file, "utf8"))).toEqual({ token: "***REDACTED***", secret: "***REDACTED***", safe: "public" });
        expect(mockCore.setFailed).not.toHaveBeenCalled();
      });

      it.each([
        ["configured", "long-configured-secret", "ablong", "ab", "-configured-secret"],
        ["built-in", "ghs_" + "A".repeat(40), "beforeghs_", "before", "A".repeat(40)],
        ["numeric", "12345678", "9123", "9", "45678"],
        ["multiline", "long\nconfigured-secret", "ablong\n", "ab", "configured-secret"],
      ])("removes full %s credentials when a shorter runtime mask starts before their overlapping prefix", (kind, secret, runtime, prefix, suffix) => {
        const { processFile } = require("./redact_secrets.cjs");
        const file = path.join(tempDir, "crossing-overlap.log");
        const text = prefix + secret;
        for (const content of [text, JSON.stringify({ [text]: text }), "output: " + JSON.stringify(text)]) {
          fs.writeFileSync(file, content);

          expect(processFile(file, kind === "built-in" ? [] : [secret], [runtime])).toBeGreaterThan(0);

          const output = fs.readFileSync(file, "utf8");
          expect(output).not.toContain(suffix);
          expect(output).toContain("***");
          expect(mockCore.setFailed).not.toHaveBeenCalled();
        }
      });

      it.each([
        ["synthetic\\n", "synthetic\npassword", false],
        ["synthetic\\u006e", "syntheticnpassword", true],
        ['"password', '"passwordtail', false],
      ])("does not fragment longer encoded runtime masks with raw configured escape prefixes (%j)", (secret, runtime, unicode) => {
        const { processFile } = require("./redact_secrets.cjs");
        const file = path.join(tempDir, "encoded-overlap.log");
        const encoded = JSON.stringify(runtime);
        for (const prefix of ["output: ", '"bad\t" ']) {
          fs.writeFileSync(file, prefix + (unicode ? encoded.replace("syntheticn", "synthetic\\u006e") : encoded));

          expect(processFile(file, [secret], [runtime])).toBeGreaterThan(0);

          const output = fs.readFileSync(file, "utf8");
          let decoded = output;
          try {
            decoded = JSON.parse(output);
          } catch {
            // A mixed record can remain text instead of a quoted JSON string.
          }
          expect(decoded).toBe(prefix + '"***"');
          expect(mockCore.setFailed).not.toHaveBeenCalled();
        }
      });

      it("redacts original configured numeric lexemes before a shorter runtime mask", () => {
        const { processFile } = require("./redact_secrets.cjs");
        const file = path.join(tempDir, "configured-numeric.json");
        fs.writeFileSync(file, '{"ids":[9007199254740993,12345678,1.2345678e+8],"safe":42}');

        expect(processFile(file, ["9007199254740993", "12345678", "1.2345678e+8"], ["123", "900"])).toBeGreaterThan(0);

        expect(JSON.parse(fs.readFileSync(file, "utf8"))).toEqual({ ids: ["***REDACTED***", "***REDACTED***", "***REDACTED***"], safe: 42 });
        expect(mockCore.setFailed).not.toHaveBeenCalled();
      });

      it("preserves generic JSON formatting and untouched numeric lexemes when configured values change", () => {
        const { processFile } = require("./redact_secrets.cjs");
        const file = path.join(tempDir, "formatted.json");
        fs.writeFileSync(file, '{\n  "text": "synthetic-password",\n  "ids": [9007199254740993, 1e2],\n  "escaped": "\\u0070ublic"\n}\n');

        expect(processFile(file, ["synthetic-password"])).toBeGreaterThan(0);

        expect(fs.readFileSync(file, "utf8")).toBe('{\n  "text": "***REDACTED***",\n  "ids": [9007199254740993, 1e2],\n  "escaped": "\\u0070ublic"\n}\n');
      });

      it("redacts decoded HTML session payloads and surrounding mixed text with one combined mask set", () => {
        const { processFile } = require("./redact_secrets.cjs");
        const secret = 'synthetic"\\\tpassword';
        const malformed = '"bad\t" "\\u006fpaque" token=' + secret;
        const payload = Buffer.from(JSON.stringify({ text: malformed, [secret]: secret }), "utf8").toString("base64");
        const file = path.join(tempDir, "session.html");
        fs.writeFileSync(file, `<html><script id="session-data">${payload}</script><pre>${malformed}</pre></html>`);

        expect(processFile(file, [secret], ["opaque", "synthetic"])).toBeGreaterThan(0);

        const raw = fs.readFileSync(file, "utf8");
        const output = raw.startsWith('"') ? JSON.parse(raw) : raw;
        const session = JSON.parse(Buffer.from(output.match(/<script id="session-data">([^<]+)<\/script>/)[1], "base64").toString("utf8"));
        const decodedText = JSON.parse(session.text);
        expect(decodedText).not.toContain(secret);
        expect(decodedText).not.toContain("password");
        expect(decodedText).not.toContain("opaque");
        expect(Object.keys(session)).toEqual(["text", "***REDACTED***"]);
        expect(session["***REDACTED***"]).toBe("***REDACTED***");
        const surrounding = output.slice(output.indexOf("<pre>"));
        expect(surrounding).not.toContain("password");
        expect(mockCore.setFailed).not.toHaveBeenCalled();
      });

      it("retains and sanitizes a private multi-megabyte artifact instead of failing closed on regex stack exhaustion", () => {
        const { processFile } = require("./redact_secrets.cjs");
        const file = path.join(tempDir, "large-record.jsonl");
        const mask = 'synthetic-"quoted"\\mask';
        fs.writeFileSync(file, JSON.stringify({ text: "x".repeat(10 * 1024 * 1024) + mask }), { mode: 0o600 });
        expect(processFile(file, [], [mask])).toBe(1);
        expect(fs.existsSync(file)).toBe(true);
        const parsed = JSON.parse(fs.readFileSync(file, "utf8"));
        expect(parsed.text.endsWith("***")).toBe(true);
        expect(parsed.text.length).toBe(10 * 1024 * 1024 + 3);
        expect(fs.statSync(file).mode & 0o777).toBe(0o600);
        expect(mockCore.setFailed).not.toHaveBeenCalled();
      });

      it.each(['"bad\\q ', '"bad\\u12gg ', '"bad\\u12 ', '"bad\u0001 ', '"bad\t ', '"bad\\v '])("sanitizes recoverable encoded masks after invalid JSON string prefixes (%j)", prefix => {
        const { processFile } = require("./redact_secrets.cjs");
        const file = path.join(tempDir, "malformed.log");
        fs.writeFileSync(file, prefix + '"\\u006fpaque"', { mode: 0o600 });
        expect(processFile(file, [], ["opaque"])).toBe(1);
        expect(fs.readFileSync(file, "utf8")).toBe(prefix + '"***"');
        expect(fs.statSync(file).mode & 0o777).toBe(0o600);
        expect(mockCore.setFailed).not.toHaveBeenCalled();
      });

      it.each(['"bad\t"', '"bad\\q"', '"bad\\u12gg"'])("conservatively sanitizes alternate quote pairings after malformed terminated tokens (%j)", prefix => {
        const { processFile } = require("./redact_secrets.cjs");
        const file = path.join(tempDir, "ambiguous.log");
        fs.writeFileSync(file, prefix + ' "\\u006fpaque"', { mode: 0o600 });
        expect(processFile(file, [], ["opaque"])).toBe(1);
        const redacted = fs.readFileSync(file, "utf8");
        expect(JSON.parse(redacted)).toBe(prefix + ' "***"');
        expect(redacted).not.toContain("\\u006fpaque");
        expect(fs.statSync(file).mode & 0o777).toBe(0o600);
        expect(mockCore.setFailed).not.toHaveBeenCalled();
      });

      it.each(["mixed.log", "malformed.json"])("removes %s if decoding a validated token unexpectedly fails", filename => {
        const { processFile } = require("./redact_secrets.cjs");
        const file = path.join(tempDir, filename);
        fs.writeFileSync(file, filename.endsWith(".json") ? '{"token":"\\u006fpaque", broken' : 'output: "\\u006fpaque"');
        const parse = JSON.parse;
        const parseSpy = vi.spyOn(JSON, "parse").mockImplementation(text => {
          if (text === '"\\u006fpaque"') throw new Error("opaque runtime-mask");
          return parse(text);
        });
        try {
          expect(processFile(file, [], ["opaque"])).toBe(0);
          expect(fs.existsSync(file)).toBe(false);
          expect(mockCore.warning).toHaveBeenCalledWith("Secret redaction failed (stage=redact, code=unknown)");
          expect(mockCore.warning.mock.calls.flat().join("\n")).not.toMatch(/opaque|runtime-mask/);
          expect(mockCore.setFailed).toHaveBeenCalled();
        } finally {
          parseSpy.mockRestore();
        }
      });

      it.each(["EACCES", "EPERM", "ENOSPC"])("reports only safe read diagnostics for secret-bearing %s errors", code => {
        const { processFile } = require("./redact_secrets.cjs");
        const file = path.join(tempDir, "secret-value.log");
        fs.writeFileSync(file, "secret-value");
        const read = vi.spyOn(fs, "readFileSync").mockImplementationOnce(() => {
          throw Object.assign(new Error("secret-value runtime-mask"), { code, path: "secret-value", payload: "runtime-mask" });
        });
        try {
          expect(processFile(file, ["secret-value"], ["runtime-mask"])).toBe(0);
          expect(fs.existsSync(file)).toBe(false);
          expect(mockCore.warning).toHaveBeenCalledWith(`Secret redaction failed (stage=read, code=${code})`);
          expect(mockCore.setFailed).toHaveBeenCalledWith("ERR_VALIDATION: Removed artifact source after secret redaction failed (input_bytes=unknown)");
          expect(mockCore.warning.mock.calls.flat().join("\n")).not.toMatch(/secret-value|runtime-mask/);
        } finally {
          read.mockRestore();
        }
      });
      it.each(["EACCES", "EPERM", "ENOSPC"])("retains the write boundary without leaking a secret-bearing %s error", code => {
        const { processFile } = require("./redact_secrets.cjs");
        const file = path.join(tempDir, "server.log");
        fs.writeFileSync(file, "secret-value");
        const write = vi.spyOn(fs, "writeFileSync").mockImplementation(() => {
          throw Object.assign(new Error("secret-value runtime-mask"), { code });
        });
        try {
          expect(processFile(file, ["secret-value"], ["runtime-mask"])).toBe(0);
          expect(fs.existsSync(file)).toBe(false);
          expect(fs.readdirSync(tempDir)).toEqual([]);
          expect(mockCore.warning).toHaveBeenCalledWith(`Secret redaction failed (stage=write, code=${code})`);
          expect(mockCore.warning.mock.calls.flat().join("\n")).not.toMatch(/secret-value|runtime-mask/);
          expect(mockCore.setFailed).toHaveBeenCalled();
        } finally {
          write.mockRestore();
        }
      });
      it.each(["EPERM", "ENOSPC"])("identifies failed atomic replacement (%s) while removing the source", code => {
        const { processFile } = require("./redact_secrets.cjs");
        const file = path.join(tempDir, "server.log");
        fs.writeFileSync(file, "secret-value");
        const write = vi.spyOn(fs, "writeFileSync").mockImplementationOnce(() => {
          throw Object.assign(new Error("secret-value"), { code: "EACCES" });
        });
        const rename = vi.spyOn(fs, "renameSync").mockImplementation(() => {
          throw Object.assign(new Error("secret-value"), { code });
        });
        try {
          expect(processFile(file, ["secret-value"], ["runtime-mask"])).toBe(0);
          expect(fs.existsSync(file)).toBe(false);
          expect(fs.readdirSync(tempDir)).toEqual([]);
          expect(mockCore.warning).toHaveBeenCalledWith(`Secret redaction failed (stage=replace, code=${code})`);
          expect(mockCore.warning.mock.calls.flat().join("\n")).not.toContain("secret-value");
          expect(mockCore.setFailed).toHaveBeenCalled();
        } finally {
          write.mockRestore();
          rename.mockRestore();
        }
      });
      it("preserves the primary diagnostic when replacement and temp cleanup both fail", () => {
        const { processFile } = require("./redact_secrets.cjs");
        const file = path.join(tempDir, "server.log");
        fs.writeFileSync(file, "secret-value");
        const write = vi.spyOn(fs, "writeFileSync").mockImplementationOnce(() => {
          throw Object.assign(new Error("secret-value runtime-mask"), { code: "EACCES" });
        });
        const rename = vi.spyOn(fs, "renameSync").mockImplementation(() => {
          throw Object.assign(new Error("secret-value runtime-mask"), { code: "ENOSPC" });
        });
        const remove = vi.spyOn(fs, "rmSync").mockImplementationOnce(() => {
          throw Object.assign(new Error("secret-value runtime-mask"), { code: "EPERM" });
        });
        try {
          expect(processFile(file, ["secret-value"], ["runtime-mask"])).toBe(0);
          expect(fs.existsSync(file)).toBe(false);
          expect(mockCore.warning).toHaveBeenCalledWith("Secret redaction failed (stage=replace, code=ENOSPC)");
          expect(mockCore.warning).toHaveBeenCalledWith("Secret redaction failed (stage=unlink, code=EPERM)");
          expect(mockCore.warning.mock.calls.flat().join("\n")).not.toMatch(/secret-value|runtime-mask/);
          expect(mockCore.setFailed).toHaveBeenCalledWith("ERR_VALIDATION: Removed artifact source after secret redaction failed (input_bytes=12)");
        } finally {
          write.mockRestore();
          rename.mockRestore();
          remove.mockRestore();
        }
      });
      it("keeps a successfully replaced artifact when temporary-directory cleanup fails", () => {
        const { processFile } = require("./redact_secrets.cjs");
        const file = path.join(tempDir, "server.log");
        fs.writeFileSync(file, "secret-value");
        const write = vi.spyOn(fs, "writeFileSync").mockImplementationOnce(() => {
          throw Object.assign(new Error("secret-value runtime-mask"), { code: "EACCES" });
        });
        const remove = vi.spyOn(fs, "rmSync").mockImplementationOnce(() => {
          throw Object.assign(new Error("secret-value runtime-mask"), { code: "EBUSY" });
        });
        try {
          expect(processFile(file, ["secret-value"], ["runtime-mask"])).toBe(1);
          expect(fs.existsSync(file)).toBe(true);
          expect(fs.readFileSync(file, "utf8")).not.toContain("secret-value");
          expect(mockCore.warning).toHaveBeenCalledWith("Failed to remove temporary redaction directory: Secret redaction failed (stage=unlink, code=EBUSY)");
          expect(mockCore.warning.mock.calls.flat().join("\n")).not.toMatch(/secret-value|runtime-mask/);
          expect(mockCore.setFailed).not.toHaveBeenCalled();
        } finally {
          write.mockRestore();
          remove.mockRestore();
        }
      });
      it.each([undefined, "secret-value", { secret: "runtime-mask" }])("does not publish unknown error codes %j", code => {
        const { processFile } = require("./redact_secrets.cjs");
        const file = path.join(tempDir, "server.log");
        fs.writeFileSync(file, "secret-value");
        const read = vi.spyOn(fs, "readFileSync").mockImplementationOnce(() => {
          throw Object.assign(new Error("secret-value runtime-mask"), { code });
        });
        try {
          expect(processFile(file, ["secret-value"])).toBe(0);
          expect(fs.existsSync(file)).toBe(false);
          expect(mockCore.warning).toHaveBeenCalledExactlyOnceWith("Secret redaction failed (stage=read, code=unknown)");
        } finally {
          read.mockRestore();
        }
      });
      it("keeps processing errors secret-safe at the redact boundary", () => {
        const { processFile } = require("./redact_secrets.cjs");
        const file = path.join(tempDir, "server.log");
        fs.writeFileSync(file, "secret-value");
        const split = String.prototype.split;
        const spy = vi.spyOn(String.prototype, "split").mockImplementation(function (...args) {
          if (this === "secret-value") throw new Error("secret-value runtime-mask");
          return split.apply(this, args);
        });
        try {
          expect(processFile(file, ["secret-value"], ["runtime-mask"])).toBe(0);
          expect(fs.existsSync(file)).toBe(false);
          expect(mockCore.warning).toHaveBeenCalledExactlyOnceWith("Secret redaction failed (stage=redact, code=unknown)");
          expect(mockCore.setFailed).toHaveBeenCalled();
        } finally {
          spy.mockRestore();
        }
      });
      it("reports unlink failures without retaining secret-bearing exceptions", () => {
        const { processFile } = require("./redact_secrets.cjs");
        const file = path.join(tempDir, "server.log");
        fs.writeFileSync(file, "secret-value");
        const read = vi.spyOn(fs, "readFileSync").mockImplementationOnce(() => {
          throw Object.assign(new Error("secret-value"), { code: "EACCES" });
        });
        const unlink = vi.spyOn(fs, "unlinkSync").mockImplementationOnce(() => {
          throw Object.assign(new Error("runtime-mask"), { code: "EPERM" });
        });
        try {
          expect(() => processFile(file, ["secret-value"], ["runtime-mask"])).toThrow(
            expect.objectContaining({
              message: "ERR_VALIDATION: Failed to remove artifact source after secret redaction failed (input_bytes=unknown)",
              errors: [expect.objectContaining({ message: "Secret redaction failed (stage=read, code=EACCES)" }), expect.objectContaining({ message: "Secret redaction failed (stage=unlink, code=EPERM)" })],
            })
          );
          expect(fs.existsSync(file)).toBe(true);
          expect(mockCore.warning).toHaveBeenCalledWith("Secret redaction failed (stage=unlink, code=EPERM)");
          expect(mockCore.warning.mock.calls.flat().join("\n")).not.toMatch(/secret-value|runtime-mask/);
        } finally {
          read.mockRestore();
          unlink.mockRestore();
        }
      });
      it.each([false, true])("processes runner-owned read-only files in read-only Go module directories (secrets: %s)", hasSecrets => {
        const { processFile } = require("./redact_secrets.cjs");
        const moduleDir = path.join(tempDir, "go-mod");
        fs.mkdirSync(moduleDir);
        const file = path.join(moduleDir, "README.md");
        fs.writeFileSync(file, "secret-value", { mode: 0o444 });
        fs.chmodSync(moduleDir, 0o555);
        const inode = fs.statSync(file).ino;
        try {
          expect(processFile(file, hasSecrets ? ["secret-value"] : [], [], true)).toBe(hasSecrets ? 1 : 0);
          expect(fs.readFileSync(file, "utf8")).toBe(hasSecrets ? "***REDACTED***" : "secret-value");
          expect(fs.statSync(file).ino).toBe(inode);
          expect(fs.statSync(file).mode & 0o777).toBe(0o600);
          expect(fs.statSync(moduleDir).mode & 0o777).toBe(0o555);
          fs.writeFileSync(file, "[custom redaction]");
          expect(fs.readFileSync(file, "utf8")).toBe("[custom redaction]");
          expect(fs.readdirSync(moduleDir)).toEqual(["README.md"]);
          expect(mockCore.setFailed).not.toHaveBeenCalled();
        } finally {
          fs.chmodSync(moduleDir, 0o700);
        }
      });
      it.each(["EACCES", "EPERM"])("prepares unchanged non-writable logs for custom masking (%s)", code => {
        const { processFile } = require("./redact_secrets.cjs");
        const file = path.join(tempDir, "server.log");
        fs.writeFileSync(file, "custom-sensitive-text", { mode: 0o444 });
        const inode = fs.statSync(file).ino;
        const access = fs.accessSync;
        const spy = vi.spyOn(fs, "accessSync").mockImplementation((target, mode) => {
          if (target === file) throw Object.assign(new Error("permission denied"), { code });
          return access(target, mode);
        });
        try {
          expect(processFile(file, [], [], true)).toBe(0);
          expect(fs.readFileSync(file, "utf8")).toBe("custom-sensitive-text");
          expect(fs.statSync(file).ino).not.toBe(inode);
          expect(fs.statSync(file).mode & 0o777).toBe(0o600);
          fs.writeFileSync(file, "[custom redaction]");
          expect(fs.readFileSync(file, "utf8")).toBe("[custom redaction]");
          expect(fs.readdirSync(tempDir)).toEqual(["server.log"]);
          expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Replaced non-writable file"));
          expect(mockCore.setFailed).not.toHaveBeenCalled();
        } finally {
          spy.mockRestore();
        }
      });
      it.each(["EACCES", "EPERM"])("fails closed without disclosing secrets in filenames or errors when permissions cannot be changed (%s)", code => {
        const { processFile } = require("./redact_secrets.cjs");
        const moduleDir = path.join(tempDir, "go-mod");
        fs.mkdirSync(moduleDir);
        const file = path.join(moduleDir, "secret-value-README.md");
        fs.writeFileSync(file, "secret-value", { mode: 0o444 });
        fs.chmodSync(moduleDir, 0o555);
        const chmod = vi.spyOn(fs, "chmodSync").mockImplementation(() => {
          throw Object.assign(new Error("permission denied: secret-value"), { code });
        });
        try {
          expect(() => processFile(file, ["secret-value"], ["runtime-mask"])).toThrow(/^ERR_VALIDATION: Failed to remove artifact source after secret redaction failed \(input_bytes=12\)$/);
          expect(fs.readFileSync(file, "utf8")).toBe("secret-value");
          expect(fs.statSync(file).mode & 0o777).toBe(0o444);
          expect(mockCore.info.mock.calls.flat().join("\n")).not.toContain("secret-value");
          expect(mockCore.warning.mock.calls.flat().join("\n")).not.toContain("secret-value");
        } finally {
          chmod.mockRestore();
          fs.chmodSync(moduleDir, 0o700);
        }
      });
      it("atomically replaces container-owned logs when writing redacted content is denied", () => {
        const { processFile } = require("./redact_secrets.cjs");
        const file = path.join(tempDir, "server.log");
        fs.writeFileSync(file, "secret-value");
        const write = fs.writeFileSync;
        const spy = vi.spyOn(fs, "writeFileSync").mockImplementation((target, ...args) => {
          if (target === file) throw Object.assign(new Error("permission denied"), { code: "EACCES" });
          return write(target, ...args);
        });
        try {
          expect(processFile(file, ["secret-value"])).toBe(1);
          expect(fs.readFileSync(file, "utf8")).toBe("***REDACTED***");
          expect(fs.statSync(file).mode & 0o777).toBe(0o600);
          expect(fs.readdirSync(tempDir)).toEqual(["server.log"]);
          expect(mockCore.setFailed).not.toHaveBeenCalled();
        } finally {
          spy.mockRestore();
        }
      });
      it("leaves writable files without matches unchanged", () => {
        const { processFile } = require("./redact_secrets.cjs");
        const file = path.join(tempDir, "server.log");
        fs.writeFileSync(file, "no secrets", { mode: 0o640 });
        const before = fs.statSync(file);
        expect(processFile(file, [])).toBe(0);
        const after = fs.statSync(file);
        expect(after.ino).toBe(before.ino);
        expect(after.mode).toBe(before.mode);
        expect(after.mtimeMs).toBe(before.mtimeMs);
      });
      it.each(["EACCES", "EPERM"])("does not require write access to sanitized files without custom masking (%s)", code => {
        const { processFile } = require("./redact_secrets.cjs");
        const directory = path.join(tempDir, "container-logs");
        fs.mkdirSync(directory);
        const file = path.join(directory, "server.log");
        fs.writeFileSync(file, "safe log content", { mode: 0o444 });
        fs.chmodSync(directory, 0o555);
        const before = fs.statSync(file);
        const access = vi.spyOn(fs, "accessSync").mockImplementation(() => {
          throw Object.assign(new Error("permission denied"), { code });
        });
        const chmod = vi.spyOn(fs, "chmodSync").mockImplementation(() => {
          throw Object.assign(new Error("container-owned file"), { code });
        });
        const unlink = vi.spyOn(fs, "unlinkSync").mockImplementation(() => {
          throw Object.assign(new Error("container-owned directory"), { code });
        });
        try {
          expect(processFile(file, ["secret-value"], ["runtime-mask"])).toBe(0);
          expect(fs.readFileSync(file, "utf8")).toBe("safe log content");
          const after = fs.statSync(file);
          expect(after.ino).toBe(before.ino);
          expect(after.mode).toBe(before.mode);
          expect(after.mtimeMs).toBe(before.mtimeMs);
          expect(access).not.toHaveBeenCalled();
          expect(chmod).not.toHaveBeenCalled();
          expect(unlink).not.toHaveBeenCalled();
          expect(mockCore.setFailed).not.toHaveBeenCalled();
        } finally {
          access.mockRestore();
          chmod.mockRestore();
          unlink.mockRestore();
          fs.chmodSync(directory, 0o700);
        }
      });
      it.each(["missing.log", "missing.html"])("tolerates disappeared files even with runtime masks (%s)", name => {
        const { processFile } = require("./redact_secrets.cjs");
        expect(processFile(path.join(tempDir, name), ["secret-value"], ["runtime-mask"])).toBe(0);
        expect(mockCore.setFailed).not.toHaveBeenCalled();
        expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Skipping file that disappeared"));
        expect(fs.readdirSync(tempDir)).toEqual([]);
      });
      it("removes an unsanitized file and reports failure when replacement is blocked", () => {
        const { processFile } = require("./redact_secrets.cjs");
        const file = path.join(tempDir, "server.log");
        fs.writeFileSync(file, "secret-value");
        const write = fs.writeFileSync;
        const spy = vi.spyOn(fs, "writeFileSync").mockImplementation((target, ...args) => {
          if (target === file) throw Object.assign(new Error("permission denied"), { code: "EACCES" });
          return write(target, ...args);
        });
        const rename = vi.spyOn(fs, "renameSync").mockImplementation(() => {
          throw Object.assign(new Error("file locked"), { code: "EBUSY" });
        });
        try {
          expect(processFile(file, ["secret-value"])).toBe(0);
          expect(fs.existsSync(file)).toBe(false);
          expect(fs.readdirSync(tempDir)).toEqual([]);
          expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("Removed artifact source"));
        } finally {
          spy.mockRestore();
          rename.mockRestore();
        }
      });
      it("does not treat missing temporary storage as a missing artifact", () => {
        const { processFile } = require("./redact_secrets.cjs");
        const file = path.join(tempDir, "server.log");
        fs.writeFileSync(file, "secret-value");
        const write = vi.spyOn(fs, "writeFileSync").mockImplementation(() => {
          throw Object.assign(new Error("missing temporary directory"), { code: "ENOENT" });
        });
        try {
          expect(processFile(file, ["secret-value"])).toBe(0);
          expect(fs.existsSync(file)).toBe(false);
          expect(mockCore.setFailed).toHaveBeenCalled();
        } finally {
          write.mockRestore();
        }
      });
      it("surfaces failure when an unsanitized file cannot be replaced or removed", () => {
        const { processFile } = require("./redact_secrets.cjs");
        const file = path.join(tempDir, "server.log");
        fs.writeFileSync(file, "secret-value");
        const write = vi.spyOn(fs, "writeFileSync").mockImplementation(() => {
          throw Object.assign(new Error("permission denied"), { code: "EACCES" });
        });
        const unlink = vi.spyOn(fs, "unlinkSync").mockImplementation(() => {
          throw Object.assign(new Error("permission denied"), { code: "EACCES" });
        });
        try {
          expect(() => processFile(file, ["secret-value"])).toThrow("Failed to remove artifact source");
        } finally {
          write.mockRestore();
          unlink.mockRestore();
        }
      });
    }),
    describe("main function integration", () => {
      it.each([false, true])("prepares unchanged files only when custom masking is configured (%s)", prepareForCustomMasking => {
        const { processFile } = require("./redact_secrets.cjs");
        const file = path.join(tempDir, "server.log");
        fs.writeFileSync(file, "custom-sensitive-text", { mode: 0o444 });
        expect(processFile(file, [], [], prepareForCustomMasking)).toBe(0);
        expect(fs.readFileSync(file, "utf8")).toBe("custom-sensitive-text");
        expect(fs.statSync(file).mode & 0o777).toBe(prepareForCustomMasking ? 0o600 : 0o444);
        expect(mockCore.setFailed).not.toHaveBeenCalled();
      });
      it.each([false, true])("passes custom masking configuration through the full scan (%s)", prepareForCustomMasking => {
        const file = path.join(tempDir, "server.log");
        fs.writeFileSync(file, "custom-sensitive-text", { mode: 0o444 });
        const modifiedScript = redactScript.replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir}", targetExtensions)`);
        return eval(`(async () => { ${modifiedScript}; await main({ prepareForCustomMasking: ${prepareForCustomMasking} }); })()`).then(() => {
          expect(fs.statSync(file).mode & 0o777).toBe(prepareForCustomMasking ? 0o600 : 0o444);
          expect(mockCore.setFailed).not.toHaveBeenCalled();
        });
      });
      it("continues scanning when a nested directory disappears", () => {
        const { findFiles } = require("./redact_secrets.cjs");
        const missing = path.join(tempDir, "missing");
        fs.mkdirSync(missing);
        fs.writeFileSync(path.join(tempDir, "server.log"), "readable");
        const readDir = fs.readdirSync;
        const spy = vi.spyOn(fs, "readdirSync").mockImplementation((dir, options) => {
          if (dir === missing) throw Object.assign(new Error("missing directory"), { code: "ENOENT" });
          return readDir(dir, options);
        });
        try {
          expect(findFiles(tempDir, [".log"])).toEqual([path.join(tempDir, "server.log")]);
          expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Skipping directory that disappeared"));
        } finally {
          spy.mockRestore();
        }
      });
      it("continues scanning when a symbolic link disappears before removal", () => {
        const { findFiles } = require("./redact_secrets.cjs");
        const link = path.join(tempDir, "linked.log");
        fs.symlinkSync(path.join(tempDir, "missing-target"), link);
        const unlink = fs.unlinkSync;
        const spy = vi.spyOn(fs, "unlinkSync").mockImplementation(target => {
          if (target === link) {
            unlink(target);
            throw Object.assign(new Error("symbolic link disappeared"), { code: "ENOENT" });
          }
          return unlink(target);
        });
        try {
          expect(findFiles(tempDir, [".log"])).toEqual([]);
          expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Skipping symbolic link that disappeared"));
        } finally {
          spy.mockRestore();
        }
      });
      it("fails closed when agent stdio disappears before mask collection", async () => {
        const stdio = path.join(tempDir, "agent-stdio.log");
        const artifact = path.join(tempDir, "server.log");
        fs.writeFileSync(stdio, "no runtime masks");
        fs.writeFileSync(artifact, "secret-value");
        process.env.GH_AW_SECRET_NAMES = "TEST_SECRET";
        process.env.SECRET_TEST_SECRET = "secret-value";
        const read = fs.readFileSync;
        const spy = vi.spyOn(fs, "readFileSync").mockImplementation((file, ...args) => {
          if (file === stdio && fs.existsSync(stdio)) fs.unlinkSync(stdio);
          return read(file, ...args);
        });
        try {
          const modifiedScript = redactScript.replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir}", targetExtensions)`);
          await eval(`(async () => { ${modifiedScript}; await main(); })()`);
          expect(fs.existsSync(artifact)).toBe(false);
          expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("Removed artifact sources after runtime mask collection failed"));
        } finally {
          spy.mockRestore();
        }
      });
      it("continues redacting artifact files when a directory is inaccessible", async () => {
        const cache = path.join(tempDir, "aw-mcp");
        const artifact = path.join(tempDir, "agent-stdio.log");
        fs.mkdirSync(cache);
        fs.writeFileSync(artifact, "secret-value");
        process.env.GH_AW_SECRET_NAMES = "TEST_SECRET";
        process.env.SECRET_TEST_SECRET = "secret-value";
        const readDir = fs.readdirSync;
        const readDirSpy = vi.spyOn(fs, "readdirSync").mockImplementation((dir, options) => {
          if (dir === cache) throw Object.assign(new Error("permission denied"), { code: "EACCES" });
          return readDir(dir, options);
        });
        try {
          const modifiedScript = redactScript.replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir}", targetExtensions)`);
          await eval(`(async () => { ${modifiedScript}; await main(); })()`);
          expect(readDirSpy).toHaveBeenCalledWith(cache, expect.anything());
          expect(fs.readFileSync(artifact, "utf8")).toBe("***REDACTED***");
          expect(mockCore.setFailed).not.toHaveBeenCalled();
          expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Skipping inaccessible directory"));
        } finally {
          readDirSpy.mockRestore();
        }
      });
      it("redacts readable MCP artifact logs", async () => {
        const logs = path.join(tempDir, "aw-mcp", "logs");
        const artifact = path.join(logs, "runs.json");
        fs.mkdirSync(logs, { recursive: true });
        fs.writeFileSync(artifact, '{"token":"secret-value"}');
        process.env.GH_AW_SECRET_NAMES = "TEST_SECRET";
        process.env.SECRET_TEST_SECRET = "secret-value";
        const modifiedScript = redactScript.replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir}", targetExtensions)`);
        await eval(`(async () => { ${modifiedScript}; await main(); })()`);
        expect(fs.readFileSync(artifact, "utf8")).toBe('{"token":"***REDACTED***"}');
        expect(mockCore.setFailed).not.toHaveBeenCalled();
      });
      it("redacts decoded keys and values in nested Claim diagnostics using agent runtime masks", async () => {
        const directory = path.join(tempDir, "claims", "original-claim");
        fs.mkdirSync(directory, { recursive: true, mode: 0o700 });
        const secret = 'runtime"\\opaque\nsecond-line';
        const encodedMask = secret.replace(/%/g, "%25").replace(/\r/g, "%0D").replace(/\n/g, "%0A");
        fs.writeFileSync(path.join(tempDir, "agent-stdio.log"), `::add-mask::${encodedMask}\n`);
        const filenames = ["safe-output-items.jsonl", "temporary-id-map.json", "safe-output-errors.json"];
        for (const filename of filenames) fs.writeFileSync(path.join(directory, filename), JSON.stringify({ [secret]: { value: secret } }) + "\n", { mode: 0o600 });
        const modifiedScript = redactScript.replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir}", targetExtensions)`);
        await eval(`(async () => { ${modifiedScript}; await main(); })()`);
        for (const filename of filenames) {
          const file = path.join(directory, filename);
          expect(JSON.parse(fs.readFileSync(file, "utf8"))).toEqual({ "***\n***": { value: "***\n***" } });
          expect(fs.statSync(file).mode & 0o777).toBe(0o600);
        }
        expect(mockCore.setFailed).not.toHaveBeenCalled();
      });
      (it("should scan for built-in patterns even when GH_AW_SECRET_NAMES is not set", async () => {
        const { main } = require("./redact_secrets.cjs");
        const directory = path.join(process.env.RUNNER_TEMP, "gh-aw");
        const artifact = path.join(directory, "token.log");
        const token = "ghp_" + "a".repeat(36);
        fs.mkdirSync(directory, { recursive: true });
        fs.writeFileSync(artifact, `Token: ${token}`);

        await main();

        expect(fs.readFileSync(artifact, "utf8")).toBe("Token: ***REDACTED***");
        expect(mockCore.info).not.toHaveBeenCalled();
        expect(mockCore.setFailed).not.toHaveBeenCalled();
      }),
        it("should redact secrets from files in /tmp using exact matching", async () => {
          const testFile = path.join(tempDir, "test.txt"),
            secretValue = "ghp_1234567890ABCDEFGHIJKLMNOPQRSTUVWxyz";
          (fs.writeFileSync(testFile, `Secret: ${secretValue} and another ${secretValue}`), (process.env.GH_AW_SECRET_NAMES = "GITHUB_TOKEN"), (process.env.SECRET_GITHUB_TOKEN = secretValue));
          const modifiedScript = redactScript.replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir.replace(/\\/g, "\\\\")}", targetExtensions)`);
          await eval(`(async () => { ${modifiedScript}; await main(); })()`);
          const redactedContent = fs.readFileSync(testFile, "utf8");
          (expect(redactedContent).toBe("Secret: ***REDACTED*** and another ***REDACTED***"), expect(mockCore.info).not.toHaveBeenCalled(), expect(mockCore.setFailed).not.toHaveBeenCalled());
        }),
        it("should handle multiple file types", async () => {
          (fs.writeFileSync(path.join(tempDir, "test1.txt"), "Secret: api-key-123"),
            fs.writeFileSync(path.join(tempDir, "test2.json"), '{"key": "api-key-456"}'),
            fs.writeFileSync(path.join(tempDir, "test3.log"), "Log: api-key-789"),
            (process.env.GH_AW_SECRET_NAMES = "API_KEY1,API_KEY2,API_KEY3"),
            (process.env.SECRET_API_KEY1 = "api-key-123"),
            (process.env.SECRET_API_KEY2 = "api-key-456"),
            (process.env.SECRET_API_KEY3 = "api-key-789"));
          const modifiedScript = redactScript.replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir.replace(/\\/g, "\\\\")}", targetExtensions)`);
          (await eval(`(async () => { ${modifiedScript}; await main(); })()`),
            expect(fs.readFileSync(path.join(tempDir, "test1.txt"), "utf8")).toBe("Secret: ***REDACTED***"),
            expect(fs.readFileSync(path.join(tempDir, "test2.json"), "utf8")).toBe('{"key": "***REDACTED***"}'),
            expect(fs.readFileSync(path.join(tempDir, "test3.log"), "utf8")).toBe("Log: ***REDACTED***"));
        }),
        it("should remove symbolic links before artifact upload", async () => {
          const target = path.join(tempDir, "target.json");
          const link = path.join(tempDir, "linked.json");
          fs.writeFileSync(target, '{"safe":true}');
          fs.symlinkSync(target, link);
          const modifiedScript = redactScript.replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir.replace(/\\/g, "\\\\")}", targetExtensions)`);
          await eval(`(async () => { ${modifiedScript}; await main(); })()`);
          expect(fs.existsSync(link)).toBe(false);
          expect(fs.existsSync(target)).toBe(true);
          expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Removed symbolic link before artifact upload"));
        }),
        it("should not log successful redaction hits", async () => {
          const testFile = path.join(tempDir, "test.txt"),
            secretValue = "sk-1234567890";
          (fs.writeFileSync(testFile, `Secret: ${secretValue} and ${secretValue}`), (process.env.GH_AW_SECRET_NAMES = "API_KEY"), (process.env.SECRET_API_KEY = secretValue));
          const modifiedScript = redactScript.replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir.replace(/\\/g, "\\\\")}", targetExtensions)`);
          (await eval(`(async () => { ${modifiedScript}; await main(); })()`), expect(mockCore.info).not.toHaveBeenCalled(), expect(fs.readFileSync(testFile, "utf8")).toBe("Secret: ***REDACTED*** and ***REDACTED***"));
        }),
        it("should not log actual secret values", async () => {
          const testFile = path.join(tempDir, "test.txt"),
            secretValue = "sk-very-secret-key-123";
          (fs.writeFileSync(testFile, `Secret: ${secretValue}`), (process.env.GH_AW_SECRET_NAMES = "SECRET_KEY"), (process.env.SECRET_SECRET_KEY = secretValue));
          const modifiedScript = redactScript.replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir.replace(/\\/g, "\\\\")}", targetExtensions)`);
          await eval(`(async () => { ${modifiedScript}; await main(); })()`);
          const allCalls = [...mockCore.debug.mock.calls, ...mockCore.info.mock.calls, ...mockCore.warning.mock.calls];
          for (const call of allCalls) {
            const callString = JSON.stringify(call);
            expect(callString).not.toContain(secretValue);
          }
        }),
        it("should skip very short values (less than 6 characters)", async () => {
          const testFile = path.join(tempDir, "test.txt");
          (fs.writeFileSync(testFile, "Short: 12345"), (process.env.GH_AW_SECRET_NAMES = "SHORT_SECRET"), (process.env.SECRET_SHORT_SECRET = "12345"));
          const modifiedScript = redactScript.replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir.replace(/\\/g, "\\\\")}", targetExtensions)`);
          (await eval(`(async () => { ${modifiedScript}; await main(); })()`), expect(fs.readFileSync(testFile, "utf8")).toBe("Short: 12345"));
        }),
        it("should redact 6-character secrets", async () => {
          const testFile = path.join(tempDir, "test.txt");
          const secretValue = "abc123";
          (fs.writeFileSync(testFile, `Secret: ${secretValue} test`), (process.env.GH_AW_SECRET_NAMES = "SIX_CHAR_SECRET"), (process.env.SECRET_SIX_CHAR_SECRET = secretValue));
          const modifiedScript = redactScript.replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir.replace(/\\/g, "\\\\")}", targetExtensions)`);
          (await eval(`(async () => { ${modifiedScript}; await main(); })()`), expect(fs.readFileSync(testFile, "utf8")).toBe("Secret: ***REDACTED*** test"));
        }),
        it("should handle multiple secrets in same file", async () => {
          const testFile = path.join(tempDir, "test.txt"),
            secret1 = "ghp_1234567890ABCDEFGHIJKLMNOPQRSTUVWxyz",
            secret2 = "sk-proj-abcdef1234567890";
          (fs.writeFileSync(testFile, `Token1: ${secret1}\nToken2: ${secret2}\nToken1 again: ${secret1}`), (process.env.GH_AW_SECRET_NAMES = "TOKEN1,TOKEN2"), (process.env.SECRET_TOKEN1 = secret1), (process.env.SECRET_TOKEN2 = secret2));
          const modifiedScript = redactScript.replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir.replace(/\\/g, "\\\\")}", targetExtensions)`);
          await eval(`(async () => { ${modifiedScript}; await main(); })()`);
          const redacted = fs.readFileSync(testFile, "utf8");
          expect(redacted).toBe("Token1: ***REDACTED***\nToken2: ***REDACTED***\nToken1 again: ***REDACTED***");
        }),
        it("should handle empty secret values gracefully", async () => {
          const testFile = path.join(tempDir, "test.txt");
          (fs.writeFileSync(testFile, "No secrets here"), (process.env.GH_AW_SECRET_NAMES = "EMPTY_SECRET"), (process.env.SECRET_EMPTY_SECRET = ""));
          const modifiedScript = redactScript.replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir.replace(/\\/g, "\\\\")}", targetExtensions)`);
          (await eval(`(async () => { ${modifiedScript}; await main(); })()`), expect(mockCore.info).not.toHaveBeenCalled(), expect(mockCore.setFailed).not.toHaveBeenCalled());
        }),
        it("should handle new file extensions (.md, .mdx, .yml, .jsonl)", async () => {
          (fs.writeFileSync(path.join(tempDir, "test.md"), "# Markdown\nSecret: api-key-md123"),
            fs.writeFileSync(path.join(tempDir, "test.mdx"), "# MDX\nSecret: api-key-mdx123"),
            fs.writeFileSync(path.join(tempDir, "test.yml"), "# YAML\nkey: api-key-yml123"),
            fs.writeFileSync(path.join(tempDir, "test.jsonl"), '{"key": "api-key-jsonl123"}'),
            (process.env.GH_AW_SECRET_NAMES = "API_MD,API_MDX,API_YML,API_JSONL"),
            (process.env.SECRET_API_MD = "api-key-md123"),
            (process.env.SECRET_API_MDX = "api-key-mdx123"),
            (process.env.SECRET_API_YML = "api-key-yml123"),
            (process.env.SECRET_API_JSONL = "api-key-jsonl123"));
          const modifiedScript = redactScript.replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir.replace(/\\/g, "\\\\")}", targetExtensions)`);
          (await eval(`(async () => { ${modifiedScript}; await main(); })()`),
            expect(fs.readFileSync(path.join(tempDir, "test.md"), "utf8")).toBe("# Markdown\nSecret: ***REDACTED***"),
            expect(fs.readFileSync(path.join(tempDir, "test.mdx"), "utf8")).toBe("# MDX\nSecret: ***REDACTED***"),
            expect(fs.readFileSync(path.join(tempDir, "test.yml"), "utf8")).toBe("# YAML\nkey: ***REDACTED***"),
            expect(fs.readFileSync(path.join(tempDir, "test.jsonl"), "utf8")).toBe('{"key": "***REDACTED***"}'));
        }),
        it("should redact secrets from .patch files (exact-value and built-in credential patterns)", async () => {
          const patchContent = [
            "diff --git a/config.js b/config.js",
            "--- a/config.js",
            "+++ b/config.js",
            "@@ -1,3 +1,3 @@",
            "-const token = 'old';",
            "+const token = 'my-patch-secret-value';",
            " const url = 'https://api.example.com';",
          ].join("\n");
          const patchFile = path.join(tempDir, "aw-abcdef.patch");
          fs.writeFileSync(patchFile, patchContent);
          process.env.GH_AW_SECRET_NAMES = "PATCH_SECRET";
          process.env.SECRET_PATCH_SECRET = "my-patch-secret-value";
          const modifiedScript = redactScript.replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir.replace(/\\/g, "\\\\")}", targetExtensions)`);
          await eval(`(async () => { ${modifiedScript}; await main(); })()`);
          const redacted = fs.readFileSync(patchFile, "utf8");
          expect(redacted).not.toContain("my-patch-secret-value");
          expect(redacted).toContain("***REDACTED***");
        }),
        it("should redact built-in credential patterns from .patch files", async () => {
          const ghToken = "ghp_" + "A".repeat(36);
          const patchContent = `diff --git a/creds.txt b/creds.txt\n+TOKEN=${ghToken}\n`;
          const patchFile = path.join(tempDir, "aw-builtin.patch");
          fs.writeFileSync(patchFile, patchContent);
          process.env.GH_AW_SECRET_NAMES = "";
          const modifiedScript = redactScript.replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir.replace(/\\/g, "\\\\")}", targetExtensions)`);
          await eval(`(async () => { ${modifiedScript}; await main(); })()`);
          const redacted = fs.readFileSync(patchFile, "utf8");
          expect(redacted).not.toContain(ghToken);
          expect(redacted).toContain("***REDACTED***");
          expect(mockCore.info).not.toHaveBeenCalled();
        }));
    }),
    describe("built-in pattern detection", () => {
      describe("GitHub tokens", () => {
        it("should redact GitHub Personal Access Token (ghp_)", async () => {
          const testFile = path.join(tempDir, "test.txt");
          const ghToken = "ghp_1234567890ABCDEFGHIJKLMNOPQRSTUVWxyz";
          fs.writeFileSync(testFile, `Using token: ${ghToken} in this file`);
          process.env.GH_AW_SECRET_NAMES = "";
          const modifiedScript = redactScript.replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir.replace(/\\/g, "\\\\")}", targetExtensions)`);
          await eval(`(async () => { ${modifiedScript}; await main(); })()`);
          const redacted = fs.readFileSync(testFile, "utf8");
          expect(redacted).toBe("Using token: ***REDACTED*** in this file");
          expect(mockCore.info).not.toHaveBeenCalled();
        });

        it("should redact GitHub Server-to-Server Token (ghs_)", async () => {
          const testFile = path.join(tempDir, "test.txt");
          const ghToken = "ghs_0123456789ABCDEFGHIJKLMNOPQRSTUVWxyz";
          fs.writeFileSync(testFile, `Server token: ${ghToken}`);
          process.env.GH_AW_SECRET_NAMES = "";
          const modifiedScript = redactScript.replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir.replace(/\\/g, "\\\\")}", targetExtensions)`);
          await eval(`(async () => { ${modifiedScript}; await main(); })()`);
          const redacted = fs.readFileSync(testFile, "utf8");
          expect(redacted).toBe("Server token: ***REDACTED***");
          expect(mockCore.info).not.toHaveBeenCalled();
        });

        it("should redact long JWT-like GitHub Server-to-Server Token (ghs_)", async () => {
          const testFile = path.join(tempDir, "test.txt");
          const tokenSegments = [
            Buffer.from(JSON.stringify({ alg: "HS256", typ: "JWT" })).toString("base64url"),
            Buffer.from(JSON.stringify({ installation_id: 123, repository: "gh-aw", scope: "read_write" })).toString("base64url"),
            `sig_${"A".repeat(40)}`,
          ];
          const ghToken = `${["gh", "s_"].join("")}${tokenSegments.join(".")}`;
          expect(ghToken).toMatch(/^ghs_[0-9A-Za-z._-]{36,}$/);
          fs.writeFileSync(testFile, `Long server token: ${ghToken}`);
          process.env.GH_AW_SECRET_NAMES = "";
          const modifiedScript = redactScript.replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir.replace(/\\/g, "\\\\")}", targetExtensions)`);
          await eval(`(async () => { ${modifiedScript}; await main(); })()`);
          const redacted = fs.readFileSync(testFile, "utf8");
          expect(redacted).toBe("Long server token: ***REDACTED***");
          expect(mockCore.info).not.toHaveBeenCalled();
        });

        it("should redact boundary-length JWT-like GitHub Server-to-Server Token (ghs_)", async () => {
          const testFile = path.join(tempDir, "test.txt");
          const ghToken = "ghs_abcdefghijk.lmnopqrstuv.wxyzABCDEFGH";
          expect(ghToken.slice(4)).toHaveLength(36);
          expect(ghToken).toMatch(/^ghs_[0-9A-Za-z._-]{36,}$/);
          fs.writeFileSync(testFile, `Boundary server token: ${ghToken}`);
          process.env.GH_AW_SECRET_NAMES = "";
          const modifiedScript = redactScript.replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir.replace(/\\/g, "\\\\")}", targetExtensions)`);
          await eval(`(async () => { ${modifiedScript}; await main(); })()`);
          const redacted = fs.readFileSync(testFile, "utf8");
          expect(redacted).toBe("Boundary server token: ***REDACTED***");
          expect(mockCore.info).not.toHaveBeenCalled();
        });

        it("should redact dash-containing GitHub Server-to-Server Token (ghs_)", async () => {
          const testFile = path.join(tempDir, "test.txt");
          const ghToken = "ghs_abcd-efghij.klmn_opqr.stuvwxyz012345";
          expect(ghToken.slice(4)).toHaveLength(36);
          expect(ghToken).toContain("-");
          expect(ghToken).toContain(".");
          expect(ghToken).toContain("_");
          expect(ghToken).toMatch(/^ghs_[0-9A-Za-z._-]{36,}$/);
          fs.writeFileSync(testFile, `Dashed server token: ${ghToken}`);
          process.env.GH_AW_SECRET_NAMES = "";
          const modifiedScript = redactScript.replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir.replace(/\\/g, "\\\\")}", targetExtensions)`);
          await eval(`(async () => { ${modifiedScript}; await main(); })()`);
          const redacted = fs.readFileSync(testFile, "utf8");
          expect(redacted).toBe("Dashed server token: ***REDACTED***");
          expect(mockCore.info).not.toHaveBeenCalled();
        });

        it("should redact GitHub OAuth Access Token (gho_)", async () => {
          const testFile = path.join(tempDir, "test.txt");
          const ghToken = "gho_0123456789ABCDEFGHIJKLMNOPQRSTUVWxyz";
          fs.writeFileSync(testFile, `OAuth: ${ghToken}`);
          process.env.GH_AW_SECRET_NAMES = "";
          const modifiedScript = redactScript.replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir.replace(/\\/g, "\\\\")}", targetExtensions)`);
          await eval(`(async () => { ${modifiedScript}; await main(); })()`);
          const redacted = fs.readFileSync(testFile, "utf8");
          expect(redacted).toBe("OAuth: ***REDACTED***");
          expect(mockCore.info).not.toHaveBeenCalled();
        });

        it("should redact GitHub User Access Token (ghu_)", async () => {
          const testFile = path.join(tempDir, "test.txt");
          const ghToken = "ghu_0123456789ABCDEFGHIJKLMNOPQRSTUVWxyz";
          fs.writeFileSync(testFile, `User token: ${ghToken}`);
          process.env.GH_AW_SECRET_NAMES = "";
          const modifiedScript = redactScript.replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir.replace(/\\/g, "\\\\")}", targetExtensions)`);
          await eval(`(async () => { ${modifiedScript}; await main(); })()`);
          const redacted = fs.readFileSync(testFile, "utf8");
          expect(redacted).toBe("User token: ***REDACTED***");
          expect(mockCore.info).not.toHaveBeenCalled();
        });

        it("should redact GitHub Fine-grained PAT (github_pat_)", async () => {
          const testFile = path.join(tempDir, "test.txt");
          const ghToken = "github_pat_0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz_0123456789ABCDEFGHI";
          fs.writeFileSync(testFile, `Fine-grained PAT: ${ghToken}`);
          process.env.GH_AW_SECRET_NAMES = "";
          const modifiedScript = redactScript.replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir.replace(/\\/g, "\\\\")}", targetExtensions)`);
          await eval(`(async () => { ${modifiedScript}; await main(); })()`);
          const redacted = fs.readFileSync(testFile, "utf8");
          expect(redacted).toBe("Fine-grained PAT: ***REDACTED***");
          expect(mockCore.info).not.toHaveBeenCalled();
        });

        it("should redact GitHub Refresh Token (ghr_)", async () => {
          const testFile = path.join(tempDir, "test.txt");
          const ghToken = "ghr_0123456789ABCDEFGHIJKLMNOPQRSTUVWxyz";
          fs.writeFileSync(testFile, `Refresh: ${ghToken}`);
          process.env.GH_AW_SECRET_NAMES = "";
          const modifiedScript = redactScript.replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir.replace(/\\/g, "\\\\")}", targetExtensions)`);
          await eval(`(async () => { ${modifiedScript}; await main(); })()`);
          const redacted = fs.readFileSync(testFile, "utf8");
          expect(redacted).toBe("Refresh: ***REDACTED***");
          expect(mockCore.info).not.toHaveBeenCalled();
        });

        it("should redact multiple GitHub token types in same file", async () => {
          const testFile = path.join(tempDir, "test.txt");
          const ghp = "ghp_1234567890ABCDEFGHIJKLMNOPQRSTUVWxyz";
          const ghs = "ghs_0123456789ABCDEFGHIJKLMNOPQRSTUVWxyz";
          const gho = "gho_0123456789ABCDEFGHIJKLMNOPQRSTUVWxyz";
          fs.writeFileSync(testFile, `PAT: ${ghp}\nServer: ${ghs}\nOAuth: ${gho}`);
          process.env.GH_AW_SECRET_NAMES = "";
          const modifiedScript = redactScript.replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir.replace(/\\/g, "\\\\")}", targetExtensions)`);
          await eval(`(async () => { ${modifiedScript}; await main(); })()`);
          const redacted = fs.readFileSync(testFile, "utf8");
          expect(redacted).toBe("PAT: ***REDACTED***\nServer: ***REDACTED***\nOAuth: ***REDACTED***");
        });
      });

      describe("Azure tokens", () => {
        it("should redact Azure Storage Account Key in connection string context", async () => {
          const testFile = path.join(tempDir, "test.txt");
          // Azure Storage Account Keys are 64-byte (512-bit) values = 86 base64 chars + "==" padding
          const azureKey = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/ABCDEFGHIJKLMNOPQRSTUV==";
          fs.writeFileSync(testFile, `DefaultEndpointsProtocol=https;AccountName=myaccount;AccountKey=${azureKey};EndpointSuffix=core.windows.net`);
          process.env.GH_AW_SECRET_NAMES = "";
          const modifiedScript = redactScript.replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir.replace(/\\/g, "\\\\")}", targetExtensions)`);
          await eval(`(async () => { ${modifiedScript}; await main(); })()`);
          const redacted = fs.readFileSync(testFile, "utf8");
          expect(redacted).toBe("DefaultEndpointsProtocol=https;AccountName=myaccount;***REDACTED***;EndpointSuffix=core.windows.net");
          expect(mockCore.info).not.toHaveBeenCalled();
        });

        it("should not falsely redact plain base64 strings without AccountKey= context", async () => {
          const testFile = path.join(tempDir, "test.txt");
          // A different 86-char base64 string (not prefixed with AccountKey=) should NOT be redacted
          const plainBase64 = "zyxwvutsrqponmlkjihgfedcbaZYXWVUTSRQPONMLKJIHGFEDCBA9876543210/+zyxwvutsrqponmlkjiha==";
          const content = `some log output with base64: ${plainBase64} and more text`;
          fs.writeFileSync(testFile, content);
          process.env.GH_AW_SECRET_NAMES = "";
          const modifiedScript = redactScript.replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir.replace(/\\/g, "\\\\")}", targetExtensions)`);
          await eval(`(async () => { ${modifiedScript}; await main(); })()`);
          const result = fs.readFileSync(testFile, "utf8");
          expect(result).toBe(content);
        });

        it("should redact Azure SAS Token", async () => {
          const testFile = path.join(tempDir, "test.txt");
          const sasToken = "?sv=2021-06-08&ss=bfqt&srt=sco&sig=AbcXyz123456+/=";
          fs.writeFileSync(testFile, `SAS Token: ${sasToken}`);
          process.env.GH_AW_SECRET_NAMES = "";
          const modifiedScript = redactScript.replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir.replace(/\\/g, "\\\\")}", targetExtensions)`);
          await eval(`(async () => { ${modifiedScript}; await main(); })()`);
          const redacted = fs.readFileSync(testFile, "utf8");
          expect(redacted).toContain("?sv");
          // SAS tokens are complex and may not always be detected
        });
      });

      describe("Google/GCP tokens", () => {
        it("should redact Google API Key", async () => {
          const testFile = path.join(tempDir, "test.txt");
          const googleKey = "AIzaSy0123456789ABCDEFGHIJKLMNOPQRSTUVW";
          fs.writeFileSync(testFile, `Google API Key: ${googleKey}`);
          process.env.GH_AW_SECRET_NAMES = "";
          const modifiedScript = redactScript.replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir.replace(/\\/g, "\\\\")}", targetExtensions)`);
          await eval(`(async () => { ${modifiedScript}; await main(); })()`);
          const redacted = fs.readFileSync(testFile, "utf8");
          expect(redacted).toBe("Google API Key: ***REDACTED***");
          expect(mockCore.info).not.toHaveBeenCalled();
        });

        it("should redact Google OAuth Access Token", async () => {
          const testFile = path.join(tempDir, "test.txt");
          const googleToken = "ya29.a0AfH6SMBxXxXxXxXxXxXxXxXxXxXxXxXxXxXxXxXx";
          fs.writeFileSync(testFile, `OAuth Token: ${googleToken}`);
          process.env.GH_AW_SECRET_NAMES = "";
          const modifiedScript = redactScript.replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir.replace(/\\/g, "\\\\")}", targetExtensions)`);
          await eval(`(async () => { ${modifiedScript}; await main(); })()`);
          const redacted = fs.readFileSync(testFile, "utf8");
          expect(redacted).toBe("OAuth Token: ***REDACTED***");
          expect(mockCore.info).not.toHaveBeenCalled();
        });
      });

      describe("AWS tokens", () => {
        it("should redact AWS Access Key ID", async () => {
          const testFile = path.join(tempDir, "test.txt");
          const awsKey = "AKIAIOSFODNN7EXAMPLE";
          fs.writeFileSync(testFile, `AWS Key: ${awsKey}`);
          process.env.GH_AW_SECRET_NAMES = "";
          const modifiedScript = redactScript.replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir.replace(/\\/g, "\\\\")}", targetExtensions)`);
          await eval(`(async () => { ${modifiedScript}; await main(); })()`);
          const redacted = fs.readFileSync(testFile, "utf8");
          expect(redacted).toBe("AWS Key: ***REDACTED***");
          expect(mockCore.info).not.toHaveBeenCalled();
        });
      });

      describe("OpenAI tokens", () => {
        it("should redact OpenAI API Key", async () => {
          const testFile = path.join(tempDir, "test.txt");
          const openaiKey = "sk-" + "0".repeat(48);
          fs.writeFileSync(testFile, `OpenAI Key: ${openaiKey}`);
          process.env.GH_AW_SECRET_NAMES = "";
          const modifiedScript = redactScript.replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir.replace(/\\/g, "\\\\")}", targetExtensions)`);
          await eval(`(async () => { ${modifiedScript}; await main(); })()`);
          const redacted = fs.readFileSync(testFile, "utf8");
          expect(redacted).toBe("OpenAI Key: ***REDACTED***");
          expect(mockCore.info).not.toHaveBeenCalled();
        });

        it("should redact OpenAI Project API Key", async () => {
          const testFile = path.join(tempDir, "test.txt");
          const openaiProjectKey = "sk-proj-" + "A".repeat(55);
          fs.writeFileSync(testFile, `OpenAI Project Key: ${openaiProjectKey}`);
          process.env.GH_AW_SECRET_NAMES = "";
          const modifiedScript = redactScript.replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir.replace(/\\/g, "\\\\")}", targetExtensions)`);
          await eval(`(async () => { ${modifiedScript}; await main(); })()`);
          const redacted = fs.readFileSync(testFile, "utf8");
          expect(redacted).toBe("OpenAI Project Key: ***REDACTED***");
          expect(mockCore.info).not.toHaveBeenCalled();
        });
      });

      describe("Anthropic tokens", () => {
        it("should redact Anthropic API Key", async () => {
          const testFile = path.join(tempDir, "test.txt");
          const anthropicKey = "sk-ant-api03-" + "B".repeat(95);
          fs.writeFileSync(testFile, `Anthropic Key: ${anthropicKey}`);
          process.env.GH_AW_SECRET_NAMES = "";
          const modifiedScript = redactScript.replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir.replace(/\\/g, "\\\\")}", targetExtensions)`);
          await eval(`(async () => { ${modifiedScript}; await main(); })()`);
          const redacted = fs.readFileSync(testFile, "utf8");
          expect(redacted).toBe("Anthropic Key: ***REDACTED***");
          expect(mockCore.info).not.toHaveBeenCalled();
        });
      });

      describe("Linear tokens", () => {
        it("should redact Linear API keys", async () => {
          const testFile = path.join(tempDir, "test.txt");
          const linearKey = "lin_api_" + "C".repeat(40);
          fs.writeFileSync(testFile, `Linear Key: ${linearKey}`);
          process.env.GH_AW_SECRET_NAMES = "";
          const modifiedScript = redactScript.replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir.replace(/\\/g, "\\\\")}", targetExtensions)`);
          await eval(`(async () => { ${modifiedScript}; await main(); })()`);
          const redacted = fs.readFileSync(testFile, "utf8");
          expect(redacted).toBe("Linear Key: ***REDACTED***");
          expect(mockCore.info).not.toHaveBeenCalled();
        });
      });

      describe("combined built-in and custom secrets", () => {
        it("should redact both built-in patterns and custom secrets", async () => {
          const testFile = path.join(tempDir, "test.txt");
          const ghToken = "ghp_1234567890ABCDEFGHIJKLMNOPQRSTUVWxyz";
          const customSecret = "my-custom-secret-key-12345678";
          fs.writeFileSync(testFile, `GitHub: ${ghToken}\nCustom: ${customSecret}`);
          process.env.GH_AW_SECRET_NAMES = "CUSTOM_KEY";
          process.env.SECRET_CUSTOM_KEY = customSecret;
          const modifiedScript = redactScript.replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir.replace(/\\/g, "\\\\")}", targetExtensions)`);
          await eval(`(async () => { ${modifiedScript}; await main(); })()`);
          const redacted = fs.readFileSync(testFile, "utf8");
          expect(redacted).toBe("GitHub: ***REDACTED***\nCustom: ***REDACTED***");
          expect(mockCore.info).not.toHaveBeenCalled();
        });

        it("should handle overlapping matches between built-in and custom secrets", async () => {
          const testFile = path.join(tempDir, "test.txt");
          const ghToken = "ghp_0123456789ABCDEFGHIJKLMNOPQRSTUVWxyz";
          fs.writeFileSync(testFile, `Token: ${ghToken} repeated: ${ghToken}`);
          process.env.GH_AW_SECRET_NAMES = "GH_TOKEN";
          process.env.SECRET_GH_TOKEN = ghToken;
          const modifiedScript = redactScript.replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir.replace(/\\/g, "\\\\")}", targetExtensions)`);
          await eval(`(async () => { ${modifiedScript}; await main(); })()`);
          const redacted = fs.readFileSync(testFile, "utf8");
          // Built-in pattern should redact it first
          expect(redacted).toBe("Token: ***REDACTED*** repeated: ***REDACTED***");
        });
      });

      describe("edge cases", () => {
        it("should handle files with no secrets", async () => {
          const testFile = path.join(tempDir, "test.txt");
          fs.writeFileSync(testFile, "This file has no secrets at all");
          process.env.GH_AW_SECRET_NAMES = "";
          const modifiedScript = redactScript.replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir.replace(/\\/g, "\\\\")}", targetExtensions)`);
          await eval(`(async () => { ${modifiedScript}; await main(); })()`);
          const content = fs.readFileSync(testFile, "utf8");
          expect(content).toBe("This file has no secrets at all");
        });

        it("should handle multiple occurrences of same built-in pattern", async () => {
          const testFile = path.join(tempDir, "test.txt");
          const ghToken = "ghp_1234567890ABCDEFGHIJKLMNOPQRSTUVWxyz";
          fs.writeFileSync(testFile, `First: ${ghToken}\nSecond: ${ghToken}\nThird: ${ghToken}`);
          process.env.GH_AW_SECRET_NAMES = "";
          const modifiedScript = redactScript.replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir.replace(/\\/g, "\\\\")}", targetExtensions)`);
          await eval(`(async () => { ${modifiedScript}; await main(); })()`);
          const redacted = fs.readFileSync(testFile, "utf8");
          expect(redacted).toBe("First: ***REDACTED***\nSecond: ***REDACTED***\nThird: ***REDACTED***");
        });

        it("should handle secrets in JSON content", async () => {
          const testFile = path.join(tempDir, "test.json");
          const ghToken = "ghp_0123456789ABCDEFGHIJKLMNOPQRSTUVWxyz";
          const googleKey = "AIzaSy0123456789ABCDEFGHIJKLMNOPQRSTUVW";
          fs.writeFileSync(testFile, JSON.stringify({ github_token: ghToken, google_api_key: googleKey }));
          process.env.GH_AW_SECRET_NAMES = "";
          const modifiedScript = redactScript.replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir.replace(/\\/g, "\\\\")}", targetExtensions)`);
          await eval(`(async () => { ${modifiedScript}; await main(); })()`);
          const redacted = fs.readFileSync(testFile, "utf8");
          expect(redacted).toContain("***REDACTED***");
          expect(redacted).toContain("***REDACTED***");
        });

        it("should handle secrets in log files with timestamps", async () => {
          const testFile = path.join(tempDir, "test.log");
          const ghToken = "ghp_0123456789ABCDEFGHIJKLMNOPQRSTUVWxyz";
          fs.writeFileSync(testFile, `[2024-01-01 12:00:00] INFO: Using token ${ghToken} for authentication`);
          process.env.GH_AW_SECRET_NAMES = "";
          const modifiedScript = redactScript.replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir.replace(/\\/g, "\\\\")}", targetExtensions)`);
          await eval(`(async () => { ${modifiedScript}; await main(); })()`);
          const redacted = fs.readFileSync(testFile, "utf8");
          expect(redacted).toBe("[2024-01-01 12:00:00] INFO: Using token ***REDACTED*** for authentication");
        });

        it("should not redact partial matches", async () => {
          const testFile = path.join(tempDir, "test.txt");
          // These should NOT be redacted (not valid token formats)
          fs.writeFileSync(testFile, "ghp_short ghs_invalid*token_because_it_has_disallowed_characters_and_is_long_enough ghs_short.segment.other ghs_12345678901234567890123456789012345 ghs_abcdefghij.klmnopqrst");
          process.env.GH_AW_SECRET_NAMES = "";
          const modifiedScript = redactScript.replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir.replace(/\\/g, "\\\\")}", targetExtensions)`);
          await eval(`(async () => { ${modifiedScript}; await main(); })()`);
          const content = fs.readFileSync(testFile, "utf8");
          // Includes a 35-char ghs_ token (below the 36-char minimum) and a short dot-separated variant.
          expect(content).toBe("ghp_short ghs_invalid*token_because_it_has_disallowed_characters_and_is_long_enough ghs_short.segment.other ghs_12345678901234567890123456789012345 ghs_abcdefghij.klmnopqrst");
        });

        it("should handle URLs with secrets", async () => {
          const testFile = path.join(tempDir, "test.txt");
          const ghToken = "ghp_0123456789ABCDEFGHIJKLMNOPQRSTUVWxyz";
          fs.writeFileSync(testFile, `https://api.github.com?token=${ghToken}`);
          process.env.GH_AW_SECRET_NAMES = "";
          const modifiedScript = redactScript.replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir.replace(/\\/g, "\\\\")}", targetExtensions)`);
          await eval(`(async () => { ${modifiedScript}; await main(); })()`);
          const redacted = fs.readFileSync(testFile, "utf8");
          expect(redacted).toBe("https://api.github.com?token=***REDACTED***");
        });

        it("should handle multiline content with various token types", async () => {
          const testFile = path.join(tempDir, "test.md");
          const content = `# Configuration
          
GitHub Token: ghp_0123456789ABCDEFGHIJKLMNOPQRSTUVWxyz
Azure Key: ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/ABCDEFGHIJKLMNOPQRSTUVWX==
Google API Key: AIzaSy0123456789ABCDEFGHIJKLMNOPQRSTUVW
AWS Key: AKIA0123456789ABCDEF

Custom secret: my-secret-123456789012`;
          fs.writeFileSync(testFile, content);
          process.env.GH_AW_SECRET_NAMES = "MY_SECRET";
          process.env.SECRET_MY_SECRET = "my-secret-123456789012";
          const modifiedScript = redactScript.replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir.replace(/\\/g, "\\\\")}", targetExtensions)`);
          await eval(`(async () => { ${modifiedScript}; await main(); })()`);
          const redacted = fs.readFileSync(testFile, "utf8");
          expect(redacted).toContain("***REDACTED***");
          expect(redacted).toContain("***REDACTED***");
          expect(redacted).toContain("***REDACTED***");
          expect(redacted).toContain("***REDACTED***");
          expect(redacted).toContain("***REDACTED***");
        });

        describe("ReDoS protection", () => {
          it("should handle pathological Azure SAS Token input without timing out", async () => {
            const testFile = path.join(tempDir, "test.txt");
            // Create pathological input that would cause ReDoS with unbounded quantifiers
            const pathological = `?sv=${"9".repeat(1000)}&srt=${"w".repeat(1000)}&sig=${"A".repeat(1000)}`;
            fs.writeFileSync(testFile, `Pathological: ${pathological}`);
            process.env.GH_AW_SECRET_NAMES = "";
            const modifiedScript = redactScript.replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir.replace(/\\/g, "\\\\")}", targetExtensions)`);

            // This should complete quickly (< 1 second) without hanging
            const startTime = Date.now();
            await eval(`(async () => { ${modifiedScript}; await main(); })()`);
            const duration = Date.now() - startTime;

            // Verify it completed quickly (should be < 1000ms, but allow 5000ms for slower CI)
            expect(duration).toBeLessThan(5000);

            // The pattern shouldn't match due to length bounds
            const content = fs.readFileSync(testFile, "utf8");
            expect(content).toBe(`Pathological: ${pathological}`);
          });

          it("should handle pathological Google OAuth token input without timing out", async () => {
            const testFile = path.join(tempDir, "test.txt");
            // Create pathological input that would cause ReDoS with unbounded quantifiers
            const pathological = `ya29.${"A".repeat(5000)}`;
            fs.writeFileSync(testFile, `Token: ${pathological}`);
            process.env.GH_AW_SECRET_NAMES = "";
            const modifiedScript = redactScript.replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir.replace(/\\/g, "\\\\")}", targetExtensions)`);

            // This should complete quickly (< 1 second) without hanging
            const startTime = Date.now();
            await eval(`(async () => { ${modifiedScript}; await main(); })()`);
            const duration = Date.now() - startTime;

            // Verify it completed quickly (should be < 1000ms, but allow 5000ms for slower CI)
            expect(duration).toBeLessThan(5000);

            // The pattern should match up to 800 chars and redact it
            const content = fs.readFileSync(testFile, "utf8");
            expect(content).toContain("***REDACTED***");
            expect(content).not.toBe(`Token: ${pathological}`);
            // Should still have unredacted 'A' chars at the end beyond 800 char limit
            expect(content).toMatch(/\*\*\*REDACTED\*\*\*A+$/);
          });

          it("should still match valid Azure SAS tokens within bounds", async () => {
            const testFile = path.join(tempDir, "test.txt");
            // Valid Azure SAS token within bounds
            const validSAS = "?sv=2021-06-08&sr=b&sig=AbCdEf0123456789+/=";
            fs.writeFileSync(testFile, `SAS: ${validSAS}`);
            process.env.GH_AW_SECRET_NAMES = "";
            const modifiedScript = redactScript.replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir.replace(/\\/g, "\\\\")}", targetExtensions)`);
            await eval(`(async () => { ${modifiedScript}; await main(); })()`);
            const redacted = fs.readFileSync(testFile, "utf8");
            // Should be redacted since it's a valid pattern within bounds
            expect(redacted).toBe("SAS: ***REDACTED***");
          });

          it("should still match valid Google OAuth tokens within bounds", async () => {
            const testFile = path.join(tempDir, "test.txt");
            // Valid Google OAuth token within bounds (typical length ~100-200 chars)
            const validToken = "ya29." + "a".repeat(150);
            fs.writeFileSync(testFile, `Token: ${validToken}`);
            process.env.GH_AW_SECRET_NAMES = "";
            const modifiedScript = redactScript.replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir.replace(/\\/g, "\\\\")}", targetExtensions)`);
            await eval(`(async () => { ${modifiedScript}; await main(); })()`);
            const redacted = fs.readFileSync(testFile, "utf8");
            // Should be redacted since it's a valid pattern within bounds
            expect(redacted).toBe("Token: ***REDACTED***");
            expect(mockCore.info).not.toHaveBeenCalled();
          });
        });
        describe("AWF_RESOLVED_CONFIG", () => {
          it.each(["", "/tmp/secret.json", "/run/awf-config-audit/run-abcdef/other.json", "/run/awf-config-audit/run-abcdef/awf-resolved-config.json\n/tmp/secret.json"])("rejects unsafe publication path %j without reading it", source => {
            const { readAWFResolvedConfig } = require("./redact_secrets.cjs");
            const read = vi.spyOn(fs, "lstatSync");
            expect(() => readAWFResolvedConfig(source)).toThrow();
            expect(read).not.toHaveBeenCalled();
            read.mockRestore();
          });

          it("preserves compatibility when older AWF does not publish the artifact", () => {
            const { stageAWFResolvedConfig, AWF_RESOLVED_CONFIG_STAGING_PATH } = require("./redact_secrets.cjs");
            const remove = vi.spyOn(fs, "rmSync").mockImplementation(() => {});
            try {
              expect(stageAWFResolvedConfig(undefined, [], [])).toBe(false);
              expect(fs.existsSync(AWF_RESOLVED_CONFIG_STAGING_PATH)).toBe(false);
              expect(remove).toHaveBeenCalledWith(AWF_RESOLVED_CONFIG_STAGING_PATH, { recursive: true, force: true });
            } finally {
              remove.mockRestore();
            }
          });

          it("stages only sanitized configuration after validating the protected source", () => {
            const { stageAWFResolvedConfig, AWF_RESOLVED_CONFIG_STAGING_PATH } = require("./redact_secrets.cjs");
            const sourcePath = "/run/awf-config-audit/run-abcdef/awf-resolved-config.json";
            const sourceStat = { isFile: () => true, isDirectory: () => false, uid: process.getuid(), nlink: 1, mode: 0o100600, ino: 42, dev: 7 };
            const directoryStat = { isFile: () => false, isDirectory: () => true, uid: process.getuid(), mode: 0o40700 };
            const rootStat = { isFile: () => false, isDirectory: () => true, uid: 0, mode: 0o40711 };
            const lstat = vi.spyOn(fs, "lstatSync").mockImplementation(file => {
              if (file === AWF_RESOLVED_CONFIG_STAGING_PATH) return undefined;
              if (file === "/run/awf-config-audit") return rootStat;
              if (file === path.dirname(sourcePath)) return directoryStat;
              if (file === sourcePath) return sourceStat;
              throw new Error(`unexpected path: ${file}`);
            });
            const open = vi.spyOn(fs, "openSync").mockReturnValue(9);
            const fstat = vi.spyOn(fs, "fstatSync").mockReturnValue(sourceStat);
            const read = vi.spyOn(fs, "readFileSync").mockReturnValue(
              JSON.stringify({
                version: "v0.28.59",
                token: "******",
                nested: { password: "fixture-password", endpoint: "https://example.test" },
              })
            );
            const close = vi.spyOn(fs, "closeSync").mockImplementation(() => {});
            const mkdtemp = vi.spyOn(fs, "mkdtempSync").mockReturnValue("/tmp/gh-aw/.awf-resolved-config-test");
            let stagedContent;
            const write = vi.spyOn(fs, "writeFileSync").mockImplementation((file, content) => {
              stagedContent = content;
            });
            const rename = vi.spyOn(fs, "renameSync").mockImplementation(() => {});
            const remove = vi.spyOn(fs, "rmSync").mockImplementation(() => {});
            try {
              expect(stageAWFResolvedConfig(sourcePath, [], [])).toBe(true);
              expect(stagedContent).toContain('"version": "v0.28.59"');
              expect(stagedContent).not.toContain("******");
              expect(stagedContent).not.toContain("fixture-password");
              expect(stagedContent).toContain("***REDACTED***");
              expect(rename).toHaveBeenCalledWith("/tmp/gh-aw/.awf-resolved-config-test/awf-resolved-config.json", AWF_RESOLVED_CONFIG_STAGING_PATH);
            } finally {
              for (const spy of [lstat, open, fstat, read, close, mkdtemp, write, rename, remove]) spy.mockRestore();
            }
          });

          it.each([
            ["wrong root ownership", root => root.uid++],
            ["unsafe run-directory permissions", (_root, directory) => (directory.mode = 0o40777)],
            ["wrong source ownership", (_root, _directory, source) => source.uid++],
            ["unsafe source permissions", (_root, _directory, source) => (source.mode = 0o100644)],
            ["symbolic link", (_root, _directory, source) => (source.isFile = () => false)],
            ["extra hard link", (_root, _directory, source) => (source.nlink = 2)],
            ["changed inode", (_root, _directory, _source, opened) => opened.ino++],
            ["changed device", (_root, _directory, _source, opened) => opened.dev++],
          ])("rejects AWF source with %s and leaves no staged output", (_description, mutate) => {
            const { stageAWFResolvedConfig, AWF_RESOLVED_CONFIG_STAGING_PATH } = require("./redact_secrets.cjs");
            const sourcePath = "/run/awf-config-audit/run-abcdef/awf-resolved-config.json";
            const source = { isFile: () => true, isDirectory: () => false, uid: process.getuid(), nlink: 1, mode: 0o100600, ino: 42, dev: 7 };
            const opened = { ...source };
            const root = { isDirectory: () => true, uid: 0, mode: 0o40711 };
            const directory = { isDirectory: () => true, uid: process.getuid(), mode: 0o40700 };
            mutate(root, directory, source, opened);
            const lstat = vi.spyOn(fs, "lstatSync").mockImplementation(file => {
              if (file === "/run/awf-config-audit") return root;
              if (file === path.dirname(sourcePath)) return directory;
              if (file === sourcePath) return source;
              throw new Error(`unexpected path: ${file}`);
            });
            const open = vi.spyOn(fs, "openSync").mockReturnValue(9);
            const fstat = vi.spyOn(fs, "fstatSync").mockReturnValue(opened);
            const read = vi.spyOn(fs, "readFileSync").mockReturnValue('{"command":"safe"}');
            const remove = vi.spyOn(fs, "rmSync").mockImplementation(() => {});
            const rename = vi.spyOn(fs, "renameSync").mockImplementation(() => {});
            const unlink = vi.spyOn(fs, "unlinkSync").mockImplementation(() => {});
            try {
              expect(() => stageAWFResolvedConfig(sourcePath, [], [])).toThrow();
              expect(read).not.toHaveBeenCalled();
              expect(rename).not.toHaveBeenCalled();
              expect(unlink).not.toHaveBeenCalled();
              expect(remove).toHaveBeenCalledWith(AWF_RESOLVED_CONFIG_STAGING_PATH, { recursive: true, force: true });
            } finally {
              for (const spy of [lstat, open, fstat, read, remove, rename, unlink]) spy.mockRestore();
            }
          });

          it("rejects malformed AWF JSON without staging output or modifying the source", () => {
            const { stageAWFResolvedConfig, AWF_RESOLVED_CONFIG_STAGING_PATH } = require("./redact_secrets.cjs");
            const sourcePath = "/run/awf-config-audit/run-abcdef/awf-resolved-config.json";
            const source = { isFile: () => true, isDirectory: () => false, uid: process.getuid(), nlink: 1, mode: 0o100600, ino: 42, dev: 7 };
            const root = { isDirectory: () => true, uid: 0, mode: 0o40711 };
            const directory = { isDirectory: () => true, uid: process.getuid(), mode: 0o40700 };
            const lstat = vi.spyOn(fs, "lstatSync").mockImplementation(file => {
              if (file === "/run/awf-config-audit") return root;
              if (file === path.dirname(sourcePath)) return directory;
              if (file === sourcePath) return source;
              throw new Error(`unexpected path: ${file}`);
            });
            const open = vi.spyOn(fs, "openSync").mockReturnValue(9);
            const fstat = vi.spyOn(fs, "fstatSync").mockReturnValue(source);
            const read = vi.spyOn(fs, "readFileSync").mockReturnValue("{malformed");
            const close = vi.spyOn(fs, "closeSync").mockImplementation(() => {});
            const remove = vi.spyOn(fs, "rmSync").mockImplementation(() => {});
            const mkdtemp = vi.spyOn(fs, "mkdtempSync");
            const rename = vi.spyOn(fs, "renameSync").mockImplementation(() => {});
            const unlink = vi.spyOn(fs, "unlinkSync").mockImplementation(() => {});
            try {
              expect(() => stageAWFResolvedConfig(sourcePath, [], [])).toThrow(/not valid JSON/);
              expect(read).toHaveBeenCalledWith(9, "utf8");
              expect(close).toHaveBeenCalledWith(9);
              expect(mkdtemp).not.toHaveBeenCalled();
              expect(rename).not.toHaveBeenCalled();
              expect(unlink).not.toHaveBeenCalled();
              expect(remove).toHaveBeenCalledWith(AWF_RESOLVED_CONFIG_STAGING_PATH, { recursive: true, force: true });
            } finally {
              for (const spy of [lstat, open, fstat, read, close, remove, mkdtemp, rename, unlink]) spy.mockRestore();
            }
          });

          it("redacts escaped known secrets from serialized configuration values", () => {
            const { stageAWFResolvedConfig } = require("./redact_secrets.cjs");
            const sourcePath = "/run/awf-config-audit/run-abcdef/awf-resolved-config.json";
            const source = { isFile: () => true, isDirectory: () => false, uid: process.getuid(), nlink: 1, mode: 0o100600, ino: 42, dev: 7 };
            const root = { isDirectory: () => true, uid: 0, mode: 0o40711 };
            const directory = { isDirectory: () => true, uid: process.getuid(), mode: 0o40700 };
            const secret = 'quoted"slash\\line\nnext';
            const lstat = vi.spyOn(fs, "lstatSync").mockImplementation(file => {
              if (file === "/run/awf-config-audit") return root;
              if (file === path.dirname(sourcePath)) return directory;
              if (file === sourcePath) return source;
              throw new Error(`unexpected path: ${file}`);
            });
            const open = vi.spyOn(fs, "openSync").mockReturnValue(9);
            const fstat = vi.spyOn(fs, "fstatSync").mockReturnValue(source);
            const read = vi.spyOn(fs, "readFileSync").mockReturnValue(JSON.stringify({ command: secret }));
            const close = vi.spyOn(fs, "closeSync").mockImplementation(() => {});
            const mkdtemp = vi.spyOn(fs, "mkdtempSync").mockReturnValue("/tmp/gh-aw/.awf-resolved-config-test");
            let stagedContent;
            const write = vi.spyOn(fs, "writeFileSync").mockImplementation((_file, content) => {
              stagedContent = content;
            });
            const rename = vi.spyOn(fs, "renameSync").mockImplementation(() => {});
            const remove = vi.spyOn(fs, "rmSync").mockImplementation(() => {});
            try {
              expect(stageAWFResolvedConfig(sourcePath, [secret], [])).toBe(true);
              expect(JSON.parse(stagedContent).command).not.toContain(secret);
              expect(JSON.parse(stagedContent).command).toBe("***");
            } finally {
              for (const spy of [lstat, open, fstat, read, close, mkdtemp, write, rename, remove]) spy.mockRestore();
            }
          });

          it.each(["invalid publication", "staging cleanup failure"])("continues redacting other artifacts after %s", async failure => {
            const { main } = require("./redact_secrets.cjs");
            const dir = path.join(process.env.RUNNER_TEMP, "gh-aw");
            fs.mkdirSync(dir, { recursive: true });
            const artifact = path.join(dir, "other.log");
            const secret = "artifact-secret-value";
            fs.writeFileSync(artifact, `value=${secret}`);
            vi.stubEnv("GH_AW_SECRET_NAMES", "TEST");
            vi.stubEnv("SECRET_TEST", secret);
            if (failure === "invalid publication") vi.stubEnv("AWF_RESOLVED_CONFIG", "/tmp/untrusted.json");
            const originalRemove = fs.rmSync;
            const remove = vi.spyOn(fs, "rmSync").mockImplementation((file, options) => {
              if (failure === "staging cleanup failure" && String(file) === "/tmp/gh-aw/awf-resolved-config.json") {
                throw Object.assign(new Error("cleanup denied"), { code: "EACCES" });
              }
              if (String(file) !== "/tmp/gh-aw/awf-resolved-config.json") {
                return originalRemove(file, options);
              }
            });
            try {
              await main();
              expect(fs.readFileSync(artifact, "utf8")).not.toContain(secret);
              expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("AWF resolved configuration was not collected safely"));
              expect(mockCore.info).not.toHaveBeenCalledWith("AWF did not publish an early resolved configuration artifact");
              if (failure === "staging cleanup failure") expect(remove).toHaveBeenCalledTimes(2);
            } finally {
              remove.mockRestore();
            }
          });
        });
      });
    }));

  describe("extractMCPGatewayTokens", () => {
    it("should extract Authorization tokens from gateway-output.json", () => {
      const configDir = path.join(tempDir, "mcp-config");
      fs.mkdirSync(configDir, { recursive: true });
      const gatewayOutput = path.join(configDir, "gateway-output.json");
      fs.writeFileSync(
        gatewayOutput,
        JSON.stringify({
          mcpServers: {
            github: {
              type: "http",
              url: "http://host.docker.internal:8080/mcp/github",
              headers: { Authorization: "my-gateway-token-abc123" },
            },
          },
        })
      );

      const script = redactScript
        .replace('path.join(process.env.RUNNER_TEMP || "/tmp", "gh-aw/mcp-config/gateway-output.json")', `"${gatewayOutput.replace(/\\/g, "\\\\")}"`)
        .replace('path.join(process.env.RUNNER_TEMP || "/tmp", "gh-aw/mcp-config/mcp-servers.json")', `"${path.join(configDir, "mcp-servers.json").replace(/\\/g, "\\\\")}"`);
      let tokens;
      eval(`(function() { ${script}; tokens = extractMCPGatewayTokens(MCP_GATEWAY_CONFIG_PATHS); })()`);
      expect(tokens).toContain("my-gateway-token-abc123");
    });

    it("should extract Authorization tokens from mcp-servers.json", () => {
      const configDir = path.join(tempDir, "mcp-config");
      fs.mkdirSync(configDir, { recursive: true });
      const mcpServers = path.join(configDir, "mcp-servers.json");
      fs.writeFileSync(
        mcpServers,
        JSON.stringify({
          mcpServers: {
            safeoutputs: {
              type: "http",
              url: "http://host.docker.internal:8080/mcp/safeoutputs",
              headers: { Authorization: "safe-output-token-xyz789" },
            },
          },
        })
      );

      const script = redactScript
        .replace('path.join(process.env.RUNNER_TEMP || "/tmp", "gh-aw/mcp-config/gateway-output.json")', `"${path.join(configDir, "gateway-output.json").replace(/\\/g, "\\\\")}"`)
        .replace('path.join(process.env.RUNNER_TEMP || "/tmp", "gh-aw/mcp-config/mcp-servers.json")', `"${mcpServers.replace(/\\/g, "\\\\")}"`);
      let tokens;
      eval(`(function() { ${script}; tokens = extractMCPGatewayTokens(MCP_GATEWAY_CONFIG_PATHS); })()`);
      expect(tokens).toContain("safe-output-token-xyz789");
    });

    it("should extract Authorization tokens from Codex TOML configuration", () => {
      const configPath = path.join(tempDir, "config.toml");
      const token = "codex-gateway-token-xyz789";
      const authHeader = ["Bearer", token].join(" ");
      fs.writeFileSync(configPath, `[mcp_servers.github]\nurl = "http://127.0.0.1:8080/github"\nhttp_headers = { Authorization = "${authHeader}" }\n`);

      const { extractMCPGatewayTokens } = require("./redact_secrets.cjs");
      const tokens = extractMCPGatewayTokens([configPath]);
      expect(tokens).toContain(authHeader);
      expect(tokens).toContain(token);
    });

    it("should extract lowercase authorization keys from Codex TOML configuration", () => {
      const configPath = path.join(tempDir, "config-lowercase.toml");
      const token = "codex-lowercase-token-abc123";
      const authHeader = ["Bearer", token].join(" ");
      fs.writeFileSync(configPath, `[mcp_servers.github]\nhttp_headers = { authorization = "${authHeader}" }\n`);

      const { extractMCPGatewayTokens } = require("./redact_secrets.cjs");
      const tokens = extractMCPGatewayTokens([configPath]);
      expect(tokens).toContain(authHeader);
      expect(tokens).toContain(token);
    });

    it("should ignore short Authorization values even when bearer-prefixed", () => {
      const configPath = path.join(tempDir, "config-short.toml");
      const authHeader = ["Bearer", "abc"].join(" ");
      fs.writeFileSync(configPath, `[mcp_servers.github]\nhttp_headers = { Authorization = "${authHeader}" }\n`);

      const { extractMCPGatewayTokens } = require("./redact_secrets.cjs");
      expect(extractMCPGatewayTokens([configPath])).toEqual([]);
    });

    it("should extract both the full Bearer header value and the bare token", () => {
      const configDir = path.join(tempDir, "mcp-config");
      fs.mkdirSync(configDir, { recursive: true });
      const gatewayOutput = path.join(configDir, "gateway-output.json");
      fs.writeFileSync(
        gatewayOutput,
        JSON.stringify({
          mcpServers: {
            github: {
              type: "http",
              url: "http://host.docker.internal:8080/mcp/github",
              headers: { Authorization: "Bearer tok-abc123def456" },
            },
          },
        })
      );

      const script = redactScript
        .replace('path.join(process.env.RUNNER_TEMP || "/tmp", "gh-aw/mcp-config/gateway-output.json")', `"${gatewayOutput.replace(/\\/g, "\\\\")}"`)
        .replace('path.join(process.env.RUNNER_TEMP || "/tmp", "gh-aw/mcp-config/mcp-servers.json")', `"${path.join(configDir, "mcp-servers.json").replace(/\\/g, "\\\\")}"`);
      let tokens;
      eval(`(function() { ${script}; tokens = extractMCPGatewayTokens(MCP_GATEWAY_CONFIG_PATHS); })()`);
      expect(tokens).toContain("Bearer tok-abc123def456");
      expect(tokens).toContain("tok-abc123def456");
    });

    it("should deduplicate tokens shared across multiple servers", () => {
      const configDir = path.join(tempDir, "mcp-config");
      fs.mkdirSync(configDir, { recursive: true });
      const gatewayOutput = path.join(configDir, "gateway-output.json");
      const sharedToken = "shared-token-same-for-all";
      fs.writeFileSync(
        gatewayOutput,
        JSON.stringify({
          mcpServers: {
            github: { type: "http", url: "http://host.docker.internal:8080/mcp/github", headers: { Authorization: sharedToken } },
            safeoutputs: { type: "http", url: "http://host.docker.internal:8080/mcp/safeoutputs", headers: { Authorization: sharedToken } },
          },
        })
      );

      const script = redactScript
        .replace('path.join(process.env.RUNNER_TEMP || "/tmp", "gh-aw/mcp-config/gateway-output.json")', `"${gatewayOutput.replace(/\\/g, "\\\\")}"`)
        .replace('path.join(process.env.RUNNER_TEMP || "/tmp", "gh-aw/mcp-config/mcp-servers.json")', `"${path.join(configDir, "mcp-servers.json").replace(/\\/g, "\\\\")}"`);
      let tokens;
      eval(`(function() { ${script}; tokens = extractMCPGatewayTokens(MCP_GATEWAY_CONFIG_PATHS); })()`);
      expect(tokens.filter(t => t === sharedToken)).toHaveLength(1);
    });

    it("should return empty array when config files do not exist", () => {
      const nonExistent = path.join(tempDir, "nonexistent.json");
      const script = redactScript
        .replace('path.join(process.env.RUNNER_TEMP || "/tmp", "gh-aw/mcp-config/gateway-output.json")', `"${nonExistent.replace(/\\/g, "\\\\")}"`)
        .replace('path.join(process.env.RUNNER_TEMP || "/tmp", "gh-aw/mcp-config/mcp-servers.json")', `"${nonExistent.replace(/\\/g, "\\\\")}"`);
      let tokens;
      eval(`(function() { ${script}; tokens = extractMCPGatewayTokens(MCP_GATEWAY_CONFIG_PATHS); })()`);
      expect(tokens).toEqual([]);
    });

    it("should silently ignore malformed JSON config files", () => {
      const configDir = path.join(tempDir, "mcp-config");
      fs.mkdirSync(configDir, { recursive: true });
      const gatewayOutput = path.join(configDir, "gateway-output.json");
      fs.writeFileSync(gatewayOutput, "not valid json {{{");

      const script = redactScript
        .replace('path.join(process.env.RUNNER_TEMP || "/tmp", "gh-aw/mcp-config/gateway-output.json")', `"${gatewayOutput.replace(/\\/g, "\\\\")}"`)
        .replace('path.join(process.env.RUNNER_TEMP || "/tmp", "gh-aw/mcp-config/mcp-servers.json")', `"${path.join(configDir, "mcp-servers.json").replace(/\\/g, "\\\\")}"`);
      let tokens;
      expect(() => {
        eval(`(function() { ${script}; tokens = extractMCPGatewayTokens(MCP_GATEWAY_CONFIG_PATHS); })()`);
      }).not.toThrow();
      expect(tokens).toEqual([]);
    });

    it("should redact MCP gateway token from diagnostic logs in main()", async () => {
      const configDir = path.join(tempDir, "mcp-config");
      fs.mkdirSync(configDir, { recursive: true });
      const gatewayOutput = path.join(configDir, "gateway-output.json");
      const gatewayToken = "super-secret-gateway-token-98765";
      fs.writeFileSync(
        gatewayOutput,
        JSON.stringify({
          mcpServers: {
            github: { type: "http", url: "http://host.docker.internal:8080/mcp/github", headers: { Authorization: gatewayToken } },
          },
        })
      );

      const logsDir = path.join(tempDir, "mcp-logs");
      fs.mkdirSync(logsDir, { recursive: true });
      const diagnosticLogs = ["github.log", "mcp-gateway.log", "safeoutputs.log", "stderr.log"].map(name => path.join(logsDir, name));
      for (const logFile of diagnosticLogs) {
        fs.writeFileSync(logFile, `{"type":"tool_result","content":"session=${gatewayToken}"}`);
      }

      let modifiedScript = redactScript
        .replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir.replace(/\\/g, "\\\\")}", targetExtensions)`)
        .replace('path.join(process.env.RUNNER_TEMP || "/tmp", "gh-aw/mcp-config/gateway-output.json")', `"${gatewayOutput.replace(/\\/g, "\\\\")}"`)
        .replace('path.join(process.env.RUNNER_TEMP || "/tmp", "gh-aw/mcp-config/mcp-servers.json")', `"${path.join(configDir, "mcp-servers.json").replace(/\\/g, "\\\\")}"`);

      await eval(`(async () => { ${modifiedScript}; await main(); })()`);

      for (const logFile of diagnosticLogs) {
        const redacted = fs.readFileSync(logFile, "utf8");
        expect(redacted).not.toContain(gatewayToken);
        expect(redacted).toContain("***REDACTED***");
      }
    });
  });
});
