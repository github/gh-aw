---
private: true
emoji: "🦙"
description: Daily test of the Copilot BYOK endpoint using a local Ollama instance with a small model
on:
  schedule: daily on weekdays
max-daily-ai-credits: 10000
permissions:
  contents: read
  issues: read
name: Daily BYOK Ollama Test
engine:
  id: copilot
  bare: true
  env:
    COPILOT_PROVIDER_BASE_URL: "http://host.docker.internal:11434/v1"
    COPILOT_PROVIDER_API_KEY: "${{ env.OLLAMA_API_KEY }}"
    COPILOT_MODEL: "qwen2.5:0.5b"
strict: true
timeout-minutes: 20
steps:
  - name: Install Ollama
    env:
      OLLAMA_VERSION: "0.31.1"
      # SHA256 of install.sh from https://github.com/ollama/ollama/releases/download/v${OLLAMA_VERSION}/install.sh
      # To update: curl -fsSL https://github.com/ollama/ollama/releases/download/vNEW_VERSION/install.sh | sha256sum
      OLLAMA_INSTALL_SHA256: "25f64b810b947145095956533e1bdf56eacea2673c55a7e586be4515fc882c9f"
    run: |
      echo "Downloading Ollama v${OLLAMA_VERSION} install script..."
      mkdir -p /tmp/gh-aw
      curl -fsSL "https://github.com/ollama/ollama/releases/download/v${OLLAMA_VERSION}/install.sh" -o /tmp/gh-aw/ollama-install.sh
      echo "${OLLAMA_INSTALL_SHA256}  /tmp/gh-aw/ollama-install.sh" | sha256sum -c -
      # runner-guard:ignore RGS-018 -- version-pinned Ollama installer is downloaded to disk and SHA-256 verified before execution.
      bash /tmp/gh-aw/ollama-install.sh
  - name: Generate Ollama API key
    run: |
      OLLAMA_API_KEY="$(openssl rand -hex 16)"
      echo "OLLAMA_API_KEY=$OLLAMA_API_KEY" >> "$GITHUB_ENV"
  - name: Start Ollama service
    env:
      OLLAMA_HOST: "0.0.0.0:11434"
      OLLAMA_LOG: "/tmp/gh-aw/ollama-serve.log"
    # runner-guard:ignore RGS-012 -- loopback-only readiness probes to the Ollama service started in this step; no secrets are sent.
    run: |
      sudo systemctl stop ollama 2>/dev/null || true
      sleep 2
      mkdir -p /tmp/gh-aw
      # Detach the server from this step's stdio and log to a file instead.
      # The Actions runner closes the step's output pipe once the step ends, so
      # anything still writing to it afterwards -- including the model runner
      # subprocesses that `ollama serve` spawns on the first inference request --
      # dies with EPIPE. Logging to a file also keeps the server output around
      # for diagnostics in later steps.
      nohup ollama serve > "$OLLAMA_LOG" 2>&1 < /dev/null &
      echo "Waiting for Ollama service..."
      for _ in $(seq 1 30); do
        # runner-guard:ignore RGS-012 -- loopback-only readiness probe for the Ollama service started above; no secrets are sent.
        if curl -sf http://localhost:11434/api/version > /dev/null 2>&1; then
          echo "Ollama is ready"
          break
        fi
        sleep 1
      done
  - name: Pull small model
    run: |
      ollama pull qwen2.5:0.5b
  - name: Warm up model
    env:
      OLLAMA_MODEL: "qwen2.5:0.5b"
      OLLAMA_LOG: "/tmp/gh-aw/ollama-serve.log"
    # runner-guard:ignore RGS-012 -- loopback-only model warm-up request to the local Ollama service.
    run: |
      # Force Ollama to load the model into memory before the agent runs.
      # A cold model (not yet loaded) can cause the OpenAI-compatible /v1/chat/completions
      # endpoint to return 503 Service Unavailable on the agent's first requests, which
      # exhausts the Copilot CLI's built-in retry budget and fails the whole run.
      echo "Warming up model '$OLLAMA_MODEL'..."
      mkdir -p /tmp/gh-aw
      BODY_FILE=/tmp/gh-aw/ollama-warmup-response.json
      STATUS_FILE=/tmp/gh-aw/ollama-warmup-status.txt
      MAX_ATTEMPTS=6
      DELAY=3
      for attempt in $(seq 1 "$MAX_ATTEMPTS"); do
        : > "$BODY_FILE"
        CURL_EXIT=0
        # runner-guard:ignore RGS-012 -- loopback-only model warm-up request to the local Ollama service.
        curl -sS -o "$BODY_FILE" -w '%{http_code}' --max-time 60 \
          -H 'Content-Type: application/json' \
          http://localhost:11434/api/generate \
          -d "{\"model\":\"${OLLAMA_MODEL}\",\"prompt\":\"hi\",\"stream\":false}" \
          > "$STATUS_FILE" || CURL_EXIT=$?
        HTTP_STATUS="$(cat "$STATUS_FILE" 2>/dev/null)"
        if [ "$HTTP_STATUS" = "200" ]; then
          echo "Model warm-up succeeded on attempt ${attempt}"
          exit 0
        fi
        echo "Warm-up attempt ${attempt}/${MAX_ATTEMPTS} failed (HTTP ${HTTP_STATUS:-none}, curl exit ${CURL_EXIT})"
        echo "Response body: $(head -c 2000 "$BODY_FILE" 2>/dev/null)"
        sleep "$DELAY"
        DELAY=$((DELAY * 2))
        if [ "$DELAY" -gt 30 ]; then DELAY=30; fi
      done
      echo "::error::Model '$OLLAMA_MODEL' failed to warm up after ${MAX_ATTEMPTS} attempts."
      echo "--- ollama ps ---"
      ollama ps || true
      echo "--- ollama processes ---"
      pgrep -a ollama || echo "no ollama process is running"
      echo "--- last 200 lines of ${OLLAMA_LOG} ---"
      tail -n 200 "$OLLAMA_LOG" 2>/dev/null || echo "no server log available"
      exit 1
  - name: Verify Ollama BYOK readiness
    env:
      OLLAMA_MODEL: "qwen2.5:0.5b"
    # runner-guard:ignore RGS-012 -- loopback-only readiness probe for the local Ollama service; no secrets are sent.
    run: |
      echo "Checking Ollama model availability..."
      if ! ollama list | grep -Fq "$OLLAMA_MODEL"; then
        echo "::error::Required model '$OLLAMA_MODEL' is not available in Ollama."
        exit 1
      fi

      echo "Waiting for Ollama OpenAI-compatible endpoint..."
      MAX_WAIT_SECONDS=30
      for _ in $(seq 1 "$MAX_WAIT_SECONDS"); do
        # runner-guard:ignore RGS-012 -- loopback-only readiness probe for the local Ollama service; no secrets are sent.
        if curl -sf http://localhost:11434/v1/models > /dev/null 2>&1; then
          echo "Ollama /v1/models is ready"
          exit 0
        fi
        sleep 1
      done

      echo "::error::Ollama /v1/models did not become ready in ${MAX_WAIT_SECONDS}s."
      exit 1
  - name: Test Ollama chat completions outside AWF
    env:
      OLLAMA_MODEL: "qwen2.5:0.5b"
    # runner-guard:ignore RGS-012 -- loopback-only inference request to the local Ollama service.
    run: |
      RESPONSE_FILE=/tmp/gh-aw/ollama-chat-response.json
      # runner-guard:ignore RGS-012 -- loopback-only inference request to the local Ollama service.
      if ! HTTP_STATUS="$(curl -sS -o "$RESPONSE_FILE" -w '%{http_code}' --max-time 120 \
        -H 'Content-Type: application/json' \
        http://localhost:11434/v1/chat/completions \
        -d "{\"model\":\"${OLLAMA_MODEL}\",\"messages\":[{\"role\":\"user\",\"content\":\"Reply with one word confirming that inference works.\"}],\"stream\":false}")"; then
        echo "::error::Direct Ollama chat completion request failed."
        exit 1
      fi
      if [ "$HTTP_STATUS" != "200" ]; then
        echo "::error::Direct Ollama chat completion returned HTTP ${HTTP_STATUS}."
        echo "Response body: $(head -c 2000 "$RESPONSE_FILE" 2>/dev/null)"
        exit 1
      fi
      if ! jq -e '.choices[0].message.content | strings | length > 0' "$RESPONSE_FILE" > /dev/null; then
        echo "::error::Direct Ollama chat completion returned an invalid response."
        echo "Response body: $(head -c 2000 "$RESPONSE_FILE" 2>/dev/null)"
        exit 1
      fi
      echo "Direct Ollama chat completion succeeded outside AWF"
