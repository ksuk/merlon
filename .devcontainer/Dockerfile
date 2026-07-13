FROM mcr.microsoft.com/devcontainers/base:bookworm@sha256:dd41e7f1a0b95eb3bc54857a3b8ffbe288e1012084de227343ef543eea8fdbff

RUN apt-get update && apt-get install -y --no-install-recommends \
    build-essential \
    ca-certificates \
    curl \
    git \
    libssl-dev \
    pkg-config \
    postgresql-client \
    protobuf-compiler \
    && rm -rf /var/lib/apt/lists/*

ARG BUF_VERSION=1.34.0
RUN arch="$(dpkg --print-architecture)" && \
    case "$arch" in \
      amd64) buf_arch="x86_64" ;; \
      arm64) buf_arch="arm64" ;; \
      *) echo "unsupported architecture: $arch" && exit 1 ;; \
    esac && \
    curl -fsSL "https://github.com/bufbuild/buf/releases/download/v${BUF_VERSION}/buf-Linux-${buf_arch}" \
      -o /usr/local/bin/buf && \
    chmod +x /usr/local/bin/buf
