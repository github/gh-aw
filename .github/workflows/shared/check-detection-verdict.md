---
jobs:
  check_detection_verdict:
    runs-on: ubuntu-latest
    needs: [agent, detection]
    if: always() && needs.agent.result == 'success'
    permissions: {}
    steps:
      - name: Require a recorded verdict from the sandbox
        env:
          RUN_DETECTION: ${{ needs.detection.outputs.run_detection }}
          CONCLUSION: ${{ needs.detection.outputs.detection_conclusion }}
          SUCCESS: ${{ needs.detection.outputs.detection_success }}
        run: |
          if [[ "$RUN_DETECTION" != true || "$CONCLUSION" != success || "$SUCCESS" != true ]]; then
            echo "::error::The smoke run did not produce a clean verdict accepted by the host."
            exit 1
          fi
---
