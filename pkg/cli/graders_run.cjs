"use strict";

const fs = require("fs");
const vm = require("vm");

const builtins = {
  "tool-success-rate": trace => {
    if (trace.toolCalls.length === 0) return 1;
    const failures = trace.toolCalls.filter(call => call.success === false || call.status === "error" || call.status === "failure" || call.error !== undefined).length;
    return (trace.toolCalls.length - failures) / trace.toolCalls.length;
  },
  "tool-failure-count": trace => trace.toolCalls.filter(call => call.success === false || call.status === "error" || call.status === "failure" || call.error !== undefined).length,
  retries: trace => trace.retryEvents.length,
  loops: trace => {
    let count = 0;
    let previous = "";
    for (const call of trace.toolCalls) {
      const key = `${String(call.name || call.tool)}:${JSON.stringify(call.arguments || call.params || "")}`;
      if (key === previous) count++;
      previous = key;
    }
    return count;
  },
  "trajectory-efficiency": trace => {
    if (trace.toolCalls.length === 0) return 1;
    return Math.min(1, new Set(trace.toolCalls.map(call => String(call.name || call.tool || ""))).size / trace.toolCalls.length);
  },
  "execution-step-count": trace => trace.totalRequests,
  "execution-duration": trace => trace.totalDurationMs,
  "working-set-rebuild-factor": trace => {
    const entries = trace.tokenUsageEntries || [];
    const valid = entries.map(entry => entry?.input_tokens).filter(value => typeof value === "number" && Number.isSafeInteger(value) && value >= 0);
    if (valid.length === 0) return null;
    const total = valid.reduce((sum, value) => sum + value, 0);
    const peak = Math.max(...valid);
    return peak === 0 ? null : Math.max(1, total / peak);
  },
  "context-growth": trace => {
    const entries = trace.tokenUsageEntries || [];
    if (entries.length < 2) return 1;
    const first = (Number(entries[0].input_tokens) || 0) + (Number(entries[0].output_tokens) || 0);
    return first === 0 ? 1 : ((Number(trace.totalInputTokens) || 0) + (Number(trace.totalOutputTokens) || 0)) / first;
  },
  "artifact-production": trace => trace.artifacts.length,
};

function deepFreeze(value) {
  if (value === null || typeof value !== "object") return value;
  Object.freeze(value);
  for (const key of Object.getOwnPropertyNames(value)) {
    if (value[key] !== null && typeof value[key] === "object" && !Object.isFrozen(value[key])) deepFreeze(value[key]);
  }
  return value;
}

function runInline(grader, trace) {
  const sandbox = {
    trace: deepFreeze(structuredClone(trace)),
    run: deepFreeze({ graderCount: 1 }),
    workflow: deepFreeze({}),
    config: deepFreeze(structuredClone(grader.config || {})),
    Date: undefined,
    fetch: undefined,
    require: undefined,
    process: undefined,
    global: undefined,
    globalThis: undefined,
    Function: undefined,
    eval: undefined,
  };
  const context = vm.createContext(sandbox, { codeGeneration: { strings: false, wasm: false } });
  const safeMath = vm.runInContext(
    `(() => {
      const value = {};
      Object.defineProperties(value, Object.getOwnPropertyDescriptors(Math));
      Object.defineProperty(value, "random", { value: undefined });
      return Object.freeze(value);
    })()`,
    context,
    { timeout: 1000 }
  );
  const helpers = deepFreeze({
    clamp: (value, low, high) => Math.max(low, Math.min(high, value)),
    ratio: (numerator, denominator) => (denominator === 0 ? 0 : numerator / denominator),
    sum: values => values.reduce((sum, value) => sum + value, 0),
  });
  const fn = vm.compileFunction(`"use strict";\n${grader.script}`, ["trace", "run", "workflow", "config", "helpers", "Math"], {
    parsingContext: context,
    filename: `grader:${grader.id}`,
  });
  context.__grader = fn;
  context.__helpers = helpers;
  context.__math = safeMath;
  return vm.runInContext("__grader(trace, run, workflow, config, __helpers, __math)", context, {
    timeout: 5000,
    filename: `grader:${grader.id}:invoke`,
  });
}

function normalize(grader, raw) {
  const object = raw !== null && typeof raw === "object" && !Array.isArray(raw) ? raw : {};
  const value = Object.hasOwn(object, "value") ? object.value : raw;
  let passed = typeof object.passed === "boolean" ? object.passed : null;
  const implementation = { id: "gh-aw/graders", version: 1, ...(grader.digest ? { digest: grader.digest } : {}) };
  if (value !== null && value !== undefined && (typeof value !== "number" || !Number.isFinite(value))) {
    return {
      id: grader.id,
      name: grader.name || grader.id,
      value: null,
      unit: grader.unit || "",
      passed: null,
      status: "error",
      source: grader.source,
      error: `grader ${grader.id} returned non-finite value: ${String(value)}`,
      implementation,
    };
  }
  if (passed === null && value !== null && value !== undefined && grader.threshold !== null && grader.threshold !== undefined) {
    passed = grader.direction === "higher_is_better" ? value >= grader.threshold : value <= grader.threshold;
  }
  const status = value === null || value === undefined ? "unavailable" : passed === false ? "fail" : "pass";
  return {
    id: grader.id,
    name: grader.name || grader.id,
    value: value ?? null,
    unit: grader.unit || "",
    passed,
    status,
    source: grader.source,
    implementation,
    ...(typeof object.severity === "string" && ["error", "warning", "info", "note"].includes(object.severity) ? { severity: object.severity } : {}),
    ...(object.details ? { details: String(object.details) } : {}),
    ...(object.message ? { message: String(object.message) } : {}),
  };
}

const input = JSON.parse(fs.readFileSync(0, "utf8"));
const grader = input.grader;
const trace = deepFreeze(input.payload);
const raw = grader.source === "builtin" ? builtins[grader.id](trace) : runInline(grader, trace);
process.stdout.write(`${JSON.stringify(normalize(grader, raw), null, 2)}\n`);
