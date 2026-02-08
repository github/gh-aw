import { describe, it, expect, beforeEach, afterEach } from "vitest";
import fs from "fs";
import path from "path";
import os from "os";

describe("firewall_blocked_domains.cjs", () => {
  let parseFirewallLogLine;
  let isRequestBlocked;
  let extractAndSanitizeDomain;
  let getBlockedDomains;
  let generateBlockedDomainsSection;
  let testDir;

  beforeEach(async () => {
    // Create a temporary directory for test files
    testDir = path.join(os.tmpdir(), `gh-aw-test-firewall-${Date.now()}`);
    fs.mkdirSync(testDir, { recursive: true });

    // Dynamic import to get fresh module state
    const module = await import("./firewall_blocked_domains.cjs");
    parseFirewallLogLine = module.parseFirewallLogLine;
    isRequestBlocked = module.isRequestBlocked;
    extractAndSanitizeDomain = module.extractAndSanitizeDomain;
    getBlockedDomains = module.getBlockedDomains;
    generateBlockedDomainsSection = module.generateBlockedDomainsSection;
  });

  afterEach(() => {
    // Clean up test directory
    if (testDir && fs.existsSync(testDir)) {
      fs.rmSync(testDir, { recursive: true, force: true });
    }
  });

  describe("parseFirewallLogLine", () => {
    it("should parse valid firewall log line with blocked request", () => {
      const line = '1761332530.474 172.30.0.20:35288 blocked.example.com:443 140.82.112.22:443 1.1 CONNECT 403 NONE_NONE:HIER_NONE blocked.example.com:443 "-"';
      const result = parseFirewallLogLine(line);

      expect(result).not.toBeNull();
      expect(result.timestamp).toBe("1761332530.474");
      expect(result.domain).toBe("blocked.example.com:443");
      expect(result.status).toBe("403");
      expect(result.decision).toBe("NONE_NONE:HIER_NONE");
    });

    it("should parse valid firewall log line with allowed request", () => {
      const line = '1761332530.474 172.30.0.20:35288 api.github.com:443 140.82.112.22:443 1.1 CONNECT 200 TCP_TUNNEL:HIER_DIRECT api.github.com:443 "-"';
      const result = parseFirewallLogLine(line);

      expect(result).not.toBeNull();
      expect(result.timestamp).toBe("1761332530.474");
      expect(result.domain).toBe("api.github.com:443");
      expect(result.status).toBe("200");
      expect(result.decision).toBe("TCP_TUNNEL:HIER_DIRECT");
    });

    it("should return null for empty line", () => {
      expect(parseFirewallLogLine("")).toBeNull();
      expect(parseFirewallLogLine("   ")).toBeNull();
    });

    it("should return null for comment line", () => {
      expect(parseFirewallLogLine("# This is a comment")).toBeNull();
    });

    it("should return null for invalid timestamp", () => {
      const line = 'invalid 172.30.0.20:35288 blocked.example.com:443 140.82.112.22:443 1.1 CONNECT 403 NONE_NONE:HIER_NONE blocked.example.com:443 "-"';
      expect(parseFirewallLogLine(line)).toBeNull();
    });

    it("should return null for lines with fewer than 10 fields", () => {
      expect(parseFirewallLogLine("1761332530.474 172.30.0.20:35288 blocked.example.com:443")).toBeNull();
    });
  });

  describe("isRequestBlocked", () => {
    it("should identify blocked request with 403 status", () => {
      expect(isRequestBlocked("NONE_NONE:HIER_NONE", "403")).toBe(true);
    });

    it("should identify blocked request with 407 status", () => {
      expect(isRequestBlocked("NONE_NONE:HIER_NONE", "407")).toBe(true);
    });

    it("should identify blocked request with NONE_NONE decision", () => {
      expect(isRequestBlocked("NONE_NONE:HIER_NONE", "0")).toBe(true);
    });

    it("should identify blocked request with TCP_DENIED decision", () => {
      expect(isRequestBlocked("TCP_DENIED:HIER_NONE", "0")).toBe(true);
    });

    it("should identify allowed request with 200 status", () => {
      expect(isRequestBlocked("TCP_TUNNEL:HIER_DIRECT", "200")).toBe(false);
    });

    it("should identify allowed request with TCP_TUNNEL decision", () => {
      expect(isRequestBlocked("TCP_TUNNEL:HIER_DIRECT", "200")).toBe(false);
    });

    it("should identify allowed request with TCP_HIT decision", () => {
      expect(isRequestBlocked("TCP_HIT:HIER_DIRECT", "200")).toBe(false);
    });

    it("should default to blocked for ambiguous requests", () => {
      expect(isRequestBlocked("UNKNOWN:UNKNOWN", "999")).toBe(true);
    });
  });

  describe("extractAndSanitizeDomain", () => {
    it("should extract and sanitize domain from domain:port format", () => {
      expect(extractAndSanitizeDomain("example.com:443")).toBe("example.com");
      expect(extractAndSanitizeDomain("api.github.com:443")).toBe("api.github.com");
      expect(extractAndSanitizeDomain("sub.domain.example.com:8080")).toBe("sub.domain.example...");
    });

    it("should handle placeholder domain", () => {
      expect(extractAndSanitizeDomain("-")).toBe("");
    });

    it("should handle empty or null input", () => {
      expect(extractAndSanitizeDomain("")).toBe("");
      expect(extractAndSanitizeDomain(null)).toBe("");
    });

    it("should sanitize special characters in domain", () => {
      expect(extractAndSanitizeDomain("ex@mple.com:443")).toBe("exmple.com");
      expect(extractAndSanitizeDomain("test_site.com:443")).toBe("testsite.com");
    });

    it("should handle domain without port", () => {
      expect(extractAndSanitizeDomain("example.com")).toBe("example.com");
    });
  });

  describe("getBlockedDomains", () => {
    it("should return empty array when logs directory does not exist", () => {
      const nonExistentDir = path.join(testDir, "nonexistent");
      const result = getBlockedDomains(nonExistentDir);

      expect(result).toEqual([]);
    });

    it("should return empty array when no log files exist", () => {
      const emptyDir = path.join(testDir, "empty");
      fs.mkdirSync(emptyDir, { recursive: true });

      const result = getBlockedDomains(emptyDir);

      expect(result).toEqual([]);
    });

    it("should extract blocked domains from single log file", () => {
      const logsDir = path.join(testDir, "logs1");
      fs.mkdirSync(logsDir, { recursive: true });

      const logContent = [
        '1761332530.474 172.30.0.20:35288 blocked1.example.com:443 140.82.112.22:443 1.1 CONNECT 403 NONE_NONE:HIER_NONE blocked1.example.com:443 "-"',
        '1761332530.475 172.30.0.20:35289 api.github.com:443 140.82.112.22:443 1.1 CONNECT 200 TCP_TUNNEL:HIER_DIRECT api.github.com:443 "-"',
        '1761332530.476 172.30.0.20:35290 blocked2.example.com:443 140.82.112.22:443 1.1 CONNECT 403 NONE_NONE:HIER_NONE blocked2.example.com:443 "-"',
      ].join("\n");

      fs.writeFileSync(path.join(logsDir, "access.log"), logContent);

      const result = getBlockedDomains(logsDir);

      expect(result).toEqual(["blocked1.example.com", "blocked2.example.com"]);
    });

    it("should deduplicate blocked domains", () => {
      const logsDir = path.join(testDir, "logs2");
      fs.mkdirSync(logsDir, { recursive: true });

      const logContent = [
        '1761332530.474 172.30.0.20:35288 blocked.example.com:443 140.82.112.22:443 1.1 CONNECT 403 NONE_NONE:HIER_NONE blocked.example.com:443 "-"',
        '1761332530.475 172.30.0.20:35289 blocked.example.com:443 140.82.112.22:443 1.1 CONNECT 403 NONE_NONE:HIER_NONE blocked.example.com:443 "-"',
        '1761332530.476 172.30.0.20:35290 blocked.example.com:443 140.82.112.22:443 1.1 CONNECT 403 NONE_NONE:HIER_NONE blocked.example.com:443 "-"',
      ].join("\n");

      fs.writeFileSync(path.join(logsDir, "access.log"), logContent);

      const result = getBlockedDomains(logsDir);

      expect(result).toEqual(["blocked.example.com"]);
    });

    it("should aggregate blocked domains from multiple log files", () => {
      const logsDir = path.join(testDir, "logs3");
      fs.mkdirSync(logsDir, { recursive: true });

      const log1Content = '1761332530.474 172.30.0.20:35288 blocked1.example.com:443 140.82.112.22:443 1.1 CONNECT 403 NONE_NONE:HIER_NONE blocked1.example.com:443 "-"';
      const log2Content = '1761332530.475 172.30.0.20:35289 blocked2.example.com:443 140.82.112.22:443 1.1 CONNECT 407 TCP_DENIED:HIER_NONE blocked2.example.com:443 "-"';

      fs.writeFileSync(path.join(logsDir, "access1.log"), log1Content);
      fs.writeFileSync(path.join(logsDir, "access2.log"), log2Content);

      const result = getBlockedDomains(logsDir);

      expect(result).toEqual(["blocked1.example.com", "blocked2.example.com"]);
    });

    it("should sort blocked domains alphabetically", () => {
      const logsDir = path.join(testDir, "logs4");
      fs.mkdirSync(logsDir, { recursive: true });

      const logContent = [
        '1761332530.474 172.30.0.20:35288 zebra.example.com:443 140.82.112.22:443 1.1 CONNECT 403 NONE_NONE:HIER_NONE zebra.example.com:443 "-"',
        '1761332530.475 172.30.0.20:35289 alpha.example.com:443 140.82.112.22:443 1.1 CONNECT 403 NONE_NONE:HIER_NONE alpha.example.com:443 "-"',
        '1761332530.476 172.30.0.20:35290 mike.example.com:443 140.82.112.22:443 1.1 CONNECT 403 NONE_NONE:HIER_NONE mike.example.com:443 "-"',
      ].join("\n");

      fs.writeFileSync(path.join(logsDir, "access.log"), logContent);

      const result = getBlockedDomains(logsDir);

      expect(result).toEqual(["alpha.example.com", "mike.example.com", "zebra.example.com"]);
    });

    it("should filter out placeholder domains", () => {
      const logsDir = path.join(testDir, "logs5");
      fs.mkdirSync(logsDir, { recursive: true });

      const logContent = [
        '1761332530.474 172.30.0.20:35288 - 140.82.112.22:443 1.1 CONNECT 403 NONE_NONE:HIER_NONE - "-"',
        '1761332530.475 172.30.0.20:35289 blocked.example.com:443 140.82.112.22:443 1.1 CONNECT 403 NONE_NONE:HIER_NONE blocked.example.com:443 "-"',
      ].join("\n");

      fs.writeFileSync(path.join(logsDir, "access.log"), logContent);

      const result = getBlockedDomains(logsDir);

      expect(result).toEqual(["blocked.example.com"]);
    });

    it("should handle invalid log lines gracefully", () => {
      const logsDir = path.join(testDir, "logs6");
      fs.mkdirSync(logsDir, { recursive: true });

      const logContent = ["# Comment line", "invalid line", '1761332530.474 172.30.0.20:35288 blocked.example.com:443 140.82.112.22:443 1.1 CONNECT 403 NONE_NONE:HIER_NONE blocked.example.com:443 "-"', "", "short"].join("\n");

      fs.writeFileSync(path.join(logsDir, "access.log"), logContent);

      const result = getBlockedDomains(logsDir);

      expect(result).toEqual(["blocked.example.com"]);
    });
  });

  describe("generateBlockedDomainsSection", () => {
    it("should return empty string when no blocked domains", () => {
      expect(generateBlockedDomainsSection([])).toBe("");
      expect(generateBlockedDomainsSection(null)).toBe("");
      expect(generateBlockedDomainsSection(undefined)).toBe("");
    });

    it("should generate details section for single blocked domain", () => {
      const result = generateBlockedDomainsSection(["blocked.example.com"]);

      expect(result).toContain("> [!WARNING]");
      expect(result).toContain("> <details>");
      expect(result).toContain("> </details>");
      expect(result).toContain("> <summary>⚠️ Firewall blocked 1 domain</summary>");
      expect(result).toContain("> - `blocked.example.com`");
      expect(result).toContain("> The following domain was blocked by the firewall during workflow execution:");
    });

    it("should generate details section for multiple blocked domains", () => {
      const domains = ["alpha.example.com", "beta.example.com", "gamma.example.com"];
      const result = generateBlockedDomainsSection(domains);

      expect(result).toContain("> [!WARNING]");
      expect(result).toContain("> <details>");
      expect(result).toContain("> </details>");
      expect(result).toContain("> <summary>⚠️ Firewall blocked 3 domains</summary>");
      expect(result).toContain("> - `alpha.example.com`");
      expect(result).toContain("> - `beta.example.com`");
      expect(result).toContain("> - `gamma.example.com`");
    });

    it("should use correct singular/plural form", () => {
      const singleResult = generateBlockedDomainsSection(["single.com"]);
      expect(singleResult).toContain("1 domain");
      expect(singleResult).toContain("domain was blocked");

      const multiResult = generateBlockedDomainsSection(["one.com", "two.com"]);
      expect(multiResult).toContain("2 domains");
      expect(multiResult).toContain("domains were blocked");
    });

    it("should format domains with backticks", () => {
      const result = generateBlockedDomainsSection(["example.com"]);
      expect(result).toMatch(/> - `example\.com`/);
    });

    it("should start with double newline and warning alert", () => {
      const result = generateBlockedDomainsSection(["example.com"]);
      expect(result).toMatch(/^\n\n> \[!WARNING\]/);
    });
  });
});
