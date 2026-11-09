// Developer fixture tooling only. Nothing in the Actions runtime imports this file.
"use strict";
const fs = require("node:fs");
const path = require("node:path");
const { execFileSync } = require("node:child_process");
const root = path.resolve(__dirname, "..");
const binary = path.join(process.env.GH_AW_TEST_METADATA_TOOL_DIR || path.join(root, "build", "test-tools"), "markdown-metadata-fixtures");
let built = false;
const records = new Map();

function compile(input) {
  if (!built) {
    fs.mkdirSync(path.dirname(binary), { recursive: true });
    execFileSync("go", ["build", "-o", binary, "./scripts/markdown-metadata-fixtures"], { cwd: root, stdio: "pipe" });
    built = true;
  }
  return JSON.parse(execFileSync(binary, [], { cwd: root, input: JSON.stringify(input), encoding: "utf8", maxBuffer: 16 * 1024 * 1024 }));
}

function readDocumentMetadata(content) {
  const result = compile({ document: content });
  if (result.error) throw new Error(result.error);
  return result;
}

function prepareMarkdownFixtures(...sources) {
  const compiled = compile({ sources });
  for (const [source, record] of Object.entries(compiled)) records.set(source, record);
  require("../actions/setup/js/markdown_metadata.cjs").installMarkdownMetadata({ version: 1, records: [...records.values()] });
  return { version: 1, records: [...records.values()] };
}

// Explicitly compile authored test inputs before calling the real runtime.
// This is fixture provisioning, not a fallback installed in the runtime helper.
function withCompilerFixtures(api) {
  const collect = (value, sources) => {
    if (typeof value === "string") {
      if (value.includes("---\n") || value.includes("---\r\n")) sources.push(value);
      if (!value.includes("\n") && fs.existsSync(value)) {
        const info = fs.lstatSync(value);
        if (info.isSymbolicLink()) return;
        if (info.isDirectory()) {
          for (const name of fs.readdirSync(value)) {
            if (!["node_modules", ".git"].includes(name)) collect(path.join(value, name), sources);
          }
        } else if (value.endsWith(".md")) {
          sources.push(fs.readFileSync(value, "utf8"));
        }
      }
    } else if (Array.isArray(value)) {
      for (const item of value) collect(item, sources);
    } else if (value && typeof value === "object") {
      for (const [key, item] of Object.entries(value)) {
        if (/(?:dir|directory|path|root)$/i.test(key)) collect(item, sources);
      }
    }
  };
  return Object.fromEntries(
    Object.entries(api).map(([name, value]) => [
      name,
      typeof value === "function" && (/(?:agent|skill|frontmatter|metadata|modelpin|processruntimeimport|processexpressions|runwithcopilotsdk)/i.test(name) || name === "main" || name === "inventoryDirectory")
        ? (...args) => {
            const sources = [];
            for (const arg of args) collect(arg, sources);
            if (sources.length) prepareMarkdownFixtures(...sources);
            return value(...args);
          }
        : value,
    ])
  );
}

module.exports = { readDocumentMetadata, prepareMarkdownFixtures, withCompilerFixtures };
