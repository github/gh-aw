import { defineWorkflow, joinSession } from "@github/copilot-sdk/extension";
import { smokeWorkflow } from "./workflow.mjs";

await joinSession({ workflows: [defineWorkflow(smokeWorkflow)] });
