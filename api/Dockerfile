FROM golang:1.25-alpine AS builder
WORKDIR /build
COPY go.mod go.sum* ./
RUN go mod download 2>/dev/null || true
COPY . .
RUN CGO_ENABLED=0 go build -o merlon-api ./cmd/merlon-api

FROM alpine:3.19
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=builder /build/merlon-api .
EXPOSE 8080
ENTRYPOINT ["./merlon-api"]
