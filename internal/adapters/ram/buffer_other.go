//go:build !linux

package ram

import (
	"bytes"
	"io"

	"github.com/Zindorg/pdf-extractext-extractor/internal/domain"
)

// Store provides an in-memory fallback on non-Linux systems for local development and testing.
type Store struct{}

func NewStore() domain.DocumentStore {
	return Store{}
}

type memoryDoc struct {
	data []byte
}

func (m memoryDoc) Open() (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(m.data)), nil
}

func (m memoryDoc) Close() error {
	return nil
}

func (Store) Save(r io.Reader) (domain.StoredDocument, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	return memoryDoc{data: data}, nil
}
