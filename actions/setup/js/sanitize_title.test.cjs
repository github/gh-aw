// @ts-check
import { describe, it, expect } from "vitest";

const { sanitizeTitle, applyTitlePrefix } = require("./sanitize_title.cjs");

describe("sanitize_title", () => {
  describe("sanitizeTitle", () => {
    describe("basic sanitization", () => {
      it("should return empty string for null/undefined", () => {
        expect(sanitizeTitle(null)).toBe("");
        expect(sanitizeTitle(undefined)).toBe("");
        expect(sanitizeTitle("")).toBe("");
      });

      it("should trim whitespace", () => {
        expect(sanitizeTitle("  Test Title  ")).toBe("Test Title");
        expect(sanitizeTitle("\n\tTest Title\n\t")).toBe("Test Title");
      });

      it("should handle normal ASCII titles", () => {
        expect(sanitizeTitle("Bug Report")).toBe("Bug Report");
        expect(sanitizeTitle("Feature Request: Add new feature")).toBe("Feature Request: Add new feature");
      });
    });

    describe("Unicode security hardening", () => {
      it("should apply NFC normalization", () => {
        // Composed vs decomposed é (U+00E9 vs U+0065 U+0301)
        const composed = "café";
        const decomposed = "café"; // Using combining character
        expect(sanitizeTitle(composed)).toBe(sanitizeTitle(decomposed));
      });

      it("should strip zero-width characters", () => {
        expect(sanitizeTitle("Test\u200BTitle")).toBe("TestTitle");
        expect(sanitizeTitle("Test\u200CTitle")).toBe("TestTitle");
        expect(sanitizeTitle("Test\u200DTitle")).toBe("TestTitle");
        expect(sanitizeTitle("Test\u2060Title")).toBe("TestTitle");
        expect(sanitizeTitle("Test\uFEFFTitle")).toBe("TestTitle");
      });

      it("should remove bidirectional override controls", () => {
        expect(sanitizeTitle("Test\u202ATitle")).toBe("TestTitle");
        expect(sanitizeTitle("Test\u202BTitle")).toBe("TestTitle");
        expect(sanitizeTitle("Test\u202CTitle")).toBe("TestTitle");
        expect(sanitizeTitle("Test\u202DTitle")).toBe("TestTitle");
        expect(sanitizeTitle("Test\u202ETitle")).toBe("TestTitle");
        expect(sanitizeTitle("Test\u2066Title")).toBe("TestTitle");
        expect(sanitizeTitle("Test\u2067Title")).toBe("TestTitle");
        expect(sanitizeTitle("Test\u2068Title")).toBe("TestTitle");
        expect(sanitizeTitle("Test\u2069Title")).toBe("TestTitle");
      });

      it("should convert fullwidth ASCII to standard ASCII", () => {
        // Fullwidth brackets
        expect(sanitizeTitle("［Test］")).toBe("[Test]");
        // Fullwidth letters and numbers (note: fullwidth space U+3000 is not converted by hardenUnicodeText)
        expect(sanitizeTitle("Ｔｅｓｔ　１２３")).toBe("Test　123");
        // Mix of fullwidth and normal
        expect(sanitizeTitle("Test［Ａｇｅｎｔ］")).toBe("Test[Agent]");
      });

      it("should handle complex Unicode attacks", () => {
        // Combining zero-width spaces with bidirectional overrides
        const malicious = "Test\u200B\u202ETi\u200Ctle\u202C\uFEFF";
        expect(sanitizeTitle(malicious)).toBe("TestTitle");
      });
    });

    describe("duplicate prefix removal", () => {
      it("should remove exact prefix match", () => {
        expect(sanitizeTitle("[Agent] Fix bug", "[Agent] ")).toBe("Fix bug");
        expect(sanitizeTitle("🤖 Fix bug", "🤖 ")).toBe("Fix bug");
        expect(sanitizeTitle("[WIP] Update docs", "[WIP] ")).toBe("Update docs");
      });

      it("should remove prefix with colon separator", () => {
        expect(sanitizeTitle("[Agent]: Fix bug", "[Agent] ")).toBe("Fix bug");
        expect(sanitizeTitle("Agent: Fix bug", "Agent ")).toBe("Fix bug");
      });

      it("should remove prefix with dash separator", () => {
        expect(sanitizeTitle("[Agent] - Fix bug", "[Agent] ")).toBe("Fix bug");
        expect(sanitizeTitle("Agent - Fix bug", "Agent ")).toBe("Fix bug");
      });

      it("should remove prefix with pipe separator", () => {
        expect(sanitizeTitle("[Agent] | Fix bug", "[Agent] ")).toBe("Fix bug");
        expect(sanitizeTitle("Agent | Fix bug", "Agent ")).toBe("Fix bug");
      });

      it("should not remove prefix from middle of title", () => {
        expect(sanitizeTitle("Fix [Agent] bug", "[Agent] ")).toBe("Fix [Agent] bug");
        expect(sanitizeTitle("Update Agent feature", "Agent ")).toBe("Update Agent feature");
      });

      it("should handle case-sensitive prefix matching", () => {
        expect(sanitizeTitle("[AGENT] Fix bug", "[Agent] ")).toBe("[AGENT] Fix bug");
        expect(sanitizeTitle("[agent] Fix bug", "[Agent] ")).toBe("[agent] Fix bug");
      });

      it("should handle empty or whitespace-only prefix", () => {
        expect(sanitizeTitle("Test Title", "")).toBe("Test Title");
        expect(sanitizeTitle("Test Title", "   ")).toBe("Test Title");
      });

      it("should prevent double prefix from agent confusion", () => {
        // Agent might generate "[Agent] [Agent] Title" if confused
        // sanitizeTitle removes all instances of the prefix, so both get removed
        expect(sanitizeTitle("[Agent] [Agent] Fix bug", "[Agent] ")).toBe("Fix bug");
      });

      it("should handle Unicode in prefixes", () => {
        // sanitizeTitle removes all instances of the prefix
        expect(sanitizeTitle("🤖 🤖 Fix bug", "🤖 ")).toBe("Fix bug");
        // After hardening, fullwidth brackets become regular brackets, then get removed as prefix
        expect(sanitizeTitle("［Agent］Fix bug", "[Agent] ")).toBe("Fix bug");
      });
    });

    describe("combined sanitization and prefix removal", () => {
      it("should apply Unicode hardening before prefix removal", () => {
        const title = "［Agent］\u200BFix\u202Ebug\u202C";
        // After hardening: "[Agent]Fixbug", then "[Agent]" prefix removed (no space), leaving "Fixbug"
        expect(sanitizeTitle(title, "[Agent] ")).toBe("Fixbug");
      });

      it("should handle malicious prefix injection attempts", () => {
        // Attacker tries to inject prefix with invisible characters
        // After hardening: "[Agent] [Agent] Fix bug", then both prefixes get removed
        const title = "[Agent]\u200B\u202A [Agent]\u202C Fix bug";
        expect(sanitizeTitle(title, "[Agent] ")).toBe("Fix bug");
      });

      it("should preserve legitimate content after sanitization", () => {
        const title = "[Agent] Fix bug #123: Update configuration";
        expect(sanitizeTitle(title, "[Agent] ")).toBe("Fix bug #123: Update configuration");
      });
    });
  });

  describe("applyTitlePrefix", () => {
    it("should add prefix to clean title", () => {
      expect(applyTitlePrefix("Fix bug", "[Agent] ")).toBe("[Agent] Fix bug");
      expect(applyTitlePrefix("Update docs", "🤖 ")).toBe("🤖 Update docs");
    });

    it("should not duplicate prefix if already present", () => {
      expect(applyTitlePrefix("[Agent] Fix bug", "[Agent] ")).toBe("[Agent] Fix bug");
      expect(applyTitlePrefix("🤖 Fix bug", "🤖 ")).toBe("🤖 Fix bug");
    });

    it("should handle empty prefix", () => {
      expect(applyTitlePrefix("Fix bug", "")).toBe("Fix bug");
      expect(applyTitlePrefix("Fix bug", "   ")).toBe("Fix bug");
    });

    it("should trim inputs", () => {
      // applyTitlePrefix should use titlePrefix as-is, but the title is trimmed
      expect(applyTitlePrefix("  Fix bug  ", "  [Agent]  ")).toBe("  [Agent]  Fix bug");
    });

    it("should handle empty title", () => {
      expect(applyTitlePrefix("", "[Agent] ")).toBe("");
      expect(applyTitlePrefix("   ", "[Agent] ")).toBe("");
    });
  });

  describe("integration scenarios", () => {
    it("should handle typical workflow: sanitize then apply prefix", () => {
      const rawTitle = "［Agent］\u200BFix\u202Ebug #123\u202C";
      const sanitized = sanitizeTitle(rawTitle, "[Agent] ");
      const final = applyTitlePrefix(sanitized, "[Agent] ");
      // After sanitization, prefix is removed, so we need to apply it again
      expect(final).toBe("[Agent] Fixbug #123");
    });

    it("should prevent agent-generated duplicate prefixes", () => {
      // Agent generates title with prefix already included
      const agentTitle = "[Agent] Update configuration";
      const sanitized = sanitizeTitle(agentTitle, "[Agent] ");
      const final = applyTitlePrefix(sanitized, "[Agent] ");
      // Prefix gets removed during sanitization, then re-applied
      expect(final).toBe("[Agent] Update configuration");
    });

    it("should handle fullwidth brackets in agent output", () => {
      // Agent uses fullwidth brackets (common in some locales)
      const agentTitle = "［Agent］Fix critical bug";
      const sanitized = sanitizeTitle(agentTitle, "[Agent] ");
      const final = applyTitlePrefix(sanitized, "[Agent] ");
      // Fullwidth brackets get converted to ASCII "[Agent]Fix", then "[Agent]" removed leaving "Fix critical bug"
      expect(final).toBe("[Agent] Fix critical bug");
    });
  });
});
