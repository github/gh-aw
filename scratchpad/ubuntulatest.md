# Ubuntu Actions Runner Image Analysis

**Last Updated**: 2026-01-23  
**Source**: [Ubuntu2404-Readme.md (20260119)](https://github.com/actions/runner-images/blob/releases/ubuntu24/20260119/images/ubuntu/Ubuntu2404-Readme.md)  
**Ubuntu Version**: 24.04.3 LTS  
**Image Version**: 20260119.4.1  
**Runner Version**: 2.331.0  
**Kernel Version**: 6.11.0-1018-azure  
**Systemd Version**: 255.4-1ubuntu8.12  

## Overview

This document provides an analysis of the default GitHub Actions Ubuntu runner image (`ubuntu-latest`, currently Ubuntu 24.04) and guidance for creating Docker images that mimic its environment. The runner image is a development environment with 200+ pre-installed tools including runtimes, databases, and build systems commonly used in CI/CD pipelines.

**Key Features**:
- Multiple language runtimes (Node.js, Python, Ruby, Go, Java, PHP, Rust)
- Container tools (Docker, Podman, Buildah)
- Cloud CLIs (AWS, Azure, Google Cloud)
- Build tools (Make, CMake, Gradle, Maven)
- Testing frameworks (Selenium, browsers)
- Development utilities (Git, SSH, compression tools)

## Included Software Summary

The Ubuntu 24.04 runner image includes:
- **Languages**: Bash, Clang, C++, Fortran, Julia, Kotlin, Node.js, Perl, Python, Ruby, Swift
- **Package Managers**: npm, pip, Homebrew, Conda, Vcpkg, RubyGems, Yarn
- **Build Tools**: Ant, Gradle, Maven, CMake, Ninja, Make
- **Container Tools**: Docker, Docker Compose, Podman, Buildah, Skopeo
- **Cloud CLIs**: AWS CLI, Azure CLI, Google Cloud CLI
- **Databases**: PostgreSQL 16.11, MySQL 8.0.44, SQLite 3.45.1
- **Web Servers**: Apache 2.4.58, Nginx 1.24.0
- **Browsers**: Chrome, Chromium, Firefox, Edge (with WebDrivers)
- **Android SDK**: Build tools, platforms, NDK

## Operating System

- **Distribution**: Ubuntu 24.04.3 LTS
- **Kernel**: 6.11.0-1018-azure
- **Architecture**: x86_64
- **Systemd**: 255.4-1ubuntu8.12

## Language Runtimes

### Node.js
- **Installed Version**: 20.19.6
- **Cached Versions**: 20.19.6, 22.21.1, 24.12.0
- **Package Managers**: 
  - npm 10.8.2
  - Yarn 1.22.22
  - nvm 0.40.3
  - n 10.2.0
- **Environment**: Pre-installed with global npm packages available

### Python
- **Default Version**: 3.12.3
- **Cached Versions**: 3.9.25, 3.10.19, 3.11.14, 3.12.12, 3.13.11, 3.14.2
- **PyPy Versions**: 
  - PyPy 3.9.19 [PyPy 7.3.16]
  - PyPy 3.10.16 [PyPy 7.3.19]
  - PyPy 3.11.13 [PyPy 7.3.20]
- **Package Managers**: 
  - pip 24.0
  - pip3 24.0
  - Pipx 1.8.0
  - Miniconda 25.11.1
- **Additional Tools**: virtualenv, poetry support via pipx

### Ruby
- **Installed Version**: 3.2.3
- **Cached Versions**: 3.2.9, 3.3.10, 3.4.8
- **Package Manager**: RubyGems 3.4.20
- **Tools**: Bundler, Fastlane 2.230.0

### Go
- **Cached Versions**: 1.22.12, 1.23.12, 1.24.11, 1.25.5
- **Installation**: Multiple versions available via cached tools

### Java
- **Versions**:
  - 8.0.472+8 (JAVA_HOME_8_X64)
  - 11.0.29+7 (JAVA_HOME_11_X64)
  - 17.0.17+10 (default) (JAVA_HOME_17_X64)
  - 21.0.9+10 (JAVA_HOME_21_X64)
  - 25.0.1+8 (JAVA_HOME_25_X64)
- **Build Tools**: Maven 3.9.12, Gradle 9.2.1, Ant 1.10.14

### PHP
- **Version**: 8.3.6
- **Composer**: 2.9.3
- **PHPUnit**: 8.5.50
- **Extensions**: Xdebug (enabled), PCOV (installed but disabled)

### Rust
- **Rust**: 1.92.0
- **Cargo**: 1.92.0
- **Rustup**: 1.28.2
- **Rustfmt**: 1.8.0
- **Rustdoc**: 1.92.0

### Haskell
- **GHC**: 9.14.1
- **Cabal**: 3.16.1.0
- **GHCup**: 0.1.50.2
- **Stack**: 3.9.1

### Other Languages
- **Bash**: 5.2.21(1)-release
- **Perl**: 5.38.2
- **Julia**: 1.12.3
- **Kotlin**: 2.3.0-release-356
- **Swift**: 6.2.3

### Compilers
- **Clang**: 16.0.6, 17.0.6, 18.1.3
- **GCC/G++**: 12.4.0, 13.3.0, 14.2.0
- **GNU Fortran**: 12.4.0, 13.3.0, 14.2.0
- **Clang-format**: 16.0.6, 17.0.6, 18.1.3
- **Clang-tidy**: 16.0.6, 17.0.6, 18.1.3

## Container Tools

### Docker
- **Docker Client**: 28.0.4
- **Docker Server**: 28.0.4
- **Docker Compose v2**: 2.38.2
- **Docker-Buildx**: 0.30.1
- **Docker Amazon ECR Credential Helper**: 0.11.0

### Container Alternatives
- **Podman**: 4.9.3
- **Buildah**: 1.33.7
- **Skopeo**: 1.13.3

### Kubernetes Tools
- **kubectl**: 1.35.0
- **Kind**: 0.31.0
- **Minikube**: 1.37.0
- **Helm**: 3.19.4
- **Kustomize**: 5.8.0

## Build Tools

- **Make**: 4.3-4.1build2
- **CMake**: 3.31.6
- **Ninja**: 1.13.2
- **Autoconf**: 2.71-3
- **Automake**: 1.16.5-1.3ubuntu1
- **Libtool**: 2.4.7-7build1
- **Bison**: 3.8.2+dfsg-1build2
- **Flex**: 2.6.4-8.2build1
- **Patchelf**: 0.18.0-1.1build1
- **Bazel**: 8.5.0
- **Bazelisk**: 1.26.0

## Databases & Services

### PostgreSQL
- **Version**: 16.11
- **User**: postgres
- **Service Status**: Disabled by default
- **Start Command**: `sudo systemctl start postgresql.service`

### MySQL
- **Version**: 8.0.44-0ubuntu0.24.04.2
- **User**: root
- **Password**: root
- **Service Status**: Disabled by default
- **Start Command**: `sudo systemctl start mysql.service`

### SQLite
- **Version**: 3.45.1
- **Development Libraries**: libsqlite3-dev 3.45.1-1ubuntu2.5

## CI/CD Tools

### Version Control
- **Git**: 2.52.0
- **Git LFS**: 3.7.1
- **Git-ftp**: 1.6.0
- **Mercurial**: 6.7.2
- **GitHub CLI**: 2.83.2

### Cloud CLIs
- **AWS CLI**: 2.32.29
- **AWS CLI Session Manager Plugin**: 1.2.764.0
- **AWS SAM CLI**: 1.151.0
- **Azure CLI**: 2.81.0
- **Azure CLI (azure-devops)**: 1.0.2
- **Google Cloud CLI**: 550.0.0

### Infrastructure as Code
- **Terraform**: Not pre-installed (install via Homebrew or direct download)
- **Pulumi**: 3.214.0
- **Packer**: 1.14.3
- **Bicep**: 0.39.26
- **Ansible**: 2.20.1

### Build & Deployment
- **Lerna**: 9.0.3
- **Parcel**: 2.16.3
- **Newman**: 6.2.1
- **Fastlane**: 2.230.0

## Testing Tools

### Browsers & Drivers
- **Google Chrome**: 143.0.7499.169
- **ChromeDriver**: 143.0.7499.169
- **Chromium**: 143.0.7499.0
- **Microsoft Edge**: 143.0.3650.96
- **Microsoft Edge WebDriver**: 143.0.3650.96
- **Mozilla Firefox**: 146.0.1
- **Geckodriver**: 0.36.0
- **Selenium Server**: 4.39.0

### Environment Variables
```bash
CHROMEWEBDRIVER=/usr/local/share/chromedriver-linux64
EDGEWEBDRIVER=/usr/local/share/edge_driver
GECKOWEBDRIVER=/usr/local/share/gecko_driver
SELENIUM_JAR_PATH=/usr/share/java/selenium-server.jar
```

## Web Servers

| Name    | Version | Config File               | Status   | Port |
|---------|---------|---------------------------|----------|------|
| Apache  | 2.4.58  | /etc/apache2/apache2.conf | inactive | 80   |
| Nginx   | 1.24.0  | /etc/nginx/nginx.conf     | inactive | 80   |

Both web servers are pre-installed but not running by default.

## Android Development

### Android SDK
- **Command Line Tools**: 12.0
- **Build Tools**: 34.0.0, 35.0.0, 35.0.1, 36.0.0, 36.1.0
- **Platform Tools**: 36.0.2
- **Platforms**: android-34 (rev 3), android-35 (rev 2), android-36 (rev 2), plus extension versions
- **NDK Versions**: 26.3.11579264, 27.3.13750724 (default), 28.2.13676358, 29.0.14206865
- **CMake**: 3.31.5, 4.1.2
- **Google Play Services**: 49
- **Google Repository**: 58

### Environment Variables
```bash
ANDROID_HOME=/usr/local/lib/android/sdk
ANDROID_NDK=/usr/local/lib/android/sdk/ndk/27.3.13750724
ANDROID_NDK_HOME=/usr/local/lib/android/sdk/ndk/27.3.13750724
ANDROID_NDK_LATEST_HOME=/usr/local/lib/android/sdk/ndk/29.0.14206865
ANDROID_NDK_ROOT=/usr/local/lib/android/sdk/ndk/27.3.13750724
ANDROID_SDK_ROOT=/usr/local/lib/android/sdk
```

## PowerShell Tools

- **PowerShell**: 7.4.13
- **Modules**:
  - Az: 12.5.0
  - Microsoft.Graph: 2.34.0
  - Pester: 5.7.1
  - PSScriptAnalyzer: 1.24.0

## .NET Tools

- **.NET Core SDK**: 8.0.122, 8.0.206, 8.0.319, 8.0.416, 9.0.112, 9.0.205, 9.0.308, 10.0.101
- **nbgv**: 3.9.50+6feeb89450

## Development Utilities

### Compression Tools
- **bzip2**: 1.0.8-5.1build0.1
- **gzip**: Available (via coreutils)
- **xz-utils**: 5.6.1+really5.4.5-1ubuntu0.2
- **zip/unzip**: 3.0-13ubuntu0.2 / 6.0-28ubuntu4.1
- **p7zip**: 16.02+transitional.1 (full and rar)
- **zstd**: 1.5.7
- **lz4**: 1.9.4-1build1.1
- **brotli**: 1.1.0-2build2

### Network Tools
- **curl**: 8.5.0-2ubuntu10.6
- **wget**: 1.21.4-1ubuntu4.1
- **aria2**: 1.37.0+debian-1build3
- **rsync**: 3.2.7-1ubuntu1.2
- **OpenSSH Client**: 9.6p1-3ubuntu13.14
- **netcat**: 1.226-1ubuntu2
- **telnet**: 0.17+2.5-3ubuntu4
- **ftp**: 20230507-2build3
- **dnsutils**: 9.18.39-0ubuntu0.24.04.2
- **iproute2**: 6.1.0-1ubuntu6.2
- **iputils-ping**: 20240117-1ubuntu0.1

### Text Processing
- **jq**: 1.7.1-3ubuntu0.24.04.1
- **yq**: 4.50.1
- **yamllint**: 1.37.1

### Code Quality
- **ShellCheck**: 0.9.0-1
- **CodeQL Action Bundle**: 2.23.8

### Miscellaneous
- **MediaInfo**: 24.01.1-1build2
- **AzCopy**: 10.31.0
- **Haveged**: 1.9.14
- **parallel**: 20231122+ds-1
- **tree**: 2.1.1-2ubuntu3.24.04.2
- **time**: 1.9-0.2build1
- **upx**: 4.2.2-3

## Environment Variables

Key environment variables set in the runner image:

```bash
# Package Management
CONDA=/usr/share/miniconda
VCPKG_INSTALLATION_ROOT=/usr/local/share/vcpkg

# Android
ANDROID_HOME=/usr/local/lib/android/sdk
ANDROID_SDK_ROOT=/usr/local/lib/android/sdk
ANDROID_NDK=/usr/local/lib/android/sdk/ndk/27.3.13750724
ANDROID_NDK_HOME=/usr/local/lib/android/sdk/ndk/27.3.13750724
ANDROID_NDK_ROOT=/usr/local/lib/android/sdk/ndk/27.3.13750724
ANDROID_NDK_LATEST_HOME=/usr/local/lib/android/sdk/ndk/29.0.14206865

# Browsers
CHROMEWEBDRIVER=/usr/local/share/chromedriver-linux64
EDGEWEBDRIVER=/usr/local/share/edge_driver
GECKOWEBDRIVER=/usr/local/share/gecko_driver
SELENIUM_JAR_PATH=/usr/share/java/selenium-server.jar

# Java (multiple versions)
JAVA_HOME_8_X64=/usr/lib/jvm/temurin-8-jdk-amd64
JAVA_HOME_11_X64=/usr/lib/jvm/temurin-11-jdk-amd64
JAVA_HOME_17_X64=/usr/lib/jvm/temurin-17-jdk-amd64
JAVA_HOME_21_X64=/usr/lib/jvm/temurin-21-jdk-amd64
JAVA_HOME_25_X64=/usr/lib/jvm/temurin-25-jdk-amd64

# System
PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
DEBIAN_FRONTEND=noninteractive
```

## Creating a Docker Image Mimic

To create a Docker image that mimics the GitHub Actions Ubuntu runner environment, follow these guidelines. Note that creating a complete replica is not practical due to the extensive software list, so focus on the tools your workflows actually need.

### Base Image

Start with the Ubuntu base image matching the runner version:

```dockerfile
FROM ubuntu:24.04

# Set environment variables
ENV DEBIAN_FRONTEND=noninteractive
ENV TZ=UTC
```

### System Setup

```dockerfile
# Update system packages
RUN apt-get update && apt-get upgrade -y && \
    apt-get install -y \
    ca-certificates \
    curl \
    wget \
    git \
    gnupg \
    lsb-release \
    software-properties-common

# Install build essentials
RUN apt-get install -y \
    build-essential \
    cmake \
    make \
    autoconf \
    automake \
    libtool \
    pkg-config \
    bison \
    flex
```

### Language Runtimes

#### Node.js
```dockerfile
# Install Node.js via NodeSource
RUN curl -fsSL https://deb.nodesource.com/setup_20.x | bash - && \
    apt-get install -y nodejs

# Install package managers
RUN npm install -g yarn npm@latest
```

#### Python
```dockerfile
# Install Python
RUN apt-get install -y \
    python3 \
    python3-pip \
    python3-venv \
    python3-dev

# Update pip
RUN python3 -m pip install --upgrade pip setuptools wheel

# Install pipx for isolated tool installation
RUN python3 -m pip install pipx && \
    python3 -m pipx ensurepath
```

#### Ruby
```dockerfile
# Install Ruby
RUN apt-get install -y \
    ruby-full \
    ruby-bundler
```

#### Go
```dockerfile
# Install Go
ARG GO_VERSION=1.23.12
RUN wget https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz && \
    tar -C /usr/local -xzf go${GO_VERSION}.linux-amd64.tar.gz && \
    rm go${GO_VERSION}.linux-amd64.tar.gz

ENV PATH=$PATH:/usr/local/go/bin
ENV GOPATH=/go
ENV PATH=$PATH:$GOPATH/bin
```

#### Java
```dockerfile
# Install Java (Temurin/AdoptOpenJDK)
RUN wget -O - https://packages.adoptium.net/artifactory/api/gpg/key/public | apt-key add - && \
    echo "deb https://packages.adoptium.net/artifactory/deb $(lsb_release -cs) main" | \
    tee /etc/apt/sources.list.d/adoptium.list && \
    apt-get update && \
    apt-get install -y temurin-17-jdk

ENV JAVA_HOME=/usr/lib/jvm/temurin-17-jdk-amd64
ENV PATH=$PATH:$JAVA_HOME/bin
```

### Container Tools

```dockerfile
# Install Docker
RUN curl -fsSL https://get.docker.com | sh

# Install Docker Compose
RUN curl -L "https://github.com/docker/compose/releases/download/v2.38.2/docker-compose-$(uname -s)-$(uname -m)" \
    -o /usr/local/bin/docker-compose && \
    chmod +x /usr/local/bin/docker-compose

# Install kubectl
RUN curl -LO "https://dl.k8s.io/release/$(curl -L -s https://dl.k8s.io/release/stable.txt)/bin/linux/amd64/kubectl" && \
    install -o root -g root -m 0755 kubectl /usr/local/bin/kubectl
```

### Cloud CLIs

```dockerfile
# Install AWS CLI
RUN curl "https://awscli.amazonaws.com/awscli-exe-linux-x86_64.zip" -o "awscliv2.zip" && \
    unzip awscliv2.zip && \
    ./aws/install && \
    rm -rf aws awscliv2.zip

# Install Azure CLI
RUN curl -sL https://aka.ms/InstallAzureCLIDeb | bash

# Install Google Cloud CLI
RUN echo "deb [signed-by=/usr/share/keyrings/cloud.google.gpg] https://packages.cloud.google.com/apt cloud-sdk main" | \
    tee -a /etc/apt/sources.list.d/google-cloud-sdk.list && \
    curl https://packages.cloud.google.com/apt/doc/apt-key.gpg | \
    apt-key --keyring /usr/share/keyrings/cloud.google.gpg add - && \
    apt-get update && apt-get install -y google-cloud-cli

# Install GitHub CLI
RUN curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg | \
    dd of=/usr/share/keyrings/githubcli-archive-keyring.gpg && \
    chmod go+r /usr/share/keyrings/githubcli-archive-keyring.gpg && \
    echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" | \
    tee /etc/apt/sources.list.d/github-cli.list && \
    apt-get update && \
    apt-get install -y gh
```

### Build Tools

```dockerfile
# Install build tools
RUN apt-get install -y \
    ant \
    maven \
    gradle

# Install additional tools
RUN apt-get install -y \
    jq \
    yq \
    rsync \
    zip \
    unzip \
    tar \
    gzip \
    bzip2 \
    xz-utils
```

### Testing Tools (Optional)

```dockerfile
# Install browsers and drivers (if needed for testing)
# Note: This is optional and adds significant size

# Install Chrome
RUN wget -q -O - https://dl-ssl.google.com/linux/linux_signing_key.pub | apt-key add - && \
    echo "deb [arch=amd64] http://dl.google.com/linux/chrome/deb/ stable main" > /etc/apt/sources.list.d/google-chrome.list && \
    apt-get update && \
    apt-get install -y google-chrome-stable

# Install ChromeDriver
RUN CHROME_DRIVER_VERSION=$(curl -sS chromedriver.storage.googleapis.com/LATEST_RELEASE) && \
    wget -O /tmp/chromedriver.zip https://chromedriver.storage.googleapis.com/${CHROME_DRIVER_VERSION}/chromedriver_linux64.zip && \
    unzip /tmp/chromedriver.zip -d /usr/local/bin/ && \
    rm /tmp/chromedriver.zip && \
    chmod +x /usr/local/bin/chromedriver
```

### Environment Configuration

```dockerfile
# Set environment variables to match runner
ENV DEBIAN_FRONTEND=noninteractive
ENV PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin

# Create common directories
RUN mkdir -p /github/workspace /github/home

# Set working directory
WORKDIR /github/workspace
```

### Complete Minimal Dockerfile Example

Here's a complete, minimal Dockerfile that covers the most common use cases:

```dockerfile
FROM ubuntu:24.04

# Avoid prompts from apt
ENV DEBIAN_FRONTEND=noninteractive
ENV TZ=UTC

# Update and install base packages
RUN apt-get update && apt-get upgrade -y && \
    apt-get install -y \
    ca-certificates \
    curl \
    wget \
    git \
    gnupg \
    lsb-release \
    software-properties-common \
    build-essential \
    cmake \
    make \
    jq \
    unzip \
    zip \
    tar \
    gzip \
    rsync \
    openssh-client && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/*

# Install Node.js 20.x
RUN curl -fsSL https://deb.nodesource.com/setup_20.x | bash - && \
    apt-get install -y nodejs && \
    npm install -g yarn npm@latest && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/*

# Install Python
RUN apt-get update && \
    apt-get install -y \
    python3 \
    python3-pip \
    python3-venv \
    python3-dev && \
    python3 -m pip install --upgrade pip setuptools wheel && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/*

# Install Docker
RUN curl -fsSL https://get.docker.com | sh && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/*

# Install GitHub CLI
RUN curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg | \
    dd of=/usr/share/keyrings/githubcli-archive-keyring.gpg && \
    chmod go+r /usr/share/keyrings/githubcli-archive-keyring.gpg && \
    echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" | \
    tee /etc/apt/sources.list.d/github-cli.list && \
    apt-get update && \
    apt-get install -y gh && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/*

# Set up working directory
RUN mkdir -p /github/workspace /github/home
WORKDIR /github/workspace

# Set environment variables
ENV PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
ENV GITHUB_WORKSPACE=/github/workspace
ENV HOME=/github/home

# Default command
CMD ["/bin/bash"]
```

### Build and Use

```bash
# Build the image
docker build -t gh-runner-mimic:ubuntu24 .

# Run interactively
docker run -it --rm \
  -v "$(pwd):/github/workspace" \
  gh-runner-mimic:ubuntu24

# Run with Docker socket (for Docker-in-Docker)
docker run -it --rm \
  -v "$(pwd):/github/workspace" \
  -v /var/run/docker.sock:/var/run/docker.sock \
  gh-runner-mimic:ubuntu24
```

## Key Differences from Runner

The GitHub Actions runner environment has several aspects that cannot be perfectly replicated in a Docker image:

### 1. GitHub Actions Context
- **Environment Variables**: The runner provides GitHub-specific context variables (`GITHUB_ACTOR`, `GITHUB_REPOSITORY`, `GITHUB_SHA`, etc.) that won't be available
- **Secrets**: GitHub Actions secrets are injected securely at runtime
- **GITHUB_TOKEN**: Automatic authentication token for GitHub API access
- **Action Inputs/Outputs**: The actions workflow communication mechanism

### 2. Pre-cached Dependencies
- The runner image has pre-downloaded and cached dependencies for faster builds
- Package manager caches (npm, pip, gem, go modules) are pre-warmed
- Docker layer caching is optimized differently

### 3. Service Configuration
- Some services (PostgreSQL, MySQL) are pre-installed but not running
- Service startup scripts and configuration may differ
- Network configuration for service containers differs

### 4. File System Layout
- The runner uses specific directory structures:
  - `/home/runner/work/{repo-name}/{repo-name}` for workspace
  - `/home/runner/work/_temp` for temporary files
  - `/home/runner/work/_actions` for action caches
- Permission models may differ

### 5. Tool Versions
- The runner image is updated regularly with new tool versions
- Your Docker image will need manual updates to stay current
- Some tools have specific patches or configurations in the runner image

### 6. Hardware & Resources
- The GitHub-hosted runner provides specific CPU/memory allocations
- GPU access is not available in standard runners
- Network bandwidth and external connectivity differ

### 7. Security Context
- GitHub Actions provides additional security isolation
- Secret handling and masking in logs
- OIDC token authentication for cloud providers

### Recommendations

1. **Target Specific Needs**: Don't try to replicate everything—focus on what your workflows actually use
2. **Test Differences**: Always test your Docker image against actual workflow requirements
3. **Use Official Images**: When possible, use official Docker images for languages/tools
4. **Regular Updates**: Keep your Docker image updated to match runner image updates
5. **Document Deviations**: Clearly document where your image differs from the runner
6. **Consider Act**: Use [nektos/act](https://github.com/nektos/act) for local GitHub Actions testing

## Maintenance Notes

- **Update Frequency**: The GitHub Actions runner image is typically updated weekly
- **Release Notes**: Check the [runner-images releases](https://github.com/actions/runner-images/releases) for changes
- **Breaking Changes**: Major version updates (e.g., Ubuntu 22.04 → 24.04) may include breaking changes
- **Deprecations**: GitHub announces runner image deprecations months in advance
- **This Document**: Should be refreshed quarterly or when significant runner updates occur

## Upcoming Changes & Announcements

As of the 20260119.4.1 image version (January 23, 2026), the following changes are noted:

1. **Docker Update (February 9, 2026)** - UPCOMING:
   - Docker Server and Client → version 29.1.x
   - Docker Compose → version 2.40.3
   - Source: [Announcement](https://github.com/actions/runner-images/issues/11457)

2. **Azure PowerShell Module (January 26, 2026)** - UPCOMING:
   - Azure PowerShell Module → version 14.6.0

3. **Recent Changes (as of January 2026)**:
   - Runner version updated to 2.331.0
   - Git updated to version 2.52.0
   - Various package updates and security patches

4. **Ubuntu 22.04 Deprecations** (completed):
   - Pre-cached Docker images removed
   - Additional Haskell (GHC) instances removed

5. **Android SDK Changes** (completed):
   - Android SDK platforms and build tools older than version 34 removed
   - Android NDK 26 removed; NDK 27 is now default

6. **Python Changes** (completed):
   - Python 3.9 removed
   - Python 3.12 is now the default on Windows images

For the latest announcements, see the [runner-images repository announcements](https://github.com/actions/runner-images#announcements).

## References

- **Runner Image Repository**: [actions/runner-images](https://github.com/actions/runner-images)
- **Documentation Source**: [Ubuntu2404-Readme.md (20260119)](https://github.com/actions/runner-images/blob/releases/ubuntu24/20260119/images/ubuntu/Ubuntu2404-Readme.md)
- **Ubuntu Documentation**: [Ubuntu 24.04 (Noble Numbat)](https://ubuntu.com/blog/tag/ubuntu-24-04-lts)
- **Docker Documentation**: [Docker Build Guide](https://docs.docker.com/build/)
- **GitHub Actions Documentation**: [Using GitHub-hosted runners](https://docs.github.com/en/actions/using-github-hosted-runners)
- **Act (Local Testing)**: [nektos/act](https://github.com/nektos/act)

---

*This document is automatically generated by the Ubuntu Actions Image Analyzer workflow. For the most current information, always refer to the official runner-images repository.*
