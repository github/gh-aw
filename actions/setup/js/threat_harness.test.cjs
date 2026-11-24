import { afterEach, beforeEach, expect, it } from "vitest";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";

const { runHarness } = require("./threat_harness.cjs");
let dir;
let sink;
let capture;
const eligibility = { prompt_injection: false, secret_leak: true, malicious_patch: false };
const prompt = "private\n".repeat(32000);
beforeEach(() => {
  dir = fs.mkdtempSync(path.join(os.tmpdir(), "quoted ' harness-"));
  sink = path.join(dir, "sink.json");
  capture = path.join(dir, "capture.json");
});
afterEach(() => fs.rmSync(dir, { recursive: true, force: true }));

function fixture(source, convention, options = {}) {
  const script = path.join(dir, "unfamiliar engine.cjs");
  fs.writeFileSync(script, `#!${process.execPath}\nconst fs = require("node:fs"); const env = process.env;\n${source}`, { mode: 0o700 });
  return {
    launch: { id: "different-engine-id", convention, command: script, model: "fixture-model", provider: "fixture-proxy", args: ["one arg with spaces"], env: { CAPTURE: capture, GH_AW_MAX_TURNS: "99" } },
    prompt,
    sink,
    eligibility,
    timeoutMs: 5000,
    ...options,
  };
}

it.each(["copilot", "claude", "codex"])("uses resolved argv with the %s convention and binds the CLI report sink", async convention => {
  const reasons = path.join(dir, "threat-detection-reasons.json");
  fs.writeFileSync(reasons, "stale");
  const result = await runHarness(
    fixture(
      `
    const args = process.argv.slice(2);
    const file = args[args.indexOf("--prompt-file")+1];
    fs.writeFileSync(env.CAPTURE, JSON.stringify({
      args, file, cwd:process.cwd(), prompt: fs.readFileSync(file, "utf8"), mode: fs.statSync(file).mode & 511,
      model: env.COPILOT_MODEL, turns: env.GH_AW_MAX_TURNS, eligible: [env.THREAT_DETECTION_ELIGIBLE_PROMPT_INJECTION, env.THREAT_DETECTION_ELIGIBLE_SECRET_LEAK, env.THREAT_DETECTION_ELIGIBLE_MALICIOUS_PATCH],
      reasons: env.THREAT_DETECTION_REASONS_FILE, stale: fs.existsSync(env.THREAT_DETECTION_REASONS_FILE)
    }));
    require("node:child_process").execFileSync("threat_detection_result", ["--result-file", env.CAPTURE + ".wrong", "--secret-leak=true", "--prompt-injection=false", "--malicious-patch=false", "--reason", "literal ' $(not-run)"], {
      env: {...env, THREAT_DETECTION_BOUND_RESULT_FILE:env.CAPTURE + ".wrong", THREAT_DETECTION_RESULT_FILE:env.CAPTURE + ".wrong"}
    });
  `,
      convention,
      { maxTurns: 7 }
    )
  );
  expect(result.error).toBeNull();
  const observed = JSON.parse(fs.readFileSync(capture));
  expect(observed).toMatchObject({ turns: "7", prompt: expect.stringContaining(prompt), mode: 0o600, reasons, stale: false, eligible: ["false", "true", "false"] });
  expect(observed.args[0]).toBe("one arg with spaces");
  if (convention === "codex") {
    expect(observed.args.slice(1, 6)).toEqual(["exec", "-c", "model=fixture-model", "-c", "model_provider=fixture-proxy"]);
    expect(observed.args).not.toContain("--dangerously-bypass-approvals-and-sandbox");
    expect(observed.args[observed.args.indexOf("--sandbox") + 1]).toBe("workspace-write");
    expect(observed.cwd).toBe(path.dirname(sink));
    expect(observed.args[observed.args.indexOf("--cd") + 1]).toBe(observed.cwd);
    expect(observed.args).not.toContain("--add-dir");
    expect(observed.args).toEqual(expect.arrayContaining(["sandbox_workspace_write.writable_roots=[]", "sandbox_workspace_write.exclude_slash_tmp=true", "sandbox_workspace_write.exclude_tmpdir_env_var=true"]));
  }
  if (convention === "claude") expect(observed.args.slice(-6, -2)).toEqual(["--model", "fixture-model", "--max-turns", "7"]);
  if (convention === "copilot") {
    expect(observed.model).toBe("fixture-model");
    expect(observed.args).toContain("--disable-builtin-mcps");
    expect(observed.args).not.toContain("--allow-all-tools");
    expect(observed.args.flatMap((arg, i) => (arg === "--allow-tool" ? [observed.args[i + 1]] : []))).toEqual([`write(${reasons})`, "shell(threat_detection_result:*)"]);
  }
  expect(fs.existsSync(observed.file)).toBe(false);
  expect(fs.existsSync(capture + ".wrong")).toBe(false);
  expect(JSON.parse(fs.readFileSync(sink)).reasons).toEqual(["literal ' $(not-run)"]);
});

