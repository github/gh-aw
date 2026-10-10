import assert from "node:assert/strict";

// Only the finite values emitted by Factory.tla are accepted, not arbitrary TLA.
export function parseValue(text) {
  let position = 0;
  const skip = () => {
    while (/\s/.test(text[position] || "") && position < text.length) position++;
  };
  function value() {
    skip();
    const sequence = text.startsWith("<<", position);
    const set = text[position] === "{";
    if (sequence || set) {
      position += sequence ? 2 : 1;
      const close = sequence ? ">>" : "}";
      const result = [];
      skip();
      while (!text.startsWith(close, position)) {
        result.push(value());
        skip();
        if (text[position] === ",") position++;
        else if (!text.startsWith(close, position)) throw new Error(`Unexpected TLA delimiter: ${text.slice(position)}`);
        skip();
      }
      position += close.length;
      return set ? result.sort((a, b) => a - b) : result;
    }
    const match = text.slice(position).match(/^(TRUE|FALSE|-?\d+|"(?:[^"\\]|\\.)*")/);
    assert.ok(match, `Unsupported TLA value: ${text.slice(position)}`);
    position += match[0].length;
    return match[0] === "TRUE" ? true : match[0] === "FALSE" ? false : JSON.parse(match[0]);
  }
  const result = value();
  skip();
  assert.equal(position, text.length, `Trailing TLA value: ${text}`);
  return result;
}

export function parseTrace(log) {
  return [...log.matchAll(/^State (\d+): ([^\n]+)\n([\s\S]*?)(?=^State \d+:|^\d[\d,]* states generated|^Progress|^The depth|^Finished|$(?![\s\S]))/gm)].map(match => {
    const state = {};
    for (const variable of match[3].matchAll(/^\/\\ (\w+) = ([\s\S]*?)(?=^\/\\ |$(?![\s\S]))/gm)) {
      state[variable[1]] = parseValue(variable[2].trim());
    }
    return { number: Number(match[1]), action: match[2], state };
  });
}

export function violations(s, c) {
  const failed = [];
  const check = (name, condition) => {
    if (!condition) failed.push(name);
  };
  const member = k => s.birth.includes(k + 1);
  const profile = k => (c.Worker === "mixed" ? (k === 0 ? "miner" : "monster") : c.Worker);
  check("TypeOK", s.phase.length === 2 && s.proposed.every(n => Number.isInteger(n) && n >= -1 && n <= c.OutputMax));
  check("ImmutableMembership", JSON.stringify(s.original) === JSON.stringify(s.birth));
  check("HomogeneousBatch", s.birth.length < 2 || profile(0) === profile(1));
  check("PolicyRequired", s.original.length === 0 || c.Installed);
  check(
    "ScopedIntents",
    s.proposed.every((n, k) => n < 0 || s.scoped[k])
  );
  check(
    "EffectRequiresCompletion",
    s.applied.every((n, k) => n <= 0 || s.completedAtWrite[k])
  );
  check(
    "ResultRequiresReadback",
    s.phase.every((p, k) => p !== "result" || s.verified[k])
  );
  check(
    "ResultCardinality",
    s.phase.every((p, k) => p !== "result" || c.NoWrites || (s.proposed[k] >= c.OutputMin && s.proposed[k] <= c.OutputMax))
  );
  check("WarningExitZero", s.scan === "unrun" || s.lintClean === ["clean", "warnings"].includes(s.scan));
  check("NoAutomaticDAG", JSON.stringify(s.admitted) === JSON.stringify([1, 2].filter(k => s.phase[k - 1] !== "absent")));
  check("BoundedAssignment", s.birth.length <= 3);
  const reached = {
    "warning-clean": s.scan === "warnings" && s.lintClean,
    "completion-only": s.phase.some((p, k) => member(k) && p === "completed" && !s.verified[k]),
    "six-outputs": s.birth.length === 2 && s.applied.every(n => n === 3),
    "quality-not-gate": s.quality === "failed" && s.phase.some((p, k) => member(k) && p === "result"),
    "tool-failure": s.scan === "toolFailed" && !s.lintClean,
    result: s.phase.some((p, k) => member(k) && p === "result" && s.verified[k]),
    "no-write": c.NoWrites && s.phase.some((p, k) => member(k) && p === "result" && s.applied[k] === 0),
  };
  check("NeverWitness", !reached[c.Witness]);
  return failed;
}

