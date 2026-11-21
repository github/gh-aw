import { describe, expect, it } from "vitest";
import { registerTests } from "./work_queue_codec_checks.cjs";
const { parseStrictJSON } = require("./work_queue_codec.cjs");
registerTests({ describe, it });

describe("strict JSON numeric options", () => {
  it("allows finite observations while checking exact integer tokens at selected paths", () => {
    const options = { finiteNumbersIn: true, exactIntegerAt: path => path.join(".") === "items.0.count" };
    expect(parseStrictJSON('{"items":[{"count":1e2,"measure":0.5}]}', options).items[0]).toMatchObject({ count: 100, measure: 0.5 });
    for (const token of ["1.0000000000000001", "1e-9999", "9007199254740991.1", "9007199254740992"]) {
      expect(() => parseStrictJSON(`{"items":[{"count":${token}}]}`, options)).toThrow(/noncanonical_number/);
    }
    expect(() => parseStrictJSON('{"items":[{"count":1,"count":2}]}', options)).toThrow(/duplicate_key/);
    expect(() => parseStrictJSON('{"measure":0.5}')).toThrow(/noncanonical_number/);
    expect(parseStrictJSON('{"measure":0.5}', { finiteNumbersIn: "measure" }).measure).toBe(0.5);
  });
});
