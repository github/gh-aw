// @ts-check

/**
 * Campaign Labels Helper
 *
 * Utility functions for handling campaign labels in safe outputs.
 * These functions normalize campaign IDs and retrieve campaign labels from environment variables.
 */

const DEFAULT_AGENTIC_CAMPAIGN_LABEL = "agentic-campaign";

/**
 * Normalize campaign IDs to the same label format used by campaign discovery.
 * Mirrors actions/setup/js/campaign_discovery.cjs.
 * @param {string} campaignId
 * @returns {string}
 */
function formatCampaignLabel(campaignId) {
  return `z_campaign_${String(campaignId)
    .toLowerCase()
    .replace(/[_\s]+/g, "-")}`;
}

/**
 * Get campaign labels implied by environment variables.
 * Returns the generic "agentic-campaign" label and the campaign-specific "z_campaign_<id>" label.
 * @returns {{enabled: boolean, labels: string[]}}
 */
function getCampaignLabelsFromEnv() {
  const campaignId = String(process.env.GH_AW_CAMPAIGN_ID ?? "").trim();

  if (!campaignId) {
    return { enabled: false, labels: [] };
  }

  return {
    enabled: true,
    labels: [DEFAULT_AGENTIC_CAMPAIGN_LABEL, formatCampaignLabel(campaignId)],
  };
}

module.exports = {
  formatCampaignLabel,
  getCampaignLabelsFromEnv,
};
