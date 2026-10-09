export const invariants = [
  "TypeOK",
  "ImmutableMembership",
  "HomogeneousBatch",
  "PolicyRequired",
  "BranchProvisioning",
  "ScopedIntents",
  "EffectRequiresCompletion",
  "ResultRequiresReadback",
  "ResultCardinality",
  "WarningExitZero",
  "NoAutomaticDAG",
  "BoundedAssignment",
];

const defaults = {
  Worker: "monster",
  Batch: true,
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
  safety("monster-batch"),
  safety("miner-singleton", { Worker: "miner", Batch: false, OutputMax: 1 }),
  safety("refiner-batch", { Worker: "refiner" }),
  safety("mixed-profiles", { Worker: "mixed" }),
  safety("no-policy", { Installed: false }),
  safety("no-write", { NoWrites: true, OutputMin: 0 }),
  negative("scope", "ScopedIntents"),
  negative("membership", "ImmutableMembership"),
  negative("mixed", "HomogeneousBatch", { Worker: "mixed" }),
  negative("policy", "PolicyRequired", { Installed: false }),
  negative("completion", "EffectRequiresCompletion"),
  negative("result", "ResultRequiresReadback"),
  negative("cardinality", "ResultCardinality"),
  negative("warning", "WarningExitZero"),
  negative("dag", "NoAutomaticDAG"),
  witness("warning-clean"),
  witness("completion-only"),
  witness("six-outputs"),
  witness("quality-not-gate", { Worker: "miner", Batch: false, OutputMax: 1 }),
  witness("tool-failure"),
  witness("result", { Worker: "refiner" }),
  witness("no-write", { NoWrites: true, OutputMin: 0 }),
];

export function configuration(entry) {
  const literal = value => (typeof value === "string" ? JSON.stringify(value) : typeof value === "boolean" ? String(value).toUpperCase() : String(value));
  return ["SPECIFICATION Spec", "CHECK_DEADLOCK FALSE", "CONSTANTS", ...Object.entries(entry.constants).map(([key, value]) => `  ${key} = ${literal(value)}`), "INVARIANTS", ...entry.invariants.map(name => `  ${name}`), ""].join("\n");
}
