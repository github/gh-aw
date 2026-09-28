---
graders:
  # Fraction of files touched by the reference patch that the agent located
  # at any point during the run. Higher is better.
  # Built-in grader implemented in actions/setup/js/trajectory_graders.cjs.
  # Built-in graders run on every trace; importing this fragment is optional.
  code-search-recall: {}
---

<!--
code-search-recall compares reference.patch.files[] (strings or { path }
records) with the resources[] entries whose kind is file, after stripping a
leading "./" or "/" from each path. The IR builder records a file resource for
every file the agent located (searched, listed, read, or edited). Runs without a
reference patch are not applicable; a reference file without a path or a trace
without a well-formed resources[] collection is unavailable.
-->
