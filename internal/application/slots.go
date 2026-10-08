package application

import (
	"context"
	"sync/atomic"

	"github.com/Zindorg/pdf-extractext-extractor/internal/domain"
)

// WorkerPool es la admisión: N huecos de extracción simultáneos con cola
// acotada. Si no hay hueco libre, la petición espera en cola hasta que se
// libera uno o expira el contexto (§8, paso 2).
//
// Se modela como un canal de tokens (semáforo) más un contador atómico de
// esperas en cola para aplicar MAX_QUEUE.
type WorkerPool struct {
	slots        chan struct{}
	maxQueue     int32
	waitingCount int32
}

// NewWorkerPool devuelve un pool capaz de sostener capacity extracciones a la
// vez, con una cola de espera de maxQueue peticiones.
func NewWorkerPool(capacity, maxQueue int) *WorkerPool {
	return &WorkerPool{
		slots:    make(chan struct{}, capacity),
		maxQueue: int32(maxQueue),
	}
}

// Acquire toma un hueco si queda alguno libre. Si no hay hueco, se encola
// hasta que se libera uno o el contexto expira. Devuelve error si el contexto
// se cancela (ErrQueueTimeout) o si la cola está llena (ErrResourceExhausted).
func (p *WorkerPool) Acquire(ctx context.Context) error {
	select {
	case p.slots <- struct{}{}:
		return nil
	default:
	}

	if p.maxQueue == 0 {
		return domain.ErrResourceExhausted
	}

	waiting := atomic.AddInt32(&p.waitingCount, 1)
	if waiting > p.maxQueue {
		atomic.AddInt32(&p.waitingCount, -1)
		return domain.ErrResourceExhausted
	}
	defer atomic.AddInt32(&p.waitingCount, -1)

	select {
	case p.slots <- struct{}{}:
		return nil
	case <-ctx.Done():
		return domain.ErrQueueTimeout
	}
}

// Release devuelve un hueco tomado.
//
// El select con default es deliberado: un Release sin ningún hueco tomado no
// debe dejar la goroutine esperando para siempre.
func (p *WorkerPool) Release() {
	select {
	case <-p.slots:
	default:
	}
}
