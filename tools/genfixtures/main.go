// Command genfixtures escribe los PDF de prueba que consumen los tests del
// adaptador de poppler. Vive en tools/ y no se publica: el Dockerfile solo
// copia el binario de cmd/server.
//
// La salida es determinista (mismo /ID, mismo IV) para que regenerar un fixture
// produzca un diff legible en lugar de ruido binario.
//
// Uso: go run ./tools/genfixtures
package main

import (
	"bytes"
	"crypto/md5"
	"crypto/rc4"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
)

// permisos es el valor de /P: -1 concede todos los permisos. Se tipa int32
// porque en la clave hay que escribirlo como uint32 en little-endian.
var permisos int32 = -1

// relleno es la cadena de relleno de 32 bytes del Algoritmo 2 del PDF 32000-1.
var relleno = []byte{
	0x28, 0xBF, 0x4E, 0x5E, 0x4E, 0x75, 0x8A, 0x41,
	0x64, 0x00, 0x4E, 0x56, 0xFF, 0xFA, 0x01, 0x08,
	0x2E, 0x2E, 0x00, 0xB6, 0xD0, 0x68, 0x3E, 0x80,
	0x2F, 0x0C, 0xA9, 0xFE, 0x64, 0x53, 0x69, 0x7A,
}

// idFichero es constante: la clave de cifrado depende de él (Algoritmo 2), así
// que fijarlo es lo que hace la salida reproducible.
var idFichero = []byte("0123456789abcdef0123456789abcdef")

type objeto struct {
	numero int
	cuerpo []byte
	flujo  []byte // opcional
}

