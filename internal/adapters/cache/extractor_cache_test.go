package cache

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Zindorg/pdf-extractext-extractor/internal/domain"
)

type spyExtractor struct {
	calls  atomic.Int32
	result *domain.ExtractionResult
	err    error
}

func (s *spyExtractor) Extract(_ context.Context, _ domain.DocumentSource) (*domain.ExtractionResult, error) {
	s.calls.Add(1)
	if s.err != nil {
		return nil, s.err
	}
	return s.result, nil
}

type byteSource struct {
	data []byte
}

func (b byteSource) Open() (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(b.data)), nil
}

func TestCachedExtractor_ReturnsCachedResultOnHit(t *testing.T) {
	spy := &spyExtractor{
		result: domain.NewExtractionResult("texto cacheado", 3).WithExtractionTimeMs(45),
	}
	cached := NewCachedExtractor(spy, 5*time.Minute, 10)
	src := byteSource{data: []byte("%PDF-1.4 contenido")}

	// Primera llamada: Cache Miss -> invoca al spy
	res1, err := cached.Extract(context.Background(), src)
	if err != nil {
		t.Fatalf("unexpected error on miss: %v", err)
	}
	if res1.Text != "texto cacheado" {
		t.Errorf("got %q; want 'texto cacheado'", res1.Text)
	}
	if spy.calls.Load() != 1 {
		t.Fatalf("spy calls = %d; want 1", spy.calls.Load())
	}

	// Segunda llamada con el mismo PDF: Cache Hit -> NO invoca al spy
	res2, err := cached.Extract(context.Background(), src)
	if err != nil {
		t.Fatalf("unexpected error on hit: %v", err)
	}
	if res2.Text != "texto cacheado" {
		t.Errorf("got %q; want 'texto cacheado'", res2.Text)
	}
	if spy.calls.Load() != 1 {
		t.Errorf("spy calls = %d; want 1 (should have been served from cache)", spy.calls.Load())
	}
}

func TestCachedExtractor_DoesNotCacheErrors(t *testing.T) {
	spy := &spyExtractor{
		err: domain.ErrExtractorFailure,
	}
	cached := NewCachedExtractor(spy, 5*time.Minute, 10)
	src := byteSource{data: []byte("%PDF-1.4 fallo")}

	_, err := cached.Extract(context.Background(), src)
	if !errors.Is(err, domain.ErrExtractorFailure) {
		t.Fatalf("got %v; want ErrExtractorFailure", err)
	}

	// Si ahora el motor se recupera, la segunda llamada debe volver a invocarlo
	spy.err = nil
	spy.result = domain.NewExtractionResult("recuperado", 1).WithExtractionTimeMs(10)

	res, err := cached.Extract(context.Background(), src)
	if err != nil {
		t.Fatalf("unexpected error on retry: %v", err)
	}
	if res.Text != "recuperado" {
		t.Errorf("got %q; want 'recuperado'", res.Text)
	}
	if spy.calls.Load() != 2 {
		t.Errorf("spy calls = %d; want 2", spy.calls.Load())
	}
}

func TestCachedExtractor_EvictsWhenMaxEntriesReached(t *testing.T) {
	spy := &spyExtractor{
		result: domain.NewExtractionResult("ok", 1).WithExtractionTimeMs(5),
	}
	cached := NewCachedExtractor(spy, 5*time.Minute, 2)

	_, _ = cached.Extract(context.Background(), byteSource{data: []byte("doc-1")})
	_, _ = cached.Extract(context.Background(), byteSource{data: []byte("doc-2")})
	_, _ = cached.Extract(context.Background(), byteSource{data: []byte("doc-3")})

	cached.mu.RLock()
	count := len(cached.items)
	cached.mu.RUnlock()

	if count > 2 {
		t.Errorf("cache size = %d; want <= 2", count)
	}
}

func TestCachedExtractor_ConcurrentAccessIsRaceFree(t *testing.T) {
	spy := &spyExtractor{
		result: domain.NewExtractionResult("concurrente", 1).WithExtractionTimeMs(12),
	}
	cached := NewCachedExtractor(spy, 50*time.Millisecond, 5)

	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			payload := []byte("%PDF-1.4 doc-" + string(rune('A'+(id%7))))
			_, _ = cached.Extract(context.Background(), byteSource{data: payload})
		}(i)
	}
	wg.Wait()
}
