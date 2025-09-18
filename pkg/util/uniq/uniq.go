package uniq

import (
	"cmp"
	"slices"
)

// UniqueSorted returns a sorted, de-duplicated copy of the input slice.
// Order is ascending according to the natural ordering of T.
// The input slice is not modified.
func UniqueSorted[T cmp.Ordered](in []T) []T {
	out := make([]T, len(in))
	copy(out, in)
	slices.Sort(out)
	return slices.Compact(out)
}
