#!/bin/bash
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
  requirement_msg="The $ENGINE_NAME engine requires either $secret_or_list secret to be configured."
  
  # Print to GitHub step summary
  {
    echo "❌ Error: $error_msg"
    echo "$requirement_msg"
    echo "Please configure one of these secrets in your repository settings."
    echo "Documentation: ${DOCS_URL@Q}"
  } >> "$GITHUB_STEP_SUMMARY"
  
  # Print to stderr
  echo "Error: $error_msg" >&2
  echo "$requirement_msg" >&2
  echo "Please configure one of these secrets in your repository settings." >&2
  echo "Documentation: ${DOCS_URL@Q}" >&2
  
  # Set step output to indicate verification failed
  if [ -n "$GITHUB_OUTPUT" ]; then
    echo "verification_result=failed" >> "$GITHUB_OUTPUT"
  fi
  
  exit 1
fi

# Log success in collapsible section
echo "<details>"
echo "<summary>Agent Environment Validation</summary>"
echo ""

# Build if/elif/else chain to match original behavior
# First secret uses if
first_secret="${SECRET_NAMES[0]}"
first_value="${!first_secret}"
if [ -n "$first_value" ]; then
  echo "✅ $first_secret: Configured"
# Middle secrets use elif (if there are more than 2 secrets)
elif [ "${#SECRET_NAMES[@]}" -gt 2 ]; then
  found=false
  for ((i=1; i<${#SECRET_NAMES[@]}-1; i++)); do
    secret_name="${SECRET_NAMES[$i]}"
    secret_value="${!secret_name}"
    if [ -n "$secret_value" ]; then
      echo "✅ $secret_name: Configured"
      found=true
      break
    fi
  done
  # Last secret uses else
  if [ "$found" = false ]; then
    last_secret="${SECRET_NAMES[-1]}"
    echo "✅ $last_secret: Configured"
  fi
# Last secret uses else (for 2 secret case)
else
  last_secret="${SECRET_NAMES[-1]}"
  if [ "${#SECRET_NAMES[@]}" -eq 2 ]; then
    echo "✅ $last_secret: Configured (using as fallback for ${SECRET_NAMES[0]})"
  else
    echo "✅ $last_secret: Configured"
  fi
fi

echo "</details>"

# Set step output to indicate verification succeeded
if [ -n "$GITHUB_OUTPUT" ]; then
  echo "verification_result=success" >> "$GITHUB_OUTPUT"
fi
