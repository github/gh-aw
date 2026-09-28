---
graders:
  # Fraction of completed objectives with declared dependencies that completed
  # before at least one prerequisite. Lower is better.
  dependency-order-violation-rate:
    name: Dependency Order Violation Rate
    unit: ratio
    direction: lower_is_better
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
        return { value: null, unit: "ratio", passed: null, message: "not applicable: no declared objectives" };
      }
      if (ir.objectives.some(objective => !isRecord(objective) || typeof objective.id !== "string" || objective.id === "")) {
        return { value: null, unit: "ratio", passed: null, message: "unavailable: objectives require unique non-empty ids" };
      }
      const byId = new Map(ir.objectives.map(objective => [objective.id, objective]));
      if (byId.size !== ir.objectives.length) {
        return { value: null, unit: "ratio", passed: null, message: "unavailable: objective ids are not unique" };
      }
      const dependent = ir.objectives.filter(objective => Array.isArray(objective.dependsOn) && objective.dependsOn.length > 0);
      if (dependent.length === 0) {
        return { value: null, unit: "ratio", passed: null, message: "not applicable: no objective dependencies" };
      }
      const hasUnknown = dependent.flatMap(objective => objective.dependsOn).some(id => typeof id !== "string" || !byId.has(id));
      if (hasUnknown) {
        return { value: null, unit: "ratio", passed: null, message: "unavailable: dependency references an unknown objective" };
      }

      const completed = dependent.filter(objective => Number.isSafeInteger(objective.satisfiedAtEventIndex) && objective.satisfiedAtEventIndex >= 0);
      if (completed.length === 0) {
        return { value: null, unit: "ratio", passed: null, message: "not applicable: no dependent objective completed" };
      }
      const violates = objective => {
        const completedAt = objective.satisfiedAtEventIndex;
        return objective.dependsOn.some(id => {
          const prerequisite = byId.get(id);
          return prerequisite.satisfiedAtEventIndex === null ||
            prerequisite.satisfiedAtEventIndex === undefined ||
            !Number.isSafeInteger(prerequisite.satisfiedAtEventIndex) ||
            prerequisite.satisfiedAtEventIndex < 0 ||
            prerequisite.satisfiedAtEventIndex > completedAt;
        });
      };
      const violations = completed.filter(violates);
      return {
        value: helpers.ratio(violations.length, completed.length),
        unit: "ratio",
        details: `completedDependentObjectives=${completed.length} violations=${violations.length}${violations.length === 0 ? "" : `; out of order: ${violations.slice(0, 5).map(objective => objective.id).join(", ")}`}`,
      };
---

<!--
dependency-order-violation-rate evaluates completed objectives that declare
objectives[].dependsOn. A completed dependent objective is a violation when any
prerequisite is incomplete, invalid, or completed at a later event index.
Unknown dependency IDs make the graph unavailable; traces without dependencies
or without a completed dependent objective are not applicable.
-->
