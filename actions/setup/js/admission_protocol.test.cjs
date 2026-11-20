import { describe, expect, it } from "vitest";
const { readAdmissionConfig, buildAdmissionIdentity, parseAdmissionDecision, readBoundedBody, FAILURE } = require("./admission_protocol.cjs");

export const identityEnv = {
  GH_AW_DEFAULT_ADMISSION_URL: "https://admission.example.org/api/admission",
  ACTIONS_ID_TOKEN_REQUEST_URL: "https://oidc.example.test/token?job=synthetic",
  ACTIONS_ID_TOKEN_REQUEST_TOKEN: "synthetic-bootstrap",
  GITHUB_REPOSITORY: "acme/project",
  GH_AW_INFO_WORKFLOW_ID: "platform/ops/.github/workflows/triage.lock.yml@refs/heads/main",
  GH_AW_ADMISSION_WORKFLOW_HASH: `${"a".repeat(64)}:${"b".repeat(64)}`,
  GITHUB_RUN_ID: "90071992547409931234567890",
  GITHUB_RUN_ATTEMPT: "2",
  GITHUB_REF: "refs/heads/main",
  GITHUB_EVENT_NAME: "workflow_dispatch",
  GITHUB_ACTOR: "original",
  GITHUB_TRIGGERING_ACTOR: "rerunner",
};

describe("admission configuration and identity", () => {
  it("does not enable admission from audience or outage policy alone", () => {
    expect(readAdmissionConfig({ GH_AW_DEFAULT_ADMISSION_AUDIENCE: "aud", GH_AW_DEFAULT_ADMISSION_ON_OUTAGE: "bad" })).toBeNull();
    expect(readAdmissionConfig(identityEnv)).toEqual({ endpoint: identityEnv.GH_AW_DEFAULT_ADMISSION_URL, audience: identityEnv.GH_AW_DEFAULT_ADMISSION_URL, onOutage: "ignore" });
  });

  it("accepts uppercase identity values and rejects C0/C1 control characters", () => {
    const uppercase = {
      ...identityEnv,
      GH_AW_DEFAULT_ADMISSION_URL: "https://ADMISSION.EXAMPLE.ORG/api/admission",
      GH_AW_DEFAULT_ADMISSION_AUDIENCE: "AUDIENCE/TEAM",
      GITHUB_REPOSITORY: "Acme/Project",
      GH_AW_INFO_WORKFLOW_ID: "Platform/Ops/.github/workflows/Triage.lock.yml@refs/heads/Main",
      GITHUB_ACTOR: "OriginalActor",
      GITHUB_TRIGGERING_ACTOR: "Rerunner",
    };
    expect(readAdmissionConfig(uppercase).audience).toBe("AUDIENCE/TEAM");
    expect(JSON.parse(buildAdmissionIdentity(uppercase))).toMatchObject({
      repo: "Acme/Project",
      actor: "OriginalActor",
      triggering_actor: "Rerunner",
    });
    for (const value of ["\u0007", "\u0085"]) {
      expect(() => readAdmissionConfig({ ...identityEnv, GH_AW_DEFAULT_ADMISSION_AUDIENCE: value })).toThrow(FAILURE);
    }
  });

  it.each([
    "http://example.org/api/admission",
    "https://user:password@example.org/api/admission",
    "https://example.org/api/admission?",
    "https://example.org/api/admission#",
    "https://example.org/api/admission?x=1",
    "https://example.org/other",
    "https://example.org/a/../api/admission",
    "https://example.org/api/%61dmission",
    " https://example.org/api/admission",
    "https://example.org\\api\\admission",
    "https://${{ vars.HOST }}/api/admission",
    "https://example.org\n/api/admission",
    "https://example.org:99999/api/admission",
    "https://[invalid]/api/admission",
    "https://[fe80::1%25eth0]/api/admission",
  ])("rejects invalid endpoints: %s", endpoint => {
    expect(() => readAdmissionConfig({ GH_AW_DEFAULT_ADMISSION_URL: endpoint })).toThrow(FAILURE);
  });

  it.each(["\n", "\u007f", "${{ vars.AUD }}", "\ud800"])("rejects invalid audiences", audience => {
    expect(() => readAdmissionConfig({ ...identityEnv, GH_AW_DEFAULT_ADMISSION_AUDIENCE: audience })).toThrow(FAILURE);
  });
  it.each(["Ignore", "BLOCK", " ignore "])("requires exact outage policy", onOutage => {
    expect(() => readAdmissionConfig({ ...identityEnv, GH_AW_DEFAULT_ADMISSION_ON_OUTAGE: onOutage })).toThrow(FAILURE);
  });

  it("preserves executing identity, distinct actors, Unicode, and large decimal strings", () => {
    const body = JSON.parse(buildAdmissionIdentity({ ...identityEnv, GITHUB_REF: 'refs/heads/🦊"\\x' }));
    expect(Object.keys(body)).toHaveLength(10);
    expect(body.workflow_hash).toBe(identityEnv.GH_AW_ADMISSION_WORKFLOW_HASH);
    expect(body.run_id).toBe(identityEnv.GITHUB_RUN_ID);
    expect(body.actor).toBe("original");
    expect(body.triggering_actor).toBe("rerunner");
    expect(body.workflow_id).toBe(identityEnv.GH_AW_INFO_WORKFLOW_ID);
    expect(body.workflow_call_id).toBe(`${identityEnv.GITHUB_RUN_ID}-2:${identityEnv.GH_AW_INFO_WORKFLOW_ID}`);
    expect(body.ref).toBe('refs/heads/🦊"\\x');
  });
  it("JSON-encodes available job identity without header percent-encoding", () => {
    const jobId = 'review/"\\\u00e9';
    expect(JSON.parse(buildAdmissionIdentity({ ...identityEnv, GITHUB_JOB: jobId })).job_id).toBe(jobId);
    expect(JSON.parse(buildAdmissionIdentity(identityEnv))).not.toHaveProperty("job_id");
    expect(() => buildAdmissionIdentity({ ...identityEnv, GITHUB_JOB: "invalid\njob" })).toThrow(FAILURE);
  });
  it.each(["", "0", "01", "-1", "+1", " 1", "1.0", "1e3", "١"])("rejects noncanonical run/attempt strings", value => {
    for (const key of ["GITHUB_RUN_ID", "GITHUB_RUN_ATTEMPT"]) expect(() => buildAdmissionIdentity({ ...identityEnv, [key]: value })).toThrow(FAILURE);
  });
  it.each(["GH_AW_INFO_WORKFLOW_ID", "GITHUB_TRIGGERING_ACTOR", "GITHUB_ACTOR"])("requires explicit current context: %s", key => {
    expect(() => buildAdmissionIdentity({ ...identityEnv, [key]: "" })).toThrow(FAILURE);
  });
  it.each(["", "unknown", "a".repeat(64), `${"a".repeat(63)}:${"b".repeat(64)}`, `${"A".repeat(64)}:${"b".repeat(64)}`, `${"a".repeat(64)}:${"b".repeat(64)}\n`])("rejects absent or malformed workflow hashes (%s)", value => {
    expect(() => buildAdmissionIdentity({ ...identityEnv, GH_AW_ADMISSION_WORKFLOW_HASH: value })).toThrow(FAILURE);
  });
  it("rejects unpaired surrogates, controls, and oversize identities", () => {
    for (const value of ["\ud800", "x\nx", "a".repeat(4097)]) expect(() => buildAdmissionIdentity({ ...identityEnv, GITHUB_REF: value })).toThrow(FAILURE);
  });
});

