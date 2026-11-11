package storage

import (
	"bytes"
	"context"
	"io"
	"testing"
)

func TestMemoryRoundtrip(t *testing.T) {
	m := NewMemory()
	ctx := context.Background()
	f, err := m.Upload(ctx, io.MultiReader(), Metadata{Name: "empty-should-fail", Size: 1, Mime: "video/mp4"})
	if err == nil {
		t.Fatalf("size mismatch accepted: %+v", f)
	}

	payload := []byte("hello-range-world")
	f, err = m.Upload(ctx, bytes.NewReader(payload), Metadata{Name: "p1", Size: int64(len(payload)), Mime: "video/mp4"})
	if err != nil {
		t.Fatal(err)
	}
	rc, err := m.DownloadRange(ctx, f, 6, 5)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(rc)
	rc.Close()
	if string(got) != "range" {
		t.Fatalf("got %q", got)
	}

	for _, tc := range [][2]int64{{-1, 2}, {0, 0}, {0, 100}, {17, 1}, {16, 2}} {
		if _, err := m.DownloadRange(ctx, f, tc[0], tc[1]); err == nil {
			t.Fatalf("range %v accepted", tc)
		}
	}
	if _, err := m.DownloadRange(ctx, File{ID: "nope"}, 0, 1); err == nil {
		t.Fatal("missing file accepted")
	}
	if err := m.Delete(ctx, f); err != nil {
		t.Fatal(err)
	}
	if len(m.Get(f.ID)) != 0 {
		t.Fatal("deleted file still present")
	}
}

func TestMemoryFailureInjection(t *testing.T) {
	m := NewMemory()
	m.FailNextUploads = 1
	ctx := context.Background()
	if _, err := m.Upload(ctx, bytes.NewReader([]byte("x")), Metadata{Name: "a", Size: 1}); err == nil {
		t.Fatal("injected failure not triggered")
	}
	if _, err := m.Upload(ctx, bytes.NewReader([]byte("x")), Metadata{Name: "a", Size: 1}); err != nil {
		t.Fatalf("failure persisted: %v", err)
	}
}
