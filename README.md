# pdf-extractext-extractor

Microservicio Go 1.22 para extracción de texto desde PDFs. Diseñado como *worker* interno en arquitectura **Hub & Spoke**: expone `POST /extract` síncrono, cumple contrato estricto [`docs/contract/workers.md`](docs/contract/workers.md) §Extractor, y prioriza resiliencia, alta concurrencia controlada y **Zero-Disk I/O**.

---

## 1. Descripción General

**pdf-extractext-extractor** recibe un PDF vía `multipart/form-data`, extrae su texto con `pdftotext` (poppler) y devuelve el resultado en JSON. No escribe en disco, no hace peticiones salientes y no conoce a otros workers (Orquestador, Resumidor, Persistidor). Más información en [docs/README.md](docs/README.md).

**Principios rectores:**
- **Resiliencia estructural:** nunca colapsa por OOM ni acumula *requests zombis* bajo estrés extremo.
- **Alta concurrencia controlada:** admisión por semáforo + cola acotada implementada con canales de Go (zero-allocation en el *fast path*).
- **Zero-Disk I/O:** el documento viaja *socket → kernel RAM (memfd) → pdftotext* sin tocar filesystem ni heap de Go.
- **Contrato HTTP inmutable:** códigos de estado, *payloads* y enumeración `reason` fijos (RFC 9457 / `workers.md §Extractor`).

---

## 2. Arquitectura y Optimizaciones

### Clean Architecture (Puertos y Adaptadores)

```
┌─────────────────────────────────────────────────────────────────┐
│ CAPA 1 · TRANSPORTE          internal/api                      │
│ net/http · handler POST /extract · DTOs · RFC 9457             │
└────────────────────────────────────┬────────────────────────────┘
                                     │ invoca el caso de uso
┌────────────────────────────────────▼────────────────────────────┐
│ CAPA 2 · APLICACIÓN        internal/application                │
│ extract_service.go · slots.go (AdmissionControl)               │
└────────────────────────────────────┬────────────────────────────┘
                                     │ usa el PUERTO
┌────────────────────────────────────▼────────────────────────────┐
│ CAPA 3 · DOMINIO           internal/domain                     │
│ ExtractionResult · Checksum SHA-256 · PageCount · Warning      │
│ 13 Motivos de fallo (errores sentinela) · puertos DocumentSource/TextExtractor
└────────────────────────────────────┬────────────────────────────┘
                                     │ implementan
┌────────────────────────────────────▼────────────────────────────┐
│ CAPA 4 · ADAPTADORES       internal/adapters                   │
│  ram/buffer.go     → memfd_create, relectible, sin ruta        │
│  poppler/          → pdfinfo + pdftotext vía os/exec, muerte por grupo
└─────────────────────────────────────────────────────────────────┘
```

**Regla de dependencia:** todo apunta hacia adentro. El dominio no sabe qué es `poppler`, ni `memfd`, ni HTTP. `cmd/server/main.go` es el *composition root* y construye de fuera hacia dentro.

---

### Zero-Disk I/O (memfd)

El adaptador **`internal/adapters/ram/buffer.go`** materializa el cuerpo entrante en un **descriptor anónimo del kernel** (`memfd_create`):

```go
descriptor, _ := unix.MemfdCreate("pdf_buffer", 0)
file := os.NewFile(uintptr(descriptor), "pdf_buffer")
io.Copy(file, requestBody)   // socket → RAM, cero heap
file.Seek(0, io.SeekStart)   // rebobina para relectura
```

**Propiedades críticas:**
- **Cero syscalls de disco:** no hay `open(O_CREAT)`, no hay `write` a tmpfs.
- **Relectura segura:** `MemfdSource.Open()` hace `Seek(0) + Dup()` → cada consumidor (`pdfinfo`, `pdftotext`) recibe su propio descriptor con offset independiente.
- **Limpieza automática:** el *file descriptor* muere cuando el handler cierra el archivo (`defer file.Close()`); el kernel recupera la memoria al instante.
- **Medición real:** 20 MiB ingeridos → **+73 KiB** de `HeapAlloc` en Go.

