---
graders:
  # Normalized dynamic time warping (nDTW) similarity between the observed
  # canonical state path and reference.states:
  # exp(-DTW(reference, observed) / (|reference| * threshold)), using a
  # discrete state distance (0 when state ids match, 1 otherwise) and a
  # success threshold of 1. Higher is better; 1.0 is an exact path match.
  trajectory-ndtw:
    name: Trajectory nDTW
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
      const ir = candidates.find(value => isRecord(value.reference) && Array.isArray(value.reference.states));
      if (!ir || ir.reference.states.length === 0) {
        return { value: null, passed: null, message: "not applicable: no reference state trajectory" };
      }
      const isId = value => typeof value === "string" && value !== "";
      const reference = ir.reference.states;
      if (!reference.every(isId)) {
        return { value: null, passed: null, message: "unavailable: reference states must be non-empty state ids" };
      }
      if (!Array.isArray(ir.events) || !ir.events.every(isRecord)) {
        return { value: null, passed: null, message: "unavailable: trace lacks well-formed events" };
      }
      const path = ir.events.filter(event => event.kind === "state_change").map(event => event.ref);
      if (path.length === 0) {
        return { value: null, passed: null, message: "unavailable: trace has no state_change events" };
      }
      if (!path.every(isId)) {
        return { value: null, passed: null, message: "unavailable: state_change event is missing a state id" };
      }

      const threshold = 1;
      let previous = [0, ...path.map(() => Infinity)];
      for (let i = 1; i <= reference.length; i += 1) {
        const current = [Infinity];
        for (let j = 1; j <= path.length; j += 1) {
          const cost = reference[i - 1] === path[j - 1] ? 0 : 1;
          current.push(cost + Math.min(previous[j], current[j - 1], previous[j - 1]));
        }
        previous = current;
      }
      const distance = previous[path.length];
      const value = Math.exp(-distance / (reference.length * threshold));
      return {
        value: Math.min(1, Math.max(0, value)),
        details: `referenceStates=${reference.length} observedStates=${path.length} dtwDistance=${distance}`,
      };
---

<!--
trajectory-ndtw computes normalized dynamic time warping between
reference.states[] and the ordered events[] whose kind is state_change (each
event ref is a canonical states[].id). The discrete state distance is 0 for
matching canonical ids and 1 otherwise, with a success threshold of 1, so
nDTW = exp(-DTW / |reference|). Runs without a reference state trajectory are
not applicable; missing state_change events or malformed state ids are
unavailable.
-->
