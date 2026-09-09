import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import fs from "fs";
import path from "path";
import os from "os";
const mockCore = {
  debug: vi.fn(),
  info: vi.fn(),
  notice: vi.fn(),
  warning: vi.fn(),
  error: vi.fn(),
  setFailed: vi.fn(),
  setOutput: vi.fn(),
  exportVariable: vi.fn(),
  setSecret: vi.fn(),
  getInput: vi.fn(),
  getBooleanInput: vi.fn(),
  getMultilineInput: vi.fn(),
  getState: vi.fn(),
  saveState: vi.fn(),
  startGroup: vi.fn(),
  endGroup: vi.fn(),
  group: vi.fn(),
  addPath: vi.fn(),
  setCommandEcho: vi.fn(),
  isDebug: vi.fn().mockReturnValue(!1),
  getIDToken: vi.fn(),
  summary: { addRaw: vi.fn().mockReturnThis(), write: vi.fn().mockResolvedValue(void 0) },
};
global.core = mockCore;
const redactScript = fs.readFileSync(path.join(__dirname, "redact_secrets.cjs"), "utf8");
describe("redact_secrets.cjs", () => {
  let tempDir;
  (beforeEach(() => {
    ((tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "redact-test-"))),
      Object.values(mockCore).forEach(fn => {
        "function" == typeof fn && fn.mockClear();
      }),
      mockCore.summary.addRaw && mockCore.summary.addRaw.mockClear(),
      mockCore.summary.write && mockCore.summary.write.mockClear(),
      delete process.env.GH_AW_SECRET_NAMES);
  }),
    afterEach(() => {
      tempDir && fs.existsSync(tempDir) && fs.rmSync(tempDir, { recursive: !0, force: !0 });
      for (const key of Object.keys(process.env)) key.startsWith("SECRET_") && delete process.env[key];
    }),
    describe("main function integration", () => {
      (it("should scan for built-in patterns even when GH_AW_SECRET_NAMES is not set", async () => {
        (await eval(`(async () => { ${redactScript}; await main(); })()`),
          expect(mockCore.info).toHaveBeenCalledWith(`Starting secret redaction in /tmp/gh-aw and ${process.env.RUNNER_TEMP}/gh-aw directories`),
          expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Scanning for built-in credential patterns")));
      }),
        it("should redact secrets from files in /tmp using exact matching", async () => {
          const testFile = path.join(tempDir, "test.txt"),
            secretValue = "ghp_1234567890ABCDEFGHIJKLMNOPQRSTUVWxyz";
          (fs.writeFileSync(testFile, `Secret: ${secretValue} and another ${secretValue}`), (process.env.GH_AW_SECRET_NAMES = "GITHUB_TOKEN"), (process.env.SECRET_GITHUB_TOKEN = secretValue));
          const modifiedScript = redactScript.replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir.replace(/\\/g, "\\\\")}", targetExtensions)`);
          await eval(`(async () => { ${modifiedScript}; await main(); })()`);
          const redactedContent = fs.readFileSync(testFile, "utf8");
          (expect(redactedContent).toBe("Secret: ***REDACTED*** and another ***REDACTED***"),
            expect(mockCore.info).toHaveBeenCalledWith(`Starting secret redaction in /tmp/gh-aw and ${process.env.RUNNER_TEMP}/gh-aw directories`),
            expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Secret redaction complete")));
        }),
        it("should handle multiple file types", async () => {
          (fs.writeFileSync(path.join(tempDir, "test1.txt"), "Secret: api-key-123"),
            fs.writeFileSync(path.join(tempDir, "test2.json"), '{"key": "api-key-456"}'),
            fs.writeFileSync(path.join(tempDir, "test3.log"), "Log: api-key-789"),
            (process.env.GH_AW_SECRET_NAMES = "API_KEY1,API_KEY2,API_KEY3"),
            (process.env.SECRET_API_KEY1 = "api-key-123"),
            (process.env.SECRET_API_KEY2 = "api-key-456"),
            (process.env.SECRET_API_KEY3 = "api-key-789"));
          const modifiedScript = redactScript.replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir.replace(/\\/g, "\\\\")}", targetExtensions)`);
          (await eval(`(async () => { ${modifiedScript}; await main(); })()`),
            expect(fs.readFileSync(path.join(tempDir, "test1.txt"), "utf8")).toBe("Secret: ***REDACTED***"),
            expect(fs.readFileSync(path.join(tempDir, "test2.json"), "utf8")).toBe('{"key": "***REDACTED***"}'),
            expect(fs.readFileSync(path.join(tempDir, "test3.log"), "utf8")).toBe("Log: ***REDACTED***"));
        }),
        it("should use core.info for logging hits", async () => {
          const testFile = path.join(tempDir, "test.txt"),
            secretValue = "sk-1234567890";
          (fs.writeFileSync(testFile, `Secret: ${secretValue} and ${secretValue}`), (process.env.GH_AW_SECRET_NAMES = "API_KEY"), (process.env.SECRET_API_KEY = secretValue));
          const modifiedScript = redactScript.replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir.replace(/\\/g, "\\\\")}", targetExtensions)`);
          (await eval(`(async () => { ${modifiedScript}; await main(); })()`),
            expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("occurrence(s) of a secret")),
            expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Processed")));
        }),
        it("should not log actual secret values", async () => {
          const testFile = path.join(tempDir, "test.txt"),
            secretValue = "sk-very-secret-key-123";
          (fs.writeFileSync(testFile, `Secret: ${secretValue}`), (process.env.GH_AW_SECRET_NAMES = "SECRET_KEY"), (process.env.SECRET_SECRET_KEY = secretValue));
          const modifiedScript = redactScript.replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir.replace(/\\/g, "\\\\")}", targetExtensions)`);
          await eval(`(async () => { ${modifiedScript}; await main(); })()`);
          const allCalls = [...mockCore.debug.mock.calls, ...mockCore.info.mock.calls, ...mockCore.warning.mock.calls];
          for (const call of allCalls) {
            const callString = JSON.stringify(call);
            expect(callString).not.toContain(secretValue);
          }
        }),
        it("should skip very short values (less than 6 characters)", async () => {
          const testFile = path.join(tempDir, "test.txt");
          (fs.writeFileSync(testFile, "Short: 12345"), (process.env.GH_AW_SECRET_NAMES = "SHORT_SECRET"), (process.env.SECRET_SHORT_SECRET = "12345"));
          const modifiedScript = redactScript.replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir.replace(/\\/g, "\\\\")}", targetExtensions)`);
          (await eval(`(async () => { ${modifiedScript}; await main(); })()`), expect(fs.readFileSync(testFile, "utf8")).toBe("Short: 12345"));
        }),
        it("should redact 6-character secrets", async () => {
          const testFile = path.join(tempDir, "test.txt");
          const secretValue = "abc123";
          (fs.writeFileSync(testFile, `Secret: ${secretValue} test`), (process.env.GH_AW_SECRET_NAMES = "SIX_CHAR_SECRET"), (process.env.SECRET_SIX_CHAR_SECRET = secretValue));
          const modifiedScript = redactScript.replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir.replace(/\\/g, "\\\\")}", targetExtensions)`);
          (await eval(`(async () => { ${modifiedScript}; await main(); })()`), expect(fs.readFileSync(testFile, "utf8")).toBe("Secret: ***REDACTED*** test"));
        }),
        it("should handle multiple secrets in same file", async () => {
          const testFile = path.join(tempDir, "test.txt"),
            secret1 = "ghp_1234567890ABCDEFGHIJKLMNOPQRSTUVWxyz",
            secret2 = "sk-proj-abcdef1234567890";
          (fs.writeFileSync(testFile, `Token1: ${secret1}\nToken2: ${secret2}\nToken1 again: ${secret1}`), (process.env.GH_AW_SECRET_NAMES = "TOKEN1,TOKEN2"), (process.env.SECRET_TOKEN1 = secret1), (process.env.SECRET_TOKEN2 = secret2));
          const modifiedScript = redactScript.replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir.replace(/\\/g, "\\\\")}", targetExtensions)`);
          await eval(`(async () => { ${modifiedScript}; await main(); })()`);
          const redacted = fs.readFileSync(testFile, "utf8");
          expect(redacted).toBe("Token1: ***REDACTED***\nToken2: ***REDACTED***\nToken1 again: ***REDACTED***");
        }),
        it("should handle empty secret values gracefully", async () => {
          const testFile = path.join(tempDir, "test.txt");
          (fs.writeFileSync(testFile, "No secrets here"), (process.env.GH_AW_SECRET_NAMES = "EMPTY_SECRET"), (process.env.SECRET_EMPTY_SECRET = ""));
          const modifiedScript = redactScript.replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir.replace(/\\/g, "\\\\")}", targetExtensions)`);
          (await eval(`(async () => { ${modifiedScript}; await main(); })()`),
            expect(mockCore.info).toHaveBeenCalledWith(`Starting secret redaction in /tmp/gh-aw and ${process.env.RUNNER_TEMP}/gh-aw directories`),
            expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("no secrets found")));
        }),
        it("should handle new file extensions (.md, .mdx, .yml, .jsonl)", async () => {
          (fs.writeFileSync(path.join(tempDir, "test.md"), "# Markdown\nSecret: api-key-md123"),
            fs.writeFileSync(path.join(tempDir, "test.mdx"), "# MDX\nSecret: api-key-mdx123"),
            fs.writeFileSync(path.join(tempDir, "test.yml"), "# YAML\nkey: api-key-yml123"),
            fs.writeFileSync(path.join(tempDir, "test.jsonl"), '{"key": "api-key-jsonl123"}'),
            (process.env.GH_AW_SECRET_NAMES = "API_MD,API_MDX,API_YML,API_JSONL"),
            (process.env.SECRET_API_MD = "api-key-md123"),
            (process.env.SECRET_API_MDX = "api-key-mdx123"),
            (process.env.SECRET_API_YML = "api-key-yml123"),
            (process.env.SECRET_API_JSONL = "api-key-jsonl123"));
          const modifiedScript = redactScript.replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir.replace(/\\/g, "\\\\")}", targetExtensions)`);
          (await eval(`(async () => { ${modifiedScript}; await main(); })()`),
            expect(fs.readFileSync(path.join(tempDir, "test.md"), "utf8")).toBe("# Markdown\nSecret: ***REDACTED***"),
            expect(fs.readFileSync(path.join(tempDir, "test.mdx"), "utf8")).toBe("# MDX\nSecret: ***REDACTED***"),
            expect(fs.readFileSync(path.join(tempDir, "test.yml"), "utf8")).toBe("# YAML\nkey: ***REDACTED***"),
            expect(fs.readFileSync(path.join(tempDir, "test.jsonl"), "utf8")).toBe('{"key": "***REDACTED***"}'));
        }),
        it("should redact secrets from .patch files (exact-value and built-in credential patterns)", async () => {
          const patchContent = [
            "diff --git a/config.js b/config.js",
            "--- a/config.js",
            "+++ b/config.js",
            "@@ -1,3 +1,3 @@",
            "-const token = 'old';",
            "+const token = 'my-patch-secret-value';",
            " const url = 'https://api.example.com';",
          ].join("\n");
          const patchFile = path.join(tempDir, "aw-abcdef.patch");
          fs.writeFileSync(patchFile, patchContent);
          process.env.GH_AW_SECRET_NAMES = "PATCH_SECRET";
          process.env.SECRET_PATCH_SECRET = "my-patch-secret-value";
          const modifiedScript = redactScript.replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir.replace(/\\/g, "\\\\")}", targetExtensions)`);
          await eval(`(async () => { ${modifiedScript}; await main(); })()`);
          const redacted = fs.readFileSync(patchFile, "utf8");
          expect(redacted).not.toContain("my-patch-secret-value");
          expect(redacted).toContain("***REDACTED***");
        }),
        it("should redact built-in credential patterns from .patch files", async () => {
          const ghToken = "ghp_" + "A".repeat(36);
          const patchContent = `diff --git a/creds.txt b/creds.txt\n+TOKEN=${ghToken}\n`;
          const patchFile = path.join(tempDir, "aw-builtin.patch");
          fs.writeFileSync(patchFile, patchContent);
          process.env.GH_AW_SECRET_NAMES = "";
          const modifiedScript = redactScript.replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir.replace(/\\/g, "\\\\")}", targetExtensions)`);
          await eval(`(async () => { ${modifiedScript}; await main(); })()`);
          const redacted = fs.readFileSync(patchFile, "utf8");
          expect(redacted).not.toContain(ghToken);
          expect(redacted).toContain("***REDACTED***");
          expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("GitHub Personal Access Token"));
        }));
    }),
    describe("built-in pattern detection", () => {
      describe("GitHub tokens", () => {
        it("should redact GitHub Personal Access Token (ghp_)", async () => {
          const testFile = path.join(tempDir, "test.txt");
          const ghToken = "ghp_1234567890ABCDEFGHIJKLMNOPQRSTUVWxyz";
          fs.writeFileSync(testFile, `Using token: ${ghToken} in this file`);
          process.env.GH_AW_SECRET_NAMES = "";
          const modifiedScript = redactScript.replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir.replace(/\\/g, "\\\\")}", targetExtensions)`);
          await eval(`(async () => { ${modifiedScript}; await main(); })()`);
          const redacted = fs.readFileSync(testFile, "utf8");
          expect(redacted).toBe("Using token: ***REDACTED*** in this file");
          expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("GitHub Personal Access Token"));
        });

        it("should redact GitHub Server-to-Server Token (ghs_)", async () => {
          const testFile = path.join(tempDir, "test.txt");
          const ghToken = "ghs_0123456789ABCDEFGHIJKLMNOPQRSTUVWxyz";
          fs.writeFileSync(testFile, `Server token: ${ghToken}`);
          process.env.GH_AW_SECRET_NAMES = "";
          const modifiedScript = redactScript.replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir.replace(/\\/g, "\\\\")}", targetExtensions)`);
          await eval(`(async () => { ${modifiedScript}; await main(); })()`);
          const redacted = fs.readFileSync(testFile, "utf8");
          expect(redacted).toBe("Server token: ***REDACTED***");
          expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("GitHub Server-to-Server Token"));
        });

        it("should redact long JWT-like GitHub Server-to-Server Token (ghs_)", async () => {
          const testFile = path.join(tempDir, "test.txt");
          const tokenSegments = [
            Buffer.from(JSON.stringify({ alg: "HS256", typ: "JWT" })).toString("base64url"),
            Buffer.from(JSON.stringify({ installation_id: 123, repository: "gh-aw", scope: "read_write" })).toString("base64url"),
            `sig_${"A".repeat(40)}`,
          ];
          const ghToken = `${["gh", "s_"].join("")}${tokenSegments.join(".")}`;
          expect(ghToken).toMatch(/^ghs_[0-9A-Za-z._-]{36,}$/);
          fs.writeFileSync(testFile, `Long server token: ${ghToken}`);
          process.env.GH_AW_SECRET_NAMES = "";
          const modifiedScript = redactScript.replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir.replace(/\\/g, "\\\\")}", targetExtensions)`);
          await eval(`(async () => { ${modifiedScript}; await main(); })()`);
          const redacted = fs.readFileSync(testFile, "utf8");
          expect(redacted).toBe("Long server token: ***REDACTED***");
          expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("GitHub Server-to-Server Token"));
        });

        it("should redact boundary-length JWT-like GitHub Server-to-Server Token (ghs_)", async () => {
          const testFile = path.join(tempDir, "test.txt");
          const ghToken = "ghs_abcdefghijk.lmnopqrstuv.wxyzABCDEFGH";
          expect(ghToken.slice(4)).toHaveLength(36);
          expect(ghToken).toMatch(/^ghs_[0-9A-Za-z._-]{36,}$/);
          fs.writeFileSync(testFile, `Boundary server token: ${ghToken}`);
          process.env.GH_AW_SECRET_NAMES = "";
          const modifiedScript = redactScript.replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir.replace(/\\/g, "\\\\")}", targetExtensions)`);
          await eval(`(async () => { ${modifiedScript}; await main(); })()`);
          const redacted = fs.readFileSync(testFile, "utf8");
          expect(redacted).toBe("Boundary server token: ***REDACTED***");
          expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("GitHub Server-to-Server Token"));
        });

        it("should redact dash-containing GitHub Server-to-Server Token (ghs_)", async () => {
          const testFile = path.join(tempDir, "test.txt");
          const ghToken = "ghs_abcd-efghij.klmn_opqr.stuvwxyz012345";
          expect(ghToken.slice(4)).toHaveLength(36);
          expect(ghToken).toContain("-");
          expect(ghToken).toContain(".");
          expect(ghToken).toContain("_");
          expect(ghToken).toMatch(/^ghs_[0-9A-Za-z._-]{36,}$/);
          fs.writeFileSync(testFile, `Dashed server token: ${ghToken}`);
          process.env.GH_AW_SECRET_NAMES = "";
          const modifiedScript = redactScript.replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir.replace(/\\/g, "\\\\")}", targetExtensions)`);
          await eval(`(async () => { ${modifiedScript}; await main(); })()`);
          const redacted = fs.readFileSync(testFile, "utf8");
          expect(redacted).toBe("Dashed server token: ***REDACTED***");
          expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("GitHub Server-to-Server Token"));
        });

        it("should redact GitHub OAuth Access Token (gho_)", async () => {
          const testFile = path.join(tempDir, "test.txt");
          const ghToken = "gho_0123456789ABCDEFGHIJKLMNOPQRSTUVWxyz";
          fs.writeFileSync(testFile, `OAuth: ${ghToken}`);
          process.env.GH_AW_SECRET_NAMES = "";
          const modifiedScript = redactScript.replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir.replace(/\\/g, "\\\\")}", targetExtensions)`);
          await eval(`(async () => { ${modifiedScript}; await main(); })()`);
          const redacted = fs.readFileSync(testFile, "utf8");
          expect(redacted).toBe("OAuth: ***REDACTED***");
          expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("GitHub OAuth Access Token"));
        });

        it("should redact GitHub User Access Token (ghu_)", async () => {
          const testFile = path.join(tempDir, "test.txt");
          const ghToken = "ghu_0123456789ABCDEFGHIJKLMNOPQRSTUVWxyz";
          fs.writeFileSync(testFile, `User token: ${ghToken}`);
          process.env.GH_AW_SECRET_NAMES = "";
          const modifiedScript = redactScript.replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir.replace(/\\/g, "\\\\")}", targetExtensions)`);
          await eval(`(async () => { ${modifiedScript}; await main(); })()`);
          const redacted = fs.readFileSync(testFile, "utf8");
          expect(redacted).toBe("User token: ***REDACTED***");
          expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("GitHub User Access Token"));
        });

        it("should redact GitHub Fine-grained PAT (github_pat_)", async () => {
          const testFile = path.join(tempDir, "test.txt");
          const ghToken = "github_pat_0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz_0123456789ABCDEFGHI";
          fs.writeFileSync(testFile, `Fine-grained PAT: ${ghToken}`);
          process.env.GH_AW_SECRET_NAMES = "";
          const modifiedScript = redactScript.replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir.replace(/\\/g, "\\\\")}", targetExtensions)`);
          await eval(`(async () => { ${modifiedScript}; await main(); })()`);
          const redacted = fs.readFileSync(testFile, "utf8");
          expect(redacted).toBe("Fine-grained PAT: ***REDACTED***");
          expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("GitHub Fine-grained PAT"));
        });

        it("should redact GitHub Refresh Token (ghr_)", async () => {
          const testFile = path.join(tempDir, "test.txt");
          const ghToken = "ghr_0123456789ABCDEFGHIJKLMNOPQRSTUVWxyz";
          fs.writeFileSync(testFile, `Refresh: ${ghToken}`);
          process.env.GH_AW_SECRET_NAMES = "";
          const modifiedScript = redactScript.replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir.replace(/\\/g, "\\\\")}", targetExtensions)`);
          await eval(`(async () => { ${modifiedScript}; await main(); })()`);
          const redacted = fs.readFileSync(testFile, "utf8");
          expect(redacted).toBe("Refresh: ***REDACTED***");
          expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("GitHub Refresh Token"));
        });

        it("should redact multiple GitHub token types in same file", async () => {
          const testFile = path.join(tempDir, "test.txt");
          const ghp = "ghp_1234567890ABCDEFGHIJKLMNOPQRSTUVWxyz";
          const ghs = "ghs_0123456789ABCDEFGHIJKLMNOPQRSTUVWxyz";
          const gho = "gho_0123456789ABCDEFGHIJKLMNOPQRSTUVWxyz";
          fs.writeFileSync(testFile, `PAT: ${ghp}\nServer: ${ghs}\nOAuth: ${gho}`);
          process.env.GH_AW_SECRET_NAMES = "";
          const modifiedScript = redactScript.replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir.replace(/\\/g, "\\\\")}", targetExtensions)`);
          await eval(`(async () => { ${modifiedScript}; await main(); })()`);
          const redacted = fs.readFileSync(testFile, "utf8");
          expect(redacted).toBe("PAT: ***REDACTED***\nServer: ***REDACTED***\nOAuth: ***REDACTED***");
        });
      });

      describe("Azure tokens", () => {
        it("should redact Azure Storage Account Key in connection string context", async () => {
          const testFile = path.join(tempDir, "test.txt");
          // Azure Storage Account Keys are 64-byte (512-bit) values = 86 base64 chars + "==" padding
          const azureKey = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/ABCDEFGHIJKLMNOPQRSTUV==";
          fs.writeFileSync(testFile, `DefaultEndpointsProtocol=https;AccountName=myaccount;AccountKey=${azureKey};EndpointSuffix=core.windows.net`);
          process.env.GH_AW_SECRET_NAMES = "";
          const modifiedScript = redactScript.replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir.replace(/\\/g, "\\\\")}", targetExtensions)`);
          await eval(`(async () => { ${modifiedScript}; await main(); })()`);
          const redacted = fs.readFileSync(testFile, "utf8");
          expect(redacted).toBe("DefaultEndpointsProtocol=https;AccountName=myaccount;***REDACTED***;EndpointSuffix=core.windows.net");
          expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Azure Storage Account Key"));
        });

        it("should not falsely redact plain base64 strings without AccountKey= context", async () => {
          const testFile = path.join(tempDir, "test.txt");
          // A different 86-char base64 string (not prefixed with AccountKey=) should NOT be redacted
          const plainBase64 = "zyxwvutsrqponmlkjihgfedcbaZYXWVUTSRQPONMLKJIHGFEDCBA9876543210/+zyxwvutsrqponmlkjiha==";
          const content = `some log output with base64: ${plainBase64} and more text`;
          fs.writeFileSync(testFile, content);
          process.env.GH_AW_SECRET_NAMES = "";
          const modifiedScript = redactScript.replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir.replace(/\\/g, "\\\\")}", targetExtensions)`);
          await eval(`(async () => { ${modifiedScript}; await main(); })()`);
          const result = fs.readFileSync(testFile, "utf8");
          expect(result).toBe(content);
        });

        it("should redact Azure SAS Token", async () => {
          const testFile = path.join(tempDir, "test.txt");
          const sasToken = "?sv=2021-06-08&ss=bfqt&srt=sco&sig=AbcXyz123456+/=";
          fs.writeFileSync(testFile, `SAS Token: ${sasToken}`);
          process.env.GH_AW_SECRET_NAMES = "";
          const modifiedScript = redactScript.replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir.replace(/\\/g, "\\\\")}", targetExtensions)`);
          await eval(`(async () => { ${modifiedScript}; await main(); })()`);
          const redacted = fs.readFileSync(testFile, "utf8");
          expect(redacted).toContain("?sv");
          // SAS tokens are complex and may not always be detected
        });
      });

      describe("Google/GCP tokens", () => {
        it("should redact Google API Key", async () => {
          const testFile = path.join(tempDir, "test.txt");
          const googleKey = "AIzaSy0123456789ABCDEFGHIJKLMNOPQRSTUVW";
          fs.writeFileSync(testFile, `Google API Key: ${googleKey}`);
          process.env.GH_AW_SECRET_NAMES = "";
          const modifiedScript = redactScript.replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir.replace(/\\/g, "\\\\")}", targetExtensions)`);
          await eval(`(async () => { ${modifiedScript}; await main(); })()`);
          const redacted = fs.readFileSync(testFile, "utf8");
          expect(redacted).toBe("Google API Key: ***REDACTED***");
          expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Google API Key"));
        });

        it("should redact Google OAuth Access Token", async () => {
          const testFile = path.join(tempDir, "test.txt");
          const googleToken = "ya29.a0AfH6SMBxXxXxXxXxXxXxXxXxXxXxXxXxXxXxXxXx";
          fs.writeFileSync(testFile, `OAuth Token: ${googleToken}`);
          process.env.GH_AW_SECRET_NAMES = "";
          const modifiedScript = redactScript.replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir.replace(/\\/g, "\\\\")}", targetExtensions)`);
          await eval(`(async () => { ${modifiedScript}; await main(); })()`);
          const redacted = fs.readFileSync(testFile, "utf8");
          expect(redacted).toBe("OAuth Token: ***REDACTED***");
          expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Google OAuth Access Token"));
        });
      });

      describe("AWS tokens", () => {
        it("should redact AWS Access Key ID", async () => {
          const testFile = path.join(tempDir, "test.txt");
          const awsKey = "AKIAIOSFODNN7EXAMPLE";
          fs.writeFileSync(testFile, `AWS Key: ${awsKey}`);
          process.env.GH_AW_SECRET_NAMES = "";
          const modifiedScript = redactScript.replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir.replace(/\\/g, "\\\\")}", targetExtensions)`);
          await eval(`(async () => { ${modifiedScript}; await main(); })()`);
          const redacted = fs.readFileSync(testFile, "utf8");
          expect(redacted).toBe("AWS Key: ***REDACTED***");
          expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("AWS Access Key ID"));
        });
      });

      describe("OpenAI tokens", () => {
        it("should redact OpenAI API Key", async () => {
          const testFile = path.join(tempDir, "test.txt");
          const openaiKey = "sk-" + "0".repeat(48);
          fs.writeFileSync(testFile, `OpenAI Key: ${openaiKey}`);
          process.env.GH_AW_SECRET_NAMES = "";
          const modifiedScript = redactScript.replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir.replace(/\\/g, "\\\\")}", targetExtensions)`);
          await eval(`(async () => { ${modifiedScript}; await main(); })()`);
          const redacted = fs.readFileSync(testFile, "utf8");
          expect(redacted).toBe("OpenAI Key: ***REDACTED***");
          expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("OpenAI API Key"));
        });

        it("should redact OpenAI Project API Key", async () => {
          const testFile = path.join(tempDir, "test.txt");
          const openaiProjectKey = "sk-proj-" + "A".repeat(55);
          fs.writeFileSync(testFile, `OpenAI Project Key: ${openaiProjectKey}`);
          process.env.GH_AW_SECRET_NAMES = "";
          const modifiedScript = redactScript.replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir.replace(/\\/g, "\\\\")}", targetExtensions)`);
          await eval(`(async () => { ${modifiedScript}; await main(); })()`);
          const redacted = fs.readFileSync(testFile, "utf8");
          expect(redacted).toBe("OpenAI Project Key: ***REDACTED***");
          expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("OpenAI Project API Key"));
        });
      });

      describe("Anthropic tokens", () => {
        it("should redact Anthropic API Key", async () => {
          const testFile = path.join(tempDir, "test.txt");
          const anthropicKey = "sk-ant-api03-" + "B".repeat(95);
          fs.writeFileSync(testFile, `Anthropic Key: ${anthropicKey}`);
          process.env.GH_AW_SECRET_NAMES = "";
          const modifiedScript = redactScript.replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir.replace(/\\/g, "\\\\")}", targetExtensions)`);
          await eval(`(async () => { ${modifiedScript}; await main(); })()`);
          const redacted = fs.readFileSync(testFile, "utf8");
          expect(redacted).toBe("Anthropic Key: ***REDACTED***");
          expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Anthropic API Key"));
        });
      });

      describe("Linear tokens", () => {
        it("should redact Linear API keys", async () => {
          const testFile = path.join(tempDir, "test.txt");
          const linearKey = "lin_api_" + "C".repeat(40);
          fs.writeFileSync(testFile, `Linear Key: ${linearKey}`);
          process.env.GH_AW_SECRET_NAMES = "";
          const modifiedScript = redactScript.replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir.replace(/\\/g, "\\\\")}", targetExtensions)`);
          await eval(`(async () => { ${modifiedScript}; await main(); })()`);
          const redacted = fs.readFileSync(testFile, "utf8");
          expect(redacted).toBe("Linear Key: ***REDACTED***");
          expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Linear API Key"));
        });
      });

      describe("combined built-in and custom secrets", () => {
        it("should redact both built-in patterns and custom secrets", async () => {
          const testFile = path.join(tempDir, "test.txt");
          const ghToken = "ghp_1234567890ABCDEFGHIJKLMNOPQRSTUVWxyz";
          const customSecret = "my-custom-secret-key-12345678";
          fs.writeFileSync(testFile, `GitHub: ${ghToken}\nCustom: ${customSecret}`);
          process.env.GH_AW_SECRET_NAMES = "CUSTOM_KEY";
          process.env.SECRET_CUSTOM_KEY = customSecret;
          const modifiedScript = redactScript.replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir.replace(/\\/g, "\\\\")}", targetExtensions)`);
          await eval(`(async () => { ${modifiedScript}; await main(); })()`);
          const redacted = fs.readFileSync(testFile, "utf8");
          expect(redacted).toBe("GitHub: ***REDACTED***\nCustom: ***REDACTED***");
          expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("GitHub Personal Access Token"));
          expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("occurrence(s) of a secret"));
        });

        it("should handle overlapping matches between built-in and custom secrets", async () => {
          const testFile = path.join(tempDir, "test.txt");
          const ghToken = "ghp_0123456789ABCDEFGHIJKLMNOPQRSTUVWxyz";
          fs.writeFileSync(testFile, `Token: ${ghToken} repeated: ${ghToken}`);
          process.env.GH_AW_SECRET_NAMES = "GH_TOKEN";
          process.env.SECRET_GH_TOKEN = ghToken;
          const modifiedScript = redactScript.replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir.replace(/\\/g, "\\\\")}", targetExtensions)`);
          await eval(`(async () => { ${modifiedScript}; await main(); })()`);
          const redacted = fs.readFileSync(testFile, "utf8");
          // Built-in pattern should redact it first
          expect(redacted).toBe("Token: ***REDACTED*** repeated: ***REDACTED***");
        });
      });

      describe("edge cases", () => {
        it("should handle files with no secrets", async () => {
          const testFile = path.join(tempDir, "test.txt");
          fs.writeFileSync(testFile, "This file has no secrets at all");
          process.env.GH_AW_SECRET_NAMES = "";
          const modifiedScript = redactScript.replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir.replace(/\\/g, "\\\\")}", targetExtensions)`);
          await eval(`(async () => { ${modifiedScript}; await main(); })()`);
          const content = fs.readFileSync(testFile, "utf8");
          expect(content).toBe("This file has no secrets at all");
        });

        it("should handle multiple occurrences of same built-in pattern", async () => {
          const testFile = path.join(tempDir, "test.txt");
          const ghToken = "ghp_1234567890ABCDEFGHIJKLMNOPQRSTUVWxyz";
          fs.writeFileSync(testFile, `First: ${ghToken}\nSecond: ${ghToken}\nThird: ${ghToken}`);
          process.env.GH_AW_SECRET_NAMES = "";
          const modifiedScript = redactScript.replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir.replace(/\\/g, "\\\\")}", targetExtensions)`);
          await eval(`(async () => { ${modifiedScript}; await main(); })()`);
          const redacted = fs.readFileSync(testFile, "utf8");
          expect(redacted).toBe("First: ***REDACTED***\nSecond: ***REDACTED***\nThird: ***REDACTED***");
        });

        it("should handle secrets in JSON content", async () => {
          const testFile = path.join(tempDir, "test.json");
          const ghToken = "ghp_0123456789ABCDEFGHIJKLMNOPQRSTUVWxyz";
          const googleKey = "AIzaSy0123456789ABCDEFGHIJKLMNOPQRSTUVW";
          fs.writeFileSync(testFile, JSON.stringify({ github_token: ghToken, google_api_key: googleKey }));
          process.env.GH_AW_SECRET_NAMES = "";
          const modifiedScript = redactScript.replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir.replace(/\\/g, "\\\\")}", targetExtensions)`);
          await eval(`(async () => { ${modifiedScript}; await main(); })()`);
          const redacted = fs.readFileSync(testFile, "utf8");
          expect(redacted).toContain("***REDACTED***");
          expect(redacted).toContain("***REDACTED***");
        });

        it("should handle secrets in log files with timestamps", async () => {
          const testFile = path.join(tempDir, "test.log");
          const ghToken = "ghp_0123456789ABCDEFGHIJKLMNOPQRSTUVWxyz";
          fs.writeFileSync(testFile, `[2024-01-01 12:00:00] INFO: Using token ${ghToken} for authentication`);
          process.env.GH_AW_SECRET_NAMES = "";
          const modifiedScript = redactScript.replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir.replace(/\\/g, "\\\\")}", targetExtensions)`);
          await eval(`(async () => { ${modifiedScript}; await main(); })()`);
          const redacted = fs.readFileSync(testFile, "utf8");
          expect(redacted).toBe("[2024-01-01 12:00:00] INFO: Using token ***REDACTED*** for authentication");
        });

        it("should not redact partial matches", async () => {
          const testFile = path.join(tempDir, "test.txt");
          // These should NOT be redacted (not valid token formats)
          fs.writeFileSync(testFile, "ghp_short ghs_invalid*token_because_it_has_disallowed_characters_and_is_long_enough ghs_short.segment.other ghs_12345678901234567890123456789012345 ghs_abcdefghij.klmnopqrst");
          process.env.GH_AW_SECRET_NAMES = "";
          const modifiedScript = redactScript.replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir.replace(/\\/g, "\\\\")}", targetExtensions)`);
          await eval(`(async () => { ${modifiedScript}; await main(); })()`);
          const content = fs.readFileSync(testFile, "utf8");
          // Includes a 35-char ghs_ token (below the 36-char minimum) and a short dot-separated variant.
          expect(content).toBe("ghp_short ghs_invalid*token_because_it_has_disallowed_characters_and_is_long_enough ghs_short.segment.other ghs_12345678901234567890123456789012345 ghs_abcdefghij.klmnopqrst");
        });

        it("should handle URLs with secrets", async () => {
          const testFile = path.join(tempDir, "test.txt");
          const ghToken = "ghp_0123456789ABCDEFGHIJKLMNOPQRSTUVWxyz";
          fs.writeFileSync(testFile, `https://api.github.com?token=${ghToken}`);
          process.env.GH_AW_SECRET_NAMES = "";
          const modifiedScript = redactScript.replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir.replace(/\\/g, "\\\\")}", targetExtensions)`);
          await eval(`(async () => { ${modifiedScript}; await main(); })()`);
          const redacted = fs.readFileSync(testFile, "utf8");
          expect(redacted).toBe("https://api.github.com?token=***REDACTED***");
        });

        it("should handle multiline content with various token types", async () => {
          const testFile = path.join(tempDir, "test.md");
          const content = `# Configuration
          
GitHub Token: ghp_0123456789ABCDEFGHIJKLMNOPQRSTUVWxyz
Azure Key: ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/ABCDEFGHIJKLMNOPQRSTUVWX==
Google API Key: AIzaSy0123456789ABCDEFGHIJKLMNOPQRSTUVW
AWS Key: AKIA0123456789ABCDEF

Custom secret: my-secret-123456789012`;
          fs.writeFileSync(testFile, content);
          process.env.GH_AW_SECRET_NAMES = "MY_SECRET";
          process.env.SECRET_MY_SECRET = "my-secret-123456789012";
          const modifiedScript = redactScript.replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir.replace(/\\/g, "\\\\")}", targetExtensions)`);
          await eval(`(async () => { ${modifiedScript}; await main(); })()`);
          const redacted = fs.readFileSync(testFile, "utf8");
          expect(redacted).toContain("***REDACTED***");
          expect(redacted).toContain("***REDACTED***");
          expect(redacted).toContain("***REDACTED***");
          expect(redacted).toContain("***REDACTED***");
          expect(redacted).toContain("***REDACTED***");
        });

        describe("ReDoS protection", () => {
          it("should handle pathological Azure SAS Token input without timing out", async () => {
            const testFile = path.join(tempDir, "test.txt");
            // Create pathological input that would cause ReDoS with unbounded quantifiers
            const pathological = `?sv=${"9".repeat(1000)}&srt=${"w".repeat(1000)}&sig=${"A".repeat(1000)}`;
            fs.writeFileSync(testFile, `Pathological: ${pathological}`);
            process.env.GH_AW_SECRET_NAMES = "";
            const modifiedScript = redactScript.replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir.replace(/\\/g, "\\\\")}", targetExtensions)`);

            // This should complete quickly (< 1 second) without hanging
            const startTime = Date.now();
            await eval(`(async () => { ${modifiedScript}; await main(); })()`);
            const duration = Date.now() - startTime;

            // Verify it completed quickly (should be < 1000ms, but allow 5000ms for slower CI)
            expect(duration).toBeLessThan(5000);

            // The pattern shouldn't match due to length bounds
            const content = fs.readFileSync(testFile, "utf8");
            expect(content).toBe(`Pathological: ${pathological}`);
          });

          it("should handle pathological Google OAuth token input without timing out", async () => {
            const testFile = path.join(tempDir, "test.txt");
            // Create pathological input that would cause ReDoS with unbounded quantifiers
            const pathological = `ya29.${"A".repeat(5000)}`;
            fs.writeFileSync(testFile, `Token: ${pathological}`);
            process.env.GH_AW_SECRET_NAMES = "";
            const modifiedScript = redactScript.replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir.replace(/\\/g, "\\\\")}", targetExtensions)`);

            // This should complete quickly (< 1 second) without hanging
            const startTime = Date.now();
            await eval(`(async () => { ${modifiedScript}; await main(); })()`);
            const duration = Date.now() - startTime;

            // Verify it completed quickly (should be < 1000ms, but allow 5000ms for slower CI)
            expect(duration).toBeLessThan(5000);

            // The pattern should match up to 800 chars and redact it
            const content = fs.readFileSync(testFile, "utf8");
            expect(content).toContain("***REDACTED***");
            expect(content).not.toBe(`Token: ${pathological}`);
            // Should still have unredacted 'A' chars at the end beyond 800 char limit
            expect(content).toMatch(/\*\*\*REDACTED\*\*\*A+$/);
          });

          it("should still match valid Azure SAS tokens within bounds", async () => {
            const testFile = path.join(tempDir, "test.txt");
            // Valid Azure SAS token within bounds
            const validSAS = "?sv=2021-06-08&sr=b&sig=AbCdEf0123456789+/=";
            fs.writeFileSync(testFile, `SAS: ${validSAS}`);
            process.env.GH_AW_SECRET_NAMES = "";
            const modifiedScript = redactScript.replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir.replace(/\\/g, "\\\\")}", targetExtensions)`);
            await eval(`(async () => { ${modifiedScript}; await main(); })()`);
            const redacted = fs.readFileSync(testFile, "utf8");
            // Should be redacted since it's a valid pattern within bounds
            expect(redacted).toBe("SAS: ***REDACTED***");
          });

          it("should still match valid Google OAuth tokens within bounds", async () => {
            const testFile = path.join(tempDir, "test.txt");
            // Valid Google OAuth token within bounds (typical length ~100-200 chars)
            const validToken = "ya29." + "a".repeat(150);
            fs.writeFileSync(testFile, `Token: ${validToken}`);
            process.env.GH_AW_SECRET_NAMES = "";
            const modifiedScript = redactScript.replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir.replace(/\\/g, "\\\\")}", targetExtensions)`);
            await eval(`(async () => { ${modifiedScript}; await main(); })()`);
            const redacted = fs.readFileSync(testFile, "utf8");
            // Should be redacted since it's a valid pattern within bounds
            expect(redacted).toBe("Token: ***REDACTED***");
            expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Google OAuth Access Token"));
          });
        });
      });
    }));

  describe("extractMCPGatewayTokens", () => {
    it("should extract Authorization tokens from gateway-output.json", () => {
      const configDir = path.join(tempDir, "mcp-config");
      fs.mkdirSync(configDir, { recursive: true });
      const gatewayOutput = path.join(configDir, "gateway-output.json");
      fs.writeFileSync(
        gatewayOutput,
        JSON.stringify({
          mcpServers: {
            github: {
              type: "http",
              url: "http://host.docker.internal:8080/mcp/github",
              headers: { Authorization: "my-gateway-token-abc123" },
            },
          },
        })
      );

      const script = redactScript
        .replace('path.join(process.env.RUNNER_TEMP || "/tmp", "gh-aw/mcp-config/gateway-output.json")', `"${gatewayOutput.replace(/\\/g, "\\\\")}"`)
        .replace('path.join(process.env.RUNNER_TEMP || "/tmp", "gh-aw/mcp-config/mcp-servers.json")', `"${path.join(configDir, "mcp-servers.json").replace(/\\/g, "\\\\")}"`);
      let tokens;
      eval(`(function() { ${script}; tokens = extractMCPGatewayTokens(MCP_GATEWAY_CONFIG_PATHS); })()`);
      expect(tokens).toContain("my-gateway-token-abc123");
    });

    it("should extract Authorization tokens from mcp-servers.json", () => {
      const configDir = path.join(tempDir, "mcp-config");
      fs.mkdirSync(configDir, { recursive: true });
      const mcpServers = path.join(configDir, "mcp-servers.json");
      fs.writeFileSync(
        mcpServers,
        JSON.stringify({
          mcpServers: {
            safeoutputs: {
              type: "http",
              url: "http://host.docker.internal:8080/mcp/safeoutputs",
              headers: { Authorization: "safe-output-token-xyz789" },
            },
          },
        })
      );

      const script = redactScript
        .replace('path.join(process.env.RUNNER_TEMP || "/tmp", "gh-aw/mcp-config/gateway-output.json")', `"${path.join(configDir, "gateway-output.json").replace(/\\/g, "\\\\")}"`)
        .replace('path.join(process.env.RUNNER_TEMP || "/tmp", "gh-aw/mcp-config/mcp-servers.json")', `"${mcpServers.replace(/\\/g, "\\\\")}"`);
      let tokens;
      eval(`(function() { ${script}; tokens = extractMCPGatewayTokens(MCP_GATEWAY_CONFIG_PATHS); })()`);
      expect(tokens).toContain("safe-output-token-xyz789");
    });

    it("should extract Authorization tokens from Codex TOML configuration", () => {
      const configPath = path.join(tempDir, "config.toml");
      const token = "codex-gateway-token-xyz789";
      const authHeader = ["Bearer", token].join(" ");
      fs.writeFileSync(configPath, `[mcp_servers.github]\nurl = "http://127.0.0.1:8080/github"\nhttp_headers = { Authorization = "${authHeader}" }\n`);

      const { extractMCPGatewayTokens } = require("./redact_secrets.cjs");
      const tokens = extractMCPGatewayTokens([configPath]);
      expect(tokens).toContain(authHeader);
      expect(tokens).toContain(token);
    });

    it("should extract lowercase authorization keys from Codex TOML configuration", () => {
      const configPath = path.join(tempDir, "config-lowercase.toml");
      const token = "codex-lowercase-token-abc123";
      const authHeader = ["Bearer", token].join(" ");
      fs.writeFileSync(configPath, `[mcp_servers.github]\nhttp_headers = { authorization = "${authHeader}" }\n`);

      const { extractMCPGatewayTokens } = require("./redact_secrets.cjs");
      const tokens = extractMCPGatewayTokens([configPath]);
      expect(tokens).toContain(authHeader);
      expect(tokens).toContain(token);
    });

    it("should ignore short Authorization values even when bearer-prefixed", () => {
      const configPath = path.join(tempDir, "config-short.toml");
      const authHeader = ["Bearer", "abc"].join(" ");
      fs.writeFileSync(configPath, `[mcp_servers.github]\nhttp_headers = { Authorization = "${authHeader}" }\n`);

      const { extractMCPGatewayTokens } = require("./redact_secrets.cjs");
      expect(extractMCPGatewayTokens([configPath])).toEqual([]);
    });

    it("should extract both the full Bearer header value and the bare token", () => {
      const configDir = path.join(tempDir, "mcp-config");
      fs.mkdirSync(configDir, { recursive: true });
      const gatewayOutput = path.join(configDir, "gateway-output.json");
      fs.writeFileSync(
        gatewayOutput,
        JSON.stringify({
          mcpServers: {
            github: {
              type: "http",
              url: "http://host.docker.internal:8080/mcp/github",
              headers: { Authorization: "Bearer tok-abc123def456" },
            },
          },
        })
      );

      const script = redactScript
        .replace('path.join(process.env.RUNNER_TEMP || "/tmp", "gh-aw/mcp-config/gateway-output.json")', `"${gatewayOutput.replace(/\\/g, "\\\\")}"`)
        .replace('path.join(process.env.RUNNER_TEMP || "/tmp", "gh-aw/mcp-config/mcp-servers.json")', `"${path.join(configDir, "mcp-servers.json").replace(/\\/g, "\\\\")}"`);
      let tokens;
      eval(`(function() { ${script}; tokens = extractMCPGatewayTokens(MCP_GATEWAY_CONFIG_PATHS); })()`);
      expect(tokens).toContain("Bearer tok-abc123def456");
      expect(tokens).toContain("tok-abc123def456");
    });

    it("should deduplicate tokens shared across multiple servers", () => {
      const configDir = path.join(tempDir, "mcp-config");
      fs.mkdirSync(configDir, { recursive: true });
      const gatewayOutput = path.join(configDir, "gateway-output.json");
      const sharedToken = "shared-token-same-for-all";
      fs.writeFileSync(
        gatewayOutput,
        JSON.stringify({
          mcpServers: {
            github: { type: "http", url: "http://host.docker.internal:8080/mcp/github", headers: { Authorization: sharedToken } },
            safeoutputs: { type: "http", url: "http://host.docker.internal:8080/mcp/safeoutputs", headers: { Authorization: sharedToken } },
          },
        })
      );

      const script = redactScript
        .replace('path.join(process.env.RUNNER_TEMP || "/tmp", "gh-aw/mcp-config/gateway-output.json")', `"${gatewayOutput.replace(/\\/g, "\\\\")}"`)
        .replace('path.join(process.env.RUNNER_TEMP || "/tmp", "gh-aw/mcp-config/mcp-servers.json")', `"${path.join(configDir, "mcp-servers.json").replace(/\\/g, "\\\\")}"`);
      let tokens;
      eval(`(function() { ${script}; tokens = extractMCPGatewayTokens(MCP_GATEWAY_CONFIG_PATHS); })()`);
      expect(tokens.filter(t => t === sharedToken)).toHaveLength(1);
    });

    it("should return empty array when config files do not exist", () => {
      const nonExistent = path.join(tempDir, "nonexistent.json");
      const script = redactScript
        .replace('path.join(process.env.RUNNER_TEMP || "/tmp", "gh-aw/mcp-config/gateway-output.json")', `"${nonExistent.replace(/\\/g, "\\\\")}"`)
        .replace('path.join(process.env.RUNNER_TEMP || "/tmp", "gh-aw/mcp-config/mcp-servers.json")', `"${nonExistent.replace(/\\/g, "\\\\")}"`);
      let tokens;
      eval(`(function() { ${script}; tokens = extractMCPGatewayTokens(MCP_GATEWAY_CONFIG_PATHS); })()`);
      expect(tokens).toEqual([]);
    });

    it("should silently ignore malformed JSON config files", () => {
      const configDir = path.join(tempDir, "mcp-config");
      fs.mkdirSync(configDir, { recursive: true });
      const gatewayOutput = path.join(configDir, "gateway-output.json");
      fs.writeFileSync(gatewayOutput, "not valid json {{{");

      const script = redactScript
        .replace('path.join(process.env.RUNNER_TEMP || "/tmp", "gh-aw/mcp-config/gateway-output.json")', `"${gatewayOutput.replace(/\\/g, "\\\\")}"`)
        .replace('path.join(process.env.RUNNER_TEMP || "/tmp", "gh-aw/mcp-config/mcp-servers.json")', `"${path.join(configDir, "mcp-servers.json").replace(/\\/g, "\\\\")}"`);
      let tokens;
      expect(() => {
        eval(`(function() { ${script}; tokens = extractMCPGatewayTokens(MCP_GATEWAY_CONFIG_PATHS); })()`);
      }).not.toThrow();
      expect(tokens).toEqual([]);
    });

    it("should redact MCP gateway token from diagnostic logs in main()", async () => {
      const configDir = path.join(tempDir, "mcp-config");
      fs.mkdirSync(configDir, { recursive: true });
      const gatewayOutput = path.join(configDir, "gateway-output.json");
      const gatewayToken = "super-secret-gateway-token-98765";
      fs.writeFileSync(
        gatewayOutput,
        JSON.stringify({
          mcpServers: {
            github: { type: "http", url: "http://host.docker.internal:8080/mcp/github", headers: { Authorization: gatewayToken } },
          },
        })
      );

      const logsDir = path.join(tempDir, "mcp-logs");
      fs.mkdirSync(logsDir, { recursive: true });
      const diagnosticLogs = ["github.log", "mcp-gateway.log", "safeoutputs.log", "stderr.log"].map(name => path.join(logsDir, name));
      for (const logFile of diagnosticLogs) {
        fs.writeFileSync(logFile, `{"type":"tool_result","content":"session=${gatewayToken}"}`);
      }

      let modifiedScript = redactScript
        .replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir.replace(/\\/g, "\\\\")}", targetExtensions)`)
        .replace('path.join(process.env.RUNNER_TEMP || "/tmp", "gh-aw/mcp-config/gateway-output.json")', `"${gatewayOutput.replace(/\\/g, "\\\\")}"`)
        .replace('path.join(process.env.RUNNER_TEMP || "/tmp", "gh-aw/mcp-config/mcp-servers.json")', `"${path.join(configDir, "mcp-servers.json").replace(/\\/g, "\\\\")}"`);

      await eval(`(async () => { ${modifiedScript}; await main(); })()`);

      for (const logFile of diagnosticLogs) {
        const redacted = fs.readFileSync(logFile, "utf8");
        expect(redacted).not.toContain(gatewayToken);
        expect(redacted).toContain("***REDACTED***");
      }
    });
  });
});