---

### Control de Admisión / Backpressure (AdmissionControl)

`internal/application/slots.go` implementa un **semáforo bloqueante con cola acotada** usando **un único canal bufferizado** (`capacity + maxQueue`):

```go
type AdmissionControl struct {
    slots    chan struct{}  // cap = MaxInFlight + MaxQueue
    capacity int            // MaxInFlight
    maxQueue int            // MAX_QUEUE
}
```

**Algoritmo `AcquireSlot(ctx)`:**
1. **Fast path (no bloqueo):** `select { case slots <- {}: return nil; default: }` — si `len(slots) < capacity`, concede hueco inmediato.
2. **Cola llena:** si `maxQueue == 0` → `ErrResourceExhausted` (HTTP 429) *inmediato*.
3. **Slow path (bloqueo con timeout):** `select { case slots <- {}: return nil; case <-ctx.Done(): return ErrQueueTimeout }` — la petición espera en el canal hasta que un `ReleaseSlot()` libere hueco o expire `QUEUE_WAIT_TIMEOUT`.

**Ventajas:**
- **Cero contención atómica:** un solo canal modela huecos + espera.
- **Fairness natural:** FIFO por semántica de canales de Go.
- **Fail-fast configurable:** `MAX_QUEUE=0` rechaza al instante; `MAX_QUEUE>0` absorbe picos.
- **Liberación garantizada:** `defer admission.ReleaseSlot()` en `ExtractService.Process` — incluso si la extracción hace *panic* o el contexto expira, el hueco vuelve al pool.

---

### Aislamiento de procesos (poppler)

`internal/adapters/poppler/process.go` ejecuta `pdftotext` en su **propio grupo de procesos** (`Setpgid: true`):

```go
cmd := exec.CommandContext(ctx, "pdftotext", "-", "-")
cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
```

- Si el contexto expira (deadline de extracción), **mata al grupo entero** (padre + cualquier hijo que poppler pueda haber lanzado).
- En builds `!linux` (tests locales) degrada a `CommandContext` estándar.

---

## 3. Variables de Entorno

Toda la configuración llega por entorno (**12-Factor**); un valor inválido **impide el arranque** (`config.LoadFromEnv` valida en la frontera).

| Variable | Defecto | Descripción |
|---|---|---|
| `HTTP_PORT` | `8081` | Puerto de escucha HTTP. |
| `MAX_IN_FLIGHT` | `0` → `runtime.NumCPU()` | Huecos de extracción simultáneos (*workers*). `0` = auto = nº de CPUs. |
| `MAX_QUEUE` | `128` | Capacidad de la cola de espera. `0` = sin cola, rechazo inmediato (429). |
| `QUEUE_WAIT_TIMEOUT` | `10s` | Tiempo máx. que una petición espera en cola antes de `503 QUEUE_TIMEOUT`. |
| `EXTRACT_TIMEOUT` | `6s` | Tiempo máx. de la extracción pura (`pdftotext`) tras obtener hueco. Excedido → `504 EXTRACTION_TIMEOUT`. |
| `SHUTDOWN_GRACE` | `25s` | Margen para terminar extracciones en curso al recibir `SIGTERM`. |
| `MAX_DOCUMENT_BYTES` | `20 MiB` | Límite de cuerpo entrante (salvaguarda + `http.MaxBytesReader`). |

### Límite elástico del servidor

`MAX_IN_FLIGHT` fija la concurrencia real de CPU (procesos `pdftotext` vivos). `MAX_QUEUE` + `QUEUE_WAIT_TIMEOUT` definen la **capacidad de absorción de picos** sin rechazar. La combinación permite dos perfiles operativos:

