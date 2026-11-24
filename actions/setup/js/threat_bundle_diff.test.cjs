const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { execFileSync } = require("node:child_process");
const { readBundleDiff } = require("./threat_bundle_diff.cjs");

it("renders only the new commits in an incremental bundle without modifying the base checkout", () => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "bundle-review-"));
  const git = (...args) =>
    execFileSync("git", ["-c", "user.name=fixture", "-c", "user.email=test@example.org", ...args], {
      cwd: root,
      encoding: "utf8",
      env: { PATH: process.env.PATH, HOME: root, GIT_CONFIG_NOSYSTEM: "1", GIT_CONFIG_GLOBAL: "/dev/null" },
    }).trim();
  try {
    git("init", "-q", "--template=");
    fs.writeFileSync(path.join(root, "doc"), "base\n");
    git("add", "doc");
    git("commit", "-qm", "old commit");
    const base = git("rev-parse", "HEAD");
    const checkout = path.join(root, "base-checkout");
    git("clone", "-q", "--local", "--no-hardlinks", root, checkout);
    fs.appendFileSync(path.join(root, "doc"), "addition\n");
    git("commit", "-qam", "new commit");
    const file = path.join(root, "aw-change.bundle");
    git("bundle", "create", file, `${base}..HEAD`);
    git("checkout", "--detach", "-q", base);
    // A local Git command would try to parse this broken repository config.
    fs.writeFileSync(path.join(checkout, ".git/config"), "[broken\n");
    const diff = readBundleDiff(file, checkout);
    expect(diff).toContain("+addition");
    expect(diff).toContain("new commit");
    expect(diff).not.toContain("old commit");
    expect(git("rev-parse", "HEAD")).toBe(base);
    expect(() => readBundleDiff(file, "")).toThrow(/local base/);
  } finally {
    fs.rmSync(root, { force: true, recursive: true });
  }
});
