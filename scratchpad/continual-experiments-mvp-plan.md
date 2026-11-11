# Continual Experiments MVP — Status and Next Work

**Status**: Active MVP (non-experimental)
**Last updated**: 2026-11-10

## Current feature status (what is implemented now)

- `experiments.<name>.continual` is non-experimental and does not emit an experimental-feature notice.
- Assignment happens at **GitHub Action runtime** in `actions/setup/js/pick_experiment.cjs`.
- Assignment is deterministic per run using the configured `seed` plus workflow/run identity inputs.
- Dynamic ramp state (`current_stage`) is treated as mutable runtime state and persisted on the workflow experiment state branch (not in frontmatter/workflow source).
- Ramp decisions are logged at runtime (stage, counts, active weights, selected variant).
- Existing non-continual experiments keep their prior behavior.

## MVP boundaries (intentionally out of scope in this iteration)

- No automatic quality-based promotion/rejection engine.
- No segment-level regression guardrail evaluator.
- No Bayesian/sequential decision engine wired into runtime stage advancement.

## Future work plan

1. **Data contract hardening**
   - Persist per-run assignment probability/allocation and assignment unit.
   - Persist an explicit enrollment epoch key (seed + harness fingerprint) so new candidates start from first canary stage.

2. **Decision ledger (runtime-consumable)**
   - Add a compact, versioned decision record in experiment state branch.
   - Keep workflow/frontmatter immutable; runtime reads/writes only ledger state.

3. **Quality-gated ramp advancement**
   - Advance stage only when persisted decision for current epoch is `CONTINUE`/`PROMOTE`.
   - Keep explicit rollback path to prior stage on regression signal.

4. **Analysis/reporting correctness**
   - Mark balance tests as N/A for adaptive ramps unless expected allocation is reconstructed from recorded probabilities.
   - Ensure CLI reports cannot display synthetic “balanced ✓” when no valid test was run.

5. **Workflow enablement rollout**
   - Keep opt-in only; enable in additional workflows incrementally.
   - Publish a short operator runbook for interpreting logs and pausing ramps.

## Reference papers / source material

- Ron Kohavi, Diane Tang, Ya Xu. *Trustworthy Online Controlled Experiments: A Practical Guide to A/B Testing* (Cambridge University Press, 2020).
- Aaditya Ramdas, Firas Hamze, et al. *Sequential Testing and Always-Valid p-Values* (sequence-safe inference foundations used in modern experimentation systems).
- Alex Deng, Ulf Knoblich, Jiannan Lu. *Applying the Delta Method in Metric Analytics: A Practical Guide with Novel Ideas* (Microsoft experimentation analytics foundations).

## Notes for next iteration

- Treat this as the minimum viable continual-ramp substrate for controlled rollout.
- Keep algorithm evolution in runtime state/ledger and analysis tooling, not immutable workflow source.
