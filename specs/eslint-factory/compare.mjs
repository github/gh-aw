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
// The repository's Go YAML parser (pkg/parser) via the developer fixture adapter; it builds that tool on load.
const metadataAdapter = path.join(root, "scripts/markdown_metadata_test_adapter.cjs");
const { readDocumentMetadata } = require(metadataAdapter);
const { ESLint } = require(require.resolve("eslint", { paths: [library] }));
export const sha256 = value => createHash("sha256").update(value).digest("hex");

function parseYAML(text, name) {
  let parsed;
  try {
    parsed = readDocumentMetadata(text);
  } catch (error) {
    assert.fail(`${name} must be unambiguous YAML: ${error instanceof Error ? error.message : String(error)}`);
  }
  return parsed.value;
}

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
  return { frontmatter: parseYAML(lines.slice(1, end).join("\n"), "Workflow frontmatter"), body: lines.slice(end + 1).join("\n") };
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
  expect("compiled policy approves every ESLint worker in pool default", facts.compiledPolicy.eslintWorkersApproved, [true, true, true]);
  expect("compiled default pool limits", facts.compiledPolicy.pool, { concurrency: 3, per_account_limit: 0, max_attempts: 3 });
  expect("dispatcher allowlist routes every ESLint worker", facts.dispatchProfiles, ["eslint-miner", "eslint-monster", "eslint-refiner"]);
  expect("dispatcher cohort grants one Claim per Dispatch", facts.claimsPerDispatch, [1, 1, 1]);
  expect("replay rejects a grouped Dispatch", facts.runtimeProbes.groupedDispatchError, "dispatch_state_conflict");
  expect("snapshot assignment carries exactly one Claim", facts.runtimeProbes.assignmentClaimCount, 1);
  expect("dispatcher worker allowlist", facts.dispatchWorkers, ["eslint-miner", "eslint-monster", "eslint-refiner"]);
  expect("dispatcher run dispatch budget", facts.dispatchMax, 3);
  expect("all workers require assignments", facts.requiredAssignments, [true, true, true]);
  expect("monster assignment safe-output maximum", facts.monsterAssignmentMax, 3);
  expect("refiner issue safe-output maximum", facts.refinerIssueMax, 3);
  expect("monster discussion safe-output maximum", facts.monsterDiscussionMax, 1);
  expect("hard HTTP response rule", facts.httpSeverity, "error");
  expect("other rules warn", facts.otherSeverities, ["warn"]);
  expect("native collector counts each singleton Claim separately", facts.runtimeProbes.collector.acceptedPerClaim, [3, 3]);
  expect("native collector rejects scoped overflow", facts.runtimeProbes.collector.overflowRejected, true);
  expect("native collector checks each Claim's minimum independently", facts.runtimeProbes.collector.minimumRejected, true);
  expect("native collector rejects a foreign claim_handle", facts.runtimeProbes.collector.foreignHandleRejected, true);
  expect("native collector rejects a multi-member assignment", facts.runtimeProbes.collector.multiMemberRejected, true);
  expect("native collector preserves one structured memory per Claim", facts.runtimeProbes.collector.memoryPerClaim, [1, 1]);
  expect("native collector rejects same-Claim memory overflow", facts.runtimeProbes.collector.memoryOverflowRejected, true);
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

  const boundaryFunctions = {};
  for (const [file, names] of [
    ["work_queue_claim_scope.cjs", ["normalizeAssignment", "normalizeClaimScope", "withClaimExecution", "assertClaimAuthorized", "claimIdentity"]],
    ["work_queue_policy.cjs", ["validatePolicyEnv", "readWorkQueuePolicyEnv", "policyAllowsWorker", "actorFromContext"]],
    ["work_queue_intents.cjs", ["normalizeSubmitParameters"]],
    ["work_queue_scheduler.cjs", ["planDispatch", "assignmentForDispatch"]],
    ["work_queue_replay.cjs", ["applyClaim", "openClaim"]],
    ["work_queue_dispatch.cjs", ["dispatchQueueIntent"]],
    ["finish_work_queue_claim.cjs", ["authorizeWorkerClaim", "assertEffectScope"]],
  ]) {
    const text = read(path.join(runtime, file));
    boundaryFunctions[file] = Object.fromEntries(names.map(name => [name, executableFunction(text, name)]));
  }
  // The compiled dispatcher policy and control config, with `${{ github.repository }}` bound to the fixture repository.
  const compiledDispatcher = parseYAML(read(path.join(root, ".github/workflows/eslint-factory-dispatcher.lock.yml")), "Compiled dispatcher");
  const controls = compiledDispatcher.jobs.safe_outputs.steps.find(step => step.id === "work_queue_controls");
  assert.ok(controls?.env, "Compiled dispatcher must supply its trusted work_queue_controls step");
  const controlEnvironment = Object.fromEntries(Object.entries(controls.env).map(([key, value]) => [key, typeof value === "string" ? value.replaceAll("${{ github.repository }}", "owner/repo") : value]));
  const runtimeProbes = await probeRuntime(sourceFiles, artifacts, controlEnvironment);
  const compiledRefiner = parseYAML(read(path.join(root, ".github/workflows/eslint-refiner.lock.yml")), "Compiled refiner");
  const preparation = compiledRefiner.jobs.work_queue_prepare_persist_eslint_memory_0.steps.find(step => step.id === "claim_adapter_context");
  assert.ok(preparation, "Declarative memory must lower to the protected preparation job");
  const adapter = JSON.parse(preparation.env.GH_AW_CLAIM_ADAPTER_CONFIG);
  const memory = workflows.refiner.frontmatter.tools["work-queue"].memory;
  assert.equal(memory.name, "persist_eslint_memory");
  assert.equal(adapter["target-repo"], memory["target-repo"]);
  assert.equal(adapter["git-tree"]["base-revision"], memory["base-revision"]);
  assert.equal(adapter["git-tree"]["branch-prefix"], memory["branch-prefix"]);
  const facts = {
    sourceRules,
    runtimeRules: Object.keys(plugin.rules).sort(),
    configRules: configuredRules.map(([name]) => name.replace("gh-aw-custom/", "")).sort(),
    httpSeverity: Object.fromEntries(configuredRules)["gh-aw-custom/require-http-response-error-listener"],
    otherSeverities: [...new Set(configuredRules.filter(([name]) => !name.endsWith("/require-http-response-error-listener") && !name.endsWith("/no-ad-hoc-jsonl-parser")).map(([, severity]) => severity))],
    packageScripts: manifest.scripts,
    dispatchWorkers: [...workflows["factory-dispatcher"].frontmatter["safe-outputs"]["dispatch-workflow"].workflows].sort(),
    dispatchMax: workflows["factory-dispatcher"].frontmatter["safe-outputs"]["dispatch-workflow"].max,
    requiredAssignments: ["miner", "refiner", "monster"].map(name => workflows[name].frontmatter.tools["work-queue"]["require-assignment"]),
    monsterAssignmentMax: workflows.monster.frontmatter["safe-outputs"]["assign-to-agent"].max,
    monsterDiscussionMax: workflows.monster.frontmatter["safe-outputs"]["create-discussion"].max,
    refinerIssueMax: workflows.refiner.frontmatter["safe-outputs"]["create-issue"].max,
    compiledPolicy: runtimeProbes.compiledPolicy,
    dispatchProfiles: runtimeProbes.dispatchProfiles,
    claimsPerDispatch: runtimeProbes.claimsPerDispatch,
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
    "The compiled dispatcher policy and control config are read from the checked-in lock with the repository bound to a fixture; queue-branch write access, immutable worker revisions, resource scope and credentials are externally provisioned; these four workflow sources do not install a miner->refiner->monster DAG.",
    "The model admits exactly two root Works with satisfied dependencies/capacity and fixed equal-priority order, and permits at most one protected prefix request in a modeled run.",
    "v4 replay grants exactly one Claim per Dispatch. The actual planner grants the three-profile cohort as three singleton Dispatches and replay rejects a grouped Dispatch; the finite model's Batch configurations are a legacy abstraction, not v4 behavior. The model projects the first compatible assignment of a request, not all native dispatches.",
    "Dispatcher prompt obligations max_claims=3/max_dispatches=3 and at most one request are not inferred to be a runtime-enforced per-run request counter.",
    "Model Effect authority abstracts successful open-Claim/bound-run-attempt/compiled-worker-approval/effect_scope checks; local probes use an in-memory queue branch and do not exercise authentication, remote APIs or concurrency.",
    "Each modeled Claim projects one output family. Whole-contract Readback abstracts all other declared outputs, memory snapshots, resource receipts and durable queue-control inventory; no profile contract is supplied by these workflow sources.",
    "Three total Copilot assignments, quality/build/test/low-false-positive bars, nonduplicate issues, and final-action wording are agent obligations; one Claim per Dispatch does not imply run-wide worker caps.",
    "Completion is distinct from verified delivery and Result. The v4 runtime has no independent delivery verifier; collector cardinality and authorization probes do not establish native writes.",
    "Warning-only diagnostics produce ESLint exit zero and monster lint-clean. Scan coverage is CJS excluding tests, not generic JS/TS goals; npm ci/build/tool failure is not evidence of defect-free source.",
    "No unbounded liveness, production refinement, AST-rule completeness, low false positives or comprehensive security guarantee is claimed.",
  ];
  const documentationFindings = [
    { category: "coverage", observation: "README goals and miner mission mention JS/TS; actual configured rules apply to non-test CJS. This is a scope distinction, not proof that all JS/TS is scanned." },
    {
      category: "warning-short-circuit",
      observation: "Monster's clean branch discards diagnostics for successful lint, including warning-only findings. Most configured rules warn; no-ad-hoc-jsonl-parser and require-http-response-error-listener are errors.",
    },
    { category: "prompt-ambiguity", observation: "Miner processes its one assigned Claim, then calls exactly one last create_pull_request/noop. The model does not turn that final-action prose into a global runtime output limit." },
    { category: "delegation", observation: "assign-to-agent delivery means the declared assignment was independently verified, not that a downstream Copilot remediation build/test succeeded." },
    {
      category: "rule-table",
      observation: "README linked rule table coverage and existing section targets are checked against registered source rules; this is not a rule-semantics audit.",
      missing: sourceRules.filter(name => !readmeRules.rules.includes(name)),
      brokenTargets: readmeRules.brokenTargets,
    },
  ];
  for (const filename of Object.keys(require.cache)) if (filename.startsWith(runtime + path.sep) && !filename.includes(path.sep + "node_modules" + path.sep)) sourceFiles.add(filename);
  sourceFiles.add(metadataAdapter);
  for (const dependency of ["typescript", "eslint"]) {
    let directory = path.dirname(require.resolve(dependency, { paths: [library] }));
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
    tool: { node: process.version, typescript: ts.version, eslint: ESLint.version, yaml: `${path.relative(root, metadataAdapter)} (pkg/parser)` },
    precheckTransformation: "Only /tmp/gh-aw/agent paths rebased into artifact fixtures; npm stub supplies measured scan exit/status or explicit failure. No npm ci, workflow run, network or native write executed.",
  };
  assert.equal(checkoutIdentity().head, checkout.head, "HEAD changed during comparison; rerun against a stable checkout");
  write(path.join(artifacts, "comparison.json"), JSON.stringify(report, null, 2) + "\n");
  return report;
}

