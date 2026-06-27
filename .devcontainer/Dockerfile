FROM ubuntu:24.04

ARG GO_VERSION=1.22.4
ARG NODE_MAJOR=22
ARG BUF_VERSION=1.34.0

ENV DEBIAN_FRONTEND=noninteractive \
    PATH=/usr/local/go/bin:/root/.cargo/bin:/usr/local/bin:$PATH

# Base tooling and PostgreSQL client.
RUN apt-get update && apt-get install -y --no-install-recommends \
        ca-certificates \
        curl \
        git \
        make \
        build-essential \
        pkg-config \
        libssl-dev \
        postgresql-client \
        gnupg \
    && rm -rf /var/lib/apt/lists/*

# Go.
RUN curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz" -o /tmp/go.tgz \
    && tar -C /usr/local -xzf /tmp/go.tgz \
    && rm /tmp/go.tgz

# Rust (stable, via rustup).
RUN curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y --default-toolchain stable

# Node.js (NodeSource).
RUN curl -fsSL "https://deb.nodesource.com/setup_${NODE_MAJOR}.x" | bash - \
    && apt-get install -y --no-install-recommends nodejs \
    && rm -rf /var/lib/apt/lists/*

# buf CLI.
RUN curl -fsSL "https://github.com/bufbuild/buf/releases/download/v${BUF_VERSION}/buf-Linux-x86_64" \
        -o /usr/local/bin/buf \
    && chmod +x /usr/local/bin/buf

# Non-root development user expected by devcontainer.json.
RUN useradd -m -s /bin/bash vscode

WORKDIR /workspace
