"use strict";

const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");
const { test } = require("node:test");
const { frontDoor } = require("./ci-front-door.cjs");
const catalog = require("../.github/ci/suite-catalog.json");

const workflow = fs.readFileSync(path.join(__dirname, "../.github/workflows/cgo.yml"), "utf8");
const jobs = Object.fromEntries([...workflow.matchAll(/^  ([\w-]+):\n([\s\S]*?)(?=^  [\w-]+:\n|$(?![\s\S]))/gm)].map(match => [match[1], match[2]]));
const optional = ["build-wasm", "actions-build", "mcp-server-compile-test", "alpine-container-test"];
const mandatory = ["go-tests", "security", "native-javascript-tests"];
const allSuites = [...mandatory, ...optional, "cross-platform-build"];
const decision = jobs["front-door-decision"];
const decisionSteps = decision.split(/^    - name: /m).slice(1);
const validator = decisionSteps.find(step => step.startsWith("Validate suite outputs\n"));
const validatorScript = validator.split("node <<'NODE'\n")[1].split("        NODE")[0];

function dependencies(job) {
  const inline = job.match(/^    needs: \[([^\]]+)\]/m);
  if (inline) return inline[1].split(",").map(id => id.trim());
  return [...(job.match(/^    needs:\n((?:    - .+\n)+)/m)?.[1] || "").matchAll(/^    - (.+)$/gm)].map(match => match[1]);
}

function condition(job) {
  return job.match(/^    if: (.+)$/m)?.[1];
}

// Evaluate the small GitHub expression subset used by this workflow's gates.
function evaluate(expression, { event = "pull_request", result = "success", suites = "", checkout = "success", build = "success", cancelled = false } = {}) {
  return vm.runInNewContext(expression.replace(/\bneeds\.([\w-]+)/g, 'needs["$1"]'), {
    github: { event_name: event },
    needs: {
      "front-door-decision": { result, outputs: { suite_ids: suites } },
      "checkout-cache": { result: checkout },
      build: { result: build },
    },
    always: () => true,
    cancelled: () => cancelled,
    fromJSON: JSON.parse,
    contains: (values, value) => values.includes(value),
  });
}

function validatedSuites(raw, outcome = "success") {
  let output;
  const diagnostics = [];
  vm.runInNewContext(validatorScript, {
    process: {
      env: { RAW_SUITE_IDS: raw, SELECTOR_OUTCOME: outcome, GITHUB_OUTPUT: "captured-output" },
      stderr: { write: message => diagnostics.push(message) },
    },
    require: name => {
      assert.equal(name, "node:fs");
      return {
        appendFileSync(file, value) {
          assert.equal(file, "captured-output");
          output = value;
        },
      };
    },
  });
  assert.match(output, /^suite_ids=.*\n$/);
  if (output === "suite_ids=\n") assert.match(diagnostics.join(""), /outputs unavailable or invalid; running all suites/);
  return output.slice("suite_ids=".length, -1);
}