describe("strict admission success", () => {
  it.each([1, 2147483647])("accepts credit boundary %i", max => {
    expect(parseAdmissionDecision(`{"max_ai_credits":${max}}`)).toEqual({ maxAiCredits: max });
  });
  it.each(["0", "-1", "2147483648", "1000.0", "1e3", '"1000"', "null", "true", "1.1", "01", "+1", "NaN", "Infinity"])("rejects credit wire spelling %s", credits => {
    expect(() => parseAdmissionDecision(`{"max_ai_credits":${credits}}`)).toThrow(FAILURE);
  });
  it.each([
    "{}",
    "[]",
    "null",
    '{"max_ai_credits":1,"other":"x"}',
    '{"max_ai_credits":1,"max_ai_credits":2}',
    `{"max_ai_credits":1,"max_ai_\\u0063redits":2}`,
    '{"max_ai_credits":1,"correlation_id":"8e53c1f4a62049b5b382704fe9d61a0c"}',
    '{"max_ai_credits":1,"traceparent":"untrusted"}',
    '{"max_ai_credits":1} trailing',
    `{\u000b"max_ai_credits":1}`,
    '{"max_ai_credits":1,"\\ud800":"x"}',
  ])("blocks invalid complete responses", text => expect(() => parseAdmissionDecision(text)).toThrow(FAILURE));
  it("decodes escaped names exactly once", () => {
    expect(parseAdmissionDecision('{"max_ai_\\u0063redits":1}')).toEqual({ maxAiCredits: 1 });
  });
  it.each([
    [{}, {}],
    [{ max_ai_credits_threat_detection: 10 }, { threatDetectionMaxAiCredits: 10 }],
    [{ max_ai_credits_evals: 20 }, { evalsMaxAiCredits: 20 }],
    [
      { max_ai_credits_threat_detection: 10, max_ai_credits_evals: 20 },
      { threatDetectionMaxAiCredits: 10, evalsMaxAiCredits: 20 },
    ],
  ])("accepts optional component allowances in either field order (%j)", (optional, expected) => {
    expect(parseAdmissionDecision(JSON.stringify({ max_ai_credits: 1000, ...optional }))).toEqual({ maxAiCredits: 1000, ...expected });
    expect(parseAdmissionDecision(JSON.stringify({ ...optional, max_ai_credits: 1000 }))).toEqual({ maxAiCredits: 1000, ...expected });
  });
  it.each(["max_ai_credits_threat_detection", "max_ai_credits_evals"])("strictly validates optional %s before accepting any decision", field => {
    for (const value of ["0", "-1", "1.1", "1000.0", "1e3", '"1"', "null", "true", "2147483648"]) {
      expect(() => parseAdmissionDecision(`{"max_ai_credits":1000,"${field}":${value}}`)).toThrow(FAILURE);
    }
    expect(() => parseAdmissionDecision(`{"${field}":1}`)).toThrow(FAILURE);
    expect(() => parseAdmissionDecision(`{"max_ai_credits":1000,"${field}":1,"${field}":2}`)).toThrow(FAILURE);
    expect(() => parseAdmissionDecision(`{"max_ai_credits":1000,"${field}":1,"${field.replace("credits", "\\u0063redits")}":2}`)).toThrow(FAILURE);
    expect(parseAdmissionDecision(`{"${field}":2147483647,"max_ai_credits":1}`)).toMatchObject({ maxAiCredits: 1 });
  });
  it("enforces the 4KiB boundary during streaming and rejects invalid UTF-8", async () => {
    expect(await readBoundedBody(new Response("a".repeat(4096)), 4096)).toHaveLength(4096);
    await expect(readBoundedBody(new Response("a".repeat(4097)), 4096)).rejects.toThrow(FAILURE);
    await expect(readBoundedBody(new Response(new Uint8Array([0xff])), 4096)).rejects.toThrow(FAILURE);
  });
});
