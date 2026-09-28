---
graders:
  # Number of declared completion conditions still unsatisfied when the trace
  # ended. Lower is better; zero means every declared objective was completed.
  premature-termination-gap:
    name: Premature Termination Gap
    unit: count
    direction: lower_is_better
    min: 0
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
        return { value: null, unit: "count", passed: null, message: "not applicable: no declared objectives" };
      }
      if (ir.objectives.some(objective => !isRecord(objective))) {
        return { value: null, unit: "count", passed: null, message: "unavailable: malformed objective metadata" };
      }
      const hasInvalidIndex = ir.objectives.some(objective =>
        objective.satisfiedAtEventIndex !== null &&
        objective.satisfiedAtEventIndex !== undefined &&
        (!Number.isSafeInteger(objective.satisfiedAtEventIndex) || objective.satisfiedAtEventIndex < 0)
      );
      if (hasInvalidIndex) {
        return { value: null, unit: "count", passed: null, message: "unavailable: invalid objective satisfaction index" };
      }

      const unmet = ir.objectives.filter(objective => objective.satisfiedAtEventIndex === null || objective.satisfiedAtEventIndex === undefined);
      const labels = unmet.slice(0, 5).map((objective, index) =>
        typeof objective.id === "string" && objective.id !== "" ? objective.id : `objective-${index + 1}`
      );
      return {
        value: unmet.length,
        unit: "count",
        details: `objectives=${ir.objectives.length} unsatisfied=${unmet.length}${labels.length === 0 ? "" : `; unsatisfied: ${labels.join(", ")}`}`,
      };
---

<!--
premature-termination-gap counts objectives[] entries whose
satisfiedAtEventIndex is null or absent at the end of the trace. It preserves
the native count rather than normalizing it. Runs without declared objectives
are not applicable; malformed objective metadata is unavailable.
-->
