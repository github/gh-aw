// @ts-check
import { defineConfig } from "astro/config";
import starlight from "@astrojs/starlight";
import sitemap from "@astrojs/sitemap";
import starlightLinksValidator from "starlight-links-validator";
import starlightGitHubAlerts from "starlight-github-alerts";
import starlightBlog from "starlight-blog";
import mermaid from "astro-mermaid";
import { fileURLToPath } from "node:url";
import { unified } from "@astrojs/markdown-remark";
import remarkStripEmojis from "./src/lib/remark/stripEmojis.js";
import remarkTableDataLabels from "./src/lib/remark/tableDataLabels.js";
import remarkInlineMarkdownInHtml from "./src/lib/remark/inlineMarkdownInHtml.js";
import remarkAgenticsWorkflow from "./src/lib/remark/agenticsWorkflow.js";
import rehypeTableWrapper from "./src/lib/rehype/tableWrapper.js";

const agenticsWorkflowsDirectory = process.env.AGENTICS_WORKFLOWS_DIR ?? fileURLToPath(new URL("../../agentics/workflows/", import.meta.url));

/**
 * Creates blog authors config with GitHub profile pictures
 * @param {Record<string, {name: string, url: string, picture?: string}>} authors
 */
function createAuthors(authors) {
  return Object.fromEntries(Object.entries(authors).map(([key, author]) => [key, { ...author, picture: author.picture ?? `https://github.com/${key}.png?size=200` }]));
}

// NOTE: A previous attempt defined a custom Shiki grammar for `aw` (agentic workflow) but
// Shiki did not register it and builds produced a warning: language "aw" not found.
// For now we alias `aw` -> `markdown` which removes the warning and still gives
// reasonable highlighting for examples that combine frontmatter + markdown.
// If richer highlighting is needed later, implement a proper TextMate grammar
// in a separate JSON file and load it here (ensure required embedded scopes exist).

