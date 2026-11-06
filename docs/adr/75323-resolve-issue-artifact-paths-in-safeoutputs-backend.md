# ADR-75323: Resolve Issue Artifact Paths in the Unprivileged Safe-Outputs Backend

**Date**: 2026-11-06
**Status**: Accepted
**Deciders**: PR author and reviewers of [PR #75323](https://github.com/github/gh-aw/pull/75323)

---

### Context

Workflows need to attach plans, links, documents, and HTML source to a newly created issue without placing every artifact in the issue body or reproducing a generated file in a tool call. The agent-side safeoutputs MCP backend can read files in the agent's execution environment, while the privileged safe-output handler creates the issue and its artifacts using GitHub credentials. Passing an agent-supplied filename to that privileged handler would turn an output request into a privileged file-read capability and would also depend on files being present in a different job. Artifact inputs and contents remain untrusted even when their files are readable by the agent. Issue creation and artifact attachment are separate GitHub API operations, so partial publication must be explicit.

### Decision

We will resolve artifact `path` inputs in the unprivileged safeoutputs MCP backend and persist only their contents; the privileged `create_issue` handler will reject any unresolved path. Each artifact will provide a name, an optional supported type, and exactly one of inline `body` or literal absolute `path`. We will limit path reads to the workspace and the agent-owned `/tmp/gh-aw/agent` output directory rather than treating all of `/tmp` as agent output. The shared artifact-upload exclusions will reject `.git` path components, system directories, and home-directory credential/configuration paths before reading, even when those paths overlap an allowed root. Linux descriptor-guarded reads will require bounded, non-empty UTF-8 text without NUL bytes from an isolated regular file, rejecting symbolic links, hard-linked files, and detected path or file replacement.

The privileged handler will independently validate artifact shape, count, and body size before issue creation, sanitize publication content, and enforce the size bound again after content transformations. Artifact types are `generic`, `plan`, `link`, and `embedded_html`, with `generic` as the default. Embedded HTML will use existing content, tag/attribute, and comment sanitizers with strict URL protocol/domain filtering across the whole content, without synthetic Markdown fences or code-region exemptions; supported HTML structure will remain intact while unsupported tags become visible text. Defaults will allow at most 10 artifacts per issue and 1 MiB of UTF-8 content per artifact; a non-positive count limit will disable attachments. Human-readable size configuration will compile to the same byte limit enforced at runtime.

After creating an issue, we will attach artifacts serially using the issue-artifact REST endpoint and existing retry handling. An attachment failure will stop the remaining attachment sequence without deleting the issue or previously attached artifacts; normal issue assignment and parent linkage will still be attempted before returning the artifact failure. The result will retain the created issue's identity and identify the failing artifact and partial attachment count rather than reporting complete success.

### Alternatives Considered

These are architectural alternatives evaluated for this record, not a claim that the PR author previously deliberated each option.

#### Alternative 1: Inline bodies only

Requiring every artifact to supply its full body would avoid file access entirely and keep the tool contract smaller. It would also require the agent to reproduce generated documents and HTML in tool-call arguments, increasing duplication and the chance of transcription errors. We retain inline bodies but add guarded agent-side reads for generated files.

#### Alternative 2: Resolve paths in the privileged handler

Reading a filename immediately before attachment would keep resolution and publication together. However, that handler has publication credentials and a different filesystem lifecycle; accepting arbitrary agent-supplied filenames would cross the wrong trust boundary. Persisting content in the unprivileged job avoids granting that capability.

#### Alternative 3: Transfer files through workflow artifacts

A separate upload/download protocol could make generated files available to the publication job and support other payload formats later. It would introduce another storage lifecycle, manifest, and validation boundary for a feature that currently publishes bounded text. This design uses the existing safe-output content handoff instead and does not introduce binary attachments.

#### Alternative 4: Roll back the issue on attachment failure

Deleting the issue and earlier attachments could approximate all-or-nothing publication. The GitHub operations are not transactional, rollback could also fail, and deleting an already visible issue could discard useful links or subsequent activity. We retain completed operations, continue normal issue follow-up work, and explicitly report partial failure.

### Consequences

#### Positive

- Generated text can become an issue artifact without being copied into the tool call or the main issue body.
- The privileged publication handler does not interpret filenames or read agent-selected local files.
- Shared limits and validation reject oversized or malformed requests before issue creation, while final size checks cover transformed content.
- Serial attachment and explicit failure reporting make the surviving issue and partial publication understandable.

#### Negative

- Files outside the permitted roots, metadata files, linked files, binary data, and oversized text are intentionally unsupported even when an agent could otherwise read them.
- Confinement and metadata exclusions are not a secret detector: sensitive content deliberately copied into an otherwise permitted file can still be published.
- Filesystem guards depend on supported operating-system primitives and detect specific replacement races; they are not a separate filesystem sandbox or protection against every concurrent in-place content modification.
- A failed attachment can leave an issue with only some requested artifacts. Retrying the whole issue-creation request is not an atomic repair and can create duplicates.

#### Neutral

- File content is captured when the MCP request is accepted; later edits to the source file do not update the recorded artifact.
- Path-based inputs belong to the agent-side MCP interface. Direct privileged-handler callers must supply bodies instead.
- The path policy, publication sanitization, and post-creation failure behavior must remain aligned with their regression tests.

---

*Decision record for PR #75323, aligned with its path, HTML-safety, and partial-failure implementation.*
