package resilience

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Zindorg/pdf-extractext-extractor/internal/domain"
)

type scriptedExtractor struct {
	calls     atomic.Int32
	failUntil int32
	err       error
}

func (s *scriptedExtractor) Extract(_ context.Context, _ domain.DocumentSource) (*domain.ExtractionResult, error) {
	n := s.calls.Add(1)
	if n <= s.failUntil {
		return nil, s.err
	}
	return domain.NewExtractionResult("recuperado", 1).WithExtractionTimeMs(10), nil
}

type dummySource struct{}

func (dummySource) Open() (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader([]byte("%PDF-1.4"))), nil
}

func TestResilientExtractor_RetriesOnTransientEngineFailureAndSucceeds(t *testing.T) {
	// Falla en los primeros 2 intentos con ErrExtractorFailure y tiene éxito en el 3ro (retry #2)
	script := &scriptedExtractor{
		failUntil: 2,
		err:       domain.ErrExtractorFailure,
	}
	cb := NewResilientExtractor(script, 5, 10*time.Second, 2, 5*time.Millisecond)

	res, err := cb.Extract(context.Background(), dummySource{})
	if err != nil {
		t.Fatalf("expected recovery after retries, got err = %v", err)
	}
	if res.Text != "recuperado" {
		t.Errorf("got %q; want 'recuperado'", res.Text)
	}
	if script.calls.Load() != 3 {
		t.Errorf("calls = %d; want 3 (1 initial + 2 retries)", script.calls.Load())
	}
}

func TestResilientExtractor_DoesNotRetryOrTripOnClient422Errors(t *testing.T) {
	script := &scriptedExtractor{
		failUntil: 100,
		err:       domain.ErrEncryptedDocument,
	}
	cb := NewResilientExtractor(script, 2, 10*time.Second, 3, 5*time.Millisecond)

	for i := 0; i < 5; i++ {
		_, err := cb.Extract(context.Background(), dummySource{})
		if !errors.Is(err, domain.ErrEncryptedDocument) {
			t.Fatalf("iteration %d: got %v; want ErrEncryptedDocument", i, err)
		}
	}

	// No debe haber reintentado ninguna vez (5 llamadas = 5 invocaciones)
	if script.calls.Load() != 5 {
		t.Errorf("calls = %d; want 5 (no retries for 422 errors)", script.calls.Load())
	}

	// El circuito debe seguir Closed
	if cb.state != StateClosed {
		t.Errorf("circuit state = %v; want StateClosed", cb.state)
	}
}

func TestResilientExtractor_OpensCircuitAfterThresholdAndRecoversAfterResetTimeout(t *testing.T) {
	script := &scriptedExtractor{
		failUntil: 100,
		err:       domain.ErrExtractorFailure,
	}
	// Threshold = 2 fallos, ResetTimeout = 40ms, 0 retries para aislar el Circuit Breaker
	cb := NewResilientExtractor(script, 2, 40*time.Millisecond, 0, time.Millisecond)

	// Fallo 1 y Fallo 2 -> Abren el circuito
	_, _ = cb.Extract(context.Background(), dummySource{})
	_, _ = cb.Extract(context.Background(), dummySource{})

	if script.calls.Load() != 2 {
		t.Fatalf("calls = %d; want 2", script.calls.Load())
	}

	// Llamada 3 con circuito Open -> Cortocircuita sin llamar al motor subyacente
	_, err := cb.Extract(context.Background(), dummySource{})
	if !errors.Is(err, domain.ErrExtractorFailure) {
		t.Fatalf("got %v; want ErrExtractorFailure when circuit is open", err)
	}
	if script.calls.Load() != 2 {
		t.Errorf("calls = %d; want 2 (circuit breaker did not short-circuit)", script.calls.Load())
	}

	// Esperamos a que venza el resetTimeout -> Pasa a Half-Open y el motor ahora responde OK
	time.Sleep(60 * time.Millisecond)
	script.failUntil = 0

	res, err := cb.Extract(context.Background(), dummySource{})
	if err != nil {
		t.Fatalf("expected Half-Open probe to succeed, got %v", err)
	}
	if res.Text != "recuperado" {
		t.Errorf("got %q; want 'recuperado'", res.Text)
	}
	if cb.state != StateClosed {
		t.Errorf("state = %v; want StateClosed after successful probe", cb.state)
	}
}
