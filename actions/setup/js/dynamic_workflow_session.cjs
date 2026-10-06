// @ts-check

const DYNAMIC_WORKFLOW_EVENT_TYPES = Object.freeze({
  task_started: "dynamicWorkflows.task_started",
  task_progress: "dynamicWorkflows.task_progress",
  task_updated: "dynamicWorkflows.task_updated",
  task_notification: "dynamicWorkflows.task_notification",
  background_tasks_changed: "dynamicWorkflows.background_tasks_changed",
});

module.exports = { DYNAMIC_WORKFLOW_EVENT_TYPES };
