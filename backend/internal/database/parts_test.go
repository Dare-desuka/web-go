package database

import (
	"context"
	"errors"
	"testing"
)

func seedMovie(t *testing.T, db DBTX, ctx context.Context, slug string, size int64) int64 {
	t.Helper()
	id, err := CreateMovie(ctx, db, Movie{Title: slug, Slug: slug, VideoSize: size, VideoMime: "video/mp4"})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestPartsLifecycle(t *testing.T) {
	db, ctx := openTestDB(t)
	movieID := seedMovie(t, db, ctx, "parts", 2500000000)
	now := NowMillis()

	plan := []PlanPart{
		{PartNumber: 1, Size: 900000000, StartOffset: 0, EndOffset: 900000000},
		{PartNumber: 2, Size: 900000000, StartOffset: 900000000, EndOffset: 1800000000},
		{PartNumber: 3, Size: 700000000, StartOffset: 1800000000, EndOffset: 2500000000},
	}
	if err := CreateParts(ctx, db, movieID, plan, now); err != nil {
		t.Fatal(err)
	}

	parts, err := ListParts(ctx, db, movieID)
	if err != nil {
		t.Fatal(err)
	}
	if len(parts) != 3 || parts[0].Status != PartWaiting {
		t.Fatalf("got %+v", parts)
	}
	if err := VerifyParts(parts, 2500000000); err != nil {
		t.Fatalf("valid plan rejected: %v", err)
	}

	// Part 1 completes, part 2 fails, part 3 stays waiting.
	if err := MarkPartUploading(ctx, db, parts[0].ID); err != nil {
		t.Fatal(err)
	}
	if err := MarkPartComplete(ctx, db, parts[0].ID, 111, "file-1"); err != nil {
		t.Fatal(err)
	}
	if err := MarkPartFailed(ctx, db, parts[1].ID, "timeout"); err != nil {
		t.Fatal(err)
	}

	pending, err := PendingParts(ctx, db, movieID)
	if err != nil {
		t.Fatal(err)
	}
	// Resume set = FAILED part 2 + WAITING part 3; COMPLETE part 1 excluded.
	if len(pending) != 2 || pending[0].PartNumber != 2 || pending[1].PartNumber != 3 {
		t.Fatalf("pending = %+v", pending)
	}
	if pending[0].RetryCount != 1 || pending[0].LastError == nil {
		t.Fatalf("failed part lacks retry info: %+v", pending[0])
	}

	// Retry only part 2.
	if err := ResetPartForRetry(ctx, db, parts[1].ID); err != nil {
		t.Fatal(err)
	}
	if err := MarkPartComplete(ctx, db, parts[1].ID, 222, "file-2"); err != nil {
		t.Fatal(err)
	}
	parts, _ = ListParts(ctx, db, movieID)
	if parts[0].TelegramMessageID == nil || *parts[0].TelegramMessageID != 111 {
		t.Fatalf("part 1 locator lost: %+v", parts[0])
	}

	if err := MarkPartComplete(ctx, db, 999999, 1, "x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing part err = %v", err)
	}
}

func TestVerifyPartsRejectsBadPlans(t *testing.T) {
	valid := []Part{
		{PartNumber: 1, Size: 900000000, StartOffset: 0, EndOffset: 900000000},
		{PartNumber: 2, Size: 600000000, StartOffset: 900000000, EndOffset: 1500000000},
	}
	if err := VerifyParts(valid, 1500000000); err != nil {
		t.Fatalf("valid rejected: %v", err)
	}

	cases := map[string]struct {
		parts []Part
		size  int64
	}{
		"empty": {nil, 100},
		"gap": {[]Part{
			{PartNumber: 1, Size: 900000000, StartOffset: 0, EndOffset: 900000000},
			{PartNumber: 2, Size: 600000000, StartOffset: 900000001, EndOffset: 1500000001},
		}, 1500000001},
		"overlap": {[]Part{
			{PartNumber: 1, Size: 900000000, StartOffset: 0, EndOffset: 900000000},
			{PartNumber: 2, Size: 600000000, StartOffset: 899999999, EndOffset: 1499999999},
		}, 1499999999},
		"size mismatch": {[]Part{
			{PartNumber: 1, Size: 5, StartOffset: 0, EndOffset: 900000000},
		}, 900000000},
		"bad numbering": {[]Part{
			{PartNumber: 1, Size: 900000000, StartOffset: 0, EndOffset: 900000000},
			{PartNumber: 3, Size: 600000000, StartOffset: 900000000, EndOffset: 1500000000},
		}, 1500000000},
		"non-zero start": {[]Part{
			{PartNumber: 1, Size: 100, StartOffset: 1, EndOffset: 101},
		}, 101},
		"total mismatch": {[]Part{
			{PartNumber: 1, Size: 900000000, StartOffset: 0, EndOffset: 900000000},
		}, 900000001},
	}
	for name, c := range cases {
		if err := VerifyParts(c.parts, c.size); err == nil {
			t.Errorf("%s: expected error, got nil", name)
		}
	}
}
