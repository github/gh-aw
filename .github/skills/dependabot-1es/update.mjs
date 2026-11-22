import { execFileSync } from "node:child_process";
import { mkdtempSync, mkdirSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { isDeepStrictEqual, parseArgs } from "node:util";

const repo = "github/gh-aw";
const base = "main";
const registry = "https://ms-feed-25.pkgs.visualstudio.com/1es-public/_packaging/npm-public/npm/registry/";
const projects = ["docs", "actions/setup/js"];
const pathFor = (project, file) => `${project}/${file}`;
const lock = "package-lock.json";
const manifest = "package.json";
const allowedPaths = projects.flatMap(project => [lock, manifest].map(file => pathFor(project, file)));
const sha = /^[0-9a-f]{40}$/;
const packageName = /^(?:@[a-z0-9][a-z0-9._-]*\/)?[a-z0-9][a-z0-9._-]*$/i;
const version = /^\d+\.\d+\.\d+(?:-[0-9a-z.-]+)?(?:\+[0-9a-z.-]+)?$/i;

class Blocked extends Error {}
class FeedUnavailable extends Error {}

function run(command, args, options = {}) {
  return execFileSync(command, args, {
    encoding: "utf8",
    maxBuffer: 16 * 1024 * 1024,
    stdio: ["ignore", "pipe", "pipe"],
    ...options,
  }).trim();
}

const git = (...args) => run("git", ["--no-pager", ...args]);
const gh = (...args) => run("gh", args, { env: { ...process.env, GH_PROMPT_DISABLED: "1", NO_COLOR: "1" } });
const api = (path, ...args) => JSON.parse(gh("api", "--hostname", "github.com", `repos/${repo}/${path}`, ...args));
const blob = (commit, path) => git("show", `${commit}:${path}`);
const mainHead = () => api(`git/ref/heads/${base}`).object.sha;

function matches(pr) {
  return pr.user?.login === "dependabot[bot]" && pr.state === "open" && pr.base?.ref === base;
}

export function changedPackages(before, after) {
  if (before.lockfileVersion !== 3 || after.lockfileVersion !== 3 || !before.packages || !after.packages) {
    throw new Blocked("unsupported npm lockfile format");
  }
  const specs = new Set();
  for (const [path, entry] of Object.entries(after.packages)) {
    if (!path) continue;
    const { name, version: v, resolved, link } = entry;
    const previous = Object.hasOwn(before.packages, path) ? before.packages[path] : undefined;
    const sourceFields = ["name", "version", "resolved", "integrity", "link"];
    if (previous && sourceFields.every(field => previous[field] === entry[field])) continue;

    const pkg = name ?? path.split("node_modules/").at(-1);
    if (link || typeof pkg !== "string" || !packageName.test(pkg) || typeof v !== "string" || !version.test(v)) {
      throw new Blocked(`unsupported dependency source for ${JSON.stringify(path)}`);
    }
    if (resolved !== undefined) {
      const tarball = `${pkg}/-/${pkg.split("/").at(-1)}-${v}.tgz`;
      if (![registry + tarball, `https://registry.npmjs.org/${tarball}`].includes(resolved)) {
        throw new Blocked(`unsupported tarball source for ${pkg}@${v}`);
      }
    }
    specs.add(`${pkg}@${v}`);
  }
  return [...specs].sort();
}

export function requireDependencyOnlyManifest(before, after) {
  const dependencyFields = new Set(["dependencies", "devDependencies", "optionalDependencies", "peerDependencies", "overrides"]);
  for (const field of new Set([...Object.keys(before), ...Object.keys(after)])) {
    if (!dependencyFields.has(field) && !isDeepStrictEqual(before[field], after[field])) {
      throw new Blocked(`package.json has a non-dependency change (${JSON.stringify(field)})`);
    }
  }
}

function npm(cwd, cache, ...args) {
  try {
    return run("npm", [...args, `--registry=${registry}`, "--replace-registry-host=npmjs", "--fetch-retries=0", "--fetch-timeout=30000", "--strict-ssl=true", `--cache=${cache}`, "--loglevel=error"], { cwd });
  } catch (error) {
    const code = error.stderr?.match(/npm error code ([A-Z0-9_]+)/i)?.[1] ?? error.code ?? "unknown error";
    const host = error.stderr?.match(/(?:GET|request to)\s+https:\/\/([a-z0-9.-]+)(?:\/|:|\s)/i)?.[1];
    const message = `npm ${args.slice(0, args[0] === "ci" ? 1 : args[0] === "view" ? 2 : 3).join(" ")} failed (${code})${host ? ` at ${host}` : ""}`;
    if (/^(?:E401|E403|E429|E5\d\d|EAI_AGAIN|ECONN\w+|EHOSTUNREACH|ENET\w+|ENOTFOUND|EPIPE|ETIMEDOUT|ERR_TLS\w*|CERT_\w+|SELF_SIGNED_CERT_IN_CHAIN|UNABLE_TO_\w+)$/.test(code)) {
      throw new FeedUnavailable(`${message}; stop and check access to the 1ES feed and its tarball hosts`);
    }
    throw new Blocked(message);
  }
}

function validatePackages(number, parent, merge, paths) {
  const tmp = mkdtempSync(join(tmpdir(), "dependabot-1es-"));
  try {
    const updates = projects
      .filter(project => paths.includes(pathFor(project, lock)))
      .map(project => {
        const before = JSON.parse(blob(parent, pathFor(project, lock)));
        const after = JSON.parse(blob(merge, pathFor(project, lock)));
        return { project, specs: changedPackages(before, after) };
      });
    if (updates.length === 0 || !updates.some(({ specs }) => specs.length > 0)) {
      throw new Blocked("no new or changed locked package versions or sources");
    }

    for (const [index, { project, specs }] of updates.entries()) {
      const cwd = join(tmp, String(index));
      const cache = join(tmp, "npm-cache");
      mkdirSync(cwd);
      for (const file of [manifest, lock]) {
        writeFileSync(join(cwd, file), blob(merge, pathFor(project, file)));
      }
      writeFileSync(join(cwd, ".npmrc"), blob(parent, pathFor(project, ".npmrc")));
      for (const spec of specs) {
        console.log(`#${number} ${project}: ${spec}`);
        const found = JSON.parse(npm(cwd, cache, "view", spec, "version", "--json"));
        if (found !== spec.slice(spec.lastIndexOf("@") + 1)) throw new Blocked(`1ES did not return ${spec}`);
        npm(cwd, cache, "cache", "add", spec, "--ignore-scripts");
      }
      npm(cwd, cache, "ci", "--ignore-scripts", "--no-audit", "--no-fund", "--include=dev", "--include=optional", "--no-offline");
    }
  } finally {
    rmSync(tmp, { recursive: true, force: true });
  }
}

function requireReady(number, head) {
  const pr = JSON.parse(gh("pr", "view", String(number), "--repo", repo, "--json", "headRefOid,mergeStateStatus,reviewDecision,statusCheckRollup"));
  if (pr.headRefOid !== head) throw new Blocked("PR head moved; rerun");
  if (pr.mergeStateStatus !== "CLEAN" || ["CHANGES_REQUESTED", "REVIEW_REQUIRED"].includes(pr.reviewDecision)) {
    throw new Blocked(`not ready to merge (${pr.mergeStateStatus}${pr.reviewDecision ? `, ${pr.reviewDecision}` : ""})`);
  }
  const good = new Set(["SUCCESS", "NEUTRAL", "SKIPPED"]);
  if (!pr.statusCheckRollup.some(check => (check.conclusion || check.state) === "SUCCESS") || pr.statusCheckRollup.some(check => !good.has(check.conclusion || check.state) || (check.status && check.status !== "COMPLETED"))) {
    throw new Blocked("checks are missing, pending, or not passing");
  }
}

function processPR(number, apply) {
  const pr = api(`pulls/${number}`);
  if (!matches(pr) || pr.draft) throw new Blocked("not an open, non-draft Dependabot PR against main");
  const files = api(`pulls/${number}/files?per_page=100`, "--paginate", "--slurp").flat();
  if (!files.length || files.some(({ filename, status }) => status !== "modified" || !allowedPaths.includes(filename))) {
    console.log(`#${number}: skipped (not a supported npm-only update)`);
    return;
  }
  if (pr.mergeable === false) throw new Blocked("merge conflicts");
  if (pr.mergeable !== true) throw new Blocked("GitHub mergeability is pending");
  const head = pr.head.sha;
  const mergeSHA = pr.merge_commit_sha;
  if (!sha.test(head) || !sha.test(mergeSHA)) throw new Blocked("invalid GitHub PR or test merge SHA");
  git("fetch", "--quiet", "--no-tags", "--no-write-fetch-head", "https://github.com/github/gh-aw.git", mergeSHA);
  const [merge, parent, testedHead, extra] = git("rev-list", "--parents", "-n", "1", mergeSHA).split(" ");
  if (!sha.test(merge) || !sha.test(parent) || testedHead !== head || extra || parent !== mainHead()) {
    throw new Blocked("GitHub test merge is stale; rerun after it refreshes");
  }
  const diff = git("diff", "--no-renames", "--name-only", parent, merge, "--");
  if (!diff) {
    console.log(`#${number}: skipped (test merge has no changes; already on main)`);
    return;
  }
  const paths = diff.split("\n");
  if (paths.some(path => !allowedPaths.includes(path))) throw new Blocked("test merge changes files outside npm scope");
  for (const project of projects) {
    const file = pathFor(project, manifest);
    if (!paths.includes(file)) continue;
    if (!paths.includes(pathFor(project, lock))) throw new Blocked(`manifest change without a lockfile change in ${project}`);
    requireDependencyOnlyManifest(JSON.parse(blob(parent, file)), JSON.parse(blob(merge, file)));
  }
  validatePackages(number, parent, merge, paths);
  requireReady(number, head);
  if (parent !== mainHead()) throw new Blocked("main moved during validation; rerun");
  if (!apply) {
    console.log(`#${number}: eligible (1ES install and GitHub checks passed)`);
    return;
  }
  const result = api(`pulls/${number}/merge`, "-X", "PUT", "-f", "merge_method=squash", "-f", `sha=${head}`);
  if (!result.merged) throw new Blocked("GitHub did not confirm the merge");
  console.log(`#${number}: merged (${result.sha})`);
}

function main() {
  const { values } = parseArgs({
    options: { apply: { type: "boolean" }, pr: { type: "string", multiple: true }, help: { type: "boolean" } },
  });
  if (values.help) {
    console.log("Usage: node update.mjs [--apply] [--pr NUMBER ...]");
    return;
  }
  if (values.pr?.some(number => !/^[1-9]\d*$/.test(number) || !Number.isSafeInteger(Number(number)))) {
    throw new Error("--pr requires a positive pull request number");
  }
  git("rev-parse", "--show-toplevel");
  const numbers = values.pr
    ? [...new Set(values.pr.map(Number))]
    : api(`pulls?state=open&base=${base}&per_page=100`, "--paginate", "--slurp")
        .flat()
        .filter(matches)
        .map(pr => pr.number);
  if (!numbers.length) {
    console.log("No open Dependabot PRs against main.");
    if (values.pr) process.exitCode = 1;
    return;
  }
  for (const number of numbers.sort((a, b) => a - b)) {
    try {
      processPR(number, values.apply);
    } catch (error) {
      if (!(error instanceof Blocked)) throw error;
      console.error(`#${number}: skipped: ${error.message}`);
      process.exitCode = 1;
    }
  }
}

if (import.meta.main) {
  try {
    main();
  } catch (error) {
    console.error(error.message.split("\n")[0]);
    process.exitCode = 1;
  }
}