post-steps:
  - name: Capture Ollama diagnostics
    if: always()
    env:
      OLLAMA_LOG: "/tmp/gh-aw/ollama-serve.log"
    run: |
      echo "--- ollama ps ---"
      ollama ps || true
      echo "--- ollama processes ---"
      pgrep -a ollama || echo "no ollama process is running"
      echo "--- last 200 lines of ${OLLAMA_LOG} ---"
      tail -n 200 "$OLLAMA_LOG" 2>/dev/null || echo "no server log available"
network:
  allowed:
    - defaults
    - host.docker.internal
safe-outputs:
  create-issue:
    expires: 24h
    close-older-issues: true
    close-older-key: "daily-byok-ollama-test"
    labels: [automation, testing]
  messages:
    footer: "> 🦙 *BYOK test via [{workflow_name}]({run_url})*{ai_credits_suffix}"
    run-started: "🦙 BYOK Ollama test starting... [{workflow_name}]({run_url})"
    run-success: "✅ [{workflow_name}]({run_url}) — BYOK endpoint responded."
    run-failure: "❌ [{workflow_name}]({run_url}) — BYOK endpoint test failed: {status}"
features:
  gh-aw-detection: true
sandbox:
  agent:
    id: awf
models:
  default-ai-credits-pricing:
    input: 0.000001
    output: 0.000001
imports:
  - shared/reporting.md
---

### Daily BYOK Endpoint Test

You are a BYOK connectivity test. Your only task is to compose a haiku and report the result.

Write a haiku (5-7-5 syllable pattern) about code, automation, or workflows.

Then create an issue with:
- Title: `BYOK Ollama Test — ${{ github.run_id }}`
- Body:
  ```
  ### 🦙 Daily BYOK Ollama Test

  **Status:** ✅ PASS — Ollama responded via BYOK
  **Model:** qwen2.5:0.5b
  **Run:** ${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ github.run_id }}

  ### Haiku

  <your haiku here>
  ```