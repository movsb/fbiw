package helpers

import (
	"slices"
	"testing"
)

func valuesForward[T any](l *List[T]) []T {
	values := make([]T, 0, l.Len())
	for e := l.Front(); e != nil; e = e.Next() {
		values = append(values, e.Value)
	}
	return values
}

func valuesBackward[T any](l *List[T]) []T {
	values := make([]T, 0, l.Len())
	for e := l.Back(); e != nil; e = e.Prev() {
		values = append(values, e.Value)
	}
	return values
}

func TestListZeroValue(t *testing.T) {
	var l List[int]
	if l.Len() != 0 || l.Front() != nil || l.Back() != nil {
		t.Fatalf("zero List is not empty: len=%d front=%v back=%v", l.Len(), l.Front(), l.Back())
	}
}

func TestListPushAndTraverse(t *testing.T) {
	var l List[int]
	two := l.PushFront(2)
	one := l.PushFront(1)
	three := l.PushBack(3)
	four := l.PushBack(4)

	if one == nil || two == nil || three == nil || four == nil {
		t.Fatal("push returned nil")
	}
	if l.Len() != 4 {
		t.Fatalf("Len() = %d, want 4", l.Len())
	}
	if got, want := valuesForward(&l), []int{1, 2, 3, 4}; !slices.Equal(got, want) {
		t.Fatalf("forward values = %v, want %v", got, want)
	}
	if got, want := valuesBackward(&l), []int{4, 3, 2, 1}; !slices.Equal(got, want) {
		t.Fatalf("backward values = %v, want %v", got, want)
	}
}

func TestListRemove(t *testing.T) {
	var l List[string]
	a := l.PushBack("a")
	b := l.PushBack("b")
	c := l.PushBack("c")

	if got := l.Remove(b); got != "b" {
		t.Fatalf("Remove(b) = %q, want b", got)
	}
	if b.Next() != nil || b.Prev() != nil {
		t.Fatal("removed element still links to the list")
	}
	if got := l.Remove(a); got != "a" {
		t.Fatalf("Remove(a) = %q, want a", got)
	}
	if got := l.Remove(c); got != "c" {
		t.Fatalf("Remove(c) = %q, want c", got)
	}
	if l.Len() != 0 || l.Front() != nil || l.Back() != nil {
		t.Fatalf("list is not empty after removals: len=%d", l.Len())
	}
	if got := l.Remove(c); got != "" {
		t.Fatalf("second Remove(c) = %q, want zero value", got)
	}
}

func TestListMove(t *testing.T) {
	var l List[int]
	one := l.PushBack(1)
	two := l.PushBack(2)
	three := l.PushBack(3)

	l.MoveToFront(three)
	if got, want := valuesForward(&l), []int{3, 1, 2}; !slices.Equal(got, want) {
		t.Fatalf("after MoveToFront: got %v, want %v", got, want)
	}
	l.MoveToBack(one)
	if got, want := valuesForward(&l), []int{3, 2, 1}; !slices.Equal(got, want) {
		t.Fatalf("after MoveToBack: got %v, want %v", got, want)
	}
	l.MoveToFront(three)
	l.MoveToBack(one)
	if got, want := valuesForward(&l), []int{3, 2, 1}; !slices.Equal(got, want) {
		t.Fatalf("moving endpoints changed order: got %v, want %v", got, want)
	}
	if l.Len() != 3 || two.Prev() != three || two.Next() != one {
		t.Fatal("move corrupted links or length")
	}
}

func TestListRejectsForeignElements(t *testing.T) {
	var first, second List[int]
	member := first.PushBack(1)
	foreign := second.PushBack(2)

	if got := first.Remove(foreign); got != 0 {
		t.Fatalf("Remove(foreign) = %d, want zero value", got)
	}
	first.MoveToFront(foreign)
	first.MoveToBack(foreign)

	removed := first.PushBack(4)
	first.Remove(removed)
	first.MoveToFront(removed)
	first.MoveToBack(removed)

	if first.Len() != 1 || first.Front() != member || first.Back() != member {
		t.Fatal("foreign or removed element operation changed the list")
	}
}
