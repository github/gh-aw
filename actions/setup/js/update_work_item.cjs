// @ts-check
const { createAzureDevOpsWorkItemHandler } = require("./azure_devops_work_items.cjs");

/**
 * Creates the Azure DevOps update work item safe-output handler.
 * @param {Object} [config] - Handler configuration
 * @returns {Promise<Function>} Message handler
 */
async function main(config = {}) {
  return createAzureDevOpsWorkItemHandler("ado_update_work_item", config);
}

module.exports = { main };
