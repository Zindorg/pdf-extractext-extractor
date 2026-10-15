# Informe Técnico: Análisis de Rendimiento y Control de Concurrencia

## 1. Arquitectura y Decisiones de Diseño
El microservicio fue diseñado siguiendo los principios de **Clean Architecture** y **Twelve-Factor App**. La configuración de concurrencia se inyecta estrictamente a través de variables de entorno, asegurando que los procesos no tengan estado interno. Se delegó el ruteo a un reverse proxy (Caddy) configurado para balancear la carga entre las 5 réplicas del servicio.

Se implementó un límite estricto de recursos a nivel infraestructura (1.0 CPU y 1024M de RAM por réplica en Docker) para garantizar condiciones de ejecución consistentes, forzando al sistema a gestionar la escasez de recursos.

## 2. Optimización del Flujo de Datos
Para reducir el consumo de CPU y latencia de disco, se optimizó el manejo de los archivos PDF:
* **Zero-Disk I/O:** Se utilizaron descriptores de archivo en memoria RAM (`memfd_create`) para inyectar los binarios directamente a `pdftotext`, evitando la escritura en el disco físico.
* **Reducción de Syscalls:** Se eliminó la dependencia paralela de `pdfinfo`. El algoritmo ahora cuenta los caracteres *Form Feed* (`\f`) directamente de la salida de `pdftotext`, reduciendo a la mitad la creación de procesos a nivel sistema operativo.

## 3. Backpressure y Control de Admisión
Se diseñó un middleware de `AdmissionControl` basado en canales de Go (Go channels) para separar el runtime HTTP de los workers de procesamiento. Durante las pruebas, se demostró empíricamente la imposibilidad de tener una configuración universal perfecta, evidenciando un "Trade-off" arquitectónico entre dos escenarios:

### Escenario A: Sobrevivir a Carga Sostenida (Modelo Abierto - Vegeta)
Bajo una inyección constante de 50 req/s, una cola larga colapsa el sistema. Se configuró un **Backpressure Estricto** (`MAX_QUEUE=15`, `QUEUE_WAIT_TIMEOUT=5s`).
* **Resultado:** El sistema protegió su CPU (evitando el *Thrashing*) y generó **529 rechazos controlados (HTTP 503)** en lugar de colgarse o devolver timeouts de red. Se logró un Fail-Fast efectivo.

### Escenario B: Absorber Picos de Tráfico (Modelo Cerrado - k6)
Ante un *Spike* de 100 Usuarios Concurrentes (VUs), el Fail-Fast estricto descarta peticiones válidas. Se configuró un **Backpressure Tolerante** (`MAX_IN_FLIGHT=2`, `MAX_QUEUE=50`, `QUEUE_WAIT_TIMEOUT=25s`).
* **Resultado:** La cola absorbió el pico temporal. El sistema alcanzó un throughput de **16.10 req/s** (rozando el límite físico del CPU) con una tasa de éxito del **98.18%**. 

## 4. Análisis del Cuello de Botella y Conclusión
Las pruebas demostraron que el límite de throughput del sistema (~16 req/s) no está ligado a ineficiencias del código en Go, sino que es un límite físico (CPU-bound). Al ejecutar 5 réplicas bajo WSL, la contención por los ciclos de reloj físicos del procesador host dicta la velocidad máxima de los procesos concurrentes de `pdftotext`. 

La arquitectura implementada demostró resiliencia estructural: aunque el hardware no pueda procesar más rápido, el middleware de admisión garantiza que el microservicio no colapse por falta de memoria (OOM) ni acumule request "zombis", degradándose de forma elegante bajo estrés extremo.