package queue

import (
	"container/heap"
	"sync"
	"time"
)

// PriorityQueue is a priority queue that supports adding, removing, and updating elements with priorities.
type PriorityQueue[T comparable] struct {
	items    map[T]*Entry[T]
	maxHeap  *entryHeap[T]
	minHeap  *entryHeap[T]
	capacity int
	sync.RWMutex
	done   chan struct{}
	wg     sync.WaitGroup
	closed bool
}

// New constructs a priority queue.
func New[T comparable](capacity int, cleanupInterval time.Duration) *PriorityQueue[T] {
	if capacity <= 0 || cleanupInterval <= 0 {
		return nil
	}

	pq := &PriorityQueue[T]{
		items:    make(map[T]*Entry[T], capacity),
		maxHeap:  &entryHeap[T]{isMax: true, entries: make([]*Entry[T], 0, capacity)},
		minHeap:  &entryHeap[T]{isMax: false, entries: make([]*Entry[T], 0, capacity)},
		capacity: capacity,
		done:     make(chan struct{}),
	}

	pq.wg.Add(1) // Start a goroutine to clean up expired entries periodically.
	go pq.cleanupLoop(cleanupInterval)

	return pq
}

// cleanupLoop periodically cleans up expired entries.
func (pq *PriorityQueue[T]) cleanupLoop(interval time.Duration) {
	defer pq.wg.Done()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			pq.Cleanup()
		case <-pq.done:
			return
		}
	}
}

// Push adds a value to the queue with a priority and TTL.
func (pq *PriorityQueue[T]) Push(value T, priority int, ttl time.Duration) {
	pq.Lock()
	defer pq.Unlock()

	if pq.closed {
		return
	}

	expireAt := time.Now().Add(ttl)
	if existingEntry, ok := pq.items[value]; ok {
		pq.updateExistingEntry(existingEntry, priority, expireAt)
		return
	}

	entry := &Entry[T]{
		Value:    value,
		Priority: priority,
		expireAt: expireAt,
	}

	if len(pq.items) >= pq.capacity {
		if !pq.removeLowestIfHigher(entry) {
			return
		}
	}

	pq.addNewEntry(entry)
}

// Pop returns the highest priority entry and removes it from the queue.
func (pq *PriorityQueue[T]) Pop() (value T) {
	pq.Lock()
	defer pq.Unlock()

	if pq.closed || pq.maxHeap.Len() == 0 {
		return value
	}

	now := time.Now()
	for pq.maxHeap.Len() > 0 {
		entry := pq.maxHeap.entries[0]
		if entry.expireAt.After(now) {
			pq.removeHighestPriorityEntry()
			return entry.Value
		}
		// Remove expired entry
		pq.removeHighestPriorityEntry()
	}

	return value
}

// Peek returns the highest priority entry without removing it.
func (pq *PriorityQueue[T]) Peek() (value T) {
	pq.Lock()
	defer pq.Unlock()

	if pq.closed || pq.maxHeap.Len() == 0 {
		return value
	}
	now := time.Now()
	for pq.maxHeap.Len() > 0 {
		entry := pq.maxHeap.entries[0]
		if entry.expireAt.After(now) {
			return entry.Value
		}
		// Remove expired entry
		pq.removeHighestPriorityEntry()
	}

	return value
}

