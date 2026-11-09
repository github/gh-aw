import { readdirSync, readFileSync } from "node:fs";
import { dirname, extname, posix, relative, resolve, win32 } from "node:path";
import { describe, expect, it } from "vitest";

const setupDirectory = resolve(import.meta.dirname, "..");
const actionsDirectory = resolve(setupDirectory, "..");
const existingGoFiles = new Set(["setup/session_parsers.go", "setup/session_parsers_test.go", "setup/sh/validate_multi_secret_test.go"]);
const parserPackages = /^(?:yaml|js-yaml|work_queue_yaml)(?:[/.]|$)/;

function normalizeSeparators(file) {
  return file.replaceAll("\\", "/");
}

function actionFiles(directory = actionsDirectory) {
  return readdirSync(directory, { withFileTypes: true }).flatMap(entry => {
    if (entry.name === "node_modules" || entry.name === ".git" || entry.name.startsWith(".setup-")) return [];
    const file = resolve(directory, entry.name);
    return entry.isDirectory() ? actionFiles(file) : entry.isFile() ? [file] : [];
  });
}

describe("Actions runtime metadata boundary", () => {
  it.each([posix, win32])("recognizes the existing Go allowlist with platform-specific paths (%#)", path => {
    const root = path.resolve("actions");
    const existing = [...existingGoFiles].map(file => normalizeSeparators(path.relative(root, path.resolve(root, file))));
    expect(existing.filter(file => !existingGoFiles.has(file))).toEqual([]);
    expect(existing).toEqual([...existingGoFiles]);
  });

  it("adds no Go implementations beyond the existing embedding and shell-test files", () => {
    const unexpected = actionFiles()
      .filter(file => extname(file) === ".go")
      .map(file => normalizeSeparators(relative(actionsDirectory, file)))
      .filter(file => !existingGoFiles.has(file));
    expect(unexpected).toEqual([]);
  });

  it("contains no Go modules or native metadata executables anywhere in Actions", () => {
    expect(actionFiles().filter(file => /(?:^|\/)(?:go\.(?:mod|sum)|markdown-metadata(?:\.exe)?)$/.test(normalizeSeparators(file)))).toEqual([]);
  });

  it("reads metadata in JavaScript without launching an external parser", () => {
    const source = readFileSync(resolve(setupDirectory, "js/markdown_metadata.cjs"), "utf8");
    expect(source).not.toMatch(/["'](?:node:)?child_process["']/);
    expect(source).not.toContain('"markdown-metadata"');
    expect(source).not.toContain('"markdown-metadata.exe"');
  });

  it("keeps repository-local runtime dependencies inside setup", () => {
    const violations = [];
    for (const file of actionFiles()) {
      if (extname(file) !== ".cjs" || /(?:\.test|_checks)\.cjs$/.test(file)) continue;
      const source = readFileSync(file, "utf8");
      for (const match of source.matchAll(/require\s*\(\s*["'](\.[^"']+)["']\s*\)/g)) {
        const dependency = resolve(dirname(file), match[1]);
        const location = relative(setupDirectory, dependency);
        if (location === ".." || location.startsWith("../") || location.startsWith("..\\")) {
          violations.push(`${relative(actionsDirectory, file)}: ${match[1]}`);
        }
      }
    }
    expect(violations).toEqual([]);
  });

  it("contains no YAML parser modules, imports, or parser API references, including in tests", () => {
    const violations = [];
    for (const file of actionFiles()) {
      if (![".js", ".cjs", ".mjs", ".ts"].includes(extname(file))) continue;
      const source = readFileSync(file, "utf8");
      const imports = [...source.matchAll(/(?:require(?:\.resolve)?\s*\(|import\s*\(|from\s*)["']([^"']+)["']/g)].map(match => match[1]);
      if (
        imports.some(name => parserPackages.test(name) || /(?:^|\/)work_queue_yaml(?:\.cjs)?$/.test(name)) ||
        /(?:^|\/)work_queue_yaml\.cjs$/.test(normalizeSeparators(file)) ||
        /\b(?:parseDocument|parseAllDocuments)\s*\(|\.parseFrontmatter\s*\(/.test(source)
      ) {
        violations.push(relative(actionsDirectory, file));
      }
    }
    expect(violations).toEqual([]);
    for (const file of actionFiles().filter(file => /(?:^|\/)package(?:-lock)?\.json$/.test(normalizeSeparators(file)))) {
      const manifest = JSON.parse(readFileSync(file, "utf8"));
      for (const dependencies of [manifest.dependencies, manifest.devDependencies, manifest.optionalDependencies]) {
        expect(Object.keys(dependencies ?? {}).filter(name => parserPackages.test(name))).toEqual([]);
        expect(Object.values(dependencies ?? {}).filter(version => typeof version === "string" && /^npm:(?:yaml|js-yaml)(?:@|$)/.test(version))).toEqual([]);
      }
      expect(Object.keys(manifest.packages ?? {}).filter(name => parserPackages.test(name.replace(/^(?:.*\/)?node_modules\//, "")))).toEqual([]);
    }
  });

  it("does not build or install Go code in setup runtime scripts", () => {
    const violations = actionFiles()
      .filter(file => [".sh", ".js", ".cjs", ".mjs"].includes(extname(file)))
      .filter(file => {
        const source = readFileSync(file, "utf8");
        return /\bgo\s+(?:build|install)\b/.test(source) || /(?:execFileSync|spawnSync|execFile|spawn)\s*\(\s*["']go["']/.test(source) || /markdown-metadata\/(?:setup\.sh|vitest_setup\.mjs)/.test(source);
      })
      .map(file => relative(actionsDirectory, file));
    expect(violations).toEqual([]);
  });
});
