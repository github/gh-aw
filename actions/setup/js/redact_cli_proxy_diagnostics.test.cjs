// @ts-check
const path = require("node:path");
const { spawnSync } = require("node:child_process");
const { PROXY_ENV_NAMES } = require("./constants.cjs");
const { MAX_DIAGNOSTIC_BYTES, redactCliProxyDiagnostics } = require("./redact_cli_proxy_diagnostics.cjs");

describe("CLI proxy console diagnostic sanitizer", () => {
  const script = path.join(__dirname, "redact_cli_proxy_diagnostics.cjs");

  function capture(content, env = {}) {
    return spawnSync(process.execPath, [script, "--capture"], {
      input: content,
      encoding: "utf8",
      env: { PATH: process.env.PATH, ...env },
    });
  }

  it("withholds the entire overflowing capture, including complete final lines", () => {
    const result = capture(`::add-mask::early-secret\n${"x".repeat(2 * MAX_DIAGNOSTIC_BYTES)}\nearly-secret late-secret\n::add-mask::late-secret\nfinal line\n`);
    expect(result.status).toBe(2);
    expect(result.stderr).toBe("");
    expect(result.stdout).toBe("");
  });

  it("withholds both a split credential and the rest of an overflowing capture", () => {
    const secret = "synthetic-credential";
    const result = capture(`${"x".repeat(MAX_DIAGNOSTIC_BYTES)}${secret}\n${"y".repeat(MAX_DIAGNOSTIC_BYTES - secret.length)}\n`, { GH_TOKEN: secret });
    expect(result.status).toBe(2);
    expect(result.stdout).toBe("");
    expect(result.stderr).toBe("");
  });

  it("does not publish a giant unterminated partial line", () => {
    const result = capture("x".repeat(2 * MAX_DIAGNOSTIC_BYTES));
    expect(result.status).toBe(2);
    expect(result.stdout).toBe("");
    expect(result.stderr).toBe("");
  });

  it("fails closed when mask metadata exceeds the capture budget", () => {
    const result = capture(`::add-mask::${"x".repeat(MAX_DIAGNOSTIC_BYTES + 1)}\nfinal line\n`);
    expect(result.status).toBe(2);
    expect(result.stdout).toBe("");
    expect(result.stderr).toBe("");
  });

  it("fails closed when many distinct small masks exceed the count budget", () => {
    const commands = Array.from({ length: 1025 }, (_, index) => `::add-mask::mask-${index}\n`).join("");
    const result = capture(`${commands}final line\n`);
    expect(result.status).toBe(1);
    expect(result.stdout).toBe("");
    expect(result.stderr).toBe("");
  });

  it("normalizes NUL bytes before matching environment credentials and collecting masks", () => {
    const secret = "synthetic-proxy-credential";
    const output = redactCliProxyDiagnostics(`synthetic-proxy-\0credential\n::add-\0mask::runtime-\0credential\nruntime-credential\nruntime-\0credential`, { GH_TOKEN: secret });
    expect(output).not.toContain("\0");
    expect(output).not.toContain(secret);
    expect(output).not.toContain("runtime-credential");
    expect(output).not.toContain("::add-mask::");
  });

  it.each([
    ['synthetic"password', 'token="synthetic"password"'],
    ['synthetic"password', 'token=synthetic\\"password'],
    ['synthetic"password', 'token="synthetic\\"password"'],
    ['synthetic"password', "token=synthetic\\u0022password"],
    ['synthetic"\\password', "token=synthetic\\u0022\\u005cpassword"],
  ])("preserves physical lines when redacting %s from %s", (secret, spelling) => {
    const content = Array.from({ length: 32 }, (_, index) => `line-${index + 1}: ${spelling}`).join("\n") + "\n";
    const redacted = redactCliProxyDiagnostics(content, { GH_TOKEN: secret });
    expect(redacted.trimEnd().split("\n")).toHaveLength(32);
    expect(redacted).not.toContain(secret);
    expect(redacted).not.toContain("password");
    const result = capture(content, { GH_TOKEN: secret });
    expect(result.status).toBe(0);
    expect(result.stderr).toBe("");
    expect(result.stdout.split("\n")).toHaveLength(20);
    expect(result.stdout).toContain("line-13:");
    expect(result.stdout).toContain("line-32:");
    expect(result.stdout).not.toContain("line-12:");
    expect(result.stdout).not.toContain("password");
  });

  it("collects masks before truncation and preserves built-in and supplied arbitrary-secret coverage", () => {
    const builtin = `ghs_${"a".repeat(40)}`;
    const content = `::add-mask::runtime-secret\n${"safe line\n".repeat(30)}runtime-secret ${builtin} x$ first-line second-line custom-value\n`;
    const result = capture(content, {
      GITHUB_TOKEN: "x$",
      GH_AW_SECRET_NAMES: "CUSTOM, MULTILINE",
      SECRET_CUSTOM: "custom-value",
      MULTILINE: " first-line \nsecond-line",
    });
    expect(result.status).toBe(0);
    expect(result.stdout.split("\n")).toHaveLength(20);
    for (const secret of ["runtime-secret", builtin, "x$", "first-line", "second-line", "custom-value"]) expect(result.stdout).not.toContain(secret);
    expect(result.stdout).toContain("safe line");
  });

  it("redacts every supported proxy environment variable", () => {
    expect(PROXY_ENV_NAMES).toEqual(["HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "http_proxy", "https_proxy", "no_proxy"]);
    for (const name of PROXY_ENV_NAMES) {
      const value = `synthetic-${name}`;
      expect(redactCliProxyDiagnostics(`proxy failure: ${value}`, { [name]: value })).not.toContain(value);
    }
  });

  it("redacts the complete capture before trailing mask commands can evict diagnostics", () => {
    const result = capture(`connection refused: runtime-secret\n${"::add-mask::runtime-secret\n".repeat(20)}`);
    expect(result.status).toBe(0);
    expect(result.stderr).toBe("");
    expect(result.stdout).toBe("connection refused: ***");
  });

  it("accepts the exact byte limit", () => {
    const result = capture("a".repeat(MAX_DIAGNOSTIC_BYTES));
    expect(result.status).toBe(0);
    expect(result.stdout).toHaveLength(MAX_DIAGNOSTIC_BYTES);
    expect(result.stderr).toBe("");
  });

  it("rejects one byte over the capture limit without publishing diagnostics", () => {
    const result = capture("a".repeat(MAX_DIAGNOSTIC_BYTES + 1));
    expect(result.status).toBe(2);
    expect(result.stdout).toBe("");
    expect(result.stderr).toBe("");
  });

  it("counts UTF-8 bytes rather than characters and emits no partial diagnostics on failure", () => {
    const result = capture("é".repeat(MAX_DIAGNOSTIC_BYTES / 2 + 1));
    expect(result.status).toBe(2);
    expect(result.stdout).toBe("");
    expect(result.stderr).toBe("");
  });

  it("counts NUL bytes against the raw capture limit", () => {
    const result = capture("\0".repeat(MAX_DIAGNOSTIC_BYTES + 1));
    expect(result.status).toBe(2);
    expect(result.stdout).toBe("");
    expect(result.stderr).toBe("");
  });

  it("withholds the entire output when redaction expands it beyond the byte cap", () => {
    const result = capture(`${"a".repeat(MAX_DIAGNOSTIC_BYTES - 6)}\nx x`, { GH_TOKEN: "x" });
    expect(result.status).toBe(1);
    expect(result.stdout).toBe("");
    expect(result.stderr).toBe("");
  });
});
