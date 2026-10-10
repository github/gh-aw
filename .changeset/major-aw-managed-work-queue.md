---
"gh-aw": major
---

Move global work-queue scheduling and backing Issue/label settings to
`.github/workflows/aw.json` under `work_queue`. Missing settings use automatic
first-submit bootstrap with one weighted-priority pool, concurrency 16, pending
limit 4,096, singleton assignments and three attempts with 30-second backoff.
AW supplies authorization, credentials and declared worker routes; producer
enrollment and author-configured worker principals are not required.

The installed Policy remains authoritative. Apply configuration changes with
an explicit, quiescent `work-queue policy --from-config --epoch EPOCH` update.
Workflow-level `work-queue-policy` and `tools.work-queue.issues` are deprecated;
conflicting definitions fail. Historical Policy ledgers remain readable.

Compatible worker deployments evolve future admissions and assignments without
draining. Compiler-derived logical contracts prevent implicit authority
expansion; explicit execution pins and frozen outstanding assignments preserve
reproducibility and recovery. Unavailable/incompatible workers pause affected
Work only, leaving identities, dependencies, Results, fairness debt and
reservations intact. Scheduling economics remain separate quiescent updates.
