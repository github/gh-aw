// @ts-check
"use strict";

/** @typedef {import("./types/command_diagnostics").CommandDiagnostic} Diagnostic */

/** @param {string} text @returns {Diagnostic[]} */
function parseGoText(text) {
  /** @type {Diagnostic[]} */
  let diagnostics = [];
  let test;
  let packageName;
  let packageStart = 0;
  for (const line of text.split("\n")) {
    const running = /^\s*=== (?:RUN|CONT|NAME)\s+(\S+)/.exec(line);
    if (running) test = running[1];
    const passed = /^\s*--- (?:PASS|SKIP): (\S+)/.exec(line);
    if (passed) {
      diagnostics = diagnostics.filter(diagnostic => diagnostic.test !== passed[1] || diagnostic.package !== packageName);
      test = undefined;
    }
    const header = /^# (.+)$/.exec(line);
    if (header) packageName = header[1];
    const failure = /^\s*--- FAIL: (\S+)/.exec(line);
    if (failure) {
      test = failure[1];
      diagnostics.push({ language: "go", severity: "error", message: `Test ${test} failed`, test, ...(packageName ? { package: packageName } : {}) });
    }
    const location = /^\s*(.+\.go):(\d+)(?::(\d+))?:\s*(.+)$/.exec(line);
    if (location) {
      diagnostics.push({
        language: "go",
        severity: "error",
        message: location[4],
        location: { path: location[1], line: Number(location[2]), ...(location[3] ? { column: Number(location[3]) } : {}) },
        ...(test ? { test } : {}),
        ...(packageName ? { package: packageName } : {}),
      });
    } else if (/^(?:panic:|fatal error:)/.test(line)) {
      diagnostics.push({ language: "go", severity: "error", message: line, ...(test ? { test } : {}), related: [] });
    } else {
      const frame = /^\s*(.+\.go):(\d+)(?:\s|$)/.exec(line);
      const previous = diagnostics.at(-1);
      if (frame && previous?.related) previous.related.push({ message: "Panic frame", location: { path: frame[1], line: Number(frame[2]) } });
      else if (/^\s+/.test(line) && previous && !previous.related && !failure && !/^\s*(?:===|---|\[)/.test(line) && line.trim()) previous.message += "\n" + line.trim();
    }
    const footer = /^FAIL\s+(\S+)/.exec(line);
    if (footer) {
      for (const diagnostic of diagnostics.slice(packageStart)) diagnostic.package ??= footer[1];
      packageStart = diagnostics.length;
      packageName = undefined;
      test = undefined;
    }
  }
  return diagnostics;
}

/** @param {string} text @returns {Diagnostic[]} */
function parseGoDiagnostics(text) {
  /** @type {Map<string, {package: string, test?: string, lines: string[], failed: boolean}>} */
  const groups = new Map();
  const plain = [];
  for (const line of text.split("\n")) {
    let record;
    if (line.startsWith("{")) {
      try {
        record = JSON.parse(line);
      } catch {
        // Non-JSON tool output is still parsed as ordinary compiler/test text.
        record = undefined;
      }
    }
    const packageName = typeof record?.Package === "string" ? record.Package : typeof record?.ImportPath === "string" ? record.ImportPath : undefined;
    if (typeof record?.Action !== "string" || packageName === undefined) {
      plain.push(line);
      continue;
    }
    const test = typeof record.Test === "string" ? record.Test : undefined;
    const key = JSON.stringify([packageName, test]);
    /** @type {{package: string, test?: string, lines: string[], failed: boolean}} */
    const group = groups.get(key) ?? { package: packageName, test, lines: [], failed: false };
    if (typeof record.Output === "string") group.lines.push(record.Output);
    if (record.Action === "fail" || record.Action === "build-fail") group.failed = true;
    groups.set(key, group);
  }
  const diagnostics = parseGoText(plain.join("\n"));
  for (const group of groups.values()) {
    if (group.test && !group.failed) continue;
    const parsed = parseGoText(group.lines.join(""));
    if (group.failed && parsed.length === 0 && (group.test || ![...groups.values()].some(other => other.package === group.package && other.test && other.failed))) {
      parsed.push({ language: "go", severity: "error", message: group.test ? `Test ${group.test} failed` : `Package ${group.package} failed` });
    }
    for (const diagnostic of parsed) {
      diagnostic.package = group.package;
      if (group.test) diagnostic.test = group.test;
      diagnostics.push(diagnostic);
    }
  }
  return diagnostics;
}

module.exports = { parseGoDiagnostics };