| Perfil | `MAX_IN_FLIGHT` | `MAX_QUEUE` | `QUEUE_WAIT_TIMEOUT` | Uso |
|---|---|---|---|---|
| **Sostenido (Vegeta 50 req/s)** | 2 | 15 | 5s | Fail-fast estricto → protege CPU, rechaza controlado (503). |
| **Pico (k6 100 VUs)** | 2 | 50 | 25s | Backpressure tolerante → absorbe burst, 98 % éxito, throughput ~16 req/s (límite físico CPU). |

---

## 4. Referencia de la API

### Endpoints

| Método | Ruta | Descripción |
|---|---|---|
| `GET` | `/health` | Latido del proceso: `200 {"status":"ok"}` |
| `POST` | `/extract` | Extrae texto de un PDF (multipart/form-data) |

---

### `POST /extract`

**Content-Type:** `multipart/form-data`  
**Campos requeridos:**

| Campo | Tipo | Descripción |
|---|---|---|
| `document` | `file` (binary) | Bytes del PDF. |
| `mime_type` | `string` | Debe ser `application/pdf` (validado por el orquestador en puerta 1). |

**Límites:** cuerpo cortado en `MAX_DOCUMENT_BYTES` (por defecto 20 MiB) vía `http.MaxBytesReader`.

---

#### ✅ 200 OK — `application/json`

```json
{
  "extracted_text": "Texto completo extraído…",
  "extraction_time_ms": 342,
  "warnings": [
    { "code": "low_text_content", "message": "extracted text unusually short for page count" }
  ]
}
```

- `extracted_text`: texto normalizado (saltos de página `\f` → párrafo en blanco).
- `extraction_time_ms`: **solo** el tiempo de `pdftotext` (excluye espera en cola).
- `warnings`: array (nunca `null`); vacío si no hay avisos.

---

#### ❌ 422 Unprocessable Entity — `application/json`

```json
{ "reason": "empty", "message": "extracted text is empty" }
```

| `reason` (enum) | Causa |
|---|---|
| `empty` | PDF sin capa de texto extraíble (salida vacía). |
| `encrypted_pdf` | PDF cifrado/DRM genérico (contraseña vacía no cuenta). |
| `password_protected` | PDF que exige contraseña de usuario/propietario. |
| `corrupt` | PDF dañado (syntax error, trailer dictionary missing, etc.). |
| `unsupported_features` | Características no soportadas por poppler (Type 3 fonts, etc.). |

> **Nota:** un `reason` fuera de este enum **nunca** produce 422; se traduce a `503 extractor_unavailable` (ver abajo).

---

#### ❌ 504 Gateway Timeout — `application/json`

```json
{ "message": "extraction deadline exceeded" }
```

- Se agota `EXTRACT_TIMEOUT` durante `pdftotext`. **Sin campo `reason`**.

---

#### ❌ 503 Service Unavailable

| Caso | Body |
|---|---|
| `ErrExtractorFailure` (fallo interno no clasificado de poppler) | `{ "reason": "extractor_unavailable", "message": "extractor internal failure" }` |
| `ErrQueueTimeout` (esperó en cola más de `QUEUE_WAIT_TIMEOUT`) | `{ "message": "queue wait deadline exceeded" }` |
| Error desconocido (catch-all) | `{ "reason": "extractor_unavailable", "message": "extractor internal failure" }` |

---

#### ❌ 429 Too Many Requests — `application/json`

```json
{ "message": "no workers available" }
```

- Cola llena (`MAX_QUEUE` agotado) **y** `MAX_QUEUE > 0`. Incluye header `Retry-After` (segundos).

---

#### ❌ 400 Bad Request — `application/json`

```json
{ "message": "document field is required" }
```

- Request no multipart, falta campo `document` o `mime_type`.

---

#### ❌ 413 Payload Too Large — `application/json`

```json
{ "message": "document exceeds maximum size" }
```

- Body > `MAX_DOCUMENT_BYTES`.

---

### Ejemplo cURL

```bash
curl -X POST http://localhost:8081/extract \
  -F "document=@documento.pdf" \
  -F "mime_type=application/pdf" \
  -H "Accept: application/json"
```

