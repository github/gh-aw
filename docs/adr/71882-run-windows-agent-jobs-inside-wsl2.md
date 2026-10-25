# ADR-71882: Run Windows Agent Job Linux Workloads Inside WSL2

**Date**: 2026-10-25
**Status**: Draft
**Deciders**: Unknown; pending maintainer review of PR #71882.

---

### Context

Agentic workflows compiled for `runs-on: windows-latest` previously ran every generated `run:` step with `shell: bash` on the native Windows host. The agent runtime (setup.sh, AWF firewall, MCP gateway, Docker-based containers) assumes a Linux userland and Linux container images, so Windows jobs tried to pull Linux images outside WSL and failed — blocking the Windows and Windows Grower workflows (issue #71387). Native JavaScript actions (`actions/github-script`, artifact upload/download, etc.) must still run on the Windows host and need to see the same runtime files, outputs, environment, and `PATH` additions that the Linux steps produce. Separately, the audit report could list created items without a resolvable resource identity.

### Decision

We will execute the gh-aw Linux runtime on Windows runners inside a dedicated WSL2 distribution (`Ubuntu-24.04`, user `gh-aw`) instead of the native Windows shell. The compiler marks Windows jobs that contain the `Setup Scripts` step with `GH_AW_WINDOWS_WSL=true` and, from that step on, sets a custom Actions shell (`node "${{ runner.temp }}/gh-aw/actions/windows_wsl.cjs" {0}`) that dispatches each script into WSL, while explicitly declared native shells are preserved. `windows_wsl.cjs` provisions the distribution (apt packages, Docker, checksum-verified Node.js matching the runner version), bridges `/tmp/gh-aw` to Windows through a directory symlink to `\\wsl.localhost\...`, forwards environment via `WSLENV`, and translates `GITHUB_ENV`/`GITHUB_OUTPUT`/`GITHUB_PATH` paths between `/mnt/<drive>/` and `<DRIVE>:/` forms. Windows jobs without the setup step keep native `shell: bash`. As a related, smaller decision, audit reports recover missing item identities only from exact temporary-ID mappings in execution artifacts (never from agent-authored text) and record an `identity_unavailable_reason` rather than inventing links.

### Alternatives Considered

#### Alternative 1: Keep native Windows bash (Git Bash) and use Windows containers / Docker Desktop

Continue emitting `shell: bash` on the Windows host and make the runtime work against the host's Docker engine. This was the previous behavior and needs no WSL provisioning, but the hosted Windows Docker engine runs Windows containers, so the Linux images required by AWF and the MCP gateway cannot be pulled — the exact failure this PR fixes. Porting the whole runtime to Windows containers would be a far larger effort.

#### Alternative 2: Run the entire job in WSL (or drop Windows agent support / route to a Linux runner)

Wrap the whole job (including native JS actions) in WSL, or simply refuse `windows-latest` for agent jobs and require Linux runners. Running everything in WSL is not possible because `uses:` actions are executed by the runner on the host. Rejecting Windows runners would avoid the bridging complexity but removes the ability to run agents against Windows-specific tooling, which the Windows and Windows Grower workflows exist to exercise.

#### Alternative 3: Use a custom container job instead of WSL

Declare a Linux `container:` for the job. GitHub-hosted Windows runners do not support Linux container jobs, so this was not viable.

### Consequences

#### Positive
- Windows agent jobs reuse the same Linux setup.sh, firewall, and container stack as Linux jobs instead of a parallel Windows implementation.
- Native JS actions and artifact uploaders see the same `/tmp/gh-aw` runtime tree, outputs, env, and host `PATH` entries via explicit path translation.
- Node.js inside WSL is pinned to the runner's version and authenticated against the official `SHASUMS256.txt` manifest.
- Audit reports no longer fabricate links; missing identities are explained, and identity recovery uses only authoritative execution artifacts.

#### Negative
- Significant per-job setup cost on Windows: WSL distribution install, `apt-get` package installs, Docker start, and Node download (bounded by a 15-minute timeout).
- The bridge is a custom, string-based translation layer (drive-letter regexes, `WSLENV` flags, CRLF/BOM stripping, `GITHUB_PATH` filtering) that may miss edge cases such as UNC paths or values that only partially look like Windows paths.
- Compiler detection relies on the literal `      - name: Setup Scripts\n` step marker and YAML indentation, which is brittle to rendering changes.
- The `gh-aw` WSL user is granted passwordless sudo and Docker group membership, expanding the in-VM privilege surface (contained within the ephemeral runner VM).
- Creating `/tmp/gh-aw` as a Windows directory symlink requires symlink privileges on the runner and fails closed if a different path already exists.

#### Neutral
- `setBashShell` is generalized to `setRunnerShell(step, shell)`; Windows `ERLANG_HOME` capture steps keep native `bash`.
- Windows workflows replace the deprecated `missing-tool` safe output with `report-missing-tool` so strict dry-run validation passes; lock files are regenerated.
- The audit cache version is bumped, invalidating previously cached audit results, and schemas gain `identity_unavailable_reason`.
- A post step (`cleanupWindowsRuntime`) removes `/tmp/gh-aw` inside WSL.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
