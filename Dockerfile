# syntax=docker/dockerfile:1.12

FROM --platform=$BUILDPLATFORM golang:1.27.1-alpine@sha256:cf6fca6641884b8433441b2b0652976f975e1d0fdd26d177eaaf8596087f3125 AS build

RUN apk add --no-cache ca-certificates

WORKDIR /src
COPY go.mod go.sum ./

RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY cmd/rule-bot-client ./cmd/rule-bot-client
COPY internal/client ./internal/client

ARG TARGETOS
ARG TARGETARCH
ARG TARGETVARIANT
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_DATE=unknown

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    set -eu; \
    export CGO_ENABLED=0 GOOS="$TARGETOS" GOARCH="$TARGETARCH"; \
    if [ "$TARGETARCH" = "arm" ]; then export GOARM="${TARGETVARIANT#v}"; fi; \
    go build -buildvcs=false -trimpath \
      -ldflags="-s -w -buildid= \
        -X github.com/Aethersailor/Rule-Bot-Client/internal/client.BuildVersion=$VERSION \
        -X github.com/Aethersailor/Rule-Bot-Client/internal/client.BuildCommit=$COMMIT \
        -X github.com/Aethersailor/Rule-Bot-Client/internal/client.BuildDate=$BUILD_DATE" \
      -o /out/rule-bot-client ./cmd/rule-bot-client

FROM scratch

ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_DATE=unknown

LABEL org.opencontainers.image.title="Rule-Bot Client" \
      org.opencontainers.image.description="Lightweight final MATCH-rule domain collector" \
      org.opencontainers.image.source="https://github.com/Aethersailor/Rule-Bot-Client" \
      org.opencontainers.image.licenses="AGPL-3.0-only" \
      org.opencontainers.image.version="$VERSION" \
      org.opencontainers.image.revision="$COMMIT" \
      org.opencontainers.image.created="$BUILD_DATE"

COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build --chown=10001:10001 /out/rule-bot-client /rule-bot-client

USER 10001:10001
WORKDIR /data
ENTRYPOINT ["/rule-bot-client"]
CMD ["--config", "/data/config.json"]
