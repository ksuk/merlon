FROM rust:1.87-slim AS builder
RUN apt-get update && apt-get install -y --no-install-recommends protobuf-compiler && rm -rf /var/lib/apt/lists/*

# Mirror monorepo layout so build.rs relative paths resolve correctly
# build.rs references ../../../proto from engine/crates/merlon-engine/
WORKDIR /build/engine
COPY engine/Cargo.toml engine/Cargo.lock* ./
COPY engine/crates/ crates/
COPY proto/ /build/proto/

RUN cargo build --release

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY --from=builder /build/engine/target/release/merlon-engine .
EXPOSE 50051
ENTRYPOINT ["./merlon-engine"]
