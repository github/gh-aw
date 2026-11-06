import { afterEach, describe, expect, it } from "vitest";
import fs from "fs";
import path from "path";
import crypto from "crypto";
import { spawn, spawnSync } from "child_process";
import { fileURLToPath } from "url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const helper = path.resolve(__dirname, "../sh/release_integrity.sh");
const workspaces = [];
const children = [];

function workspace() {
  const root = path.join(__dirname, `.release-staging-test-${crypto.randomUUID()}`);
  fs.mkdirSync(root, { mode: 0o700 });
  workspaces.push(root);
  return root;
}

function environment(root, extra = {}) {
  return {
    ...process.env,
    RUNNER_TEMP: root,
    GITHUB_RUN_ID: "123",
    GITHUB_RUN_ATTEMPT: "1",
    GITHUB_JOB: "agent",
    RUNNER_NAME: "shared-runner",
    RELEASE_HELPER: helper,
    ...extra,
  };
}

async function allocate(root, extra = {}) {
  const child = spawn(
    "bash",
    [
      "-c",
      `set -euo pipefail
umask 000
source "$RELEASE_HELPER"
create_release_staging awf
printf '%s\\n' "$TEMP_DIR"
IFS= read -r result
exit "$result"`,
    ],
    { cwd: root, env: environment(root, extra), stdio: ["pipe", "pipe", "pipe"] }
  );
  children.push(child);
  let stdout = "";
  let stderr = "";
  child.stdout.on("data", chunk => {
    stdout += chunk;
  });
  child.stderr.on("data", chunk => {
    stderr += chunk;
  });
  const completion = new Promise((resolve, reject) => {
    child.once("error", reject);
    child.once("close", (code, signal) => resolve({ code, signal, stdout, stderr }));
  });
  const dir = await new Promise((resolve, reject) => {
    child.stdout.on("data", () => {
      if (stdout.includes("\n")) resolve(stdout.trim());
    });
    child.once("error", reject);
    child.once("close", () => {
      if (!stdout.includes("\n")) reject(new Error(`Allocation failed: ${stderr}`));
    });
  });
  return {
    dir,
    child,
    completion,
    finish(code = 0) {
      child.stdin.end(`${code}\n`);
      return completion;
    },
  };
}

afterEach(async () => {
  for (const child of children.splice(0)) {
    if (child.exitCode === null && child.signalCode === null) {
      const closed = new Promise(resolve => child.once("close", resolve));
      child.kill("SIGTERM");
      await closed;
    }
  }
  for (const root of workspaces.splice(0)) {
    fs.rmSync(root, { recursive: true, force: true });
  }
});