---

## 5. Pruebas y Rendimiento

### Metodología: Test-Driven Development (TDD)

- **Dominio:** 13 motivos de fallo, equivalencia HTTP, checksum, normalización — tests puros sin I/O.
- **Aplicación:** admisión, saturación, plazos, liberación de hueco tras fallo — *stubs* en memoria.
- **Adaptador poppler:** PDF válido, sin texto, cifrado, corrupto, > `MAX_PAGES`, muerte por deadline — binario real + `testdata/`.
- **Adaptador RAM:** buffer se relee dos veces, no toca disco, `memfd` efímero por petición.
- **API:** `200` y shape exacto, cada status de error, `Content-Type`, `Retry-After` — `httptest`.
- **E2E:** extracción real con `read_only: true` y `cap_drop: ALL` vía `docker compose`.

```bash
# Suite completa con detección de carreras
make test          # go test ./... -race -count=1
```

---

### Stress Testing (k6)

**Escenario:** *Spike* de **100 VUs** (usuarios virtuales concurrentes) durante 20 s, 4 PDFs reales rotando.

**Configuración tolerante (docker-compose.yml):**
```yaml
MAX_IN_FLIGHT: "2"
MAX_QUEUE: "50"
QUEUE_WAIT_TIMEOUT: "25s"
EXTRACT_TIMEOUT: "15s"
```

**Resultados observados:**
| Métrica | Valor |
|---|---|
| Throughput pico | **16.10 req/s** (límite físico CPU 1 core × 5 réplicas) |
| Tasa de éxito | **98.18 %** |
| PDFs procesados | **600+** |
| Errores | `504 Gateway Timeout` controlados cuando la cola se saturó |
| Colapso OOM | **0** — arquitectura degradó elegantemente |

**Conclusión:** el middleware de admisión garantiza que, aunque el hardware no procese más rápido, el servicio **nunca colapsa por memoria** ni acumula *requests zombis*.

---

## 6. Despliegue

### Requisitos
- Docker 24+ / Docker Compose v2
- Go 1.22 (solo para builds locales)

---

### 1. Levantar la pila completa (orquestador + 5 réplicas extractor + Caddy proxy)

```bash
make docker-up
# o directamente:
docker compose up -d --build
```

- **Proxy:** Caddy en `:80` / `:443` (balancea entre réplicas por DNS `extractor`).
- **Extractor:** 5 réplicas, `read_only: true`, `cap_drop: ALL`, usuario `appuser` (UID 10001).
- **Límites por réplica:** `cpus: '1.0'`, `memory: 1024M`.
- **Healthcheck:** `GET /ready` (vive + huecos libres) cada 10 s.

---

### 2. Ejecutar tests localmente

```bash
# Compila y testea con race detector
make test

# Solo build
make build

# Limpia contenedores
make docker-down
```

---

### 3. Variables de entorno en producción (docker-compose.yml)

```yaml
environment:
  HTTP_PORT: "8080"
  MAX_IN_FLIGHT: "2"         # Workers concurrentes por réplica
  MAX_QUEUE: "50"            # Cola de espera
  QUEUE_WAIT_TIMEOUT: "25s"  # Timeout en cola
  EXTRACT_TIMEOUT: "15s"     # Timeout de extracción
  MAX_DOCUMENT_BYTES: "20971520"
  GOMAXPROCS: "1"            # 1 hilo OS por réplica (evita contención GOMAXPROCS > CPU quota)
```

---

### 4. Arquitectura de red (Hub & Spoke)

```
[CLIENTE] → :80 (Caddy/Traefik) → [ORQUESTADOR] → http://extractor:8080/extract → [EXTRACTOR x5]
                                                              ↑
                                                      red interna `mired`
```

- El Extractor **solo** escucha en red interna (`mired`).
- Sin puertos publicados al exterior; el proxy es la única frontera pública.
- Descubrimiento por **nombre de servicio DNS** (`extractor:8080`) — cambiar el nombre rompe el despliegue; cambiar la IP no.

