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

FROM alpine:3.24.1@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=api-builder /build/merlon-api .
COPY --from=ui-builder /ui/dist ./ui
ENV MERLON_UI_DIR=/app/ui
EXPOSE 8080
ENTRYPOINT ["./merlon-api"]
