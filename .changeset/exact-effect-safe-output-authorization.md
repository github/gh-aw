---
"gh-aw": patch
---

Add exact-effect (`resolved_effect`) safe-output authorization for `merge-pull-request` and `remove-labels`. On their native manager and per-Claim routes, a protected invocation now freezes the exact outbound request plan, obtains one authority decision bound to the plan digest, revalidates mutable preconditions, and sends only the approved requests through a verifying one-shot transport. Other outputs, including custom scripts, actions and jobs, remain `invocation`-bound. `gh aw validate --authorization-policy` route entries gain optional `effect_adapter` and `scope_binding` fields.
