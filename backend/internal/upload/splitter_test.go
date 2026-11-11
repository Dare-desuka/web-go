package upload

import (
	"testing"

	"web-go/backend/internal/config"
)

func TestSplitExamples(t *testing.T) {
	cases := []struct {
		name  string
		total int64
		sizes []int64
	}{
		{"700MB", 700_000_000, []int64{700_000_000}},
		{"exactly 900MB decimal", 900_000_000, []int64{900_000_000}},
		{"900MB+1", 900_000_001, []int64{900_000_000, 1}},
		{"1.5GB", 1_500_000_000, []int64{900_000_000, 600_000_000}},
		{"2.5GB", 2_500_000_000, []int64{900_000_000, 900_000_000, 700_000_000}},
		{"4GB", 4_000_000_000, []int64{900_000_000, 900_000_000, 900_000_000, 900_000_000, 400_000_000}},
		{"one byte", 1, []int64{1}},
	}
	for _, c := range cases {
		plan, err := Split(c.total, 900_000_000)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if len(plan) != len(c.sizes) {
			t.Fatalf("%s: %d parts, want %d", c.name, len(plan), len(c.sizes))
		}
		var offset int64
		for i, p := range plan {
			if p.PartNumber != int64(i+1) || p.Size != c.sizes[i] ||
				p.StartOffset != offset || p.EndOffset != offset+p.Size {
				t.Fatalf("%s part %d: %+v", c.name, i+1, p)
			}
			offset = p.EndOffset
		}
		if offset != c.total {
			t.Fatalf("%s: last end %d != total %d", c.name, offset, c.total)
		}
	}
}

func TestSplitMiBConstant(t *testing.T) {
	// PRD 28.1: internal constant is 900 MiB in bytes, whatever the UI label says.
	if config.DefaultMaxPartSize != 943718400 {
		t.Fatalf("DefaultMaxPartSize = %d, want 943718400", config.DefaultMaxPartSize)
	}
	plan, err := Split(943718400, config.DefaultMaxPartSize)
	if err != nil || len(plan) != 1 {
		t.Fatalf("plan=%v err=%v", plan, err)
	}
	plan, err = Split(943718401, config.DefaultMaxPartSize)
	if err != nil || len(plan) != 2 || plan[1].Size != 1 {
		t.Fatalf("plan=%v err=%v", plan, err)
	}
}

func TestSplitRejectsBadInput(t *testing.T) {
	for _, tc := range [][2]int64{{0, 100}, {-5, 100}, {100, 0}, {100, -1}} {
		if _, err := Split(tc[0], tc[1]); err == nil {
			t.Fatalf("Split(%d,%d) accepted", tc[0], tc[1])
		}
	}
}

func TestValidate(t *testing.T) {
	const maxMovie = int64(21474836480)
	if err := Validate(1000, "video/mp4", maxMovie); err != nil {
		t.Fatalf("valid rejected: %v", err)
	}
	if err := Validate(1000, "video/mp4; codecs=avc1", maxMovie); err != nil {
		t.Fatalf("mime with params rejected: %v", err)
	}
	for name, tc := range map[string]struct {
		size int64
		mime string
	}{
		"empty":   {0, "video/mp4"},
		"too big": {maxMovie + 1, "video/mp4"},
		"no mime": {1000, ""},
		"image":   {1000, "image/png"},
		"text":    {1000, "text/plain"},
	} {
		if err := Validate(tc.size, tc.mime, maxMovie); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}
