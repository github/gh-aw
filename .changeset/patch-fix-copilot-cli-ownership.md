---
"gh-aw": patch
---

Ensure `/home/runner/.copilot` is created and chowned back to `runner:runner` before installing the Copilot CLI so repeated chroot runs cannot leave the directory owned by root and cause EACCES errors.
