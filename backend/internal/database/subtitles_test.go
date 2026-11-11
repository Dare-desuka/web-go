package database

import (
	"errors"
	"testing"
)

func TestSubtitles(t *testing.T) {
	db, ctx := openTestDB(t)
	movieID := seedMovie(t, db, ctx, "sub-film", 100)

	idID, err := AddSubtitle(ctx, db, Subtitle{MovieID: movieID, Language: "id", Label: "Indonesia", FilePath: "/s/id.vtt"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := AddSubtitle(ctx, db, Subtitle{MovieID: movieID, Language: "en", Label: "English", FilePath: "/s/en.vtt"}); err != nil {
		t.Fatal(err)
	}
	// ponytail: 0005 allows same-language variants (Indonesia, Indonesia2, ...).
	if _, err := AddSubtitle(ctx, db, Subtitle{MovieID: movieID, Language: "id", Label: "Indonesia2", FilePath: "/s/x.vtt"}); err != nil {
		t.Fatalf("second id track err = %v", err)
	}

	subs, err := ListSubtitles(ctx, db, movieID)
	if err != nil {
		t.Fatal(err)
	}
	if len(subs) != 3 || subs[0].Language != "en" {
		t.Fatalf("got %+v, want [en id id] ordered", subs)
	}

	got, err := GetSubtitleByID(ctx, db, idID)
	if err != nil || got.FilePath != "/s/id.vtt" || got.MovieID != movieID {
		t.Fatalf("got %+v err %v", got, err)
	}
	if _, err := GetSubtitleByID(ctx, db, 999999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing err = %v", err)
	}

	if err := DeleteSubtitle(ctx, db, idID, movieID); err != nil {
		t.Fatal(err)
	}
	if err := DeleteSubtitle(ctx, db, idID, movieID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("double delete err = %v", err)
	}
}

func TestSubtitlesCascadeOnMovieDelete(t *testing.T) {
	db, ctx := openTestDB(t)
	movieID := seedMovie(t, db, ctx, "sub-cascade", 100)
	if _, err := AddSubtitle(ctx, db, Subtitle{MovieID: movieID, Language: "id", Label: "Indonesia", FilePath: "/s/id.vtt"}); err != nil {
		t.Fatal(err)
	}
	if err := DeleteMovie(ctx, db, movieID); err != nil {
		t.Fatal(err)
	}
	subs, err := ListSubtitles(ctx, db, movieID)
	if err != nil {
		t.Fatal(err)
	}
	if len(subs) != 0 {
		t.Fatalf("subtitles survived movie delete: %+v", subs)
	}
}
