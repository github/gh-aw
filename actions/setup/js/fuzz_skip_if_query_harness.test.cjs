// @ts-check
/**
 * Property-based fuzz tests for the skip-if search query parser.
 * Invariants live in fuzz_skip_if_query_harness.cjs and are shared with the Go fuzzer
 * (pkg/workflow/skip_if_query_fuzz_test.go).
 */
import { describe, it, expect } from "vitest";

const { checkSkipIfQueryInvariants, buildModelQuery, FRAGMENTS } = require("./fuzz_skip_if_query_harness.cjs");
const { parseSearchQuery } = require("./skip_if_query.cjs");

/**
 * Deterministic mulberry32 PRNG so failures are reproducible.
 * @param {number} seed
 */
function prng(seed) {
  return () => {
    seed = (seed + 0x6d2b79f5) | 0;
    let t = Math.imul(seed ^ (seed >>> 15), 1 | seed);
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

const PAYLOADS = ["", "x", "octo/demo", "a,b", '"', '"quoted"', "-neg", "with space", "日本語", "\u00a0", "a:b", "@me", "Bug"];

describe("fuzz_skip_if_query - seed corpus", () => {
  it.each([
    "is:issue is:open label:bug repo:octo/demo",
    'is:pr is:merged label:"needs triage" repo:octo/demo sort:created-desc',
    "label:a label:b assignee:octocat repo:octo/demo",
    'is:issue in:title "[daily-report]" repo:octo/demo',
    'label:"a b"c repo:octo/demo',
    'label:a"b c" repo:octo/demo',
    "repo:octo/demo repo:Octo/Demo author:A author:a",
    "is:issue is:merged repo:octo/demo",
    "",
    '"',
    "\t\n ",
  ])("holds invariants for %j", query => {
    expect(checkSkipIfQueryInvariants(query)).toEqual([]);
  });

  it("rejects values with partial quoting so they fall back to search", () => {
    expect(parseSearchQuery('label:"a b"c repo:octo/demo').ok).toBe(false);
    expect(parseSearchQuery('label:a"b c" repo:octo/demo').ok).toBe(false);
  });
});

describe("fuzz_skip_if_query - generated queries", () => {
  it("holds invariants for grammar-generated queries", () => {
    const rand = prng(0x5eed);
    let accepted = 0;
    for (let i = 0; i < 3000; i++) {
      const bytes = Array.from({ length: 1 + Math.floor(rand() * 8) }, () => Math.floor(rand() * 256));
      const payload = PAYLOADS[Math.floor(rand() * PAYLOADS.length)];
      // Most generated queries need a repo qualifier to exercise the accepted path.
      const query = `${buildModelQuery(bytes, payload)}${rand() < 0.7 ? " repo:octo/demo" : ""}`;
      const violations = checkSkipIfQueryInvariants(query);
      if (violations.length) throw new Error(`invariants failed for ${JSON.stringify(query)}: ${violations.join("; ")}`);
      if (parseSearchQuery(query).ok) accepted++;
    }
    // Guard against a generator that only produces rejected queries.
    expect(accepted).toBeGreaterThan(100);
  });

  it("holds invariants for random character soup", () => {
    const rand = prng(42);
    const alphabet = 'abc:/-," \t@isrepolabe';
    for (let i = 0; i < 2000; i++) {
      const length = Math.floor(rand() * 40);
      const query = Array.from({ length }, () => alphabet[Math.floor(rand() * alphabet.length)]).join("");
      const violations = checkSkipIfQueryInvariants(query);
      if (violations.length) throw new Error(`invariants failed for ${JSON.stringify(query)}: ${violations.join("; ")}`);
    }
  });

  it("covers every fragment in the generator vocabulary", () => {
    for (const fragment of FRAGMENTS) {
      expect(checkSkipIfQueryInvariants(`${fragment} repo:octo/demo`)).toEqual([]);
    }
  });
});
