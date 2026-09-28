---
graders:
  # Longest correct execution prefix against reference.toolCalls, with
  # parameter correctness credit, normalized by reference length.
  # Higher is better; 1.0 reproduces the full reference with exact arguments.
  tool-wise-score:
    name: Tool-Wise Score
    unit: ratio
    direction: higher_is_better
    min: 0.0
    max: 1.0
    script: |
      const isRecord = value => value !== null && typeof value === "object" && !Array.isArray(value);
      const candidates = [
        trace,
        trace.trajectoryIR,
        trace.trajectoryIr,
        trace.ir,
        isRecord(trace.agentOutput) ? trace.agentOutput.trajectoryIR : null,
        isRecord(trace.agentOutput) ? trace.agentOutput.trajectoryIr : null,
        isRecord(trace.agentOutput) ? trace.agentOutput.trajectory : null,
        isRecord(trace.agentOutput) ? trace.agentOutput : null,
      ].filter(isRecord);
      const ir = candidates.find(value => isRecord(value.reference) && Array.isArray(value.reference.toolCalls));
      if (!ir || ir.reference.toolCalls.length === 0) {
        return { value: null, passed: null, message: "not applicable: no reference tool-call trajectory" };
      }
      const validCall = call => isRecord(call) && typeof call.name === "string" && call.name !== "" && (call.arguments === undefined || call.arguments === null || isRecord(call.arguments));
      const expected = ir.reference.toolCalls;
      if (!expected.every(validCall)) {
        return { value: null, passed: null, message: "unavailable: malformed reference tool call" };
      }
      if (!Array.isArray(ir.toolCalls) || !ir.toolCalls.every(validCall)) {
        return { value: null, passed: null, message: "unavailable: trace lacks well-formed toolCalls" };
      }
      const observed = ir.toolCalls
        .map((call, position) => ({ call, position }))
        .sort((a, b) => (Number.isFinite(a.call.eventIndex) && Number.isFinite(b.call.eventIndex) ? a.call.eventIndex - b.call.eventIndex : 0) || a.position - b.position)
        .map(entry => entry.call);

      const canonical = value => {
        if (Array.isArray(value)) return `[${value.map(canonical).join(",")}]`;
        if (isRecord(value)) return `{${Object.keys(value).sort().map(key => `${JSON.stringify(key)}:${canonical(value[key])}`).join(",")}}`;
        return JSON.stringify(value) ?? "undefined";
      };
      const argumentScore = (reference, actual) => {
        const keys = Object.keys(reference ?? {});
        if (keys.length === 0) return 1;
        const got = actual ?? {};
        return keys.filter(key => Object.hasOwn(got, key) && canonical(got[key]) === canonical(reference[key])).length / keys.length;
      };

      let prefix = 0;
      let credit = 0;
      while (prefix < expected.length && prefix < observed.length && observed[prefix].name === expected[prefix].name) {
        credit += (1 + argumentScore(expected[prefix].arguments, observed[prefix].arguments)) / 2;
        prefix += 1;
      }
      const diverged = prefix < expected.length ? `; diverged at step ${prefix + 1}: expected ${expected[prefix].name}, got ${prefix < observed.length ? observed[prefix].name : "end of trace"}` : "";
      return {
        value: helpers.ratio(credit, expected.length),
        details: `referenceSteps=${expected.length} observedSteps=${observed.length} correctPrefix=${prefix} credit=${credit.toFixed(2)}${diverged}`,
      };
---

<!--
tool-wise-score compares the ordered toolCalls[] (sorted by eventIndex) with
reference.toolCalls[]. The execution prefix extends while tool names match the
reference step-for-step. Each prefix step earns 0.5 for the correct tool plus
0.5 times the fraction of reference argument keys whose values match exactly
(key-order independent). The score is total credit divided by the reference
length. Runs without a reference trajectory are not applicable; malformed
reference or observed tool calls are unavailable.
-->
