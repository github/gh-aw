# Dockerfile for GitHub Agentic Workflows compiler
# Provides a minimal container with gh-aw, gh CLI, git, and jq

# Use Alpine for minimal size (official distribution)
FROM alpine:3.21

# Install required dependencies
RUN apk add --no-cache \
    git \
    jq \
    bash \
    curl \
    ca-certificates \
    github-cli

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

# Default command runs MCP server
# Note: Binary path detection is automatic via os.Executable()
CMD ["mcp-server"]

# Metadata labels
LABEL org.opencontainers.image.source="https://github.com/github/gh-aw"
LABEL org.opencontainers.image.description="GitHub Agentic Workflows - Write agentic workflows in natural language markdown"
LABEL org.opencontainers.image.licenses="MIT"
