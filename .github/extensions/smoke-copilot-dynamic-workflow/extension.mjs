import { defineWorkflow, joinSession } from "@microsoft/copilot-sdk/extension";
import { smokeWorkflow } from "./workflow.mjs";

await joinSession({ workflows: [defineWorkflow(smokeWorkflow)] });
