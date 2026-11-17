// @ts-check

const fs = require("fs");
const vm = require("vm");

function main() {
  const input = JSON.parse(fs.readFileSync(0, "utf8"));
  if (input.contract !== undefined && input.contract !== "operational-value") throw new Error("unsupported custom grader contract");
  const sandbox = Object.assign(Object.create(null), {
    __input: JSON.stringify(input.data),
    Date: undefined,
    Intl: undefined,
    Temporal: undefined,
    performance: undefined,
    console: undefined,
    fetch: undefined,
    require: undefined,
    process: undefined,
    global: undefined,
    Function: undefined,
    eval: undefined,
    WebAssembly: undefined,
  });
  const context = vm.createContext(sandbox, { codeGeneration: { strings: false, wasm: false }, microtaskMode: "afterEvaluate" });
  vm.runInContext(
    `(() => {
      const data = JSON.parse(__input);
      delete globalThis.__input;
      const freeze = value => {
        if (value && typeof value === "object" && !Object.isFrozen(value)) {
          Object.values(value).forEach(freeze);
          Object.freeze(value);
        }
        return value;
      };
      const session = freeze(data.session);
      const events = new Map();
      for (const event of session) {
        const bucket = events.get(event.type) ?? [];
        bucket.push(event);
        events.set(event.type, bucket);
      }
      for (const bucket of events.values()) Object.freeze(bucket);
      const calls = new Map(freeze(data.lookups.toolCallsById));
      const toolCallsById = Object.freeze({
        size: calls.size,
        get: key => calls.get(key),
        has: key => calls.has(key),
        entries: () => calls.entries(),
        keys: () => calls.keys(),
        values: () => calls.values(),
        [Symbol.iterator]: () => calls[Symbol.iterator](),
      });
      const lookups = freeze({
        ...data.lookups,
        eventsOfType: type => events.get(type) ?? [],
        toolCallsById,
      });
      globalThis.session = session;
      globalThis.config = freeze(data.config);
      globalThis.helpers = Object.freeze({ createSessionLookups: () => lookups });
      Object.defineProperty(Math, "random", { value: undefined });
    })()`,
    context,
    { timeout: 5000, filename: "grader:bootstrap" }
  );
  context.__grader = vm.compileFunction(`"use strict";\n${input.script}`, ["session", "config", "helpers"], { parsingContext: context, filename: `grader:${input.id}` });
  const validateResult =
    input.contract === "operational-value"
      ? `if (!arrayIsArray(value) || value.length === 0) throw new Error("operational-value script must return a nonempty metric array");
        const ids = new SetCtor();
        for (const metric of value) {
          if (!metric || typeof metric !== "object" ||
              reflectApply(arrayJoin, reflectApply(arraySort, objectKeys(metric), []), [","]) !== "id,value" ||
              typeof metric.id !== "string" || !reflectApply(stringTrim, metric.id, []) || reflectApply(setHas, ids, [metric.id]) ||
              (metric.value !== null && (typeof metric.value !== "number" || !numberIsFinite(metric.value)))) {
            throw new Error("operational-value metrics must contain unique nonempty ids and finite numeric or null values");
          }
          reflectApply(setAdd, ids, [metric.id]);
        }`
      : `const metric = value && typeof value === "object" ? value.value : value;
        if (metric !== null && (typeof metric !== "number" || !numberIsFinite(metric))) throw new Error("custom grader must return a finite numeric value or null");`;
  const result = vm.runInContext(
    `(() => {
      try {
        const arrayIsArray = Array.isArray;
        const objectKeys = Object.keys;
        const numberIsFinite = Number.isFinite;
        const stringTrim = String.prototype.trim;
        const arraySort = Array.prototype.sort;
        const arrayJoin = Array.prototype.join;
        const reflectApply = Reflect.apply;
        const SetCtor = Set;
        const setHas = Set.prototype.has;
        const setAdd = Set.prototype.add;
        const value = __grader(session, config, helpers);
        if (value && typeof value.then === "function") throw new Error("custom graders must be synchronous");
        ${validateResult}
        return JSON.stringify({ ok: true, value });
      } catch (error) {
        return JSON.stringify({ ok: false, error: error instanceof Error ? error.message : String(error) });
      }
    })()`,
    context,
    { timeout: 5000, filename: `grader:${input.id}:invoke` }
  );
  if (typeof result !== "string") throw new Error("invalid serialized grader result");
  process.stdout.write(result);
}

try {
  main();
} catch {
  // Never inspect or coerce a value thrown across the VM boundary.
  process.stdout.write(JSON.stringify({ ok: false, error: "Custom grader worker failed or timed out" }));
}
