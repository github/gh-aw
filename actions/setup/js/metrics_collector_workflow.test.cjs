import { afterEach, describe, expect, it, vi } from "vitest";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import vm from "node:vm";
import { execFileSync } from "node:child_process";
import { createRequire } from "node:module";
import { getPatchDiffSizeBytes } from "./git_patch_utils.cjs";
import { validateMemoryStep, validateRepoMemoryBaseline } from "./validate_memory_step.cjs";
import { getRepoMemoryBaselinePath } from "./memory_custom_validation.cjs";

const require = createRequire(import.meta.url);
const repoRoot = path.resolve(import.meta.dirname, "../../..");
const source = fs.readFileSync(path.join(repoRoot, ".github/workflows/metrics-collector.md"), "utf8");
const frontmatter = source.split("---")[1];
const maxFileBytes = Number(frontmatter.match(/max-file-size: (\d+)/)[1]);
const maxPatchBytes = Number(frontmatter.match(/max-patch-size: (\d+)/)[1]);
const gate = source.match(/    validation:\n      script: \|\n([\s\S]*?)\n  - id: lifecycle/)[1].replace(/^        /gm, "");
const prune = source.match(/node <<'NODE'\n([\s\S]*?)\nNODE/)[1];
const example = JSON.parse(source.match(/Create a JSON object following this schema:\n\n```json\n([\s\S]*?)\n```/)[1]);
const tempDirs = [];
const migratedSnapshotBytes = 460000;

function temporaryDirectory() {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "metrics-contract-"));
  tempDirs.push(dir);
  return dir;
}

function snapshot(bytes = migratedSnapshotBytes, timestamp = "2026-10-14T02:00:00Z") {
  const data = {
    ...example,
    timestamp,
    workflows: Object.fromEntries(Array.from({ length: 340 }, (_, index) => [`workflow-${index}`, example.workflows["workflow-name"]])),
    ecosystem: { ...example.ecosystem, total_workflows: 340 },
    collection_note: "",
  };
  const initialBytes = Buffer.byteLength(JSON.stringify(data, null, 2) + "\n");
  if (initialBytes > bytes) throw new Error(`Fixture schema is already ${initialBytes} bytes, exceeding requested ${bytes}`);
  data.collection_note = "x".repeat(bytes - initialBytes);
  return JSON.stringify(data, null, 2) + "\n";
}

function writeSnapshot(root, content = snapshot()) {
  fs.mkdirSync(path.join(root, "daily"), { recursive: true });
  fs.writeFileSync(path.join(root, "latest.json"), content);
  const day = JSON.parse(content).timestamp.slice(0, 10);
  fs.writeFileSync(path.join(root, "daily", `${day}.json`), content);
  return content;
}

function snapshotWithMutation(mutate) {
  const root = temporaryDirectory();
  const data = JSON.parse(snapshot());
  mutate(data);
  writeSnapshot(root, JSON.stringify(data, null, 2) + "\n");
  return root;
}

function runGate(root, script = gate, phase = "upload") {
  class FixtureDate extends Date {
    static now() {
      return Date.parse("2026-10-14T03:00:00Z");
    }
  }
  vm.runInNewContext(`(function () { ${script.replace('path.join(memoryRoot, "metrics")', JSON.stringify(root))} })()`, {
    fs,
    path,
    Buffer,
    memoryPhase: phase,
    Date: FixtureDate,
  });
}

function runPrune(root) {
  vm.runInNewContext(prune.replace('"/tmp/gh-aw/repo-memory/default/metrics"', JSON.stringify(root)), { require });
}

function git(root, args) {
  return execFileSync("git", args, { cwd: root, encoding: "utf8", stdio: "pipe" });
}

function initializeRepository() {
  const root = temporaryDirectory();
  git(root, ["init", "--quiet"]);
  git(root, ["config", "user.name", "Metrics test"]);
  git(root, ["config", "user.email", "metrics-test@example.invalid"]);
  git(root, ["config", "core.hooksPath", "/dev/null"]);
  git(root, ["remote", "add", "origin", path.join(root, "unused-local-remote")]);
  git(root, ["commit", "--quiet", "--allow-empty", "-m", "Fixture baseline"]);
  return root;
}

