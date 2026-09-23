import { describe, it, expect, vi } from "vitest";
import { createRequire } from "module";
import { createHash } from "crypto";

const require = createRequire(import.meta.url);
const {
  runProcess,
  formatDuration,
  sleep,
  buildCopilotSDKEnv,
  isCopilotSDKEnabled,
  resolveStallWarningIntervalMs,
  formatStepTimeoutBudget,
  DEFAULT_STALL_WARNING_INTERVAL_MS,
  MIN_STALL_WARNING_INTERVAL_MS,
  MAX_STALL_WARNING_INTERVAL_MS,
} = require("./process_runner.cjs");

describe("process_runner.cjs", () => {
  describe("formatDuration", () => {
    it("formats zero milliseconds as 0s", () => {
      expect(formatDuration(0)).toBe("0s");
    });

    it("formats sub-minute durations as seconds only", () => {
      expect(formatDuration(1000)).toBe("1s");
      expect(formatDuration(45000)).toBe("45s");
      expect(formatDuration(59999)).toBe("59s");
    });

    it("formats exactly one minute", () => {
      expect(formatDuration(60000)).toBe("1m 0s");
    });

    it("formats minutes and seconds", () => {
      expect(formatDuration(192000)).toBe("3m 12s");
      expect(formatDuration(125500)).toBe("2m 5s");
    });

    it("truncates sub-second precision", () => {
      expect(formatDuration(1999)).toBe("1s");
    });
  });

  describe("sleep", () => {
    it("returns a promise that resolves after the given delay", async () => {
      vi.useFakeTimers();
      try {
        const promise = sleep(1000);
        vi.advanceTimersByTime(1000);
        await expect(promise).resolves.toBeUndefined();
      } finally {
        vi.useRealTimers();
      }
    });

    it("resolves immediately for 0ms", async () => {
      await expect(sleep(0)).resolves.toBeUndefined();
    });
  });

  describe("runProcess", () => {
    it("resolves with exitCode 0 for a successful command", async () => {
      const logs = [];
      const result = await runProcess({
        command: process.execPath,
        args: ["-e", "process.exit(0)"],
        attempt: 0,
        log: msg => logs.push(msg),
      });
      expect(result.exitCode).toBe(0);
      expect(result.durationMs).toBeGreaterThanOrEqual(0);
    });

    it("writes provided stdin to the child and closes the stream", async () => {
      const logs = [];
      const stdin = Buffer.concat([Buffer.from("prompt\n", "utf8"), Buffer.from([0, 0xff]), Buffer.alloc(128 * 1024, 0x42)]);
      const expectedHash = createHash("sha256").update(stdin).digest("hex");
      const result = await runProcess({
        command: process.execPath,
        args: ["-e", "const chunks=[]; process.stdin.on('data', chunk => chunks.push(chunk)); process.stdin.on('end', () => process.stdout.write(require('crypto').createHash('sha256').update(Buffer.concat(chunks)).digest('hex')));"],
        attempt: 0,
        log: msg => logs.push(msg),
        stdin,
      });
      expect(result.exitCode).toBe(0);
      expect(result.output).toContain(expectedHash);
    });

    it("resolves with the actual non-zero exit code on failure", async () => {
      const logs = [];
      const result = await runProcess({
        command: process.execPath,
        args: ["-e", "process.exit(42)"],
        attempt: 0,
        log: msg => logs.push(msg),
      });
      expect(result.exitCode).toBe(42);
    });

    it("synthesizes the conventional 128+signal exit code when the child is killed by a fatal signal", async () => {
      const logs = [];
      const result = await runProcess({
        command: process.execPath,
        // Self-signal rather than relying on the OS to deliver SIGSEGV so the test is
        // deterministic across platforms; Node reports code=null, signal="SIGSEGV" here,
        // exactly as it would for a real crash.
        args: ["-e", "process.kill(process.pid, 'SIGSEGV')"],
        attempt: 0,
        log: msg => logs.push(msg),
      });
      expect(result.exitCode).toBe(139); // 128 + SIGSEGV(11)
      expect(logs.some(l => l.includes("signal=SIGSEGV"))).toBe(true);
    });

    it("collects stdout output and sets hasOutput", async () => {
      const logs = [];
      const result = await runProcess({
        command: process.execPath,
        args: ["-e", 'process.stdout.write("hello stdout"); process.exit(0)'],
        attempt: 0,
        log: msg => logs.push(msg),
      });
      expect(result.hasOutput).toBe(true);
      expect(result.output).toContain("hello stdout");
    });

    it("collects stderr output and sets hasOutput", async () => {
      const logs = [];
      const result = await runProcess({
        command: process.execPath,
        args: ["-e", 'process.stderr.write("hello stderr"); process.exit(1)'],
        attempt: 0,
        log: msg => logs.push(msg),
      });
      expect(result.hasOutput).toBe(true);
      expect(result.output).toContain("hello stderr");
    });

    it("sets hasOutput false when no output is produced", async () => {
      const logs = [];
      const result = await runProcess({
        command: process.execPath,
        args: ["-e", "process.exit(1)"],
        attempt: 0,
        log: msg => logs.push(msg),
      });
      expect(result.hasOutput).toBe(false);
      expect(result.output).toBe("");
    });

    it("logs spawning with logArgs instead of args when provided", async () => {
      const logs = [];
      await runProcess({
        command: process.execPath,
        args: ["-e", "process.exit(0)"],
        attempt: 0,
        log: msg => logs.push(msg),
        logArgs: ["<redacted>"],
      });
      const spawnLog = logs.find(l => l.includes("spawning"));
      expect(spawnLog).toContain("<redacted>");
      expect(spawnLog).not.toContain("-e");
    });

    it("falls back to args for logging when logArgs is not provided", async () => {
      const logs = [];
      await runProcess({
        command: process.execPath,
        args: ["-e", "process.exit(0)"],
        attempt: 0,
        log: msg => logs.push(msg),
      });
      const spawnLog = logs.find(l => l.includes("spawning"));
      expect(spawnLog).toContain("-e");
    });

    it("uses the attempt number in log messages", async () => {
      const logs = [];
      await runProcess({
        command: process.execPath,
        args: ["-e", "process.exit(0)"],
        attempt: 2,
        log: msg => logs.push(msg),
      });
      expect(logs.some(l => l.includes("attempt 3"))).toBe(true);
    });

    it("resolves with exitCode 1 and hasOutput false when command is not found", async () => {
      const logs = [];
      const result = await runProcess({
        command: "/nonexistent-binary-xyz",
        args: [],
        attempt: 0,
        log: msg => logs.push(msg),
      });
      expect(result.exitCode).toBe(1);
      const errorLog = logs.find(l => l.includes("failed to start process"));
      expect(errorLog).toBeTruthy();
    });

    it("collects combined stdout and stderr in output", async () => {
      const logs = [];
      const result = await runProcess({
        command: process.execPath,
        args: ["-e", 'process.stdout.write("out"); process.stderr.write("err"); process.exit(0)'],
        attempt: 0,
        log: msg => logs.push(msg),
      });
      expect(result.output).toContain("out");
      expect(result.output).toContain("err");
    });

    it("resolves with durationMs as a non-negative number", async () => {
      const logs = [];
      const result = await runProcess({
        command: process.execPath,
        args: ["-e", "process.exit(0)"],
        attempt: 0,
        log: msg => logs.push(msg),
      });
      expect(typeof result.durationMs).toBe("number");
      expect(result.durationMs).toBeGreaterThanOrEqual(0);
    });

    it("terminates a hung process after terminal-result inactivity", async () => {
      const logs = [];
      const result = await runProcess({
        command: process.execPath,
        args: ["-e", 'process.stdout.write("done"); setInterval(() => {}, 1000);'],
        attempt: 0,
        log: msg => logs.push(msg),
        postResultWatchdog: {
          shouldArm: () => true,
          inactivityTimeoutMs: 100,
          pollIntervalMs: 25,
          termGraceMs: 200,
        },
      });
      expect(result.exitCode).not.toBe(0);
      expect(result.durationMs).toBeLessThan(5000);
      expect(logs.some(line => line.includes("post-result watchdog armed"))).toBe(true);
      expect(logs.some(line => line.includes("post-result watchdog terminating idle process"))).toBe(true);
    });

    it("sets watchdogFired=true when post-result watchdog terminates the process", async () => {
      const logs = [];
      const result = await runProcess({
        command: process.execPath,
        args: ["-e", 'process.stdout.write("done"); setInterval(() => {}, 1000);'],
        attempt: 0,
        log: msg => logs.push(msg),
        postResultWatchdog: {
          shouldArm: () => true,
          inactivityTimeoutMs: 100,
          pollIntervalMs: 25,
          termGraceMs: 200,
        },
      });
      expect(result.watchdogFired).toBe(true);
      expect(logs.some(line => line.includes("watchdogFired=true"))).toBe(true);
    });

    it("sets watchdogFired=false when the process exits normally without the watchdog firing", async () => {
      const logs = [];
      const result = await runProcess({
        command: process.execPath,
        args: ["-e", "process.exit(0)"],
        attempt: 0,
        log: msg => logs.push(msg),
      });
      expect(result.watchdogFired).toBe(false);
    });

    it("sets watchdogFired=false when watchdog is configured but does not arm", async () => {
      const logs = [];
      const result = await runProcess({
        command: process.execPath,
        args: ["-e", "setTimeout(() => process.exit(0), 100);"],
        attempt: 0,
        log: msg => logs.push(msg),
        postResultWatchdog: {
          shouldArm: () => false,
          inactivityTimeoutMs: 50,
          pollIntervalMs: 25,
          termGraceMs: 100,
        },
      });
      expect(result.watchdogFired).toBe(false);
    });

    it("does not terminate processes when watchdog is not armed", async () => {
      const logs = [];
      const result = await runProcess({
        command: process.execPath,
        args: ["-e", "setTimeout(() => process.exit(0), 250);"],
        attempt: 0,
        log: msg => logs.push(msg),
        postResultWatchdog: {
          shouldArm: () => false,
          inactivityTimeoutMs: 50,
          pollIntervalMs: 25,
          termGraceMs: 100,
        },
      });
      expect(result.exitCode).toBe(0);
      expect(logs.some(line => line.includes("post-result watchdog terminating idle process"))).toBe(false);
    });

    it("terminates a running process when runtime guard requests it", async () => {
      const logs = [];
      let checks = 0;
      const result = await runProcess({
        command: process.execPath,
        args: ["-e", "setInterval(() => process.stdout.write('.'), 20);"],
        attempt: 0,
        log: msg => logs.push(msg),
        runtimeGuard: {
          shouldTerminate: () => {
            checks += 1;
            if (checks < 3) return false;
            return { terminate: true, reason: "test guard tripped" };
          },
          pollIntervalMs: 25,
          termGraceMs: 200,
        },
      });
      expect(result.exitCode).not.toBe(0);
      expect(result.runtimeGuardFired).toBe(true);
      expect(result.runtimeGuardReason).toContain("test guard tripped");
      expect(result.watchdogFired).toBe(false);
      expect(logs.some(line => line.includes("runtime guard requested termination"))).toBe(true);
    });

    it("escalates to SIGKILL after the grace period even when the poll interval is longer", async () => {
      const logs = [];
      const started = Date.now();
      const result = await runProcess({
        command: process.execPath,
        // Ignores SIGTERM, so only SIGKILL can stop it.
        args: ["-e", "process.on('SIGTERM', () => {}); setInterval(() => {}, 50);"],
        attempt: 0,
        log: msg => logs.push(msg),
        runtimeGuard: {
          shouldTerminate: () => ({ terminate: true, reason: "kill escalation test" }),
          pollIntervalMs: 50,
          // Grace period is far shorter than the poll interval below would allow.
          termGraceMs: 150,
        },
      });
      const elapsed = Date.now() - started;
      expect(result.runtimeGuardFired).toBe(true);
      expect(result.exitCode).not.toBe(0);
      expect(logs.some(line => line.includes("runtime guard forcing process exit after 150ms grace (SIGKILL)"))).toBe(true);
      // Without the dedicated escalation timer this would take multiple poll cycles.
      expect(elapsed).toBeLessThan(5000);
    });

    it("supports an asynchronous shouldTerminate without overlapping polls", async () => {
      const logs = [];
      let inFlight = 0;
      let maxInFlight = 0;
      let checks = 0;
      const result = await runProcess({
        command: process.execPath,
        args: ["-e", "setInterval(() => process.stdout.write('.'), 20);"],
        attempt: 0,
        log: msg => logs.push(msg),
        runtimeGuard: {
          shouldTerminate: async () => {
            inFlight += 1;
            maxInFlight = Math.max(maxInFlight, inFlight);
            await new Promise(resolve => setTimeout(resolve, 60));
            inFlight -= 1;
            checks += 1;
            return checks < 2 ? false : { terminate: true, reason: "async guard tripped" };
          },
          pollIntervalMs: 25,
          termGraceMs: 200,
        },
      });
      expect(maxInFlight).toBe(1);
      expect(result.runtimeGuardFired).toBe(true);
      expect(result.runtimeGuardReason).toContain("async guard tripped");
      expect(result.watchdogFired).toBe(false);
    });

    it("does not enable watchdog when inactivityTimeoutMs is missing or invalid", async () => {
      const logs = [];
      const result = await runProcess({
        command: process.execPath,
        args: ["-e", "setTimeout(() => process.exit(0), 100);"],
        attempt: 0,
        log: msg => logs.push(msg),
        postResultWatchdog: {
          shouldArm: () => true,
          // intentionally missing inactivityTimeoutMs
        },
      });
      expect(result.exitCode).toBe(0);
      expect(logs.some(line => line.includes("post-result watchdog armed"))).toBe(false);
    });

    it("truncates logArgs to 200 chars in spawn log", async () => {
      const logs = [];
      const longArg = "x".repeat(300);
      await runProcess({
        command: process.execPath,
        args: ["-e", "process.exit(0)"],
        attempt: 0,
        log: msg => logs.push(msg),
        logArgs: [longArg],
      });
      const spawnLog = logs.find(l => l.includes("spawning"));
      // logArgs is a single arg made entirely of 'x' characters.  After truncation to 200
      // chars the spawn log line must end with at most 200 consecutive x's.
      const trailingXs = spawnLog?.match(/x+$/)?.[0] ?? "";
      expect(trailingXs.length).toBeLessThanOrEqual(200);
    });

    it("spawns the child process in GITHUB_WORKSPACE when set", async () => {
      const os = require("os");
      const origWorkspace = process.env.GITHUB_WORKSPACE;
      const tmpDir = os.tmpdir();
      try {
        process.env.GITHUB_WORKSPACE = tmpDir;
        const logs = [];
        const result = await runProcess({
          command: process.execPath,
          args: ["-e", "process.stdout.write(process.cwd()); process.exit(0)"],
          attempt: 0,
          log: msg => logs.push(msg),
        });
        // The child process cwd should match GITHUB_WORKSPACE (resolve symlinks for comparison)
        const { realpathSync } = require("fs");
        expect(realpathSync(result.output.trim())).toBe(realpathSync(tmpDir));
      } finally {
        if (origWorkspace === undefined) {
          delete process.env.GITHUB_WORKSPACE;
        } else {
          process.env.GITHUB_WORKSPACE = origWorkspace;
        }
      }
    });

    it("GH_AW_ENGINE_CWD takes precedence over GITHUB_WORKSPACE as spawn cwd", async () => {
      const os = require("os");
      const origWorkspace = process.env.GITHUB_WORKSPACE;
      const origEngineCwd = process.env.GH_AW_ENGINE_CWD;
      const tmpDir = os.tmpdir();
      try {
        process.env.GITHUB_WORKSPACE = "/should-not-be-used";
        process.env.GH_AW_ENGINE_CWD = tmpDir;
        const logs = [];
        const result = await runProcess({
          command: process.execPath,
          args: ["-e", "process.stdout.write(process.cwd()); process.exit(0)"],
          attempt: 0,
          log: msg => logs.push(msg),
        });
        const { realpathSync } = require("fs");
        expect(realpathSync(result.output.trim())).toBe(realpathSync(tmpDir));
      } finally {
        if (origWorkspace === undefined) {
          delete process.env.GITHUB_WORKSPACE;
        } else {
          process.env.GITHUB_WORKSPACE = origWorkspace;
        }
        if (origEngineCwd === undefined) {
          delete process.env.GH_AW_ENGINE_CWD;
        } else {
          process.env.GH_AW_ENGINE_CWD = origEngineCwd;
        }
      }
    });

    describe("copilot sdk env helpers", () => {
      it("detects copilot sdk mode from COPILOT_SDK_URI", () => {
        expect(isCopilotSDKEnabled({ COPILOT_SDK_URI: "http://127.0.0.1:3000" })).toBe(true);
        expect(isCopilotSDKEnabled({})).toBe(false);
      });

      it("returns empty env when sdk mode is disabled", () => {
        expect(buildCopilotSDKEnv({})).toEqual({});
      });

      it("forwards COPILOT_SDK_URI in sdk mode", () => {
        expect(buildCopilotSDKEnv({ COPILOT_SDK_URI: "http://127.0.0.1:3000" })).toEqual({
          COPILOT_SDK_URI: "http://127.0.0.1:3000",
          COPILOT_SDK_LOG_LEVEL: "all",
        });
      });

      it("derives COPILOT_SDK_SEND_TIMEOUT_MS from GH_AW_TIMEOUT_MINUTES", () => {
        expect(
          buildCopilotSDKEnv({
            COPILOT_SDK_URI: "http://127.0.0.1:3000",
            GH_AW_TIMEOUT_MINUTES: "60",
          })
        ).toEqual({
          COPILOT_SDK_URI: "http://127.0.0.1:3000",
          COPILOT_SDK_LOG_LEVEL: "all",
          COPILOT_SDK_SEND_TIMEOUT_MS: "3570000",
        });
      });

      it("respects an explicit COPILOT_SDK_SEND_TIMEOUT_MS override", () => {
        expect(
          buildCopilotSDKEnv({
            COPILOT_SDK_URI: "http://127.0.0.1:3000",
            GH_AW_TIMEOUT_MINUTES: "60",
            COPILOT_SDK_SEND_TIMEOUT_MS: "1234",
          })
        ).toEqual({
          COPILOT_SDK_URI: "http://127.0.0.1:3000",
          COPILOT_SDK_LOG_LEVEL: "all",
          COPILOT_SDK_SEND_TIMEOUT_MS: "1234",
        });
      });

      it("ignores invalid GH_AW_TIMEOUT_MINUTES values", () => {
        expect(
          buildCopilotSDKEnv({
            COPILOT_SDK_URI: "http://127.0.0.1:3000",
            GH_AW_TIMEOUT_MINUTES: "not-a-number",
          })
        ).toEqual({
          COPILOT_SDK_URI: "http://127.0.0.1:3000",
          COPILOT_SDK_LOG_LEVEL: "all",
        });
      });

      it("respects an explicit COPILOT_SDK_LOG_LEVEL override", () => {
        expect(
          buildCopilotSDKEnv({
            COPILOT_SDK_URI: "http://127.0.0.1:3000",
            COPILOT_SDK_LOG_LEVEL: "error",
          })
        ).toEqual({
          COPILOT_SDK_URI: "http://127.0.0.1:3000",
          COPILOT_SDK_LOG_LEVEL: "error",
        });
      });
    });
  });
  describe("resolveStallWarningIntervalMs", () => {
    it("defaults when the env var is unset or blank", () => {
      expect(resolveStallWarningIntervalMs({})).toBe(DEFAULT_STALL_WARNING_INTERVAL_MS);
      expect(resolveStallWarningIntervalMs({ GH_AW_HARNESS_STALL_WARNING_MS: "  " })).toBe(DEFAULT_STALL_WARNING_INTERVAL_MS);
    });

    it("defaults when the env var is not a number", () => {
      expect(resolveStallWarningIntervalMs({ GH_AW_HARNESS_STALL_WARNING_MS: "soon" })).toBe(DEFAULT_STALL_WARNING_INTERVAL_MS);
    });

    it("disables stall warnings when explicitly set to zero or negative", () => {
      expect(resolveStallWarningIntervalMs({ GH_AW_HARNESS_STALL_WARNING_MS: "0" })).toBe(0);
      expect(resolveStallWarningIntervalMs({ GH_AW_HARNESS_STALL_WARNING_MS: "-1" })).toBe(0);
    });

    it("clamps configured values to the supported range", () => {
      expect(resolveStallWarningIntervalMs({ GH_AW_HARNESS_STALL_WARNING_MS: "10" })).toBe(MIN_STALL_WARNING_INTERVAL_MS);
      expect(resolveStallWarningIntervalMs({ GH_AW_HARNESS_STALL_WARNING_MS: String(MAX_STALL_WARNING_INTERVAL_MS * 2) })).toBe(MAX_STALL_WARNING_INTERVAL_MS);
      expect(resolveStallWarningIntervalMs({ GH_AW_HARNESS_STALL_WARNING_MS: "60000" })).toBe(60000);
    });
  });

  describe("formatStepTimeoutBudget", () => {
    it("returns an empty string when the step timeout is unknown or invalid", () => {
      expect(formatStepTimeoutBudget(Date.now(), {})).toBe("");
      expect(formatStepTimeoutBudget(Date.now(), { GH_AW_TIMEOUT_MINUTES: "0" })).toBe("");
      expect(formatStepTimeoutBudget(Date.now(), { GH_AW_TIMEOUT_MINUTES: "nope" })).toBe("");
    });

    it("reports the remaining step budget when the timeout is known", () => {
      const fragment = formatStepTimeoutBudget(Date.now(), { GH_AW_TIMEOUT_MINUTES: "10" });
      expect(fragment).toContain("timeout-minutes=10");
      expect(fragment).toContain("GitHub Actions will cancel this step in about");
    });

    it("reports an exhausted budget when the step timeout has elapsed", () => {
      const fragment = formatStepTimeoutBudget(Date.now() - 20 * 60 * 1000, { GH_AW_TIMEOUT_MINUTES: "10" });
      expect(fragment).toContain("has been reached");
    });
  });

  describe("runProcess stall watchdog", () => {
    it("logs a stall warning when the process produces no output", async () => {
      const logs = [];
      await runProcess({
        command: process.execPath,
        args: ["-e", "setTimeout(() => process.exit(0), 400);"],
        attempt: 0,
        log: msg => logs.push(msg),
        stallWarningIntervalMs: 100,
      });
      const stallLogs = logs.filter(line => line.includes("stall watchdog: no output"));
      expect(stallLogs.length).toBeGreaterThan(0);
      expect(stallLogs[0]).toContain("the step may be hung");
      expect(logs.some(line => line.includes("stallWarnings="))).toBe(true);
    });

    it("logs a recovery line when output resumes after a stall", async () => {
      const logs = [];
      await runProcess({
        command: process.execPath,
        args: ["-e", 'setTimeout(() => { process.stdout.write("late"); process.exit(0); }, 300);'],
        attempt: 0,
        log: msg => logs.push(msg),
        stallWarningIntervalMs: 100,
      });
      expect(logs.some(line => line.includes("stall watchdog: output resumed after"))).toBe(true);
    });

    it("does not log stall warnings when the process keeps producing output", async () => {
      const logs = [];
      await runProcess({
        command: process.execPath,
        args: ["-e", 'const t = setInterval(() => process.stdout.write("."), 25); setTimeout(() => { clearInterval(t); process.exit(0); }, 400);'],
        attempt: 0,
        log: msg => logs.push(msg),
        stallWarningIntervalMs: 200,
      });
      expect(logs.some(line => line.includes("stall watchdog: no output"))).toBe(false);
    });

    it("does not log stall warnings when the interval is disabled", async () => {
      const logs = [];
      await runProcess({
        command: process.execPath,
        args: ["-e", "setTimeout(() => process.exit(0), 300);"],
        attempt: 0,
        log: msg => logs.push(msg),
        stallWarningIntervalMs: 0,
      });
      expect(logs.some(line => line.includes("stall watchdog"))).toBe(false);
    });
  });
});
