package streaming

import (
	"bytes"
	"context"
	"testing"

	"web-go/backend/internal/database"
	"web-go/backend/internal/storage"
)

func seedStreamParts(t *testing.T, store *storage.Memory, sizes ...int64) ([]database.Part, []byte) {
	t.Helper()
	var data []byte
	var parts []database.Part
	var offset int64
	for i, size := range sizes {
		chunk := make([]byte, size)
		for j := range chunk {
			chunk[j] = byte((offset + int64(j)) % 251)
		}
		data = append(data, chunk...)
		f, err := store.Upload(context.Background(), bytes.NewReader(chunk),
			storage.Metadata{Name: "p", Size: size, Mime: "video/mp4"})
		if err != nil {
			t.Fatal(err)
		}
		fid := f.ID
		parts = append(parts, database.Part{
			ID:             int64(i + 1),
			MovieID:        1,
			PartNumber:     int64(i + 1),
			Size:           size,
			StartOffset:    offset,
			EndOffset:      offset + size,
			TelegramFileID: &fid,
			Status:         database.PartComplete,
		})
		offset += size
	}
	return parts, data
}

func TestStreamCrossPart(t *testing.T) {
	store := storage.NewMemory()
	parts, data := seedStreamParts(t, store, 1000, 1000, 500)

	// Range crossing all three parts.
	segs, err := MapRange(900, 2100, parts)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := Stream(context.Background(), &out, store, parts, segs); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out.Bytes(), data[900:2101]) {
		t.Fatal("cross-part bytes mismatch")
	}
}

func TestStreamFullMovie(t *testing.T) {
	store := storage.NewMemory()
	parts, data := seedStreamParts(t, store, 1000, 1000, 500)

	segs, err := MapRange(0, int64(len(data))-1, parts)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := Stream(context.Background(), &out, store, parts, segs); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out.Bytes(), data) {
		t.Fatal("full movie bytes mismatch")
	}
}

func TestStreamFailsOnMissingFile(t *testing.T) {
	store := storage.NewMemory()
	parts, _ := seedStreamParts(t, store, 100, 100)
	missing := "gone"
	parts[1].TelegramFileID = &missing

	segs, err := MapRange(50, 150, parts)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err = Stream(context.Background(), &out, store, parts, segs)
	if err == nil {
		t.Fatal("missing file accepted")
	}
	// First 50 bytes (part 1) were already written before the failure.
	if out.Len() != 50 {
		t.Fatalf("partial output = %d bytes", out.Len())
	}
}

func TestStreamFailsOnIncompletePart(t *testing.T) {
	store := storage.NewMemory()
	parts, _ := seedStreamParts(t, store, 100)
	parts[0].TelegramFileID = nil

	segs, err := MapRange(0, 99, parts)
	if err != nil {
		t.Fatal(err)
	}
	if err := Stream(context.Background(), &bytes.Buffer{}, store, parts, segs); !IsMissingPart(err) {
		t.Fatalf("err = %v, want missing part", err)
	}
}

func TestStreamRespectsCancel(t *testing.T) {
	store := storage.NewMemory()
	parts, _ := seedStreamParts(t, store, 100, 100)

	segs, err := MapRange(0, 199, parts)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Stream(ctx, &bytes.Buffer{}, store, parts, segs); err == nil {
		t.Fatal("cancelled context accepted")
	}
}
