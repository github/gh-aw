import { readdirSync, readFileSync } from "node:fs";
import { extname, relative, resolve } from "node:path";
import { describe, expect, it } from "vitest";

const setupDirectory = resolve(import.meta.dirname, "..");
const existingGoFiles = new Set(["session_parsers.go", "session_parsers_test.go", "sh/validate_multi_secret_test.go"]);
const parserPackages = /^(?:yaml|js-yaml|work_queue_yaml)(?:[/.]|$)/;

function setupFiles(directory = setupDirectory) {
  return readdirSync(directory, { withFileTypes: true }).flatMap(entry => {
    if (entry.name === "node_modules" || entry.name === ".git") return [];
    const file = resolve(directory, entry.name);
    return entry.isDirectory() ? setupFiles(file) : entry.isFile() ? [file] : [];
  });
}

describe("setup runtime metadata boundary", () => {
  it("adds no Go implementations beyond the existing embedding and shell-test files", () => {
    const unexpected = setupFiles()
      .filter(file => extname(file) === ".go")
      .map(file => relative(setupDirectory, file))
      .filter(file => !existingGoFiles.has(file));
    expect(unexpected).toEqual([]);
  });

  it("contains no YAML parser modules, imports, or parser API references, including in tests", () => {
    const violations = [];
    for (const file of setupFiles()) {
      if (![".js", ".cjs", ".mjs", ".ts"].includes(extname(file))) continue;
      const source = readFileSync(file, "utf8");
      const imports = [...source.matchAll(/(?:require(?:\.resolve)?\s*\(|import\s*\(|from\s*)["']([^"']+)["']/g)].map(match => match[1]);
      if (
        imports.some(name => parserPackages.test(name) || /(?:^|\/)work_queue_yaml(?:\.cjs)?$/.test(name)) ||
        /(?:^|\/)work_queue_yaml\.cjs$/.test(file) ||
        /\b(?:parseDocument|parseAllDocuments)\s*\(|\.parseFrontmatter\s*\(/.test(source)
      ) {
        violations.push(relative(setupDirectory, file));
      }
    }
    expect(violations).toEqual([]);
    const manifest = JSON.parse(readFileSync(resolve(setupDirectory, "js/package.json"), "utf8"));
    const lock = JSON.parse(readFileSync(resolve(setupDirectory, "js/package-lock.json"), "utf8"));
    for (const dependencies of [manifest.dependencies, manifest.devDependencies, manifest.optionalDependencies]) {
      expect(Object.keys(dependencies ?? {}).filter(name => parserPackages.test(name))).toEqual([]);
    }
    expect(Object.keys(lock.packages).filter(name => parserPackages.test(name.replace(/^node_modules\//, "")))).toEqual([]);
  });

  it("does not build or install Go code in setup runtime scripts", () => {
    const violations = setupFiles()
      .filter(file => [".sh", ".js", ".cjs", ".mjs"].includes(extname(file)))
      .filter(file => {
        const source = readFileSync(file, "utf8");
        return /\bgo\s+(?:build|install)\b/.test(source) || /(?:execFileSync|spawnSync|execFile|spawn)\s*\(\s*["']go["']/.test(source);
      })
      .map(file => relative(setupDirectory, file));
    expect(violations).toEqual([]);
  });
});
