---
runtimes:
  python:
    version: "3.12"
excluded-env:
  - COPILOT_GITHUB_TOKEN
  - OPENAI_API_KEY
  - CODEX_API_KEY
  - ANTHROPIC_API_KEY
  - GITHUB_TOKEN
  - GH_TOKEN
  - GITHUB_ENTERPRISE_TOKEN
  - GH_ENTERPRISE_TOKEN
  - GH_AW_GITHUB_TOKEN
  - GH_AW_AGENT_TOKEN
  - GH_AW_GITHUB_MCP_SERVER_TOKEN
  - GITHUB_MCP_SERVER_TOKEN
  - LANGSMITH_API_KEY
  - LANGCHAIN_API_KEY
engine:
  id: langgraph
  detection-engine: copilot
  version: "1.2.23"
  display-name: LangGraph
  description: LangGraph tool-calling agent graph with shell and file tools and AWF-proxied inference
  experimental: true
  mcp: false
  provider:
    name: github
  behaviors:
    secret-strategy: universal-llm-consumer
    installation:
      package-manager: pip
      package-name: langgraph
      step-name: Install LangGraph Python packages
      docs-url: https://docs.langchain.com/oss/python/langgraph/quickstart
      # Complete Linux x86-64 / CPython 3.12 wheel lock. Regenerate with
      # pip download --only-binary=:all: and sha256sum.
      requirements:
        - name: annotated_types
          version: "0.8.0"
          sha256: f072f4d804ea359e4eaf198b1af7a8b0943881a87f31bb764f8bf219bb9419e0
        - name: anthropic
          version: "2.10.0"
          sha256: 1b91b2b4aa2bab44db8712e8a9053454bcff6daa3e6f5f11d85dc38b37d0a2a3
        - name: anyio
          version: "4.16.0"
          sha256: c66ae634e0b8228396b801741beb3294326d4b6102f7bba4041c0dfb58c29026
        - name: certifi
          version: "2026.10.23"
          sha256: e9b54de76e530bb6cf0ccd036787103acb58fd42ba930ea8bf948ddbaa2f61d1
        - name: charset_normalizer
          version: "3.5.2"
          sha256: 3d31298449090ab8d47b7b1b2a555ff73cac7ed438a08b7ac160980c7ebed649
        - name: distro
          version: "1.9.0"
          sha256: 7bffd925d65168f85027d8da9af6bddab658135b840670a223589bc0c8ef02b2
        - name: docstring_parser
          version: "0.18.0"
          sha256: b3fcbed555c47d8479be0796ef7e19c2670d428d72e96da63f3a40122860374b
        - name: h11
          version: "0.16.0"
          sha256: 63cf8bbe7522de3bf65932fda1d9c2772064ffb3dae62d55932da54b31cb6c86
        - name: httpcore
          version: "1.0.9"
          sha256: 2d400746a40668fc9dec9810239072b40b4484b640a8c38fd654a024c7a1bf55
        - name: httpcore2
          version: "2.16.2"
          sha256: 16051ae2a42f776007543f32de9349a317b23d2cbb4e1456b4bac0e88b87419a
        - name: httpx
          version: "0.28.1"
          sha256: d909fcccc110f8c7faf814ca82a9a4d816bc5a6dbfea25d6591d6985b8ba59ad
        - name: httpx2
          version: "2.16.2"
          sha256: f05c13aade4c45dbf1c9060647a1058f0acad62398fad96e741269edb33d46f5
        - name: idna
          version: "3.20"
          sha256: ab7ae7122974553370f0bdb919e1a960b2cd1bc1ef0276416d896db81c14582c
        - name: jiter
          version: "0.19.0"
          sha256: 0956b5f58b804221dfb929c120e6d4032138fed20c1cc0ef683a2d1326255e46
        - name: jsonpatch
          version: "1.47"
          sha256: c9ba6de59c481bf2be05041514ecb0d97f994aa645c4e00831805dbe2281811a
        - name: jsonpointer
          version: "3.12"
          sha256: 89f391335ee2719ae72149e1bf30a4d4c7c5d92f15a3f53faa8d20cc7c6c7f19
        - name: langchain_anthropic
          version: "1.7.11"
          sha256: 1610011c7da9f30f04ef7f3118168acaf034afe9b2b0ceb76c73a9bbb25bc90b
        - name: langchain_core
          version: "1.8.3"
          sha256: 4d2764d2ab4faf3fec9f17515f15c0d3485a6ef55f75c088f6929944225ff578
        - name: langchain_openai
          version: "1.8.3"
          sha256: e383c2c2f53ec2b4985fb90504f56005310df1d56a758387fe6459b7775fafbf
        - name: langchain_protocol
          version: "0.0.21"
          sha256: 7f42f9eae7c08bb9d74850057da50b5483970a3df91b7a8a5aadec6a3dfa4d12
        - name: langgraph
          version: "1.2.23"
          sha256: 148eb984aed7641248875634f9b06ec8965aa2582751483c186527eed38c2c06
        - name: langgraph_checkpoint
          version: "4.3.2"
          sha256: 60e02eb2c2eefc4c22fb1549f1c90b85ce9617377604596c5f34fbd6af0bd6e9
        - name: langgraph_prebuilt
          version: "1.1.3"
          sha256: 7b55eb1c2004348b73b9af9e2f81e5f3eda47dcf13d885ccb4195538426c55ce
        - name: langgraph_sdk
          version: "0.4.14"
          sha256: d47a6755890ae537aa80613a3138680ac77f5d256a2d3953af95a96328e6964d
        - name: langsmith
          version: "0.15.0"
          sha256: d6f7aa67e4c984ce7c7b6a1bdb7fc50ce3caaca032b42805f0f6a2d6458f42be
        - name: openai
          version: "3.55.1"
          sha256: 9136b1a760401aa150d084b0a673a4eca5b434917f3afe0378321cf544d5bdc2
        - name: opentelemetry_api
          version: "1.45.2"
          sha256: 2cbd737377e710ee740d261c94e93fa0f08e6d037c18f17513cc0a2d94479a34
        - name: orjson
          version: "3.13.0"
          sha256: bceadfd314bd238f584fc229a4bbaf0e573597e7a026dec5429fbf29fd66c641
        - name: ormsgpack
          version: "1.13.1"
          sha256: f817950f3c268af028bcd764a7d9c7c9743f358d00f34ea0962a9dbfee3b3889
        - name: packaging
          version: "26.3"
          sha256: d7193f7c8e4e93f444fde0262bf90af30e16fa0ad0ad44cb553c87339b23cd1c
        - name: pydantic
          version: "2.14.4"
          sha256: 653a2ad9f6d8858529c77c72717d0ea991d6c6837fe683246b2f4b2bbfc3fadc
        - name: pydantic_core
          version: "2.50.5"
          sha256: 470bbc2495367f60d3ca849a8039e4c164701d10637e7743f4427d92651bdb6d
        - name: pyyaml
          version: "6.0.3"
          sha256: ba1cc08a7ccde2d2ec775841541641e4548226580ab850948cbfda66a1befcdc
        - name: regex
          version: "2026.11.19"
          sha256: 8a966a5afe94ed9fb06deeecd9e3e51c3a5ab7d920e13f8128338e3fcdfed2b0
        - name: requests
          version: "2.34.2"
          sha256: 2a0d60c172f83ac6ab31e4554906c0f3b3588d37b5cb939b1c061f4907e278e0
        - name: requests_toolbelt
          version: "1.0.0"
          sha256: cccfdd665f0a24fcf4726e690f65639d272bb0637b9b92dfd91a5568ccf6bd06
        - name: sniffio
          version: "1.3.1"
          sha256: 2f6da418d1f1e0fddd844478f41680e794e6051915791a034ff65e5f100525a2
        - name: tenacity
          version: "9.2.1"
          sha256: 9e56f17539296baab7beabb08b92f6ee3d7be92d8be72d763360677c2ad6580e
        - name: tiktoken
          version: "0.14.0"
          sha256: 7896eea257fe497a2b7134474d909156c6744ce8da35bce88011a960e008aa0d
        - name: truststore
          version: "0.10.5"
          sha256: 9aaaedaefaf06d8b206278cf8b5012bc897f485a874503501e12d776df78951c
        - name: typing_extensions
          version: "4.16.0"
          sha256: 481caa481374e813c1b176ada14e97f1f67a4539ce9cfeb3f350d78d6370c2e8
        - name: typing_inspection
          version: "0.4.5"
          sha256: bc210535442b39e1bf2805e0c7a17dfd6a4ace760c84c7f7d16245d23a6b1c39
        - name: urllib3
          version: "2.8.0"
          sha256: 0cf3cae568d36aa9576b28dfb35f11328f1cb974ca7647d9475ebb86c75ac6e3
        - name: uuid_utils
          version: "0.19.0"
          sha256: b8060530a8fa27f11947489c79bc36e6ebb465b2d714945364dc397283309417
        - name: websockets
          version: "17.4"
          sha256: 9ae1429ee9e52b139167ed0169757806a27ed7307e942e8c62037a9184e9843b
        - name: xxhash
          version: "4.1.0"
          sha256: 34b86625dc36b54403b525e1d7b74a64343d9b65cee6e94872c6bd70722267ac
        - name: zstandard
          version: "0.25.0"
          sha256: 5a56ba0db2d244117ed744dfa8f6f5b366e14148e00de44723413b2f3938a902
    capabilities:
      max-turns: true
    output-files:
      - /tmp/gh-aw/agent/langgraph.traj.json
    network:
      defaults:
        - host.docker.internal
      provider-domains:
        copilot: api.githubcopilot.com
        anthropic: api.anthropic.com
        openai: api.openai.com
    execution:
      command-name: python3
      step-name: Execute LangGraph agent
      model-env-var: GH_AW_LANGGRAPH_SELECTED_MODEL
      provider-env-mode: universal-llm-consumer
      write-timestamp: true
      env:
        LANGSMITH_TRACING: "false"
        LANGCHAIN_TRACING_V2: "false"
        GH_AW_LANGGRAPH_TRAJECTORY: /tmp/gh-aw/agent/langgraph.traj.json
      args:
        - -I
        - -c
        - |
          import json
          import os
          import subprocess
          import sys
          from importlib.metadata import version
          from pathlib import Path

          sys.path.insert(0, os.environ["GH_AW_PYTHON_PACKAGES"])
          assert version("langgraph") == os.environ["GH_AW_ENGINE_VERSION"]

          from langchain_core.messages import HumanMessage, SystemMessage, ToolMessage, messages_to_dict
          from langchain_core.tools import tool
          from langgraph.errors import GraphRecursionError
          from langgraph.graph import END, START, MessagesState, StateGraph

          cwd = Path(os.environ["GH_AW_ENGINE_CWD"])
          limit = int(os.environ["GH_AW_LANGGRAPH_STEP_LIMIT"])
          output_limit = 20000

          def clip(text):
              if len(text) <= output_limit:
                  return text
              return text[:output_limit] + f"\n[truncated {len(text) - output_limit} characters]"

          @tool
          def run_shell(command: str) -> str:
              """Run a bash command in the repository working directory and return its exit code, stdout, and stderr."""
              try:
                  result = subprocess.run(["bash", "-c", command], cwd=cwd, capture_output=True, text=True, timeout=600)
              except subprocess.TimeoutExpired:
                  return "error: command timed out after 600 seconds"
              return clip(f"exit code: {result.returncode}\nstdout:\n{result.stdout}\nstderr:\n{result.stderr}")

          @tool
          def read_file(path: str) -> str:
              """Read a UTF-8 text file. Relative paths resolve from the repository working directory."""
              try:
                  return clip((cwd / path).read_text())
              except (OSError, UnicodeDecodeError) as error:
                  return f"error: {error}"

          @tool
          def write_file(path: str, content: str) -> str:
              """Create or overwrite a UTF-8 text file. Relative paths resolve from the repository working directory."""
              target = cwd / path
              try:
                  target.parent.mkdir(parents=True, exist_ok=True)
                  target.write_text(content)
              except OSError as error:
                  return f"error: {error}"
              return f"wrote {len(content)} characters to {target}"

          if os.environ["GH_AW_LANGGRAPH_CLIENT"] == "anthropic":
              from langchain_anthropic import ChatAnthropic
              model = ChatAnthropic(
                  model=os.environ["GH_AW_LANGGRAPH_MODEL"],
                  base_url=os.environ["GH_AW_LANGGRAPH_API_BASE"],
                  api_key="awf-proxy",
                  max_tokens=8192,
              )
          else:
              from langchain_openai import ChatOpenAI
              model = ChatOpenAI(
                  model=os.environ["GH_AW_LANGGRAPH_MODEL"],
                  base_url=os.environ["GH_AW_LANGGRAPH_API_BASE"],
                  api_key="awf-proxy",
              )

          tools = [run_shell, read_file, write_file]
          tools_by_name = {entry.name: entry for entry in tools}
          model_with_tools = model.bind_tools(tools)
          system = SystemMessage(
              "You are an autonomous software engineering agent running non-interactively in a GitHub Actions sandbox. "
              f"The repository working directory is {cwd}. Use the run_shell, read_file, and write_file tools to "
              "complete the task. gh-aw tools and safe outputs are available as shell commands. "
              "When the task is complete, reply with a concise final summary and do not call any tools."
          )

          def llm_call(state: MessagesState):
              return {"messages": [model_with_tools.invoke([system] + state["messages"])]}

          def tool_node(state: MessagesState):
              results = []
              for call in state["messages"][-1].tool_calls:
                  selected = tools_by_name.get(call["name"])
                  try:
                      content = selected.invoke(call["args"]) if selected else f"error: unknown tool {call['name']}"
                  except Exception as error:
                      content = f"error: {error}"
                  results.append(ToolMessage(content=content, tool_call_id=call["id"]))
              return {"messages": results}

          def should_continue(state: MessagesState):
              return "tool_node" if state["messages"][-1].tool_calls else END

          builder = StateGraph(MessagesState)
          builder.add_node("llm_call", llm_call)
          builder.add_node("tool_node", tool_node)
          builder.add_edge(START, "llm_call")
          builder.add_conditional_edges("llm_call", should_continue, ["tool_node", END])
          builder.add_edge("tool_node", "llm_call")
          agent = builder.compile()

          trajectory = Path(os.environ["GH_AW_LANGGRAPH_TRAJECTORY"])
          trajectory.parent.mkdir(parents=True, exist_ok=True)
          fd = os.open(trajectory, os.O_CREAT | os.O_WRONLY | os.O_TRUNC, 0o600)
          os.fchmod(fd, 0o600)
          os.close(fd)
          messages = [HumanMessage(Path(os.environ["GH_AW_PROMPT"]).read_text())]
          status = "Failed"
          try:
              # Each model call and tool execution is one superstep, so this bounds model calls to `limit`.
              for state in agent.stream({"messages": messages}, {"recursion_limit": 2 * limit}, stream_mode="values"):
                  messages = state["messages"]
              final = messages[-1]
              if final.type == "ai" and not final.tool_calls:
                  status = "Completed"
                  print(final.text, flush=True)
          except GraphRecursionError:
              status = "LimitsExceeded"
          finally:
              trajectory.write_text(json.dumps({"exit_status": status, "messages": messages_to_dict(messages)}, indent=2))
              print(f"LangGraph exit status: {status}", flush=True)
          if status != "Completed":
              raise SystemExit(1)
    harness-script: |
      const { spawnSync } = require("child_process");
      const { constants } = require("os");
      const { resolve } = require("path");
      const { fetchAWFReflect, deriveBaseUrlFromModelsURL, normalizeReflectProviderName, REFLECT_PROVIDER_ALIASES } = require("./awf_reflect.cjs");

      const log = message => process.stderr.write(`[langgraph] ${message}\n`);
      const main = async () => {
        const [command, ...args] = process.argv.slice(2);
        if (!command) throw new Error("LangGraph command is required");
        if (process.env.AWF_REFLECT_ENABLED !== "1") {
          throw new Error("LangGraph requires the AWF sandbox and a configured LLM provider");
        }
        const selected = process.env.GH_AW_LANGGRAPH_SELECTED_MODEL;
        const separator = selected?.indexOf("/") ?? -1;
        if (separator <= 0 || separator === selected.length - 1) {
          throw new Error("GH_AW_LANGGRAPH_SELECTED_MODEL must use provider/model format");
        }
        const provider = process.env.GH_AW_LLM_PROVIDER;
        if (!["github", "openai", "anthropic"].includes(provider)) {
          throw new Error("LangGraph supports copilot, openai/codex, or anthropic models");
        }
        const limit = process.env.GH_AW_MAX_TURNS || "20";
        if (!/^[1-9][0-9]*$/.test(limit) || !Number.isSafeInteger(Number(limit))) {
          throw new Error("GH_AW_MAX_TURNS must be a positive safe integer");
        }
        if (!process.env.GH_AW_PROMPT || !process.env.GITHUB_WORKSPACE) {
          throw new Error("GH_AW_PROMPT and GITHUB_WORKSPACE are required");
        }
        const reflected = await fetchAWFReflect({ logger: log });
        if (!reflected.ok || !reflected.reflectData) {
          throw new Error(`Unable to discover the LangGraph LLM endpoint: ${reflected.reason || "empty /reflect response"}`);
        }
        const aliases = REFLECT_PROVIDER_ALIASES[provider];
        const endpoint = reflected.reflectData.endpoints?.find(entry =>
          entry?.configured === true && aliases.has(normalizeReflectProviderName(entry.provider)) &&
          typeof entry.models_url === "string"
        );
        if (!endpoint) {
          throw new Error(`No configured LangGraph /reflect models endpoint for provider ${provider}`);
        }
        let base = deriveBaseUrlFromModelsURL(endpoint.models_url);
        // The Anthropic SDK appends /v1 itself; OpenAI-compatible clients expect it in the base URL.
        if (provider === "anthropic") base = base.replace(/\/+$/, "").replace(/\/v1$/, "");
        // CLI wrappers embed gateway credentials; only their nonsecret path/metadata bindings are inherited.
        const env = {};
        for (const name of [
          "PATH", "HOME", "LANG", "LC_ALL", "TERM", "TMPDIR", "RUNNER_TEMP",
          "GITHUB_WORKSPACE", "GITHUB_REPOSITORY", "GH_HOST", "GH_CONFIG_DIR",
          "NO_PROXY", "no_proxy",
          "GH_AW_MCP_DEFERRED_SERVERS", "GH_AW_SAFE_OUTPUTS_TOOLS_PATH",
          "ENGINE_CONFORMANCE_SENTINEL",
          "GH_AW_PROMPT", "GH_AW_ENGINE_VERSION", "GH_AW_PYTHON_PACKAGES",
          "GH_AW_LANGGRAPH_TRAJECTORY",
        ]) {
          if (process.env[name] !== undefined) env[name] = process.env[name];
        }
        for (const name of ["HTTP_PROXY", "HTTPS_PROXY", "http_proxy", "https_proxy", "AWF_CLI_PROXY_URL"]) {
          const value = process.env[name];
          if (!value) continue;
          let url;
          try { url = new URL(value); } catch { throw new Error(`${name} must be a credential-free HTTP(S) URL`); }
          if (!["http:", "https:"].includes(url.protocol) || url.username || url.password) {
            throw new Error(`${name} must be a credential-free HTTP(S) URL`);
          }
          env[name] = value;
        }
        Object.assign(env, {
          LANGSMITH_TRACING: "false",
          LANGCHAIN_TRACING_V2: "false",
          GH_AW_ENGINE_CWD: resolve(process.env.GITHUB_WORKSPACE, process.env.GH_AW_ENGINE_CWD || "."),
          GH_AW_LANGGRAPH_CLIENT: provider === "anthropic" ? "anthropic" : "openai",
          GH_AW_LANGGRAPH_MODEL: selected.slice(separator + 1),
          GH_AW_LANGGRAPH_API_BASE: base,
          GH_AW_LANGGRAPH_STEP_LIMIT: limit,
        });
        const result = spawnSync(command, args, { env, cwd: env.GH_AW_ENGINE_CWD, stdio: "inherit" });
        if (result.error) throw result.error;
        if (result.status !== 0) {
          const error = new Error(`LangGraph execution failed: ${result.signal || `exit code ${result.status ?? "unknown"}`}`);
          error.exitCode = result.status ?? (constants.signals[result.signal] ? 128 + constants.signals[result.signal] : 1);
          throw error;
        }
      };
      main().catch(error => {
        log(error instanceof Error ? error.message : String(error));
        process.exitCode = error.exitCode || 1;
      });
