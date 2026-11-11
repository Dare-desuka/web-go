package telegram

import (
	"bytes"
	"context"
	"crypto/rand"
	"io"
	"os"
	"testing"
	"time"

	"web-go/backend/internal/storage"
)

// TestLiveTelegram exercises the real channel end-to-end. It NEVER runs in CI:
// enable explicitly with TELEGRAM_LIVE_TEST=1 and real env credentials.
// The operator runs it themselves; it uploads ~256 KiB, reads it back in two
// ranges, then deletes the message so the channel stays clean.
func TestLiveTelegram(t *testing.T) {
	if os.Getenv("TELEGRAM_LIVE_TEST") != "1" {
		t.Skip("live test disabled (TELEGRAM_LIVE_TEST=1 to enable)")
	}
	cfg, err := ParseConfig(
		os.Getenv("TELEGRAM_API_ID"),
		os.Getenv("TELEGRAM_API_HASH"),
		os.Getenv("TELEGRAM_SESSION"),
		os.Getenv("TELEGRAM_CHANNEL_ID"),
	)
	if err != nil {
		t.Fatalf("bad live config: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	client := NewClient(cfg)
	go func() {
		if err := client.Run(ctx); err != nil {
			t.Logf("client run ended: %v", err)
		}
	}()
	store := NewStorage(client)

	payload := make([]byte, 256*1024)
	if _, err := rand.Read(payload); err != nil {
		t.Fatal(err)
	}
	file, err := store.Upload(ctx, bytes.NewReader(payload), storage.Metadata{
		Name: "live-test.bin", Size: int64(len(payload)), Mime: "application/octet-stream",
	})
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Delete(context.Background(), file); err != nil {
			t.Logf("cleanup delete: %v", err)
		}
	})

	for _, r := range [][2]int64{{0, 100}, {1000, 4096}, {int64(len(payload)) - 10, 10}} {
		rc, err := store.DownloadRange(ctx, file, r[0], r[1])
		if err != nil {
			t.Fatalf("download %v: %v", r, err)
		}
		got, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, payload[r[0]:r[0]+r[1]]) {
			t.Fatalf("range %v mismatch", r)
		}
	}
}
