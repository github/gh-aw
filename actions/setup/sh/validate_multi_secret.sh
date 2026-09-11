#!/bin/bash
set +o histexpand

set -e

# validate_multi_secret.sh - Validate that at least one secret from a list is configured
#
# Usage: validate_multi_secret.sh SECRET_NAME1 [SECRET_NAME2 ...] ENGINE_NAME DOCS_URL
#
# Arguments:
#   SECRET_NAME1, SECRET_NAME2, ... : Environment variable names to check (at least one required)
#   ENGINE_NAME                     : Name of the engine requiring the secrets
#   DOCS_URL                        : Documentation URL for secret configuration
#
# Environment:
#   The script expects the secret values to be available as environment variables
#
# Exit codes:
#   0 - At least one secret is configured
#   1 - All secrets are empty or not set

# Parse arguments
if [ "$#" -lt 3 ]; then
  echo "Usage: $0 SECRET_NAME1 [SECRET_NAME2 ...] ENGINE_NAME DOCS_URL" >&2
  exit 1
fi

# Extract docs URL (last argument)
DOCS_URL="${!#}"

# Extract engine name (second to last argument)
ENGINE_NAME="${@: -2:1}"

# Remaining arguments are secret names (all except last two)
ARGS=("$@")
SECRET_NAMES=("${ARGS[@]:0:$#-2}")

if [ "${#SECRET_NAMES[@]}" -eq 0 ]; then
  echo "Error: At least one secret name is required" >&2
  exit 1
fi

is_invalid_secret_value() {
  local value="$1"
  if [[ "$value" =~ ^[[:space:]]*$ ]]; then
    INVALID_SECRET_REASON="whitespace-only"
    return 0
  fi
  if [[ "$value" =~ ^[[:space:]]*(null|undefined)[[:space:]]*$ ]]; then
    INVALID_SECRET_REASON="placeholder value"
    return 0
  fi
  return 1
}

format_present_secret() {
  local secret_name="$1"
  local secret_value="$2"
  local detail="${3:-}"
  if [ -n "$detail" ]; then
    echo "$secret_name: present (length=${#secret_value}; $detail)"
  else
    echo "$secret_name: present (length=${#secret_value})"
  fi
}

# Reject configured secrets with values that are known placeholder artifacts.
for secret_name in "${SECRET_NAMES[@]}"; do
  # Use indirect expansion to get the value of the variable named by secret_name
  secret_value="${!secret_name}"
  if [ -n "$secret_value" ]; then
    if is_invalid_secret_value "$secret_value"; then
      error_msg="$secret_name secret has an invalid value: $INVALID_SECRET_REASON (length=${#secret_value})"
      {
        echo "Error: $error_msg"
        echo ""
        echo "Regenerate or reconfigure the secret required by $ENGINE_NAME."
        echo ""
        echo "Documentation: $DOCS_URL"
      } >> "$GITHUB_STEP_SUMMARY"

      echo "Error: $error_msg" >&2
      echo "Regenerate or reconfigure the secret required by $ENGINE_NAME." >&2
      echo "" >&2
      echo "Documentation: $DOCS_URL" >&2

      if [ -n "$GITHUB_OUTPUT" ]; then
        echo "verification_result=failed" >> "$GITHUB_OUTPUT"
      fi

      exit 1
    fi
  fi
done

# Check if all secrets are empty
all_empty=true
for secret_name in "${SECRET_NAMES[@]}"; do
  # Use indirect expansion to get the value of the variable named by secret_name
  secret_value="${!secret_name}"
  if [ -n "$secret_value" ]; then
    all_empty=false
    break
  fi
done

# If all secrets are empty, print error and exit
if [ "$all_empty" = true ]; then
  # Build error message
  if [ "${#SECRET_NAMES[@]}" -eq 2 ]; then
    error_msg="Neither ${SECRET_NAMES[0]} nor ${SECRET_NAMES[1]} secret is set"
  else
    # Join secret names with ", "
    secret_list=$(IFS=", "; echo "${SECRET_NAMES[*]}")
    error_msg="None of the following secrets are set: $secret_list"
  fi
  
  # Build requirement message
  # Join secret names with " or "
  secret_or_list=$(IFS=" or "; echo "${SECRET_NAMES[*]}")
  requirement_msg="$secret_or_list is required by $ENGINE_NAME."
  
  # Print to GitHub step summary with troubleshooting tips
  {
    echo "Error: $error_msg"
    echo ""
    echo "$requirement_msg"
    echo ""
    echo "**How to fix:**"
    echo "1. Go to your repository Settings → Secrets and variables → Actions"
    echo "2. Add a new repository secret with one of the required names"
    echo ""
    echo "**Common causes if you believe the secret is already configured:**"
    echo "- **Organization secrets** must have repository access granted (Settings → Secrets → Repository access)"
    echo "- **Environment secrets** are only available if the job specifies that environment"
    echo "- **Secret name mismatch** - verify the exact spelling (case-sensitive)"
    echo ""
    echo "Documentation: $DOCS_URL"
  } >> "$GITHUB_STEP_SUMMARY"
  
  # Print to stderr
  echo "Error: $error_msg" >&2
  echo "$requirement_msg" >&2
  echo "" >&2
  echo "Common causes if the secret appears to be configured:" >&2
  echo "  - Organization secrets must have repository access granted" >&2
  echo "  - Environment secrets require the job to specify that environment" >&2
  echo "  - Secret names are case-sensitive - verify exact spelling" >&2
  echo "" >&2
  echo "Documentation: $DOCS_URL" >&2
  
  # Set step output to indicate verification failed
  if [ -n "$GITHUB_OUTPUT" ]; then
    echo "verification_result=failed" >> "$GITHUB_OUTPUT"
  fi
  
  exit 1
