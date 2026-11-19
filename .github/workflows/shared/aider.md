---
runtimes:
  python:
    version: "3.12"
engine:
  id: aider
  detection-engine: copilot
  version: "0.86.2"
  display-name: Aider
  description: Aider AI pair programming CLI running in scripting (non-interactive) mode
  experimental: true
  mcp: false
  provider:
    name: github
  behaviors:
    secret-strategy: universal-llm-consumer
    installation:
      package-manager: pip
      package-name: aider-chat
      step-name: Install Aider Python packages
      docs-url: https://aider.chat/docs/install.html
      # Complete Linux x86-64 / CPython 3.12 wheel lock. Regenerate with
      # pip download --only-binary=:all: and sha256sum.
      requirements:
        - name: aider-chat
          version: "0.86.2"
          sha256: 64f6a0c66c9f4633ad9f479bca3e64ebcba02b9da03c6b604b74a44736b2416e
        - name: aiohappyeyeballs
          version: "2.6.1"
          sha256: f349ba8f4b75cb25c99c5c2d84e997e485204d2902a9597802b0371f09331fb8
        - name: aiohttp
          version: "3.13.3"
          sha256: 9ae8dd55c8e6c4257eae3a20fd2c8f41edaea5992ed67156642493b8daf3cecc
        - name: aiosignal
          version: "1.4.0"
          sha256: 053243f8b92b990551949e63930a839ff0cf0b0ebbe0597b0f3fb19e1a0fe82e
        - name: annotated_doc
          version: "0.0.4"
          sha256: 571ac1dc6991c450b25a9c2d84a3705e2ae7a53467b5d111c24fa8baabbed320
        - name: annotated_types
          version: "0.7.0"
          sha256: 1f02e8b43a8fbbc3f3e0d4f0f4bfc8131bcb4eebe8849b8e5c773f3a1c582a53
        - name: anyio
          version: "4.12.1"
          sha256: d405828884fc140aa80a3c667b8beed277f1dfedec42ba031bd6ac3db606ab6c
        - name: asgiref
          version: "3.11.1"
          sha256: e8667a091e69529631969fd45dc268fa79b99c92c5fcdda727757e52146ec133
        - name: attrs
          version: "25.4.0"
          sha256: adcf7e2a1fb3b36ac48d97835bb6d8ade15b8dcce26aba8bf1d14847b57a3373
        - name: backoff
          version: "2.2.1"
          sha256: 63579f9a0628e06278f7e47b7d7d5b6ce20dc65c5e96a6f3ca99a6adca0396e8
        - name: beautifulsoup4
          version: "4.14.3"
          sha256: 0918bfe44902e6ad8d57732ba310582e98da931428d231a5ecb9e7c703a735bb
        - name: certifi
          version: "2026.1.4"
          sha256: 9943707519e4add1115f44c2bc244f782c0249876bf51b6599fee1ffbedd685c
        - name: cffi
          version: "2.0.0"
          sha256: 3e17ed538242334bf70832644a32a7aae3d83b57567f9fd60a26257e992b79ba
        - name: charset_normalizer
          version: "3.4.4"
          sha256: 11d694519d7f29d6cd09f6ac70028dba10f92f6cdd059096db198c283794ac86
        - name: click
          version: "8.3.1"
          sha256: 981153a64e25f12d547d3426c367a4857371575ee7ad18df2a6183ab0545b2a6
        - name: configargparse
          version: "1.7.1"
          sha256: 8b586a31f9d873abd1ca527ffbe58863c99f36d896e2829779803125e83be4b6
        - name: diff_match_patch
          version: "20241021"
          sha256: 93cea333fb8b2bc0d181b0de5e16df50dd344ce64828226bda07728818936782
        - name: diskcache
          version: "5.6.3"
          sha256: 5e31b2d5fbad117cc363ebaf6b689474db18a1f6438bc82358b024abd4c2ca19
        - name: distro
          version: "1.9.0"
          sha256: 7bffd925d65168f85027d8da9af6bddab658135b840670a223589bc0c8ef02b2
        - name: fastapi
          version: "0.128.8"
          sha256: 5618f492d0fe973a778f8fec97723f598aa9deee495040a8d51aaf3cf123ecf1
        - name: fastuuid
          version: "0.14.0"
          sha256: 808527f2407f58a76c916d6aa15d58692a4a019fdf8d4c32ac7ff303b7d7af09
        - name: filelock
          version: "3.20.3"
          sha256: 4b0dda527ee31078689fc205ec4f1c1bf7d56cf88b6dc9426c4f230e46c2dce1
        - name: flake8
          version: "7.3.0"
          sha256: b9696257b9ce8beb888cdbe31cf885c90d31928fe202be0889a7cdafad32f01e
        - name: frozenlist
          version: "1.8.0"
          sha256: 494a5952b1c597ba44e0e78113a7266e656b9794eec897b19ead706bd7074383
        - name: fsspec
          version: "2026.2.0"
          sha256: 98de475b5cb3bd66bedd5c4679e87b4fdfe1a3bf4d707b151b3c07e58c9a2437
        - name: gitdb
          version: "4.0.12"
          sha256: 67073e15955400952c6565cc3e707c554a4eea2e428946f7a4c162fab9bd9bcf
        - name: gitpython
          version: "3.1.46"
          sha256: 79812ed143d9d25b6d176a10bb511de0f9c67b1fa641d82097b0ab90398a2058
        - name: grep_ast
          version: "0.9.0"
          sha256: a3973dca99f1abc026a01bbbc70e00a63860c8ff94a56182ff18b089836826d7
        - name: h11
          version: "0.16.0"
          sha256: 63cf8bbe7522de3bf65932fda1d9c2772064ffb3dae62d55932da54b31cb6c86
        - name: hf_xet
          version: "1.2.0"
          sha256: 3651fd5bfe0281951b988c0facbe726aa5e347b103a675f49a3fa8144c7968fd
        - name: httpcore
          version: "1.0.9"
          sha256: 2d400746a40668fc9dec9810239072b40b4484b640a8c38fd654a024c7a1bf55
        - name: httpx
          version: "0.28.1"
          sha256: d909fcccc110f8c7faf814ca82a9a4d816bc5a6dbfea25d6591d6985b8ba59ad
        - name: huggingface_hub
          version: "1.4.1"
          sha256: 9931d075fb7a79af5abc487106414ec5fba2c0ae86104c0c62fd6cae38873d18
        - name: idna
          version: "3.11"
          sha256: 771a87f49d9defaf64091e6e6fe9c18d4833f140bd19464795bc32d966ca37ea
        - name: importlib_metadata
          version: "7.2.1"
          sha256: ffef94b0b66046dd8ea2d619b701fe978d9264d38f3998bc4c27ec3b146a87c8
        - name: importlib_resources
          version: "6.5.2"
          sha256: 789cfdc3ed28c78b67a06acb8126751ced69a3d5f79c095a98298cd8a760ccec
        - name: jinja2
          version: "3.1.6"
          sha256: 85ece4451f492d0c13c5dd7c13a64681a86afae63a5f347908daf103ce6d2f67
        - name: jiter
          version: "0.13.0"
          sha256: bdaba7d87e66f26a2c45d8cbadcbfc4bf7884182317907baf39cfe9775bb4d93
        - name: json5
          version: "0.13.0"
          sha256: 9a08e1dd65f6a4d4c6fa82d216cf2477349ec2346a38fd70cc11d2557499fbcc
        - name: jsonschema
          version: "4.26.0"
          sha256: d489f15263b8d200f8387e64b4c3a75f06629559fb73deb8fdfb525f2dab50ce
        - name: jsonschema_specifications
          version: "2025.9.1"
          sha256: 98802fee3a11ee76ecaca44429fda8a41bff98b00a0f2838151b113f210cc6fe
        - name: litellm
          version: "1.81.10"
          sha256: 9efa1cbe61ac051f6500c267b173d988ff2d511c2eecf1c8f2ee546c0870747c
        - name: markdown_it_py
          version: "4.0.0"
          sha256: 87327c59b172c5011896038353a81343b6754500a08cd7a4973bb48c6d578147
        - name: markupsafe
          version: "3.0.3"
          sha256: d6dd0be5b5b189d31db7cda48b91d7e0a9795f31430b7f271219ab30f1d3ac9d
        - name: mccabe
          version: "0.7.0"
          sha256: 6c2d30ab6be0e4a46919781807b4f0d834ebdd6c6e3dca0bda5a15f863427b6e
        - name: mdurl
          version: "0.1.2"
          sha256: 84008a41e51615a49fc9966191ff91509e3c40b939176e643fd50a5c2196b8f8
        - name: mixpanel
          version: "5.0.0"
          sha256: 5c5fee75a304bd2940828a08a31f74fae7f02d5fe81914a851ed87e8884096ec
        - name: mslex
          version: "1.3.0"
          sha256: c7074b347201b3466fc077c5692fbce9b5f62a63a51f537a53fbbd02eff2eea4
        - name: multidict
          version: "6.7.1"
          sha256: bfde23ef6ed9db7eaee6c37dcec08524cb43903c60b285b172b6c094711b3961
        - name: networkx
          version: "3.4.2"
          sha256: df5d4365b724cf81b8c6a7312509d0c22386097011ad1abe274afd5e9d3bbc5f
        - name: numpy
          version: "1.26.4"
          sha256: 675d61ffbfa78604709862923189bad94014bef562cc35cf61d3a07bba02a7ed
        - name: openai
          version: "2.20.0"
          sha256: 38d989c4b1075cd1f76abc68364059d822327cf1a932531d429795f4fc18be99
        - name: orjson
          version: "3.11.7"
          sha256: e7745312efa9e11c17fbd3cb3097262d079da26930ae9ae7ba28fb738367cbad
        - name: oslex
          version: "0.1.3"
          sha256: 71acb8a1d42ed78ddd213a1d3a628bbf837f758bd2999c91df7ce57972466bdf
        - name: packaging
          version: "26.0"
          sha256: b36f1fef9334a5588b4166f8bcd26a14e521f2b55e6b9de3aaa80d3ff7a37529
        - name: pathspec
          version: "1.0.4"
          sha256: fb6ae2fd4e7c921a165808a552060e722767cfa526f99ca5156ed2ce45a5c723
        - name: pexpect
          version: "4.9.0"
          sha256: 7236d1e080e4936be2dc3e326cec0af72acf9212a7e1d060210e70a47e253523
        - name: pillow
          version: "12.1.1"
          sha256: 7aac39bcf8d4770d089588a2e1dd111cbaa42df5a94be3114222057d68336bd0
        - name: posthog
          version: "7.8.6"
          sha256: 21809f73e8e8f09d2bc273b09582f1a9f997b66f51fc626ef5bd3c5bdffd8bcd
        - name: prompt_toolkit
          version: "3.0.52"
          sha256: 9aac639a3bbd33284347de5ad8d68ecc044b91a762dc39b7c21095fcd6a19955
        - name: propcache
          version: "0.4.1"
          sha256: 15932ab57837c3368b024473a525e25d316d8353016e7cc0e5ba9eb343fbb1cf
        - name: psutil
          version: "7.2.2"
          sha256: 076a2d2f923fd4821644f5ba89f059523da90dc9014e85f8e45a5774ca5bc6f9
        - name: ptyprocess
          version: "0.7.0"
          sha256: 4b41f3967fce3af57cc7e94b888626c18bf37a083e3651ca8feeb66d492fef35
        - name: pycodestyle
          version: "2.14.0"
          sha256: dd6bf7cb4ee77f8e016f9c8e74a35ddd9f67e1d5fd4184d86c3b98e07099f42d
        - name: pycparser
          version: "3.0"
          sha256: b727414169a36b7d524c1c3e31839a521725078d7b2ff038656844266160a992
        - name: pydantic
          version: "2.12.5"
          sha256: e561593fccf61e8a20fc46dfc2dfe075b8be7d0188df33f221ad1f0139180f9d
        - name: pydantic_core
          version: "2.41.5"
          sha256: eceb81a8d74f9267ef4081e246ffd6d129da5d87e37a77c9bde550cb04870c1c
        - name: pydub
          version: "0.25.1"
          sha256: 65617e33033874b59d87db603aa1ed450633288aefead953b30bded59cb599a6
        - name: pyflakes
          version: "3.4.0"
          sha256: f742a7dbd0d9cb9ea41e9a24a918996e8170c799fa528688d40dd582c8265f4f
        - name: pygments
          version: "2.19.2"
          sha256: 86540386c03d588bb81d44bc3928634ff26449851e99741617ecb9037ee5ec0b
        - name: pypandoc
          version: "1.16.2"
          sha256: c200c1139c8e3247baf38d1e9279e85d9f162499d1999c6aa8418596558fe79b
        - name: pyperclip
          version: "1.11.0"
          sha256: 299403e9ff44581cb9ba2ffeed69c7aa96a008622ad0c46cb575ca75b5b84273
        - name: python_dateutil
          version: "2.9.0.post0"
          sha256: a8b2bc7bffae282281c8140a97d3aa9c14da0b136dfe83f850eea9a5f7470427
        - name: python_dotenv
          version: "1.2.1"
          sha256: b81ee9561e9ca4004139c6cbba3a238c32b03e4894671e181b671e8cb8425d61
        - name: pyyaml
          version: "6.0.3"
          sha256: ba1cc08a7ccde2d2ec775841541641e4548226580ab850948cbfda66a1befcdc
        - name: referencing
          version: "0.37.0"
          sha256: 381329a9f99628c9069361716891d34ad94af76e461dcb0335825aecc7692231
        - name: regex
          version: "2026.1.15"
          sha256: c32bef3e7aeee75746748643667668ef941d28b003bfc89994ecf09a10f7a1b5
        - name: requests
          version: "2.32.5"
          sha256: 2462f94637a34fd532264295e186976db0f5d453d1cdd31473c85a6a161affb6
        - name: rich
          version: "14.3.2"
          sha256: 08e67c3e90884651da3239ea668222d19bea7b589149d8014a21c633420dbb69
        - name: rpds_py
          version: "0.30.0"
          sha256: 47f236970bccb2233267d89173d3ad2703cd36a0e2a6e92d0560d333871a3d23
        - name: scipy
          version: "1.15.3"
          sha256: 271e3713e645149ea5ea3e97b57fdab61ce61333f97cfae392c28ba786f9bb49
        - name: shellingham
          version: "1.5.4"
          sha256: 7ecfff8f2fd72616f7481040475a65b2bf8af90a56c89140852d1120324e8686
        - name: shtab
          version: "1.8.0"
          sha256: f0922a82174b4007e06ac0bac4f79abd826c5cca88e201bfd927f889803c571d
        - name: six
          version: "1.17.0"
          sha256: 4721f391ed90541fddacab5acf947aa0d3dc7d27b2e1e8eda2be8970586c3274
        - name: smmap
          version: "5.0.2"
          sha256: b30115f0def7d7531d22a0fb6502488d879e75b260a9db4d0819cfb25403af5e
        - name: sniffio
          version: "1.3.1"
          sha256: 2f6da418d1f1e0fddd844478f41680e794e6051915791a034ff65e5f100525a2
        - name: socksio
          version: "1.0.0"
          sha256: 95dc1f15f9b34e8d7b16f06d74b8ccf48f609af32ab33c608d08761c5dcbb1f3
        - name: sounddevice
          version: "0.5.5"
          sha256: 30ff99f6c107f49d25ad16a45cacd8d91c25a1bcdd3e81a206b921a3a6405b1f
        - name: soundfile
          version: "0.13.1"
          sha256: 03267c4e493315294834a0870f31dbb3b28a95561b80b134f0bd3cf2d5f0e618
        - name: soupsieve
          version: "2.8.3"
          sha256: ed64f2ba4eebeab06cc4962affce381647455978ffc1e36bb79a545b91f45a95
        - name: starlette
          version: "0.52.1"
          sha256: 0029d43eb3d273bc4f83a08720b4912ea4b071087a3b48db01b7c839f7954d74
        - name: tiktoken
          version: "0.12.0"
          sha256: edde1ec917dfd21c1f2f8046b86348b0f54a2c0547f68149d8600859598769ad
        - name: tokenizers
          version: "0.22.2"
          sha256: 369cc9fc8cc10cb24143873a0d95438bb8ee257bb80c71989e3ee290e8d72c67
        - name: tqdm
          version: "4.67.3"
          sha256: ee1e4c0e59148062281c49d80b25b67771a127c85fc9676d3be5f243206826bf
        - name: tree_sitter
          version: "0.25.2"
          sha256: b43a9e4c89d4d0839de27cd4d6902d33396de700e9ff4c5ab7631f277a85ead9
        - name: tree_sitter_c_sharp
          version: "0.23.1"
          sha256: 035d259e64c41d02cc45afc3b8b46388b232e7d16d84734d851cca7334761da5
        - name: tree_sitter_embedded_template
          version: "0.25.0"
          sha256: a7c88c3dd8b94b3c9efe8ae071ff6b1b936a27ac5f6e651845c3b9631fa4c1c2
        - name: tree_sitter_language_pack
          version: "0.13.0"
          sha256: 0d4f261fce387ae040dae7e4d1c1aca63d84c88320afcc0961c123bec0be8377
        - name: tree_sitter_yaml
          version: "0.7.2"
          sha256: f1a5c60c98b6c4c037aae023569f020d0c489fad8dc26fdfd5510363c9c29a41
        - name: typer
          version: "0.23.0"
          sha256: 79f4bc262b6c37872091072a3cb7cb6d7d79ee98c0c658b4364bdcde3c42c913
        - name: typer_slim
          version: "0.23.0"
          sha256: 1d693daf22d998a7b1edab8413cdcb8af07254154ce3956c1664dc11b01e2f8b
        - name: typing_extensions
          version: "4.15.0"
          sha256: f0fa19c6845758ab08074a0cfa8b7aecb71c999ca73d62883bc25cc018c4e548
        - name: typing_inspection
          version: "0.4.2"
          sha256: 4ed1cacbdc298c220f1bd249ed5287caa16f34d44ef4e9c3d0cbad5b521545e7
        - name: urllib3
          version: "2.6.3"
          sha256: bf272323e553dfb2e87d9bfd225ca7b0f467b919d7bbd355436d3fd37cb0acd4
        - name: watchfiles
          version: "1.1.1"
          sha256: 1db5d7ae38ff20153d542460752ff397fcf5c96090c1230803713cf3147a6803
        - name: wcwidth
          version: "0.6.0"
          sha256: 1a3a1e510b553315f8e146c54764f4fb6264ffad731b3d78088cdb1478ffbdad
        - name: yarl
          version: "1.22.0"
          sha256: 50678a3b71c751d58d7908edc96d332af328839eea883bb554a43f539101277a
        - name: zipp
          version: "3.23.0"
          sha256: 071652d6115ed432f5ce1d34c336c0adfd6a884660d1e9712a256d3d3bd4b14e
    manifest:
      files:
        - .aider.conf.yml
        - CONVENTIONS.md
      path-prefixes:
        - .aider/
    network:
      defaults:
        - host.docker.internal
        - github.com
        - raw.githubusercontent.com
        - api.github.com
        - objects.githubusercontent.com
      provider-domains:
        copilot: api.githubcopilot.com
        anthropic: api.anthropic.com
        openai: api.openai.com
    execution:
      command-name: python3
      args:
        - -c
        - |
          import json
          import math
          import os
          import sys
          import time
          from pathlib import Path
          from uuid import uuid4

          sys.path.insert(0, os.environ["GH_AW_PYTHON_PACKAGES"])
          from aider import main as aider_main
          from aider.coders import base_coder
          from aider.io import InputOutput

          try:
              max_reflections = int(os.environ.get("GH_AW_AIDER_MAX_REFLECTIONS", "3"))
          except ValueError as error:
              raise ValueError("GH_AW_AIDER_MAX_REFLECTIONS must be an integer from 1 to 8") from error
          if not 1 <= max_reflections <= 8:
              raise ValueError("GH_AW_AIDER_MAX_REFLECTIONS must be an integer from 1 to 8")

          session_id = str(uuid4())
          started_at = time.monotonic()
          turns = 0
          errors = []
          response_usage = []
          observed_cost = None
          capturing_response = False
          command_results = []

          def emit(event_type, data):
              print("\n" + json.dumps({
                  "type": event_type,
                  "timestamp": int(time.time() * 1000),
                  "data": {"sourceEngine": "aider", "sessionId": session_id, **data},
              }), flush=True)

          def native_value(value):
              if hasattr(value, "model_dump"):
                  return value.model_dump()
              return value

          def field(value, name):
              if isinstance(value, dict):
                  return value.get(name)
              return getattr(value, name, None)

          def has_field(value, name):
              return name in value if isinstance(value, dict) else hasattr(value, name)

          def accounting(usage):
              mapped = {}
              for target, names in {
                  "input_tokens": ("prompt_tokens", "input_tokens"),
                  "output_tokens": ("completion_tokens", "output_tokens"),
                  "total_tokens": ("total_tokens",),
                  "cache_read_input_tokens": ("cache_read_input_tokens", "prompt_cache_hit_tokens"),
                  "cache_creation_input_tokens": ("cache_creation_input_tokens",),
              }.items():
                  for name in names:
                      if name in usage:
                          mapped[target] = usage[name]
                          break
              for target, details, name in (
                  ("cache_read_input_tokens", "prompt_tokens_details", "cached_tokens"),
                  ("reasoning_output_tokens", "completion_tokens_details", "reasoning_tokens"),
              ):
                  nested = usage.get(details)
                  if target not in mapped and isinstance(nested, dict) and name in nested:
                      mapped[target] = nested[name]
              if "prompt_tokens" in usage:
                  mapped["input_tokens_include_cache"] = not (
                      "cache_read_input_tokens" in usage or "cache_creation_input_tokens" in usage
                  )
              return mapped

          def total_usage():
              if not response_usage:
                  return {}
              result = {}
              for name in set.intersection(*(set(usage) for usage in response_usage)):
                  values = [usage[name] for usage in response_usage]
                  if name == "input_tokens_include_cache":
                      if all(value is values[0] for value in values):
                          result[name] = values[0]
                  elif all(type(value) is int and 0 <= value <= 9007199254740991 for value in values):
                      total = sum(values)
                      if total <= 9007199254740991:
                          result[name] = total
              return result

          class WorkflowInputOutput(InputOutput):
              def confirm_ask(self, question, *args, **kwargs):
                  if question in ("Run shell command?", "Run shell commands?"):
                      kwargs["explicit_yes_required"] = False
                  return super().confirm_ask(question, *args, **kwargs)

              def assistant_output(self, message, pretty=None):
                  global turns
                  if not capturing_response and message:
                      turns += 1
                      emit("assistant.message", {"content": message})
                  # Keep empty-response warnings without duplicating rendered provider text.
                  if not message:
                      return super().assistant_output(message, pretty)

              def tool_error(self, message="", strip=True):
                  content = native_value(message)
                  if not isinstance(content, (str, int, float, bool, list, dict, type(None))):
                      content = str(message)
                  errors.append(content)
                  emit("session.error", {"content": content})
                  return super().tool_error(message, strip)

          original_show_send_output = base_coder.Coder.show_send_output
          original_calculate_usage = base_coder.Coder.calculate_and_show_tokens_and_cost

          def workflow_show_send_output(coder, completion):
              global turns, capturing_response
              # Aider accumulates shell suggestions across reflections in run_one.
              # Only commands parsed from this response may run after its edits.
              coder.shell_commands = []
              choices = field(completion, "choices")
              usage = native_value(field(completion, "usage"))
              if isinstance(usage, dict):
                  response_usage.append(accounting(usage))
              else:
                  response_usage.append({})
              if choices:
                  turns += 1
              for choice in choices or []:
                  message = field(choice, "message")
                  if message is None:
                      continue
                  metadata = {}
                  for name in ("id", "model", "created"):
                      if has_field(completion, name):
                          metadata["apiCallId" if name == "id" else name] = field(completion, name)
                  metadata["message"] = native_value(message)
                  finish_reason = field(choice, "finish_reason")
                  if has_field(choice, "finish_reason"):
                      metadata["finishReason"] = finish_reason
                  if isinstance(usage, dict):
                      metadata["usage"] = usage
                  reasoning = field(message, "reasoning_content")
                  if reasoning is None:
                      reasoning = field(message, "reasoning")
                  if reasoning is None:
                      provider_reasoning = field(field(message, "provider_specific_fields"), "reasoning_text")
                      if isinstance(provider_reasoning, str):
                          reasoning = provider_reasoning
                  content = field(message, "content")
                  partial = {"partial": True} if finish_reason == "length" else {}
                  if reasoning is not None:
                      emit("assistant.reasoning", {**metadata, **partial, "content": reasoning})
                  refusal = field(message, "refusal")
                  if refusal is not None or finish_reason == "content_filter":
                      emit("assistant.refusal", {
                          **metadata,
                          "reason": "content_filter" if finish_reason == "content_filter" else "refusal",
                          **({"content": refusal if refusal is not None else content} if refusal is not None or has_field(message, "content") else {}),
                      })
                  elif has_field(message, "content"):
                      emit("assistant.message", {**metadata, **partial, "content": content})
              capturing_response = True
              try:
                  return original_show_send_output(coder, completion)
              finally:
                  capturing_response = False

          def workflow_calculate_usage(coder, messages, completion=None):
              global observed_cost
              result = original_calculate_usage(coder, messages, completion)
              if getattr(coder, "usage_report", None) and coder.main_model.info.get("input_cost_per_token"):
                  cost = getattr(coder, "total_cost", None)
                  if type(cost) in (int, float) and math.isfinite(cost) and cost >= 0:
                      observed_cost = cost
              return result

          original_run_cmd = base_coder.run_cmd
          original_run_shell_commands = base_coder.Coder.run_shell_commands
          original_get_chat_files_messages = base_coder.Coder.get_chat_files_messages

          def workflow_get_chat_files_messages(coder):
              coder.max_reflections = max_reflections
              messages = original_get_chat_files_messages(coder)
              for message in messages:
                  if message["role"] == "user" and message["content"] == coder.gpt_prompts.files_no_full_files:
                      message["content"] = (
                          "No editable files have been added yet. This is expected for this unattended workflow, "
                          "not a request to wait for files. Follow the workflow request: first run its repository "
                          "discovery commands in fenced bash blocks, then use their results to choose the next step. "
                          "You can create new files with SEARCH/REPLACE blocks with an empty SEARCH section. "
                          "Do not stop or ask the user to supply files merely because the initial file list is empty. "
                          "Emit the appropriate safeoutputs command only after evaluating the actual task results."
                      )
              if getattr(coder, "num_reflections", 0) == coder.max_reflections:
                  messages.append({
                      "role": "user",
                      "content": (
                          "This is your last available reflection. Do not repeat discovery or start more edits. "
                          "Execute the required safeoutputs command in a fenced bash block now. "
                          "If the task could not be completed, use safeoutputs report_incomplete --reason \"...\" "
                          "with the actual blocker. Do not claim success or use noop for unfinished work, "
                          "and do not repeat a safe output already recorded."
                      ),
                  })
              return messages

          def workflow_run_shell_commands(coder):
              start = len(command_results)
              shared_output = original_run_shell_commands(coder)
              results = command_results[start:]
              if (results or getattr(coder, "aider_edited_files", None)) and not coder.reflected_message:
                  coder.reflected_message = (
                      "Continue the workflow using the applied edits and shell results already in the conversation. "
                      "If the task is finished, stop; otherwise perform the next step, including "
                      "the required safeoutputs command. Do not repeat a recorded safe output.\n\n"
                      + "\n\n".join(results)
                  )
              return shared_output

          def workflow_run_cmd(command, *args, **kwargs):
              tool_id = str(uuid4())
              emit("tool.execution_start", {
                  "toolCallId": tool_id, "toolName": "bash", "input": {"command": command},
              })
              tool_started_at = time.monotonic()
              try:
                  exit_code, output = original_run_cmd(command, *args, **kwargs)
              except BaseException as error:
                  emit("tool.execution_complete", {
                      "toolCallId": tool_id, "toolName": "bash", "success": False,
                      "error": str(error), "status": "failed",
                      "durationMs": int((time.monotonic() - tool_started_at) * 1000),
                  })
                  raise
              command_results.append(f"Command: {command}\nExit code: {exit_code}")
              emit("tool.execution_complete", {
                  "toolCallId": tool_id, "toolName": "bash", "success": exit_code == 0,
                  "exitCode": exit_code, "output": output,
                  "durationMs": int((time.monotonic() - tool_started_at) * 1000),
              })
              return exit_code, output

          aider_main.InputOutput = WorkflowInputOutput
          base_coder.Coder.show_send_output = workflow_show_send_output
          base_coder.Coder.calculate_and_show_tokens_and_cost = workflow_calculate_usage
          base_coder.run_cmd = workflow_run_cmd
          base_coder.Coder.run_shell_commands = workflow_run_shell_commands
          base_coder.Coder.get_chat_files_messages = workflow_get_chat_files_messages
          emit("session.init", {"model": os.environ.get("AIDER_MODEL"), "cwd": os.getcwd()})
          emit("user.message", {
              "content": Path(os.environ["GH_AW_PROMPT"]).read_text(encoding="utf-8"),
          })
          terminal = {}
          try:
              exit_code = aider_main.main()
              if type(exit_code) is int:
                  terminal = {
                      "status": "completed" if exit_code == 0 else "failed",
                      "sourceType": "process.exit",
                      "exitCode": exit_code,
                  }
          except BaseException as error:
              errors.append(str(error))
              terminal = {"status": "failed", "sourceType": type(error).__name__}
              raise
          finally:
              usage = total_usage()
              emit("session.result", {
                  **terminal,
                  "numTurns": turns, "durationMs": int((time.monotonic() - started_at) * 1000),
                  "errors": errors,
                  **({"usage": usage} if usage else {}),
                  **({"totalCostUsd": observed_cost} if observed_cost is not None else {}),
              })
          sys.exit(exit_code)
        - --yes-always
        - --edit-format
        - diff
        - --no-auto-commits
        - --no-check-update
        - --no-show-release-notes
        - --no-detect-urls
        - --no-pretty
        - --no-stream
        - --no-fancy-input
        - --analytics-disable
      step-name: Execute Aider CLI
      model-env-var: AIDER_MODEL
      provider-env-mode: universal-llm-consumer
      write-timestamp: true
      env:
        AIDER_GIT: "false"
        AIDER_CHECK_UPDATE: "false"
        AIDER_ANALYTICS_DISABLE: "true"
    harness-script: |
      const { readFileSync } = require("fs");
      const { join } = require("path");
      const { homedir } = require("os");
      const { runProcess } = require("./process_runner.cjs");
      const { fetchAWFReflect, deriveBaseUrlFromModelsURL, normalizeReflectProviderName, REFLECT_PROVIDER_ALIASES } = require("./awf_reflect.cjs");

      const [command, ...commandArgs] = process.argv.slice(2);
      const log = message => process.stderr.write(`[aider-harness] ${message}\n`);

      const main = async () => {
        if (!command) throw new Error("Aider command is required");
        const selectedModel = process.env.AIDER_MODEL;
        const separator = selectedModel?.indexOf("/") ?? -1;
        if (separator <= 0 || separator === selectedModel.length - 1) {
          throw new Error("AIDER_MODEL must use provider/model format");
        }
        const provider = process.env.GH_AW_LLM_PROVIDER;
        if (!["github", "anthropic", "openai"].includes(provider)) {
          throw new Error("GH_AW_LLM_PROVIDER must be github, anthropic, or openai");
        }
        const isAnthropic = provider === "anthropic";
        let model = selectedModel.slice(separator + 1);
        if (provider === "github") {
          model = model.replace(/^(claude-(?:haiku|sonnet|opus)-\d+)-(\d+)$/, "$1.$2");
        }
        const promptFile = process.env.GH_AW_PROMPT;
        if (!promptFile) throw new Error("GH_AW_PROMPT is not set");
        readFileSync(promptFile, "utf8");

        let baseURL;
        let apiKey;
        if (process.env.AWF_REFLECT_ENABLED === "1") {
          const result = await fetchAWFReflect({ logger: log });
          if (!result.ok || !result.reflectData) {
            throw new Error("Unable to discover the Aider LLM endpoint from /reflect");
          }
          const aliases = REFLECT_PROVIDER_ALIASES[provider];
          const endpoint = result.reflectData.endpoints?.find(
            entry => entry?.configured === true && aliases.has(normalizeReflectProviderName(entry.provider))
          );
          if (!endpoint || typeof endpoint.models_url !== "string") {
            throw new Error(`No configured /reflect models endpoint found for provider ${provider}`);
          }
          baseURL = deriveBaseUrlFromModelsURL(endpoint.models_url);
          apiKey = "awf-proxy";
        } else {
          if (provider === "github") throw new Error("Aider Copilot routing requires the AWF sandbox");
          baseURL = isAnthropic ? process.env.ANTHROPIC_BASE_URL || "https://api.anthropic.com" : process.env.OPENAI_BASE_URL || "https://api.openai.com/v1";
          apiKey = isAnthropic ? process.env.ANTHROPIC_API_KEY : process.env.OPENAI_API_KEY || process.env.CODEX_API_KEY;
          if (!apiKey) throw new Error("Aider provider API key is required without AWF");
        }
        const localBin = join(homedir(), ".local", "bin");
        const env = {
          ...process.env,
          PATH: `${localBin}:${process.env.PATH || ""}`,
          AIDER_MODEL: `${isAnthropic ? "anthropic" : "openai"}/${model}`,
        };
        delete env.GITHUB_COPILOT_TOKEN;
        delete env.COPILOT_GITHUB_TOKEN;
        delete env.CODEX_API_KEY;
        if (isAnthropic) {
          // LiteLLM 1.81.10 appends /v1/messages to the Anthropic base URL.
          const anthropicBaseURL = baseURL.replace(/\/+$/, "").replace(/\/v1$/, "");
          env.ANTHROPIC_API_BASE = anthropicBaseURL;
          env.ANTHROPIC_BASE_URL = anthropicBaseURL;
          env.ANTHROPIC_API_KEY = apiKey;
          env.AIDER_ANTHROPIC_API_KEY = apiKey;
          delete env.OPENAI_API_KEY;
          delete env.AIDER_OPENAI_API_KEY;
          delete env.AIDER_OPENAI_API_BASE;
        } else {
          env.OPENAI_API_BASE = baseURL;
          env.OPENAI_BASE_URL = baseURL;
          env.AIDER_OPENAI_API_BASE = baseURL;
          env.OPENAI_API_KEY = apiKey;
          env.AIDER_OPENAI_API_KEY = apiKey;
          delete env.ANTHROPIC_API_KEY;
          delete env.AIDER_ANTHROPIC_API_KEY;
        }
        let reportedError = false;
        const observeLine = line => {
          if (/\blitellm\.\w*Error:/.test(line)) reportedError = true;
        };
        const result = await runProcess({
          command,
          args: [...commandArgs, "--message-file", promptFile],
          logArgs: ["(configured arguments)", "--message-file", "(prompt file)"],
          attempt: 0,
          log,
          env,
          maxCollectedOutputBytes: 1024 * 1024,
          onStdoutLine: observeLine,
          onStderrLine: observeLine,
        });
        if (result.exitCode !== 0) {
          process.exitCode = result.exitCode;
          throw new Error(`Aider execution failed with exit code ${result.exitCode}`);
        }
        if (reportedError) throw new Error("Aider execution reported a LiteLLM error");
      };

      main().catch(error => {
        log(error instanceof Error ? error.message : String(error));
        if (!process.exitCode) process.exitCode = 1;
      });
    log-parser: |
      function parseLog(logContent) {
        const { parseLogEntries, generateCopilotCliStyleSummary } = require("./log_parser_shared.cjs");
        const { isSessionEvent } = require("./agent_session.cjs");
        // Shell stdout can contain JSON; interpreted evidence requires Aider attribution.
        // Unknown native extensions stay opaque, including within core namespaces.
        const interpretedTypes = new Set([
          "session.init", "session.start", "session.info", "session.shutdown",
          "session.task_complete", "user.message",
          "assistant.message", "assistant.message_delta", "assistant.message_update",
          "assistant.reasoning", "assistant.reasoning_delta", "assistant.refusal",
          "skill.inventory", "skill.conflict", "skill.invoked", "skill.context_delivered_ref",
          "assistant.usage", "assistant.turn_end", "model.call_failure", "model.inventory",
          "tool.execution_start", "tool.execution_update", "tool.execution_complete",
          "session.result", "session.error", "agent.execution", "harness.decision",
          "harness.retry", "durable.restore", "durable.session",
          "harness.attempt", "engine.retry",
          "detection.result", "session.format",
          "session.collection", "session.collection_warning",
          "prompt.system", "prompt.user",
          "mcp.rpc.request", "mcp.rpc.response", "mcp.difc.filtered",
          "mcp.guard.blocked", "mcp.tool_call", "mcp.event", "mcp.configuration",
          "firewall.http_access", "firewall.token_usage", "firewall.model_routing",
          "firewall.upstream_error", "firewall.steering", "firewall.event", "model_routing.outcome",
          "model_endpoint.mismatch", "model.resolution", "model.config", "model.baseline_estimate",
          "safe_output.request", "safe_output.result", "safe_output.execution", "safe_output.error",
          "lifecycle.adoption",
          "experiment.state", "experiment.assignment", "grader.manifest", "grader.result",
          "eval.result", "github_api.rate_limit", "usage.report", "execution.result",
          "guardrail.admission", "guardrail.daily_aic", "guard.tool_denials_exceeded", "workflow.info", "workflow.job_result", "memory.push",
          "workflow.run_started", "workflow.run_updated", "workflow.run_settled",
          "dynamicWorkflows.task_started", "dynamicWorkflows.task_progress",
          "dynamicWorkflows.task_updated", "dynamicWorkflows.task_notification",
          "dynamicWorkflows.background_tasks_changed",
          "claude.assistant_error", "claude.api_retry", "turn.failed",
          "claude.stream_event", "claude.assistant_snapshot", "gemini.message_snapshot",
          "pi.message_snapshot",
        ]);
        const isInterpreted = entry => interpretedTypes.has(entry.type) ||
          entry.type.startsWith("subagent.") || entry.type.startsWith("otel.");
        const logEntries = (parseLogEntries(logContent) || []).filter(
          entry => isSessionEvent(entry) && (
            entry.data.sourceEngine === "aider" ||
            (entry.data.sourceEngine === undefined && !isInterpreted(entry))
          )
        );
        // Older Aider versions expose provider failures before any model reply.
        // Require the native startup preamble; unframed conversation/tool text
        // cannot establish diagnostic attribution.
        if (!logEntries.length) {
          const lines = logContent.split("\n");
          let banner = false;
          let startup = false;
          for (let index = 0; index < lines.length; index++) {
            const line = lines[index];
            if (/^Aider v\d+\.\d+\.\d+$/.test(line)) banner = true;
            if (banner && /^Repo-map: /.test(line)) {
              startup = true;
              continue;
            }
            if (!startup || !line.trim() || /^Retrying in \d+(?:\.\d+)? seconds\.\.\.$/.test(line)) continue;
            const diagnostic = line.match(/^litellm\.([A-Za-z][A-Za-z0-9]*Error): .+/);
            if (!diagnostic) {
              startup = false;
              continue;
            }
            let content = line + (index < lines.length - 1 ? "\n" : "");
            while (index + 1 < lines.length) {
              const next = lines[index + 1];
              if (!next.trim() || /^(?:Retrying in |litellm\.|```|~~~|>|[\[{"]|(?:assistant|user|tool|exec)(?::|\s*$)|Error:|Process exiting)/i.test(next)) break;
              content += next + (index + 1 < lines.length - 1 ? "\n" : "");
              index++;
            }
            logEntries.push({
              type: "session.error",
              data: { sourceEngine: "aider", errorType: diagnostic[1], content },
            });
          }
        }
        return {
          markdown: generateCopilotCliStyleSummary(logEntries),
          logEntries,
          mcpFailures: [],
          maxTurnsHit: false,
        };
      }
---

## Aider execution constraints

Aider receives the workflow with `--message-file`. The initial file list can be empty:
begin with the workflow's discovery commands, not a request for the user to attach files.
After shell commands, a bounded reflection loop lets you consume their results and continue.
Shell suggestions are scoped to each response: an edit-only reply never reruns a prior command.
After applied edits, continue with verification and reporting even if no shell command was suggested.
Plan discovery, edits, verification, and the terminal safe output within that limit:
The default is three reflections. Workflows requiring separate validation phases can
set `env.GH_AW_AIDER_MAX_REFLECTIONS` to an integer from 1 to 8; invalid values fail startup.

- **Edit files with *SEARCH/REPLACE* blocks.** Aider applies them for you, including for new
  files (empty `SEARCH` section). Put the repository-relative filename on its own line
  immediately before the opening code fence. Use exactly `<<<<<<< SEARCH`, `=======`,
  and `>>>>>>> REPLACE` on separate lines inside that fence. Never nest code fences.
  Do not write source files with `cat`/heredocs.
- **Put shell commands in ```bash blocks, one complete command per line.** Aider executes each
  line separately, so multi-line commands, backslash continuations and heredocs do not work.
  Chain steps with `&&` or `;` on a single line instead.
- **Suggest at most a few commands**; they all run from the repository root.
- **Emit safe outputs through the `safeoutputs` MCP CLI**, for example
  `safeoutputs noop --message "..."`. Every `safeoutputs` command must be inside
  a ```bash block. Commands in prose or inline code are not executed.
- **Reserve the final reflection for completion reporting.** If edits or verification
  remain unfinished, call `safeoutputs report_incomplete --reason "..."` with the blocker.
  Never use `noop` to represent failed or incomplete work.

<!--
# Aider CLI

Unsupported sample engine definition for [Aider](https://github.com/Aider-AI/aider), the
open-source AI pair programming CLI ([docs](https://aider.chat/docs/)).
Import this file and set `engine: id: aider` to use it:

```yaml
engine:
  id: aider
model: copilot/auto
imports:
  - shared/aider.md
```

`model` must use `provider/model` format. Supported providers are `copilot`,
`anthropic`, `openai`, and `codex` (an alias for `openai`). The sample pins
`aider-chat==0.86.2`, the latest stable PyPI package (GitHub's latest release
entry still names 0.86.0).
Requests use the selected provider's configured AWF `/reflect` endpoint,
preserving its API path prefix. Copilot and OpenAI use Aider's
`openai/<model>` LiteLLM form; Anthropic uses native `anthropic/<model>` requests.
Endpoint and authentication settings are supplied through environment variables,
without overwriting repository `.aider.conf.yml` settings.
Copilot Claude aliases such as `claude-sonnet-4-5` are normalized to the dotted
model IDs exposed by the proxy, such as `claude-sonnet-4.5`.
Without AWF, OpenAI and Anthropic use their provider API key and optional
`OPENAI_BASE_URL` or `ANTHROPIC_BASE_URL`. Copilot requires AWF.

Aider runs in scripting mode: the generated prompt file is passed with
`--message-file`. A small Python entrypoint subclasses `InputOutput` so
`--yes-always` also accepts suggested shell commands, which stock Aider
deliberately declines when explicit confirmation is required. The adapter
changes only the two shell-command confirmation prompts; other explicit
confirmation requirements remain intact. Commands run in the configured
workflow sandbox.
After suggested commands execute, the entrypoint uses Aider's native reflection
loop to let the model consume their output and exit codes before deciding the
next step. Empty output and nonzero command exits also trigger continuation.
When Aider would insert its native "no files shared" chat message, the entrypoint
instead explains that an empty initial file list is expected and directs the
model to the workflow's discovery commands. Existing editable-file content and
repository-map messages are unchanged. This removes an interactive file-supply
assumption, not the requirement to inspect results before editing or emitting
a safe output; it cannot guarantee model compliance.
The default three-reflection limit is unchanged unless the workflow explicitly
sets `GH_AW_AIDER_MAX_REFLECTIONS` (1 to 8). Exhausting the configured limit without a
terminal safe output remains an incomplete task. Only the safe-output gateway,
not shell text or the entrypoint, establishes that a terminal output was recorded.
The entrypoint emits canonical session events for exact provider reply text,
separate reasoning and structured refusals, shell commands and their results,
errors, and session boundaries. Plaintext reasoning falls back from
`reasoning_content` and `reasoning` to LiteLLM's
`provider_specific_fields.reasoning_text`; explicitly empty strings are retained.
Opaque provider reasoning is never promoted to plaintext reasoning events.
Provider token counts and observed Aider cost
are captured before display rounding; missing accounting remains absent, and
session totals are emitted once. Length-limited replies retain `partial: true`.
Repository configuration that enables streaming instead of this profile's
`--no-stream` is outside this response-hook coverage. Historical logs with only
rounded `Tokens:`/`Cost:` prose cannot recover exact accounting, and older runs
without structured events cannot reconstruct conversations. Before-response
LiteLLM diagnostics are retained as `session.error` only when the native Aider
startup preamble establishes their origin; quoted, tool and conversation text
does not establish attribution. These diagnostics do not imply a session result.
The declarative log parser publishes
normalized agent logs used by Actions summaries and the unified session artifact;
local log reconstruction also recognizes these events.
The edit format is pinned to `diff` (the editblock coder) because the proxied
model names are unknown to Aider and would otherwise fall back to the `whole`
format, which rejects ```bash blocks and cannot run shell commands.
Aider reports some LiteLLM request failures with exit code 0, so the harness
also detects those errors in its output and fails the workflow.
Output is forwarded as it arrives, without Node's synchronous output-buffer
limit, and child exit codes and termination signals are preserved.
Aider has no MCP client, so the compiler exposes MCP-backed tools through
`cli-proxy` and GitHub access through `gh-proxy`. Both proxies are enabled
automatically and cannot be disabled for this engine.
-->
