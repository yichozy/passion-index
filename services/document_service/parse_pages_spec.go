package document_service

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// PAGES_SPEC_MAX caps total page expansion so "1-99999" can't blow up
// the sections query.
const PAGES_SPEC_MAX = 50

// ParsePagesSpec parses a pages spec into a sorted, de-duplicated list.
// Accepted forms (whitespace tolerated): "5", "3,7,10", "5-10".
// Pages are 1-based. Errors on: empty spec, non-numbers, N > M ranges,
// non-positive pages, and expansion beyond PAGES_SPEC_MAX.
//
// Shared by the sections handler and the chat tool layer — two call
// sites, hence a function.
func ParsePagesSpec(spec string) ([]int, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil, fmt.Errorf("empty pages spec")
	}
	seen := map[int]bool{}
	var pages []int
	for _, part := range strings.Split(spec, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, fmt.Errorf("empty page in spec %q", spec)
		}
		if start_text, end_text, is_range := strings.Cut(part, "-"); is_range {
			start, err := strconv.Atoi(strings.TrimSpace(start_text))
			if err != nil {
				return nil, fmt.Errorf("bad range start in %q", part)
			}
			end, err := strconv.Atoi(strings.TrimSpace(end_text))
			if err != nil {
				return nil, fmt.Errorf("bad range end in %q", part)
			}
			if start < 1 || end < start {
				return nil, fmt.Errorf("bad range %q (pages are 1-based, start ≤ end)", part)
			}
			for page := start; page <= end; page++ {
				if !seen[page] {
					seen[page] = true
					pages = append(pages, page)
				}
			}
			continue
		}
		page, err := strconv.Atoi(part)
		if err != nil {
			return nil, fmt.Errorf("bad page %q", part)
		}
		if page < 1 {
			return nil, fmt.Errorf("page %d out of range (pages are 1-based)", page)
		}
		if !seen[page] {
			seen[page] = true
			pages = append(pages, page)
		}
	}
	if len(pages) > PAGES_SPEC_MAX {
		return nil, fmt.Errorf("pages spec expands to %d pages, max %d", len(pages), PAGES_SPEC_MAX)
	}
	sort.Ints(pages)
	return pages, nil
}
