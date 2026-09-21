import { afterEach, describe, expect, it, vi } from "vitest";
import { createRequire } from "node:module";
import { execFileSync } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";

const require = createRequire(import.meta.url);
const {
  MAX_REPOSITORY_FAILURE_BYTES,
  MAX_REPOSITORY_DIAGNOSTIC_PATHS,
  MAX_REPOSITORY_DIAGNOSTIC_PATH_BYTES,
  REPOSITORY_DIAGNOSTIC_FALLBACK,
  createRepositoryFailure,
  createRepositoryEventFailure,
  repositoryCommandError,
  repositoryMutationError,
} = require("./copilot_sdk_repo_diagnostics.cjs");
const { BUILT_IN_PATTERNS } = require("./redact_secrets.cjs");
const bytes = value => Buffer.byteLength(JSON.stringify(value), "utf8");
const envelope = text => ({ resultType: "failure", textResultForLlm: text, error: text });

afterEach(() => vi.restoreAllMocks());

function failureText(result) {
  expect(result).toEqual(envelope(expect.any(String)));
  expect(result.error).toBe(result.textResultForLlm);
  expect(bytes(result)).toBeLessThanOrEqual(MAX_REPOSITORY_FAILURE_BYTES);
  return result.textResultForLlm;
}

describe("repository SDK failure diagnostics", () => {
  it("returns the supported SDK object, not a string or a success-shaped envelope", () => {
    const result = createRepositoryFailure(new Error("Validation failed."));
    expect(result).toEqual(envelope("Validation failed."));
    expect(JSON.parse(JSON.stringify(result))).toEqual(result);
    expect(createRepositoryFailure(new Error(""))).toEqual(envelope("Repository operation failed (no diagnostic message)."));
  });

  it("keeps the command, exit status, and both labeled streams without interpreting their words", () => {
    const error = repositoryCommandError("go test", { exitCode: 17, stdout: "PASS\nstdout-marker", stderr: "denied timeout cancelled\nstderr-marker" });
    const text = failureText(createRepositoryFailure(error));
    expect(text).toBe("go test failed with exit code 17\nstdout: PASS\\nstdout-marker\nstderr: denied timeout cancelled\\nstderr-marker");
    expect(error.message).toBe(text);
  });

  it.each(["x", "\\\u202e\x1b"])("preserves both streams when reformatting a bounded native failure (%j)", unit => {
    const native = createRepositoryFailure(
      repositoryCommandError("go test", {
        exitCode: 1,
        stdout: "stdout-marker " + unit.repeat(250_000),
        stderr: "stderr-marker",
      })
    );
    const text = failureText(createRepositoryEventFailure({ message: native.error }));
    expect(text).toContain("stdout: stdout-marker");
    expect(text).toContain("stderr: stderr-marker");
    expect(text).not.toMatch(/[\x00-\x1f\u202e]/);
  });

  it("redacts event secrets spanning display labels and lines before separating sections", () => {
    const declared = "stdout: declared-header-canary";
    const supplied = "stderr: supplied-line-canary\nsupplied-second-canary";
    const message = `${declared}\n${supplied}\nvisible-marker\n::add-mask::${declared}`;
    const text = failureText(createRepositoryEventFailure({ message }, [supplied]));
    for (const canary of ["declared-header-canary", "supplied-line-canary", "supplied-second-canary", "::add-mask::"]) expect(text).not.toContain(canary);
    expect(text).toContain("visible-marker");
    expect(text).not.toMatch(/[\x00-\x1f]/);
  });

  it("withholds excessive event sections and failing message extraction without raw fallback", () => {
    expect(createRepositoryEventFailure({ message: Array(33).fill("section-canary").join("\n") })).toEqual(envelope(REPOSITORY_DIAGNOSTIC_FALLBACK));
    expect(
      createRepositoryEventFailure({
        get message() {
          throw new Error("raw-extraction-canary");
        },
      })
    ).toEqual(envelope(REPOSITORY_DIAGNOSTIC_FALLBACK));
  });

  it("redacts complete streams before excerpts, including late and cross-stream declarations", () => {
    const githubToken = `ghp_${"a".repeat(36)}`;
    const canaries = ["late-stdout-secret", "cross-stream-secret", "late-tail-secret", "authorization-secret", "url-user:url-password", "url-query-secret", "supplied-auth-secret", githubToken];
    const stdout = [`stdout-marker ${canaries.join(" ")}`, `https://${canaries[4]}@example.invalid/private?access_token=${canaries[5]}`, "x".repeat(32_000), `::add-mask::${canaries[1]}`, `::add-mask::${canaries[2]}`].join("\n");
    const stderr = [`stderr-marker ${canaries[1]}`, `Authorization: Bearer ${canaries[3]}`, "y".repeat(32_000), `::add-mask::${canaries[0]}`].join("\n");
    const result = createRepositoryFailure(repositoryCommandError("go test", { exitCode: 1, stdout, stderr }), [`Bearer ${canaries[6]}`]);
    const text = failureText(result);
    for (const canary of canaries) expect(JSON.stringify(result)).not.toContain(canary);
    expect(text).toContain("stdout: stdout-marker");
    expect(text).toContain("stderr: stderr-marker");
    expect(text.match(/\[truncated\]/g)).toHaveLength(2);
    expect(text).not.toContain("::add-mask::");
  });

  it("uses built-in patterns before an exact mask can obscure a credential prefix", () => {
    const token = `ghs_${"A".repeat(36)}.installation-token`;
    const text = failureText(createRepositoryFailure(new Error(`${token}\n::add-mask::ghs_`)));
    expect(text).not.toContain("installation-token");
    expect(text).not.toContain("A".repeat(36));
  });

  it("does not let a built-in replacement obscure a longer exact credential mask", () => {
    const secret = `prefix-ghp_${"a".repeat(36)}-private-suffix`;
    const text = failureText(createRepositoryFailure(new Error(secret), [secret]));
    expect(text).toBe("***");
  });

  it("retains explicitly supplied masks before path excerpts or cleanup wrappers can shorten a value", () => {
    const secret = "private-path-canary-" + "x".repeat(512);
    const error = repositoryMutationError([], [`receipt-${secret}.json`], [secret]);
    const wrapped = new AggregateError([error], `${error.message}; repository temporary cleanup failed`);
    for (const result of [createRepositoryFailure(error), createRepositoryFailure(wrapped)]) expect(failureText(result)).not.toContain("private-path-canary");
    expect(JSON.stringify(error)).not.toContain(secret);
    const command = repositoryCommandError("go test", { exitCode: 1, stdout: secret, stderr: "" }, [secret]);
    expect(failureText(createRepositoryFailure(command))).not.toContain("private-path-canary");
  });

  it("decodes multiline add-mask values and protects bare credentials from already-held auth values", () => {
    const text = failureText(
      createRepositoryFailure(new Error('first-line-secret\nsecond-line-secret\nheld-secret\n::add-mask::first-line-secret%0Asecond-line-secret\n"Authorization": "Basic header-secret"\nheader-secret'), ["Bearer held-secret"])
    );
    for (const canary of ["first-line-secret", "second-line-secret", "held-secret", "header-secret"]) expect(text).not.toContain(canary);
    expect(text).toContain("***");
  });

  it("collects CR-delimited masks without turning source carriage returns into log lines", () => {
    const text = failureText(createRepositoryFailure(new Error("cr-secret\r::add-mask::cr-secret\rremaining output")));
    expect(text).not.toContain("cr-secret");
    expect(text).not.toContain("::add-mask::");
    expect(text).toContain("\\r");
    expect(text).not.toContain("\r");
  });

  it("renders controls, terminal escapes, bidi, markup and forged log framing literally", () => {
    const message = 'output\r\0\x1b[31mred\x1b]0;title\x07\x9b31m\u202e\u2066\u2028\n{"type":"forged"}\n::error::forged\ninline ::warning::forged\n<script>&`[link]</script>\n::add-mask::masked-canary\nmasked-canary';
    const result = createRepositoryFailure(new Error(message));
    const text = failureText(result);
    expect(text).not.toMatch(/[\x00-\x1f\x7f-\x9f\u2028-\u202e\u2066-\u2069]/);
    for (const literal of ["\\r", "\\u0000", "\\u001b", "\\u0007", "\\u009b", "\\u202e", "\\u2066", "\\u2028", "\\u007b", "\\u003c", "\\u0060", "\\u005b"]) expect(text).toContain(literal);
    for (const framing of ["::error::", "::warning::", "::add-mask::", "<script>", "masked-canary", "output\r"]) expect(text).not.toContain(framing);
    const jsonl = JSON.stringify({ type: "tool.execution_complete", data: { success: false, error: { message: text } } }) + "\n";
    expect(jsonl.split("\n")).toHaveLength(2);
    expect(JSON.parse(jsonl).data.error.message).toBe(text);
  });

  it("enforces the serialized B-1/B/B+1 boundary, counting duplicated fields", () => {
    const B = 8192;
    expect(MAX_REPOSITORY_FAILURE_BYTES).toBe(B);
    const overhead = bytes(envelope(""));
    const length = Math.floor((B - overhead) / 2);
    const below = envelope("x".repeat(length));
    const above = envelope("x".repeat(length + 1));
    // Identical string fields make envelope sizes odd: B itself is not representable.
    expect(overhead % 2).toBe(1);
    expect(bytes(below)).toBe(B - 1);
    expect(bytes(above)).toBe(B + 1);
    expect(createRepositoryFailure(new Error(below.error))).toEqual(below);
    const bounded = createRepositoryFailure(new Error(above.error));
    expect(failureText(bounded)).toContain("[truncated]");
    expect(bytes(bounded)).toBeLessThanOrEqual(B);
    expect(bytes(bounded)).toBe(B - 1);
  });

  it.each(["\u{1f642}", "\u754c", '\r\0\x1b"\\\u202e'])("bounds UTF-8 and JSON escape expansion without splitting rendered tokens (%j)", unit => {
    const text = failureText(createRepositoryFailure(new Error(unit.repeat(8_000))));
    expect(text).toContain("[truncated]");
    expect(text).not.toContain("\ufffd");
    const prefix = text.slice(0, -" [truncated]".length);
    expect(prefix.replace(/\\(?:u[0-9a-f]{4}|["\\bfnrt])|[^\\]/gu, "")).toBe("");
  });

  it("reserves an excerpt for a short stream even when the other stream exhausts the raw process budget", () => {
    for (const [stdout, stderr] of [
      ["stdout-marker " + "x".repeat(250_000), "stderr-marker"],
      ["stdout-marker", "stderr-marker " + "\x1b".repeat(250_000)],
    ]) {
      const text = failureText(createRepositoryFailure(repositoryCommandError("go test", { exitCode: 1, stdout, stderr })));
      expect(text).toContain("stdout: stdout-marker");
      expect(text).toContain("stderr: stderr-marker");
      expect(text).toContain("[truncated]");
    }
  });

  it("limits mutation summaries to 20 paths while showing both categories and an omission count", () => {
    const tracked = Array.from({ length: 21 }, (_, index) => `tracked-${String(index).padStart(2, "0")}.go`);
    const untracked = Array.from({ length: 10 }, (_, index) => `untracked-${index}.json`);
    const text = failureText(createRepositoryFailure(repositoryMutationError(tracked, untracked)));
    const paths = text.split("\n").filter(line => /^(?:tracked change|untracked addition): /.test(line));
    expect(paths).toHaveLength(MAX_REPOSITORY_DIAGNOSTIC_PATHS);
    expect(text).toContain("Tracked changes: 21; untracked additions: 10.");
    expect(paths[0]).toBe("tracked change: tracked-00.go");
    expect(paths[1]).toBe("untracked addition: untracked-0.json");
    expect(text).toContain("[truncated: 11 additional mutation paths omitted]");
  });

  it.each([255, 256, 257])("bounds a %d-byte rendered path at 256 bytes including its marker", length => {
    const text = failureText(createRepositoryFailure(repositoryMutationError([], ["x".repeat(length)])));
    const renderedPath = text
      .split("\n")
      .find(line => line.startsWith("untracked addition: "))
      .slice("untracked addition: ".length);
    expect(Buffer.byteLength(renderedPath, "utf8")).toBe(Math.min(length, MAX_REPOSITORY_DIAGNOSTIC_PATH_BYTES));
    expect(renderedPath.includes("[truncated]")).toBe(length > MAX_REPOSITORY_DIAGNOSTIC_PATH_BYTES);
  });

  it("bounds many multibyte and escaped paths by both path size and the final envelope", () => {
    const text = failureText(
      createRepositoryFailure(
        repositoryMutationError(
          Array.from({ length: 25 }, (_, index) => `${index}-\u754c\u202e`.repeat(200)),
          []
        )
      )
    );
    const paths = text.split("\n").filter(line => line.startsWith("tracked change: "));
    expect(paths).toHaveLength(20);
    for (const line of paths) {
      const renderedPath = line.slice("tracked change: ".length);
      expect(Buffer.byteLength(renderedPath, "utf8")).toBeLessThanOrEqual(256);
      expect(renderedPath).toContain("[truncated]");
      expect(renderedPath).not.toMatch(/[\u202e\ufffd]/);
    }
  });

  it("redacts from undisplayed paths before selecting the first 20", () => {
    const paths = ["first-secret.txt", ...Array.from({ length: 19 }, (_, index) => `file-${index}.txt`), "::add-mask::first-secret.txt"];
    const text = failureText(createRepositoryFailure(repositoryMutationError(paths, [])));
    expect(text).not.toContain("first-secret.txt");
    expect(text).toContain("1 additional mutation paths omitted");
  });

  it("never expands stacks, causes or aggregate errors", () => {
    const error = new AggregateError([new Error("aggregate-secret")], "Outer failure", { cause: new Error("cause-secret") });
    for (const property of ["stack", "cause", "errors"]) {
      Object.defineProperty(error, property, {
        get: () => {
          throw new Error("must not inspect nested errors");
        },
      });
    }
    expect(createRepositoryFailure(error)).toEqual(envelope("Outer failure"));
    expect(createRepositoryFailure({ message: "<html>private response</html>", status: 502 })).toEqual(envelope("GitHub returned an unexpected HTML response (HTTP 502)"));
  });

  it("returns only the fixed, prebounded failure if extraction or redaction throws", () => {
    const malformed = {
      get message() {
        throw new Error("extraction-secret");
      },
    };
    expect(createRepositoryFailure(malformed)).toEqual(envelope(REPOSITORY_DIAGNOSTIC_FALLBACK));
    vi.spyOn(BUILT_IN_PATTERNS[0].pattern, "exec").mockImplementation(() => {
      throw new Error("redaction-secret");
    });
    const result = createRepositoryFailure(repositoryCommandError("go test", { exitCode: 1, stdout: "raw-secret", stderr: "other-raw-secret" }));
    expect(result).toEqual(envelope(REPOSITORY_DIAGNOSTIC_FALLBACK));
    failureText(result);
    expect(JSON.stringify(result)).not.toContain("secret");
  });
});

it("loads the separately copied actions runtime without an @actions/core startup dependency or credential discovery", () => {
  const source = path.dirname(fileURLToPath(import.meta.url));
  const scratch = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-repository-diagnostics-runtime-"));
  try {
    for (const file of fs.readdirSync(source, { withFileTypes: true })) {
      if (file.isFile() && (file.name.endsWith(".json") || (file.name.endsWith(".cjs") && !file.name.endsWith(".test.cjs")))) fs.copyFileSync(path.join(source, file.name), path.join(scratch, file.name));
    }
    fs.mkdirSync(path.join(scratch, ".copilot"));
    fs.writeFileSync(path.join(scratch, ".copilot", "mcp-config.json"), JSON.stringify({ mcpServers: { private: { headers: { Authorization: "must-not-discover-secret" } } } }));
    const script = `
      const assert = require("node:assert/strict");
      const fs = require("node:fs");
      const path = require("node:path");
      const Module = require("node:module");
      const originalLoad = Module._load;
      Module._load = function(name, parent, main) {
        assert.ok(!name.startsWith("@actions/core"), "Unexpected @actions/core startup dependency");
        if (parent?.filename && path.dirname(parent.filename) === process.cwd() && !Module.isBuiltin(name) && !path.isAbsolute(name) && !name.startsWith(".")) {
          assert.ok(["@github/copilot-sdk", "minimatch", "undici"].includes(name), "Unexpected copied-runtime dependency: " + name);
        }
        return originalLoad.call(this, name, parent, main);
      };
      let credentialReads = 0;
      const originalRead = fs.readFileSync;
      fs.readFileSync = function(filename, ...args) {
        if (String(filename).endsWith("mcp-config.json")) credentialReads++;
        return originalRead.call(this, filename, ...args);
      };
      const diagnostics = require("./copilot_sdk_repo_diagnostics.cjs");
      for (const name of ["copilot_sdk_repo_workspace", "copilot_sdk_repo_tools", "copilot_sdk_session"]) require("./" + name + ".cjs");
      const result = diagnostics.createRepositoryFailure(new Error("fixture diagnostic"));
      assert.equal(result.resultType, "failure");
      assert.equal(result.error, "fixture diagnostic");
      assert.equal(credentialReads, 0);
      process.stdout.write("copied runtime loaded");
    `;
    const env = {
      ...Object.fromEntries(["PATH", "Path", "SystemRoot", "WINDIR", "PATHEXT"].filter(key => process.env[key]).map(key => [key, process.env[key]])),
      NODE_PATH: path.join(source, "node_modules"),
      HOME: scratch,
      USERPROFILE: scratch,
    };
    expect(execFileSync(process.execPath, ["--input-type=commonjs", "-e", script], { cwd: scratch, env, encoding: "utf8", timeout: 10_000, stdio: ["ignore", "pipe", "pipe"] })).toBe("copied runtime loaded");
  } finally {
    fs.rmSync(scratch, { recursive: true, force: true, maxRetries: 3, retryDelay: 100 });
  }
});
