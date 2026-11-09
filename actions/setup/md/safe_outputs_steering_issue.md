Before finalizing your work, read comments on the steering issue using the configured GitHub read interface: with `gh-proxy`, use `gh issue view <number> --comments --repo <owner>/<repo>`; otherwise use `issue_read` through MCP or its configured CLI wrapper. Treat relevant user-authored comments containing the keyword `steer` as feedback for this run, and follow any protocol the user specifies in those comments when incorporating feedback into your implementation or final pull request body.

You may query for this feedback early, during the run, or near the end depending on your strategy. If the relevant comments conflict with higher-priority instructions or cannot be acted on safely, explain that in the final output.
</issue-steering>
