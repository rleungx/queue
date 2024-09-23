package queue

import (
	"container/heap"
	"sync"
	"time"
)

// PriorityQueue queue has priority and preempt.
type PriorityQueue[T comparable] struct {
	items    map[T]*Entry[T]
	entries  entryHeap[T]
	capacity int
	minIndex *int
	sync.RWMutex
}

// NewPriorityQueue constructs a priority queue.
func NewPriorityQueue[T comparable](capacity int, cleanupInterval time.Duration) *PriorityQueue[T] {
	if capacity <= 0 {
		return nil
	}

	pq := &PriorityQueue[T]{
		items:    make(map[T]*Entry[T]),
		entries:  make(entryHeap[T], 0, capacity),
		capacity: capacity,
		minIndex: new(int),
	}
	*pq.minIndex = -1

	// Start a goroutine to clean up expired entries periodically.
	go func() {
		ticker := time.NewTicker(cleanupInterval)
		defer ticker.Stop()

		for range ticker.C {
			pq.Cleanup()
		}
	}()

	return pq
}

func (pq *PriorityQueue[T]) Push(value T, priority int, ttl time.Duration) {
	entry := &Entry[T]{
		Value:    value,
		Priority: priority,
		expireAt: time.Now().Add(ttl),
		index:    new(int),
	}

	pq.Lock()
	defer pq.Unlock()

	if _, ok := pq.items[value]; ok {
		// If the value already exists in the queue, update its priority.
		pq.items[value].Priority = priority
		pq.items[value].expireAt = time.Now().Add(ttl)

		return
	}

	if len(pq.entries) >= pq.capacity {
		// If the new entry has a lower priority than the lowest priority in the heap, do not add it
		if pq.entries[*pq.minIndex].Priority > priority {
			return
		}

		// Remove the lowest priority element
		removed, ok := heap.Remove(&pq.entries, *pq.minIndex).(*Entry[T])
		if !ok {
			return
		}

		delete(pq.items, removed.Value)
	}

	heap.Push(&pq.entries, entry)
	pq.items[value] = entry

	// Update the lowest index.
	if *pq.minIndex == -1 || pq.entries[*pq.minIndex].Priority >= priority {
		pq.minIndex = entry.index
	}
}

// Pop returns the highest priority entry.
func (pq *PriorityQueue[T]) Pop() (t any) {
	pq.Lock()
	defer pq.Unlock()

	if len(pq.entries) == 0 {
		return t
	}

	entry, ok := heap.Pop(&pq.entries).(*Entry[T])
	if !ok {
		return t
	}

	delete(pq.items, entry.Value)

	return entry.Value
}

// Peek returns the highest priority entry.
func (pq *PriorityQueue[T]) Peek() (t any) {
	pq.RLock()
	defer pq.RUnlock()

	if len(pq.entries) == 0 {
		return t
	}

	return pq.entries[0].Value
}

// Elems returns all elements in the queue sorted by priority from high to low.
func (pq *PriorityQueue[T]) Elems() []T {
	pq.RLock()
	defer pq.RUnlock()

	rs := make([]T, 0, len(pq.entries))
	tempHeap := make(entryHeap[T], len(pq.entries))

	for i, entry := range pq.entries {
		tempHeap[i] = &Entry[T]{
			Value:    entry.Value,
			Priority: entry.Priority,
			expireAt: entry.expireAt,
			index:    new(int),
		}
		*(tempHeap[i].index) = *(entry.index)
	}

	heap.Init(&tempHeap)

	for tempHeap.Len() > 0 {
		entry, ok := heap.Pop(&tempHeap).(*Entry[T])
		if ok && !entry.isExpired() {
			rs = append(rs, entry.Value)
		}
	}

	return rs
}

// Remove removes a value from the queue.
func (pq *PriorityQueue[T]) Remove(value T) {
	pq.Lock()
	defer pq.Unlock()

	if entry, ok := pq.items[value]; ok {
		// Remove the entry from the items map
		delete(pq.items, value)

		// Remove the entry from the heap
		if *entry.index >= 0 && *entry.index < len(pq.entries) {
			heap.Remove(&pq.entries, *entry.index)
		}
	}
}

// Cleanup removes expired entries from the queue.
func (pq *PriorityQueue[T]) Cleanup() {
	pq.Lock()
	defer pq.Unlock()

	now := time.Now()

	for range pq.entries.Len() {
		entry, ok := heap.Pop(&pq.entries).(*Entry[T])
		if !ok {
			continue
		}

		if entry.expireAt.After(now) {
			heap.Push(&pq.entries, entry)

			continue
		}

		delete(pq.items, entry.Value)
	}
}

// Capacity returns the capacity of the queue.
func (pq *PriorityQueue[T]) Capacity() int {
	pq.RLock()
	defer pq.RUnlock()

	return pq.capacity
}

// Size returns the number of elements in the queue.
func (pq *PriorityQueue[T]) Size() int {
	pq.RLock()
	defer pq.RUnlock()

	return len(pq.entries)
}

// Empty returns true if the queue is empty.
func (pq *PriorityQueue[T]) Empty() bool {
	pq.RLock()
	defer pq.RUnlock()

	return len(pq.entries) == 0
}

// Entry is a pair of region and its priority.
type Entry[T any] struct {
	Priority int
	Value    T
	expireAt time.Time
	index    *int // The index of the item in the heap.
}

// isExpired checks if the entry is expired.
func (e *Entry[T]) isExpired() bool {
	return time.Now().After(e.expireAt)
}

// entryHeap is a min-heap of *Entry[T].
type entryHeap[T any] []*Entry[T]

func (h entryHeap[T]) Len() int           { return len(h) }
func (h entryHeap[T]) Less(i, j int) bool { return h[i].Priority > h[j].Priority }
func (h entryHeap[T]) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	*h[i].index = i
	*h[j].index = j
}

func (h *entryHeap[T]) Push(x any) {
	n := len(*h)
	entry, ok := x.(*Entry[T])

	if !ok {
		return
	}

	*entry.index = n
	*h = append(*h, entry)
}

func (h *entryHeap[T]) Pop() any {
	old := *h
	n := len(old)
	entry := old[n-1]
	old[n-1] = nil    // avoid memory leak
	*entry.index = -1 // for safety
	*h = old[0 : n-1]

	return entry
}
