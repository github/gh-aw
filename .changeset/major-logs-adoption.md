---
"gh-aw": major
---

Use `gh aw logs --adoption` for shared delivery and adoption reporting, without a
standalone reporting command.
For one run, pipe its ID to `gh aw logs --stdin --repo owner/repo --adoption --json`.
Read per-run observations from `.adoption[]` and episode summaries from `.episode_adoption`.
Combine `--episodes --adoption` to include recorded episode membership and hops
with the automatically joined adoption summaries, without a separate offline join.
Remove historical repository scoring and the separate offline episode-join command.
Episode summaries are included in the logs report. JSONL export uses `--adoption-dir`.

Reuse successful GitHub API reads across selected runs within each collection,
including GraphQL queries and labels already returned on issue or PR objects.
Request 100 items per page for paginated REST reads.
Batch impact-report adoption collection into one CLI invocation. Later collections
still check current state, and failed or partial responses are not retained.
