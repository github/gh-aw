import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import fs from "fs";
import path from "path";
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
  toPlatformPath: vi.fn(),
  toPosixPath: vi.fn(),
  toWin32Path: vi.fn(),
  summary: { addRaw: vi.fn().mockReturnThis(), write: vi.fn().mockResolvedValue() },
};
((global.core = mockCore),
  describe("sanitize_output.cjs", () => {
    let sanitizeScript, sanitizeContentFunction;
    (beforeEach(() => {
      (vi.clearAllMocks(), delete process.env.GH_AW_SAFE_OUTPUTS, delete process.env.GH_AW_ALLOWED_DOMAINS);
      const scriptPath = path.join(process.cwd(), "sanitize_output.cjs");
      sanitizeScript = fs.readFileSync(scriptPath, "utf8");
      const scriptWithExport = sanitizeScript.replace("module.exports = { main };", "global.testSanitizeContent = sanitizeContent;");
      (eval(scriptWithExport), (sanitizeContentFunction = global.testSanitizeContent));
    }),
      describe("sanitizeContent function", () => {
        (it("should handle null and undefined inputs", () => {
          (expect(sanitizeContentFunction(null)).toBe(""), expect(sanitizeContentFunction(void 0)).toBe(""), expect(sanitizeContentFunction("")).toBe(""));
        }),
          it("should neutralize @mentions by wrapping in backticks", () => {
            const result = sanitizeContentFunction("Hello @user and @org/team");
            (expect(result).toContain("`@user`"), expect(result).toContain("`@org/team`"));
          }),
          it("should not neutralize @mentions inside code blocks", () => {
            const result = sanitizeContentFunction("Check `@user` in code and @realuser outside");
            (expect(result).toContain("`@user`"), expect(result).toContain("`@realuser`"));
          }),
          it("should neutralize bot trigger phrases", () => {
            const result = sanitizeContentFunction("This fixes #123 and closes #456. Also resolves #789");
            (expect(result).toContain("`fixes #123`"), expect(result).toContain("`closes #456`"), expect(result).toContain("`resolves #789`"));
          }),
          it("should remove control characters except newlines and tabs", () => {
            const result = sanitizeContentFunction("Hello\0world\f\nNext line\tbad");
            (expect(result).not.toContain("\0"), expect(result).not.toContain("\f"), expect(result).not.toContain(""), expect(result).toContain("\n"), expect(result).toContain("\t"));
          }),
          it("should convert XML tags to parentheses format", () => {
            const result = sanitizeContentFunction('<script>alert("test")<\/script> & more');
            (expect(result).toContain("(script)"), expect(result).toContain("(/script)"), expect(result).toContain('"test"'), expect(result).toContain("& more"));
          }),
          it("should handle self-closing XML tags without whitespace", () => {
            const result = sanitizeContentFunction('Self-closing: <br/> <img src="test.jpg"/> <meta charset="utf-8"/>');
            (expect(result).toContain("<br/>"), expect(result).toContain('(img src="test.jpg"/)'), expect(result).toContain('(meta charset="utf-8"/)'));
          }),
          it("should handle self-closing XML tags with whitespace", () => {
            const result = sanitizeContentFunction('With spaces: <br /> <img src="test.jpg" /> <meta charset="utf-8" />');
            (expect(result).toContain("<br />"), expect(result).toContain('(img src="test.jpg" /)'), expect(result).toContain('(meta charset="utf-8" /)'));
          }),
          it("should handle XML tags with various whitespace patterns", () => {
            const result = sanitizeContentFunction('Various: <div\tclass="test">content</div> <span\n  id="test">text</span>');
            (expect(result).toContain('(div\tclass="test")content(/div)'), expect(result).toContain('(span\n  id="test")text(/span)'));
          }),
          it("should preserve non-XML uses of < and > characters", () => {
            const result = sanitizeContentFunction("Math: x < y, array[5] > 3, and <div>content</div>");
            (expect(result).toContain("x < y"), expect(result).toContain("5] > 3"), expect(result).toContain("(div)content(/div)"));
          }),
          it("should handle mixed XML tags and comparison operators", () => {
            const result = sanitizeContentFunction("Compare: a < b and then <script>alert(1)<\/script> plus c > d");
            (expect(result).toContain("a < b"), expect(result).toContain("(script)alert(1)(/script)"), expect(result).toContain("c > d"));
          }),
          it("should block HTTP URLs while preserving HTTPS URLs", () => {
            const result = sanitizeContentFunction("HTTP: http://bad.com and HTTPS: https://github.com");
            (expect(result).toContain("(bad.com/redacted)"), expect(result).toContain("https://github.com"), expect(result).not.toContain("http://bad.com"));
          }),
          it("should block various unsafe protocols", () => {
            const result = sanitizeContentFunction("Bad: ftp://file.com javascript:alert(1) file://local data:text/html,<script>");
            (expect(result).toContain("(redacted)"), expect(result).not.toContain("ftp://"), expect(result).not.toContain("javascript:"), expect(result).not.toContain("file://"), expect(result).not.toContain("data:"));
          }),
          it("should preserve HTTPS URLs for allowed domains", () => {
            const result = sanitizeContentFunction("Links: https://github.com/user/repo https://github.io/page https://githubusercontent.com/file");
            (expect(result).toContain("https://github.com/user/repo"), expect(result).toContain("https://github.io/page"), expect(result).toContain("https://githubusercontent.com/file"));
          }),
          it("should block HTTPS URLs for disallowed domains", () => {
            const result = sanitizeContentFunction("Bad: https://evil.com/malware Good: https://github.com/repo");
            (expect(result).toContain("(evil.com/redacted)"), expect(result).toContain("https://github.com/repo"), expect(result).not.toContain("https://evil.com"));
          }),
          it("should respect custom allowed domains from environment", () => {
            const originalServerUrl = process.env.GITHUB_SERVER_URL,
              originalApiUrl = process.env.GITHUB_API_URL;
            (delete process.env.GITHUB_SERVER_URL, delete process.env.GITHUB_API_URL, (process.env.GH_AW_ALLOWED_DOMAINS = "example.com,trusted.org"));
            const scriptWithExport = sanitizeScript.replace("await main();", "global.testSanitizeContent = sanitizeContent;");
            eval(scriptWithExport);
            const customSanitize = global.testSanitizeContent,
              input = "Links: https://example.com/page https://trusted.org/file https://github.com/repo",
              result = customSanitize(input);
            (expect(result).toContain("https://example.com/page"),
              expect(result).toContain("https://trusted.org/file"),
              expect(result).toContain("(github.com/redacted)"),
              expect(result).not.toContain("https://github.com/repo"),
              originalServerUrl && (process.env.GITHUB_SERVER_URL = originalServerUrl),
              originalApiUrl && (process.env.GITHUB_API_URL = originalApiUrl));
          }),
          it("should allow GitHub domains from environment variables", () => {
            ((process.env.GITHUB_SERVER_URL = "https://github.example.com"), (process.env.GITHUB_API_URL = "https://api.github.example.com"), (process.env.GH_AW_ALLOWED_DOMAINS = "custom.com"));
            const scriptWithExport = sanitizeScript.replace("await main();", "global.testSanitizeContent = sanitizeContent;");
            eval(scriptWithExport);
            const customSanitize = global.testSanitizeContent,
              input = "Links: https://custom.com/page https://github.example.com/repo https://api.github.example.com/v1 https://raw.github.example.com/file https://blocked.com/page",
              result = customSanitize(input);
            (expect(result).toContain("https://custom.com/page"),
              expect(result).toContain("https://github.example.com/repo"),
              expect(result).toContain("https://api.github.example.com/v1"),
              expect(result).toContain("https://raw.github.example.com/file"),
              expect(result).toContain("(blocked.com/redacted)"),
              expect(result).not.toContain("https://blocked.com/page"),
              delete process.env.GITHUB_SERVER_URL,
              delete process.env.GITHUB_API_URL,
              delete process.env.GH_AW_ALLOWED_DOMAINS);
          }),
          it("should allow raw.githubusercontent.com for github.com", () => {
            ((process.env.GITHUB_SERVER_URL = "https://github.com"), (process.env.GITHUB_API_URL = "https://api.github.com"), (process.env.GH_AW_ALLOWED_DOMAINS = ""));
            const scriptWithExport = sanitizeScript.replace("await main();", "global.testSanitizeContent = sanitizeContent;");
            eval(scriptWithExport);
            const customSanitize = global.testSanitizeContent,
              input = "Raw content: https://raw.githubusercontent.com/owner/repo/main/file.txt and API: https://api.github.com/repos/owner/repo",
              result = customSanitize(input);
            (expect(result).toContain("https://raw.githubusercontent.com/owner/repo/main/file.txt"),
              expect(result).toContain("https://api.github.com/repos/owner/repo"),
              expect(result).not.toContain("(redacted)"),
              delete process.env.GITHUB_SERVER_URL,
              delete process.env.GITHUB_API_URL,
              delete process.env.GH_AW_ALLOWED_DOMAINS);
          }),
          it("should handle subdomain matching correctly", () => {
            const result = sanitizeContentFunction("Subdomains: https://api.github.com/v1 https://docs.github.com/guide");
            (expect(result).toContain("https://api.github.com/v1"), expect(result).toContain("https://docs.github.com/guide"));
          }),
          it("should truncate content that exceeds maximum length", () => {
            const longContent = "x".repeat(6e5),
              result = sanitizeContentFunction(longContent);
            (expect(result.length).toBeLessThan(6e5), expect(result).toContain("[Content truncated due to length]"));
          }),
          it("should truncate content that exceeds maximum lines", () => {
            const manyLines = "\n".repeat(7e4),
              result = sanitizeContentFunction(manyLines),
              lines = result.split("\n");
            (expect(lines.length).toBeLessThanOrEqual(65001), expect(result).toContain("[Content truncated due to line count]"));
          }),
          it("should remove ANSI escape sequences", () => {
            const result = sanitizeContentFunction("[31mRed text[0m [1;32mBold green[m");
            (expect(result).not.toContain("["), expect(result).toContain("Red text"), expect(result).toContain("Bold green"));
          }),
          it("should handle complex mixed content correctly", () => {
            const input =
                "\n# Issue Report by @user\n\nThis fixes #123 and has links:\n- HTTP: http://bad.com (should be blocked)\n- HTTPS: https://github.com/repo (should be preserved)\n- JavaScript: javascript:alert('xss') (should be blocked)\n\n<script>alert(\"xss\")<\/script>\n\nSpecial chars: \0 & \"quotes\" 'apostrophes'\n      ".trim(),
              result = sanitizeContentFunction(input);
            (expect(result).toContain("`@user`"),
              expect(result).toContain("`fixes #123`"),
              expect(result).toContain("(redacted)"),
              expect(result).toContain("https://github.com/repo"),
              expect(result).not.toContain("http://bad.com"),
              expect(result).not.toContain("javascript:alert"),
              expect(result).toContain("(script)"),
              expect(result).toContain('"quotes"'),
              expect(result).toContain("'apostrophes'"),
              expect(result).toContain("&"),
              expect(result).not.toContain("\0"),
              expect(result).not.toContain(""));
          }),
          it("should trim excessive whitespace", () => {
            const result = sanitizeContentFunction("   \n\n  Content with spacing  \n\n  ");
            expect(result).toBe("Content with spacing");
          }),
          it("should handle empty environment variable gracefully", () => {
            const originalServerUrl = process.env.GITHUB_SERVER_URL,
              originalApiUrl = process.env.GITHUB_API_URL;
            (delete process.env.GITHUB_SERVER_URL, delete process.env.GITHUB_API_URL, (process.env.GH_AW_ALLOWED_DOMAINS = "  ,  ,  "));
            const scriptWithExport = sanitizeScript.replace("await main();", "global.testSanitizeContent = sanitizeContent;");
            eval(scriptWithExport);
            const customSanitize = global.testSanitizeContent,
              input = "Link: https://github.com/repo",
              result = customSanitize(input);
            (expect(result).toContain("(github.com/redacted)"),
              expect(result).not.toContain("https://github.com/repo"),
              originalServerUrl && (process.env.GITHUB_SERVER_URL = originalServerUrl),
              originalApiUrl && (process.env.GITHUB_API_URL = originalApiUrl));
          }),
          it("should handle @mentions with various formats", () => {
            const result = sanitizeContentFunction("Contact @user123, @org-name/team_name, @a, and @normalname");
            (expect(result).toContain("`@user123`"), expect(result).toContain("`@org-name/team_name`"), expect(result).toContain("`@a`"), expect(result).toContain("`@normalname`"));
          }),
          it("should not neutralize @mentions at start of backticked expressions", () => {
            const result = sanitizeContentFunction("Code: `@user.method()` and normal @user mention");
            (expect(result).toContain("`@user.method()`"), expect(result).toContain("`@user`"));
          }),
          it("should handle various bot trigger phrase formats", () => {
            const result = sanitizeContentFunction("Fix #123, close #abc, FIXES #XYZ, resolves #456, fixes    #789");
            (expect(result).toContain("`Fix #123`"), expect(result).toContain("`close #abc`"), expect(result).toContain("`FIXES #XYZ`"), expect(result).toContain("`resolves #456`"), expect(result).toContain("`fixes #789`"));
          }),
          it("should handle edge cases in protocol filtering", () => {
            const result = sanitizeContentFunction(
              '\n        Protocols: HTTP://CAPS.COM, https://github.com/path?query=value#fragment\n        More: mailto:user@domain.com tel:+1234567890 ssh://server:22/path\n        Edge: ://malformed http:// https:// \n        Nested: (https://github.com) [http://bad.com] "ftp://files.com"\n      '
            );
            (expect(result).toContain("(redacted)"),
              expect(result).toContain("https://github.com/path?query=value#fragment"),
              expect(result).toContain("(redacted)"),
              expect(result).not.toContain("HTTP://CAPS.COM"),
              expect(result).not.toContain("mailto:user@domain.com"),
              expect(result).not.toContain("tel:+1234567890"),
              expect(result).not.toContain("ssh://server:22/path"));
          }),
          it("should preserve HTTPS URLs in various contexts", () => {
            const result = sanitizeContentFunction(
              "\n        Links in text: Visit https://github.com/user/repo for details.\n        In parentheses: (https://github.io/docs)\n        In brackets: [https://githubusercontent.com/file.txt]\n        Multiple: https://github.com https://github.io https://githubassets.com\n      "
            );
            (expect(result).toContain("https://github.com/user/repo"),
              expect(result).toContain("https://github.io/docs"),
              expect(result).toContain("https://githubusercontent.com/file.txt"),
              expect(result).toContain("https://github.com"),
              expect(result).toContain("https://github.io"),
              expect(result).toContain("https://githubassets.com"));
          }),
          it("should handle complex domain matching scenarios", () => {
            const result = sanitizeContentFunction(
              "\n        Valid: https://api.github.com/v4/graphql https://docs.github.com/en/\n        Invalid: https://github.com.evil.com https://notgithub.com\n        Edge: https://github.com.attacker.com https://sub.github.io.fake.com\n      "
            );
            (expect(result).toContain("https://api.github.com/v4/graphql"),
              expect(result).toContain("https://docs.github.com/en/"),
              expect(result).toContain("/redacted"),
              expect(result).not.toContain("https://github.com.evil.com"),
              expect(result).not.toContain("https://notgithub.com"),
              expect(result).not.toContain("https://github.com.attacker.com"),
              expect(result).not.toContain("https://sub.github.io.fake.com"));
          }),
          it("should handle URLs with special characters and edge cases", () => {
            const result = sanitizeContentFunction(
              "\n        URLs: https://github.com/user/repo-name_with.dots\n        Query: https://github.com/search?q=test&type=code\n        Fragment: https://github.com/user/repo#readme\n        Port: https://github.dev:443/workspace\n        Auth: https://github.com/repo (user info stripped by domain parsing)\n      "
            );
            (expect(result).toContain("https://github.com/user/repo-name_with.dots"),
              expect(result).toContain("https://github.com/search?q=test&type=code"),
              expect(result).toContain("https://github.com/user/repo#readme"),
              expect(result).toContain("https://github.dev:443/workspace"),
              expect(result).toContain("https://github.com/repo"));
          }),
          it("should handle length truncation at exact boundary", () => {
            const input = "x".repeat(524288),
              result = sanitizeContentFunction(input);
            (expect(result.length).toBe(524288), expect(result).not.toContain("[Content truncated due to length]"));
            const overLength = "x".repeat(524388),
              overResult = sanitizeContentFunction(overLength);
            (expect(overResult).toContain("[Content truncated due to length]"), expect(overResult.length).toBeLessThan(overLength.length));
          }),
          it("should handle line truncation at exact boundary", () => {
            const input = Array(65e3).fill("line").join("\n"),
              result = sanitizeContentFunction(input),
              lines = result.split("\n");
            (expect(lines.length).toBe(65e3), expect(result).not.toContain("[Content truncated due to line count]"));
            const overLines = Array(65001).fill("line").join("\n"),
              overResult = sanitizeContentFunction(overLines),
              overResultLines = overResult.split("\n");
            (expect(overResultLines.length).toBeLessThanOrEqual(65001), expect(overResult).toContain("[Content truncated due to line count]"));
          }),
          it("should handle various ANSI escape sequence patterns", () => {
            const result = sanitizeContentFunction("\n        Color: [31mRed[0m [1;32mBold Green[m\n        Cursor: [2J[H Clear and home\n        Other: [?25h Show cursor [K Clear line\n        Complex: [38;5;196mTrueColor[0m\n      ");
            (expect(result).not.toContain("["),
              expect(result).toContain("Red"),
              expect(result).toContain("Bold Green"),
              expect(result).toContain("Clear and home"),
              expect(result).toContain("Show cursor"),
              expect(result).toContain("Clear line"),
              expect(result).toContain("TrueColor"));
          }),
          it("should handle XML tag conversion in complex nested content", () => {
            const result = sanitizeContentFunction('\n        <xml attr="value & \'quotes\'">\n          <![CDATA[<script>alert("xss")<\/script>]]>\n          \x3c!-- comment with "quotes" & \'apostrophes\' --\x3e\n        </xml>\n      ');
            (expect(result).toContain("(xml attr=\"value & 'quotes'\")"), expect(result).toContain('(![CDATA[(script)alert("xss")(/script)]])'), expect(result).not.toContain("comment with"), expect(result).toContain("(/xml)"));
          }),
          it("should handle non-string inputs robustly", () => {
            (expect(sanitizeContentFunction(123)).toBe(""),
              expect(sanitizeContentFunction({})).toBe(""),
              expect(sanitizeContentFunction([])).toBe(""),
              expect(sanitizeContentFunction(!0)).toBe(""),
              expect(sanitizeContentFunction(!1)).toBe(""));
          }),
          it("should preserve line breaks and tabs in content structure", () => {
            const result = sanitizeContentFunction("Line 1\n\t\tIndented line\n\n\nDouble newline\n\n\tTab at start");
            (expect(result).toContain("\n"),
              expect(result).toContain("\t"),
              expect(result.split("\n").length).toBeGreaterThan(1),
              expect(result).toContain("Line 1"),
              expect(result).toContain("Indented line"),
              expect(result).toContain("Tab at start"));
          }),
          it("should handle simultaneous protocol and domain filtering", () => {
            const result = sanitizeContentFunction(
              "\n        Good HTTPS: https://github.com/repo\n        Bad HTTPS: https://evil.com/malware  \n        Bad HTTP allowed domain: http://github.com/repo\n        Mixed: https://evil.com/path?goto=https://github.com/safe\n      "
            );
            (expect(result).toContain("https://github.com/repo"),
              expect(result).toContain("/redacted"),
              expect(result).not.toContain("https://evil.com"),
              expect(result).not.toContain("http://github.com"),
              expect(result).toContain("https://github.com/safe"));
          }));
      }),
      describe("main function", () => {
        (beforeEach(() => {
          const testFile = "/tmp/gh-aw/test-output.txt";
          (fs.existsSync(testFile) && fs.unlinkSync(testFile), (global.fs = fs));
        }),
          afterEach(() => {
            delete global.fs;
          }),
          it("should handle missing GH_AW_SAFE_OUTPUTS environment variable", async () => {
            (delete process.env.GH_AW_SAFE_OUTPUTS,
              await eval(`(async () => { ${sanitizeScript}; await main(); })()`),
              expect(mockCore.info).toHaveBeenCalledWith("GH_AW_SAFE_OUTPUTS not set, no output to collect"),
              expect(mockCore.setOutput).toHaveBeenCalledWith("output", ""));
          }),
          it("should handle non-existent output file", async () => {
            ((process.env.GH_AW_SAFE_OUTPUTS = "/tmp/gh-aw/non-existent-file.txt"),
              await eval(`(async () => { ${sanitizeScript}; await main(); })()`),
              expect(mockCore.info).toHaveBeenCalledWith("Output file does not exist: /tmp/gh-aw/non-existent-file.txt"),
              expect(mockCore.setOutput).toHaveBeenCalledWith("output", ""));
          }),
          it("should handle empty output file", async () => {
            const testFile = "/tmp/gh-aw/test-empty-output.txt";
            (fs.writeFileSync(testFile, "   \n  \t  \n  "),
              (process.env.GH_AW_SAFE_OUTPUTS = testFile),
              await eval(`(async () => { ${sanitizeScript}; await main(); })()`),
              expect(mockCore.info).toHaveBeenCalledWith("Output file is empty"),
              expect(mockCore.setOutput).toHaveBeenCalledWith("output", ""),
              fs.unlinkSync(testFile));
          }),
          it("should process and sanitize output file content", async () => {
            const testContent = "Hello @user! This fixes #123. Link: http://bad.com and https://github.com/repo",
              testFile = "/tmp/gh-aw/test-output.txt";
            (fs.writeFileSync(testFile, testContent),
              (process.env.GH_AW_SAFE_OUTPUTS = testFile),
              await eval(`(async () => { ${sanitizeScript}; await main(); })()`),
              expect(mockCore.info).toHaveBeenCalledWith(expect.stringMatching(/Collected agentic output \(sanitized\):.*@user/)));
            const outputCall = mockCore.setOutput.mock.calls.find(call => "output" === call[0]);
            expect(outputCall).toBeDefined();
            const sanitizedOutput = outputCall[1];
            (expect(sanitizedOutput).toContain("`@user`"),
              expect(sanitizedOutput).toContain("`fixes #123`"),
              expect(sanitizedOutput).toContain("/redacted"),
              expect(sanitizedOutput).toContain("https://github.com/repo"),
              fs.unlinkSync(testFile));
          }),
          it("should truncate log output for very long content", async () => {
            const longContent = "x".repeat(250),
              testFile = "/tmp/gh-aw/test-long-output.txt";
            (fs.writeFileSync(testFile, longContent), (process.env.GH_AW_SAFE_OUTPUTS = testFile), await eval(`(async () => { ${sanitizeScript}; await main(); })()`));
            const logCalls = mockCore.info.mock.calls,
              outputLogCall = logCalls.find(call => call[0] && call[0].includes("Collected agentic output (sanitized):"));
            (expect(outputLogCall).toBeDefined(), expect(outputLogCall[0]).toContain("..."), expect(outputLogCall[0].length).toBeLessThan(longContent.length), fs.unlinkSync(testFile));
          }),
          it("should handle file read errors gracefully", async () => {
            const testFile = "/tmp/gh-aw/test-no-read.txt";
            fs.writeFileSync(testFile, "test content");
            const originalReadFileSync = fs.readFileSync,
              readFileSyncSpy = vi.spyOn(fs, "readFileSync").mockImplementation(() => {
                throw new Error("Permission denied");
              });
            process.env.GH_AW_SAFE_OUTPUTS = testFile;
            let thrownError = null;
            try {
              await eval(`(async () => { ${sanitizeScript}; await main(); })()`);
            } catch (error) {
              thrownError = error;
            }
            (expect(thrownError).toBeTruthy(), expect(thrownError.message).toContain("Permission denied"), readFileSyncSpy.mockRestore(), fs.existsSync(testFile) && fs.unlinkSync(testFile));
          }),
          it("should handle binary file content", async () => {
            const binaryData = Buffer.from([0, 1, 2, 255, 254, 253]),
              testFile = "/tmp/gh-aw/test-binary.txt";
            (fs.writeFileSync(testFile, binaryData), (process.env.GH_AW_SAFE_OUTPUTS = testFile), await eval(`(async () => { ${sanitizeScript}; await main(); })()`));
            const outputCall = mockCore.setOutput.mock.calls.find(call => "output" === call[0]);
            (expect(outputCall).toBeDefined(), fs.unlinkSync(testFile));
          }),
          it("should handle content with only whitespace", async () => {
            const whitespaceContent = "   \n\n\t\t  \r\n  ",
              testFile = "/tmp/gh-aw/test-whitespace.txt";
            (fs.writeFileSync(testFile, whitespaceContent),
              (process.env.GH_AW_SAFE_OUTPUTS = testFile),
              await eval(`(async () => { ${sanitizeScript}; await main(); })()`),
              expect(mockCore.info).toHaveBeenCalledWith("Output file is empty"),
              expect(mockCore.setOutput).toHaveBeenCalledWith("output", ""),
              fs.unlinkSync(testFile));
          }),
          it("should handle very large files with mixed content", async () => {
            const lineContent = 'This is a line with @user and https://evil.com plus <script>alert("xss")<\/script>\n',
              repeatedContent = lineContent.repeat(7e4),
              testFile = "/tmp/gh-aw/test-large-mixed.txt";
            (fs.writeFileSync(testFile, repeatedContent), (process.env.GH_AW_SAFE_OUTPUTS = testFile), await eval(`(async () => { ${sanitizeScript}; await main(); })()`));
            const outputCall = mockCore.setOutput.mock.calls.find(call => "output" === call[0]);
            expect(outputCall).toBeDefined();
            const result = outputCall[1];
            (expect(result).toMatch(/\[Content truncated due to (line count|length)\]/), expect(result).toContain("`@user`"), expect(result).toContain("/redacted"), expect(result).toContain("(script)"), fs.unlinkSync(testFile));
          }),
          it("should preserve log message format for short content", async () => {
            const shortContent = "Short message with @user",
              testFile = "/tmp/gh-aw/test-short.txt";
            (fs.writeFileSync(testFile, shortContent), (process.env.GH_AW_SAFE_OUTPUTS = testFile), await eval(`(async () => { ${sanitizeScript}; await main(); })()`));
            const logCalls = mockCore.info.mock.calls,
              outputLogCall = logCalls.find(call => call[0] && call[0].includes("Collected agentic output (sanitized):"));
            (expect(outputLogCall).toBeDefined(), expect(outputLogCall[0]).not.toContain("..."), expect(outputLogCall[0]).toContain("`@user`"), fs.unlinkSync(testFile));
          }));
      }),
      describe("Command Neutralization", () => {
        (beforeEach(() => {
          (vi.clearAllMocks(), fs.existsSync("/tmp/gh-aw") || fs.mkdirSync("/tmp/gh-aw", { recursive: !0 }));
        }),
          it("should neutralize command at the start of text", async () => {
            process.env.GH_AW_COMMAND = "test-bot";
            const content = "/test-bot please analyze this code",
              testFile = "/tmp/gh-aw/test-command-start.txt";
            (fs.writeFileSync(testFile, content), (process.env.GH_AW_SAFE_OUTPUTS = testFile), await eval(`(async () => { ${sanitizeScript}; await main(); })()`));
            const outputCall = mockCore.setOutput.mock.calls.find(call => "output" === call[0]);
            expect(outputCall).toBeDefined();
            const result = outputCall[1];
            (expect(result).toContain("`/test-bot`"), expect(result).not.toMatch(/^\/test-bot/), fs.unlinkSync(testFile), delete process.env.GH_AW_COMMAND);
          }),
          it("should not neutralize command when it appears later in text", async () => {
            process.env.GH_AW_COMMAND = "helper";
            const content = "I need help from /helper please",
              testFile = "/tmp/gh-aw/test-command-middle.txt";
            (fs.writeFileSync(testFile, content), (process.env.GH_AW_SAFE_OUTPUTS = testFile), await eval(`(async () => { ${sanitizeScript}; await main(); })()`));
            const outputCall = mockCore.setOutput.mock.calls.find(call => "output" === call[0]);
            expect(outputCall).toBeDefined();
            const result = outputCall[1];
            (expect(result).toContain("/helper"), expect(result).toContain("I need help from /helper please"), fs.unlinkSync(testFile), delete process.env.GH_AW_COMMAND);
          }),
          it("should handle command at start with leading whitespace", async () => {
            process.env.GH_AW_COMMAND = "review-bot";
            const content = "  \n/review-bot analyze this PR",
              testFile = "/tmp/gh-aw/test-command-whitespace.txt";
            (fs.writeFileSync(testFile, content), (process.env.GH_AW_SAFE_OUTPUTS = testFile), await eval(`(async () => { ${sanitizeScript}; await main(); })()`));
            const outputCall = mockCore.setOutput.mock.calls.find(call => "output" === call[0]);
            expect(outputCall).toBeDefined();
            const result = outputCall[1];
            (expect(result).toContain("`/review-bot`"), fs.unlinkSync(testFile), delete process.env.GH_AW_COMMAND);
          }),
          it("should not modify text when no command is configured", async () => {
            const content = "/some-bot do something",
              testFile = "/tmp/gh-aw/test-no-command.txt";
            (fs.writeFileSync(testFile, content), (process.env.GH_AW_SAFE_OUTPUTS = testFile), await eval(`(async () => { ${sanitizeScript}; await main(); })()`));
            const outputCall = mockCore.setOutput.mock.calls.find(call => "output" === call[0]);
            expect(outputCall).toBeDefined();
            const result = outputCall[1];
            (expect(result).toContain("/some-bot"), fs.unlinkSync(testFile));
          }),
          it("should handle special characters in command name", async () => {
            process.env.GH_AW_COMMAND = "test-bot_v2";
            const content = "/test-bot_v2 execute task",
              testFile = "/tmp/gh-aw/test-special-chars.txt";
            (fs.writeFileSync(testFile, content), (process.env.GH_AW_SAFE_OUTPUTS = testFile), await eval(`(async () => { ${sanitizeScript}; await main(); })()`));
            const outputCall = mockCore.setOutput.mock.calls.find(call => "output" === call[0]);
            expect(outputCall).toBeDefined();
            const result = outputCall[1];
            (expect(result).toContain("`/test-bot_v2`"), fs.unlinkSync(testFile), delete process.env.GH_AW_COMMAND);
          }),
          it("should combine command neutralization with other sanitizations", async () => {
            process.env.GH_AW_COMMAND = "analyze-bot";
            const content = "/analyze-bot check @user for https://evil.com issues",
              testFile = "/tmp/gh-aw/test-combined.txt";
            (fs.writeFileSync(testFile, content), (process.env.GH_AW_SAFE_OUTPUTS = testFile), await eval(`(async () => { ${sanitizeScript}; await main(); })()`));
            const outputCall = mockCore.setOutput.mock.calls.find(call => "output" === call[0]);
            expect(outputCall).toBeDefined();
            const result = outputCall[1];
            (expect(result).toContain("`/analyze-bot`"), expect(result).toContain("`@user`"), expect(result).toContain("/redacted"), fs.unlinkSync(testFile), delete process.env.GH_AW_COMMAND);
          }));
      }),
      describe("URL Redaction Logging", () => {
        (beforeEach(() => {
          (vi.clearAllMocks(), fs.existsSync("/tmp/gh-aw") || fs.mkdirSync("/tmp/gh-aw", { recursive: !0 }));
        }),
          it("should log when HTTPS URLs with disallowed domains are redacted", async () => {
            const content = "Check out https://evil.com/malware for details",
              testFile = "/tmp/gh-aw/test-url-logging-https.txt";
            (fs.writeFileSync(testFile, content), (process.env.GH_AW_SAFE_OUTPUTS = testFile), await eval(`(async () => { ${sanitizeScript}; await main(); })()`));
            const infoCalls = mockCore.info.mock.calls,
              redactionLog = infoCalls.find(call => call[0] && call[0].includes("Redacted URL: evil.com"));
            (expect(redactionLog).toBeDefined(), expect(redactionLog[0]).toBe("Redacted URL: evil.com"));
            const debugCalls = mockCore.debug.mock.calls,
              fullUrlLog = debugCalls.find(call => call[0] && call[0].includes("Redacted URL (full): https://evil.com/malware"));
            (expect(fullUrlLog).toBeDefined(), fs.unlinkSync(testFile));
          }),
          it("should log when HTTP URLs are redacted", async () => {
            const content = "Visit http://example.com for more info",
              testFile = "/tmp/gh-aw/test-url-logging-http.txt";
            (fs.writeFileSync(testFile, content), (process.env.GH_AW_SAFE_OUTPUTS = testFile), await eval(`(async () => { ${sanitizeScript}; await main(); })()`));
            const infoCalls = mockCore.info.mock.calls,
              redactionLog = infoCalls.find(call => call[0] && call[0].includes("Redacted URL: example.com"));
            (expect(redactionLog).toBeDefined(), expect(redactionLog[0]).toBe("Redacted URL: example.com"));
            const debugCalls = mockCore.debug.mock.calls,
              fullUrlLog = debugCalls.find(call => call[0] && call[0].includes("Redacted URL (full): http://example.com"));
            (expect(fullUrlLog).toBeDefined(), fs.unlinkSync(testFile));
          }),
          it("should log when javascript: URLs are redacted", async () => {
            const content = "Click here: javascript:alert('xss')",
              testFile = "/tmp/gh-aw/test-url-logging-js.txt";
            (fs.writeFileSync(testFile, content), (process.env.GH_AW_SAFE_OUTPUTS = testFile), await eval(`(async () => { ${sanitizeScript}; await main(); })()`));
            const infoCalls = mockCore.info.mock.calls,
              redactionLog = infoCalls.find(call => call[0] && call[0].includes("Redacted URL: javascript:a"));
            (expect(redactionLog).toBeDefined(), expect(redactionLog[0]).toBe("Redacted URL: javascript:a..."));
            const debugCalls = mockCore.debug.mock.calls,
              fullUrlLog = debugCalls.find(call => call[0] && call[0].includes("Redacted URL (full): javascript:alert("));
            (expect(fullUrlLog).toBeDefined(), fs.unlinkSync(testFile));
          }),
          it("should log multiple URL redactions", async () => {
            const content = "Links: http://bad1.com, https://bad2.com, ftp://bad3.com",
              testFile = "/tmp/gh-aw/test-url-logging-multiple.txt";
            (fs.writeFileSync(testFile, content), (process.env.GH_AW_SAFE_OUTPUTS = testFile), await eval(`(async () => { ${sanitizeScript}; await main(); })()`));
            const infoCalls = mockCore.info.mock.calls,
              redactionLogs = infoCalls.filter(call => call[0] && call[0].startsWith("Redacted URL:"));
            (expect(redactionLogs.length).toBeGreaterThanOrEqual(3),
              expect(redactionLogs.some(log => log[0].includes("bad1.com"))).toBe(!0),
              expect(redactionLogs.some(log => log[0].includes("bad2.com"))).toBe(!0),
              expect(redactionLogs.some(log => log[0].includes("bad3.com"))).toBe(!0),
              fs.unlinkSync(testFile));
          }),
          it("should not log when HTTPS URLs with allowed domains are preserved", async () => {
            const content = "Visit https://github.com for more info",
              testFile = "/tmp/gh-aw/test-url-logging-allowed.txt";
            (fs.writeFileSync(testFile, content), (process.env.GH_AW_SAFE_OUTPUTS = testFile), await eval(`(async () => { ${sanitizeScript}; await main(); })()`));
            const infoCalls = mockCore.info.mock.calls,
              redactionLogs = infoCalls.filter(call => call[0] && call[0].includes("Redacted URL: github.com"));
            (expect(redactionLogs.length).toBe(0), fs.unlinkSync(testFile));
          }),
          it("should log when data: URLs are redacted", async () => {
            const content = "Image: data:text/html,<script>alert(1)<\/script>",
              testFile = "/tmp/gh-aw/test-url-logging-data.txt";
            (fs.writeFileSync(testFile, content), (process.env.GH_AW_SAFE_OUTPUTS = testFile), await eval(`(async () => { ${sanitizeScript}; await main(); })()`));
            const infoCalls = mockCore.info.mock.calls,
              redactionLog = infoCalls.find(call => call[0] && call[0].includes("Redacted URL: data:text/ht"));
            (expect(redactionLog).toBeDefined(), fs.unlinkSync(testFile));
          }),
          it("should handle mixed content with both redacted and allowed URLs", async () => {
            const content = "Good: https://github.com/repo Bad: https://evil.com/bad More: http://another.bad",
              testFile = "/tmp/gh-aw/test-url-logging-mixed.txt";
            (fs.writeFileSync(testFile, content), (process.env.GH_AW_SAFE_OUTPUTS = testFile), await eval(`(async () => { ${sanitizeScript}; await main(); })()`));
            const infoCalls = mockCore.info.mock.calls,
              redactionLogs = infoCalls.filter(call => call[0] && call[0].startsWith("Redacted URL:"));
            (expect(redactionLogs.length).toBeGreaterThanOrEqual(2),
              expect(redactionLogs.some(log => log[0].includes("evil.com"))).toBe(!0),
              expect(redactionLogs.some(log => log[0].includes("another.bad"))).toBe(!0),
              expect(redactionLogs.some(log => log[0].includes("github.com"))).toBe(!1),
              fs.unlinkSync(testFile));
          }));
      }));
  }));
