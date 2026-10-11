package inmem

import (
	"slices"
	"sync/atomic"
)

// A List is an append-only list. The zero value is an empty list that is ready to use. It is safe for concurrent use.
//
// The list is copied on every Add, which makes a reader's view immutable and costs time in proportion to the length of
// the list: it is for the few dozen entries of a test, and not for a hot path.
type List[T any] struct {
	items atomic.Pointer[[]T]
}

// Add appends v.
func (l *List[T]) Add(v T) {
	for {
		old := l.items.Load()
		var current []T
		if old != nil {
			current = *old
		}
		// The copy keeps a reader who holds the old list from seeing the new entry appear.
		next := append(slices.Clone(current), v)
		if l.items.CompareAndSwap(old, &next) {
			return
		}
	}
}

// Len returns the number of entries.
func (l *List[T]) Len() int {
	if items := l.items.Load(); items != nil {
		return len(*items)
	}
	return 0
}

// All returns the entries added so far, in order. The caller must not change the slice.
func (l *List[T]) All() []T {
	if items := l.items.Load(); items != nil {
		return *items
	}
	return nil
}
