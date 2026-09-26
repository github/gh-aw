import { describe, it, expect } from "vitest";
import { getApiProxyEventName, countSteeringEventsByTypeInApiProxyJsonl, countSteeringEventsInApiProxyJsonl } from "./steering_helpers.cjs";

describe("steering_helpers", () => {
  describe("getApiProxyEventName", () => {
    it("returns empty string for non-object inputs", () => {
      expect(getApiProxyEventName(null)).toBe("");
      expect(getApiProxyEventName(undefined)).toBe("");
      expect(getApiProxyEventName("string")).toBe("");
      expect(getApiProxyEventName(42)).toBe("");
      expect(getApiProxyEventName([])).toBe("");
    });

    it("returns top-level event field", () => {
      expect(getApiProxyEventName({ event: "token_steering", request_id: "r1" })).toBe("token_steering");
    });

    it("returns top-level type field when event is absent", () => {
      expect(getApiProxyEventName({ type: "model_steering", request_id: "r2" })).toBe("model_steering");
    });

    it("returns snake-case and camel-case event name fields", () => {
      expect(getApiProxyEventName({ event_name: "timeout_steering" })).toBe("timeout_steering");
      expect(getApiProxyEventName({ eventName: "model_steering" })).toBe("model_steering");
    });

    it("returns payload.event when top-level fields are absent", () => {
      expect(getApiProxyEventName({ payload: { event: "steering" }, request_id: "r3" })).toBe("steering");
    });

    it("returns payload.type when payload.event is absent", () => {
      expect(getApiProxyEventName({ payload: { type: "token_steering" }, request_id: "r4" })).toBe("token_steering");
    });

    it("returns empty string when payload is not a plain object", () => {
      expect(getApiProxyEventName({ payload: null })).toBe("");
      expect(getApiProxyEventName({ payload: [] })).toBe("");
      expect(getApiProxyEventName({ payload: "str" })).toBe("");
    });

    it("returns empty string when no recognized field is present", () => {
      expect(getApiProxyEventName({ request_id: "r5", status: "ok" })).toBe("");
    });
  });

  describe("countSteeringEventsByTypeInApiProxyJsonl", () => {
    it("aggregates counters for each normalized steering event", () => {
      const content = ['{"event":"TOKEN_STEERING"}', '{"type":"token_steering"}', '{"event_name":"timeout_steering"}', '{"eventName":"model_steering"}'].join("\n");
      expect(countSteeringEventsByTypeInApiProxyJsonl(content)).toEqual({
        model_steering: 1,
        timeout_steering: 1,
        token_steering: 2,
      });
    });
  });

  describe("countSteeringEventsInApiProxyJsonl", () => {
    it("counts events with exact 'steering' name", () => {
      const content = '{"event":"steering","request_id":"r1"}\n';
      expect(countSteeringEventsInApiProxyJsonl(content)).toBe(1);
    });

    it("counts events ending with _steering", () => {
      const content = ['{"event":"token_steering"}', '{"event":"model_steering"}'].join("\n");
      expect(countSteeringEventsInApiProxyJsonl(content)).toBe(2);
    });

    it("matches event names case-insensitively", () => {
      const content = ['{"event":"TOKEN_STEERING"}', '{"event":"Steering"}'].join("\n");
      expect(countSteeringEventsInApiProxyJsonl(content)).toBe(2);
    });

    it("counts steering events via top-level type field", () => {
      const content = '{"type":"token_steering","request_id":"r2"}\n';
      expect(countSteeringEventsInApiProxyJsonl(content)).toBe(1);
    });

    it("counts steering events via payload.event field", () => {
      const content = '{"payload":{"event":"token_steering"},"request_id":"r3"}\n';
      expect(countSteeringEventsInApiProxyJsonl(content)).toBe(1);
    });

    it("counts steering events via payload.type field", () => {
      const content = '{"payload":{"type":"model_steering"},"request_id":"r4"}\n';
      expect(countSteeringEventsInApiProxyJsonl(content)).toBe(1);
    });

    it("ignores non-steering events", () => {
      const content = ['{"event":"request"}', '{"event":"response"}', '{"type":"auth"}'].join("\n");
      expect(countSteeringEventsInApiProxyJsonl(content)).toBe(0);
    });

    it("ignores malformed JSONL lines", () => {
      const content = ['{"event":"steering"}', "not-json", '{"event":"token_steering"}'].join("\n");
      expect(countSteeringEventsInApiProxyJsonl(content)).toBe(2);
    });

    it("returns 0 for empty content", () => {
      expect(countSteeringEventsInApiProxyJsonl("")).toBe(0);
    });
  });
});
