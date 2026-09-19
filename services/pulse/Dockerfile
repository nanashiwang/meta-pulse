# syntax=docker/dockerfile:1

# Build context is the repo root so paths match the monorepo layout.
FROM golang:1.22-alpine AS builder
ARG META_PULSE_VERSION=dev
ARG META_PULSE_REVISION=unknown
WORKDIR /src

# go.sum is copied with a glob: it does not exist until the first dependency is
# added, and a bare `COPY go.sum` would break the build the moment Gin or GORM
# lands.
COPY services/pulse/go.mod services/pulse/go.sum* ./
RUN go mod download

COPY services/pulse/cmd ./cmd
COPY services/pulse/internal ./internal
COPY services/pulse/migrations ./migrations

RUN export CGO_ENABLED=0 GOOS=linux; \
    flags="-X github.com/nanashiwang/meta-pulse/internal/buildinfo.Version=${META_PULSE_VERSION} -X github.com/nanashiwang/meta-pulse/internal/buildinfo.Revision=${META_PULSE_REVISION}"; \
    go build -trimpath -ldflags "$flags" -o /out/meta-pulse-api ./cmd/api && \
    go build -trimpath -ldflags "$flags" -o /out/meta-pulse-worker ./cmd/worker && \
    go build -trimpath -ldflags "$flags" -o /out/meta-pulse-tool ./cmd/tool

FROM alpine:3.20
ARG META_PULSE_REVISION=unknown
ARG META_PULSE_VERSION=dev
LABEL org.opencontainers.image.revision=$META_PULSE_REVISION
LABEL org.opencontainers.image.version=$META_PULSE_VERSION
RUN apk add --no-cache ca-certificates tzdata
RUN addgroup -S pulse && adduser -S -G pulse pulse
RUN mkdir -p /app/runtime-keys && chown pulse:pulse /app/runtime-keys && chmod 700 /app/runtime-keys
WORKDIR /app
COPY --from=builder /out/ /usr/local/bin/
USER pulse

EXPOSE 8088 8089
ENTRYPOINT ["meta-pulse-api"]
