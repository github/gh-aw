# ADR-73984: Replace Python Enclave Installers with CJS Run by a Checksum-Pinned Node Binary

**Date**: 2026-07-21
**Status**: Draft
**Deciders**: pelikhan, [TODO: verify additional reviewers]

---

### Context

The setup action (`actions/setup/`) included two repository-owned privileged installers written in Python: `sh/install_awf_enclave_helper.py` and `sh/install_enclave_diagnostics.py`. Generated workflows ran them as root with `sudo /usr/bin/python3 -I`. They checked checksums, read files with size limits, created protected installation directories, published helper files atomically, and validated sudoers policies. The rest of the setup action is JavaScript and shell. Keeping Python files there meant a second language runtime had to be trusted and maintained. Nothing stopped new `.py` files from being added as build dependencies. Because these installers run with root privileges, the interpreter that runs them has to be trustworthy. The runner's system Node, or any Node found through a runner-writable `PATH`, does not meet that bar.

### Decision

We will replace both Python installers with standalone CommonJS files: `actions/setup/js/install_awf_enclave_helper.cjs` and `actions/setup/js/install_enclave_diagnostics_trusted.cjs`. They keep the same security behavior. They run under a separately pinned Node v24.22.1 binary. That binary is downloaded from nodejs.org, and the workflow checks the SHA-256 of both the archive and the extracted binary for x64 and ARM64 before installing it as root-owned mode `0500`. The workflow then runs it with `--no-addons --no-global-search-paths` (`pkg/workflow/enclave_installer_node.go`). We will also add `make lint-setup-no-python` (`scripts/check-setup-no-python.sh`) to `lint` and to every binary build target. It recursively rejects any `.py` file under `actions/setup/`, excluding `node_modules`. The upstream AWF storage helper (`enclave-storage-helper.py`), which is a downloaded release asset, stays in Python and is still invoked through `/usr/bin/python3 -I`.

### Alternatives Considered

#### Alternative 1: Keep the Python installers

Keep the two `.py` installers and run them with the system `python3 -I`. This works today and needs no new runtime pinning. We rejected it because it keeps a second language in a JS/shell-only action. The installer logic would also be tested and reviewed separately from the rest of the CJS code. Nothing would stop more Python from creeping into the setup action.

#### Alternative 2: Run the CJS installers with the runner's system Node or the Node on `PATH`

This is simpler and needs no extra download or checksum pins. We rejected it because the installers run as root, and a runner-writable `PATH` or an unverified system Node would let unprivileged code choose the privileged interpreter. Pinning the archive and binary digests keeps the integrity guarantees that `/usr/bin/python3 -I` was expected to provide.

#### Alternative 3: Rewrite the installers in shell

The setup action already uses shell, so this would avoid a pinned Node. [TODO: verify] We probably rejected it because bounded reads, atomic publication, and sudoers validation are harder to get right and to unit test in shell than in CJS. The PR adds Vitest coverage (`trusted_enclave_installers.test.cjs`) for that logic.

### Consequences

#### Positive
- The setup action source is JavaScript and shell only, and the build and lint enforce that rule.
- The installer logic is unit-tested with the existing JS test tooling, so the Go tests no longer need `python3` to parse or run the installers.
- The privileged interpreter is checksum-verified and root-owned, and it is not resolved through a mutable `PATH`.

#### Negative
- Each enclave install now downloads a Node archive of up to about 128 MiB, which adds network dependency, time, and another failure point.
- Node version and digest pins (archive and binary, for x64 and arm64) are now hard-coded in Go. Each Node upgrade needs a manual pin update.
- Only x64 and ARM64 are supported. Other architectures fail hard.
- Python is still required at runtime for the upstream AWF storage helper, so the move away from Python is only partial.

#### Neutral
- Generated workflow commands and installer checksum pins change, so all lock files must be recompiled.
- Every `make build*` target now runs the lint first, so a stray `.py` file fails the build before compilation.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
