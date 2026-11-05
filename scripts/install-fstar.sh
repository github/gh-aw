#!/bin/bash
set -euo pipefail

expected_sha256="${1:-}"
if [[ ! "$expected_sha256" =~ ^[[:xdigit:]]{64}$ ]]; then
    echo "Usage: $0 <published F* archive SHA256>" >&2
    exit 2
fi

if [[ "$(uname -s)" != Linux || "$(uname -m)" != x86_64 ]]; then
    echo "The pinned F* toolchain requires Linux x86_64 (use ubuntu-24.04)." >&2
    exit 1
fi

tools=/tmp/gh-aw/agent/fstar-tools
mkdir -p "$tools"
curl --fail --location --silent --show-error \
    --retry 2 --connect-timeout 10 --max-time 240 \
    --output "$tools/fstar.tar.gz" \
    'https://github.com/FStarLang/FStar/releases/download/v2026.10.25/fstar-v2026.10.25-Linux-x86_64.tar.gz'
printf '%s  %s\n' "$expected_sha256" "$tools/fstar.tar.gz" | sha256sum --check --strict
tar -xzf "$tools/fstar.tar.gz" --no-same-owner -C "$tools"

fstar="$tools/fstar/bin/fstar.exe"
z3="$tools/fstar/lib/fstar/z3-5.3.0/bin/z3"
"$fstar" --version | tee "$tools/fstar-version.txt"
grep -Fx 'F* 2026.10.25' "$tools/fstar-version.txt"
"$z3" -version | tee "$tools/z3-version.txt"
grep -Fx 'Z3 version 5.3.0 - 64 bit' "$tools/z3-version.txt"

smoke=$(mktemp -d "$tools/smoke.XXXXXX")
trap 'rm -rf "$smoke"' EXIT
printf '%s\n' 'module Smoke' 'let increment (n:nat) : Tot (r:nat{r == n + 1}) = n + 1' > "$smoke/Smoke.fst"
"$fstar" --smt "$z3" --z3version 5.3.0 --cache_off --z3rlimit 50 \
    --message_format human "$smoke/Smoke.fst"
