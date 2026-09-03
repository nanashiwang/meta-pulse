# syntax=docker/dockerfile:1

# Build context is the repo root so paths match the monorepo layout.
FROM golang:1.22-alpine AS builder
WORKDIR /src

# go.sum is copied with a glob: it does not exist until the first dependency is
# added, and a bare `COPY go.sum` would break the build the moment Gin or GORM
# lands.
COPY services/pulse/go.mod services/pulse/go.sum* ./
RUN go mod download

COPY services/pulse/cmd ./cmd
COPY services/pulse/internal ./internal

RUN CGO_ENABLED=0 GOOS=linux go build -o /out/meta-pulse-api ./cmd/api && \
    CGO_ENABLED=0 GOOS=linux go build -o /out/meta-pulse-worker ./cmd/worker && \
    CGO_ENABLED=0 GOOS=linux go build -o /out/meta-pulse-tool ./cmd/tool

FROM alpine:3.20
RUN addgroup -S pulse && adduser -S -G pulse pulse
WORKDIR /app
COPY --from=builder /out/ /usr/local/bin/
USER pulse

EXPOSE 8088
ENTRYPOINT ["meta-pulse-api"]