afterEach(() => {
  for (const dir of tempDirs.splice(0)) fs.rmSync(dir, { recursive: true, force: true });
  vi.unstubAllEnvs();
});

describe("Metrics Collector serialized storage contract", () => {
  it("keeps the gate budgets consistent with repo-memory configuration", () => {
    expect(Number(gate.match(/maxFileBytes = (\d+)/)[1])).toBe(maxFileBytes);
    expect(Number(gate.match(/maxPatchBytes = (\d+)/)[1])).toBe(maxPatchBytes);
    expect(maxPatchBytes).toBeLessThanOrEqual(1048576);
    expect(frontmatter).toContain('file-glob: "metrics/**"');
  });

  it("emits the same validator and budgets before upload and in the push job", () => {
    const lock = fs.readFileSync(path.join(repoRoot, ".github/workflows/metrics-collector.lock.yml"), "utf8");
    const scripts = [...lock.matchAll(/VALIDATION_SCRIPT_B64: ([A-Za-z0-9+/=]+)/g)];
    expect(scripts.length).toBeGreaterThanOrEqual(3);
    for (const [, encoded] of scripts) expect(Buffer.from(encoded, "base64").toString("utf8").trim()).toBe(gate.trim());
    expect(Number(lock.match(/MAX_FILE_SIZE: (\d+)/)[1])).toBe(maxFileBytes);
    expect(Number(lock.match(/MAX_PATCH_SIZE: (\d+)/)[1])).toBe(maxPatchBytes);
    const countLimit = Number(lock.match(/MAX_FILE_COUNT: (\d+)/)[1]);
    expect(Number(gate.match(/maxArtifactFiles = (\d+)/)[1])).toBe(countLimit);
    expect(lock.indexOf("Validate repo-memory domain content (default)")).toBeLessThan(lock.indexOf("Upload repo-memory artifact (default)"));
    const upload = lock.match(/- name: Upload repo-memory artifact \(default\)\n([\s\S]*?)\n      - name:/)[1];
    expect(upload).toContain("steps.validate_repo_memory_64656661756c74.outcome == 'success'");
    expect(lock).toContain("ALLOWED_EXTENSIONS: '[\".json\"]'");
    expect(lock).toContain('FILE_GLOB_FILTER: "metrics/**"');
  });

  it("fits the complete migrated 340-workflow schema without dropping fields", () => {
    const root = temporaryDirectory();
    const content = writeSnapshot(root);
    expect(Buffer.byteLength(content)).toBe(migratedSnapshotBytes);
    expect(Buffer.byteLength(content)).toBeGreaterThan(102400);
    expect(Buffer.byteLength(JSON.stringify(JSON.parse(content)))).toBeGreaterThan(131072);
    expect(Buffer.byteLength(content)).toBeLessThan(maxFileBytes);
    expect(Object.keys(JSON.parse(content).workflows)).toHaveLength(340);
    runGate(root);
    expect(fs.readFileSync(path.join(root, "latest.json"), "utf8")).toBe(content);
    expect(fs.readFileSync(path.join(root, "daily/2026-10-14.json"), "utf8")).toBe(content);
  });

  it("rejects workflow totals that do not match outcome counts", () => {
    const root = snapshotWithMutation(data => data.workflows["workflow-0"].workflow_runs.total++);
    expect(() => runGate(root)).toThrow("Workflow run total does not match outcome counts: workflow-0.");
  });

  it.each([
    [
      "negative counts",
      runs => {
        runs.failed = -1;
      },
    ],
    [
      "fractional counts",
      runs => {
        runs.skipped = 0.5;
      },
    ],
    [
      "null counts",
      runs => {
        runs.action_required = null;
      },
    ],
  ])("rejects %s", (_, mutate) => {
    const root = snapshotWithMutation(data => mutate(data.workflows["workflow-0"].workflow_runs));
    expect(() => runGate(root)).toThrow("Workflow run counts must be non-negative integers: workflow-0.");
  });

  it.each([
    [
      "a null workflow",
      data => {
        data.workflows["workflow-0"] = null;
      },
    ],
    [
      "null workflow runs",
      data => {
        data.workflows["workflow-0"].workflow_runs = null;
      },
    ],
    [
      "missing workflow runs",
      data => {
        delete data.workflows["workflow-0"].workflow_runs;
      },
    ],
  ])("rejects %s with a clear count validation error", (_, mutate) => {
    const root = snapshotWithMutation(mutate);
    expect(() => runGate(root)).toThrow("Workflow run counts must be non-negative integers: workflow-0.");
  });

  it.each([
    [
      "executed count",
      runs => {
        runs.executed++;
      },
    ],
    [
      "success rate offset by 0.0009",
      runs => {
        runs.success_rate += 0.0009;
      },
    ],
    [
      "success rate offset by 0.001",
      runs => {
        runs.success_rate += 0.001;
      },
    ],
  ])("rejects an inconsistent %s", (_, mutate) => {
    const root = snapshotWithMutation(data => mutate(data.workflows["workflow-0"].workflow_runs));
    expect(() => runGate(root)).toThrow("Workflow executed count or success rate is inconsistent: workflow-0.");
  });

  it("accepts zero executions only with a null success rate", () => {
    const root = snapshotWithMutation(data => {
      Object.assign(data.workflows["workflow-0"].workflow_runs, {
        total: 0,
        successful: 0,
        failed: 0,
        skipped: 0,
        action_required: 0,
        executed: 0,
        success_rate: null,
      });
    });
    expect(() => runGate(root)).not.toThrow();
  });

  it("rejects a numeric success rate when there were zero executions", () => {
    const root = snapshotWithMutation(data => {
      Object.assign(data.workflows["workflow-0"].workflow_runs, {
        total: 0,
        successful: 0,
        failed: 0,
        skipped: 0,
        action_required: 0,
        executed: 0,
        success_rate: 0,
      });
    });
    expect(() => runGate(root)).toThrow("Workflow executed count or success rate is inconsistent: workflow-0.");
  });

  it("requires a partial status and note for unclassified conclusions", () => {
    const validRoot = snapshotWithMutation(data => {
      data.collection_status = "partial";
      data.collection_note = "workflow-0 (neutral: 1)";
      data.unclassified_conclusions = [{ workflow: "workflow-0", conclusion: "neutral", count: 1 }];
    });
    expect(() => runGate(validRoot)).not.toThrow();

    const invalidRoot = snapshotWithMutation(data => {
      data.unclassified_conclusions = [{ workflow: "workflow-0", conclusion: "neutral", count: 1 }];
    });
    expect(() => runGate(invalidRoot)).toThrow("Unclassified conclusions require partial collection status.");

    const missingNoteRoot = snapshotWithMutation(data => {
      data.collection_status = "partial";
      data.collection_note = "";
      data.unclassified_conclusions = [{ workflow: "workflow-0", conclusion: "neutral", count: 1 }];
    });
    expect(() => runGate(missingNoteRoot)).toThrow("Partial metrics must include a collection note.");

    const vagueNoteRoot = snapshotWithMutation(data => {
      data.collection_status = "partial";
      data.collection_note = "There was an unclassified conclusion.";
      data.unclassified_conclusions = [{ workflow: "workflow-0", conclusion: "neutral", count: 1 }];
    });
    expect(() => runGate(vagueNoteRoot)).toThrow("Collection note must identify each unclassified conclusion and count.");
  });

  it("classifies startup_failure as a failed completed run", () => {
    expect(source).toContain('Failed runs (conclusion: "failure", "cancelled", "timed_out", "startup_failure")');
    expect(source).toContain("`startup_failure` as failed because it is a completed run that did not succeed.");
  });

  it.each(["bootstrap", "next-day", "both-replaced"])("measures the actual %s staged diff, including prefixes and headers", mode => {
    const root = initializeRepository();
    const metrics = path.join(root, "metrics");
    if (mode !== "bootstrap") {
      writeSnapshot(metrics, snapshot(migratedSnapshotBytes, mode === "next-day" ? "2026-10-13T02:00:00Z" : "2026-10-14T01:00:00Z"));
      git(root, ["add", "."]);
      git(root, ["commit", "--quiet", "-m", "Previous metrics"]);
    }
    const content = writeSnapshot(metrics, snapshot(480000));
    runGate(metrics);
    git(root, ["add", "."]);
    const diff = git(root, ["diff", "--cached", "--no-renames"]);
    const enforcedBytes = getPatchDiffSizeBytes(diff);
    expect(enforcedBytes).toBeGreaterThan(0);
    expect(enforcedBytes).toBeLessThan(maxPatchBytes);
    // Headers/context are not charged by the runtime's net-byte helper, but still fit.
    expect(Buffer.byteLength(diff)).toBeLessThan(maxPatchBytes);
    if (mode === "bootstrap") {
      expect(enforcedBytes).toBeGreaterThan(2 * Buffer.byteLength(content));
      expect(enforcedBytes).toBeGreaterThan(131072 * 1.2);
    }
  });

  it("fails closed at the file cap without altering oversized data", () => {
    const root = temporaryDirectory();
    const content = writeSnapshot(root, snapshot(maxFileBytes + 1));
    expect(() => runGate(root)).toThrow("exceeds the file or two-copy patch budget");
    expect(fs.readFileSync(path.join(root, "latest.json"), "utf8")).toBe(content);
  });

  it("fails closed when line-prefix overhead exceeds the two-copy patch cap", () => {
    const root = temporaryDirectory();
    const content = writeSnapshot(root);
    const fileBytes = Buffer.byteLength(content);
    const tightBudget = 2 * fileBytes;
    expect(() => runGate(root, gate.replace(`maxPatchBytes = ${maxPatchBytes}`, `maxPatchBytes = ${tightBudget}`))).toThrow("two-copy patch budget");
  });

  it.each(["missing", "different", "invalid-json"])("rejects a %s daily/latest pair", mode => {
    const root = temporaryDirectory();
    writeSnapshot(root);
    if (mode === "missing") fs.unlinkSync(path.join(root, "daily/2026-10-14.json"));
    if (mode === "different") fs.writeFileSync(path.join(root, "daily/2026-10-14.json"), "{}");
    if (mode === "invalid-json") fs.writeFileSync(path.join(root, "latest.json"), "{");
    expect(() => runGate(root)).toThrow();
  });

  it("bounds the artifact to 30 dated snapshots plus latest regardless of checkout mtimes, not backing-branch history", () => {
    const artifact = temporaryDirectory();
    const branch = temporaryDirectory();
    const content = writeSnapshot(artifact);
    fs.mkdirSync(path.join(branch, "daily"));
    for (let offset = 0; offset < 45; offset++) {
      const date = new Date("2026-10-14T00:00:00Z");
      date.setUTCDate(date.getUTCDate() - offset);
      const filename = `${date.toISOString().slice(0, 10)}.json`;
      // Every checkout file has a fresh mtime, including historical snapshots.
      fs.writeFileSync(path.join(artifact, "daily", filename), content);
      fs.writeFileSync(path.join(branch, "daily", filename), content);
    }
    expect(() => runGate(artifact)).toThrow("30-day upload window");
    runPrune(artifact);
    runGate(artifact);
    const dailyFiles = fs.readdirSync(path.join(artifact, "daily")).sort();
    expect(dailyFiles).toHaveLength(30);
    expect(dailyFiles[0]).toBe("2026-09-15.json");
    expect(fs.readdirSync(path.join(branch, "daily"))).toHaveLength(45);
    const files = [path.join(artifact, "latest.json"), ...dailyFiles.map(name => path.join(artifact, "daily", name))];
    expect(files).toHaveLength(31);
    expect(files.length).toBeLessThan(100); // Default changed-file limit.
    const artifactBytes = files.reduce((total, file) => total + fs.statSync(file).size, 0);
    expect(artifactBytes).toBe(31 * migratedSnapshotBytes);
    expect(artifactBytes).toBeLessThanOrEqual(31 * maxFileBytes);
  });

  it("allows omitted backing-branch history only in the push phase", () => {
    const root = temporaryDirectory();
    writeSnapshot(root);
    fs.writeFileSync(path.join(root, "daily/2026-01-01.json"), "{}");
    expect(() => runGate(root)).toThrow("30-day upload window");
    expect(() => runGate(root, gate, "baseline")).toThrow("30-day upload window");
    runGate(root, gate, "push");
    expect(fs.existsSync(path.join(root, "daily/2026-01-01.json"))).toBe(true);
    fs.writeFileSync(path.join(root, "daily/2026-10-14.json"), "{}");
    expect(() => runGate(root, gate, "push")).toThrow("exactly match");
  });

  it.each(["2026-09-99.json", "2026-09-31.json"])("rejects impossible calendar dates: %s", name => {
    const root = temporaryDirectory();
    writeSnapshot(root);
    fs.writeFileSync(path.join(root, "daily", name), "{}");
    expect(() => runGate(root)).toThrow("valid calendar dates");
  });

  it.each(["json", "JSON", "JsOn"])("rejects an oversized retained .%s file before upload", extension => {
    const root = temporaryDirectory();
    writeSnapshot(root);
    fs.writeFileSync(path.join(root, `retained.${extension}`), snapshot(maxFileBytes + 1, "2026-10-13T02:00:00Z"));
    expect(() => runGate(root)).toThrow("Retained metrics JSON");
  });

  it.each([
    ["bytes", "json"],
    ["bytes", "JSON"],
    ["bytes", "JsOn"],
    ["files", "json"],
    ["files", "JSON"],
    ["files", "JsOn"],
  ])("enforces the complete artifact %s cap, including nested supplemental .%s", (budget, extension) => {
    const root = temporaryDirectory();
    writeSnapshot(root);
    fs.mkdirSync(path.join(root, "extra"));
    const count = budget === "bytes" ? 40 : 100;
    for (let index = 0; index < count; index++) {
      fs.writeFileSync(path.join(root, "extra", `${index}.${extension}`), budget === "bytes" ? snapshot() : "{}");
    }
    expect(() => runGate(root)).toThrow("16MiB or 100-file artifact budget");
  });

  it.each(["JSON", "JsOn"])("rejects alternate-case dated .%s filenames", extension => {
    const root = temporaryDirectory();
    writeSnapshot(root);
    fs.writeFileSync(path.join(root, `daily/2026-10-13.${extension}`), "{}");
    expect(() => runGate(root)).toThrow("lowercase .json");
  });

  it("preserves supplemental JSON rather than manufacturing success by dropping data", () => {
    const root = temporaryDirectory();
    writeSnapshot(root);
    fs.writeFileSync(path.join(root, "daily/summary.json"), '{"note":"supplemental data"}\n');
    runGate(root);
    expect(fs.readFileSync(path.join(root, "daily/summary.json"), "utf8")).toBe('{"note":"supplemental data"}\n');
  });

  it("rejects a future dated snapshot", () => {
    const root = temporaryDirectory();
    writeSnapshot(root);
    fs.writeFileSync(path.join(root, "daily/2026-10-15.json"), "{}");
    expect(() => runGate(root)).toThrow();
  });

  it.each(["upload", "baseline"])("wires the actual %s validation entry point and fails closed before upload", phase => {
    const root = temporaryDirectory();
    writeSnapshot(path.join(root, "metrics"), snapshot(migratedSnapshotBytes, new Date().toISOString()));
    const memoryId = path.basename(root);
    const script = `if (memoryPhase !== ${JSON.stringify(phase)}) throw new Error("incorrect entry-point phase");\n${gate}`;
    vi.stubEnv("MEMORY_DIR", root);
    vi.stubEnv("MEMORY_ID", memoryId);
    vi.stubEnv("ALLOWED_EXTENSIONS", '[".json"]');
    vi.stubEnv("FILE_GLOB_FILTER", "metrics/**");
    vi.stubEnv("VALIDATION_SCRIPT_B64", Buffer.from(script).toString("base64"));
    const core = { info: vi.fn(), warning: vi.fn(), setFailed: vi.fn() };
    const baselinePath = getRepoMemoryBaselinePath(memoryId);
    try {
      if (phase === "upload") {
        expect(validateMemoryStep(core, { kind: "repo" })).toBe(true);
        fs.writeFileSync(path.join(root, "metrics/daily/2020-01-01.json"), "{}");
        expect(validateMemoryStep(core, { kind: "repo" })).toBe(false);
        expect(core.setFailed).toHaveBeenCalled();
      } else {
        validateRepoMemoryBaseline(core);
        expect(JSON.parse(fs.readFileSync(baselinePath, "utf8")).ok).toBe(true);
        fs.writeFileSync(path.join(root, "metrics/daily/2020-01-01.json"), "{}");
        validateRepoMemoryBaseline(core);
        expect(JSON.parse(fs.readFileSync(baselinePath, "utf8")).ok).toBe(false);
        expect(core.warning).toHaveBeenCalled();
      }
    } finally {
      fs.rmSync(baselinePath, { force: true });
      fs.rmSync(baselinePath.replace(/\.json$/, ".log"), { force: true });
    }
  });

  it.each(["current", "old-file-cap", "old-patch-cap", "invalid-pair"])("exercises the real push validator with %s budgets and mocked external effects", async mode => {
    const workspace = initializeRepository();
    const artifact = temporaryDirectory();
    const current = snapshot(migratedSnapshotBytes, new Date().toISOString());
    writeSnapshot(path.join(artifact, "metrics"), current);
    fs.mkdirSync(path.join(workspace, "metrics/daily"), { recursive: true });
    fs.writeFileSync(path.join(workspace, "metrics/daily/2020-01-01.json"), "{}");
    git(workspace, ["add", "."]);
    git(workspace, ["commit", "--quiet", "-m", "Backing branch history"]);
    if (mode === "invalid-pair") {
      const day = JSON.parse(current).timestamp.slice(0, 10);
      fs.writeFileSync(path.join(artifact, "metrics/daily", `${day}.json`), "{}");
    }
    const core = { info: vi.fn(), warning: vi.fn(), error: vi.fn(), setFailed: vi.fn(), setOutput: vi.fn() };
    const pushSignedCommits = vi.fn().mockResolvedValue(undefined);
    const execGitSync = (args, options = {}) => {
      if (args[0] === "fetch" || args[0] === "checkout") return "";
      return git(options.cwd || workspace, args);
    };
    const mocks = { "./git_helpers.cjs": { execGitSync }, "./push_signed_commits.cjs": { pushSignedCommits } };
    const module = { exports: {} };
    vm.runInNewContext(fs.readFileSync(path.join(import.meta.dirname, "push_repo_memory.cjs"), "utf8"), {
      require: id => mocks[id] || require(id),
      module,
      core,
      context: { repo: { owner: "owner", repo: "repo" } },
      github: {},
      exec: {},
      process: {
        env: {
          ARTIFACT_DIR: artifact,
          GITHUB_WORKSPACE: workspace,
          MEMORY_ID: "default",
          TARGET_REPO: "owner/repo",
          BRANCH_NAME: "memory/meta-orchestrators",
          GH_TOKEN: "fixture-token",
          FILE_GLOB_FILTER: "metrics/**",
          ALLOWED_EXTENSIONS: '[".json"]',
          MAX_FILE_SIZE: String(mode === "old-file-cap" ? 102400 : maxFileBytes),
          MAX_PATCH_SIZE: String(mode === "old-patch-cap" ? 131072 : maxPatchBytes),
          VALIDATION_SCRIPT_B64: Buffer.from(gate).toString("base64"),
        },
      },
    });
    await module.exports.main();
    if (mode === "current") {
      expect(core.setFailed).not.toHaveBeenCalled();
      expect(pushSignedCommits).toHaveBeenCalledOnce();
      expect(fs.readFileSync(path.join(workspace, "metrics/latest.json"), "utf8")).toBe(current);
      expect(fs.existsSync(path.join(workspace, "metrics/daily/2020-01-01.json"))).toBe(true);
    } else {
      expect(core.setFailed).toHaveBeenCalled();
      expect(pushSignedCommits).not.toHaveBeenCalled();
    }
  });
});
