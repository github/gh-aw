#!/usr/bin/env bash
set +o histexpand
set -euo pipefail

if [[ "$GH_AW_MODEL_AGENT_OVERRIDE" == "true" ]]; then
  echo "Model routing disabled: vars.${GH_AW_MODEL_AGENT_VARIABLE} selects the model"
elif [[ "$GH_AW_DEFAULT_MODEL_OVERRIDE" == "true" ]]; then
  echo "Model routing disabled: vars.${GH_AW_DEFAULT_MODEL_VARIABLE} selects the model"
else
  echo "Model routing enabled: goal ${GH_AW_MODEL_ROUTING_GOAL}, mode ${GH_AW_MODEL_ROUTING_MODE}"
fi

printf 'model-routing=%s\n' "$GH_AW_MODEL_ROUTING" >> "$GITHUB_OUTPUT"
