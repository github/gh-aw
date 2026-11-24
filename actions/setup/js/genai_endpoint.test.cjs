import { describe, expect, it } from "vitest";

const { genAiServerAddress } = require("./genai_endpoint.cjs");

describe("genAiServerAddress", () => {
  it.each([
    ["https://example.ai.gateway.azure.com/default/models/openai/v1", "example.ai.gateway.azure.com"],
    ["https://gw.azure-api.net:8443/v1?key=private#fragment", "gw.azure-api.net"],
    ["http://localhost:11434/v1", "localhost"],
    ["api.openai.com:8443", "api.openai.com"],
    ["https://[2001:db8::1]:8443/v1", "2001:db8::1"],
    ["2001:db8::1", "2001:db8::1"],
    ["fe80::1%eth0", "fe80::1"],
    ["${{ secrets.ENDPOINT }}", ""],
    ["/private/endpoint", ""],
    ["file:/private/endpoint", ""],
    ["file:///private/endpoint", ""],
    ["https:/gw.example.com/v1", ""],
    ["mytoken/v1", ""],
    ["https:///sk-SECRET/v1", ""],
    ["https:/sk-SECRET/v1", ""],
    ["https:443", ""],
    ["ftp://gw.example.com/v1", ""],
    ["https://", ""],
    ["not a url", ""],
    ["", ""],
    [undefined, ""],
    [{ hostname: "example.com" }, ""],
  ])("extracts a safe hostname from %s", (endpoint, expected) => {
    expect(genAiServerAddress(endpoint)).toBe(expected);
  });

  it("strips URL user information", () => {
    const endpoint = new URL("https://gw.azure-api.net/v1");
    endpoint.username = "user";
    endpoint.password = "placeholder";
    expect(genAiServerAddress(endpoint.href)).toBe("gw.azure-api.net");
  });
});
