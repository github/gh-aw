# Ubuntu Actions Runner Image Analysis

**Last Updated**: 2026-02-14
**Source**: [Ubuntu 24.04 Runner Image Documentation](https://github.com/actions/runner-images/blob/main/images/ubuntu/Ubuntu2404-Readme.md)
**Ubuntu Version**: 24.04.3 LTS
**Image Version**: 20260201.15.1
**Kernel Version**: 6.11.0-1018-azure

## Overview

This document provides a comprehensive analysis of the default GitHub Actions Ubuntu runner image (ubuntu-latest) and guidance for creating Docker images that mimic its environment. The ubuntu-latest runner is currently based on Ubuntu 24.04.3 LTS and includes a wide range of development tools, language runtimes, build systems, databases, and CI/CD utilities.

The runner image is maintained by GitHub in the [actions/runner-images](https://github.com/actions/runner-images) repository and is updated regularly with security patches and new tool versions.

## Included Software Summary

The Ubuntu 24.04 runner includes:
- **Operating System**: Ubuntu 24.04.3 LTS with Linux kernel 6.11.0
- **Language Runtimes**: Node.js, Python, Ruby, Go, Java, PHP, Rust, Swift, Kotlin, Julia, and more
- **Container Tools**: Docker 28.0.4, Docker Compose 2.38.2, Podman, Buildah, Skopeo
- **Build Tools**: CMake, Make, Gradle, Maven, Ant, Bazel
- **Databases**: PostgreSQL 16.11, MySQL 8.0.44, SQLite 3.45.1
- **CI/CD Tools**: GitHub CLI, Azure CLI, AWS CLI, Google Cloud CLI
- **Testing Tools**: Selenium, multiple browsers (Chrome, Firefox, Edge)
- **Package Managers**: npm, pip, gem, cargo, composer, and more

## Operating System

- **Distribution**: Ubuntu 24.04.3 LTS (Noble Numbat)
- **Kernel**: Linux 6.11.0-1018-azure
- **Architecture**: x86_64
- **Systemd Version**: 255.4-1ubuntu8.12

## Language Runtimes

### Node.js
- **Available Versions**: 20.20.0 (installed), 22.22.0, 24.13.0 (cached)
- **Default Version**: 20.20.0
- **Package Managers**:
  - npm: 10.8.2
  - yarn: 1.22.22
  - pnpm (via npm install)
- **Version Manager**: nvm 0.40.4

### Python
- **Installed Version**: 3.12.3 (system default)
- **Cached Versions**: 3.10.19, 3.11.14, 3.12.12, 3.13.11, 3.14.2
- **PyPy Versions**: 3.9.19, 3.10.16, 3.11.13
- **Package Managers**:
  - pip: 24.0
  - pip3: 24.0
  - pipx: 1.8.0
- **Additional Tools**: Miniconda 25.11.1

### Ruby
- **Installed Version**: 3.2.3
- **Cached Versions**: 3.2.10, 3.3.10, 3.4.8, 4.0.1
- **Package Manager**: RubyGems 3.4.20
- **Additional Tools**: Bundler (included with RubyGems)

### Go
- **Cached Versions**: 1.22.12, 1.23.12, 1.24.12, 1.25.6
- **Installation**: Managed via setup-go action or manual installation

### Java
Multiple Java versions are pre-installed:
- **Java 8**: 8.0.472+8 (JAVA_HOME_8_X64)
- **Java 11**: 11.0.30+7 (JAVA_HOME_11_X64)
- **Java 17**: 17.0.18+8 (default) (JAVA_HOME_17_X64)
- **Java 21**: 21.0.10+7 (JAVA_HOME_21_X64)
- **Java 25**: 25.0.2+10 (JAVA_HOME_25_X64)

### PHP
- **Version**: 8.3.6
- **Package Manager**: Composer 2.9.5
- **Testing Tool**: PHPUnit 8.5.52
- **Extensions**: Xdebug and PCOV (Xdebug enabled by default)

### Rust
- **Version**: 1.93.0
- **Cargo**: 1.93.0
- **Rustup**: 1.28.2
- **Rustfmt**: 1.8.0

### Other Languages
- **Kotlin**: 2.3.0-release-356
- **Swift**: 6.2.3
- **Julia**: 1.12.4
- **Perl**: 5.38.2
- **Bash**: 5.2.21(1)-release

### Compilers
- **Clang**: 16.0.6, 17.0.6, 18.1.3
- **GNU C++**: 12.4.0, 13.3.0, 14.2.0
- **GNU Fortran**: 12.4.0, 13.3.0, 14.2.0

## Container Tools

### Docker
- **Client Version**: 28.0.4
- **Server Version**: 28.0.4
- **Docker Compose**: 2.38.2
- **Docker Buildx**: 0.31.1
- **Credential Helpers**: Amazon ECR Credential Helper 0.11.0

### Alternative Container Tools
- **Podman**: 4.9.3
- **Buildah**: 1.33.7
- **Skopeo**: 1.13.3

### Kubernetes Tools
- **kubectl**: 1.35.0
- **helm**: 3.20.0
- **minikube**: 1.38.0
- **kind**: 0.31.0
- **kustomize**: 5.8.0

## Build Tools

- **Make**: 4.3
- **CMake**: 3.31.6
- **Ninja**: 1.13.2
- **Autoconf**: 2.71-3
- **Automake**: 1.16.5
- **gcc/g++**: 13.2.0 (default), with 12.4.0 and 14.2.0 also available
- **Bazel**: 9.0.0
- **Bazelisk**: 1.28.1

## Project Management & Build Systems

- **Maven**: 3.9.12
- **Gradle**: 9.3.1
- **Ant**: 1.10.14
- **Lerna**: 9.0.3

### Haskell Build Tools
- **Cabal**: 3.16.1.0
- **GHC**: 9.14.1
- **GHCup**: 0.1.50.2
- **Stack**: 3.9.1

## Databases & Services

### PostgreSQL
- **Version**: 16.11
- **Default User**: postgres
- **Service Status**: Disabled by default
- **Start Command**: `sudo systemctl start postgresql.service`

### MySQL
- **Version**: 8.0.44-0ubuntu0.24.04.2
- **Default User**: root
- **Default Password**: root
- **Service Status**: Disabled by default
- **Start Command**: `sudo systemctl start mysql.service`

### SQLite
- **Version**: 3.45.1

## Web Servers

### Apache2
- **Version**: 2.4.58
- **Config File**: /etc/apache2/apache2.conf
- **Service Status**: inactive
- **Listen Port**: 80

### Nginx
- **Version**: 1.24.0
- **Config File**: /etc/nginx/nginx.conf
- **Service Status**: inactive
- **Listen Port**: 80

## CI/CD Tools

### GitHub CLI
- **Version**: 2.86.0
- **Installed**: Pre-configured and ready to use

### Cloud Provider CLIs
- **AWS CLI**: 2.33.12
  - AWS SAM CLI: 1.153.1
  - AWS CLI Session Manager Plugin: 1.2.764.0
- **Azure CLI**: 2.82.0
  - Azure DevOps Extension: 1.0.2
- **Google Cloud CLI**: 554.0.0

### Infrastructure as Code
- **Terraform**: Not pre-installed
- **Pulumi**: 3.218.0
- **Ansible**: 2.20.2
- **Packer**: 1.14.3
- **Bicep**: 0.40.2

### Other DevOps Tools
- **Fastlane**: 2.231.1
- **CodeQL Action Bundle**: 2.24.0

## Browsers and Testing Tools

### Browsers
- **Google Chrome**: 144.0.7559.109
- **Chromium**: 144.0.7559.0
- **Microsoft Edge**: 144.0.3719.104
- **Mozilla Firefox**: 147.0.2

### Browser Drivers
- **ChromeDriver**: 144.0.7559.109
- **Microsoft Edge WebDriver**: 144.0.3719.104
- **Geckodriver**: 0.36.0
- **Selenium Server**: 4.40.0

### Environment Variables
| Variable | Value |
|----------|-------|
| CHROMEWEBDRIVER | /usr/local/share/chromedriver-linux64 |
| EDGEWEBDRIVER | /usr/local/share/edge_driver |
| GECKOWEBDRIVER | /usr/local/share/gecko_driver |
| SELENIUM_JAR_PATH | /usr/share/java/selenium-server.jar |

## .NET Tools

- **.NET SDK Versions**: 8.0.123, 8.0.206, 8.0.319, 8.0.417, 9.0.113, 9.0.205, 9.0.310, 10.0.102
- **nbgv**: 3.9.50+6feeb89450

## PowerShell Tools

- **PowerShell**: 7.4.13
- **PowerShell Modules**:
  - Az: 14.6.0
  - Microsoft.Graph: 2.35.0
  - Pester: 5.7.1
  - PSScriptAnalyzer: 1.24.0

## Android Development

### Android SDK Components
- **Command Line Tools**: 12.0
- **Build-tools**: 36.0.0, 36.1.0, 35.0.0, 35.0.1, 34.0.0
- **Platform-Tools**: 36.0.2
- **CMake**: 3.31.5, 4.1.2
- **NDK**: 27.3.13750724 (default), 28.2.13676358, 29.0.14206865

### Environment Variables
| Variable | Value |
|----------|-------|
| ANDROID_HOME | /usr/local/lib/android/sdk |
| ANDROID_SDK_ROOT | /usr/local/lib/android/sdk |
| ANDROID_NDK | /usr/local/lib/android/sdk/ndk/27.3.13750724 |
| ANDROID_NDK_HOME | /usr/local/lib/android/sdk/ndk/27.3.13750724 |
| ANDROID_NDK_ROOT | /usr/local/lib/android/sdk/ndk/27.3.13750724 |
| ANDROID_NDK_LATEST_HOME | /usr/local/lib/android/sdk/ndk/29.0.14206865 |

## System Utilities

### Package Managers
- **Homebrew**: 5.0.12 (installed at /home/linuxbrew, not in PATH by default)
- **Vcpkg**: Installed from commit 6d7bf7ef21
- **Miniconda**: 25.11.1

### Version Control
- **Git**: 2.52.0
- **Git LFS**: 3.7.1
- **Git-ftp**: 1.6.0
- **Mercurial**: 6.7.2

### Compression Tools
- **bzip2**: 1.0.8
- **gzip**: Included with coreutils
- **pigz**: 2.8 (parallel gzip)
- **lz4**: 1.9.4
- **xz-utils**: 5.6.1
- **zstd**: 1.5.7
- **zip/unzip**: 3.0 / 6.0
- **p7zip**: 16.02
- **upx**: 4.2.2

### Utilities
- **jq**: 1.7.1 (JSON processor)
- **yq**: 4.52.2 (YAML processor)
- **yamllint**: 1.38.0
- **curl**: 8.5.0
- **wget**: 1.21.4
- **rsync**: 3.2.7
- **aria2**: 1.37.0 (download utility)
- **AzCopy**: 10.31.1
- **newman**: 6.2.2 (Postman CLI)
- **shellcheck**: 0.9.0

## Key Environment Variables

```bash
# Paths
PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin

# Java
JAVA_HOME=/usr/lib/jvm/temurin-17-jdk-amd64
JAVA_HOME_8_X64=/usr/lib/jvm/temurin-8-jdk-amd64
JAVA_HOME_11_X64=/usr/lib/jvm/temurin-11-jdk-amd64
JAVA_HOME_17_X64=/usr/lib/jvm/temurin-17-jdk-amd64
JAVA_HOME_21_X64=/usr/lib/jvm/temurin-21-jdk-amd64
JAVA_HOME_25_X64=/usr/lib/jvm/temurin-25-jdk-amd64

# Android
ANDROID_HOME=/usr/local/lib/android/sdk
ANDROID_SDK_ROOT=/usr/local/lib/android/sdk
ANDROID_NDK=/usr/local/lib/android/sdk/ndk/27.3.13750724

# Package Managers
CONDA=/usr/share/miniconda
VCPKG_INSTALLATION_ROOT=/usr/local/share/vcpkg

# Browser Drivers
CHROMEWEBDRIVER=/usr/local/share/chromedriver-linux64
EDGEWEBDRIVER=/usr/local/share/edge_driver
GECKOWEBDRIVER=/usr/local/share/gecko_driver
SELENIUM_JAR_PATH=/usr/share/java/selenium-server.jar
```

## Creating a Docker Image Mimic

To create a Docker image that mimics the GitHub Actions Ubuntu runner environment, follow these guidelines. Note that replicating the entire runner image (~20GB+) is not practical, so focus on the tools you need.

### Base Image

Start with the Ubuntu 24.04 base image:

```dockerfile
FROM ubuntu:24.04

# Prevent interactive prompts during package installation
ENV DEBIAN_FRONTEND=noninteractive

# Set timezone (GitHub Actions uses UTC)
RUN ln -snf /usr/share/zoneinfo/UTC /etc/localtime && echo UTC > /etc/timezone
```

### System Packages

Install essential system packages:

```dockerfile
# Update and upgrade system packages
RUN apt-get update && apt-get upgrade -y

# Install build essentials and common tools
RUN apt-get install -y \
    build-essential \
    curl \
    wget \
    git \
    unzip \
    zip \
    tar \
    gzip \
    bzip2 \
    xz-utils \
    ca-certificates \
    gnupg \
    lsb-release \
    software-properties-common \
    sudo \
    jq \
    vim \
    nano
```

### Node.js Installation

Install Node.js using NodeSource or nvm:

```dockerfile
# Install Node.js 20 (current LTS)
RUN curl -fsSL https://deb.nodesource.com/setup_20.x | bash - && \
    apt-get install -y nodejs

# Install yarn and pnpm globally
RUN npm install -g yarn pnpm

# Verify installations
RUN node --version && npm --version && yarn --version && pnpm --version
```

### Python Installation

Install Python 3.12 (default on Ubuntu 24.04):

```dockerfile
# Install Python and pip
RUN apt-get install -y \
    python3 \
    python3-pip \
    python3-venv \
    python3-dev

# Create python and pip aliases
RUN ln -s /usr/bin/python3 /usr/bin/python && \
    ln -s /usr/bin/pip3 /usr/bin/pip

# Install pipx for global tool installation
RUN python3 -m pip install --user pipx && \
    python3 -m pipx ensurepath

# Verify installation
RUN python --version && pip --version
```

### Docker Installation

Install Docker (for Docker-in-Docker scenarios):

```dockerfile
# Add Docker's official GPG key
RUN install -m 0755 -d /etc/apt/keyrings && \
    curl -fsSL https://download.docker.com/linux/ubuntu/gpg -o /etc/apt/keyrings/docker.asc && \
    chmod a+r /etc/apt/keyrings/docker.asc

# Add Docker repository
RUN echo \
    "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/ubuntu \
    $(. /etc/os-release && echo \"$VERSION_CODENAME\") stable" | \
    tee /etc/apt/sources.list.d/docker.list > /dev/null

# Install Docker
RUN apt-get update && apt-get install -y \
    docker-ce \
    docker-ce-cli \
    containerd.io \
    docker-buildx-plugin \
    docker-compose-plugin

# Note: You'll need to run the container with --privileged and possibly mount the Docker socket
```

### GitHub CLI

Install the GitHub CLI:

```dockerfile
# Add GitHub CLI repository
RUN curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg | \
    dd of=/usr/share/keyrings/githubcli-archive-keyring.gpg && \
    chmod go+r /usr/share/keyrings/githubcli-archive-keyring.gpg && \
    echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" | \
    tee /etc/apt/sources.list.d/github-cli.list > /dev/null

# Install GitHub CLI
RUN apt-get update && apt-get install -y gh

# Verify installation
RUN gh --version
```

### Java Installation

Install multiple Java versions using Temurin:

```dockerfile
# Add Adoptium repository for Eclipse Temurin
RUN wget -qO - https://packages.adoptium.net/artifactory/api/gpg/key/public | \
    gpg --dearmor -o /usr/share/keyrings/adoptium.gpg && \
    echo "deb [signed-by=/usr/share/keyrings/adoptium.gpg] https://packages.adoptium.net/artifactory/deb $(lsb_release -cs) main" | \
    tee /etc/apt/sources.list.d/adoptium.list

# Install multiple Java versions
RUN apt-get update && apt-get install -y \
    temurin-17-jdk \
    temurin-21-jdk

# Set Java 17 as default
ENV JAVA_HOME=/usr/lib/jvm/temurin-17-jdk-amd64
ENV PATH="${JAVA_HOME}/bin:${PATH}"

# Verify installation
RUN java -version
```

### Go Installation

Install Go:

```dockerfile
# Install Go
ARG GO_VERSION=1.23.12
RUN wget https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz && \
    tar -C /usr/local -xzf go${GO_VERSION}.linux-amd64.tar.gz && \
    rm go${GO_VERSION}.linux-amd64.tar.gz

ENV PATH="/usr/local/go/bin:${PATH}"
ENV GOPATH=/root/go
ENV PATH="${GOPATH}/bin:${PATH}"

# Verify installation
RUN go version
```

### Ruby Installation

Install Ruby using system packages or rbenv:

```dockerfile
# Install Ruby
RUN apt-get install -y ruby-full

# Verify installation
RUN ruby --version && gem --version
```

### Cloud CLI Tools

Install AWS CLI, Azure CLI, or Google Cloud SDK as needed:

```dockerfile
# Install AWS CLI v2
RUN curl "https://awscli.amazonaws.com/awscli-exe-linux-x86_64.zip" -o "awscliv2.zip" && \
    unzip awscliv2.zip && \
    ./aws/install && \
    rm -rf aws awscliv2.zip

# Install Azure CLI
RUN curl -sL https://aka.ms/InstallAzureCLIDeb | bash

# Install Google Cloud SDK
RUN echo "deb [signed-by=/usr/share/keyrings/cloud.google.gpg] https://packages.cloud.google.com/apt cloud-sdk main" | \
    tee -a /etc/apt/sources.list.d/google-cloud-sdk.list && \
    curl https://packages.cloud.google.com/apt/doc/apt-key.gpg | \
    gpg --dearmor -o /usr/share/keyrings/cloud.google.gpg && \
    apt-get update && apt-get install -y google-cloud-sdk
```

### Cleanup

Clean up apt cache to reduce image size:

```dockerfile
# Cleanup
RUN apt-get clean && \
    rm -rf /var/lib/apt/lists/* /tmp/* /var/tmp/*
```

### Complete Dockerfile Example

Here's a comprehensive Dockerfile that includes commonly used tools:

```dockerfile
FROM ubuntu:24.04

# Prevent interactive prompts
ENV DEBIAN_FRONTEND=noninteractive

# Set timezone
RUN ln -snf /usr/share/zoneinfo/UTC /etc/localtime && echo UTC > /etc/timezone

# Update system and install essentials
RUN apt-get update && apt-get upgrade -y && \
    apt-get install -y \
    build-essential \
    curl \
    wget \
    git \
    unzip \
    zip \
    tar \
    gzip \
    bzip2 \
    xz-utils \
    ca-certificates \
    gnupg \
    lsb-release \
    software-properties-common \
    sudo \
    jq \
    vim \
    nano \
    openssh-client \
    rsync

# Install Node.js 20 LTS
RUN curl -fsSL https://deb.nodesource.com/setup_20.x | bash - && \
    apt-get install -y nodejs && \
    npm install -g yarn pnpm

# Install Python 3.12
RUN apt-get install -y \
    python3 \
    python3-pip \
    python3-venv \
    python3-dev && \
    ln -s /usr/bin/python3 /usr/bin/python && \
    ln -s /usr/bin/pip3 /usr/bin/pip

# Install Docker
RUN install -m 0755 -d /etc/apt/keyrings && \
    curl -fsSL https://download.docker.com/linux/ubuntu/gpg -o /etc/apt/keyrings/docker.asc && \
    chmod a+r /etc/apt/keyrings/docker.asc && \
    echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/ubuntu \
    $(. /etc/os-release && echo \"$VERSION_CODENAME\") stable" | \
    tee /etc/apt/sources.list.d/docker.list > /dev/null && \
    apt-get update && \
    apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin

# Install GitHub CLI
RUN curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg | \
    dd of=/usr/share/keyrings/githubcli-archive-keyring.gpg && \
    chmod go+r /usr/share/keyrings/githubcli-archive-keyring.gpg && \
    echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" | \
    tee /etc/apt/sources.list.d/github-cli.list > /dev/null && \
    apt-get update && \
    apt-get install -y gh

# Install Java (Temurin 17 & 21)
RUN wget -qO - https://packages.adoptium.net/artifactory/api/gpg/key/public | \
    gpg --dearmor -o /usr/share/keyrings/adoptium.gpg && \
    echo "deb [signed-by=/usr/share/keyrings/adoptium.gpg] https://packages.adoptium.net/artifactory/deb $(lsb_release -cs) main" | \
    tee /etc/apt/sources.list.d/adoptium.list && \
    apt-get update && \
    apt-get install -y temurin-17-jdk temurin-21-jdk

# Set Java 17 as default
ENV JAVA_HOME=/usr/lib/jvm/temurin-17-jdk-amd64
ENV PATH="${JAVA_HOME}/bin:${PATH}"

# Install Go
ARG GO_VERSION=1.23.12
RUN wget https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz && \
    tar -C /usr/local -xzf go${GO_VERSION}.linux-amd64.tar.gz && \
    rm go${GO_VERSION}.linux-amd64.tar.gz
ENV PATH="/usr/local/go/bin:${PATH}"
ENV GOPATH=/root/go
ENV PATH="${GOPATH}/bin:${PATH}"

# Install Ruby
RUN apt-get install -y ruby-full

# Set up environment variables
ENV DEBIAN_FRONTEND=
ENV PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin

# Cleanup
RUN apt-get clean && \
    rm -rf /var/lib/apt/lists/* /tmp/* /var/tmp/*

# Set working directory
WORKDIR /workspace

# Default command
CMD ["/bin/bash"]
```

### Building and Running

Build the Docker image:

```bash
docker build -t ubuntu-runner-mimic:latest .
```

Run the container:

```bash
# Basic run
docker run -it --rm ubuntu-runner-mimic:latest

# With Docker support (Docker-in-Docker)
docker run -it --rm --privileged -v /var/run/docker.sock:/var/run/docker.sock ubuntu-runner-mimic:latest

# With volume mount for workspace
docker run -it --rm -v $(pwd):/workspace ubuntu-runner-mimic:latest
```

## Key Differences from Runner

Important aspects that cannot be perfectly replicated in a custom Docker image:

### 1. GitHub Actions Context

The official runner includes GitHub Actions-specific features:
- **Environment variables**: `GITHUB_WORKSPACE`, `GITHUB_REPOSITORY`, `GITHUB_SHA`, `RUNNER_TEMP`, etc.
- **Actions toolkit**: Pre-installed binaries and scripts for GitHub Actions features
- **Workflow context**: Access to secrets, contexts, and GitHub API with built-in authentication
- **Problem matchers**: Automatic detection of warnings/errors in logs

### 2. Pre-cached Dependencies

The runner image has pre-cached:
- Docker images for common services
- Package manager caches (npm, pip, gem)
- Tool installations to speed up workflows
- This results in faster builds and reduced network usage

### 3. Service Configuration

- Services like PostgreSQL and MySQL are installed but disabled by default
- The runner has specific service start/stop mechanisms
- Network configuration and service discovery may differ

### 4. File System Layout

- The runner uses specific directory structures: `/home/runner`, `/opt`, `/usr/share`
- Cache directories and tool locations follow GitHub's conventions
- Permissions and ownership may differ from a standard Docker container

### 5. Performance and Resource Limits

- GitHub-hosted runners have specific CPU, memory, and disk limits
- Network bandwidth and speed may differ significantly
- Storage is ephemeral and cleaned between runs

### 6. Pre-installed Certificates and Trust Stores

- The runner has GitHub's root certificates pre-installed
- SSL/TLS certificate handling may behave differently
- Internal GitHub services may not be accessible

### 7. Tool Versioning and Updates

- GitHub updates the runner image regularly
- Your custom image will need manual maintenance to stay current
- Version mismatches can cause unexpected behavior

### 8. Size Considerations

- The full GitHub Actions runner image is ~20GB+
- A practical mimic should focus on required tools only
- Consider using multi-stage builds to reduce final image size

## Maintenance Notes

### Keeping Your Image Updated

1. **Monitor the runner-images repository**:
   - Watch for releases: https://github.com/actions/runner-images/releases
   - Review announcements: Check the README for upcoming changes

2. **Rebuild your image regularly**:
   ```bash
   # Update base image
   docker pull ubuntu:24.04
   
   # Rebuild your custom image
   docker build --no-cache -t ubuntu-runner-mimic:latest .
   ```

3. **Test compatibility**:
   - Run your CI/CD workflows in the custom image
   - Compare behavior with GitHub-hosted runners
   - Test tool versions match your requirements

### Version Pinning

For reproducible builds, pin specific versions:

```dockerfile
# Pin Node.js version
ARG NODE_VERSION=20.20.0
RUN curl -fsSL https://deb.nodesource.com/setup_20.x | bash - && \
    apt-get install -y nodejs=${NODE_VERSION}*

# Pin Go version
ARG GO_VERSION=1.23.12
RUN wget https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz && \
    tar -C /usr/local -xzf go${GO_VERSION}.linux-amd64.tar.gz

# Pin Python packages
RUN pip install \
    pytest==7.4.0 \
    black==23.7.0 \
    mypy==1.4.1
```

### Recommended Update Frequency

- **Security patches**: Weekly or as CVEs are announced
- **Tool updates**: Monthly or when new LTS versions are released
- **Base image**: After each Ubuntu 24.04 point release
- **Full rebuild**: Quarterly to ensure fresh package lists

## References

- **Runner Image Repository**: https://github.com/actions/runner-images
- **Ubuntu 24.04 Documentation**: https://github.com/actions/runner-images/blob/main/images/ubuntu/Ubuntu2404-Readme.md
- **Ubuntu Server Documentation**: https://ubuntu.com/server/docs
- **Docker Documentation**: https://docs.docker.com/
- **GitHub Actions Documentation**: https://docs.github.com/en/actions
- **Adoptium (Eclipse Temurin)**: https://adoptium.net/
- **NodeSource**: https://github.com/nodesource/distributions

## Use Cases for Custom Runner Images

### Local Development

Create a consistent development environment:

```bash
docker run -it --rm \
  -v $(pwd):/workspace \
  -w /workspace \
  ubuntu-runner-mimic:latest \
  bash
```

### Self-Hosted Runners

Use as a base for self-hosted GitHub Actions runners:

```dockerfile
FROM ubuntu-runner-mimic:latest

# Install GitHub Actions runner
ARG RUNNER_VERSION=2.314.1
RUN cd /opt && \
    curl -o actions-runner-linux-x64.tar.gz -L \
    https://github.com/actions/runner/releases/download/v${RUNNER_VERSION}/actions-runner-linux-x64-${RUNNER_VERSION}.tar.gz && \
    tar xzf actions-runner-linux-x64.tar.gz && \
    rm actions-runner-linux-x64.tar.gz

# Configure runner startup
COPY start-runner.sh /opt/start-runner.sh
RUN chmod +x /opt/start-runner.sh

CMD ["/opt/start-runner.sh"]
```

### CI/CD Testing

Test workflows locally before pushing:

```bash
# Run tests in a GitHub Actions-like environment
docker run --rm \
  -v $(pwd):/workspace \
  -w /workspace \
  ubuntu-runner-mimic:latest \
  npm test
```

### Reproducible Builds

Ensure builds work identically across environments:

```bash
# Build with specific tool versions
docker run --rm \
  -v $(pwd):/workspace \
  -w /workspace \
  ubuntu-runner-mimic:1.0.0 \
  make build
```

---

*This document is maintained by the Ubuntu Actions Image Analyzer workflow. For updates or corrections, please open an issue or pull request in the [github/gh-aw](https://github.com/github/gh-aw) repository.*
