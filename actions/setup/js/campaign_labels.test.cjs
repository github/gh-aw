// @ts-check
import { describe, it, expect, afterEach } from "vitest";
import { formatCampaignLabel, getCampaignLabelsFromEnv } from "./campaign_labels.cjs";

describe("formatCampaignLabel", () => {
  it("should format simple campaign ID", () => {
    expect(formatCampaignLabel("test")).toBe("z_campaign_test");
  });

  it("should convert uppercase to lowercase", () => {
    expect(formatCampaignLabel("TEST")).toBe("z_campaign_test");
    expect(formatCampaignLabel("MyTest")).toBe("z_campaign_mytest");
  });

  it("should replace spaces with hyphens", () => {
    expect(formatCampaignLabel("my test")).toBe("z_campaign_my-test");
    expect(formatCampaignLabel("my   test")).toBe("z_campaign_my-test");
  });

  it("should replace underscores with hyphens", () => {
    expect(formatCampaignLabel("my_test")).toBe("z_campaign_my-test");
    expect(formatCampaignLabel("my___test")).toBe("z_campaign_my-test");
  });

  it("should handle mixed spaces and underscores", () => {
    expect(formatCampaignLabel("my_ _test")).toBe("z_campaign_my-test");
    expect(formatCampaignLabel("my _ test")).toBe("z_campaign_my-test");
  });

  it("should handle numeric campaign IDs", () => {
    expect(formatCampaignLabel("123")).toBe("z_campaign_123");
    expect(formatCampaignLabel("2024_Q1")).toBe("z_campaign_2024-q1");
  });

  it("should handle empty string", () => {
    expect(formatCampaignLabel("")).toBe("z_campaign_");
  });

  it("should handle complex campaign IDs", () => {
    expect(formatCampaignLabel("My_Test Campaign 123")).toBe("z_campaign_my-test-campaign-123");
  });
});

describe("getCampaignLabelsFromEnv", () => {
  // Store original env var
  const originalEnv = process.env.GH_AW_CAMPAIGN_ID;

  afterEach(() => {
    // Restore original env var
    if (originalEnv !== undefined) {
      process.env.GH_AW_CAMPAIGN_ID = originalEnv;
    } else {
      delete process.env.GH_AW_CAMPAIGN_ID;
    }
  });

  it("should return disabled with empty labels when no campaign ID", () => {
    delete process.env.GH_AW_CAMPAIGN_ID;
    const result = getCampaignLabelsFromEnv();

    expect(result.enabled).toBe(false);
    expect(result.labels).toEqual([]);
  });

  it("should return disabled with empty labels when campaign ID is empty string", () => {
    process.env.GH_AW_CAMPAIGN_ID = "";
    const result = getCampaignLabelsFromEnv();

    expect(result.enabled).toBe(false);
    expect(result.labels).toEqual([]);
  });

  it("should return disabled with empty labels when campaign ID is whitespace", () => {
    process.env.GH_AW_CAMPAIGN_ID = "   ";
    const result = getCampaignLabelsFromEnv();

    expect(result.enabled).toBe(false);
    expect(result.labels).toEqual([]);
  });

  it("should return enabled with generic and specific labels", () => {
    process.env.GH_AW_CAMPAIGN_ID = "test";
    const result = getCampaignLabelsFromEnv();

    expect(result.enabled).toBe(true);
    expect(result.labels).toEqual(["agentic-campaign", "z_campaign_test"]);
  });

  it("should trim whitespace from campaign ID", () => {
    process.env.GH_AW_CAMPAIGN_ID = "  test  ";
    const result = getCampaignLabelsFromEnv();

    expect(result.enabled).toBe(true);
    expect(result.labels).toEqual(["agentic-campaign", "z_campaign_test"]);
  });

  it("should handle uppercase campaign ID", () => {
    process.env.GH_AW_CAMPAIGN_ID = "TEST";
    const result = getCampaignLabelsFromEnv();

    expect(result.enabled).toBe(true);
    expect(result.labels).toEqual(["agentic-campaign", "z_campaign_test"]);
  });

  it("should handle campaign ID with spaces", () => {
    process.env.GH_AW_CAMPAIGN_ID = "my test";
    const result = getCampaignLabelsFromEnv();

    expect(result.enabled).toBe(true);
    expect(result.labels).toEqual(["agentic-campaign", "z_campaign_my-test"]);
  });

  it("should handle campaign ID with underscores", () => {
    process.env.GH_AW_CAMPAIGN_ID = "my_test";
    const result = getCampaignLabelsFromEnv();

    expect(result.enabled).toBe(true);
    expect(result.labels).toEqual(["agentic-campaign", "z_campaign_my-test"]);
  });

  it("should handle complex campaign ID", () => {
    process.env.GH_AW_CAMPAIGN_ID = "My_Test Campaign 123";
    const result = getCampaignLabelsFromEnv();

    expect(result.enabled).toBe(true);
    expect(result.labels).toEqual(["agentic-campaign", "z_campaign_my-test-campaign-123"]);
  });

  it("should handle numeric campaign ID", () => {
    process.env.GH_AW_CAMPAIGN_ID = "123";
    const result = getCampaignLabelsFromEnv();

    expect(result.enabled).toBe(true);
    expect(result.labels).toEqual(["agentic-campaign", "z_campaign_123"]);
  });
});
