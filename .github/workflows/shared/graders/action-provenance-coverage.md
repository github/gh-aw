---
graders:
  # Fraction of consequential actions with a provenance path to a tool call or
  # observation in the canonical Trajectory IR. Higher is better.
  action-provenance-coverage:
    name: Action Provenance Coverage
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
      const ir = candidates.find(value =>
        Array.isArray(value.actions) &&
        Array.isArray(value.provenanceEdges) &&
        Array.isArray(value.toolCalls) &&
        Array.isArray(value.observations)
      );
      if (!ir) {
        return { value: null, unit: "ratio", passed: null, message: "not applicable: trace lacks actions, provenanceEdges, toolCalls, or observations" };
      }
      if (ir.actions.some(action => !isRecord(action) || typeof action.consequential !== "boolean")) {
        return { value: null, unit: "ratio", passed: null, message: "unavailable: action is missing the canonical consequential flag" };
      }

      const consequential = ir.actions.filter(action => action.consequential === true);
      if (consequential.length === 0) {
        return { value: null, unit: "ratio", passed: null, message: "not applicable: no consequential actions" };
      }
      if (consequential.some(action => typeof action.id !== "string" || action.id === "")) {
        return { value: null, unit: "ratio", passed: null, message: "unavailable: consequential action is missing an id" };
      }

      const roots = new Set();
      for (const item of [...ir.toolCalls, ...ir.observations]) {
        if (isRecord(item) && typeof item.id === "string" && item.id !== "") roots.add(item.id);
      }
      const parents = new Map();
      for (const edge of ir.provenanceEdges) {
        if (!isRecord(edge) || typeof edge.from !== "string" || edge.from === "" || typeof edge.to !== "string" || edge.to === "") continue;
        if (!parents.has(edge.to)) parents.set(edge.to, []);
        parents.get(edge.to).push(edge.from);
      }
      const hasRoot = start => {
        const pending = [start];
        const seen = new Set();
        while (pending.length > 0) {
          const current = pending.pop();
          if (roots.has(current)) return true;
          if (seen.has(current)) continue;
          seen.add(current);
          for (const parent of parents.get(current) ?? []) pending.push(parent);
        }
        return false;
      };
      const covered = consequential.filter(action => hasRoot(action.id));
      const missing = consequential.filter(action => !hasRoot(action.id)).slice(0, 5).map(action => action.id);
      return {
        value: helpers.ratio(covered.length, consequential.length),
        unit: "ratio",
        details: `consequentialActions=${consequential.length} covered=${covered.length}${missing.length === 0 ? "" : `; missing provenance: ${missing.join(", ")}`}`,
      };
---

<!--
action-provenance-coverage selects consequential actions using the canonical
actions[].consequential flag (not a substring guess on actions[].type) and
follows provenanceEdges backward from each actions[].id to a toolCalls[].id or
observations[].id evidence root. Read-only actions are outside the
denominator. Missing graph fields, a missing consequential flag, or missing
action IDs make the result unavailable.
-->