// Independent executable replay of the finite abstraction. This is a trace
// sanity check, NOT a correspondence proof for the production queue.
export function successors(s, c) {
  const next = [];
  const add = change => {
    const candidate = structuredClone(s);
    change(candidate);
    next.push(candidate);
  };
  for (let k = 0; k < 2; k++) {
    if ((c.Installed || c.Fault === "policy") && s.phase[k] === "absent")
      add(t => {
        t.phase[k] = "queued";
        t.admitted = [...s.admitted, k + 1].sort((a, b) => a - b);
      });
    if (!s.original.includes(k + 1)) continue;
    if (s.phase[k] === "claimed") add(t => (t.phase[k] = "completed"));
    if (["claimed", "completed"].includes(s.phase[k])) add(t => (t.phase[k] = "cancelled"));
    if (s.proposed[k] === -1 && ["claimed", "completed"].includes(s.phase[k])) {
      for (let n = 0; n <= c.OutputMax; n++) {
        if (c.NoWrites && n !== 0) continue;
        for (const explicit of [false, true]) {
          if (!explicit && s.original.length !== 1 && c.Fault !== "scope") continue;
          add(t => {
            t.proposed[k] = n;
            t.scoped[k] = explicit || s.birth.length === 1;
          });
        }
      }
    }
    if (s.proposed[k] >= 0 && s.applied[k] === -1 && (s.phase[k] === "completed" || (c.Fault === "completion" && s.phase[k] === "claimed"))) {
      add(t => {
        t.applied[k] = s.proposed[k];
        t.completedAtWrite[k] = s.phase[k] === "completed";
        if (c.Fault === "dag") t.phase = t.phase.map(p => (p === "absent" ? "queued" : p));
      });
    }
    if (s.phase[k] === "completed" && !s.verified[k] && s.applied[k] === s.proposed[k] && s.proposed[k] >= 0 && (s.proposed[k] >= c.OutputMin || c.NoWrites || c.Fault === "cardinality")) add(t => (t.verified[k] = true));
    if (s.phase[k] === "completed" && (s.verified[k] || c.Fault === "result")) add(t => (t.phase[k] = "result"));
  }
  if (!s.requested && (c.Installed || c.Fault === "policy")) {
    const eligible = [1, 2].filter(k => s.phase[k - 1] === "queued");
    const prefix = c.Batch && eligible.length === 2 && (c.Worker !== "mixed" || c.Fault === "mixed") ? eligible : eligible.slice(0, 1);
    add(t => {
      t.original = [...prefix];
      t.birth = [...prefix];
      t.requested = true;
      for (const k of prefix) t.phase[k - 1] = "claimed";
    });
  }
  if (s.original.length && c.Worker === "monster" && s.scan === "unrun") {
    for (const scan of ["clean", "warnings", "errors", "installFailed", "buildFailed", "toolFailed"]) {
      add(t => {
        t.scan = scan;
        t.lintClean = scan === "clean" || (scan === "warnings" && c.Fault !== "warning");
      });
    }
  }
  if (s.original.length && s.quality === "unchecked") {
    for (const quality of ["passed", "failed"]) add(t => (t.quality = quality));
  }
  if (c.Fault === "membership" && s.original.length === 2 && s.original.some(k => s.phase[k - 1] === "cancelled")) add(t => (t.original = s.original.filter(k => s.phase[k - 1] !== "cancelled")));
  return next;
}

export function validateTrace(trace, entry) {
  assert.ok(trace.length > 1, "Missing nontrivial counterexample/witness");
  assert.deepEqual(trace[0].state, {
    phase: ["absent", "absent"],
    admitted: [],
    original: [],
    birth: [],
    requested: false,
    proposed: [-1, -1],
    scoped: [false, false],
    applied: [-1, -1],
    completedAtWrite: [false, false],
    verified: [false, false],
    scan: "unrun",
    lintClean: false,
    quality: "unchecked",
  });
  for (let index = 0; index < trace.length; index++) {
    assert.equal(trace[index].number, index + 1);
    if (index)
      assert.ok(
        successors(trace[index - 1].state, entry.constants).some(s => {
          try {
            assert.deepEqual(s, trace[index].state);
            return true;
          } catch {
            return false;
          }
        }),
        `Trace transition ${index + 1} is outside the finite abstraction`
      );
    const failed = violations(trace[index].state, entry.constants).filter(name => entry.invariants.includes(name));
    assert.deepEqual(failed, index === trace.length - 1 ? [entry.expected] : []);
  }
  return true;
}

export function readableTrace(trace, entry) {
  const lines = [`# ${entry.name}`, "", `Expected TLC diagnostic: \`${entry.expected}\`.`, "", "| State | Transition | Original | Phase | Proposed / applied | Readback | Scan / flag | Quality |", "|---|---|---|---|---|---|---|---|"];
  const escapeCell = value =>
    String(value)
      .replaceAll("\\", "\\\\")
      .replaceAll("|", "\\|")
      .replace(/\r\n|\n|\r/g, "<br>");
  for (const { number, action, state: s } of trace) {
    const cells = [number, action, s.original.join(",") || "none", s.phase.join(", "), `${s.proposed.join(",")} / ${s.applied.join(",")}`, s.verified.join(","), `${s.scan} / ${s.lintClean}`, s.quality];
    lines.push(`| ${cells.map(escapeCell).join(" | ")} |`);
  }
  return lines.join("\n") + "\n";
}
