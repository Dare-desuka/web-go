package streaming

import (
	"testing"

	"web-go/backend/internal/database"
)

// 2.5 GB decimal plan: P1 [0,900M), P2 [900M,1800M), P3 [1800M,2500M).
func testParts() []database.Part {
	const M = int64(1000000)
	return []database.Part{
		{ID: 1, MovieID: 1, PartNumber: 1, Size: 900 * M, StartOffset: 0, EndOffset: 900 * M, Status: database.PartComplete},
		{ID: 2, MovieID: 1, PartNumber: 2, Size: 900 * M, StartOffset: 900 * M, EndOffset: 1800 * M, Status: database.PartComplete},
		{ID: 3, MovieID: 1, PartNumber: 3, Size: 700 * M, StartOffset: 1800 * M, EndOffset: 2500 * M, Status: database.PartComplete},
	}
}

func TestMapSinglePart(t *testing.T) {
	const M = int64(1000000)
	segs, err := MapRange(100, 200, testParts())
	if err != nil {
		t.Fatal(err)
	}
	if len(segs) != 1 || segs[0] != (Segment{PartNumber: 1, LocalOffset: 100, Length: 101}) {
		t.Fatalf("%+v", segs)
	}

	// Only Part 2: global 950M -> local 50M.
	segs, err = MapRange(950*M, 950*M+100, testParts())
	if err != nil {
		t.Fatal(err)
	}
	if len(segs) != 1 || segs[0] != (Segment{PartNumber: 2, LocalOffset: 50 * M, Length: 101}) {
		t.Fatalf("%+v", segs)
	}
}

func TestMapCrossPart(t *testing.T) {
	const M = int64(1000000)
	// AGENTS example: bytes=850MB-950MB splits at the 900M boundary.
	segs, err := MapRange(850*M, 950*M, testParts())
	if err != nil {
		t.Fatal(err)
	}
	want := []Segment{
		{PartNumber: 1, LocalOffset: 850 * M, Length: 50 * M},
		{PartNumber: 2, LocalOffset: 0, Length: 50*M + 1},
	}
	if len(segs) != 2 || segs[0] != want[0] || segs[1] != want[1] {
		t.Fatalf("%+v", segs)
	}
}

func TestMapBoundaryBytes(t *testing.T) {
	const M = int64(1000000)
	// Last byte of P1 + first byte of P2.
	segs, err := MapRange(900*M-1, 900*M, testParts())
	if err != nil {
		t.Fatal(err)
	}
	if len(segs) != 2 ||
		segs[0] != (Segment{PartNumber: 1, LocalOffset: 900*M - 1, Length: 1}) ||
		segs[1] != (Segment{PartNumber: 2, LocalOffset: 0, Length: 1}) {
		t.Fatalf("%+v", segs)
	}
}

func TestMapCrossThreeParts(t *testing.T) {
	const M = int64(1000000)
	segs, err := MapRange(800*M, 1900*M, testParts())
	if err != nil {
		t.Fatal(err)
	}
	if len(segs) != 3 ||
		segs[0].PartNumber != 1 || segs[0].Length != 100*M ||
		segs[1].PartNumber != 2 || segs[1].Length != 900*M ||
		segs[2].PartNumber != 3 || segs[2].LocalOffset != 0 || segs[2].Length != 100*M+1 {
		t.Fatalf("%+v", segs)
	}
}

func TestMapRejectsGaps(t *testing.T) {
	const M = int64(1000000)
	gapped := []database.Part{
		{ID: 1, PartNumber: 1, Size: 900 * M, StartOffset: 0, EndOffset: 900 * M},
		{ID: 2, PartNumber: 2, Size: 600 * M, StartOffset: 1000 * M, EndOffset: 1600 * M}, // gap!
	}
	if _, err := MapRange(850*M, 950*M, gapped); err == nil {
		t.Fatal("gap accepted")
	}
	if _, err := MapRange(0, 10, nil); err == nil {
		t.Fatal("empty parts accepted")
	}
	if _, err := MapRange(100, 50, testParts()); err == nil {
		t.Fatal("inverted range accepted")
	}
}

func TestParseRange(t *testing.T) {
	const size = int64(2500000000)
	cases := []struct {
		header        string
		start, length int64
		partial       bool
		ok            bool
	}{
		{"", 0, size, false, true},
		{"bytes=0-", 0, size, true, true},
		{"bytes=100-200", 100, 101, true, true},
		{"bytes=500-", 500, size - 500, true, true},
		{"bytes=-500", size - 500, 500, true, true},
		{"bytes=-99999999999", 0, size, true, true},      // suffix larger than file
		{"bytes=2499999999-", 2499999999, 1, true, true}, // last byte
		{"bytes=0-99999999999", 0, size, true, true},     // end clamped
		{"bytes=2500000000-", 0, 0, false, false},        // start == size
		{"bytes=3000000000-4000000000", 0, 0, false, false},
		{"bytes=200-100", 0, 0, false, false},
		{"bytes=0-100,200-300", 0, 0, false, false}, // multi-range rejected
		{"bytes=-0", 0, 0, false, false},
		{"items=0-100", 0, 0, false, false},
		{"bytes=abc-def", 0, 0, false, false},
		{"bytes=", 0, 0, false, false},
		{"  bytes=10-20  ", 10, 11, true, true},
	}
	for _, c := range cases {
		start, length, partial, err := ParseRange(c.header, size)
		if c.ok && (err != nil || start != c.start || length != c.length || partial != c.partial) {
			t.Errorf("%q -> (%d,%d,%v,%v)", c.header, start, length, partial, err)
		}
		if !c.ok && err == nil {
			t.Errorf("%q accepted", c.header)
		}
	}
	if _, _, _, err := ParseRange("bytes=0-", 0); err == nil {
		t.Error("empty movie accepted")
	}
}
