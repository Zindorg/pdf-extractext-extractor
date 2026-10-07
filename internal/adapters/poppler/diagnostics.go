package poppler

import (
	"context"
	"strings"

	"github.com/Zindorg/pdf-extractext-extractor/internal/domain"
)

// runDiagnostics condensa lo que dejó un proceso de poppler que terminó con
// error: su código de salida y el stderr completo.
type runDiagnostics struct {
	ExitCode int
	Stderr   string
}

// clavesCifrado y clavesCorrupcion son las huellas que poppler deja en el
// stderr según el tipo de fallo.
var clavesCifrado = []string{"password", "encrypted", "drm"}

var clavesCorrupcion = []string{"syntax error", "damaged", "corrupted"}

// classifyReport traduce un fallo de poppler a un motivo del dominio, en este
// orden:
//
//  1. el contexto no está vivo: venció el plazo o el cliente canceló;
//  2. el stderr habla de una contraseña o de DRM: el documento está cifrado;
//  3. el stderr habla de un PDF dañado: el documento está roto;
//  4. cualquier otro fallo con salida no nula: el documento está roto.
//
// Solo se clasifican los fallos con código de salida no nulo: poppler escribe
// avisos de sintaxis en el stderr incluso cuando el resultado es utilizable,
// y esos no invalidan la extracción.
//
// Deuda conocida: el punto 1 funde la cancelación del cliente con el plazo
// agotado, que en el contrato HTTP son dos cosas distintas —EXTRACTION_TIMEOUT
// responde 504 y CANCELED no escribe respuesta. Hoy no existe sentinela ni
// test que las distinga, así que se comportan igual; separarlas es un ciclo
// con su propia prueba.
func classifyReport(ctx context.Context, reporte runDiagnostics) error {
	if ctx.Err() != nil {
		return domain.ErrExtractionTimeout
	}

	if reporte.ExitCode == 0 {
		return nil
	}

	detalle := strings.ToLower(reporte.Stderr)
	for _, clave := range clavesCifrado {
		if strings.Contains(detalle, clave) {
			return domain.ErrEncryptedDocument
		}
	}

	for _, clave := range clavesCorrupcion {
		if strings.Contains(detalle, clave) {
			return domain.ErrCorruptDocument
		}
	}

	return domain.ErrCorruptDocument
}
