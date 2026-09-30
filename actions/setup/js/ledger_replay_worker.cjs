// @ts-check
"use strict";

const vm = require("node:vm");

const MAX_INPUT_BYTES = 16 * 1024 * 1024;
const MAX_OUTPUT_BYTES = 4 * 1024 * 1024;

async function main() {
  let raw = "";
  for await (const chunk of process.stdin) {
    raw += chunk;
    if (Buffer.byteLength(raw) > MAX_INPUT_BYTES) throw new RangeError("Replay input exceeds size limit");
  }
  const input = JSON.parse(raw);
  const context = vm.createContext(
    {
      inputJSON: JSON.stringify({ records: input.records, config: input.config }),
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
    globalThis.replayInput = freeze(JSON.parse(inputJSON));
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
  context.invoke = vm.compileFunction(`"use strict";\n${input.script}`, ["records", "config", "Math"], {
    parsingContext: context,
    filename: "ledger:replay",
  });
  const result = vm.runInContext("invoke(replayInput.records, replayInput.config, safeMath)", context, { timeout: 3000 });
  const seen = new Set();
  function check(value) {
    if (typeof value === "number" && !Number.isFinite(value)) throw new TypeError("Non-finite replay value");
    if (value === undefined || typeof value === "function" || typeof value === "bigint" || typeof value === "symbol") throw new TypeError("Invalid replay value");
    if (value && typeof value === "object") {
      if (seen.has(value)) throw new TypeError("Cyclic replay value");
      seen.add(value);
      for (const child of Object.values(value)) check(child);
      seen.delete(value);
    }
  }
  check(result);
  const output = JSON.stringify(result);
  if (!output || Buffer.byteLength(output) > MAX_OUTPUT_BYTES) throw new RangeError("Replay output exceeds size limit");
  process.stdout.write(output);
}

main().catch(() => {
  process.stdout.write(JSON.stringify({ error: "Replay worker failed" }));
  process.exitCode = 1;
});
