---
"gh-aw": patch
---

Fix sandboxed threat detection with ARC/DinD topology by staging the detector and inputs on the shared runner volume, mounting the detection directory read-write, and collecting results for the existing artifact and conclusion steps.
