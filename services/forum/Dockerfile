# Meta Pulse forum: Apache Answer + the Meta Pulse user center plugin.
#
# Answer's source is NOT vendored. Go plugins must be compiled into the binary
# (Go has no dynamic plugin loading), so we rebuild Answer with our plugin via
# `answer build --with`. Upstream stays a dependency, never a fork.

FROM apache/answer:1.7.1 AS answer-base

FROM golang:1.25-alpine AS builder

RUN apk add --no-cache build-base git bash nodejs npm && npm install -g pnpm

COPY --from=answer-base /usr/bin/answer /usr/bin/answer

WORKDIR /build
COPY services/forum-plugin/user-center-pulse ./user-center-pulse

# Build from the local plugin source rather than a published module, so the
# forum image always matches this commit.
RUN answer build \
    --with github.com/nanashiwang/meta-pulse/services/forum-plugin/user-center-pulse@v0.1.0=/build/user-center-pulse \
    --output /out/answer

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata
COPY --from=answer-base /data /data
COPY --from=builder /out/answer /usr/bin/answer

VOLUME /data
EXPOSE 80
ENTRYPOINT ["/usr/bin/answer", "run", "-C", "/data/conf"]