it("streams a large direct CLI prompt and scrubs the inherited turn limit", async () => {
  const opts = fixture(`fs.writeFileSync(env.CAPTURE, JSON.stringify({args:process.argv.slice(2), prompt:fs.readFileSync(0,"utf8"), turns:env.GH_AW_MAX_TURNS}));`, "claude");
  opts.launch.mode = "direct";
  expect((await runHarness(opts)).error).toBeNull();
  const observed = JSON.parse(fs.readFileSync(capture));
  expect(observed.prompt).toContain(prompt);
  expect(observed.args.at(-1)).toBe("-");
  expect(observed.args).not.toContain("--max-turns");
  expect(observed.turns).toBeUndefined();
});

it("returns errors rather than throwing on spawn or nonzero exit", async () => {
  const opts = fixture("process.exit(27);", "codex");
  expect(await runHarness(opts)).toMatchObject({ timedOut: false, exitCode: 27, error: expect.any(Error) });
  opts.launch.command += ".absent";
  expect(await runHarness(opts)).toMatchObject({ timedOut: false, error: expect.objectContaining({ code: "ENOENT" }) });
});

it("stops waiting when a complete (but potentially ineligible) report lands", async () => {
  const opts = fixture(`fs.writeFileSync(env.THREAT_DETECTION_RESULT_FILE, JSON.stringify({secret_leak:false, malicious_patch:true, prompt_injection:false, reasons:["ineligible"]})); setInterval(()=>{},1000);`, "copilot");
  expect(await runHarness(opts)).toMatchObject({ timedOut: false, cancelled: false, error: null });
});

it.skipIf(process.platform !== "linux").each(["timeout", "cancel"])("kills nested engine process groups on %s", async reason => {
  const controller = new AbortController();
  const opts = fixture(
    `
    const child = require("node:child_process").spawn(process.execPath, ["-e","setInterval(()=>{},1000)"], {detached:true,stdio:"ignore"});
    fs.writeFileSync(env.CAPTURE,String(child.pid));
    setInterval(()=>{},1000);
  `,
    "copilot",
    { signal: controller.signal, timeoutMs: reason === "timeout" ? 1000 : 4000 }
  );
  const run = runHarness(opts);
  let pid;
  try {
    await expect.poll(() => fs.existsSync(capture)).toBe(true);
    pid = Number(fs.readFileSync(capture));
    if (reason === "cancel") controller.abort();
    expect(await run).toMatchObject({ timedOut: reason === "timeout", cancelled: reason === "cancel", error: expect.any(Error) });
    // Some container init processes reap orphans lazily: zombies are no longer running.
    await expect.poll(() => !fs.existsSync(`/proc/${pid}`) || fs.readFileSync(`/proc/${pid}/stat`, "utf8").split(" ")[2] === "Z").toBe(true);
  } finally {
    controller.abort();
    if (pid) {
      try {
        process.kill(pid, "SIGKILL");
      } catch {}
    }
    await run;
  }
});
