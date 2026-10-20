package contract

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Zindorg/pdf-extractext-extractor/internal/adapters/poppler"
	"github.com/Zindorg/pdf-extractext-extractor/internal/api"
	"github.com/Zindorg/pdf-extractext-extractor/internal/application"
	"github.com/Zindorg/pdf-extractext-extractor/internal/config"
)

// readFixture lee un PDF de testdata/.
func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	path := filepath.Join("..", "..", "internal", "adapters", "poppler", "testdata", name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("fixture %s: %v", name, err)
	}
	return data
}

// newMultipartRequest crea request multipart con document + mime_type.
func newMultipartRequest(t *testing.T, document []byte, mimeType string) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, _ := w.CreateFormFile("document", "test.pdf")
	part.Write(document)
	w.WriteField("mime_type", mimeType)
	w.Close()
	req := httptest.NewRequest(http.MethodPost, "/extract", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	return req
}

// newTestHandler crea un handler completo (composition root de main.go)
// para tests de contrato de caja negra.
func newTestHandler(t *testing.T) *api.ExtractionHandler {
	t.Helper()
	cfg := config.Config{
		MaxInFlight:      2,
		MaxQueue:         4,
		QueueWaitTimeout: 5 * time.Second,
		ExtractTimeout:   5 * time.Second,
		MaxDocumentBytes: 10 * 1024 * 1024,
	}
	return api.NewExtractionHandler(
		application.NewExtractionService(
			poppler.New(),
			application.NewAdmissionControl(cfg.MaxInFlight, cfg.MaxQueue),
			cfg.QueueWaitTimeout,
			cfg.ExtractTimeout,
		),
		cfg.MaxDocumentBytes,
	)
}

// makeRequest ejecuta el router (con el handler) y devuelve el recorder.
func makeRequest(t *testing.T, handler *api.ExtractionHandler, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	router := api.NewRouter(handler)
	router.ServeHTTP(rec, req)
	return rec
}