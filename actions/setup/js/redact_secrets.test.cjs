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
          expect(mockCore.info).toHaveBeenCalledWith("Starting secret redaction in /tmp/gh-aw and /opt/gh-aw directories"),
          expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Scanning for built-in credential patterns")));
      }),
        it("should redact secrets from files in /tmp using exact matching", async () => {
          const testFile = path.join(tempDir, "test.txt"),
            secretValue = "ghp_1234567890ABCDEFGHIJKLMNOPQRSTUVWxyz";
          (fs.writeFileSync(testFile, `Secret: ${secretValue} and another ${secretValue}`), (process.env.GH_AW_SECRET_NAMES = "GITHUB_TOKEN"), (process.env.SECRET_GITHUB_TOKEN = secretValue));
          const modifiedScript = redactScript.replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir.replace(/\\/g, "\\\\")}", targetExtensions)`);
          await eval(`(async () => { ${modifiedScript}; await main(); })()`);
          const redactedContent = fs.readFileSync(testFile, "utf8");
          (expect(redactedContent).toBe("Secret: ghp************************************* and another ghp*************************************"),
            expect(mockCore.info).toHaveBeenCalledWith("Starting secret redaction in /tmp/gh-aw and /opt/gh-aw directories"),
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
            expect(fs.readFileSync(path.join(tempDir, "test1.txt"), "utf8")).toBe("Secret: api********"),
            expect(fs.readFileSync(path.join(tempDir, "test2.json"), "utf8")).toBe('{"key": "api********"}'),
            expect(fs.readFileSync(path.join(tempDir, "test3.log"), "utf8")).toBe("Log: api********"));
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
          (await eval(`(async () => { ${modifiedScript}; await main(); })()`), expect(fs.readFileSync(testFile, "utf8")).toBe("Secret: abc*** test"));
        }),
        it("should handle multiple secrets in same file", async () => {
          const testFile = path.join(tempDir, "test.txt"),
            secret1 = "ghp_1234567890ABCDEFGHIJKLMNOPQRSTUVWxyz",
            secret2 = "sk-proj-abcdef1234567890";
          (fs.writeFileSync(testFile, `Token1: ${secret1}\nToken2: ${secret2}\nToken1 again: ${secret1}`), (process.env.GH_AW_SECRET_NAMES = "TOKEN1,TOKEN2"), (process.env.SECRET_TOKEN1 = secret1), (process.env.SECRET_TOKEN2 = secret2));
          const modifiedScript = redactScript.replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir.replace(/\\/g, "\\\\")}", targetExtensions)`);
          await eval(`(async () => { ${modifiedScript}; await main(); })()`);
          const redacted = fs.readFileSync(testFile, "utf8");
          expect(redacted).toBe("Token1: ghp*************************************\nToken2: sk-*********************\nToken1 again: ghp*************************************");
        }),
        it("should handle empty secret values gracefully", async () => {
          const testFile = path.join(tempDir, "test.txt");
          (fs.writeFileSync(testFile, "No secrets here"), (process.env.GH_AW_SECRET_NAMES = "EMPTY_SECRET"), (process.env.SECRET_EMPTY_SECRET = ""));
          const modifiedScript = redactScript.replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir.replace(/\\/g, "\\\\")}", targetExtensions)`);
          (await eval(`(async () => { ${modifiedScript}; await main(); })()`),
            expect(mockCore.info).toHaveBeenCalledWith("Starting secret redaction in /tmp/gh-aw and /opt/gh-aw directories"),
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
            expect(fs.readFileSync(path.join(tempDir, "test.md"), "utf8")).toBe("# Markdown\nSecret: api**********"),
            expect(fs.readFileSync(path.join(tempDir, "test.mdx"), "utf8")).toBe("# MDX\nSecret: api***********"),
            expect(fs.readFileSync(path.join(tempDir, "test.yml"), "utf8")).toBe("# YAML\nkey: api***********"),
            expect(fs.readFileSync(path.join(tempDir, "test.jsonl"), "utf8")).toBe('{"key": "api*************"}'));
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
          expect(redacted).toBe("Using token: ghp************************************* in this file");
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
          expect(redacted).toBe("Server token: ghs*************************************");
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
          expect(redacted).toBe("OAuth: gho*************************************");
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
          expect(redacted).toBe("User token: ghu*************************************");
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
          expect(redacted).toBe("Fine-grained PAT: git******************************************************************************************");
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
          expect(redacted).toBe("Refresh: ghr*************************************");
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
          expect(redacted).toBe("PAT: ghp*************************************\nServer: ghs*************************************\nOAuth: gho*************************************");
        });
      });

      describe("Azure tokens", () => {
        it("should redact Azure Storage Account Key", async () => {
          const testFile = path.join(tempDir, "test.txt");
          const azureKey = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/ABCDEFGHIJKLMNOPQRSTUVWX==";
          fs.writeFileSync(testFile, `Azure Key: ${azureKey}`);
          process.env.GH_AW_SECRET_NAMES = "";
          const modifiedScript = redactScript.replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir.replace(/\\/g, "\\\\")}", targetExtensions)`);
          await eval(`(async () => { ${modifiedScript}; await main(); })()`);
          const redacted = fs.readFileSync(testFile, "utf8");
          expect(redacted).toBe("Azure Key: ABC***************************************************************************************");
          expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Azure Storage Account Key"));
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
          expect(redacted).toBe("Google API Key: AIz************************************");
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
          expect(redacted).toBe("OAuth Token: ya2********************************************");
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
          expect(redacted).toBe("AWS Key: AKI*****************");
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
          expect(redacted).toBe("OpenAI Key: sk-************************************************");
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
          expect(redacted).toBe("OpenAI Project Key: sk-************************************************************");
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
          expect(redacted).toBe("Anthropic Key: sk-*********************************************************************************************************");
          expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Anthropic API Key"));
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
          expect(redacted).toBe("GitHub: ghp*************************************\nCustom: my-**************************");
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
          expect(redacted).toBe("Token: ghp************************************* repeated: ghp*************************************");
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
          expect(redacted).toBe("First: ghp*************************************\nSecond: ghp*************************************\nThird: ghp*************************************");
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
          expect(redacted).toContain("ghp*************************************");
          expect(redacted).toContain("AIz************************************");
        });

        it("should handle secrets in log files with timestamps", async () => {
          const testFile = path.join(tempDir, "test.log");
          const ghToken = "ghp_0123456789ABCDEFGHIJKLMNOPQRSTUVWxyz";
          fs.writeFileSync(testFile, `[2024-01-01 12:00:00] INFO: Using token ${ghToken} for authentication`);
          process.env.GH_AW_SECRET_NAMES = "";
          const modifiedScript = redactScript.replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir.replace(/\\/g, "\\\\")}", targetExtensions)`);
          await eval(`(async () => { ${modifiedScript}; await main(); })()`);
          const redacted = fs.readFileSync(testFile, "utf8");
          expect(redacted).toBe("[2024-01-01 12:00:00] INFO: Using token ghp************************************* for authentication");
        });

        it("should not redact partial matches", async () => {
          const testFile = path.join(tempDir, "test.txt");
          // These should NOT be redacted (not valid token formats)
          fs.writeFileSync(testFile, "ghp_short ghs_toolong_this_is_not_a_valid_token_because_its_way_too_long");
          process.env.GH_AW_SECRET_NAMES = "";
          const modifiedScript = redactScript.replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir.replace(/\\/g, "\\\\")}", targetExtensions)`);
          await eval(`(async () => { ${modifiedScript}; await main(); })()`);
          const content = fs.readFileSync(testFile, "utf8");
          // These should remain unchanged since they don't match the exact pattern
          expect(content).toBe("ghp_short ghs_toolong_this_is_not_a_valid_token_because_its_way_too_long");
        });

        it("should handle URLs with secrets", async () => {
          const testFile = path.join(tempDir, "test.txt");
          const ghToken = "ghp_0123456789ABCDEFGHIJKLMNOPQRSTUVWxyz";
          fs.writeFileSync(testFile, `https://api.github.com?token=${ghToken}`);
          process.env.GH_AW_SECRET_NAMES = "";
          const modifiedScript = redactScript.replace('findFiles("/tmp/gh-aw", targetExtensions)', `findFiles("${tempDir.replace(/\\/g, "\\\\")}", targetExtensions)`);
          await eval(`(async () => { ${modifiedScript}; await main(); })()`);
          const redacted = fs.readFileSync(testFile, "utf8");
          expect(redacted).toBe("https://api.github.com?token=ghp*************************************");
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
          expect(redacted).toContain("ghp*************************************");
          expect(redacted).toContain("ABC***************************************************************************************");
          expect(redacted).toContain("AIz************************************");
          expect(redacted).toContain("AKI*****************");
          expect(redacted).toContain("my-*******************");
        });
      });
    }));
});
