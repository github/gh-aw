// @ts-check
"use strict";

/** @param {string} text @returns {import("./types/command_diagnostics").CommandDiagnostic[]} */
function parsePythonDiagnostics(text) {
  /** @type {import("./types/command_diagnostics").CommandDiagnostic[]} */
  const diagnostics = [];
  /** @type {import("./types/command_diagnostics").DiagnosticLocation[]} */
  let frames = [];
  let test;
  let inTraceback = false;
  for (const raw of text.split("\n")) {
    const line = raw.replace(/^E\s{2,}/, "");
    const frame = /^\s*File "(.+\.py)", line (\d+)(?:, in .+)?/.exec(line);
    const pytestFrame = /^(.+\.py):(\d+):(?:\s*(.*))?$/.exec(line);
    const testHeader = /^(?:FAIL|ERROR): (.+)$/.exec(line);
    const pytestHeader = /^_+\s+(.+?)\s+_+$/.exec(line);
    if (testHeader || pytestHeader) {
      test = (testHeader ?? pytestHeader)?.[1];
      frames = [];
    }
    if (/Traceback \(most recent call last\):/.test(line)) inTraceback = true;
    if (frame) {
      inTraceback = true;
      frames.push({ path: frame[1], line: Number(frame[2]) });
    } else if (pytestFrame) {
      frames.push({ path: pytestFrame[1], line: Number(pytestFrame[2]) });
      if (pytestFrame[3] && /(?:Error|Exception|Interrupt|Exit)\b/.test(pytestFrame[3])) {
        diagnostics.push({ language: "python", severity: "error", message: pytestFrame[3], location: frames.at(-1), ...(test ? { test } : {}) });
        frames = [];
      }
    } else {
      const exception = /^([A-Za-z_][\w.]*(?:Error|Exception|Warning|Interrupt|Exit)|Exception|SystemExit|KeyboardInterrupt)(?::\s*(.*))?$/.exec(line.trim());
      if (exception && (inTraceback || frames.length || /^E\s/.test(raw))) {
        diagnostics.push({
          language: "python",
          severity: "error",
          code: exception[1],
          message: line.trim(),
          ...(frames.length ? { location: frames.at(-1) } : {}),
          ...(test ? { test } : {}),
          ...(frames.length > 1 ? { related: frames.slice(0, -1).map(location => ({ message: "Traceback frame", location })) } : {}),
        });
        frames = [];
        inTraceback = false;
      }
    }
    const summary = /^(?:FAILED|ERROR)\s+(.+?\.py(?:::[^\s]+)*)(?:\s+-\s+(.+))?$/.exec(line);
    if (summary && !diagnostics.some(diagnostic => diagnostic.test === summary[1])) {
      diagnostics.push({ language: "python", severity: "error", test: summary[1], message: summary[2] || `${summary[1]} failed` });
    }
  }
  return diagnostics;
}

module.exports = { parsePythonDiagnostics };