// https://astro.build/config
export default defineConfig({
  site: "https://github.github.com",
  base: "/gh-aw/",
  trailingSlash: "always",
  markdown: {
    processor: unified({
      remarkPlugins: [remarkStripEmojis, remarkTableDataLabels, remarkInlineMarkdownInHtml, [remarkAgenticsWorkflow, { sourceDirectory: agenticsWorkflowsDirectory }]],
      rehypePlugins: [rehypeTableWrapper],
    }),
  },
  vite: {
    server: {
      fs: {
        allow: [fileURLToPath(new URL("../", import.meta.url))],
      },
    },
  },
  devToolbar: {
    enabled: false,
  },
  experimental: {
    clientPrerender: false,
  },
  redirects: {
    // Hosted wizard
    "/wizard/": "https://githubnext.github.io/gh-aw-wizard/",

    // Examples renamed to Gallery
    "/examples/": "/gh-aw/gallery/",
    "/examples/ai-issue-triage/": "/gh-aw/gallery/ai-issue-triage/",
    "/examples/ai-release-notes/": "/gh-aw/gallery/ai-release-notes/",
    "/examples/automated-pr-review/": "/gh-aw/gallery/automated-pr-review/",
    "/examples/ci-failure-investigation/": "/gh-aw/gallery/ci-failure-investigation/",
    "/examples/code-improvement/": "/gh-aw/gallery/code-improvement/",
    "/examples/docs-automation/": "/gh-aw/gallery/docs-automation/",
    "/examples/maintaining-repos/": "/gh-aw/gallery/maintaining-repos/",
    "/examples/metrics-analytics/": "/gh-aw/gallery/metrics-analytics/",
    "/examples/multi-repo/": "/gh-aw/gallery/multi-repo/",
    "/examples/multi-repo/code-quality-monitoring/": "/gh-aw/gallery/multi-repo/code-quality-monitoring/",
    "/examples/multi-repo/dependabot-rollout/": "/gh-aw/gallery/multi-repo/dependabot-rollout/",
    "/examples/multi-repo/feature-sync/": "/gh-aw/gallery/multi-repo/feature-sync/",
    "/examples/multi-repo/issue-tracking/": "/gh-aw/gallery/multi-repo/issue-tracking/",
    "/examples/multi-repo/triage-from-side-repo/": "/gh-aw/gallery/multi-repo/triage-from-side-repo/",
    "/examples/security-review/": "/gh-aw/gallery/security-review/",

    // Status → Labs → Agent Factory → Agent Factory Status chain
    "/status/": "/gh-aw/agent-factory-status/",
    "/labs/": "/gh-aw/agent-factory-status/",
    "/agent-factory/": "/gh-aw/agent-factory-status/",

    // Blog post date correction
    "/blog/2026-01-12-meet-the-workflows/": "/gh-aw/blog/2026-01-13-meet-the-workflows/",

    // Start-here → Get-started → current paths
    "/start-here/concepts/": "/gh-aw/introduction/how-they-work/",
    "/start-here/quick-start/": "/gh-aw/setup/quick-start/",

    // Get-started → current paths
    "/get-started/concepts/": "/gh-aw/introduction/how-they-work/",
    "/get-started/quick-start/": "/gh-aw/setup/quick-start/",

    // Introduction how-it-works → how-they-work
    "/introduction/how-it-works/": "/gh-aw/introduction/how-they-work/",
    "/introduction/slides/": "/gh-aw/",
    "/slides/": "/gh-aw/",

    // Tools → Setup renames
    "/tools/cli/": "/gh-aw/setup/cli/",
    "/tools/mcp-server/": "/gh-aw/reference/gh-aw-as-mcp-server/",
    "/tools/agentic-authoring/": "/gh-aw/setup/creating-workflows/",

    // Samples → Patterns renames
    "/samples/coding-development/": "/gh-aw/patterns/deterministic-ops/",
    "/samples/quality-testing/": "/gh-aw/patterns/deterministic-ops/",
    "/samples/triage-analysis/": "/gh-aw/patterns/issue-ops/",
    "/samples/research-planning/": "/gh-aw/patterns/deterministic-ops/",

    // Setup renames
    "/setup/agentic-authoring/": "/gh-aw/setup/creating-workflows/",
    "/setup/mcp-server/": "/gh-aw/reference/gh-aw-as-mcp-server/",

    // Reference renames
    "/reference/custom-agents/": "/gh-aw/reference/copilot-custom-agents/",
    "/reference/custom-agent/": "/gh-aw/reference/custom-agent-for-aw/",
    "/reference/assign-to-copilot/": "/gh-aw/reference/copilot-cloud-agent/",

    // Organization Practices moved under Guides
    "/organization-practices/": "/gh-aw/practices/organization-practices/",
    "/organization-practices/safe-rollout/": "/gh-aw/practices/safe-rollout/",
    "/organization-practices/sharing-workflows/": "/gh-aw/practices/sharing-workflows/",

    // Guides → Patterns renames
    "/guides/chatops/": "/gh-aw/patterns/chat-ops/",
    "/guides/issueops/": "/gh-aw/patterns/issue-ops/",
    "/guides/labelops/": "/gh-aw/patterns/label-ops/",
    "/guides/dailyops/": "/gh-aw/patterns/deterministic-ops/",
    "/guides/dispatchops/": "/gh-aw/patterns/dispatch-ops/",
    "/guides/monitoring/": "/gh-aw/experimental/monitoring-with-projects/",
    "/guides/multirepoops/": "/gh-aw/patterns/multi-repo-ops/",
    "/guides/orchestration/": "/gh-aw/patterns/orchestrator-ops/",
    "/patterns/orchestration/": "/gh-aw/patterns/orchestrator-ops/",
    "/guides/siderepoops/": "/gh-aw/patterns/multi-repo-ops/",
    "/patterns/side-repo-ops/": "/gh-aw/patterns/multi-repo-ops/",
    "/guides/specops/": "/gh-aw/patterns/spec-ops/",
    "/guides/researchplanassign/": "/gh-aw/patterns/research-plan-assign-ops/",
    "/guides/trialops/": "/gh-aw/experimental/trial-ops/",

    // Examples → Patterns renames
    "/examples/comment-triggered/chatops/": "/gh-aw/patterns/chat-ops/",
    "/examples/scheduled/dailyops/": "/gh-aw/patterns/deterministic-ops/",
    "/examples/issue-pr-events/issueops/": "/gh-aw/patterns/issue-ops/",
    "/examples/issue-pr-events/labelops/": "/gh-aw/patterns/label-ops/",
    "/examples/issue-pr-events/projectops/": "/gh-aw/patterns/project-ops/",

    // Patterns unhyphenated → hyphenated slugs
    "/patterns/centralrepoops/": "/gh-aw/patterns/central-repo-ops/",
    "/patterns/chatops/": "/gh-aw/patterns/chat-ops/",
    "/patterns/dailyops/": "/gh-aw/patterns/deterministic-ops/",
    "/patterns/dataops/": "/gh-aw/patterns/deterministic-ops/",
    "/patterns/dispatchops/": "/gh-aw/patterns/dispatch-ops/",
    "/patterns/issueops/": "/gh-aw/patterns/issue-ops/",
    "/patterns/labelops/": "/gh-aw/patterns/label-ops/",
    "/patterns/multirepoops/": "/gh-aw/patterns/multi-repo-ops/",
    "/patterns/projectops/": "/gh-aw/patterns/project-ops/",
    "/patterns/siderepoops/": "/gh-aw/patterns/multi-repo-ops/",
    "/patterns/specops/": "/gh-aw/patterns/spec-ops/",
    "/patterns/researchplanassignops/": "/gh-aw/patterns/research-plan-assign-ops/",
    "/patterns/batchops/": "/gh-aw/patterns/batch-ops/",
    "/patterns/taskops/": "/gh-aw/patterns/research-plan-assign-ops/",
    "/patterns/trialops/": "/gh-aw/experimental/trial-ops/",
    "/patterns/workqueueops/": "/gh-aw/patterns/workqueue-ops/",

    // Guides → new locations
    "/guides/deterministic-agentic-patterns/": "/gh-aw/patterns/deterministic-ops/",
    "/patterns/deterministic-agentic-patterns/": "/gh-aw/patterns/deterministic-ops/",
    "/guides/organization-practices/safe-rollout/": "/gh-aw/practices/safe-rollout/",
    "/guides/organization-practices/sharing-workflows/": "/gh-aw/practices/sharing-workflows/",
    "/guides/organization-practices/": "/gh-aw/guides/using-at-scale/",
    "/guides/maintaining-repos/": "/gh-aw/gallery/maintaining-repos/",
    "/practices/maintaining-repos/": "/gh-aw/gallery/maintaining-repos/",
    "/guides/self-hosted-runners/": "/gh-aw/reference/self-hosted-runners/",
    "/guides/packaging-imports/": "/gh-aw/guides/working-with-workflows/#adding-existing-workflows",
    "/guides/agentic-authoring/": "/gh-aw/guides/working-with-workflows/",
    "/guides/editing-workflows/": "/gh-aw/guides/working-with-workflows/#editing-workflows",
    "/guides/reusing-workflows/": "/gh-aw/guides/working-with-workflows/#adding-existing-workflows",
    "/guides/upgrading/": "/gh-aw/guides/working-with-workflows/#upgrading-workflows",
    "/guides/web-search/": "/gh-aw/reference/web-search/",
    "/guides/custom-otlp-attributes/": "/gh-aw/reference/open-telemetry/",
    "/guides/telemetry/": "/gh-aw/reference/open-telemetry/",
    "/guides/opentelemetry/": "/gh-aw/reference/open-telemetry/",
    "/experimental/opentelemetry/": "/gh-aw/reference/open-telemetry/",
    "/patterns/opentelemetry/": "/gh-aw/reference/open-telemetry/",
    "/guides/getting-started-mcp/": "/gh-aw/guides/mcps/",
    "/patterns/data-ops/": "/gh-aw/patterns/deterministic-ops/",
    "/patterns/expert-ops/": "/gh-aw/patterns/monitor-ops/",
    "/patterns/agentic-ops/": "/gh-aw/patterns/monitor-ops/",
    "/patterns/task-ops/": "/gh-aw/patterns/research-plan-assign-ops/",
    "/examples/comment-triggered/": "/gh-aw/patterns/chat-ops/",
    "/examples/issue-pr-events/": "/gh-aw/patterns/issue-ops/",
    "/examples/issue-pr-events/triage-analysis/": "/gh-aw/patterns/issue-ops/",
    "/examples/issue-pr-events/coding-development/": "/gh-aw/patterns/deterministic-ops/",
    "/examples/issue-pr-events/quality-testing/": "/gh-aw/patterns/deterministic-ops/",
    "/examples/scheduled/": "/gh-aw/patterns/deterministic-ops/",
    "/examples/scheduled/research-planning/": "/gh-aw/patterns/deterministic-ops/",
    "/examples/manual/": "/gh-aw/patterns/dispatch-ops/",
    "/examples/project-tracking/": "/gh-aw/patterns/project-ops/",
    "/guides/audit-with-agents/": "/gh-aw/reference/audit/",
    "/guides/ephemerals/": "/gh-aw/reference/ephemerals/",
    "/guides/memoryops/": "/gh-aw/patterns/memory-ops/",
    "/guides/memory-ops/": "/gh-aw/patterns/memory-ops/",
    "/guides/serena/": "/gh-aw/reference/serena/",
    "/guides/experiments/": "/gh-aw/experimental/experiments/",
    "/practices/experiments/": "/gh-aw/experimental/experiments/",
    "/practices/experiments-specification/": "/gh-aw/experimental/experiments-specification/",
    "/reference/experiments-specification/": "/gh-aw/experimental/experiments-specification/",
    "/reference/ai-credits-specification/": "/gh-aw/specs/ai-credits-specification/",
    "/reference/copilot-sdk-driver-specification/": "/gh-aw/specs/copilot-sdk-driver-specification/",
    "/reference/effective-tokens-specification/": "/gh-aw/specs/effective-tokens-specification/",
    "/reference/forecast-specification/": "/gh-aw/specs/forecast-specification/",
    "/reference/frontmatter-hash-specification/": "/gh-aw/specs/frontmatter-hash-specification/",
    "/reference/fuzzy-schedule-specification/": "/gh-aw/specs/fuzzy-schedule-specification/",
    "/reference/mcp-scripts-specification/": "/gh-aw/specs/mcp-scripts-specification/",
    "/reference/model-alias-specification/": "/gh-aw/specs/model-alias-specification/",
    "/reference/repository-package-manifest-specification/": "/gh-aw/specs/repository-package-manifest-specification/",
    "/reference/safe-outputs-specification/": "/gh-aw/specs/safe-outputs-specification/",
    "/practices/organization-practices/": "/gh-aw/guides/using-at-scale/",

    "/reference/awf-reflect/": "/gh-aw/experimental/awf-reflect/",

    // Patterns → Experimental
    "/patterns/correction-ops/": "/gh-aw/experimental/correction-ops/",
    "/patterns/trial-ops/": "/gh-aw/experimental/trial-ops/",
    "/patterns/monitoring/": "/gh-aw/experimental/monitoring-with-projects/",

    // Guides and use cases → Examples (moved)
    "/guides/ai-issue-triage/": "/gh-aw/gallery/ai-issue-triage/",
    "/guides/automated-pr-review/": "/gh-aw/gallery/automated-pr-review/",
    "/guides/docs-automation/": "/gh-aw/gallery/docs-automation/",
    "/guides/ai-release-notes/": "/gh-aw/gallery/ai-release-notes/",
    "/use-cases/ai-issue-triage/": "/gh-aw/gallery/ai-issue-triage/",
    "/use-cases/automated-pr-review/": "/gh-aw/gallery/automated-pr-review/",
    "/use-cases/ai-release-notes/": "/gh-aw/gallery/ai-release-notes/",
    "/use-cases/docs-automation/": "/gh-aw/gallery/docs-automation/",

    // Guides → Reference (moved)
    "/guides/azure-openai-byok/": "/gh-aw/reference/azure-openai-byok/",
    "/guides/arc-dind-copilot-agent/": "/gh-aw/reference/arc-dind-copilot-agent/",
    "/guides/governance/": "/gh-aw/reference/governance/",
    "/guides/open-telemetry/": "/gh-aw/reference/open-telemetry/",
    "/guides/third-party-agent/": "/gh-aw/reference/third-party-agent/",

    // Reference → Practices and Experimental (moved)
    "/reference/measuring-impact/": "/gh-aw/practices/measuring-impact/",
    "/reference/qmd/": "/gh-aw/experimental/qmd/",
    "/reference/drive-memory/": "/gh-aw/experimental/drive-memory/",
    "/reference/trace-graders/": "/gh-aw/experimental/trace-graders/",
    "/reference/enclaves/": "/gh-aw/experimental/enclaves/",
    "/reference/lockdown-mode/": "/gh-aw/reference/integrity/",
    "/reference/repository-package-manifest/": "/gh-aw/reference/aw-yml-package-manifest/",
  },
  integrations: [
    sitemap(),
    mermaid(),
    starlight({
      title: "GitHub Agentic Workflows",
      description: "Write agentic workflows in natural language using markdown files and run them as GitHub Actions workflows.",
      favicon: "/favicon.svg",
      logo: {
        src: "./src/assets/agentic-workflow.svg",
        replacesTitle: false,
      },
      components: {
        Banner: "./src/components/RecruitmentBanner.astro",
        Head: "./src/components/CustomHead.astro",
        SkipLink: "./src/components/SkipLink.astro",
        SocialIcons: "./src/components/CustomHeader.astro",
        ThemeSelect: "./src/components/ThemeToggle.astro",
        Footer: "./src/components/CustomFooter.astro",
        SiteTitle: "./src/components/CustomLogo.astro",
      },
      customCss: ["./src/styles/custom.css"],
      social: [{ icon: "github", label: "GitHub", href: "https://github.com/github/gh-aw" }],
      tableOfContents: {
        minHeadingLevel: 2,
        maxHeadingLevel: 4,
      },
      pagination: true,
      expressiveCode: {
        frames: {
          showCopyToClipboardButton: true,
        },
        shiki: {
          langs: /** @type {any[]} */ ["markdown", "yaml"],
          langAlias: { aw: "markdown" },
        },
      },
      plugins: [
        starlightBlog({
          recentPostCount: 12,
          authors: createAuthors({
            githubnext: {
              name: "GitHub Next",
              url: "https://githubnext.com/",
            },
            dsyme: {
              name: "Don Syme",
              url: "https://dsyme.net/",
            },
            pelikhan: {
              name: "Peli de Halleux",
              url: "https://www.microsoft.com/research/people/jhalleux/",
            },
            mnkiefer: {
              name: "Mara Kiefer",
              url: "https://github.com/mnkiefer",
            },
            claude: {
              name: "Claude",
              url: "https://claude.ai",
              picture: "/gh-aw/claude.png",
            },
            codex: {
              name: "Codex",
              url: "https://openai.com/index/openai-codex/",
              picture: "/gh-aw/codex.png",
            },
            gemini: {
              name: "Gemini",
              url: "https://gemini.google.com",
              picture: "/gh-aw/gemini.png",
            },
            copilot: {
              name: "Copilot",
              url: "https://github.com/features/copilot",
              picture: "https://avatars.githubusercontent.com/in/1143301?s=64&amp;v=4",
            },
          }),
        }),
        starlightGitHubAlerts(),
        starlightLinksValidator({
          errorOnRelativeLinks: true,
          errorOnLocalLinks: true,
          exclude: ({ file, link }) =>
            (file.includes("/src/generated/workshop-markdown/") && link.includes("?__gh_aw_workshop_local__=") && link.startsWith("/gh-aw/workshop/?__gh_aw_workshop_local__=")) ||
            link === "/gh-aw/gallery/ai-issue-triage/" ||
            link === "/gh-aw/gallery/ci-failure-investigation/",
        }),
      ],
      sidebar: [
        {
          label: "Introduction",
          items: [
            { label: "What are Agentic Workflows?", link: "/introduction/overview/" },
            { label: "How Agentic Workflows Work", link: "/introduction/how-they-work/" },
            { label: "Security Architecture", link: "/introduction/architecture/" },
          ],
        },
        {
          label: "Setup",
          items: [
            { label: "Quick Start", link: "/setup/quick-start/" },
            { label: "Creating New Workflows", link: "/setup/creating-workflows/" },
            { label: "Working with Workflows", link: "/guides/working-with-workflows/" },
            { label: "Creation Wizard", link: "/wizard/" },
            { label: "CLI Commands", link: "/setup/cli/" },
          ],
        },
        {
          label: "AI Engines",
          items: [
            { label: "GitHub Copilot", link: "/engines/copilot/" },
            { label: "Claude Code", link: "/engines/claude/" },
            { label: "OpenAI Codex", link: "/engines/codex/" },
            { label: "Google Gemini", link: "/engines/gemini/" },
            { label: "Pi", link: "/engines/pi/" },
          ],
        },
        {
          label: "Guides",
          items: [
            { label: "GitHub Actions Primer", link: "/guides/github-actions-primer/" },
            { label: "Using MCPs", link: "/guides/mcps/" },
            { label: "Network Configuration", link: "/guides/network-configuration/" },
            { label: "Agentic Automation Workshop", link: "/workshop/" },
            { label: "Using at Scale in Organizations", link: "/guides/using-at-scale/" },
          ],
        },
        {
          label: "Gallery",
          items: [
            { label: "Gallery Overview", link: "/gallery/" },
            { label: "Issue Triage", link: "/gallery/ai-issue-triage/" },
            { label: "Pull Request Review", link: "/gallery/automated-pr-review/" },
            { label: "Documentation Maintenance", link: "/gallery/docs-automation/" },
            { label: "Repository Maintenance", link: "/gallery/maintaining-repos/" },
            { label: "CI Failure Investigation", link: "/gallery/ci-failure-investigation/" },
            { label: "Code Improvement", link: "/gallery/code-improvement/" },
            { label: "Dependency Analysis", link: "/patterns/research-plan-assign-ops/" },
            { label: "Metrics and Analytics", link: "/gallery/metrics-analytics/" },
            { label: "Repository Reporting", link: "/gallery/ai-release-notes/" },
            { label: "Security Review", link: "/gallery/security-review/" },
            { label: "Multi-Repository Examples", link: "/gallery/multi-repo/" },
            { label: "Triage from Side Repo", link: "/gallery/multi-repo/triage-from-side-repo/" },
            { label: "Code Quality Monitoring", link: "/gallery/multi-repo/code-quality-monitoring/" },
            { label: "Feature Synchronization", link: "/gallery/multi-repo/feature-sync/" },
            { label: "Cross-Repository Issue Tracking", link: "/gallery/multi-repo/issue-tracking/" },
            { label: "Dependabot Rollout", link: "/gallery/multi-repo/dependabot-rollout/" },
          ],
        },
        {
          label: "Design Patterns",
          items: [
            { label: "BatchOps", link: "/patterns/batch-ops/" },
            { label: "CentralRepoOps", link: "/patterns/central-repo-ops/" },
            { label: "ChatOps", link: "/patterns/chat-ops/" },
            { label: "DeterministicOps", link: "/patterns/deterministic-ops/" },
            { label: "DispatchOps", link: "/patterns/dispatch-ops/" },
            { label: "IssueOps", link: "/patterns/issue-ops/" },
            { label: "LabelOps", link: "/patterns/label-ops/" },
            { label: "MemoryOps", link: "/patterns/memory-ops/" },
            { label: "MonitorOps", link: "/patterns/monitor-ops/" },
            { label: "FeatureOps", link: "/patterns/feature-grower/" },
            { label: "MultiRepoOps", link: "/patterns/multi-repo-ops/" },
            { label: "OrchestratorOps", link: "/patterns/orchestrator-ops/" },
            { label: "ProjectOps", link: "/patterns/project-ops/" },
            { label: "ResearchPlanAssignOps", link: "/patterns/research-plan-assign-ops/" },
            { label: "SpecOps", link: "/patterns/spec-ops/" },
            { label: "WorkQueueOps", link: "/patterns/workqueue-ops/" },
          ],
        },
        {
          label: "Practices",
          items: [
            { label: "Measuring Impact", link: "/practices/measuring-impact/" },
            { label: "Safe Rollout in Organizations", link: "/practices/safe-rollout/" },
            { label: "Sharing Workflows in Organizations", link: "/practices/sharing-workflows/" },
          ],
        },
        {
          label: "Reference",
          items: [
            { label: "AI Engines", link: "/reference/engines/" },
            { label: "AI Engines (Custom)", link: "/reference/third-party-agent/" },
            { label: "AI Engines (Azure BYOK)", link: "/reference/azure-openai-byok/" },
            { label: "Authentication", link: "/reference/auth/" },
            { label: "Authentication (Projects)", link: "/reference/auth-projects/" },
            { label: "Compilation Process", link: "/reference/compilation-process/" },
            { label: "Configuration Governance", link: "/reference/governance/" },
            { label: "Cost Management", link: "/reference/cost-management/" },
            { label: "Cost Management (Auditing Workflows)", link: "/reference/audit/" },
            { label: "Cost Management (Billing)", link: "/reference/billing/" },
            { label: "Cost Management (Rate Limiting)", link: "/reference/rate-limiting-controls/" },
            { label: "Cost Management (Model Tables)", link: "/reference/model-tables/" },
            { label: "Enterprise Configuration", link: "/reference/enterprise-configuration/" },
            { label: "Enterprise Configuration (Compiler Controls)", link: "/reference/compiler-enterprise-environment-controls/" },
            { label: "Environment Variables", link: "/reference/environment-variables/" },
            { label: "FAQ", link: "/reference/faq/" },
            { label: "GH-AW Agent", link: "/reference/custom-agent-for-aw/" },
            { label: "GH-AW as MCP Server", link: "/reference/gh-aw-as-mcp-server/" },
            { label: "GitHub (Checkout)", link: "/reference/checkout/" },
            { label: "GitHub (Read Tools)", link: "/reference/github-tools/" },
            { label: "GitHub (Read Permissions)", link: "/reference/permissions/" },
            { label: "GitHub (Integrity Filtering)", link: "/reference/integrity/" },
            { label: "GitHub (Cross-Repository)", link: "/reference/cross-repository/" },
            { label: "GitHub (Fork Support)", link: "/reference/fork-support/" },
            { label: "GitHub (Artifacts)", link: "/reference/artifacts/" },
            { label: "Glossary", link: "/reference/glossary/" },
            { label: "Imports", link: "/reference/imports/" },
            { label: "Imports (APM)", link: "/reference/dependencies/" },
            { label: "Imports (Copilot Agent Files)", link: "/reference/copilot-custom-agents/" },
            { label: "Imports (Copilot Inline Sub-Agents)", link: "/reference/inline-sub-agents/" },
            { label: "Imports (Dependabot)", link: "/reference/dependabot/" },
            { label: "Outcomes", link: "/reference/outcomes/" },
            { label: "OpenTelemetry", link: "/reference/open-telemetry/" },
            { label: "OpenTelemetry Attributes", link: "/reference/open-telemetry-attributes/" },
            { label: "Package Manifest", link: "/reference/aw-yml-package-manifest/" },
            { label: "Releases and Versioning", link: "/reference/releases/" },
            { label: "Safe Outputs", link: "/reference/safe-outputs/" },
            { label: "Safe Outputs (Copilot Cloud Agent)", link: "/reference/copilot-cloud-agent/" },
            { label: "Safe Outputs (Custom)", link: "/reference/custom-safe-outputs/" },
            { label: "Safe Outputs (Ephemerals)", link: "/reference/ephemerals/" },
            { label: "Safe Outputs (Footers)", link: "/reference/footers/" },
            { label: "Safe Outputs (Pull Requests)", link: "/reference/safe-outputs-pull-requests/" },
            { label: "Safe Outputs (Staged Mode)", link: "/reference/staged-mode/" },
            { label: "Safe Outputs (Triggering CI)", link: "/reference/triggering-ci/" },
            { label: "Sandbox", link: "/reference/sandbox/" },
            { label: "Sandbox (Agent Runtimes)", link: "/reference/agent-runtimes/" },
            { label: "Sandbox (MCP Gateway)", link: "/reference/mcp-gateway/" },
            { label: "Sandbox (Network Access)", link: "/reference/network/" },
            { label: "Self-Hosted Runners", link: "/reference/self-hosted-runners/" },
            { label: "Self-Hosted Runners (ARC DinD)", link: "/reference/arc-dind-copilot-agent/" },
            { label: "Threat Detection", link: "/reference/threat-detection/" },
            { label: "Tools", link: "/reference/tools/" },
            { label: "Tools (Cache Memory)", link: "/reference/cache-memory/" },
            { label: "Tools (Repo Memory)", link: "/reference/repo-memory/" },
            { label: "Tools (Playwright)", link: "/reference/playwright/" },
            { label: "Tools (Serena)", link: "/reference/serena/" },
            { label: "Tools (Web Search)", link: "/reference/web-search/" },
            { label: "Workflows", link: "/reference/workflow-structure/" },
            { label: "Workflows (Frontmatter)", link: "/reference/frontmatter/" },
            { label: "Workflows (Markdown)", link: "/reference/markdown/" },
            { label: "Workflows (Triggers)", link: "/reference/triggers/" },
            { label: "Workflows (Schedule Syntax)", link: "/reference/schedule-syntax/" },
            { label: "Workflows (Command Triggers)", link: "/reference/command-triggers/" },
            { label: "Workflows (Concurrency)", link: "/reference/concurrency/" },
            { label: "Workflows (Custom Steps and Jobs)", link: "/reference/steps-jobs/" },
            { label: "Workflows (Feature Flags)", link: "/reference/feature-flags/" },
            { label: "Workflows (MCP Scripts)", link: "/reference/mcp-scripts/" },
            { label: "Workflows (Templating)", link: "/reference/templating/" },
            { label: "Workflows (Full)", link: "/reference/frontmatter-full/" },
          ],
        },
        {
          label: "Specs",
          collapsed: true,
          items: [{ autogenerate: { directory: "specs" } }],
        },
        {
          label: "Contributing",
          collapsed: true,
          items: [{ label: "Error Message Style Guide", link: "/contributing/error-messages/" }],
        },
        {
          label: "Troubleshooting",
          items: [{ autogenerate: { directory: "troubleshooting" } }],
        },
        {
          label: "Experimental",
          collapsed: true,
          items: [
            { label: "A/B Experiments", link: "/experimental/experiments/" },
            { label: "A/B Experiments (Spec)", link: "/experimental/experiments-specification/" },
            { label: "AWF Reflect Route", link: "/experimental/awf-reflect/" },
            { label: "CorrectionOps", link: "/experimental/correction-ops/" },
            { label: "Drive Memory (Private Preview)", link: "/experimental/drive-memory/" },
            { label: "Private Repository Enclaves", link: "/experimental/enclaves/" },
            { label: "Graders", link: "/experimental/trace-graders/" },
            { label: "Monitoring with Projects", link: "/experimental/monitoring-with-projects/" },
            { label: "QMD Documentation Search", link: "/experimental/qmd/" },
            { label: "Safe-Outputs Samples", link: "/experimental/safe-outputs-samples/" },
            { label: "TrialOps", link: "/experimental/trial-ops/" },
            { label: "WASM Compilation", link: "/reference/wasm-compilation/" },
          ],
        },
        { label: "Agent Factory", link: "/agent-factory-status/" },
        { label: "Editors", link: "/reference/editors/" },
        { label: "About", link: "/about/" },
      ],
    }),
  ],
});
