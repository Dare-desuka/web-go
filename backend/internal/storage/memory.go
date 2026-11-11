package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"sync"
)

// Memory is an in-memory Storage fake for pipeline/streaming tests.
// Test-scale only: Upload buffers the whole part, so never use it for
// multi-GB data. FailNextUploads/FailNextDownloads inject transient errors
// to exercise retry paths.
type Memory struct {
	mu                sync.Mutex
	files             map[string][]byte
	next              int
	UploadCalls       int
	UploadCallsByName map[string]int
	FailNextUploads   int
	FailNextDownloads int
}

func NewMemory() *Memory {
	return &Memory{files: map[string][]byte{}, UploadCallsByName: map[string]int{}}
}

// Get returns stored bytes for assertions. Empty slice when missing.
func (m *Memory) Get(id string) []byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.files[id]
}

func (m *Memory) Upload(ctx context.Context, r io.Reader, meta Metadata) (File, error) {
	if err := ctx.Err(); err != nil {
		return File{}, err
	}
	m.mu.Lock()
	if m.FailNextUploads > 0 {
		m.FailNextUploads--
		m.mu.Unlock()
		return File{}, errors.New("injected upload failure")
	}
	m.mu.Unlock()

	// ponytail: full buffer is fine here, this fake never sees multi-GB data.
	data, err := io.ReadAll(r)
	if err != nil {
		return File{}, err
	}
	if int64(len(data)) != meta.Size {
		return File{}, fmt.Errorf("size mismatch: got %d, want %d", len(data), meta.Size)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.next++
	id := "mem-" + strconv.Itoa(m.next)
	m.files[id] = data
	m.UploadCalls++
	m.UploadCallsByName[meta.Name]++
	return File{ID: id}, nil
}

func (m *Memory) DownloadRange(_ context.Context, f File, offset, length int64) (io.ReadCloser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.FailNextDownloads > 0 {
		m.FailNextDownloads--
		return nil, errors.New("injected download failure")
	}
	data, ok := m.files[f.ID]
	if !ok {
		return nil, fmt.Errorf("file %q not found", f.ID)
	}
	if offset < 0 || length <= 0 || offset > int64(len(data)) || offset+length > int64(len(data)) {
		return nil, fmt.Errorf("range [%d,%d) out of bounds (size %d)", offset, offset+length, len(data))
	}
	return io.NopCloser(bytes.NewReader(data[offset : offset+length])), nil
}

func (m *Memory) Delete(_ context.Context, f File) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.files, f.ID)
	return nil
}
