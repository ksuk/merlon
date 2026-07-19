FROM node:22.23.1-alpine3.24@sha256:16e22a550f3863206a3f701448c45f7912c6896a62de43add43bb9c86130c3e2 AS ui-builder
WORKDIR /ui
COPY ui/package.json ui/package-lock.json* ./
RUN npm ci
COPY ui/ .
RUN npm run build

FROM golang:1.25.12-alpine3.24@sha256:56961d79ea8129efddcc0b8643fd8a5416b4e6228cfd477e3fd61deb2672c587 AS api-builder
WORKDIR /build
COPY api/go.mod api/go.sum* ./
RUN go mod download 2>/dev/null || true
COPY api/ .
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -ldflags "-X main.version=$VERSION" -o merlon-api ./cmd/merlon-api
# merlon-demogen is only used here, to produce the demo dataset copied into
# the "demo" target below (PH7 T2). It is deterministic (fixed default
# seed/anchor, see api/internal/demogen), so this reproduces the same
# dataset `make demogen` writes to deploy/seed/demo/ for local (non-Docker)
# use. Neither the demogen binary itself nor its Go sources are copied into
# any runtime image.
#
# demogen's CDD/TM/country-risk config defaults are the relative path
# "../content/_sample/..." (api/internal/demogen/demogen.go), i.e. it
# expects to run from a directory whose parent has a content/ sibling, the
# same layout `make demogen` gets by running from api/ in the checked-out
# repo. /content below (copied from the build context's top-level content/,
# not the api/ subtree COPY above) reproduces that sibling relationship for
# WORKDIR /build.
COPY content/ /content
RUN CGO_ENABLED=0 go build -o merlon-demogen ./cmd/merlon-demogen && \
    mkdir -p /build/demo-data && \
    ./merlon-demogen -out /build/demo-data

FROM alpine:3.24.1@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b AS runtime-base
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=api-builder /build/merlon-api .
COPY --from=ui-builder /ui/dist ./ui
ENV MERLON_UI_DIR=/app/ui
EXPOSE 8080
ENTRYPOINT ["./merlon-api"]

# demo is a data-only target on top of the standard runtime image: it adds
# the generated demo dataset (deploy/seed/demo/*.json equivalent) at
# /app/demo-data so docker-compose.demo.yml can point MERLON_DEMO_DATA_DIR at
# it (api/internal/seed's JSON loader, PH7 T2). No extra env vars or
# binaries are added here — the standard `runtime` target's size and
# contents are unaffected by this stage's existence.
FROM runtime-base AS demo
COPY --from=api-builder /build/demo-data /app/demo-data

# Keep the standard runtime as the final/default stage so plain docker build
# and docker-compose.yml do not include the demo dataset.
FROM runtime-base AS runtime
