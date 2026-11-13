---
name: awf-release-integrator
description: Upgrade gh-aw firewall and MCP gateway releases, refresh trusted pins, and identify follow-up spec tasks.
---

# AWF Release Integrator

Use this skill when updating `github/gh-aw` to newer `github/gh-aw-firewall` or `github/gh-aw-mcpg` releases.

## Goal

Land the version bump cleanly, rebuild the generated artifacts, and review upstream release/spec changes for any follow-up work that should accompany the bump.

## Required sources

Consult these sources before editing anything:

1. The latest `github/gh-aw-firewall` and `github/gh-aw-mcpg` release metadata and bodies.
2. The current gh-aw version pins in `pkg/constants/version_constants.go`.
3. The canonical AWF config sources spec in `specs/awf-config-sources-spec.md`.
4. The embedded AWF schema in `pkg/workflow/schemas/awf-config.schema.json`.
5. AWF config integration code in:
   - `pkg/workflow/awf_config.go`
   - `pkg/workflow/awf_config_build.go`
   - `pkg/workflow/awf_config_schema.go`
   - `pkg/workflow/awf_config_policy.go`
   - `pkg/workflow/awf_helpers.go`
   - related AWF tests under `pkg/workflow/`

For upstream spec review, compare these files from the target `github/gh-aw-firewall` release or tag:

- `docs/awf-config-spec.md`
- `docs/awf-config.schema.json`
- `src/awf-config-schema.json`
- any release assets such as `awf-config.schema.json`

## Update procedure

1. Read `pkg/constants/version_constants.go` and record:
   - `DefaultFirewallVersion`
   - every `AWF*MinVersion` constant
   - `DefaultMCPGatewayVersion`
2. Look up the latest release metadata for both `github/gh-aw-firewall` and `github/gh-aw-mcpg`; assess each default independently.
3. If neither release is newer than its default, report that no version bump is needed and only continue with spec/release-note review if explicitly requested.
4. For a newer AWF release:
   - bump `DefaultFirewallVersion`
   - update any `AWF*MinVersion` constants that must move because the new release introduces or changes gated flags/features
   - add a reviewed `checksums.txt` digest to `actions/setup/sh/release_integrity.sh` using `scripts/update-release-pins.sh`. Obtain the digest from an independently trusted source such as the GitHub release API asset metadata, and verify it matches the downloaded manifest; never trust a checksum downloaded only alongside the release.
   - add the exact digest to the pin assertions in `actions/setup/sh/release_integrity_test.sh` and confirm `pkg/workflow/firewall_version_pinning_test.go` accepts the new compiler default.
5. For a newer MCP gateway release:
   - bump `DefaultMCPGatewayVersion` and update its direct test expectation
   - add the new image and verified digest to `.github/aw/actions-lock.json`; run `make build` to synchronize `pkg/actionpins/data/action_pins.json` and `pkg/workflow/data/action_pins.json`
6. Review release notes for:
   - new flags
   - removed or deprecated flags
   - schema/config additions
   - security fixes
   - behavioral changes that could require new tests, docs, or ADR/spec updates
7. Review the upstream AWF specification and schema changes against:
   - `pkg/workflow/schemas/awf-config.schema.json`
   - `specs/awf-config-sources-spec.md`
   - local AWF config generation and validation code
8. Update any directly related gh-aw files needed for a complete integration, such as:
   - embedded schema copies
   - version-gated helpers/tests
   - specs or ADRs documenting newly surfaced AWF behavior
9. Run `make build`, then run `make recompile` twice to refresh image SHA pins and generated workflow locks.
10. Add or update a patch changeset when the bump changes shipped behavior.

## Required validation

After the update procedure, run focused validation for any touched Go code or schema logic, especially AWF-related tests, plus the release-integrity tests when adding an AWF checksum pin.

## Expected output

Summarize:

- current gh-aw AWF version → target release
- current MCP gateway version → target release
- updated constants, reviewed checksum pin, and image digests
- release-note highlights
- specification/schema differences reviewed
- additional recommended follow-up updates that are not yet implemented

## Review heuristics

When deciding whether more than a version bump is needed, specifically check for:

- new AWF schema properties not represented in gh-aw
- new CLI flags that need `AWF*MinVersion` gates
- config fields present in schema but absent from gh-aw generation/validation
- drift that should update `specs/awf-config-sources-spec.md`
- tests whose expected pinned AWF version or schema URLs need refresh
