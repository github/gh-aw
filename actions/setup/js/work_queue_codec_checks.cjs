"use strict";

const assert = require("node:assert/strict");
const { MAX_PARSE_BYTES, MAX_SNAPSHOT_PARSE_BYTES, canonical, canonicalBytes, fingerprint, parseStrictJSON, utf8Compare, validateReason } = require("./work_queue_codec.cjs");

function registerTests({ describe, it }) {
  describe("work queue canonical codec", () => {
    it("sorts object members by UTF-8 bytes, independently of insertion and Unicode UTF-16 order", () => {
      const a = { "\u{10000}": "𐀀", "\uE000": "", "2": 2, "10": 10, "<>&": "<>&", __proto__: null };
      const b = Object.fromEntries(Object.entries(a).reverse());
      assert.equal(canonical(a), canonical(b));
      assert.equal(canonical(a), '{"10":10,"2":2,"<>&":"<>&","":"","𐀀":"𐀀"}');
      assert.equal(utf8Compare("\uE000", "\u{10000}"), -1);
      assert.equal(canonicalBytes("é"), 4);
    });
    it("rejects duplicate decoded keys, unsafe numeric IDs, invalid Unicode, and unknown JSON values", () => {
      for (const input of ['{"x":1,"x":2}', '{"x":1,"\\u0078":2}', '{"x":{"a":1,"a":1}}', "9007199254740992", "1.0", "1e0", "-0", '"\\uD800"', "[1,]", '{"a":1,}']) {
        assert.throws(() => parseStrictJSON(input), /codec_invalid|duplicate_key|noncanonical_number|invalid_unicode/);
      }
      for (const input of [undefined, NaN, Infinity, 1.1, 9007199254740992, 1n, "\uD800", new Date()]) {
        assert.throws(() => canonical(input), /codec_invalid|noncanonical_number|invalid_unicode/);
      }
      assert.equal(canonical(parseStrictJSON('{"__proto__":{},"value":0}')), '{"__proto__":{},"value":0}');
    });
    it("preserves independently specified codec rejection codes", () => {
      const fixture = require("../../../specs/work-queue/fixtures/canonical.json");
      for (const test of fixture.error_codes) {
        assert.throws(() => parseStrictJSON(test.input), { code: test.code }, test.name);
      }
      for (const value of ["\uD800", "\uDC00"]) {
        assert.throws(() => parseStrictJSON(`"${value}"`), { code: "invalid_unicode" });
        assert.throws(() => canonical(value), { code: "invalid_unicode" });
      }
      for (const input of ['"\\uZZZZ"', '"\\x00"', '"unterminated']) {
        assert.throws(() => parseStrictJSON(input), { code: "codec_invalid" });
      }
    });
    it("rejects typed nonintegral, unsafe and negative-zero values without changing them", () => {
      const fixture = require("../../../specs/work-queue/fixtures/canonical.json");
      for (const test of fixture.typed_number_rejections) {
        assert.throws(() => canonical(JSON.parse(test.input)), { code: test.code }, test.name);
      }
    });
    it("binds fingerprints to stable actor, kind, and semantic parameters, never branch state", () => {
      const actor = { role: "dispatcher", principal: "approved" };
      const parameters = { pool: "default", max_claims: 1, max_dispatches: 1 };
      const hash = fingerprint(actor, "dispatch_next", parameters);
      assert.match(hash, /^[a-f0-9]{64}$/);
      assert.equal(hash, fingerprint({ principal: "approved", role: "dispatcher" }, "dispatch_next", { max_dispatches: 1, max_claims: 1, pool: "default" }));
      assert.notEqual(hash, fingerprint(actor, "dispatch_next", { ...parameters, max_claims: 2 }));
    });
    it("bounds JSON nesting before unbounded recursion", () => {
      assert.throws(() => parseStrictJSON("[".repeat(66) + "0" + "]".repeat(66)), /resource_limit/);
    });
    it("permits bounded snapshot framing above 80 MiB without enlarging default wire parsing", () => {
      assert.equal(MAX_PARSE_BYTES, 80 * 1024 * 1024);
      assert.equal(MAX_SNAPSHOT_PARSE_BYTES, 161 * 1024 * 1024);
      const decodedLength = (MAX_PARSE_BYTES - 2) / 2;
      const transactionLog = `"${"\\\\".repeat(decodedLength)}"`;
      const frame = `{"transactionLog":${JSON.stringify(transactionLog)}}`;
      assert.equal(Buffer.byteLength(transactionLog), MAX_PARSE_BYTES);
      assert.ok(Buffer.byteLength(frame) > 2 * MAX_PARSE_BYTES);
      assert.ok(Buffer.byteLength(frame) <= MAX_SNAPSHOT_PARSE_BYTES);
      assert.throws(() => parseStrictJSON(frame), { code: "resource_limit" });
      const snapshot = parseStrictJSON(frame, { maxBytes: MAX_SNAPSHOT_PARSE_BYTES });
      assert.equal(snapshot.transactionLog, transactionLog);
      assert.equal(parseStrictJSON(snapshot.transactionLog).length, decodedLength);
      assert.throws(() => parseStrictJSON(frame), { code: "resource_limit" });
    });
    it("validates framing overrides and counts UTF-8 bytes at the exact boundary", () => {
      assert.equal(parseStrictJSON('"é"', { maxBytes: 4 }), "é");
      assert.throws(() => parseStrictJSON('"é"', { maxBytes: 3 }), { code: "resource_limit" });
      for (const maxBytes of [0, -1, 1.5, NaN, Infinity, null, "4", MAX_SNAPSHOT_PARSE_BYTES + 1]) {
        assert.throws(() => parseStrictJSON("null", { maxBytes }), { code: "resource_limit" });
      }
      assert.equal(parseStrictJSON("null", { maxBytes: MAX_SNAPSHOT_PARSE_BYTES }), null);
    });
    it("retains every strict codec guard with the larger framing limit", () => {
      const options = { maxBytes: MAX_SNAPSHOT_PARSE_BYTES };
      for (const test of require("../../../specs/work-queue/fixtures/canonical.json").error_codes) {
        assert.throws(() => parseStrictJSON(test.input, options), { code: test.code }, test.name);
      }
      assert.throws(() => parseStrictJSON("[".repeat(66) + "0" + "]".repeat(66), options), { code: "resource_limit" });
      assert.throws(() => parseStrictJSON(`[${"0,".repeat(16384)}0]`, options), { code: "resource_limit" });
      assert.equal(canonical(parseStrictJSON('{"__proto__":{},"value":0}', options)), '{"__proto__":{},"value":0}');
    });
    it("accepts only bounded sanitized reason codes without trailing newline anchor loopholes", () => {
      for (const reason of ["A", "0", "code_with.punctuation:and-hyphens", "x".repeat(128)]) assert.equal(validateReason(reason), reason);
      for (const reason of ["", "x".repeat(129), "retained-control-" + "x".repeat(220), "leading space", ".leading", "_leading", ":leading", "-leading", "é", "code\n", "code\r", "code\t", "code\u0000", undefined, 1])
        assert.throws(() => validateReason(reason), /reason_invalid/);
    });
  });
}

if (require.main === module) registerTests(require("node:test"));
module.exports = { registerTests };
