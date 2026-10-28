# syntax=docker/dockerfile:1

# Extractor: necesita poppler-utils, así que el runtime es debian-slim (no distroless).
# Se compensa recortando privilegios: usuario sin shell ni home, binario de solo lectura
# y sin binarios setuid/setgid.
ARG GO_VERSION=1.27

# ---------- Build ----------
FROM golang:${GO_VERSION}-bookworm AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download && go mod verify

# Requiere .dockerignore (.git, .env, tests, docs) para no copiar basura ni secretos.
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux \
    go build -trimpath -buildvcs=false -ldflags="-s -w" -o /out/extractor ./cmd/server

# ---------- Runtime ----------
FROM debian:bookworm-slim

ARG VERSION=dev
LABEL org.opencontainers.image.title="pdf-extractext-extractor" \
      org.opencontainers.image.description="Worker de extracción de texto (poppler)" \
      org.opencontainers.image.source="https://github.com/Zindorg/pdf-extractext-extractor" \
      org.opencontainers.image.version="${VERSION}"

RUN apt-get update \
    && apt-get install -y --no-install-recommends poppler-utils ca-certificates \
    && rm -rf /var/lib/apt/lists/* \
    # Quita setuid/setgid: ningún binario del runtime puede escalar privilegios.
    && find / -xdev -perm /6000 -type f -exec chmod a-s {} + 2>/dev/null || true \
    # Usuario de sistema sin shell ni home.
    && groupadd --system --gid 10001 appuser \
    && useradd --system --uid 10001 --gid 10001 --no-create-home \
               --home-dir /nonexistent --shell /usr/sbin/nologin appuser

COPY --from=build --chown=root:root --chmod=0555 /out/extractor /extractor

ENV HTTP_PORT=8080 \
    GOMAXPROCS=1 \
    HOME=/nonexistent

# Zero-Disk: el documento vive en un memfd, no se escribe nada en el filesystem.
USER 10001:10001

EXPOSE 8080
ENTRYPOINT ["/extractor"]