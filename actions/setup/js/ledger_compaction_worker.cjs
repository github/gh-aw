// @ts-check
"use strict";

// Isolated worker for user-configured ledger compaction selection scripts. It runs under the
// Node.js permission model with an empty environment, no require/process, and returns only a
// JSON selection ({ sources: string[] }) that the planner validates before building a plan.

const vm = require("node:vm");

const MAX_INPUT_BYTES = 16 * 1024 * 1024;
const MAX_OUTPUT_BYTES = 1024 * 1024;

async function main() {
  let raw = "";
  for await (const chunk of process.stdin) {
    raw += chunk;
    if (Buffer.byteLength(raw) > MAX_INPUT_BYTES) throw new RangeError("Compaction input exceeds size limit");
  }
  const input = JSON.parse(raw);
  const context = vm.createContext(
    {
      inputJSON: JSON.stringify({ segments: input.segments, options: input.options }),
      process: undefined,
      require: undefined,
      fetch: undefined,
      Date: undefined,
      Intl: undefined,
      performance: undefined,
      crypto: undefined,
      eval: undefined,
      Function: undefined,
      WebAssembly: undefined,
    },
    { codeGeneration: { strings: false, wasm: false } }
  );
  vm.runInContext(
    `
    "use strict";
    const freeze = value => {
      if (value && typeof value === "object") {
        for (const key of Object.keys(value)) freeze(value[key]);
        Object.freeze(value);
      }
      return value;
    };
    globalThis.compactionInput = freeze(JSON.parse(inputJSON));
    const safeMath = Object.create(null);
    for (const key of Object.getOwnPropertyNames(Math)) {
      if (key !== "random") Object.defineProperty(safeMath, key, Object.getOwnPropertyDescriptor(Math, key));
    }
    Object.freeze(safeMath);
    globalThis.safeMath = safeMath;
    globalThis.Math = safeMath;
  `,
    context,
    { timeout: 1000 }
  );
  context.invoke = vm.compileFunction(`"use strict";\n${input.script}`, ["segments", "options", "Math"], {
    parsingContext: context,
    filename: "ledger:compaction",
  });
  const result = vm.runInContext("invoke(compactionInput.segments, compactionInput.options, safeMath)", context, { timeout: 10000 });
  const output = JSON.stringify(result === undefined ? null : result);
  if (!output || Buffer.byteLength(output) > MAX_OUTPUT_BYTES) throw new RangeError("Compaction output exceeds size limit");
  process.stdout.write(output);
}

main().catch(() => {
  process.stdout.write(JSON.stringify({ error: "Compaction worker failed" }));
  process.exitCode = 1;
});
