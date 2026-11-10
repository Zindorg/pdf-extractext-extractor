package application

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Zindorg/pdf-extractext-extractor/internal/domain"
)

type fixedExtractor struct{}

func (fixedExtractor) Extract(context.Context, domain.DocumentSource) (*domain.ExtractionResult, error) {
	return domain.NewExtractionResult("extracted text", 1), nil
}

type blockingExtractor struct {
	entered chan struct{}
	unblock chan struct{}
}

func (b *blockingExtractor) Extract(context.Context, domain.DocumentSource) (*domain.ExtractionResult, error) {
	select {
	case b.entered <- struct{}{}:
	default:
	}
	<-b.unblock
	return domain.NewExtractionResult("extracted text", 1), nil
}

type emptySource struct{}

func (emptySource) Open() (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("")), nil
}

func TestAdmissionControl_NeverExceedsMaxInFlight(t *testing.T) {
	ac := NewAdmissionControl(2, 20)
	var cur, peak atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := ac.AcquireSlot(context.Background()); err != nil {
				t.Error(err)
				return
			}
			defer ac.ReleaseSlot()
			n := cur.Add(1)
			for {
				p := peak.Load()
				if n <= p || peak.CompareAndSwap(p, n) {
					break
				}
			}
			time.Sleep(10 * time.Millisecond)
			cur.Add(-1)
		}()
	}
	wg.Wait()
	if p := peak.Load(); p > 2 {
		t.Fatalf("peak concurrency %d; max allowed 2", p)
	}
}

func TestProcess_ReleasesSlotAfterExtraction(t *testing.T) {
	admission := NewAdmissionControl(1, 128)
	service := NewExtractionService(fixedExtractor{}, admission, 8*time.Second, 4*time.Second)

	result, err := service.Process(context.Background(), emptySource{})
	if err != nil {
		t.Fatalf("first extraction should succeed, got: %v", err)
	}
	if result.Text != "extracted text" {
		t.Errorf("Process() didn't return the text from the adapter: %q", result.Text)
	}

	if _, err := service.Process(context.Background(), emptySource{}); err != nil {
		t.Errorf("slot not released after first extraction: %v", err)
	}
}

func TestProcess_ReturnsErrResourceExhaustedWhenNoSlotAvailable(t *testing.T) {
	admission := NewAdmissionControl(1, 0)
	extractor := &blockingExtractor{
		entered: make(chan struct{}, 1),
		unblock: make(chan struct{}),
	}
	service := NewExtractionService(extractor, admission, 8*time.Second, 4*time.Second)

	var errOccupied error
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, errOccupied = service.Process(context.Background(), emptySource{})
	}()

	<-extractor.entered

	if _, err := service.Process(context.Background(), emptySource{}); !errors.Is(err, domain.ErrResourceExhausted) {
		t.Errorf("Process() with exhausted capacity returned %v; expected %v", err, domain.ErrResourceExhausted)
	}

	close(extractor.unblock)
	<-done
	if errOccupied != nil {
		t.Errorf("extraction holding the slot should succeed, got: %v", errOccupied)
	}

	if _, err := service.Process(context.Background(), emptySource{}); err != nil {
		t.Errorf("slot not free after other extraction finished: %v", err)
	}
}

func TestAdmissionControl_ExtraReleaseDoesNotSaturate(t *testing.T) {
	admission := NewAdmissionControl(1, 0)
	admission.ReleaseSlot()

	ctx := context.Background()
	if err := admission.AcquireSlot(ctx); err != nil {
		t.Errorf("AcquireSlot() should grant the single free slot: %v", err)
	}
	if err := admission.AcquireSlot(ctx); !errors.Is(err, domain.ErrResourceExhausted) {
		t.Errorf("AcquireSlot() granted a second slot with capacity 1: %v", err)
	}
}

func TestProcess_ExtractionTimeMsExcludesQueueWait(t *testing.T) {
	slowExtractor := &timedExtractor{delay: 50 * time.Millisecond}

	admission := NewAdmissionControl(1, 1)
	service := NewExtractionService(slowExtractor, admission, 5*time.Second, 5*time.Second)

	done1 := make(chan struct{})
	go func() {
		defer close(done1)
		_, _ = service.Process(context.Background(), emptySource{})
	}()

	time.Sleep(10 * time.Millisecond)

	start := time.Now()
	result, err := service.Process(context.Background(), emptySource{})
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Process() failed: %v", err)
	}

	if result.ExtractionTimeMs < 30 || result.ExtractionTimeMs > 75 {
		t.Errorf("ExtractionTimeMs = %dms; expected ~50ms (extraction only), total was %dms",
			result.ExtractionTimeMs, elapsed.Milliseconds())
	}

	<-done1
}

type timedExtractor struct {
	delay time.Duration
}

func (t *timedExtractor) Extract(ctx context.Context, _ domain.DocumentSource) (*domain.ExtractionResult, error) {
	start := time.Now()
	select {
	case <-time.After(t.delay):
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return domain.NewExtractionResult("text", 1).
		WithExtractionTimeMs(time.Since(start).Milliseconds()), nil
}
