"use strict";

const assert = require("node:assert/strict");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { execFileSync, spawnSync } = require("node:child_process");
const { test } = require("node:test");
const { filters, localDependencies } = require("./ci-job-filters.cjs");

const repository = path.resolve(__dirname, "..");
const script = path.join(__dirname, "ci-job-filters.cjs");
const select = (workflow, ...files) => filters(workflow, files, "pull_request");
const enabled = result => Object.keys(result).filter(key => result[key]);
const workflowSource = name => fs.readFileSync(path.join(repository, ".github/workflows", `${name}.yml`), "utf8");

test("unrelated scripts do not start expensive CGO or CJS jobs", () => {
  for (const workflow of ["cgo", "cjs"]) {
    assert.deepEqual(enabled(select(workflow, "scripts/pr-sous-chef.mjs", "scripts/pr-sous-chef.test.mjs")), []);
  }
  assert.deepEqual(enabled(select("cjs", "scripts/update-cache.js")), ["lint-js"]);
  assert.deepEqual(enabled(select("cjs", "scripts/unusual\nname.js")), ["lint-js"]);
});

test("Go changes select Go checks without duplicating full and impacted tests", () => {
  const selected = select("cgo", "pkg/workflow/compiler.go");
  for (const key of ["go", "build", "wasm", "native-js", "go-format", "go-lint", "go-custom", "go-vuln", "actions", "conformance", "alpine"]) {
    assert.equal(selected[key], true, key);
  }
  assert.equal(selected["full-go-tests"], false);
  assert.equal(selected.validate, false);
  assert.equal(selected.shell, false);
  assert.deepEqual(enabled(select("cjs", "pkg/workflow/compiler.go")), []);
});

test("Go dependencies, embedded data, fixtures and deleted resources require full coverage", () => {
  for (const file of ["go.mod", "go.sum", "pkg/parser/schemas/main_workflow_schema.json", "pkg/workflow/data/engines/copilot.md", "pkg/cli/data/models.json", "pkg/workflow/testdata/deleted.yml", "internal/tools/actions-build/main.go"]) {
    const selected = select("cgo", file);
    assert.equal(selected.go, true, file);
    assert.equal(selected.build, true, file);
    assert.equal(selected["full-go-tests"], !file.endsWith(".go"), file);
  }
  assert.equal(select("cgo", "pkg/linters/testdata/src/example/example.go")["full-go-tests"], true);
});

test("lint configuration selects only the affected Go linter", () => {
  assert.deepEqual(enabled(select("cgo", ".golangci.yml")), ["go-lint"]);
});

test("runtime edits keep full JS coverage but skip unrelated integrations and Go binaries", () => {
  const file = "actions/setup/js/add_comment.cjs";
  const selected = select("cjs", file);
  for (const key of ["js-tests", "js-typecheck", "lint-js"]) assert.equal(selected[key], true, key);
  for (const key of ["eslint-tests", "docker", "artifact", "nvx"]) assert.equal(selected[key], false, key);
  const go = select("cgo", file);
  for (const key of ["go", "build", "wasm", "go-lint", "go-custom", "go-vuln", "alpine"]) assert.equal(go[key], false, key);
  assert.equal(go.actions, true);
  assert.equal(go.conformance, true);
});

test("factory edits select lint and factory tests without repeating runtime shards", () => {
  assert.deepEqual(enabled(select("cjs", "eslint-factory/rules/no-raw-log.cjs")), ["lint-js", "eslint-tests"]);
});

test("prompt edits run runtime tests without installing lint or typecheck dependencies", () => {
  assert.deepEqual(enabled(select("cjs", "actions/setup/md/task_complete.md")), ["js-tests"]);
});

test("OTLP and artifact integrations include their transitive local dependencies", () => {
  const cases = [
    ["otlp", ["actions/setup/js/scripts/test_otlp_protobuf.mjs"]],
    ["artifact", ["actions/setup/js/artifact_client.cjs", "actions/setup/js/artifact_client_live_api.test.cjs"]],
  ];
  for (const [key, roots] of cases) {
    const dependencies = localDependencies(roots);
    assert.ok(dependencies.size > roots.length);
    for (const file of dependencies) assert.equal(select("cjs", file)[key], true, `${key}: ${file}`);
  }
  assert.equal(select("cjs", "actions/setup/js/otlp_protobuf.cjs").otlp, true);
  assert.equal(select("cjs", "actions/setup/js/error_helpers.cjs").artifact, true);
});

