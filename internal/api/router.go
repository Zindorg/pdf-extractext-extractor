package api

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// NewRouter mounts GET /health, GET /ready, and POST /extract.
func NewRouter(h *ExtractionHandler, ready func() bool) *http.ServeMux {
	if ready == nil {
		ready = func() bool { return true }
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", h.HandleHealth)
	mux.HandleFunc("GET /ready", func(w http.ResponseWriter, _ *http.Request) {
		if !ready() {
			writeContractError(w, http.StatusServiceUnavailable, "", "saturated or draining")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(healthResponse{Status: "ready"})
	})
	mux.HandleFunc("POST /extract", h.HandleExtract)

	return mux
}

// RateLimit enforces a Token Bucket rate limiter per replica, excluding health probes.
func RateLimit(rps int, burst int, next http.Handler) http.Handler {
	if rps <= 0 || burst <= 0 {
		return next
	}

	var mu sync.Mutex
	tokens := float64(burst)
	lastRefill := time.Now()
	rate := float64(rps)
	maxTokens := float64(burst)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" || r.URL.Path == "/ready" {
			next.ServeHTTP(w, r)
			return
		}

		mu.Lock()
		now := time.Now()
		elapsed := now.Sub(lastRefill).Seconds()
		tokens += elapsed * rate
		if tokens > maxTokens {
			tokens = maxTokens
		}
		lastRefill = now

		if tokens < 1.0 {
			mu.Unlock()
			writeContractError(w, http.StatusTooManyRequests, "", "rate limit exceeded")
			return
		}
		tokens -= 1.0
		mu.Unlock()

		next.ServeHTTP(w, r)
	})
}

type idempotentResponse struct {
	status      int
	contentType string
	body        []byte
	expiresAt   time.Time
}

// Idempotency caches 200 OK and 422 responses by Idempotency-Key header for SAGA orchestrator retries.
func Idempotency(ttl time.Duration, next http.Handler) http.Handler {
	if ttl <= 0 {
		return next
	}

	var mu sync.RWMutex
	store := make(map[string]idempotentResponse)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Header.Get("Idempotency-Key")
		if key == "" || r.Method != http.MethodPost {
			next.ServeHTTP(w, r)
			return
		}

		mu.RLock()
		cached, ok := store[key]
		mu.RUnlock()

		if ok && time.Now().Before(cached.expiresAt) {
			w.Header().Set("Content-Type", cached.contentType)
			w.Header().Set("X-Idempotent-Replay", "true")
			w.WriteHeader(cached.status)
			_, _ = w.Write(cached.body)
			return
		}

		rec := &bodyRecorder{
			ResponseWriter: w,
			status:         http.StatusOK,
			buf:            &bytes.Buffer{},
		}
		next.ServeHTTP(rec, r)

		// Solo guardamos resultados determinísticos (200 OK o 422 Unprocessable Entity)
		if rec.status == http.StatusOK || rec.status == http.StatusUnprocessableEntity {
			mu.Lock()
			if len(store) >= 500 {
				now := time.Now()
				for k, v := range store {
					if now.After(v.expiresAt) {
						delete(store, k)
					}
				}
			}
			store[key] = idempotentResponse{
				status:      rec.status,
				contentType: w.Header().Get("Content-Type"),
				body:        rec.buf.Bytes(),
				expiresAt:   time.Now().Add(ttl),
			}
			mu.Unlock()
		}
	})
}

type bodyRecorder struct {
	http.ResponseWriter
	status int
	buf    *bytes.Buffer
}

func (b *bodyRecorder) WriteHeader(c int) {
	b.status = c
	b.ResponseWriter.WriteHeader(c)
}

func (b *bodyRecorder) Write(p []byte) (int, error) {
	_, _ = b.buf.Write(p)
	return b.ResponseWriter.Write(p)
}

// Observe emits structured JSON logs to stdout per request with correlation_id (12-Factor XI).
func Observe(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Correlation-ID")
		if id == "" {
			b := make([]byte, 8)
			_, _ = rand.Read(b)
			id = hex.EncodeToString(b)
		}
		w.Header().Set("X-Correlation-ID", id)
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		start := time.Now()
		next.ServeHTTP(rec, r)
		log.Info("request",
			"correlation_id", id,
			"idempotency_key", r.Header.Get("Idempotency-Key"),
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"duration_ms", time.Since(start).Milliseconds(),
		)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(c int) {
	s.status = c
	s.ResponseWriter.WriteHeader(c)
}
