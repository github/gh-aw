---
graders:
  # Fraction of issued actions that were structurally valid in the state in
  # which they were issued (actions[].validAtIssueTime). Higher is better.
  grounding-accuracy:
    name: Grounding Accuracy
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
      const ir = candidates.find(value => Array.isArray(value.actions));
      if (!ir || ir.actions.length === 0) {
        return { value: null, passed: null, message: "not applicable: no issued actions" };
      }
      if (ir.actions.some(action => !isRecord(action) || typeof action.validAtIssueTime !== "boolean")) {
        return { value: null, passed: null, message: "unavailable: every action requires a boolean validAtIssueTime" };
      }

      const invalid = ir.actions
        .map((action, index) => ({ action, label: typeof action.id === "string" && action.id !== "" ? action.id : `action-${index + 1}` }))
        .filter(entry => entry.action.validAtIssueTime === false);
      const valid = ir.actions.length - invalid.length;
      const labels = invalid.slice(0, 5).map(entry => entry.label);
      return {
        value: helpers.ratio(valid, ir.actions.length),
        details: `actions=${ir.actions.length} valid=${valid}${labels.length === 0 ? "" : `; ungrounded: ${labels.join(", ")}`}`,
      };
---

<!--
grounding-accuracy divides the number of actions[] entries whose
validAtIssueTime is true by the number of issued actions. The IR builder decides
validity against the valid-action schema of the state in which the action was
issued (for example, the tool existed, required arguments were present, and the
target resource existed). Runs without actions are not applicable; actions
missing a boolean validAtIssueTime make the result unavailable rather than
being guessed.
-->
