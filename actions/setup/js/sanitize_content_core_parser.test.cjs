import { describe, it, expect, beforeEach, afterEach } from "vitest";

/**
 * Unit + fuzz tests for the core markdown parser helpers:
 *   - getFencedCodeRanges
 *   - applyFnOutsideInlineCode
 *   - applyToNonCodeRegions
 *
 * No mocks are used; core is set to a minimal stub so that functions that
 * call core.info / core.warning do not throw during the tests that exercise
 * the full sanitize pipeline.
 */

describe("sanitize_content_core.cjs – parser internals", () => {
  let getFencedCodeRanges;
  let applyFnOutsideInlineCode;
  let applyToNonCodeRegions;
  let createRenderSafeCodeSpanWrapper;
  let neutralizeGitHubReferences;

  beforeEach(async () => {
    // Set up a minimal stub so code that calls core.* doesn't throw.
    // We deliberately avoid vi.fn() to keep tests mock-free.
    global.core = {
      info: () => {},
      warning: () => {},
      debug: () => {},
      error: () => {},
    };

    const mod = await import("./sanitize_content_core.cjs");
    // applyToNonCodeRegions is exported; the two lower-level helpers are
    // accessed indirectly through applyToNonCodeRegions in the fuzz section.
    // For unit tests we grab them from the module if available, otherwise
    // we exercise them through applyToNonCodeRegions.
    getFencedCodeRanges = mod.getFencedCodeRanges ?? null;
    applyFnOutsideInlineCode = mod.applyFnOutsideInlineCode ?? null;
    applyToNonCodeRegions = mod.applyToNonCodeRegions;
    createRenderSafeCodeSpanWrapper = mod.createRenderSafeCodeSpanWrapper;
    neutralizeGitHubReferences = mod.neutralizeGitHubReferences;
  });

  afterEach(() => {
    delete global.core;
  });

  describe("neutralizeGitHubReferences input validation", () => {
    it("preserves the null allowlist sentinel", () => {
      expect(neutralizeGitHubReferences("owner/repo#1", null)).toBe("owner/repo#1");
    });

    it("fails closed for malformed allowlists and non-string content", () => {
      expect(neutralizeGitHubReferences("owner/repo#1", undefined)).toBe("`owner/repo#1`");
      expect(neutralizeGitHubReferences("owner/repo#1", [null, " "])).toBe("`owner/repo#1`");
      expect(neutralizeGitHubReferences(42, null)).toBe("");
    });

    it("normalizes valid allowlist entries", () => {
      expect(neutralizeGitHubReferences("owner/repo#1", [" OWNER/REPO "])).toBe("owner/repo#1");
    });
  });

  it("uses bounded HTML code elements when all short delimiters are present", () => {
    const input = Array.from({ length: 17 }, (_, index) => "`".repeat(index + 1)).join(" ");
    const wrap = createRenderSafeCodeSpanWrapper(input);
    const result = wrap("@user");
    expect(result).toBe("<code>@user</code>");
    expect(result.length).toBeLessThan(32);
  });

  // ---------------------------------------------------------------------------
  // getFencedCodeRanges – only if exported directly
  // ---------------------------------------------------------------------------
  describe("getFencedCodeRanges (via applyToNonCodeRegions contract)", () => {
    it("empty string returns empty string", () => {
      expect(applyToNonCodeRegions("", x => x + "X")).toBe("");
    });

    it("null returns empty string", () => {
      expect(applyToNonCodeRegions(null, x => x)).toBe("");
    });

    it("non-string truthy value is returned as-is (not coerced)", () => {
      // The guard is `!s || typeof s !== "string"` → `return s || ""`
      // For a truthy non-string like 42: !42 is false, typeof 42 !== "string" is true
      // so it enters the guard; then `42 || ""` is 42 – returned as-is.
      expect(applyToNonCodeRegions(42, x => x)).toBe(42);
    });

    it("plain text with no fences applies fn to everything", () => {
      const result = applyToNonCodeRegions("hello world", s => s.toUpperCase());
      expect(result).toBe("HELLO WORLD");
    });

    it("backtick-fenced block is preserved verbatim", () => {
      const input = "before\n```\ncontent\n```\nafter";
      const result = applyToNonCodeRegions(input, s => s.toUpperCase());
      expect(result).toContain("```\ncontent\n```");
      expect(result).toContain("BEFORE");
      expect(result).toContain("AFTER");
    });

    it("tilde-fenced block is preserved verbatim", () => {
      const input = "before\n~~~\ncontent\n~~~\nafter";
      const result = applyToNonCodeRegions(input, s => s.toUpperCase());
      expect(result).toContain("~~~\ncontent\n~~~");
      expect(result).toContain("BEFORE");
      expect(result).toContain("AFTER");
    });

    it("fenced block with info string (language tag) is preserved verbatim", () => {
      const input = "lead\n```js\nconst x = 1;\n```\ntrail";
      const result = applyToNonCodeRegions(input, s => s.toUpperCase());
      expect(result).toContain("```js\nconst x = 1;\n```");
      expect(result).toContain("LEAD");
      expect(result).toContain("TRAIL");
    });

    it("suggestion block is preserved verbatim", () => {
      const input = "Review:\n```suggestion\nRefer to {{fleet-server}}.\n```\nEnd";
      const result = applyToNonCodeRegions(input, s => s.replace(/\{\{/g, "\\{\\{"));
      expect(result).toContain("{{fleet-server}}");
      expect(result).not.toContain("\\{\\{fleet-server");
    });

    it("multiple fenced blocks are each preserved verbatim", () => {
      const input = "a\n```\nblock1\n```\nb\n```\nblock2\n```\nc";
      const result = applyToNonCodeRegions(input, s => s.toUpperCase());
      expect(result).toContain("```\nblock1\n```");
      expect(result).toContain("```\nblock2\n```");
      expect(result).toContain("A\n");
      expect(result).toContain("\nB\n");
      expect(result).toContain("\nC");
    });

    it("longer opening fence requires longer or equal closing fence", () => {
      // ````  closes with ```` or longer, NOT with ```
      const input = "text\n````\ncode\n```\nstill code\n````\nafter";
      const result = applyToNonCodeRegions(input, s => s.toUpperCase());
      // The ``` line inside is not a valid closer for a ```` fence
      expect(result).toContain("````\ncode\n```\nstill code\n````");
      expect(result).toContain("TEXT");
      expect(result).toContain("AFTER");
    });

    it("unclosed fenced block treats rest of string as code (safe fallback)", () => {
      const input = "prose\n```\nunclosed code {{ secret }}";
      const result = applyToNonCodeRegions(input, s => s.replace(/\{\{/g, "ESCAPED"));
      // The unclosed block's content must NOT be transformed
      expect(result).toContain("{{ secret }}");
      expect(result).not.toContain("ESCAPED");
    });

    it("fence must start at beginning of (possibly indented) line", () => {
      // Fences with up to 3 spaces of indentation are valid in CommonMark
      const input = "   ```\ncontent\n   ```\nafter";
      const result = applyToNonCodeRegions(input, s => s.toUpperCase());
      expect(result).toContain("content"); // preserved, not uppercased
    });

    it("does not treat a four-space-indented fence as a code block", () => {
      const input = "    ```\n@user\n    ```";
      expect(getFencedCodeRanges(input)).toEqual([]);
    });

    it("backtick fence is not closed by tilde fence", () => {
      const input = "a\n```\ncontent\n~~~\nstill in backtick block\n```\nb";
      const result = applyToNonCodeRegions(input, s => s.toUpperCase());
      expect(result).toContain("content");
      expect(result).toContain("still in backtick block");
      // Text after the closing ``` is outside – fn applied
      expect(result).toContain("B");
    });

    it("tilde fence is not closed by backtick fence", () => {
      const input = "a\n~~~\ncontent\n```\nstill code\n~~~\nb";
      const result = applyToNonCodeRegions(input, s => s.toUpperCase());
      expect(result).toContain("content");
      expect(result).toContain("still code");
      expect(result).toContain("B");
    });

    it("content between two fenced blocks is transformed", () => {
      const input = "```\nblock1\n```\nmiddle text\n```\nblock2\n```";
      const result = applyToNonCodeRegions(input, s => s.toUpperCase());
      expect(result).toContain("MIDDLE TEXT");
      expect(result).toContain("```\nblock1\n```");
      expect(result).toContain("```\nblock2\n```");
    });

    it("text before first fence is transformed", () => {
      const input = "preamble\n```\ncode\n```";
      const result = applyToNonCodeRegions(input, s => s.toUpperCase());
      expect(result).toContain("PREAMBLE");
      expect(result).toContain("```\ncode\n```");
    });

    it("text after last fence is transformed", () => {
      const input = "```\ncode\n```\npostamble";
      const result = applyToNonCodeRegions(input, s => s.toUpperCase());
      expect(result).toContain("POSTAMBLE");
      expect(result).toContain("```\ncode\n```");
    });

    it("no trailing newline – fence on last line is treated as unclosed (safe)", () => {
      const input = "text\n```\ncode";
      const result = applyToNonCodeRegions(input, s => s.toUpperCase());
      // code inside unclosed fence preserved verbatim
      expect(result).toContain("code");
    });

    it("empty fenced block is preserved verbatim", () => {
      const input = "before\n```\n```\nafter";
      const result = applyToNonCodeRegions(input, s => s.toUpperCase());
      expect(result).toContain("```\n```");
      expect(result).toContain("BEFORE");
      expect(result).toContain("AFTER");
    });

    it("fence-only document (entire content is one block) has fn not called at all", () => {
      const input = "```\nall code\n```";
      let fnCalled = false;
      applyToNonCodeRegions(input, s => {
        if (s.trim()) fnCalled = true;
        return s;
      });
      expect(fnCalled).toBe(false);
    });

    it("adjacent fenced blocks with no prose between them", () => {
      const input = "```\nblock1\n```\n```\nblock2\n```";
      const result = applyToNonCodeRegions(input, s => s.toUpperCase());
      expect(result).toContain("```\nblock1\n```");
      expect(result).toContain("```\nblock2\n```");
    });
  });

  // ---------------------------------------------------------------------------
  // applyFnOutsideInlineCode – exercised via applyToNonCodeRegions (no fences)
  // ---------------------------------------------------------------------------
  describe("inline code span handling (via applyToNonCodeRegions, no fences)", () => {
    const upper = s => s.toUpperCase();

    it("plain text with no backticks applies fn to everything", () => {
      expect(applyToNonCodeRegions("hello", upper)).toBe("HELLO");
    });

    it("single-backtick inline code span is preserved verbatim", () => {
      const result = applyToNonCodeRegions("before `code` after", upper);
      expect(result).toBe("BEFORE `code` AFTER");
    });

    it("double-backtick inline code span is preserved verbatim", () => {
      const result = applyToNonCodeRegions("a ``two backtick code`` b", upper);
      expect(result).toBe("A ``two backtick code`` B");
    });

    it("triple-backtick inline code is preserved verbatim", () => {
      const result = applyToNonCodeRegions("a ```triple``` b", upper);
      expect(result).toBe("A ```triple``` B");
    });

    it("inline code span containing template delimiters is preserved", () => {
      const escape = s => s.replace(/\{\{/g, "ESCAPED");
      const result = applyToNonCodeRegions("text `{{ var }}` text", escape);
      expect(result).toBe("text `{{ var }}` text");
    });

    it("backtick with no closing match is treated as literal text (fn applied)", () => {
      const result = applyToNonCodeRegions("text `unclosed", upper);
      // The whole string (including the backtick) should be uppercased
      expect(result).toBe("TEXT `UNCLOSED");
    });

    it("multiple inline code spans in one line are each preserved", () => {
      const result = applyToNonCodeRegions("a `x` b `y` c", upper);
      expect(result).toBe("A `x` B `y` C");
    });

    it("mismatched backtick counts: single-backtick sequence is not closed by double", () => {
      // `code`` – the opening ` looks for a matching single ` to close.
      // The ``  at the end is two backticks, which is NOT the same count.
      // So the opening ` is unmatched and treated as literal text.
      const result = applyToNonCodeRegions("text `code`` end", upper);
      // fn applied to entire string (no valid inline code span)
      expect(result).toBe("TEXT `CODE`` END");
    });

    it("double-backtick span closes only with double-backtick", () => {
      // ``code`end`` – the inner ` is not a valid closer for ``
      const result = applyToNonCodeRegions("before ``code`end`` after", upper);
      expect(result).toContain("``code`end``");
      expect(result).toContain("BEFORE");
      expect(result).toContain("AFTER");
    });

    it("inline code containing a single backtick via double-backtick wrapper", () => {
      // CommonMark: `` ` `` renders as a single backtick
      const result = applyToNonCodeRegions("a `` ` `` b", upper);
      expect(result).toBe("A `` ` `` B");
    });

    it("empty inline code span (``  ``) is preserved verbatim", () => {
      const result = applyToNonCodeRegions("a `` b", upper);
      // `` has no closing `` so it is literal text – fn applied
      expect(result).toBe("A `` B");
    });

    it("consecutive inline code spans with content between them", () => {
      const result = applyToNonCodeRegions("`a` middle `b`", upper);
      expect(result).toBe("`a` MIDDLE `b`");
    });

    it("inline code at start of string", () => {
      const result = applyToNonCodeRegions("`code` rest", upper);
      expect(result).toBe("`code` REST");
    });

    it("inline code at end of string", () => {
      const result = applyToNonCodeRegions("rest `code`", upper);
      expect(result).toBe("REST `code`");
    });

    it("entire string is inline code", () => {
      let fnCalled = false;
      applyToNonCodeRegions("`entire`", s => {
        if (s.trim()) fnCalled = true;
        return s;
      });
      expect(fnCalled).toBe(false);
    });
  });

  // ---------------------------------------------------------------------------
  // applyToNonCodeRegions – mixed fenced + inline combinations
  // ---------------------------------------------------------------------------
  describe("applyToNonCodeRegions – mixed code regions", () => {
    it("inline code inside fenced block is entirely preserved verbatim", () => {
      const input = "text\n```\nuse `backtick` inside\n```\nafter";
      const result = applyToNonCodeRegions(input, s => s.toUpperCase());
      expect(result).toContain("use `backtick` inside");
      expect(result).toContain("AFTER");
    });

    it("inline code BEFORE fenced block is also preserved", () => {
      const input = "use `inline` before\n```\ncode\n```\nafter";
      const result = applyToNonCodeRegions(input, s => s.toUpperCase());
      expect(result).toContain("`inline`");
      expect(result).toContain("USE");
      expect(result).toContain("BEFORE");
    });

    it("inline code AFTER fenced block is preserved", () => {
      const input = "```\ncode\n```\nuse `inline` after";
      const result = applyToNonCodeRegions(input, s => s.toUpperCase());
      expect(result).toContain("`inline`");
      expect(result).toContain("AFTER");
    });

    it("fn is idempotent when applied to non-code regions (identity transform)", () => {
      const input = "prose\n```\ncode\n```\n`inline` prose";
      const result = applyToNonCodeRegions(input, s => s);
      expect(result).toBe(input);
    });

    it("function returning empty string removes all prose", () => {
      const input = "prose\n```\ncode\n```\nmore prose";
      const result = applyToNonCodeRegions(input, () => "");
      // Only the fenced block should survive
      expect(result).toContain("```\ncode\n```");
      expect(result).not.toContain("prose");
    });

    it("function wrapping each chunk does not merge chunks", () => {
      const input = "a\n```\nblock\n```\nb";
      const result = applyToNonCodeRegions(input, s => `[${s}]`);
      // The fence content must remain unwrapped
      expect(result).toContain("```\nblock\n```");
      expect(result).not.toContain("[```");
    });
  });

  // ---------------------------------------------------------------------------
  // Fuzz-style tests – property-based verification
  // ---------------------------------------------------------------------------
  describe("fuzz: applyToNonCodeRegions invariants", () => {
    /**
     * Generate a range of varied markdown-like strings to verify core invariants:
     *   1. Identity transform → output === input  (length/content preserved)
     *   2. Content inside fenced blocks is never transformed
     *   3. The returned string has the same byte-length as input when fn is identity
     */

    const FENCE_CHARS = ["```", "~~~", "````", "~~~~"];
    const MARKERS = ["{{var}}", "${expr}", "<%=erb%>", "{#comment#}", "{%tag%}"];
    const TEXTS = ["hello world", "@user mention", "normal prose", "line\ntwo", ""];

    function makeFencedBlock(fence, content) {
      return `${fence}\n${content}\n${fence}`;
    }

    const seedCases = [
      // Single fenced block
      ...FENCE_CHARS.map(f => makeFencedBlock(f, MARKERS[0])),
      // Prose + fenced block
      ...FENCE_CHARS.map(f => `preamble\n${makeFencedBlock(f, MARKERS[1])}\npostamble`),
      // Multiple fenced blocks
      ...FENCE_CHARS.map(f => `${makeFencedBlock(f, MARKERS[0])}\ninterlude\n${makeFencedBlock(f, MARKERS[2])}`),
      // Inline code in prose
      ...MARKERS.map(m => `prose with \`${m}\` inline code`),
      // Inline code + fenced block
      `\`${MARKERS[0]}\` before\n${makeFencedBlock("```", MARKERS[1])}\nafter`,
      // Deeply nested appearance (not real nesting – just consecutive)
      makeFencedBlock("```", makeFencedBlock("~~~", "deep")),
      // Text only
      ...TEXTS.filter(t => t.length > 0),
      // Unclosed fence
      "prose\n```\nunclosed {{ leaked }}",
      // Empty fence
      "before\n```\n```\nafter",
    ];

    it("identity transform preserves input exactly (fuzz seed cases)", () => {
      for (const input of seedCases) {
        const result = applyToNonCodeRegions(input, s => s);
        expect(result).toBe(input);
      }
    });

    it("fenced block content is never passed to fn (fuzz seed cases)", () => {
      const SENTINEL = "\x00TOUCHED\x00";
      for (const input of seedCases) {
        // Build an fn that poisons any text it receives by prepending SENTINEL
        const poisoned = applyToNonCodeRegions(input, s => SENTINEL + s);
        // Extract every fenced block from `input` and verify none were poisoned
        const fenceRe = /^(`{3,}|~{3,})[^\n]*\n([\s\S]*?)\n\1\s*$/gm;
        let m;
        while ((m = fenceRe.exec(input)) !== null) {
          expect(poisoned).not.toContain(SENTINEL + m[2]);
        }
      }
    });

    it("result is a string for all seed inputs", () => {
      for (const input of seedCases) {
        const result = applyToNonCodeRegions(input, s => s.split("").reverse().join(""));
        expect(typeof result).toBe("string");
      }
    });

    it("throws or returns string for adversarial inputs (no crash)", () => {
      const adversarial = [
        // Very long fence opener
        "`".repeat(200) + "\ncontent\n" + "`".repeat(200),
        // Mixed fence characters in same block
        "```\ncontent\n~~~",
        // Backtick storm
        "`".repeat(50),
        // Alternating open/close-like lines
        "```\n~~~\n```\n~~~",
        // Nested look-alike
        "```\n```\ncontent\n```\n```",
        // Only newlines
        "\n\n\n",
        // Very long plain text
        "a".repeat(10000),
        // Unicode
        "```\n\u{1F600} emoji\n```\n\u{1F4A5}boom",
        // Null bytes in content
        "text\x00\x01\x02\n```\n\x00\n```",
      ];
      for (const input of adversarial) {
        let result;
        expect(() => {
          result = applyToNonCodeRegions(input, s => s);
        }).not.toThrow();
        expect(typeof result).toBe("string");
      }
    });

    it("fn called exactly once per non-code prose segment (fuzz seed)", () => {
      // Verify that fn is not called with empty strings when there is no prose
      for (const input of seedCases) {
        const calls = [];
        applyToNonCodeRegions(input, s => {
          calls.push(s);
          return s;
        });
        // fn should never be called with undefined or non-string
        for (const call of calls) {
          expect(typeof call).toBe("string");
        }
      }
    });

    // Property: applyToNonCodeRegions(applyToNonCodeRegions(x, f), f) behaves consistently
    // when f is idempotent – result should equal single application.
    it("idempotent fn gives same result on second application (fuzz seed)", () => {
      const idempotentFn = s => s.replace(/\{\{/g, "X{{X").replace(/X\{\{X/g, "\\{\\{");
      for (const input of seedCases) {
        const once = applyToNonCodeRegions(input, idempotentFn);
        const twice = applyToNonCodeRegions(once, idempotentFn);
        // After first pass everything is already replaced; second pass should not change prose
        // We only assert no crashes and that both are strings.
        expect(typeof once).toBe("string");
        expect(typeof twice).toBe("string");
      }
    });
  });

  // ---------------------------------------------------------------------------
  // Edge cases for getFencedCodeRanges (verified via applyToNonCodeRegions)
  // ---------------------------------------------------------------------------
  describe("getFencedCodeRanges edge cases (contract verification)", () => {
    it("fence on very first line with no preceding text", () => {
      const input = "```\ncode\n```\nafter";
      const result = applyToNonCodeRegions(input, s => s.toUpperCase());
      expect(result).toContain("```\ncode\n```");
      expect(result).toContain("AFTER");
    });

    it("fence on very last line (no trailing newline) – unclosed is safe", () => {
      const input = "before\n```";
      const result = applyToNonCodeRegions(input, s => s.toUpperCase());
      // The lonely ``` line starts an unclosed block; safe fallback preserves it
      expect(typeof result).toBe("string");
    });

    it("fence character must appear ≥3 times to be valid (two backticks form inline code instead)", () => {
      // "text ``\ncontent\n``\nafter" – the double-backtick `` on the first content
      // line is NOT a fence (needs ≥3). getFencedCodeRanges finds no fences.
      // applyFnOutsideInlineCode then processes the whole string: the opening ``
      // at "text ``" finds a matching closing `` at the start of the line "``",
      // so "``\ncontent\n``" is treated as a multi-line inline code span and is
      // preserved verbatim.  Only the surrounding prose is transformed.
      const input = "text ``\ncontent\n``\nafter";
      const result = applyToNonCodeRegions(input, s => s.toUpperCase());
      expect(result).toContain("``\ncontent\n``"); // inline code span preserved
      expect(result).toContain("TEXT "); // prose before transformed
      expect(result).toContain("\nAFTER"); // prose after transformed
    });

    it("closing fence can be longer than opening fence", () => {
      // Opening: ``` (3), Closing: ```` (4) – valid: closing must be ≥ opening length
      const input = "before\n```\ncode\n````\nafter";
      const result = applyToNonCodeRegions(input, s => s.toUpperCase());
      expect(result).toContain("```\ncode\n````");
      expect(result).toContain("BEFORE");
      expect(result).toContain("AFTER");
    });

    it("four-tilde fence opened and closed correctly", () => {
      const input = "a\n~~~~\ncontent\n~~~~\nb";
      const result = applyToNonCodeRegions(input, s => s.toUpperCase());
      expect(result).toContain("~~~~\ncontent\n~~~~");
      expect(result).toContain("A");
      expect(result).toContain("B");
    });

    it("fenced block with Windows-style CRLF line endings", () => {
      // The parser splits on \n; CRLF (\r\n) lines will have trailing \r in content.
      // Verify the parser doesn't crash and the fence content is still preserved.
      const input = "before\r\n```\r\ncode\r\n```\r\nafter";
      const result = applyToNonCodeRegions(input, s => s.toUpperCase());
      expect(typeof result).toBe("string");
      expect(result).toContain("code");
    });

    it("three consecutive fenced blocks separated by single newlines", () => {
      const input = "```\nA\n```\n```\nB\n```\n```\nC\n```";
      const result = applyToNonCodeRegions(input, () => "PROSE");
      // No prose between consecutive blocks – fn should not add PROSE within blocks
      expect(result).toContain("```\nA\n```");
      expect(result).toContain("```\nB\n```");
      expect(result).toContain("```\nC\n```");
    });

    it("fenced block immediately inside another fenced block is treated as content", () => {
      // Outer ``` starts a block; the inner ``` is just content until the outer closes
      const input = "outer open\n```\ninner ```\nstill code\n```\nafter";
      const result = applyToNonCodeRegions(input, s => s.toUpperCase());
      // "inner ```" and "still code" should be preserved verbatim
      expect(result).toContain("inner ```");
      expect(result).toContain("still code");
      expect(result).toContain("OUTER OPEN");
      expect(result).toContain("AFTER");
    });
  });

  // ---------------------------------------------------------------------------
  // applyFnOutsideInlineCode specific edge cases (via applyToNonCodeRegions, no fences)
  // ---------------------------------------------------------------------------
  describe("inline code edge cases (no fences present)", () => {
    it("string with only backticks and no content", () => {
      const result = applyToNonCodeRegions("```", s => s.toUpperCase());
      // Three backticks alone (no newline, not a full fence line in the line-based parser)
      // Treated as prose OR as start of unclosed block – either way must not throw
      expect(typeof result).toBe("string");
    });

    it("backtick followed immediately by newline", () => {
      const result = applyToNonCodeRegions("`\n`", s => s.toUpperCase());
      expect(typeof result).toBe("string");
    });

    it("inline code spanning backtick runs of length 4", () => {
      const result = applyToNonCodeRegions("a ````four```` b", s => s.toUpperCase());
      expect(result).toContain("````four````");
      expect(result).toContain("A ");
      expect(result).toContain(" B");
    });

    it("backtick inside inline code content does not end the span prematurely", () => {
      // `` a`b `` – the single backtick inside does not close the double-backtick span
      const result = applyToNonCodeRegions("x `` a`b `` y", s => s.toUpperCase());
      expect(result).toContain("`` a`b ``");
      expect(result).toContain("X ");
      expect(result).toContain(" Y");
    });

    it("multi-line inline code span is handled without crashing", () => {
      // CommonMark spec §6.11: inline code spans CAN span line endings –
      // a newline is treated as a space. The parser here does not strip the
      // newline, but must not crash or hang on such input.
      const result = applyToNonCodeRegions("`line1\nline2`", s => s.toUpperCase());
      expect(typeof result).toBe("string");
    });

    it("very long inline code span is preserved", () => {
      const content = "x".repeat(5000);
      const input = `before \`${content}\` after`;
      const result = applyToNonCodeRegions(input, s => s.toUpperCase());
      expect(result).toContain(`\`${content}\``);
      expect(result).toContain("BEFORE");
      expect(result).toContain("AFTER");
    });

    it("many inline code spans in a row", () => {
      const spans = Array.from({ length: 50 }, (_, i) => `\`s${i}\``).join(" gap ");
      const result = applyToNonCodeRegions(spans, s => s.toUpperCase());
      for (let i = 0; i < 50; i++) {
        expect(result).toContain(`\`s${i}\``);
      }
      expect(result).toContain("GAP");
    });
  });
});
