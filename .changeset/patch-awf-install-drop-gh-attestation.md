---
"gh-aw": patch
---

Remove `gh attestation verify` and release-identity checks from `install_awf_binary.sh`. AWF installs now rely on the bundled `checksums.txt` digest pin and per-asset SHA256 verification.
