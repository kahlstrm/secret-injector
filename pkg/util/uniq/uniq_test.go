package uniq

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestUniqueSorted_Ints(t *testing.T) {
	in := []int{5, 1, 3, 3, 2, 5, 4, 1}
	out := UniqueSorted(in)
	assert.Equal(t, []int{1, 2, 3, 4, 5}, out)
	// Ensure input slice not modified
	assert.Equal(t, []int{5, 1, 3, 3, 2, 5, 4, 1}, in)
}

func TestUniqueSorted_Strings(t *testing.T) {
	in := []string{"b", "a", "c", "b", "a"}
	out := UniqueSorted(in)
	assert.Equal(t, []string{"a", "b", "c"}, out)
}

func TestUniqueSorted_Empty(t *testing.T) {
	var in []string
	out := UniqueSorted(in)
	assert.Len(t, out, 0)
}
