package database

import (
	"testing"
)

func TestPublishMovieUnit(t *testing.T) {
	db, ctx := openTestDB(t)
	id, err := CreateMovie(ctx, db, Movie{Title: "P", Slug: "p", VideoSize: 1000, VideoMime: "video/mp4"})
	if err != nil {
		t.Fatal(err)
	}
	// Unpublish path first needs READY; publish validates parts.
	if err := PublishMovie(ctx, db, id); err == nil {
		t.Fatal("published without parts")
	}
	now := NowMillis()
	if err := CreateParts(ctx, db, id, []PlanPart{
		{PartNumber: 1, Size: 1000, StartOffset: 0, EndOffset: 1000},
	}, now); err != nil {
		t.Fatal(err)
	}
	parts, _ := ListParts(ctx, db, id)
	_ = MarkPartUploading(ctx, db, parts[0].ID)
	_ = MarkPartComplete(ctx, db, parts[0].ID, 1, "f")
	if err := PublishMovie(ctx, db, id); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if err := PublishMovie(ctx, db, id); err != ErrInvalidTransition {
		t.Fatalf("republish err = %v", err)
	}
	// READY -> DRAFT unpublish is a legal transition now.
	if err := ValidateTransition(StatusREADY, StatusDRAFT); err != nil {
		t.Fatalf("unpublish transition: %v", err)
	}
	if err := SetMovieStatus(ctx, db, id, StatusDRAFT); err != nil {
		t.Fatal(err)
	}
}

func TestListAllAndUpdateSource(t *testing.T) {
	db, ctx := openTestDB(t)
	if _, err := CreateMovie(ctx, db, Movie{Title: "A", Slug: "a", VideoSize: 1, VideoMime: "video/mp4"}); err != nil {
		t.Fatal(err)
	}
	id, err := CreateMovie(ctx, db, Movie{Title: "B", Slug: "b", VideoSize: 1, VideoMime: "video/mp4", Status: StatusDRAFT})
	if err != nil {
		t.Fatal(err)
	}
	all, err := ListAll(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 || all[0].Slug != "b" {
		t.Fatalf("%+v", all)
	}
	if err := UpdateMovieSource(ctx, db, id, 2500, "video/x-matroska"); err != nil {
		t.Fatal(err)
	}
	m, _ := GetMovieByID(ctx, db, id)
	if m.VideoSize != 2500 || m.VideoMime != "video/x-matroska" {
		t.Fatalf("%+v", m)
	}
	if err := UpdateMovieSource(ctx, db, 999999, 1, "video/mp4"); err != ErrNotFound {
		t.Fatalf("err = %v", err)
	}
}
