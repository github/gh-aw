#!/usr/bin/env bash
set +o histexpand

# Sourced by installers from the trusted setup action, never from the download origin.
# Review new pins against authenticated release provenance before committing them.
# Add pins with scripts/update-release-pins.sh using an independently trusted manifest SHA256.
# Each entry binds repository, immutable release version, asset name, and SHA256.
# The optional fourth argument is a literal pin from trusted workflow frontmatter.
release_artifact_sha256() {
  local repository="$1" version="$2" asset="$3" digest="${4:-}"
  version="v${version#v}"
  if [ -z "$digest" ]; then
    digest=$(awk -v repository="$repository" -v version="$version" -v asset="$asset" '
    $1 == repository && $2 == version && $3 == asset { count++; digest = $4 }
    END { if (count == 1) print digest; else exit 1 }
  ' <<'PINS'
github/gh-aw-firewall v0.28.62 checksums.txt 0b03f6ea5b638356a799fa541ade4fd7902a6ac8d7c3f0252ee09d8259a4ce8f
github/copilot-cli v1.0.98 SHA256SUMS.txt 372f0c8d3d50076455cd19ef080dc59dc2a32401d8b2d4f77ce190c8ab13bc59
github/copilot-cli v1.0.21 SHA256SUMS.txt fd9b2fb92a39aeb2a41815a6aa0bf3853307c812b3451a45f5a2217be003987d
github/gh-aw-firewall v0.28.70 checksums.txt 9a35879a2f405fa7a1aa275a67b96d01eb0d0c6c8fd850637a1ad8ad55b500da
github/copilot-cli v1.0.101 SHA256SUMS.txt 018625eac68670d66ede904b9b82cdcd6ab8f87ba20e6e1fbdf0b62c8295d5be
github/gh-aw-firewall v0.29.0 checksums.txt 2d49727b3b65630b5d0ba5375168687a852afac319d05c911d4769c43fa2ed61
github/copilot-cli v1.0.103 SHA256SUMS.txt 26cd3e4b32cde498e4961fb4d48e7b85d7859fc69af1a645a19201fdd577bdb2
PINS
    ) || {
      local config_field="engine"
      if [ "$repository" = github/gh-aw-firewall ]; then
        config_field="sandbox.agent"
      fi
      echo "ERROR: No reviewed SHA256 pin for ${repository} ${version} ${asset}; refusing to install an unverified release." >&2
      echo "Fix: run 'gh extension upgrade gh-aw', then 'gh aw compile <workflow.md>' and commit the regenerated .lock.yml. Ensure the workflow uses a setup action revision containing this release pin." >&2
      echo "For an intentional release override, set ${config_field}.version to a literal release version and ${config_field}.checksums-sha256 to the independently authenticated SHA256 of ${asset} (not the CLI archive)." >&2
      echo "Maintainers: add the reviewed pin with 'bash scripts/update-release-pins.sh ${repository} ${version} ${asset} <trusted-manifest-sha256>'; do not trust a digest solely because it was downloaded alongside the executable." >&2
      return 1
    }
  fi
  if [[ ! "$digest" =~ ^[0-9a-f]{64}$ ]]; then
    echo "ERROR: Invalid reviewed SHA256 pin for ${repository} ${version} ${asset}" >&2
    return 1
  fi
  printf '%s\n' "$digest"
}

verify_release_digest() {
  local file="$1" expected="$2" actual
  if [[ ! "$expected" =~ ^[0-9a-f]{64}$ ]]; then
    echo "ERROR: A trusted lowercase SHA256 digest is required" >&2
    return 1
  fi
  if command -v sha256sum >/dev/null 2>&1; then
    actual=$(sha256sum "$file") || return 1
  elif command -v shasum >/dev/null 2>&1; then
    actual=$(shasum -a 256 "$file") || return 1
  else
    echo "ERROR: No sha256sum or shasum found" >&2
    return 1
  fi
  actual="${actual%% *}"
  if [ "$actual" != "$expected" ]; then
    echo "ERROR: Trusted SHA256 verification failed for $(basename "$file"); refusing installation" >&2
    return 1
  fi
}