async function probeRuntime(sourceFiles, artifacts, controlEnvironment) {
  const load = file => {
    const filename = path.join(runtime, file);
    sourceFiles.add(filename);
    return require(filename);
  };
  const scope = load("work_queue_claim_scope.cjs");
  const policyModule = load("work_queue_policy.cjs");
  const replay = load("work_queue_replay.cjs");
  const scheduler = load("work_queue_scheduler.cjs");
  const { normalizeSubmitParameters } = load("work_queue_intents.cjs");
  const { allowedWorkerWorkflows, dispatchQueueIntent } = load("work_queue_dispatch.cjs");
  const { authorizeWorkerClaim } = load("finish_work_queue_claim.cjs");
  const { ESLINT_WORKERS, buildESLintFactoryPlan } = load("eslint_factory_portfolio.cjs");
  const queue = load("work_queue_test_helpers.cjs");
  const errorCode = action => {
    try {
      action();
    } catch (error) {
      return error.code;
    }
    assert.fail("Expected the v4 boundary to reject");
  };
  const rejectedCode = async action => {
    try {
      await action();
    } catch (error) {
      return error.code;
    }
    assert.fail("Expected the v4 boundary to reject");
  };

  // The compiled policy is the only policy: no ambient default, a closed shape, and the repository bound by the compiler.
  assert.equal(policyModule.readWorkQueuePolicyEnv(process.env), null, "Run the comparison without an ambient compiled policy");
  const policy = policyModule.readWorkQueuePolicyEnv(controlEnvironment);
  assert.ok(policy, "Compiled dispatcher must carry GH_AW_WORK_QUEUE_POLICY");
  assert.equal(policyModule.validatePolicyEnv(policy), policy);
  assert.equal(policy.effect_scope, queue.repository);
  assert.equal(
    errorCode(() => policyModule.validatePolicyEnv({ ...policy, pools: { ...policy.pools, default: { ...policy.pools.default, default_profile: "eslint-miner" } } })),
    "policy_invalid"
  );
  const controlConfig = JSON.parse(controlEnvironment.GH_AW_WORK_QUEUE_CONTROL_CONFIG);
  const allowed = allowedWorkerWorkflows(controlConfig);
  const profiles = Object.fromEntries(policy.workers.filter(worker => allowed.has(policyModule.workerWorkflow(worker))).map(worker => [worker, { workflow: policyModule.workerWorkflow(worker) }]));

  // The dispatcher's staged cohort: submission resolves each node to an approved worker; an unapproved one is rejected.
  const plan = buildESLintFactoryPlan({ date: "2026-11-23", repository: queue.repository, repositoryId: "1" });
  const works = normalizeSubmitParameters({ nodes: plan.nodes }, policy);
  assert.equal(
    errorCode(() => normalizeSubmitParameters({ nodes: [{ ...plan.nodes[0], worker_profile: "unapproved-worker" }] }, policy)),
    "work_queue_profile_not_approved"
  );
  const submitted = queue.submit([], works);
  const pool = policyModule.poolEnv(policy, plan.dispatch.pool);
  const parameters = { ...plan.dispatch, concurrency: pool.concurrency, per_account_limit: pool.per_account_limit, profiles };
  const planned = queue.dispatchNext(submitted, parameters);
  let log = planned.log;
  let state = replay.replayTransactions(log);
  const claimsPerDispatch = planned.decision.dispatches.map(id => state.dispatches.get(id).claims.length);

  // Replay, not just the planner, refuses to group the request's Claims into one Dispatch.
  const base = replay.replayTransactions(submitted);
  const request = replay.newRequest("dispatch_next", queue.dispatcher, parameters);
  const grants = scheduler.planDispatch(base, parameters, request.id).operations;
  assert.ok(grants.length >= 2, "Grouping probe needs two grants");
  const groupedDispatchError = errorCode(() =>
    replay.appendCommit(
      base,
      replay.buildCommit(
        base,
        queue.dispatcher,
        request,
        grants.map(grant => ({ ...grant, dispatch_id: grants[0].dispatch_id })),
        1
      )
    )
  );

  // Launch and bind two singleton Dispatches; both members use handle h1.
  const [first, second] = planned.decision.dispatches;
  const runs = { [first]: "200", [second]: "201" };
  const workerWorkflow = id => policyModule.workerWorkflow(state.dispatches.get(id).worker_profile);
  for (const id of [first, second]) {
    log = queue.launch(log, id, true, queue.dispatcher, runs[id]);
    log = queue.bind(log, id, queue.workerActor(runs[id], 1, workerWorkflow(id)));
  }
  state = replay.replayTransactions(log);
  const assignment = scope.normalizeAssignment(scheduler.assignmentForDispatch(state, first));
  const sibling = scope.normalizeAssignment(scheduler.assignmentForDispatch(state, second));
  const [member] = assignment.claims;
  assert.ok(Object.isFrozen(assignment) && Object.isFrozen(assignment.claims) && Object.isFrozen(member.work));
  assert.equal(member.handle, sibling.claims[0].handle, "Distinct Dispatches may reuse a handle");
  for (const invalid of [
    { ...assignment, claims: [member, sibling.claims[0]] },
    { ...assignment, claims: [] },
    { ...assignment, version: 4 },
  ])
    assert.equal(
      errorCode(() => scope.normalizeAssignment(invalid)),
      "claim_scope_invalid"
    );

  // Selection: omission picks the one member; foreign selectors and agent-supplied authority fail.
  assert.equal(scope.normalizeClaimScope({ type: "noop" }, assignment).claim_handle, member.handle);
  assert.throws(() => scope.normalizeClaimScope({ type: "noop", claim_handle: "foreign" }, assignment), /foreign/);
  assert.throws(() => scope.normalizeClaimScope({ type: "noop", claim_id: sibling.claims[0].claim_id }, assignment), /conflicts/);
  for (const field of ["assignment", "work_queue_assignment", "authorized", "run_attempt"]) assert.throws(() => scope.normalizeClaimScope({ type: "noop", [field]: true }, assignment), /trusted authority/);
  // The full identity, not the reused handle, keys per-Claim artifacts.
  assert.notDeepEqual(scope.claimIdentity(member.handle, assignment), scope.claimIdentity(member.handle, sibling));
  assert.notEqual(scope.claimArtifactPath(artifacts, member.handle, assignment), scope.claimArtifactPath(artifacts, member.handle, sibling));

  const authorized = async request => ({ authorized: true, claim_handle: request.claim_handle });
  await scope.withClaimExecution({ assignment, claim_handle: member.handle, authorize: authorized }, async () => {
    await scope.assertClaimAuthorized({ type: "noop" });
    await assert.rejects(scope.assertClaimAuthorized({ type: "noop", claim_handle: "foreign" }), /foreign/);
    assert.throws(() => scope.withClaimExecution({ assignment: sibling, claim_handle: member.handle }, async () => {}), /nested execution/);
  });
  assert.throws(() => scope.withClaimExecution({ assignment, claim_handle: "foreign" }, async () => {}), /foreign Claim/);
  const noWrites = structuredClone(assignment);
  noWrites.claims[0].work.effect_contract = { kind: "none" };
  await scope.withClaimExecution({ assignment: noWrites, claim_handle: member.handle, authorize: authorized }, () => assert.rejects(scope.assertClaimAuthorized({ type: "create_issue" }, { effect: true, resource: {} }), /prohibits/));

  const collector = await probeCollector([assignment, sibling], artifacts, sourceFiles);

  // Native authorization against the replayed ledger on an in-memory queue branch.
  const github = queue.fakeQueueGitHub(queue.ledgerText(log));
  const core = { info() {}, warning() {}, debug() {}, error() {} };
  const workerContext = (runId, runAttempt = 1) => ({ repo: { owner: "owner", repo: "repo" }, runId: Number(runId), runAttempt, payload: {} });
  const authorize = (target, options = {}) =>
    authorizeWorkerClaim({ githubClient: github.githubClient, context: workerContext(runs[target.dispatch_id]), assignment: target, claim_handle: target.claims[0].handle, policy, core, ...options });
  const completedIntent = new Map([[member.handle, "completed"]]);
  assert.equal((await authorize(assignment, { requireCompletion: false })).authorized, true);
  assert.equal((await authorize(assignment, { finishIntents: new Map() })).state, "failed");
  assert.equal((await authorize(assignment, { finishIntents: completedIntent })).authorized, true);
  assert.equal((await authorize(assignment, { context: workerContext(runs[first], 2), requireCompletion: false })).state, "unbound");
  const unapproved = { ...policy, workers: policy.workers.filter(worker => worker !== state.dispatches.get(first).worker_profile) };
  assert.equal(await rejectedCode(() => authorize(assignment, { requireCompletion: false, policy: unapproved })), "E004");
  assert.equal(await rejectedCode(() => authorize(assignment, { requireCompletion: false, resource: { repository: "other/repo" } })), "E004");

  // Real finish commits settle exactly their own Dispatch; settled Claims are suppressed, never authorized.
  log = queue.finish(log, first, queue.workerActor(runs[first], 1, workerWorkflow(first)));
  log = queue.finish(log, second, queue.workerActor(runs[second], 1, workerWorkflow(second)), { [sibling.claims[0].handle]: "failed" });
  github.install(queue.ledgerText(log));
  state = replay.replayTransactions(log);
  assert.equal(state.dispatches.get(first).state, "closed");
  assert.deepEqual(scheduler.assignmentForDispatch(state, first).claims, []);
  const completed = await authorize(assignment, { finishIntents: completedIntent });
  assert.deepEqual([completed.authorized, completed.suppressed, completed.state], [false, true, "completed"]);
  const cancelled = await authorize(sibling, { requireCompletion: false });
  assert.deepEqual([cancelled.authorized, cancelled.suppressed, cancelled.state], [false, true, "cancelled"]);

  // The dispatcher publishes nothing without a compiled policy or an allowlisted worker.
  const writes = github.state.writes;
  const dispatch = options =>
    dispatchQueueIntent({
      githubClient: github.githubClient,
      context: { repo: { owner: "owner", repo: "repo" }, runId: 100, runAttempt: 1, payload: {} },
      role: "dispatcher",
      core,
      config: controlConfig,
      message: { type: "work_queue_dispatch_next", ...plan.dispatch },
      remainingDispatches: plan.dispatch.max_dispatches,
      ...options,
    });
  assert.equal(await rejectedCode(() => dispatch({ policy: null })), "policy_missing");
  assert.equal(await rejectedCode(() => dispatch({ policy, config: { ...controlConfig, work_queue_workflows: [] } })), "work_queue_worker_not_allowlisted");
  assert.equal(github.state.writes, writes);

  return {
    assignmentProtocol: 4,
    compiledPolicy: { pool, eslintWorkersApproved: ESLINT_WORKERS.map(worker => policyModule.policyAllowsWorker(policy, plan.dispatch.pool, worker)) },
    dispatchProfiles: Object.keys(profiles).sort(),
    claimsPerDispatch,
    groupedDispatchError,
    assignmentClaimCount: assignment.claims.length,
    frozenOriginal: true,
    multiMemberAssignmentBlocked: true,
    foreignSelectorsBlocked: true,
    reusedHandleIdentitiesDistinct: true,
    nestedClaimEscapeBlocked: true,
    agentAuthorityOverrideBlocked: true,
    boundRunAttemptChecked: true,
    completedIntentRequired: true,
    workerApprovalAndEffectScopeChecked: true,
    settledClaimsSuppressed: true,
    noWriteContractBlocksEffects: true,
    unapprovedProfileBlocked: true,
    noPolicyDispatchBlocked: true,
    unallowlistedDispatchBlocked: true,
    collector,
    diagnosticCaveat:
      "Authorization replays the real ledger from an in-memory queue branch with a fixture repository; Completion and ClaimCancellation are real finish commits, not mutated projections. No remote API, credential or native write is exercised.",
  };
}

