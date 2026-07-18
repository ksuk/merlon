FROM mcr.microsoft.com/devcontainers/base:bookworm@sha256:dd41e7f1a0b95eb3bc54857a3b8ffbe288e1012084de227343ef543eea8fdbff

RUN apt-get update && apt-get install -y --no-install-recommends \
    build-essential \
    ca-certificates \
    curl \
    git \
    libssl-dev \
    pkg-config \
    postgresql-client \
    && rm -rf /var/lib/apt/lists/*