test("inference permissions are isolated from PR code, tests, and cache operations", () => {
  assert.ok(decision);
  for (const [id, job] of Object.entries(jobs)) {
    if (id === "front-door-decision") continue;
    assert.doesNotMatch(job, /copilot-requests: write|CI_DECISION_TOKEN/, id);
  }
  assert.match(decision, /permissions:\n      contents: read\n      copilot-requests: write\n/);
  assert.equal((workflow.match(/copilot-requests: write/g) || []).length, 1);
  assert.equal((workflow.match(/CI_DECISION_TOKEN:/g) || []).length, 1);
  assert.doesNotMatch(decision, /actions\/cache|npm |npx |--test|uses: \.\//);
  assert.doesNotMatch(workflow, /CI_DECISION_URL|secrets\.COPILOT_GITHUB_TOKEN/);
  assert.deepEqual(dependencies(decision), []);
});

test("same-PR selector and shared-client edits cannot become inference-job executable code", () => {
  assert.equal(decisionSteps.length, 3);
  const [trusted, selector] = decisionSteps;
  const pinnedCheckout = /uses: actions\/checkout@[a-f0-9]{40}\b/;
  assert.match(trusted, pinnedCheckout);
  assert.match(trusted, /ref: \$\{\{ github\.event\.pull_request\.base\.sha \}\}/);
  assert.match(trusted, /path: trusted\n/);
  assert.match(trusted, /fetch-depth: 1\n/);
  assert.match(trusted, /persist-credentials: false/);
  assert.match(trusted, /sparse-checkout-cone-mode: false\n/);
  const sparseFiles = trusted.split("sparse-checkout: |\n")[1].trim().split(/\s+/);
  assert.deepEqual(sparseFiles, ["/scripts/ci-front-door.cjs", "/scripts/ci-front-door-diff.cjs", "/actions/setup/js/decisions_client.cjs", "/.github/ci/suite-catalog.json", "/.github/ci/decision-prompt.txt"]);
  assert.equal((decision.match(/uses: actions\/checkout@/g) || []).length, 1);
  assert.doesNotMatch(decision, /path: evidence|CI_DIFF_WORKSPACE/);
  assert.match(selector, /working-directory: trusted\n/);
  assert.match(selector, /CI_DIFF_TOKEN: \$\{\{ github\.token \}\}/);
  assert.match(selector, /CI_DIFF_BASE_SHA: \$\{\{ github\.event\.pull_request\.base\.sha \}\}/);
  assert.match(selector, /CI_DIFF_HEAD_SHA: \$\{\{ github\.sha \}\}/);
  assert.match(selector, /CI_DECISION_TOKEN: \$\{\{ github\.token \}\}/);
  assert.match(selector, /run: node scripts\/ci-front-door\.cjs\n/);
  assert.match(selector, /continue-on-error: true/);
  assert.doesNotMatch(selector, /run:.*evidence|working-directory: evidence/);
  assert.doesNotMatch(validator, /working-directory:|require\((?!'node:fs')/);
  assert.doesNotMatch(decision, /^    (?:env|defaults):/m);
});

test("initial rollout never bootstraps executable selector code from the PR", () => {
  assert.equal(condition(decision), "github.event_name == 'pull_request'");
  assert.match(decisionSteps[1], /if: hashFiles\('trusted\/scripts\/ci-front-door\.cjs'\) != ''/);
  const suites = validatedSuites("", "skipped");
  assert.equal(suites, "");
  for (const id of optional) assert.equal(evaluate(condition(jobs[id]), { suites }), true, id);
});

test("invalid, missing, and unsuccessful selector outputs fail open before job expressions parse JSON", () => {
  for (const raw of ["", "not-json", "null", "{}", "[]", '["unknown"]', JSON.stringify([...allSuites, "unknown"]), JSON.stringify([...mandatory, "security"]), JSON.stringify(optional)]) {
    const suites = validatedSuites(raw);
    assert.equal(suites, "", raw);
    for (const id of optional) assert.equal(evaluate(condition(jobs[id]), { suites }), true, `${id}: ${raw}`);
  }
  for (const outcome of ["failure", "skipped", "cancelled", ""]) assert.equal(validatedSuites(JSON.stringify(mandatory), outcome), "", outcome);
  assert.equal(validatedSuites(JSON.stringify(mandatory)), JSON.stringify(mandatory));
  assert.match(decision, /suite_ids: \$\{\{ steps\.validated\.outputs\.suite_ids \}\}/);
});

test("optional suite gates survive skipped or failed decision jobs and all non-PR events", () => {
  for (const id of optional) {
    assert.deepEqual(dependencies(jobs[id]), ["checkout-cache", "front-door-decision"], id);
    const expression = condition(jobs[id]);
    assert.match(expression, /^always\(\) && !cancelled\(\)/, id);
    assert.equal(evaluate(expression, { suites: JSON.stringify(mandatory) }), false, id);
    assert.equal(evaluate(expression, { suites: JSON.stringify([...mandatory, id]) }), true, id);
    for (const result of ["failure", "skipped", "cancelled", ""]) {
      assert.equal(evaluate(expression, { result, suites: JSON.stringify(mandatory) }), true, `${id}: ${result}`);
    }
    for (const event of ["push", "schedule", "workflow_dispatch"]) {
      assert.equal(evaluate(expression, { event, result: "skipped" }), true, `${id}: ${event}`);
    }
    assert.equal(evaluate(expression, { checkout: "failure" }), false, id);
    assert.equal(evaluate(expression, { cancelled: true }), false, id);
  }
});

test("PR selector results filter actual optional jobs and macOS after output validation", async () => {
  const matrix = jobs["cross-platform-build"].match(/^        os: \$\{\{ (.+) \}\}$/m)[1];
  for (const [file, enabled, inferenceCalls] of [
    ["scripts/workflow-timing.test.cjs", [], 1],
    ["cmd/gh-aw-wasm/main.go", ["build-wasm"], 1],
    ["go.mod", optional, 0],
  ]) {
    let calls = 0;
    const selection = await frontDoor({
      catalog,
      diff: { files: [file], patch: "", truncated: false, unavailable: false },
      prompt: "",
      token: "test-token",
      decide: async () => {
        calls++;
        return { suite_ids: [], confidence: 1 };
      },
    });
    assert.equal(calls, inferenceCalls, file);
    const suites = validatedSuites(JSON.stringify(selection.suite_ids));
    for (const id of optional) {
      assert.equal(evaluate(condition(jobs[id]), { suites }), enabled.includes(id), `${file}: ${id}`);
    }
    assert.equal(JSON.stringify(evaluate(matrix, { suites })), JSON.stringify(inferenceCalls ? ["windows-latest"] : ["macos-latest", "windows-latest"]), file);
    for (const id of mandatory) assert.ok(JSON.parse(suites).includes(id), `${file}: ${id}`);
  }
});

test("non-PR full selection enables every optional gate without inference", async () => {
  const matrix = jobs["cross-platform-build"].match(/^        os: \$\{\{ (.+) \}\}$/m)[1];
  for (const event of ["push", "schedule", "workflow_dispatch"]) {
    const selection = await frontDoor({
      catalog,
      diff: { files: ["scripts/workflow-timing.test.cjs"], patch: "", truncated: false, unavailable: false },
      fullRun: true,
      decide: () => assert.fail("non-PR events must not invoke inference"),
    });
    const suites = validatedSuites(JSON.stringify(selection.suite_ids));
    for (const id of optional) assert.equal(evaluate(condition(jobs[id]), { event, suites, result: "skipped" }), true, `${event}: ${id}`);
    assert.equal(JSON.stringify(evaluate(matrix, { event, suites, result: "skipped" })), '["macos-latest","windows-latest"]');
  }
});

test("mandatory checks are independent of selection and Windows is never omitted", () => {
  for (const id of ["security", "lint-go-govulncheck", "safe-outputs-conformance", "native-javascript-tests", "impacted-go-tests", "build", "verify-integration-build", "test"]) {
    assert.ok(jobs[id], id);
    assert.ok(!dependencies(jobs[id]).includes("front-door-decision"), id);
    assert.doesNotMatch(jobs[id], /outputs\.suite_ids/, id);
  }
  const crossPlatform = jobs["cross-platform-build"];
  const matrix = crossPlatform.match(/^        os: \$\{\{ (.+) \}\}$/m)[1];
  assert.equal(evaluate(condition(crossPlatform), { result: "failure" }), true);
  assert.equal(evaluate(condition(crossPlatform), { result: "skipped", event: "push" }), true);
  assert.equal(evaluate(condition(crossPlatform), { build: "failure" }), false);
  const omitted = { suites: JSON.stringify(mandatory) };
  assert.equal(JSON.stringify(evaluate(matrix, omitted)), '["windows-latest"]');
  for (const settings of [{}, { ...omitted, result: "failure" }, { ...omitted, result: "skipped" }, { ...omitted, event: "push" }, { suites: JSON.stringify(allSuites) }]) {
    assert.equal(JSON.stringify(evaluate(matrix, settings)), '["macos-latest","windows-latest"]');
  }
  assert.match(workflow, /schedule:\n[\s\S]*cron:/);
});

test("the full workflow needs graph is acyclic and conformance covers the trust boundary", () => {
  const visited = new Set();
  function visit(id, active = new Set()) {
    assert.ok(jobs[id], `unknown dependency: ${id}`);
    assert.ok(!active.has(id), `dependency cycle at ${id}`);
    if (visited.has(id)) return;
    for (const dependency of dependencies(jobs[id])) visit(dependency, new Set([...active, id]));
    visited.add(id);
  }
  for (const id of Object.keys(jobs)) visit(id);
  assert.deepEqual(dependencies(jobs["checkout-cache"]), []);
  assert.deepEqual(dependencies(jobs["front-door-conformance"]), []);
  assert.match(jobs["front-door-conformance"], /node --test scripts\/ci-front-door-workflow\.test\.cjs/);
});
