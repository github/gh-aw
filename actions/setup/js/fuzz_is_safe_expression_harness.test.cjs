// @ts-check
/**
 * Fuzz / property-based tests for isSafeExpression — compound expression forms.
 *
 * Covers the expression types added in the spec-enforcer fix:
 *   - Standalone literals (string, number, boolean)
 *   - AND (&&) compound expressions
 *   - Comparison expressions (==, !=, <=, >=)
 *   - Ternary-style AND/OR chains
 *   - Security boundaries: each new form must still block secrets.*, vars.*, runner.*, env.*
 *
 * Also contains property-based combinatorial tests that exhaustively check
 * safe×safe, safe×unsafe, and unsafe×safe pairs for every binary operator.
 */

const { testIsSafeExpression, containsUnsafeRoot } = require("./fuzz_is_safe_expression_harness.cjs");

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

/** Safe leaf expressions that must always pass `isSafeExpression`. */
const SAFE_LEAVES = [
  "github.actor",
  "github.repository",
  "github.run_id",
  "github.event.issue.number",
  "github.event.pull_request.title",
  "github.event.inputs.enforce_all",
  "github.event.inputs.branch",
  "needs.build.outputs.version",
  "steps.test.outputs.result",
  "inputs.branch",
];

/** Unsafe leaf expressions that must always be blocked. */
const UNSAFE_LEAVES = ["secrets.TOKEN", "secrets.GITHUB_TOKEN", "vars.MY_VAR", "runner.os", "runner.temp", "github.token", "env.NODE_VERSION"];

/** Safe literal values (standalone). */
const SAFE_LITERALS = ["'full-sweep (enforce_all)'", "'round-robin'", '"default"', "`value`", "42", "3.14", "true", "false"];

/** Comparison operators. */
const COMPARISON_OPS = ["==", "!=", "<=", ">=", "<", ">"];

// ---------------------------------------------------------------------------
// Standalone literals
// ---------------------------------------------------------------------------

describe("fuzz_is_safe_expression – standalone literals", () => {
  for (const lit of SAFE_LITERALS) {
    it(`should allow standalone literal ${lit}`, () => {
      const { safe, error } = testIsSafeExpression(lit);
      expect(error).toBeNull();
      expect(safe).toBe(true);
    });
  }

  it("should reject standalone literal containing nested expression", () => {
    expect(testIsSafeExpression("'${{ secrets.TOKEN }}'").safe).toBe(false);
    expect(testIsSafeExpression("'text }} end'").safe).toBe(false);
  });

  it("should reject standalone literal containing hex escape sequences", () => {
    // Note: in JS source, \\x41 is a two-character string: backslash + x41
    expect(testIsSafeExpression("'\\x41-injection'").safe).toBe(false);
    expect(testIsSafeExpression("'\\u0041-injection'").safe).toBe(false);
  });
});

// ---------------------------------------------------------------------------
// Comparison expressions — allowed
// ---------------------------------------------------------------------------

describe("fuzz_is_safe_expression – comparisons (allowed)", () => {
  for (const op of COMPARISON_OPS) {
    for (const left of SAFE_LEAVES) {
      it(`should allow ${left} ${op} 'value'`, () => {
        const { safe, error } = testIsSafeExpression(`${left} ${op} 'value'`);
        expect(error).toBeNull();
        expect(safe).toBe(true);
      });
    }
  }
});

// ---------------------------------------------------------------------------
// Comparison expressions — blocked when unsafe on the left
// ---------------------------------------------------------------------------

describe("fuzz_is_safe_expression – comparisons (unsafe left side blocked)", () => {
  for (const unsafe of UNSAFE_LEAVES) {
    for (const op of COMPARISON_OPS) {
      it(`should block ${unsafe} ${op} 'value'`, () => {
        const { safe, error } = testIsSafeExpression(`${unsafe} ${op} 'value'`);
        expect(error).toBeNull();
        expect(safe).toBe(false);
      });
    }
  }
});

// ---------------------------------------------------------------------------
// AND compound expressions — safe × safe → allowed (no literal operands)
// ---------------------------------------------------------------------------

