---
runtimes:
  python:
    version: "3.12"
pre-agent-steps:
  - name: Stage hash-locked mini-SWE-agent wheels
    env:
      GH_AW_MINI_DEPS: /tmp/gh-aw/agent/mini-swe-agent-deps
      GH_AW_MINI_DEPS_LOCK_SHA256: aefd92655165b1cc9fad1e303bc0f9d02833cd2de8f4bdc0a4f03573c63b4aa7
    run: |
          python3 -I - <<'PY'
          import hashlib
          import os
          import platform
          import subprocess
          import sys
          from pathlib import Path

          # Self-contained Linux x86-64 / CPython 3.12 wheel lock.
          requirements = """
          aiohappyeyeballs==2.7.1 --hash=sha256:9243213661e29250eb41368e5daa826fc017156c3b8a11440826b2e3ed376472
          aiohttp==3.14.5 --hash=sha256:20f085697d7e911f1f73c43ed03fafbed1e7121797e2eb5428efa80398060584
          aiosignal==1.4.0 --hash=sha256:053243f8b92b990551949e63930a839ff0cf0b0ebbe0597b0f3fb19e1a0fe82e
          annotated-doc==0.0.5 --hash=sha256:117bac03a25ede5df5440e855b32d556049ca169ead221505badf432fed4b101
          annotated-types==0.8.0 --hash=sha256:f072f4d804ea359e4eaf198b1af7a8b0943881a87f31bb764f8bf219bb9419e0
          anyio==4.16.0 --hash=sha256:c66ae634e0b8228396b801741beb3294326d4b6102f7bba4041c0dfb58c29026
          attrs==26.1.0 --hash=sha256:c647aa4a12dfbad9333ca4e71fe62ddc36f4e63b2d260a37a8b83d2f043ac309
          boto3==1.43.126 --hash=sha256:dd701a973b02a73930e546a6a15d13edbfbedc3180bb3d55b0a88678c284ccd6
          botocore==1.43.126 --hash=sha256:2f2239a47025a2fa173cd145ab8803ac02b76114864bf5b66475ff1cb88e6b0a
          certifi==2026.10.23 --hash=sha256:e9b54de76e530bb6cf0ccd036787103acb58fd42ba930ea8bf948ddbaa2f61d1
          charset-normalizer==3.5.2 --hash=sha256:3d31298449090ab8d47b7b1b2a555ff73cac7ed438a08b7ac160980c7ebed649
          click==8.5.1 --hash=sha256:8bb5376d2006950386f8c6427613334a2acadc5f559cb80e638d00b4df957e37
          datasets==5.1.0 --hash=sha256:86fd08b280485aab82894d2c81842c4151d93b575efea0054315104cb7bc3cec
          dill==0.4.1 --hash=sha256:1e1ce33e978ae97fcfcff5638477032b801c46c7c65cf717f95fbc2248f79a9d
          fastuuid==0.14.0 --hash=sha256:808527f2407f58a76c916d6aa15d58692a4a019fdf8d4c32ac7ff303b7d7af09
          filelock==4.2.9 --hash=sha256:a379c50a729e5b68c8519a4d26489f755d9769960a236ecd838190e2a78db2e9
          frozenlist==1.8.0 --hash=sha256:494a5952b1c597ba44e0e78113a7266e656b9794eec897b19ead706bd7074383
          fsspec==2026.7.0 --hash=sha256:b57ddbafedfaef7018c1ecab32aa200a9d7ca26b77965f64e48b70061249d279
          h11==0.16.0 --hash=sha256:63cf8bbe7522de3bf65932fda1d9c2772064ffb3dae62d55932da54b31cb6c86
          h2==4.4.1 --hash=sha256:0e25f1462b23c9cb82d9eb02e28bc706dac2a68cb457c6a0d74d63c8a2a5d0e6
          hf-xet==1.8.0 --hash=sha256:29dab7c904cd1914a0439c6e1306c12c7497c8bd32290fbe563ebf51ca4b6d6d
          hpack==4.2.0 --hash=sha256:858ac0b02280fa582b5080d68db0899c62a80375e0e5413a74970c5e518b6986
          httpcore==1.0.9 --hash=sha256:2d400746a40668fc9dec9810239072b40b4484b640a8c38fd654a024c7a1bf55
          httpcore2==2.16.0 --hash=sha256:5c4358319f2602d984ad93182c9fee5160f22e9da7babdda870a5e7c4e2dc34f
          httpx==0.28.1 --hash=sha256:d909fcccc110f8c7faf814ca82a9a4d816bc5a6dbfea25d6591d6985b8ba59ad
          httpx2==2.16.0 --hash=sha256:fd2c5d7909e8d86fe92d53020c310ca0e4e431b838c207c5754b9bbf0308d49a
          huggingface_hub==1.33.0 --hash=sha256:04e434b06e100eddbce9a6e817d72693a7884b10a79bd67ab48080d5c07eb899
          hyperframe==6.1.0 --hash=sha256:b03380493a519fce58ea5af42e4a42317bf9bd425596f7a0835ffce80f1a42e5
          idna==3.20 --hash=sha256:ab7ae7122974553370f0bdb919e1a960b2cd1bc1ef0276416d896db81c14582c
          importlib_metadata==8.9.0 --hash=sha256:e0f761b6ea91ced3b0844c14c9d955224d538105921f8e6754c00f6ca79fba7f
          Jinja2==3.1.6 --hash=sha256:85ece4451f492d0c13c5dd7c13a64681a86afae63a5f347908daf103ce6d2f67
          jiter==0.19.0 --hash=sha256:0956b5f58b804221dfb929c120e6d4032138fed20c1cc0ef683a2d1326255e46
          jmespath==1.1.0 --hash=sha256:a5663118de4908c91729bea0acadca56526eb2698e83de10cd116ae0f4e97c64
          jsonschema==4.26.0 --hash=sha256:d489f15263b8d200f8387e64b4c3a75f06629559fb73deb8fdfb525f2dab50ce
          jsonschema-specifications==2025.9.1 --hash=sha256:98802fee3a11ee76ecaca44429fda8a41bff98b00a0f2838151b113f210cc6fe
          linkify-it-py==2.2.0 --hash=sha256:3adc40eb5af300b2605fcfdb968c24e1d780a90f1f2221af7c15e5111e94d443
          litellm==1.108.3 --hash=sha256:4ac55c4a80be5b131659b1f42487de96fed69c6717745953c74fede1d6a5113c
          markdown-it-py==4.2.0 --hash=sha256:9f7ebbcd14fe59494226453aed97c1070d83f8d24b6fc3a3bcf9a38092641c4a
          MarkupSafe==3.0.4 --hash=sha256:8e124f974786f831d6043728e38296969d3579db8896fe004682f5758e613581
          mdit-py-plugins==0.6.1 --hash=sha256:214c82fb2ac524472ab6a5bcab1de80f73b50443e187f401bfd77efbc7c6481d
          mdurl==0.1.2 --hash=sha256:84008a41e51615a49fc9966191ff91509e3c40b939176e643fd50a5c2196b8f8
          mini-swe-agent==2.4.6 --hash=sha256:a35463c553ac825c7773b03cfa69cd44958e3af20155dcc5711fdf9e4c67cd54
          multidict==7.2.1 --hash=sha256:1fb8c1a3480c130c3a6422d5f16f3cad6552bf08c2089dd288ba7134649183b5
          multiprocess==0.70.19 --hash=sha256:3a56c0e85dd5025161bac5ce138dcac1e49174c7d8e74596537e729fd5c53c28
          numpy==2.5.4 --hash=sha256:fbde6962867ee75b48b0ee29b2b9372ec5d617799dbaf38e82dc0596f2f7738a
          openai==3.46.0 --hash=sha256:7b650117e9bf539029bf880c2eff94a26f281394f6572284efa5d068d4b1c98e
          packaging==26.3 --hash=sha256:d7193f7c8e4e93f444fde0262bf90af30e16fa0ad0ad44cb553c87339b23cd1c
          pandas==3.1.1 --hash=sha256:028490c8ac2e03e18c9b08b7f3612e149b03570209bcf741d23833a7a5377ab2
          platformdirs==4.14.5 --hash=sha256:8e778c6fa421516f4d3369449f8df3b0e1b72c0e0005fe88e3ec772d0a14e750
          prompt_toolkit==3.0.53 --hash=sha256:01c0891d7f9237d5e339f7d3e42cdae80b7534abb1c7c0e3352efba6231492f2
          propcache==0.5.4 --hash=sha256:2814ecd8e818f487bee4b0f921bc4d1c176cc5fc71ac0f072d0fa67eda4ac14b
          pyarrow==26.0.1 --hash=sha256:c718bf97a92a6dea97fd1062c3b9f9e2e83cb9cc926e752289fa4ffff41738f9
          pydantic==2.14.3 --hash=sha256:3f7b347782ab7e4cf4c798496ae3a82787d9f21ec12676a9c564355eeef8daa8
          pydantic-settings==2.16.2 --hash=sha256:8d523e5c129b097686bde019bd6109bff53f4c8c3f63f1ddf9933ffa42e5ab4f
          pydantic_core==2.50.4 --hash=sha256:c81d98e27d9d9261909dcccab1ba5d6ea1c98b0bb9470e368604614fd1d36ddd
          Pygments==2.21.0 --hash=sha256:2363c69b61c4a97c838da3b130dcd6468f4848992b21a82f2a63ec34377137d9
          python-dateutil==2.9.0.post0 --hash=sha256:a8b2bc7bffae282281c8140a97d3aa9c14da0b136dfe83f850eea9a5f7470427
          python-dotenv==1.2.4 --hash=sha256:42269a8a5b3fd54ffa6f3d84b18abed50064717576b4ecf03dc4a55d8aa04fdc
          PyYAML==6.0.3 --hash=sha256:ba1cc08a7ccde2d2ec775841541641e4548226580ab850948cbfda66a1befcdc
          referencing==0.37.0 --hash=sha256:381329a9f99628c9069361716891d34ad94af76e461dcb0335825aecc7692231
          regex==2026.11.7 --hash=sha256:36f1bff9b25e6fe78ad664aacf0cf99ccba6927b32ba31de42a3ca21f9fb15a8
          requests==2.34.2 --hash=sha256:2a0d60c172f83ac6ab31e4554906c0f3b3588d37b5cb939b1c061f4907e278e0
          rich==15.0.0 --hash=sha256:33bd4ef74232fb73fe9279a257718407f169c09b78a87ad3d296f548e27de0bb
          rpds-py==2026.9.1 --hash=sha256:addeda51556dac7c1a2f14cda62db8b621cd12afba3091d03a96c72932387eab
          s3transfer==0.19.3 --hash=sha256:24eb7535804306355e31c952a90022bf5b2f9d760f9a11062c7493a217c08553
          shellingham==1.5.4 --hash=sha256:7ecfff8f2fd72616f7481040475a65b2bf8af90a56c89140852d1120324e8686
          six==1.17.0 --hash=sha256:4721f391ed90541fddacab5acf947aa0d3dc7d27b2e1e8eda2be8970586c3274
          sniffio==1.3.1 --hash=sha256:2f6da418d1f1e0fddd844478f41680e794e6051915791a034ff65e5f100525a2
          tenacity==9.2.1 --hash=sha256:9e56f17539296baab7beabb08b92f6ee3d7be92d8be72d763360677c2ad6580e
          textual==8.2.8 --hash=sha256:267375fd402dc8d981457212efa71f0e3365fd17bba144ba9bb3ed7563cb374a
          tiktoken==0.14.0 --hash=sha256:7896eea257fe497a2b7134474d909156c6744ce8da35bce88011a960e008aa0d
          tokenizers==0.23.3 --hash=sha256:376851d22bcf9d650a5c3090bb83e6cf9e895fbf0595369fa4cd43c1f69b5f87
          tqdm==4.70.1 --hash=sha256:c293e525e6fef9c20e8728fd4612df02a0aa31bb5fe91ecd93e123b1b7bffa73
          truststore==0.10.5 --hash=sha256:9aaaedaefaf06d8b206278cf8b5012bc897f485a874503501e12d776df78951c
          typer==0.28.0 --hash=sha256:afd18929180b706c5e9a239f53c3c8136903553c0c2d460598bbaa924191d4ab
          typing-inspection==0.4.5 --hash=sha256:bc210535442b39e1bf2805e0c7a17dfd6a4ace760c84c7f7d16245d23a6b1c39
          typing_extensions==4.16.0 --hash=sha256:481caa481374e813c1b176ada14e97f1f67a4539ce9cfeb3f350d78d6370c2e8
          urllib3==2.8.0 --hash=sha256:0cf3cae568d36aa9576b28dfb35f11328f1cb974ca7647d9475ebb86c75ac6e3
          wcwidth==0.9.4 --hash=sha256:b185c8b927b23af2721be1783499d1cfa5c028b098b0858951b4ae224477f1c2
          xxhash==4.0.2 --hash=sha256:7c7c95dba490be3b4e5a2fa5595b4d4f088fe7b8726e5998b8c1d83b1f9eb3e4
          yarl==1.25.1 --hash=sha256:c6f117789d22dce188e5754e8bc65b7e6ebf8cb73963b9fa761f672a5883769d
          zipp==4.1.2 --hash=sha256:b30cb6cd253122169c47de245ca24a86bac2a5d3802ba15bf7ba6414eb9e144d
          """
          if hashlib.sha256(requirements.encode()).hexdigest() != os.environ["GH_AW_MINI_DEPS_LOCK_SHA256"]:
              raise RuntimeError("mini-SWE-agent dependency lock does not match the trusted bootstrap lock")
          if not requirements.strip():
              raise RuntimeError("mini-SWE-agent dependency lock is required")
          if f"mini-swe-agent=={os.environ['GH_AW_ENGINE_VERSION']} --hash=" not in requirements:
              raise RuntimeError("mini-SWE-agent version must match the dependency lock")
          if sys.version_info[:2] != (3, 12) or sys.platform != "linux" or platform.machine() != "x86_64":
              raise RuntimeError("mini-SWE-agent wheel lock requires Linux x86-64 and Python 3.12")
          dependencies = Path(os.environ["GH_AW_MINI_DEPS"])
          dependencies.mkdir(parents=True, exist_ok=True)
          lock = dependencies / "requirements.lock"
          lock.write_text(requirements)
          subprocess.run([
              sys.executable, "-I", "-m", "pip", "download", "--quiet",
              "--isolated", "--disable-pip-version-check", "--only-binary=:all:",
              "--require-hashes",
              "--dest", str(dependencies / "wheels"),
              "--requirement", str(lock),
          ], check=True)
          PY
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
engine:
  id: mini-swe-agent
  detection-engine: copilot
  version: "2.4.6"
  display-name: mini-SWE-agent
  description: mini-SWE-agent with non-interactive shell execution and AWF-proxied inference
  experimental: true
  mcp: false
  provider:
    name: github
  behaviors:
    secret-strategy: universal-llm-consumer
    capabilities:
      max-turns: true
    output-files:
      - /tmp/gh-aw/agent/mini-swe-agent.traj.json
    network:
      defaults:
        - host.docker.internal
      provider-domains:
        copilot: api.githubcopilot.com
        anthropic: api.anthropic.com
        openai: api.openai.com
    execution:
      command-name: python3
      step-name: Execute mini-SWE-agent
      model-env-var: GH_AW_MINI_MODEL
      provider-env-mode: universal-llm-consumer
      write-timestamp: true
      env:
        MSWEA_SILENT_STARTUP: "1"
        LITELLM_TELEMETRY: "false"
        LITELLM_LOCAL_MODEL_COST_MAP: "True"
        GH_AW_MINI_DEPS: /tmp/gh-aw/agent/mini-swe-agent-deps
        GH_AW_MINI_DEPS_LOCK_SHA256: aefd92655165b1cc9fad1e303bc0f9d02833cd2de8f4bdc0a4f03573c63b4aa7
        GH_AW_MINI_TRAJECTORY: /tmp/gh-aw/agent/mini-swe-agent.traj.json
      args:
        - -I
        - -c
        - |
          import os
          import hashlib
          import platform
          import subprocess
          import sys
          from importlib.metadata import version
          from pathlib import Path

          dependencies = Path(os.environ["GH_AW_MINI_DEPS"])
          lock = dependencies / "requirements.lock"
          requirements = lock.read_text()
          if hashlib.sha256(requirements.encode()).hexdigest() != os.environ["GH_AW_MINI_DEPS_LOCK_SHA256"]:
              raise RuntimeError("mini-SWE-agent dependency lock does not match the trusted bootstrap lock")
          if not requirements.strip():
              raise RuntimeError("mini-SWE-agent dependency lock is required")
          if f"mini-swe-agent=={os.environ['GH_AW_ENGINE_VERSION']} --hash=" not in requirements:
              raise RuntimeError("mini-SWE-agent version must match the dependency lock")
          if sys.version_info[:2] != (3, 12) or sys.platform != "linux" or platform.machine() != "x86_64":
              raise RuntimeError("mini-SWE-agent wheel lock requires Linux x86-64 and Python 3.12")
          subprocess.run([
              sys.executable, "-I", "-m", "pip", "install", "--quiet",
              "--isolated", "--disable-pip-version-check", "--only-binary=:all:",
              "--require-hashes", "--no-index", "--find-links", str(dependencies / "wheels"),
              "--target", str(dependencies),
              "--requirement", str(lock),
          ], check=True)
          sys.path.insert(0, str(dependencies))
          assert version("mini-swe-agent") == os.environ["GH_AW_ENGINE_VERSION"]

          import yaml
          from minisweagent.agents.default import DefaultAgent
          from minisweagent.config import builtin_config_dir
          from minisweagent.environments.local import LocalEnvironment
          from minisweagent.models.litellm_textbased_model import LitellmTextbasedModel

          config = yaml.safe_load((builtin_config_dir / "default.yaml").read_text())
          config["agent"]["step_limit"] = int(os.environ["GH_AW_MINI_STEP_LIMIT"])
          config["agent"]["cost_limit"] = 0
          trajectory = Path(os.environ["GH_AW_MINI_TRAJECTORY"])
          trajectory.parent.mkdir(parents=True, exist_ok=True)
          fd = os.open(trajectory, os.O_CREAT | os.O_WRONLY | os.O_TRUNC, 0o600)
          os.fchmod(fd, 0o600)
          os.close(fd)
          config["agent"]["output_path"] = trajectory
          model = LitellmTextbasedModel(
              **config["model"],
              model_name=os.environ["GH_AW_MINI_LITELLM_MODEL"],
              cost_tracking="ignore_errors",
          )
          model.config.model_kwargs.update(
              api_base=os.environ["GH_AW_MINI_API_BASE"],
              api_key="awf-proxy",
          )
          environment = LocalEnvironment(
              **config["environment"],
              cwd=os.environ["GH_AW_ENGINE_CWD"],
          )
          agent = DefaultAgent(model, environment, **config["agent"])
          result = agent.run(Path(os.environ["GH_AW_PROMPT"]).read_text())
          status = result.get("exit_status")
          print(f"mini-SWE-agent exit status: {status}", flush=True)
          if status != "Submitted":
              raise SystemExit(1)
    harness-script: |
      const { spawnSync } = require("child_process");
      const { constants } = require("os");
      const { resolve } = require("path");
      const { fetchAWFReflect, deriveBaseUrlFromModelsURL, normalizeReflectProviderName, REFLECT_PROVIDER_ALIASES } = require("./awf_reflect.cjs");

      const log = message => process.stderr.write(`[mini-swe-agent] ${message}\n`);
      const main = async () => {
        const [command, ...args] = process.argv.slice(2);
        if (!command) throw new Error("mini-SWE-agent command is required");
        if (process.env.AWF_REFLECT_ENABLED !== "1") {
          throw new Error("mini-SWE-agent requires the AWF sandbox and a configured LLM provider");
        }
        const selected = process.env.GH_AW_MINI_MODEL;
        const separator = selected?.indexOf("/") ?? -1;
        if (separator <= 0 || separator === selected.length - 1) {
          throw new Error("GH_AW_MINI_MODEL must use provider/model format");
        }
        const provider = process.env.GH_AW_LLM_PROVIDER;
        if (!["github", "openai", "anthropic"].includes(provider)) {
          throw new Error("mini-SWE-agent supports copilot, openai/codex, or anthropic models");
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
          throw new Error(`Unable to discover the mini-SWE-agent LLM endpoint: ${reflected.reason || "empty /reflect response"}`);
        }
        const aliases = REFLECT_PROVIDER_ALIASES[provider];
        const endpoint = reflected.reflectData.endpoints?.find(entry =>
          entry?.configured === true && aliases.has(normalizeReflectProviderName(entry.provider)) &&
          typeof entry.models_url === "string"
        );
        if (!endpoint) {
          throw new Error(`No configured mini-SWE-agent /reflect models endpoint for provider ${provider}`);
        }
        const model = selected.slice(separator + 1);
        let base = deriveBaseUrlFromModelsURL(endpoint.models_url);
        if (provider === "anthropic") base = base.replace(/\/+$/, "").replace(/\/v1$/, "");
        // CLI wrappers embed gateway credentials; only their nonsecret path/metadata bindings are inherited.
        const env = {};
        for (const name of [
          "PATH", "HOME", "LANG", "LC_ALL", "TERM", "TMPDIR", "RUNNER_TEMP",
          "GITHUB_WORKSPACE", "GITHUB_REPOSITORY", "GH_HOST", "GH_CONFIG_DIR",
          "NO_PROXY", "no_proxy",
          "GH_AW_MCP_DEFERRED_SERVERS", "GH_AW_SAFE_OUTPUTS_TOOLS_PATH",
          "ENGINE_CONFORMANCE_SENTINEL",
          "GH_AW_PROMPT", "GH_AW_ENGINE_VERSION", "GH_AW_MINI_DEPS", "GH_AW_MINI_DEPS_LOCK_SHA256",
          "GH_AW_MINI_TRAJECTORY",
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
          MSWEA_SILENT_STARTUP: "1",
          LITELLM_TELEMETRY: "false",
          LITELLM_LOCAL_MODEL_COST_MAP: "True",
          GH_AW_ENGINE_CWD: resolve(process.env.GITHUB_WORKSPACE, process.env.GH_AW_ENGINE_CWD || "."),
          GH_AW_MINI_LITELLM_MODEL: `${provider === "anthropic" ? "anthropic" : "openai"}/${model}`,
          GH_AW_MINI_API_BASE: base,
          GH_AW_MINI_STEP_LIMIT: limit,
        });
        const result = spawnSync(command, args, { env, cwd: env.GH_AW_ENGINE_CWD, stdio: "inherit" });
        if (result.error) throw result.error;
        if (result.status !== 0) {
          const error = new Error(`mini-SWE-agent execution failed: ${result.signal || `exit code ${result.status ?? "unknown"}`}`);
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
# mini-SWE-agent

This unsupported experimental sample pins `mini-swe-agent==2.4.6` from PyPI
and uses Python 3.12 on Linux x86-64. The imported definition embeds the complete
version-and-SHA256 wheel requirements lock; pip requires hashes and refuses
unlocked dependencies or version overrides that do not match the lock.
Trusted `pre-agent-steps` download only hash-verified binary wheels before AWF
starts. Inside AWF, pip re-verifies the lock and installs offline with
`--no-index --find-links` at the mounted path
`/tmp/gh-aw/agent/mini-swe-agent-deps`, not in the host user site. Isolated
Python ignores workspace imports and Python environment search paths; only
the explicit dependency directory is added. Bootstrap downloads do not grant
the agent access to PyPI; the engine defaults contain only inference transport.
The shared import's `runtimes.python` declaration explicitly opts into the Python
ecosystem, including `pypi.org` and `files.pythonhosted.org`, for agent execution.
For a registry-free agent, publish a copy of this import without `runtimes` and
provide Linux x86-64 / Python 3.12 on the runner independently. Keep its
`pre-agent-steps`: they are required even without the runtime declaration.
To deliberately restore agent package access, use `network.allowed: [python]`
or declare `runtimes.python` in the caller.
Import it and select a workflow-facing `provider/model`:

```yaml
engine:
  id: mini-swe-agent
model: copilot/claude-haiku-5.5
imports:
  - shared/mini-swe-agent.md
sandbox:
  agent:
    id: awf
```

Copilot uses `copilot-requests: write` or `COPILOT_GITHUB_TOKEN`; OpenAI/Codex
and Anthropic use their standard gh-aw provider secrets. AWF is required for
all providers. The harness discovers the matching endpoint through `/reflect`,
maps Copilot/OpenAI/Codex models to LiteLLM's `openai/<model>` and Anthropic
models to `anthropic/<model>`, and uses a placeholder key. Real provider
credentials are excluded from the agent environment.
The child environment uses an explicit allowlist of runtime, credential-free
AWF proxy URLs, bypass hosts, and CLI-proxy path/metadata bindings, not arbitrary
workflow environment variables or secrets.

The pinned Python API bypasses CLI first-run setup and interactive confirmations.
It reads the workflow prompt from a file, uses the packaged `default.yaml`
with the text-based LiteLLM model and local shell environment inside AWF, and
fails unless the agent explicitly submits with
`echo COMPLETE_TASK_AND_SUBMIT_FINAL_OUTPUT`. `max-turns` limits model calls,
defaulting to 20 when unset.
LiteLLM's best-effort cost estimates do not enforce a dollar budget; AWF
enforces the workflow inference budget.

There is no native MCP client: configured tools and safe outputs are available
through gh-aw's shell CLI proxies. Native shell command allowlists, disabling
file edits, Agent Plugins, repository-specific mini configuration, and structured
session parsing are not supported. The private native trajectory at
`/tmp/gh-aw/agent/mini-swe-agent.traj.json` is retained as untrusted diagnostic
data in the unified agent artifact, including after failed execution, only
after secret redaction and recovery-envelope validation succeed. Neither the
trajectory nor stdout is verified tool or token-accounting evidence.
Dependencies must have binary wheels; source
builds are deliberately disabled.

The workflow prompt remains executable agent instructions, not a data-only
payload. Treat issue, comment, and repository text as untrusted input: no prompt
preamble or opt-in flag makes shell execution safe. AWF network restrictions,
credential exclusion, inference budgets, and policy-controlled CLI proxies are
the enforcement boundaries. Use this shell-capable engine only for workflows
that intentionally allow local commands; unsupported command allowlists or
disabled file-editing policies must not be treated as enforced.
-->
