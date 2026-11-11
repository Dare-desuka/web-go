// Package streaming serves virtual movies as one continuous byte stream.
// Pure pieces (range parsing, part mapping) are isolated for unit testing;
// Stream copies cross-part bytes to any io.Writer without loading the movie.
package streaming

import (
	"fmt"
	"strconv"
	"strings"
)

// ParseRange resolves an HTTP Range header against a virtual movie of size
// bytes. Returns start offset and length. partial=false means "no Range
// header": serve 200 with the full body. Any error means 416 with
// Content-Range: bytes */size. Multi-range is rejected (MVP serves one
// continuous stream, which is all HTML5 video needs).
func ParseRange(header string, size int64) (start, length int64, partial bool, err error) {
	if size <= 0 {
		return 0, 0, false, fmt.Errorf("empty movie")
	}
	header = strings.TrimSpace(header)
	if header == "" {
		return 0, size, false, nil
	}
	if !strings.HasPrefix(strings.ToLower(header), "bytes=") {
		return 0, 0, false, fmt.Errorf("bad unit")
	}
	spec := strings.TrimSpace(header[len("bytes="):])
	if strings.Contains(spec, ",") {
		return 0, 0, false, fmt.Errorf("multi-range not supported")
	}
	// Suffix range: bytes=-N (last N bytes).
	if strings.HasPrefix(spec, "-") {
		n, err := strconv.ParseInt(strings.TrimSpace(spec[1:]), 10, 64)
		if err != nil || n <= 0 {
			return 0, 0, false, fmt.Errorf("bad suffix")
		}
		if n > size {
			n = size
		}
		return size - n, n, true, nil
	}
	lo, hi, hasHi := spec, "", false
	if i := strings.Index(spec, "-"); i >= 0 {
		lo, hi, hasHi = spec[:i], spec[i+1:], true
	}
	start, err = strconv.ParseInt(strings.TrimSpace(lo), 10, 64)
	if err != nil || start < 0 {
		return 0, 0, false, fmt.Errorf("bad start")
	}
	if start >= size {
		return 0, 0, false, fmt.Errorf("start beyond size")
	}
	if !hasHi || strings.TrimSpace(hi) == "" {
		return start, size - start, true, nil
	}
	end, err := strconv.ParseInt(strings.TrimSpace(hi), 10, 64)
	if err != nil || end < start {
		return 0, 0, false, fmt.Errorf("bad end")
	}
	if end >= size {
		end = size - 1 // RFC 7233: clamp, still satisfiable
	}
	return start, end - start + 1, true, nil
}
