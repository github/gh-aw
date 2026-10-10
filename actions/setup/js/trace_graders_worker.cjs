// @ts-check

const vm = require("vm");

function readStdin() {
  return new Promise(resolve => {
    let data = "";
    process.stdin.setEncoding("utf-8");
    process.stdin.on("data", chunk => {
      data += chunk;
    });
    process.stdin.on("end", () => resolve(data));
  });
}

async function main() {
  const raw = await readStdin();
  let payload;
  try {
    payload = JSON.parse(raw || "{}");
  } catch (err) {
    const message = err instanceof Error ? err.message : String(err);
    process.stderr.write(`invalid worker payload: ${message}\n`);
    process.exit(1);
  }

  try {
    const script = String(payload.script || "");

    const sandbox = Object.assign(Object.create(null), {
      __payload: JSON.stringify({
        trace: payload.trace || {},
        run: { graderCount: Number(payload.graderCount) || 0 },
        workflow: {},
        config: payload.config || {},
      }),
      Date: undefined,
      fetch: undefined,
      require: undefined,
      process: undefined,
      global: undefined,
      Function: undefined,
      eval: undefined,
      undefined,
      NaN,
      Infinity,
    });
    const context = vm.createContext(sandbox, { codeGeneration: { strings: false, wasm: false } });
    const timeoutMs = Number(payload.timeoutMs) || 5000;

    vm.runInContext(
      `
        "use strict";
        (() => {
        const root = globalThis;
        const deepFreeze = value => {
          if (value === null || typeof value !== "object") return value;
          Object.freeze(value);
          for (const key of Object.getOwnPropertyNames(value)) {
            if (value[key] !== null && typeof value[key] === "object" && !Object.isFrozen(value[key])) {
              deepFreeze(value[key]);
            }
          }
          return value;
        };
        const data = JSON.parse(__payload);
        for (const [key, value] of Object.entries(data)) {
          Object.defineProperty(root, key, {
            value: deepFreeze(value),
            writable: false,
            enumerable: true,
            configurable: false
          });
        }
        (() => {
          const m = {};
          const descriptors = Object.getOwnPropertyDescriptors(Math);
          for (const key of Object.keys(descriptors)) {
            Object.defineProperty(m, key, descriptors[key]);
          }
          Object.defineProperty(m, "random", {
            value: undefined,
            writable: false,
            enumerable: true,
            configurable: false
          });
          const safeMath = Object.freeze(m);
          Object.defineProperty(root, "__math", {
            value: safeMath,
            writable: false,
            enumerable: false,
            configurable: false
          });
          Object.defineProperty(root, "helpers", {
            value: Object.freeze({
            clamp: (v, lo, hi) => safeMath.max(lo, safeMath.min(hi, v)),
            ratio: (num, den) => (den === 0 ? 0 : num / den),
            sum: arr => arr.reduce((a, b) => a + b, 0)
            }),
            writable: false,
            enumerable: true,
            configurable: false
          });
        })();
        delete root.__payload;
        Object.defineProperty(root, "globalThis", {
          value: undefined,
          writable: false,
          enumerable: false,
          configurable: false
        });
        })();
      `,
      context,
      { timeout: 1000, filename: "grader:bootstrap" }
    );

    const graderFn = vm.compileFunction(`"use strict";\n${script}`, ["trace", "run", "workflow", "config", "helpers", "Math"], {
      parsingContext: context,
      filename: `grader:${String(payload.id || "unknown")}`,
    });
    context.__grader = graderFn;
    const result = vm.runInContext(
      `(() => {
        try {
          const value = __grader(trace, run, workflow, config, helpers, __math);
          const metricValue = value !== null && typeof value === "object" && Object.hasOwn(value, "value") ? value.value : value;
          if (typeof metricValue === "number" && !Number.isFinite(metricValue)) {
            throw new Error("custom grader returned non-finite numeric value");
          }
          return JSON.stringify({ ok: true, value });
        } catch (err) {
          return JSON.stringify({ ok: false, error: err instanceof Error ? err.message : String(err) });
        }
      })()`,
      context,
      { timeout: timeoutMs, filename: `grader:${String(payload.id || "unknown")}:invoke` }
    );
    process.stdout.write(result);
  } catch (err) {
    const message = err instanceof Error ? err.message : String(err);
    process.stdout.write(JSON.stringify({ ok: false, error: message }));
  }
}

main().catch(err => {
  const message = err instanceof Error ? err.stack || err.message : String(err);
  process.stdout.write(JSON.stringify({ ok: false, error: message }), () => {
    process.exit(1);
  });
});