// Elems returns all elements in the queue sorted by priority from high to low.
func (pq *PriorityQueue[T]) Elems() []T {
	pq.RLock()
	defer pq.RUnlock()

	if pq.closed || pq.maxHeap.Len() == 0 {
		return nil
	}
	rs := make([]T, 0, len(pq.maxHeap.entries))
	tempHeap := entryHeap[T]{entries: make([]*Entry[T], len(pq.maxHeap.entries)), isMax: true}

	for i, entry := range pq.maxHeap.entries {
		tempHeap.entries[i] = &Entry[T]{
			Value:    entry.Value,
			Priority: entry.Priority,
			expireAt: entry.expireAt,
			maxIndex: entry.maxIndex,
		}
	}

	heap.Init(&tempHeap)

	for tempHeap.Len() > 0 {
		entry := heap.Pop(&tempHeap).(*Entry[T])
		if !entry.isExpired() {
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

		// Remove the entry from the heaps
		pq.removeFromMaxHeap(entry)
		pq.removeFromMinHeap(entry)
	}
}

// Close stops the cleanup goroutine.
func (pq *PriorityQueue[T]) Close() {
	pq.Lock()
	if pq.closed {
		pq.Unlock()
		return
	}
	close(pq.done)
	pq.closed = true
	pq.Unlock()

	// Wait for the cleanup loop to finish after releasing the lock
	pq.wg.Wait()
}

// Cleanup removes expired entries from the queue.
func (pq *PriorityQueue[T]) Cleanup() {
	pq.Lock()
	defer pq.Unlock()

	if pq.closed {
		return
	}

	now := time.Now()
	maxCleanup := 1000 // Set a reasonable maximum number of items to clean up
	cleaned := 0

	for pq.minHeap.Len() > 0 && pq.minHeap.entries[0].expireAt.Before(now) && cleaned < maxCleanup {
		entry := heap.Pop(pq.minHeap).(*Entry[T])
		heap.Remove(pq.maxHeap, entry.maxIndex)
		delete(pq.items, entry.Value)
		cleaned++
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

	return len(pq.maxHeap.entries)
}

// Empty returns true if the queue is empty.
func (pq *PriorityQueue[T]) Empty() bool {
	pq.RLock()
	defer pq.RUnlock()

	return len(pq.maxHeap.entries) == 0
}

// updateExistingEntry updates an existing entry.
func (pq *PriorityQueue[T]) updateExistingEntry(entry *Entry[T], priority int, expireAt time.Time) {
	if entry.Priority != priority || entry.expireAt != expireAt {
		entry.Priority = priority
		entry.expireAt = expireAt
		heap.Fix(pq.maxHeap, entry.maxIndex)
		heap.Fix(pq.minHeap, entry.minIndex)
	}
}

func (pq *PriorityQueue[T]) removeHighestPriorityEntry() {
	entry := heap.Pop(pq.maxHeap).(*Entry[T])
	heap.Remove(pq.minHeap, entry.minIndex)
	delete(pq.items, entry.Value)
}

// tryReplaceLowestPriority attempts to replace the lowest priority entry.
func (pq *PriorityQueue[T]) removeLowestIfHigher(newEntry *Entry[T]) bool {
	if len(pq.minHeap.entries) > 0 && pq.minHeap.entries[0].Priority < newEntry.Priority {
		removed := heap.Pop(pq.minHeap).(*Entry[T])
		pq.removeFromMaxHeap(removed)
		delete(pq.items, removed.Value)
		return true
	}
	return false
}

// addNewEntry adds a new entry to the queue.
func (pq *PriorityQueue[T]) addNewEntry(entry *Entry[T]) {
	heap.Push(pq.maxHeap, entry)
	heap.Push(pq.minHeap, entry)
	pq.items[entry.Value] = entry
}

// Entry is a pair of region and its priority.
type Entry[T any] struct {
	Priority int
	Value    T
	expireAt time.Time
	maxIndex int // The index of the item in the max heap.
	minIndex int // The index of the item in the min heap.
}

// isExpired checks if the entry is expired.
func (e *Entry[T]) isExpired() bool {
	return e.expireAt.Before(time.Now())
}

type entryHeap[T comparable] struct {
	entries []*Entry[T]
	isMax   bool
}

func (h entryHeap[T]) Len() int { return len(h.entries) }

func (h entryHeap[T]) Less(i, j int) bool {
	if h.isMax {
		return h.entries[i].Priority > h.entries[j].Priority
	}
	return h.entries[i].Priority < h.entries[j].Priority
}
func (h entryHeap[T]) Swap(i, j int) {
	h.entries[i], h.entries[j] = h.entries[j], h.entries[i]
	if h.isMax {
		h.entries[i].maxIndex = i
		h.entries[j].maxIndex = j
	} else {
		h.entries[i].minIndex = i
		h.entries[j].minIndex = j
	}
}

func (h *entryHeap[T]) Push(x interface{}) {
	n := len(h.entries)
	item := x.(*Entry[T])
	if h.isMax {
		item.maxIndex = n
	} else {
		item.minIndex = n
	}
	h.entries = append(h.entries, item)
}

func (h *entryHeap[T]) Pop() interface{} {
	old := h.entries
	n := len(old)
	item := old[n-1]
	old[n-1] = nil
	if h.isMax {
		item.maxIndex = -1
	} else {
		item.minIndex = -1
	}
	h.entries = old[0 : n-1]
	return item
}

// removeFromMaxHeap removes an entry from the max heap.
func (pq *PriorityQueue[T]) removeFromMaxHeap(entry *Entry[T]) {
	if entry.maxIndex >= 0 && entry.maxIndex < len(pq.maxHeap.entries) {
		heap.Remove(pq.maxHeap, entry.maxIndex)
	}
}

// removeFromMinHeap removes an entry from the min heap.
func (pq *PriorityQueue[T]) removeFromMinHeap(entry *Entry[T]) {
	if entry.minIndex >= 0 && entry.minIndex < len(pq.minHeap.entries) {
		heap.Remove(pq.minHeap, entry.minIndex)
	}
}
