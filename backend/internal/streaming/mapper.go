package streaming

import (
	"fmt"

	"web-go/backend/internal/database"
)

// Segment is one contiguous slice of a single part:
// virtual [globalStart, globalStart+Length) == part bytes
// [LocalOffset, LocalOffset+Length).
type Segment struct {
	PartNumber  int64
	LocalOffset int64
	Length      int64
}

// MapRange resolves virtual [start,end] (inclusive) to ordered part segments.
// parts must be ordered by part_number with [start,end) continuity (as
// produced by upload.Split + database.VerifyParts). Gaps or overhangs are
// errors: the handler turns them into 503, never corrupt bytes.
func MapRange(start, end int64, parts []database.Part) ([]Segment, error) {
	if start < 0 || end < start {
		return nil, fmt.Errorf("invalid range")
	}
	var segs []Segment
	for _, p := range parts {
		partLast := p.EndOffset - 1 // inclusive, since end is exclusive
		if end < p.StartOffset || start > partLast {
			continue
		}
		s := max(start, p.StartOffset)
		e := min(end, partLast)
		segs = append(segs, Segment{
			PartNumber:  p.PartNumber,
			LocalOffset: s - p.StartOffset,
			Length:      e - s + 1,
		})
	}
	if len(segs) == 0 {
		return nil, fmt.Errorf("range uncovered")
	}
	// Segments must tile [start,end] exactly in order: any part gap,
	// overlap, or misalignment fails instead of serving corrupt bytes.
	expect := start
	for _, sg := range segs {
		if sg.PartNumber < 1 || sg.PartNumber > int64(len(parts)) {
			return nil, fmt.Errorf("range misaligned at part %d", sg.PartNumber)
		}
		// DB rows are 1..N ordered (UNIQUE + ORDER BY + VerifyParts).
		part := parts[sg.PartNumber-1]
		if part.PartNumber != sg.PartNumber ||
			part.StartOffset+sg.LocalOffset != expect {
			return nil, fmt.Errorf("range misaligned at part %d", sg.PartNumber)
		}
		expect += sg.Length
	}
	if expect != end+1 {
		return nil, fmt.Errorf("range has a gap")
	}
	return segs, nil
}
