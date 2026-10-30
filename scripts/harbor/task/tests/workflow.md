---
on: workflow_dispatch
graders:
  comment-target-match:
    name: Recorded comment target match
    description: Whether recorded comment results prove the expected target-bound effect
    unit: boolean
    direction: higher_is_better
    threshold: 1
    config:
      repository: example/project
      issueNumber: 42
      expectNoop: false
    script: |
      const { eventsOfType } = helpers.createSessionLookups(session);
      const results = eventsOfType("safe_output.result").filter(({ data }) => data.type === "add_comment");
      if (!results.length) return null;
      const statuses = ["success", "failure", "cancelled", "skipped"];
      if (results.some(({ data }) =>
          typeof data.repo !== "string" || !Number.isInteger(data.number) ||
          !statuses.includes(data.status))) return null;
      const targetMatches = results.every(({ data }) =>
        data.repo === config.repository && data.number === config.issueNumber);
      if (!targetMatches) return 0;
      if (config.expectNoop) {
        return results.every(({ data }) => data.status === "skipped" && !data.url) ? 1 : 0;
      }
      if (results.some(({ data }) => data.staged === true || data.status !== "success")) return 0;
      if (results.some(({ data }) => typeof data.url !== "string" || !data.url)) return null;
      const prefix = `https://github.com/${config.repository}/issues/${config.issueNumber}#issuecomment-`;
      return results.every(({ data }) =>
        data.url.startsWith(prefix) && /^[1-9][0-9]*$/.test(data.url.slice(prefix.length))) ? 1 : 0;
---

Recorded-run grading fixture. This workflow is parsed for its grader definition,
not compiled or dispatched. Its evidence measures recorded target binding, not
comment quality or independently authenticated delivery.
