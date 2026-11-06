---
env:
  KOKA_BIN: /tmp/gh-aw/agent/koka/bin/koka
steps:
  - name: Install Koka 3.2.9 (Linux x64)
    shell: bash
    run: |
      # runner-guard:ignore RGS-018
      set -euo pipefail
      test "$(uname -s)" = Linux
      test "$(uname -m)" = x86_64
      sudo apt-get update -qq
      sudo apt-get install -y --no-install-recommends \
        build-essential ca-certificates curl libgmp-dev
      tools=/tmp/gh-aw/agent/koka
      mkdir -p "$tools"
      curl --fail --location --silent --show-error \
        --retry 2 --connect-timeout 10 --max-time 180 \
        --output "$tools/koka.tar.gz" \
        'https://github.com/koka-lang/koka/releases/download/v3.2.9/koka-v3.2.9-linux-x64.tar.gz'
      printf '%s  %s\n' \
        310459831a7c6fa6a0cd8e0e02d6b5b3b36441d5e7ce24168bda016cd2e95eee \
        "$tools/koka.tar.gz" | sha256sum --check --strict
      tar -xzf "$tools/koka.tar.gz" --no-same-owner -C "$tools"
      printf '%s\n' "$tools/bin" >> "$GITHUB_PATH"
      "$tools/bin/koka" --version
---

<!--
Import shared/koka.md to install checksum-verified Koka 3.2.9 and native
build dependencies on a Linux x64 runner. KOKA_BIN and the bundled libraries
live under the sandbox-visible /tmp/gh-aw/agent mount. Separate containers
still need their own C compiler and headers. This fragment does not grant
shell tools, network access, or safe outputs.
-->
