---
graders:
  # For runs that left one or more declared objectives unsatisfied, measures
  # whether the failure was due to insufficient search: 1 - (observations /
  # distinctStatesVisited), clamped to [0, 1]. distinctStatesVisited comes
  # from distinct state_change event refs, falling back to the declared
  # states[] count when no state_change events are recorded. Runs with all
  # objectives satisfied score 0 (no exploration error to attribute). This is
  # the complement of exploitation-error, which covers runs that had
  # enough evidence but failed anyway. Lower is better: fewer
  # unmet objectives attributable to insufficient search.
  exploration-error:
    name: Exploration Error
    unit: ratio
    direction: lower_is_better
    min: 0.0
    max: 1.0
    script: |
      const isRecord = value => value !== null && typeof value === "object" && !Array.isArray(value);
      const candidates = [
        trace.trajectoryIR,
        trace.trajectoryIr,
        trace.ir,
        isRecord(trace.agentOutput) ? trace.agentOutput.trajectoryIR : null,
        isRecord(trace.agentOutput) ? trace.agentOutput.trajectoryIr : null,
        isRecord(trace.agentOutput) ? trace.agentOutput.trajectory : null,
        isRecord(trace.agentOutput) ? trace.agentOutput : null,
      ].filter(isRecord);

      const candidate =
        candidates.find(value => Array.isArray(value.objectives) && value.objectives.some(isRecord)) ??
        candidates.find(value =>
          Array.isArray(value.objectives) ||
          Array.isArray(value.events) ||
          Array.isArray(value.states) ||
          Array.isArray(value.observations)
        ) ??
        null;
      const objectives = (candidate && Array.isArray(candidate.objectives) ? candidate.objectives : []).filter(isRecord);
      if (objectives.length === 0) {
        return { value: null, unit: "ratio", passed: null, message: "not applicable: no declared objectives in the trace" };
      }

      const unmet = objectives.filter(objective => objective.satisfiedAtEventIndex === null || objective.satisfiedAtEventIndex === undefined);
      if (unmet.length === 0) {
        return { value: 0, unit: "ratio", details: `objectives=${objectives.length} unmet=0; all objectives satisfied` };
      }

      const events = (candidate && Array.isArray(candidate.events) ? candidate.events : []).filter(isRecord);
      const states = (candidate && Array.isArray(candidate.states) ? candidate.states : []).filter(isRecord);
      const observations = (candidate && Array.isArray(candidate.observations) ? candidate.observations : []).filter(isRecord);

      const stateChangeEvents = events.filter(event => event.kind === "state_change");
      let distinctStatesVisited = 0;
      let source = "";
      if (stateChangeEvents.length > 0) {
        const visited = new Set(stateChangeEvents.map(event => (typeof event.ref === "string" ? event.ref : JSON.stringify(event.ref))));
        distinctStatesVisited = visited.size;
        source = "state_change events";
      } else if (states.length > 0) {
        distinctStatesVisited = states.length;
        source = "declared states[]";
      } else {
        return { value: null, unit: "ratio", passed: null, message: "not applicable: no state_change events or declared states in the trace" };
      }

      const value = helpers.clamp(1 - observations.length / distinctStatesVisited, 0, 1);
      const unmetDescriptions = unmet.slice(0, 5).map(objective => (typeof objective.id === "string" && objective.id !== "" ? objective.id : objective.description));

      return {
        value,
        unit: "ratio",
        details: `objectives=${objectives.length} unmet=${unmet.length} observations=${observations.length} distinctStatesVisited=${distinctStatesVisited} (from ${source})${unmetDescriptions.length === 0 ? "" : `; unmet objectives: ${unmetDescriptions.join(", ")}`}`,
      };
---

<!--
exploration-error attributes objective failure to insufficient search: for
runs that left one or more declared objectives unsatisfied
(satisfiedAtEventIndex null/undefined), it computes
1 - (observations / distinctStatesVisited), clamped to [0, 1]. A low
observation-to-state ratio (few observations gathered across the states the
run visited) yields a score near 1 -- the run likely failed because it never
gathered enough evidence. distinctStatesVisited is the count of distinct
refs across events[] of kind "state_change"; when no such events are
recorded it falls back to the declared states[] count. Runs with all
objectives satisfied score 0 -- there is no exploration error to attribute,
since exploration failures only apply to failed runs. This is the
complement of exploitation-error, which covers runs that had enough
evidence but misused it. Reports not-applicable
(passed: null) when no objectives are declared, or when neither
state_change events nor declared states are present in the trace.
-->
