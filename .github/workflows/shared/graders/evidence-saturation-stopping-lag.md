---
graders:
  # Number of trace events after the event where the final declared objective
  # became satisfied. Lower is better: stop promptly once evidence is complete.
  evidence-saturation-stopping-lag:
    name: Evidence Saturation Stopping Lag
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
      const ir = candidates.find(value => Array.isArray(value.objectives) && Array.isArray(value.events));
      if (!ir || ir.objectives.length === 0) {
        return { value: null, unit: "count", passed: null, message: "not applicable: no declared objectives" };
      }
      const objectives = ir.objectives.filter(isRecord);
      if (objectives.length !== ir.objectives.length) {
        return { value: null, unit: "count", passed: null, message: "unavailable: malformed objective metadata" };
      }
      if (objectives.some(objective => objective.satisfiedAtEventIndex === null || objective.satisfiedAtEventIndex === undefined)) {
        return { value: null, unit: "count", passed: null, message: "not applicable: evidence never saturated" };
      }
      const satisfactionIndexes = objectives.map(objective => objective.satisfiedAtEventIndex);
      if (satisfactionIndexes.some(index => !Number.isSafeInteger(index) || index < 0)) {
        return { value: null, unit: "count", passed: null, message: "unavailable: invalid objective satisfaction index" };
      }

      const eventIndexes = ir.events.filter(isRecord).map(event => event.index);
      if (eventIndexes.length !== ir.events.length || eventIndexes.some(index => !Number.isSafeInteger(index) || index < 0)) {
        return { value: null, unit: "count", passed: null, message: "unavailable: invalid event index" };
      }
      const saturationIndex = Math.max(...satisfactionIndexes);
      if (!eventIndexes.includes(saturationIndex)) {
        return { value: null, unit: "count", passed: null, message: "unavailable: saturation event is absent from the trace" };
      }
      const lag = eventIndexes.filter(index => index > saturationIndex).length;
      return {
        value: lag,
        unit: "count",
        details: `objectives=${objectives.length} saturationEventIndex=${saturationIndex} eventsAfterSaturation=${lag}`,
      };
---

<!--
evidence-saturation-stopping-lag finds the latest
objectives[].satisfiedAtEventIndex and counts events[] entries after it. It is
not applicable until every declared objective is satisfied. Invalid indexes or
a missing saturation event make the result unavailable.
-->
