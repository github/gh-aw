export const invariants = [
  "TypeOK",
  "ImmutableMembership",
  "SingleClaimDispatch",
  "PolicyRequired",
  "BranchProvisioning",
  "ScopedIntents",
  "EffectRequiresCompletion",
  "ResultRequiresReadback",
  "ResultCardinality",
  "RuleDiagnosticsGate",
  "NoAutomaticDAG",
  "BoundedAssignment",
];

const defaults = {
  Worker: "monster",
  Installed: true,
  NoWrites: false,
  OutputMin: 1,
  OutputMax: 3,
  Fault: "none",
  Witness: "none",
};

const safety = (name, overrides = {}) => ({
  name,
  constants: { ...defaults, ...overrides },
  invariants,
  expected: "exhaustive",
});
const negative = (fault, invariant, overrides = {}) => ({
  name: `negative-${fault}`,
  constants: { ...defaults, Fault: fault, ...overrides },
  invariants: ["TypeOK", invariant],
  expected: invariant,
});
const witness = (name, overrides = {}) => ({
  name: `witness-${name}`,
  constants: { ...defaults, Witness: name, ...overrides },
  invariants: [...invariants, "NeverWitness"],
  expected: "NeverWitness",
});

export const cases = [
  safety("monster"),
  safety("miner", { Worker: "miner", OutputMax: 1 }),
  safety("refiner", { Worker: "refiner" }),
  safety("mixed-profiles", { Worker: "mixed" }),
  safety("no-policy", { Installed: false }),
  safety("no-write", { NoWrites: true, OutputMin: 0 }),
  negative("scope", "ScopedIntents"),
  negative("membership", "ImmutableMembership"),
  negative("group", "SingleClaimDispatch"),
  negative("policy", "PolicyRequired", { Installed: false }),
  negative("completion", "EffectRequiresCompletion"),
  negative("result", "ResultRequiresReadback"),
  negative("cardinality", "ResultCardinality"),
  negative("warning", "RuleDiagnosticsGate"),
  negative("dag", "NoAutomaticDAG"),
  witness("warning-actionable"),
  witness("completion-only"),
  witness("three-outputs"),
  witness("quality-not-gate", { Worker: "miner", OutputMax: 1 }),
  witness("tool-failure"),
  witness("result", { Worker: "refiner" }),
  witness("no-write", { NoWrites: true, OutputMin: 0 }),
];

export function configuration(entry) {
  const literal = value => (typeof value === "string" ? JSON.stringify(value) : typeof value === "boolean" ? String(value).toUpperCase() : String(value));
  return ["SPECIFICATION Spec", "CHECK_DEADLOCK FALSE", "CONSTANTS", ...Object.entries(entry.constants).map(([key, value]) => `  ${key} = ${literal(value)}`), "INVARIANTS", ...entry.invariants.map(name => `  ${name}`), ""].join("\n");
}
