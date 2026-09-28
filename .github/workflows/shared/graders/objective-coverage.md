---
graders:
  # Fraction of explicitly declared objectives that were completed during the
  # trace. Higher is better; 1.0 means every declared objective was satisfied.
  objective-coverage:
    name: Objective Coverage
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
      const ir = candidates.find(value => Array.isArray(value.objectives));
      if (!ir || ir.objectives.length === 0) {
        return { value: null, passed: null, message: "not applicable: no declared objectives" };
      }
      if (ir.objectives.some(objective => !isRecord(objective))) {
        return { value: null, passed: null, message: "unavailable: malformed objective metadata" };
      }
      const isUnset = index => index === null || index === undefined;
      if (ir.objectives.some(objective => !isUnset(objective.satisfiedAtEventIndex) && (!Number.isSafeInteger(objective.satisfiedAtEventIndex) || objective.satisfiedAtEventIndex < 0))) {
        return { value: null, passed: null, message: "unavailable: invalid objective satisfaction index" };
      }

      const total = ir.objectives.length;
      const unmet = ir.objectives
        .map((objective, index) => ({ objective, label: typeof objective.id === "string" && objective.id !== "" ? objective.id : `objective-${index + 1}` }))
        .filter(entry => isUnset(entry.objective.satisfiedAtEventIndex));
      const completed = total - unmet.length;
      const labels = unmet.slice(0, 5).map(entry => entry.label);
      return {
        value: helpers.ratio(completed, total),
        details: `objectives=${total} completed=${completed}${labels.length === 0 ? "" : `; incomplete: ${labels.join(", ")}`}`,
      };
---

<!--
objective-coverage divides the number of objectives[] entries with a valid
satisfiedAtEventIndex by the number of declared objectives. It is the
normalized complement of premature-termination-gap. Runs without declared
objectives are not applicable; malformed objective metadata or invalid
satisfaction indexes are unavailable.
-->
