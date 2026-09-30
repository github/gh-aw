---
import-schema:
  session-artifact:
    type: choice
    required: true
    options:
      - agent-stdio
      - copilot-events
post-steps:
  - name: Verify agent session artifact
    if: always()
    env:
      GH_AW_SESSION_ARTIFACT: ${{ github.aw.import-inputs.session-artifact }}
    run: |
      set -euo pipefail
      roots=(/tmp/gh-aw "${RUNNER_TEMP:-/tmp}/gh-aw")

      if [ "$GH_AW_SESSION_ARTIFACT" = "copilot-events" ]; then
        for root in "${roots[@]}"; do
          session_dir="$root/sandbox/agent/logs/copilot-session-state"
          if [ -d "$session_dir" ]; then
            session_file="$(find "$session_dir" -type f -name events.jsonl -size +0c -print -quit)"
            if [ -n "$session_file" ]; then
              echo "Found non-empty Copilot session events: $session_file"
              exit 0
            fi
          fi
        done
        echo "::error::No non-empty Copilot session events.jsonl found in the agent logs"
        exit 1
      fi

      for root in "${roots[@]}"; do
        session_file="$root/agent-stdio.log"
        if [ -s "$session_file" ]; then
          echo "Found non-empty agent session transcript: $session_file"
          exit 0
        fi
      done

      echo "::error::No non-empty agent-stdio.log found in the agent logs"
      exit 1
---
