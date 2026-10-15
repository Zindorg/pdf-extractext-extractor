#!/bin/bash

# Configuración estricta del Trabajo Práctico
RATE="50"
DURATION="30s"
TIMEOUT="30s" # Clave para cumplir el requisito de no cortar antes
TARGETS="targets.txt"
BIN_OUTPUT="results.bin"
JSON_OUTPUT="results.json"
PLOT_OUTPUT="plot.html"

echo "Preparando archivo de targets..."
rm -f $TARGETS

# Asumiendo que la carpeta oficial de la cátedra está en tests/stress/pdfs/
# Reemplaza los nombres de los PDFs si son diferentes
PDFS=(
  "tests/stress/pdfs/978-84-11920-35-3 Filosofia Lean.pdf"
  "tests/stress/pdfs/2020-Scrum-Guide-Spanish-Latin-South-American.pdf"
  "tests/stress/pdfs/Essential-Kanban-Condensed-v1.0.0.pdf"
  "tests/stress/pdfs/historias_usuario_scrum_manager.pdf"
)

# Generar el archivo de targets rotando los 4 PDFs
for PDF in "${PDFS[@]}"; do
  if [ -f "$PDF" ]; then
    echo "POST http://localhost:8000/extract" >> $TARGETS
    echo "Host: extract.universidad.localhost" >> $TARGETS
    echo "Content-Type: application/pdf" >> $TARGETS
    echo "@${PDF}" >> $TARGETS
    echo "" >> $TARGETS
  else
    echo "ADVERTENCIA: No se encontró el archivo $PDF"
  fi
done

echo "Iniciando ataque de carga fija: $RATE req/s por $DURATION (Timeout: $TIMEOUT)..."
# Ejecutamos vegeta con el flag de timeout añadido
vegeta attack -rate="${RATE}" -duration="${DURATION}" -timeout="${TIMEOUT}" -targets="${TARGETS}" | tee "${BIN_OUTPUT}" | vegeta report

echo "Generando reportes adicionales..."
vegeta report -type=json < "${BIN_OUTPUT}" > "${JSON_OUTPUT}"
vegeta plot < "${BIN_OUTPUT}" > "${PLOT_OUTPUT}"

echo "¡Prueba finalizada! Puedes ver el gráfico abriendo ${PLOT_OUTPUT} en tu navegador."