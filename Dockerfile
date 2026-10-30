# Dockerfile for GitHub Agentic Workflows compiler
# Provides gh-aw, gh CLI, and native strict compilation dependencies

# Use Google's Docker Hub mirror to avoid unauthenticated pull limits and auth outages.
# Keep the official Alpine digest so the mirrored image is identical.
FROM mirror.gcr.io/library/alpine:3.24@sha256:be2656061b536c1883c4034790e21d0a09c5aaaf51968b89368a3c0371ea2c9a

# Install required dependencies
RUN apk add --no-cache \
    git \
    jq \
    bash \
    curl \
    ca-certificates \
    shellcheck \
    github-cli

# Fail the image build if strict compilation's native scanner is unavailable.
RUN shellcheck --version

# Accept build argument for binary name (defaults to linux-amd64)
ARG BINARY=gh-aw-linux-amd64

# Create a directory for the binary
WORKDIR /usr/local/bin

# Copy the gh-aw binary from build context
COPY ${BINARY} /usr/local/bin/gh-aw

# Ensure the binary is executable
RUN chmod +x /usr/local/bin/gh-aw

# Configure git to trust all directories to avoid "dubious ownership" errors
# This is necessary when the container runs with mounted volumes owned by different users
RUN git config --global --add safe.directory '*'

# Set working directory for users
WORKDIR /workspace

# Set the entrypoint to gh-aw
ENTRYPOINT ["gh-aw"]

# Default command runs MCP server with actor validation enabled
# The GITHUB_ACTOR environment variable must be set for logs and audit tools to be available
# Binary path detection is automatic via os.Executable()
CMD ["mcp-server", "--validate-actor"]

# Metadata labels
LABEL org.opencontainers.image.source="https://github.com/github/gh-aw"
LABEL org.opencontainers.image.description="GitHub Agentic Workflows - Write agentic workflows in natural language markdown"
LABEL org.opencontainers.image.licenses="MIT"
