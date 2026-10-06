package main

import (
	"bytes"
	"testing"
)

// Red contra descifrarFlujo, que todavía no existe: aquí el rojo es un error
// de compilación, igual que en el dominio.
//
// Lo que este test NO demuestra: que la clave por objeto sea la que dicta el
// PDF 32000-1. Cifrar y descifrar comparten claveObjeto, así que una
// derivación sistemáticamente incorrecta pasaría igualmente. Para eso hace
// falta poppler.
func TestCifrarFlujo_VuelveAlTextoOriginalSiSeDescifra(t *testing.T) {
	clave := claveFichero(rellenarClave("secreto"), valorO("secreto", ""), idFichero)
	claro := []byte("BT\n/F1 18 Tf\n72 720 Td\n(cifrado.pdf) Tj\nET\n")

	cifrado := cifrarFlujo(clave, 4, 0, claro)

	descifrado, err := descifrarFlujo(clave, 4, 0, cifrado)
	if err != nil {
		t.Fatalf("no se pudo descifrar: %v", err)
	}

	if !bytes.Equal(descifrado, claro) {
		t.Errorf("el texto descifrado no coincide con el original:\n  descifrado = %q\n  original   = %q", descifrado, claro)
	}
}

func TestComprobarDescifrado_DetectaUnFlujoAlterado(t *testing.T) {
	clave := claveFichero(rellenarClave("secreto"), valorO("secreto", ""), idFichero)
	plano := []byte("BT\n/F1 18 Tf\n72 720 Td\n(objeto) Tj\nET\n")
	cifrado := cifrarFlujo(clave, 4, 0, plano)

	if err := comprobarDescifrado(clave, 4, cifrado, plano); err != nil {
		t.Fatalf("un flujo recién cifrado debe descifrar de vuelta: %v", err)
	}

	cifrado[len(cifrado)/2] ^= 0xFF
	if err := comprobarDescifrado(clave, 4, cifrado, plano); err == nil {
		t.Error("se esperaba un error: el flujo alterado no devuelve el texto original")
	}
}
