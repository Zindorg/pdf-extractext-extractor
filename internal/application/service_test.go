package application

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/Zindorg/pdf-extractext-extractor/internal/domain"
)

// fixedExtractor is a TextExtractor that always returns the same result and
// never looks at the source: this test admits and delegates, doesn't extract.
type fixedExtractor struct{}

func (fixedExtractor) Extract(context.Context, domain.DocumentSource) (*domain.ExtractionResult, error) {
	return domain.NewExtractionResult("extracted text", 1), nil
}

// blockingExtractor doesn't return from Extract until unblocked, allowing a
// test to hold a slot occupied while calling Process again.
type blockingExtractor struct {
	entered chan struct{} // signals, non-blocking, that it's inside
	unblock chan struct{} // test closes this to let it finish
}

func (b *blockingExtractor) Extract(context.Context, domain.DocumentSource) (*domain.ExtractionResult, error) {
	select {
	case b.entered <- struct{}{}:
	default:
	}
	<-b.unblock

	return domain.NewExtractionResult("extracted text", 1), nil
}

// emptySource is a source that the test extractors never actually open.
type emptySource struct{}

func (emptySource) Open() (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("")), nil
}

// If the slot weren't released, this second serial call would return
// ErrResourceExhausted and the service would lose capacity forever.
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

// With no free slot and no queue (maxQueue=0), admission is immediate:
// returns ErrResourceExhausted without waiting.
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

	<-extractor.entered // the other request is inside Extract holding the slot

	if _, err := service.Process(context.Background(), emptySource{}); !errors.Is(err, domain.ErrResourceExhausted) {
		t.Errorf("Process() with exhausted capacity returned %v; expected %v", err, domain.ErrResourceExhausted)
	}

	close(extractor.unblock)
	<-done // synchronizes read of errOccupied
	if errOccupied != nil {
		t.Errorf("extraction holding the slot should succeed, got: %v", errOccupied)
	}

	if _, err := service.Process(context.Background(), emptySource{}); err != nil {
		t.Errorf("slot not free after other extraction finished: %v", err)
	}
}

// A ReleaseSlot without a prior AcquireSlot must not block the calling
// goroutine, nor create a slot above the pool's capacity.
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
