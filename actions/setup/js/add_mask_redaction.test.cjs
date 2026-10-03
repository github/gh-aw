// @ts-check
import { describe, it, expect } from "vitest";
import { createRequire } from "module";

const require = createRequire(import.meta.url);
const { applyAddMaskRedaction, collectAddMaskedValues, isAddMaskCommandLine, redactArtifactMaskedValues, redactMaskedValues, unescapeWorkflowCommandValue } = require("./add_mask_redaction.cjs");

describe("add_mask_redaction", () => {
  describe("isAddMaskCommandLine", () => {
    it("detects add-mask command lines", () => {
      expect(isAddMaskCommandLine("::add-mask::secret")).toBe(true);
      expect(isAddMaskCommandLine("2026-01-01 ::add-mask::secret")).toBe(true);
      expect(isAddMaskCommandLine("no command here")).toBe(false);
    });
  });

  describe("unescapeWorkflowCommandValue", () => {
    it("decodes workflow command escapes", () => {
      expect(unescapeWorkflowCommandValue("a%0Ab%0Dc%25d")).toBe("a\nb\rc%d");
    });
  });

  describe("collectAddMaskedValues", () => {
    it("collects masked values from log content", () => {
      const log = ["starting", "::add-mask::abcd1234", "::add-mask::zz", "done"].join("\n");
      const result = collectAddMaskedValues(log);
      expect(result).toContain("abcd1234");
      expect(result).toContain("zz");
      expect(result).toHaveLength(2);
    });

    it("expands multi-line masked values into individual lines", () => {
      const log = "::add-mask::line-one%0Aline-two\n";
      expect(collectAddMaskedValues(log).sort()).toEqual(["line-one", "line-two"]);
    });

    it("splits carriage-return separated values", () => {
      const log = "::add-mask::part-one%0Dpart-two\n";
      expect(collectAddMaskedValues(log).sort()).toEqual(["part-one", "part-two"]);
    });

    it("keeps both the verbatim and trimmed forms of a padded value", () => {
      const values = collectAddMaskedValues("::add-mask:: padded-secret \n");
      expect(values).toContain(" padded-secret ");
      expect(values).toContain("padded-secret");
    });

    it("ignores empty values and de-duplicates", () => {
      const log = ["::add-mask::", "::add-mask::   ", "::add-mask::dup", "::add-mask::dup"].join("\n");
      expect(collectAddMaskedValues(log)).toEqual(["dup"]);
    });

    it("returns an empty array for empty content", () => {
      expect(collectAddMaskedValues("")).toEqual([]);
    });
  });

  describe("redactMaskedValues", () => {
    it("replaces every occurrence with ***", () => {
      expect(redactMaskedValues("token=abc and abc again", ["abc"])).toBe("token=*** and *** again");
    });

    it("handles regex metacharacters in masked values", () => {
      expect(redactMaskedValues("value a.c here", ["a.c"])).toBe("value *** here");
      expect(redactMaskedValues("value abc here", ["a.c"])).toBe("value abc here");
    });

    it("returns the text unchanged when there are no masked values", () => {
      expect(redactMaskedValues("plain", [])).toBe("plain");
    });
  });

  describe("applyAddMaskRedaction", () => {
    it("strips add-mask command lines and redacts masked values", () => {
      const log = ["output using s3cr3t", "::add-mask::s3cr3t", "more output"].join("\n");
      const maskedValues = collectAddMaskedValues(log);
      expect(applyAddMaskRedaction(log, maskedValues)).toBe(["output using ***", "more output"].join("\n"));
    });

    it("returns falsy input unchanged", () => {
      expect(applyAddMaskRedaction("", ["x"])).toBe("");
    });

    describe("redactArtifactMaskedValues", () => {
      it("redacts decoded JSON strings and keys without mutating syntax or primitives", () => {
        const secret = 'quote"\\percent%0A';
        const value = { [secret]: [secret, 0, false, null], nested: { output: "first\nsecond" } };
        const masks = collectAddMaskedValues(`::add-mask::${secret.replace(/%/g, "%25")}\n::add-mask::first%0Asecond\n`);
        const redacted = redactArtifactMaskedValues(JSON.stringify(value, null, 2) + "\n", masks);
        expect(JSON.parse(redacted)).toEqual({ "***": ["***", 0, false, null], nested: { output: "***\n***" } });
        expect(redacted.endsWith("\n")).toBe(true);
        expect(value[secret][0]).toBe(secret);
      });

      it("keeps JSON valid when a short mask matches JSON punctuation", () => {
        expect(JSON.parse(redactArtifactMaskedValues('{"text":"\\""}\n', ['"']))).toEqual({ text: "***" });
      });

      it("redacts JSONL, Unicode escapes, and malformed adjacent records", () => {
        const content = '::add-mask::opaque\n{"data":"\\u006fpaque"}\n{"broken":"\\u006fpaque",\nplain opaque\n{"data":"safe"}\n';
        const redacted = redactArtifactMaskedValues(content, ["opaque"]);
        expect(redacted).toBe('{"data":"***"}\n{"broken":"***",\nplain ***\n{"data":"safe"}\n');
      });

      it("preserves untouched formatting and empty or unmasked inputs", () => {
        const content = '  {\n    "empty": "", "zero": 0\n  }\n';
        expect(redactArtifactMaskedValues(content, ["absent"])).toBe(content);
        expect(redactArtifactMaskedValues(content, [])).toBe(content);
        expect(redactArtifactMaskedValues("", ["secret"])).toBe("");
      });
    });
  });
});