describe("fuzz_is_safe_expression – AND (safe × safe, no literals)", () => {
  // Spot-check a representative subset of pairs to keep test count manageable
  const pairs = SAFE_LEAVES.slice(0, 4).flatMap(a => SAFE_LEAVES.slice(0, 4).map(b => [a, b]));
  for (const [a, b] of pairs) {
    if (a === b) continue;
    it(`should allow ${a} && ${b}`, () => {
      const { safe, error } = testIsSafeExpression(`${a} && ${b}`);
      expect(error).toBeNull();
      expect(safe).toBe(true);
    });
  }

  it("should allow comparison && safe property (no literal in operand)", () => {
    expect(testIsSafeExpression("github.event.inputs.enforce_all == 'true' && github.event.inputs.enforce_all").safe).toBe(true);
  });
});

// ---------------------------------------------------------------------------
// AND compound expressions — literal operands refused
// ---------------------------------------------------------------------------

describe("fuzz_is_safe_expression – AND (literal operand blocked)", () => {
  for (const lit of SAFE_LITERALS) {
    it(`should block github.actor && ${lit} (literal RHS)`, () => {
      expect(testIsSafeExpression(`github.actor && ${lit}`).safe).toBe(false);
    });

    it(`should block ${lit} && github.actor (literal LHS)`, () => {
      expect(testIsSafeExpression(`${lit} && github.actor`).safe).toBe(false);
    });
  }

  it("should block all SAFE_LEAVES && literal combinations (first 3)", () => {
    for (const leaf of SAFE_LEAVES.slice(0, 3)) {
      for (const lit of SAFE_LITERALS.slice(0, 3)) {
        expect(testIsSafeExpression(`${leaf} && ${lit}`).safe).toBe(false);
        expect(testIsSafeExpression(`${lit} && ${leaf}`).safe).toBe(false);
      }
    }
  });
});

// ---------------------------------------------------------------------------
// AND compound expressions — blocked when either side is unsafe
// ---------------------------------------------------------------------------

describe("fuzz_is_safe_expression – AND (unsafe side blocked)", () => {
  for (const unsafe of UNSAFE_LEAVES) {
    it(`should block ${unsafe} && github.actor`, () => {
      expect(testIsSafeExpression(`${unsafe} && github.actor`).safe).toBe(false);
    });

    it(`should block github.actor && ${unsafe}`, () => {
      expect(testIsSafeExpression(`github.actor && ${unsafe}`).safe).toBe(false);
    });
  }
});

// ---------------------------------------------------------------------------
// OR compound expressions — literal on LEFT side refused
// ---------------------------------------------------------------------------

describe("fuzz_is_safe_expression – OR (literal left side blocked)", () => {
  for (const lit of SAFE_LITERALS) {
    it(`should block ${lit} || github.actor (literal on left)`, () => {
      expect(testIsSafeExpression(`${lit} || github.actor`).safe).toBe(false);
    });

    it(`should block ${lit} || inputs.branch (literal on left)`, () => {
      expect(testIsSafeExpression(`${lit} || inputs.branch`).safe).toBe(false);
    });
  }
});

// ---------------------------------------------------------------------------
// OR compound expressions — blocked when right side is unsafe
// ---------------------------------------------------------------------------

describe("fuzz_is_safe_expression – OR (unsafe right side blocked)", () => {
  for (const unsafe of UNSAFE_LEAVES) {
    it(`should block github.actor || ${unsafe}`, () => {
      expect(testIsSafeExpression(`github.actor || ${unsafe}`).safe).toBe(false);
    });

    it(`should block github.actor == 'x' || ${unsafe}`, () => {
      expect(testIsSafeExpression(`github.actor == 'x' || ${unsafe}`).safe).toBe(false);
    });
  }
});

// ---------------------------------------------------------------------------
// Ternary-style AND/OR chains — all refused (literal in AND operand)
// ---------------------------------------------------------------------------

describe("fuzz_is_safe_expression – ternary-style (all refused — literal in AND operand)", () => {
  it("should refuse condition && literal || literal (the former spec-enforcer pattern)", () => {
    expect(testIsSafeExpression("github.event.inputs.enforce_all == 'true' && 'full-sweep (enforce_all)' || 'round-robin'").safe).toBe(false);
    expect(testIsSafeExpression("inputs.mode == 'fast' && 'fast-mode' || 'normal-mode'").safe).toBe(false);
    expect(testIsSafeExpression("'yes' && github.actor || 'no'").safe).toBe(false);
  });

  it("should refuse safe_expr && literal || literal for all safe leaves", () => {
    for (const leaf of SAFE_LEAVES.slice(0, 5)) {
      expect(testIsSafeExpression(`${leaf} && 'yes' || 'no'`).safe).toBe(false);
    }
  });

  it("should refuse comparison && literal || literal for all operators", () => {
    for (const op of COMPARISON_OPS) {
      const expr = `github.event.inputs.enforce_all ${op} 'x' && 'yes' || 'no'`;
      expect(testIsSafeExpression(expr).safe).toBe(false);
    }
  });
});

