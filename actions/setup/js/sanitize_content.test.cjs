import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";

describe("sanitize_content.cjs", () => {
  let mockCore;
  let sanitizeContent;

  beforeEach(async () => {
    // Mock core actions methods
    mockCore = {
      debug: vi.fn(),
      info: vi.fn(),
      warning: vi.fn(),
      error: vi.fn(),
    };
    global.core = mockCore;

    // Import the module
    const module = await import("./sanitize_content.cjs");
    sanitizeContent = module.sanitizeContent;
  });

  afterEach(() => {
    delete global.core;
    delete process.env.GH_AW_ALLOWED_DOMAINS;
    delete process.env.GH_AW_ALLOWED_GITHUB_REFS;
    delete process.env.GH_AW_COMMAND;
    delete process.env.GITHUB_SERVER_URL;
    delete process.env.GITHUB_API_URL;
    delete process.env.GITHUB_REPOSITORY;
  });

  describe("basic sanitization", () => {
    it("should return empty string for null or undefined input", () => {
      expect(sanitizeContent(null)).toBe("");
      expect(sanitizeContent(undefined)).toBe("");
    });

    it("should return empty string for non-string input", () => {
      expect(sanitizeContent(123)).toBe("");
      expect(sanitizeContent({})).toBe("");
      expect(sanitizeContent([])).toBe("");
    });

    it("should trim whitespace", () => {
      expect(sanitizeContent("  hello world  ")).toBe("hello world");
      expect(sanitizeContent("\n\thello\n\t")).toBe("hello");
    });

    it("should preserve normal text", () => {
      expect(sanitizeContent("Hello, this is normal text.")).toBe("Hello, this is normal text.");
    });
  });

  describe("command neutralization", () => {
    beforeEach(() => {
      process.env.GH_AW_COMMAND = "bot";
    });

    it("should neutralize command at start of text", () => {
      const result = sanitizeContent("/bot do something");
      expect(result).toBe("`/bot` do something");
    });

    it("should neutralize command after whitespace", () => {
      const result = sanitizeContent("  /bot do something");
      expect(result).toBe("`/bot` do something");
    });

    it("should not neutralize command in middle of text", () => {
      const result = sanitizeContent("hello /bot world");
      expect(result).toBe("hello /bot world");
    });

    it("should handle special regex characters in command name", () => {
      process.env.GH_AW_COMMAND = "my-bot+test";
      const result = sanitizeContent("/my-bot+test action");
      expect(result).toBe("`/my-bot+test` action");
    });

    it("should not neutralize when no command is set", () => {
      delete process.env.GH_AW_COMMAND;
      const result = sanitizeContent("/bot do something");
      expect(result).toBe("/bot do something");
    });
  });

  describe("@mention neutralization", () => {
    it("should neutralize @mentions", () => {
      const result = sanitizeContent("Hello @user");
      expect(result).toBe("Hello `@user`");
    });

    it("should neutralize @org/team mentions", () => {
      const result = sanitizeContent("Hello @myorg/myteam");
      expect(result).toBe("Hello `@myorg/myteam`");
    });

    it("should not neutralize @mentions already in backticks", () => {
      const result = sanitizeContent("Already `@user` mentioned");
      expect(result).toBe("Already `@user` mentioned");
    });

    it("should neutralize multiple @mentions", () => {
      const result = sanitizeContent("@user1 and @user2 are here");
      expect(result).toBe("`@user1` and `@user2` are here");
    });

    it("should not neutralize email addresses", () => {
      const result = sanitizeContent("Contact email@example.com");
      expect(result).toBe("Contact email@example.com");
    });
  });

  describe("@mention allowedAliases", () => {
    it("should not neutralize mentions in allowedAliases list", () => {
      const result = sanitizeContent("Hello @author", { allowedAliases: ["author"] });
      expect(result).toBe("Hello @author");
    });

    it("should neutralize mentions not in allowedAliases list", () => {
      const result = sanitizeContent("Hello @other", { allowedAliases: ["author"] });
      expect(result).toBe("Hello `@other`");
    });

    it("should handle multiple mentions with some allowed", () => {
      const result = sanitizeContent("Hello @author and @other", { allowedAliases: ["author"] });
      expect(result).toBe("Hello @author and `@other`");
    });

    it("should handle case-insensitive matching for allowedAliases", () => {
      const result = sanitizeContent("Hello @Author", { allowedAliases: ["author"] });
      expect(result).toBe("Hello @Author");
    });

    it("should handle multiple allowed aliases", () => {
      const result = sanitizeContent("Hello @user1 and @user2 and @other", {
        allowedAliases: ["user1", "user2"],
      });
      expect(result).toBe("Hello @user1 and @user2 and `@other`");
    });

    it("should work with options object containing both maxLength and allowedAliases", () => {
      const result = sanitizeContent("Hello @author and @other", {
        maxLength: 524288,
        allowedAliases: ["author"],
      });
      expect(result).toBe("Hello @author and `@other`");
    });

    it("should handle empty allowedAliases array", () => {
      const result = sanitizeContent("Hello @user", { allowedAliases: [] });
      expect(result).toBe("Hello `@user`");
    });

    it("should not neutralize org/team mentions in allowedAliases", () => {
      const result = sanitizeContent("Hello @myorg/myteam", { allowedAliases: ["myorg/myteam"] });
      expect(result).toBe("Hello @myorg/myteam");
    });

    it("should preserve backward compatibility with numeric maxLength parameter", () => {
      const result = sanitizeContent("Hello @user", 524288);
      expect(result).toBe("Hello `@user`");
    });

    it("should log escaped mentions for debugging", () => {
      const result = sanitizeContent("Hello @user1 and @user2", { allowedAliases: ["user1"] });
      expect(result).toBe("Hello @user1 and `@user2`");
      expect(mockCore.info).toHaveBeenCalledWith("Escaped mention: @user2 (not in allowed list)");
    });

    it("should log multiple escaped mentions", () => {
      const result = sanitizeContent("@user1 @user2 @user3", { allowedAliases: ["user1"] });
      expect(result).toBe("@user1 `@user2` `@user3`");
      expect(mockCore.info).toHaveBeenCalledWith("Escaped mention: @user2 (not in allowed list)");
      expect(mockCore.info).toHaveBeenCalledWith("Escaped mention: @user3 (not in allowed list)");
    });

    it("should not log when all mentions are allowed", () => {
      const result = sanitizeContent("Hello @user1 and @user2", { allowedAliases: ["user1", "user2"] });
      expect(result).toBe("Hello @user1 and @user2");
      // Should not call core.info with any "Escaped mention" messages
      const escapedMentionCalls = mockCore.info.mock.calls.filter(call => call[0].includes("Escaped mention"));
      expect(escapedMentionCalls).toHaveLength(0);
    });
  });

  describe("XML comments removal", () => {
    it("should remove XML comments", () => {
      const result = sanitizeContent("Hello <!-- comment --> world");
      expect(result).toBe("Hello  world");
    });

    it("should remove malformed XML comments", () => {
      const result = sanitizeContent("Hello <!--! comment --!> world");
      expect(result).toBe("Hello  world");
    });

    it("should remove multiline XML comments", () => {
      const result = sanitizeContent("Hello <!-- multi\nline\ncomment --> world");
      expect(result).toBe("Hello  world");
    });
  });

  describe("XML/HTML tag conversion", () => {
    it("should convert opening tags to parentheses", () => {
      const result = sanitizeContent("Hello <div>world</div>");
      expect(result).toBe("Hello (div)world(/div)");
    });

    it("should convert tags with attributes to parentheses", () => {
      const result = sanitizeContent('<div class="test">content</div>');
      expect(result).toBe('(div class="test")content(/div)');
    });

    it("should preserve allowed safe tags", () => {
      const allowedTags = ["b", "blockquote", "br", "code", "details", "em", "h1", "h2", "h3", "h4", "h5", "h6", "hr", "i", "li", "ol", "p", "pre", "strong", "sub", "summary", "sup", "table", "tbody", "td", "th", "thead", "tr", "ul"];
      allowedTags.forEach(tag => {
        const result = sanitizeContent(`<${tag}>content</${tag}>`);
        expect(result).toBe(`<${tag}>content</${tag}>`);
      });
    });

    it("should preserve self-closing br tags", () => {
      const result = sanitizeContent("Hello <br/> world");
      expect(result).toBe("Hello <br/> world");
    });

    it("should preserve br tags without slash", () => {
      const result = sanitizeContent("Hello <br> world");
      expect(result).toBe("Hello <br> world");
    });

    it("should convert self-closing tags that are not allowed", () => {
      const result = sanitizeContent("Hello <img/> world");
      expect(result).toBe("Hello (img/) world");
    });

    it("should handle CDATA sections", () => {
      const result = sanitizeContent("<![CDATA[<script>alert('xss')</script>]]>");
      expect(result).toBe("(![CDATA[(script)alert('xss')(/script)]])");
    });

    it("should preserve inline formatting tags", () => {
      const input = "This is <strong>bold</strong>, <i>italic</i>, and <b>bold too</b> text.";
      const result = sanitizeContent(input);
      expect(result).toBe(input);
    });

    it("should preserve list structure tags", () => {
      const input = "<ul><li>Item 1</li><li>Item 2</li></ul>";
      const result = sanitizeContent(input);
      expect(result).toBe(input);
    });

    it("should preserve ordered list tags", () => {
      const input = "<ol><li>First</li><li>Second</li></ol>";
      const result = sanitizeContent(input);
      expect(result).toBe(input);
    });

    it("should preserve blockquote tags", () => {
      const input = "<blockquote>This is a quote</blockquote>";
      const result = sanitizeContent(input);
      expect(result).toBe(input);
    });

    it("should handle mixed allowed tags with formatting", () => {
      const input = "<p>This is <strong>bold</strong> and <em>italic</em> text.<br>New line here.</p>";
      const result = sanitizeContent(input);
      expect(result).toBe(input);
    });

    it("should handle nested list structure", () => {
      const input = "<ul><li>Item 1<ul><li>Nested item</li></ul></li><li>Item 2</li></ul>";
      const result = sanitizeContent(input);
      expect(result).toBe(input);
    });

    it("should preserve details and summary tags", () => {
      const result1 = sanitizeContent("<details>content</details>");
      expect(result1).toBe("<details>content</details>");

      const result2 = sanitizeContent("<summary>content</summary>");
      expect(result2).toBe("<summary>content</summary>");
    });

    it("should convert removed tags that are no longer allowed", () => {
      // Tag that was previously allowed but is now removed: u
      const result3 = sanitizeContent("<u>content</u>");
      expect(result3).toBe("(u)content(/u)");
    });

    it("should preserve heading tags h1-h6", () => {
      const headings = ["h1", "h2", "h3", "h4", "h5", "h6"];
      headings.forEach(tag => {
        const input = `<${tag}>Heading</${tag}>`;
        const result = sanitizeContent(input);
        expect(result).toBe(input);
      });
    });

    it("should preserve hr tag", () => {
      const result = sanitizeContent("Content before<hr>Content after");
      expect(result).toBe("Content before<hr>Content after");
    });

    it("should preserve pre tag", () => {
      const input = "<pre>Code block content</pre>";
      const result = sanitizeContent(input);
      expect(result).toBe(input);
    });

    it("should preserve sub and sup tags", () => {
      const input1 = "H<sub>2</sub>O";
      const result1 = sanitizeContent(input1);
      expect(result1).toBe(input1);

      const input2 = "E=mc<sup>2</sup>";
      const result2 = sanitizeContent(input2);
      expect(result2).toBe(input2);
    });

    it("should preserve table structure tags", () => {
      const input = "<table><thead><tr><th>Header</th></tr></thead><tbody><tr><td>Data</td></tr></tbody></table>";
      const result = sanitizeContent(input);
      expect(result).toBe(input);
    });
  });

  describe("ANSI escape sequence removal", () => {
    it("should remove ANSI color codes", () => {
      const result = sanitizeContent("\x1b[31mred text\x1b[0m");
      expect(result).toBe("red text");
    });

    it("should remove various ANSI codes", () => {
      const result = sanitizeContent("\x1b[1;32mBold Green\x1b[0m");
      expect(result).toBe("Bold Green");
    });
  });

  describe("control character removal", () => {
    it("should remove control characters", () => {
      const result = sanitizeContent("test\x00\x01\x02\x03content");
      expect(result).toBe("testcontent");
    });

    it("should preserve newlines and tabs", () => {
      const result = sanitizeContent("test\ncontent\twith\ttabs");
      expect(result).toBe("test\ncontent\twith\ttabs");
    });

    it("should remove DEL character", () => {
      const result = sanitizeContent("test\x7Fcontent");
      expect(result).toBe("testcontent");
    });
  });

  describe("URL protocol sanitization", () => {
    it("should allow HTTPS URLs", () => {
      const result = sanitizeContent("Visit https://github.com");
      expect(result).toBe("Visit https://github.com");
    });

    it("should redact HTTP URLs with sanitized domain", () => {
      const result = sanitizeContent("Visit http://example.com");
      expect(result).toContain("(example.com/redacted)");
      expect(mockCore.info).toHaveBeenCalled();
    });

    it("should redact javascript: URLs", () => {
      const result = sanitizeContent("Click javascript:alert('xss')");
      expect(result).toContain("(redacted)");
    });

    it("should redact data: URLs", () => {
      const result = sanitizeContent("Image data:image/png;base64,abc123");
      expect(result).toContain("(redacted)");
    });

    it("should preserve file paths with colons", () => {
      const result = sanitizeContent("C:\\path\\to\\file");
      expect(result).toBe("C:\\path\\to\\file");
    });

    it("should preserve namespace patterns", () => {
      const result = sanitizeContent("std::vector::push_back");
      expect(result).toBe("std::vector::push_back");
    });
  });

  describe("URL domain filtering", () => {
    it("should allow default GitHub domains", () => {
      const urls = ["https://github.com/repo", "https://api.github.com/endpoint", "https://raw.githubusercontent.com/file", "https://example.github.io/page"];

      urls.forEach(url => {
        const result = sanitizeContent(`Visit ${url}`);
        expect(result).toBe(`Visit ${url}`);
      });
    });

    it("should redact disallowed domains with sanitized domain", () => {
      const result = sanitizeContent("Visit https://evil.com/malicious");
      expect(result).toContain("(evil.com/redacted)");
      expect(mockCore.info).toHaveBeenCalled();
    });

    it("should use custom allowed domains from environment", () => {
      process.env.GH_AW_ALLOWED_DOMAINS = "example.com,trusted.net";
      const result = sanitizeContent("Visit https://example.com/page");
      expect(result).toBe("Visit https://example.com/page");
    });

    it("should extract and allow GitHub Enterprise domains", () => {
      process.env.GITHUB_SERVER_URL = "https://github.company.com";
      const result = sanitizeContent("Visit https://github.company.com/repo");
      expect(result).toBe("Visit https://github.company.com/repo");
    });

    it("should allow subdomains of allowed domains", () => {
      const result = sanitizeContent("Visit https://subdomain.github.com/page");
      expect(result).toBe("Visit https://subdomain.github.com/page");
    });

    it("should log redacted domains", () => {
      sanitizeContent("Visit https://verylongdomainnamefortest.com/page");
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Redacted URL:"));
      expect(mockCore.debug).toHaveBeenCalledWith(expect.stringContaining("Redacted URL (full):"));
    });

    it("should support wildcard domain patterns (*.example.com)", () => {
      process.env.GH_AW_ALLOWED_DOMAINS = "*.example.com";
      const result = sanitizeContent("Visit https://subdomain.example.com/page and https://another.example.com/path");
      expect(result).toBe("Visit https://subdomain.example.com/page and https://another.example.com/path");
    });

    it("should allow base domain when wildcard pattern is used", () => {
      process.env.GH_AW_ALLOWED_DOMAINS = "*.example.com";
      const result = sanitizeContent("Visit https://example.com/page");
      expect(result).toBe("Visit https://example.com/page");
    });

    it("should redact domains not matching wildcard pattern", () => {
      process.env.GH_AW_ALLOWED_DOMAINS = "*.example.com";
      const result = sanitizeContent("Visit https://evil.com/malicious");
      expect(result).toContain("(evil.com/redacted)");
    });

    it("should support mixed wildcard and plain domains", () => {
      process.env.GH_AW_ALLOWED_DOMAINS = "github.com,*.githubusercontent.com,api.example.com";
      const result = sanitizeContent("Visit https://github.com/repo, https://raw.githubusercontent.com/user/repo/main/file.txt, " + "https://api.example.com/endpoint, and https://subdomain.githubusercontent.com/file");
      expect(result).toBe("Visit https://github.com/repo, https://raw.githubusercontent.com/user/repo/main/file.txt, " + "https://api.example.com/endpoint, and https://subdomain.githubusercontent.com/file");
    });

    it("should redact domains with wildcards that don't match pattern", () => {
      process.env.GH_AW_ALLOWED_DOMAINS = "*.github.com";
      const result = sanitizeContent("Visit https://github.io/page");
      expect(result).toContain("(github.io/redacted)");
    });

    it("should handle multiple levels of subdomains with wildcard", () => {
      process.env.GH_AW_ALLOWED_DOMAINS = "*.example.com";
      const result = sanitizeContent("Visit https://deep.nested.example.com/page");
      expect(result).toBe("Visit https://deep.nested.example.com/page");
    });
  });

  describe("domain sanitization", () => {
    let sanitizeDomainName;

    beforeEach(async () => {
      const module = await import("./sanitize_content_core.cjs");
      sanitizeDomainName = module.sanitizeDomainName;
    });

    it("should keep domains with 3 or fewer parts unchanged", () => {
      expect(sanitizeDomainName("example.com")).toBe("example.com");
      expect(sanitizeDomainName("sub.example.com")).toBe("sub.example.com");
      // deep.sub.example.com has 4 parts, so it should be truncated
      expect(sanitizeDomainName("a.b.c")).toBe("a.b.c");
    });

    it("should truncate domains with more than 3 parts", () => {
      expect(sanitizeDomainName("a.b.c.d.com")).toBe("a.b.c...");
      expect(sanitizeDomainName("one.two.three.four.five.com")).toBe("one.two.three...");
    });

    it("should remove non-alphanumeric characters from each part", () => {
      expect(sanitizeDomainName("ex@mple.com")).toBe("exmple.com");
      expect(sanitizeDomainName("my-domain.co.uk")).toBe("mydomain.co.uk");
      expect(sanitizeDomainName("test_site.com")).toBe("testsite.com");
    });

    it("should handle empty parts after sanitization", () => {
      expect(sanitizeDomainName("...example.com")).toBe("example.com");
      expect(sanitizeDomainName("test..com")).toBe("test.com");
      expect(sanitizeDomainName("a.-.-.b.com")).toBe("a.b.com");
    });

    it("should handle domains with ports", () => {
      expect(sanitizeDomainName("example.com:8080")).toBe("example.com8080");
    });

    it("should handle complex special characters", () => {
      expect(sanitizeDomainName("ex!@#$ample.c%^&*om")).toBe("example.com");
      expect(sanitizeDomainName("test.ex@mple.co-uk")).toBe("test.exmple.couk");
    });

    it("should handle null and undefined inputs", () => {
      expect(sanitizeDomainName(null)).toBe("");
      expect(sanitizeDomainName(undefined)).toBe("");
    });

    it("should handle empty string", () => {
      expect(sanitizeDomainName("")).toBe("");
    });

    it("should handle non-string inputs", () => {
      expect(sanitizeDomainName(123)).toBe("");
      expect(sanitizeDomainName({})).toBe("");
    });

    it("should handle domains that become empty after sanitization", () => {
      expect(sanitizeDomainName("...")).toBe("");
      expect(sanitizeDomainName("@#$")).toBe("");
    });

    it("should truncate with ... for 4+ parts after sanitization", () => {
      expect(sanitizeDomainName("alpha.beta.gamma.delta.epsilon.com")).toBe("alpha.beta.gamma...");
    });

    it("should handle mixed case domains", () => {
      expect(sanitizeDomainName("Example.COM")).toBe("Example.COM");
      expect(sanitizeDomainName("Sub.Example.Com")).toBe("Sub.Example.Com");
    });

    it("should handle unicode characters", () => {
      expect(sanitizeDomainName("tëst.com")).toBe("tst.com");
      expect(sanitizeDomainName("例え.com")).toBe("com");
    });

    it("should apply sanitization in actual URL redaction for HTTP", () => {
      const result = sanitizeContent("Visit http://sub.example.malicious.com/path");
      expect(result).toContain("(sub.example.malicious.../redacted)");
    });

    it("should apply sanitization in actual URL redaction for HTTPS", () => {
      const result = sanitizeContent("Visit https://very.deep.nested.subdomain.evil.com/path");
      expect(result).toContain("(very.deep.nested.../redacted)");
    });

    it("should handle domains with special characters in URL context", () => {
      // The regex captures domain up to first special character like @
      // So http://ex@mple-domain.co_uk.net captures only "ex" as domain
      const result = sanitizeContent("Visit http://ex@mple-domain.co_uk.net/path");
      expect(result).toContain("(ex/redacted)");
    });

    it("should preserve simple domain structure", () => {
      const result = sanitizeContent("Visit http://test.com/path");
      expect(result).toContain("(test.com/redacted)");
    });

    it("should handle subdomain with 3 parts correctly", () => {
      // api.v2.example.com has 4 parts, so it will be truncated
      const result = sanitizeContent("Visit http://api.v2.example.com/endpoint");
      expect(result).toContain("(api.v2.example.../redacted)");
    });

    it("should handle 5+ part domains", () => {
      expect(sanitizeDomainName("a.b.c.d.e.f.com")).toBe("a.b.c...");
    });

    it("should handle domains starting with numbers", () => {
      expect(sanitizeDomainName("123.456.example.com")).toBe("123.456.example...");
    });

    it("should handle single part domain", () => {
      expect(sanitizeDomainName("localhost")).toBe("localhost");
    });
  });

  describe("bot trigger neutralization", () => {
    it("should neutralize 'fixes #123' patterns", () => {
      const result = sanitizeContent("This fixes #123");
      expect(result).toBe("This `fixes #123`");
    });

    it("should neutralize 'closes #456' patterns", () => {
      const result = sanitizeContent("PR closes #456");
      expect(result).toBe("PR `closes #456`");
    });

    it("should neutralize 'resolves #789' patterns", () => {
      const result = sanitizeContent("This resolves #789");
      expect(result).toBe("This `resolves #789`");
    });

    it("should handle various bot trigger verbs", () => {
      const triggers = ["fix", "fixes", "close", "closes", "resolve", "resolves"];
      triggers.forEach(verb => {
        const result = sanitizeContent(`This ${verb} #123`);
        expect(result).toBe(`This \`${verb} #123\``);
      });
    });

    it("should neutralize alphanumeric issue references", () => {
      const result = sanitizeContent("fixes #abc123def");
      expect(result).toBe("`fixes #abc123def`");
    });
  });

  describe("GitHub reference neutralization", () => {
    beforeEach(() => {
      delete process.env.GH_AW_ALLOWED_GITHUB_REFS;
      delete process.env.GITHUB_REPOSITORY;
    });

    afterEach(() => {
      delete process.env.GH_AW_ALLOWED_GITHUB_REFS;
      delete process.env.GITHUB_REPOSITORY;
    });

    it("should allow all references by default (no env var set)", () => {
      const result = sanitizeContent("See issue #123 and owner/repo#456");
      // When no env var is set, all references are allowed
      expect(result).toBe("See issue #123 and owner/repo#456");
    });

    it("should restrict to current repo only when 'repo' is specified", () => {
      process.env.GITHUB_REPOSITORY = "myorg/myrepo";
      process.env.GH_AW_ALLOWED_GITHUB_REFS = "repo";

      const result = sanitizeContent("See issue #123 and other/repo#456");
      expect(result).toBe("See issue #123 and `other/repo#456`");
    });

    it("should allow current repo references with 'repo' keyword", () => {
      process.env.GITHUB_REPOSITORY = "myorg/myrepo";
      process.env.GH_AW_ALLOWED_GITHUB_REFS = "repo";

      const result = sanitizeContent("See myorg/myrepo#123");
      expect(result).toBe("See myorg/myrepo#123");
    });

    it("should allow specific repos in the list", () => {
      process.env.GITHUB_REPOSITORY = "myorg/myrepo";
      process.env.GH_AW_ALLOWED_GITHUB_REFS = "repo,other/allowed-repo";

      const result = sanitizeContent("See #123, other/allowed-repo#456, and bad/repo#789");
      expect(result).toBe("See #123, other/allowed-repo#456, and `bad/repo#789`");
    });

    it("should handle multiple allowed repos", () => {
      process.env.GITHUB_REPOSITORY = "myorg/myrepo";
      process.env.GH_AW_ALLOWED_GITHUB_REFS = "myorg/myrepo,other/repo,another/repo";

      const result = sanitizeContent("Issues: myorg/myrepo#1, other/repo#2, another/repo#3, blocked/repo#4");
      expect(result).toBe("Issues: myorg/myrepo#1, other/repo#2, another/repo#3, `blocked/repo#4`");
    });

    it("should be case-insensitive for repo names", () => {
      process.env.GITHUB_REPOSITORY = "MyOrg/MyRepo";
      process.env.GH_AW_ALLOWED_GITHUB_REFS = "repo";

      const result = sanitizeContent("Issues: myorg/myrepo#123, MYORG/MYREPO#456");
      expect(result).toBe("Issues: myorg/myrepo#123, MYORG/MYREPO#456");
    });

    it("should not escape references inside backticks", () => {
      process.env.GITHUB_REPOSITORY = "myorg/myrepo";
      process.env.GH_AW_ALLOWED_GITHUB_REFS = "repo";

      const result = sanitizeContent("Already escaped: `other/repo#123`");
      expect(result).toBe("Already escaped: `other/repo#123`");
    });

    it("should handle issue numbers with alphanumeric characters", () => {
      process.env.GITHUB_REPOSITORY = "myorg/myrepo";
      process.env.GH_AW_ALLOWED_GITHUB_REFS = "repo";

      const result = sanitizeContent("See #abc123 and other/repo#def456");
      expect(result).toBe("See #abc123 and `other/repo#def456`");
    });

    it("should handle references in different contexts", () => {
      process.env.GITHUB_REPOSITORY = "myorg/myrepo";
      process.env.GH_AW_ALLOWED_GITHUB_REFS = "repo";

      const result = sanitizeContent("Start #123 middle other/repo#456 end");
      expect(result).toBe("Start #123 middle `other/repo#456` end");
    });

    it("should trim whitespace in allowed-refs list", () => {
      process.env.GITHUB_REPOSITORY = "myorg/myrepo";
      process.env.GH_AW_ALLOWED_GITHUB_REFS = " repo , other/repo ";

      const result = sanitizeContent("See myorg/myrepo#123 and other/repo#456");
      expect(result).toBe("See myorg/myrepo#123 and other/repo#456");
    });

    it("should log when escaping references", () => {
      process.env.GITHUB_REPOSITORY = "myorg/myrepo";
      process.env.GH_AW_ALLOWED_GITHUB_REFS = "repo";

      sanitizeContent("See other/repo#123");
      expect(mockCore.info).toHaveBeenCalledWith("Escaped GitHub reference: other/repo#123 (not in allowed list)");
    });

    it("should escape all references when allowed-refs is empty array", () => {
      process.env.GITHUB_REPOSITORY = "myorg/myrepo";
      process.env.GH_AW_ALLOWED_GITHUB_REFS = "";

      const result = sanitizeContent("See #123 and myorg/myrepo#456 and other/repo#789");
      expect(result).toBe("See `#123` and `myorg/myrepo#456` and `other/repo#789`");
    });

    it("should handle empty allowed-refs list (all references escaped)", () => {
      process.env.GITHUB_REPOSITORY = "myorg/myrepo";
      process.env.GH_AW_ALLOWED_GITHUB_REFS = "";

      const result = sanitizeContent("See #123 and other/repo#456");
      expect(result).toBe("See `#123` and `other/repo#456`");
    });

    it("should escape references when current repo is not in list", () => {
      process.env.GITHUB_REPOSITORY = "myorg/myrepo";
      process.env.GH_AW_ALLOWED_GITHUB_REFS = "other/allowed";

      const result = sanitizeContent("See #123 and myorg/myrepo#456");
      expect(result).toBe("See `#123` and `myorg/myrepo#456`");
    });

    it("should handle references with hyphens in repo names", () => {
      process.env.GITHUB_REPOSITORY = "my-org/my-repo";
      process.env.GH_AW_ALLOWED_GITHUB_REFS = "repo";

      const result = sanitizeContent("See my-org/my-repo#123 and other-org/other-repo#456");
      expect(result).toBe("See my-org/my-repo#123 and `other-org/other-repo#456`");
    });

    it("should handle references with underscores in repo names", () => {
      process.env.GITHUB_REPOSITORY = "myorg/my_repo";
      process.env.GH_AW_ALLOWED_GITHUB_REFS = "repo";

      const result = sanitizeContent("See myorg/my_repo#123 and otherorg/other_repo#456");
      expect(result).toBe("See myorg/my_repo#123 and `otherorg/other_repo#456`");
    });

    it("should handle references with dots in repo names", () => {
      process.env.GITHUB_REPOSITORY = "myorg/my.repo";
      process.env.GH_AW_ALLOWED_GITHUB_REFS = "repo,other/repo.test";

      const result = sanitizeContent("See myorg/my.repo#123 and other/repo.test#456");
      expect(result).toBe("See myorg/my.repo#123 and other/repo.test#456");
    });

    it("should handle multiple references in same sentence", () => {
      process.env.GITHUB_REPOSITORY = "myorg/myrepo";
      process.env.GH_AW_ALLOWED_GITHUB_REFS = "repo,other/allowed";

      const result = sanitizeContent("Related to #1, #2, other/allowed#3, and blocked/repo#4");
      expect(result).toBe("Related to #1, #2, other/allowed#3, and `blocked/repo#4`");
    });

    it("should handle references at start and end of string", () => {
      process.env.GITHUB_REPOSITORY = "myorg/myrepo";
      process.env.GH_AW_ALLOWED_GITHUB_REFS = "repo";

      const result = sanitizeContent("#123 in the middle other/repo#456");
      expect(result).toBe("#123 in the middle `other/repo#456`");
    });

    it("should not escape references in code blocks", () => {
      process.env.GITHUB_REPOSITORY = "myorg/myrepo";
      process.env.GH_AW_ALLOWED_GITHUB_REFS = "repo";

      const result = sanitizeContent("Code: `other/repo#123` end");
      expect(result).toBe("Code: `other/repo#123` end");
    });

    it("should handle mixed case in repo specification", () => {
      process.env.GITHUB_REPOSITORY = "MyOrg/MyRepo";
      process.env.GH_AW_ALLOWED_GITHUB_REFS = "myorg/myrepo,Other/Repo";

      const result = sanitizeContent("See MyOrg/MyRepo#1, myorg/myrepo#2, OTHER/REPO#3, blocked/repo#4");
      expect(result).toBe("See MyOrg/MyRepo#1, myorg/myrepo#2, OTHER/REPO#3, `blocked/repo#4`");
    });

    it("should handle very long issue numbers", () => {
      process.env.GITHUB_REPOSITORY = "myorg/myrepo";
      process.env.GH_AW_ALLOWED_GITHUB_REFS = "repo";

      const result = sanitizeContent("See #123456789012345 and other/repo#999999999");
      expect(result).toBe("See #123456789012345 and `other/repo#999999999`");
    });

    it("should handle no GITHUB_REPOSITORY env var with 'repo' keyword", () => {
      delete process.env.GITHUB_REPOSITORY;
      process.env.GH_AW_ALLOWED_GITHUB_REFS = "repo";

      const result = sanitizeContent("See #123 and other/repo#456");
      // When GITHUB_REPOSITORY is not set, #123 targets empty string which won't match "repo", so not escaped
      // But since we're trying to restrict to "repo" only, and current repo is unknown, all refs stay as-is
      // because the restriction only applies when it can be determined
      expect(result).toBe("See #123 and `other/repo#456`");
    });

    it("should handle specific repo allowed but not current", () => {
      process.env.GITHUB_REPOSITORY = "myorg/myrepo";
      process.env.GH_AW_ALLOWED_GITHUB_REFS = "other/specific";

      const result = sanitizeContent("See #123 and other/specific#456");
      expect(result).toBe("See `#123` and other/specific#456");
    });

    it("should preserve spacing around escaped references", () => {
      process.env.GITHUB_REPOSITORY = "myorg/myrepo";
      process.env.GH_AW_ALLOWED_GITHUB_REFS = "repo";

      const result = sanitizeContent("Before other/repo#123 after");
      expect(result).toBe("Before `other/repo#123` after");
    });
  });

  describe("content truncation", () => {
    it("should truncate content exceeding max length", () => {
      const longContent = "x".repeat(600000);
      const result = sanitizeContent(longContent);

      expect(result.length).toBeLessThan(longContent.length);
      expect(result).toContain("[Content truncated due to length]");
    });

    it("should truncate content exceeding max lines", () => {
      const manyLines = Array(70000).fill("line").join("\n");
      const result = sanitizeContent(manyLines);

      expect(result.split("\n").length).toBeLessThan(70000);
      expect(result).toContain("[Content truncated due to line count]");
    });

    it("should respect custom max length parameter", () => {
      const content = "x".repeat(200);
      const result = sanitizeContent(content, 100);

      expect(result.length).toBeLessThanOrEqual(100 + 50); // +50 for truncation message
      expect(result).toContain("[Content truncated");
    });

    it("should not truncate short content", () => {
      const shortContent = "This is a short message";
      const result = sanitizeContent(shortContent);

      expect(result).toBe(shortContent);
      expect(result).not.toContain("[Content truncated");
    });
  });

  describe("combined sanitization", () => {
    it("should apply all sanitizations correctly", () => {
      const input = `  
        <!-- comment -->
        Hello @user, visit https://github.com
        <script>alert('xss')</script>
        This fixes #123
        \x1b[31mRed text\x1b[0m
      `;

      const result = sanitizeContent(input);

      expect(result).not.toContain("<!-- comment -->");
      expect(result).toContain("`@user`");
      expect(result).toContain("https://github.com");
      expect(result).not.toContain("<script>");
      expect(result).toContain("(script)");
      expect(result).toContain("`fixes #123`");
      expect(result).not.toContain("\x1b[31m");
      expect(result).toContain("Red text");
    });

    it("should handle malicious XSS attempts", () => {
      const maliciousInputs = ['<img src=x onerror="alert(1)">', "javascript:alert(document.cookie)", '<svg onload="alert(1)">', "data:text/html,<script>alert(1)</script>"];

      maliciousInputs.forEach(input => {
        const result = sanitizeContent(input);
        expect(result).not.toContain("<img");
        expect(result).not.toContain("javascript:");
        expect(result).not.toContain("<svg");
        expect(result).not.toContain("data:");
      });
    });

    it("should preserve allowed HTML in safe context", () => {
      const input = "<table><thead><tr><th>Header</th></tr></thead><tbody><tr><td>Data</td></tr></tbody></table>";
      const result = sanitizeContent(input);

      expect(result).toBe(input);
    });
  });

  describe("edge cases", () => {
    it("should handle empty string", () => {
      expect(sanitizeContent("")).toBe("");
    });

    it("should handle whitespace-only input", () => {
      expect(sanitizeContent("   \n\t  ")).toBe("");
    });

    it("should handle content with only control characters", () => {
      const result = sanitizeContent("\x00\x01\x02\x03");
      expect(result).toBe("");
    });

    it("should handle content with multiple consecutive spaces", () => {
      const result = sanitizeContent("hello     world");
      expect(result).toBe("hello     world");
    });

    it("should handle Unicode characters", () => {
      const result = sanitizeContent("Hello 世界 🌍");
      expect(result).toBe("Hello 世界 🌍");
    });

    it("should handle URLs in query parameters", () => {
      const input = "https://github.com/redirect?url=https://github.com/target";
      const result = sanitizeContent(input);

      expect(result).toContain("github.com");
      expect(result).not.toContain("(redacted)");
    });

    it("should handle nested backticks", () => {
      const result = sanitizeContent("Already `@user` and @other");
      expect(result).toBe("Already `@user` and `@other`");
    });
  });

  describe("redacted domains collection", () => {
    let getRedactedDomains;
    let clearRedactedDomains;
    let writeRedactedDomainsLog;
    const fs = require("fs");
    const path = require("path");

    beforeEach(async () => {
      const module = await import("./sanitize_content.cjs");
      getRedactedDomains = module.getRedactedDomains;
      clearRedactedDomains = module.clearRedactedDomains;
      writeRedactedDomainsLog = module.writeRedactedDomainsLog;
      // Clear collected domains before each test
      clearRedactedDomains();
    });

    it("should collect redacted HTTPS domains", () => {
      sanitizeContent("Visit https://evil.com/malware");
      const domains = getRedactedDomains();
      expect(domains.length).toBe(1);
      expect(domains[0]).toBe("evil.com");
    });

    it("should collect redacted HTTP domains", () => {
      sanitizeContent("Visit http://example.com");
      const domains = getRedactedDomains();
      expect(domains.length).toBe(1);
      expect(domains[0]).toBe("example.com");
    });

    it("should collect redacted dangerous protocols", () => {
      sanitizeContent("Click javascript:alert(1)");
      const domains = getRedactedDomains();
      expect(domains.length).toBe(1);
      expect(domains[0]).toBe("javascript:");
    });

    it("should collect multiple redacted domains", () => {
      sanitizeContent("Visit https://bad1.com and http://bad2.com");
      const domains = getRedactedDomains();
      expect(domains.length).toBe(2);
      expect(domains).toContain("bad1.com");
      expect(domains).toContain("bad2.com");
    });

    it("should not collect allowed domains", () => {
      sanitizeContent("Visit https://github.com/repo");
      const domains = getRedactedDomains();
      expect(domains.length).toBe(0);
    });

    it("should clear collected domains", () => {
      sanitizeContent("Visit https://evil.com");
      expect(getRedactedDomains().length).toBe(1);
      clearRedactedDomains();
      expect(getRedactedDomains().length).toBe(0);
    });

    it("should return a copy of domains array", () => {
      sanitizeContent("Visit https://evil.com");
      const domains1 = getRedactedDomains();
      const domains2 = getRedactedDomains();
      expect(domains1).not.toBe(domains2);
      expect(domains1).toEqual(domains2);
    });

    describe("writeRedactedDomainsLog", () => {
      const testDir = "/tmp/gh-aw-test-redacted";
      const testFile = `${testDir}/redacted-urls.log`;

      afterEach(() => {
        // Clean up test files
        if (fs.existsSync(testFile)) {
          fs.unlinkSync(testFile);
        }
        if (fs.existsSync(testDir)) {
          fs.rmSync(testDir, { recursive: true, force: true });
        }
      });

      it("should return null when no domains collected", () => {
        const result = writeRedactedDomainsLog(testFile);
        expect(result).toBeNull();
        expect(fs.existsSync(testFile)).toBe(false);
      });

      it("should write domains to log file", () => {
        sanitizeContent("Visit https://evil.com/malware");
        const result = writeRedactedDomainsLog(testFile);
        expect(result).toBe(testFile);
        expect(fs.existsSync(testFile)).toBe(true);

        const content = fs.readFileSync(testFile, "utf8");
        expect(content).toContain("evil.com");
        // Should NOT contain the full URL, only the domain
        expect(content).not.toContain("https://evil.com/malware");
      });

      it("should write multiple domains to log file", () => {
        sanitizeContent("Visit https://bad1.com and http://bad2.com");
        writeRedactedDomainsLog(testFile);

        const content = fs.readFileSync(testFile, "utf8");
        const lines = content.trim().split("\n");
        expect(lines.length).toBe(2);
        expect(content).toContain("bad1.com");
        expect(content).toContain("bad2.com");
      });

      it("should create directory if it does not exist", () => {
        const nestedFile = `${testDir}/nested/redacted-urls.log`;
        sanitizeContent("Visit https://evil.com");
        writeRedactedDomainsLog(nestedFile);
        expect(fs.existsSync(nestedFile)).toBe(true);

        // Clean up nested directory
        fs.unlinkSync(nestedFile);
        fs.rmdirSync(path.dirname(nestedFile));
      });

      it("should use default path when not specified", () => {
        const defaultPath = "/tmp/gh-aw/redacted-urls.log";
        sanitizeContent("Visit https://evil.com");
        const result = writeRedactedDomainsLog();
        expect(result).toBe(defaultPath);
        expect(fs.existsSync(defaultPath)).toBe(true);

        // Clean up
        fs.unlinkSync(defaultPath);
      });
    });
  });

  describe("Unicode hardening transformations", () => {
    describe("zero-width character removal", () => {
      it("should remove zero-width space (U+200B)", () => {
        const input = "Hello\u200BWorld";
        const expected = "HelloWorld";
        expect(sanitizeContent(input)).toBe(expected);
      });

      it("should remove zero-width non-joiner (U+200C)", () => {
        const input = "Test\u200CText";
        const expected = "TestText";
        expect(sanitizeContent(input)).toBe(expected);
      });

      it("should remove zero-width joiner (U+200D)", () => {
        const input = "Hello\u200DWorld";
        const expected = "HelloWorld";
        expect(sanitizeContent(input)).toBe(expected);
      });

      it("should remove word joiner (U+2060)", () => {
        const input = "Word\u2060Joiner";
        const expected = "WordJoiner";
        expect(sanitizeContent(input)).toBe(expected);
      });

      it("should remove byte order mark (U+FEFF)", () => {
        const input = "\uFEFFHello World";
        const expected = "Hello World";
        expect(sanitizeContent(input)).toBe(expected);
      });

      it("should remove multiple zero-width characters", () => {
        const input = "A\u200BB\u200CC\u200DD\u2060E\uFEFFF";
        const expected = "ABCDEF";
        expect(sanitizeContent(input)).toBe(expected);
      });

      it("should handle text with only zero-width characters", () => {
        const input = "\u200B\u200C\u200D";
        const expected = "";
        expect(sanitizeContent(input)).toBe(expected);
      });
    });

    describe("Unicode normalization (NFC)", () => {
      it("should normalize composed characters", () => {
        // e + combining acute accent -> precomposed é
        const input = "cafe\u0301"; // café with combining accent
        const result = sanitizeContent(input);
        // After NFC normalization, should be composed form
        expect(result).toBe("café");
        // Verify it's the precomposed character (U+00E9)
        expect(result.charCodeAt(3)).toBe(0x00e9);
      });

      it("should normalize multiple combining characters", () => {
        const input = "n\u0303"; // ñ with combining tilde
        const result = sanitizeContent(input);
        expect(result).toBe("ñ");
      });

      it("should handle already normalized text", () => {
        const input = "Hello World";
        const expected = "Hello World";
        expect(sanitizeContent(input)).toBe(expected);
      });
    });

    describe("full-width ASCII conversion", () => {
      it("should convert full-width exclamation mark", () => {
        const input = "Hello\uFF01"; // Full-width !
        const expected = "Hello!";
        expect(sanitizeContent(input)).toBe(expected);
      });

      it("should convert full-width letters", () => {
        const input = "\uFF21\uFF22\uFF23"; // Full-width ABC
        const expected = "ABC";
        expect(sanitizeContent(input)).toBe(expected);
      });

      it("should convert full-width digits", () => {
        const input = "\uFF11\uFF12\uFF13"; // Full-width 123
        const expected = "123";
        expect(sanitizeContent(input)).toBe(expected);
      });

      it("should convert full-width parentheses", () => {
        const input = "\uFF08test\uFF09"; // Full-width (test)
        const expected = "(test)";
        expect(sanitizeContent(input)).toBe(expected);
      });

      it("should convert mixed full-width and normal text", () => {
        const input = "Hello\uFF01 \uFF37orld"; // Hello! World with full-width ! and W
        const expected = "Hello! World";
        expect(sanitizeContent(input)).toBe(expected);
      });

      it("should convert full-width at sign", () => {
        const input = "\uFF20user"; // Full-width @user
        // Note: @ mention will also be neutralized
        const result = sanitizeContent(input);
        expect(result).toBe("`@user`");
      });

      it("should handle entire sentence in full-width", () => {
        const input = "\uFF28\uFF45\uFF4C\uFF4C\uFF4F"; // Full-width Hello
        const expected = "Hello";
        expect(sanitizeContent(input)).toBe(expected);
      });
    });

    describe("directional override removal", () => {
      it("should remove left-to-right embedding (U+202A)", () => {
        const input = "Hello\u202AWorld";
        const expected = "HelloWorld";
        expect(sanitizeContent(input)).toBe(expected);
      });

      it("should remove right-to-left embedding (U+202B)", () => {
        const input = "Hello\u202BWorld";
        const expected = "HelloWorld";
        expect(sanitizeContent(input)).toBe(expected);
      });

      it("should remove pop directional formatting (U+202C)", () => {
        const input = "Hello\u202CWorld";
        const expected = "HelloWorld";
        expect(sanitizeContent(input)).toBe(expected);
      });

      it("should remove left-to-right override (U+202D)", () => {
        const input = "Hello\u202DWorld";
        const expected = "HelloWorld";
        expect(sanitizeContent(input)).toBe(expected);
      });

      it("should remove right-to-left override (U+202E)", () => {
        const input = "Hello\u202EWorld";
        const expected = "HelloWorld";
        expect(sanitizeContent(input)).toBe(expected);
      });

      it("should remove left-to-right isolate (U+2066)", () => {
        const input = "Hello\u2066World";
        const expected = "HelloWorld";
        expect(sanitizeContent(input)).toBe(expected);
      });

      it("should remove right-to-left isolate (U+2067)", () => {
        const input = "Hello\u2067World";
        const expected = "HelloWorld";
        expect(sanitizeContent(input)).toBe(expected);
      });

      it("should remove first strong isolate (U+2068)", () => {
        const input = "Hello\u2068World";
        const expected = "HelloWorld";
        expect(sanitizeContent(input)).toBe(expected);
      });

      it("should remove pop directional isolate (U+2069)", () => {
        const input = "Hello\u2069World";
        const expected = "HelloWorld";
        expect(sanitizeContent(input)).toBe(expected);
      });

      it("should remove multiple directional controls", () => {
        const input = "A\u202AB\u202BC\u202CD\u202DE\u202EF\u2066G\u2067H\u2068I\u2069J";
        const expected = "ABCDEFGHIJ";
        expect(sanitizeContent(input)).toBe(expected);
      });
    });

    describe("combined Unicode attacks", () => {
      it("should handle combination of zero-width and directional controls", () => {
        const input = "Hello\u200B\u202EWorld\u200C";
        const expected = "HelloWorld";
        expect(sanitizeContent(input)).toBe(expected);
      });

      it("should handle combination of full-width and zero-width", () => {
        const input = "\uFF28\u200Bello"; // Full-width H + zero-width space + ello
        const expected = "Hello";
        expect(sanitizeContent(input)).toBe(expected);
      });

      it("should handle all transformations together", () => {
        // Full-width H, zero-width space, combining accent, RTL override, normal text
        const input = "\uFF28\u200Be\u0301\u202Ello";
        const expected = "Héllo";
        expect(sanitizeContent(input)).toBe(expected);
      });

      it("should prevent visual spoofing with mixed scripts", () => {
        // Example: trying to hide malicious text with RTL override
        const input = "filename\u202E.txt.exe";
        // Should remove the RTL override
        const expected = "filename.txt.exe";
        expect(sanitizeContent(input)).toBe(expected);
      });

      it("should handle deeply nested Unicode attacks", () => {
        const input = "\uFEFF\u200B\uFF21\u202E\u0301\u200C";
        // BOM + ZWS + full-width A + RTL + combining + ZWNJ
        const result = sanitizeContent(input);
        // Should result in just "A" with the combining accent normalized
        expect(result.replace(/\u0301/g, "")).toBe("A");
      });
    });

    describe("edge cases and boundary conditions", () => {
      it("should handle empty string", () => {
        expect(sanitizeContent("")).toBe("");
      });

      it("should handle string with only invisible characters", () => {
        const input = "\u200B\u202E\uFEFF";
        expect(sanitizeContent(input)).toBe("");
      });

      it("should preserve regular whitespace", () => {
        const input = "Hello   World\t\nTest";
        const result = sanitizeContent(input);
        // Should preserve spaces, tabs, and newlines (though trimmed at end)
        expect(result).toContain("Hello");
        expect(result).toContain("World");
      });

      it("should not affect emoji", () => {
        const input = "Hello 👋 World 🌍";
        const result = sanitizeContent(input);
        expect(result).toContain("👋");
        expect(result).toContain("🌍");
      });

      it("should handle long text with scattered Unicode attacks", () => {
        const longText = "A".repeat(100) + "\u200B" + "B".repeat(100) + "\u202E" + "C".repeat(100);
        const result = sanitizeContent(longText);
        // Should remove the invisible characters
        expect(result.length).toBe(300); // 100 + 100 + 100
        expect(result.includes("\u200B")).toBe(false);
        expect(result.includes("\u202E")).toBe(false);
      });
    });
  });

  describe("HTML entity decoding for @mention bypass prevention", () => {
    it("should decode &commat; and neutralize resulting @mention", () => {
      const result = sanitizeContent("Please review &commat;pelikhan");
      expect(result).toBe("Please review `@pelikhan`");
    });

    it("should decode double-encoded &amp;commat; and neutralize resulting @mention", () => {
      const result = sanitizeContent("Please review &amp;commat;pelikhan");
      expect(result).toBe("Please review `@pelikhan`");
    });

    it("should decode &#64; (decimal) and neutralize resulting @mention", () => {
      const result = sanitizeContent("Please review &#64;pelikhan");
      expect(result).toBe("Please review `@pelikhan`");
    });

    it("should decode double-encoded &amp;#64; and neutralize resulting @mention", () => {
      const result = sanitizeContent("Please review &amp;#64;pelikhan");
      expect(result).toBe("Please review `@pelikhan`");
    });

    it("should decode &#x40; (hex lowercase) and neutralize resulting @mention", () => {
      const result = sanitizeContent("Please review &#x40;pelikhan");
      expect(result).toBe("Please review `@pelikhan`");
    });

    it("should decode &#X40; (hex uppercase) and neutralize resulting @mention", () => {
      const result = sanitizeContent("Please review &#X40;pelikhan");
      expect(result).toBe("Please review `@pelikhan`");
    });

    it("should decode double-encoded &amp;#x40; and neutralize resulting @mention", () => {
      const result = sanitizeContent("Please review &amp;#x40;pelikhan");
      expect(result).toBe("Please review `@pelikhan`");
    });

    it("should decode double-encoded &amp;#X40; and neutralize resulting @mention", () => {
      const result = sanitizeContent("Please review &amp;#X40;pelikhan");
      expect(result).toBe("Please review `@pelikhan`");
    });

    it("should decode multiple HTML-encoded @mentions", () => {
      const result = sanitizeContent("&commat;user1 and &#64;user2 and &#x40;user3");
      expect(result).toBe("`@user1` and `@user2` and `@user3`");
    });

    it("should decode mixed HTML entities and normal @mentions", () => {
      const result = sanitizeContent("&commat;user1 and @user2");
      expect(result).toBe("`@user1` and `@user2`");
    });

    it("should decode HTML entities in org/team mentions", () => {
      const result = sanitizeContent("&commat;myorg/myteam should review");
      expect(result).toBe("`@myorg/myteam` should review");
    });

    it("should decode general decimal entities correctly", () => {
      const result = sanitizeContent("&#72;&#101;&#108;&#108;&#111;"); // "Hello"
      expect(result).toBe("Hello");
    });

    it("should decode general hex entities correctly", () => {
      const result = sanitizeContent("&#x48;&#x65;&#x6C;&#x6C;&#x6F;"); // "Hello"
      expect(result).toBe("Hello");
    });

    it("should decode double-encoded general entities correctly", () => {
      const result = sanitizeContent("&amp;#72;ello"); // "&Hello"
      expect(result).toBe("Hello");
    });

    it("should handle invalid code points gracefully", () => {
      const result = sanitizeContent("Invalid &#999999999; entity");
      expect(result).toBe("Invalid &#999999999; entity"); // Keep original if invalid
    });

    it("should handle malformed HTML entities without crashing", () => {
      const result = sanitizeContent("Malformed &# or &#x entity");
      expect(result).toBe("Malformed &# or &#x entity");
    });

    it("should decode entities before Unicode hardening", () => {
      // Ensure entity decoding happens as part of hardenUnicodeText
      const result = sanitizeContent("&#xFF01;"); // Full-width exclamation (U+FF01)
      expect(result).toBe("!"); // Should become ASCII !
    });

    it("should decode entities in combination with other sanitization", () => {
      const result = sanitizeContent("&commat;user <!-- comment --> text");
      expect(result).toBe("`@user`  text");
    });

    it("should decode entities even in backticks (security-first approach)", () => {
      // Entities are decoded during Unicode hardening, which happens before
      // mention neutralization. This is intentional - we decode entities early
      // to prevent bypasses, then the @mention gets neutralized properly.
      const result = sanitizeContent("`&commat;user`");
      expect(result).toBe("`@user`");
    });

    it("should preserve legitimate URLs after entity decoding", () => {
      const result = sanitizeContent("Visit https://github.com/user");
      expect(result).toBe("Visit https://github.com/user");
    });

    it("should decode case-insensitive named entities", () => {
      const result = sanitizeContent("&COMMAT;user and &CoMmAt;user2");
      expect(result).toBe("`@user` and `@user2`");
    });

    it("should decode entities with mixed case hex digits", () => {
      const result = sanitizeContent("&#x4O; is invalid but &#x4A; is valid"); // Note: using letter 'O' not digit '0'
      expect(result).toContain("&#x4O;"); // Invalid should remain
      expect(result).toContain("J"); // Valid 0x4A = J
    });

    it("should handle zero code point", () => {
      const result = sanitizeContent("&#0;text");
      // Code point 0 is valid but typically removed as control character
      expect(result).toContain("text");
    });

    it("should respect allowed aliases even with HTML-encoded mentions", () => {
      const result = sanitizeContent("&commat;author is allowed", { allowedAliases: ["author"] });
      expect(result).toBe("@author is allowed");
    });
  });
});
