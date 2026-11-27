---
jobs:
  check_detection_status:
    runs-on: ubuntu-latest
    needs: [agent, detection, check_detection_verdict]
    detection-gate: status-only
    if: always() && needs.agent.result == 'success'
    permissions: {}
    steps:
      - name: Require successful detection and verdict checks
        env:
          DETECTION_RESULT: ${{ needs.detection.result }}
          VERDICT_RESULT: ${{ needs.check_detection_verdict.result }}
        run: |
          if [[ "$DETECTION_RESULT" != success || "$VERDICT_RESULT" != success ]]; then
            echo "::error::The smoke run did not complete threat detection and its verdict check successfully."
            exit 1
          fi
  check_detection_verdict:
    runs-on: ubuntu-latest
    needs: [agent, detection]
    if: always() && needs.agent.result == 'success' && needs.detection.result == 'success'
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
