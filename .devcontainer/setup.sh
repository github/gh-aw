#!/bin/bash
set +o histexpand
set -e

# install project dependencies
make deps

# configure the Vertex API access
# take vertex_api_json from environment variable and write it to `.credentials/vertex_api.json`
if [ -z "$VERTEX_API_JSON" ]; then
  echo "VERTEX_API_JSON environment variable is not set. Please set it to your Vertex API JSON credentials."
  exit 0
fi  
mkdir -p .credentials
echo "$VERTEX_API_JSON" > .credentials/vertex_api.json


#install gcloud CLI
sudo apt-get install apt-transport-https ca-certificates gnupg curl -y
curl https://packages.cloud.google.com/apt/doc/apt-key.gpg | sudo gpg --dearmor -o /usr/share/keyrings/cloud.google.gpg
echo "deb [signed-by=/usr/share/keyrings/cloud.google.gpg] https://packages.cloud.google.com/apt cloud-sdk main" | sudo tee -a /etc/apt/sources.list.d/google-cloud-sdk.list
sudo apt-get update && sudo apt-get install google-cloud-cli -y

export GOOGLE_APPLICATION_CREDENTIALS=.credentials/vertex_api.json
export CLAUDE_CODE_USE_VERTEX=1
export CLOUD_ML_REGION=us-east5
export ANTHROPIC_VERTEX_PROJECT_ID=github-next

# install uvx
UV_VERSION=0.12.23
case "$(uname -m)" in
  x86_64)
    UV_ARCH=x86_64
    UV_ARCHIVE_SHA256=9167d72b3319674b6303c4cbe071854bba13ebdf3d76b1a7cbdc175471fb66d6
    ;;
  aarch64|arm64)
    UV_ARCH=aarch64
    UV_ARCHIVE_SHA256=6524bd338177ed50d035d39354e12545e993bbeba2ecbddf0480c5b3a81d313f
    ;;
  *)
    echo "Unsupported architecture for uv: $(uname -m)" >&2
    exit 1
    ;;
esac
UV_DIRECTORY="uv-${UV_ARCH}-unknown-linux-gnu"
UV_ARCHIVE="$(mktemp "${TMPDIR:-/tmp}/uv.XXXXXX.tar.gz")"
trap 'rm -f "$UV_ARCHIVE"' EXIT
curl -fsSL "https://github.com/astral-sh/uv/releases/download/${UV_VERSION}/${UV_DIRECTORY}.tar.gz" -o "$UV_ARCHIVE"
printf '%s  %s\n' "$UV_ARCHIVE_SHA256" "$UV_ARCHIVE" | sha256sum -c -
mkdir -p "$HOME/.local/bin"
tar -xzf "$UV_ARCHIVE" -C "$HOME/.local/bin" --strip-components=1 "${UV_DIRECTORY}/uv" "${UV_DIRECTORY}/uvx"
for profile in "$HOME/.profile" "$HOME/.bashrc"; do
  touch "$profile"
  if ! grep -Fq 'export PATH="$HOME/.local/bin:$PATH"' "$profile"; then
    printf '\nexport PATH="$HOME/.local/bin:$PATH"\n' >> "$profile"
  fi
done