fi

# Validate COPILOT_GITHUB_TOKEN is a fine-grained PAT if it's one of the secrets being validated
for secret_name in "${SECRET_NAMES[@]}"; do
  if [ "$secret_name" = "COPILOT_GITHUB_TOKEN" ]; then
    secret_value="${!secret_name}"
    if [ -n "$secret_value" ]; then
      # Check token type by prefix
      # github_pat_ = Fine-grained PAT (valid)
      # ghp_ = Classic PAT (invalid)
      # gho_ = OAuth token (invalid)
      if [[ "$secret_value" == ghp_* ]]; then
        {
          echo "❌ Error: COPILOT_GITHUB_TOKEN is a classic Personal Access Token (ghp_...)"
          echo "Classic PATs are not supported for GitHub Copilot."
          echo "Please create a fine-grained PAT (github_pat_...) at:"
          echo "https://github.com/settings/personal-access-tokens/new"
          echo ""
          echo "Configure the token with:"
          echo "• Resource owner: Your personal account"
          echo "• Repository access: \"Public repositories\""
          echo "• Account permissions → Copilot Requests: Read-only"
        } >> "$GITHUB_STEP_SUMMARY"
        
        echo "Error: COPILOT_GITHUB_TOKEN is a classic Personal Access Token (ghp_...)" >&2
        echo "Classic PATs are not supported for GitHub Copilot." >&2
        echo "Please create a fine-grained PAT (github_pat_...) at: https://github.com/settings/personal-access-tokens/new" >&2
        
        if [ -n "$GITHUB_OUTPUT" ]; then
          echo "verification_result=failed" >> "$GITHUB_OUTPUT"
        fi
        exit 1
      elif [[ "$secret_value" == gho_* ]]; then
        {
          echo "❌ Error: COPILOT_GITHUB_TOKEN is an OAuth token (gho_...)"
          echo "OAuth tokens are not supported for GitHub Copilot."
          echo "Please create a fine-grained PAT (github_pat_...) at:"
          echo "https://github.com/settings/personal-access-tokens/new"
        } >> "$GITHUB_STEP_SUMMARY"
        
        echo "Error: COPILOT_GITHUB_TOKEN is an OAuth token (gho_...)" >&2
        echo "OAuth tokens are not supported for GitHub Copilot." >&2
        echo "Please create a fine-grained PAT (github_pat_...) at: https://github.com/settings/personal-access-tokens/new" >&2
        
        if [ -n "$GITHUB_OUTPUT" ]; then
          echo "verification_result=failed" >> "$GITHUB_OUTPUT"
        fi
        exit 1
      elif [[ "$secret_value" != github_pat_* ]]; then
        {
          echo "❌ Error: COPILOT_GITHUB_TOKEN has an unrecognized format"
          echo "GitHub Copilot requires a fine-grained PAT (starting with 'github_pat_')."
          echo "Please create a fine-grained PAT at:"
          echo "https://github.com/settings/personal-access-tokens/new"
        } >> "$GITHUB_STEP_SUMMARY"
        
        echo "Error: COPILOT_GITHUB_TOKEN has an unrecognized format" >&2
        echo "GitHub Copilot requires a fine-grained PAT (starting with 'github_pat_')." >&2
        echo "Please create a fine-grained PAT at: https://github.com/settings/personal-access-tokens/new" >&2
        
        if [ -n "$GITHUB_OUTPUT" ]; then
          echo "verification_result=failed" >> "$GITHUB_OUTPUT"
        fi
        exit 1
      fi
    fi
    break
  fi
done

# Log success in collapsible section
echo "<details>"
echo "<summary>Agent Environment Validation</summary>"
echo ""

# Build if/elif/else chain to match original behavior
# First secret uses if
first_secret="${SECRET_NAMES[0]}"
first_value="${!first_secret}"
if [ -n "$first_value" ]; then
  # Show extra info for COPILOT_GITHUB_TOKEN indicating fine-grained PAT
  if [ "$first_secret" = "COPILOT_GITHUB_TOKEN" ]; then
    echo "✅ $(format_present_secret "$first_secret" "$first_value" "fine-grained PAT")"
  else
    echo "✅ $(format_present_secret "$first_secret" "$first_value")"
  fi
# Middle secrets use elif (if there are more than 2 secrets)
elif [ "${#SECRET_NAMES[@]}" -gt 2 ]; then
  found=false
  for ((i=1; i<${#SECRET_NAMES[@]}-1; i++)); do
    secret_name="${SECRET_NAMES[$i]}"
    secret_value="${!secret_name}"
    if [ -n "$secret_value" ]; then
      echo "✅ $(format_present_secret "$secret_name" "$secret_value")"
      found=true
      break
    fi
  done
  # Last secret uses else
  if [ "$found" = false ]; then
    last_secret="${SECRET_NAMES[-1]}"
    last_value="${!last_secret}"
    if [ -n "$last_value" ]; then
      echo "✅ $(format_present_secret "$last_secret" "$last_value")"
    fi
  fi
# Last secret uses else (for 2 secret case)
else
  last_secret="${SECRET_NAMES[-1]}"
  last_value="${!last_secret}"
  if [ "${#SECRET_NAMES[@]}" -eq 2 ]; then
    echo "✅ $(format_present_secret "$last_secret" "$last_value" "using as fallback for ${SECRET_NAMES[0]}")"
  else
    echo "✅ $(format_present_secret "$last_secret" "$last_value")"
  fi
fi

echo "</details>"

# Set step output to indicate verification succeeded
if [ -n "$GITHUB_OUTPUT" ]; then
  echo "verification_result=success" >> "$GITHUB_OUTPUT"
fi