---

<!--
# LangGraph

This unsupported experimental sample runs a [LangGraph](https://docs.langchain.com/oss/python/langgraph/quickstart)
tool-calling agent built with the Graph API: a `StateGraph` over
`MessagesState` with an `llm_call` node, a `tool_node`, and a conditional edge
that ends the graph when the model stops requesting tools.

It pins `langgraph==1.2.23`, `langchain-openai==1.8.3`, and
`langchain-anthropic==1.7.11` from PyPI on Python 3.12 / Linux x86-64. The
imported definition declares the complete version-and-SHA256 wheel lock in
`behaviors.installation.requirements`. Before AWF starts, the compiler-generated
installation step runs `pip install --require-hashes --only-binary=:all:` into
`/tmp/gh-aw/agent/langgraph-python/site-packages`, verifies the installed
`langgraph` version, and exposes the directory as `GH_AW_PYTHON_PACKAGES`.
Isolated Python ignores workspace imports and Python environment search paths; only the
explicit dependency directory is added. The shared import's `runtimes.python`
declaration opts into Python ecosystem network access for agent execution;
vendor the definition without `runtimes` for registry-free execution.

Import it and select a workflow-facing `provider/model`:

```yaml
engine:
  id: langgraph
model: copilot/claude-haiku-5.5
imports:
  - shared/langgraph.md
sandbox:
  agent:
    id: awf
```

Copilot uses `copilot-requests: write` or `COPILOT_GITHUB_TOKEN`; OpenAI/Codex
and Anthropic use their standard gh-aw provider secrets. AWF is required for
all providers. The harness discovers the matching endpoint through `/reflect`,
uses `ChatOpenAI` for Copilot/OpenAI/Codex and `ChatAnthropic` for Anthropic,
and passes a placeholder key. Real provider and LangSmith credentials are
excluded from the agent environment, and LangSmith tracing is disabled.
The child environment uses an explicit allowlist of runtime, credential-free
AWF proxy URLs, bypass hosts, and CLI-proxy path/metadata bindings.

The graph exposes `run_shell`, `read_file`, and `write_file` tools rooted at
the workspace. The run succeeds only when the model returns a final answer
without tool calls. `max-turns` bounds model calls through the LangGraph
recursion limit, defaulting to 20.

There is no native MCP client: configured tools and safe outputs are available
through gh-aw's shell CLI proxies. Native shell command allowlists, disabling
file edits, Agent Plugins, LangGraph checkpointers, and structured session
parsing are not supported. The private message trajectory at
`/tmp/gh-aw/agent/langgraph.traj.json` is retained as untrusted diagnostic data
in the unified agent artifact after secret redaction and recovery-envelope
validation succeed; it is not verified tool or token-accounting evidence.

Treat issue, comment, and repository text as untrusted input. AWF network
restrictions, credential exclusion, inference budgets, and policy-controlled
CLI proxies are the enforcement boundaries. Use this shell-capable engine only
for workflows that intentionally allow local commands.
-->
