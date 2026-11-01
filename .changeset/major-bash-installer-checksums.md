---
"gh-aw": major
---

Require release checksums before the Bash installer executes or installs a downloaded binary.

**Breaking change:** Pins to older releases without `checksums.txt` now fail instead of installing an unverified binary. Unavailable or invalid checksums leave the existing installation unchanged.

**Migration:** Update pins to a release that publishes checksums. If an older pin is unavoidable, explicitly pass `--skip-checksum` to the Bash installer. This disables integrity verification and may allow a corrupted or tampered binary to be executed and installed; it is never enabled automatically.

Latest installs retry the already-resolved release tag's checksum URL if the latest checksum redirect is unavailable. The fallback checksum must match the actual downloaded binary; unavailable or mismatched fallback checksums still abort installation.
