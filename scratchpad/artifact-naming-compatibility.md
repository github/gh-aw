# Artifact Naming Backward/Forward Compatibility

## Overview

The `gh aw logs` and `gh aw audit` commands maintain backward and forward compatibility with both old and new artifact naming schemes.

## How It Works

### Artifact Download Process

1. **GitHub Actions Upload**: Workflows upload files with artifact names:
   - Old naming (pre-v5): `aw_info.json`, `safe_output.jsonl`, `agent_output.json`, `prompt.txt`
   - New naming (v5+): `aw-info`, `safe-output`, `agent-output`, `prompt`

2. **GitHub CLI Download**: When running `gh run download <run-id>`:
   - Creates a directory for each artifact using the artifact name
   - Extracts files into that directory preserving original filenames
   - Example: Artifact `aw-info` containing `aw_info.json` → `aw-info/aw_info.json`

3. **Flattening**: The `flattenSingleFileArtifacts()` function:
   - Detects directories containing exactly one file
   - Moves the file to the root directory
   - Removes the empty artifact directory
   - Example: `aw-info/aw_info.json` → `aw_info.json`

4. **CLI Commands**: Both `logs` and `audit` commands expect files at root:
   - `aw_info.json` - Engine configuration
   - `safe_output.jsonl` - Safe outputs
   - `agent_output.json` - Agent outputs
   - `prompt.txt` - Input prompt

## Compatibility Matrix

### Single-File Artifacts

These artifacts contain exactly one file and are flattened to the root directory by `flattenSingleFileArtifacts()`:

| Artifact Name (Old) | Artifact Name (New) | File in Artifact | After Flattening | CLI Expects |
|---------------------|---------------------|------------------|------------------|-------------|
| `aw_info.json` | `aw-info` | `aw_info.json` | `aw_info.json` | ✅ |
| `safe_output.jsonl` | `safe-output` | `safe_output.jsonl` | `safe_output.jsonl` | ✅ |
| `agent_output.json` | `agent-output` | `agent_output.json` | `agent_output.json` | ✅ |
| `prompt.txt` | `prompt` | `prompt.txt` | `prompt.txt` | ✅ |
| `threat-detection.log` | `detection` | `detection.log` | `detection.log` | ✅ |

### Multi-File Artifacts

These artifacts are initially downloaded by `gh run download` as directory trees that retain their internal structure. However, unlike the single-file artifact handling above, `gh aw logs` / `gh aw audit` may perform additional post-processing for some multi-file artifacts (notably `agent` and `activation`) to move expected files into the final layout used by the CLI.

| Artifact Name | Constant | Contents | Notes |
|---------------|----------|----------|-------|
| `firewall-audit-logs` | `constants.FirewallAuditArtifactName` | AWF structured audit/observability logs | Uploaded by all firewall-enabled workflows; retains directory structure after download |
| `agent` | `constants.AgentArtifactName` | Unified agent job outputs (logs, safe outputs, token usage) | Downloaded as a directory tree, then post-processed by CLI flattening/reorganization helpers |
| `activation` | `constants.ActivationArtifactName` | Activation job output (`aw_info.json`, `prompt.txt`) | Downloaded as a directory tree, then post-processed by CLI flattening helpers for downstream use |

#### `firewall-audit-logs` Directory Structure

The `firewall-audit-logs` artifact (constant: `constants.FirewallAuditArtifactName`) is uploaded by all firewall-enabled agentic workflows. It is **separate** from the `agent` artifact and must be downloaded independently.

```text
firewall-audit-logs/
├── api-proxy-logs/
│   └── token-usage.jsonl        ← Token usage data (input/output/cache tokens per request)
├── squid-logs/
│   └── access.log               ← Network policy log (domain allow/deny decisions)
├── audit.jsonl                  ← Firewall audit trail (policy matches, rule evaluations)
└── policy-manifest.json         ← Policy configuration snapshot
```

**Downloading firewall audit logs with `gh run download`:**

```bash
# Download only the firewall-audit-logs artifact
gh run download <run-id> -n firewall-audit-logs

# The data is then at:
#   firewall-audit-logs/api-proxy-logs/token-usage.jsonl
#   firewall-audit-logs/squid-logs/access.log
#   firewall-audit-logs/audit.jsonl
#   firewall-audit-logs/policy-manifest.json
```

**Recommended: Use `gh aw logs` instead of `gh run download`:**

The `gh aw logs` command knows the correct artifact names and handles backward compatibility automatically:

```bash
# Download and analyze all logs (including firewall data)
gh aw logs <run-id>

# Download only firewall artifacts
gh aw logs <run-id> --artifacts firewall

# Output as JSON for programmatic use
gh aw logs <run-id> --artifacts firewall --json
```

> **⚠️ Common mistake:** Downloading `agent-artifacts` or `agent` and expecting to find `token-usage.jsonl` there. Token usage data lives in the `firewall-audit-logs` artifact, not in the agent artifact.

## Risks / Edge Cases

The `flattenSingleFileArtifacts()` function assumes that artifacts containing exactly one file are safe to flatten to the root directory. The following edge cases present data-loss or correctness risks and should be handled explicitly:

1. **Empty artifact directory:** An artifact directory that was created but contains zero files will not trigger flattening. However, if a caller iterates over the directory expecting to find a specific file (e.g., `aw_info.json`), it will silently receive nothing. The function must skip empty directories without error, but callers must not treat a missing file as an empty file — they must surface a "not found" error.

2. **Unexpected multi-file artifact where single-file was expected:** If an artifact that is always expected to contain one file (e.g., `aw-info`) unexpectedly contains multiple files after a future schema change, `flattenSingleFileArtifacts()` will skip flattening and leave the directory intact. Downstream CLI code that looks for `aw_info.json` at the root will then fail with an unspecific "file not found" error rather than a diagnostic that identifies the artifact's unexpected structure. A guard should log a warning when a nominally single-file artifact is encountered with multiple files.

3. **File name collision at root:** If two separate artifact directories each contain a file with the same name (e.g., both `aw-info/aw_info.json` and a legacy root-level `aw_info.json` exist in the same download directory), flattening would overwrite the pre-existing file. The function must check for conflicts before moving files and abort with an error rather than silently overwriting.

4. **Artifact name matches an existing root file:** A new artifact type introduced in a future version might coincidentally produce a file whose name collides with one already written by an older artifact. The flattening logic must detect this case (same as collision above) and surface it explicitly instead of masking the conflict.

5. **Non-directory entry in artifact list:** If `gh run download` creates a file (not a directory) at the artifact level — for example, due to a platform-specific behavior — `flattenSingleFileArtifacts()` must skip it gracefully without attempting to read its contents as a directory.

## Testing

Tests ensure compatibility:
- `TestArtifactNamingBackwardCompatibility`: Tests both old and new naming
- `TestAuditCommandFindsNewArtifacts`: Verifies audit command works with new names
- `TestFlattenSingleFileArtifactsWithAuditFiles`: Tests flattening with new names

## Design Rationale

The separation of concerns ensures compatibility:
- **Artifact Names**: Metadata for GitHub Actions (can change)
- **File Names**: Actual file content (preserved)
- **Flattening**: Bridges the gap between artifact structure and CLI expectations

This design means the CLI doesn't need to know about artifact naming changes - it always looks for the same filenames at the root level, regardless of how they were packaged as artifacts.
