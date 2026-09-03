# syntax=docker/dockerfile:1

FROM golang:1.22-alpine AS builder
WORKDIR /src

COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal

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
