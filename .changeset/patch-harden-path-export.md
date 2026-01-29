---
"gh-aw": patch
---

Sanitize the PATH export used by AWF firewall agents by sourcing a new `sanitize_path.sh` helper so empty elements and stray colons are removed before updating PATH.
