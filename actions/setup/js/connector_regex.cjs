// @ts-check

const { Worker, isMainThread, parentPort, workerData } = require("node:worker_threads");
const { ERR_VALIDATION } = require("./error_codes.cjs");

const ABSOLUTE_END = "(?![\\s\\S])";
const ASCII_SPACE = "\\t\\n\\f\\r ";

/**
 * Keep the portable RE2 subset's matching semantics, without executing an
 * author-defined expression on the credentialed host's main thread.
 * @param {string} pattern
 */
function connectorPatternSource(pattern) {
  if (typeof pattern !== "string" || pattern.length > 4096) throw new Error(`${ERR_VALIDATION}: Connector pattern exceeds its bound`);
  const dotAll = pattern.startsWith("(?s)");
  if (dotAll) pattern = pattern.slice(4);
  let source = "";
  let inClass = false;
  for (let index = 0; index < pattern.length; index++) {
    const character = pattern[index];
    if (character === "\\") {
      const escaped = pattern[++index];
      if (!escaped || "pPQE".includes(escaped)) throw new Error(`${ERR_VALIDATION}: Unsupported nonportable connector pattern`);
      if (escaped === "s") source += inClass ? ASCII_SPACE : `[${ASCII_SPACE}]`;
      else if (escaped === "S") {
        if (inClass) throw new Error(`${ERR_VALIDATION}: Unsupported nonportable connector pattern`);
        source += `[^${ASCII_SPACE}]`;
      } else if (escaped === "A") {
        if (inClass) throw new Error(`${ERR_VALIDATION}: Unsupported nonportable connector pattern`);
        source += "^";
      } else if (escaped === "z") {
        if (inClass) throw new Error(`${ERR_VALIDATION}: Unsupported nonportable connector pattern`);
        source += ABSOLUTE_END;
      } else if (escaped === "a") source += "\\x07";
      else source += `\\${escaped}`;
    } else if (character === "[") {
      if (pattern[index + 1] === ":") throw new Error(`${ERR_VALIDATION}: Unsupported nonportable connector pattern`);
      inClass = true;
      source += character;
    } else if (character === "]") {
      inClass = false;
      source += character;
    } else if (!inClass && character === "(" && pattern[index + 1] === "?" && pattern[index + 2] !== ":") {
      throw new Error(`${ERR_VALIDATION}: Unsupported nonportable connector pattern`);
    } else if (!inClass && character === "$") source += ABSOLUTE_END;
    else if (!inClass && character === ".") source += dotAll ? "[\\s\\S]" : "[^\\n]";
    else source += character;
  }
  return source;
}

/**
 * @param {string} pattern
 * @param {string} value
 * @returns {Promise<boolean>}
 */
async function matchesConnectorPattern(pattern, value) {
  if (typeof value !== "string" || Array.from(value).length > 65536 || Buffer.byteLength(value, "utf8") > 262144) throw new Error(`${ERR_VALIDATION}: Connector pattern input exceeds its bound`);
  const source = connectorPatternSource(pattern);
  let wellFormed = "";
  for (const character of value) {
    const rune = character.codePointAt(0);
    wellFormed += rune !== undefined && rune >= 0xd800 && rune <= 0xdfff ? "\ufffd" : character;
  }
  const worker = new Worker(__filename, {
    workerData: { connectorPattern: true, source, value: wellFormed },
    resourceLimits: { maxOldGenerationSizeMb: 32, stackSizeMb: 2 },
  });
  return new Promise((resolve, reject) => {
    let settled = false;
    /** @param {boolean | undefined} result @param {Error | undefined} failure */
    const finish = async (result, failure) => {
      if (settled) return;
      settled = true;
      clearTimeout(timer);
      try {
        await worker.terminate();
      } catch {
        reject(new Error(`${ERR_VALIDATION}: Connector pattern worker could not terminate`));
        return;
      }
      if (failure) reject(failure);
      else resolve(result === true);
    };
    const timer = setTimeout(() => {
      void finish(undefined, new Error(`${ERR_VALIDATION}: Connector pattern exceeded its validation budget`));
    }, 1000);
    worker.once("message", message => {
      if (typeof message?.match === "boolean") void finish(message.match, undefined);
      else void finish(undefined, new Error(`${ERR_VALIDATION}: Unsupported connector pattern`));
    });
    worker.once("error", () => {
      void finish(undefined, new Error(`${ERR_VALIDATION}: Connector pattern worker failed`));
    });
    worker.once("exit", () => {
      if (!settled) void finish(undefined, new Error(`${ERR_VALIDATION}: Connector pattern worker exited without a result`));
    });
  });
}

if (!isMainThread && workerData?.connectorPattern === true) {
  try {
    const match = new RegExp(workerData.source, "u").test(workerData.value);
    parentPort?.postMessage({ match });
  } catch {
    parentPort?.postMessage({ unsupported: true });
  }
}

module.exports = { matchesConnectorPattern, connectorPatternSource };
