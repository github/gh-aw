#!/usr/bin/env bash
set +o histexpand
set -euo pipefail

# Dynamic routing: the main agent's model is an expression, resolved here once and
# published for later jobs. A router value turns routing on with its mode; any other
# value, including an empty one, keeps the non-routed path.
if [[ -n "${GH_AW_MODEL_SELECTOR+set}" ]]; then
  selector="$GH_AW_MODEL_SELECTOR"
  if [[ "$selector" == *$'\n'* || "$selector" == *$'\r'* ]]; then
    echo "ERR_CONFIG: the workflow's model resolved to a value containing a line break." >&2
    exit 1
  fi
  selector="${selector#"${selector%%[![:space:]]*}"}"
  selector="${selector%"${selector##*[![:space:]]}"}"
  mode=""
  case "$selector" in
    router) mode="auto" ;;
    router/economy) mode="economy" ;;
    router/balanced) mode="balanced" ;;
    router/robust) mode="robust" ;;
  esac
  base="${selector%%\?*}"
  base="${base,,}"
  if [[ -z "$mode" && ( "$base" == "router" || "$base" == router/* ) ]]; then
    echo "ERR_CONFIG: the workflow's model resolved to an unsupported router value. Use router, router/economy, router/balanced, or router/robust exactly, without parameters." >&2
    exit 1
  fi
  routing=false
  if [[ -n "$mode" ]]; then
    routing=true
    echo "Model routing enabled: the workflow's model resolved to ${selector} (goal ${GH_AW_MODEL_ROUTING_GOAL}, mode ${mode})"
  else
    echo "Model routing disabled: the workflow's model is not a router value"
  fi
  printf 'model-routing=%s\nmodel-routing-mode=%s\nmodel-selector=%s\n' "$routing" "$mode" "$selector" >> "$GITHUB_OUTPUT"
  exit 0
fi

if [[ "$GH_AW_MODEL_AGENT_OVERRIDE" == "true" ]]; then
  echo "Model routing disabled: vars.${GH_AW_MODEL_AGENT_VARIABLE} selects the model"
elif [[ "$GH_AW_DEFAULT_MODEL_OVERRIDE" == "true" ]]; then
  echo "Model routing disabled: vars.${GH_AW_DEFAULT_MODEL_VARIABLE} selects the model"
else
  echo "Model routing enabled: goal ${GH_AW_MODEL_ROUTING_GOAL}, mode ${GH_AW_MODEL_ROUTING_MODE}"
fi

printf 'model-routing=%s\n' "$GH_AW_MODEL_ROUTING" >> "$GITHUB_OUTPUT"
