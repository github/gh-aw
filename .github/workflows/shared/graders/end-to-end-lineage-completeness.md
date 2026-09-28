---
graders:
  # Fraction of final safe outputs that can be traced backward through the
  # canonical Trajectory IR provenance graph to a tool call or observation.
  # Higher is better: every delivered output should have recorded evidence.
  end-to-end-lineage-completeness:
    name: End-to-End Lineage Completeness
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
        Array.isArray(value.events) &&
        Array.isArray(value.provenanceEdges) &&
        Array.isArray(value.toolCalls) &&
        Array.isArray(value.observations)
      );
      if (!ir) {
        return { value: null, unit: "ratio", passed: null, message: "not applicable: trace lacks events, provenanceEdges, toolCalls, or observations" };
      }

      const outputEvents = ir.events.filter(event => isRecord(event) && event.kind === "safe_output");
      if (outputEvents.length === 0) {
        return { value: null, unit: "ratio", passed: null, message: "not applicable: no final safe outputs" };
      }
      const outputIds = outputEvents.map(event => event.ref);
      if (outputIds.some(id => typeof id !== "string" || id === "")) {
        return { value: null, unit: "ratio", passed: null, message: "unavailable: safe output is missing a provenance reference" };
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
      const complete = outputIds.filter(hasRoot);
      const missing = outputIds.filter(id => !hasRoot(id)).slice(0, 5);
      return {
        value: helpers.ratio(complete.length, outputIds.length),
        unit: "ratio",
        details: `outputs=${outputIds.length} traceable=${complete.length}${missing.length === 0 ? "" : `; missing lineage: ${missing.join(", ")}`}`,
      };
---

<!--
end-to-end-lineage-completeness follows provenanceEdges backward from each
events[].ref whose kind is safe_output and counts the fraction that reaches a
toolCalls[].id or observations[].id evidence root. It reports unavailable when
the trace lacks the graph contract or a safe output lacks its required ref, and
not-applicable when the run emitted no safe output.
-->