test("specialized integrations include package manifests, shell inputs and version pins", () => {
  for (const file of ["actions/setup/js/package.json", "actions/setup/js/package-lock.json"]) {
    const selected = select("cjs", file);
    for (const key of ["js-tests", "js-typecheck", "lint-js", "otlp", "docker", "artifact"]) assert.equal(selected[key], true, `${key}: ${file}`);
  }
  const shell = select("cgo", "actions/setup/sh/release_integrity.sh");
  assert.equal(shell.shell, true);
  assert.equal(shell["go-custom"], false);
  assert.equal(select("cjs", "actions/setup/sh/release_integrity.sh").docker, true);
  assert.equal(select("cjs", "actions/setup/sh/release_integrity.sh")["js-tests"], true);
  for (const file of ["pkg/constants/constants.go", "pkg/workflow/nvx_copilot_musl_integration_test.go", "actions/setup/js/nvx_assets.cjs", "actions/setup/js/copilot_sdk_mcp_config_path.cjs", "go.mod", "go.sum"]) {
    assert.equal(select("cjs", file).nvx, true, file);
  }
  assert.deepEqual(enabled(select("cgo", "Dockerfile")), ["alpine"]);
});

test("main and manual runs retain every input group and control changes force coverage", () => {
  for (const workflow of ["cgo", "cjs"]) {
    for (const event of ["push", "workflow_dispatch"]) {
      assert.ok(Object.values(filters(workflow, [], event)).every(Boolean), `${workflow}: ${event}`);
    }
    for (const file of ["Makefile", "scripts/ci-job-filters.cjs", "scripts/ci-job-filters.test.cjs", `.github/workflows/${workflow}.yml`]) {
      assert.ok(Object.values(select(workflow, file)).every(Boolean), file);
    }
    assert.ok(Object.values(select(workflow)).every(value => !value));
  }
  assert.throws(() => filters("unknown", [], "pull_request"), /Unknown CI workflow/);
});

test("workflow outputs and job conditions are wired to the selector", () => {
  for (const workflow of ["cgo", "cjs"]) {
    const source = workflowSource(workflow);
    for (const match of source.matchAll(/needs\.checkout-cache\.outputs\.([\w-]+)/g)) {
      const key = match[1];
      assert.ok(key in filters(workflow, [], "pull_request"), `${workflow}: ${key}`);
      assert.ok(source.includes(`${key}: \${{ steps.changes.outputs.${key} }}`), `${workflow}: output ${key}`);
    }
    assert.match(source, /fetch-depth: 2/);
    assert.match(source, /persist-credentials: false/);
    assert.ok(source.includes(`run: node scripts/ci-job-filters.cjs ${workflow}`));
    const jobs = source.split(/^jobs:\n/m)[1];
    const exceptions = new Set(["checkout-cache", "test", "canary-go", "bench", "fuzz", "security-scan", "summarize-timing", "cleanup-cache"]);
    for (const match of jobs.matchAll(/^  ([\w-]+):\n([\s\S]*?)(?=^  [\w-]+:\n|$(?![\s\S]))/gm)) {
      if (!exceptions.has(match[1])) assert.match(match[2], /if: needs\.checkout-cache\.outputs\./, `${workflow}: ungated job ${match[1]}`);
    }
  }
  const go = workflowSource("cgo");
  assert.match(go, /test:\n[\s\S]*?if: needs\.checkout-cache\.outputs\.full-go-tests == 'true'/);
  assert.match(go, /impacted-go-tests:\n\s+if: .*full-go-tests != 'true'/);
  const js = workflowSource("cjs");
  assert.doesNotMatch(js, /make test-impacted-js/);
  assert.match(js, /Run ESLint factory tests/);
  assert.match(js, /Test CI job filters and workflow contracts/);
});

