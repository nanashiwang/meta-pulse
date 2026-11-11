# Meta Pulse forum: Apache Answer + the Meta Pulse user center plugin.
#
# Answer's source is NOT vendored. Go plugins must be compiled into the binary
# (Go has no dynamic plugin loading), so we rebuild Answer with our plugin via
# `answer build --with`. Upstream stays a dependency, never a fork.

FROM apache/answer:1.7.1 AS answer-base

FROM golang:1.25-alpine AS builder

# Answer v1.7.1 declares pnpm@9.7.0 in ui/package.json. Installing the
# floating latest pnpm makes old Answer releases depend on future package
# manager behaviour and currently fails its pre-install identity check.
ARG PNPM_VERSION=9.7.0
RUN apk add --no-cache build-base git bash nodejs npm patch python3 \
    && npm install -g "pnpm@${PNPM_VERSION}" \
    && test "$(pnpm --version)" = "${PNPM_VERSION}"

COPY --from=answer-base /usr/bin/answer /usr/bin/answer

# Build against the pinned Answer source so the community session policy is
# explicit, reviewable, and kept in sync with the runtime binary.
RUN mkdir -p /tmp/answer-source /build \
    && printf 'module answer-source\ngo 1.25\n' > /tmp/answer-source/go.mod \
    && cd /tmp/answer-source \
    && go mod download github.com/apache/answer@v1.7.1 \
    && ANSWER_DIR="$(go env GOPATH)/pkg/mod/github.com/apache/answer@v1.7.1" \
    && cp -r "$ANSWER_DIR" /build/answer \
    && chmod -R u+w /build/answer
COPY services/forum/answer-session-retention.patch /build/answer-session-retention.patch
COPY services/forum/answer-optional-admin-email.patch /build/answer-optional-admin-email.patch
COPY services/forum/answer-admin-user-list.patch /build/answer-admin-user-list.patch
COPY services/forum/answer-admin-privileges-compact.patch /build/answer-admin-privileges-compact.patch
COPY services/forum/answer-mail-errors.patch /build/answer-mail-errors.patch
RUN cd /build/answer \
    && patch -p1 --fuzz=0 < /build/answer-session-retention.patch \
    && patch -p1 --fuzz=0 < /build/answer-optional-admin-email.patch \
    && patch -p1 --fuzz=0 < /build/answer-admin-user-list.patch \
    && patch -p1 --fuzz=0 < /build/answer-admin-privileges-compact.patch \
    && patch -p1 --fuzz=0 < /build/answer-mail-errors.patch
ENV ANSWER_MODULE=/build/answer

WORKDIR /build
COPY services/forum-plugin/user-center-pulse ./user-center-pulse
COPY services/forum-plugin/captcha-turnstile ./captcha-turnstile

# Fail before the expensive Answer build if vendor would drop a UI dependency.
COPY services/forum/verify_frontend.py /build/services/forum/verify_frontend.py
RUN mkdir -p /build/services/forum-plugin \
    && ln -s /build/user-center-pulse /build/services/forum-plugin/user-center-pulse \
    && ln -s /build/captcha-turnstile /build/services/forum-plugin/captcha-turnstile \
    && python3 /build/services/forum/verify_frontend.py

# Build from the local plugin source rather than a published module, so the
# forum image always matches this commit.
RUN answer build \
    --with github.com/nanashiwang/meta-pulse/services/forum-plugin/user-center-pulse@v0.1.0=/build/user-center-pulse \
    --with github.com/nanashiwang/meta-pulse/services/forum-plugin/captcha-turnstile@v0.1.0=/build/captcha-turnstile \
    --output /out/answer

COPY services/forum/verify_binary.py /build/services/forum/verify_binary.py
RUN python3 /build/services/forum/verify_binary.py /out/answer

RUN cd /build/user-center-pulse && CGO_ENABLED=0 go build -o /out/forum-config ./cmd/forum-config

FROM alpine:3.20
ARG META_PULSE_REVISION=unknown
ARG META_PULSE_VERSION=dev
LABEL org.opencontainers.image.revision=$META_PULSE_REVISION
LABEL org.opencontainers.image.version=$META_PULSE_VERSION
RUN apk add --no-cache ca-certificates tzdata
COPY --from=answer-base /data /data
COPY --from=builder /out/answer /usr/bin/answer
COPY --from=builder /out/forum-config /usr/bin/forum-config
COPY services/forum/entrypoint.sh /usr/bin/metar-forum-entrypoint
RUN chmod 755 /usr/bin/metar-forum-entrypoint

VOLUME /data
EXPOSE 80
ENTRYPOINT ["/usr/bin/metar-forum-entrypoint"]
