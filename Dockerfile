# syntax=docker/dockerfile:1.7

FROM golang:1.27.0-bookworm AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .

ARG TARGETOS=linux
ARG TARGETARCH=amd64
RUN --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o /out/error-tracer ./cmd/error-tracer && \
    mkdir -p /out/data

FROM scratch

LABEL org.opencontainers.image.source="https://github.com/soulteary/Error-Tracer" \
      org.opencontainers.image.licenses="Apache-2.0" \
      org.opencontainers.image.title="Error-Tracer"

COPY --from=build --chown=65532:65532 /out/error-tracer /error-tracer
COPY --from=build --chown=65532:65532 /out/data /data
COPY --from=build /src/LICENSE /licenses/Error-Tracer/LICENSE
COPY --from=build /src/NOTICE /licenses/Error-Tracer/NOTICE

USER 65532:65532
WORKDIR /data

ENV ERROR_TRACER_ADDRESS=:8080 \
    ERROR_TRACER_DATABASE_PATH=/data/error-tracer.db

EXPOSE 8080
STOPSIGNAL SIGTERM
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 CMD ["/error-tracer", "healthcheck"]

ENTRYPOINT ["/error-tracer"]
