# Etapa 1: compila el binario. El builder es un Go 1.22 Debian bookworm, igual
# que el runtime de la etapa 2, para no arrastrar sorpresas de glibc.
FROM golang:1.22-bookworm AS builder
WORKDIR /src

# Primero los módulos para aprovechar la caché de capas: solo cambian cuando
# cambian las dependencias (go.sum existe desde el memfd, Issue 7).
COPY go.mod go.sum ./
RUN go mod download

COPY . .
# CGO_ENABLED=0: binario estático, sin dependencias dinámicas en el runner.
# -trimpath: rutas reproduciblemente relativas dentro del binario.
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -o /extractor ./cmd/server

# Etapa 2: runtime mínimo. Solo poppler-utils (el motor) y ca-certificates.
FROM debian:bookworm-slim

RUN apt-get update \
    && apt-get install -y --no-install-recommends poppler-utils ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# Usuario no-root: el proceso no toca disco (Zero-Disk: el body vive en un
# memfd del kernel), así que no necesita más privilegios que ejecutar.
RUN useradd --system --uid 10001 --shell /usr/sbin/nologin appuser

COPY --from=builder /extractor /extractor

# El defecto local de la app es 8081 (§9); la imagen lo marca en 8080 para que
# un `docker run` suelto matchee el EXPOSE. docker-compose lo inyecta igual.
ENV HTTP_PORT=8080
ENV GOMAXPROCS=1

EXPOSE 8080

USER appuser

ENTRYPOINT ["/extractor"]