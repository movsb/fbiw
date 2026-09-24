package helpers

type Element[T any] struct {
	prev, next *Element[T]
	list       *List[T]
	Value      T
}

func (e *Element[T]) Next() *Element[T] {
	return e.next
}

func (e *Element[T]) Prev() *Element[T] {
	return e.prev
}

// List is a doubly linked list. Its zero value is ready to use.
// A List must not be copied after first use.
type List[T any] struct {
	head, tail *Element[T]
	len        int
}

func (l *List[T]) Len() int {
	return l.len
}

func (l *List[T]) Front() *Element[T] {
	return l.head
}

func (l *List[T]) Back() *Element[T] {
	return l.tail
}

func (l *List[T]) PushFront(value T) *Element[T] {
	e := &Element[T]{Value: value}
	l.insertBefore(e, l.head)
	return e
}

func (l *List[T]) PushBack(value T) *Element[T] {
	e := &Element[T]{Value: value}
	l.insertAfter(e, l.tail)
	return e
}

// Remove removes e from l and returns e's value.
// If e is not an element of l, Remove leaves l unchanged and returns the zero value of T.
func (l *List[T]) Remove(e *Element[T]) (value T) {
	if e == nil || e.list != l {
		return value
	}

	if e.prev == nil {
		l.head = e.next
	} else {
		e.prev.next = e.next
	}
	if e.next == nil {
		l.tail = e.prev
	} else {
		e.next.prev = e.prev
	}
	l.len--

	value = e.Value
	e.prev = nil
	e.next = nil
	e.list = nil
	return value
}

// MoveToFront moves e to the front of l.
// If e is not an element of l, MoveToFront leaves l unchanged.
func (l *List[T]) MoveToFront(e *Element[T]) {
	if e == nil || e.list != l || e == l.head {
		return
	}
	l.unlink(e)
	e.prev = nil
	e.next = l.head
	l.head.prev = e
	l.head = e
}

// MoveToBack moves e to the back of l.
// If e is not an element of l, MoveToBack leaves l unchanged.
func (l *List[T]) MoveToBack(e *Element[T]) {
	if e == nil || e.list != l || e == l.tail {
		return
	}
	l.unlink(e)
	e.prev = l.tail
	e.next = nil
	l.tail.next = e
	l.tail = e
}

func (l *List[T]) insertBefore(e, mark *Element[T]) {
	e.list = l
	e.next = mark
	if mark == nil {
		l.head = e
		l.tail = e
	} else {
		e.prev = mark.prev
		mark.prev = e
		if e.prev == nil {
			l.head = e
		} else {
			e.prev.next = e
		}
	}
	l.len++
}

func (l *List[T]) insertAfter(e, mark *Element[T]) {
	e.list = l
	e.prev = mark
	if mark == nil {
		l.head = e
		l.tail = e
	} else {
		e.next = mark.next
		mark.next = e
		if e.next == nil {
			l.tail = e
		} else {
			e.next.prev = e
		}
	}
	l.len++
}

func (l *List[T]) unlink(e *Element[T]) {
	if e.prev == nil {
		l.head = e.next
	} else {
		e.prev.next = e.next
	}
	if e.next == nil {
		l.tail = e.prev
	} else {
		e.next.prev = e.prev
	}
}
