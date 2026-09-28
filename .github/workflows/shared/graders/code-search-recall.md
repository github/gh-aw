---
graders:
  # Fraction of files touched by the reference patch that the agent located
  # at any point during the run. Higher is better.
  code-search-recall:
    name: Code Search Recall
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
      const ir = candidates.find(value => isRecord(value.reference) && isRecord(value.reference.patch) && Array.isArray(value.reference.patch.files));
      if (!ir || ir.reference.patch.files.length === 0) {
        return { value: null, passed: null, message: "not applicable: no reference patch files" };
      }
      const normalize = value => value.trim().replace(/^\.\//, "").replace(/^\/+/, "");
      const required = ir.reference.patch.files.map(file => (typeof file === "string" ? file : isRecord(file) && typeof file.path === "string" ? file.path : ""));
      if (required.some(file => normalize(file) === "")) {
        return { value: null, passed: null, message: "unavailable: reference patch file is missing a path" };
      }
      if (!Array.isArray(ir.resources) || !ir.resources.every(isRecord)) {
        return { value: null, passed: null, message: "unavailable: trace lacks well-formed resources" };
      }

      const located = new Set();
      for (const resource of ir.resources) {
        if (resource.kind === "file" && typeof resource.uri === "string" && normalize(resource.uri) !== "") located.add(normalize(resource.uri));
      }
      const requiredSet = [...new Set(required.map(normalize))];
      const missed = requiredSet.filter(file => !located.has(file));
      const found = requiredSet.length - missed.length;
      return {
        value: helpers.ratio(found, requiredSet.length),
        details: `referenceFiles=${requiredSet.length} located=${found}${missed.length === 0 ? "" : `; never located: ${missed.slice(0, 5).join(", ")}`}`,
      };
---

<!--
code-search-recall compares reference.patch.files[] (strings or { path }
records) with the resources[] entries whose kind is file, after stripping a
leading "./" or "/" from each path. The IR builder records a file resource for
every file the agent located (searched, listed, read, or edited). Runs without a
reference patch are not applicable; a reference file without a path or a trace
without a well-formed resources[] collection is unavailable.
-->
