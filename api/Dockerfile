FROM node:22-alpine AS ui-builder
WORKDIR /ui
COPY ui/package.json ui/package-lock.json* ./
RUN npm ci
COPY ui/ .
RUN npm run build

FROM golang:1.26-alpine AS api-builder
WORKDIR /build
COPY api/go.mod api/go.sum* ./
RUN go mod download 2>/dev/null || true
COPY api/ .
RUN CGO_ENABLED=0 go build -o merlon-api ./cmd/merlon-api

FROM alpine:3.19
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=api-builder /build/merlon-api .
COPY --from=ui-builder /ui/dist ./ui
ENV MERLON_UI_DIR=/app/ui
EXPOSE 8080
ENTRYPOINT ["./merlon-api"]
