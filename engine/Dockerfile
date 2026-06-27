FROM rust:1.87-slim AS builder
WORKDIR /build
COPY Cargo.toml Cargo.lock* ./
COPY crates/ crates/
RUN cargo build --release

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY --from=builder /build/target/release/merlon-engine .
EXPOSE 50051
ENTRYPOINT ["./merlon-engine"]