func main() {
	base := filepath.Join("internal", "adapters", "poppler", "testdata")
	if err := os.MkdirAll(base, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	cifrado, err := pdfCifrado()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	ficheros := map[string][]byte{
		"valido.pdf":    pdfValido(),
		"sin_texto.pdf": pdfSinTexto(),
		"cifrado.pdf":   cifrado,
		"corrupto.pdf":  pdfCorrupto(),
	}

	for nombre, contenido := range ficheros {
		ruta := filepath.Join(base, nombre)
		if err := os.WriteFile(ruta, contenido, 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Printf("%s  %d bytes\n", ruta, len(contenido))
	}
}

// ---------------------------------------------------------------- PDF sin cifrar

// paginaDevuelve los objetos de un documento de paginas páginas. Las líneas de
// texto se pasan por página; si no hay texto para esa página se dibuja un
// rectángulo y no se escribe ningún operador de texto, que es lo que hace una
// página escaneada.
func paginaDevuelve(paginas int, texto []string) []objeto {
	objetos := []objeto{
		{numero: 1, cuerpo: []byte("<< /Type /Catalog /Pages 2 0 R >>")},
	}

	kids := ""
	for i := 0; i < paginas; i++ {
		kids += fmt.Sprintf("%d 0 R ", 3+i*2)
	}

	objetos = append(objetos, objeto{
		numero: 2,
		cuerpo: []byte(fmt.Sprintf(
			"<< /Type /Pages /Kids [%s] /Count %d >>", kids[:len(kids)-1], paginas)),
	})

	for i := 0; i < paginas; i++ {
		pagina := 3 + i*2
		contenido := 4 + i*2

		objetos = append(objetos, objeto{
			numero: pagina,
			cuerpo: []byte(fmt.Sprintf(
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] "+
					"/Resources << /Font << /F1 %d 0 R >> >> /Contents %d 0 R >>",
				3+paginas*2, contenido)),
		})

		var flujo []byte
		if i < len(texto) {
			flujo = []byte(textoDePagina(texto[i]))
		} else {
			flujo = []byte("q\n1 1 1 rg\n0 0 595 842 re\nf\nQ\n")
		}
		objetos = append(objetos, objeto{
			numero: contenido,
			cuerpo: []byte(fmt.Sprintf("<< /Length %d >>", len(flujo))),
			flujo:  flujo,
		})
	}

	objetos = append(objetos, objeto{
		numero: 3 + paginas*2,
		cuerpo: []byte("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>"),
	})

	return objetos
}

func textoDePagina(lineas string) string {
	var b bytes.Buffer
	b.WriteString("BT\n/F1 18 Tf\n72 720 Td\n14 TL\n")
	for _, linea := range bytes.Split([]byte(lineas), []byte("\n")) {
		fmt.Fprintf(&b, "(%s) Tj\nT*\n", linea)
	}
	b.WriteString("ET\n")
	return b.String()
}

func pdfValido() []byte {
	return escribirPDF(paginaDevuelve(2, []string{
		"valido.pdf pagina 1\nfixture de extraccion de texto",
		"valido.pdf pagina 2\nla segunda pagina tambien tiene capa de texto",
	}), 1, 0)
}

func pdfSinTexto() []byte {
	return escribirPDF(paginaDevuelve(2, nil), 1, 0)
}

// ---------------------------------------------------------------- PDF cifrar

// pdfCifrado escribe un documento con cifrado RC4 de 40 bits (/V 1 /R 2), la
// revisión más antigua de la norma y la única que poppler acepta sin exigir
// además las claves de permisos /OE y /UE, que esta estructura no lleva.
//
// La contraseña de usuario es "secreto": quien no la conoce recibe
// "Command Line Error: Incorrect password" con código de salida 1, que es el
// comportamiento que clasifica domain.ErrEncryptedDocument.
func pdfCifrado() ([]byte, error) {
	const paginas = 1

	objetos := paginaDevuelve(paginas, []string{
		"cifrado.pdf\ndocumento protegido con contraseña",
	})

	o := valorO("secreto", "proteccion")
	clave := claveFichero(rellenarClave("secreto"), o, idFichero)

	// Solo los flujos llevan datos que cifrar: el resto del documento son
	// números y nombres, que no se cifran.
	for i := range objetos {
		if objetos[i].flujo == nil {
			continue
		}

		plano := objetos[i].flujo
		cifrado := cifrarFlujo(clave, objetos[i].numero, 0, plano)

		if err := comprobarDescifrado(clave, objetos[i].numero, cifrado, plano); err != nil {
			return nil, fmt.Errorf("cifrado.pdf: %w", err)
		}

		objetos[i].flujo = cifrado
		objetos[i].cuerpo = []byte(fmt.Sprintf("<< /Length %d >>", len(cifrado)))
	}

	// La fuente ocupa el objeto 3 + 2*páginas; el /Encrypt va detrás.
	numeroCifrado := 3 + paginas*2 + 1

	objetos = append(objetos, objeto{
		numero: numeroCifrado,
		cuerpo: []byte(fmt.Sprintf(
			"<< /Filter /Standard /V 1 /R 2 /Length 40 /P %d "+
				"/O <%s> /U <%s> >>",
			permisos,
			hex.EncodeToString(o),
			hex.EncodeToString(valorU(clave)),
		)),
	})

	return escribirPDF(objetos, 1, numeroCifrado), nil
}

// pdfCorrupto devuelve un PDF válido cortado antes de la xref y del trailer.
// poppler no encuentra el diccionario de trailer y falla con "Syntax Error"
// sin mencionar ninguna contraseña: exactamente lo que el clasificador de
// errores debe distinguir de un documento cifrado.
func pdfCorrupto() []byte {
	valido := pdfValido()

	corte := bytes.Index(valido, []byte("xref"))
	if corte <= 0 {
		corte = len(valido) / 2
	}

	return valido[:corte]
}

// ------------------------------------------------------------------ escritor

func escribirPDF(objetos []objeto, raiz, cifrado int) []byte {
	var b bytes.Buffer

	b.WriteString("%PDF-1.7\n%\xE2\xE3\xCF\xD3\n")

	offsets := make([]int, len(objetos)+1)
	for _, o := range objetos {
		offsets[o.numero] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n", o.numero)
		b.Write(o.cuerpo)
		if o.flujo != nil {
			b.WriteString("\nstream\n")
			b.Write(o.flujo)
			b.WriteString("\nendstream")
		}
		b.WriteString("\nendobj\n")
	}

	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objetos)+1)
	for i := 1; i <= len(objetos); i++ {
		fmt.Fprintf(&b, "%010d 00000 n \n", offsets[i])
	}

	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root %d 0 R", len(objetos)+1, raiz)
	if cifrado > 0 {
		fmt.Fprintf(&b, " /Encrypt %d 0 R", cifrado)
	}
	fmt.Fprintf(&b, " /ID [<%s> <%s>] >>\nstartxref\n%d\n%%%%EOF\n",
		hex.EncodeToString(idFichero), hex.EncodeToString(idFichero), xref)

	return b.Bytes()
}

// ------------------------------------------- manejador de seguridad (RC4, R 2)

// rellenarClaveAlgoritmo2 recorta o rellena la contraseña hasta 32 bytes.
func rellenarClave(password string) []byte {
	b := make([]byte, 32)
	copy(b, password)
	copy(b[len(password):], relleno)
	return b
}

