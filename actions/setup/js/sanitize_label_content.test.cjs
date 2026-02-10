import { describe, it, expect } from "vitest";
const { sanitizeLabelContent } = require("./sanitize_label_content.cjs");
describe("sanitize_label_content.cjs", () => {
  describe("sanitizeLabelContent", () => {
    (it("should return empty string for null input", () => {
      expect(sanitizeLabelContent(null)).toBe("");
    }),
      it("should return empty string for undefined input", () => {
        expect(sanitizeLabelContent(void 0)).toBe("");
      }),
      it("should return empty string for non-string input", () => {
        (expect(sanitizeLabelContent(123)).toBe(""), expect(sanitizeLabelContent({})).toBe(""), expect(sanitizeLabelContent([])).toBe(""));
      }),
      it("should trim whitespace from input", () => {
        (expect(sanitizeLabelContent("  test  ")).toBe("test"), expect(sanitizeLabelContent("\n\ttest\n\t")).toBe("test"));
      }),
      it("should remove control characters", () => {
        expect(sanitizeLabelContent("test\0\blabel")).toBe("testlabel");
      }),
      it("should remove DEL character (0x7F)", () => {
        expect(sanitizeLabelContent("testlabel")).toBe("testlabel");
      }),
      it("should preserve newline character", () => {
        expect(sanitizeLabelContent("test\nlabel")).toBe("test\nlabel");
      }),
      it("should remove ANSI escape codes", () => {
        expect(sanitizeLabelContent("[31mred text[0m")).toBe("red text");
      }),
      it("should remove various ANSI codes", () => {
        expect(sanitizeLabelContent("[1;32mBold Green[0m[4mUnderline[0m")).toBe("Bold GreenUnderline");
      }),
      it("should neutralize @mentions by wrapping in backticks", () => {
        (expect(sanitizeLabelContent("Hello @user")).toBe("Hello `@user`"), expect(sanitizeLabelContent("@user said something")).toBe("`@user` said something"));
      }),
      it("should neutralize @org/team mentions", () => {
        expect(sanitizeLabelContent("Hello @myorg/myteam")).toBe("Hello `@myorg/myteam`");
      }),
      it("should not neutralize @mentions already in backticks", () => {
        expect(sanitizeLabelContent("Already `@user` handled")).toBe("Already `@user` handled");
      }),
      it("should neutralize multiple @mentions", () => {
        expect(sanitizeLabelContent("@user1 and @user2 are here")).toBe("`@user1` and `@user2` are here");
      }),
      it("should remove HTML special characters", () => {
        expect(sanitizeLabelContent("test<>&'\"label")).toBe("testlabel");
      }),
      it("should remove less-than signs", () => {
        expect(sanitizeLabelContent("a < b")).toBe("a  b");
      }),
      it("should remove greater-than signs", () => {
        expect(sanitizeLabelContent("a > b")).toBe("a  b");
      }),
      it("should remove ampersands", () => {
        expect(sanitizeLabelContent("test & label")).toBe("test  label");
      }),
      it("should remove single and double quotes", () => {
        expect(sanitizeLabelContent('test\'s "label"')).toBe("tests label");
      }),
      it("should handle complex input with multiple sanitizations", () => {
        expect(sanitizeLabelContent("  @user [31mred[0m <tag> test&label  ")).toBe("`@user` red tag testlabel");
      }),
      it("should handle empty string input", () => {
        expect(sanitizeLabelContent("")).toBe("");
      }),
      it("should handle whitespace-only input", () => {
        expect(sanitizeLabelContent("   \n\t  ")).toBe("");
      }),
      it("should preserve normal alphanumeric characters", () => {
        (expect(sanitizeLabelContent("bug123")).toBe("bug123"), expect(sanitizeLabelContent("feature-request")).toBe("feature-request"));
      }),
      it("should preserve hyphens and underscores", () => {
        expect(sanitizeLabelContent("test-label_123")).toBe("test-label_123");
      }),
      it("should handle consecutive control characters", () => {
        expect(sanitizeLabelContent("test\0label")).toBe("testlabel");
      }),
      it("should handle @mentions at various positions", () => {
        (expect(sanitizeLabelContent("start @user end")).toBe("start `@user` end"), expect(sanitizeLabelContent("@user at start")).toBe("`@user` at start"), expect(sanitizeLabelContent("at end @user")).toBe("at end `@user`"));
      }),
      it("should not treat email-like patterns as @mentions after alphanumerics", () => {
        expect(sanitizeLabelContent("email@example.com")).toBe("email@example.com");
      }),
      it("should handle username edge cases", () => {
        (expect(sanitizeLabelContent("@a")).toBe("`@a`"), expect(sanitizeLabelContent("@user-name-123")).toBe("`@user-name-123`"));
      }),
      it("should combine all sanitization rules correctly", () => {
        expect(sanitizeLabelContent('  [31m@user[0m says <hello> & "goodbye"  ')).toBe("`@user` says hello  goodbye");
      }));
  });

  describe("Unicode hardening for labels", () => {
    it("should remove zero-width characters", () => {
      expect(sanitizeLabelContent("bug\u200Blabel")).toBe("buglabel");
      expect(sanitizeLabelContent("test\u200C\u200D\u2060label")).toBe("testlabel");
    });

    it("should convert full-width ASCII to normal ASCII", () => {
      expect(sanitizeLabelContent("\uFF21\uFF22\uFF23")).toBe("ABC");
      expect(sanitizeLabelContent("bug\uFF01")).toBe("bug!");
    });

    it("should remove directional override characters", () => {
      expect(sanitizeLabelContent("label\u202Etest")).toBe("labeltest");
      expect(sanitizeLabelContent("bug\u202A\u202B\u202Cfix")).toBe("bugfix");
    });

    it("should normalize Unicode characters (NFC)", () => {
      const labelWithCombining = "cafe\u0301"; // café with combining accent
      const result = sanitizeLabelContent(labelWithCombining);
      expect(result).toBe("café");
      expect(result.charCodeAt(3)).toBe(0x00e9); // Precomposed é
    });

    it("should handle combination of Unicode attacks in labels", () => {
      const maliciousLabel = "\uFF42\u200Bug\u202E\uFEFF";
      expect(sanitizeLabelContent(maliciousLabel)).toBe("bug");
    });

    it("should preserve emoji in labels", () => {
      expect(sanitizeLabelContent("🐛 bug")).toBe("🐛 bug");
      expect(sanitizeLabelContent("✨ enhancement")).toBe("✨ enhancement");
    });
  });
});
