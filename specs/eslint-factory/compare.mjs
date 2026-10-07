import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import vm from "node:vm";
import { createHash } from "node:crypto";
import { createRequire } from "node:module";
import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";

export const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../..");
const require = createRequire(import.meta.url);
const library = path.join(root, "eslint-factory");
const runtime = path.join(root, "actions/setup/js");
const ts = require(require.resolve("typescript", { paths: [library] }));
const yaml = require(require.resolve("yaml", { paths: [runtime] }));
const { ESLint } = require(require.resolve("eslint", { paths: [library] }));
export const sha256 = value => createHash("sha256").update(value).digest("hex");

export function run(command, args, options = {}) {
  const result = spawnSync(command, args, { cwd: root, encoding: "utf8", timeout: 60_000, maxBuffer: 16 * 1024 * 1024, ...options });
  if (result.error) throw result.error;
  return { status: result.status, stdout: result.stdout, stderr: result.stderr };
}

export function checkoutIdentity() {
  const head = run("git", ["rev-parse", "HEAD"], { timeout: 10_000 });
  assert.equal(head.status, 0, head.stderr);
  assert.match(head.stdout.trim(), /^[a-f0-9]{40,64}$/);
  const originMain = run("git", ["rev-parse", "--verify", "refs/remotes/origin/main"], { timeout: 10_000 });
  return {
    head: head.stdout.trim(),
    originMain: originMain.status === 0 ? originMain.stdout.trim() : null,
    sourceState: "Current working-tree bytes, including uncommitted formalization and README changes; source hashes, not HEAD alone, identify measured code.",
  };
}

export function workflow(text) {
  const lines = text.split("\n");
  assert.equal(lines[0], "---");
  const end = lines.indexOf("---", 1);
  assert.ok(end > 0, "Workflow frontmatter must close");
  const parsed = yaml.parseDocument(lines.slice(1, end).join("\n"), { uniqueKeys: true });
  assert.equal(parsed.errors.length, 0, "Workflow YAML must be unambiguous");
  return { frontmatter: parsed.toJS(), body: lines.slice(end + 1).join("\n") };
}

export function registration(text) {
  const source = ts.createSourceFile("index.ts", text, ts.ScriptTarget.Latest, true, ts.ScriptKind.TS);
  let initializer;
  function visit(node) {
    if (ts.isVariableDeclaration(node) && node.name.getText(source) === "plugin") initializer = node.initializer;
    ts.forEachChild(node, visit);
  }
  visit(source);
  assert.ok(initializer && ts.isObjectLiteralExpression(initializer), "Expected the actual plugin object");
  const rules = initializer.properties.find(property => property.name?.getText(source) === "rules")?.initializer;
  assert.ok(rules && ts.isObjectLiteralExpression(rules));
  return rules.properties
    .map(property => {
      assert.ok(ts.isPropertyAssignment(property) && ts.isStringLiteral(property.name) && ts.isIdentifier(property.initializer));
      return property.name.text;
    })
    .sort();
}

function evaluateConfig(text, plugin, filename) {
  const module = { exports: {} };
  vm.runInNewContext(
    text,
    {
      module,
      require: target => {
        assert.equal(target, "./dist/index.js", "Config dependency changed; review the executable extraction");
        return plugin;
      },
    },
    { filename, timeout: 2000 }
  );
  return module.exports;
}

