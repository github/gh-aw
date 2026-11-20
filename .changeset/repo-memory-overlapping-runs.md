---
"gh-aw": patch
---

Keep repo-memory rows saved by overlapping runs. The agent job now records the commit it cloned for each memory, and the `push_repo_memory` job replays only this run's changes onto the latest branch instead of copying the agent's whole files over it: files the run did not change keep their latest version, `.jsonl` files changed by both runs keep the rows of both, and other files keep this run's version of the lines both runs changed. Every repo-memory commit now requires the branch head it was prepared on, so a save that lands just before another run commits leads to a rebase and retry rather than being overwritten. When the starting commit is unknown or no longer in the branch history, the push copies files as before and logs a warning.
