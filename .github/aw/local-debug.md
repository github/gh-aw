---
name: local-debug
description: Diagnose and patch gh-aw workflows; run active debug loops where permitted and stop on a workflow-dispatch 403.
---

# Workflow Diagnosis, Patching, and Active Debugging

Choose the execution mode first. Do not emulate GitHub Actions.
Load [debug-agentic-workflow.md](debug-agentic-workflow.md) only for additional log/audit guidance.

## Choose the Mode

| Mode | Scope |
| --- | --- |
| **Diagnose and patch** | Inspect existing evidence, reproduce components with fixtures, edit, test, and compile using available tools. No live-testing uploads or workflow dispatch. |
| **Debug** | Permitted execution context: an active edit -> test -> compile -> human review -> upload/run -> audit loop within approved bounds. A dispatch 403 ends this loop. |

Respect explicit host/repository no-dispatch rules, including Copilot cloud's
restriction. Otherwise, do not infer run capability merely from sandboxing or
authentication: handle the actual run response. `--dev` is usable in either mode
when its checks are available; it grants neither run access nor approval.

## Strategy

1. **Anchor the failure.** Record source/lock revision, versions, trigger and inputs. Read crash logs and `gh aw audit RUN_ID --json`; identify the first failing boundary, not downstream errors. Label evidence: observed, executed locally, fixture-backed, inferred, or unavailable.

2. **Inspect before executing.** Isolate compilation, prompt rendering, MCP/tool, agent, or safe-output handling. Use `gh aw mcp list WORKFLOW`, `gh aw mcp list-tools WORKFLOW --server NAME`, and `gh aw mcp inspect WORKFLOW --server NAME`. Inspection can start/connect servers: use scoped test credentials and metadata only, not write-tool calls.

3. **Reproduce one boundary.** Run the real component with minimal inputs and existing test doubles. Mock context, APIs, or absent credentials; replay only redacted HTTP fixtures. Use disposable files and bounded timeouts. Never contact production or disable the firewall to fill gaps. Leave OIDC, approvals, hosted tokens, and runner semantics unverified until live testing.

4. **Fix and gate.** Make a minimal change and regression test. Confirm the CLI supports `--dev`; never silently downgrade. Compile the actual lock using the commands below. `--dev` forces `--strict`, `--staged`, validation and analysis flags, failing on warnings or failed/missing checks. Never substitute `strict: true`, suppress findings, use `--approve`, or commit to erase warnings. Failed compilation artifacts are not approved test inputs.

5. **Constrain credentials.** Recommend, but do not require, a protected test environment with rotated, restricted development credentials. Inspect existing environments; provision only with authorization and revoke replaced credentials at their issuer. Never copy/rotate production secrets. Authorized repository, organization, and enterprise-provided shared secrets remain in job scope; they are not automatically exported as process environment variables. Review bindings, permissions, OIDC, destinations, expiry and budgets. Pass only explicit test bindings to harnesses; keep production/dispatch credentials out.

6. **Debug mode only: iterate against hosted uncertainty.** Before each upload/dispatch, require human validation of source, lock, triggers, imports/pins, permissions, credentials, destinations and limits. Bind an unchanged remote ref to the reviewed commit, then use `gh aw run WORKFLOW --ref REVIEWED_REF`. Default to one time/spend-bounded run per approval. **If starting the run returns `403 Forbidden`, stop live attempts immediately and switch to diagnose/patch.** Preserve the redacted denial and patch; report blocked hosted verification. Do not retry unchanged, automatically swap credentials/API/session, push to provoke a run, or ask for manual dispatch as a workaround. On success, verify the run SHA, audit results, and convert divergences into regressions. Repeat within budget, revalidating every revision; stop when resolved or blocked.

## 403 and Codespaces Credential Triage

Codespaces is not proof of run access or SAML authorization. Use `gh auth status`
to identify the host and active credential source, never to print token values.
[`GH_TOKEN`, then `GITHUB_TOKEN`](https://cli.github.com/manual/gh_help_environment)
override stored `gh` credentials. Inspect the denial and any `X-GitHub-SSO` hint;
do not assume every 403 means missing Actions permissions. Classic PATs need
[organization SSO authorization](https://docs.github.com/en/authentication/authenticating-with-single-sign-on/authorizing-a-personal-access-token-for-use-with-single-sign-on);
fine-grained tokens are authorized during creation. Keep the loop stopped.
Resume only after an explicitly authorized authentication/SSO repair and renewed
live-test validation; never bypass SAML or expose tokens/authorization URLs.

## Development Compilation

```bash
gh aw compile WORKFLOW --dev
# Recommended when a reviewed test environment exists:
gh aw compile WORKFLOW --dev --environment gh-aw-debug
```

`--environment` replaces the environment on every compiled lock-file job,
including approval, custom, and framework jobs; inspect the resulting protections.
Reusable-workflow caller jobs cannot declare environments, so this override fails
rather than skipping them. Review the callee separately if compiling without it.
Staging does not neutralize arbitrary scripts, custom jobs, or external MCP effects.

Report the mode, root cause, minimal fix, regression evidence, and remaining uncertainty.
Keep local execution, fixture simulation, and live Actions results distinct.
