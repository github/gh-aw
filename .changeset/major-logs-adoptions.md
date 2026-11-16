---
"gh-aw": major
---

Move live adoption evaluation to `gh aw logs --adoptions` and remove `gh aw adoptions`.
For one run, pipe its ID to `gh aw logs --stdin --repo owner/repo --adoptions --json`.
Read per-run observations from `.adoptions[]` and episode summaries from `.episode_adoptions`.
Combine `--episodes --adoptions` to include recorded episode membership and hops
with the automatically joined adoption summaries, without a separate offline join.
Remove historical repository scoring and the separate offline episode-join command.
Episode summaries are included in the logs report. JSONL export uses `--adoptions-dir`.

Reuse successful GitHub API reads across selected runs within each collection,
including GraphQL queries and labels already returned on issue or PR objects.
Request 100 items per page for paginated REST reads.
Batch impact-report adoption collection into one CLI invocation. Later collections
still check current state, and failed or partial responses are not retained.
