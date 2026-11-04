// @ts-check
import { describe, it, expect, beforeEach, vi } from "vitest";
import { main } from "./call_workflow.cjs";

// Mock the core GitHub Actions toolkit
global.core = {
  info: vi.fn(),
  warning: vi.fn(),
  error: vi.fn(),
  setOutput: vi.fn(),
};

global.context = {
  repo: { owner: "github", repo: "gh-aw" },
  runId: 99999,
  actor: "mnkiefer",
  eventName: "workflow_dispatch",
  payload: {},
};

describe("call_workflow handler factory", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    process.env.GITHUB_RUN_ID = "99999";
    process.env.GITHUB_RUN_ATTEMPT = "2";
    process.env.GITHUB_WORKFLOW_REF = "github/gh-aw/.github/workflows/smoke-call-workflow.lock.yml@refs/heads/otel-for-episodes";
    process.env.GITHUB_AW_OTEL_TRACE_ID = "f".repeat(32);
    process.env.GITHUB_AW_OTEL_PARENT_SPAN_ID = "abcdef1234567890";
  });

  function parsePayloadOutput() {
    const payloadArg = core.setOutput.mock.calls.find(call => call[0] === "call_workflow_payload")?.[1];
    expect(typeof payloadArg).toBe("string");
    return JSON.parse(payloadArg);
  }

  it("should create a handler function", async () => {
    const handler = await main({});
    expect(typeof handler).toBe("function");
  });

  it("should select a workflow and set outputs", async () => {
    const config = {
      workflows: ["spring-boot-bugfix", "frontend-dep-upgrade"],
      max: 1,
    };
    const handler = await main(config);

    const message = {
      type: "call_workflow",
      workflow_name: "spring-boot-bugfix",
      inputs: {
        environment: "staging",
        version: "1.2.3",
      },
    };

    const result = await handler(message);

    expect(result.success).toBe(true);
    expect(result.delegated).toBe(true);
    expect(result.workflow_name).toBe("spring-boot-bugfix");
    expect(core.setOutput).toHaveBeenCalledWith("call_workflow_name", "spring-boot-bugfix");
    const payload = parsePayloadOutput();
    expect(payload.environment).toBe("staging");
    expect(payload.version).toBe("1.2.3");
    expect(typeof payload.aw_context).toBe("string");
  });

  it("should reject unknown workflow names", async () => {
    const config = {
      workflows: ["worker-a", "worker-b"],
      max: 1,
    };
    const handler = await main(config);

    const message = {
      type: "call_workflow",
      workflow_name: "unauthorized-worker",
      inputs: {},
    };

    const result = await handler(message);

    expect(result.success).toBe(false);
    expect(result.error).toContain("not in the allowed workflows list");
    expect(core.setOutput).not.toHaveBeenCalled();
  });

  it("should reject empty workflow names", async () => {
    const config = {
      workflows: ["worker-a"],
      max: 1,
    };
    const handler = await main(config);

    const message = {
      type: "call_workflow",
      workflow_name: "",
      inputs: {},
    };

    const result = await handler(message);

    expect(result.success).toBe(false);
    expect(result.error).toContain("empty");
    expect(core.setOutput).not.toHaveBeenCalled();
  });

  it("should enforce max count limit", async () => {
    const config = {
      workflows: ["worker-a", "worker-b"],
      max: 1,
    };
    const handler = await main(config);

    // First call should succeed
    const result1 = await handler({ workflow_name: "worker-a", inputs: {} });
    expect(result1.success).toBe(true);

    // Second call should fail because max is 1
    const result2 = await handler({ workflow_name: "worker-b", inputs: {} });
    expect(result2.success).toBe(false);
    expect(result2.error).toContain("Max count");
  });

  it("should serialise inputs as JSON payload", async () => {
    const config = {
      workflows: ["worker-a"],
      max: 1,
    };
    const handler = await main(config);

    const inputs = {
      package_manager: "npm",
      dry_run: true,
      count: 42,
    };

    await handler({ workflow_name: "worker-a", inputs });

    const payload = parsePayloadOutput();
    expect(payload.package_manager).toBe("npm");
    expect(payload.dry_run).toBe(true);
    expect(payload.count).toBe(42);
    expect(typeof payload.aw_context).toBe("string");
  });

  it("should inject serialized aw_context into the forwarded payload", async () => {
    const handler = await main({ workflows: ["worker-a"], max: 1 });

    await handler({ workflow_name: "worker-a", inputs: { task: "validate" } });

    const payload = parsePayloadOutput();
    expect(payload.task).toBe("validate");
    expect(typeof payload.aw_context).toBe("string");

    const awContext = JSON.parse(payload.aw_context);
    expect(awContext.episode_id).toBe("99999-2:github/gh-aw/.github/workflows/smoke-call-workflow.lock.yml@refs/heads/otel-for-episodes");
    expect(awContext.hop_id).toBe("99999-2:github/gh-aw/.github/workflows/smoke-call-workflow.lock.yml@refs/heads/otel-for-episodes");
    expect(awContext.parent_hop_id).toBe("");
    expect(awContext.origin_event).toBe("workflow_dispatch");
    expect(awContext.root_repo).toBe("github/gh-aw");
    expect(awContext.root_workflow_id).toBe("github/gh-aw/.github/workflows/smoke-call-workflow.lock.yml@refs/heads/otel-for-episodes");
    expect(awContext.root_run_id).toBe("99999");
    expect(awContext.workflow_call_id).toBe("99999-2:github/gh-aw/.github/workflows/smoke-call-workflow.lock.yml@refs/heads/otel-for-episodes");
    expect(awContext.otel_trace_id).toBe("f".repeat(32));
    expect(awContext.otel_parent_span_id).toBe("abcdef1234567890");
  });

  it("should allow any workflow when allowed list is empty", async () => {
    // An empty workflows array is treated as permissive (no restriction).
    // In practice, the compiler always populates this list from frontmatter,
    // so this case should not occur during normal usage.
    const config = {
      workflows: [],
      max: 5,
    };
    const handler = await main(config);

    // When no allowed list, any workflow should pass
    const result = await handler({ workflow_name: "any-workflow", inputs: {} });
    expect(result.success).toBe(true);
  });

  it.each([false, true])("forwards unique webhook arrays without logging bodies, staged=%s", async staged => {
    const previous = context.payload;
    const first = { delivery_id: "a", payload: { url: "https://api.github.com/orgs/org/projectsV2/1/items/1", type: ["edited"] } };
    context.payload = { client_payload: { aw_context: { webhook: { event: "projects_v2_item", deliveries: [first, first] } } } };
    try {
      const handler = await main({ workflows: ["worker-a"], staged });
      const result = await handler({ workflow_name: "worker-a", inputs: {} });
      expect(result.success).toBe(true);
      const forwarded = JSON.parse(JSON.parse(result.payload).aw_context);
      expect(forwarded.items).toHaveLength(1);
      expect(forwarded.webhook.deliveries).toEqual([first]);
      expect(core.info.mock.calls.flat().join("\n")).not.toContain("do-not-log-webhook");
    } finally {
      context.payload = previous;
    }
  });

  it("should handle missing inputs gracefully", async () => {
    const config = {
      workflows: ["worker-a"],
      max: 1,
    };
    const handler = await main(config);

    const result = await handler({ workflow_name: "worker-a" });

    expect(result.success).toBe(true);
    const payload = parsePayloadOutput();
    expect(payload).toHaveProperty("aw_context");
    expect(Object.keys(payload)).toEqual(["aw_context"]);
  });
});
