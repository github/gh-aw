# ADR-56976: Align Copilot SDK Tool Catalog with Compiled Workflow Tools

**Date**: 2026-08-29
**Status**: Draft
**Deciders**: pelikhan, adr-writer agent

---

### Context

This pull request changes the generated workflow lock files so Copilot SDK runs no longer advertise an unrestricted or misleading tool set through `GH_AW_COPILOT_SDK_SERVER_ARGS`. The PR body explains that SDK mode could expose a different tool catalog than the workflow actually declared, including cases where `web_fetch` was permitted but invisible or where disabled bash and editing tools still appeared visible. The diff shows a repeated pattern across many compiled workflows: replacing `--allow-all-tools` with explicit `--allow-tool` entries and adding a new `GH_AW_COPILOT_SDK_TOOL_CONFIG` environment variable that carries versioned capability and permission data. Because this is a cross-workflow change to how compiled tool permissions are represented and enforced, the architectural decision should be recorded explicitly.

### Decision

We will make the compiler emit an explicit, versioned Copilot SDK tool contract and require generated workflows to pass that contract to the SDK runtime. Instead of relying on broad `--allow-all-tools` behavior, workflows will enumerate the allowed SDK tools and provide `GH_AW_COPILOT_SDK_TOOL_CONFIG` so runtime-visible capabilities, explicit disables, and permission checks derive from the same compiled source of truth. We chose this approach because the PR evidence shows the main problem is drift between declared workflow tools and the SDK-visible catalog, and a compiler-owned contract gives one auditable representation that can fail closed when inconsistent.

### Alternatives Considered

#### Alternative 1: Keep Using Broad SDK Tool Flags and Document the Mismatch

Continue passing permissive SDK server arguments such as `--allow-all-tools` and rely on documentation or manual discipline to explain which tools are actually intended.

This was considered because it avoids adding a new configuration contract and touching many generated lock files. It was not chosen because the PR evidence identifies a real correctness and enforcement problem: the visible SDK tool set can diverge from the compiled workflow definition, which makes documentation insufficient and leaves policy enforcement ambiguous.

#### Alternative 2: Derive Runtime Tool Filtering Only Inside the SDK Layer

Implement filtering inside the runtime without a compiler-emitted contract, inferring the allowed tools from existing arguments or internal defaults.

This was considered because it localizes the implementation to the runtime boundary. It was not chosen because the PR body explicitly calls for compiler-owned tool state, preservation of explicitly disabled tools, and fail-closed validation; inferring from runtime inputs alone would make it harder to guarantee that the catalog, permissions, and compiled workflow declaration stay aligned.

### Consequences

#### Positive
- Generated workflows now express tool permissions more explicitly, making the allowed SDK surface auditable from compiled artifacts.
- The runtime can present a tool catalog that matches the compiled workflow contract, reducing capability drift and misleading tool visibility.
- Explicitly disabled tools such as `github`, `bash`, or editing capabilities can be preserved in the contract rather than being lost behind permissive defaults.

#### Negative
- The compiler and runtime now share an additional compatibility surface through `GH_AW_COPILOT_SDK_TOOL_CONFIG`, which must remain versioned and validated over time.
- A cross-cutting change to generated lock files increases maintenance burden because many compiled workflows need regeneration when the contract shape changes.
- Fail-closed behavior on malformed or inconsistent tool contracts can make workflow runs break more abruptly until configuration bugs are fixed.

#### Neutral
- The immediate implementation impact in this PR is primarily on generated `.lock.yml` files rather than handwritten workflow markdown or end-user workflow syntax.
- The contract introduces a structured place to carry future capability metadata, but this ADR does not by itself define every future tool type or permission rule.
- Evals workflows also move from permissive defaults to explicit shell-only permission sets, aligning test/runtime variants under the same general contract model.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
