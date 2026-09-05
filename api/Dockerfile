# Both builder stages are pinned to BUILDPLATFORM so a multi-architecture build
# never runs a toolchain under emulation: the UI bundle is architecture-neutral,
# and Go cross-compiles to TARGETARCH natively (see api-builder below). Only the
# runtime stage varies per target architecture.
FROM --platform=$BUILDPLATFORM node:26.8.1-alpine3.24@sha256:2d984a15c9b54fd0aeb608b8e0d0d83529eb34d2966db27a1fb4f1edc3d298a3 AS ui-builder
WORKDIR /ui
COPY ui/package.json ui/package-lock.json* ./
RUN npm ci
COPY ui/ .
RUN npm run build

FROM --platform=$BUILDPLATFORM golang:1.27.0-alpine3.24@sha256:4c9fe60190a2a3350ddc51de80d0224b8a6698d12bdfc999fee45ea9d6c46dbc AS api-builder
WORKDIR /build
COPY api/go.mod api/go.sum* ./
# Not suppressed: a checksum mismatch, a proxy outage, or a GOSUMDB failure has
# to stop the build here rather than surface later as a confusing `go build`
# error in the next layer -- or, for docker-compose.dev.yml, as a container
# that needs network at start-up because the download never happened.
RUN go mod download
COPY api/ .
ARG VERSION=dev
ARG REVISION=
ARG BUILT_AT=
# TARGETARCH is supplied by BuildKit. CGO is already disabled, so setting GOARCH
# is all a cross-build needs and no C toolchain or emulator is involved.
ARG TARGETARCH
RUN CGO_ENABLED=0 GOARCH=${TARGETARCH:-} go build -ldflags "-X github.com/ksuk/merlon/api/internal/buildinfo.Version=$VERSION -X github.com/ksuk/merlon/api/internal/buildinfo.Commit=$REVISION -X github.com/ksuk/merlon/api/internal/buildinfo.BuiltAt=$BUILT_AT" -o merlon-api ./cmd/merlon-api
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
#
# Unlike merlon-api above, demogen is deliberately not cross-compiled: it is
# executed during the build, so it has to be a build-platform binary. Its
# output is JSON and therefore identical whatever architecture produced it.
COPY content/ /content
RUN CGO_ENABLED=0 go build -o merlon-demogen ./cmd/merlon-demogen && \
    mkdir -p /build/demo-data && \
    ./merlon-demogen -out /build/demo-data

# Everything the runtime image needs beyond the binary is produced here, on the
# build platform, so the runtime stage below contains no RUN at all. A RUN in a
# stage built for a foreign architecture would need binfmt/QEMU on the builder;
# keeping the runtime stage instruction-free means the multi-architecture build
# is pure cross-compilation and cannot fail because a runner lacks emulation.
# CA certificates and the passwd/group entries are architecture-independent
# text, and this stage uses the same pinned Alpine as the runtime stage.
FROM --platform=$BUILDPLATFORM alpine:3.24.1@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b AS rootfs
RUN apk add --no-cache ca-certificates && \
    addgroup -g 10001 -S merlon && \
    adduser -u 10001 -S -G merlon -H -s /sbin/nologin merlon

FROM alpine:3.24.1@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b AS runtime-base
COPY --from=rootfs /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
# A fixed uid/gid rather than a name, so ownership is predictable regardless of
# the host's user database.
COPY --from=rootfs /etc/passwd /etc/passwd
COPY --from=rootfs /etc/group /etc/group

WORKDIR /app
COPY --from=api-builder --chown=root:root /build/merlon-api .
COPY --from=ui-builder --chown=root:root /ui/dist ./ui
# The API's production defaults resolve these policies relative to /app.
# Copy only root-level YAML: sample presets and demo content belong exclusively
# to the demo target below.
COPY --from=api-builder --chown=root:root /content/*.yaml ./content/
COPY --chmod=0555 api/healthcheck.sh /usr/local/bin/merlon-healthcheck
ENV MERLON_UI_DIR=/app/ui
EXPOSE 8080

# OCI annotations. `source` and `revision` are what let a consumer tie a pulled
# image back to the commit its provenance attestation was issued for, so the
# release workflow passes REVISION; local builds leave it empty rather than
# claiming a commit they cannot prove.
ARG VERSION=dev
ARG REVISION=
LABEL org.opencontainers.image.title="Merlon" \
      org.opencontainers.image.description="Self-hosted AML/CFT compliance software: CDD scoring and transaction monitoring" \
      org.opencontainers.image.source="https://github.com/ksuk/merlon" \
      org.opencontainers.image.documentation="https://github.com/ksuk/merlon/blob/main/docs/index.md" \
      org.opencontainers.image.licenses="BUSL-1.1" \
      org.opencontainers.image.vendor="Keisuke Kuribara" \
      org.opencontainers.image.version="$VERSION" \
      org.opencontainers.image.revision="$REVISION" \
      io.github.ksuk.merlon.governance.mode="single-maintainer" \
      io.github.ksuk.merlon.governance.independent-approval="false" \
      io.github.ksuk.merlon.governance.separation-of-duties="false" \
      io.github.ksuk.merlon.governance.adr="ADR-0016"

# merlon-api writes nothing to the filesystem at runtime — configuration is
# environment-driven and all state lives in PostgreSQL — so /app stays owned by
# root and readable-only by the service account. This image also runs
# unmodified with `--read-only`.
USER 10001:10001

# The image-level healthcheck asserts liveness only: the process is up and
# serving. Readiness — initial setup complete, database reachable, engine
# loaded — stays at /healthz/ready for orchestrators and compose files that
# deliberately gate on it. A readiness-based healthcheck here would mark every
# fresh deployment unhealthy until initial setup, and an orchestrator that
# restarts unhealthy containers would then kill the instance before anyone
# could create the first administrator account.
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
  CMD ["/usr/local/bin/merlon-healthcheck"]

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
