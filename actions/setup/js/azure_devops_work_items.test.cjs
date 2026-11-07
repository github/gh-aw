import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createAzureDevOpsWorkItemHandler, resolveWorkItemReference } from "./azure_devops_work_items.cjs";

global.core = {
  debug: vi.fn(),
  info: vi.fn(),
  warning: vi.fn(),
};

describe("azure_devops_work_items", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    process.env.SYSTEM_ACCESSTOKEN = "test-token";
    process.env.AZURE_DEVOPS_ORG_URL = "https://dev.azure.com/test-org";
    process.env.SYSTEM_TEAMPROJECT = "test-project";
    process.env.GITHUB_RUN_ID = "123";
    process.env.GITHUB_RUN_ATTEMPT = "1";
    global.fetch = vi.fn();
  });
  afterEach(() => {
    delete process.env.SYSTEM_ACCESSTOKEN;
    delete process.env.AZURE_DEVOPS_ORG_URL;
    delete process.env.SYSTEM_TEAMPROJECT;
    delete process.env.GITHUB_RUN_ID;
    delete process.env.GITHUB_RUN_ATTEMPT;
    delete global.fetch;
  });

  it("creates a work item through the configured organization and project", async () => {
    global.fetch.mockResolvedValue({
      ok: true,
      status: 200,
      statusText: "OK",
      text: vi.fn().mockResolvedValue(JSON.stringify({ id: 42, url: "https://dev.azure.com/test-org/_apis/wit/workItems/42" })),
    });

    const result = await createAzureDevOpsWorkItemHandler("ado_create_work_item", {
      work_item_type: "Task",
      area_path: "test-project\\Platform",
      max: 1,
      body_footer: "Configured footer",
    })(
      {
        temporary_id: "#aw_item",
        title: "Fix the build",
        description: "Detailed description of the build failure.",
      },
      {}
    );

    expect(result).toMatchObject({
      success: true,
      temporaryId: "#aw_item",
      number: 42,
    });
    expect(global.fetch).toHaveBeenCalledOnce();
    expect(global.fetch.mock.calls[0][0]).toBe("https://dev.azure.com/test-org/test-project/_apis/wit/workitems/$Task?api-version=7.0");
    expect(global.fetch.mock.calls[0][1]).toMatchObject({
      method: "POST",
      redirect: "manual",
      headers: {
        Accept: "application/json",
        "Content-Type": "application/json-patch+json",
      },
    });
    const patch = JSON.parse(global.fetch.mock.calls[0][1].body);
    expect(patch.find(operation => operation.path === "/fields/System.Description").value).toBe("Detailed description of the build failure.\n\nConfigured footer");
    expect(core.debug).toHaveBeenNthCalledWith(1, "Azure DevOps API request started: POST");
    expect(core.debug).toHaveBeenNthCalledWith(2, "Azure DevOps API request completed: POST HTTP 200");
    expect(core.debug.mock.calls.flat().join(" ")).not.toContain("test-token");
  });

  it("uses standardized staged logging without work-item content", async () => {
    await createAzureDevOpsWorkItemHandler("ado_create_work_item", {
      staged: true,
      work_item_type: "Task",
    })(
      {
        temporary_id: "#aw_item",
        title: "Sensitive customer incident",
        description: "Detailed sensitive customer incident description.",
      },
      {}
    );

    expect(core.info).toHaveBeenCalledWith("🎭 Staged Mode Preview — Would create Azure DevOps Task");
    expect(core.info.mock.calls.flat().join(" ")).not.toContain("Sensitive customer incident");
  });

  it("does not log staged attachment paths", async () => {
    await createAzureDevOpsWorkItemHandler("ado_upload_workitem_attachment", {
      staged: true,
    })({ work_item_id: 42, file_path: "private/customer-data.pdf" }, {});

    expect(core.info).toHaveBeenCalledWith("🎭 Staged Mode Preview — Would attach a file to Azure DevOps work item 42");
    expect(core.info.mock.calls.flat().join(" ")).not.toContain("private/customer-data.pdf");
  });

  it("rejects updates to fields not enabled by configuration", async () => {
    const result = await createAzureDevOpsWorkItemHandler("ado_update_work_item", {
      target: "*",
      title: false,
    })({ id: 42, title: "New title" }, {});

    expect(result).toEqual({
      success: false,
      error: "title updates are not enabled by ado_update_work_item",
    });
    expect(global.fetch).not.toHaveBeenCalled();
  });

  it("rejects area paths outside configured prefixes", async () => {
    const result = await createAzureDevOpsWorkItemHandler("ado_update_work_item", {
      staged: true,
      area_path: true,
      allowed_area_prefixes: ["test-project\\Platform"],
    })({ id: 42, area_path: "test-project\\Other" }, {});

    expect(result).toEqual({
      success: false,
      error: "area_path is not permitted by the configured area-path prefixes",
    });
    expect(global.fetch).not.toHaveBeenCalled();
  });

  it.each([
    { target: 6779224, id: 6779224 },
    { target: "*", id: 6779224 },
    { target: "6779224", id: 6779224 },
    { target: " 6779224 ", id: 6779224 },
    { target: ["6779224", 6779225], id: 6779224 },
    { target: ["6779224", 6779225], id: 6779225 },
  ])("allows work item $id for target $target without an area-path request", async ({ target, id }) => {
    global.fetch.mockResolvedValue({
      ok: true,
      status: 200,
      text: vi.fn().mockResolvedValue(JSON.stringify({ id: 1 })),
    });

    const result = await createAzureDevOpsWorkItemHandler("ado_comment_on_work_item", {
      target,
    })({ work_item_id: id, body: "A comment" }, {});

    expect(result).toMatchObject({ success: true, number: id });
    expect(global.fetch).toHaveBeenCalledOnce();
    expect(global.fetch.mock.calls[0][1].method).toBe("POST");
    expect(global.fetch.mock.calls[0][0]).toBe(`https://dev.azure.com/test-org/test-project/_apis/wit/workItems/${id}/comments?api-version=7.1-preview.4`);
  });

  it.each([{ target: "6779224" }, { target: " 6779224 " }, { target: ["6779224", 6779226] }])("rejects an ID outside target $target without an area-path request", async ({ target }) => {
    const result = await createAzureDevOpsWorkItemHandler("ado_comment_on_work_item", {
      target,
    })({ work_item_id: 6779225, body: "A comment" }, {});

    expect(result).toEqual({
      success: false,
      error: "work item #6779225 is not permitted by the target configuration",
    });
    expect(global.fetch).not.toHaveBeenCalled();
  });

  it.each(["OneCRM\\Team", "OneCRM\\Team\\Subteam", "onecrm\\team"])("preserves area-path matching for %s", async areaPath => {
    global.fetch
      .mockResolvedValueOnce({
        ok: true,
        status: 200,
        text: vi.fn().mockResolvedValue(JSON.stringify({ fields: { "System.AreaPath": areaPath } })),
      })
      .mockResolvedValueOnce({
        ok: true,
        status: 200,
        text: vi.fn().mockResolvedValue(JSON.stringify({ id: 1 })),
      });

    const result = await createAzureDevOpsWorkItemHandler("ado_comment_on_work_item", {
      target: "OneCRM\\Team",
    })({ work_item_id: 6779224, body: "A comment" }, {});

    expect(result).toMatchObject({ success: true, number: 6779224 });
    expect(global.fetch).toHaveBeenCalledTimes(2);
    expect(global.fetch.mock.calls[0][1].method).toBe("GET");
    expect(global.fetch.mock.calls[0][0]).toBe("https://dev.azure.com/test-org/test-project/_apis/wit/workitems/6779224?api-version=7.0&fields=System.AreaPath");
  });

  it.each(["", " \t "])("rejects empty target %j without an area-path request", async target => {
    const result = await createAzureDevOpsWorkItemHandler("ado_comment_on_work_item", {
      target,
    })({ work_item_id: 6779224, body: "A comment" }, {});

    expect(result).toEqual({
      success: false,
      error: "target must not be empty; expression targets must evaluate to a work-item ID",
    });
    expect(global.fetch).not.toHaveBeenCalled();
  });

  it("rejects reserved agent identities", async () => {
    const result = await createAzureDevOpsWorkItemHandler("ado_assign_work_item", {
      target: "*",
    })({ id: 42, assignee: "GitHub Copilot" }, {});

    expect(result).toEqual({
      success: false,
      error: "E001: assignee 'GitHub Copilot' is a reserved identity",
    });
    expect(global.fetch).not.toHaveBeenCalled();
  });

  it.each([
    {
      type: "ado_assign_work_item",
      config: { target: "*" },
      message: { work_item_id: 42, assignee: "" },
      error: "E001: assignee must not be empty",
    },
    {
      type: "ado_create_work_item",
      config: { work_item_type: "Task" },
      message: { temporary_id: "#aw_item", title: "Fix the build", description: "Detailed description of the build failure.", tags: "not-an-array" },
      error: "E001: tags must be an array",
    },
    {
      type: "ado_create_work_item",
      config: { work_item_type: "Task" },
      message: { temporary_id: "#aw_item", title: "Fix the build", description: "Detailed description of the build failure.", tags: ["bad;tag"] },
      error: "E001: tag 'bad;tag' contains a semicolon",
    },
  ])("prefixes validation failures with E001", async ({ type, config, message, error }) => {
    const result = await createAzureDevOpsWorkItemHandler(type, config)(message);

    expect(result).toEqual({ success: false, error });
    expect(global.fetch).not.toHaveBeenCalled();
  });

  it("prefixes invalid tag errors with E001", async () => {
    const result = await createAzureDevOpsWorkItemHandler("ado_create_work_item", {
      work_item_type: "Task",
    })({
      temporary_id: "#aw_item",
      title: "Fix the build",
      description: "Detailed description of the build failure.",
      tags: [""],
    });

    expect(result).toEqual({
      success: false,
      error: "E001: tags must not contain empty values",
    });
    expect(global.fetch).not.toHaveBeenCalled();
  });

  it("rejects temporary IDs from a different provider", () => {
    expect(() =>
      resolveWorkItemReference(
        "#aw_issue",
        {
          aw_issue: {
            repo: "owner/repo",
            number: 42,
          },
        },
        false
      )
    ).toThrow("has not been resolved by ado_create_work_item in this run");
  });
});
