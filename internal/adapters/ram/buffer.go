//go:build linux

package ram

import (
	"io"
	"os"

	"golang.org/x/sys/unix"

	"github.com/Zindorg/pdf-extractext-extractor/internal/domain"
)

// Store implements domain.DocumentStore backed by an anonymous kernel memfd[cite: 1, 2].
type Store struct{}

// NewStore returns a Linux memfd-backed DocumentStore.
func NewStore() domain.DocumentStore {
	return Store{}
}

type storedDoc struct {
	MemfdSource
}

func (d storedDoc) Close() error {
	return d.file.Close()
}

// Save streams r directly into a memfd descriptor without buffering on the Go heap.
func (Store) Save(r io.Reader) (domain.StoredDocument, error) {
	f, err := NewBuffer(r)
	if err != nil {
		return nil, err
	}
	return storedDoc{MemfdSource{file: f}}, nil
}

// NewBuffer creates a memfd and copies r into it, returning the rewound file[cite: 2].
func NewBuffer(r io.Reader) (*os.File, error) {
	descriptor, err := unix.MemfdCreate("pdf_buffer", 0)
	if err != nil {
		return nil, err
	}

	file := os.NewFile(uintptr(descriptor), "pdf_buffer")
	if _, err := io.Copy(file, r); err != nil {
		_ = file.Close()
		return nil, err
	}

	if _, err := file.Seek(0, io.SeekStart); err != nil {
		_ = file.Close()
		return nil, err
	}

	return file, nil
}

// MemfdSource makes a memfd re-readable across multiple passes[cite: 2].
type MemfdSource struct {
	file *os.File
}

func (m MemfdSource) Open() (io.ReadCloser, error) {
	if _, err := m.file.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}

	dup, err := unix.Dup(int(m.file.Fd()))
	if err != nil {
		return nil, err
	}

	return os.NewFile(uintptr(dup), m.file.Name()), nil
}

// Source wraps a memfd in a re-readable DocumentSource[cite: 2].
func Source(file *os.File) domain.DocumentSource {
	return MemfdSource{file: file}
}