// valorO implementa el Algoritmo 3 en su variante de revisión 2: una sola
// pasada de RC4 sobre la contraseña de usuario rellenada. El /O se cifra con
// RC4 siempre, incluso con revisiones superiores: es el único punto del
// repositorio que usa RC4 y lo hace por norma, no por gusto.
//
//nolint:staticcheck // SA1019: el Algoritmo 3 del PDF 32000-1 obliga a RC4 para /O
func valorO(usuario, duenho string) []byte {
	suma := md5.Sum(rellenarClave(duenho))

	c, err := rc4.NewCipher(suma[:5])
	if err != nil {
		panic(err)
	}

	datos := rellenarClave(usuario)
	salida := make([]byte, len(datos))
	c.XORKeyStream(salida, datos)

	return salida
}

// claveFichero implementa el Algoritmo 2 del PDF 32000-1, tabla 3.13:
// MD5(contraseña + O + P + ID), una sola pasada y 5 bytes de salida en la
// revisión 2: ni las 50 iteraciones ni el recorte a n bytes aplican hasta la
// revisión 3, pero el /P se suma desde la revisión 2. El /ID se toma entero,
// tal y como hace poppler en Decrypt::makeFileKey2.
func claveFichero(usuario, o, id []byte) []byte {
	p := make([]byte, 4)
	binary.LittleEndian.PutUint32(p, uint32(permisos))

	h := md5.New()
	h.Write(usuario)
	h.Write(o)
	h.Write(p)
	h.Write(id)
	suma := h.Sum(nil)

	return suma[:5]
}

// valorU implementa el Algoritmo 5 en su variante de revisión 2: RC4 de la
// cadena de relleno con la clave del fichero. Devuelve 32 bytes.
//
//nolint:staticcheck // SA1019: el Algoritmo 5 del PDF 32000-1 obliga a RC4 para /U
func valorU(clave []byte) []byte {
	c, err := rc4.NewCipher(clave)
	if err != nil {
		panic(err)
	}

	u := make([]byte, len(relleno))
	c.XORKeyStream(u, relleno)

	return u
}

// claveObjeto implementa el Algoritmo 1 para /V < 5.
func claveObjeto(clave []byte, numero, generacion int) []byte {
	h := md5.New()
	h.Write(clave)

	var sufijo [5]byte
	binary.LittleEndian.PutUint32(sufijo[0:4], uint32(numero))
	binary.LittleEndian.PutUint16(sufijo[3:5], uint16(generacion))
	h.Write(sufijo[:])

	n := len(clave) + 5
	if n > 16 {
		n = 16
	}

	return h.Sum(nil)[:n]
}

// cifrarFlujo aplica el Algoritmo 1: RC4 con la clave por objeto. La clave se
// deriva del número y la generación, así que dos objetos con el mismo texto
// producen bytes distintos, como exige la norma.
//
//nolint:staticcheck // SA1019: el Algoritmo 1 del PDF 32000-1 obliga a RC4 en /R 2
func cifrarFlujo(clave []byte, numero, generacion int, datos []byte) []byte {
	c, err := rc4.NewCipher(claveObjeto(clave, numero, generacion))
	if err != nil {
		panic(err)
	}

	salida := make([]byte, len(datos))
	c.XORKeyStream(salida, datos)

	return salida
}

// descifrarFlujo deshace cifrarFlujo. RC4 es un simple XOR contra un flujo de
// claves, así que descifrar es aplicar la misma operación: la clave por objeto
// y su longitud son lo único que distingue una llamada de otra.
//
// Devuelve error porque el llamante lo usa para verificar que el fixture que va
// a escribir es legible, y esa verificación tiene que poder fallar.
//
//nolint:staticcheck // SA1019: el Algoritmo 1 del PDF 32000-1 obliga a RC4 en /R 2
func descifrarFlujo(clave []byte, numero, generacion int, datos []byte) ([]byte, error) {
	c, err := rc4.NewCipher(claveObjeto(clave, numero, generacion))
	if err != nil {
		return nil, err
	}

	plano := make([]byte, len(datos))
	c.XORKeyStream(plano, datos)

	return plano, nil
}

// comprobarDescifrado hace el viaje de ida y vuelta sobre un flujo recién
// cifrado. Si el resultado no devuelve el texto original, el PDF que se va a
// escribir sería ilegible: se aborta la generación en lugar de dejar un
// fixture roto en testdata/.
func comprobarDescifrado(clave []byte, numero int, cifrado, plano []byte) error {
	descifrado, err := descifrarFlujo(clave, numero, 0, cifrado)
	if err != nil {
		return fmt.Errorf("objeto %d: no se pudo descifrar: %w", numero, err)
	}

	if !bytes.Equal(descifrado, plano) {
		return fmt.Errorf("objeto %d: el flujo descifrado no devuelve el texto original", numero)
	}

	return nil
}