describe("invocation-owned release staging", () => {
  it("allocates private unique directories for concurrent jobs on the same runner", async () => {
    const root = workspace();
    const jobs = await Promise.all(Array.from({ length: 12 }, () => allocate(root)));
    expect(new Set(jobs.map(job => job.dir)).size).toBe(jobs.length);
    for (const job of jobs) {
      expect(path.dirname(job.dir)).toBe(root);
      expect(fs.statSync(job.dir).mode & 0o777).toBe(0o700);
      fs.writeFileSync(path.join(job.dir, "artifact"), job.dir);
    }
    for (const [index, job] of jobs.entries()) {
      expect((await job.finish()).code).toBe(0);
      expect(fs.existsSync(job.dir)).toBe(false);
      for (const activeJob of jobs.slice(index + 1)) {
        expect(fs.readFileSync(path.join(activeJob.dir, "artifact"), "utf8")).toBe(activeJob.dir);
      }
    }
  });

  it("does not delete another job's staging when an installation fails", async () => {
    const root = workspace();
    const failed = await allocate(root);
    const active = await allocate(root);
    expect((await failed.finish(7)).code).toBe(7);
    expect(fs.existsSync(failed.dir)).toBe(false);
    expect(fs.existsSync(active.dir)).toBe(true);
    expect((await active.finish()).code).toBe(0);
  });

  it("cleans only its allocation on interruption without sudo", async () => {
    const root = workspace();
    const interrupted = await allocate(root);
    const active = await allocate(root);
    interrupted.child.kill("SIGTERM");
    const result = await interrupted.completion;
    expect(result.code).toBe(143);
    expect(fs.existsSync(interrupted.dir)).toBe(false);
    expect(fs.existsSync(active.dir)).toBe(true);
    await active.finish();
  });

  it("leaves hard-killed allocations untouched and allocates a fresh root on retry", async () => {
    const root = workspace();
    const killed = await allocate(root);
    fs.writeFileSync(path.join(killed.dir, "leftover"), "do not reclaim");
    killed.child.kill("SIGKILL");
    expect((await killed.completion).signal).toBe("SIGKILL");
    const retry = await allocate(root, { GITHUB_RUN_ATTEMPT: "2" });
    expect(retry.dir).not.toBe(killed.dir);
    expect(fs.readFileSync(path.join(killed.dir, "leftover"), "utf8")).toBe("do not reclaim");
    await retry.finish();
    expect(fs.existsSync(killed.dir)).toBe(true);
  });

  it("refuses cleanup when its allocation has been replaced by a symlink", async () => {
    const root = workspace();
    const job = await allocate(root);
    const parked = `${job.dir}-parked`;
    const otherJob = path.join(root, "other-job");
    fs.mkdirSync(otherJob);
    fs.writeFileSync(path.join(otherJob, "artifact"), "preserved");
    fs.renameSync(job.dir, parked);
    fs.symlinkSync(otherJob, job.dir);
    expect((await job.finish()).stderr).toContain("refusing to clean replaced");
    expect(fs.lstatSync(job.dir).isSymbolicLink()).toBe(true);
    expect(fs.readFileSync(path.join(otherJob, "artifact"), "utf8")).toBe("preserved");
    expect(fs.existsSync(parked)).toBe(true);
  });

  it("refuses cleanup when a different inode occupies the allocation path", async () => {
    const root = workspace();
    const job = await allocate(root);
    fs.renameSync(job.dir, `${job.dir}-parked`);
    fs.mkdirSync(job.dir, { mode: 0o700 });
    fs.writeFileSync(path.join(job.dir, "artifact"), "other invocation");
    expect((await job.finish()).stderr).toContain("refusing to clean replaced");
    expect(fs.readFileSync(path.join(job.dir, "artifact"), "utf8")).toBe("other invocation");
  });

  it("warns about unreclaimable owned files without escalating privileges", () => {
    const root = workspace();
    const result = spawnSync(
      "bash",
      [
        "-c",
        `set -euo pipefail
source "$RELEASE_HELPER"
create_release_staging copilot
printf '%s\\n' "$TEMP_DIR"
rm() { return 1; }
sudo() { echo "unexpected privilege escalation" >&2; exit 99; }
exit 7`,
      ],
      { cwd: root, env: environment(root), encoding: "utf8" }
    );
    expect(result.status).toBe(7);
    expect(result.stderr).toContain("unable to reclaim owned installer staging directory without elevated permissions");
    expect(result.stderr).not.toContain("unexpected privilege escalation");
    expect(fs.existsSync(result.stdout.trim())).toBe(true);
  });

  it("does not adopt or remove a colliding pre-existing allocation", () => {
    const root = workspace();
    const fakeBin = path.join(root, "bin");
    fs.mkdirSync(fakeBin);
    fs.writeFileSync(path.join(fakeBin, "od"), "#!/bin/sh\nprintf '00000000000000000000000000000000\\n'\n", { mode: 0o755 });
    const colliding = path.join(root, "awf-install-00000000000000000000000000000000");
    fs.mkdirSync(colliding);
    fs.writeFileSync(path.join(colliding, "artifact"), "other invocation");
    const result = spawnSync("bash", ["-c", 'set -euo pipefail; source "$RELEASE_HELPER"; create_release_staging awf'], {
      cwd: root,
      env: environment(root, { PATH: `${fakeBin}:${process.env.PATH}` }),
      encoding: "utf8",
    });
    expect(result.status).toBe(1);
    expect(result.stderr).toContain("Could not allocate a unique release staging directory");
    expect(fs.readFileSync(path.join(colliding, "artifact"), "utf8")).toBe("other invocation");
  });

  it("uses the current work directory when RUNNER_TEMP is absent", async () => {
    const root = workspace();
    const job = await allocate(root, { RUNNER_TEMP: "" });
    expect(path.dirname(job.dir)).toBe(root);
    await job.finish();
  });

  it("canonicalizes the staging parent before recording ownership", async () => {
    const root = workspace();
    const alias = path.join(root, "alias");
    fs.symlinkSync(root, alias);
    const job = await allocate(root, { RUNNER_TEMP: alias });
    expect(path.dirname(job.dir)).toBe(root);
    await job.finish();
  });

  it("converts a Windows RUNNER_TEMP path for Bash without changing its environment", async () => {
    const root = workspace();
    const bin = path.join(root, "bin");
    const log = path.join(root, "cygpath.log");
    fs.mkdirSync(bin);
    fs.writeFileSync(path.join(bin, "cygpath"), '#!/bin/sh\nprintf "%s\\n" "$2" > "$CYGPATH_LOG"\nprintf "%s\\n" "$STAGING_PARENT"\n', { mode: 0o755 });
    const windowsPath = "C:\\runner\\job temp";
    const job = await allocate(root, {
      RUNNER_TEMP: windowsPath,
      PATH: `${bin}:${process.env.PATH}`,
      CYGPATH_LOG: log,
      STAGING_PARENT: root,
    });
    expect(fs.readFileSync(log, "utf8").trim()).toBe(windowsPath);
    expect(path.dirname(job.dir)).toBe(root);
    await job.finish();
  });

  it("supports BSD stat ownership snapshots without requiring GNU stat", async () => {
    const root = workspace();
    const bin = path.join(root, "bin");
    fs.mkdirSync(bin);
    const stat = spawnSync("bash", ["-c", "command -v stat"], { encoding: "utf8" }).stdout.trim();
    fs.writeFileSync(
      path.join(bin, "stat"),
      `#!/bin/sh
if [ "$1" = "-c" ]; then exit 1; fi
exec "$REAL_STAT" -c "$2" "$3"
`,
      { mode: 0o755 }
    );
    const job = await allocate(root, { PATH: `${bin}:${process.env.PATH}`, REAL_STAT: stat });
    expect((await job.finish()).code).toBe(0);
    expect(fs.existsSync(job.dir)).toBe(false);
  });
});
