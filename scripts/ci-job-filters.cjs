"use strict";

const fs = require("node:fs");
const path = require("node:path");
const { execFileSync } = require("node:child_process");

const repository = path.resolve(__dirname, "..");

function localDependencies(roots) {
  const dependencies = new Set();
  function visit(file) {
    if (dependencies.has(file)) return;
    dependencies.add(file);
    const absolute = path.join(repository, file);
    if (!fs.existsSync(absolute)) return;
    const source = fs.readFileSync(absolute, "utf8");
    for (const match of source.matchAll(/\b(?:require\(\s*|from\s+)["'](\.[^"']+)["']/g)) {
      const resolved = path.resolve(path.dirname(absolute), match[1]);
      const relative = path.relative(repository, resolved);
      if (relative.startsWith("..") || path.isAbsolute(relative)) {
        throw new Error(`CI dependency escapes the repository: ${file}: ${match[1]}`);
      }
      const candidates = [relative, `${relative}.cjs`, `${relative}.js`, `${relative}.json`, `${relative}/index.js`];
      const candidate = candidates.find(name => fs.existsSync(path.join(repository, name)) && fs.statSync(path.join(repository, name)).isFile());
      if (candidate) visit(candidate);
      else candidates.forEach(name => dependencies.add(name));
    }
  }
  roots.forEach(visit);
  return dependencies;
}

function filters(workflow, files, eventName) {
  if (!["cgo", "cjs"].includes(workflow)) throw new Error(`Unknown CI workflow: ${workflow}`);
  const changed = expression => files.some(file => expression.test(file));
  const control = changed(/^(Makefile|scripts\/ci-job-filters(?:\.test)?\.cjs|\.github\/workflows\/ci\.yml)$/) || files.includes(`.github/workflows/${workflow}.yml`);
  const goDependencies = changed(/^go\.(mod|sum)$/);
  // Non-Go package files include embedded schemas, engine definitions and test fixtures.
  const go = goDependencies || changed(/(^|\/)[^/]+\.go$|^(cmd|pkg|internal)\//);
  const goFiles = changed(/\.go$/);
  const setup = changed(/^actions\/setup\//);
  const build = go || changed(/^scripts\/(build[^/]*|sync-[^/]*|check-agent-local-settings[^/]*)$|^install-gh-aw\.(sh|ps1)$|^\.github\/aw\/(actions-lock|compat|releases)(\.schema)?\.json$/);
  let selected;
  if (workflow === "cgo") {
    selected = {
      go,
      "full-go-tests": goDependencies || files.some(file => /^(cmd|pkg|internal)\//.test(file) && (!file.endsWith(".go") || file.includes("/testdata/"))),
      build,
      wasm: go || changed(/^scripts\/(test-wasm|build-wasm)/),
      "native-js": go || changed(/^scripts\/.*actions.*parser/),
      "go-format": goFiles || changed(/^scripts\/format-changed/),
      "go-lint": goFiles || goDependencies || changed(/^\.golangci\.ya?ml$/),
      "go-custom": goFiles || goDependencies,
      "go-vuln": go || changed(/^scripts\/.*govuln/),
      actions: go || changed(/^actions\/|^scripts\/.*actions/),
      validate: changed(/^\.github\/workflows\/|^\.github\/aw\/(compat|releases)(\.schema)?\.json$|^\.github\/skills\/agentic-workflows\//),
      conformance: go || setup || changed(/^scripts\/check-safe-outputs-conformance/),
      shell: changed(/^actions\/.*\.sh$|^scripts\/check-action-sh-no-python/),
      alpine: build || changed(/^Dockerfile$|^\.dockerignore$/),
    };
  } else {
    const runtime = changed(/^actions\/setup\/(js|md)\/|^pkg\/cli\/data\/models\.json$|^pkg\/workflow\/data\/model_aliases\.json$|^docs\/public\/schemas\/.*session\.schema\.json$/);
    const jsFiles = changed(/^actions\/setup\/js\/.*\.(cjs|js|mjs|ts|json)$/s);
    const npm = changed(/^actions\/setup\/js\/package(-lock)?\.json$/);
    const dependencyChanged = roots => {
      const dependencies = localDependencies(roots);
      return files.some(file => dependencies.has(file));
    };
    selected = {
      "js-tests": runtime || changed(/^actions\/setup\/sh\/release_integrity\.sh$/),
      "js-typecheck": jsFiles,
      "lint-js":
        jsFiles ||
        changed(/^eslint-factory\/|^pkg\/cli\/data\/models\.json$|^pkg\/workflow\/data\/model_aliases\.json$|^docs\/public\/schemas\/.*session\.schema\.json$|^scripts\/.*\.js$|^scripts\/validate-cjs-syntax\.sh$|^\.prettier(ignore|rc.*)$/s),
      "eslint-tests": changed(/^eslint-factory\//),
      otlp: npm || dependencyChanged(["actions/setup/js/scripts/test_otlp_protobuf.mjs"]),
      docker: npm || changed(/^actions\/setup\/js\/(release_staging\.test\.cjs|vitest\.config\.mjs)$|^actions\/setup\/sh\/release_integrity\.sh$/),
      artifact: npm || changed(/^actions\/setup\/js\/vitest\.artifact-integration\.config\.mjs$/) || dependencyChanged(["actions/setup/js/artifact_client.cjs", "actions/setup/js/artifact_client_live_api.test.cjs"]),
      nvx: goDependencies || changed(/^pkg\/constants\/|^pkg\/workflow\/nvx_copilot_musl_integration_test\.go$|^actions\/setup\/js\/(nvx_assets|copilot_sdk_mcp_config_path)\.cjs$/),
    };
  }
  return Object.fromEntries(Object.entries(selected).map(([key, value]) => [key, eventName !== "pull_request" || control || value]));
}

function main() {
  const workflow = process.argv[2];
  const eventName = process.env.GITHUB_EVENT_NAME;
  if (!eventName || !process.env.GITHUB_OUTPUT) throw new Error("GITHUB_EVENT_NAME and GITHUB_OUTPUT are required");
  // The merge commit's first parent is the PR base; depth 2 contains the entire PR diff.
  const files = eventName === "pull_request" ? execFileSync("git", ["diff", "--name-only", "--no-renames", "-z", "HEAD^1", "HEAD", "--"], { encoding: "utf8" }).split("\0").filter(Boolean) : [];
  const selected = filters(workflow, files, eventName);
  fs.appendFileSync(
    process.env.GITHUB_OUTPUT,
    Object.entries(selected)
      .map(([key, value]) => `${key}=${value}\n`)
      .join("")
  );
  if (process.env.GITHUB_STEP_SUMMARY) {
    fs.appendFileSync(
      process.env.GITHUB_STEP_SUMMARY,
      `## Targeted CI jobs\n\n| Input group | Run |\n| --- | --- |\n${Object.entries(selected)
        .map(([key, value]) => `| ${key} | ${value} |`)
        .join("\n")}\n`
    );
  }
}

module.exports = { filters, localDependencies };
if (require.main === module) main();
