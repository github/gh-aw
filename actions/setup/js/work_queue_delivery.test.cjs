// @ts-check
import { describe, expect, it } from "vitest";
import checks from "./work_queue_claim_scope_checks.cjs";

checks.registerTests({ describe, it });

it("registers dispatch identity and manifest checks as independent siblings before execution", async () => {
  let registering = true;
  /** @type {Map<string, () => unknown>} */
  const callbacks = new Map();
  checks.registerTests({
    describe(_name, register) {
      expect(registering, "suite registration cannot occur inside a running check").toBe(true);
      register();
    },
    it(name, run) {
      expect(registering, "test registration cannot occur inside a running check").toBe(true);
      expect(callbacks.has(name), "each check must be registered exactly once").toBe(false);
      callbacks.set(name, run);
    },
  });
  registering = false;
  for (const name of ["does not reuse local handles, factory contexts or receipts across immutable dispatch identities", "pins manifest writers to the full original Claim and persists immutable attribution"]) {
    const run = callbacks.get(name);
    expect(run, "both checks must exist before any test callback runs").toBeTypeOf("function");
    if (!run) throw new Error(`Missing sibling check: ${name}`);
    await run();
  }
});
