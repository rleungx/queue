package queue

import (
	"container/heap"
	"sync"
	"time"
)

// PriorityQueue queue has priority and preempt
type PriorityQueue[T comparable] struct {
	items    map[T]*Entry[T]
	entries  entryHeap[T]
	capacity int
	mu       sync.Mutex
}

// NewPriorityQueue constructs a priority queue
func NewPriorityQueue[T comparable](capacity int, cleanupInterval time.Duration) *PriorityQueue[T] {
	if capacity <= 0 {
		return nil
	}

	pq := &PriorityQueue[T]{
		items:    make(map[T]*Entry[T]),
		entries:  make(entryHeap[T], 0, capacity),
		capacity: capacity,
	}

	// Start a goroutine to clean up expired entries periodically
	go func() {
		ticker := time.NewTicker(cleanupInterval)
		defer ticker.Stop()
		for range ticker.C {
			pq.cleanup()
		}
	}()

	return pq
}

func (pq *PriorityQueue[T]) Put(value T, priority int, ttl time.Duration) {
	entry := &Entry[T]{
		Value:    value,
		Priority: priority,
		expireAt: time.Now().Add(ttl),
	}

	pq.mu.Lock()
	defer pq.mu.Unlock()

	heap.Push(&pq.entries, entry)
	pq.items[value] = entry

	if len(pq.entries) > pq.capacity {
		removed := heap.Pop(&pq.entries).(*Entry[T])
		delete(pq.items, removed.Value)
	}
}

// Tail returns the lowest priority entry
func (pq *PriorityQueue[T]) Tail() T {
	pq.mu.Lock()
	defer pq.mu.Unlock()

	if len(pq.entries) == 0 {
		var zero T
		return zero
	}

	return pq.entries[0].Value
}

// Elems returns all elements in the queue sorted by priority from high to low
func (pq *PriorityQueue[T]) Elems() []*Entry[T] {
	pq.mu.Lock()
	defer pq.mu.Unlock()

	rs := make([]*Entry[T], 0, len(pq.entries))
	tempHeap := make(entryHeap[T], len(pq.entries))

	for i, entry := range pq.entries {
		tempHeap[i] = &Entry[T]{
			Value:    entry.Value,
			Priority: entry.Priority,
			expireAt: entry.expireAt,
			index:    entry.index,
		}
	}
	heap.Init(&tempHeap)

	for tempHeap.Len() > 0 {
		entry := heap.Pop(&tempHeap).(*Entry[T])
		if !entry.isExpired() {
			rs = append(rs, entry)
		}
	}

	// Reverse the slice to get elements from high to low priority
	for i, j := 0, len(rs)-1; i < j; i, j = i+1, j-1 {
		rs[i], rs[j] = rs[j], rs[i]
	}

	return rs
}

// Remove removes a value from the queue
func (pq *PriorityQueue[T]) Remove(value T) {
	pq.mu.Lock()
	defer pq.mu.Unlock()

	if entry, ok := pq.items[value]; ok {
		// Remove the entry from the items map
		delete(pq.items, value)

		// Remove the entry from the heap
		if entry.index >= 0 && entry.index < len(pq.entries) {
			heap.Remove(&pq.entries, entry.index)
		}
	}
}

// cleanup removes expired entries from the queue
func (pq *PriorityQueue[T]) cleanup() {
	pq.mu.Lock()
	defer pq.mu.Unlock()

	for _, entry := range pq.items {
		if entry.isExpired() {
			delete(pq.items, entry.Value)
		}
	}

	// Remove expired entries from the heap
	filteredEntries := pq.entries[:0]
	for _, entry := range pq.entries {
		if !entry.isExpired() {
			filteredEntries = append(filteredEntries, entry)
		}
	}
	pq.entries = filteredEntries
	heap.Init(&pq.entries)
}

// Entry is a pair of region and its priority
type Entry[T any] struct {
	Priority int
	Value    T
	expireAt time.Time
	index    int // The index of the item in the heap
}

// isExpired checks if the entry is expired
func (e *Entry[T]) isExpired() bool {
	return time.Now().After(e.expireAt)
}

// entryHeap is a min-heap of *Entry[T]
type entryHeap[T any] []*Entry[T]

func (h entryHeap[T]) Len() int           { return len(h) }
func (h entryHeap[T]) Less(i, j int) bool { return h[i].Priority < h[j].Priority }
func (h entryHeap[T]) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].index = i
	h[j].index = j
}

func (h *entryHeap[T]) Push(x any) {
	n := len(*h)
	entry := x.(*Entry[T])
	entry.index = n
	*h = append(*h, entry)
}

func (h *entryHeap[T]) Pop() any {
	old := *h
	n := len(old)
	entry := old[n-1]
	old[n-1] = nil   // avoid memory leak
	entry.index = -1 // for safety
	*h = old[0 : n-1]
	return entry
}
