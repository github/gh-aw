import { defineWorkflow, joinSession } from "@github/copilot-sdk/extension";
import { communityIssueStatus } from "./workflow.mjs";

await joinSession({ workflows: [defineWorkflow(communityIssueStatus)] });
