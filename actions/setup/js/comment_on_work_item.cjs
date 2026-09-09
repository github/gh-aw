// @ts-check
const { createAzureDevOpsWorkItemHandler } = require("./azure_devops_work_items.cjs");
async function main(config = {}) {
  return createAzureDevOpsWorkItemHandler("ado_comment_on_work_item", config);
}
module.exports = { main };
