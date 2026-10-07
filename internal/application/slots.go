package application

// WorkerPool es la admisión: N huecos de extracción simultáneos. Si no queda
// ninguno libre, la petición se rechaza en el acto, sin cola (§8, paso 2).
//
// Se modela como un canal de tokens vacíos, que es el semáforo canónico de Go:
// un canal vacío significa que toda la capacidad está libre y uno lleno que se
// ha agotado.
type WorkerPool struct {
	slots chan struct{}
}

// NewWorkerPool devuelve un pool capaz de sostener capacity extracciones a la
// vez.
func NewWorkerPool(capacity int) *WorkerPool {
	return &WorkerPool{slots: make(chan struct{}, capacity)}
}

// Acquire toma un hueco si queda alguno libre. Nunca espera: devuelve false en
// cuanto la capacidad está agotada, para que el llamante rechace la petición.
func (p *WorkerPool) Acquire() bool {
	select {
	case p.slots <- struct{}{}:
		return true
	default:
		return false
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
