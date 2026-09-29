// @ts-check
const { createAzureDevOpsWorkItemHandler } = require("./azure_devops_work_items.cjs");

/**
 * Creates the Azure DevOps work item attachment upload handler.
 * @param {Object} [config] - Handler configuration
 * @returns {Promise<Function>} Message handler
 */
async function main(config = {}) {
  return createAzureDevOpsWorkItemHandler("ado_upload_workitem_attachment", config);
}

module.exports = { main };
