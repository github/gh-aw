# ADR-73046: Topology-Aware LLM Gateway URL for Detection and Evals Jobs

**Date**: 2026-05-12
**Status**: Draft
**Deciders**: Unknown (not recorded in PR #73046)

---

### Context

The compiler decides the LLM gateway host that it gives agent engines. `llmGatewayBaseURL` in `pkg/workflow/llm_provider.go` uses `isAWFNetworkIsolationEnabled` to choose between `http://api-proxy:<port>` and `http://host.docker.internal:<port>`. `isAWFNetworkIsolationEnabled` returns false for inline threat detection and evals runs because those runs attach no MCP sidecars to the isolated topology. AWF still runs the agent for those jobs on its network-isolation topology, where `host.docker.internal` cannot reach the API proxy. As a result, non-Anthropic-provider engines in detection and evals jobs failed. For example, with Claude on Copilot, AWF's pre-flight check fails, the CLI exits with code 1, and every BinEval answer becomes `UNKNOWN` (issue #73045). The one predicate was answering two questions: "should sidecars attach to the isolated network?" and "is the agent on the isolated network?"

### Decision

We will add a separate predicate, `isAWFAgentOnNetworkIsolationTopology` in `pkg/workflow/firewall.go`. It answers only whether AWF places the agent on the network-isolation topology, so it depends only on the sandbox runtime profile. Like `isAWFNetworkIsolationEnabled`, it handles a disabled sandbox, NVX, and `SupportsHostAccess`. Unlike `isAWFNetworkIsolationEnabled`, it does not exempt detection and evals runs. `llmGatewayBaseURL` is the only call site that switches to the new predicate. `isAWFNetworkIsolationEnabled` and its other call sites (MCP gateway host, DIFC/CLI proxy host, and the `network` section of `awf-config.json`) stay unchanged. Splitting the predicate fixes gateway addressing without changing sidecar behavior for detection and evals.

### Alternatives Considered

#### Alternative 1: Remove the detection/evals exemption from `isAWFNetworkIsolationEnabled`

This would make one predicate follow the actual topology everywhere. It would also change the MCP gateway host, the DIFC/CLI proxy host, and the generated `awf-config.json` network section for detection and evals jobs. Those jobs have no sidecars to attach, so the change would affect behavior well beyond the bug and risk regressions in them. It was rejected in favor of a one-call-site change, which `TestDetectionAndEvalsSidecarOutputUnchanged` pins.

#### Alternative 2: Special-case detection/evals inside `llmGatewayBaseURL`

`llmGatewayBaseURL` could check `IsDetectionRun`/`IsEvalsRun` directly and force `api-proxy`. This would add job-type knowledge to URL construction and could break runtimes with host access, such as `docker-sudo-iptables`, which must keep `host.docker.internal`. A named, profile-based topology predicate keeps that logic in one place, next to the existing isolation logic.

### Consequences

#### Positive
- The LLM gateway URL follows where the agent actually runs, so detection and evals jobs for non-Anthropic-provider engines reach the API proxy and record real results instead of `UNKNOWN`.
- The change is narrow: one call site moves, and sidecar-related output for detection and evals stays byte-for-byte the same.
- Runtimes with host access still get `host.docker.internal`.

#### Negative
- Two similar predicates now exist (`isAWFNetworkIsolationEnabled` and `isAWFAgentOnNetworkIsolationTopology`) with partly duplicated logic. Future call sites must choose the right one, and the two can drift apart.
- Several compiled lock files change (`ANTHROPIC_BASE_URL`, `OPENAI_BASE_URL`, `GITHUB_COPILOT_BASE_URL`, `GEMINI_API_BASE_URL` in detection and evals jobs). The end-to-end fix can be confirmed only by a live run after merge.

#### Neutral
- The existing detection/evals exemption in `isAWFNetworkIsolationEnabled` now applies only to sidecar attachment, which its comment already describes.
- User-set base URLs, such as `COPILOT_PROVIDER_BASE_URL` in the `daily-byok-ollama-test` agent job, are not affected.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