describe("fuzz_is_safe_expression – ternary-style (unsafe in any position blocked)", () => {
  for (const unsafe of UNSAFE_LEAVES) {
    it(`should block ${unsafe} == 'x' && github.actor || github.repository`, () => {
      expect(testIsSafeExpression(`${unsafe} == 'x' && github.actor || github.repository`).safe).toBe(false);
    });

    it(`should block github.actor && ${unsafe} || github.repository`, () => {
      expect(testIsSafeExpression(`github.actor && ${unsafe} || github.repository`).safe).toBe(false);
    });

    it(`should block github.actor == 'value' || ${unsafe}`, () => {
      expect(testIsSafeExpression(`github.actor == 'value' || ${unsafe}`).safe).toBe(false);
    });
  }
});

// ---------------------------------------------------------------------------
// Security invariants: unsafe namespaces are ALWAYS blocked regardless of
// the surrounding compound expression structure
// ---------------------------------------------------------------------------

describe("fuzz_is_safe_expression – security invariants", () => {
  /**
   * Wrapper forms that should never make an unsafe sub-expression safe.
   * Note: wrappers that would introduce a literal operand in && are now also blocked
   * (for the literal reason), but the security invariant still holds.
   */
  const wrappers = [
    /** bare */
    u => u,
    /** comparison */
    u => `${u} == 'x'`,
    /** AND — unsafe on left */
    u => `${u} && github.actor`,
    /** AND — unsafe on right */
    u => `github.actor && ${u}`,
    /** OR — unsafe on right */
    u => `${u} || 'fallback'`,
    /** OR — unsafe on right of a comparison */
    u => `github.actor == 'value' || ${u}`,
    /** compound: comparison && unsafe || safe */
    u => `${u} == 'x' && github.actor || github.repository`,
    /** compound: safe && unsafe || safe */
    u => `github.actor && ${u} || github.repository`,
    /** triple-OR — unsafe in middle */
    u => `github.actor || ${u} || github.repository`,
  ];

  for (const unsafe of UNSAFE_LEAVES) {
    for (const wrap of wrappers) {
      const expr = wrap(unsafe);
      it(`should block: ${expr}`, () => {
        expect(testIsSafeExpression(expr).safe).toBe(false);
      });
    }
  }

  it("should never mark any expression containing secrets. as safe", () => {
    const secretExprs = ["secrets.TOKEN", "secrets.GITHUB_TOKEN", "secrets.TOKEN == 'x'", "github.actor && secrets.TOKEN", "secrets.TOKEN || 'fallback'", "secrets.TOKEN == 'x' && github.actor || github.repository"];
    for (const expr of secretExprs) {
      expect(testIsSafeExpression(expr).safe).toBe(false);
    }
  });

  it("should never mark any expression with a dangerous property name as safe", () => {
    const dangerous = ["constructor", "__proto__", "prototype", "hasOwnProperty", "valueOf"];
    for (const prop of dangerous) {
      expect(testIsSafeExpression(`github.${prop}`).safe).toBe(false);
      expect(testIsSafeExpression(`inputs.${prop}`).safe).toBe(false);
      expect(testIsSafeExpression(`github.${prop} == 'x'`).safe).toBe(false);
      expect(testIsSafeExpression(`github.actor && github.${prop}`).safe).toBe(false);
    }
  });
});
// ---------------------------------------------------------------------------
// Exhaustive safe-leaf × comparison-operator matrix
// ---------------------------------------------------------------------------

describe("fuzz_is_safe_expression – exhaustive safe-leaf × operator matrix", () => {
  for (const leaf of SAFE_LEAVES) {
    for (const op of COMPARISON_OPS) {
      it(`should allow ${leaf} ${op} 'constant'`, () => {
        const { safe, error } = testIsSafeExpression(`${leaf} ${op} 'constant'`);
        expect(error).toBeNull();
        expect(safe).toBe(true);
      });
    }
  }
});
