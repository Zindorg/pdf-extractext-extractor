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
	"crypto/aes"
	"crypto/cipher"
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

	ficheros := map[string][]byte{
		"valido.pdf":    pdfValido(),
		"sin_texto.pdf": pdfSinTexto(),
		"cifrado.pdf":   pdfCifrado(),
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

// ---------------------------------------------------------------- PDF cifrado

func pdfCifrado() []byte {
	const paginas = 1

	objetos := paginaDevuelve(paginas, []string{
		"cifrado.pdf\ndocumento protegido con AES-128",
	})

	clave := claveFichero(rellenarClave("secreto"), valorO("secreto", ""), idFichero)

	// Solo los flujos llevan datos que cifrar: el resto del documento son
	// números y nombres, que no se cifran.
	for i := range objetos {
		if objetos[i].flujo != nil {
			objetos[i].flujo = cifrarFlujo(clave, objetos[i].numero, 0, objetos[i].flujo)
			objetos[i].cuerpo = []byte(fmt.Sprintf("<< /Length %d >>", len(objetos[i].flujo)))
		}
	}

	// La fuente ocupa el objeto 3 + 2*páginas; el /Encrypt va detrás.
	numeroCifrado := 3 + paginas*2 + 1

	objetos = append(objetos, objeto{
		numero: numeroCifrado,
		cuerpo: []byte(fmt.Sprintf(
			"<< /Filter /Standard /V 4 /R 6 /Length 128 /P %d "+
				"/O <%s> /U <%s> "+
				"/CF << /StdCF << /CFM /AESV2 /AuthEvent /DocOpen /Length 16 >> >> "+
				"/StmF /StdCF /StrF /StdCF >>",
			permisos,
			hex.EncodeToString(valorO("secreto", "")),
			hex.EncodeToString(valorU(idFichero)),
		)),
	})

	return escribirPDF(objetos, 1, numeroCifrado)
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

// ------------------------------------------------- manejador de seguridad (AESV2)

// rellenarClaveAlgoritmo2 recorta o rellena la contraseña hasta 32 bytes.
func rellenarClave(password string) []byte {
	b := make([]byte, 32)
	copy(b, password)
	copy(b[len(password):], relleno)
	return b
}

// valorO implementa el Algoritmo 3. El /O se cifra con RC4 siempre, incluso
// con /V 4 en AES: es el único punto del repositorio que usa RC4 y lo hace por
// norma, no por gusto.
//
//nolint:staticcheck // SA1019: el Algoritmo 3 del PDF 32000-1 obliga a RC4 para /O
func valorO(usuario, duenho string) []byte {
	duenhoRelleno := rellenarClave(duenho)
	suma := md5.Sum(duenhoRelleno)

	c, err := rc4.NewCipher(suma[:5])
	if err != nil {
		panic(err)
	}

	datos := rellenarClave(usuario)
	for i := 0; i < 20; i++ {
		dato := make([]byte, len(datos))
		c.XORKeyStream(dato, datos)
		datos = dato
	}

	return datos
}

// claveFichero implementa el Algoritmo 2 para /R 6 con longitud de clave 128.
func claveFichero(usuario, o []byte, id []byte) []byte {
	p := make([]byte, 4)
	binary.LittleEndian.PutUint32(p, uint32(permisos))

	h := md5.New()
	h.Write(usuario)
	h.Write(o)
	h.Write(p)
	h.Write(id)
	suma := h.Sum(nil)

	// /R >= 3: otras 50 iteraciones sobre los primeros 16 bytes.
	for i := 0; i < 50; i++ {
		s := md5.Sum(suma[:16])
		suma = s[:]
	}

	return suma[:16]
}

// valorU implementa el Algoritmo 5 para /R >= 3: 16 bytes de hash y 32 de relleno.
func valorU(id []byte) []byte {
	h := md5.New()
	h.Write(relleno)
	h.Write(id)
	suma := h.Sum(nil)

	for i := 0; i < 50; i++ {
		s := md5.Sum(suma[:16])
		suma = s[:]
	}

	u := make([]byte, 48)
	copy(u, suma[:16])
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

func cifrarFlujo(clave []byte, numero, generacion int, datos []byte) []byte {
	bloque, err := aes.NewCipher(claveObjeto(clave, numero, generacion))
	if err != nil {
		panic(err)
	}

	// IV fijo derivado del número de objeto: AES lo toma del principio del
	// flujo, y un IV aleatorio haría la salida irreproducible. Es un fixture de
	// prueba, no criptografía de producción.
	iv := iv(numero, generacion)

	rellenoPKCS5 := aes.BlockSize - len(datos)%aes.BlockSize
	padded := make([]byte, 0, len(datos)+rellenoPKCS5)
	padded = append(padded, datos...)
	padded = append(padded, bytes.Repeat([]byte{byte(rellenoPKCS5)}, rellenoPKCS5)...)

	cifrado := make([]byte, len(padded))
	cipher.NewCBCEncrypter(bloque, iv).CryptBlocks(cifrado, padded)

	return append(iv, cifrado...)
}

func iv(numero, generacion int) []byte {
	h := md5.New()
	fmt.Fprintf(h, "extractor-fixture:%d:%d", numero, generacion)
	suma := h.Sum(nil)
	return suma[:aes.BlockSize]
}
