import { afterEach, describe, expect, it } from "vitest";
import fs from "fs";
import path from "path";
import os from "os";
import { spawn, spawnSync } from "child_process";
import { fileURLToPath } from "url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const helper = path.resolve(__dirname, "../sh/release_integrity.sh");
const dockerImage = process.env.GH_AW_TEST_DOCKER_IMAGE;
const workspaces = [];
const children = [];

function workspace() {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-release-staging-"));
  workspaces.push(root);
  return root;
}

function finishFixtureCleanup(actions) {
  const errors = [];
  for (const action of actions) {
    try {
      action();
    } catch (error) {
      errors.push(error);
    }
  }
  if (errors.length) throw new AggregateError(errors, "Failed to clean Docker ownership fixtures");
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
  it("attempts every fixture cleanup before reporting failures", () => {
    const attempted = [];
    const failure = new Error("first allocation cannot be restored");
    expect(() =>
      finishFixtureCleanup([
        () => {
          attempted.push("first");
          throw failure;
        },
        () => {
          attempted.push("second");
        },
        () => {
          attempted.push("third");
        },
      ])
    ).toThrow("Failed to clean Docker ownership fixtures");
    expect(attempted).toEqual(["first", "second", "third"]);
  });

  it("runs simultaneous rootless installers with shared scratch without chown, sudo, or cross-job cleanup", async () => {
    const root = workspace();
    const sharedTemp = path.join(root, "shared-temp");
    const bin = path.join(root, "bin");
    const cache = path.join(root, "toolcache");
    const cacheBin = path.join(cache, "copilot-cli", "1.0.103", "x64", "bin");
    const forbiddenLog = path.join(root, "forbidden.log");
    fs.mkdirSync(sharedTemp);
    fs.mkdirSync(bin);
    fs.mkdirSync(cacheBin, { recursive: true });
    fs.writeFileSync(
      path.join(cacheBin, "copilot"),
      `#!/bin/sh
touch "$INSTALLER_READY"
while [ ! -e "$INSTALLER_RELEASE" ]; do sleep 0.05; done
printf 'copilot 1.0.103\\n'
`,
      { mode: 0o755 }
    );
    for (const command of ["sudo", "chown", "curl"]) {
      fs.writeFileSync(path.join(bin, command), '#!/bin/sh\necho forbidden >> "$FORBIDDEN_LOG"\nexit 99\n', { mode: 0o755 });
    }
    fs.writeFileSync(path.join(bin, "uname"), '#!/bin/sh\ncase "$1" in -m) echo x86_64 ;; *) echo Linux ;; esac\n', { mode: 0o755 });
    const otherChroot = path.join(sharedTemp, "awf-existing-chroot-home");
    fs.mkdirSync(otherChroot);
    fs.writeFileSync(path.join(otherChroot, "artifact"), "another invocation");
    const installer = path.resolve(__dirname, "../sh/install_copilot_cli.sh");
    const jobs = [0, 1].map(index => {
      const workdir = path.join(root, `runner-${index}`);
      const ready = path.join(workdir, "ready");
      const release = path.join(workdir, "release");
      fs.mkdirSync(workdir);
      const child = spawn("bash", [installer, "--rootless", "1.0.103"], {
        cwd: workdir,
        env: environment(sharedTemp, {
          HOME: path.join(workdir, "home"),
          TMPDIR: sharedTemp,
          RUNNER_TOOL_CACHE: cache,
          GITHUB_PATH: "",
          PATH: `${bin}:${process.env.PATH}`,
          INSTALLER_READY: ready,
          INSTALLER_RELEASE: release,
          FORBIDDEN_LOG: forbiddenLog,
        }),
        stdio: ["ignore", "pipe", "pipe"],
      });
      children.push(child);
      let output = "";
      child.stdout.on("data", chunk => {
        output += chunk;
      });
      child.stderr.on("data", chunk => {
        output += chunk;
      });
      const completion = new Promise((resolve, reject) => {
        child.once("error", reject);
        child.once("close", code => resolve({ code, output }));
      });
      return { child, ready, release, completion };
    });
    try {
      const deadline = Date.now() + 10000;
      while (!jobs.every(job => fs.existsSync(job.ready))) {
        if (Date.now() > deadline) throw new Error("Concurrent installers did not reach the toolcache barrier");
        await new Promise(resolve => setTimeout(resolve, 25));
      }
      const stagingNames = () => fs.readdirSync(sharedTemp).filter(name => name.startsWith("copilot-install-"));
      expect(stagingNames()).toHaveLength(2);
      for (const name of stagingNames()) {
        const dir = path.join(sharedTemp, name);
        expect(fs.statSync(dir).mode & 0o777).toBe(0o700);
        const chroot = path.join(dir, "awf-own-chroot-home");
        fs.mkdirSync(chroot);
        fs.writeFileSync(path.join(chroot, "artifact"), name);
      }
      fs.writeFileSync(jobs[0].release, "");
      const first = await jobs[0].completion;
      expect(first.code, first.output).toBe(0);
      expect(stagingNames()).toHaveLength(1);
      const remaining = stagingNames()[0];
      expect(fs.readFileSync(path.join(sharedTemp, remaining, "awf-own-chroot-home/artifact"), "utf8")).toBe(remaining);
      expect(fs.readFileSync(path.join(otherChroot, "artifact"), "utf8")).toBe("another invocation");
      fs.writeFileSync(jobs[1].release, "");
      const second = await jobs[1].completion;
      expect(second.code, second.output).toBe(0);
      expect(stagingNames()).toHaveLength(0);
      expect(fs.readFileSync(path.join(otherChroot, "artifact"), "utf8")).toBe("another invocation");
      expect(fs.existsSync(forbiddenLog)).toBe(false);
    } finally {
      for (const job of jobs) fs.writeFileSync(job.release, "");
      await Promise.all(jobs.map(job => job.completion));
    }
  });

  it.skipIf(process.getuid && process.getuid() === 0)("rejects an unwritable Copilot directory without reclaiming or re-owning it", () => {
    const root = workspace();
    const home = path.join(root, "home");
    const copilot = path.join(home, ".copilot");
    const bin = path.join(root, "bin");
    const log = path.join(root, "forbidden.log");
    fs.mkdirSync(copilot, { recursive: true });
    fs.mkdirSync(bin);
    for (const command of ["sudo", "chown"]) {
      fs.writeFileSync(path.join(bin, command), '#!/bin/sh\necho forbidden >> "$FORBIDDEN_LOG"\nexit 99\n', { mode: 0o755 });
    }
    fs.chmodSync(copilot, 0o555);
    try {
      const result = spawnSync("bash", [path.resolve(__dirname, "../sh/install_copilot_cli.sh"), "--rootless", "1.0.103"], {
        cwd: root,
        env: environment(root, { HOME: home, PATH: `${bin}:${process.env.PATH}`, FORBIDDEN_LOG: log }),
        encoding: "utf8",
      });
      expect(result.status).toBe(1);
      expect(result.stderr).toContain("--rootless requires a writable Copilot directory");
      expect(result.stderr).toContain("existing files will not be reclaimed or re-owned");
      expect(fs.statSync(copilot).mode & 0o777).toBe(0o555);
      expect(fs.existsSync(log)).toBe(false);
      expect(fs.readdirSync(root).filter(name => name.startsWith("copilot-install-"))).toHaveLength(0);
    } finally {
      fs.chmodSync(copilot, 0o700);
    }
  });

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

  it.skipIf(!dockerImage || !process.getuid || process.getuid() === 0)(
    "isolates concurrent Docker-written leftovers and warns without sudo on cleanup",
    async () => {
      const root = workspace();
      const jobs = await Promise.all([allocate(root), allocate(root)]);
      const containerIDs = [];
      const docker = (...args) => {
        const result = spawnSync("docker", args, { encoding: "utf8", timeout: 30000 });
        expect(result.status, result.stderr || String(result.error || "")).toBe(0);
        return result.stdout.trim();
      };
      const mount = dir => ["--mount", `type=bind,source=${dir},target=/staging`];
      const common = ["--network", "none", "--user", "0", "--entrypoint", "/bin/sh"];
      try {
        for (const job of jobs) {
          const id = docker(
            "create",
            "--pull",
            "never",
            ...common,
            ...mount(job.dir),
            dockerImage,
            "-c",
            "set -eu; mkdir -m 700 /staging/firewall; echo owned > /staging/firewall/artifact; touch /staging/ready; while [ ! -e /staging/finish ]; do sleep 0.1; done"
          );
          expect(id).toMatch(/^[a-f0-9]{64}$/);
          containerIDs.push(id);
        }
        docker("start", ...containerIDs);
        const deadline = Date.now() + 10000;
        while (!jobs.every(job => fs.existsSync(path.join(job.dir, "ready")))) {
          if (Date.now() > deadline) throw new Error("Docker ownership fixtures did not become ready");
          await new Promise(resolve => setTimeout(resolve, 50));
        }
        expect(docker("inspect", "--format", "{{.State.Running}}", ...containerIDs).split("\n")).toEqual(["true", "true"]);
        for (const job of jobs) {
          expect(fs.statSync(path.join(job.dir, "firewall")).uid).toBe(0);
          expect(fs.statSync(path.join(job.dir, "firewall")).mode & 0o777).toBe(0o700);
          fs.writeFileSync(path.join(job.dir, "finish"), "");
        }
        expect(docker("wait", ...containerIDs).split("\n")).toEqual(["0", "0"]);
        docker("rm", ...containerIDs);
        containerIDs.length = 0;

        const failed = await jobs[0].finish(7);
        expect(failed.code).toBe(7);
        expect(failed.stderr).toContain("unable to reclaim owned installer staging directory without elevated permissions");
        expect(fs.existsSync(path.join(jobs[1].dir, "firewall"))).toBe(true);
        const finished = await jobs[1].finish();
        expect(finished.code).toBe(0);
        expect(finished.stderr).toContain("unable to reclaim owned installer staging directory without elevated permissions");

        const retry = await allocate(root, { GITHUB_RUN_ATTEMPT: "2" });
        expect(jobs.map(job => job.dir)).not.toContain(retry.dir);
        await retry.finish();
        expect(jobs.every(job => fs.existsSync(path.join(job.dir, "firewall")))).toBe(true);
        // Exercise partial fixture creation: one allocation has no firewall to restore.
        docker("run", "--rm", "--pull", "never", ...common, ...mount(jobs[0].dir), dockerImage, "-c", "rm -rf /staging/firewall");
      } finally {
        // Restore permissions only on these test-created bind-mount fixtures.
        // Production cleanup deliberately never escalates to reclaim such files.
        finishFixtureCleanup([
          ...containerIDs.map(id => () => docker("rm", "-f", id)),
          ...jobs.map(job => () => {
            if (!fs.existsSync(job.dir)) return;
            docker("run", "--rm", "--pull", "never", ...common, ...mount(job.dir), dockerImage, "-c", "if [ -d /staging/firewall ]; then chmod -R a+rwX /staging/firewall; fi");
          }),
        ]);
      }
    },
    60000
  );
});