---

### Endurecimiento (Hardening) del contenedor

El `Dockerfile` y `docker-compose.yml` aplican defensa en profundidad:

| Medida | Implementación |
|---|---|
| **Filesystem read-only** | `read_only: true` en compose; rootfs inmutable. |
| **Capabilities drop** | `cap_drop: [ALL]` — ningún privilegio Linux. |
| **No-new-privileges** | `security_opt: [no-new-privileges:true]` — evita escalada vía setuid. |
| **Usuario no-root** | `USER appuser` (UID 10001, sin shell, sin home). |
| **Tmpfs mínimo** | Solo `/tmp:size=8m,mode=1777` para `pdftotext` (requiere directorio escribible). |
| **Red aislada** | Solo red `mired` (interno); sin egress, sin acceso a BD, sin Traefik. |
| **Límites de recursos** | `cpus: '1.0'`, `memory: 1024M` por réplica — evita *noisy neighbor*. |
| **GOMEMLIMIT** | `GOMEMLIMIT=410MiB` (80 % del cgroup) — GC proactivo antes de OOM. |

---

## 📁 Estructura del Proyecto

```
pdf-extractext-extractor/
├── cmd/server/main.go                 # Composition root: config, wiring, graceful shutdown
├── internal/
│   ├── config/config.go               # Env → struct tipado, validado al arrancar
│   ├── domain/
│   │   ├── extraction.go              # ExtractionResult, Checksum, PageCount, Warning
│   │   ├── errors.go                  # 13 Motivos de fallo (errores sentinela)
│   │   └── ports.go                   # DocumentSource, TextExtractor (puertos)
│   ├── application/
│   │   ├── extract_service.go         # Caso de uso: admisión + plazos + delegación
│   │   └── slots.go                   # AdmissionControl (semáforo + cola acotada)
│   ├── adapters/
│   │   ├── ram/buffer.go              # memfd_create, relectible, zero-heap
│   │   └── poppler/
│   │       ├── inspector.go           # TextExtractor → pdfinfo + pdftotext
│   │       ├── process.go             # exec con Setpgid, muerte por grupo
│   │       └── diagnostics.go         # stderr poppler → Motivo de fallo
│   └── api/
│       ├── router.go                  # Rutas (GET /health, POST /extract)
│       ├── handler.go                 # POST /extract → DTOs → RFC 9457
│       ├── dto.go                     # ExtractRequest/Response/ErrorResponse
│       └── problem.go                 # RFC 9457 ProblemDetails
├── testdata/                          # PDFs de prueba (válido, sin texto, cifrado, corrupto)
├── tests/
│   ├── contract/                      # Tests de contrato HTTP (workers.md)
│   └── stress/spike_tests.js          # k6 spike 100 VUs
├── tools/genfixtures/                 # Generador de fixtures (no se publica)
├── docs/adr/                          # ADR-0001…0006 (decisiones arquitectónicas)
├── Dockerfile                         # Multi-stage: golang:1.22-bookworm → debian:slim + poppler-utils
├── docker-compose.yml                 # 5 réplicas + Caddy proxy + límites estrictos
├── Makefile                           # build, test, docker-up, docker-down
├── go.mod / go.sum                    # Go 1.22.2, golang.org/x/sys v0.30.0
└── README.md                          # Este archivo
```

---

## 📚 Documentación Adicional

- `ARCHITECTURE_EXTRACTOR.md` — Blueprint completo: vistas, contratos, pipeline, límites, observabilidad, gobernanza.
- `Especificaciones_v3.md` — Especificación del sistema completo (Orquestador, Resumidor, Persistidor, MongoDB).
- `INFORME_TECNICO.md` — Análisis de rendimiento, trade-offs de backpressure, resultados Vegeta/k6.
- `docs/adr/` — Decisiones arquitectónicas registradas (ADR-0001 a ADR-0006).

---

## 📄 Licencia

MIT — ver `LICENSE`.