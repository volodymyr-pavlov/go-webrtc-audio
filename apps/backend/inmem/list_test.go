package inmem

import (
	"sync"
	"testing"
)

func TestAnEmptyListIsReadyToUse(t *testing.T) {
	var l List[int]
	if l.Len() != 0 || l.All() != nil {
		t.Fatalf("%d entries, %v", l.Len(), l.All())
	}
}

func TestAddKeepsTheOrderAndAViewNeverChanges(t *testing.T) {
	var l List[string]
	l.Add("a")
	view := l.All()
	l.Add("b")
	l.Add("c")
	if got := l.All(); len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Fatalf("%v", got)
	}
	if len(view) != 1 || view[0] != "a" {
		t.Fatalf("a view taken earlier changed: %v", view)
	}
}

func TestManyGoroutinesMayAddAtOnceAndNoneIsLost(t *testing.T) {
	var l List[int]
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				l.Add(g*100 + i)
				_ = l.Len()
				_ = l.All()
			}
		}()
	}
	wg.Wait()
	seen := map[int]bool{}
	for _, v := range l.All() {
		seen[v] = true
	}
	if l.Len() != 800 || len(seen) != 800 {
		t.Fatalf("%d entries, %d different", l.Len(), len(seen))
	}
}
