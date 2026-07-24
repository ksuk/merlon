FROM mcr.microsoft.com/devcontainers/base:bookworm@sha256:bb7b81b6e5be17b5267f92f4ffda534fea37dab1df97b5e86c1f9b91da5c0b5d

RUN apt-get update && apt-get install -y --no-install-recommends \
    build-essential \
    ca-certificates \
    curl \
    git \
    libssl-dev \
    pkg-config \
    postgresql-client \
    && rm -rf /var/lib/apt/lists/*
