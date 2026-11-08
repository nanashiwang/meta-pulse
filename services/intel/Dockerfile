# syntax=docker/dockerfile:1

# Build context is the repo root, matching services/pulse.
FROM golang:1.22-alpine AS builder
WORKDIR /src

COPY services/intel/go.mod services/intel/go.sum ./
RUN go mod download

COPY services/intel/cmd ./cmd
COPY services/intel/internal ./internal
COPY services/intel/migrations ./migrations

RUN export CGO_ENABLED=0 GOOS=linux; \
    go build -trimpath -o /out/metar-intel-api ./cmd/api && \
    go build -trimpath -o /out/metar-intel-worker ./cmd/worker && \
    go build -trimpath -o /out/metar-intel-tool ./cmd/tool

FROM alpine:3.20
ARG META_PULSE_REVISION=unknown
ARG META_PULSE_VERSION=dev
LABEL org.opencontainers.image.revision=$META_PULSE_REVISION
LABEL org.opencontainers.image.version=$META_PULSE_VERSION
RUN apk add --no-cache ca-certificates tzdata
RUN addgroup -S intel && adduser -S -G intel intel
WORKDIR /app
COPY --from=builder /out/ /usr/local/bin/
USER intel

EXPOSE 8090
ENTRYPOINT ["metar-intel-api"]
