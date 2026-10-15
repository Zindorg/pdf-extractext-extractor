//go:build linux

// Package ram materializes the incoming document in an anonymous kernel buffer
// (memfd), with no filesystem path (§7 and §8 of the architecture): the body
// goes from socket to RAM via io.Copy and never touches the Go heap; the
// descriptor dies with the request when the handler closes it.
package ram

import (
	"io"
	"os"

	"golang.org/x/sys/unix"

	"github.com/Zindorg/pdf-extractext-extractor/internal/domain"
)

// NewBuffer creates a memfd and copies r into it, returning the file
// rewound to the start, ready for re-reading.
//
// If the copy fails, the descriptor is closed before returning and NewBuffer
// returns the error that caused it — e.g. *http.MaxBytesError when the
// body exceeds the handler's limit.
func NewBuffer(r io.Reader) (*os.File, error) {
	descriptor, err := unix.MemfdCreate("pdf_buffer", 0)
	if err != nil {
		return nil, err
	}

	file := os.NewFile(uintptr(descriptor), "pdf_buffer")
	if _, err := io.Copy(file, r); err != nil {
		file.Close()
		return nil, err
	}

	if _, err := file.Seek(0, io.SeekStart); err != nil {
		file.Close()
		return nil, err
	}

	return file, nil
}

// MemfdSource makes a memfd re-readable for multiple poppler passes: each
// Open() rewinds the base and returns a duplicated descriptor.
//
// The duplicate is essential: poppler closes the reader it is given
// (internal/adapters/poppler), and dup shares the file offset of the
// file description — that's why Seek precedes Dup and stays on the base,
// which is the one the handler closes with defer file.Close().
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

// Source wraps a memfd in a re-readable DocumentSource. The *os.File remains
// owned by the caller, who must close it when the request ends.
func Source(file *os.File) domain.DocumentSource {
	return MemfdSource{file: file}
}