export function readmeTable(text) {
  const sections = new Set([...text.matchAll(/^### `([^`]+)`\s*$/gm)].map(match => match[1].toLowerCase()));
  const rows = [...text.matchAll(/^\| \[`([^`]+)`\]\((#[^)]+)\)/gm)].map(match => ({ rule: match[1], target: match[2] }));
  return {
    rules: rows.map(row => row.rule),
    brokenTargets: rows.filter(row => !sections.has(row.target.slice(1).toLowerCase())),
  };
}

export function compareFacts(facts) {
  const mismatches = [];
  const expect = (label, actual, expected) => {
    if (JSON.stringify(actual) !== JSON.stringify(expected)) mismatches.push({ label, actual, expected });
  };
  expect("registered source rules equal executable exports", facts.sourceRules, facts.runtimeRules);
  expect("configured rules equal registered rules", facts.configRules, facts.sourceRules);
  expect("README linked rule table covers registered rules", [...facts.readmeRuleLinks].sort(), facts.sourceRules);
  expect("README rule table targets existing sections", facts.readmeBrokenTargets, []);
  expect("default profile is a singleton", facts.defaultProfileMax, 1);
  expect("dispatcher worker allowlist", facts.dispatchWorkers, ["eslint-miner", "eslint-monster", "eslint-refiner"]);
  expect("dispatcher run dispatch budget", facts.dispatchMax, 3);
  expect("all workers require assignments", facts.requiredAssignments, [true, true, true]);
  expect("monster assignment safe-output maximum", facts.monsterAssignmentMax, 3);
  expect("refiner issue safe-output maximum", facts.refinerIssueMax, 3);
  expect("monster discussion safe-output maximum", facts.monsterDiscussionMax, 1);
  expect("hard HTTP response rule", facts.httpSeverity, "error");
  expect("other rules warn", facts.otherSeverities, ["warn"]);
  expect("native collector counts each Claim separately", facts.runtimeProbes.collector.acceptedPerClaim, [3, 3]);
  expect("native collector rejects scoped overflow", facts.runtimeProbes.collector.overflowRejected, true);
  expect("native collector checks sibling minimum independently", facts.runtimeProbes.collector.siblingMinimumRejected, true);
  expect("warning-only ESLint exits zero", facts.scanProbes.warning.status, 0);
  expect("hard-error ESLint exits nonzero", facts.scanProbes.error.status, 1);
  expect("changed scan excludes nested CJS", facts.scanProbes.changed.status, 0);
  expect("recursive scan includes nested CJS", facts.scanProbes.recursive.status, 1);
  expect("actual configuration coverage", facts.coverage, { cjs: true, nestedCjs: true, js: false, ts: false, testCjs: false, testJs: false });
  expect("monster precheck flag includes warnings", facts.precheck.warning.flag, true);
  expect("monster precheck flag excludes hard errors", facts.precheck.error.flag, false);
  expect("monster precheck flag excludes installation failure", facts.precheck.installFailed.flag, false);
  expect("installation failure aborts precheck", facts.precheck.installFailed.status, 1);
  expect("monster precheck flag excludes build failure", facts.precheck.buildFailed.flag, false);
  expect("monster precheck flag excludes tooling failure", facts.precheck.toolFailed.flag, false);
  expect("memory adapter", facts.memoryAdapter, {
    mode: "script",
    effectType: "git_tree",
    repository: "github/gh-aw",
    base: "46b68a61c366a01d86dc319b8e689d09a48bad04",
    prefix: "memory/eslint-refiner-runs",
  });
  return mismatches;
}

function executableFunction(text, name) {
  const source = ts.createSourceFile("runtime.cjs", text, ts.ScriptTarget.Latest, true, ts.ScriptKind.JS);
  const declarations = source.statements.filter(node => ts.isFunctionDeclaration(node) && node.name?.text === name);
  assert.equal(declarations.length, 1, `Missing runtime boundary ${name}`);
  return { line: source.getLineAndCharacterOfPosition(declarations[0].getStart(source)).line + 1, sha256: sha256(declarations[0].getText(source)) };
}

function mkdir(directory) {
  fs.mkdirSync(directory, { recursive: true });
  return directory;
}
function write(filename, content) {
  mkdir(path.dirname(filename));
  fs.writeFileSync(filename, content);
}

export async function analyze(artifacts) {
  const checkout = checkoutIdentity();
  artifacts = path.resolve(artifacts);
  assert.ok(artifacts !== root && !artifacts.startsWith(library + path.sep), "Use a separate artifact directory");
  mkdir(artifacts);
  const scratch = mkdir(path.join(artifacts, "scratch"));
  const commandEnvironment = { ...process.env, TMPDIR: scratch, npm_config_cache: path.join(artifacts, "npm-cache") };
  const sourceFiles = new Set();
  const sourceHashes = new Map(
    fs
      .readdirSync(runtime)
      .filter(name => name.endsWith(".cjs"))
      .map(name => {
        const filename = path.join(runtime, name);
        return [filename, sha256(fs.readFileSync(filename))];
      })
  );
  const read = filename => {
    filename = path.resolve(filename);
    sourceFiles.add(filename);
    const content = fs.readFileSync(filename, "utf8");
    const hash = sha256(content);
    if (sourceHashes.has(filename)) assert.equal(hash, sourceHashes.get(filename), `Source changed during comparison: ${filename}`);
    sourceHashes.set(filename, hash);
    return content;
  };
  const manifest = JSON.parse(read(path.join(library, "package.json")));
  const readme = read(path.join(library, "README.md"));
  const readmeRules = readmeTable(readme);
  const workflows = Object.fromEntries(
    ["factory-dispatcher", "miner", "refiner", "monster"].map(name => {
      const filename = path.join(root, `.github/workflows/eslint-${name}.md`);
      return [name, workflow(read(filename))];
    })
  );
  const fixture = mkdir(path.join(artifacts, "library-fixture"));
  const fixtureLibrary = mkdir(path.join(fixture, "eslint-factory"));
  const fixtureRuntime = path.join(fixture, "actions/setup/js");
  fs.rmSync(fixtureRuntime, { recursive: true, force: true });
  mkdir(fixtureRuntime);
  for (const [name, target] of [
    ["src", path.join(library, "src")],
    ["node_modules", path.join(library, "node_modules")],
  ]) {
    const link = path.join(fixtureLibrary, name);
    if (!fs.existsSync(link)) fs.symlinkSync(target, link, "dir");
    assert.equal(fs.realpathSync(link), fs.realpathSync(target));
  }
  for (const name of ["package.json", "tsconfig.json", "eslint.config.cjs"]) write(path.join(fixtureLibrary, name), read(path.join(library, name)));
  const config = ts.readConfigFile(path.join(library, "tsconfig.json"), ts.sys.readFile);
  assert.equal(config.error, undefined);
  const parsed = ts.parseJsonConfigFileContent(config.config, ts.sys, library);
  assert.equal(parsed.errors.length, 0);
  for (const filename of parsed.fileNames) read(filename);
  const build = run("npm", ["run", "build", "--silent"], { cwd: fixtureLibrary, env: commandEnvironment });
  write(path.join(artifacts, "build.log"), build.stdout + build.stderr);
  assert.equal(build.status, 0, `Actual library build failed:\n${build.stdout}${build.stderr}`);
  const plugin = require(path.join(fixtureLibrary, manifest.main));
  const sourceRules = registration(read(path.join(library, "src/index.ts")));
  const configText = read(path.join(library, "eslint.config.cjs"));
  const executedConfig = evaluateConfig(configText, plugin, path.join(library, "eslint.config.cjs"));
  const configuredRules = executedConfig.flatMap(block => Object.entries(block.rules || {}));
  const eslint = new ESLint({ cwd: fixtureRuntime, overrideConfigFile: path.join(fixtureLibrary, "eslint.config.cjs") });
  const coverage = {};
  for (const [name, filename] of Object.entries({ cjs: "probe.cjs", nestedCjs: "nested/probe.cjs", js: "probe.js", ts: "probe.ts", testCjs: "probe.test.cjs", testJs: "probe.test.js" })) {
    const resolved = await eslint.calculateConfigForFile(path.join(fixtureRuntime, filename));
    coverage[name] = !!resolved?.rules?.["gh-aw-custom/no-throw-plain-object"];
  }
  const warningSource = 'throw { message: "warning-only fixture" };\n';
  const errorSource = 'const http = require("node:http");\nhttp.get("https://example.invalid", response => { response.resume(); }).on("error", () => {});\n';
  const warningResults = await eslint.lintText(warningSource, { filePath: path.join(fixtureRuntime, "warning.cjs") });
  assert.ok(warningResults[0].warningCount > 0 && warningResults[0].errorCount === 0);
  const errorResults = await eslint.lintText(errorSource, { filePath: path.join(fixtureRuntime, "error.cjs") });
  assert.ok(errorResults[0].messages.some(message => message.ruleId === "gh-aw-custom/require-http-response-error-listener" && message.severity === 2));
  const scanProbes = {};
  const executeScan = name => {
    const result = run("npm", ["run", name, "--silent"], { cwd: fixtureLibrary, env: commandEnvironment });
    return result;
  };
  write(path.join(fixtureRuntime, "warning.cjs"), warningSource);
  write(path.join(fixtureRuntime, "ignored.test.cjs"), errorSource);
  write(path.join(fixtureRuntime, "uncovered.js"), errorSource);
  write(path.join(fixtureRuntime, "uncovered.ts"), errorSource);
  scanProbes.warning = executeScan("lint:setup-js");
  write(path.join(fixtureRuntime, "error.cjs"), errorSource);
  scanProbes.error = executeScan("lint:setup-js");
  fs.unlinkSync(path.join(fixtureRuntime, "error.cjs"));
  write(path.join(fixtureRuntime, "nested/error.cjs"), errorSource);
  scanProbes.changed = executeScan("lint:setup-js:changed");
  scanProbes.recursive = executeScan("lint:setup-js");
  for (const [name, result] of Object.entries(scanProbes)) write(path.join(artifacts, `scan-${name}.log`), result.stdout + result.stderr);

  const precheckSource = workflows.monster.frontmatter.steps.find(step => step.id === "eslint_scan")?.run;
  assert.equal(typeof precheckSource, "string");
  assert.ok(precheckSource.includes("/tmp/gh-aw/agent"), "Review the precheck path substitution when the workflow changes");
  const bin = mkdir(path.join(artifacts, "precheck-bin"));
  const npmStub = path.join(bin, "npm");
  write(npmStub, '#!/bin/sh\nif [ "$1" = ci ]; then exit "$NPM_CI_EXIT"; fi\nif [ "$1" = run ] && [ "$2" = lint:setup-js ]; then printf "%s\\n" "$LINT_LOG"; exit "$LINT_EXIT"; fi\nexit 99\n');
  fs.chmodSync(npmStub, 0o755);
  const precheck = {};
  for (const [name, ci, lint, log] of [
    ["warning", 0, scanProbes.warning.status, scanProbes.warning.stdout + scanProbes.warning.stderr],
    ["error", 0, scanProbes.error.status, scanProbes.error.stdout + scanProbes.error.stderr],
    ["installFailed", 1, 0, "install unavailable"],
    ["buildFailed", 0, 1, "tsc: build failed"],
    ["toolFailed", 0, 2, "ESLint: tooling failure"],
  ]) {
    const directory = mkdir(path.join(artifacts, "precheck", name));
    mkdir(path.join(directory, "eslint-factory"));
    mkdir(path.join(directory, ".github/skills"));
    const output = path.join(directory, "agent");
    const transformed = precheckSource.replaceAll("/tmp/gh-aw/agent", output);
    write(path.join(directory, "precheck.sh"), transformed);
    const result = run("bash", ["precheck.sh"], {
      cwd: directory,
      env: { ...commandEnvironment, PATH: bin + path.delimiter + process.env.PATH, NPM_CI_EXIT: String(ci), LINT_EXIT: String(lint), LINT_LOG: log },
    });
    precheck[name] = {
      status: result.status,
      flag: fs.existsSync(path.join(output, "lint-clean.flag")),
      diagnostics: fs.existsSync(path.join(output, "eslint-diagnostics.txt")) ? fs.readFileSync(path.join(output, "eslint-diagnostics.txt"), "utf8") : null,
    };
    write(path.join(directory, "execution.log"), result.stdout + result.stderr);
  }

  const policy = require(path.join(runtime, "work_queue_policy.cjs"));
  const boundaryFunctions = {};
  for (const [file, names] of [
    ["work_queue_claim_scope.cjs", ["normalizeAssignment", "normalizeClaimScope", "withClaimExecution", "assertClaimAuthorized"]],
    ["work_queue_policy.cjs", ["defaultPolicy", "bindingAuthority", "claimAuthority", "validateSubmissionEntitlement"]],
    ["work_queue_delivery.cjs", ["validateDeliveryContract", "inspectClaimDelivery", "verifyClaimDelivery"]],
  ]) {
    const text = read(path.join(runtime, file));
    boundaryFunctions[file] = Object.fromEntries(names.map(name => [name, executableFunction(text, name)]));
  }
  const runtimeProbes = await probeRuntime(sourceFiles, artifacts);
  const adapter = workflows.refiner.frontmatter["safe-outputs"]["claim-adapters"].persist_eslint_memory;
  const facts = {
    sourceRules,
    runtimeRules: Object.keys(plugin.rules).sort(),
    configRules: configuredRules.map(([name]) => name.replace("gh-aw-custom/", "")).sort(),
    httpSeverity: Object.fromEntries(configuredRules)["gh-aw-custom/require-http-response-error-listener"],
    otherSeverities: [...new Set(configuredRules.filter(([name]) => !name.endsWith("/require-http-response-error-listener")).map(([, severity]) => severity))],
    packageScripts: manifest.scripts,
    dispatchWorkers: [...workflows["factory-dispatcher"].frontmatter["safe-outputs"]["dispatch-workflow"].workflows].sort(),
    dispatchMax: workflows["factory-dispatcher"].frontmatter["safe-outputs"]["dispatch-workflow"].max,
    requiredAssignments: ["miner", "refiner", "monster"].map(name => workflows[name].frontmatter.tools["work-queue"]["require-assignment"]),
    monsterAssignmentMax: workflows.monster.frontmatter["safe-outputs"]["assign-to-agent"].max,
    monsterDiscussionMax: workflows.monster.frontmatter["safe-outputs"]["create-discussion"].max,
    refinerIssueMax: workflows.refiner.frontmatter["safe-outputs"]["create-issue"].max,
    defaultProfileMax: policy.defaultPolicy({ repository: "owner/repo", principal: "11" }).pools.default.profiles.default.max_claims,
    readmeRuleLinks: readmeRules.rules,
    readmeBrokenTargets: readmeRules.brokenTargets,
    coverage,
    scanProbes,
    precheck,
    runtimeProbes,
    boundaryFunctions,
    memoryAdapter: { mode: adapter.mode, effectType: adapter["effect-type"], repository: adapter["target-repo"], base: adapter["git-tree"]["base-revision"], prefix: adapter["git-tree"]["branch-prefix"] },
  };
  const mismatches = compareFacts(facts);
  const assumptions = [
    "Policy, authorized producers, immutable worker revisions, numeric resource scope and credentials are externally provisioned; these four workflow sources do not install a miner->refiner->monster DAG.",
    "The model admits exactly two root Works with satisfied dependencies/capacity and fixed equal-priority order, and permits at most one protected prefix request in a modeled run.",
    "The model projects the first compatible assignment of that request, not all native dispatches. Actual mixed-profile probes produce two singleton dispatches; leaving the other modeled profile queued is projection, not a claimed scheduler behavior.",
    "Dispatcher prompt obligations max_claims=3/max_dispatches=3 and at most one request are not inferred to be a runtime-enforced per-run request counter.",
    "Model Effect authority abstracts successful winning Claim/native attempt/principal/resource checks; local probes do not exercise cryptographic authentication, remote APIs or concurrency.",
    "Each modeled Claim projects one output family. Whole-contract Readback abstracts all other declared outputs, memory snapshots, resource receipts and durable queue-control inventory; no profile contract is supplied by these workflow sources.",
    "Three total Copilot assignments, quality/build/test/low-false-positive bars, nonduplicate issues, and final-action wording are agent obligations; configured Claim counts do not imply run-wide worker caps.",
    "Completion is distinct from independent verified delivery and Result. Probe inspectClaimDelivery is diagnostic; protected Result attestations require additional trusted inventory and channel provenance.",
    "Warning-only diagnostics produce ESLint exit zero and monster lint-clean. Scan coverage is CJS excluding tests, not generic JS/TS goals; npm ci/build/tool failure is not evidence of defect-free source.",
    "No unbounded liveness, production refinement, AST-rule completeness, low false positives or comprehensive security guarantee is claimed.",
  ];
  const documentationFindings = [
    { category: "coverage", observation: "README goals and miner mission mention JS/TS; actual configured rules apply to non-test CJS. This is a scope distinction, not proof that all JS/TS is scanned." },
    { category: "warning-short-circuit", observation: "Monster's clean branch discards diagnostics for successful lint, including warning-only findings. Most configured rules warn; require-http-response-error-listener is error." },
    { category: "prompt-ambiguity", observation: "Miner says independently process every Claim, then exactly one last create_pull_request/noop. The model does not turn that final-action prose into a global runtime output limit." },
    { category: "delegation", observation: "assign-to-agent delivery means the declared assignment was independently verified, not that a downstream Copilot remediation build/test succeeded." },
    {
      category: "rule-table",
      observation: "README linked rule table coverage and existing section targets are checked against registered source rules; this is not a rule-semantics audit.",
      missing: sourceRules.filter(name => !readmeRules.rules.includes(name)),
      brokenTargets: readmeRules.brokenTargets,
    },
  ];
  for (const filename of Object.keys(require.cache)) if (filename.startsWith(runtime + path.sep) && !filename.includes(path.sep + "node_modules" + path.sep)) sourceFiles.add(filename);
  for (const dependency of ["typescript", "eslint", "yaml"]) {
    const base = dependency === "yaml" ? runtime : library;
    let directory = path.dirname(require.resolve(dependency, { paths: [base] }));
    while (!fs.existsSync(path.join(directory, "package.json"))) directory = path.dirname(directory);
    sourceFiles.add(path.join(directory, "package.json"));
  }
  const sources = [...sourceFiles].sort().map(filename => {
    const hash = sha256(fs.readFileSync(filename));
    if (sourceHashes.has(filename)) assert.equal(hash, sourceHashes.get(filename), `Source changed during comparison: ${filename}`);
    return { path: path.relative(root, filename), sha256: hash };
  });
  const report = {
    checkout,
    facts,
    mismatches,
    assumptions,
    documentationFindings,
    sources,
    tool: { node: process.version, typescript: ts.version, eslint: ESLint.version, yaml: JSON.parse(fs.readFileSync(require.resolve("yaml/package.json", { paths: [runtime] }), "utf8")).version },
    precheckTransformation: "Only /tmp/gh-aw/agent paths rebased into artifact fixtures; npm stub supplies measured scan exit/status or explicit failure. No npm ci, workflow run, network or native write executed.",
  };
  assert.equal(checkoutIdentity().head, checkout.head, "HEAD changed during comparison; rerun against a stable checkout");
  write(path.join(artifacts, "comparison.json"), JSON.stringify(report, null, 2) + "\n");
  return report;
}

async function probeRuntime(sourceFiles, artifacts) {
  const load = file => {
    const filename = path.join(runtime, file);
    sourceFiles.add(filename);
    return require(filename);
  };
  const scope = load("work_queue_claim_scope.cjs");
  const delivery = load("work_queue_delivery.cjs");
  const policy = load("work_queue_policy.cjs");
  const { queueFixture } = load("work_queue_lifecycle.test_helpers.cjs");
  const replay = load("work_queue_replay.cjs");
  const singleton = queueFixture({ count: 2, batch: 1 });
  assert.equal(singleton.assignment.claims.length, 1);
  const batch = queueFixture({ count: 2, batch: 2, bound: true });
  const assignment = scope.normalizeAssignment(batch.assignment);
  assert.equal(assignment.version, 3);
  assert.equal(assignment.claims.length, 2);
  assert.ok(Object.isFrozen(assignment) && Object.isFrozen(assignment.claims) && Object.isFrozen(assignment.claims[0].work));
  assert.throws(() => scope.normalizeAssignment({ ...assignment, version: 2 }));
  assert.throws(() => scope.normalizeClaimScope({ type: "noop" }, assignment), /original multi-Claim/);
  assert.throws(() => scope.normalizeClaimScope({ type: "noop", claim_handle: "foreign" }, assignment));
  for (const field of ["assignment", "work_queue_assignment", "authorized", "run_attempt"]) assert.throws(() => scope.normalizeClaimScope({ type: "noop", claim_handle: assignment.claims[0].handle, [field]: true }, assignment));
  assert.equal(scope.normalizeClaimScope({ type: "noop" }, singleton.assignment).claim_handle, singleton.assignment.claims[0].handle);
  const first = assignment.claims[0];
  const second = assignment.claims[1];
  const collector = await probeCollector(assignment, artifacts, sourceFiles);
  const actor = { ...batch.workerActor, dispatch_id: assignment.dispatch_id, claim_handle: first.handle };
  policy.claimAuthority(batch.state, first.claim_id, actor, false);
  assert.throws(() => policy.claimAuthority(batch.state, first.claim_id, actor, true), /must be completed/);
  assert.throws(() => policy.claimAuthority(batch.state, first.claim_id, { ...actor, run_attempt: 2 }, false));
  const authorized = async request => ({ authorized: true, claim_handle: request.claim_handle });
  await scope.withClaimExecution({ assignment, claim_handle: first.handle, authorize: authorized }, async () => {
    await scope.assertClaimAuthorized({ type: "noop", claim_handle: first.handle });
    await assert.rejects(scope.assertClaimAuthorized({ type: "noop", claim_handle: second.handle }), /escape/);
    assert.throws(() => scope.withClaimExecution({ assignment, claim_handle: second.handle }, async () => {}), /nested execution/);
    await assert.rejects(scope.assertClaimAuthorized({ type: "create_issue", claim_handle: first.handle }, { effect: true, resource: {} }), /prohibits/);
  });
  const completedState = structuredClone(batch.state);
  completedState.claims.get(first.claim_id).state = "completed";
  completedState.works.get(first.work_id).state = "completed";
  policy.claimAuthority(completedState, first.claim_id, actor, true);
  assert.notEqual(completedState.works.get(first.work_id).state, "result");
  assert.throws(() => policy.claimAuthority(completedState, first.claim_id, { ...actor, principal: "999" }, true));
  completedState.claims.get(first.claim_id).state = "cancelled";
  assert.throws(() => policy.claimAuthority(completedState, first.claim_id, actor, true));
  assert.throws(() => scope.normalizeClaimScope({ type: "noop" }, assignment), /original multi-Claim/);
  const request = replay.newRequest("no-policy", "dispatch_next", batch.dispatcher, { pool: "default", max_claims: 3, max_dispatches: 3, max_bytes: 48 * 1024 });
  assert.throws(() => replay.generateRequestOperations(replay.newState(), request, batch.dispatcher, 1000, "commit"), /policy/i);
  const work = [...batch.state.works.values()][0];
  assert.throws(() => policy.validateSubmissionEntitlement(batch.state, work, { ...batch.administrator, role: "producer", principal: "999" }));
  const mixed = queueFixture({
    count: 2,
    batch: 2,
    configurePolicy: value => {
      value.pools.default.profiles.alternate = { ...value.pools.default.profiles.default, workflow: ".github/workflows/alternate.lock.yml" };
    },
    configureWork: (value, index) => {
      if (index === 1) value.worker_profile = "alternate";
    },
  });
  assert.equal(mixed.state.dispatches.size, 2);
  assert.ok([...mixed.state.dispatches.values()].every(dispatch => dispatch.claims.length === 1));
  const contract = { version: 1, outputs: [{ type: "create_issue", min: 1, max: 3 }] };
  delivery.validateDeliveryContract(contract);
  assert.throws(() => delivery.validateDeliveryContract({ version: 1, outputs: [{ type: "create_issue", min: 4, max: 3 }] }));
  const adapted = structuredClone(assignment);
  for (const claim of adapted.claims) claim.work.effect_contract = contract;
  const perClaimCounts = [];
  for (const claim of adapted.claims) {
    const messages = Array.from({ length: 3 }, (_, index) => ({ type: "create_issue", claim_handle: claim.handle, title: `local-${index}` }));
    const result = await delivery.inspectClaimDelivery({ assignment: adapted, claim_handle: claim.handle, authorize: authorized, messages, results: [] });
    assert.equal(result.verification, "unknown");
    assert.equal(result.reason, "Missing exact nondelegated delivery receipt");
    perClaimCounts.push(messages.length);
    const overflow = await delivery.inspectClaimDelivery({ assignment: adapted, claim_handle: claim.handle, authorize: authorized, messages: [...messages, messages[0]], results: [] });
    assert.equal(overflow.reason, "Immutable output cardinality contract was not met");
    const missing = await delivery.inspectClaimDelivery({ assignment: adapted, claim_handle: claim.handle, authorize: authorized, messages: [], results: [] });
    assert.equal(missing.reason, "Immutable output cardinality contract was not met");
    const nominalSuccess = await delivery.inspectClaimDelivery({
      assignment: adapted,
      claim_handle: claim.handle,
      authorize: authorized,
      messages,
      results: messages.map((_, messageIndex) => ({ messageIndex, success: true, result: {} })),
    });
    assert.equal(nominalSuccess.reason, "Independent delivery verifier unavailable");
    const facade = await delivery.verifyClaimDelivery(claim, { assignment: adapted, contract, attempt: 1, run: { run_id: "42", run_attempt: 1 }, success: true });
    assert.deepEqual(facade, { verified: false, effects: "unknown" });
  }
  return {
    assignmentProtocol: 3,
    singletonClaimCount: singleton.assignment.claims.length,
    permittedBatchClaimCount: assignment.claims.length,
    mixedProfileDispatchSizes: [...mixed.state.dispatches.values()].map(dispatch => dispatch.claims.length),
    frozenOriginal: true,
    foreignSelectorsBlocked: true,
    nestedClaimEscapeBlocked: true,
    agentAuthorityOverrideBlocked: true,
    nativeAttemptAndPrincipalChecked: true,
    completedAuthorityRequired: true,
    cancelledAuthorityBlocked: true,
    noWriteContractBlocksEffects: true,
    noPolicyDispatchBlocked: true,
    unauthorizedProducerBlocked: true,
    perClaimCounts,
    collector,
    totalProjectedOutputs: perClaimCounts.reduce((a, b) => a + b, 0),
    overflowAndMissingCountsBlocked: true,
    nominalSuccessDoesNotProveDelivery: true,
    diagnosticCaveat:
      "Positive completed state is a detached local fixture mutation to isolate claimAuthority; no durable Completion/Result was fabricated. Counts passed only the diagnostic cardinality barrier, not native effects or trusted delivery.",
  };
}

async function probeCollector(assignment, artifacts, sourceFiles) {
  const directory = mkdir(path.join(artifacts, "collector"));
  const filename = path.join(runtime, "collect_ndjson_output.cjs");
  sourceFiles.add(filename);
  const scope = require(path.join(runtime, "work_queue_claim_scope.cjs"));
  const constants = require(path.join(runtime, "constants.cjs"));
  const nativeRequire = createRequire(filename);
  const logs = [];
  const outputs = new Map();
  const core = {
    info: message => logs.push(message),
    warning: message => logs.push(message),
    error: message => logs.push(message),
    setFailed: message => logs.push(message),
    setOutput: (key, value) => outputs.set(key, value),
    exportVariable: () => {},
  };
  const validation = { create_issue: { defaultMax: 3, fields: { title: { required: true, type: "string" }, body: { required: true, type: "string" } } } };
  const environment = {
    ...process.env,
    RUNNER_TEMP: directory,
    GH_AW_SAFE_OUTPUTS: path.join(directory, "input.jsonl"),
    GH_AW_SAFE_OUTPUTS_CONFIG_PATH: path.join(directory, "config.json"),
    GH_AW_VALIDATION_CONFIG_PATH: path.join(directory, "validation.json"),
    GH_AW_VALIDATION_CONFIG: JSON.stringify(validation),
  };
  write(environment.GH_AW_SAFE_OUTPUTS_CONFIG_PATH, JSON.stringify({ create_issue: { min: 1, max: 3 } }));
  write(environment.GH_AW_VALIDATION_CONFIG_PATH, JSON.stringify(validation));
  const savedCore = global.core;
  const savedConfig = process.env.GH_AW_VALIDATION_CONFIG;
  const validator = nativeRequire("./safe_output_type_validator.cjs");
  const module = { exports: {} };
  // Execute unchanged collector code with explicit fixture authority and output
  // root. The legacy patch-directory existence probe never touches that path.
  vm.runInNewContext(
    fs.readFileSync(filename, "utf8"),
    {
      module,
      core,
      process: { env: environment },
      Buffer,
      TextDecoder,
      console,
      require: target => {
        if (target === "./work_queue_claim_scope.cjs") return { ...scope, readClaimScopeContext: () => ({ assignment }), normalizeRuntimeMessage: message => scope.normalizeClaimScope(message, assignment) };
        if (target === "./constants.cjs") return { ...constants, TMP_GH_AW_PATH: directory };
        if (target === "fs") return { ...fs, existsSync: name => (name === "/tmp/gh-aw" ? false : fs.existsSync(name)) };
        return nativeRequire(target);
      },
    },
    { filename, timeout: 2000 }
  );
  try {
    global.core = core;
    process.env.GH_AW_VALIDATION_CONFIG = JSON.stringify(validation);
    validator.resetValidationConfigCache();
    const members = assignment.claims;
    const messages = members.flatMap(claim => Array.from({ length: 3 }, (_, index) => ({ type: "create_issue", claim_handle: claim.handle, title: `fixture-${claim.handle}-${index}`, body: "inert local fixture" })));
    const collect = async (name, values) => {
      outputs.clear();
      write(environment.GH_AW_SAFE_OUTPUTS, values.map(value => JSON.stringify(value)).join("\n"));
      await module.exports.main();
      const output = JSON.parse(outputs.get("output"));
      write(path.join(directory, name + ".json"), JSON.stringify(output, null, 2) + "\n");
      return output.items;
    };
    const accepted = await collect("six-outputs", messages);
    assert.equal(accepted.filter(item => !item._claimScopeError).length, 6);
    const acceptedPerClaim = members.map(claim => accepted.filter(item => !item._claimScopeError && item.claim_handle === claim.handle).length);
    assert.deepEqual(acceptedPerClaim, [3, 3]);
    const overflow = await collect("overflow", [...messages.slice(0, 3), messages[0], ...messages.slice(3)]);
    assert.ok(overflow.some(item => item.claim_handle === members[0].handle && item._claimScopeError?.includes("Too many items")));
    assert.equal(overflow.filter(item => item.claim_handle === members[1].handle && !item._claimScopeError).length, 3);
    const missing = await collect("missing-sibling", messages.slice(0, 3));
    assert.ok(missing.some(item => item.claim_handle === members[1].handle && item._claimScopeError?.includes("Too few items")));
    return {
      acceptedPerClaim,
      overflowRejected: true,
      siblingMinimumRejected: true,
      caveat: "Actual collector main with fixture-supplied trusted assignment, artifact output root and nonexistent legacy patch directory; no live resource effect.",
    };
  } finally {
    global.core = savedCore;
    if (savedConfig === undefined) delete process.env.GH_AW_VALIDATION_CONFIG;
    else process.env.GH_AW_VALIDATION_CONFIG = savedConfig;
    validator.resetValidationConfigCache();
    write(path.join(directory, "execution.log"), logs.join("\n") + "\n");
  }
}
