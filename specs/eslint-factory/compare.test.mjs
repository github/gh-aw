import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { test } from "node:test";
import { analyze, compareFacts, registration, workflow, root, run, sha256, checkoutIdentity, readmeTable } from "./compare.mjs";
import { cases, configuration } from "./cases.mjs";
import { parseTrace, parseValue, validateTrace } from "./trace.mjs";

test("source-bound registration reads syntax, not matching rule-name strings", () => {
  const text = fs.readFileSync(path.join(root, "eslint-factory/src/index.ts"), "utf8");
  assert.deepEqual(registration(text + '\n// "imaginary-registered-rule": imaginaryRule\n'), registration(text));
  const changed = text.replace('"no-throw-plain-object": noThrowPlainObjectRule', '"renamed-rule": noThrowPlainObjectRule');
  assert.notDeepEqual(registration(changed), registration(text));
});

test("workflow facts come from parsed frontmatter, not body examples", () => {
  const text = fs.readFileSync(path.join(root, ".github/workflows/eslint-monster.md"), "utf8");
  const parsed = workflow(text + "\n```yaml\nassign-to-agent:\n  max: 900\n```\n");
  assert.equal(parsed.frontmatter["safe-outputs"]["assign-to-agent"].max, 3);
  assert.throws(() => workflow("---\nname: one\nname: two\n---\n"), /unambiguous/);
});

test("README rule-table additions link existing sections and detect omitted or broken rows", () => {
  const text = fs.readFileSync(path.join(root, "eslint-factory/README.md"), "utf8");
  const table = readmeTable(text);
  assert.deepEqual(table.brokenTargets, []);
  for (const rule of ["no-async-foreach-callback", "no-single-char-string-replace", "no-string-fallback-for-non-string-message", "require-http-response-error-listener"]) {
    assert.equal(table.rules.filter(name => name === rule).length, 1);
    assert.ok(text.includes(`[\`${rule}\`](#${rule})`));
    const broken = text.replace(`[\`${rule}\`](#${rule})`, `[\`${rule}\`](#nonexistent-rule-section)`);
    assert.deepEqual(readmeTable(broken).brokenTargets, [{ rule, target: "#nonexistent-rule-section" }]);
    const omitted = text
      .split("\n")
      .filter(line => !line.startsWith(`| [\`${rule}\`]`))
      .join("\n");
    assert.ok(omitted.includes(`### \`${rule}\``), "The section still exists; only table coverage is removed");
    assert.ok(!readmeTable(omitted).rules.includes(rule));
  }
});

test("actual build, library scan commands, precheck and native Claim boundaries", async () => {
  const artifacts = process.env.FACTORY_ARTIFACTS || path.join(root, ".eslint-factory-formal-artifacts", "tests");
  const report = await analyze(artifacts);
  assert.equal(report.checkout.head, checkoutIdentity().head);
  assert.deepEqual(report.mismatches, []);
  const mutations = [
    facts => (facts.httpSeverity = "warn"),
    facts => (facts.scanProbes.warning.status = 1),
    facts => (facts.coverage.ts = true),
    facts => (facts.precheck.warning.flag = false),
    facts => (facts.defaultProfileMax = 16),
    facts => (facts.monsterAssignmentMax = 2),
    facts => (facts.requiredAssignments[0] = false),
    facts => (facts.memoryAdapter.base = "b".repeat(40)),
    facts => facts.configRules.pop(),
    facts => facts.runtimeRules.push("unregistered-rule"),
    facts => (facts.runtimeProbes.collector.acceptedPerClaim[1] = 0),
    facts => facts.readmeRuleLinks.pop(),
    facts => facts.readmeBrokenTargets.push({ rule: "require-http-response-error-listener", target: "#missing" }),
  ];
  for (const mutate of mutations) {
    const changed = structuredClone(report.facts);
    mutate(changed);
    assert.ok(compareFacts(changed).length, "Changed semantic fact must fail comparison");
  }
  const directory = path.join(artifacts, "library-fixture/eslint-factory");
  const changedConfig = fs.readFileSync(path.join(root, "eslint-factory/eslint.config.cjs"), "utf8").replace('"gh-aw-custom/require-http-response-error-listener": "error"', '"gh-aw-custom/require-http-response-error-listener": "warn"');
  assert.notEqual(changedConfig, fs.readFileSync(path.join(root, "eslint-factory/eslint.config.cjs"), "utf8"));
  fs.writeFileSync(path.join(directory, "mutated.config.cjs"), changedConfig);
  const changedScan = run(
    process.execPath,
    [path.join(root, "eslint-factory/node_modules/eslint/bin/eslint.js"), "--config", path.join(directory, "mutated.config.cjs"), path.join(artifacts, "library-fixture/actions/setup/js/nested/error.cjs")],
    { cwd: path.join(artifacts, "library-fixture/actions/setup/js") }
  );
  assert.equal(changedScan.status, 0, changedScan.stdout + changedScan.stderr);
  assert.ok(changedScan.stdout.includes("gh-aw-custom/require-http-response-error-listener"), "Mutated fixture must actually be scanned");
  const changedFacts = structuredClone(report.facts);
  changedFacts.scanProbes.error.status = changedScan.status;
  assert.ok(compareFacts(changedFacts).some(mismatch => mismatch.label === "hard-error ESLint exits nonzero"));
  fs.writeFileSync(path.join(artifacts, "mutated-rule-severity.log"), changedScan.stdout + changedScan.stderr);
});

test("finite configurations include exact negative controls and guarded witnesses", () => {
  assert.equal(new Set(cases.map(entry => entry.name)).size, cases.length);
  for (const entry of cases) {
    assert.ok(configuration(entry).includes("CHECK_DEADLOCK FALSE"));
    if (entry.name.startsWith("negative-")) assert.ok(entry.invariants.includes(entry.expected));
    if (entry.name.startsWith("witness-")) {
      assert.equal(entry.constants.Fault, "none");
      assert.equal(entry.expected, "NeverWitness");
    }
  }
});

test("trace parser rejects unsupported values; generated traces are source-bound and replayed", () => {
  assert.deepEqual(parseValue('<<"completed", -1, TRUE>>'), ["completed", -1, true]);
  assert.throws(() => parseValue("UNSUPPORTED"));
  const artifacts = process.env.FACTORY_MODEL_ARTIFACTS;
  if (!artifacts) return;
  const validation = JSON.parse(fs.readFileSync(path.join(artifacts, "validation.json"), "utf8"));
  for (const entry of cases.filter(entry => entry.expected !== "exhaustive")) {
    const directory = path.join(artifacts, entry.name);
    const document = JSON.parse(fs.readFileSync(path.join(directory, "trace.json"), "utf8"));
    assert.equal(document.source.checkoutHead, validation.checkout.head);
    assert.equal(document.source.originMain, validation.checkout.originMain);
    const trace = parseTrace(fs.readFileSync(path.join(directory, "tlc.log"), "utf8"));
    assert.deepEqual(document.trace, trace);
    for (const [field, filename] of [
      ["modelSha256", "Factory.tla"],
      ["configSha256", "Factory.cfg"],
      ["logSha256", "tlc.log"],
    ])
      assert.equal(document.source[field], sha256(fs.readFileSync(path.join(directory, filename))));
    validateTrace(trace, entry);
    const changed = structuredClone(trace);
    changed.at(-1).state.requested = !changed.at(-1).state.requested;
    assert.throws(() => validateTrace(changed, entry), /outside/);
  }
});