async function probeCollector(assignments, artifacts, sourceFiles) {
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
  // The trusted assignment of the run being collected; each run owns exactly one Claim.
  let assignment = assignments[0];
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
    const collect = async (target, name, values) => {
      assignment = target;
      outputs.clear();
      write(environment.GH_AW_SAFE_OUTPUTS, values.map(value => JSON.stringify(value)).join("\n"));
      await module.exports.main();
      const output = JSON.parse(outputs.get("output"));
      write(path.join(directory, name + ".json"), JSON.stringify(output, null, 2) + "\n");
      return output.items;
    };
    const accepted = items => items.filter(item => !item._claimScopeError);
    const issues = target => Array.from({ length: 3 }, (_, index) => ({ type: "create_issue", title: `fixture-${target.dispatch_id}-${index}`, body: "inert local fixture" }));
    // Two singleton Dispatches that reuse handle h1 are collected in their own runs.
    const acceptedPerClaim = [];
    for (const [index, target] of assignments.entries()) {
      const items = await collect(target, `issues-${index}`, issues(target));
      assert.ok(accepted(items).every(item => item.claim_handle === target.claims[0].handle));
      acceptedPerClaim.push(accepted(items).length);
    }
    const [first, second] = assignments;
    const overflow = await collect(first, "overflow", [...issues(first), issues(first)[0]]);
    assert.ok(overflow.some(item => item._claimScopeError?.includes("Too many items")));
    assert.equal(accepted(overflow).length, 3);
    // A foreign selector is rejected and does not count toward the Claim's minimum.
    const foreign = await collect(second, "foreign-handle", [{ ...issues(second)[0], claim_handle: "foreign" }]);
    assert.equal(accepted(foreign).length, 0);
    const foreignHandleRejected = foreign.some(item => item.claim_handle === "foreign" && /foreign/.test(item._claimScopeError));
    const minimumRejected = foreign.some(item => item.claim_handle === second.claims[0].handle && item._claimScopeError?.includes("Too few items"));
    // A snapshot assignment with two members executes nothing.
    const multiMember = await collect({ dispatch_id: first.dispatch_id, claims: [first.claims[0], second.claims[0]] }, "multi-member", issues(first));
    const multiMemberRejected = multiMember.length > 0 && accepted(multiMember).length === 0;
    const compiledFile = path.join(root, ".github/workflows/eslint-refiner.lock.yml");
    sourceFiles.add(compiledFile);
    const compiled = parseYAML(fs.readFileSync(compiledFile, "utf8"), "Compiled refiner");
    const configStep = compiled.jobs.agent.steps.find(step => step.env?.GH_AW_SAFE_OUTPUTS_CONFIG);
    assert.ok(configStep, "Compiled refiner must supply its collector configuration");
    const memoryConfig = JSON.parse(configStep.env.GH_AW_SAFE_OUTPUTS_CONFIG).persist_eslint_memory;
    assert.equal(memoryConfig.max, 1);
    assert.equal(memoryConfig.inputs.memory.type, "object");
    write(environment.GH_AW_SAFE_OUTPUTS_CONFIG_PATH, JSON.stringify({ persist_eslint_memory: memoryConfig }));
    const memory = target => ({ type: "persist_eslint_memory", memory: { work_id: target.claims[0].work_id, strategy: "inert fixture", findings: [], metrics: {}, next_actions: [] } });
    const memoryPerClaim = [];
    for (const [index, target] of assignments.entries()) {
      const items = await collect(target, `memory-${index}`, [memory(target)]);
      assert.deepEqual(
        accepted(items).map(item => item.memory),
        [memory(target).memory]
      );
      memoryPerClaim.push(accepted(items).length);
    }
    const excessMemory = await collect(first, "memory-overflow", [memory(first), memory(first)]);
    assert.ok(excessMemory.some(item => item._claimScopeError?.includes("Too many items")));
    assert.equal(accepted(excessMemory).length, 1);
    return {
      acceptedPerClaim,
      overflowRejected: true,
      minimumRejected,
      foreignHandleRejected,
      multiMemberRejected,
      memoryPerClaim,
      memoryOverflowRejected: true,
      caveat: "Actual collector main once per singleton assignment with fixture-supplied trusted assignment, artifact output root and nonexistent legacy patch directory; no live resource effect.",
    };
  } finally {
    global.core = savedCore;
    if (savedConfig === undefined) delete process.env.GH_AW_VALIDATION_CONFIG;
    else process.env.GH_AW_VALIDATION_CONFIG = savedConfig;
    validator.resetValidationConfigCache();
    write(path.join(directory, "execution.log"), logs.join("\n") + "\n");
  }
}
