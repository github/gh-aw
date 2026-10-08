// @ts-check
const { createAzureDevOpsWorkItemHandler } = require("./azure_devops_work_items.cjs");

/**
 * @typedef {import("./types/handler-factory").HandlerFactoryFunction} HandlerFactoryFunction
 */

/**
 * @type {HandlerFactoryFunction}
 */
async function main(config = {}) {
  return createAzureDevOpsWorkItemHandler("ado_update_work_item", config);
}

module.exports = { main };
