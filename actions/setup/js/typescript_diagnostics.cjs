// @ts-check
"use strict";

/** @param {string} text @returns {import("./types/command_diagnostics").CommandDiagnostic[]} */
function parseTypeScriptDiagnostics(text) {
  /** @type {import("./types/command_diagnostics").CommandDiagnostic[]} */
  const diagnostics = [];
  for (const line of text.split("\n")) {
    const standard = /^(.+\.(?:[cm]?tsx?|jsx?|json))\((\d+),(\d+)\):\s*(error|warning)\s+(TS\d+):\s*(.*)$/.exec(line);
    const pretty = /^(.+\.(?:[cm]?tsx?|jsx?|json)):(\d+):(\d+)\s+-\s+(error|warning)\s+(TS\d+):\s*(.*)$/.exec(line);
    const located = standard ?? pretty;
    const global = /^(error|warning)\s+(TS\d+):\s*(.*)$/.exec(line);
    if (located) {
      diagnostics.push({
        language: "typescript",
        severity: located[4] === "warning" ? "warning" : "error",
        code: located[5],
        message: located[6],
        location: { path: located[1], line: Number(located[2]), column: Number(located[3]) },
      });
    } else if (global) {
      diagnostics.push({ language: "typescript", severity: global[1] === "warning" ? "warning" : "error", code: global[2], message: global[3] });
    } else {
      const previous = diagnostics.at(-1);
      const related = /^\s+(.+\.(?:[cm]?tsx?|jsx?|json)):(\d+):(\d+)\s*$/.exec(line);
      if (previous && related) {
        previous.related ??= [];
        previous.related.push({ message: "Related location", location: { path: related[1], line: Number(related[2]), column: Number(related[3]) } });
      } else if (previous && /^\s+\S/.test(line) && !/^\s*(?:\d+|~|Found \d+)/.test(line)) {
        const lastRelated = previous.related?.at(-1);
        if (lastRelated) lastRelated.message += "\n" + line.trim();
        else previous.message += "\n" + line.trim();
      }
    }
  }
  return diagnostics;
}

module.exports = { parseTypeScriptDiagnostics };
