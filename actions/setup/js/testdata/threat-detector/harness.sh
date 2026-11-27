#!/bin/sh
# Deterministic stand-in for the model. The reporting tool and its supervisor are real.
set -eu
cat > prompt-seen
printf '1\n' >> attempts
report() {
  threat_detection_result --prompt-injection=false --secret-leak="$1" --malicious-patch="$2" --reason 'private fixture reason'
}
case "$SCENARIO" in
  clean|strict-warning) threat_detection_result --prompt-injection=false --secret-leak=false --malicious-patch=false ;;
  correct)
    if report true true >feedback 2>&1; then exit 9; fi
    grep -q -- '--malicious-patch=true is not eligible' feedback
    test ! -f "$THREAT_DETECTION_RESULT_FILE"
    report true false ;;
  absent|incorrect|retry)
    if [ "$SCENARIO" = retry ] && grep -q 'previous verdict was rejected as structurally ineligible' prompt-seen; then
      report true false
    else
      # Bypass the advisory check, but use the real tool to write the result.
      if [ "$SCENARIO" = absent ]; then
        unset THREAT_DETECTION_ELIGIBLE_MALICIOUS_PATCH
      else
        export THREAT_DETECTION_ELIGIBLE_MALICIOUS_PATCH=true
      fi
      report true true
    fi ;;
  timeout) sleep 10 ;;
  recorded-at-deadline)
    # The short deadline fires before the normal result watcher polls.
    printf '%s' '{"prompt_injection":false,"secret_leak":true,"malicious_patch":false,"reasons":["private fixture reason"]}' > "$THREAT_DETECTION_RESULT_FILE"
    sleep 10 ;;
  failed) exit 3 ;;
esac
