FROM rust:1.87.0-slim@sha256:437507c3e719e4f968033b88d851ffa9f5aceeb2dcc2482cc6cb7647811a55eb AS builder
RUN apt-get update && apt-get install -y --no-install-recommends protobuf-compiler && rm -rf /var/lib/apt/lists/*

# Mirror monorepo layout so build.rs relative paths resolve correctly
# build.rs references ../../../proto from engine/crates/merlon-engine/
WORKDIR /build/engine
COPY engine/Cargo.toml engine/Cargo.lock* ./
COPY engine/crates/ crates/
COPY proto/ /build/proto/

RUN cargo build --release

FROM debian:bookworm-slim@sha256:60eac759739651111db372c07be67863818726f754804b8707c90979bda511df
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY --from=builder /build/engine/target/release/merlon-engine .
EXPOSE 50051
ENTRYPOINT ["./merlon-engine"]