test("workflow triggers include targeted job inputs", () => {
  const inputs = {
    cgo: [
      ".golangci.yml",
      "Dockerfile",
      ".dockerignore",
      ".github/aw/actions-lock.json",
      "scripts/ci-job-filters.cjs",
      "internal/tools/actions-build/main.go",
      "actions/setup/js/add_comment.cjs",
      ".github/workflows/example.lock.yml",
      ".github/skills/agentic-workflows/SKILL.md",
    ],
    cjs: [
      ".prettierignore",
      "eslint-factory/rules/example.cjs",
      "scripts/ci-job-filters.test.cjs",
      "scripts/workflow-tool-contracts.test.cjs",
      "actions/setup/sh/release_integrity.sh",
      "pkg/workflow/data/model_aliases.json",
      "pkg/constants/constants.go",
      "pkg/workflow/nvx_copilot_musl_integration_test.go",
      "go.mod",
      "go.sum",
    ],
  };
  for (const [workflow, files] of Object.entries(inputs)) {
    for (const event of ["push", "pull_request"]) {
      const section = workflowSource(workflow).match(new RegExp(`^  ${event}:\\n([\\s\\S]*?)(?=^  \\w|^jobs:)`, "m"))[1];
      const patterns = [...section.matchAll(/^\s+- '([^']+)'/gm)].map(match => match[1]);
      for (const file of files)
        assert.ok(
          patterns.some(pattern => path.matchesGlob(file, pattern)),
          `${workflow}: ${event}: ${file}`
        );
    }
  }
});

test("CLI uses the entire PR diff, including deletions, renames and unusual filenames", () => {
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), "ci-job-filters-"));
  const git = (...args) => execFileSync("git", args, { cwd: directory, encoding: "utf8", stdio: ["pipe", "pipe", "pipe"] }).trim();
  try {
    git("init", "-q");
    git("config", "user.email", "ci-test@example.invalid");
    git("config", "user.name", "CI filter test");
    fs.mkdirSync(path.join(directory, "pkg/workflow"), { recursive: true });
    fs.writeFileSync(path.join(directory, "pkg/workflow/resource.json"), "{}");
    git("add", ".");
    git("commit", "-qm", "base");
    const base = git("rev-parse", "HEAD");
    const branch = git("branch", "--show-current");
    git("checkout", "-qb", "feature");
    git("mv", "pkg/workflow/resource.json", "renamed resource.json");
    git("commit", "-qm", "rename resource");
    fs.mkdirSync(path.join(directory, "scripts"));
    fs.writeFileSync(path.join(directory, "scripts/unusual\nname.js"), "");
    git("add", ".");
    git("commit", "-qm", "later unrelated script");
    git("checkout", "-q", branch);
    fs.writeFileSync(path.join(directory, "base-only.go"), "package base");
    git("add", ".");
    git("commit", "-qm", "unrelated base update");
    git("merge", "--no-ff", "-qm", "PR merge", "feature");
    assert.notEqual(git("rev-parse", "HEAD^1"), base);

    const checkout = path.join(directory, "shallow");
    execFileSync("git", ["clone", "-q", "--depth=2", `file://${directory}`, checkout]);
    for (const workflow of ["cgo", "cjs"]) {
      const output = path.join(directory, `${workflow}.output`);
      execFileSync(process.execPath, [script, workflow], { cwd: checkout, env: { ...process.env, GITHUB_EVENT_NAME: "pull_request", GITHUB_OUTPUT: output, GITHUB_STEP_SUMMARY: "" } });
      const actual = Object.fromEntries(
        fs
          .readFileSync(output, "utf8")
          .trim()
          .split("\n")
          .map(line => {
            const [key, value] = line.split("=");
            return [key, value === "true"];
          })
      );
      assert.deepEqual(actual, select(workflow, "pkg/workflow/resource.json", "renamed resource.json", "scripts/unusual\nname.js"));
    }
    const failure = spawnSync(process.execPath, [script, "cgo"], { cwd: directory, env: { ...process.env, GITHUB_EVENT_NAME: "pull_request", GITHUB_OUTPUT: "" } });
    assert.notEqual(failure.status, 0);
  } finally {
    fs.rmSync(directory, { recursive: true, force: true });
  }
});
