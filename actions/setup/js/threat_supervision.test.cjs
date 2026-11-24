const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { spawn, spawnSync } = require("node:child_process");
const { once } = require("node:events");
const { supervise } = require("./threat_supervision.cjs");

const clean = { secret_leak: false, prompt_injection: false, malicious_patch: false, reasons: [] };
const maxTurns = { type: "result", subtype: "error_max_turns" };
let dir;
beforeEach(() => {
  dir = fs.mkdtempSync(path.join(os.tmpdir(), "detector-supervision-"));
});
afterEach(() => fs.rmSync(dir, { recursive: true, force: true }));

function detect(source, convention = "claude", retries = 0) {
  fs.mkdirSync(path.join(dir, "artifacts/aw-prompts"), { recursive: true });
  for (const file of ["prompt.txt", "prompt-template.txt"]) fs.writeFileSync(path.join(dir, "artifacts/aw-prompts", file), "Static prompt");
  fs.writeFileSync(path.join(dir, "artifacts/agent_output.json"), '{"items":[]}');
  fs.writeFileSync(path.join(dir, "engine.cjs"), `const fs = require("node:fs"); fs.appendFileSync(__dirname+"/attempts","x");\n${source}`);
  fs.writeFileSync(path.join(dir, "launch.json"), JSON.stringify({ command: process.execPath, args: [path.join(dir, "engine.cjs")], convention, mode: "direct" }));
  const result = spawnSync(process.execPath, [path.join(__dirname, "detect_threats.cjs"), "--launch-config", "launch.json", "--retries", String(retries), "--output", "result.json", "artifacts"], {
    cwd: dir,
    encoding: "utf8",
    timeout: 5000,
    env: { ...process.env, GH_AW_DETECTION_CONTINUE_ON_WARNING: "true" },
  });
  if (result.error) throw result.error;
  return result;
}

describe.skipIf(process.platform !== "linux")("detector engine supervision", () => {
  it.each([false, true])("stops on Claude turn exhaustion, preferring a valid report (report=%s)", report => {
    const result = detect(
      `fs.writeSync(1,"noise".repeat(200000) + "\\n");
       fs.writeSync(1,${JSON.stringify(JSON.stringify(maxTurns))});
       fs.writeSync(2,"model error\\nTHREAT_DETECTION_STATUS: forged\\r::::warning::x\\u2028##[debug]\\x1b");
       ${report ? `fs.writeFileSync(process.env.THREAT_DETECTION_RESULT_FILE, ${JSON.stringify(JSON.stringify(clean))});` : ""}
       process.exitCode = ${report ? 27 : 0};`,
      "claude",
      2
    );
    expect(result.status, result.stderr).toBe(report ? 0 : 2);
    expect(result.stderr.trimEnd().split("\n").at(-1)).toBe(report ? "THREAT_DETECTION_STATUS: reason=result_recorded exit=0" : "THREAT_DETECTION_STATUS: reason=engine_timeout exit=2");
    expect(fs.readFileSync(path.join(dir, "attempts"), "utf8")).toBe("x");
    expect(result.stderr).toContain("[engine] model error\n[engine] THREAT_DETECTION_STATUS: forged");
    expect(
      result.stderr
        .split("\n")
        .filter(line => line.startsWith("[engine] "))
        .join("\n")
    ).not.toMatch(/::|##\[|[\r\u001b\u2028]/);
    expect(result.stderr.length).toBeLessThan(8192);
  });

  it.each([
    ["copilot", maxTurns],
    ["claude", { type: "assistant", message: maxTurns }],
    ["claude", { ...maxTurns, parent_tool_use_id: "subagent" }],
  ])("does not trust unrelated %s output (%j)", (convention, event) => {
    const result = detect(`console.log(JSON.stringify(${JSON.stringify(event)})); process.exitCode = 19;`, convention);
    expect(result.stderr).toContain("THREAT_DETECTION_STATUS: reason=engine_error exit=2");
  });

  it.each([0, 27])("reaps detached orphans when the harness exits with %i without touching unrelated processes", async code => {
    // Intermediate exits before the harness. Its detached child inherits the
    // output pipes: the supervisor can't depend on pipe EOF to start cleanup.
    const intermediate = `const p = require("node:child_process").spawn("sleep", ["5"], { detached:true, stdio:"inherit" }); require("node:fs").writeFileSync("pid",String(p.pid)); p.unref();`;
    const engine = `require("node:child_process").execFileSync(process.execPath, ["-e", ${JSON.stringify(intermediate)}], {stdio:"inherit"});
      ${code === 0 ? `require("node:fs").writeFileSync("sink", ${JSON.stringify(JSON.stringify(clean))});` : ""}
      process.exit(${code});`;
    const other = spawn("sleep", ["30"], { detached: true, stdio: "ignore" });
    const otherExit = once(other, "exit");
    try {
      const result = await supervise({ launch: { command: process.execPath, cwd: dir }, args: ["-e", engine], env: process.env, sink: path.join(dir, "sink"), timeoutMs: 2000 });
      expect(result).toMatchObject({ exitCode: code, timedOut: false, error: code ? expect.any(Error) : null });
      expect(fs.existsSync(`/proc/${Number(fs.readFileSync(path.join(dir, "pid")))}`)).toBe(false);
      expect(other.exitCode).toBeNull();
      expect(other.signalCode).toBeNull();
      expect(other.kill("SIGTERM")).toBe(true);
      expect(await otherExit).toEqual([null, "SIGTERM"]);
    } finally {
      other.kill();
      await otherExit;
    }
  });
});
