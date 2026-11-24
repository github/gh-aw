// Inspect a staged bundle offline; never check out its contents, run hooks,
// invoke diff drivers, or expose a general-purpose Git command to the model.
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { spawnSync } = require("node:child_process");

const maxBundle = 8 * 1024 * 1024;
const maxDiff = 128 * 1024;
const oid = /^[a-f0-9]{40}$/;

function readBundleDiff(file, workspace = process.env.GITHUB_WORKSPACE) {
  const fd = fs.openSync(file, fs.constants.O_RDONLY | fs.constants.O_NONBLOCK | fs.constants.O_NOFOLLOW);
  let data;
  try {
    const stat = fs.fstatSync(fd);
    if (!stat.isFile() || stat.size > maxBundle) throw new Error("Bundle exceeds the inspection limit or is not a regular file");
    data = Buffer.alloc(stat.size);
    let length = 0;
    while (length < data.length) {
      const n = fs.readSync(fd, data, length, data.length - length, length);
      if (!n) throw new Error("Bundle changed during inspection");
      length += n;
    }
  } finally {
    fs.closeSync(fd);
  }
  const end = data.subarray(0, 65536).indexOf("\n\n");
  if (end < 0) throw new Error("Invalid or oversized bundle header");
  const [version, ...refs] = data.toString("utf8", 0, end).split("\n");
  if (!["# v2 git bundle", "# v3 git bundle"].includes(version) || refs.length > 64) throw new Error("Unsupported bundle header");
  const tips = new Set();
  const bases = new Set();
  for (const ref of refs) {
    if (ref === "@object-format=sha1") continue;
    const token = ref.split(" ", 1)[0];
    const sha = token.replace(/^-/, "");
    if (!oid.test(sha)) throw new Error("Unsupported bundle reference");
    (token.startsWith("-") ? bases : tips).add(sha);
  }
  if (!tips.size) throw new Error("Bundle has no heads to inspect");
  const repo = fs.mkdtempSync(path.join(os.tmpdir(), "threat-bundle-"));
  const env = { PATH: process.env.PATH, HOME: repo, GIT_CONFIG_NOSYSTEM: "1", GIT_CONFIG_GLOBAL: "/dev/null", GIT_TERMINAL_PROMPT: "0", GIT_ALLOW_PROTOCOL: "", GIT_NO_LAZY_FETCH: "1", GIT_NO_REPLACE_OBJECTS: "1" };
  function git(args, cwd = repo, input = undefined) {
    const child = spawnSync("git", ["-c", "core.hooksPath=/dev/null", "-c", "gc.auto=0", ...args], { cwd, env, input, encoding: "utf8", timeout: 3000, killSignal: "SIGKILL", maxBuffer: maxDiff });
    if (child.status !== 0 || child.error) throw new Error("Offline bundle inspection failed (missing prerequisite, invalid data, or resource limit)");
    return child.stdout;
  }
  try {
    git(["init", "--bare", "--quiet", "--template="]);
    if (bases.size) {
      if (!workspace) throw new Error("Incremental bundle requires a local base checkout");
      const objects = git(["rev-parse", "--path-format=absolute", "--git-path", "objects"], workspace).trimEnd();
      // Git reads prerequisites from this store; it writes new objects only into the disposable repo.
      if (!path.isAbsolute(objects) || /[\r\n]/.test(objects)) throw new Error("Invalid local object directory");
      fs.writeFileSync(path.join(repo, "objects/info/alternates"), `${objects}\n`, { flag: "wx" });
    }
    git(["index-pack", "--stdin", "--fix-thin", "--strict", `--max-input-size=${maxBundle}`], repo, data.subarray(end + 2));
    return git(["log", "--no-color", "--no-ext-diff", "--no-textconv", "--no-renames", "--root", "--diff-merges=first-parent", "--format=commit %H%n%s", "-p", ...tips, "--not", ...bases, "--"]);
  } finally {
    fs.rmSync(repo, { recursive: true, force: true });
  }
}

module.exports = { readBundleDiff };
