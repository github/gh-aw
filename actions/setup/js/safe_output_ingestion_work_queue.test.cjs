import { describe, it } from "vitest";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const { queueFixture } = require("./work_queue_lifecycle.test_helpers.cjs");
const { temporaryDirectory } = require("./work_queue_effect_test_helpers.cjs");
const claimScope = require("./work_queue_claim_scope.cjs");
const constants = require("./constants.cjs");
const ENV_KEYS = ["RUNNER_TEMP", "GH_AW_AGENT_OUTPUT", "GH_AW_SAFE_OUTPUTS", "GH_AW_SAFE_OUTPUTS_CONFIG_PATH", "GH_AW_VALIDATION_CONFIG_PATH", "GH_AW_VALIDATION_CONFIG", "GH_AW_WORK_QUEUE_ENABLED", "GH_AW_WORK_QUEUE_ROLE"];
const validation = {
  noop: { defaultMax: 1, fields: { message: { required: true, type: "string" } } },
  create_issue: { defaultMax: 1, fields: { title: { required: true, type: "string" }, body: { required: true, type: "string" } } },
};

async function fixture(options, callback) {
  const root = temporaryDirectory("safe-output-ingestion");
  const savedEnv = ENV_KEYS.map(key => process.env[key]);
  const savedCore = global.core;
  const savedContext = global.context;
  const modules = ["./constants.cjs", "./work_queue_claim_scope.cjs", "./collect_ndjson_output.cjs", "./load_agent_output.cjs"].map(name => require.resolve(name));
  const savedModules = modules.map(name => require.cache[name]);
  const exists = fs.existsSync;
  const readdir = fs.readdirSync;
  const outputs = new Map();
  const warnings = [];
  const errors = [];
  const failures = [];
  const queue = options.claims ? queueFixture({ count: options.claims }) : null;
  let normalizationCalls = 0;
  try {
    const constantsModule = require.cache[modules[0]];
    const scopeModule = require.cache[modules[1]];
    assert.ok(constantsModule);
    assert.ok(scopeModule);
    require.cache[modules[0]] = { ...constantsModule, exports: { ...constants, TMP_GH_AW_PATH: root } };
    require.cache[modules[1]] = {
      ...scopeModule,
      exports: {
        ...claimScope,
        readClaimScopeContext: () => {
          if (options.privateExecution) throw new Error("Trusted execution must not reload ambient scope");
          return queue && !options.runtimeScopeDisabled ? { assignment: queue.assignment } : null;
        },
        currentClaimHandle: () => (options.privateExecution ? claimScope.currentClaimHandle() : options.handle || null),
        currentClaimAssignment: () => (options.privateExecution ? claimScope.currentClaimAssignment() : options.handle ? queue?.assignment : undefined),
        normalizeRuntimeMessage: item => {
          normalizationCalls++;
          assert.ok(queue, "Disabled ingestion must not invoke queue normalization");
          if (options.privateExecution) return claimScope.normalizeRuntimeMessage(item);
          return claimScope.normalizeClaimScope(item, queue.assignment);
        },
      },
    };
    delete require.cache[modules[2]];
    delete require.cache[modules[3]];
    global.core = new Proxy(
      {
        ...savedCore,
        info: () => {},
        warning: message => warnings.push(message),
        error: message => errors.push(message),
        setFailed: message => failures.push(message),
        setOutput: (key, value) => outputs.set(key, value),
        exportVariable: () => {},
      },
      { get: (target, name) => target[name] || (() => {}) }
    );
    global.context = {
      ...savedContext,
      eventName: "workflow_dispatch",
      repo: { owner: "owner", repo: "repo" },
      payload: { repository: { full_name: "owner/repo", name: "repo", owner: { login: "owner" } } },
    };
    fs.existsSync = filename => (filename === "/tmp/gh-aw" ? Boolean(options.patches?.length) : exists(filename));
    fs.readdirSync = new Proxy(readdir, {
      apply: (target, receiver, args) => (args[0] === "/tmp/gh-aw" ? options.patches || [] : Reflect.apply(target, receiver, args)),
    });
    process.env.RUNNER_TEMP = root;
    process.env.GH_AW_SAFE_OUTPUTS = path.join(root, "source.jsonl");
    process.env.GH_AW_AGENT_OUTPUT = path.join(root, constants.AGENT_OUTPUT_FILENAME);
    process.env.GH_AW_SAFE_OUTPUTS_CONFIG_PATH = path.join(root, "config.json");
    process.env.GH_AW_VALIDATION_CONFIG_PATH = path.join(root, "validation.json");
    fs.writeFileSync(process.env.GH_AW_SAFE_OUTPUTS_CONFIG_PATH, JSON.stringify(options.config || { noop: { max: 1 }, create_issue: { max: 1 } }));
    fs.writeFileSync(process.env.GH_AW_VALIDATION_CONFIG_PATH, JSON.stringify(validation));
    const collector = require(modules[2]);
    const loader = require(modules[3]);
    const test = {
      assignment: queue?.assignment,
      outputs,
      warnings,
      errors,
      failures,
      get normalizationCalls() {
        return normalizationCalls;
      },
      async collect(source) {
        fs.writeFileSync(process.env.GH_AW_SAFE_OUTPUTS, typeof source === "string" ? source : source.map(item => JSON.stringify(item)).join("\n"));
        await collector.main();
        return JSON.parse(outputs.get("output"));
      },
      load(items, loadOptions = {}) {
        fs.writeFileSync(process.env.GH_AW_AGENT_OUTPUT, typeof items === "string" ? items : JSON.stringify({ items }));
        return loader.loadAgentOutput(loadOptions);
      },
    };
    if (options.privateExecution) {
      await claimScope.withClaimExecution({ assignment: queue.assignment, claim_handle: options.handle || queue.assignment.claims[0].handle }, async () => {
        process.env.GH_AW_WORK_QUEUE_ENABLED = "false";
        process.env.GH_AW_WORK_QUEUE_ROLE = "observer";
        await callback(test);
      });
    } else {
      await callback(test);
    }
  } finally {
    fs.existsSync = exists;
    fs.readdirSync = readdir;
    modules.forEach((name, index) => {
      if (savedModules[index]) require.cache[name] = savedModules[index];
      else delete require.cache[name];
    });
    global.core = savedCore;
    global.context = savedContext;
    ENV_KEYS.forEach((key, index) => {
      if (savedEnv[index] === undefined) delete process.env[key];
      else process.env[key] = savedEnv[index];
    });
    require("./safe_output_type_validator.cjs").resetValidationConfigCache();
    fs.rmSync(root, { recursive: true, force: true });
  }
}

