import { defineConfig } from "vitest/config";
import config from "./vitest.config.mjs";

export default defineConfig({
  ...config,
  test: {
    ...config.test,
    include: ["rendering_scripts_verifier.test.cjs"],
    env: {
      GH_AW_VERIFY_RENDERING: "true",
      GH_AW_RENDERING_INPUT: process.env.GH_AW_RENDERING_INPUT || "",
    },
  },
});
