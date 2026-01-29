# Artifact Naming Backward/Forward Compatibility

## Overview

The `gh aw logs` and `gh aw audit` commands maintain full backward and forward compatibility with both old and new artifact naming schemes.

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

| Artifact Name (Old) | Artifact Name (New) | File in Artifact | After Flattening | CLI Expects |
|---------------------|---------------------|------------------|------------------|-------------|
| `aw_info.json` | `aw-info` | `aw_info.json` | `aw_info.json` | ✅ |
| `safe_output.jsonl` | `safe-output` | `safe_output.jsonl` | `safe_output.jsonl` | ✅ |
| `agent_output.json` | `agent-output` | `agent_output.json` | `agent_output.json` | ✅ |
| `prompt.txt` | `prompt` | `prompt.txt` | `prompt.txt` | ✅ |

## Testing

Comprehensive tests ensure compatibility:
- `TestArtifactNamingBackwardCompatibility`: Tests both old and new naming
- `TestAuditCommandFindsNewArtifacts`: Verifies audit command works with new names
- `TestFlattenSingleFileArtifactsWithAuditFiles`: Tests flattening with new names

## Key Insight

The separation of concerns ensures compatibility:
- **Artifact Names**: Metadata for GitHub Actions (can change)
- **File Names**: Actual file content (preserved)
- **Flattening**: Bridges the gap between artifact structure and CLI expectations

This design means the CLI doesn't need to know about artifact naming changes - it always looks for the same filenames at the root level, regardless of how they were packaged as artifacts.
