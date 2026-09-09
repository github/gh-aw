---
graders:
  # LZ76 (Kaspar & Schuster) incremental-parsing complexity of the ordered
  # canonical event-symbol sequence, projected purely from the Trajectory
  # IR's events[] (no states, provenance, or objectives required).
  # tool_call events are qualified by their ref (finer alphabet
  # granularity, matching the same convention used by event-entropy-rate);
  # other events use their kind. The raw phrase count produced by the
  # incremental copy/insert parsing rule is normalized by the theoretical
  # asymptotic upper bound n / log_b(n) (b = alphabet size) to stay in
  # [0, 1] and remain comparable across traces of different
  # lengths/alphabets. Unlike event-entropy-rate (first-order,
  # previous-symbol-conditioned predictability), LZ76 complexity captures
  # compressibility from arbitrary-length repeated substrings/motifs
  # anywhere earlier in the sequence, without assuming a Markov model.
  # Lower values indicate a highly compressible trace dominated by a few
  # repeated motifs (likely stuck in a loop); values near 1 indicate a
  # near-incompressible, structurally diverse trace.
  lempel-ziv-trajectory-complexity:
    name: Lempel-Ziv Trajectory Complexity
    unit: ratio
    direction: higher_is_better
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

      let events = [];
      for (const candidate of candidates) {
        if (Array.isArray(candidate.events)) {
          events = candidate.events;
          break;
        }
      }

      // Build the ordered canonical event-symbol sequence: tool_call events
      // are qualified by their ref for finer alphabet granularity, other
      // events use their kind alone.
      const ordered = events.map((event, position) => {
        if (!isRecord(event) || typeof event.kind !== "string" || event.kind === "") return null;
        const index = Number(event.index);
        const order = Number.isFinite(index) ? index : position;
        const symbol = event.kind === "tool_call" && typeof event.ref === "string" && event.ref !== "" ? `tool_call:${event.ref}` : event.kind;
        return { symbol, order };
      }).filter(entry => entry !== null).sort((a, b) => a.order - b.order);

      const sequence = ordered.map(entry => entry.symbol);
      const n = sequence.length;
      if (n < 2) {
        return { value: 0, unit: "ratio", passed: null, message: "not applicable: fewer than two events" };
      }

      const alphabetSize = new Set(sequence).size;
      if (alphabetSize < 2) {
        return { value: 0, unit: "ratio", details: `events=${n} alphabetSize=${alphabetSize}; LZ76 complexity is vacuously 0 with a single repeated symbol` };
      }

      // LZ76 incremental parsing (Kaspar & Schuster, 1987): scan the
      // sequence, incrementally extending the current phrase while it can
      // be found as a substring starting anywhere in [0, prefixLen);
      // start a new phrase (increment complexity) each time the search
      // pointer catches up to the end of the already-parsed prefix.
      let complexity = 1;
      let prefixLen = 1;
      let subSeqLen = 1;
      let maxSubSeqLen = 1;
      let pointer = 0;
      while (prefixLen + subSeqLen <= n) {
        if (sequence[pointer + subSeqLen - 1] === sequence[prefixLen + subSeqLen - 1]) {
          subSeqLen += 1;
        } else {
          maxSubSeqLen = Math.max(subSeqLen, maxSubSeqLen);
          pointer += 1;
          if (pointer === prefixLen) {
            complexity += 1;
            prefixLen += maxSubSeqLen;
            pointer = 0;
            maxSubSeqLen = 1;
            subSeqLen = 1;
          } else {
            subSeqLen = 1;
          }
        }
      }
      if (subSeqLen !== 1) complexity += 1;

      // Normalize by the theoretical asymptotic upper bound n / log_b(n)
      // (b = alphabet size) so the value stays in [0, 1] and remains
      // comparable across traces of different lengths/alphabets.
      const upperBound = n / (Math.log(n) / Math.log(alphabetSize));
      const normalized = helpers.clamp(complexity / upperBound, 0, 1);

      return {
        value: normalized,
        unit: "ratio",
        details: `events=${n} alphabetSize=${alphabetSize} rawComplexity=${complexity} upperBound=${upperBound.toFixed(4)}`,
      };
---

<!--
lempel-ziv-trajectory-complexity computes the normalized LZ76 (Kaspar &
Schuster) incremental-parsing complexity of the ordered canonical
event-symbol sequence built purely from the Trajectory IR's events[] (no
states, provenance, or objectives required). It measures whole-sequence
compressibility from arbitrary-length repeated substrings/motifs anywhere
earlier in the sequence, without assuming a Markov model — distinct from
event-entropy-rate, which only captures first-order (previous-symbol-
conditioned) statistical predictability, and from the recurrence-* family,
which requires canonical states and measures revisit density/structure
rather than whole-sequence compressibility.
-->