describe("safe-output ingestion queue compatibility", () => {
  it("keeps ordinary limits global and strips undeclared Claim fields", async () => {
    await fixture({}, async test => {
      const output = await test.collect([
        { type: "create_issue", title: "first", body: "first", claim_handle: "h1" },
        { type: "create_issue", title: "second", body: "second", claim_handle: "h2" },
      ]);
      assert.deepEqual(output.items, [{ type: "create_issue", title: "first", body: "first" }]);
      assert.match(output.errors[0], /Too many items.*Maximum allowed: 1/);
      assert.equal(test.normalizationCalls, 0);
      assert.deepEqual(test.failures, []);
    });
  });

  it("keeps ordinary custom-job payloads and limits unchanged", async () => {
    await fixture({ config: { custom_job: { max: 1, inputs: { value: { type: "string", required: true } } } } }, async test => {
      const output = await test.collect([
        { type: "custom_job", value: "first", claim_handle: "h1" },
        { type: "custom_job", value: "second", claim_handle: "h2" },
      ]);
      assert.deepEqual(output.items, [{ type: "custom_job", value: "first" }]);
      assert.match(output.errors[0], /Too many items/);
      assert.equal(test.normalizationCalls, 0);
    });
  });

  it("preserves ordinary global minimum counts and diagnostics", async () => {
    await fixture({ config: { noop: { min: 2, max: 2 } } }, async test => {
      const output = await test.collect([{ type: "noop", message: "completed", claim_handle: "untrusted" }]);
      assert.deepEqual(output.items, [{ type: "noop", message: "completed" }]);
      assert.deepEqual(output.errors, ["Too few items of type 'noop'. Minimum required: 2, found: 1."]);
      assert.equal(test.normalizationCalls, 0);
    });
  });

  it("preserves ordinary repair and duplicate-key JSON behavior", async () => {
    await fixture({ config: { noop: { max: 2 } } }, async test => {
      const output = await test.collect('{"type":"noop","message":"repaired",}\n{"type":"noop","message":"before","message":"after"}');
      assert.deepEqual(output.items, [
        { type: "noop", message: "repaired" },
        { type: "noop", message: "after" },
      ]);
      assert.deepEqual(output.errors, []);
    });
  });

  it("preserves the ordinary missing-type diagnostic", async () => {
    await fixture({}, async test => {
      const output = await test.collect([{ message: "no type" }]);
      assert.equal(output.errors[0], "Line 1: Missing required 'type' field");
      assert.equal(output.items[0].type, "report_incomplete");
    });
  });

  it("keeps ordinary probing noops out of the terminal output budget", async () => {
    await fixture({}, async test => {
      const output = await test.collect([
        { type: "noop", message: "probe" },
        { type: "noop", message: "task completed without changes" },
      ]);
      assert.deepEqual(output.items, [{ type: "noop", message: "task completed without changes" }]);
      assert.deepEqual(output.errors, []);
    });
  });

  it("preserves ordinary patch detection even with empty safe output", async () => {
    await fixture({ patches: ["aw-existing.patch"] }, async test => {
      const output = await test.collect("");
      assert.equal(output.items[0].type, "report_incomplete");
      assert.equal(test.outputs.get("has_patch"), "true");
    });
  });

  it("leaves ordinary loaded items and extra metadata untouched", async () => {
    await fixture({}, test => {
      const items = [{ type: "custom_job", claim_handle: "foreign", extra: { preserved: true } }, null, 7];
      assert.deepEqual(test.load(items), { success: true, items });
      assert.equal(test.normalizationCalls, 0);
    });
  });

  for (const claims of [1, 2]) {
    describe(`${claims}-Claim ingestion`, () => {
      it("attributes empty-output minimum failures to every original member", async () => {
        await fixture({ claims, config: { noop: { min: 1, max: 2 } } }, async test => {
          const output = await test.collect("");
          assert.deepEqual(
            output.items.map(item => item.claim_handle),
            test.assignment.claims.map(member => member.handle)
          );
          assert.ok(output.items.every(item => /Minimum required: 1, found: 0/.test(item._claimScopeError)));
          assert.equal(output.errors.length, claims);
          assert.ok(output.errors.every((error, index) => error.includes(test.assignment.claims[index].handle)));
        });
      });

      it("does not count invalid output toward a member's minimum", async () => {
        await fixture({ claims, config: { create_issue: { min: 1, max: 2 } } }, async test => {
          const output = await test.collect(test.assignment.claims.map(member => ({ type: "create_issue", title: "missing body", claim_handle: member.handle })));
          assert.equal(output.items.length, claims * 2);
          for (const member of test.assignment.claims) {
            const rejected = output.items.filter(item => item.claim_handle === member.handle);
            assert.equal(rejected.length, 2);
            assert.ok(rejected.every(item => item._claimScopeError));
            assert.match(rejected[1]._claimScopeError, /Minimum required: 1, found: 0/);
          }
        });
      });

      it("preserves real private execution for collection and loading after ambient changes", async () => {
        await fixture({ claims, privateExecution: true, handle: claims === 1 ? "h1" : "h2", config: { noop: { min: 1, max: 2 } } }, async test => {
          const selected = test.assignment.claims[claims - 1].handle;
          const output = await test.collect([{ type: "noop", message: "completed", ...(claims === 1 ? {} : { claim_handle: selected }) }]);
          assert.deepEqual(output.errors, []);
          assert.deepEqual(output.items, [{ type: "noop", message: "completed", claim_handle: selected }]);
          assert.deepEqual(test.load([...output.items, { type: "noop", message: "foreign", claim_handle: "foreign" }]), { success: true, items: output.items });
          assert.equal(test.load('{"items":[],"items":[]}').success, false);
          if (claims > 1) {
            const ambiguous = await test.collect([{ type: "noop", message: "missing selector" }]);
            assert.equal(ambiguous.items[0]._claimScopeErrorCode, "claim_scope_required");
            assert.equal(ambiguous.items[1].claim_handle, selected);
            assert.match(ambiguous.items[1]._claimScopeError, /Minimum required: 1, found: 0/);
          }
        });
      });

      it("keeps valid member outputs independently scoped", async () => {
        await fixture({ claims }, async test => {
          const messages = test.assignment.claims.map((member, index) => ({
            type: "create_issue",
            title: `issue ${index}`,
            body: `body ${index}`,
            ...(claims === 1 ? {} : { claim_handle: member.handle }),
          }));
          const output = await test.collect(messages);
          assert.deepEqual(output.errors, []);
          assert.deepEqual(
            output.items.map(item => item.claim_handle),
            test.assignment.claims.map(member => member.handle)
          );
          assert.equal(output.items.length, claims);
          assert.equal(test.normalizationCalls, claims);
        });
      });

      it("rejects repair-only JSON instead of relaxing queue parsing", async () => {
        await fixture({ claims }, async test => {
          const output = await test.collect('{"type":"noop","message":"bad",}');
          assert.equal(output.items.length, 1);
          assert.ok(output.items[0]._claimScopeError);
          assert.equal(output.items[0].type, "invalid");
          assert.equal(test.failures.length, 0);
        });
      });

      it("retains explicitly scoped noops even when their text looks like a probe", async () => {
        await fixture({ claims }, async test => {
          const output = await test.collect(test.assignment.claims.map(member => ({ type: "noop", message: "probe", claim_handle: member.handle })));
          assert.equal(output.items.length, claims);
          assert.deepEqual(output.errors, []);
          assert.ok(output.items.every(item => item.message === "probe" && !item._claimScopeError));
        });
      });

      it("does not infer successful output or discover legacy patches when empty", async () => {
        await fixture({ claims, patches: ["aw-existing.patch"] }, async test => {
          const output = await test.collect("");
          assert.deepEqual(output.items, []);
          assert.equal(test.outputs.get("has_patch"), "false");
        });
      });

      it("requires trusted standalone selection even for a singleton", async () => {
        await fixture({ claims }, test => {
          assert.match(test.load([{ type: "noop", message: "unselected" }]).error, /trusted per-Claim execution context/);
        });
      });

      it("preserves the complete immutable batch for explicit partitioning", async () => {
        await fixture({ claims }, test => {
          const messages = test.assignment.claims.map(member => ({ type: "noop", message: member.handle, claim_handle: member.handle }));
          const result = test.load(messages, { partitioning: true });
          assert.equal(result.success, true);
          assert.deepEqual(result.items, messages);
        });
      });
    });
  }

  it("enforces maxima per Claim without charging rejected messages to a sibling", async () => {
    await fixture({ claims: 2 }, async test => {
      const [first, second] = test.assignment.claims;
      const output = await test.collect([
        { type: "create_issue", title: "first", body: "first", claim_handle: first.handle },
        { type: "create_issue", title: "over limit", body: "extra", claim_handle: first.handle },
        { type: "create_issue", title: "second", body: "second", claim_handle: second.handle },
      ]);
      assert.deepEqual(
        output.items.filter(item => !item._claimScopeError).map(item => item.title),
        ["first", "second"]
      );
      assert.equal(output.items[1].claim_handle, first.handle);
      assert.match(output.items[1]._claimScopeError, /Too many items/);
    });
  });

  it("does not let a valid sibling satisfy another Claim's minimum", async () => {
    await fixture({ claims: 2, config: { noop: { min: 1, max: 2 } } }, async test => {
      const [first, second] = test.assignment.claims;
      const valid = { type: "noop", message: "completed", claim_handle: first.handle };
      const output = await test.collect([valid]);
      assert.deepEqual(output.items[0], valid);
      assert.equal(output.items[1].claim_handle, second.handle);
      assert.match(output.items[1]._claimScopeError, /Minimum required: 1, found: 0/);
      assert.equal(output.errors.length, 1);
      assert.ok(output.errors[0].includes(second.handle));
      assert.deepEqual(test.load(output.items, { partitioning: true }).items, output.items);
    });
  });

  it("rejects missing and foreign multi-Claim selectors without dropping valid members", async () => {
    await fixture({ claims: 2 }, async test => {
      const output = await test.collect([
        { type: "noop", message: "missing" },
        { type: "noop", message: "foreign", claim_handle: "foreign" },
        ...[null, 7, {}].map(claim_handle => ({ type: "noop", message: "malformed", claim_handle })),
        ...test.assignment.claims.map(member => ({ type: "noop", message: member.handle, claim_handle: member.handle })),
      ]);
      assert.equal(output.items.filter(item => item._claimScopeError).length, 5);
      assert.deepEqual(
        output.items.filter(item => !item._claimScopeError).map(item => item.claim_handle),
        test.assignment.claims.map(member => member.handle)
      );
    });
  });

  it("loads only the selected Claim while preserving its originally multi-member assignment", async () => {
    await fixture({ claims: 2, handle: "h2" }, test => {
      const messages = [
        { type: "noop", message: "no selector" },
        { type: "noop", message: "other member", claim_handle: "h1" },
        { type: "noop", message: "foreign", claim_handle: "foreign" },
        { type: "noop", message: "selected", claim_handle: "h2" },
      ];
      assert.deepEqual(test.load(messages), { success: true, items: [messages[3]] });
      assert.equal(test.assignment.claims.length, 2);
    });
  });

  it("retains selected-Claim loading and strict parsing when runtime flags are disabled", async () => {
    await fixture({ claims: 2, handle: "h2", runtimeScopeDisabled: true }, test => {
      const selected = { type: "noop", message: "selected", claim_handle: "h2" };
      assert.deepEqual(test.load([{ type: "noop", message: "other", claim_handle: "h1" }, selected]), { success: true, items: [selected] });
      assert.equal(test.load('{"items":[],"items":[]}').success, false);
    });
  });

  it("retains queue ingestion validation within a trusted Claim despite disabled runtime flags", async () => {
    await fixture({ claims: 1, handle: "h1", runtimeScopeDisabled: true }, async test => {
      const output = await test.collect([{ type: "noop", message: "probe" }]);
      assert.deepEqual(output.items, [{ type: "noop", message: "probe", claim_handle: "h1" }]);
      assert.equal(test.normalizationCalls, 1);
      const invalid = await test.collect('{"type":"noop","message":"repair-only",}');
      assert.ok(invalid.items[0]._claimScopeError);
    });
  });
});
