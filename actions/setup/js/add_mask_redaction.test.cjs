// @ts-check
import { describe, it, expect, vi } from "vitest";
import { createRequire } from "module";

const require = createRequire(import.meta.url);
const { applyAddMaskRedaction, collectAddMaskedValues, isAddMaskCommandLine, redactArtifactMaskedValues, transformJSONStrings, unescapeWorkflowCommandValue } = require("./add_mask_redaction.cjs");
const { redactMaskedValues } = require("./mask_value_matcher.cjs");

describe("add_mask_redaction", () => {
  describe("seeded parser fuzz tests", () => {
    const seeds = [1, 42, 2026, 0xdeadbeef, 0xffffffff];
    const alphabet = ["a", "z", "0", '"', "\\", "/", "\b", "\f", "\n", "\r", "\t", "\u0000", "\u001f", "\u2028", "\u2029", "\ud800", "\udfff", "\ud83d\ude80"];

    function randomFor(seed) {
      let state = seed;
      return max => {
        state = (Math.imul(state, 1664525) + 1013904223) >>> 0;
        return Math.floor((state / 0x100000000) * max);
      };
    }

    function randomText(random) {
      return Array.from({ length: random(24) }, () => alphabet[random(alphabet.length)]).join("");
    }

    function encodeString(value, random) {
      let encoded = '"';
      for (let index = 0; index < value.length; index++) {
        const character = value[index];
        encoded += random(2) ? `\\u${value.charCodeAt(index).toString(16).padStart(4, "0")}` : JSON.stringify(character).slice(1, -1);
      }
      return encoded + '"';
    }

    it.each(seeds)("matches an independent bounded token grammar after random insertions, deletions, and truncations (seed %i)", seed => {
      const random = randomFor(seed);
      const fragments = ['"', "\\", "\\u", "\\u0000", "\\u12gg", "\\q", '""', "\t", "\n", "\r", "plain", ":", ","];
      // The regex oracle only sees short fuzz records, never the large production inputs.
      const tokenPattern = /"(?:[^"\\\x00-\x1f]|\\(?:["\\/bfnrt]|u[0-9a-fA-F]{4}))*"/g;
      for (let iteration = 0; iteration < 500; iteration++) {
        let content = Array.from({ length: 1 + random(8) }, () => (random(2) ? encodeString(randomText(random), random) : fragments[random(fragments.length)])).join("");
        const position = random(content.length + 1);
        switch (random(3)) {
          case 0:
            content = content.slice(0, position) + String.fromCharCode(random(0x10000)) + content.slice(position);
            break;
          case 1:
            content = content.slice(0, position) + content.slice(position + 1);
            break;
          case 2:
            content = content.slice(0, position);
            break;
        }
        const tokens = [];
        const spans = [];
        const result = transformJSONStrings(
          content,
          token => {
            expect(typeof JSON.parse(token), `seed ${seed}, iteration ${iteration}`).toBe("string");
            tokens.push(token);
            spans.push(token);
            return token;
          },
          plain => {
            spans.push(plain);
            return plain;
          }
        );
        const context = `seed ${seed}, iteration ${iteration}, input ${JSON.stringify(content)}`;
        expect(tokens, context).toEqual(Array.from(content.matchAll(tokenPattern), match => match[0]));
        expect(spans.join(""), context).toBe(content);
        expect(result, context).toBe(content);
        expect(redactArtifactMaskedValues(content, ["ABSENT_RUNTIME_MASK"]), context).toBe(content);
      }
    });

    it.each(seeds)("redacts randomly escaped masks in nested JSON, JSONL, and mixed records against a decoded oracle (seed %i)", seed => {
      const random = randomFor(seed);
      for (let iteration = 0; iteration < 300; iteration++) {
        const mask = `MASK-${seed}-${iteration}-` + randomText(random);
        const left = randomText(random);
        const right = randomText(random);
        const value = left + mask + right + mask;
        const expected = value.replaceAll(mask, "***");
        const key = `key-${mask}`;
        const encodedValue = encodeString(value, random);
        const encodedKey = encodeString(key, random);
        const nested = `{${encodedKey}:[${encodedValue},{"value":${encodedValue}},null,true,false,42]}`;
        const context = `seed ${seed}, iteration ${iteration}`;
        const expectedNested = { "key-***": [expected, { value: expected }, null, true, false, 42] };
        expect(JSON.parse(redactArtifactMaskedValues(nested, [mask])), context).toEqual(expectedNested);
        const jsonl = ` \t${nested}\r\n${encodedValue}\n`;
        const redactedLines = redactArtifactMaskedValues(jsonl, [mask]).trim().split("\n");
        expect(
          redactedLines.map(line => JSON.parse(line)),
          context
        ).toEqual([expectedNested, expected]);
        const mixed = `output: ${encodedValue} end`;
        expect(redactArtifactMaskedValues(mixed, [mask]), context).toBe(`output: ${JSON.stringify(expected)} end`);
        const selected = redactArtifactMaskedValues(mixed, [mask], undefined, () => "***");
        expect(JSON.parse(selected.slice("output: ".length, -" end".length)), context).toBe(expected);
        expect(redactArtifactMaskedValues(nested, ["ABSENT_RUNTIME_MASK"]), context).toBe(nested);
        expect(redactArtifactMaskedValues(jsonl, ["ABSENT_RUNTIME_MASK"]), context).toBe(jsonl);
        expect(redactArtifactMaskedValues(mixed, ["ABSENT_RUNTIME_MASK"]), context).toBe(mixed);
        expect(
          redactArtifactMaskedValues(mixed, ["ABSENT_RUNTIME_MASK"], undefined, () => "***"),
          context
        ).toBe(mixed);
      }
    });

    it.each(seeds)("finds randomly encoded masks after ambiguous malformed quotes without rewriting unmatched records (seed %i)", seed => {
      const random = randomFor(seed);
      const prefixes = ['"bad\\q"', '"bad\\u12gg"', '"bad\\u123"', '"bad\t"', '"bad\u0000"'];
      for (let iteration = 0; iteration < 300; iteration++) {
        const prefix = prefixes[random(prefixes.length)];
        const before = (random(2) ? String.raw`C:\temp\file.txt \u0041 \\u006fpaque ` : "") + "a".repeat(random(16));
        const after = "z".repeat(random(16)) + (random(2) ? String.raw` D:\new\report.txt \u0042` : "");
        const encodedMask = "\\u006f" + encodeString("paque", random).slice(1, -1);
        const content = `${prefix} "${before}${encodedMask}${after}"`;
        const context = `seed ${seed}, iteration ${iteration}, input ${JSON.stringify(content)}`;
        expect(JSON.parse(redactArtifactMaskedValues(content, ["opaque"])), context).toBe(`${prefix} "${before}***${after}"`);
        expect(JSON.parse(redactArtifactMaskedValues(content, ["opaque"], undefined, () => "***")), context).toBe(`${prefix} "${before}***${after}"`);
        expect(redactArtifactMaskedValues(content, ["ABSENT_RUNTIME_MASK"]), context).toBe(content);
      }
    });

    it.each(seeds)("preserves original numeric lexemes while masking generated high-precision tokens (seed %i)", seed => {
      const random = randomFor(seed);
      for (let iteration = 0; iteration < 300; iteration++) {
        const integer = String(1 + random(9)) + Array.from({ length: 20 + random(20) }, () => String(random(10))).join("");
        const fraction = random(2) ? "." + String(random(10000)) : "";
        const exponent = random(2) ? `e${random(2) ? "+" : "-"}${random(1000)}` : "";
        const token = (random(2) ? "-" : "") + integer + fraction + exponent;
        const content = ` \t[${token},-0,1e+09,9007199254740993,1e400]\r\n`;
        const context = `seed ${seed}, iteration ${iteration}, token ${token}`;
        const redacted = redactArtifactMaskedValues(content, [token]);
        expect(redacted, context).toBe(' \t["***",-0,1e+09,9007199254740993,1e400]\r\n');
        expect(JSON.parse(redacted)[0], context).toBe("***");
        expect(redactArtifactMaskedValues(content, ["ABSENT_RUNTIME_MASK"]), context).toBe(content);
      }
    });
  });

  describe("isAddMaskCommandLine", () => {
    it("detects add-mask command lines", () => {
      expect(isAddMaskCommandLine("::add-mask::secret")).toBe(true);
      expect(isAddMaskCommandLine("2026-01-01 ::add-mask::secret")).toBe(true);
      expect(isAddMaskCommandLine("no command here")).toBe(false);
    });

    it("preserves only standalone unexpanded variable references", () => {
      for (const payload of ['${VAR}"', "${_var123}'", "${VAR}", '${VAR}"  \r']) {
        expect(isAddMaskCommandLine(`echo "::add-mask::${payload}`)).toBe(false);
      }
      for (const payload of ['literal"', '${VAR}literal"', 'literal${VAR}"', '${VAR:-literal}"', '${123}"', '${VAR}" # literal']) {
        expect(isAddMaskCommandLine(`echo "::add-mask::${payload}`)).toBe(true);
      }
      expect(isAddMaskCommandLine("::add-mask::${VAR}")).toBe(true);
      expect(isAddMaskCommandLine("2026-01-01 ::add-mask::${VAR}")).toBe(true);
      expect(isAddMaskCommandLine('literal echo "::add-mask::${VAR}"')).toBe(true);
      const text = ['echo "::add-mask::${VAR}"', 'echo "::add-mask::literal"', "after"].join("\n");
      expect(applyAddMaskRedaction(text, [])).toBe(['echo "::add-mask::${VAR}"', "after"].join("\n"));
      expect(applyAddMaskRedaction(text, ["${VAR}"])).toBe('echo "::add-mask::***"\nafter');
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

    it("ignores shell source references while collecting literal runtime masks", () => {
      const log = ['run: echo "::add-mask::${SOURCE_VAR}"', "::add-mask::${RUNTIME_VALUE}"].join("\n");
      expect(collectAddMaskedValues(log)).toEqual(["${RUNTIME_VALUE}"]);
      expect(applyAddMaskRedaction(log, collectAddMaskedValues(log))).toBe('run: echo "::add-mask::${SOURCE_VAR}"');
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
      it.each([
        ['{"configured":["configured",12345678],"live":"runtime","safe":42}', '{"***REDACTED***":["***REDACTED***","***REDACTED***"],"live":"***","safe":42}'],
        ['plain configured runtime "\\u0063onfigured" "\\u0072untime"', 'plain ***REDACTED*** *** "***REDACTED***" "***"'],
        ['"bad\t" "\\u0063onfigured \\u0072untime" C:\\temp\\file.txt', JSON.stringify('"bad\t" "***REDACTED*** ***" C:\\temp\\file.txt')],
      ])("supports per-value replacement markers throughout encoding-aware redaction (%j)", (content, expected) => {
        const replacement = value => (value === "runtime" ? "***" : "***REDACTED***");
        expect(redactArtifactMaskedValues(content, ["configured", "12345678", "runtime"], undefined, replacement)).toBe(expected);
      });

      it("preserves original JSON whitespace, safe escapes, and numeric lexemes with explicit replacement markers", () => {
        const content = ' \t{ "configured" : "\\u0063onfigured", "safe" : "\\u0041", "values" : [12345678,-0,1e+09,9007199254740993,1e400] }\r\n';
        const expected = ' \t{ "***REDACTED***" : "***REDACTED***", "safe" : "\\u0041", "values" : ["***REDACTED***",-0,1e+09,9007199254740993,1e400] }\r\n';
        expect(redactArtifactMaskedValues(content, ["configured", "12345678"], undefined, () => "***REDACTED***")).toBe(expected);
      });

      it.each([
        ["synthetic\\n", "synthetic\npassword", JSON.stringify("synthetic\npassword")],
        ["synthetic\\u006e", "syntheticnpassword", '"synthetic\\u006epassword"'],
        ['"password', '"passwordtail', JSON.stringify('"passwordtail')],
      ])("resolves longer encoded masks before raw configured prefixes (%j)", (configured, runtime, encoded) => {
        const masks = [configured, runtime].sort((a, b) => b.length - a.length);
        const replacement = value => (value === configured ? "***REDACTED***" : "***");
        expect(redactArtifactMaskedValues("output: " + encoded, masks, undefined, replacement)).toBe('output: "***"');
        expect(JSON.parse(redactArtifactMaskedValues('"bad\t" ' + encoded, masks, undefined, replacement))).toBe('"bad\t" "***"');
      });

      it("resolves raw configured masks spanning recoverable token boundaries before shorter decoded masks", () => {
        const configured = 'synthetic"\\\tpassword';
        const content = '"bad\t" "\\u006fpaque" token=' + configured;
        const replacement = value => (value === configured ? "***REDACTED***" : "***");
        expect(JSON.parse(redactArtifactMaskedValues(content, [configured, "synthetic", "opaque"], undefined, replacement))).toBe('"bad\t" "***" token=***REDACTED***');
      });

      it.each(["***", "***REDACTED***"])("keeps a fully masked raw shell command as a plain marker (%s)", marker => {
        const content = 'export PAYLOAD_DIR="/synthetic/session/payload"';
        expect(redactArtifactMaskedValues(content, [content], undefined, () => marker)).toBe(marker);
        expect(redactArtifactMaskedValues(content + "\n", [content], undefined, () => marker)).toBe(marker + "\n");
      });

      it("coalesces raw and encoded matches that overlap at different source positions", () => {
        const content = String.raw`\u0061bc\nf`;
        expect(JSON.parse(redactArtifactMaskedValues(content, [String.raw`u0061bc\nf`, String.raw`c\nf`, "ab"], undefined, () => "***"))).toBe("***");
      });

      it.each([
        ["long-configured-secret", "ablong", "ablong-configured-secret"],
        ["12345678", "9123", "912345678"],
      ])("coalesces shorter earlier runtime overlaps across text, JSON strings, keys, and numeric lexemes (%j)", (configured, runtime, content) => {
        const replacement = value => (value === configured ? "***REDACTED***" : "***");
        const masks = [configured, runtime];
        expect(redactMaskedValues(content, masks, replacement)).toBe("***REDACTED***");
        expect(redactArtifactMaskedValues(content, masks, undefined, replacement)).toBe(/^\d+$/.test(content) ? '"***REDACTED***"' : "***REDACTED***");
        expect(redactArtifactMaskedValues(JSON.stringify({ [content]: content }), masks, undefined, replacement)).toBe('{"***REDACTED***":"***REDACTED***"}');
        expect(redactArtifactMaskedValues("output: " + JSON.stringify(content), masks, undefined, replacement)).toBe('output: "***REDACTED***"');
      });

      it("keeps literal Unicode escapes distinct with explicit replacement markers", () => {
        const content = '"bad\t" ' + JSON.stringify("\\u006fpaque");
        expect(redactArtifactMaskedValues(content, ["opaque"], undefined, () => "***")).toBe(content);
      });

      it("fails closed if decoding an original validated token unexpectedly fails with explicit replacement markers", () => {
        const encoded = '"\\u006fpaque"';
        const failure = new Error("synthetic decoding failure");
        const original = JSON.parse;
        const spy = vi.spyOn(JSON, "parse").mockImplementation(value => {
          if (value === encoded) throw failure;
          return original(value);
        });
        try {
          expect(() => redactArtifactMaskedValues("output: " + encoded, ["opaque"], undefined, () => "***")).toThrow("Unable to decode JSON log string during secret redaction");
        } finally {
          spy.mockRestore();
        }
      });

      it("keeps the structured JSON visitor authoritative and propagates its failures", () => {
        const visitor = vi.fn(value => ({ wrapped: value }));
        expect(redactArtifactMaskedValues('{"safe":42}', [], visitor, () => "***REDACTED***")).toBe('{"wrapped":{"safe":42}}');
        expect(visitor).toHaveBeenCalledExactlyOnceWith({ safe: 42 });
        const error = new Error("synthetic visitor failure");
        expect(() =>
          redactArtifactMaskedValues('{"safe":42}', [], () => {
            throw error;
          })
        ).toThrow(error);
      });

      describe("JSON string scanner boundaries", () => {
        const encodedMask = '"\\u006fpaque"';
        const validStrings = [
          "",
          "ordinary ASCII",
          'quote"then opaque',
          "backslash\\then opaque",
          "slash/then opaque",
          "backspace\bthen opaque",
          "formfeed\fthen opaque",
          "newline\nthen opaque",
          "return\rthen opaque",
          "tab\tthen opaque",
          "\u0000opaque\u001f",
          "caf\u00e9 opaque \u2603",
          "\ud83d\ude80opaque",
          "\ud800opaque\udfff",
          "\u2028opaque\u2029",
          '""opaque""',
        ];

        it.each(validStrings.map(value => [JSON.stringify(value), value]))("matches decoded-string masking for valid token %s", (encoded, value) => {
          const expected = JSON.stringify(value.replaceAll("opaque", "***"));
          expect(redactArtifactMaskedValues(encoded, ["opaque"])).toBe(expected);
          expect(redactArtifactMaskedValues(`before ${encoded} after`, ["opaque"])).toBe(`before ${expected} after`);
        });

        it.each(Array.from({ length: 17 }, (_, count) => count))("recognizes closing quotes after %i pairs of escaped backslashes", count => {
          const safe = JSON.stringify("\\".repeat(count));
          const escapedQuote = JSON.stringify("\\".repeat(count) + '"opaque');
          const content = `prefix ${safe} ${encodedMask} ${escapedQuote} suffix`;
          const expected = `prefix ${safe} "***" ${JSON.stringify("\\".repeat(count) + '"***')} suffix`;
          expect(redactArtifactMaskedValues(content, ["opaque"])).toBe(expected);
        });

        it.each(["\\u006fpaque", "op\\u0061que", "\\u006F\\u0070\\u0061\\u0071\\u0075\\u0065"])("decodes Unicode escapes at every mask position (%s)", encoded => {
          expect(redactArtifactMaskedValues(`log "${encoded}"`, ["opaque"])).toBe('log "***"');
        });

        it.each(Array.from({ length: 32 }, (_, code) => code))("recovers after unescaped control character U+%i", code => {
          const prefix = '"invalid' + String.fromCharCode(code) + " ";
          expect(redactArtifactMaskedValues(prefix + encodedMask, ["opaque"])).toBe(prefix + '"***"');
        });

        it.each(["a", "c", "e", "v", "x", "0", "1", "'", "U", " ", "\t", "\r", "\n", "\u2028", "\u2029"])("recovers after illegal escaped character %j", escaped => {
          const prefix = '"invalid\\' + escaped + " ";
          expect(redactArtifactMaskedValues(prefix + encodedMask, ["opaque"])).toBe(prefix + '"***"');
        });

        it.each(["", "0", "00", "000", "gggg", "000g", "00g0", "0g00", "g000", "+000", "-000", " 000"])("recovers after malformed Unicode escape %j", digits => {
          const prefix = '"invalid\\u' + digits + " ";
          expect(redactArtifactMaskedValues(prefix + encodedMask, ["opaque"])).toBe(prefix + '"***"');
        });

        it.each(['"bad\t"', '"bad\u0001"', '"bad\\q"', '"bad\\u12gg"'])("conservatively redacts alternate quote pairings after terminated malformed tokens (%j)", prefix => {
          const content = `${prefix} ${encodedMask}`;
          const redacted = redactArtifactMaskedValues(content, ["opaque"]);
          expect(JSON.parse(redacted)).toBe(`${prefix} "***"`);
          expect(redacted).not.toContain("\\u006fpaque");
          expect(redactArtifactMaskedValues(content, ["absent"])).toBe(content);
        });

        it("conservatively redacts escapes before invalid controls and in unterminated strings", () => {
          for (const content of ['"\\u006fpaque\t"', '"\\u006fpaque', '"\\u006fpaque\\']) {
            const redacted = redactArtifactMaskedValues(content, ["opaque"]);
            expect(JSON.parse(redacted)).toBe(content.replace("\\u006fpaque", "***"));
          }
        });

        it("redacts encoded masks crossing quote boundaries in an alternate pairing", () => {
          const content = '"bad\t" "foo\\"bar"';
          expect(JSON.parse(redactArtifactMaskedValues(content, ['foo"bar']))).toBe('"bad\t" "***"');
        });

        it.each(["opaque", 'foo"bar', "a\tb", "a\\b", "\ud83d\ude80"])("only rewrites source spans participating in conservative matches (%j)", mask => {
          const encodedMask = Array.from({ length: mask.length }, (_, index) => `\\u${mask.charCodeAt(index).toString(16).padStart(4, "0")}`).join("");
          const prefix = String.raw`before C:\temp\file.txt \\server\folder \n \t \b \f \r \" \/ \u0041 "bad` + '\t" "';
          const suffix = String.raw`" after D:\new\report.txt \\server\share \u0042 \\u006fpaque`;
          const content = prefix + encodedMask + suffix;
          expect(JSON.parse(redactArtifactMaskedValues(content, [mask]))).toBe(prefix + "***" + suffix);
          expect(redactArtifactMaskedValues(content, ["absent"])).toBe(content);
        });

        it("preserves unmatched escapes between multiple conservative matches and overlapping candidates", () => {
          const content = '"bad\t" ' + String.raw`"\u006fpaque" C:\temp\file.txt "\u006fpaque-tail" \\u006fpaque`;
          expect(JSON.parse(redactArtifactMaskedValues(content, ["opaque-tail", "opaque"]))).toBe('"bad\t" ' + String.raw`"***" C:\temp\file.txt "***" \\u006fpaque`);
        });

        it("does not recursively decode literal Unicode escapes after malformed prefixes", () => {
          const content = '"bad\t" ' + JSON.stringify("\\u006fpaque");
          expect(redactArtifactMaskedValues(content, ["opaque"])).toBe(content);
        });

        it.each(["", "plain", '"', '"unterminated', '"ends\\', '"ends\\u', '"ends\\u123', '"bad\\q', '""', '"" ""', '"safe"tail', '"safe" "unterminated'])("preserves unmatched or truncated input byte-for-byte (%j)", content => {
          expect(redactArtifactMaskedValues(content, ["absent"])).toBe(content);
        });

        it.each(['"unterminated opaque', '"ends opaque\\', '"bad\\q opaque', 'opaque "safe" opaque', '"safe" opaque "unterminated'])("still applies raw masks outside recoverable tokens (%j)", content => {
          expect(redactArtifactMaskedValues(content, ["opaque"])).toBe(content.replaceAll("opaque", "***"));
        });

        it("handles adjacent strings, empty strings, invalid prefixes, and trailing text together", () => {
          const content = `start ""${encodedMask}""${encodedMask} "bad\\q ${encodedMask} trailing`;
          const redactedToken = JSON.stringify("***");
          expect(redactArtifactMaskedValues(content, ["opaque"])).toBe(`start ""${redactedToken}""${redactedToken} "bad\\q ${redactedToken} trailing`);
        });

        it("keeps literal escaped Unicode distinct from an actual encoded mask", () => {
          const literal = JSON.stringify("\\u006fpaque");
          expect(redactArtifactMaskedValues(`log ${literal} ${encodedMask}`, ["opaque"])).toBe(`log ${literal} "***"`);
        });

        it("keeps numeric-looking string tokens separate from original numeric tokens", () => {
          const content = ' \t{"escaped":"\\u0031e400","quoted":"9007199254740993","ids":[1e400,9007199254740993,-0,1e+09]}\r\n';
          const redacted = redactArtifactMaskedValues(content, ["1e400", "9007199254740993"]);
          expect(JSON.parse(redacted)).toEqual({ escaped: "***", quoted: "***", ids: ["***", "***", 0, 1000000000] });
          expect(redacted.startsWith(" \t")).toBe(true);
          expect(redacted.endsWith("\r\n")).toBe(true);
        });

        it("matches a deterministic decoded-string oracle across generated escape combinations", () => {
          const alphabet = ["a", '"', "\\", "/", "\b", "\f", "\n", "\r", "\t", "\u0000", "\u001f", "\u2028", "\ud800", "\ud83d\ude80"];
          for (let index = 0; index < 256; index++) {
            const left = alphabet[index % alphabet.length];
            const right = alphabet[Math.floor(index / alphabet.length) % alphabet.length];
            const value = left + right + "opaque" + right + left;
            const encoded = JSON.stringify(value);
            const expected = JSON.stringify(value.replaceAll("opaque", "***"));
            expect(redactArtifactMaskedValues(`record ${encoded} end`, ["opaque"]), `generated record ${index}`).toBe(`record ${expected} end`);
            const untouched = `record ${encoded} end`;
            expect(redactArtifactMaskedValues(untouched, ["absent"]), `unchanged record ${index}`).toBe(untouched);
          }
        });

        it("redacts thousands of adjacent tokens without losing delimiters or unchanged bytes", () => {
          const safe = '"safe\\\\\\"value"';
          const record = `prefix ${safe}:${encodedMask}:`;
          expect(redactArtifactMaskedValues(record.repeat(4096) + "tail", ["opaque"])).toBe(`prefix ${safe}:"***":`.repeat(4096) + "tail");
        });
      });

      it.each(["json", "mixed"])("redacts multi-megabyte %s records without exhausting the regex stack", shape => {
        const mask = 'synthetic-"quoted"\\mask';
        const value = "x".repeat(10 * 1024 * 1024) + mask;
        const record = JSON.stringify({ text: value, number: shape === "mixed" ? 42 : 12345678 });
        const prefix = shape === "mixed" ? "tool output: " : "";
        const redacted = redactArtifactMaskedValues(prefix + record, [mask, "12345678"]);
        expect(redacted.includes(mask)).toBe(false);
        expect(redacted.includes(JSON.stringify(mask).slice(1, -1))).toBe(false);
        const parsed = JSON.parse(redacted.slice(prefix.length));
        expect(parsed.text.length).toBe(10 * 1024 * 1024 + 3);
        expect(parsed.text.endsWith("***")).toBe(true);
        expect(parsed.number).toBe(shape === "mixed" ? 42 : "***");
      });

      it("handles many escaped quotes and both odd and even backslash runs", () => {
        const mask = 'synthetic-"quoted"\\mask';
        const value = '\\"'.repeat(1024 * 1024) + mask + "\\\\";
        const record = JSON.stringify({ text: value, safe: "12345678" });
        const redacted = redactArtifactMaskedValues(record, [mask]);
        const parsed = JSON.parse(redacted);
        expect(parsed.text.length).toBe(2 * 1024 * 1024 + 5);
        expect(parsed.text.endsWith("***\\\\")).toBe(true);
        expect(parsed.safe).toBe("12345678");
      });

      it.each([false, true])("redacts a multi-megabyte malformed escaped-quote record without rescanning its rejected prefix (explicit selector %j)", selector => {
        const prefix = '"' + '\\"'.repeat(1024 * 1024);
        const suffix = String.raw`\q "safe" C:\temp\file.txt`;
        const content = prefix + String.raw`\u006fpaque` + suffix;
        const replacement = selector ? () => "***" : undefined;
        expect(JSON.parse(redactArtifactMaskedValues(content, ["opaque"], undefined, replacement))).toBe(prefix + "***" + suffix);
        expect(redactArtifactMaskedValues(content, ["absent"], undefined, replacement)).toBe(content);
      });

      it.each(["\n", "\r", "\u2028", "\u2029"])("recovers encoded masks after an invalid escaped line terminator", terminator => {
        const mask = 'synthetic-"quoted"\\mask';
        const prefix = '"invalid\\' + terminator;
        expect(redactArtifactMaskedValues(prefix + JSON.stringify(mask), [mask])).toBe(prefix + '"***"');
      });

      it.each(['"bad\\q ', '"bad\\u12gg ', '"bad\\u12 ', '"bad\u0001 ', '"bad\t ', '"bad\\v '])("recovers Unicode-encoded masks after invalid JSON string prefixes (%j)", prefix => {
        expect(redactArtifactMaskedValues(prefix + '"\\u006fpaque"', ["opaque"])).toBe(prefix + '"***"');
      });

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

      it.each(["12345678", "-12345678", "12345678.5", "1.12345678e+9", "12345678901234567890"])("redacts numeric scalar %s using its original token and keeps JSON valid", token => {
        const redacted = redactArtifactMaskedValues(` \t${token}\r\n`, ["12345678"]);
        expect(redacted).toBe(` \t${JSON.stringify(token.replaceAll("12345678", "***"))}\r\n`);
        expect(JSON.parse(redacted)).toBe(token.replaceAll("12345678", "***"));
      });

      it("redacts numeric scalar records within mixed logs and JSONL", () => {
        const content = 'starting\n12345678\n{"data":"12345678"}\n-12345678\n123456\nfalse\nnull\n';
        const redacted = redactArtifactMaskedValues(content, ["12345678"]);
        expect(redacted).toBe('starting\n"***"\n{"data":"***"}\n"-***"\n123456\nfalse\nnull\n');
        for (const line of redacted.trim().split("\n").slice(1)) expect(() => JSON.parse(line)).not.toThrow();
      });

      it.each(["12345678", "-12345678", "12345678.5", "1.12345678e+9", "12345678901234567890"])("redacts nested numeric token %s without numeric normalization", token => {
        const content = ` \t{"token":${token},"ids":[${token},42],"nested":{"items":[{"token":${token}}]}}\r\n`;
        const redacted = redactArtifactMaskedValues(content, ["12345678"]);
        const masked = token.replaceAll("12345678", "***");
        expect(JSON.parse(redacted)).toEqual({ token: masked, ids: [masked, 42], nested: { items: [{ token: masked }] } });
        expect(redacted).not.toContain("12345678");
        expect(redacted.startsWith(" \t")).toBe(true);
        expect(redacted.endsWith("\r\n")).toBe(true);
      });

      it.each(["9007199254740993", "123456789012345678901234567890", "1e400"])("redacts an exact nested numeric credential %s before precision loss", token => {
        const content = `{"ids":[${token}],"text":"safe"}`;
        expect(JSON.parse(redactArtifactMaskedValues(content, [token]))).toEqual({ ids: ["***"], text: "safe" });
      });

      it("redacts nested numeric records in JSONL and mixed logs", () => {
        const records = '{"token":12345678,"safe":42}\n[42,{"ids":[12345678901234567890]}]\n{"nested":{"token":1.12345678e+9}}\n';
        const expected = [{ token: "***", safe: 42 }, [42, { ids: ["***90***90"] }], { nested: { token: "1.***e+9" } }];
        for (const prefix of ["", "::add-mask::12345678\nstarting\n"]) {
          const redacted = redactArtifactMaskedValues(prefix + records, ["12345678"]);
          expect(redacted).not.toContain("12345678");
          const lines = redacted.trim().split("\n");
          if (prefix) expect(lines.shift()).toBe("starting");
          expect(lines.map(line => JSON.parse(line))).toEqual(expected);
        }
      });

      it.each(["\u00a0", "\ufeff", "\f"])("preserves trimmed record framing while redacting numeric masks (%j)", whitespace => {
        for (const terminator of ["\n", "\r\n", ""]) {
          const content = `${whitespace}{"pin":123456,"text":"safe"}${whitespace}${terminator}`;
          expect(redactArtifactMaskedValues(content, ["123456"])).toBe(`${whitespace}{"pin":"***","text":"safe"}${whitespace}${terminator}`);
          expect(redactArtifactMaskedValues(content, ["123456"], undefined, () => "[MASK]")).toBe(`${whitespace}{"pin":"[MASK]","text":"safe"}${whitespace}${terminator}`);
          expect(redactArtifactMaskedValues(content, ["123456"], value => ({ ...value, text: "sanitized" }))).toBe(`${whitespace}{"pin":"***","text":"sanitized"}${whitespace}${terminator}`);
          expect(redactArtifactMaskedValues(content, ["ABSENT_RUNTIME_MASK"])).toBe(content);
        }
      });

      it("does not match normalized numeric representations or alter unmatched nested records", () => {
        const content = '  {"values":[-0,1e+09,9007199254740993,1e400],"escaped":"quote\\"12345","safe":false}\r\n';
        expect(redactArtifactMaskedValues(content, ["12345678", "1000000000", "9007199254740992", "Infinity"])).toBe(content);
      });

      it("preserves nonmatching numeric tokens alongside matching nested numbers", () => {
        const content = '  {"values":[12345678,-0,1e+09,9007199254740993,1e400]}\r\n';
        expect(redactArtifactMaskedValues(content, ["12345678"])).toBe('  {"values":["***",-0,1e+09,9007199254740993,1e400]}\r\n');
      });

      it("keeps matching quoted numeric strings and numeric keys valid", () => {
        const content = '{"12345678":"quote\\"12345678","values":[12345678,123456]}';
        expect(JSON.parse(redactArtifactMaskedValues(content, ["12345678"]))).toEqual({ "***": 'quote"***', values: ["***", 123456] });
      });

      it.each(["123456", "-0", "1e+09", "9007199254740993", "1e400", "false", "null"])("preserves unmatched scalar %s byte-for-byte", token => {
        const content = ` \t${token}\r\n`;
        expect(redactArtifactMaskedValues(content, ["12345678"])).toBe(content);
      });

      it("redacts JSONL, Unicode escapes, and malformed adjacent records", () => {
        const content = '::add-mask::opaque\n{"data":"\\u006fpaque"}\n{"broken":"\\u006fpaque",\nplain opaque\n{"data":"safe"}\n';
        const redacted = redactArtifactMaskedValues(content, ["opaque"]);
        expect(redacted).toBe('{"data":"***"}\n{"broken":"***",\nplain ***\n{"data":"safe"}\n');
      });

      it("preserves blank lines, mixed line endings and unterminated records while redacting events", () => {
        const content = '::add-mask::opaque\r\n\r\n {"data":"safe"} \r\n{"data":"\\u006fpaque"}\r\n{"broken":"\\u006fpaque",\nplain opaque\r\n \t\r\nlast opaque';
        expect(redactArtifactMaskedValues(content, ["opaque"])).toBe('\r\n {"data":"safe"} \r\n{"data":"***"}\r\n{"broken":"***",\nplain ***\r\n \t\r\nlast ***');
      });

      it("redacts multi-megabyte valid and invalid adjacent event lines", () => {
        const padding = "x".repeat(3 * 1024 * 1024);
        const content = `{"data":"${padding}\\u006fpaque"}\r\n{"broken":"${padding}\\u006fpaque",\nplain opaque`;
        expect(redactArtifactMaskedValues(content, ["opaque"])).toBe(`{"data":"${padding}***"}\r\n{"broken":"${padding}***",\nplain ***`);
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
