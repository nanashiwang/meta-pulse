FROM golang:1.25-alpine AS build
WORKDIR /src
COPY services/dnsprobe/go.mod services/dnsprobe/go.sum ./
RUN go mod download
COPY services/dnsprobe/ ./
RUN CGO_ENABLED=0 go build -trimpath -o /out/dnsprobe ./cmd/api
FROM alpine:3.20
RUN addgroup -S probe && adduser -S -G probe probe
COPY --from=build /out/dnsprobe /usr/local/bin/dnsprobe
USER probe
EXPOSE 8091 5353/tcp 5353/udp
ENTRYPOINT ["dnsprobe"]
