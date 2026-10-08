import { describe, it, expect } from "vitest";
import { createRequire } from "node:module";
const require = createRequire(import.meta.url);
const { parseDiagnostics, parseDiagnosticLanguages, isDiagnosticReport, MAX_INPUT_BYTES, MAX_REPORT_BYTES } = require("./command_diagnostics.cjs");
const { renderDiagnostics } = require("./command_diagnostics_render.cjs");

const context = { root: "/repo", cwd: "/repo" };
const parse = (output, languages = ["go", "typescript", "python"], extra = {}) => parseDiagnostics({ output, ...context, ...extra }, languages);

describe("built-in command diagnostics", () => {
  it("accepts scalar/list selection and rejects empty, duplicate, and external parsers", () => {
    expect(parseDiagnosticLanguages("go")).toEqual(["go"]);
    expect(parseDiagnosticLanguages(["go", "typescript", "python"])).toEqual(["go", "typescript", "python"]);
    expect(parseDiagnosticLanguages(undefined)).toEqual([]);
    for (const value of [[], ["go", "go"], "external/parser", null, false, ["python", 1]]) expect(() => parseDiagnosticLanguages(value)).toThrow();
  });

  it("parses Go compiler/vet locations, continuation text, packages and subtests", () => {
    const report = parse("# example/pkg\npkg/main.go:12:8: undefined: missing\n\tadditional compiler detail\n--- FAIL: TestExample/subtest (0.00s)\n    main_test.go:9: expected 2, got 1\nFAIL example/pkg 0.003s");
    expect(report.diagnostics).toEqual(
      expect.arrayContaining([
        expect.objectContaining({ language: "go", package: "example/pkg", location: { path: "pkg/main.go", line: 12, column: 8 }, message: "undefined: missing\nadditional compiler detail" }),
        expect.objectContaining({ test: "TestExample/subtest", location: { path: "main_test.go", line: 9 }, message: "expected 2, got 1" }),
      ])
    );
    expect(isDiagnosticReport(report)).toBe(true);
  });

  it("correlates interleaved go test JSON packages and tests", () => {
    const output = [
      { Action: "output", Package: "example/a", Test: "TestA", Output: "a_test.go:3: wrong value\n" },
      { Action: "output", Package: "example/b", Test: "TestB", Output: "b_test.go:4: wrong type\n" },
      { Action: "fail", Package: "example/b", Test: "TestB" },
      { Action: "fail", Package: "example/a", Test: "TestA" },
    ]
      .map(JSON.stringify)
      .join("\n");
    expect(parse(output).diagnostics).toMatchObject([
      { language: "go", package: "example/a", test: "TestA", location: { path: "a_test.go", line: 3 } },
      { language: "go", package: "example/b", test: "TestB", location: { path: "b_test.go", line: 4 } },
    ]);
  });

  it("groups Go panic frames instead of treating them as compiler errors", () => {
    const report = parse("panic: unexpected nil\n\ngoroutine 1 [running]:\nexample.main()\n\t/repo/main.go:18 +0x1\n");
    expect(report.diagnostics).toMatchObject([{ message: "panic: unexpected nil", related: [{ message: "Panic frame", location: { path: "main.go", line: 18 } }] }]);
  });

  it("does not treat passing Go test logs as errors and recognizes JSON build failures", () => {
    const passing = "=== RUN   TestLog\n    log_test.go:4: a test log\n--- PASS: TestLog (0.00s)\nPASS";
    expect(parse(passing, ["go"]).diagnostics).toEqual([]);
    const json = [
      { Action: "output", Package: "example/pkg", Test: "TestLog", Output: "log_test.go:4: a test log\n" },
      { Action: "pass", Package: "example/pkg", Test: "TestLog" },
      { Action: "build-output", ImportPath: "example/broken", Output: "broken.go:3:2: undefined: missing\n" },
      { Action: "build-fail", ImportPath: "example/broken" },
    ]
      .map(JSON.stringify)
      .join("\n");
    expect(parse(json, ["go"]).diagnostics).toMatchObject([{ package: "example/broken", location: { path: "broken.go", line: 3, column: 2 } }]);
    expect(parse(json, ["go"]).diagnostics).toHaveLength(1);
  });

  it.each([
    "src/main.ts(4,2): error TS2322: Type 'number' is not assignable to type 'string'.",
    "\u001b[96msrc/main.ts\u001b[0m:\u001b[93m4\u001b[0m:\u001b[93m2\u001b[0m - \u001b[91merror\u001b[0m TS2322: Type 'number' is not assignable to type 'string'.",
  ])("parses tsc standard and ANSI pretty output: %s", output => {
    expect(parse(output, ["typescript"]).diagnostics).toMatchObject([{ code: "TS2322", location: { path: "src/main.ts", line: 4, column: 2 } }]);
  });

  it("preserves tsc global errors, continuations, and related locations", () => {
    expect(parse("error TS18003: No inputs were found.\nsrc/main.ts(4,2): error TS2322: Wrong type\n  Type 'A' is not assignable.\n  src/types.ts:2:3\n    The expected type comes from here.", ["typescript"]).diagnostics).toMatchObject([
      { code: "TS18003", message: "No inputs were found." },
      { code: "TS2322", message: "Wrong type\nType 'A' is not assignable.", related: [{ location: { path: "src/types.ts", line: 2, column: 3 } }] },
    ]);
  });

  it("includes TypeScript configuration file diagnostics", () => {
    expect(parse("tsconfig.json(2,3): error TS5023: Unknown compiler option.", ["typescript"]).diagnostics).toMatchObject([{ code: "TS5023", location: { path: "tsconfig.json", line: 2, column: 3 } }]);
  });

  it("redacts authorization values echoed earlier in the command output", () => {
    expect(JSON.stringify(parse("main.go:1: echoed test-auth-secret\nAuthorization: Bearer test-auth-secret"))).not.toContain("test-auth-secret");
  });

  it("parses Python syntax/indentation errors and exception frames", () => {
    const report = parse('Traceback (most recent call last):\n  File "/repo/main.py", line 2, in <module>\n  File "/repo/helper.py", line 7, in run\nValueError: invalid argument\n', ["python"]);
    expect(report.diagnostics).toMatchObject([{ code: "ValueError", location: { path: "helper.py", line: 7 }, related: [{ location: { path: "main.py", line: 2 } }] }]);
    expect(parse('  File "/repo/main.py", line 4\n    if True\nSyntaxError: expected colon', ["python"]).diagnostics).toMatchObject([{ code: "SyntaxError", location: { path: "main.py", line: 4 } }]);
    expect(parse('  File "/repo/main.py", line 5\nIndentationError: unexpected indent', ["python"]).diagnostics[0].code).toBe("IndentationError");
  });

  it("handles pytest collection/test failures, unittest and chained exceptions", () => {
    expect(parse("________________ test_example ________________\nE   AssertionError: wrong value\ntest_example.py:8: AssertionError\nFAILED test_example.py::test_example - AssertionError", ["python"]).diagnostics).toEqual(
      expect.arrayContaining([expect.objectContaining({ location: { path: "test_example.py", line: 8 } })])
    );
    expect(parse("ERROR collecting test_example.py\nE   ImportError: missing module", ["python"]).diagnostics).toEqual(expect.arrayContaining([expect.objectContaining({ code: "ImportError" })]));
    const chain =
      'ERROR: test_example (tests.TestExample)\nTraceback (most recent call last):\n  File "/repo/tests.py", line 3\nKeyError: missing\n\nDuring handling of the above exception, another exception occurred:\n\nTraceback (most recent call last):\n  File "/repo/tests.py", line 6\nValueError: invalid\n';
    expect(parse(chain, ["python"]).diagnostics.map(item => item.code)).toEqual(["KeyError", "ValueError"]);
  });

  it("does not infer a failure from PASS, timeout, permission words or unknown output", () => {
    expect(parse("PASS\npermission denied\ntimeout\nordinary output").diagnostics).toEqual([]);
    expect(parse("src/main.ts(1,1): error TS1005: bad", ["go"]).diagnostics).toEqual([]);
  });

  it("uses command cwd for locations, rejects traversal and keeps external locations as text", () => {
    expect(parse("main.go:1: bad", ["go"], { cwd: "/repo/sub" }).diagnostics[0].location.path).toBe("sub/main.go");
    for (const filename of ["../main.go", "/external/main.go", "/repo/sub/../main.go"]) {
      const diagnostic = parse(`${filename}:1: bad`, ["go"]).diagnostics[0];
      expect(diagnostic.location).toBeUndefined();
      expect(diagnostic.message).toContain(filename);
    }
    expect(parse("C:\\repo\\main.go:3:2: bad", ["go"], { root: "C:\\repo", cwd: "C:\\repo" }).diagnostics[0].location).toEqual({ path: "main.go", line: 3, column: 2 });
  });

  it("redacts complete cross-stream inputs before parsing and display selection", () => {
    const report = parseDiagnostics({ stdout: "main.go:1: late-secret", stderr: "::add-mask::late-secret", ...context }, ["go"]);
    expect(JSON.stringify(report)).not.toContain("late-secret");
    const supplied = parseDiagnostics({ output: "main.go:1: supplied-secret", ...context }, ["go"], { secrets: ["supplied-secret"] });
    expect(JSON.stringify(supplied)).not.toContain("supplied-secret");
    expect(JSON.stringify(parse("main.go:1: Authorization: Bearer arbitrary-secret"))).not.toContain("arbitrary-secret");
  });

  it("deduplicates streams and enforces input, report, and UTF-8 display budgets", () => {
    const line = "main.go:1: bad";
    expect(parseDiagnostics({ stdout: line, stderr: line, ...context }, ["go"]).diagnostics).toHaveLength(1);
    const oversized = parse("x".repeat(MAX_INPUT_BYTES + 1));
    expect(oversized).toMatchObject({ truncated: true, diagnostics: [], issues: ["Diagnostic input exceeds 256 KiB; parsing withheld."] });
    const report = parse(Array.from({ length: 200 }, (_, i) => `main.go:${i + 1}: ${"😀".repeat(200)}`).join("\n"));
    expect(report.truncated).toBe(true);
    expect(report.diagnostics.length).toBeLessThanOrEqual(50);
    expect(Buffer.byteLength(JSON.stringify(report))).toBeLessThanOrEqual(MAX_REPORT_BYTES);
    const rendered = renderDiagnostics(report);
    expect(Buffer.byteLength(rendered)).toBeLessThanOrEqual(4000);
    expect(rendered).not.toContain("\ufffd");
    expect(rendered).toContain("[diagnostics truncated]");
  });

  it("renders controls and markup literally and rejects malformed reports", () => {
    const rendered = renderDiagnostics(parse("main.go:1: <script>`::error::\u202e"));
    expect(rendered).not.toContain("<script>");
    expect(rendered).not.toContain("::error::");
    expect(rendered).not.toContain("\u202e");
    expect(() => renderDiagnostics({ version: 1, diagnostics: "not an array" })).toThrow("Invalid command diagnostic report");
  });

  it("withholds unsafe locations and marks bounded related messages as truncated", () => {
    const report = parse("src/main.ts(1,1): error TS2322: Wrong type\n  /outside/types.ts:2:3\n    " + "x".repeat(1500), ["typescript"]);
    expect(report.truncated).toBe(true);
    expect(report.diagnostics[0].related[0].location).toBeUndefined();
    expect(report.diagnostics[0].related[0].message).toContain("/outside/types.ts:2:");
    expect(isDiagnosticReport(report)).toBe(true);
    const unsafe = parse("unsafe\u0085.go:1: bad", ["go"]);
    expect(unsafe.diagnostics[0].location).toBeUndefined();
    expect(isDiagnosticReport(unsafe)).toBe(true);
    expect(() => renderDiagnostics(unsafe)).not.toThrow();
    expect(isDiagnosticReport({ ...report, diagnostics: [{ language: "go", severity: "error", message: "bad", location: { path: "https://example.com/main.go", line: 1 } }] })).toBe(false);
  });
});
