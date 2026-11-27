### Failed Jobs in Workflow Run

**Workflow:** [{workflow_name}]({workflow_source_url})  
**Run:** {run_url}

The following jobs failed in this workflow run:

{failed_jobs_list}

**Failure boundary:** Non-agent job. A downstream publishing failure is separate from an engine driver process failure and does not establish a provider outage.

A failure boundary identifies where failure was observed, not its cause: either the engine driver process or a non-agent job. The job result and failed step numbers locate the failure, but do not identify its cause. Inspect the linked job before attributing it to validation, permissions, infrastructure, or an external service. If no failed step metadata is available, use the linked workflow run.

_Step diagnostics contain only bounded runtime metadata; step names, agent text, prompts, and tool payloads are excluded._
